package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testCause(action LifecycleAction) LifecycleCause {
	return LifecycleCause{OperationID: testID[Operation](5), Action: action, ProjectVersion: 3}
}
func testScope() ScopeRef {
	return ScopeRef{Kind: ProjectScope, ProjectID: testID[identity.Project](1)}
}
func testPending() PendingRef {
	return PendingRef{Participant: ArtifactObjectParticipant, Kind: "object", ID: testID[ResourceIdentity](9)}
}
func testOperation() LifecycleOperation {
	return LifecycleOperation{ID: testID[Operation](5), ProjectID: testID[identity.Project](1), Action: Archive, ProjectVersion: 3, State: OperationAccepted, Version: 1, RequiredParticipants: []ParticipantName{ArtifactObjectParticipant, SecretParticipant, OutboxParticipant, AuditParticipant}, CompletedParticipants: []ParticipantName{}, PendingResources: []PendingRef{}, CreatedAt: testTime(), UpdatedAt: testTime()}
}

func TestOwnerGateMatrixCannotGrantServiceOrAgentAuthority(t *testing.T) {
	for _, state := range []Lifecycle{Active, Archiving, Archived, Deleting} {
		for _, gate := range []InitializationGate{InitializationPending, Initialized} {
			for _, intent := range []identity.AccessIntent{identity.Read, identity.Mutate, identity.Launch, identity.Resume, identity.Lifecycle, identity.Converge, "unknown"} {
				allowed := gate == Initialized && state != Deleting && (intent == identity.Read || state == Active && (intent == identity.Mutate || intent == identity.Launch || intent == identity.Resume))
				if (CheckOwnerGate(state, gate, intent) == nil) != allowed {
					t.Fatalf("gate %s %s %s", state, gate, intent)
				}
			}
		}
	}
	if CheckOwnerGate("deleted", Initialized, identity.Read) == nil || CheckOwnerGate(Active, "yes", identity.Read) == nil {
		t.Fatal("invented state accepted")
	}
	if CheckOwnerActorKind(testHuman(2, 10)) != nil {
		t.Fatal("human dispatch")
	}
	agent, _ := identity.NewAgentRun(testID[identity.Project](1), testID[identity.Agent](7), testID[identity.Execution](8))
	requireCode(t, CheckOwnerActorKind(agent), foundation.DependencyUnbound)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(testID[identity.Project](1))
	service, _ := registration.Actor(testID[Operation](5).String(), scope)
	requireCode(t, CheckOwnerActorKind(service), foundation.Forbidden)
}

func TestLifecycleEdgesAndRetryPreserveOriginalGate(t *testing.T) {
	op := ptr(testID[Operation](5))
	valid := []LifecycleChangedPayload{{op, Active, Archiving, Archive}, {op, Archiving, Archived, Archive}, {nil, Archived, Active, Restore}, {op, Active, Deleting, Delete}, {op, Archived, Deleting, Delete}}
	allowed := map[string]bool{}
	for _, edge := range valid {
		if err := edge.Validate(); err != nil {
			t.Fatal(err)
		}
		allowed[string(edge.From)+"/"+string(edge.To)+"/"+string(edge.Action)] = true
	}
	for _, from := range []Lifecycle{Active, Archiving, Archived, Deleting} {
		for _, to := range []Lifecycle{Active, Archiving, Archived, Deleting} {
			for _, action := range []LifecycleAction{Archive, Restore, Delete} {
				operation := op
				if action == Restore {
					operation = nil
				}
				key := string(from) + "/" + string(to) + "/" + string(action)
				if (ValidateLifecycleTransition(from, to, action, operation) == nil) != allowed[key] {
					t.Fatalf("edge %s", key)
				}
			}
		}
	}
	if ValidateLifecycleTransition(Active, Archiving, Archive, nil) == nil || ValidateLifecycleTransition(Archived, Active, Restore, op) == nil {
		t.Fatal("operation presence ignored")
	}
	for _, v := range []struct {
		action   LifecycleAction
		from, to OperationState
		resume   OperationPhase
	}{{Archive, OperationAccepted, OperationStopping, ""}, {Archive, OperationStopping, OperationCompleted, ""}, {Delete, OperationStopping, OperationCleaning, ""}, {Delete, OperationCleaning, OperationCompleted, ""}, {Archive, OperationFailed, OperationStopping, StopPhase}, {Delete, OperationFailed, OperationCleaning, CleanupPhase}} {
		if err := ValidateOperationTransition(v.action, v.from, v.to, v.resume); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []struct {
		action   LifecycleAction
		from, to OperationState
		resume   OperationPhase
	}{{Archive, OperationStopping, OperationCleaning, ""}, {Delete, OperationStopping, OperationCompleted, ""}, {Archive, OperationCompleted, OperationStopping, StopPhase}, {Delete, OperationFailed, OperationCleaning, StopPhase}, {Archive, OperationFailed, OperationStopping, ""}, {Delete, OperationAccepted, OperationCompleted, ""}} {
		if ValidateOperationTransition(v.action, v.from, v.to, v.resume) == nil {
			t.Fatal("invalid progress edge accepted")
		}
	}
}

func testRegistrations() []ParticipantRegistration {
	return []ParticipantRegistration{
		{Name: ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []ReferenceKind{"object"}},
		{Name: SecretParticipant, ContractVersion: 1, OwnerModule: "secret"},
		{Name: OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []ParticipantName{SecretParticipant, ArtifactObjectParticipant}},
		{Name: AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []ParticipantName{OutboxParticipant}},
	}
}
func TestRequiredManifestNoSilentMissingDomains(t *testing.T) {
	entries := testRegistrations()
	m, err := NewRequiredManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := m.Digest()
	entries[0].ReferenceKinds[0] = "secret"
	out := m.Entries()
	out[0].Name = "changed"
	again, _ := m.Digest()
	if digest != again {
		t.Fatal("manifest alias")
	}
	if err := m.Require(ArtifactObjectParticipant, SecretParticipant, OutboxParticipant, AuditParticipant); err != nil {
		t.Fatal(err)
	}
	requireCode(t, m.Require(SkillsParticipant), foundation.DependencyUnbound)
	if _, err := NewRequiredManifest(nil); err == nil {
		t.Fatal("empty manifest treated as complete")
	}
	missing := testRegistrations()[:3]
	if _, err := NewRequiredManifest(missing); err == nil {
		t.Fatal("missing audit accepted")
	}
	wrongOrder := testRegistrations()
	wrongOrder[2].CleanupAfter = nil
	if _, err := NewRequiredManifest(wrongOrder); err == nil {
		t.Fatal("outbox before producer accepted")
	}
	cycle := testRegistrations()
	cycle[0].CleanupAfter = []ParticipantName{AuditParticipant}
	if _, err := NewRequiredManifest(cycle); err == nil {
		t.Fatal("cleanup cycle accepted")
	}
	if err := m.ValidateRefs([]PendingRef{testPending()}); err != nil {
		t.Fatal(err)
	}
	ref := testPending()
	ref.Kind = "unregistered"
	if m.ValidateRefs([]PendingRef{ref}) == nil {
		t.Fatal("unregistered resource kind accepted")
	}
	if json.Unmarshal([]byte(`[]`), &m) == nil {
		t.Fatal("JSON minted trusted manifest")
	}
}

func TestLifecycleSafeProgressAndScopeVariants(t *testing.T) {
	op := testOperation()
	raw, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	var decoded LifecycleOperation
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatal("operation round trip")
	}
	op.State = OperationCompleted
	op.CompletedParticipants = append([]ParticipantName{}, op.RequiredParticipants...)
	op.CompletedAt = ptr(testTime())
	op.CompletedProjectVersion = ptr(foundation.Version(4))
	if op.Validate() != nil || op.ProjectVersion != 3 {
		t.Fatal("completed version overwrote cause")
	}
	op.CompletedProjectVersion = ptr(foundation.Version(5))
	if op.Validate() == nil {
		t.Fatal("completion skipped a Project version")
	}
	op.CompletedProjectVersion = ptr(foundation.Version(4))
	op.ProjectVersion = 4
	if op.Validate() == nil {
		t.Fatal("completion reused acceptance version")
	}
	op.ProjectVersion = 3
	op.PendingResources = []PendingRef{testPending()}
	if op.Validate() == nil {
		t.Fatal("completed with pending refs")
	}
	tooMany := testOperation()
	for n := 0; n < 101; n++ {
		ref := testPending()
		ref.ID = testID[ResourceIdentity](n + 100)
		tooMany.PendingResources = append(tooMany.PendingResources, ref)
	}
	if tooMany.Validate() == nil {
		t.Fatal("unbounded DTO refs")
	}
	tooMany.PendingResources = tooMany.PendingResources[:100]
	tooMany.PendingRefsTruncated = true
	if tooMany.Validate() != nil {
		t.Fatal("valid truncated DTO rejected")
	}
	scope := testScope()
	scope.MeetingID = ptr(testID[Meeting](11))
	if scope.Validate() == nil {
		t.Fatal("Project scope with meeting field")
	}
	scope.Kind = MeetingScope
	if scope.Validate() != nil {
		t.Fatal("typed Meeting reserved variant")
	}
	requireCode(t, RequireProjectScope(scope), foundation.DependencyUnbound)
	for _, raw := range []string{`{"kind":"project","project_id":"01960000-0000-7000-8000-000000000001","meeting_id":null}`, `{"kind":"meeting","project_id":"01960000-0000-7000-8000-000000000001"}`} {
		var s ScopeRef
		if json.Unmarshal([]byte(raw), &s) == nil {
			t.Fatal("scope variant decoded")
		}
	}
}

func TestReportsBindCauseAndDoNotTruncateOrGrantAuthority(t *testing.T) {
	cause, scope := testCause(Delete), testScope()
	refs := []PendingRef{testPending()}
	report, err := NewStopReport(ArtifactObjectParticipant, cause, scope, StopDetails{State: StopPending, UnknownRefs: refs, SafeReason: ReasonOutcomeUnknown})
	if err != nil {
		t.Fatal(err)
	}
	refs[0].ID = testID[ResourceIdentity](100)
	out := report.Details()
	out.UnknownRefs[0].ID = testID[ResourceIdentity](101)
	if report.Details().UnknownRefs[0] != testPending() {
		t.Fatal("report aliases references")
	}
	if !report.Matches(ArtifactObjectParticipant, cause, scope) {
		t.Fatal("report mapping")
	}
	wrong := cause
	wrong.ProjectVersion++
	if report.Matches(ArtifactObjectParticipant, wrong, scope) {
		t.Fatal("wrong cause accepted")
	}
	if _, err := NewStopReport(ArtifactObjectParticipant, cause, scope, StopDetails{State: Stopped, ActiveRefs: []PendingRef{testPending()}}); err == nil {
		t.Fatal("cancel or active work treated as stopped")
	}
	many := make([]PendingRef, 150)
	for i := range many {
		many[i] = testPending()
		many[i].ID = testID[ResourceIdentity](200 + i)
	}
	full, err := NewStopReport(ArtifactObjectParticipant, cause, scope, StopDetails{State: StopPending, UnknownRefs: many})
	if err != nil || len(full.Details().UnknownRefs) != 150 {
		t.Fatal("correctness report was DTO-truncated")
	}
	checkpoint, err := NewCleanupCheckpoint(ArtifactObjectParticipant, cause, scope, 1, []byte("provider-private-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	bytes := checkpoint.Bytes()
	bytes[0] = 'x'
	if string(checkpoint.Bytes()) != "provider-private-checkpoint" {
		t.Fatal("checkpoint aliases bytes")
	}
	cleanup, err := NewCleanupReport(ArtifactObjectParticipant, cause, scope, CleanupDetails{State: CleanupPending, Checkpoint: &checkpoint, RemainingRefs: []PendingRef{testPending()}})
	if err != nil || !cleanup.Matches(ArtifactObjectParticipant, cause, scope) {
		t.Fatal("cleanup mapping")
	}
	if _, err := NewCleanupReport(ArtifactObjectParticipant, wrong, scope, CleanupDetails{State: CleanupPending, Checkpoint: &checkpoint}); err == nil {
		t.Fatal("cross-cause checkpoint accepted")
	}
	if _, err := NewCleanupReport(ArtifactObjectParticipant, testCause(Archive), scope, CleanupDetails{State: CleanupCompleted}); err == nil {
		t.Fatal("archive invoked cleanup")
	}
	for _, target := range []any{&report, &cleanup, &checkpoint} {
		if json.Unmarshal([]byte(`{}`), target) == nil {
			t.Fatal("JSON minted report/checkpoint")
		}
		raw, err := json.Marshal(target)
		if err != nil || strings.Contains(string(raw), "provider-private") {
			t.Fatal("unsafe report encoding")
		}
	}
	if got := fmt.Sprintf("%#v", struct{ checkpoint CleanupCheckpoint }{checkpoint}); strings.Contains(got, "provider-private") {
		t.Fatal("nested checkpoint formatting leaked")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				d := report.Details()
				d.UnknownRefs[0].ID = testID[ResourceIdentity](999)
				b := checkpoint.Bytes()
				b[0] = 'x'
				_ = cleanup.Details()
			}
		})
	}
	wg.Wait()
}

func TestCompletedDeleteOperationCannotEscapeAsPublicResult(t *testing.T) {
	operation := testOperation()
	operation.Action = Delete
	operation.State = OperationCompleted
	operation.CompletedParticipants = append([]ParticipantName{}, operation.RequiredParticipants...)
	operation.CompletedAt = ptr(testTime())
	if err := operation.Validate(); err != nil {
		t.Fatal("internal completed operation shape:", err)
	}
	result := LifecycleResult{Operation: &operation}
	requireCode(t, result.Validate(), foundation.InvalidArgument)
	if _, err := json.Marshal(result); err == nil {
		t.Fatal("public lifecycle result exposed a completed delete operation")
	}
	retry := CommandResult{Command: RetryLifecycleCommand, Lifecycle: &result}
	if _, err := json.Marshal(retry); err == nil {
		t.Fatal("Retry wrapper exposed a completed delete operation")
	}
	operation.Action = Archive
	operation.CompletedProjectVersion = ptr(foundation.Version(4))
	if err := result.Validate(); err != nil {
		t.Fatal("completed archive operation should remain representable:", err)
	}
	operation.Action = Delete
	operation.State = OperationCleaning
	operation.CompletedAt = nil
	operation.CompletedProjectVersion = nil
	if err := result.Validate(); err != nil {
		t.Fatal("unfinished delete operation should remain representable:", err)
	}
	result = LifecycleResult{Receipt: ptr(testReceipt(t))}
	if _, err := json.Marshal(result); err != nil {
		t.Fatal("minimal delete receipt:", err)
	}
}
