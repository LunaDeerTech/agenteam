package contract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	object "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Compile the consumer's complete interface independently of any implementation.
// No concrete successful provider or database behavior is supplied by C0.
var _ interface {
	GetProfile(context.Context, identity.Actor) (ProfileView, error)
	UpdateProfile(context.Context, ProfileChange) (ProfileView, error)
	SetTheme(context.Context, ThemeChange) (ProfileView, error)
	PutAvatar(context.Context, AvatarUpload) (ProfileView, error)
	DeleteAvatar(context.Context, ProfileMutation) (ProfileView, error)
	ReadAvatar(context.Context, identity.Actor, AvatarRange) (*object.ObjectReader, error)
} = ProfilePort(nil)

var _ interface {
	CurrentUserRouteInTx(context.Context, foundation.Tx, identity.Actor) (UserRoute, error)
} = CurrentUserRoutes(nil)

func profileContractMutation(t *testing.T) ProfileMutation {
	t.Helper()
	user, err := foundation.ParseID[identity.User]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	session, err := foundation.ParseID[identity.Session]("01900000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	return ProfileMutation{Actor: actor, Key: "profile-command", ExpectedVersion: 1}
}

func profileContractInvalid(t *testing.T, err error) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != foundation.InvalidArgument || fault.CommitState != foundation.NotStarted {
		t.Fatalf("expected safe structural rejection, got %v", err)
	}
}

func TestProfileContractMutationRequiresHumanAndValidCommand(t *testing.T) {
	base := profileContractMutation(t)
	project, _ := foundation.ParseID[identity.Project]("01900000-0000-7000-8000-000000000003")
	agent, _ := foundation.ParseID[identity.Agent]("01900000-0000-7000-8000-000000000004")
	execution, _ := foundation.ParseID[identity.Execution]("01900000-0000-7000-8000-000000000005")
	agentActor, err := identity.NewAgentRun(project, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := identity.RegisterService(identity.AccountMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	serviceActor, err := registration.Actor("01900000-0000-7000-8000-000000000006", identity.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*ProfileMutation)
	}{
		{"zero_actor", func(r *ProfileMutation) { r.Actor = identity.Actor{} }},
		{"agent", func(r *ProfileMutation) { r.Actor = agentActor }},
		{"service", func(r *ProfileMutation) { r.Actor = serviceActor }},
		{"empty_key", func(r *ProfileMutation) { r.Key = "" }},
		{"invalid_key", func(r *ProfileMutation) { r.Key = "private\nkey" }},
		{"long_key", func(r *ProfileMutation) { r.Key = foundation.IdempotencyKey(strings.Repeat("a", 129)) }},
		{"zero_version", func(r *ProfileMutation) { r.ExpectedVersion = 0 }},
		{"negative_version", func(r *ProfileMutation) { r.ExpectedVersion = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			profileContractInvalid(t, r.Validate())
		})
	}
	for _, version := range []foundation.Version{1, 9007199254740993, math.MaxInt64} {
		r := base
		r.ExpectedVersion = version
		r.Key = foundation.IdempotencyKey(strings.Repeat("a", 128))
		if err := r.Validate(); err != nil {
			t.Fatal("valid structural command", err)
		}
	}
}

func TestProfileContractPatchPreservesMissingAndExplicitValues(t *testing.T) {
	base := profileContractMutation(t)
	profileContractInvalid(t, (ProfileChange{ProfileMutation: base}).Validate())
	for _, tc := range []struct {
		name string
		text string
		ok   bool
	}{
		{"minimum", "a00", true}, {"maximum", strings.Repeat("a", 32), true},
		{"case_preserved", "A-B", true}, {"stored_admin", "admin", true},
		{"reserved_is_business_rule", "settings", true}, {"inner_hyphens", "a--z", true},
		{"empty", "", false}, {"short", "ab", false}, {"long", strings.Repeat("a", 33), false},
		{"leading_hyphen", "-abc", false}, {"trailing_hyphen", "abc-", false},
		{"no_trim", " abc", false}, {"underscore", "a_b", false},
		{"non_ascii", "aKb", false}, {"invalid_utf8", "ab\xff", false},
		{"control", "ab\n", false}, {"path", "a/b", false},
	} {
		t.Run("username/"+tc.name, func(t *testing.T) {
			value := tc.text
			r := ProfileChange{ProfileMutation: base, Username: &value}
			err := r.Validate()
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				profileContractInvalid(t, err)
			}
			if value != tc.text {
				t.Fatal("validation rewrote the input")
			}
		})
	}
	for _, tc := range []struct {
		name string
		text string
		ok   bool
	}{
		{"empty_uses_email", "", true}, {"spaces_preserved", "  A name  ", true},
		{"eighty_ascii", strings.Repeat("a", 80), true},
		{"eighty_four_byte_runes", strings.Repeat("🌱", 80), true},
		{"combining_codepoints", strings.Repeat("e\u0301", 40), true},
		{"not_grapheme_count", strings.Repeat("e\u0301", 41), false},
		{"eighty_one_ascii", strings.Repeat("a", 81), false},
		{"byte_limit", strings.Repeat("🌱", 81), false},
		{"bad_utf8", "\xff", false}, {"newline", "private\nname", false},
		{"nul", "private\x00name", false}, {"del", "private\x7fname", false},
		{"unicode_control", "private\u0085name", false},
	} {
		t.Run("display/"+tc.name, func(t *testing.T) {
			value := tc.text
			r := ProfileChange{ProfileMutation: base, DisplayName: &value}
			err := r.Validate()
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				profileContractInvalid(t, err)
				if strings.Contains(fmt.Sprint(err), tc.text) {
					t.Fatal("invalid name echoed by the error")
				}
			}
			if value != tc.text {
				t.Fatal("validation rewrote the input")
			}
		})
	}
	username, display := "valid-name", ""
	r := ProfileChange{ProfileMutation: base, Username: &username, DisplayName: &display}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.ProfileMutation = ProfileMutation{}
	profileContractInvalid(t, r.Validate())
}

func TestProfileContractThemeUsesClosedValues(t *testing.T) {
	base := profileContractMutation(t)
	for _, theme := range []Theme{SystemTheme, LightTheme, DarkTheme} {
		if err := (ThemeChange{ProfileMutation: base, Theme: theme}).Validate(); err != nil {
			t.Fatal(err)
		}
		profileContractInvalid(t, (ThemeChange{Theme: theme}).Validate())
	}
	for _, theme := range []Theme{"", "auto", "SYSTEM", " dark", "light\n"} {
		profileContractInvalid(t, (ThemeChange{ProfileMutation: base, Theme: theme}).Validate())
	}
}

type profileBodySpy struct{ reads, closes int }

func (b *profileBodySpy) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *profileBodySpy) Close() error             { b.closes++; return nil }

type profileNilFunctionBody func()

func (profileNilFunctionBody) Read([]byte) (int, error) { panic("must not read") }
func (profileNilFunctionBody) Close() error             { panic("must not close") }

type profileNilMapBody map[string]byte

func (profileNilMapBody) Read([]byte) (int, error) { panic("must not read") }
func (profileNilMapBody) Close() error             { panic("must not close") }

type profileNilSliceBody []byte

func (profileNilSliceBody) Read([]byte) (int, error) { panic("must not read") }
func (profileNilSliceBody) Close() error             { panic("must not close") }

type profileNilChannelBody chan byte

func (profileNilChannelBody) Read([]byte) (int, error) { panic("must not read") }
func (profileNilChannelBody) Close() error             { panic("must not close") }

func TestAvatarContractUploadOnlyValidatesStructure(t *testing.T) {
	base := profileContractMutation(t)
	body := &profileBodySpy{}
	for _, media := range []string{"image/jpeg", "image/png", "image/webp"} {
		for _, size := range []int64{-1, 1, 5 << 20} {
			if err := (AvatarUpload{base, media, size, body}).Validate(); err != nil {
				t.Fatalf("valid declaration %s/%d: %v", media, size, err)
			}
		}
	}
	for _, size := range []int64{math.MinInt64, -2, 0, (5 << 20) + 1, math.MaxInt64} {
		profileContractInvalid(t, (AvatarUpload{base, "image/jpeg", size, body}).Validate())
	}
	for _, media := range []string{"", "image/jpg", "image/gif", "image/svg+xml", "IMAGE/PNG", "image/png;name=private", " image/png", "image/png\r\n"} {
		profileContractInvalid(t, (AvatarUpload{base, media, 1, body}).Validate())
	}
	for _, value := range []io.ReadCloser{nil, (*profileBodySpy)(nil), profileNilFunctionBody(nil), profileNilMapBody(nil), profileNilSliceBody(nil), profileNilChannelBody(nil)} {
		profileContractInvalid(t, (AvatarUpload{base, "image/jpeg", -1, value}).Validate())
	}
	profileContractInvalid(t, (AvatarUpload{MediaType: "image/jpeg", ByteSize: 1, Body: body}).Validate())
	if body.reads != 0 || body.closes != 0 {
		t.Fatal("parameter validation consumed or closed caller's stream")
	}
}

func TestAvatarContractRangeMatrixAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value AvatarRange
		ok    bool
	}{
		{"all", AvatarRange{AvatarRangeAll, 0, 0}, true},
		{"all_offset", AvatarRange{AvatarRangeAll, 1, 0}, false},
		{"all_length", AvatarRange{AvatarRangeAll, 0, 1}, false},
		{"closed", AvatarRange{AvatarRangeClosed, 0, 1}, true},
		{"closed_max_end", AvatarRange{AvatarRangeClosed, math.MaxInt64 - 1, 1}, true},
		{"closed_max_length", AvatarRange{AvatarRangeClosed, 0, math.MaxInt64}, true},
		{"closed_add_overflow", AvatarRange{AvatarRangeClosed, math.MaxInt64, 1}, false},
		{"closed_large_overflow", AvatarRange{AvatarRangeClosed, math.MaxInt64, math.MaxInt64}, false},
		{"closed_negative_offset", AvatarRange{AvatarRangeClosed, -1, 1}, false},
		{"closed_zero_length", AvatarRange{AvatarRangeClosed, 0, 0}, false},
		{"closed_negative_length", AvatarRange{AvatarRangeClosed, 0, math.MinInt64}, false},
		{"from_zero", AvatarRange{AvatarRangeFrom, 0, 0}, true},
		{"from_max_not_pretend_416", AvatarRange{AvatarRangeFrom, math.MaxInt64, 0}, true},
		{"from_negative", AvatarRange{AvatarRangeFrom, -1, 0}, false},
		{"from_extra_length", AvatarRange{AvatarRangeFrom, 0, 1}, false},
		{"suffix_one", AvatarRange{AvatarRangeSuffix, 0, 1}, true},
		{"suffix_max_not_pretend_416", AvatarRange{AvatarRangeSuffix, 0, math.MaxInt64}, true},
		{"suffix_zero", AvatarRange{AvatarRangeSuffix, 0, 0}, false},
		{"suffix_negative", AvatarRange{AvatarRangeSuffix, 0, -1}, false},
		{"suffix_extra_offset", AvatarRange{AvatarRangeSuffix, 1, 1}, false},
		{"missing_kind", AvatarRange{"", 0, 0}, false},
		{"case_alias", AvatarRange{"ALL", 0, 0}, false},
		{"unknown_kind", AvatarRange{"bounded", 0, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.value.Validate()
			if tc.ok {
				if err != nil || !tc.value.Kind.Valid() {
					t.Fatal("valid unresolved range rejected", err)
				}
			} else {
				profileContractInvalid(t, err)
			}
		})
	}
	for _, kind := range []AvatarRangeKind{"", "ALL", "bounded", "all\n"} {
		if kind.Valid() {
			t.Fatal("range kind accepted outside the closed set")
		}
	}
}
