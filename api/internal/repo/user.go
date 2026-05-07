package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID              uuid.UUID
	EmailHash       []byte
	EmailEncrypted  []byte
	DisplayName     string
	PasswordHash    string
	CreatedAt       time.Time
	DeletedAt       *time.Time
	Avatar          []byte
	AvatarUpdatedAt *time.Time
	WeekStart       int16
}

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(p *pgxpool.Pool) *UserRepo { return &UserRepo{pool: p} }

const userCols = `id, email_hash, email_encrypted, display_name, password_hash, created_at, deleted_at, avatar_updated_at, week_start`

func scanUser(row pgx.Row, u *User) error {
	return row.Scan(&u.ID, &u.EmailHash, &u.EmailEncrypted, &u.DisplayName,
		&u.PasswordHash, &u.CreatedAt, &u.DeletedAt, &u.AvatarUpdatedAt, &u.WeekStart)
}

func (r *UserRepo) Create(ctx context.Context, u *User) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO users (email_hash, email_encrypted, display_name, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`, u.EmailHash, u.EmailEncrypted, u.DisplayName, u.PasswordHash).Scan(&u.ID, &u.CreatedAt)
}

// FindByEmailHash returns only non-deleted users.
func (r *UserRepo) FindByEmailHash(ctx context.Context, emailHash []byte) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx, `
		SELECT `+userCols+`
		FROM users WHERE email_hash = $1 AND deleted_at IS NULL
	`, emailHash), &u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID returns the user regardless of soft-delete state. Callers that want
// only active users should check DeletedAt themselves.
func (r *UserRepo) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := scanUser(r.pool.QueryRow(ctx, `
		SELECT `+userCols+`
		FROM users WHERE id = $1
	`, id), &u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateDisplayName renames the user.
func (r *UserRepo) UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET display_name = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeekStart sets the user's preferred first day of the week.
// Caller must validate that v is in the allowed set (currently 0 or 1).
func (r *UserRepo) UpdateWeekStart(ctx context.Context, id uuid.UUID, v int16) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET week_start = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, v)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePasswordHash rotates the encoded Argon2id hash.
func (r *UserRepo) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, hash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAvatar stores the normalized 8x8 PNG bytes and stamps avatar_updated_at.
// Pass nil png to clear the avatar.
func (r *UserRepo) SetAvatar(ctx context.Context, id uuid.UUID, png []byte) error {
	var tag pgconn.CommandTag
	var err error
	if png == nil {
		tag, err = r.pool.Exec(ctx, `
			UPDATE users SET avatar = NULL, avatar_updated_at = NULL
			WHERE id = $1 AND deleted_at IS NULL
		`, id)
	} else {
		tag, err = r.pool.Exec(ctx, `
			UPDATE users SET avatar = $2, avatar_updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL
		`, id, png)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetAvatar returns the raw PNG bytes or ErrNotFound when the user has no avatar.
func (r *UserRepo) GetAvatar(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var png []byte
	err := r.pool.QueryRow(ctx, `
		SELECT avatar FROM users WHERE id = $1
	`, id).Scan(&png)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if png == nil {
		return nil, ErrNotFound
	}
	return png, nil
}

// SoftDelete marks the account as deleted, scrubs identifying fields, and
// renames the user to a tombstone referencing their own UUID so historical
// ledger entries stay traceable. Sessions should be cleared separately.
func (r *UserRepo) SoftDelete(ctx context.Context, id uuid.UUID, tombstone string, scrambledHash, scrambledEnc []byte, scrambledPwHash string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET
			deleted_at        = now(),
			display_name      = $2,
			email_hash        = $3,
			email_encrypted   = $4,
			password_hash     = $5,
			avatar            = NULL,
			avatar_updated_at = NULL
		WHERE id = $1 AND deleted_at IS NULL
	`, id, tombstone, scrambledHash, scrambledEnc, scrambledPwHash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
