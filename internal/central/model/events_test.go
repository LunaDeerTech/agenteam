package model

import (
	"context"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
)

type noIOOutboxStore struct{ outbox.Store }

func TestModelEventsUseExplicitCatalogAndWrongOwnerFailsBeforeSQL(t *testing.T) {
	catalog := ec.NewCatalog()
	events, e := DefineEvents(catalog)
	if e != nil || !events.valid() {
		t.Fatal("events registration", e)
	}
	if _, e = DefineEvents(catalog); e == nil {
		t.Fatal("duplicate definitions accepted")
	}
	if _, e = DefineEvents(nil); e == nil {
		t.Fatal("nil catalog accepted")
	}
	sealed := ec.NewCatalog()
	_ = sealed.Seal()
	if _, e = DefineEvents(sealed); e == nil {
		t.Fatal("sealed catalog accepted")
	}
	resource := mustID[mc.Provider](t).String()
	now, _ := f.NewInstant(time.Now())
	header, e := newHeader(ConfigurationChangedEvent, ConfigurationAggregate, resource, 1, now)
	if e != nil {
		t.Fatal(e)
	}
	event, e := ec.NewEvent(events.data().configuration, header, mc.ConfigurationChanged{AggregateID: resource, Version: 1, Scope: "system", ChangedFields: []string{"created"}})
	if e != nil {
		t.Fatal(e)
	}
	if !catalog.Owns(event) {
		t.Fatal("event detached from registration catalog")
	}
	wrong := ec.NewCatalog()
	if _, e = DefineEvents(wrong); e != nil {
		t.Fatal(e)
	}
	real, e := outbox.New(&noIOOutboxStore{}, wrong, outbox.Authorizations{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = real.PrepareAppend(context.Background(), testActor(t), event); e == nil {
		t.Fatal("real Outbox accepted different catalog")
	}
}

func TestModelEventPlansBindFullActorAndExactRegisteredPayload(t *testing.T) {
	store := &noIOStore{}
	authority := pureAuthority(t, store)
	actor := testActor(t)
	now, _ := f.NewInstant(time.Now())
	d := testDependencies(t)
	s, e := New(store, authority, d)
	if e != nil {
		t.Fatal(e)
	}
	resource := mustID[mc.Model](t).String()
	p := &preparedCommand{authority: authority, actor: actor, plan: mutationPlan{CommandID: mustID[struct{}](t).String(), Resource: resource, Receipt: mc.CommandReceipt{Kind: "model.update", ResourceID: resource, Version: 2}, Changed: []string{"name"}, At: now}}
	if e = s.prepareEvents(p); e != nil {
		t.Fatal(e)
	}
	ctx := commandContext(context.Background(), p)
	event := p.events[0]
	deps, e := authority.DiscoverAppend(ctx, actor, event.Summary())
	if e != nil || deps.Validate() != nil {
		t.Fatal(e)
	}
	if _, e = authority.DiscoverAppend(context.Background(), actor, event.Summary()); e == nil {
		t.Fatal("request without issuer context accepted")
	}
	altered := event.Summary()
	altered.PayloadDigest = hash([]byte("changed"))
	if _, e = authority.DiscoverAppend(ctx, actor, altered); e == nil {
		t.Fatal("changed payload accepted")
	}
	if _, e = authority.DiscoverAppend(ctx, testActor(t), event.Summary()); e == nil {
		t.Fatal("other actor accepted")
	}
	other := pureAuthority(t, store)
	if _, e = other.DiscoverAppend(ctx, actor, event.Summary()); e == nil {
		t.Fatal("other issuer accepted")
	}
}

func TestModelMeetingSummaryConfigurationEvents(t *testing.T) {
	store := &noIOStore{}
	service, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	for _, deleting := range []bool{false, true} {
		plan := summaryPlanFixture(t)
		if deleting {
			plan.Kind = "model.delete"
			plan.Resource = mustID[mc.Model](t).String()
			plan.Receipt = mc.CommandReceipt{Kind: "model.delete", ResourceID: plan.Resource, Version: 2}
			plan.Changed = []string{"deleted", "replacement"}
			before := &selectionRecord{ID: mustID[struct{}](t).String(), Version: 2, Configured: true, Embedding: mustID[mc.Model](t).String(), Memory: plan.Resource}
			after := *before
			after.Version++
			after.Memory = plan.AfterMeetingSummary.Model
			plan.BeforeSelection = before
			plan.AfterSelection = &after
		}
		prepared := &preparedCommand{plan: plan}
		if e = service.prepareEvents(prepared); e != nil {
			t.Fatal(e)
		}
		want := 1
		if deleting {
			want = 3
		}
		if len(prepared.plan.Events) != want || len(prepared.events) != want {
			t.Fatal("configuration events omitted an owner")
		}
		for _, event := range prepared.plan.Events {
			if event.Header.EventType != ConfigurationChangedEvent || !validPersistedEvent(event) {
				t.Fatal("Summary invented an embedding change or invalid event")
			}
		}
		last := prepared.plan.Events[want-1]
		if last.Header.AggregateID.String() != plan.AfterMeetingSummary.ID || *last.Header.AggregateVersion != plan.AfterMeetingSummary.Version {
			t.Fatal("Summary event uses another owner's identity/version")
		}
	}
}
