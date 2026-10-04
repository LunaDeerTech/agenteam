package contract

import (
	"context"
	"io"
	"math"
	"reflect"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	object "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// AvatarMetadata describes the current, re-encoded avatar. It contains no
// storage key or reference and does not establish that its payload is readable.
type AvatarMetadata struct {
	MediaType string
	ByteSize  foundation.Progress
	SHA256    foundation.Digest
}

// ProfileView is a service projection, not an HTTP wire envelope. A nil Avatar
// means that the user has no current avatar.
type ProfileView struct {
	User   User
	Avatar *AvatarMetadata
}

type ProfileMutation struct {
	Actor           identity.Actor
	Key             foundation.IdempotencyKey
	ExpectedVersion foundation.Version
}

// Validate checks structure only. The service must still check the current
// Session and User under its complete transaction lock plan.
func (r ProfileMutation) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != identity.Human || r.Key.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return Invalid()
	}
	return nil
}

type ProfileChange struct {
	ProfileMutation
	Username    *string
	DisplayName *string
}

func (r ProfileChange) Validate() error {
	if r.ProfileMutation.Validate() != nil || r.Username == nil && r.DisplayName == nil {
		return Invalid()
	}
	if r.Username != nil && !validProfileUsername(*r.Username) {
		return Invalid()
	}
	if r.DisplayName != nil && !validProfileDisplayName(*r.DisplayName) {
		return Invalid()
	}
	return nil
}

type ThemeChange struct {
	ProfileMutation
	Theme Theme
}

func (r ThemeChange) Validate() error {
	if r.ProfileMutation.Validate() != nil || !r.Theme.Valid() {
		return Invalid()
	}
	return nil
}

// AvatarUpload owns a single raw image stream at the service boundary. The
// service closes Body on every exit and verifies the actual length, container,
// media type and original bytes. Validate never reads or closes the stream.
type AvatarUpload struct {
	ProfileMutation
	MediaType string
	ByteSize  int64
	Body      io.ReadCloser
}

func (r AvatarUpload) Validate() error {
	if r.ProfileMutation.Validate() != nil || nilAvatarBody(r.Body) || r.ByteSize != -1 && (r.ByteSize < 1 || r.ByteSize > 5<<20) {
		return Invalid()
	}
	switch r.MediaType {
	case "image/jpeg", "image/png", "image/webp":
		return nil
	default:
		return Invalid()
	}
}

type AvatarRangeKind string

const (
	AvatarRangeAll    AvatarRangeKind = "all"
	AvatarRangeClosed AvatarRangeKind = "closed"
	AvatarRangeFrom   AvatarRangeKind = "from"
	AvatarRangeSuffix AvatarRangeKind = "suffix"
)

func (k AvatarRangeKind) Valid() bool {
	switch k {
	case AvatarRangeAll, AvatarRangeClosed, AvatarRangeFrom, AvatarRangeSuffix:
		return true
	default:
		return false
	}
}

// AvatarRange describes an unresolved range for the exact current object.
// Closed uses Offset >= 0 and Length > 0; from uses Offset >= 0 and Length == 0;
// suffix uses Offset == 0 and Length > 0; all requires both fields to be zero.
type AvatarRange struct {
	Kind           AvatarRangeKind
	Offset, Length int64
}

// Validate does not infer EOF or satisfiability without the object's size.
func (r AvatarRange) Validate() error {
	switch r.Kind {
	case AvatarRangeAll:
		if r.Offset == 0 && r.Length == 0 {
			return nil
		}
	case AvatarRangeClosed:
		if r.Offset >= 0 && r.Length > 0 && r.Offset <= math.MaxInt64-r.Length {
			return nil
		}
	case AvatarRangeFrom:
		if r.Offset >= 0 && r.Length == 0 {
			return nil
		}
	case AvatarRangeSuffix:
		if r.Offset == 0 && r.Length > 0 {
			return nil
		}
	}
	return Invalid()
}

// ProfilePort is the current user's profile boundary. Implementations must
// check current authority for every call; parameter validation is no grant.
// ReadAvatar keeps its actual account operation registered until the returned
// reader really closes and joins, including the underlying object lease.
type ProfilePort interface {
	GetProfile(context.Context, identity.Actor) (ProfileView, error)
	UpdateProfile(context.Context, ProfileChange) (ProfileView, error)
	SetTheme(context.Context, ThemeChange) (ProfileView, error)
	PutAvatar(context.Context, AvatarUpload) (ProfileView, error)
	DeleteAvatar(context.Context, ProfileMutation) (ProfileView, error)
	ReadAvatar(context.Context, identity.Actor, AvatarRange) (*object.ObjectReader, error)
}

// UserRoute is a current persisted route, including the bootstrap admin name.
// It is not a user-creation request and is not authorization by username.
type UserRoute struct {
	UserID   identity.UserID
	Username string
	Version  foundation.Version
}

// CurrentUserRoutes requires a live transaction from the same Store, a held
// User SH (or EX) lock, and revalidation of the current Session and its User.
// It must not open a transaction, acquire locks, touch activity or query others.
type CurrentUserRoutes interface {
	CurrentUserRouteInTx(context.Context, foundation.Tx, identity.Actor) (UserRoute, error)
}

func validProfileUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	// Canonicalization and current-user reserved-name rules belong to the
	// service, so validation neither rewrites input nor rejects stored admin.
	return true
}

func validProfileDisplayName(s string) bool {
	if !utf8.ValidString(s) || len(s) > 320 || utf8.RuneCountInString(s) > 80 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func nilAvatarBody(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	v := reflect.ValueOf(body)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
