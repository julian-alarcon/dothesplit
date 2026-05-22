package server_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// configureSMTP writes a single smtp_config row so the API treats the
// instance as SMTP-configured. Real connectivity isn't needed - only the
// API path that *enqueues* (and triggers the verification flow) is
// exercised here.
func configureSMTP(t *testing.T, ts *testStack) {
	t.Helper()
	_, err := ts.pool.Exec(context.Background(), `
		INSERT INTO smtp_config (id, host, port, from_address, tls_mode)
		VALUES (true, 'localhost', 2525, 'noreply@example.test', 'none')
		ON CONFLICT (id) DO UPDATE SET
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			from_address = EXCLUDED.from_address,
			tls_mode = EXCLUDED.tls_mode
	`)
	require.NoError(t, err)
}

// pinVerificationCode overwrites the most recent unconsumed register-purpose
// token's code_hash to SHA-256(known) so the test can submit `known`. We
// can't recover the cleartext from the row (it's hashed) and the email body
// is plain text in the outbox - this is the simplest equivalent.
func pinVerificationCode(t *testing.T, ts *testStack, known string) {
	t.Helper()
	sum := sha256.Sum256([]byte(known))
	tag, err := ts.pool.Exec(context.Background(), `
		UPDATE email_verification_tokens
		SET code_hash = $1, attempts = 0
		WHERE id = (
			SELECT id FROM email_verification_tokens
			WHERE consumed_at IS NULL AND purpose = 'register'
			ORDER BY created_at DESC LIMIT 1
		)
	`, sum[:])
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one pending register token")
}

// pinPasswordResetCode is the password_reset analogue of
// pinVerificationCode: rewrites the most recent active password_reset
// token's code_hash so the test can submit a known code.
func pinPasswordResetCode(t *testing.T, ts *testStack, known string) {
	t.Helper()
	sum := sha256.Sum256([]byte(known))
	tag, err := ts.pool.Exec(context.Background(), `
		UPDATE email_verification_tokens
		SET code_hash = $1, attempts = 0
		WHERE id = (
			SELECT id FROM email_verification_tokens
			WHERE consumed_at IS NULL AND purpose = 'password_reset'
			ORDER BY created_at DESC LIMIT 1
		)
	`, sum[:])
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one pending password_reset token")
}

// TestEmailVerificationFlow covers the SMTP-configured registration path.
func TestEmailVerificationFlow(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	// Bootstrap admin first via the install ceremony (no SMTP yet → auto-verified).
	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")

	// Configure SMTP so subsequent registrations enter the verify path.
	configureSMTP(t, ts)

	body := map[string]any{
		"email":        "alice@test.dev",
		"password":     "passwordpassword",
		"display_name": "Alice",
	}
	resp, out := request(t, "POST", base+"/v1/auth/register", body, nil)
	require.Equal(t, http.StatusCreated, resp.StatusCode, out)
	vreq, ok := out["verification_required"].(bool)
	require.True(t, ok, "missing verification_required: %v", out)
	require.True(t, vreq)
	require.Nil(t, sessionCookie(resp), "register must not set a session cookie when verification is required")

	// Login is refused with 403 + email_unverified.
	resp, errBody := request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email":    "alice@test.dev",
		"password": "passwordpassword",
	}, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, "email_unverified", errBody["code"])

	// Pin the code and submit it.
	knownCode := "123456"
	pinVerificationCode(t, ts, knownCode)

	resp, _ = request(t, "POST", base+"/v1/auth/verify", map[string]any{
		"email": "alice@test.dev",
		"code":  knownCode,
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	c := sessionCookie(resp)
	require.NotNil(t, c, "verify must set a session cookie on success")

	// Login now succeeds.
	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email":    "alice@test.dev",
		"password": "passwordpassword",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestEmailVerificationWrongCode confirms a bad code returns 400, then that
// the attempts counter blocks further submissions after 5 misses.
func TestEmailVerificationWrongCode(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	configureSMTP(t, ts)
	_, _ = request(t, "POST", base+"/v1/auth/register", map[string]any{
		"email":        "bob@test.dev",
		"password":     "passwordpassword",
		"display_name": "Bob",
	}, nil)

	for i := 0; i < 5; i++ {
		resp, _ := request(t, "POST", base+"/v1/auth/verify", map[string]any{
			"email": "bob@test.dev",
			"code":  "000000",
		}, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	}
	// 6th attempt should be rate-limited.
	resp, _ := request(t, "POST", base+"/v1/auth/verify", map[string]any{
		"email": "bob@test.dev",
		"code":  "000000",
	}, nil)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
}

// TestNotificationPrefsRoundTrip writes prefs through the API and reads them
// back. Defaults are off; PATCH replaces the blob.
func TestNotificationPrefsRoundTrip(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL
	_, cookie := registerUser(t, base, "carol@test.dev", "passwordpassword", "Carol")

	// Initial GET - all flags off (absent or false).
	resp, out := request(t, "GET", base+"/v1/me/notifications", nil, cookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	if v, ok := out["notify_settlement"].(bool); ok {
		require.False(t, v)
	}

	// PATCH - turn settlement on.
	resp, _ = request(t, "PATCH", base+"/v1/me/notifications", map[string]any{
		"notify_settlement": true,
	}, cookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Read back.
	resp, out = request(t, "GET", base+"/v1/me/notifications", nil, cookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	v, ok := out["notify_settlement"].(bool)
	require.True(t, ok, "missing notify_settlement: %v", out)
	require.True(t, v)
	if v2, ok2 := out["notify_recurring_run"].(bool); ok2 {
		require.False(t, v2)
	}
}

// TestRegisterAutoVerifiesWithoutSMTP confirms the bootstrap-friendly
// fallback: with no smtp_config row, registration sets a session cookie
// directly and login works immediately.
func TestRegisterAutoVerifiesWithoutSMTP(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL
	// First registration goes through the install ceremony (sets a cookie too).
	_, cookie := registerUser(t, base, "first@test.dev", "passwordpassword", "First")
	require.NotNil(t, cookie)

	// Second registration still has no SMTP configured → 201 with cookie,
	// verification_required=false, login works without a verify step.
	body := map[string]any{
		"email":        "second@test.dev",
		"password":     "passwordpassword",
		"display_name": "Second",
	}
	resp, out := request(t, "POST", base+"/v1/auth/register", body, nil)
	require.Equal(t, http.StatusCreated, resp.StatusCode, out)
	if v, ok := out["verification_required"].(bool); ok {
		require.False(t, v)
	}
	require.NotNil(t, sessionCookie(resp))

	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email":    "second@test.dev",
		"password": "passwordpassword",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestPasswordResetHappyPath covers the full forgot-password flow:
// register a user (auto-verified, no SMTP yet) → configure SMTP →
// request reset → pin known code → confirm with new password → log in
// with the new password.
func TestPasswordResetHappyPath(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	registerUser(t, base, "alice@test.dev", "passwordpassword", "Alice")
	configureSMTP(t, ts)

	resp, _ := request(t, "POST", base+"/v1/auth/password-reset/request", map[string]any{
		"email": "alice@test.dev",
	}, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	knownCode := "654321"
	pinPasswordResetCode(t, ts, knownCode)

	resp, _ = request(t, "POST", base+"/v1/auth/password-reset/confirm", map[string]any{
		"email":        "alice@test.dev",
		"code":         knownCode,
		"new_password": "newpasswordnewpassword",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, sessionCookie(resp), "confirm must set a session cookie on success")

	// Old password no longer works.
	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email":    "alice@test.dev",
		"password": "passwordpassword",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// New password works.
	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email":    "alice@test.dev",
		"password": "newpasswordnewpassword",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestPasswordResetWrongCode confirms 5 wrong attempts each return 400 and
// the 6th flips to 429 (token attempts >= verifyMaxAttempts).
func TestPasswordResetWrongCode(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	registerUser(t, base, "bob@test.dev", "passwordpassword", "Bob")
	configureSMTP(t, ts)

	resp, _ := request(t, "POST", base+"/v1/auth/password-reset/request", map[string]any{
		"email": "bob@test.dev",
	}, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	for i := 0; i < 5; i++ {
		resp, _ := request(t, "POST", base+"/v1/auth/password-reset/confirm", map[string]any{
			"email":        "bob@test.dev",
			"code":         "000000",
			"new_password": "newpasswordnewpassword",
		}, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	}
	// 6th attempt should be rate-limited.
	resp, _ = request(t, "POST", base+"/v1/auth/password-reset/confirm", map[string]any{
		"email":        "bob@test.dev",
		"code":         "000000",
		"new_password": "newpasswordnewpassword",
	}, nil)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
}

// TestPasswordResetExpired forces the token's expires_at into the past and
// expects 410 Gone on confirm.
func TestPasswordResetExpired(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	registerUser(t, base, "carol@test.dev", "passwordpassword", "Carol")
	configureSMTP(t, ts)

	resp, _ := request(t, "POST", base+"/v1/auth/password-reset/request", map[string]any{
		"email": "carol@test.dev",
	}, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	knownCode := "999999"
	pinPasswordResetCode(t, ts, knownCode)
	// Force-expire the token.
	_, err := ts.pool.Exec(context.Background(),
		`UPDATE email_verification_tokens SET expires_at = now() - interval '1 minute' WHERE consumed_at IS NULL AND purpose = 'password_reset'`)
	require.NoError(t, err)

	resp, _ = request(t, "POST", base+"/v1/auth/password-reset/confirm", map[string]any{
		"email":        "carol@test.dev",
		"code":         knownCode,
		"new_password": "newpasswordnewpassword",
	}, nil)
	require.Equal(t, http.StatusGone, resp.StatusCode)
}

// TestPasswordResetEnumerationSafe asserts that requesting reset for an
// unknown email still returns 204 and never creates a token row.
func TestPasswordResetEnumerationSafe(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	registerUser(t, base, "admin@test.dev", "passwordpassword", "Admin")
	configureSMTP(t, ts)

	resp, _ := request(t, "POST", base+"/v1/auth/password-reset/request", map[string]any{
		"email": "ghost@test.dev",
	}, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	var n int
	err := ts.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_verification_tokens WHERE purpose = 'password_reset'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 0, n, "no password_reset token should be created for an unknown email")
}

