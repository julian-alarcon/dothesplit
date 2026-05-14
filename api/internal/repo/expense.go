package repo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Expense struct {
	ID          uuid.UUID
	GroupID     uuid.UUID
	PayerID     uuid.UUID
	CreatedBy   uuid.UUID
	CategoryID  uuid.UUID
	AmountCents int64
	Currency    string
	Description string
	IncurredAt  time.Time
	CreatedAt   time.Time
	DeletedAt   *time.Time
	Splits      []Split
}

type Split struct {
	ExpenseID  uuid.UUID
	UserID     uuid.UUID
	ShareCents int64
}

type ExpenseRepo struct {
	pool *pgxpool.Pool
}

func NewExpenseRepo(p *pgxpool.Pool) *ExpenseRepo { return &ExpenseRepo{pool: p} }

// CreateWithSplits inserts an expense and its splits atomically.
func (r *ExpenseRepo) CreateWithSplits(ctx context.Context, e *Expense) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO expenses (group_id, payer_id, created_by, category_id, amount_cents, currency, description, incurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`, e.GroupID, e.PayerID, e.CreatedBy, e.CategoryID, e.AmountCents, e.Currency, e.Description, e.IncurredAt).
		Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return err
	}
	for i := range e.Splits {
		e.Splits[i].ExpenseID = e.ID
		if _, err := tx.Exec(ctx, `
			INSERT INTO splits (expense_id, user_id, share_cents) VALUES ($1, $2, $3)
		`, e.ID, e.Splits[i].UserID, e.Splits[i].ShareCents); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListByGroup returns non-deleted expenses with their splits, newest first.
func (r *ExpenseRepo) ListByGroup(ctx context.Context, groupID uuid.UUID) ([]Expense, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, group_id, payer_id, created_by, category_id, amount_cents, currency, description, incurred_at, created_at
		FROM expenses
		WHERE group_id = $1 AND deleted_at IS NULL
		ORDER BY incurred_at DESC, created_at DESC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var exps []Expense
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.GroupID, &e.PayerID, &e.CreatedBy, &e.CategoryID, &e.AmountCents,
			&e.Currency, &e.Description, &e.IncurredAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		exps = append(exps, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(exps) == 0 {
		return exps, nil
	}
	// Fetch splits in a single query.
	ids := make([]uuid.UUID, len(exps))
	for i, e := range exps {
		ids[i] = e.ID
	}
	srows, err := r.pool.Query(ctx, `
		SELECT expense_id, user_id, share_cents FROM splits WHERE expense_id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	splitsByExpense := map[uuid.UUID][]Split{}
	for srows.Next() {
		var s Split
		if err := srows.Scan(&s.ExpenseID, &s.UserID, &s.ShareCents); err != nil {
			return nil, err
		}
		splitsByExpense[s.ExpenseID] = append(splitsByExpense[s.ExpenseID], s)
	}
	for i := range exps {
		exps[i].Splits = splitsByExpense[exps[i].ID]
	}
	return exps, srows.Err()
}

// FindByIDs returns the non-deleted expenses (with their splits) for the given
// IDs, keyed by id. Missing or soft-deleted IDs are simply absent.
func (r *ExpenseRepo) FindByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Expense, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]Expense{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, group_id, payer_id, created_by, category_id, amount_cents, currency, description, incurred_at, created_at
		FROM expenses
		WHERE id = ANY($1) AND deleted_at IS NULL
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]Expense, len(ids))
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.GroupID, &e.PayerID, &e.CreatedBy, &e.CategoryID, &e.AmountCents,
			&e.Currency, &e.Description, &e.IncurredAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out[e.ID] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	hitIDs := make([]uuid.UUID, 0, len(out))
	for id := range out {
		hitIDs = append(hitIDs, id)
	}
	srows, err := r.pool.Query(ctx, `
		SELECT expense_id, user_id, share_cents FROM splits WHERE expense_id = ANY($1)
	`, hitIDs)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var s Split
		if err := srows.Scan(&s.ExpenseID, &s.UserID, &s.ShareCents); err != nil {
			return nil, err
		}
		e := out[s.ExpenseID]
		e.Splits = append(e.Splits, s)
		out[s.ExpenseID] = e
	}
	return out, srows.Err()
}

func (r *ExpenseRepo) FindByID(ctx context.Context, id uuid.UUID) (*Expense, error) {
	var e Expense
	err := r.pool.QueryRow(ctx, `
		SELECT id, group_id, payer_id, created_by, category_id, amount_cents, currency, description, incurred_at, created_at, deleted_at
		FROM expenses WHERE id = $1
	`, id).Scan(&e.ID, &e.GroupID, &e.PayerID, &e.CreatedBy, &e.CategoryID, &e.AmountCents, &e.Currency,
		&e.Description, &e.IncurredAt, &e.CreatedAt, &e.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	srows, err := r.pool.Query(ctx, `
		SELECT expense_id, user_id, share_cents FROM splits WHERE expense_id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var s Split
		if err := srows.Scan(&s.ExpenseID, &s.UserID, &s.ShareCents); err != nil {
			return nil, err
		}
		e.Splits = append(e.Splits, s)
	}
	return &e, srows.Err()
}

func (r *ExpenseRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE expenses SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Update applies description / amount / category / payer / splits changes in one tx.
// If newSplits is non-nil, existing splits are replaced wholesale (the service layer
// has already resolved per-user shares via resolveSplits). Otherwise, if amountCents
// changed, existing splits are rescaled proportionally - preserving each user's
// relative share; rounding remainder goes to the first splits in user_id order.
// Every non-nil change writes an expense_revisions row; split changes are recorded
// as a single 'splits' row with JSON before/after.
func (r *ExpenseRepo) Update(
	ctx context.Context,
	id, editorID uuid.UUID,
	description *string,
	amountCents *int64,
	categoryID *uuid.UUID,
	payerID *uuid.UUID,
	incurredAt *time.Time,
	newSplits []Split,
) (*Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var e Expense
	err = tx.QueryRow(ctx, `
		SELECT id, group_id, payer_id, created_by, category_id, amount_cents, currency, description, incurred_at, created_at, deleted_at
		FROM expenses WHERE id = $1 FOR UPDATE
	`, id).Scan(&e.ID, &e.GroupID, &e.PayerID, &e.CreatedBy, &e.CategoryID, &e.AmountCents, &e.Currency,
		&e.Description, &e.IncurredAt, &e.CreatedAt, &e.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if e.DeletedAt != nil {
		return nil, ErrNotFound
	}

	revisions := []struct{ field, oldV, newV string }{}
	if description != nil && *description != e.Description {
		revisions = append(revisions, struct{ field, oldV, newV string }{"description", e.Description, *description})
		e.Description = *description
	}
	if categoryID != nil && *categoryID != e.CategoryID {
		revisions = append(revisions, struct{ field, oldV, newV string }{"category_id", e.CategoryID.String(), categoryID.String()})
		e.CategoryID = *categoryID
	}
	if payerID != nil && *payerID != e.PayerID {
		revisions = append(revisions, struct{ field, oldV, newV string }{"payer_id", e.PayerID.String(), payerID.String()})
		e.PayerID = *payerID
	}
	if incurredAt != nil && !incurredAt.Equal(e.IncurredAt) {
		// Store as RFC3339 UTC so old/new round-trips through Time.Parse on
		// the frontend. Truncating to the wire precision keeps idempotent
		// re-saves from generating spurious revision rows.
		oldStr := e.IncurredAt.UTC().Format(time.RFC3339)
		newStr := incurredAt.UTC().Format(time.RFC3339)
		if oldStr != newStr {
			revisions = append(revisions, struct{ field, oldV, newV string }{"incurred_at", oldStr, newStr})
			e.IncurredAt = *incurredAt
		}
	}

	existingSplits, err := fetchSplitsForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	var splitsToWrite []Split
	splitsChanged := false
	if newSplits != nil {
		resolved := make([]Split, len(newSplits))
		copy(resolved, newSplits)
		for i := range resolved {
			resolved[i].ExpenseID = id
		}
		splitsChanged = !splitsEqual(existingSplits, resolved)
		if splitsChanged {
			oldJSON, err := marshalSplitsForRevision(existingSplits)
			if err != nil {
				return nil, err
			}
			newJSON, err := marshalSplitsForRevision(resolved)
			if err != nil {
				return nil, err
			}
			revisions = append(revisions, struct{ field, oldV, newV string }{"splits", oldJSON, newJSON})
			splitsToWrite = resolved
		}
	}

	if amountCents != nil && *amountCents != e.AmountCents {
		oldAmount := e.AmountCents
		revisions = append(revisions, struct{ field, oldV, newV string }{
			"amount_cents",
			strconv.FormatInt(oldAmount, 10),
			strconv.FormatInt(*amountCents, 10),
		})
		if splitsToWrite == nil {
			rescaled := rescaleSplits(existingSplits, oldAmount, *amountCents)
			if !splitsEqual(existingSplits, rescaled) {
				splitsToWrite = rescaled
			}
		}
		e.AmountCents = *amountCents
	}

	if len(revisions) == 0 {
		return &e, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE expenses SET description = $2, amount_cents = $3, category_id = $4, payer_id = $5, incurred_at = $6
		WHERE id = $1
	`, id, e.Description, e.AmountCents, e.CategoryID, e.PayerID, e.IncurredAt); err != nil {
		return nil, err
	}

	if splitsChanged {
		if _, err := tx.Exec(ctx, `DELETE FROM splits WHERE expense_id = $1`, id); err != nil {
			return nil, err
		}
		for _, s := range splitsToWrite {
			if _, err := tx.Exec(ctx, `
				INSERT INTO splits (expense_id, user_id, share_cents) VALUES ($1, $2, $3)
			`, id, s.UserID, s.ShareCents); err != nil {
				return nil, err
			}
		}
	} else {
		for _, s := range splitsToWrite {
			if _, err := tx.Exec(ctx, `
				UPDATE splits SET share_cents = $3 WHERE expense_id = $1 AND user_id = $2
			`, id, s.UserID, s.ShareCents); err != nil {
				return nil, err
			}
		}
	}

	for _, rv := range revisions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO expense_revisions (expense_id, edited_by, field, old_value, new_value)
			VALUES ($1, $2, $3, $4, $5)
		`, id, editorID, rv.field, rv.oldV, rv.newV); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Reload splits for the returned expense.
	srows, err := r.pool.Query(ctx, `SELECT expense_id, user_id, share_cents FROM splits WHERE expense_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var s Split
		if err := srows.Scan(&s.ExpenseID, &s.UserID, &s.ShareCents); err != nil {
			return nil, err
		}
		e.Splits = append(e.Splits, s)
	}
	return &e, srows.Err()
}

func fetchSplitsForUpdate(ctx context.Context, tx pgx.Tx, expenseID uuid.UUID) ([]Split, error) {
	rows, err := tx.Query(ctx, `SELECT expense_id, user_id, share_cents FROM splits WHERE expense_id = $1 ORDER BY user_id`, expenseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Split
	for rows.Next() {
		var s Split
		if err := rows.Scan(&s.ExpenseID, &s.UserID, &s.ShareCents); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// marshalSplitsForRevision emits a compact JSON array of {user_id, share_cents},
// sorted by user_id, for stable before/after diffs in expense_revisions.
func marshalSplitsForRevision(splits []Split) (string, error) {
	type row struct {
		UserID     string `json:"user_id"`
		ShareCents int64  `json:"share_cents"`
	}
	rows := make([]row, len(splits))
	for i, s := range splits {
		rows[i] = row{UserID: s.UserID.String(), ShareCents: s.ShareCents}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UserID < rows[j].UserID })
	b, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func splitsEqual(a, b []Split) bool {
	if len(a) != len(b) {
		return false
	}
	index := make(map[uuid.UUID]int64, len(a))
	for _, s := range a {
		index[s.UserID] = s.ShareCents
	}
	for _, s := range b {
		if v, ok := index[s.UserID]; !ok || v != s.ShareCents {
			return false
		}
	}
	return true
}

// ListRevisions returns the full edit history for an expense (oldest first).
func (r *ExpenseRepo) ListRevisions(ctx context.Context, expenseID uuid.UUID) ([]ExpenseRevision, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, expense_id, edited_by, edited_at, field, old_value, new_value
		FROM expense_revisions WHERE expense_id = $1 ORDER BY edited_at ASC
	`, expenseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpenseRevision
	for rows.Next() {
		var rv ExpenseRevision
		if err := rows.Scan(&rv.ID, &rv.ExpenseID, &rv.EditedBy, &rv.EditedAt,
			&rv.Field, &rv.OldValue, &rv.NewValue); err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

type ExpenseRevision struct {
	ID        uuid.UUID
	ExpenseID uuid.UUID
	EditedBy  uuid.UUID
	EditedAt  time.Time
	Field     string
	OldValue  string
	NewValue  string
}

// rescaleSplits turns existing share_cents into new shares proportional to
// the new total. Rounding leftovers go to the first splits in user_id order
// (matching the order enforced at read time).
func rescaleSplits(existing []Split, oldTotal, newTotal int64) []Split {
	out := make([]Split, len(existing))
	copy(out, existing)
	if oldTotal == 0 || len(out) == 0 {
		return out
	}
	var assigned int64
	for i := range out {
		share := out[i].ShareCents * newTotal / oldTotal
		out[i].ShareCents = share
		assigned += share
	}
	for i := int64(0); i < newTotal-assigned; i++ {
		out[int(i)%len(out)].ShareCents++
	}
	return out
}

