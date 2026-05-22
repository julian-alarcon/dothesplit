package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Setup is the single-row first-run install state.
type Setup struct {
	TokenHash        []byte
	TokenGeneratedAt time.Time
	CompletedAt      *time.Time
	CompletedBy      *uuid.UUID
}

type SetupRepo struct {
	pool *pgxpool.Pool
}

func NewSetupRepo(p *pgxpool.Pool) *SetupRepo { return &SetupRepo{pool: p} }

// Get returns the row, or ErrNotFound when no install ceremony has yet been
// kicked off (the boot path lazily inserts it).
func (r *SetupRepo) Get(ctx context.Context) (*Setup, error) {
	var s Setup
	err := r.pool.QueryRow(ctx, `
		SELECT token_hash, token_generated_at, completed_at, completed_by
		FROM app_setup WHERE id = true
	`).Scan(&s.TokenHash, &s.TokenGeneratedAt, &s.CompletedAt, &s.CompletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert writes the (hash, generatedAt) pair. The UPDATE branch is gated on
// `completed_at IS NULL` so a once-completed row can never have its token
// rotated underneath it - re-opening setup is impossible without manual SQL.
func (r *SetupRepo) Upsert(ctx context.Context, hash []byte, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO app_setup (id, token_hash, token_generated_at)
		VALUES (true, $1, $2)
		ON CONFLICT (id) DO UPDATE SET
			token_hash = EXCLUDED.token_hash,
			token_generated_at = EXCLUDED.token_generated_at
		WHERE app_setup.completed_at IS NULL
	`, hash, at)
	return err
}

// Complete marks setup as finished. q may be a *pgx.Tx so completion can be
// committed atomically with the admin-user creation in SetupService.
func (r *SetupRepo) Complete(ctx context.Context, q Querier, by uuid.UUID) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	_, err := q.Exec(ctx, `
		UPDATE app_setup
		   SET completed_at = now(), completed_by = $1
		 WHERE id = true AND completed_at IS NULL
	`, by)
	return err
}

// Locked reports whether the install ceremony is finished. Returns false (=>
// "still in setup mode") when no row exists yet OR the row's completed_at is
// null. Returns true when completed_at IS NOT NULL.
func (r *SetupRepo) Locked(ctx context.Context) (bool, error) {
	var locked bool
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT completed_at IS NOT NULL FROM app_setup WHERE id = true),
			false
		)
	`).Scan(&locked)
	return locked, err
}
