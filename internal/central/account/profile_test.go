package account

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func b04ProfileActor(t *testing.T) identity.Actor {
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
	return actor
}

// This observer rejects every transaction requirement. It proves only local
// delegation/no-new-Tx behavior; live/foreign/ended PG handles await integration.
type b04ProfileLockObserver struct {
	Store
	called int
	tx     foundation.Tx
	locks  []foundation.LockRequest
	err    error
}

func (s *b04ProfileLockObserver) RequireHeldLocks(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.called++
	s.tx = tx
	s.locks = append([]foundation.LockRequest(nil), locks...)
	return s.err
}

func TestB04ProfileRouteRejectsZeroWithoutOpeningTransaction(t *testing.T) {
	actor := b04ProfileActor(t)
	observer := &b04ProfileLockObserver{err: errors.New("fixture requirement rejected")}
	a := &Authority{data: func() *authorityState { return &authorityState{store: observer} }}
	if _, err := a.CurrentUserRouteInTx(context.Background(), foundation.Tx{}, actor); err == nil || observer.called != 0 {
		t.Fatal("zero transaction entered the authority or opened a transaction")
	}
	tx := foundation.NewTx()
	if _, err := a.CurrentUserRouteInTx(context.Background(), tx, actor); !errors.Is(err, observer.err) {
		t.Fatal("requirement rejection was not retained", err)
	}
	want := userLock(actor.Details().UserID, foundation.Shared)
	if observer.called != 1 || observer.tx != tx || len(observer.locks) != 1 || observer.locks[0].Mode != want.Mode || observer.locks[0].Key.Canonical() != want.Key.Canonical() {
		t.Fatal("route did not delegate exact same-transaction User SH requirement")
	}
	if _, err := a.CurrentUserRouteInTx(context.Background(), tx, identity.Actor{}); err == nil || observer.called != 1 {
		t.Fatal("invalid actor reached storage")
	}
	var absent *Authority
	if _, err := absent.CurrentUserRouteInTx(context.Background(), tx, actor); err == nil {
		t.Fatal("absent authority accepted")
	}
}

func TestB04ProfileRouteProjectsCanonicalStoredAdmin(t *testing.T) {
	actor := b04ProfileActor(t)
	userID, _ := foundation.ParseID[identity.User](actor.Details().UserID)
	user := c.User{ID: userID, Username: "admin", Version: math.MaxInt64}
	route, err := projectUserRoute(user)
	if err != nil || route.UserID != userID || route.Username != "admin" || route.Version != math.MaxInt64 {
		t.Fatal("bootstrap route rejected or rewritten", err)
	}
	for _, name := range []string{"", "ad", "Admin", " admin", "admin ", "-admin", "admin-", "a/b", "aKb", strings.Repeat("a", 33)} {
		user.Username = name
		if _, err := projectUserRoute(user); err == nil {
			t.Fatal("malformed persisted route accepted")
		}
	}
	user.Username = "valid-name"
	user.Version = 0
	if _, err := projectUserRoute(user); err == nil {
		t.Fatal("zero route version accepted")
	}
	user.Version = 1
	user.ID = c.UserID{}
	if _, err := projectUserRoute(user); err == nil {
		t.Fatal("zero User accepted")
	}
}

func TestB04ProfilePatchNormalizesOnlyRequestedFields(t *testing.T) {
	current := c.User{Username: "admin", DisplayName: "Original name"}
	mutation := c.ProfileMutation{Actor: b04ProfileActor(t), Key: "profile-change", ExpectedVersion: 1}
	name, display := "ADMIN", ""
	patch, err := normalizeProfilePatch(current, c.ProfileChange{ProfileMutation: mutation, Username: &name, DisplayName: &display})
	if err != nil || patch.username != "admin" || patch.displayName != "" || name != "ADMIN" {
		t.Fatal("keeping the original reserved name/clearing display failed", err)
	}
	name = "New-Route"
	patch, err = normalizeProfilePatch(current, c.ProfileChange{ProfileMutation: mutation, Username: &name})
	if err != nil || patch.username != "new-route" || patch.displayName != current.DisplayName || name != "New-Route" {
		t.Fatal("normalization changed missing/caller fields", err)
	}
	current.Username = "ordinary-user"
	for _, reserved := range []string{"ADMIN", "settings", "root"} {
		name = reserved
		if _, err := normalizeProfilePatch(current, c.ProfileChange{ProfileMutation: mutation, Username: &name}); err == nil {
			t.Fatal("new reserved route accepted")
		}
	}
	display = strings.Repeat("🌱", 80)
	patch, err = normalizeProfilePatch(current, c.ProfileChange{ProfileMutation: mutation, DisplayName: &display})
	if err != nil || patch.username != current.Username || patch.displayName != display {
		t.Fatal("display-only patch altered route", err)
	}
	display += "x"
	if _, err := normalizeProfilePatch(current, c.ProfileChange{ProfileMutation: mutation, DisplayName: &display}); err == nil {
		t.Fatal("oversized display accepted")
	}
}

func TestB04ProfileProjectionExcludesPrivateRecordAndCopiesMetadata(t *testing.T) {
	const private = "fixture-private-password-phc"
	record := userRecord{user: c.User{Username: "admin", Theme: c.DarkTheme, Version: 1}, phc: private, passwordVersion: 991, sequence: 772}
	metadata := c.AvatarMetadata{MediaType: "image/jpeg", ByteSize: 17, SHA256: foundation.Digest("sha256:" + strings.Repeat("a", 64))}
	view := projectProfile(record, &metadata)
	metadata.MediaType = "changed"
	if view.Avatar == nil || view.Avatar.MediaType != "image/jpeg" || projectProfile(record, nil).Avatar != nil {
		t.Fatal("avatar optionality/copy changed after projection")
	}
	// User requires its ID for JSON; use a real typed fixture identity.
	view.User.ID, _ = foundation.ParseID[identity.User](b04ProfileActor(t).Details().UserID)
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{private, "passwordVersion", "sequence", "password_phc", "object_key"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("profile copied private record fields")
		}
	}
}
