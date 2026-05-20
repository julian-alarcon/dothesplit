package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

var (
	// ErrSetupCompleted is returned when /v1/setup/admin is called after the
	// install ceremony has already finished. Handlers map to 410 Gone.
	ErrSetupCompleted = errors.New("setup already completed")
	// ErrInvalidToken is returned when the supplied setup token does not
	// match the SHA-256 hash on app_setup. Handlers map to 401.
	ErrInvalidToken = errors.New("invalid setup token")
)

// setupTokenBytes is the entropy of every freshly-generated install token
// (base64 RawURL encoded for transport).
const setupTokenBytes = 32

// SetupService owns the first-run install ceremony: generating + rotating
// the cleartext token at boot, and atomically minting the very first admin
// in lockstep with marking app_setup.completed_at.
type SetupService struct {
	pool  *pgxpool.Pool
	repo  *repo.SetupRepo
	auth  *AuthService
	audit *repo.AuditRepo
}

func NewSetupService(pool *pgxpool.Pool, r *repo.SetupRepo, auth *AuthService, audit *repo.AuditRepo) *SetupService {
	return &SetupService{pool: pool, repo: r, auth: auth, audit: audit}
}

// Locked reports whether the install ceremony is finished. Wraps the repo.
func (s *SetupService) Locked(ctx context.Context) (bool, error) {
	return s.repo.Locked(ctx)
}

// EnsureToken is called from main.go on every boot. While the ceremony is
// pending it rotates the token (new random 32 bytes, new SHA-256 hash, same
// row) and returns the cleartext so the boot banner can print it. Once the
// ceremony has completed, returns alreadyCompleted=true and an empty
// cleartext — there is no path that re-issues a post-install token.
//
// Rotation rationale: we never persist the cleartext anywhere. If the
// operator restarts before completing setup, they pick up the latest
// banner's token; the previous one is dead. This is the same model TLS
// bootstrap, K3s, and Forgejo use; storing cleartext lowers the bar for
// any DB-read attacker (e.g., a leaked logical-backup dump).
func (s *SetupService) EnsureToken(ctx context.Context) (cleartext string, freshlyGenerated bool, alreadyCompleted bool, err error) {
	cur, err := s.repo.Get(ctx)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return "", false, false, err
	}
	if cur != nil && cur.CompletedAt != nil {
		return "", false, true, nil
	}
	raw := make([]byte, setupTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", false, false, fmt.Errorf("setup: read random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if err := s.repo.Upsert(ctx, hash[:], time.Now()); err != nil {
		return "", false, false, fmt.Errorf("setup: persist token: %w", err)
	}
	return token, true, false, nil
}

// CompleteWithToken is the atomic install ceremony.
//
// Sequence inside one transaction:
//  1. SELECT … FOR UPDATE on app_setup → primary serializer; second caller
//     blocks until first commits/rolls back.
//  2. Bail out if completed_at is already set (someone else just won).
//  3. constant-time compare SHA-256(supplied) vs stored hash.
//  4. AuthService.RegisterTx — bootstrap path applies because count==0
//     under the same tx, so the new user gets role='admin' and a
//     'bootstrap_admin' audit row is inserted.
//  5. SetupRepo.Complete — stamps completed_at + completed_by.
//  6. AuditRepo.Insert with action='setup_completed'.
//  7. Commit. Session is issued post-tx (issuance failure means the operator
//     just logs in normally; the install state is already committed).
func (s *SetupService) CompleteWithToken(ctx context.Context, tokenCT, email, displayName, password string) (*User, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	var hash []byte
	var completedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT token_hash, completed_at FROM app_setup
		 WHERE id = true
		   FOR UPDATE
	`).Scan(&hash, &completedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrInvalidToken
		}
		return nil, "", err
	}
	if completedAt != nil {
		return nil, "", ErrSetupCompleted
	}

	supplied := sha256.Sum256([]byte(tokenCT))
	if subtle.ConstantTimeCompare(supplied[:], hash) != 1 {
		return nil, "", ErrInvalidToken
	}

	out, repoUser, err := s.auth.RegisterTx(ctx, tx, email, password, displayName)
	if err != nil {
		return nil, "", err
	}

	// Bootstrap admin is auto-verified — they typed their email into the
	// setup form themselves, and SMTP definitionally isn't configured yet.
	if _, err := tx.Exec(ctx, `UPDATE users SET email_verified_at = now() WHERE id = $1`, repoUser.ID); err != nil {
		return nil, "", err
	}
	out.EmailVerifiedAt = ptrNow()

	if err := s.repo.Complete(ctx, tx, repoUser.ID); err != nil {
		return nil, "", err
	}
	meta, _ := json.Marshal(map[string]any{"reason": "first_admin_created"})
	if err := s.audit.Insert(ctx, tx, &repo.AuditEntry{
		ActorUserID: repoUser.ID,
		Action:      "setup_completed",
		Success:     true,
		Metadata:    meta,
	}); err != nil {
		return nil, "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}

	token, err := s.auth.IssueSession(ctx, repoUser.ID)
	if err != nil {
		return nil, "", err
	}
	return out, token, nil
}
