package model

import (
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelCursorBindsStableUserQueryAndOriginalWatermark(t *testing.T) {
	actor := testActor(t)
	store := &noIOStore{}
	s, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	b, e := listBinding(actor, "providers", "")
	if e != nil {
		t.Fatal(e)
	}
	at, _ := f.NewInstant(time.Now())
	older, _ := f.NewInstant(at.Time().Add(-time.Second))
	top, last := mustID[mc.Provider](t).String(), mustID[mc.Provider](t).String()
	token, e := s.signPage(b, pagePosition{}, at, top, older, last)
	if e != nil {
		t.Fatal(e)
	}
	uid, _ := f.ParseID[id.User](actor.Details().UserID)
	newSession, _ := id.NewHuman(uid, mustID[id.Session](t))
	same, _ := listBinding(newSession, "providers", "")
	for _, limit := range []int{1, 100} {
		p, e := s.pagePosition(SystemQuery{Cursor: token, Limit: limit}, same)
		if e != nil || p.WaterAt != at || p.WaterID != top || p.AfterID != last {
			t.Fatal("limit/Session altered original page binding", e)
		}
	}
	for _, binding := range []struct {
		actor          id.Actor
		kind, provider string
	}{{testActor(t), "providers", ""}, {actor, "models", top}} {
		other, _ := listBinding(binding.actor, binding.kind, binding.provider)
		if _, e = s.pagePosition(SystemQuery{Cursor: token, Limit: 1}, other); e == nil {
			t.Fatal("cursor accepted different user/query")
		}
	}
	if _, e = s.pagePosition(SystemQuery{Cursor: token + "x", Limit: 1}, b); e == nil {
		t.Fatal("tampered signature accepted")
	}
	if _, e = s.pagePosition(SystemQuery{Limit: 0}, b); e == nil {
		t.Fatal("unbounded limit accepted")
	}
}
func TestModelUnconfiguredSelectionIsExplicitAbsence(t *testing.T) {
	r := &selectionRecord{ID: mustID[struct{}](t).String(), Version: 1}
	v, e := selectionView(r)
	if e != nil || v.Configured != nil || v.Version != 1 {
		t.Fatal("technical identity invented empty model selection", e)
	}
	r.Configured = true
	if _, e = selectionView(r); e == nil {
		t.Fatal("required selectors omitted")
	}
}
