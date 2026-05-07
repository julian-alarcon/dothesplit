package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"

	"github.com/google/uuid"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
)

// Avatar pipeline constants.
//
//   - The browser uploads a 1x1-pixel-per-cell 8x8 PNG (typically ~150-250 B).
//   - The server validates it, then upscales to 256x256 via nearest-neighbour
//     and stores the larger PNG. This keeps the rendered bitmap crisply blocky
//     on every browser (including ones with inconsistent
//     `image-rendering: pixelated` support) without leaking more information —
//     it's still the same 64 color samples, just represented with more pixels.
//   - Storage ceiling is generous: a 256x256 low-color PNG comfortably fits in
//     a few kilobytes.
const (
	AvatarClientSize    = 8                                      // what the browser must upload (64 pixels total)
	AvatarUpscaleFactor = 32                                     // each source pixel becomes a 32x32 block
	AvatarRenderSize    = AvatarClientSize * AvatarUpscaleFactor // 256
	AvatarClientMaxB    = 1024
	AvatarStorageMaxB   = 16 * 1024
)

var (
	ErrWrongPassword = errors.New("old password does not match")
	ErrBadAvatar     = errors.New("avatar must be an 8x8 PNG under 1024 bytes")
	ErrUserDeleted   = errors.New("account is already deleted")
)

type MeService struct {
	users    *repo.UserRepo
	sessions *repo.SessionRepo
	email    *crypto.EmailCipher
	pepper   []byte
}

func NewMeService(users *repo.UserRepo, sessions *repo.SessionRepo, email *crypto.EmailCipher, pepper []byte) *MeService {
	return &MeService{users: users, sessions: sessions, email: email, pepper: pepper}
}

// Rename updates the display name on an active account.
func (s *MeService) Rename(ctx context.Context, userID uuid.UUID, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return errors.New("display_name required")
	}
	if len(displayName) > 80 {
		return errors.New("display_name must be 80 chars or fewer")
	}
	return s.users.UpdateDisplayName(ctx, userID, displayName)
}

// SetWeekStart updates the user's preferred first day of the week.
// Allowed values: 0 (Sunday) or 1 (Monday).
func (s *MeService) SetWeekStart(ctx context.Context, userID uuid.UUID, v int16) error {
	if v != 0 && v != 1 {
		return errors.New("week_start must be 0 (Sunday) or 1 (Monday)")
	}
	return s.users.UpdateWeekStart(ctx, userID, v)
}

// ChangePassword rotates the password after verifying the old one and revokes
// every session the user has (the caller issues a fresh cookie).
func (s *MeService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	if len(newPassword) < 10 {
		return errors.New("new password must be at least 10 characters")
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return ErrUserDeleted
	}
	ok, err := crypto.VerifyPassword(u.PasswordHash, oldPassword, s.pepper)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWrongPassword
	}
	newHash, err := crypto.HashPassword(newPassword, s.pepper)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePasswordHash(ctx, userID, newHash); err != nil {
		return err
	}
	// Nuke all other sessions for this user; caller will issue a fresh one.
	return s.sessions.DeleteAllForUser(ctx, userID)
}

// SetAvatarFromBase64 validates the client-side 8x8 PNG, upscales it to
// AvatarRenderSize x AvatarRenderSize via nearest-neighbour, and stores the
// larger PNG.
//
// Why upscale on the server: storing a 1-pixel-per-cell 8x8 bitmap forces the
// browser to scale it up with CSS, and `image-rendering: pixelated` has
// inconsistent support (notably on iOS Safari / some Android WebViews). A
// pre-scaled 256x256 PNG renders blocky everywhere with no CSS tricks.
//
// This doesn't leak more information than the 8x8 source — it's the same
// 64 color samples, just represented with more pixels.
//
// We also re-encode from a fresh RGBA canvas to strip ancillary PNG chunks
// (tEXt, iTXt, color profiles, EXIF) the client may have attached.
func (s *MeService) SetAvatarFromBase64(ctx context.Context, userID uuid.UUID, b64 string) error {
	if len(b64) == 0 || len(b64) > 2*AvatarClientMaxB {
		return ErrBadAvatar
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBadAvatar, err.Error())
	}
	if len(raw) == 0 || len(raw) > AvatarClientMaxB {
		return ErrBadAvatar
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBadAvatar, err.Error())
	}
	b := img.Bounds()
	if b.Dx() != AvatarClientSize || b.Dy() != AvatarClientSize {
		return ErrBadAvatar
	}

	// Sample each source pixel into an AvatarUpscaleFactor-sized block of the
	// destination canvas. Equivalent to nearest-neighbour upscaling; every
	// output pixel is a direct copy of one source pixel, no blending.
	big := image.NewRGBA(image.Rect(0, 0, AvatarRenderSize, AvatarRenderSize))
	for sy := 0; sy < AvatarClientSize; sy++ {
		for sx := 0; sx < AvatarClientSize; sx++ {
			c := img.At(b.Min.X+sx, b.Min.Y+sy)
			for dy := 0; dy < AvatarUpscaleFactor; dy++ {
				for dx := 0; dx < AvatarUpscaleFactor; dx++ {
					big.Set(sx*AvatarUpscaleFactor+dx, sy*AvatarUpscaleFactor+dy, c)
				}
			}
		}
	}

	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, big); err != nil {
		return err
	}
	if out.Len() > AvatarStorageMaxB {
		// Large solid-color regions compress to hundreds of bytes, so we'd have
		// to work hard to hit this ceiling, but reject if we ever do.
		return ErrBadAvatar
	}
	return s.users.SetAvatar(ctx, userID, out.Bytes())
}

// ClearAvatar removes the user's avatar so the UI falls back to initials.
func (s *MeService) ClearAvatar(ctx context.Context, userID uuid.UUID) error {
	return s.users.SetAvatar(ctx, userID, nil)
}

// GetAvatar returns the stored 8x8 PNG bytes of the target user. Callers are
// expected to enforce authorization (share-a-group) before calling.
func (s *MeService) GetAvatar(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	return s.users.GetAvatar(ctx, userID)
}

// SoftDelete tombstones the account: scrambles email_hash / email_encrypted /
// password_hash so no one can re-discover the deleted user by re-registering
// or by dump inspection, renames the user to a stable tombstone derived from
// the first 8 hex chars of their UUID, and destroys every active session.
//
// Existing expenses / splits / settlements keep pointing at this row, so the
// ledger stays intact; UI renders "Deleted user #xxxxxxxx" for these entries.
func (s *MeService) SoftDelete(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return ErrUserDeleted
	}

	tombstone := "Deleted user #" + u.ID.String()[:8]

	// Random 32-byte sentinel for fields we never want to match against a
	// real user or to decrypt cleanly again.
	scrambled := make([]byte, 32)
	if _, err := rand.Read(scrambled); err != nil {
		return err
	}

	if err := s.users.SoftDelete(ctx, userID, tombstone, scrambled, scrambled, "!deleted:"+u.ID.String()); err != nil {
		return err
	}
	return s.sessions.DeleteAllForUser(ctx, userID)
}
