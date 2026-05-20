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
	ID                 uuid.UUID
	EmailHash          []byte
	EmailEncrypted     []byte
	DisplayName        string
	PasswordHash       string
	CreatedAt          time.Time
	DeletedAt          *time.Time
	Avatar             []byte
	AvatarUpdatedAt    *time.Time
	WeekStart          int16
	Timezone           *string
	Role               string
	MustChangePassword bool
	EmailVerifiedAt    *time.Time
	// NotificationPrefs is the raw JSONB blob; service layer parses it into
	// a typed projection so callers don't deal with map[string]any here.
	NotificationPrefs []byte
}

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(p *pgxpool.Pool) *UserRepo { return &UserRepo{pool: p} }

const userCols = `id, email_hash, email_encrypted, display_name, password_hash, created_at, deleted_at, avatar_updated_at, week_start, timezone, role, must_change_password, email_verified_at, notification_prefs`

func scanUser(row pgx.Row, u *User) error {
	return row.Scan(&u.ID, &u.EmailHash, &u.EmailEncrypted, &u.DisplayName,
		&u.PasswordHash, &u.CreatedAt, &u.DeletedAt, &u.AvatarUpdatedAt, &u.WeekStart, &u.Timezone,
		&u.Role, &u.MustChangePassword, &u.EmailVerifiedAt, &u.NotificationPrefs)
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

// UpdateTimezone sets the user's IANA timezone override. Pass nil to clear the
// override (which means "use device-detected zone" on the client). Caller must
// validate the IANA name before persisting.
func (r *UserRepo) UpdateTimezone(ctx context.Context, id uuid.UUID, tz *string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET timezone = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, tz)
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

// UpdatePasswordHashWithFlag rotates the encoded Argon2id hash and sets
// must_change_password atomically in a single UPDATE so a half-applied state
// (new hash, stale flag) cannot leave the user trapped.
func (r *UserRepo) UpdatePasswordHashWithFlag(ctx context.Context, id uuid.UUID, hash string, mustChange bool) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, must_change_password = $3
		WHERE id = $1 AND deleted_at IS NULL
	`, id, hash, mustChange)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActive returns the number of non-deleted users. Used by the bootstrap
// path inside a transaction with an advisory lock to detect "first user".
func (r *UserRepo) CountActive(ctx context.Context, q Querier) (int, error) {
	var n int
	if q == nil {
		q = poolQuerier{r.pool}
	}
	err := q.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// CountActiveAdmins returns the number of non-deleted admins. Used by the
// last-admin guard before delete/demote.
func (r *UserRepo) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role='admin' AND deleted_at IS NULL`).Scan(&n)
	return n, err
}

// SetRole sets the user's role. Caller is responsible for the last-admin guard
// when demoting.
func (r *UserRepo) SetRole(ctx context.Context, id uuid.UUID, role string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE users SET role = $2 WHERE id = $1 AND deleted_at IS NULL`, id, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMustChangePassword toggles the force-change flag. Used by admin reset
// flows that don't change the hash directly (rare).
func (r *UserRepo) SetMustChangePassword(ctx context.Context, id uuid.UUID, v bool) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE users SET must_change_password = $2 WHERE id = $1 AND deleted_at IS NULL`, id, v)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPaginated returns a page of users ordered by created_at DESC.
// includeDeleted controls whether soft-deleted rows are returned.
// Returns the rows plus the total count for paginator UIs.
func (r *UserRepo) ListPaginated(ctx context.Context, limit, offset int, includeDeleted bool) ([]User, int, error) {
	where := "WHERE deleted_at IS NULL"
	if includeDeleted {
		where = ""
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM users `+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+userCols+` FROM users
		`+where+`
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := scanUser(rows, &u); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// CreateWithRole inserts a user with an explicit role. Returns the created
// user's id and created_at. Intended for admin-driven user creation and the
// bootstrap-admin path. The caller is responsible for hashing the password.
// If tx is non-nil the insert participates in it; otherwise a new pool query
// is used.
func (r *UserRepo) CreateWithRole(ctx context.Context, q Querier, u *User, role string, mustChange bool) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	return q.QueryRow(ctx, `
		INSERT INTO users (email_hash, email_encrypted, display_name, password_hash, role, must_change_password)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, u.EmailHash, u.EmailEncrypted, u.DisplayName, u.PasswordHash, role, mustChange).
		Scan(&u.ID, &u.CreatedAt)
}

// MarkEmailVerified stamps email_verified_at = now() and is idempotent: a
// second call is a no-op rather than an error so the verify flow can be
// safely retried by the user without producing a 404.
func (r *UserRepo) MarkEmailVerified(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	_, err := q.Exec(ctx, `
		UPDATE users SET email_verified_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND email_verified_at IS NULL
	`, id)
	return err
}

// UpdateEmail replaces the user's email_hash and email_encrypted atomically.
// Caller is responsible for the soft-delete-aware uniqueness check (the
// partial unique index `users_email_hash_active_key` will surface a unique
// violation as a pgconn.PgError with code 23505).
func (r *UserRepo) UpdateEmail(ctx context.Context, q Querier, id uuid.UUID, emailHash, emailEnc []byte) error {
	if q == nil {
		q = poolQuerier{r.pool}
	}
	ct, err := q.Exec(ctx, `
		UPDATE users SET email_hash = $2, email_encrypted = $3
		WHERE id = $1 AND deleted_at IS NULL
	`, id, emailHash, emailEnc)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateNotificationPrefs writes the JSONB blob verbatim. The caller is
// responsible for validating keys (the service layer rejects unknown ones).
func (r *UserRepo) UpdateNotificationPrefs(ctx context.Context, id uuid.UUID, prefs []byte) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET notification_prefs = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, id, prefs)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
