package model

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func resolutionTestDraft(t *testing.T, r mc.ResolveRequest) resolutionDraft {
	t.Helper()
	return resolutionDraft{Format: 1, Snapshot: resolutionSnapshotDTO{Format: 1, ID: mustID[mc.Snapshot](t), ProjectID: r.Consumer.ProjectID, Identity: mc.ModelIdentity{ProviderID: mustID[mc.Provider](t), ModelID: *r.ModelRef, ProviderName: "provider", ModelName: "model", ProviderModelID: "model-1", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: adapter.OpenAIChatTextRevision}, Endpoint: "https://provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}, Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, StructuredOutputModes: []string{"text"}}, CredentialID: mustID[sc.Credential](t).String()}, ProviderVersion: 2, ModelVersion: 3, LeaseID: mustID[sc.Lease](t).String()}
}
func TestModelCurrentResolutionStableSubjectAndExactActorPlan(t *testing.T) {
	r := resolutionTestRequest(t)
	user := mustID[id.User](t)
	r.Actor, _ = id.NewHuman(user, mustID[id.Session](t))
	other := r.Clone()
	other.Actor, _ = id.NewHuman(user, mustID[id.Session](t))
	if resolutionSemantic(r) != resolutionSemantic(other) {
		t.Fatal("Session changed durable semantic")
	}
	a, _ := mc.ResolveBinding(r)
	b, _ := mc.ResolveBinding(other)
	if a == b {
		t.Fatal("plan lost exact Session")
	}
	x, _ := resolutionIdentity(r)
	y, _ := resolutionIdentity(other)
	if x.Canonical() != y.Canonical() {
		t.Fatal("Session changed unit")
	}
	other.ModelRef = new(mc.ModelID)
	*other.ModelRef = mustID[mc.Model](t)
	if resolutionSemantic(r) == resolutionSemantic(other) {
		t.Fatal("different requested model reused semantic")
	}
	y, _ = resolutionIdentity(other)
	if x.Canonical() != y.Canonical() {
		t.Fatal("different request escaped same unit conflict")
	}
	other = r.Clone()
	other.Consumer.Purpose = mc.AgentCompaction
	other.Purpose = mc.AgentCompaction
	y, _ = resolutionIdentity(other)
	if x.Canonical() == y.Canonical() {
		t.Fatal("execution purpose units collapsed")
	}
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, mustID[mc.Call](t).String())
	r.LeaseOwner = owner
	other.LeaseOwner = owner
	x, _ = resolutionIdentity(r)
	y, _ = resolutionIdentity(other)
	if x.Canonical() != y.Canonical() {
		t.Fatal("same call escaped purpose conflict")
	}
}
func TestModelCurrentResolutionSnapshotDTOAndPlanCopies(t *testing.T) {
	r := resolutionTestRequest(t)
	d := resolutionTestDraft(t, r)
	if e := d.validate(); e != nil {
		t.Fatal(e)
	}
	raw, _ := encoded(d)
	var decoded resolutionDraft
	if e := resolutionDecode(raw, 256<<10, &decoded); e != nil {
		t.Fatal(e)
	}
	if !resolutionEqual(decoded, d) || decoded.validate() != nil {
		t.Fatal("opaque credential DTO did not roundtrip")
	}
	s, e := d.Snapshot.snapshot()
	if e != nil {
		t.Fatal(e)
	}
	s.Capabilities.InputModalities[0] = "file"
	s.HeaderOverwrite["x-leak"] = "value"
	s.Parameters[0] = '['
	s, e = d.Snapshot.snapshot()
	if e != nil || s.Capabilities.InputModalities[0] != "text" || len(s.HeaderOverwrite) != 0 || string(s.Parameters) != "{}" {
		t.Fatal("snapshot mutation escaped")
	}
	for _, raw := range []string{`{"format_version":2}`, `{"format_version":1,"Unknown":true}`, `{} {}`} {
		var target resolutionDraft
		if e = resolutionDecode([]byte(raw), 256<<10, &target); e == nil && target.validate() == nil {
			t.Fatal("bad persisted format accepted")
		}
	}
	key, _ := f.ProjectLock(r.Consumer.ProjectID.String())
	locks, e := resolutionUnion([]f.LockRequest{{Key: key, Mode: f.Shared}, {Key: key, Mode: f.Exclusive}})
	if e != nil || len(locks) != 1 || locks[0].Mode != f.Exclusive {
		t.Fatal("union weakened required lock")
	}
}

func TestModelCurrentResolutionDurableIdentityClosedFacts(t *testing.T) {
	r := resolutionTestRequest(t)
	agent := resolutionRequest(r)
	human := agent
	human.Actor = id.ActorDetails{Kind: id.Human, UserID: mustID[id.User](t).String()}
	service := agent
	service.Actor = id.ActorDetails{Kind: id.Service, ServiceName: id.OutboundService, CauseRef: mustID[struct{}](t).String(), ProjectID: r.Consumer.ProjectID.String()}
	for _, d := range []resolutionRequestDTO{agent, human, service} {
		if d.validate() != nil {
			t.Fatal("valid stable initiator rejected")
		}
		unit, e := resolutionUnitIdentity(d.Consumer, d.Purpose, d.Owner)
		want, _ := resolutionIdentity(r)
		if e != nil || unit.Canonical() != want.Canonical() {
			t.Fatal("unit differs from public request")
		}
	}
	cases := map[string]func(*resolutionRequestDTO){
		"empty-actor":        func(d *resolutionRequestDTO) { d.Actor = id.ActorDetails{} },
		"unknown-kind":       func(d *resolutionRequestDTO) { d.Actor.Kind = "other" },
		"human-missing-user": func(d *resolutionRequestDTO) { d.Actor = human.Actor; d.Actor.UserID = "" },
		"human-session-persisted": func(d *resolutionRequestDTO) {
			d.Actor = human.Actor
			d.Actor.SessionID = mustID[id.Session](t).String()
		},
		"human-extra-role": func(d *resolutionRequestDTO) { d.Actor = human.Actor; d.Actor.ServiceName = id.SecretService },
		"agent-project":    func(d *resolutionRequestDTO) { d.Actor.ProjectID = mustID[id.Project](t).String() },
		"agent-execution":  func(d *resolutionRequestDTO) { d.Actor.ExecutionID = mustID[id.Execution](t).String() },
		"agent-user":       func(d *resolutionRequestDTO) { d.Actor.UserID = mustID[id.User](t).String() },
		"service-role":     func(d *resolutionRequestDTO) { d.Actor = service.Actor; d.Actor.ServiceName = "model-unregistered" },
		"service-cause":    func(d *resolutionRequestDTO) { d.Actor = service.Actor; d.Actor.CauseRef = "arbitrary" },
		"service-project": func(d *resolutionRequestDTO) {
			d.Actor = service.Actor
			d.Actor.ProjectID = mustID[id.Project](t).String()
		},
		"owner-mismatch": func(d *resolutionRequestDTO) { d.Owner.ID = mustID[id.Execution](t).String() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := resolutionRequest(r)
			mutate(&d)
			if d.validate() == nil {
				t.Fatal("bad durable identity accepted")
			}
		})
	}
	changed := agent
	changed.Owner = sc.OwnerDetails{Kind: sc.ModelCallOwner, ID: mustID[mc.Call](t).String()}
	a, _ := resolutionUnitIdentity(agent.Consumer, agent.Purpose, agent.Owner)
	b, _ := resolutionUnitIdentity(changed.Consumer, changed.Purpose, changed.Owner)
	if a.Canonical() == b.Canonical() {
		t.Fatal("different owner produced same canonical identity")
	}
}

func TestModelMeetingSummaryResolutionUnitMappingAndLocks(t *testing.T) {
	r := meetingResolutionTestRequest(t)
	identity, e := resolutionIdentity(r)
	if e != nil {
		t.Fatal(e)
	}
	dto := resolutionRequest(r)
	if e := dto.validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*mc.ResolveRequest){
		func(c *mc.ResolveRequest) { c.Purpose = mc.MeetingSummaryUpdate; c.Consumer.Purpose = c.Purpose },
		func(c *mc.ResolveRequest) { c.Consumer.MeetingID = mustID[struct{}](t).String() },
		func(c *mc.ResolveRequest) { c.Consumer.OperationID = mustID[struct{}](t).String() },
		func(c *mc.ResolveRequest) { v := f.Version(1); c.Selection.Version = &v },
		func(c *mc.ResolveRequest) { c.Actor, _ = id.NewHuman(mustID[id.User](t), mustID[id.Session](t)) },
	} {
		c := r.Clone()
		change(&c)
		other, e := resolutionIdentity(c)
		if e != nil || other.Canonical() != identity.Canonical() || resolutionSemantic(c) == resolutionSemantic(r) {
			t.Fatal("same Call escaped semantic conflict", e)
		}
	}
	copy := r.Clone()
	uid, _ := f.ParseID[id.User](r.Actor.Details().UserID)
	copy.Actor, _ = id.NewHuman(uid, mustID[id.Session](t))
	if resolutionSemantic(copy) != resolutionSemantic(r) {
		t.Fatal("new Session changed durable meaning")
	}
	b, _ := mc.ResolveBinding(r)
	other, _ := mc.ResolveBinding(copy)
	if b == other {
		t.Fatal("new Session reused in-memory plan")
	}
	draft := resolutionDraft{Snapshot: resolutionSnapshotDTO{Identity: mc.ModelIdentity{ProviderID: mustID[mc.Provider](t), ModelID: mustID[mc.Model](t)}}}
	locks, e := resolutionUnion(resolutionBaseLocks(r, identity, mustID[mc.Snapshot](t), draft))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"model-meeting-summary-selection", "model-platform-selection", "model-references"} {
		want := systemLock(name, f.Shared)
		found := false
		for _, lock := range locks {
			if lock.Key.Canonical() == want.Key.Canonical() {
				found = lock.Mode == f.Shared
			}
		}
		if !found {
			t.Fatal("missing Summary loader shared lock", name)
		}
	}
	for i := 1; i < len(locks); i++ {
		if f.CompareLockKeys(locks[i-1].Key, locks[i].Key) >= 0 {
			t.Fatal("lock union unordered or duplicate")
		}
	}
	// Extending the selection vocabulary must not add fields to format v1.
	encodedDTO, e := encoded(dto)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encodedDTO, &fields) != nil || len(fields) != 9 || string(fields["format_version"]) != "1" {
		t.Fatalf("durable format changed: %s", encodedDTO)
	}
}

// Golden format-v1 bytes from the unchanged direct/memory DTO contract. These
// are serialization regressions, not claims of a cross-binary receipt upgrade.
func TestModelMeetingSummaryPreservesLegacyResolutionBytes(t *testing.T) {
	const key = "018f0000-0000-7000-8000-000000000001"
	project, _ := f.ParseID[id.Project](key)
	agent, _ := f.ParseID[id.Agent](key)
	execution, _ := f.ParseID[id.Execution](key)
	user, _ := f.ParseID[id.User](key)
	session, _ := f.ParseID[id.Session](key)
	model, _ := f.ParseID[mc.Model](key)
	actor, _ := id.NewHuman(user, session)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, key)
	r := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: project, Purpose: mc.AgentGeneration, AgentID: &agent, ExecutionID: &execution}, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &model, Selection: &mc.SelectionRef{Kind: "direct"}, LeaseOwner: owner}
	prefix := `{"format_version":1,"Actor":{"Kind":"human","UserID":"` + key + `","SessionID":"","ProjectID":"","AgentID":"","ExecutionID":"","ServiceName":"","CauseRef":""},"Consumer":`
	suffix := `,"ReasoningEffort":"","Owner":{"kind":"model_call","id":"` + key + `"}}`
	direct := prefix + `{"kind":"agent","project_id":"` + key + `","purpose":"agent_generation","agent_id":"` + key + `","execution_id":"` + key + `"},"Purpose":"agent_generation","Source":"current_selection","ModelRef":"` + key + `","Selection":{"kind":"direct"}` + suffix
	memory := prefix + `{"kind":"memory","project_id":"` + key + `","purpose":"memory_extraction","agent_id":"` + key + `","operation_id":"` + key + `"},"Purpose":"memory_extraction","Source":"current_selection","ModelRef":null,"Selection":{"kind":"platform","selector":"memory"}` + suffix
	for _, want := range []string{direct, memory} {
		if e := r.Validate(); e != nil {
			t.Fatal(e)
		}
		raw, e := encoded(resolutionRequest(r))
		if e != nil || string(raw) != want || resolutionSemantic(r) != hash([]byte(want)) {
			t.Fatalf("legacy bytes changed: %s; %v", raw, e)
		}
		r.Consumer.Kind = mc.MemoryConsumer
		r.Consumer.Purpose = mc.MemoryExtraction
		r.Consumer.ExecutionID = nil
		r.Consumer.OperationID = key
		r.Purpose = mc.MemoryExtraction
		r.ModelRef = nil
		r.Selection = &mc.SelectionRef{Kind: "platform", Selector: mc.MemorySelector}
	}
}
