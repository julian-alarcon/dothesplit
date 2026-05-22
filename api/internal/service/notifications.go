// NotificationService manages per-user email opt-ins and is the single
// entry point services use to enqueue activity notifications. Centralising
// the pref check + outbox enqueue keeps call sites tiny.
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

// NotificationPrefs is the typed projection of users.notification_prefs.
// All fields default to false ("off"); absent JSONB keys mean off too.
type NotificationPrefs struct {
	NotifyRecurringRun bool `json:"notify_recurring_run,omitempty"`
	NotifySettlement   bool `json:"notify_settlement,omitempty"`
	NotifyGroupAdded   bool `json:"notify_group_added,omitempty"`
}

// Pref keys recognised by the service. Anything outside this set is rejected
// by Update so a stray key in the request body can't silently land in the DB.
const (
	PrefKeyRecurringRun = "notify_recurring_run"
	PrefKeySettlement   = "notify_settlement"
	PrefKeyGroupAdded   = "notify_group_added"
)

type NotificationService struct {
	users  *repo.UserRepo
	mailer *MailerService
	email  *crypto.EmailCipher
}

func NewNotificationService(users *repo.UserRepo, mailer *MailerService, email *crypto.EmailCipher) *NotificationService {
	return &NotificationService{users: users, mailer: mailer, email: email}
}

// GetPrefs returns the user's preferences, parsing the raw JSONB.
func (n *NotificationService) GetPrefs(ctx context.Context, userID uuid.UUID) (*NotificationPrefs, error) {
	u, err := n.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return parsePrefs(u.NotificationPrefs), nil
}

// UpdatePrefs writes a new preferences blob. Unknown keys cause an error.
func (n *NotificationService) UpdatePrefs(ctx context.Context, userID uuid.UUID, p *NotificationPrefs) (*NotificationPrefs, error) {
	if p == nil {
		p = &NotificationPrefs{}
	}
	blob, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if err := n.users.UpdateNotificationPrefs(ctx, userID, blob); err != nil {
		return nil, err
	}
	return p, nil
}

// NotifyIfEnabled looks up the user's pref for the given key and, if true,
// renders the template and enqueues an outbox row. Silently no-ops when the
// pref is off, the user is soft-deleted, or the email can't be decrypted -
// notifications are best-effort and must never fail the underlying action.
func (n *NotificationService) NotifyIfEnabled(ctx context.Context, q repo.Querier, userID uuid.UUID, prefKey, template string, vars TemplateVars) error {
	u, err := n.users.FindByID(ctx, userID)
	if err != nil {
		return nil // user disappeared between action and notify; drop silently
	}
	if u.DeletedAt != nil {
		return nil
	}
	if u.EmailVerifiedAt == nil {
		return nil // never email someone whose address we haven't confirmed
	}
	prefs := parsePrefs(u.NotificationPrefs)
	if !prefEnabled(prefs, prefKey) {
		return nil
	}
	to, err := n.email.Decrypt(u.EmailEncrypted)
	if err != nil {
		return nil
	}
	if vars.DisplayName == "" {
		vars.DisplayName = u.DisplayName
	}
	return n.mailer.Enqueue(ctx, q, to, template, vars)
}

func parsePrefs(blob []byte) *NotificationPrefs {
	p := &NotificationPrefs{}
	if len(blob) == 0 {
		return p
	}
	_ = json.Unmarshal(blob, p)
	return p
}

func prefEnabled(p *NotificationPrefs, key string) bool {
	switch key {
	case PrefKeyRecurringRun:
		return p.NotifyRecurringRun
	case PrefKeySettlement:
		return p.NotifySettlement
	case PrefKeyGroupAdded:
		return p.NotifyGroupAdded
	}
	return false
}

// ValidatePrefKeys ensures the JSON body only contains keys we know. Helpers
// for handlers that bind a permissive map. Returns the first unknown key, if
// any, for a clear error message.
func ValidatePrefKeys(raw map[string]any) error {
	for k := range raw {
		switch k {
		case PrefKeyRecurringRun, PrefKeySettlement, PrefKeyGroupAdded:
			// ok
		default:
			return fmt.Errorf("unknown preference key: %s", k)
		}
	}
	return nil
}
