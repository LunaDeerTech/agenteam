package work

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func pureBlockerRecord(t *testing.T, resolved bool) (*blockerRecord, i.Actor, event.Summary) {
	t.Helper()
	old, a, _ := pureTaskRecord(t)
	before := old.Plan.After.Task.Clone()
	at, _ := f.NewInstant(before.UpdatedAt.Time().Add(time.Second))
	id := pureID[c.TaskBlockerIdentity](t, 60)
	command := pureID[c.TaskBlockerCommand](t, 61)
	history := pureID[c.TaskEvent](t, 62)
	ev := pureID[event.EventIdentity](t, 63)
	input := blockerInput{Command: c.TaskBlockerCommandAdd, Project: before.ProjectID, Target: before.ID, User: old.User, Expected: before.Version, Add: &c.TaskBlockerCreate{BlockerID: id, Type: c.TaskBlockerRelyOn, Description: "private blocker text", Metadata: c.TaskBlockerMetadata{RelyOn: &c.TaskBlockerRelyOnMetadata{RelatedTaskID: pureID[c.Task](t, 70)}}}}
	actor := c.TaskEventActor{Type: i.Human, UserID: old.User, Source: "task_domain"}
	b := c.TaskBlocker{ID: id, ProjectID: before.ProjectID, TaskID: before.ID, Type: input.Add.Type, Description: input.Add.Description, Metadata: input.Add.Metadata.Clone(), CreatedAt: at, CreatedBy: actor}
	after := before.Clone()
	after.Version++
	after.UpdatedAt = at
	h := c.TaskBlockerEvent{ID: history, ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: after.Version, Type: c.TaskBlockerEventAdded, Actor: actor, OperationID: command, CorrelationID: command, Payload: c.TaskBlockerEventPayload{Added: &c.TaskBlockerAddedPayload{BlockerID: id, BlockerType: b.Type}}, CreatedAt: at}
	var bb *c.TaskBlocker
	change := c.TaskBlockerAddedChange
	if resolved {
		b.CreatedAt = before.CreatedAt
		v := b.Clone()
		bb = &v
		comment := "private resolution text"
		b.ResolvedAt = &at
		b.ResolvedBy = &actor
		b.ResolutionComment = &comment
		input.Command = c.TaskBlockerCommandResolve
		input.Add = nil
		input.Resolve = &c.TaskBlockerResolve{BlockerID: id, ResolutionComment: &comment}
		change = c.TaskBlockerResolvedChange
		h.Type = c.TaskBlockerEventResolved
		h.Payload = c.TaskBlockerEventPayload{Resolved: &c.TaskBlockerResolvedPayload{BlockerID: id, BlockerType: b.Type, ResolutionComment: &comment}}
	}
	header, e := blockerHeader(ev, after, at)
	if e != nil {
		t.Fatal(e)
	}
	p := c.TaskBlockersChanged{OperationID: command, ActorUserID: old.User, TaskEventID: history, BlockerID: id, Change: change}
	factory, e := c.RegisterTaskBlockerEvents(event.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	typed, e := factory.NewTaskBlockersChanged(header, p)
	if e != nil {
		t.Fatal(e)
	}
	d, e := input.semantic(a, "blocker-key")
	if e != nil {
		t.Fatal(e)
	}
	r := &blockerRecord{ID: command, Project: input.Project, User: old.User, Command: input.Command, Key: "blocker-key", Semantic: d, Input: input, Revision: 1, State: "planned", TaskEventID: history, EventID: ev, Created: at, Plan: &blockerPlan{Before: before, BlockerBefore: bb, After: c.TaskBlockerMutation{Task: after, Blocker: b, TaskEventID: history, EventIDs: []event.EventID{ev}}, Placement: old.Plan.Placement, QueryGeneration: 2, TaskEvent: h, Header: header, Payload: p}}
	if e = validateBlockerRecord(r, a); e != nil {
		t.Fatal("valid blocker plan", e)
	}
	return r, a, typed.Summary()
}
func TestTaskBlockerPlanBindsImmutableProposalAndProducer(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		r, a, summary := pureBlockerRecord(t, resolved)
		binding, locks, opaque, e := blockerEventBinding(r, a, summary)
		if e != nil {
			t.Fatal(e)
		}
		issuer := oc.NewPlanIssuer()
		deps, e := oc.NewDependencies(issuer, binding, locks, opaque)
		if e != nil || !deps.Matches(issuer, binding) || deps.Matches(oc.NewPlanIssuer(), binding) {
			t.Fatal("issuer")
		}
		other, _, _, e := blockerEventBinding(r, pureActor(t, 3), summary)
		if e != nil || other == binding {
			t.Fatal("Session omitted")
		}
		r.Revision++
		other, _, _, e = blockerEventBinding(r, a, summary)
		if e != nil || other == binding {
			t.Fatal("revision omitted")
		}
		if len(locks) != 6 {
			t.Fatal("incomplete domain lock union", len(locks))
		}
		for _, lock := range locks {
			if strings.HasPrefix(lock.Key.Canonical(), "rank-group/") {
				t.Fatal("unnecessary rank lock")
			}
		}
	}
	for name, mutate := range map[string]func(*blockerRecord){"task-title": func(r *blockerRecord) { r.Plan.After.Task.Title = "forged" }, "task-rank": func(r *blockerRecord) { r.Plan.After.Task.ManualRank = "00000000000000000000000000000001" }, "before-version": func(r *blockerRecord) { r.Plan.Before.Version++ }, "body": func(r *blockerRecord) { r.Plan.After.Blocker.Description = "forged" }, "creator": func(r *blockerRecord) { r.Plan.After.Blocker.CreatedBy.UserID = pureID[i.User](t, 99) }, "operation": func(r *blockerRecord) { r.Plan.TaskEvent.OperationID = pureID[c.TaskBlockerCommand](t, 99) }, "event": func(r *blockerRecord) { r.Plan.After.EventIDs[0] = pureID[event.EventIdentity](t, 99) }, "payload": func(r *blockerRecord) { r.Plan.Payload.BlockerID = pureID[c.TaskBlockerIdentity](t, 99) }, "generation": func(r *blockerRecord) { r.Plan.QueryGeneration = math.MaxInt64 }, "placement": func(r *blockerRecord) { r.Plan.Placement.State = c.Completed }, "type": func(r *blockerRecord) { r.Plan.TaskEvent.Type = c.TaskBlockerEventResolved }, "time": func(r *blockerRecord) { r.Created, _ = f.NewInstant(r.Created.Time().Add(time.Hour)) }} {
		t.Run(name, func(t *testing.T) {
			r, a, summary := pureBlockerRecord(t, false)
			mutate(r)
			if validateBlockerRecord(r, a) == nil {
				t.Fatal("forged plan")
			}
			if _, _, _, e := blockerEventBinding(r, a, summary); e == nil {
				t.Fatal("forged producer")
			}
		})
	}
	r, a, _ := pureBlockerRecord(t, true)
	r.State = "completed"
	out := r.Plan.After.Clone()
	r.Receipt = &out
	at := r.Created
	r.Committed = &at
	if e := validateBlockerRecord(r, a); e != nil {
		t.Fatal("completed immutable receipt", e)
	}
}
func TestTaskBlockerPrivatePlanNestedClosure(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		r, _, _ := pureBlockerRecord(t, resolved)
		raw, e := canonical(r.Plan)
		if e != nil {
			t.Fatal(e)
		}
		var valid blockerPlan
		if e = valid.UnmarshalJSON(raw); e != nil {
			t.Fatal(e)
		}
		mutations := 0
		var walk func([]byte, func([]byte))
		walk = func(raw []byte, check func([]byte)) {
			var m map[string]json.RawMessage
			if json.Unmarshal(raw, &m) != nil || m == nil {
				return
			}
			for key, value := range m {
				copy := map[string]json.RawMessage{}
				for k, v := range m {
					copy[k] = v
				}
				delete(copy, key)
				copy[strings.ToUpper(key)] = value
				bad, _ := json.Marshal(copy)
				check(bad)
				mutations++
				key := key
				walk(value, func(nested []byte) {
					next := map[string]json.RawMessage{}
					for k, v := range m {
						next[k] = v
					}
					next[key] = nested
					bad, _ := json.Marshal(next)
					check(bad)
				})
			}
		}
		walk(raw, func(bad []byte) {
			var next blockerPlan
			if next.UnmarshalJSON(bad) == nil {
				t.Fatal("nested alias accepted")
			}
		})
		if mutations < 60 {
			t.Fatal("nested matrix incomplete", mutations)
		}
		input, e := canonical(r.Input)
		if e != nil {
			t.Fatal(e)
		}
		var in blockerInput
		if e = in.UnmarshalJSON(input); e != nil {
			t.Fatal(e)
		}
		if in.UnmarshalJSON(append(input, []byte(` {}`)...)) == nil {
			t.Fatal("trailing private input")
		}
	}
}
