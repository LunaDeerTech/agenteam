package project

import (
	"fmt"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"strings"
	"testing"
	"time"
)

func independentGateID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K](fmt.Sprintf("01920000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal("gate ID fixture failed")
	}
	return v
}
func TestIndependentTaskProjectGateExactTuple(t *testing.T) {
	user := independentGateID[i.User](t, 1)
	actor, err := i.NewHuman(user, independentGateID[i.Session](t, 2))
	if err != nil {
		t.Fatal("gate actor fixture failed")
	}
	at, err := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal("gate time fixture failed")
	}
	version := f.Version(1)
	base := oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: independentGateID[i.Project](t, 3), Actor: actor, Stage: oc.CurrentAccess, Event: e.Summary{Producer: "work", PayloadDigest: f.Digest("sha256:" + strings.Repeat("a", 64)), Header: e.Header{EventID: independentGateID[e.EventIdentity](t, 4), EventType: "work.task_changed", SchemaVersion: 1, OccurredAt: at, Scope: e.Scope{Kind: e.ProjectScope, ProjectID: independentGateID[e.Project](t, 3)}, AggregateType: "work.task", AggregateID: independentGateID[e.Aggregate](t, 5), AggregateVersion: &version}}}
	var currentDigest f.Digest
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		d := base
		d.Stage = stage
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal("valid gate request rejected")
		}
		digest, locks, err := workEventBinding(r)
		if err != nil || len(locks) != 2 || locks[0].Mode != f.Shared || locks[1].Mode != f.Shared {
			t.Error("task gate binding rejected or changed lock contract")
		}
		if stage == oc.CurrentAccess {
			currentDigest = digest
		} else if digest != currentDigest {
			t.Error("two stages changed binding")
		}
	}
	for _, pair := range [][2]e.StableName{{"work.milestone_changed", "work.milestone"}, {"work.sprint_changed", "work.sprint"}} {
		d := base
		d.Event.Header.EventType = pair[0]
		d.Event.Header.AggregateType = pair[1]
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal("old schema fixture invalid")
		}
		if _, _, err := workEventBinding(r); err != nil {
			t.Error("old schema gate regressed")
		}
	}
	for _, alter := range []func(*oc.ProjectRequestDetails){func(d *oc.ProjectRequestDetails) { d.Event.Producer = "other" }, func(d *oc.ProjectRequestDetails) { d.Event.Header.EventType = "work.task_deleted" }, func(d *oc.ProjectRequestDetails) { d.Event.Header.AggregateType = "work.sprint" }, func(d *oc.ProjectRequestDetails) { d.Event.Header.SchemaVersion = 2 }, func(d *oc.ProjectRequestDetails) { v := f.Sequence(1); d.Event.Header.AggregateSequence = &v }, func(d *oc.ProjectRequestDetails) {
		v := f.Sequence(1)
		d.Event.Header.AggregateSequence = &v
		d.Event.Header.AggregateVersion = nil
	}} {
		d := base
		alter(&d)
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal("negative gate request was not structurally valid")
		}
		if _, _, err := workEventBinding(r); err == nil {
			t.Error("gate broadened beyond exact three schema pairs")
		}
	}
	d := base
	d.Actor, _ = i.NewHuman(user, independentGateID[i.Session](t, 9))
	r, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal("renewed actor fixture invalid")
	}
	changed, _, err := workEventBinding(r)
	if err != nil || changed == currentDigest {
		t.Error("project gate did not bind full current actor")
	}
}
