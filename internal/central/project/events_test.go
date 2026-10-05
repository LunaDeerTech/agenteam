package project

import (
	"encoding/json"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestEventFactUsesTypedCanonicalPayload(t *testing.T) {
	catalog := event.NewCatalog()
	events, e := c.RegisterProjectEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	p := testProject(t)
	now, _ := foundation.NewInstant(time.Now())
	id := testID[event.EventIdentity](t)
	h, e := eventHeader(id, p.ID, c.CreatedEventName, 1, now)
	if e != nil {
		t.Fatal(e)
	}
	payload := c.CreatedPayload{OwnerUserID: p.OwnerUserID, CreationID: testID[c.Creation](t)}
	ev, e := events.NewCreated(h, payload)
	if e != nil {
		t.Fatal(e)
	}
	header, _ := ev.HeaderJSON()
	reordered, _ := json.Marshal(map[string]string{"creation_id": payload.CreationID.String(), "owner_user_id": payload.OwnerUserID.String()})
	if e = exactEvent(ev.Summary(), header, reordered); e != nil {
		t.Fatal("typed JSONB payload lost identity", e)
	}
	summary := ev.Summary()
	summary.Header.EventID = testID[event.EventIdentity](t)
	if exactEvent(summary, header, reordered) == nil {
		t.Fatal("event ID substitution")
	}
	summary = ev.Summary()
	summary.PayloadDigest = digest([]byte("other"))
	if exactEvent(summary, header, reordered) == nil {
		t.Fatal("payload substitution")
	}
}
func TestAppendPlanBindsCurrentSession(t *testing.T) {
	actor := testActor(t)
	other := testActor(t)
	// Same stable user and a fresh session keep command identity, but each
	// discovered dependency plan is bound to the actual current caller.
	user, _ := foundation.ParseID[identity.User](actor.Details().UserID)
	session, _ := foundation.ParseID[identity.Session](other.Details().SessionID)
	other, _ = identity.NewHuman(user, session)
	p := testProject(t)
	at, _ := foundation.NewInstant(time.Now())
	h, _ := eventHeader(testID[event.EventIdentity](t), p.ID, c.UpdatedEventName, 2, at)
	summary := event.Summary{Producer: c.ProjectProducer, Header: h, PayloadDigest: digest([]byte("payload"))}
	a, e := appendBinding(actor, summary)
	if e != nil {
		t.Fatal(e)
	}
	b, e := appendBinding(other, summary)
	if e != nil || a == b {
		t.Fatal("stale Session plan reused", e)
	}
}
