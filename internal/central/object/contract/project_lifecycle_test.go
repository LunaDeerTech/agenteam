package contract

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	stopProjectID = "01900000-0000-7000-8000-000000000051"
	stopOperation = "01900000-0000-7000-8000-000000000052"
	stopOtherID   = "01900000-0000-7000-8000-000000000053"
)

func stopID[K any](t *testing.T, raw string) foundation.ID[K] {
	t.Helper()
	id, err := foundation.ParseID[K](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func stopCause(t *testing.T) ProjectStopCause {
	t.Helper()
	cause, err := NewProjectStopCause(ProjectStopCauseDetails{
		ProjectID: stopID[identity.Project](t, stopProjectID), OperationID: stopID[ProjectStopOperation](t, stopOperation),
		Action: ProjectStopArchive, ProjectVersion: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cause
}
func stopActor(t *testing.T, cause ProjectStopCause, role identity.ServiceName) identity.Actor {
	t.Helper()
	reg, err := identity.RegisterService(role)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := identity.InProject(cause.Details().ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := reg.Actor(cause.Details().OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
func stopRequest(t *testing.T, cause ProjectStopCause, step ProjectStopStep) ProjectStopRequest {
	t.Helper()
	r, err := NewProjectStopRequest(ProjectStopRequestDetails{stopActor(t, cause, identity.ProjectLifecycle), cause, ObjectStopComponent, step})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func stopDependencies(t *testing.T, request ProjectStopRequest) AccessDependencies {
	t.Helper()
	binding, err := ProjectStopBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	key, err := foundation.ProjectLock(request.Details().Cause.Details().ProjectID.String())
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewAccessDependencies(binding, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func checkStopEnum[T ~string](t *testing.T, values []T, validate func(T) error) {
	t.Helper()
	for _, value := range values {
		if validate(value) != nil {
			t.Fatal("valid enum rejected", value)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded T
		if err = json.Unmarshal(raw, &decoded); err != nil || decoded != value {
			t.Fatal("enum roundtrip", value, err)
		}
	}
	for _, invalid := range []T{"", "future", "restore", T(strings.ToUpper(string(values[0])))} {
		if validate(invalid) == nil {
			t.Fatal("open enum", invalid)
		}
		if _, err := json.Marshal(invalid); err == nil {
			t.Fatal("invalid enum marshalled", invalid)
		}
	}
	for _, raw := range []string{`null`, `""`, `"future"`, `"restore"`, `true`, `0`, `{}`, `[]`, `"archive" false`, ``} {
		before := values[0]
		decoded := before
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil || decoded != before {
			t.Fatal("invalid decode accepted or mutated value", raw, decoded)
		}
	}
}

func TestProjectStopClosedEnums(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopAction{ProjectStopArchive, ProjectStopDelete}, ProjectStopAction.Validate)
	})
	t.Run("component", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopComponent{ObjectStopComponent, ArtifactStopComponent}, ProjectStopComponent.Validate)
	})
	t.Run("step", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopStep{RequestProjectStopStep, InspectProjectStopStep}, ProjectStopStep.Validate)
	})
	t.Run("authorization", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopAuthorizationMode{ContinueProjectStop, ReadProjectStop}, ProjectStopAuthorizationMode.Validate)
	})
	t.Run("state", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopState{ProjectStopped, ProjectStopPending, ProjectStopFailed}, ProjectStopState.Validate)
	})
	t.Run("reason", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopReason{ProjectStopDependencyUnbound, ProjectStopDependencyUnavailable, ProjectStopWorkPending, ProjectStopOutcomeUnknown, ProjectStopOperationFailed}, ProjectStopReason.Validate)
	})
	t.Run("reference", func(t *testing.T) {
		checkStopEnum(t, []ProjectStopRefKind{StopObjectPreparation, StopObjectUpload, StopObjectAttempt, StopObjectLease, StopObjectTransfer, StopObjectDownload, StopArtifactUpload, StopArtifactCreation}, ProjectStopRefKind.Validate)
	})
	var nilAction *ProjectStopAction
	if nilAction.UnmarshalJSON([]byte(`"archive"`)) == nil {
		t.Fatal("nil enum target accepted")
	}
}

func TestProjectStopCauseAndActorBoundaries(t *testing.T) {
	cause := stopCause(t)
	for name, mutate := range map[string]func(*ProjectStopCauseDetails){
		"missing project":   func(d *ProjectStopCauseDetails) { d.ProjectID = identity.ProjectID{} },
		"missing operation": func(d *ProjectStopCauseDetails) { d.OperationID = ProjectStopOperationID{} },
		"restore":           func(d *ProjectStopCauseDetails) { d.Action = "restore" },
		"missing version":   func(d *ProjectStopCauseDetails) { d.ProjectVersion = 0 },
		"negative version":  func(d *ProjectStopCauseDetails) { d.ProjectVersion = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			d := cause.Details()
			mutate(&d)
			if _, err := NewProjectStopCause(d); err == nil {
				t.Fatal("invalid cause accepted")
			}
		})
	}
	request := stopRequest(t, cause, RequestProjectStopStep)
	human, err := identity.NewHuman(stopID[identity.User](t, stopOtherID), stopID[identity.Session](t, stopOperation))
	if err != nil {
		t.Fatal(err)
	}
	otherDetails := cause.Details()
	otherDetails.ProjectID = stopID[identity.Project](t, stopOtherID)
	other, err := NewProjectStopCause(otherDetails)
	if err != nil {
		t.Fatal(err)
	}
	wrongOperation := cause.Details()
	wrongOperation.OperationID = stopID[ProjectStopOperation](t, stopOtherID)
	wrongCause, err := NewProjectStopCause(wrongOperation)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ProjectStopRequestDetails){
		"human":           func(d *ProjectStopRequestDetails) { d.Actor = human },
		"other service":   func(d *ProjectStopRequestDetails) { d.Actor = stopActor(t, cause, identity.ObjectService) },
		"other project":   func(d *ProjectStopRequestDetails) { d.Actor = stopActor(t, other, identity.ProjectLifecycle) },
		"other operation": func(d *ProjectStopRequestDetails) { d.Actor = stopActor(t, wrongCause, identity.ProjectLifecycle) },
		"zero actor":      func(d *ProjectStopRequestDetails) { d.Actor = identity.Actor{} },
		"zero cause":      func(d *ProjectStopRequestDetails) { d.Cause = ProjectStopCause{} },
		"component":       func(d *ProjectStopRequestDetails) { d.Component = "secret" },
		"step":            func(d *ProjectStopRequestDetails) { d.Step = "cleanup" },
	} {
		t.Run(name, func(t *testing.T) {
			d := request.Details()
			mutate(&d)
			if _, err := NewProjectStopRequest(d); err == nil {
				t.Fatal("mixed request accepted")
			}
		})
	}
	d := request.Details()
	d.Step = InspectProjectStopStep
	if request.Details().Step != RequestProjectStopStep || request.Validate() != nil || cause.Validate() != nil {
		t.Fatal("request projection mutated original")
	}
	if (ProjectStopCause{}).Equal(ProjectStopCause{}) || (ProjectStopRequest{}).Equal(ProjectStopRequest{}) {
		t.Fatal("invalid zero equal")
	}
}

func TestProjectStopBindingIncludesEveryValidVariant(t *testing.T) {
	r := stopRequest(t, stopCause(t), RequestProjectStopStep)
	baseline, err := ProjectStopBinding(r)
	if err != nil || baseline.Validate() != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ProjectStopRequestDetails, *ProjectStopCauseDetails){
		"project": func(_ *ProjectStopRequestDetails, c *ProjectStopCauseDetails) {
			c.ProjectID = stopID[identity.Project](t, stopOtherID)
		},
		"operation": func(_ *ProjectStopRequestDetails, c *ProjectStopCauseDetails) {
			c.OperationID = stopID[ProjectStopOperation](t, stopOtherID)
		},
		"version":   func(_ *ProjectStopRequestDetails, c *ProjectStopCauseDetails) { c.ProjectVersion++ },
		"action":    func(_ *ProjectStopRequestDetails, c *ProjectStopCauseDetails) { c.Action = ProjectStopDelete },
		"component": func(d *ProjectStopRequestDetails, _ *ProjectStopCauseDetails) { d.Component = ArtifactStopComponent },
		"step":      func(d *ProjectStopRequestDetails, _ *ProjectStopCauseDetails) { d.Step = InspectProjectStopStep },
	} {
		t.Run(name, func(t *testing.T) {
			d, cause := r.Details(), r.Details().Cause.Details()
			change(&d, &cause)
			d.Cause, err = NewProjectStopCause(cause)
			if err != nil {
				t.Fatal(err)
			}
			d.Actor = stopActor(t, d.Cause, identity.ProjectLifecycle)
			other, err := NewProjectStopRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := ProjectStopBinding(other)
			if err != nil || binding == baseline || r.Equal(other) {
				t.Fatal("request field omitted from binding", err)
			}
			if fmt.Sprint(r) != fmt.Sprint(other) {
				t.Fatal("safety projection should not expose request identity")
			}
		})
	}
	copy := stopRequest(t, stopCause(t), RequestProjectStopStep)
	got, err := ProjectStopBinding(copy)
	if err != nil || got != baseline || !r.Equal(copy) {
		t.Fatal("binding depends on allocation")
	}
	if _, err := ProjectStopBinding(ProjectStopRequest{}); err == nil {
		t.Fatal("zero request hashed")
	}
}

func TestProjectStopAuthorizationExactTxDependenciesAndReadOnly(t *testing.T) {
	r := stopRequest(t, stopCause(t), RequestProjectStopStep)
	deps := stopDependencies(t, r)
	tx := foundation.NewTx()
	grant, err := NewProjectStopAuthorization(tx, r, deps, ContinueProjectStop)
	if err != nil || grant.Validate() != nil || grant.Mode() != ContinueProjectStop || !grant.Matches(tx, r, deps) {
		t.Fatal("valid binding", err)
	}
	changedLocks := deps.Locks()
	changedLocks[0].Mode = foundation.Exclusive
	changed, err := NewAccessDependencies(deps.Mapping(), changedLocks)
	if err != nil {
		t.Fatal(err)
	}
	otherMapping, err := NewAccessDependencies(foundation.Digest("sha256:"+strings.Repeat("1", 64)), deps.Locks())
	if err != nil {
		t.Fatal(err)
	}
	inspect := stopRequest(t, r.Details().Cause, InspectProjectStopStep)
	if grant.Matches(foundation.NewTx(), r, deps) || grant.Matches(tx, r, changed) || grant.Matches(tx, r, otherMapping) || grant.Matches(tx, inspect, deps) || grant.Matches(tx, r, AccessDependencies{}) {
		t.Fatal("grant escaped its exact physical transaction/request/dependencies")
	}
	if _, err := NewProjectStopAuthorization(tx, r, deps, ReadProjectStop); err == nil {
		t.Fatal("read-only permission accepted for stop request")
	}
	read, err := NewProjectStopAuthorization(tx, inspect, stopDependencies(t, inspect), ReadProjectStop)
	if err != nil || read.Mode() != ReadProjectStop {
		t.Fatal("inspection denied", err)
	}
	for _, tc := range []struct {
		tx   foundation.Tx
		r    ProjectStopRequest
		d    AccessDependencies
		mode ProjectStopAuthorizationMode
	}{{foundation.Tx{}, r, deps, ContinueProjectStop}, {tx, ProjectStopRequest{}, deps, ContinueProjectStop}, {tx, r, AccessDependencies{}, ContinueProjectStop}, {tx, r, deps, "allow"}} {
		if _, err := NewProjectStopAuthorization(tc.tx, tc.r, tc.d, tc.mode); err == nil {
			t.Fatal("invalid authorization constructed")
		}
	}
	if (ProjectStopAuthorization{}).Matches(tx, r, deps) || (ProjectStopAuthorization{}).Validate() == nil || (ProjectStopAuthorization{}).Mode() != "" {
		t.Fatal("zero authorization accepted")
	}
}

func TestProjectStopReportsKeepUnknownAndRejectFalseStopped(t *testing.T) {
	cause := stopCause(t)
	ref := ProjectStopRef{StopObjectAttempt, stopID[ProjectStopResource](t, stopOtherID)}
	for name, d := range map[string]ObjectStopDetails{
		"stopped active":     {State: ProjectStopped, ActiveRefs: []ProjectStopRef{ref}},
		"stopped unknown":    {State: ProjectStopped, UnknownRefs: []ProjectStopRef{ref}},
		"stopped reason":     {State: ProjectStopped, SafeReason: ProjectStopWorkPending},
		"failed no reason":   {State: ProjectStopFailed},
		"failed unknown":     {State: ProjectStopFailed, SafeReason: ProjectStopOutcomeUnknown},
		"failed unknown ref": {State: ProjectStopFailed, SafeReason: ProjectStopOperationFailed, UnknownRefs: []ProjectStopRef{ref}},
		"duplicate":          {State: ProjectStopPending, ActiveRefs: []ProjectStopRef{ref, ref}},
		"overlapping":        {State: ProjectStopPending, ActiveRefs: []ProjectStopRef{ref}, UnknownRefs: []ProjectStopRef{ref}},
		"raw reason":         {State: ProjectStopPending, SafeReason: "raw SQL error"},
		"unknown state":      {State: "completed"},
		"zero ref":           {State: ProjectStopPending, ActiveRefs: []ProjectStopRef{{Kind: StopObjectAttempt}}},
		"raw kind":           {State: ProjectStopPending, ActiveRefs: []ProjectStopRef{{Kind: "table_name", ID: ref.ID}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewObjectStopReport(ObjectStopComponent, cause, d); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
	for _, d := range []ObjectStopDetails{
		{State: ProjectStopped}, {State: ProjectStopPending},
		{State: ProjectStopPending, UnknownRefs: []ProjectStopRef{ref}, SafeReason: ProjectStopOutcomeUnknown},
		{State: ProjectStopFailed, ActiveRefs: []ProjectStopRef{ref}, SafeReason: ProjectStopOperationFailed},
	} {
		r, err := NewObjectStopReport(ObjectStopComponent, cause, d)
		if err != nil || r.Validate() != nil || !r.Matches(ObjectStopComponent, cause) {
			t.Fatal("valid provider projection", err)
		}
	}
	artifact := ProjectStopRef{StopArtifactCreation, ref.ID}
	d := ObjectStopDetails{State: ProjectStopPending, ActiveRefs: []ProjectStopRef{artifact, ref}}
	if _, err := NewObjectStopReport(ObjectStopComponent, cause, d); err == nil {
		t.Fatal("object report claimed artifact work")
	}
	r, err := NewObjectStopReport(ArtifactStopComponent, cause, d)
	if err != nil || !r.Matches(ArtifactStopComponent, cause) || r.Matches(ObjectStopComponent, cause) {
		t.Fatal("artifact cannot combine object and own work", err)
	}
	other := cause.Details()
	other.ProjectVersion++
	changed, err := NewProjectStopCause(other)
	if err != nil || r.Matches(ArtifactStopComponent, changed) {
		t.Fatal("report accepted for another cause", err)
	}
	if _, err := NewObjectStopReport("future", cause, d); err == nil {
		t.Fatal("unknown component")
	}
	if _, err := NewObjectStopReport(ArtifactStopComponent, ProjectStopCause{}, d); err == nil {
		t.Fatal("missing report cause")
	}
}

func TestProjectStopProjectionIsolationAndConcurrentReaders(t *testing.T) {
	cause := stopCause(t)
	ref := ProjectStopRef{StopObjectAttempt, stopID[ProjectStopResource](t, stopOtherID)}
	d := ObjectStopDetails{State: ProjectStopPending, ActiveRefs: []ProjectStopRef{ref}}
	report, err := NewObjectStopReport(ObjectStopComponent, cause, d)
	if err != nil {
		t.Fatal(err)
	}
	d.ActiveRefs[0].Kind = StopObjectLease
	if report.Details().ActiveRefs[0] != ref {
		t.Fatal("constructor retained caller slice")
	}
	r := stopRequest(t, cause, InspectProjectStopStep)
	deps := stopDependencies(t, r)
	tx := foundation.NewTx()
	grant, err := NewProjectStopAuthorization(tx, r, deps, ReadProjectStop)
	if err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 50 {
				out := report.Details()
				out.ActiveRefs[0].Kind = StopObjectTransfer
				locks := deps.Locks()
				locks[0].Mode = foundation.Exclusive
				if report.Details().ActiveRefs[0] != ref || !grant.Matches(tx, r, deps) || deps.Locks()[0].Mode != foundation.Shared {
					t.Error("mutable projection escaped immutable contract")
					return
				}
			}
		})
	}
	readers.Wait()
}

func TestProjectStopOpaqueDecodeAndSafeFormatting(t *testing.T) {
	cause := stopCause(t)
	r := stopRequest(t, cause, InspectProjectStopStep)
	deps := stopDependencies(t, r)
	grant, err := NewProjectStopAuthorization(foundation.NewTx(), r, deps, ReadProjectStop)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewObjectStopReport(ObjectStopComponent, cause, ObjectStopDetails{State: ProjectStopped})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{cause, r, grant, report, ProjectStopCause{}, ProjectStopRequest{}, ProjectStopAuthorization{}, ObjectStopReport{}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		logged := value.(slog.LogValuer).LogValue().String()
		for _, formatted := range []string{string(raw), fmt.Sprintf("%v %+v %#v", value, value, value), logged} {
			if strings.Contains(formatted, "01900000") || strings.Contains(formatted, "sha256") || strings.Contains(formatted, "func(") {
				t.Fatal("opaque projection leaked identity", formatted)
			}
		}
	}
	for _, raw := range []string{`null`, `{}`, `"object_project_stop_report"`, `{"ProjectID":"` + stopProjectID + `"}`} {
		for _, target := range []any{&cause, &r, &grant, &report} {
			if json.Unmarshal([]byte(raw), target) == nil {
				t.Fatal("untrusted JSON minted trusted value")
			}
		}
	}
	if cause.Validate() != nil || r.Validate() != nil || grant.Validate() != nil || report.Validate() != nil {
		t.Fatal("rejected decode corrupted prior value")
	}
	if (ProjectStopCause{}).Validate() == nil || (ProjectStopRequest{}).Validate() == nil || (ObjectStopReport{}).Validate() == nil {
		t.Fatal("zero opaque value valid")
	}
}
