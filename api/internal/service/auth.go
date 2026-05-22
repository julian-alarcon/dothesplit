// Package service holds business logic that sits between HTTP handlers and repositories.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already registered")
	// ErrSetupRequired is returned by Register when the instance is still in
	// first-run setup mode. Handlers map it to 403 with code='setup_required'.
	ErrSetupRequired = errors.New("setup required")
	// ErrEmailUnverified is returned by Login when the account exists but the
	// user has not yet confirmed their email address. Handlers map it to
	// 403 with code='email_unverified' so the frontend can route to /verify.
	ErrEmailUnverified = errors.New("email not verified")
	// ErrInvalidCode and friends serve the verify/confirm flows.
	ErrInvalidCode       = errors.New("invalid code")
	ErrCodeExpired       = errors.New("code expired or already used")
	ErrVerifyRateLimited = errors.New("verification rate limited")
)

// SetupLocker is the minimal interface AuthService needs from the setup
// repo. Defined here (and not in the setup file) to keep the dep graph
// acyclic: AuthService → SetupLocker, and SetupService → AuthService.
type SetupLocker interface {
	Locked(ctx context.Context) (bool, error)
}

type AuthService struct {
	users        *repo.UserRepo
	sessions     *repo.SessionRepo
	audit        *repo.AuditRepo
	verification *repo.VerificationRepo
	mailer       *MailerService
	setupLock    SetupLocker
	pool         *pgxpool.Pool
	email        *crypto.EmailCipher
	pepper       []byte
	sessTTL      time.Duration

	// stepUpFails counts recent failed step-up password verifications keyed
	// by user ID, so handlers performing destructive admin actions can short-
	// circuit before they even hash a guess. Lazy-initialized.
	stepUpFails sync.Map // map[uuid.UUID]*stepUpCounter
}

type stepUpCounter struct {
	mu       sync.Mutex
	count    int
	windowAt time.Time
}

// stepUpWindow / stepUpMaxFails define the per-user lockout policy. Five bad
// guesses inside a minute lock the account out of step-up for the rest of the
// window. The counter resets on the next successful verify or after the
// window expires.
const (
	stepUpWindow   = time.Minute
	stepUpMaxFails = 5
)

// ErrStepUpRateLimited is returned when too many failed step-up verifications
// have piled up for one user inside the rate-limit window. Handlers map this
// to HTTP 423 Locked.
var ErrStepUpRateLimited = errors.New("step-up rate limited")

func NewAuthService(pool *pgxpool.Pool, users *repo.UserRepo, sessions *repo.SessionRepo, audit *repo.AuditRepo, verification *repo.VerificationRepo, mailer *MailerService, setupLock SetupLocker, email *crypto.EmailCipher, pepper []byte, sessTTL time.Duration) *AuthService {
	return &AuthService{
		users:        users,
		sessions:     sessions,
		audit:        audit,
		verification: verification,
		mailer:       mailer,
		setupLock:    setupLock,
		pool:         pool,
		email:        email,
		pepper:       pepper,
		sessTTL:      sessTTL,
	}
}

// User is a service-layer projection of a user with the decrypted email.
type User struct {
	ID              uuid.UUID
	Email           string
	DisplayName     string
	CreatedAt       time.Time
	HasAvatar       bool
	AvatarUpdatedAt *time.Time
	DeletedAt       *time.Time
	WeekStart       int16
	Timezone        *string
	IsAdmin         bool
	EmailVerifiedAt *time.Time
}

func (s *AuthService) toUser(u *repo.User) (*User, error) {
	email, err := s.email.Decrypt(u.EmailEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt email: %w", err)
	}
	return &User{
		ID:              u.ID,
		Email:           email,
		DisplayName:     u.DisplayName,
		CreatedAt:       u.CreatedAt,
		HasAvatar:       u.AvatarUpdatedAt != nil,
		AvatarUpdatedAt: u.AvatarUpdatedAt,
		DeletedAt:       u.DeletedAt,
		WeekStart:       u.WeekStart,
		Timezone:        u.Timezone,
		IsAdmin:         u.Role == "admin",
		EmailVerifiedAt: u.EmailVerifiedAt,
	}, nil
}

// RegisterResult is what /v1/auth/register returns to the handler. When the
// instance has SMTP configured the new account is unverified and no session
// is issued: SessionToken is "" and VerificationRequired is true. When SMTP
// is unconfigured the account is auto-verified and a session is issued, just
// like the historical behaviour (so the first bootstrap admin can register
// before SMTP exists).
type RegisterResult struct {
	User                 *User
	SessionToken         string
	VerificationRequired bool
}

// Register creates a user via /v1/auth/register. While first-run setup is
// pending it returns ErrSetupRequired so the only path that can mint the
// very first user is /v1/setup/admin (which calls RegisterTx directly inside
// its own atomic ceremony).
func (s *AuthService) Register(ctx context.Context, email, password, displayName string) (*RegisterResult, error) {
	if s.setupLock != nil {
		locked, err := s.setupLock.Locked(ctx)
		if err != nil {
			return nil, err
		}
		if !locked {
			return nil, ErrSetupRequired
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out, _, err := s.RegisterTx(ctx, tx, email, password, displayName)
	if err != nil {
		return nil, err
	}

	smtpReady := false
	if s.mailer != nil {
		ok, ierr := s.mailer.IsConfigured(ctx)
		if ierr == nil {
			smtpReady = ok
		}
	}

	if !smtpReady {
		// Auto-verify so the user can log in immediately. This happens on a
		// fresh install before SMTP is configured (and on every register
		// while SMTP stays unconfigured); recorded in audit so an admin can
		// see retroactively that the gate was open.
		if _, err := tx.Exec(ctx, `UPDATE users SET email_verified_at = now() WHERE id = $1`, out.ID); err != nil {
			return nil, err
		}
		meta, _ := json.Marshal(map[string]any{"reason": "smtp_unconfigured"})
		_ = s.audit.Insert(ctx, tx, &repo.AuditEntry{
			ActorUserID:  out.ID,
			TargetUserID: &out.ID,
			Action:       "auto_verified_no_smtp",
			Success:      true,
			Metadata:     meta,
		})
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		token, err := s.issueSession(ctx, out.ID)
		if err != nil {
			return nil, err
		}
		out.EmailVerifiedAt = ptrNow()
		return &RegisterResult{User: out, SessionToken: token, VerificationRequired: false}, nil
	}

	// SMTP is configured - issue a 6-digit code, enqueue the email, do NOT
	// open a session. The user must call /v1/auth/verify with the code.
	code, err := generateNumericCode(6)
	if err != nil {
		return nil, err
	}
	tok := &repo.VerificationToken{
		UserID:    out.ID,
		Purpose:   repo.PurposeRegister,
		CodeHash:  hashCode(code),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	if err := s.verification.Insert(ctx, tx, tok); err != nil {
		return nil, err
	}
	if err := s.mailer.Enqueue(ctx, tx, email, "verify_register", TemplateVars{
		DisplayName: displayName,
		Code:        code,
		NewEmail:    email,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &RegisterResult{User: out, SessionToken: "", VerificationRequired: true}, nil
}

func ptrNow() *time.Time { t := time.Now(); return &t }

// RegisterTx is the bootstrap-aware user-creation core, callable inside a
// caller-owned transaction. /v1/setup/admin uses this so the install
// ceremony can commit the user creation atomically with the
// app_setup.completed_at update.
//
// Bootstrap rules: the first non-deleted user becomes role='admin' and
// gets a 'bootstrap_admin' audit row. Concurrent first registrations are
// serialized on pg_advisory_xact_lock('admin_bootstrap'), so only one
// caller observes count==0. Returns the service-level User projection AND
// the underlying repo.User row (the latter is what SetupService needs to
// stamp `completed_by`).
func (s *AuthService) RegisterTx(ctx context.Context, tx pgx.Tx, email, password, displayName string) (*User, *repo.User, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || password == "" || displayName == "" {
		return nil, nil, errors.New("email, password, and display_name are required")
	}
	if len(password) < 10 {
		return nil, nil, errors.New("password must be at least 10 characters")
	}

	emailHash := s.email.HashEmail(email)
	if _, err := s.users.FindByEmailHash(ctx, emailHash); err == nil {
		return nil, nil, ErrEmailTaken
	} else if !errors.Is(err, repo.ErrNotFound) {
		return nil, nil, err
	}

	emailEnc, err := s.email.Encrypt(email)
	if err != nil {
		return nil, nil, err
	}
	pwdHash, err := crypto.HashPassword(password, s.pepper)
	if err != nil {
		return nil, nil, err
	}

	u := &repo.User{
		EmailHash:      emailHash,
		EmailEncrypted: emailEnc,
		DisplayName:    displayName,
		PasswordHash:   pwdHash,
	}

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('admin_bootstrap'))`); err != nil {
		return nil, nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return nil, nil, err
	}
	role := "user"
	if n == 0 {
		role = "admin"
	}
	u.Role = role
	if err := s.users.CreateWithRole(ctx, tx, u, role); err != nil {
		return nil, nil, err
	}
	if role == "admin" {
		meta, _ := json.Marshal(map[string]any{"reason": "first_user"})
		if err := s.audit.Insert(ctx, tx, &repo.AuditEntry{
			ActorUserID: u.ID,
			Action:      "bootstrap_admin",
			Success:     true,
			Metadata:    meta,
		}); err != nil {
			return nil, nil, err
		}
	}

	out, err := s.toUser(u)
	if err != nil {
		return nil, nil, err
	}
	return out, u, nil
}

// Login verifies credentials and issues a session. Returns (user, token).
// Authentication-failure errors intentionally share a single sentinel to avoid
// enumeration of which users exist.
func (s *AuthService) Login(ctx context.Context, email, password string) (*User, string, error) {
	emailHash := s.email.HashEmail(email)
	u, err := s.users.FindByEmailHash(ctx, emailHash)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	ok, err := crypto.VerifyPassword(u.PasswordHash, password, s.pepper)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return nil, "", ErrEmailUnverified
	}
	token, err := s.issueSession(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	out, err := s.toUser(u)
	if err != nil {
		return nil, "", err
	}
	return out, token, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteByTokenHash(ctx, hashToken(token))
}

// Resolve returns the user for a raw session token, or ErrInvalidCredentials.
func (s *AuthService) Resolve(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	sess, err := s.sessions.FindByTokenHash(ctx, hashToken(token))
	if errors.Is(err, repo.ErrNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	u, err := s.users.FindByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if u.DeletedAt != nil {
		return nil, ErrInvalidCredentials
	}
	return s.toUser(u)
}

// IssueSession creates a fresh session token for the given user. Exposed for
// handlers that need to refresh the cookie after wiping all sessions (e.g. on
// password change).
func (s *AuthService) IssueSession(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.issueSession(ctx, userID)
}

func (s *AuthService) issueSession(ctx context.Context, userID uuid.UUID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sess := &repo.Session{
		UserID:    userID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().Add(s.sessTTL),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return "", err
	}
	return token, nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// VerifyPassword re-checks a user's password for step-up authorization.
// Returns ErrStepUpRateLimited when too many recent failures pile up;
// ErrInvalidCredentials on a bad password; nil on success. Failures and the
// user/admin-not-found case are intentionally indistinguishable to callers.
func (s *AuthService) VerifyPassword(ctx context.Context, userID uuid.UUID, password string) error {
	if s.lockedOut(userID) {
		return ErrStepUpRateLimited
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		s.recordStepUpFailure(userID)
		return ErrInvalidCredentials
	}
	if u.DeletedAt != nil {
		s.recordStepUpFailure(userID)
		return ErrInvalidCredentials
	}
	ok, err := crypto.VerifyPassword(u.PasswordHash, password, s.pepper)
	if err != nil {
		return err
	}
	if !ok {
		s.recordStepUpFailure(userID)
		return ErrInvalidCredentials
	}
	s.clearStepUpFailures(userID)
	return nil
}

func (s *AuthService) lockedOut(userID uuid.UUID) bool {
	v, ok := s.stepUpFails.Load(userID)
	if !ok {
		return false
	}
	c := v.(*stepUpCounter)
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.windowAt) > stepUpWindow {
		return false
	}
	return c.count >= stepUpMaxFails
}

func (s *AuthService) recordStepUpFailure(userID uuid.UUID) {
	v, _ := s.stepUpFails.LoadOrStore(userID, &stepUpCounter{})
	c := v.(*stepUpCounter)
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.windowAt) > stepUpWindow {
		c.count = 0
		c.windowAt = time.Now()
	}
	c.count++
}

func (s *AuthService) clearStepUpFailures(userID uuid.UUID) {
	s.stepUpFails.Delete(userID)
}
