// Package server wires middlewares and registers routes onto a gin engine.
package server

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/handlers"
	mw "github.com/julian-alarcon/dothesplit/api/internal/middleware"
)

// New builds the top-level http.Handler using Gin.
func New(s *handlers.Server) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestID())
	r.Use(mw.Logger(slog.Default()))
	r.Use(mw.SecurityHeaders(s.Cfg.CookieSecure))
	r.Use(mw.CORS(s.Cfg.WebOrigin))
	r.Use(mw.Session(s.Auth, s.Cfg.CookieSecure))

	// Health probes are unversioned infrastructure endpoints.
	r.GET("/healthz", s.Healthz)
	r.GET("/readyz", s.Readyz)

	// All business endpoints live under /v1. Breaking changes → /v2 and
	// run both mounts in parallel until clients migrate.
	v1 := r.Group("/v1")

	// Setup ceremony. Public: /v1/setup/status returns one bool (consumed
	// by the web middleware on every render); /v1/setup/admin is the only
	// path that mints the first admin and is rate-limited tighter than
	// /auth/register because every successful POST is a one-shot.
	v1.GET("/setup/status", s.GetSetupStatus)
	v1.POST("/setup/admin", mw.SetupRateLimiter(), s.CompleteSetup)

	// Auth (rate-limited register + login; logout is public so a stale
	// cookie can clear itself without hitting the limiter).
	authG := v1.Group("")
	authG.Use(mw.LoginRateLimiter())
	authG.POST("/auth/register", s.Register)
	authG.POST("/auth/login", s.Login)
	authG.POST("/auth/verify", s.VerifyEmail)
	authG.POST("/auth/verify/resend", s.ResendVerification)
	v1.POST("/auth/logout", s.Logout)

	// Authenticated endpoints.
	auth := v1.Group("")
	auth.Use(mw.RequireSession())
	auth.Use(mw.EnforcePasswordChange())
	auth.GET("/me", s.Me)
	auth.PATCH("/me", s.UpdateMe)
	auth.DELETE("/me", s.DeleteMe)
	auth.POST("/me/password", s.ChangePassword)
	auth.POST("/me/email/change-request", s.ChangeEmailRequest)
	auth.POST("/me/email/change-confirm", s.ChangeEmailConfirm)
	auth.GET("/me/notifications", s.GetMyNotifications)
	auth.PATCH("/me/notifications", s.UpdateMyNotifications)
	auth.PUT("/me/avatar", s.SetAvatar)
	auth.DELETE("/me/avatar", s.DeleteAvatar)
	auth.GET("/users/:id/avatar", s.GetUserAvatar)

	auth.GET("/groups", s.ListGroups)
	auth.POST("/groups", s.CreateGroup)
	auth.PATCH("/groups/:id", s.UpdateGroup)
	auth.DELETE("/groups/:id", s.DeleteGroup)
	auth.POST("/groups/:id/members", s.AddGroupMember)
	auth.DELETE("/groups/:id/members/:userId", s.RemoveGroupMember)

	auth.GET("/groups/:id/expenses", s.ListExpenses)
	auth.POST("/groups/:id/expenses", s.CreateExpense)
	auth.GET("/expenses/:id", s.GetExpense)
	auth.PATCH("/expenses/:id", s.UpdateExpense)
	auth.DELETE("/expenses/:id", s.DeleteExpense)
	auth.GET("/expenses/:id/revisions", s.ListExpenseRevisions)

	auth.GET("/categories", s.ListCategories)

	auth.GET("/groups/:id/balances", s.GetBalances)

	auth.GET("/groups/:id/settlements", s.ListSettlements)
	auth.POST("/groups/:id/settlements", s.CreateSettlement)
	auth.GET("/settlements/:id", s.GetSettlement)
	auth.DELETE("/settlements/:id", s.DeleteSettlement)

	auth.GET("/groups/:id/activity", s.ListActivity)

	auth.GET("/groups/:id/recurring-expenses", s.ListRecurringExpenses)
	auth.POST("/groups/:id/recurring-expenses", s.CreateRecurringExpense)
	auth.DELETE("/recurring-expenses/:id", s.DeleteRecurringExpense)

	// Admin endpoints. RequireAdmin re-loads the user from DB on every
	// request so role revocation is immediate; it also stamps no-store +
	// X-Frame-Options on the response.
	admin := auth.Group("/admin")
	admin.Use(mw.RequireAdmin(s.Users))
	admin.GET("/users", s.AdminListUsers)
	admin.POST("/users", s.AdminCreateUser)
	admin.GET("/users/:id", s.AdminGetUser)
	admin.DELETE("/users/:id", s.AdminDeleteUser)
	admin.POST("/users/:id/password", s.AdminResetUserPassword)
	admin.PATCH("/users/:id/role", s.AdminSetUserRole)
	admin.GET("/groups", s.AdminListGroups)
	admin.DELETE("/groups/:id", s.AdminDeleteGroup)
	admin.GET("/smtp", s.AdminGetSmtp)
	admin.PUT("/smtp", s.AdminUpdateSmtp)
	admin.GET("/smtp/password", s.AdminRevealSmtpPassword)
	admin.POST("/smtp/test", s.AdminTestSmtp)
	admin.POST("/smtp/send-test", s.AdminSendSmtpTestEmail)
	admin.GET("/audit", s.AdminListAudit)

	return r
}

// requestID adds a short request identifier to each request.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = randomID()
		}
		c.Header("X-Request-Id", id)
		c.Set("request_id", id)
		c.Next()
	}
}
