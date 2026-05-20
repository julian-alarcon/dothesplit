// Command api starts the DoTheSplit HTTP API server.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julian-alarcon/dothesplit/api/internal/config"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/handlers"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/server"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", slog.String("err", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("pgx pool", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	email, err := crypto.NewEmailCipher(cfg.EmailEncKey, cfg.EmailHMACKey)
	if err != nil {
		logger.Error("email cipher", slog.String("err", err.Error()))
		os.Exit(1)
	}

	users := repo.NewUserRepo(pool)
	sessions := repo.NewSessionRepo(pool)
	groups := repo.NewGroupRepo(pool)
	expenses := repo.NewExpenseRepo(pool)
	settlements := repo.NewSettlementRepo(pool)
	balances := repo.NewBalanceRepo(pool)
	recurring := repo.NewRecurringRepo(pool)
	categories := repo.NewCategoryRepo(pool)
	activityRepo := repo.NewActivityRepo(pool)
	auditRepo := repo.NewAuditRepo(pool)
	smtpRepo := repo.NewSmtpRepo(pool)
	setupRepo := repo.NewSetupRepo(pool)
	verificationRepo := repo.NewVerificationRepo(pool)
	outboxRepo := repo.NewEmailOutboxRepo(pool)
	mailerSvc := service.NewMailerService(smtpRepo, outboxRepo, email, logger)
	auth := service.NewAuthService(pool, users, sessions, auditRepo, verificationRepo, mailerSvc, setupRepo, email, cfg.PasswordPepper,
		time.Duration(cfg.SessionTTLDay)*24*time.Hour)
	notificationSvc := service.NewNotificationService(users, mailerSvc, email)

	meSvc := service.NewMeService(users, sessions, email, cfg.PasswordPepper)
	categorySvc := service.NewCategoryService(categories)
	groupSvc := service.NewGroupService(groups, users, balances, email)
	expenseSvc := service.NewExpenseService(expenses, groups, categorySvc)
	balanceSvc := service.NewBalanceService(balances, groups)
	settlementSvc := service.NewSettlementService(settlements, groups)
	recurringSvc := service.NewRecurringService(recurring, expenses, groups, categorySvc)
	activitySvc := service.NewActivityService(groupSvc, activityRepo, expenses, settlements, recurring)
	adminSvc := service.NewAdminService(pool, users, groups, sessions, auditRepo, email, cfg.PasswordPepper)
	smtpSvc := service.NewSmtpService(smtpRepo, email)
	setupSvc := service.NewSetupService(pool, setupRepo, auth, auditRepo)

	// Wire notifications into the services that produce them. The hook is
	// optional so tests can construct services without a real mailer.
	groupSvc.SetNotifications(notificationSvc)
	settlementSvc.SetNotifications(users, notificationSvc)
	recurringSvc.SetNotifications(users, notificationSvc)

	// First-run setup: rotate the install token on every boot until consumed.
	// The cleartext is logged once as a warning so the operator can grab it
	// from `docker compose logs api`. Once setup is completed the banner is
	// suppressed and the token cleartext is gone — only its SHA-256 lives in
	// app_setup, and even that is unreachable from any post-setup code path.
	if ct, _, completed, err := setupSvc.EnsureToken(ctx); err != nil {
		logger.Error("setup ensure token", slog.String("err", err.Error()))
		os.Exit(1)
	} else if !completed {
		logger.Warn("first-run setup required",
			slog.String("url", cfg.WebOrigin+"/setup"),
			slog.String("token", ct),
			slog.String("note", "Visit the URL and paste the token. This banner stops once setup is consumed."),
		)
	}

	srv := &handlers.Server{
		Cfg: cfg, Pool: pool,
		Auth:          auth,
		MeSvc:         meSvc,
		Groups:        groupSvc,
		Categories:    categorySvc,
		Expenses:      expenseSvc,
		Balances:      balanceSvc,
		Settlements:   settlementSvc,
		Recurring:     recurringSvc,
		Activity:      activitySvc,
		Admin:         adminSvc,
		Smtp:          smtpSvc,
		Setup:         setupSvc,
		Mailer:        mailerSvc,
		Notifications: notificationSvc,
		Users:         users,
		Audit:         auditRepo,
	}
	h := server.New(srv)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("listening", slog.String("addr", cfg.HTTPAddr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http serve", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", slog.String("err", err.Error()))
	}
}
