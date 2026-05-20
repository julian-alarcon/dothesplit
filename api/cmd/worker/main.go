// Command worker runs background jobs (recurring expenses for v1).
// Uses a Postgres advisory lock so only one instance materializes at a time.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julian-alarcon/dothesplit/api/internal/config"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

// advisoryKey is any fixed int64; this one happens to spell "dtsrec" in hex-ish.
const advisoryKey int64 = 0x00DEADBEEFDA71EC

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", slog.String("err", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("pool", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	emailCipher, err := crypto.NewEmailCipher(cfg.EmailEncKey, cfg.EmailHMACKey)
	if err != nil {
		logger.Error("email cipher", slog.String("err", err.Error()))
		os.Exit(1)
	}

	recurring := repo.NewRecurringRepo(pool)
	expenses := repo.NewExpenseRepo(pool)
	groups := repo.NewGroupRepo(pool)
	users := repo.NewUserRepo(pool)
	categories := repo.NewCategoryRepo(pool)
	smtpRepo := repo.NewSmtpRepo(pool)
	outboxRepo := repo.NewEmailOutboxRepo(pool)

	categorySvc := service.NewCategoryService(categories)
	mailerSvc := service.NewMailerService(smtpRepo, outboxRepo, emailCipher, logger)
	notificationSvc := service.NewNotificationService(users, mailerSvc, emailCipher)
	svc := service.NewRecurringService(recurring, expenses, groups, categorySvc)
	svc.SetNotifications(users, notificationSvc)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	logger.Info("worker started")
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopping")
			return
		case <-ticker.C:
			if err := runOnce(ctx, pool, svc, mailerSvc, logger); err != nil {
				logger.Error("tick", slog.String("err", err.Error()))
			}
		}
	}
}

// runOnce acquires a session-level advisory lock, materializes due recurring
// expenses, then drains the email outbox. Both run under the same lock so a
// second worker instance is a no-op.
func runOnce(ctx context.Context, pool *pgxpool.Pool, svc *service.RecurringService, mailer *service.MailerService, logger *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var gotLock bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryKey).Scan(&gotLock); err != nil {
		return err
	}
	if !gotLock {
		logger.Debug("another worker holds the lock; skipping tick")
		return nil
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryKey); err != nil {
			logger.Warn("unlock", slog.String("err", err.Error()))
		}
	}()

	n, err := svc.Tick(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		logger.Info("materialized recurring expenses", slog.Int("count", n))
	}

	// Drain up to 50 outbox rows per tick. Sending happens inline; SMTP
	// errors are recorded on the row and retried on subsequent ticks.
	if mailer != nil {
		sent, err := mailer.DispatchOutbox(ctx, 50)
		if err != nil {
			logger.Warn("outbox dispatch", slog.String("err", err.Error()))
		} else if sent > 0 {
			logger.Info("sent emails", slog.Int("count", sent))
		}
	}
	return nil
}
