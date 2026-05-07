package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) Register(c *gin.Context) {
	var req apigen.RegisterRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	u, token, err := s.Auth.Register(c.Request.Context(), string(req.Email), req.Password, req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			writeErr(c, http.StatusConflict, "email_taken", "email already registered")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	s.setSessionCookie(c, token)
	c.JSON(http.StatusCreated, toAPIUser(u))
}

func (s *Server) Login(c *gin.Context) {
	var req apigen.LoginRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	u, token, err := s.Auth.Login(c.Request.Context(), string(req.Email), req.Password)
	if err != nil {
		writeErr(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	s.setSessionCookie(c, token)
	c.JSON(http.StatusOK, toAPIUser(u))
}

func (s *Server) Logout(c *gin.Context) {
	if token, err := c.Cookie(middleware.SessionCookieName(s.Cfg.CookieSecure)); err == nil {
		_ = s.Auth.Logout(c.Request.Context(), token)
	}
	s.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) Me(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	c.JSON(http.StatusOK, toAPIUser(u))
}

func toAPIUser(u *service.User) apigen.User {
	out := apigen.User{
		Id:              u.ID,
		Email:           openapi_types.Email(u.Email),
		DisplayName:     u.DisplayName,
		CreatedAt:       u.CreatedAt,
		HasAvatar:       u.HasAvatar,
		AvatarUpdatedAt: u.AvatarUpdatedAt,
		DeletedAt:       u.DeletedAt,
		WeekStart:       apigen.UserWeekStart(u.WeekStart),
	}
	return out
}
