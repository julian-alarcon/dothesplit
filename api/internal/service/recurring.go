package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

type RecurringService struct {
	recurring  *repo.RecurringRepo
	expenses   *repo.ExpenseRepo
	groups     *repo.GroupRepo
	categories *CategoryService
}

func NewRecurringService(r *repo.RecurringRepo, e *repo.ExpenseRepo, g *repo.GroupRepo, c *CategoryService) *RecurringService {
	return &RecurringService{recurring: r, expenses: e, groups: g, categories: c}
}

type CreateRecurringInput struct {
	GroupID     uuid.UUID
	PayerID     uuid.UUID
	CategoryID  *uuid.UUID
	AmountCents int64
	Currency    string
	Description string
	Mode        SplitMode
	Splits      []SplitInput
	Cadence     string
	NextRunAt   time.Time
}

func (s *RecurringService) Create(ctx context.Context, actorID uuid.UUID, in CreateRecurringInput) (*repo.RecurringExpense, error) {
	if ok, err := s.groups.IsMember(ctx, in.GroupID, actorID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotMember
	}
	if !isValidCadence(in.Cadence) {
		return nil, errors.New("invalid cadence")
	}
	if in.AmountCents <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	// Validate the template by trying to resolve it now against the current members.
	if _, err := resolveSplits(in.Mode, in.AmountCents, in.Splits); err != nil {
		return nil, err
	}
	if in.Currency == "" {
		g, err := s.groups.FindByID(ctx, in.GroupID)
		if err != nil {
			return nil, err
		}
		in.Currency = g.DefaultCurrency
	}
	cat, err := s.categories.Resolve(ctx, in.CategoryID)
	if err != nil {
		return nil, err
	}
	tmpl := make([]repo.SplitTemplateEntry, len(in.Splits))
	for i, sp := range in.Splits {
		tmpl[i] = repo.SplitTemplateEntry{UserID: sp.UserID, Value: sp.Value}
	}
	e := &repo.RecurringExpense{
		GroupID: in.GroupID, PayerID: in.PayerID,
		CategoryID:  cat.ID,
		AmountCents: in.AmountCents, Currency: in.Currency,
		Description: in.Description, Mode: string(in.Mode),
		SplitTemplate: tmpl,
		Cadence:       in.Cadence, NextRunAt: in.NextRunAt,
	}
	if err := s.recurring.Create(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *RecurringService) List(ctx context.Context, actorID, groupID uuid.UUID) ([]repo.RecurringExpense, error) {
	if ok, err := s.groups.IsMember(ctx, groupID, actorID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotMember
	}
	return s.recurring.ListByGroup(ctx, groupID)
}

// Delete is allowed for any group member (v1 simplification).
func (s *RecurringService) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	e, err := s.recurring.FindByID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.ErrNotFound
	}
	if err != nil {
		return err
	}
	if ok, err := s.groups.IsMember(ctx, e.GroupID, actorID); err != nil {
		return err
	} else if !ok {
		return ErrForbidden
	}
	return s.recurring.SoftDelete(ctx, id)
}

// Tick materializes every due recurring expense into a regular expense row and
// advances next_run_at by the cadence. Returns the number of expenses created.
func (s *RecurringService) Tick(ctx context.Context) (int, error) {
	tx, due, err := s.recurring.ClaimDue(ctx, 100)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit below replaces this

	created := 0
	for _, r := range due {
		splits := make([]SplitInput, len(r.SplitTemplate))
		for i, t := range r.SplitTemplate {
			splits[i] = SplitInput{UserID: t.UserID, Value: t.Value}
		}
		shares, err := resolveSplits(SplitMode(r.Mode), r.AmountCents, splits)
		if err != nil {
			return 0, fmt.Errorf("resolve splits for recurring %s: %w", r.ID, err)
		}
		// The recurring template doesn't track who set it up, so attribute
		// materialized expenses to the payer. Good enough — it's the same
		// user the template "belongs to" — and avoids leaving a NULL behind.
		e := &repo.Expense{
			GroupID:     r.GroupID,
			PayerID:     r.PayerID,
			CreatedBy:   r.PayerID,
			CategoryID:  r.CategoryID,
			AmountCents: r.AmountCents,
			Currency:    r.Currency,
			Description: r.Description,
			IncurredAt:  r.NextRunAt,
			Splits:      shares,
		}
		// Inserts use their own short-lived transaction; the outer tx only holds
		// the FOR UPDATE lock on the recurring rows while we schedule them.
		if err := s.expenses.CreateWithSplits(ctx, e); err != nil {
			return 0, err
		}
		next := advanceCadence(r.NextRunAt, r.Cadence)
		if err := s.recurring.UpdateNextRunTx(ctx, tx, r.ID, next); err != nil {
			return 0, err
		}
		created++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func isValidCadence(c string) bool {
	switch c {
	case "daily", "weekly", "monthly":
		return true
	}
	return false
}

func advanceCadence(from time.Time, cadence string) time.Time {
	switch cadence {
	case "daily":
		return from.AddDate(0, 0, 1)
	case "weekly":
		return from.AddDate(0, 0, 7)
	case "monthly":
		return from.AddDate(0, 1, 0)
	}
	return from
}
