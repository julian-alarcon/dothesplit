// MailerService renders + enqueues + sends transactional email.
//
// Dispatch is asynchronous: callers always Enqueue (which only writes a row),
// and the worker drains the outbox every minute. This keeps register/login/
// settlement endpoints fast and immune to SMTP outages.
package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

type MailerService struct {
	smtp   *repo.SmtpRepo
	outbox *repo.EmailOutboxRepo
	email  *crypto.EmailCipher
	logger *slog.Logger
}

func NewMailerService(s *repo.SmtpRepo, ob *repo.EmailOutboxRepo, email *crypto.EmailCipher, logger *slog.Logger) *MailerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MailerService{smtp: s, outbox: ob, email: email, logger: logger}
}

// IsConfigured returns true iff the smtp_config row exists with a usable host
// and from_address. Used by the auth flow to decide between "send code" and
// "auto-verify on register".
func (m *MailerService) IsConfigured(ctx context.Context) (bool, error) {
	c, err := m.smtp.Get(ctx)
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(c.Host) != "" && c.Port > 0 && strings.Contains(c.FromAddress, "@"), nil
}

// Enqueue renders a template and persists the row. The recipient address is
// AES-GCM encrypted using the same key as users.email so plaintext addresses
// never sit at rest in this table. May participate in a caller transaction.
func (m *MailerService) Enqueue(ctx context.Context, q repo.Querier, to, template string, vars TemplateVars) error {
	subject, body := renderTemplate(template, vars)
	if subject == "" {
		return fmt.Errorf("unknown email template: %s", template)
	}
	enc, err := m.email.Encrypt(to)
	if err != nil {
		return fmt.Errorf("encrypt recipient: %w", err)
	}
	row := &repo.OutboxRow{
		ToEmailEnc: enc,
		Subject:    subject,
		Body:       body,
		Template:   template,
	}
	return m.outbox.Enqueue(ctx, q, row)
}

func renderTemplate(template string, vars TemplateVars) (string, string) {
	switch template {
	case "verify_register":
		return RenderVerifyRegister(vars)
	case "verify_change_email":
		return RenderVerifyChangeEmail(vars)
	case "welcome":
		return RenderWelcome(vars)
	case "recurring_run":
		return RenderRecurringRun(vars)
	case "settlement_created":
		return RenderSettlementCreated(vars)
	case "group_member_added":
		return RenderGroupMemberAdded(vars)
	case "smtp_test":
		return RenderSmtpTest(vars)
	}
	return "", ""
}

// backoff returns the delay before the next attempt given the attempts that
// have already happened (i.e. attempts==1 means the first retry). Caps out at
// dead-letter once attempts reach the partial-index limit (5) — the worker's
// claim query won't pick those rows up again.
func backoff(attempts int16) time.Duration {
	switch attempts {
	case 1:
		return 1 * time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	case 4:
		return 1 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// DispatchOutbox claims up to `limit` due rows and tries to send them. Marks
// each as sent or schedules a retry. Returns the number of successful sends
// (for logging). When SMTP is unconfigured it returns immediately with no
// claim, so the rows wait until the admin fixes the config.
func (m *MailerService) DispatchOutbox(ctx context.Context, limit int) (int, error) {
	ok, err := m.IsConfigured(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	cfg, err := m.smtp.Get(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := m.outbox.ClaimDue(ctx, limit)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range rows {
		to, err := m.email.Decrypt(row.ToEmailEnc)
		if err != nil {
			_ = m.outbox.MarkFailed(ctx, row.ID, "decrypt_failed", backoff(row.Attempts+1))
			continue
		}
		if err := m.sendOne(ctx, cfg, to, row.Subject, row.Body); err != nil {
			m.logger.Warn("outbox send failed",
				slog.String("template", row.Template),
				slog.String("err", err.Error()),
				slog.Int("attempts", int(row.Attempts)+1))
			_ = m.outbox.MarkFailed(ctx, row.ID, truncErr(err.Error()), backoff(row.Attempts+1))
			continue
		}
		_ = m.outbox.MarkSent(ctx, row.ID)
		sent++
	}
	return sent, nil
}

func truncErr(s string) string {
	const max = 500
	if len(s) > max {
		return s[:max]
	}
	return s
}

// SendNow renders a template and dispatches it synchronously, bypassing the
// outbox. Returns the SMTP error directly so admin UIs (specifically the
// "Send test email" button) can surface a precise failure code instead of
// waiting on the worker's next tick. Most call sites should use Enqueue
// instead — synchronous send blocks the request.
func (m *MailerService) SendNow(ctx context.Context, to, template string, vars TemplateVars) error {
	subject, body := renderTemplate(template, vars)
	if subject == "" {
		return fmt.Errorf("unknown email template: %s", template)
	}
	cfg, err := m.smtp.Get(ctx)
	if err != nil {
		return err
	}
	return m.sendOne(ctx, cfg, to, subject, body)
}

// sendOne actually talks SMTP. Mirrors SmtpService.Test()'s dial logic but
// follows through with MAIL/RCPT/DATA instead of NOOP.
func (m *MailerService) sendOne(ctx context.Context, cfg *repo.SmtpConfig, to, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var conn net.Conn
	var err error
	switch cfg.TLSMode {
	case "tls":
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	default:
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if cfg.TLSMode == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if cfg.Username != nil && *cfg.Username != "" {
		var pass string
		if len(cfg.PasswordEncrypted) > 0 {
			pass, err = m.email.Decrypt(cfg.PasswordEncrypted)
			if err != nil {
				return fmt.Errorf("password decrypt: %w", err)
			}
		}
		auth := smtp.PlainAuth("", *cfg.Username, pass, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	msg := rfc5322Headers(cfg.FromAddress, to, subject, body) + body
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	_ = client.Quit()
	return nil
}
