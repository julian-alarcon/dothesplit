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

	// Auth (rate-limited register + login; logout is public so a stale
	// cookie can clear itself without hitting the limiter).
	authG := v1.Group("")
	authG.Use(mw.LoginRateLimiter())
	authG.POST("/auth/register", s.Register)
	authG.POST("/auth/login", s.Login)
	v1.POST("/auth/logout", s.Logout)

	// Authenticated endpoints.
	auth := v1.Group("")
	auth.Use(mw.RequireSession())
	auth.GET("/me", s.Me)
	auth.PATCH("/me", s.UpdateMe)
	auth.DELETE("/me", s.DeleteMe)
	auth.POST("/me/password", s.ChangePassword)
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

	auth.GET("/groups/:id/recurring-expenses", s.ListRecurringExpenses)
	auth.POST("/groups/:id/recurring-expenses", s.CreateRecurringExpense)
	auth.DELETE("/recurring-expenses/:id", s.DeleteRecurringExpense)

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
