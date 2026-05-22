package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// stepUp verifies the actor's password and writes a failure audit row when it
// doesn't match. Returns true to continue, false on rejection (response is
// already written).
func (s *Server) stepUp(c *gin.Context, actorID uuid.UUID, password, action string, targetUser, targetGroup *uuid.UUID) bool {
	if password == "" {
		writeErr(c, http.StatusBadRequest, "bad_request", "password required for step-up")
		return false
	}
	err := s.Auth.VerifyPassword(c.Request.Context(), actorID, password)
	if err == nil {
		return true
	}
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	if errors.Is(err, service.ErrStepUpRateLimited) {
		s.Admin.LogStepUpFailure(c.Request.Context(), actorID, action, targetUser, targetGroup, ip, ua)
		writeErr(c, http.StatusLocked, "step_up_rate_limited", "too many failed step-up attempts; try again in a minute")
		return false
	}
	s.Admin.LogStepUpFailure(c.Request.Context(), actorID, action, targetUser, targetGroup, ip, ua)
	writeErr(c, http.StatusUnauthorized, "step_up_failed", "step-up password did not match")
	return false
}

func parsePagination(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Server) AdminListUsers(c *gin.Context) {
	limit, offset := parsePagination(c)
	includeDeleted := c.Query("include_deleted") == "true"
	rows, total, err := s.Admin.ListUsers(c.Request.Context(), limit, offset, includeDeleted)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "list users failed")
		return
	}
	out := apigen.AdminUserListResponse{Limit: limit, Offset: offset, Total: total, Items: make([]apigen.AdminUser, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, toAPIAdminUser(r))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) AdminCreateUser(c *gin.Context) {
	actor := middleware.User(c)
	var req apigen.AdminUserCreateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	role := "user"
	if req.Role != nil {
		role = string(*req.Role)
	}
	out, err := s.Admin.CreateUser(c.Request.Context(), actor.ID,
		string(req.Email), req.DisplayName, role, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			writeErr(c, http.StatusConflict, "email_taken", "email already registered")
		case errors.Is(err, service.ErrSmtpUnconfigured):
			writeErr(c, http.StatusServiceUnavailable, "smtp_unconfigured",
				"configure SMTP before inviting users - they receive a welcome email to set their password")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	c.JSON(http.StatusCreated, toAPIAdminUser(*out))
}

func (s *Server) AdminDeleteUser(c *gin.Context) {
	actor := middleware.User(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req apigen.StepUpRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if !s.stepUp(c, actor.ID, req.Password, "admin_delete_user", &id, nil) {
		return
	}
	if err := s.Admin.DeleteUser(c.Request.Context(), actor.ID, id, c.ClientIP(), c.Request.UserAgent()); err != nil {
		switch {
		case errors.Is(err, service.ErrLastAdmin):
			writeErr(c, http.StatusConflict, "last_admin", "cannot remove the last admin")
		case errors.Is(err, service.ErrCannotTargetSelf):
			writeErr(c, http.StatusConflict, "cannot_target_self", "admins cannot delete their own account here")
		case errors.Is(err, repo.ErrNotFound):
			writeErr(c, http.StatusNotFound, "not_found", "user not found")
		default:
			writeErr(c, http.StatusInternalServerError, "internal", "delete user failed")
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) AdminResetUserPassword(c *gin.Context) {
	actor := middleware.User(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req apigen.StepUpRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if !s.stepUp(c, actor.ID, req.Password, "admin_reset_password", &id, nil) {
		return
	}
	if err := s.Admin.ResetUserPassword(c.Request.Context(), actor.ID, id, c.ClientIP(), c.Request.UserAgent()); err != nil {
		switch {
		case errors.Is(err, repo.ErrNotFound):
			writeErr(c, http.StatusNotFound, "not_found", "user not found")
		case errors.Is(err, service.ErrSmtpUnconfigured):
			writeErr(c, http.StatusServiceUnavailable, "smtp_unconfigured",
				"configure SMTP before sending reset emails")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) AdminGetUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	view, err := s.Admin.GetUser(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeErr(c, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", "get user failed")
		return
	}
	c.JSON(http.StatusOK, toAPIAdminUser(*view))
}

func (s *Server) AdminSetUserRole(c *gin.Context) {
	actor := middleware.User(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req apigen.AdminSetUserRoleRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	action := "admin_set_user_role"
	if string(req.Role) == "admin" {
		action = "admin_promote_user"
	} else if string(req.Role) == "user" {
		action = "admin_demote_user"
	}
	if !s.stepUp(c, actor.ID, req.Password, action, &id, nil) {
		return
	}
	view, err := s.Admin.SetUserRole(c.Request.Context(), actor.ID, id,
		string(req.Role), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrLastAdmin):
			writeErr(c, http.StatusConflict, "last_admin", "cannot demote the last admin")
		case errors.Is(err, service.ErrCannotTargetSelf):
			writeErr(c, http.StatusConflict, "cannot_target_self", "admins cannot change their own role here")
		case errors.Is(err, repo.ErrNotFound):
			writeErr(c, http.StatusNotFound, "not_found", "user not found")
		default:
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, toAPIAdminUser(*view))
}

func (s *Server) AdminListGroups(c *gin.Context) {
	limit, offset := parsePagination(c)
	rows, total, err := s.Admin.ListGroups(c.Request.Context(), limit, offset)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "list groups failed")
		return
	}
	out := apigen.AdminGroupListResponse{Limit: limit, Offset: offset, Total: total, Items: make([]apigen.AdminGroup, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, apigen.AdminGroup{
			Id:              r.ID,
			Name:            r.Name,
			DefaultCurrency: r.DefaultCurrency,
			CreatedBy:       r.CreatedBy,
			CreatedAt:       r.CreatedAt,
			MemberCount:     r.MemberCount,
			ExpenseCount:    r.ExpenseCount,
		})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) AdminDeleteGroup(c *gin.Context) {
	actor := middleware.User(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req apigen.StepUpRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	if !s.stepUp(c, actor.ID, req.Password, "admin_delete_group", nil, &id) {
		return
	}
	if err := s.Admin.DeleteGroup(c.Request.Context(), actor.ID, id, c.ClientIP(), c.Request.UserAgent()); err != nil {
		switch {
		case errors.Is(err, repo.ErrNotFound):
			writeErr(c, http.StatusNotFound, "not_found", "group not found")
		default:
			writeErr(c, http.StatusInternalServerError, "internal", "delete group failed")
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) AdminGetSmtp(c *gin.Context) {
	cfg, err := s.Smtp.Get(c.Request.Context())
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeErr(c, http.StatusNotFound, "not_found", "smtp not configured")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", "get smtp failed")
		return
	}
	c.JSON(http.StatusOK, toAPISmtp(cfg))
}

func (s *Server) AdminUpdateSmtp(c *gin.Context) {
	actor := middleware.User(c)
	var req apigen.SmtpConfigUpdateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	in := service.SmtpUpdateInput{
		Host:        req.Host,
		Port:        req.Port,
		Username:    req.Username,
		FromAddress: string(req.FromAddress),
		TLSMode:     string(req.TlsMode),
		Password:    req.SmtpPassword,
		UpdatedBy:   actor.ID,
	}
	if req.AllowPlaintextCredentials != nil {
		in.AllowPlaintextCredentials = *req.AllowPlaintextCredentials
	}
	cfg, err := s.Smtp.Update(c.Request.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSmtpInvalid), errors.Is(err, service.ErrSmtpPlaintextDisabled):
			writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		default:
			writeErr(c, http.StatusInternalServerError, "internal", "update smtp failed")
		}
		return
	}
	_ = s.Audit.Insert(c.Request.Context(), nil, &repo.AuditEntry{
		ActorUserID: actor.ID,
		Action:      "admin_update_smtp",
		IP:          strPtrOrNil(c.ClientIP()),
		UserAgent:   strPtrOrNil(c.Request.UserAgent()),
		Success:     true,
	})
	c.JSON(http.StatusOK, toAPISmtp(cfg))
}

func (s *Server) AdminTestSmtp(c *gin.Context) {
	res, err := s.Smtp.Test(c.Request.Context())
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeErr(c, http.StatusNotFound, "not_found", "smtp not configured")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", "test smtp failed")
		return
	}
	out := apigen.SmtpTestResponse{Success: res.Success}
	if res.Error != "" {
		e := res.Error
		out.Error = &e
	}
	c.JSON(http.StatusOK, out)
}

// AdminRevealSmtpPassword returns the stored SMTP password as cleartext.
// Admin-only and explicitly audit-logged: revealing a credential is the kind
// of action ops should be able to discover later in the audit feed, both for
// incident response and for regular review.
func (s *Server) AdminRevealSmtpPassword(c *gin.Context) {
	actor := middleware.User(c)
	if actor == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	pw, err := s.Smtp.RevealPassword(c.Request.Context())
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeErr(c, http.StatusNotFound, "not_found", "smtp not configured")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", "reveal smtp password failed")
		return
	}
	if pw == "" {
		writeErr(c, http.StatusNotFound, "not_found", "no password stored")
		return
	}
	_ = s.Audit.Insert(c.Request.Context(), nil, &repo.AuditEntry{
		ActorUserID: actor.ID,
		Action:      "admin_view_smtp_password",
		IP:          strPtrOrNil(c.ClientIP()),
		UserAgent:   strPtrOrNil(c.Request.UserAgent()),
		Success:     true,
	})
	c.JSON(http.StatusOK, apigen.SmtpPasswordResponse{Password: pw})
}

// AdminSendSmtpTestEmail dispatches a real plain-text test email to the
// admin's own address, synchronously. Bypasses the outbox so SMTP errors
// surface immediately in the UI instead of disappearing into worker retries.
func (s *Server) AdminSendSmtpTestEmail(c *gin.Context) {
	actor := middleware.User(c)
	if actor == nil {
		writeErr(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ok, err := s.Mailer.IsConfigured(c.Request.Context())
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "load smtp config failed")
		return
	}
	if !ok {
		writeErr(c, http.StatusNotFound, "not_found", "smtp not configured")
		return
	}
	out := apigen.SmtpTestResponse{Success: true}
	if err := s.Mailer.SendNow(c.Request.Context(), actor.Email, "smtp_test", service.TemplateVars{
		DisplayName: actor.DisplayName,
		WebOrigin:   s.Cfg.WebOrigin,
	}); err != nil {
		out.Success = false
		msg := err.Error()
		out.Error = &msg
	}
	_ = s.Audit.Insert(c.Request.Context(), nil, &repo.AuditEntry{
		ActorUserID: actor.ID,
		Action:      "admin_send_smtp_test",
		IP:          strPtrOrNil(c.ClientIP()),
		UserAgent:   strPtrOrNil(c.Request.UserAgent()),
		Success:     out.Success,
	})
	c.JSON(http.StatusOK, out)
}

func (s *Server) AdminListAudit(c *gin.Context) {
	limit, offset := parsePagination(c)
	action := c.Query("action")
	rows, total, err := s.Admin.ListAudit(c.Request.Context(), action, limit, offset)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", "list audit failed")
		return
	}
	out := apigen.AdminAuditListResponse{Limit: limit, Offset: offset, Total: total, Items: make([]apigen.AdminAuditEntry, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, toAPIAuditEntry(r))
	}
	c.JSON(http.StatusOK, out)
}

func toAPIAdminUser(u service.AdminUserView) apigen.AdminUser {
	out := apigen.AdminUser{
		Id:          u.ID,
		DisplayName: u.DisplayName,
		Role:        apigen.AdminUserRole(u.Role),
		CreatedAt:   u.CreatedAt,
		DeletedAt:   u.DeletedAt,
		HasAvatar:   u.HasAvatar,
		WeekStart:   apigen.AdminUserWeekStart(u.WeekStart),
	}
	// Email is nullable on the wire because soft-deleted users have a
	// scrambled email_encrypted that can't be decrypted.
	if u.Email != "" {
		e := openapi_types.Email(u.Email)
		out.Email = &e
	}
	return out
}

func toAPISmtp(c *service.SmtpConfig) apigen.SmtpConfig {
	out := apigen.SmtpConfig{
		Host:        c.Host,
		Port:        c.Port,
		Username:    c.Username,
		FromAddress: openapi_types.Email(c.FromAddress),
		TlsMode:     apigen.SmtpTlsMode(c.TLSMode),
		PasswordSet: c.PasswordSet,
	}
	if !c.UpdatedAt.IsZero() {
		t := c.UpdatedAt
		out.UpdatedAt = &t
	}
	out.UpdatedBy = c.UpdatedBy
	return out
}

func toAPIAuditEntry(e repo.AuditEntry) apigen.AdminAuditEntry {
	out := apigen.AdminAuditEntry{
		Id:            e.ID,
		ActorUserId:   e.ActorUserID,
		TargetUserId:  e.TargetUserID,
		TargetGroupId: e.TargetGroupID,
		Action:        e.Action,
		Ip:            e.IP,
		UserAgent:     e.UserAgent,
		Success:       e.Success,
		CreatedAt:     e.CreatedAt,
	}
	if len(e.Metadata) > 0 {
		var m map[string]interface{}
		_ = json.Unmarshal(e.Metadata, &m)
		if m != nil {
			out.Metadata = &m
		}
	}
	return out
}

// strPtrOrNil mirrors service.strPtr so handlers don't import unexported helpers.
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
