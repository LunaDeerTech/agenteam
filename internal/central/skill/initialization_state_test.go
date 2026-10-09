package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func stateID[T any](n int) f.ID[T] {
	v, e := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func stateRow(t *testing.T) initializationRow {
	t.Helper()
	bundle, e := AddSkills(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	b, e := freezeBundle(bundle)
	if e != nil {
		t.Fatal(e)
	}
	request := pc.InitializationRequest{CreationID: stateID[pc.Creation](1), ProjectID: stateID[id.Project](2), InitializationKey: "initialization-private-canary"}
	digest, e := initializationSemantic(request, b)
	if e != nil {
		t.Fatal(e)
	}
	at, _ := f.ParseInstant("2026-10-09T01:02:03Z")
	return initializationRow{request: request, skill: stateID[pc.Skill](3), revision: stateID[sc.Revision](4), bundle: b, semantic: digest, phase: initializationPlanned, version: 1, created: at, updated: at}
}
func TestInitializationOriginalContentAndClosedStates(t *testing.T) {
	base := stateRow(t)
	if e := base.validate(); e != nil {
		t.Fatal(e)
	}
	for name, edit := range map[string]func(*initializationRow){
		"bundle_changed":       func(r *initializationRow) { r.bundle.id += ".other" },
		"description_changed":  func(r *initializationRow) { r.bundle.description += " altered" },
		"digest_changed":       func(r *initializationRow) { r.semantic = f.Digest("sha256:" + strings.Repeat("a", 64)) },
		"planned_attempt":      func(r *initializationRow) { r.attempt = stateID[oc.Attempt](6) },
		"reserved_no_attempt":  func(r *initializationRow) { r.phase = initializationReserved },
		"published_no_attempt": func(r *initializationRow) { r.phase = initializationPublished },
		"failed_no_reason":     func(r *initializationRow) { r.phase = initializationFailed },
		"unknown_phase":        func(r *initializationRow) { r.phase = "completed" },
		"bad_parent":           func(r *initializationRow) { r.request.ProjectID = id.ProjectID{} },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			edit(&r)
			if r.validate() == nil {
				t.Fatal("corrupt durable facts accepted")
			}
		})
	}
	for _, phase := range []initializationPhase{initializationReserved, initializationPublished} {
		r := base
		r.phase = phase
		r.attempt = stateID[oc.Attempt](6)
		r.object = stateID[oc.StoredObject](7)
		r.upload = stateID[oc.Upload](8)
		if e := r.validate(); e != nil {
			t.Fatal(e)
		}
	}
	r := base
	r.phase = initializationFailed
	r.reason = pc.SafeReason("outcome_unknown")
	if e := r.validate(); e != nil {
		t.Fatal(e)
	}
	changed := base.request
	changed.InitializationKey = "other-key"
	var known *f.Fault
	if e := base.matches(changed, base.bundle); !errors.As(e, &known) || known.Code != f.IdempotencyKeyReused {
		t.Fatal("original key changed")
	}
	if strings.Contains(fmt.Sprintf("%+v", base), "private-canary") {
		t.Fatal("unsafe format")
	}
}
func TestInitializationRealParentLockAndActorShape(t *testing.T) {
	r := stateRow(t)
	o, e := r.owner()
	if e != nil || o.Details().ID != r.revision.String() {
		t.Fatal("revision owner")
	}
	locks, e := r.locks(f.Exclusive, stateID[oc.StoredObject](7))
	if e != nil || len(locks) != 4 {
		t.Fatal("lock union", e)
	}
	expected := skillLock(r.skill, f.Exclusive)
	wrong := skillLock(sc.SkillID(r.revision), f.Exclusive)
	found := false
	for n, l := range locks {
		if n > 0 && f.CompareLockKeys(locks[n-1].Key, l.Key) >= 0 {
			t.Fatal("unordered union")
		}
		if l.Key.Canonical() == expected.Key.Canonical() {
			found = true
		}
		if l.Key.Canonical() == wrong.Key.Canonical() {
			t.Fatal("revision mistaken for skill parent")
		}
	}
	if !found {
		t.Fatal("missing real parent")
	}
	registration, _ := id.RegisterService(id.ProjectInitialization)
	scope, _ := id.InProject(r.request.ProjectID)
	actor, e := registration.Actor(r.request.CreationID.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	if e = initializationActor(actor, r.request); e != nil {
		t.Fatal(e)
	}
	human, _ := id.NewHuman(stateID[id.User](9), stateID[id.Session](10))
	if initializationActor(human, r.request) == nil {
		t.Fatal("human initialization")
	}
	wrongRequest := r.request
	wrongRequest.CreationID = stateID[pc.Creation](8)
	if initializationActor(actor, wrongRequest) == nil {
		t.Fatal("wrong creation")
	}
}
func TestInitializationUnknownKeepsOriginalAttempt(t *testing.T) {
	r := stateRow(t)
	command, e := initializationIdentity(r.request)
	if e != nil {
		t.Fatal(e)
	}
	cause, _ := f.NewCommandsCause(command)
	attempt := stateID[f.TransactionAttempt](10)
	original := f.UnknownResult(attempt, cause)
	err := commitError(original)
	var known *f.Fault
	if !errors.As(err, &known) || known.CommitState != f.Unknown || known.Code != f.CommitUnknown || known.RetryHint != "lookup" {
		t.Fatal("lost Unknown state")
	}
	recovered, ok := UnknownAttempt(err)
	if !ok || recovered.AttemptID() != attempt || recovered.Cause().Details().Primary.Canonical() != command.Canonical() {
		t.Fatal("lost original provenance")
	}
	if portError(err) != err {
		t.Fatal("replaced original error")
	}
	for _, v := range []any{err, struct{ Err error }{err}} {
		if strings.Contains(fmt.Sprint(v), "private-canary") {
			t.Fatal("unsafe cause")
		}
	}
	if e = commitError(f.CommittedResult()); e != nil {
		t.Fatal(e)
	}
}
