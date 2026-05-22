package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

var (
	ErrNotMember           = errors.New("user is not a group member")
	ErrInviteeNotFound     = errors.New("invitee is not registered")
	ErrNotCreator          = errors.New("only the group creator can perform this action")
	ErrBadCurrency         = errors.New("default_currency must be a 3-letter code")
	ErrCurrencyLocked      = errors.New("default_currency is locked once the group has expenses or settlements")
	ErrBadDefaultSplit     = errors.New("invalid default_split")
	ErrCannotRemoveCreator = errors.New("the group creator cannot leave or be removed; transfer ownership or delete the group")
	ErrBalanceNotZero      = errors.New("settle up first: removing a member with a non-zero balance would silently drop their share of the ledger")
	ErrNewOwnerNotMember   = errors.New("new owner must be an existing group member")
)

// DefaultGroupCurrency is used when a group is created without an explicit currency.
const DefaultGroupCurrency = "EUR"

type GroupService struct {
	groups        *repo.GroupRepo
	users         *repo.UserRepo
	balances      *repo.BalanceRepo
	email         *crypto.EmailCipher
	notifications *NotificationService
}

func NewGroupService(g *repo.GroupRepo, u *repo.UserRepo, b *repo.BalanceRepo, e *crypto.EmailCipher) *GroupService {
	return &GroupService{groups: g, users: u, balances: b, email: e}
}

// SetNotifications wires in the notification service. Optional - if unset,
// AddMember silently skips the notification step (used by tests that don't
// need to exercise the mailer).
func (s *GroupService) SetNotifications(n *NotificationService) { s.notifications = n }

// Create a group. The creator is auto-added as a member. Empty currency → DefaultGroupCurrency.
func (s *GroupService) Create(ctx context.Context, name, defaultCurrency string, creatorID uuid.UUID) (*repo.Group, []repo.GroupMember, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, errors.New("name is required")
	}
	cur, err := normalizeCurrency(defaultCurrency)
	if err != nil {
		return nil, nil, err
	}
	if cur == "" {
		cur = DefaultGroupCurrency
	}
	g, err := s.groups.Create(ctx, name, cur, creatorID)
	if err != nil {
		return nil, nil, err
	}
	members, err := s.groups.ListMembers(ctx, g.ID)
	if err != nil {
		return nil, nil, err
	}
	return g, members, nil
}

// UpdateGroupInput captures partial-update fields. nil pointer = leave unchanged.
// For DefaultSplit: pointer to nil/empty slice clears it; pointer to a 2-entry
// slice replaces it. CreatedBy transfers ownership; only the current owner
// may set it, and the target must already be a group member.
type UpdateGroupInput struct {
	Name            *string
	DefaultCurrency *string
	DefaultSplit    *[]repo.DefaultSplitEntry
	CreatedBy       *uuid.UUID
}

// Update applies a partial update. Any group member may update name /
// default_currency / default_split; only the current creator may transfer
// ownership via CreatedBy.
func (s *GroupService) Update(ctx context.Context, groupID, actorID uuid.UUID, in UpdateGroupInput) (*repo.Group, []repo.GroupMember, error) {
	if err := s.RequireMember(ctx, groupID, actorID); err != nil {
		return nil, nil, err
	}
	if in.Name == nil && in.DefaultCurrency == nil && in.DefaultSplit == nil && in.CreatedBy == nil {
		return nil, nil, errors.New("nothing to update")
	}
	if in.Name != nil {
		trimmed := strings.TrimSpace(*in.Name)
		if trimmed == "" {
			return nil, nil, errors.New("name cannot be empty")
		}
		in.Name = &trimmed
	}
	if in.DefaultCurrency != nil {
		cur, err := normalizeCurrency(*in.DefaultCurrency)
		if err != nil {
			return nil, nil, err
		}
		if cur == "" {
			return nil, nil, ErrBadCurrency
		}
		in.DefaultCurrency = &cur
		current, err := s.groups.FindByID(ctx, groupID)
		if err != nil {
			return nil, nil, err
		}
		if cur != current.DefaultCurrency {
			hasActivity, err := s.groups.HasActivity(ctx, groupID)
			if err != nil {
				return nil, nil, err
			}
			if hasActivity {
				return nil, nil, ErrCurrencyLocked
			}
		}
	}
	if in.DefaultSplit != nil {
		split := *in.DefaultSplit
		if len(split) > 0 {
			members, err := s.groups.ListMembers(ctx, groupID)
			if err != nil {
				return nil, nil, err
			}
			if len(members) != 2 {
				return nil, nil, fmt.Errorf("%w: only valid for 2-member groups", ErrBadDefaultSplit)
			}
			if err := validateDefaultSplit(split, members); err != nil {
				return nil, nil, err
			}
		}
	}
	if in.CreatedBy != nil {
		current, err := s.groups.FindByID(ctx, groupID)
		if err != nil {
			return nil, nil, err
		}
		if actorID != current.CreatedBy {
			return nil, nil, ErrNotCreator
		}
		ok, err := s.groups.IsMember(ctx, groupID, *in.CreatedBy)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, ErrNewOwnerNotMember
		}
	}
	g, err := s.groups.Update(ctx, groupID, repo.UpdateInput{
		Name:            in.Name,
		DefaultCurrency: in.DefaultCurrency,
		DefaultSplit:    in.DefaultSplit,
		CreatedBy:       in.CreatedBy,
	})
	if err != nil {
		return nil, nil, err
	}
	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	return g, members, nil
}

func validateDefaultSplit(split []repo.DefaultSplitEntry, members []repo.GroupMember) error {
	if len(split) != 2 {
		return fmt.Errorf("%w: must contain exactly 2 entries", ErrBadDefaultSplit)
	}
	memberIDs := map[uuid.UUID]bool{}
	for _, m := range members {
		memberIDs[m.UserID] = true
	}
	seen := map[uuid.UUID]bool{}
	var sum int64
	for _, e := range split {
		if !memberIDs[e.UserID] {
			return fmt.Errorf("%w: user is not a group member", ErrBadDefaultSplit)
		}
		if seen[e.UserID] {
			return fmt.Errorf("%w: duplicate user", ErrBadDefaultSplit)
		}
		if e.BasisPoints < 0 || e.BasisPoints > 10000 {
			return fmt.Errorf("%w: basis_points must be 0..10000", ErrBadDefaultSplit)
		}
		seen[e.UserID] = true
		sum += e.BasisPoints
	}
	if sum != 10000 {
		return fmt.Errorf("%w: basis_points must sum to 10000", ErrBadDefaultSplit)
	}
	return nil
}

// Delete removes the group. Only the creator may delete it. Cascades via FK.
func (s *GroupService) Delete(ctx context.Context, groupID, actorID uuid.UUID) error {
	g, err := s.groups.FindByID(ctx, groupID)
	if err != nil {
		return err
	}
	if g.CreatedBy != actorID {
		return ErrNotCreator
	}
	return s.groups.Delete(ctx, groupID)
}

// normalizeCurrency uppercases a 3-letter code. Empty input returns "".
func normalizeCurrency(cur string) (string, error) {
	cur = strings.TrimSpace(cur)
	if cur == "" {
		return "", nil
	}
	if len(cur) != 3 {
		return "", ErrBadCurrency
	}
	return strings.ToUpper(cur), nil
}

func (s *GroupService) List(ctx context.Context, userID uuid.UUID) ([]repo.Group, map[uuid.UUID][]repo.GroupMember, error) {
	groups, err := s.groups.ListForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	membersByGroup := make(map[uuid.UUID][]repo.GroupMember, len(groups))
	for _, g := range groups {
		m, err := s.groups.ListMembers(ctx, g.ID)
		if err != nil {
			return nil, nil, err
		}
		membersByGroup[g.ID] = m
	}
	return groups, membersByGroup, nil
}

// RequireMember returns ErrNotMember if userID isn't in groupID.
func (s *GroupService) RequireMember(ctx context.Context, groupID, userID uuid.UUID) error {
	ok, err := s.groups.IsMember(ctx, groupID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	return nil
}

// ShareAnyGroup reports whether two users are in at least one group together.
func (s *GroupService) ShareAnyGroup(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return s.groups.ShareAnyGroup(ctx, a, b)
}

// AddMember looks up the invitee by email_hash; 404 if unregistered.
// Only an existing group member may add others.
// If the group had a pinned default_split (only valid for 2 members), it is
// silently cleared once the membership grows past 2.
func (s *GroupService) AddMember(ctx context.Context, groupID, actorID uuid.UUID, email string) (*repo.GroupMember, error) {
	if err := s.RequireMember(ctx, groupID, actorID); err != nil {
		return nil, err
	}
	invitee, err := s.users.FindByEmailHash(ctx, s.email.HashEmail(email))
	if errors.Is(err, repo.ErrNotFound) {
		return nil, ErrInviteeNotFound
	}
	if err != nil {
		return nil, err
	}
	m, err := s.groups.AddMember(ctx, groupID, invitee.ID)
	if err != nil {
		return nil, err
	}
	if s.notifications != nil && invitee.ID != actorID {
		group, gerr := s.groups.FindByID(ctx, groupID)
		if gerr == nil {
			actor, aerr := s.users.FindByID(ctx, actorID)
			actorName := ""
			if aerr == nil {
				actorName = actor.DisplayName
			}
			_ = s.notifications.NotifyIfEnabled(ctx, nil, invitee.ID,
				PrefKeyGroupAdded, "group_member_added", TemplateVars{
					DisplayName: invitee.DisplayName,
					ActorName:   actorName,
					GroupName:   group.Name,
				})
		}
	}
	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(members) > 2 {
		if err := s.groups.ClearDefaultSplit(ctx, groupID); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// RemoveMember removes targetID from groupID. Authz: actor must be the group
// creator (to remove anyone else) OR the actor must equal targetID (leaving).
// The creator can't leave or be removed - transfer ownership or delete the
// group instead.
//
// Balance gate: a creator removing somebody *else* is blocked when the target
// has a non-zero balance, since silently writing off a third party's debt is
// a high-blast-radius action they didn't consent to. A member leaving on their
// own accord is allowed regardless - the UI surfaces a warning so they
// understand the consequence. Their splits and settlements stay in the
// ledger; they just stop showing up in the balance JOIN.
//
// If membership ends up below 2, any pinned default_split is cleared.
func (s *GroupService) RemoveMember(ctx context.Context, groupID, actorID, targetID uuid.UUID) error {
	g, err := s.groups.FindByID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := s.RequireMember(ctx, groupID, actorID); err != nil {
		return err
	}
	ok, err := s.groups.IsMember(ctx, groupID, targetID)
	if err != nil {
		return err
	}
	if !ok {
		return repo.ErrNotFound
	}
	// Authz: creator can remove anyone; non-creator can only remove themselves.
	if actorID != g.CreatedBy && actorID != targetID {
		return ErrNotCreator
	}
	if targetID == g.CreatedBy {
		return ErrCannotRemoveCreator
	}
	if actorID != targetID {
		net, err := s.balances.NetForUser(ctx, groupID, targetID)
		if err != nil {
			return err
		}
		if net != 0 {
			return ErrBalanceNotZero
		}
	}
	if err := s.groups.RemoveMember(ctx, groupID, targetID); err != nil {
		return err
	}
	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return err
	}
	if len(members) < 2 {
		if err := s.groups.ClearDefaultSplit(ctx, groupID); err != nil {
			return err
		}
	}
	return nil
}

// Get returns a group + its members, enforcing membership.
func (s *GroupService) Get(ctx context.Context, groupID, userID uuid.UUID) (*repo.Group, []repo.GroupMember, error) {
	if err := s.RequireMember(ctx, groupID, userID); err != nil {
		return nil, nil, err
	}
	g, err := s.groups.FindByID(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	return g, members, nil
}
