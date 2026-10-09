package blocker_test

// Independent public-contract probes. Oracles come from the accepted service
// card and literal canonical-v1 projections, not the implementation's tests.
import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func id[K any](n int) f.ID[K] {
	v, err := f.ParseID[K](fmt.Sprintf("019a0000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func ptr[T any](v T) *T { return &v }
func instant(second int) f.Instant {
	v, err := f.NewInstant(time.Date(2026, 10, 9, 1, 2, second, 123456000, time.UTC))
	if err != nil {
		panic(err)
	}
	return v
}
func actor(user, session int) i.Actor {
	v, err := i.NewHuman(id[i.User](user), id[i.Session](session))
	if err != nil {
		panic(err)
	}
	return v
}
func record(resolved bool) c.TaskBlocker {
	v := c.TaskBlocker{ID: id[c.TaskBlockerIdentity](1), ProjectID: id[i.Project](2), TaskID: id[c.Task](3), Type: c.TaskBlockerRelyOn, Description: "independent <body> & 中文\n", Metadata: c.TaskBlockerMetadata{RelyOn: &c.TaskBlockerRelyOnMetadata{RelatedTaskID: id[c.Task](4)}}, CreatedAt: instant(3), CreatedBy: c.TaskEventActor{Type: i.Human, UserID: id[i.User](5), Source: "task_domain"}}
	if resolved {
		v.ResolvedAt = ptr(instant(4))
		v.ResolvedBy = ptr(c.TaskEventActor{Type: i.Human, UserID: id[i.User](6), Source: "task_domain"})
		v.ResolutionComment = ptr("resolved <evidence> & 中文\n")
	}
	return v
}
func mutation(resolved bool) c.TaskBlockerMutation {
	b := record(resolved)
	at := b.CreatedAt
	if resolved {
		at = *b.ResolvedAt
	}
	return c.TaskBlockerMutation{Task: c.Task{ID: b.TaskID, ProjectID: b.ProjectID, MilestoneID: id[c.Milestone](7), SprintID: id[pc.Sprint](8), Title: "independent task", Description: "task description", Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium, State: c.TaskStateBacklog, Plan: "task plan", ManualRank: strings.Repeat("8", 32), Version: 2, CreatedAt: instant(2), UpdatedAt: at}, Blocker: b, TaskEventID: id[c.TaskEvent](9), EventIDs: []event.EventID{id[event.EventIdentity](10)}}
}
func history(resolved bool) c.TaskBlockerEvent {
	m := mutation(resolved)
	v := c.TaskBlockerEvent{ID: m.TaskEventID, ProjectID: m.Task.ProjectID, TaskID: m.Task.ID, TaskVersion: m.Task.Version, Type: c.TaskBlockerEventAdded, Actor: m.Blocker.CreatedBy, OperationID: id[c.TaskBlockerCommand](11), CorrelationID: id[c.TaskBlockerCommand](11), CreatedAt: m.Task.UpdatedAt, Payload: c.TaskBlockerEventPayload{Added: &c.TaskBlockerAddedPayload{BlockerID: m.Blocker.ID, BlockerType: m.Blocker.Type}}}
	if resolved {
		v.Type = c.TaskBlockerEventResolved
		v.Actor = *m.Blocker.ResolvedBy
		v.Payload = c.TaskBlockerEventPayload{Resolved: &c.TaskBlockerResolvedPayload{BlockerID: m.Blocker.ID, BlockerType: m.Blocker.Type, ResolutionComment: m.Blocker.ResolutionComment}}
	}
	return v
}
func changed() c.TaskBlockersChanged {
	return c.TaskBlockersChanged{OperationID: id[c.TaskBlockerCommand](11), ActorUserID: id[i.User](5), TaskEventID: id[c.TaskEvent](9), BlockerID: id[c.TaskBlockerIdentity](1), Change: c.TaskBlockerAddedChange}
}
func header() event.Header {
	return event.Header{EventID: id[event.EventIdentity](10), EventType: c.TaskBlockersChangedName, SchemaVersion: 1, OccurredAt: instant(3), Scope: event.Scope{Kind: event.ProjectScope, ProjectID: id[event.Project](2)}, AggregateType: c.TaskAggregate, AggregateID: id[event.Aggregate](3), AggregateVersion: ptr(f.Version(2))}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func bad(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid value accepted")
	}
}
func code(t *testing.T, err error, want f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != want || fault.Code.Safe() != want || fault.CommitState != f.NotStarted {
		t.Fatalf("fault = %v, want %s / NotStarted", err, want)
	}
}

func TestIndependentBlockerRecordAndResolution(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		for _, waiting := range []bool{false, true} {
			v := record(resolved)
			if waiting {
				v.Type = c.TaskBlockerWaitingForHuman
				v.Metadata = c.TaskBlockerMetadata{WaitingForHuman: &c.TaskBlockerWaitingForHumanMetadata{}}
			}
			raw := mustJSON(t, v)
			out, err := c.DecodeTaskBlocker(raw)
			if err != nil || !reflect.DeepEqual(v, out) {
				t.Fatalf("round trip: %v", err)
			}
		}
	}
	for name, mut := range map[string]func(*c.TaskBlocker){
		"at_without_actor": func(v *c.TaskBlocker) { v.ResolvedBy = nil }, "actor_without_at": func(v *c.TaskBlocker) { v.ResolvedAt = nil }, "before_creation": func(v *c.TaskBlocker) { v.ResolvedAt = ptr(instant(2)) }, "nonhuman_creator": func(v *c.TaskBlocker) { v.CreatedBy.Type = i.AgentRun }, "nonhuman_resolver": func(v *c.TaskBlocker) { v.ResolvedBy.Type = i.Service }, "creator_source": func(v *c.TaskBlocker) { v.CreatedBy.Source = "client" }, "resolver_source": func(v *c.TaskBlocker) { v.ResolvedBy.Source = "client" }, "empty_comment": func(v *c.TaskBlocker) { v.ResolutionComment = ptr("") }, "blank_comment": func(v *c.TaskBlocker) { v.ResolutionComment = ptr(" \t\n\u2003") }, "long_comment": func(v *c.TaskBlocker) { v.ResolutionComment = ptr(strings.Repeat("a", 1025)) }, "long_description": func(v *c.TaskBlocker) { v.Description = strings.Repeat("a", 1025) }, "forbidden_cc": func(v *c.TaskBlocker) { v.Description = "a\x00" }, "both_metadata": func(v *c.TaskBlocker) { v.Metadata.WaitingForHuman = &c.TaskBlockerWaitingForHumanMetadata{} }, "wrong_metadata": func(v *c.TaskBlocker) { v.Metadata.RelyOn = nil }, "unknown_type": func(v *c.TaskBlocker) { v.Type = "future" },
	} {
		t.Run(name, func(t *testing.T) {
			v := record(true)
			mut(&v)
			bad(t, v.Validate())
			_, err := v.MarshalJSON()
			bad(t, err)
		})
	}
	v := record(false)
	v.ResolutionComment = ptr("premature")
	bad(t, v.Validate())
	v = record(true)
	v.ResolutionComment = nil
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.ResolvedAt = ptr(v.CreatedAt)
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []c.TaskBlockerType{c.TaskBlockerTechnical, c.TaskBlockerWaitingForMeetingApproval, c.TaskBlockerUserCancelledExecution} {
		v = record(false)
		v.Type = kind
		code(t, v.Validate(), f.DependencyUnbound)
	}
}

// Build invalid JSON without routing it through any production codec or a
// normalizer that could discard duplicate keys or token whitespace.
func object(raw []byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(err)
	}
	return m
}
func pack(m map[string]json.RawMessage) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for n, k := range keys {
		if n > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}
func edit(raw []byte, path []string, change func(map[string]json.RawMessage) []byte) []byte {
	m := object(raw)
	if len(path) == 0 {
		return change(m)
	}
	m[path[0]] = edit(m[path[0]], path[1:], change)
	return pack(m)
}
func paths(raw []byte, prefix []string) [][]string {
	out := [][]string{append([]string{}, prefix...)}
	for k, v := range object(raw) {
		if len(v) > 0 && v[0] == '{' {
			out = append(out, paths(v, append(append([]string{}, prefix...), k))...)
		}
	}
	return out
}

type codecProbe struct {
	name   string
	value  any
	fresh  func() json.Unmarshaler
	decode func([]byte) (any, error)
	cap    int
}

func probes() []codecProbe {
	m := mutation(true)
	return []codecProbe{
		{"record", record(true), func() json.Unmarshaler { return new(c.TaskBlocker) }, func(b []byte) (any, error) { return c.DecodeTaskBlocker(b) }, c.MaxTaskBlockerRecordBytes},
		{"resolve", c.TaskBlockerResolve{BlockerID: m.Blocker.ID, ResolutionComment: ptr("ok")}, func() json.Unmarshaler { return new(c.TaskBlockerResolve) }, func(b []byte) (any, error) { return c.DecodeTaskBlockerResolve(b) }, c.MaxTaskBlockerResolveBytes},
		{"mutation", m, func() json.Unmarshaler { return new(c.TaskBlockerMutation) }, func(b []byte) (any, error) { return c.DecodeTaskBlockerMutation(b) }, c.MaxTaskBlockerMutationBytes},
		{"lookup_request", c.TaskBlockerCommandLookupRequest{ProjectID: m.Task.ProjectID, Command: c.TaskBlockerCommandResolve, IdempotencyKey: "independent-key", SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}, func() json.Unmarshaler { return new(c.TaskBlockerCommandLookupRequest) }, func(b []byte) (any, error) { return c.DecodeTaskBlockerCommandLookupRequest(b) }, c.MaxTaskBlockerLookupBytes},
		{"lookup", c.TaskBlockerCommandLookup{Status: c.LookupCommitted, Receipt: &m}, func() json.Unmarshaler { return new(c.TaskBlockerCommandLookup) }, func(b []byte) (any, error) { return c.DecodeTaskBlockerCommandLookup(b) }, c.MaxTaskBlockerMutationBytes + 1024},
		{"history", history(true), func() json.Unmarshaler { return new(c.TaskBlockerEvent) }, func(b []byte) (any, error) { return c.DecodeTaskBlockerEvent(b) }, c.MaxTaskBlockerEventBytes},
		{"outbox_payload", changed(), func() json.Unmarshaler { return new(c.TaskBlockersChanged) }, func(b []byte) (any, error) { return c.DecodeTaskBlockersChanged(b) }, c.MaxTaskBlockersChangedBytes},
	}
}
func TestIndependentBlockerStrictNestedAndAtomic(t *testing.T) {
	for _, p := range probes() {
		t.Run(p.name, func(t *testing.T) {
			raw := mustJSON(t, p.value)
			check := func(label string, b []byte) {
				t.Helper()
				t.Run(label, func(t *testing.T) {
					dst := p.fresh()
					if err := dst.UnmarshalJSON(raw); err != nil {
						t.Fatal(err)
					}
					before := mustJSON(t, dst)
					err := dst.UnmarshalJSON(b)
					bad(t, err)
					if !bytes.Equal(before, mustJSON(t, dst)) {
						t.Fatal("failed decode changed receiver")
					}
					zero, err := p.decode(b)
					bad(t, err)
					if !reflect.ValueOf(zero).IsZero() {
						t.Fatal("Decode failure returned a partial object")
					}
				})
			}
			for _, path := range paths(raw, nil) {
				var target = raw
				for _, k := range path {
					target = object(target)[k]
				}
				fields := object(target)
				prefix := strings.Join(path, "/")
				check(prefix+"/unknown", edit(raw, path, func(m map[string]json.RawMessage) []byte { m["unknown"] = json.RawMessage(`true`); return pack(m) }))
				for k, v := range fields {
					key := k
					value := bytes.Clone(v)
					check(prefix+"/missing/"+k, edit(raw, path, func(m map[string]json.RawMessage) []byte { delete(m, key); return pack(m) }))
					check(prefix+"/case/"+k, edit(raw, path, func(m map[string]json.RawMessage) []byte {
						delete(m, key)
						m[strings.ToUpper(key)] = value
						return pack(m)
					}))
					check(prefix+"/duplicate/"+k, edit(raw, path, func(m map[string]json.RawMessage) []byte {
						b := pack(m)
						escaped := fmt.Sprintf(`"\u%04x%s":`, rune(key[0]), key[1:])
						return append(append(append(b[:len(b)-1], ','), []byte(escaped)...), append(value, '}')...)
					}))
					if k != "resolved_at" && k != "resolved_by" && k != "resolution_comment" && k != "assignee_agent_id" {
						check(prefix+"/null/"+k, edit(raw, path, func(m map[string]json.RawMessage) []byte { m[key] = json.RawMessage(`null`); return pack(m) }))
					}
					check(prefix+"/scalar/"+k, edit(raw, path, func(m map[string]json.RawMessage) []byte { m[key] = json.RawMessage(`false`); return pack(m) }))
				}
			}
			for label, b := range map[string][]byte{"null": []byte(`null`), "array": []byte(`[]`), "tail": append(bytes.Clone(raw), []byte(` {}`)...), "bad_utf8": append([]byte{0xff}, raw...), "surrogate": bytes.Replace(raw, []byte(`"id":`), []byte(`"\ud800":`), 1)} {
				if bytes.Equal(b, raw) {
					continue
				}
				check(label, b)
			}
			limit := append(bytes.Repeat([]byte(" "), p.cap-len(raw)), raw...)
			if _, err := p.decode(limit); err != nil {
				t.Fatalf("at raw cap rejected: %v", err)
			}
			check("raw_cap_plus_one", append([]byte(" "), limit...))
			typedNil := reflect.Zero(reflect.TypeOf(p.fresh())).Interface().(json.Unmarshaler)
			bad(t, typedNil.UnmarshalJSON(raw))
		})
	}
}

func TestIndependentBlockerNestedCapsAndMaximumText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		root   any
		path   []string
		cap    int
		decode func([]byte) (any, error)
	}{
		{"record_metadata", record(true), []string{"metadata"}, 1024, func(b []byte) (any, error) { return c.DecodeTaskBlocker(b) }},
		{"mutation_blocker", mutation(true), []string{"blocker"}, c.MaxTaskBlockerRecordBytes, func(b []byte) (any, error) { return c.DecodeTaskBlockerMutation(b) }},
		{"lookup_receipt", c.TaskBlockerCommandLookup{Status: c.LookupCommitted, Receipt: ptr(mutation(true))}, []string{"receipt"}, c.MaxTaskBlockerMutationBytes, func(b []byte) (any, error) { return c.DecodeTaskBlockerCommandLookup(b) }},
		{"history_payload", history(true), []string{"payload"}, 8192, func(b []byte) (any, error) { return c.DecodeTaskBlockerEvent(b) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := mustJSON(t, tc.root)
			for _, extra := range []int{0, 1} {
				wire := edit(raw, tc.path, func(m map[string]json.RawMessage) []byte {
					v := pack(m)
					return append(append([]byte{'{'}, bytes.Repeat([]byte(" "), tc.cap+extra-len(v))...), v[1:]...)
				})
				_, err := tc.decode(wire)
				if extra == 0 && err != nil {
					t.Fatalf("nested at-cap rejected: %v", err)
				}
				if extra == 1 {
					bad(t, err)
				}
			}
		})
	}
	m := mutation(true)
	m.Blocker.Description = strings.Repeat("&", 1024)
	m.Blocker.ResolutionComment = ptr(strings.Repeat("<", 1024))
	m.Task.Title = strings.Repeat("&", 256)
	m.Task.Description = strings.Repeat("<", 32768)
	m.Task.Plan = strings.Repeat(">", 32768)
	raw := mustJSON(t, m)
	got, err := c.DecodeTaskBlockerMutation(raw)
	if err != nil || !reflect.DeepEqual(m, got) {
		t.Fatalf("maximum roundtrip: %v", err)
	}
	t.Logf("maximum escaped mutation bytes=%d, blocker bytes=%d", len(raw), len(mustJSON(t, m.Blocker)))
	for _, text := range []string{`\ud800`, `\udfff`} {
		raw := mustJSON(t, record(false))
		raw = edit(raw, nil, func(m map[string]json.RawMessage) []byte {
			m["description"] = json.RawMessage(`"` + text + `"`)
			return pack(m)
		})
		_, err := c.DecodeTaskBlocker(raw)
		bad(t, err)
	}
	for _, text := range []string{strings.Repeat("中", 341) + "x", strings.Repeat("x", 1024)} {
		v := record(false)
		v.Description = text
		raw := mustJSON(t, v)
		if _, err := c.DecodeTaskBlocker(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndependentBlockerReceiptAndClone(t *testing.T) {
	for name, mut := range map[string]func(*c.TaskBlockerMutation){"project": func(v *c.TaskBlockerMutation) { v.Blocker.ProjectID = id[i.Project](22) }, "task": func(v *c.TaskBlockerMutation) { v.Blocker.TaskID = id[c.Task](23) }, "time": func(v *c.TaskBlockerMutation) { v.Task.UpdatedAt = instant(5) }, "version_one": func(v *c.TaskBlockerMutation) { v.Task.Version = 1 }, "zero_history": func(v *c.TaskBlockerMutation) { v.TaskEventID = c.TaskEventID{} }, "nil_events": func(v *c.TaskBlockerMutation) { v.EventIDs = nil }, "empty_events": func(v *c.TaskBlockerMutation) { v.EventIDs = []event.EventID{} }, "two_events": func(v *c.TaskBlockerMutation) { v.EventIDs = append(v.EventIDs, id[event.EventIdentity](23)) }} {
		t.Run(name, func(t *testing.T) { v := mutation(true); mut(&v); bad(t, v.Validate()) })
	}
	for _, status := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved, "bad"} {
		for _, receipt := range []*c.TaskBlockerMutation{nil, ptr(mutation(true))} {
			v := c.TaskBlockerCommandLookup{Status: status, Receipt: receipt}
			valid := status == c.LookupCommitted && receipt != nil || (status == c.LookupInProgress || status == c.LookupNotObserved) && receipt == nil
			if (v.Validate() == nil) != valid {
				t.Fatal("lookup correlation")
			}
		}
	}
	m := mutation(true)
	m.Task.AssigneeAgentID = ptr(id[i.Agent](17))
	before := mustJSON(t, m)
	clone := m.Clone()
	clone.Blocker.Metadata.RelyOn.RelatedTaskID = id[c.Task](40)
	*clone.Blocker.ResolvedAt = instant(6)
	clone.Blocker.ResolvedBy.UserID = id[i.User](41)
	*clone.Blocker.ResolutionComment = "changed"
	*clone.Task.AssigneeAgentID = id[i.Agent](42)
	clone.EventIDs[0] = id[event.EventIdentity](43)
	if !bytes.Equal(before, mustJSON(t, m)) {
		t.Fatal("mutation clone shares mutable storage")
	}
	q := c.TaskBlockerCommandLookup{Status: c.LookupCommitted, Receipt: &m}
	qc := q.Clone()
	qc.Receipt.Blocker.Metadata.RelyOn.RelatedTaskID = id[c.Task](44)
	if !bytes.Equal(before, mustJSON(t, m)) {
		t.Fatal("lookup clone shares nested metadata")
	}
	h := history(true)
	before = mustJSON(t, h)
	hc := h.Clone()
	*hc.Payload.Resolved.ResolutionComment = "changed"
	hc.Payload.Resolved.BlockerID = id[c.TaskBlockerIdentity](45)
	if !bytes.Equal(before, mustJSON(t, h)) {
		t.Fatal("history clone shares payload")
	}
	r := c.TaskBlockerResolve{BlockerID: id[c.TaskBlockerIdentity](1), ResolutionComment: ptr("evidence")}
	rc := r.Clone()
	*rc.ResolutionComment = "changed"
	if *r.ResolutionComment != "evidence" {
		t.Fatal("resolve clone shares comment")
	}
}

// The literal is manually ordered by canonical-v1 keys; no production digest,
// marshaler or canonicalizer participates in constructing the oracle.
func golden(command, request string, expected string, user int) f.Digest {
	raw := fmt.Sprintf(`{"actor_user_id":"019a0000-0000-7000-8000-%012x","command":"%s","expected_version":"%s","format":"work-task-blocker-v1","project_id":"019a0000-0000-7000-8000-000000000002","request":%s,"task_id":"019a0000-0000-7000-8000-000000000003"}`, user, command, expected, request)
	sum := sha256.Sum256([]byte(raw))
	return f.Digest(fmt.Sprintf("sha256:%x", sum))
}
func TestIndependentBlockerDigestGoldenAndIdentity(t *testing.T) {
	m := f.CommandMeta{RequestID: id[f.Request](12), IdempotencyKey: "key-A", ExpectedVersion: ptr(f.Version(7))}
	a := actor(5, 13)
	p := id[i.Project](2)
	task := id[c.Task](3)
	r := c.TaskBlockerCreate{BlockerID: id[c.TaskBlockerIdentity](1), Type: c.TaskBlockerRelyOn, Description: " &中\n", Metadata: c.TaskBlockerMetadata{RelyOn: &c.TaskBlockerRelyOnMetadata{RelatedTaskID: id[c.Task](4)}}}
	request := `{"blocker_id":"019a0000-0000-7000-8000-000000000001","description":" \u0026中\n","metadata":{"related_task_id":"019a0000-0000-7000-8000-000000000004"},"type":"rely_on"}`
	want := golden("work.task.blocker.add", request, "7", 5)
	got, err := c.TaskBlockerAddDigest(a, m, p, task, r)
	if err != nil || got != want {
		t.Fatalf("add independent golden: got %s want %s err %v", got, want, err)
	}
	resolve := c.TaskBlockerResolve{BlockerID: r.BlockerID}
	wantResolve := golden("work.task.blocker.resolve", `{"blocker_id":"019a0000-0000-7000-8000-000000000001","resolution_comment":null}`, "7", 5)
	dg, err := c.TaskBlockerResolveDigest(a, m, p, task, resolve)
	if err != nil || dg != wantResolve {
		t.Fatalf("resolve independent golden: %v", err)
	}
	resolve.ResolutionComment = ptr(" <ok>\n")
	wantResolve = golden("work.task.blocker.resolve", `{"blocker_id":"019a0000-0000-7000-8000-000000000001","resolution_comment":" \u003cok\u003e\n"}`, "7", 5)
	dg, err = c.TaskBlockerResolveDigest(a, m, p, task, resolve)
	if err != nil || dg != wantResolve {
		t.Fatalf("resolve comment golden: %v", err)
	}
	for _, mut := range []func(*f.CommandMeta){func(m *f.CommandMeta) { m.RequestID = id[f.Request](99) }, func(m *f.CommandMeta) { m.IdempotencyKey = "key-B" }} {
		n := m
		mut(&n)
		v, err := c.TaskBlockerAddDigest(actor(5, 99), n, p, task, r)
		if err != nil || v != got {
			t.Fatal("transport/session changed semantic")
		}
	}
	for name, run := range map[string]func() (f.Digest, error){"user": func() (f.Digest, error) { return c.TaskBlockerAddDigest(actor(6, 13), m, p, task, r) }, "version": func() (f.Digest, error) {
		n := m
		n.ExpectedVersion = ptr(f.Version(8))
		return c.TaskBlockerAddDigest(a, n, p, task, r)
	}, "project": func() (f.Digest, error) { return c.TaskBlockerAddDigest(a, m, id[i.Project](22), task, r) }, "task": func() (f.Digest, error) { return c.TaskBlockerAddDigest(a, m, p, id[c.Task](33), r) }, "description": func() (f.Digest, error) {
		n := r.Clone()
		n.Description = strings.TrimSpace(n.Description)
		return c.TaskBlockerAddDigest(a, m, p, task, n)
	}, "related": func() (f.Digest, error) {
		n := r.Clone()
		n.Metadata.RelyOn.RelatedTaskID = id[c.Task](44)
		return c.TaskBlockerAddDigest(a, m, p, task, n)
	}} {
		t.Run(name, func(t *testing.T) {
			v, err := run()
			if err != nil || v == got {
				t.Fatal("semantic field omitted")
			}
		})
	}
	for _, mut := range []func(*f.CommandMeta){func(m *f.CommandMeta) { m.ExpectedVersion = nil }, func(m *f.CommandMeta) { m.ExpectedVersion = ptr(f.Version(0)) }, func(m *f.CommandMeta) { m.RequestID = f.ID[f.Request]{} }, func(m *f.CommandMeta) { m.IdempotencyKey = "" }} {
		n := m
		mut(&n)
		_, err := c.TaskBlockerAddDigest(a, n, p, task, r)
		code(t, err, f.InvalidArgument)
	}
	agent, err := i.NewAgentRun(p, id[i.Agent](14), id[i.Execution](15))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := i.RegisterService(i.OutboxDelivery)
	if err != nil {
		t.Fatal(err)
	}
	service, err := reg.Actor(id[f.Request](16).String(), i.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		a    i.Actor
		want f.Code
	}{{i.Actor{}, f.Unauthenticated}, {agent, f.DependencyUnbound}, {service, f.Forbidden}} {
		_, err := c.TaskBlockerAddDigest(tc.a, m, p, task, r)
		code(t, err, tc.want)
		_, err = c.TaskBlockerResolveDigest(tc.a, m, p, task, resolve)
		code(t, err, tc.want)
	}
	addIdentity, err := c.TaskBlockerCommandIdentity(p, c.TaskBlockerCommandAdd, m.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	resolveIdentity, err := c.TaskBlockerCommandIdentity(p, c.TaskBlockerCommandResolve, m.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if addIdentity.Namespace() != "project" || !reflect.DeepEqual(addIdentity.OwnerIDs(), []string{p.String()}) || addIdentity.Command() != "work.task.blocker.add" || addIdentity.Canonical() == resolveIdentity.Canonical() {
		t.Fatal("identity separation")
	}
	for _, cmd := range []c.TaskBlockerCommandName{"work.task.create", "work.task.transfer", ""} {
		_, err := c.TaskBlockerCommandIdentity(p, cmd, m.IdempotencyKey)
		bad(t, err)
	}
}

func TestIndependentBlockerEventFactoryAndIsolation(t *testing.T) {
	cat := event.NewCatalog()
	factory, err := c.RegisterTaskBlockerEvents(cat)
	if err != nil || !factory.Valid() {
		t.Fatalf("register: %v", err)
	}
	_, err = c.RegisterTaskBlockerEvents(cat)
	bad(t, err)
	_, err = c.RegisterTaskBlockerEvents(nil)
	bad(t, err)
	sealed := event.NewCatalog()
	if err := sealed.Seal(); err != nil {
		t.Fatal(err)
	}
	_, err = c.RegisterTaskBlockerEvents(sealed)
	bad(t, err)
	e, err := factory.NewTaskBlockersChanged(header(), changed())
	if err != nil {
		t.Fatal(err)
	}
	v, err := factory.DecodeTaskBlockersChanged(e)
	if err != nil || v != changed() {
		t.Fatal("factory roundtrip")
	}
	restored, err := factory.Restore(header(), e.PayloadBytes())
	if err != nil || restored.Header().EventID != e.Header().EventID {
		t.Fatal("restore")
	}
	foreign, err := c.RegisterTaskBlockerEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	_, err = foreign.DecodeTaskBlockersChanged(e)
	bad(t, err)
	var zero c.TaskBlockerEvents
	_, err = zero.NewTaskBlockersChanged(header(), changed())
	bad(t, err)
	for name, mut := range map[string]func(*event.Header){"type": func(h *event.Header) { h.EventType = "work.task_changed" }, "aggregate": func(h *event.Header) { h.AggregateType = "work.sprint" }, "schema": func(h *event.Header) { h.SchemaVersion = 2 }, "system": func(h *event.Header) { h.Scope = event.Scope{Kind: event.SystemScope} }, "version_one": func(h *event.Header) { h.AggregateVersion = ptr(f.Version(1)) }, "no_version": func(h *event.Header) { h.AggregateVersion = nil }, "sequence": func(h *event.Header) { h.AggregateSequence = ptr(f.Sequence(1)) }} {
		t.Run(name, func(t *testing.T) {
			h := header()
			mut(&h)
			_, err := factory.NewTaskBlockersChanged(h, changed())
			bad(t, err)
			_, err = factory.Restore(h, mustJSON(t, changed()))
			bad(t, err)
		})
	}
	oldFactory, err := c.RegisterTaskEvents(cat)
	if err != nil {
		t.Fatal(err)
	}
	_, err = oldFactory.DecodeTaskChanged(e)
	bad(t, err)
	for _, resolved := range []bool{false, true} {
		h := history(resolved)
		raw := mustJSON(t, h)
		var old c.TaskEvent
		bad(t, old.UnmarshalJSON(raw))
		out, err := c.DecodeTaskBlockerEvent(raw)
		if err != nil || !reflect.DeepEqual(h, out) {
			t.Fatalf("history roundtrip: %v", err)
		}
		h.CorrelationID = id[c.TaskBlockerCommand](88)
		bad(t, h.Validate())
		h = history(resolved)
		h.Type = "task_created"
		bad(t, h.Validate())
		h = history(resolved)
		h.TaskVersion = 1
		bad(t, h.Validate())
	}
	// Identical UUID JSON may also be transition-shaped; provenance is deliberately
	// not a pure-decoder claim. The resolved comment restriction remains distinct.
	h := history(false)
	if _, err := c.DecodeTaskTransitionEvent(mustJSON(t, h)); err != nil {
		t.Fatal("same-shape history unexpectedly claims provenance")
	}
	limit := append(bytes.Repeat([]byte(" "), c.MaxTaskBlockersChangedBytes-len(e.PayloadBytes())), e.PayloadBytes()...)
	_, err = factory.Restore(header(), limit)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.Restore(header(), append([]byte(" "), limit...))
	bad(t, err)
	payload := e.PayloadBytes()
	payload[0] = '['
	if bytes.Equal(payload, e.PayloadBytes()) {
		t.Fatal("event exposes mutable payload bytes")
	}
	hcopy := e.Header()
	*hcopy.AggregateVersion = 99
	if *e.Header().AggregateVersion != 2 {
		t.Fatal("event exposes mutable header")
	}
}

func TestIndependentBlockerSafeLogAndConcurrentReads(t *testing.T) {
	items := []any{record(true), c.TaskBlockerResolve{BlockerID: id[c.TaskBlockerIdentity](1), ResolutionComment: ptr("secret")}, mutation(true), history(true), history(true).Payload, changed(), c.TaskBlockersAll, c.TaskBlockerCommandAdd, c.TaskBlockerEventAdded, c.TaskBlockerAddedChange, c.TaskBlockerEvents{}, c.TaskBlockerCommandLookup{Status: c.LookupCommitted, Receipt: ptr(mutation(true))}, c.TaskBlockerCommandLookupRequest{}}
	for _, v := range items {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if got := fmt.Sprintf(verb, v); got != "work_task_blocker" {
				t.Fatalf("unsafe format %T: %s", v, got)
			}
		}
		lv, ok := v.(slog.LogValuer)
		if !ok || lv.LogValue().String() != "work_task_blocker" {
			t.Fatalf("unsafe slog %T", v)
		}
	}
	m := mutation(true)
	raw := mustJSON(t, m)
	r := c.TaskBlockerResolve{BlockerID: m.Blocker.ID, ResolutionComment: ptr("evidence")}
	meta := f.CommandMeta{RequestID: id[f.Request](12), IdempotencyKey: "concurrent", ExpectedVersion: ptr(f.Version(2))}
	a := actor(5, 13)
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 40; round++ {
				if err := m.Validate(); err != nil {
					t.Error(err)
				}
				clone := m.Clone()
				clone.Blocker.Metadata.RelyOn.RelatedTaskID = id[c.Task](88)
				if _, err := c.DecodeTaskBlockerMutation(raw); err != nil {
					t.Error(err)
				}
				if _, err := m.MarshalJSON(); err != nil {
					t.Error(err)
				}
				if _, err := c.TaskBlockerResolveDigest(a, meta, m.Task.ProjectID, m.Task.ID, r); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
