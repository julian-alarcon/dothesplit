package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

type SettlementService struct {
	settlements   *repo.SettlementRepo
	groups        *repo.GroupRepo
	users         *repo.UserRepo
	notifications *NotificationService
}

func NewSettlementService(s *repo.SettlementRepo, g *repo.GroupRepo) *SettlementService {
	return &SettlementService{settlements: s, groups: g}
}

// SetNotifications wires in user lookup + notification dispatch. Optional;
// when unset the service silently skips notifications (used by tests that
// don't need to exercise the mailer).
func (s *SettlementService) SetNotifications(users *repo.UserRepo, n *NotificationService) {
	s.users = users
	s.notifications = n
}

type CreateSettlementInput struct {
	GroupID     uuid.UUID
	FromUserID  uuid.UUID
	ToUserID    uuid.UUID
	AmountCents int64
	Note        string
	SettledAt   time.Time
}

func (s *SettlementService) Create(ctx context.Context, actorID uuid.UUID, in CreateSettlementInput) (*repo.Settlement, error) {
	if in.AmountCents <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	if in.FromUserID == in.ToUserID {
		return nil, errors.New("from and to must differ")
	}
	// Actor must be the payer (from_user).
	if actorID != in.FromUserID {
		return nil, ErrForbidden
	}
	// Both parties must be group members.
	for _, uid := range []uuid.UUID{in.FromUserID, in.ToUserID} {
		ok, err := s.groups.IsMember(ctx, in.GroupID, uid)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotMember
		}
	}
	if in.SettledAt.IsZero() {
		in.SettledAt = time.Now().UTC()
	}
	st := &repo.Settlement{
		GroupID:     in.GroupID,
		FromUser:    in.FromUserID,
		ToUser:      in.ToUserID,
		AmountCents: in.AmountCents,
		Note:        in.Note,
		SettledAt:   in.SettledAt,
	}
	if err := s.settlements.Create(ctx, st); err != nil {
		return nil, err
	}

	if s.notifications != nil && s.users != nil {
		group, gerr := s.groups.FindByID(ctx, in.GroupID)
		groupName := ""
		groupCurrency := "USD"
		if gerr == nil {
			groupName = group.Name
			groupCurrency = group.DefaultCurrency
		}
		fromUser, _ := s.users.FindByID(ctx, in.FromUserID)
		toUser, _ := s.users.FindByID(ctx, in.ToUserID)
		fromName, toName := "Someone", "another member"
		if fromUser != nil {
			fromName = fromUser.DisplayName
		}
		if toUser != nil {
			toName = toUser.DisplayName
		}
		amount := formatMoney(in.AmountCents, groupCurrency)
		members, _ := s.groups.ListMembers(ctx, in.GroupID)
		// Notify everyone in the group who's opted in (best-effort; failures
		// don't roll back the settlement).
		for _, m := range members {
			actorName := fromName + " paid " + toName
			desc := fromName + " -> " + toName
			_ = s.notifications.NotifyIfEnabled(ctx, nil, m.UserID,
				PrefKeySettlement, "settlement_created", TemplateVars{
					ActorName:   actorName,
					GroupName:   groupName,
					Description: desc,
					Amount:      amount,
				})
		}
	}
	return st, nil
}

// formatMoney renders an integer-cent value as e.g. "12.34 EUR". Plain text;
// no narrowSymbol, no Intl - Go has no equivalent and the email is plain text
// anyway.
func formatMoney(cents int64, currency string) string {
	whole := cents / 100
	frac := cents % 100
	if frac < 0 {
		frac = -frac
	}
	return fmtSign(whole, frac) + " " + currency
}

func fmtSign(whole, frac int64) string {
	return fmtInt(whole) + "." + fmtTwo(frac)
}

func fmtInt(n int64) string { return fmt.Sprintf("%d", n) }
func fmtTwo(n int64) string { return fmt.Sprintf("%02d", n) }

// Get returns a single settlement by id, enforcing group membership.
func (s *SettlementService) Get(ctx context.Context, actorID, id uuid.UUID) (*repo.Settlement, error) {
	st, err := s.settlements.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st.DeletedAt != nil {
		return nil, repo.ErrNotFound
	}
	ok, err := s.groups.IsMember(ctx, st.GroupID, actorID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}
	return st, nil
}

// Delete soft-deletes a settlement. Any group member may delete; the row is
// preserved with deleted_at so the audit trail survives.
func (s *SettlementService) Delete(ctx context.Context, actorID, settlementID uuid.UUID) error {
	st, err := s.settlements.FindByID(ctx, settlementID)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.ErrNotFound
	}
	if err != nil {
		return err
	}
	if st.DeletedAt != nil {
		return repo.ErrNotFound
	}
	ok, err := s.groups.IsMember(ctx, st.GroupID, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	return s.settlements.SoftDelete(ctx, settlementID)
}

func (s *SettlementService) List(ctx context.Context, actorID, groupID uuid.UUID) ([]repo.Settlement, error) {
	ok, err := s.groups.IsMember(ctx, groupID, actorID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}
	return s.settlements.ListByGroup(ctx, groupID)
}
