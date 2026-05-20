package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VerificationPurpose string

const (
	PurposeRegister      VerificationPurpose = "register"
	PurposeChangeEmail   VerificationPurpose = "change_email"
	PurposePasswordReset VerificationPurpose = "password_reset"
)

// VerificationToken stores a hashed 6-digit code. The code itself is never
// persisted; only SHA-256(code). For change_email we also stash the new
// address (hash + AES-GCM ciphertext) so confirm has everything it needs in
// one row without a separate scratch table.
type VerificationToken struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Purpose       VerificationPurpose
	CodeHash      []byte
	NewEmailHash  []byte
	NewEmailEnc   []byte
	Attempts      int16
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	CreatedAt     time.Time
}

type VerificationRepo struct {
	pool *pgxpool.Pool
}

func NewVerificationRepo(p *pgxpool.Pool) *VerificationRepo { return &VerificationRepo{pool: p} }

// Insert creates a fresh token row. May participate in an outer tx.
func (r *VerificationRepo) Insert(ctx context.Context, q Querier, t *VerificationToken) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	return q.QueryRow(ctx, `
		INSERT INTO email_verification_tokens
			(user_id, purpose, code_hash, new_email_hash, new_email_enc, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, attempts, created_at
	`, t.UserID, string(t.Purpose), t.CodeHash, t.NewEmailHash, t.NewEmailEnc, t.ExpiresAt).
		Scan(&t.ID, &t.Attempts, &t.CreatedAt)
}

// FindActive returns the most recent unconsumed, unexpired token for the
// (user, purpose) pair, or ErrNotFound. Used by verify/confirm flows.
func (r *VerificationRepo) FindActive(ctx context.Context, userID uuid.UUID, purpose VerificationPurpose) (*VerificationToken, error) {
	var t VerificationToken
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, purpose, code_hash, new_email_hash, new_email_enc, attempts, expires_at, consumed_at, created_at
		FROM email_verification_tokens
		WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, string(purpose)).Scan(
		&t.ID, &t.UserID, &t.Purpose, &t.CodeHash, &t.NewEmailHash, &t.NewEmailEnc,
		&t.Attempts, &t.ExpiresAt, &t.ConsumedAt, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Consume stamps consumed_at = now() so the token can't be reused.
func (r *VerificationRepo) Consume(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	ct, err := q.Exec(ctx,
		`UPDATE email_verification_tokens SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementAttempts records a failed code submission.
func (r *VerificationRepo) IncrementAttempts(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE email_verification_tokens SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

// InvalidateAll soft-cancels every active token for (user, purpose). Called
// before issuing a fresh code on resend so the previous one stops working
// immediately.
func (r *VerificationRepo) InvalidateAll(ctx context.Context, q Querier, userID uuid.UUID, purpose VerificationPurpose) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	_, err := q.Exec(ctx, `
		UPDATE email_verification_tokens SET consumed_at = now()
		WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL
	`, userID, string(purpose))
	return err
}
