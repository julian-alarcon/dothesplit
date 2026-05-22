package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

// SmtpConfig is the service-layer projection of repo.SmtpConfig with the
// password ciphertext replaced by a boolean indicator. The plaintext is never
// returned to API callers; only the encrypted bytes ever leave the DB.
type SmtpConfig struct {
	Host        string
	Port        int
	Username    *string
	FromAddress string
	TLSMode     string
	PasswordSet bool
	UpdatedAt   time.Time
	UpdatedBy   *uuid.UUID
}

type SmtpUpdateInput struct {
	Host                       string
	Port                       int
	Username                   *string
	FromAddress                string
	TLSMode                    string
	Password                   *string // nil = leave; "" = clear; non-empty = set
	AllowPlaintextCredentials  bool
	UpdatedBy                  uuid.UUID
}

var (
	ErrSmtpInvalid           = errors.New("invalid smtp configuration")
	ErrSmtpPlaintextDisabled = errors.New("plaintext credentials require allow_plaintext_credentials=true")
)

type SmtpService struct {
	repo  *repo.SmtpRepo
	email *crypto.EmailCipher
}

func NewSmtpService(r *repo.SmtpRepo, email *crypto.EmailCipher) *SmtpService {
	return &SmtpService{repo: r, email: email}
}

func (s *SmtpService) Get(ctx context.Context) (*SmtpConfig, error) {
	c, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return toServiceSmtp(c), nil
}

func toServiceSmtp(c *repo.SmtpConfig) *SmtpConfig {
	return &SmtpConfig{
		Host:        c.Host,
		Port:        c.Port,
		Username:    c.Username,
		FromAddress: c.FromAddress,
		TLSMode:     c.TLSMode,
		PasswordSet: len(c.PasswordEncrypted) > 0,
		UpdatedAt:   c.UpdatedAt,
		UpdatedBy:   c.UpdatedBy,
	}
}

// Update validates the input, encrypts the password (if provided), and writes
// the single-row config. Refuses to persist a username/password combination
// over plaintext SMTP unless the caller explicitly opts in.
func (s *SmtpService) Update(ctx context.Context, in SmtpUpdateInput) (*SmtpConfig, error) {
	if err := validateSmtp(in); err != nil {
		return nil, err
	}
	row := &repo.SmtpConfig{
		Host:        strings.TrimSpace(in.Host),
		Port:        in.Port,
		Username:    in.Username,
		FromAddress: strings.TrimSpace(in.FromAddress),
		TLSMode:     in.TLSMode,
		UpdatedBy:   &in.UpdatedBy,
	}

	leavePassword := in.Password == nil
	if !leavePassword {
		if *in.Password == "" {
			row.PasswordEncrypted = nil // clear
		} else {
			ct, err := s.email.Encrypt(*in.Password)
			if err != nil {
				return nil, fmt.Errorf("encrypt smtp password: %w", err)
			}
			row.PasswordEncrypted = ct
		}
	}
	if err := s.repo.Upsert(ctx, row, leavePassword); err != nil {
		return nil, err
	}
	c, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return toServiceSmtp(c), nil
}

func validateSmtp(in SmtpUpdateInput) error {
	if strings.TrimSpace(in.Host) == "" {
		return fmt.Errorf("%w: host required", ErrSmtpInvalid)
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("%w: port out of range", ErrSmtpInvalid)
	}
	if !strings.Contains(in.FromAddress, "@") {
		return fmt.Errorf("%w: from_address must be an email", ErrSmtpInvalid)
	}
	switch in.TLSMode {
	case "none", "starttls", "tls":
	default:
		return fmt.Errorf("%w: tls_mode must be one of none|starttls|tls", ErrSmtpInvalid)
	}
	hasUser := in.Username != nil && *in.Username != ""
	hasPass := in.Password != nil && *in.Password != ""
	if in.TLSMode == "none" && (hasUser || hasPass) && !in.AllowPlaintextCredentials {
		return ErrSmtpPlaintextDisabled
	}
	return nil
}

// RevealPassword returns the stored SMTP password as cleartext, or "" when
// no password is configured. Caller is responsible for authorization (admin
// role) and for writing the audit row - this is the only ingress that
// exposes the cleartext outside the SMTP send path, so it's deliberately
// kept narrow and the audit happens at the handler layer where the actor
// identity lives.
func (s *SmtpService) RevealPassword(ctx context.Context) (string, error) {
	c, err := s.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	if len(c.PasswordEncrypted) == 0 {
		return "", nil
	}
	return s.email.Decrypt(c.PasswordEncrypted)
}

type SmtpTestResult struct {
	Success bool
	Error   string
}

// Test opens a real SMTP connection using the stored config and reports
// whether the negotiation succeeded. Errors are mapped to short, non-
// sensitive codes so the response never echoes the password or hostname
// substring an attacker might use to fingerprint internal infrastructure.
func (s *SmtpService) Test(ctx context.Context) (*SmtpTestResult, error) {
	c, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var conn net.Conn
	switch c.TLSMode {
	case "tls":
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: c.Host})
		if err != nil {
			return &SmtpTestResult{Success: false, Error: "tls_handshake_failed"}, nil
		}
	default:
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return &SmtpTestResult{Success: false, Error: "dial_timeout"}, nil
		}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return &SmtpTestResult{Success: false, Error: "smtp_handshake_failed"}, nil
	}
	defer client.Close()

	if c.TLSMode == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: c.Host}); err != nil {
			return &SmtpTestResult{Success: false, Error: "starttls_failed"}, nil
		}
	}
	if c.Username != nil && *c.Username != "" {
		var pass string
		if len(c.PasswordEncrypted) > 0 {
			pass, err = s.email.Decrypt(c.PasswordEncrypted)
			if err != nil {
				return &SmtpTestResult{Success: false, Error: "password_decrypt_failed"}, nil
			}
		}
		auth := smtp.PlainAuth("", *c.Username, pass, c.Host)
		if err := client.Auth(auth); err != nil {
			return &SmtpTestResult{Success: false, Error: "auth_failed"}, nil
		}
	}
	if err := client.Noop(); err != nil {
		return &SmtpTestResult{Success: false, Error: "noop_failed"}, nil
	}
	_ = client.Quit()
	return &SmtpTestResult{Success: true}, nil
}
