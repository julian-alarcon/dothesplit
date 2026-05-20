// Package handlers implements the HTTP surface using Gin.
package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/config"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

// Server bundles dependencies for all handlers.
type Server struct {
	Cfg         *config.Config
	Pool        *pgxpool.Pool
	Auth        *service.AuthService
	MeSvc       *service.MeService
	Groups      *service.GroupService
	Categories  *service.CategoryService
	Expenses    *service.ExpenseService
	Balances    *service.BalanceService
	Settlements *service.SettlementService
	Recurring   *service.RecurringService
	Activity    *service.ActivityService
	Admin         *service.AdminService
	Smtp          *service.SmtpService
	Setup         *service.SetupService
	Mailer        *service.MailerService
	Notifications *service.NotificationService
	Users         *repo.UserRepo
	Audit         *repo.AuditRepo
}

func writeErr(c *gin.Context, status int, code, message string) {
	c.JSON(status, apigen.Error{Code: code, Message: message})
}

// bindStrictJSON decodes the request body into dst, rejecting unknown fields
// and any trailing tokens. Matches additionalProperties: false in the spec.
// On failure it writes a 400 and returns false; callers should return early.
func bindStrictJSON(c *gin.Context, dst any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(c, http.StatusBadRequest, "bad_request", "unexpected trailing JSON")
		return false
	}
	return true
}

// setSessionCookie writes the canonical session cookie.
func (s *Server) setSessionCookie(c *gin.Context, token string) {
	maxAge := int(time.Duration(s.Cfg.SessionTTLDay) * 24 * time.Hour / time.Second)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.SessionCookieName(s.Cfg.CookieSecure), token, maxAge,
		"/", s.Cfg.CookieDomain, s.Cfg.CookieSecure, true)
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.SessionCookieName(s.Cfg.CookieSecure), "", -1,
		"/", s.Cfg.CookieDomain, s.Cfg.CookieSecure, true)
}
