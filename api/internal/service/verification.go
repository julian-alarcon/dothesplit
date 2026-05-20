package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

// verifyMaxAttempts is the per-token limit before further submissions are
// rate-limited regardless of correctness. Mirrors the step-up password rate
// limit in spirit but applies per-token (the token itself expires in 15min).
const verifyMaxAttempts = 5

// generateNumericCode returns a uniformly-random N-digit string. The result
// is zero-padded so the leading-zero codes are not shorter than N.
func generateNumericCode(digits int) (string, error) {
	if digits <= 0 || digits > 9 {
		return "", errors.New("digits out of range")
	}
	max := int64(1)
	for i := 0; i < digits; i++ {
		max *= 10
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", digits, n.Int64()), nil
}

// hashCode applies SHA-256 to the verification code. Collisions are not a
// concern for a 6-digit space; constant-time comparison happens at the
// caller using subtle.ConstantTimeCompare via bytes.Equal-equivalent logic.
func hashCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// VerifyEmail completes a pending registration by matching the 6-digit code,
// stamping email_verified_at, and issuing the session that Register withheld.
func (s *AuthService) VerifyEmail(ctx context.Context, email, code string) (*User, string, error) {
	emailHash := s.email.HashEmail(email)
	u, err := s.users.FindByEmailHash(ctx, emailHash)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, "", ErrInvalidCode
	}
	if err != nil {
		return nil, "", err
	}
	if u.EmailVerifiedAt != nil {
		// Already verified — treat the code as expired/used so we don't leak
		// information about prior tokens.
		return nil, "", ErrCodeExpired
	}

	tok, err := s.verification.FindActive(ctx, u.ID, repo.PurposeRegister)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, "", ErrCodeExpired
	}
	if err != nil {
		return nil, "", err
	}
	if tok.Attempts >= verifyMaxAttempts {
		return nil, "", ErrVerifyRateLimited
	}
	if !constantTimeEqual(tok.CodeHash, hashCode(strings.TrimSpace(code))) {
		_ = s.verification.IncrementAttempts(ctx, tok.ID)
		meta, _ := json.Marshal(map[string]any{"attempts": int(tok.Attempts) + 1})
		_ = s.audit.Insert(ctx, nil, &repo.AuditEntry{
			ActorUserID:  u.ID,
			TargetUserID: &u.ID,
			Action:       "email_verify_failed",
			Success:      false,
			Metadata:     meta,
		})
		return nil, "", ErrInvalidCode
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	if err := s.verification.Consume(ctx, tx, tok.ID); err != nil {
		return nil, "", err
	}
	if err := s.users.MarkEmailVerified(ctx, tx, u.ID); err != nil {
		return nil, "", err
	}
	// Welcome email is best-effort; queue inside the same tx so it commits
	// atomically with verification.
	if s.mailer != nil {
		_ = s.mailer.Enqueue(ctx, tx, email, "welcome", TemplateVars{DisplayName: u.DisplayName})
	}
	_ = s.audit.Insert(ctx, tx, &repo.AuditEntry{
		ActorUserID:  u.ID,
		TargetUserID: &u.ID,
		Action:       "email_verified",
		Success:      true,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}

	// Re-fetch so the returned User has email_verified_at populated.
	u2, err := s.users.FindByID(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	out, err := s.toUser(u2)
	if err != nil {
		return nil, "", err
	}
	token, err := s.issueSession(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	return out, token, nil
}

// ResendVerification invalidates the previous code (if any) and issues a
// fresh one. To avoid account enumeration the function never returns an
// error specific to "no such user" or "already verified" — it returns nil
// silently and the handler always responds 204.
func (s *AuthService) ResendVerification(ctx context.Context, email string) error {
	if s.mailer == nil {
		return nil
	}
	smtpReady, _ := s.mailer.IsConfigured(ctx)
	if !smtpReady {
		return nil
	}
	emailHash := s.email.HashEmail(email)
	u, err := s.users.FindByEmailHash(ctx, emailHash)
	if errors.Is(err, repo.ErrNotFound) {
		return nil
	}
	if err != nil {
		return nil
	}
	if u.EmailVerifiedAt != nil {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil
	}
	defer tx.Rollback(ctx)

	if err := s.verification.InvalidateAll(ctx, tx, u.ID, repo.PurposeRegister); err != nil {
		return nil
	}
	code, err := generateNumericCode(6)
	if err != nil {
		return nil
	}
	if err := s.verification.Insert(ctx, tx, &repo.VerificationToken{
		UserID:    u.ID,
		Purpose:   repo.PurposeRegister,
		CodeHash:  hashCode(code),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}); err != nil {
		return nil
	}
	if err := s.mailer.Enqueue(ctx, tx, email, "verify_register", TemplateVars{
		DisplayName: u.DisplayName,
		Code:        code,
	}); err != nil {
		return nil
	}
	_ = tx.Commit(ctx)
	return nil
}

// RequestEmailChange begins a change-email flow. The current password is
// re-verified (step-up); a 6-digit code is sent to the *new* address. The
// new address is held in the token row and only persisted to users.email
// when ConfirmEmailChange succeeds.
func (s *AuthService) RequestEmailChange(ctx context.Context, userID uuid.UUID, newEmail, password string) error {
	newEmail = strings.TrimSpace(newEmail)
	if newEmail == "" {
		return errors.New("new_email is required")
	}
	if err := s.VerifyPassword(ctx, userID, password); err != nil {
		return err
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return ErrInvalidCredentials
	}
	currentEmail, err := s.email.Decrypt(u.EmailEncrypted)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(currentEmail), newEmail) {
		return ErrEmailTaken
	}
	newHash := s.email.HashEmail(newEmail)
	if other, err := s.users.FindByEmailHash(ctx, newHash); err == nil && other.ID != userID {
		return ErrEmailTaken
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}

	smtpReady := false
	if s.mailer != nil {
		ok, _ := s.mailer.IsConfigured(ctx)
		smtpReady = ok
	}
	if !smtpReady {
		// We never silently change the email without a confirmation hop:
		// surface a clear error so the admin knows SMTP is required.
		return errors.New("email change requires SMTP to be configured")
	}

	newEnc, err := s.email.Encrypt(newEmail)
	if err != nil {
		return err
	}
	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.verification.InvalidateAll(ctx, tx, userID, repo.PurposeChangeEmail); err != nil {
		return err
	}
	if err := s.verification.Insert(ctx, tx, &repo.VerificationToken{
		UserID:       userID,
		Purpose:      repo.PurposeChangeEmail,
		CodeHash:     hashCode(code),
		NewEmailHash: newHash,
		NewEmailEnc:  newEnc,
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}); err != nil {
		return err
	}
	if err := s.mailer.Enqueue(ctx, tx, newEmail, "verify_change_email", TemplateVars{
		DisplayName: u.DisplayName,
		Code:        code,
		NewEmail:    newEmail,
	}); err != nil {
		return err
	}
	_ = s.audit.Insert(ctx, tx, &repo.AuditEntry{
		ActorUserID:  userID,
		TargetUserID: &userID,
		Action:       "email_change_requested",
		Success:      true,
	})
	return tx.Commit(ctx)
}

// ConfirmEmailChange checks the code and, on success, swaps email_hash +
// email_encrypted over to the values cached on the token row. All sessions
// for the user are revoked and a fresh session token is returned so the
// current browser stays logged in.
func (s *AuthService) ConfirmEmailChange(ctx context.Context, userID uuid.UUID, code string) (*User, string, error) {
	tok, err := s.verification.FindActive(ctx, userID, repo.PurposeChangeEmail)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, "", ErrCodeExpired
	}
	if err != nil {
		return nil, "", err
	}
	if tok.Attempts >= verifyMaxAttempts {
		return nil, "", ErrVerifyRateLimited
	}
	if !constantTimeEqual(tok.CodeHash, hashCode(strings.TrimSpace(code))) {
		_ = s.verification.IncrementAttempts(ctx, tok.ID)
		return nil, "", ErrInvalidCode
	}
	if len(tok.NewEmailHash) == 0 || len(tok.NewEmailEnc) == 0 {
		return nil, "", ErrCodeExpired
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	if err := s.verification.Consume(ctx, tx, tok.ID); err != nil {
		return nil, "", err
	}
	if err := s.users.UpdateEmail(ctx, tx, userID, tok.NewEmailHash, tok.NewEmailEnc); err != nil {
		// Translate the partial-unique-index violation into ErrEmailTaken
		// so the handler maps it to 409 instead of a 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, "", ErrEmailTaken
		}
		return nil, "", err
	}
	_ = s.audit.Insert(ctx, tx, &repo.AuditEntry{
		ActorUserID:  userID,
		TargetUserID: &userID,
		Action:       "email_changed",
		Success:      true,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}

	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return nil, "", err
	}
	u2, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	out, err := s.toUser(u2)
	if err != nil {
		return nil, "", err
	}
	token, err := s.issueSession(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	return out, token, nil
}

// constantTimeEqual is a simple wrapper around subtle.ConstantTimeCompare
// returning a bool. We hash both sides first so the comparison length is
// fixed (32 bytes for SHA-256).
func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// Compile-time check: pgconn.PgError remains the type pgx surfaces.
var _ = (*pgconn.PgError)(nil)

// Reference crypto so go vet doesn't complain in case future refactors drop
// the import path elsewhere.
var _ = crypto.NewEmailCipher
