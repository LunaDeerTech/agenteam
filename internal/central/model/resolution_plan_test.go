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
