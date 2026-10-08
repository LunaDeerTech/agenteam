package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func testID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func testActor(t *testing.T, user, session int) i.Actor {
	t.Helper()
	a, err := i.NewHuman(testID[i.User](t, user), testID[i.Session](t, session))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func testMeta(t *testing.T, version *f.Version) f.CommandMeta {
	return f.CommandMeta{RequestID: testID[f.Request](t, 3), IdempotencyKey: "work-key", ExpectedVersion: version}
}
func testMilestone(t *testing.T) Milestone {
	at, err := f.NewInstant(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return Milestone{ID: testID[Milestone](t, 4), ProjectID: testID[i.Project](t, 5), Title: " original 文本 ", Description: "body\t\n\r", ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: at, UpdatedAt: at}
}
func requireCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var v *f.Fault
	if !errors.As(err, &v) || v.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestWorkStructureTextAndScalarBoundaries(t *testing.T) {
	for _, v := range []string{"x", strings.Repeat("界", 256), " x ", strings.Repeat("😀", 256)} {
		if err := ValidateTitle(v); err != nil {
			t.Fatalf("valid title %d bytes: %v", len(v), err)
		}
	}
	for n, v := range []string{"", " \t", strings.Repeat("x", 257), strings.Repeat("😀", 257), "a\x00b", "a\x7fb", "a\u0085b", string([]byte{0xff})} {
		t.Run(fmt.Sprint(n), func(t *testing.T) { requireCode(t, ValidateTitle(v), f.InvalidArgument) })
	}
	for _, v := range []string{"", strings.Repeat("x", 32768), "a\n\r\tb"} {
		if ValidateDescription(v) != nil {
			t.Fatal("valid description rejected")
		}
	}
	for _, v := range []string{strings.Repeat("x", 32769), "a\u009fb", "a\x0bb", string([]byte{0xfe})} {
		requireCode(t, ValidateDescription(v), f.InvalidArgument)
	}
	for _, v := range []string{strings.Repeat("0", 32), strings.Repeat("f", 32), "7F" + strings.Repeat("0", 30), "1", ""} {
		requireCode(t, ValidateRank(v), f.InvalidArgument)
	}
	if ValidateRank("00000000000000000000000000000001") != nil {
		t.Fatal("rank lower neighbor rejected")
	}
	m := testMilestone(t)
	m.Version = 0
	if _, err := json.Marshal(m); err == nil {
		t.Fatal("zero version encoded")
	}
	m = testMilestone(t)
	m.CreatedAt, m.UpdatedAt = m.UpdatedAt, m.CreatedAt
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"version":"1"`), []byte(`"version":1`), 1)
	if json.Unmarshal(raw, &m) == nil {
		t.Fatal("numeric version decoded")
	}
}
func TestWorkStructureStrictJSONPresenceAndAtomicDecode(t *testing.T) {
	id := testID[Milestone](t, 4).String()
	base := `{"milestone_id":"` + id + `","title":"x"}`
	var good CreateMilestoneRequest
	if err := json.Unmarshal([]byte(base), &good); err != nil || good.Description != "" {
		t.Fatal("omitted description default", err)
	}
	bad := []string{`{"milestone_id":"` + id + `","title":"x","title":"y"}`, `{"milestone_id":"` + id + `","Title":"x"}`, `{"milestone_id":"` + id + `","title":"x","other":1}`, `{"milestone_id":"` + id + `","title":null}`, `{"milestone_id":"` + id + `","title":"x","description":null}`, base + ` {}`, `{"milestone_id":"` + id + `","title":"\ud800"}`, `{"milestone_id":"` + id + `","title":"\udc00"}`, `{"milestone_id":"` + id + `","title":"\ud800\u0041"}`, strings.Replace(base, "-7000-", "-4000-", 1), `[]`, `null`, "{" + strings.Repeat(" ", MaxRequestBytes) + base[1:]}
	for n, raw := range bad {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			v := good
			if json.Unmarshal([]byte(raw), &v) == nil {
				t.Fatal("invalid request accepted")
			}
			if v != good {
				t.Fatal("failed decoder changed receiver")
			}
		})
	}
	// Method-level raw input includes outer whitespace; encoding/json strips it.
	atLimit := []byte(strings.Repeat(" ", MaxRequestBytes-len(base)) + base)
	var direct CreateMilestoneRequest
	if err := direct.UnmarshalJSON(atLimit); err != nil || direct != good {
		t.Fatal("complete raw request at limit rejected", err)
	}
	overLimit := append([]byte(" "), atLimit...)
	if direct.UnmarshalJSON(overLimit) == nil || direct != good {
		t.Fatal("complete raw request over limit accepted or receiver changed")
	}
	var trimmed CreateMilestoneRequest
	if err := json.Unmarshal(overLimit, &trimmed); err != nil || trimmed != good {
		t.Fatal("standard decoder outer-whitespace behavior changed", err)
	}
	if err := json.Unmarshal([]byte(`{"milestone_id":"`+id+`","title":"\ud83d\ude00"}`), new(CreateMilestoneRequest)); err != nil {
		t.Fatal("paired surrogate rejected", err)
	}
	invalidUTF8 := append([]byte(`{"milestone_id":"`+id+`","title":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	if json.Unmarshal(invalidUTF8, new(CreateMilestoneRequest)) == nil {
		t.Fatal("invalid UTF8 accepted")
	}
	for _, raw := range []string{`{}`, `{"title":null}`, `{"description":null}`, `{"TITLE":"x"}`, `{"title":"x","title":"x"}`} {
		if json.Unmarshal([]byte(raw), new(UpdateFields)) == nil {
			t.Fatal("invalid patch accepted")
		}
	}
	var patch UpdateFields
	if err := json.Unmarshal([]byte(`{"description":""}`), &patch); err != nil || patch.Description == nil || *patch.Description != "" || patch.Title != nil {
		t.Fatal("clear presence lost", err)
	}
	if json.Unmarshal([]byte(`{"before_id":null}`), new(ReorderMilestoneRequest)) == nil {
		t.Fatal("explicit null anchor accepted")
	}
	if json.Unmarshal([]byte(`{}`), new(ReorderMilestoneRequest)) != nil {
		t.Fatal("tail omission rejected")
	}
}
func TestWorkStructureSixDigestsAndStableHumanIdentity(t *testing.T) {
	a := testActor(t, 1, 2)
	b := testActor(t, 1, 9)
	project := testID[i.Project](t, 5)
	mid := testID[Milestone](t, 4)
	sid := testID[pc.Sprint](t, 6)
	version := f.Version(7)
	title := "new title"
	anchorM := testID[Milestone](t, 8)
	anchorS := testID[pc.Sprint](t, 8)
	tests := []struct {
		name   string
		create bool
		run    func(i.Actor, f.CommandMeta, ProjectID) (f.Digest, error)
	}{
		{"milestone-create", true, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return CreateMilestoneDigest(a, m, p, CreateMilestoneRequest{MilestoneID: mid, Title: title})
		}},
		{"milestone-update", false, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return UpdateMilestoneDigest(a, m, p, mid, UpdateFields{Title: &title})
		}},
		{"milestone-reorder", false, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return ReorderMilestoneDigest(a, m, p, mid, ReorderMilestoneRequest{BeforeID: &anchorM})
		}},
		{"sprint-create", true, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return CreateSprintDigest(a, m, p, CreateSprintRequest{SprintID: sid, MilestoneID: mid, Title: title})
		}},
		{"sprint-update", false, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return UpdateSprintDigest(a, m, p, sid, UpdateFields{Title: &title})
		}},
		{"sprint-reorder", false, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
			return ReorderSprintDigest(a, m, p, sid, ReorderSprintRequest{MilestoneID: mid, BeforeID: &anchorS})
		}},
	}
	seen := map[f.Digest]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := testMeta(t, &version)
			if tc.create {
				m.ExpectedVersion = nil
			}
			d, err := tc.run(a, m, project)
			if err != nil || d.Validate() != nil || seen[d] {
				t.Fatal("invalid or colliding digest", err)
			}
			seen[d] = true
			m.RequestID = testID[f.Request](t, 91)
			m.IdempotencyKey = "different-key"
			same, err := tc.run(b, m, project)
			if err != nil || same != d {
				t.Fatal("transport/key/session entered semantic", err)
			}
			for _, other := range []struct {
				actor   i.Actor
				project ProjectID
			}{{testActor(t, 7, 8), project}, {a, testID[i.Project](t, 55)}} {
				value, e := tc.run(other.actor, m, other.project)
				if e != nil || value == d {
					t.Fatal("owner/project omitted", e)
				}
			}
			m.ExpectedVersion = nil
			if tc.create {
				m.ExpectedVersion = &version
			}
			_, err = tc.run(a, m, project)
			requireCode(t, err, f.InvalidArgument)
		})
	}
	m := testMeta(t, &version)
	plain, err := UpdateMilestoneDigest(a, m, project, mid, UpdateFields{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	space := " " + title
	changed, err := UpdateMilestoneDigest(a, m, project, mid, UpdateFields{Title: &space})
	if err != nil || plain == changed {
		t.Fatal("raw body collapsed", err)
	}
	empty := ""
	changed, err = UpdateMilestoneDigest(a, m, project, mid, UpdateFields{Title: &title, Description: &empty})
	if err != nil || plain == changed {
		t.Fatal("patch presence collapsed", err)
	}
	next := version + 1
	m.ExpectedVersion = &next
	changed, err = UpdateMilestoneDigest(a, m, project, mid, UpdateFields{Title: &title})
	if err != nil || plain == changed {
		t.Fatal("expected version omitted", err)
	}
	m = testMeta(t, &version)
	changed, err = UpdateMilestoneDigest(a, m, project, testID[Milestone](t, 88), UpdateFields{Title: &title})
	if err != nil || plain == changed {
		t.Fatal("target omitted", err)
	}
	tail, err := ReorderMilestoneDigest(a, m, project, mid, ReorderMilestoneRequest{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := ReorderMilestoneDigest(a, m, project, mid, ReorderMilestoneRequest{BeforeID: &anchorM})
	if err != nil || tail == before {
		t.Fatal("before omitted", err)
	}
	_, err = CreateMilestoneDigest(i.Actor{}, testMeta(t, nil), project, CreateMilestoneRequest{MilestoneID: mid, Title: title})
	requireCode(t, err, f.Unauthenticated)
	agent, err := i.NewAgentRun(project, testID[i.Agent](t, 7), testID[i.Execution](t, 8))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateMilestoneDigest(agent, testMeta(t, nil), project, CreateMilestoneRequest{MilestoneID: mid, Title: title})
	requireCode(t, err, f.DependencyUnbound)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(testID[struct{}](t, 1).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateMilestoneDigest(service, testMeta(t, nil), project, CreateMilestoneRequest{MilestoneID: mid, Title: title})
	requireCode(t, err, f.Forbidden)
}
func TestWorkStructureResponseLifecycleAndCopyBoundaries(t *testing.T) {
	m := testMilestone(t)
	eventID := testID[event.EventIdentity](t, 6)
	result := StructureMutation{Command: MilestoneCreate, Changed: true, Milestone: &m, EventID: &eventID}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var restored StructureMutation
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	copy := restored.Clone()
	copy.Milestone.Title = "different"
	*copy.EventID = testID[event.EventIdentity](t, 7)
	if restored.Milestone.Title != m.Title || *restored.EventID != eventID {
		t.Fatal("receipt aliases caller copy")
	}
	for _, mutate := range []func(*StructureMutation){func(v *StructureMutation) { v.Changed = false }, func(v *StructureMutation) { v.EventID = nil }, func(v *StructureMutation) { v.Command = SprintCreate }, func(v *StructureMutation) { v.Milestone = nil }} {
		v := result.Clone()
		mutate(&v)
		if _, err = json.Marshal(v); err == nil {
			t.Fatal("invalid union encoded")
		}
	}
	for _, value := range []CommandLookup{{State: LookupCommitted}, {State: LookupInProgress, Result: &result}, {State: "unknown"}} {
		if _, err = json.Marshal(value); err == nil {
			t.Fatal("invalid lookup encoded")
		}
	}
	sprint := Sprint{ID: testID[pc.Sprint](t, 6), ProjectID: m.ProjectID, MilestoneID: m.ID, Title: m.Title, Description: m.Description, ManualRank: m.ManualRank, Version: 1, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, State: Planned}
	if sprint.Validate() != nil {
		t.Fatal("planned invalid")
	}
	at := m.CreatedAt
	human := ActorHistory{Kind: i.Human, UserID: testID[i.User](t, 1).String()}
	sprint.StartedAt = &at
	sprint.StartedBy = &human
	sprint.State = Current
	if sprint.Validate() != nil {
		t.Fatal("current invalid")
	}
	clone := sprint.Clone()
	clone.StartedBy.UserID = testID[i.User](t, 12).String()
	if sprint.StartedBy.UserID == clone.StartedBy.UserID {
		t.Fatal("actor history alias")
	}
	sprint.State = Planned
	if sprint.Validate() == nil {
		t.Fatal("impossible lifecycle accepted")
	}
	sprint.State = Completed
	sprint.CompletedAt = &at
	sprint.CompletedBy = &human
	if sprint.Validate() != nil {
		t.Fatal("completed invalid")
	}
	sprint.CompletedBy = nil
	if sprint.Validate() == nil {
		t.Fatal("missing lifecycle actor accepted")
	}
	var history ActorHistory
	if json.Unmarshal([]byte(`{"kind":"human","user_id":"`+human.UserID+`","session_id":"`+testID[i.Session](t, 2).String()+`"}`), &history) == nil {
		t.Fatal("session admitted into history")
	}
}
func TestWorkStructurePlacementAggregateCodecBudget(t *testing.T) {
	m := testMilestone(t)
	m.Description = strings.Repeat("<", MaxDescriptionBytes)
	s := Sprint{ID: testID[pc.Sprint](t, 6), ProjectID: m.ProjectID, MilestoneID: m.ID, Title: m.Title, Description: m.Description, ManualRank: m.ManualRank, Version: 1, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, State: Planned}
	value := Placement{Milestone: m, Sprint: s}
	for _, individual := range []any{m, s} {
		raw, err := json.Marshal(individual)
		if err != nil || len(raw) > MaxRequestBytes {
			t.Fatal("legal individual object exceeded its original budget", err)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) <= MaxRequestBytes || len(raw) > 2*MaxRequestBytes || bytes.Count(raw, []byte(`\u003c`)) != 2*MaxDescriptionBytes {
		t.Fatal("two fully escaped legal objects failed aggregate encoding", err)
	}
	var restored Placement
	if err = json.Unmarshal(raw, &restored); err != nil || restored.Milestone != m || restored.Sprint != s {
		t.Fatal("aggregate round trip lost the full parent or child", err)
	}
	again, err := json.Marshal(restored)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("aggregate encoding is not stable", err)
	}
	for _, child := range []bool{false, true} {
		bad := value
		if child {
			bad.Sprint.Description += "<"
		} else {
			bad.Milestone.Description += "<"
		}
		if _, err = json.Marshal(bad); err == nil {
			t.Fatal("aggregate budget bypassed individual description validation")
		}
	}
	// The larger aggregate budget does not widen request or receipt decoding.
	eid := testID[event.EventIdentity](t, 7)
	receipt := StructureMutation{Command: MilestoneCreate, Changed: true, Milestone: &m, EventID: &eid}
	request := CreateMilestoneRequest{MilestoneID: m.ID, Title: m.Title, Description: m.Description}
	for _, item := range []struct {
		name   string
		value  any
		decode func([]byte) error
	}{
		{"request", request, func(raw []byte) error { return json.Unmarshal(raw, new(CreateMilestoneRequest)) }},
		{"receipt", receipt, func(raw []byte) error { return json.Unmarshal(raw, new(StructureMutation)) }},
	} {
		t.Run(item.name, func(t *testing.T) {
			raw, err := json.Marshal(item.value)
			if err != nil || len(raw) > MaxRequestBytes {
				t.Fatal("legal single object encoding failed", err)
			}
			atLimit := append([]byte("{"+strings.Repeat(" ", MaxRequestBytes-len(raw))), raw[1:]...)
			if len(atLimit) != MaxRequestBytes || item.decode(atLimit) != nil {
				t.Fatal("single-object exact raw cap rejected")
			}
			overLimit := append([]byte("{ "), atLimit[1:]...)
			if len(overLimit) != MaxRequestBytes+1 || item.decode(overLimit) == nil {
				t.Fatal("single-object original raw cap widened")
			}
		})
	}
}
func TestWorkStructureSafeFormattingAndExplicitJSON(t *testing.T) {
	body := "sensitive-body-marker"
	m := testMilestone(t)
	m.Title = body
	key := f.IdempotencyKey("sensitive-key-marker")
	d := f.Digest("sha256:" + strings.Repeat("a", 64))
	values := []any{m, CreateMilestoneRequest{MilestoneID: m.ID, Title: body}, UpdateFields{Description: &body}, CommandLookupRequest{ProjectID: m.ProjectID, Command: MilestoneCreate, Key: key, Semantic: d}}
	for _, v := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			output := fmt.Sprintf(format, v)
			if strings.Contains(output, body) || strings.Contains(output, string(key)) || strings.Contains(output, string(d)) {
				t.Fatal("implicit formatting exposed data")
			}
		}
		var log bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&log, nil))
		logger.Info("safe", "value", v)
		if strings.Contains(log.String(), body) || strings.Contains(log.String(), string(key)) {
			t.Fatal("slog exposed data")
		}
	}
	raw, err := json.Marshal(m)
	if err != nil || !bytes.Contains(raw, []byte(body)) {
		t.Fatal("explicit DTO encoding lost content", err)
	}
}
