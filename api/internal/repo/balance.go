package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NetBalance struct {
	UserID      uuid.UUID
	DisplayName string
	NetCents    int64
}

type BalanceRepo struct {
	pool *pgxpool.Pool
}

func NewBalanceRepo(p *pgxpool.Pool) *BalanceRepo { return &BalanceRepo{pool: p} }

// NetBalances computes per-user net cents for a group in a single query.
// Positive means the user is owed; negative means they owe.
func (r *BalanceRepo) NetBalances(ctx context.Context, groupID uuid.UUID) ([]NetBalance, error) {
	rows, err := r.pool.Query(ctx, `
		WITH paid AS (
			SELECT payer_id AS user_id, SUM(amount_cents) AS paid_cents
			FROM expenses
			WHERE group_id = $1 AND deleted_at IS NULL
			GROUP BY payer_id
		),
		owed AS (
			SELECT s.user_id, SUM(s.share_cents) AS owed_cents
			FROM splits s JOIN expenses e ON e.id = s.expense_id
			WHERE e.group_id = $1 AND e.deleted_at IS NULL
			GROUP BY s.user_id
		),
		settled_out AS (
			SELECT from_user AS user_id, SUM(amount_cents) AS paid_out
			FROM settlements
			WHERE group_id = $1 AND deleted_at IS NULL
			GROUP BY from_user
		),
		settled_in AS (
			SELECT to_user AS user_id, SUM(amount_cents) AS received
			FROM settlements
			WHERE group_id = $1 AND deleted_at IS NULL
			GROUP BY to_user
		)
		SELECT m.user_id, u.display_name,
			COALESCE(p.paid_cents, 0) - COALESCE(o.owed_cents, 0)
			+ COALESCE(so.paid_out, 0) - COALESCE(si.received, 0) AS net_cents
		FROM group_members m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN paid p         ON p.user_id  = m.user_id
		LEFT JOIN owed o         ON o.user_id  = m.user_id
		LEFT JOIN settled_out so ON so.user_id = m.user_id
		LEFT JOIN settled_in si  ON si.user_id = m.user_id
		WHERE m.group_id = $1
		ORDER BY net_cents DESC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NetBalance
	for rows.Next() {
		var b NetBalance
		if err := rows.Scan(&b.UserID, &b.DisplayName, &b.NetCents); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// NetForUser returns one user's net cents for a group, computed from their
// historical paid/owed/settled rows — independent of current membership.
// Used to gate member removal: a non-zero net would silently disappear from
// the ledger if we let the user leave.
func (r *BalanceRepo) NetForUser(ctx context.Context, groupID, userID uuid.UUID) (int64, error) {
	var net int64
	err := r.pool.QueryRow(ctx, `
		SELECT
			  COALESCE((SELECT SUM(amount_cents) FROM expenses
			            WHERE group_id = $1 AND payer_id = $2 AND deleted_at IS NULL), 0)
			- COALESCE((SELECT SUM(s.share_cents) FROM splits s JOIN expenses e ON e.id = s.expense_id
			            WHERE e.group_id = $1 AND s.user_id = $2 AND e.deleted_at IS NULL), 0)
			+ COALESCE((SELECT SUM(amount_cents) FROM settlements
			            WHERE group_id = $1 AND from_user = $2 AND deleted_at IS NULL), 0)
			- COALESCE((SELECT SUM(amount_cents) FROM settlements
			            WHERE group_id = $1 AND to_user = $2 AND deleted_at IS NULL), 0)
	`, groupID, userID).Scan(&net)
	return net, err
}
