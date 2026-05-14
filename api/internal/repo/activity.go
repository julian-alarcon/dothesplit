package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActivityKind string

const (
	ActivityExpense    ActivityKind = "expense"
	ActivitySettlement ActivityKind = "settlement"
)

// ActivityRow is a single (kind, occurred_at, created_at, id) tuple - just
// enough to drive keyset pagination. The service hydrates the full payloads
// in batch. created_at is the tiebreaker when two rows share occurred_at
// (e.g. multiple expenses dated the same day with no time component).
type ActivityRow struct {
	Kind       ActivityKind
	OccurredAt time.Time
	CreatedAt  time.Time
	ID         uuid.UUID
}

type ActivityRepo struct {
	pool *pgxpool.Pool
}

func NewActivityRepo(p *pgxpool.Pool) *ActivityRepo { return &ActivityRepo{pool: p} }

// ListByGroup returns up to `limit` (occurred_at DESC, created_at DESC, id DESC)
// rows from the merged expenses + settlements feed for the group, optionally
// continuing strictly after the given cursor row. Soft-deleted rows are
// excluded. Caller passes `limit + 1` if it wants to detect a next page.
func (r *ActivityRepo) ListByGroup(ctx context.Context, groupID uuid.UUID, limit int, after *ActivityRow) ([]ActivityRow, error) {
	args := []any{groupID}
	cursorPredicate := ""
	if after != nil {
		// Lexicographic comparison on the (occurred_at, created_at, id) tuple,
		// strictly less than the cursor (we already returned that row).
		args = append(args, after.OccurredAt, after.CreatedAt, after.ID)
		cursorPredicate = "AND (occurred_at, created_at, id) < ($2, $3, $4)"
	}
	limitArg := len(args) + 1
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT kind, occurred_at, created_at, id FROM (
			SELECT 'expense'::text AS kind, incurred_at AS occurred_at, created_at, id
			FROM expenses
			WHERE group_id = $1 AND deleted_at IS NULL
			UNION ALL
			SELECT 'settlement'::text AS kind, settled_at AS occurred_at, created_at, id
			FROM settlements
			WHERE group_id = $1 AND deleted_at IS NULL
		) feed
		WHERE TRUE %s
		ORDER BY occurred_at DESC, created_at DESC, id DESC
		LIMIT $%d
	`, cursorPredicate, limitArg)
	query = strings.TrimSpace(query)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityRow
	for rows.Next() {
		var row ActivityRow
		var kind string
		if err := rows.Scan(&kind, &row.OccurredAt, &row.CreatedAt, &row.ID); err != nil {
			return nil, err
		}
		row.Kind = ActivityKind(kind)
		out = append(out, row)
	}
	return out, rows.Err()
}
