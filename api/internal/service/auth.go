// Package service holds business logic that sits between HTTP handlers and repositories.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already registered")
)

type AuthService struct {
	users    *repo.UserRepo
	sessions *repo.SessionRepo
	email    *crypto.EmailCipher
	pepper   []byte
	sessTTL  time.Duration
}

func NewAuthService(users *repo.UserRepo, sessions *repo.SessionRepo, email *crypto.EmailCipher, pepper []byte, sessTTL time.Duration) *AuthService {
	return &AuthService{users: users, sessions: sessions, email: email, pepper: pepper, sessTTL: sessTTL}
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
	}, nil
}

// Register creates a user and opens a session. Returns the user and the plaintext
// session token (to be set as a cookie by the handler).
func (s *AuthService) Register(ctx context.Context, email, password, displayName string) (*User, string, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || password == "" || displayName == "" {
		return nil, "", errors.New("email, password, and display_name are required")
	}
	if len(password) < 10 {
		return nil, "", errors.New("password must be at least 10 characters")
	}

	emailHash := s.email.HashEmail(email)
	if _, err := s.users.FindByEmailHash(ctx, emailHash); err == nil {
		return nil, "", ErrEmailTaken
	} else if !errors.Is(err, repo.ErrNotFound) {
		return nil, "", err
	}

	emailEnc, err := s.email.Encrypt(email)
	if err != nil {
		return nil, "", err
	}
	pwdHash, err := crypto.HashPassword(password, s.pepper)
	if err != nil {
		return nil, "", err
	}

	u := &repo.User{
		EmailHash:      emailHash,
		EmailEncrypted: emailEnc,
		DisplayName:    displayName,
		PasswordHash:   pwdHash,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, "", err
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
