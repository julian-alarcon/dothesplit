package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/julian-alarcon/dothesplit/api/internal/repo"

)

var ErrBadCursor = errors.New("invalid cursor")

const (
	activityDefaultLimit = 50
	activityMaxLimit     = 100
)

type ActivityService struct {
	groups      *GroupService
	activity    *repo.ActivityRepo
	expenses    *repo.ExpenseRepo
	settlements *repo.SettlementRepo
	recurring   *repo.RecurringRepo
}

func NewActivityService(g *GroupService, a *repo.ActivityRepo, e *repo.ExpenseRepo, s *repo.SettlementRepo, r *repo.RecurringRepo) *ActivityService {
	return &ActivityService{groups: g, activity: a, expenses: e, settlements: s, recurring: r}
}

// ActivityItem mirrors the OpenAPI schema: exactly one of Expense / Settlement
// is set, with the discriminator in Kind. Cadence is non-empty only when the
// item is an expense whose content matches a recurring template.
type ActivityItem struct {
	Kind       repo.ActivityKind
	OccurredAt time.Time
	Cadence    string // empty when not applicable
	Expense    *repo.Expense
	Settlement *repo.Settlement
}

type ActivityPage struct {
	Items      []ActivityItem
	NextCursor string // empty when there are no more items
}

// List returns one page of the merged activity feed for the group. It enforces
// membership, hydrates expense/settlement payloads in batched queries, and
// emits an opaque cursor that continues strictly after the last returned row.
func (s *ActivityService) List(ctx context.Context, actorID, groupID uuid.UUID, limit int, cursor string) (*ActivityPage, error) {
	if err := s.groups.RequireMember(ctx, groupID, actorID); err != nil {
		return nil, err
	}
	after, err := decodeActivityCursor(cursor)
	if err != nil {
		return nil, ErrBadCursor
	}
	if limit <= 0 {
		limit = activityDefaultLimit
	}
	if limit > activityMaxLimit {
		limit = activityMaxLimit
	}
	rows, err := s.activity.ListByGroup(ctx, groupID, limit+1, after)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	// Collect ids per kind for batch hydration.
	var expenseIDs, settlementIDs []uuid.UUID
	for _, r := range rows {
		switch r.Kind {
		case repo.ActivityExpense:
			expenseIDs = append(expenseIDs, r.ID)
		case repo.ActivitySettlement:
			settlementIDs = append(settlementIDs, r.ID)
		}
	}
	expenses, err := s.expenses.FindByIDs(ctx, expenseIDs)
	if err != nil {
		return nil, err
	}
	settlements, err := s.settlements.FindByIDs(ctx, settlementIDs)
	if err != nil {
		return nil, err
	}
	// Cadence match: same content-equality heuristic the dashboard used to do
	// client-side. Only worth running when the page contains any expenses.
	var cadenceByExpense map[uuid.UUID]string
	if len(expenseIDs) > 0 {
		templates, err := s.recurring.ListByGroup(ctx, groupID)
		if err != nil {
			return nil, err
		}
		cadenceByExpense = make(map[uuid.UUID]string, len(expenseIDs))
		for _, id := range expenseIDs {
			e, ok := expenses[id]
			if !ok {
				continue
			}
			for _, t := range templates {
				if t.Description == e.Description &&
					t.AmountCents == e.AmountCents &&
					t.PayerID == e.PayerID &&
					t.Currency == e.Currency &&
					t.CategoryID == e.CategoryID {
					cadenceByExpense[id] = t.Cadence
					break
				}
			}
		}
	}
	items := make([]ActivityItem, 0, len(rows))
	for _, row := range rows {
		item := ActivityItem{Kind: row.Kind, OccurredAt: row.OccurredAt}
		switch row.Kind {
		case repo.ActivityExpense:
			e, ok := expenses[row.ID]
			if !ok {
				// The row was soft-deleted between the index query and the
				// hydration query - skip it. Pagination still progresses.
				continue
			}
			item.Expense = &e
			if c, ok := cadenceByExpense[row.ID]; ok {
				item.Cadence = c
			}
		case repo.ActivitySettlement:
			st, ok := settlements[row.ID]
			if !ok {
				continue
			}
			item.Settlement = &st
		}
		items = append(items, item)
	}
	page := &ActivityPage{Items: items}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = encodeActivityCursor(last)
	}
	return page, nil
}

// Cursor format: base64-url(occurred_at_rfc3339nano | kind | uuid). Pipes are
// safe because RFC3339 doesn't use them and our kinds don't either.
func encodeActivityCursor(r repo.ActivityRow) string {
	raw := r.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" + string(r.Kind) + "|" + r.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeActivityCursor(s string) (*repo.ActivityRow, error) {
	if s == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(string(decoded), "|", 3)
	if len(parts) != 3 {
		return nil, errors.New("malformed cursor")
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, err
	}
	kind := repo.ActivityKind(parts[1])
	if kind != repo.ActivityExpense && kind != repo.ActivitySettlement {
		return nil, errors.New("malformed cursor kind")
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return nil, err
	}
	return &repo.ActivityRow{Kind: kind, OccurredAt: occurredAt, ID: id}, nil
}
