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
	auth := service.NewAuthService(users, sessions, email, cfg.PasswordPepper,
		time.Duration(cfg.SessionTTLDay)*24*time.Hour)
	meSvc := service.NewMeService(users, sessions, email, cfg.PasswordPepper)
	categorySvc := service.NewCategoryService(categories)
	groupSvc := service.NewGroupService(groups, users, balances, email)
	expenseSvc := service.NewExpenseService(expenses, groups, categorySvc)
	balanceSvc := service.NewBalanceService(balances, groups)
	settlementSvc := service.NewSettlementService(settlements, groups)
	recurringSvc := service.NewRecurringService(recurring, expenses, groups, categorySvc)

	srv := &handlers.Server{
		Cfg: cfg, Pool: pool,
		Auth:        auth,
		MeSvc:       meSvc,
		Groups:      groupSvc,
		Categories:  categorySvc,
		Expenses:    expenseSvc,
		Balances:    balanceSvc,
		Settlements: settlementSvc,
		Recurring:   recurringSvc,
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
