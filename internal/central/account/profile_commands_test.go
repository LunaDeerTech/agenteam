package account

import (
	"bytes"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestB04ProfileCommandUsesStableActorAndExplicitBusinessFields(t *testing.T) {
	keys := testKeys(t)
	state := &serviceState{keys: keys}
	core := &Service{data: func() *serviceState { return state }}
	p := &ProfileService{data: func() *profileState { return &profileState{core: core} }}
	r := c.ProfileMutation{Actor: b04ProfileActor(t), Key: "same-intent", ExpectedVersion: 9007199254740993}
	get := func(r c.ProfileMutation, name string, fields ...any) []byte {
		t.Helper()
		mac, e := p.profileMAC(r, name, fields...)
		if e != nil {
			t.Fatal(e)
		}
		value, e := mac(keys.current())
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	want := get(r, "profile-update", "safe-name", nil)
	otherSession, _ := foundation.NewID[identity.Session]()
	user, _ := foundation.ParseID[identity.User](r.Actor.Details().UserID)
	changed := r
	changed.Actor, _ = identity.NewHuman(user, otherSession)
	if !bytes.Equal(want, get(changed, "profile-update", "safe-name", nil)) {
		t.Fatal("Session entered stable command semantic")
	}
	for _, got := range [][]byte{get(r, "profile-update", "other-name", nil), get(r, "profile-update", "safe-name", ""), get(r, "theme", "safe-name", nil), get(c.ProfileMutation{Actor: r.Actor, Key: r.Key, ExpectedVersion: r.ExpectedVersion + 1}, "profile-update", "safe-name", nil)} {
		if bytes.Equal(want, got) {
			t.Fatal("business semantic collapsed")
		}
	}
	if _, e := p.profileMAC(r, "profile-update", make(chan int)); e == nil {
		t.Fatal("encoding failure hashed as empty input")
	}
}
