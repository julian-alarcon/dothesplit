package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

// UpdateMe applies a partial update to the current user. Currently supports
// display_name and week_start; either or both may be supplied.
func (s *Server) UpdateMe(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.UpdateMeRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if req.DisplayName == nil && req.WeekStart == nil && req.Timezone == nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "nothing to update")
		return
	}
	if req.DisplayName != nil {
		if err := s.MeSvc.Rename(c.Request.Context(), u.ID, *req.DisplayName); err != nil {
			switch {
			case errors.Is(err, repo.ErrNotFound):
				writeErr(c, http.StatusNotFound, "not_found", "user not found")
			default:
				writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
			}
			return
		}
	}
	if req.WeekStart != nil {
		if err := s.MeSvc.SetWeekStart(c.Request.Context(), u.ID, int16(*req.WeekStart)); err != nil {
			switch {
			case errors.Is(err, repo.ErrNotFound):
				writeErr(c, http.StatusNotFound, "not_found", "user not found")
			default:
				writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
			}
			return
		}
	}
	if req.Timezone != nil {
		if err := s.MeSvc.SetTimezone(c.Request.Context(), u.ID, *req.Timezone); err != nil {
			switch {
			case errors.Is(err, repo.ErrNotFound):
				writeErr(c, http.StatusNotFound, "not_found", "user not found")
			case errors.Is(err, service.ErrBadTimezone):
				writeErr(c, http.StatusBadRequest, "bad_request", "unknown timezone")
			default:
				writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
			}
			return
		}
	}
	// Reload through AuthService so the response reflects any newly-set fields.
	fresh, err := s.Auth.Resolve(c.Request.Context(), currentSessionToken(c, s))
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, toAPIUser(fresh))
}

// ChangePassword verifies the old password and rotates to a new one. All other
// sessions are revoked; the caller's current session is refreshed with a new
// cookie so the user stays logged in.
func (s *Server) ChangePassword(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.ChangePasswordRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if err := s.MeSvc.ChangePassword(c.Request.Context(), u.ID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, service.ErrWrongPassword):
			writeErr(c, http.StatusUnauthorized, "invalid_credentials", "old password is incorrect")
		case errors.Is(err, service.ErrUserDeleted):
			writeErr(c, http.StatusUnauthorized, "unauthorized", "account is deleted")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	// Every session (including ours) was revoked. Issue a fresh one so the
	// user doesn't have to log in again from the same browser.
	token, err := s.Auth.IssueSession(c.Request.Context(), u.ID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.setSessionCookie(c, token)
	c.Status(http.StatusNoContent)
}

// SetAvatar validates and stores an 8x8 PNG.
func (s *Server) SetAvatar(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.SetAvatarRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if err := s.MeSvc.SetAvatarFromBase64(c.Request.Context(), u.ID, req.PngBase64); err != nil {
		if errors.Is(err, service.ErrBadAvatar) {
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteAvatar clears the avatar; the UI falls back to initials.
func (s *Server) DeleteAvatar(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.MeSvc.ClearAvatar(c.Request.Context(), u.ID); err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteMe soft-deletes the calling account, scrubs PII, nukes sessions, and
// clears the session cookie.
func (s *Server) DeleteMe(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.MeSvc.SoftDelete(c.Request.Context(), u.ID); err != nil {
		if errors.Is(err, service.ErrUserDeleted) {
			writeErr(c, http.StatusUnauthorized, "unauthorized", "account is already deleted")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

// GetUserAvatar serves the 8x8 PNG for a user the caller shares a group with.
func (s *Server) GetUserAvatar(c *gin.Context) {
	me := middleware.User(c)
	if me == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	target, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if me.ID != target {
		shares, err := s.Groups.ShareAnyGroup(c.Request.Context(), me.ID, target)
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if !shares {
			writeErr(c, http.StatusForbidden, "forbidden", "not authorized to view this avatar")
			return
		}
	}
	png, err := s.MeSvc.GetAvatar(c.Request.Context(), target)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeErr(c, http.StatusNotFound, "not_found", "no avatar set")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Data(http.StatusOK, "image/png", png)
}

// currentSessionToken is a helper that reads the raw cookie used to identify
// the caller. Used when we need to re-Resolve() to see freshly-updated fields.
func currentSessionToken(c *gin.Context, s *Server) string {
	tok, _ := c.Cookie(middleware.SessionCookieName(s.Cfg.CookieSecure))
	return tok
}

// ChangeEmailRequest begins the change-email flow: re-verifies the password
// (step-up), persists a token row keyed on the *new* email, and enqueues a
// 6-digit code to that new address. The caller's email is unchanged until
// they confirm.
func (s *Server) ChangeEmailRequest(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.ChangeEmailRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	err := s.Auth.RequestEmailChange(c.Request.Context(), u.ID, string(req.NewEmail), req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			writeErr(c, http.StatusUnauthorized, "invalid_credentials", "current password is incorrect")
		case errors.Is(err, service.ErrStepUpRateLimited):
			writeErr(c, http.StatusLocked, "rate_limited", "too many failed password attempts")
		case errors.Is(err, service.ErrEmailTaken):
			writeErr(c, http.StatusConflict, "email_taken", "email already in use")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	c.Status(http.StatusAccepted)
}

// ChangeEmailConfirm consumes the code, swaps the email, revokes other
// sessions, and refreshes the caller's cookie.
func (s *Server) ChangeEmailConfirm(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.ConfirmEmailChangeRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	user, token, err := s.Auth.ConfirmEmailChange(c.Request.Context(), u.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCode):
			writeErr(c, http.StatusBadRequest, "invalid_code", "verification code is incorrect")
		case errors.Is(err, service.ErrCodeExpired):
			writeErr(c, http.StatusGone, "code_expired", "verification code has expired or is no longer valid")
		case errors.Is(err, service.ErrVerifyRateLimited):
			writeErr(c, http.StatusTooManyRequests, "too_many_attempts", "too many incorrect attempts; request a new code")
		case errors.Is(err, service.ErrEmailTaken):
			writeErr(c, http.StatusConflict, "email_taken", "email already in use")
		default:
			writeErr(c, http.StatusInternalServerError, "internal", "confirm failed")
		}
		return
	}
	s.setSessionCookie(c, token)
	c.JSON(http.StatusOK, toAPIUser(user))
}

// GetMyNotifications returns the caller's notification preferences.
func (s *Server) GetMyNotifications(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	p, err := s.Notifications.GetPrefs(c.Request.Context(), u.ID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "load prefs failed")
		return
	}
	c.JSON(http.StatusOK, toAPIPrefs(p))
}

// UpdateMyNotifications writes the caller's notification preferences.
func (s *Server) UpdateMyNotifications(c *gin.Context) {
	u := middleware.User(c)
	if u == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req apigen.NotificationPrefs
	if !bindStrictJSON(c, &req) {
		return
	}
	in := &service.NotificationPrefs{}
	if req.NotifyRecurringRun != nil {
		in.NotifyRecurringRun = *req.NotifyRecurringRun
	}
	if req.NotifySettlement != nil {
		in.NotifySettlement = *req.NotifySettlement
	}
	if req.NotifyGroupAdded != nil {
		in.NotifyGroupAdded = *req.NotifyGroupAdded
	}
	out, err := s.Notifications.UpdatePrefs(c.Request.Context(), u.ID, in)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "save prefs failed")
		return
	}
	c.JSON(http.StatusOK, toAPIPrefs(out))
}

func toAPIPrefs(p *service.NotificationPrefs) apigen.NotificationPrefs {
	r := p.NotifyRecurringRun
	st := p.NotifySettlement
	ga := p.NotifyGroupAdded
	return apigen.NotificationPrefs{
		NotifyRecurringRun: &r,
		NotifySettlement:   &st,
		NotifyGroupAdded:   &ga,
	}
}
