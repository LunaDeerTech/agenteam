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

func testInitialization() InitializationRequest {
	return InitializationRequest{CreationID: testID[Creation](3), ProjectID: testID[identity.Project](1), InitializationKey: "creation/3"}
}

// This is a discovery mapping fixture, not an initialization-authorized actor.
// It deliberately uses the lifecycle role: discovery alone grants no authority.
func testDiscoveryActor(request InitializationRequest) identity.Actor {
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		panic(err)
	}
	scope, err := identity.InProject(request.ProjectID)
	if err != nil {
		panic(err)
	}
	actor, err := registration.Actor(request.CreationID.String(), scope)
	if err != nil {
		panic(err)
	}
	return actor
}
func testInitializationReceipt() InitializationReceipt {
	return InitializationReceipt{CreationID: testID[Creation](3), ProjectID: testID[identity.Project](1), AddSkillsID: testID[Skill](4), Revision: 1}
}

func TestInitializationClosedSuccessAndFailureVariants(t *testing.T) {
	request := testInitialization()
	receipt := testInitializationReceipt()
	valid := []InitializationResult{{State: InitializationResultPending, CreationID: request.CreationID, ProjectID: request.ProjectID}, {State: InitializationFailed, CreationID: request.CreationID, ProjectID: request.ProjectID, SafeReason: ReasonDependencyUnbound}, {State: InitializationCompleted, CreationID: request.CreationID, ProjectID: request.ProjectID, AddSkillsID: ptr(receipt.AddSkillsID), Revision: ptr(receipt.Revision)}}
	for _, result := range valid {
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var restored InitializationResult
		if json.Unmarshal(raw, &restored) != nil || !restored.Matches(request) {
			t.Fatal("valid initialization result")
		}
	}
	for _, mutate := range []func(*InitializationResult){func(r *InitializationResult) { r.AddSkillsID = nil }, func(r *InitializationResult) { r.Revision = nil }, func(r *InitializationResult) { r.Revision = ptr(foundation.Revision(0)) }, func(r *InitializationResult) { r.SafeReason = ReasonWorkPending }, func(r *InitializationResult) { r.State = InitializationResultPending }, func(r *InitializationResult) { r.State = "ready" }} {
		value := valid[2]
		mutate(&value)
		if value.Validate() == nil {
			t.Fatal("incomplete/false initialization success")
		}
	}
	failure := valid[1]
	failure.SafeReason = ""
	if failure.Validate() == nil {
		t.Fatal("unclassified failure")
	}
	failure.SafeReason = "raw provider response"
	if failure.Validate() == nil {
		t.Fatal("unsafe provider reason")
	}
	for _, raw := range []string{`{"state":"pending","creation_id":"01960000-0000-7000-8000-000000000003","project_id":"01960000-0000-7000-8000-000000000001","add_skills_id":null}`, `{"state":"completed","creation_id":"01960000-0000-7000-8000-000000000003","project_id":"01960000-0000-7000-8000-000000000001"}`} {
		var result InitializationResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			t.Fatal("invalid provider wire accepted")
		}
	}
	request.InitializationKey = "other"
	if !receipt.Matches(request) {
		t.Fatal("receipt shape unexpectedly pretends to certify key; plan must bind it")
	}
	request.ProjectID = testID[identity.Project](9)
	if receipt.Matches(request) {
		t.Fatal("cross-project receipt accepted")
	}
}

func TestPlanIssuerRequestActorAndLockBinding(t *testing.T) {
	request := testInitialization()
	actor := testDiscoveryActor(request)
	receipt := testInitializationReceipt()
	issuer := NewInitializationPlanIssuer()
	projectLock, _ := foundation.ProjectLock(request.ProjectID.String())
	skillLock, _ := foundation.AggregateLock(foundation.SkillAggregate, receipt.AddSkillsID.String())
	locks := []foundation.LockRequest{{Key: skillLock, Mode: foundation.Exclusive}, {Key: projectLock, Mode: foundation.Shared}, {Key: projectLock, Mode: foundation.Exclusive}}
	plan, err := issuer.Plan(actor, request, receipt, locks)
	if err != nil {
		t.Fatal(err)
	}
	if !issuer.Matches(plan, actor, request) || NewInitializationPlanIssuer().Matches(plan, actor, request) || (InitializationPlanIssuer{}).Matches(plan, actor, request) {
		t.Fatal("issuer mapping")
	}
	changed := request
	changed.InitializationKey = "other-key"
	if issuer.Matches(plan, actor, changed) {
		t.Fatal("key omitted from mapping")
	}
	changed = request
	changed.CreationID = testID[Creation](9)
	if issuer.Matches(plan, testDiscoveryActor(changed), changed) {
		t.Fatal("creation/cause omitted from mapping")
	}
	if issuer.Matches(plan, testHuman(2, 10), request) {
		t.Fatal("actor omitted from mapping")
	}
	normalized := plan.RequiredLocks()
	if len(normalized) != 2 || normalized[0].Key.Canonical() != projectLock.Canonical() || normalized[0].Mode != foundation.Exclusive || normalized[1].Key.Canonical() != skillLock.Canonical() {
		t.Fatal("complete strongest-mode ordered lock union")
	}
	locks[2].Mode = foundation.Shared
	normalized[0].Mode = foundation.Shared
	if plan.RequiredLocks()[0].Mode != foundation.Exclusive {
		t.Fatal("plan lock alias")
	}
	proposed := plan.ProposedReceipt()
	proposed.Revision = 99
	if plan.ProposedReceipt().Revision != 1 {
		t.Fatal("plan receipt alias")
	}
	for _, locks := range [][]foundation.LockRequest{nil, {{Key: projectLock, Mode: foundation.Shared}}, {{Key: projectLock, Mode: "invalid"}}, {{Key: foundation.LockKey{}, Mode: foundation.Exclusive}}} {
		if _, err := issuer.Plan(actor, request, receipt, locks); err == nil {
			t.Fatal("invalid or incomplete locks accepted")
		}
	}
	if _, err := issuer.Plan(testHuman(2, 10), request, receipt, plan.RequiredLocks()); err == nil {
		t.Fatal("Human masqueraded as service discovery")
	}
	foreign := receipt
	foreign.ProjectID = testID[identity.Project](9)
	if _, err := issuer.Plan(actor, request, foreign, plan.RequiredLocks()); err == nil {
		t.Fatal("foreign Skill mapping accepted")
	}
	registration, err := identity.RegisterService(identity.ProjectInitialization)
	if err != nil {
		t.Fatal("B02 initialization role missing", err)
	}
	scope, _ := identity.InProject(request.ProjectID)
	initializationActor, err := registration.Actor(request.CreationID.String(), scope)
	if err != nil || initializationActor.Details().ServiceName != identity.ProjectInitialization {
		t.Fatal("initialization identity binding", err)
	}
	if _, err = issuer.Plan(initializationActor, request, receipt, plan.RequiredLocks()); err != nil {
		t.Fatal("registered initialization discovery", err)
	}
	wrongCause, _ := registration.Actor(testID[Creation](9).String(), scope)
	if _, err = issuer.Plan(wrongCause, request, receipt, plan.RequiredLocks()); err == nil {
		t.Fatal("wrong initialization cause accepted")
	}
	if _, err = identity.RegisterService("project-initialization-unknown"); err == nil {
		t.Fatal("initialization role closed set widened")
	}
}

func TestOpaquePlanEncodingAndConcurrentCopies(t *testing.T) {
	request := testInitialization()
	actor := testDiscoveryActor(request)
	issuer := NewInitializationPlanIssuer()
	gate, _ := foundation.ProjectLock(request.ProjectID.String())
	plan, err := issuer.Plan(actor, request, testInitializationReceipt(), []foundation.LockRequest{{Key: gate, Mode: foundation.Exclusive}})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []any{&plan, &issuer} {
		if json.Unmarshal([]byte(`{"completed":true}`), target) == nil {
			t.Fatal("JSON minted plan/issuer")
		}
		raw, err := json.Marshal(target)
		if err != nil || strings.Contains(string(raw), request.InitializationKey.String()) || strings.Contains(string(raw), request.ProjectID.String()) {
			t.Fatal("opaque plan leaked request")
		}
	}
	text := fmt.Sprintf("%#v", struct {
		plan InitializationConfirmationPlan
	}{plan})
	if strings.Contains(text, "creation/3") || strings.Contains(text, request.ProjectID.String()) {
		t.Fatal("nested plan formatting leaked")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				locks := plan.RequiredLocks()
				locks[0].Mode = foundation.Shared
				if !issuer.Matches(plan, actor, request) {
					t.Error("immutable plan lost binding")
				}
			}
		})
	}
	wg.Wait()
}
