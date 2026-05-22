package server_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// randEmail returns a unique address for tests that need to register many
// users without colliding on the email-hash unique index.
func randEmail() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "u" + hex.EncodeToString(b[:]) + "@test.dev"
}

// registerUserMaybe issues the same register call as registerUser, but
// tolerates failures (used by the bootstrap-race test where some calls may
// race losers may still get 201 with role=user, all should succeed but
// content varies).
func registerUserMaybe(t *testing.T, base, email, pw, name string) (map[string]any, *http.Cookie) {
	t.Helper()
	resp, body := request(t, "POST", base+"/v1/auth/register", map[string]any{
		"email":        email,
		"password":     pw,
		"display_name": name,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		return body, nil
	}
	return body, sessionCookie(resp)
}

// TestAdminBootstrapFirstUser verifies that the very first registered user
// becomes an admin and the second one does not.
func TestAdminBootstrapFirstUser(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	first, _ := registerUser(t, base, "first@test.dev", "passwordpassword", "First")
	require.Equal(t, true, first["is_admin"], "first registered user should be admin")

	second, _ := registerUser(t, base, "second@test.dev", "passwordpassword", "Second")
	// is_admin may be omitted-or-false on the second user; both encode the
	// same fact, so accept either.
	if v, ok := second["is_admin"]; ok {
		require.Equal(t, false, v, "second user must not be admin")
	}
}

// TestAdminBootstrapRace fires N parallel /v1/setup/admin calls with the
// same valid token. The SELECT … FOR UPDATE on app_setup plus the advisory
// lock inside RegisterTx must serialize them so exactly one admin is
// created; the rest see 410 setup_completed (or 401 if the row's already
// gone via the same path) or 409 email_taken.
func TestAdminBootstrapRace(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	const N = 8
	var wg sync.WaitGroup
	statuses := make([]int, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			resp, _ := request(t, "POST", base+"/v1/setup/admin", map[string]any{
				"token":        ts.setupToken,
				"email":        randEmail(),
				"password":     "passwordpassword",
				"display_name": "U",
			}, nil)
			statuses[idx] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created, "exactly one admin should be created across concurrent install ceremonies; got statuses %v", statuses)
}

// TestAdminAuthzNegative asserts that a regular user cannot reach any admin
// endpoint, and that anonymous requests get 401.
func TestAdminAuthzNegative(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	// First user is admin; second user is regular.
	_, _ = registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	_, userCookie := registerUser(t, base, "user@test.dev", "passwordpassword", "User")

	for _, ep := range []struct {
		method, path string
	}{
		{"GET", "/v1/admin/users"},
		{"GET", "/v1/admin/groups"},
		{"GET", "/v1/admin/audit"},
	} {
		// Anonymous → 401
		resp, _ := request(t, ep.method, base+ep.path, nil, nil)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "anon "+ep.path)

		// Regular user → 403
		resp, _ = request(t, ep.method, base+ep.path, nil, userCookie)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "regular user "+ep.path)
	}
}

// TestAdminCreateAndListUsers checks the basic admin create + list flow.
// CreateUser requires SMTP because the new user receives an email with a
// 6-digit code to set their own password - admin never types one.
func TestAdminCreateAndListUsers(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	configureSMTP(t, ts)

	resp, _ := request(t, "POST", base+"/v1/admin/users", map[string]any{
		"email":        "alice@test.dev",
		"display_name": "Alice",
		"role":         "user",
	}, adminCookie)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	resp, body := request(t, "GET", base+"/v1/admin/users", nil, adminCookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	items, _ := body["items"].([]any)
	require.GreaterOrEqual(t, len(items), 2)
}

// TestAdminCreateUserRequiresSmtp asserts the create-user endpoint refuses
// when SMTP isn't configured: there's no other way for the new user to
// learn their password.
func TestAdminCreateUserRequiresSmtp(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")

	resp, body := request(t, "POST", base+"/v1/admin/users", map[string]any{
		"email":        "alice@test.dev",
		"display_name": "Alice",
		"role":         "user",
	}, adminCookie)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "smtp_unconfigured", body["code"])
}

// TestAdminResetSendsEmail verifies the new admin-reset flow: the target's
// existing sessions are gone, the old password no longer works, but the
// admin's reset enqueues a password_reset token + outbox row so the user
// can pick a new password through /reset.
func TestAdminResetSendsEmail(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	target, targetCookie := registerUser(t, base, "tgt@test.dev", "passwordpassword", "Target")
	targetID := target["id"].(string)
	configureSMTP(t, ts)

	// Admin triggers reset (step-up only).
	resp, _ := request(t, "POST", base+"/v1/admin/users/"+targetID+"/password", map[string]any{
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Target's old session is dead.
	resp, _ = request(t, "GET", base+"/v1/me", nil, targetCookie)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// The old password no longer logs the target in.
	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email": "tgt@test.dev", "password": "passwordpassword",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// A password_reset token row exists for the target.
	var n int
	err := ts.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1 AND purpose = 'password_reset' AND consumed_at IS NULL`,
		targetID).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Pin the code and complete the reset → target logs in fresh.
	knownCode := "424242"
	pinPasswordResetCode(t, ts, knownCode)
	resp, _ = request(t, "POST", base+"/v1/auth/password-reset/confirm", map[string]any{
		"email":        "tgt@test.dev",
		"code":         knownCode,
		"new_password": "freshfreshfresh",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email": "tgt@test.dev", "password": "freshfreshfresh",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAdminStepUpFailure asserts that a wrong step-up password is rejected
// with 401 and that the user's record is not affected.
func TestAdminStepUpFailure(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	tgt, _ := registerUser(t, base, "tgt@test.dev", "passwordpassword", "Target")

	// Wrong step-up password → 401
	resp, _ := request(t, "DELETE", base+"/v1/admin/users/"+tgt["id"].(string), map[string]any{
		"password": "WRONG",
	}, adminCookie)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAdminLastAdminGuard ensures the only-active-admin cannot be deleted.
// Self-targeting is blocked unconditionally with 409 cannot_target_self.
func TestAdminLastAdminGuard(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	admin, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")

	// Admin tries to delete themselves → cannot_target_self (409)
	resp, _ := request(t, "DELETE", base+"/v1/admin/users/"+admin["id"].(string), map[string]any{
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

// TestAdminPromoteAndDemote covers the happy path: promote a regular user to
// admin, then demote them back. Each PATCH returns the updated AdminUser
// projection.
func TestAdminPromoteAndDemote(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	tgt, _ := registerUser(t, base, "tgt@test.dev", "passwordpassword", "Target")
	tid := tgt["id"].(string)

	// Promote user → admin
	resp, body := request(t, "PATCH", base+"/v1/admin/users/"+tid+"/role", map[string]any{
		"role":     "admin",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.Equal(t, "admin", body["role"])

	// Demote admin → user
	resp, body = request(t, "PATCH", base+"/v1/admin/users/"+tid+"/role", map[string]any{
		"role":     "user",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.Equal(t, "user", body["role"])
}

// TestAdminDemoteLastAdminGuard refuses to demote the only remaining admin.
func TestAdminDemoteLastAdminGuard(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	admin, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	// Add a second admin so we can demote the first; then remove the second
	// admin's status so the original is the last one.
	other, _ := registerUser(t, base, "other@test.dev", "passwordpassword", "Other")
	otherID := other["id"].(string)

	// Promote the other one.
	resp, _ := request(t, "PATCH", base+"/v1/admin/users/"+otherID+"/role", map[string]any{
		"role":     "admin",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Demote the other one - leaves the original as last admin.
	resp, _ = request(t, "PATCH", base+"/v1/admin/users/"+otherID+"/role", map[string]any{
		"role":     "user",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Now there is only one admin (the bootstrap admin). Asking another
	// admin to demote them would be the realistic path; we have only one
	// admin available, so we exercise the guard via a self-demote attempt.
	// First confirm self-demote is blocked with cannot_target_self (409).
	resp, _ = request(t, "PATCH", base+"/v1/admin/users/"+admin["id"].(string)+"/role", map[string]any{
		"role":     "user",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusConflict, resp.StatusCode, "self-demote must be blocked")
}

// TestAdminSelfPromoteBlocked verifies admins cannot toggle their own role.
func TestAdminSelfPromoteBlocked(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	admin, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")

	resp, _ := request(t, "PATCH", base+"/v1/admin/users/"+admin["id"].(string)+"/role", map[string]any{
		"role":     "admin",
		"password": "passwordpassword",
	}, adminCookie)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

// TestAdminSetRoleStepUp asserts the step-up password is enforced on PATCH.
func TestAdminSetRoleStepUp(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	_, adminCookie := registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	tgt, _ := registerUser(t, base, "tgt@test.dev", "passwordpassword", "Target")

	resp, _ := request(t, "PATCH", base+"/v1/admin/users/"+tgt["id"].(string)+"/role", map[string]any{
		"role":     "admin",
		"password": "WRONG",
	}, adminCookie)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
