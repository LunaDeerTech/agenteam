package contract_test

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func pvRef(t *testing.T) sc.CredentialRef {
	t.Helper()
	scope, err := i.InProject(pvID[i.Project](t, 3))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := sc.NewCredentialRef(pvID[sc.Credential](t, 7), scope)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func pvResult(t *testing.T, kind sc.MutationKind) sc.ProjectVariableWriteResultFields {
	t.Helper()
	r := pvWrite(t, kind)
	v := sc.ProjectVariableWriteResultFields{ReceiptID: pvID[sc.ProjectVariableReceipt](t, 8), ProjectID: r.ProjectID, VariableID: r.VariableID, UserID: pvID[i.User](t, 1), Identity: r.Identity, Kind: kind, ExpectedVersion: r.ExpectedVersion, Ref: pvRef(t), Version: 1, Effect: sc.ProjectVariableCreated}
	if kind == sc.Update {
		v.Effect = sc.ProjectVariableUnchanged
		v.Version = 3
	}
	if kind == sc.Delete {
		v.Effect = sc.ProjectVariableDeleted
		v.Version = 4
		v.Deleted = true
	}
	return v
}

func TestProjectVariablePlanIssuerRequestAndMinimumLocks(t *testing.T) {
	request := pvRequest(t, sc.Create)
	ref := pvRef(t)
	issuer := sc.NewPlanIssuer()
	basis := sc.ProjectVariableWriteBasisFields{Request: request, Ref: ref, Receipt: sc.ProjectVariableWriteNotObserved()}
	locks, err := sc.ProjectVariableWriteLocks(request, ref)
	if err != nil || len(locks) != 5 {
		t.Fatal("minimal lock union")
	}
	plan, err := sc.NewProjectVariableWritePlan(issuer, basis, locks)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Matches(issuer, request) || plan.Matches(sc.NewPlanIssuer(), request) || plan.Matches(sc.PlanIssuer{}, request) {
		t.Fatal("issuer binding")
	}
	for n := range locks {
		missing := append([]f.LockRequest(nil), locks[:n]...)
		missing = append(missing, locks[n+1:]...)
		if _, err := sc.NewProjectVariableWritePlan(issuer, basis, missing); err == nil {
			t.Fatal("missing required lock accepted")
		}
		if locks[n].Mode == f.Exclusive {
			weak := append([]f.LockRequest(nil), locks...)
			weak[n].Mode = f.Shared
			if _, err := sc.NewProjectVariableWritePlan(issuer, basis, weak); err == nil {
				t.Fatal("weakened required lock accepted")
			}
		}
	}
	changes := []func(*sc.ProjectVariableWriteFields){
		func(v *sc.ProjectVariableWriteFields) {
			v.Actor, _ = i.NewHuman(pvID[i.User](t, 1), pvID[i.Session](t, 9))
		},
		func(v *sc.ProjectVariableWriteFields) {
			v.Actor, _ = i.NewHuman(pvID[i.User](t, 10), pvID[i.Session](t, 2))
		},
		func(v *sc.ProjectVariableWriteFields) { v.VariableID = pvID[i.ProjectVariable](t, 11) },
		func(v *sc.ProjectVariableWriteFields) {
			v.Identity, _ = f.NewCommandIdentity("projectvariable", []string{v.ProjectID.String()}, "project.secret_variable.create", "another-key")
		},
	}
	for _, change := range changes {
		v := pvWrite(t, sc.Create)
		change(&v)
		r, err := sc.NewProjectVariableWriteRequest(v)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Matches(issuer, r) {
			t.Fatal("request substitution accepted")
		}
	}
	locks[0].Mode = f.Shared
	copy, err := plan.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	copy[0].Mode = f.Shared
	got, _ := plan.RequiredLocks()
	want, _ := sc.ProjectVariableWriteLocks(request, ref)
	for n := range got {
		if f.CompareLockKeys(got[n].Key, want[n].Key) != 0 || got[n].Mode != want[n].Mode {
			t.Fatal("lock copy aliases input/output")
		}
	}
	if _, err := (sc.ProjectVariableWritePlan{}).RequiredLocks(); err == nil {
		t.Fatal("zero plan returns empty valid locks")
	}
	if json.Unmarshal([]byte(`{}`), &plan) == nil || !plan.Matches(issuer, request) {
		t.Fatal("plan JSON fabricated or overwrote authority")
	}
}

func TestProjectVariableHistoricalBasisAndObservation(t *testing.T) {
	for _, kind := range []sc.MutationKind{sc.Create, sc.Update, sc.Delete} {
		t.Run(string(kind), func(t *testing.T) {
			result := pvResult(t, kind)
			observed, err := sc.NewProjectVariableWriteObservation(result)
			if err != nil {
				t.Fatal(err)
			}
			if result.ExpectedVersion != nil {
				*result.ExpectedVersion = 99
			}
			got, err := observed.Result()
			if err != nil {
				t.Fatal(err)
			}
			if got.ExpectedVersion != nil {
				if *got.ExpectedVersion != 7 {
					t.Fatal("result aliases input")
				}
				*got.ExpectedVersion = 100
				again, _ := observed.Result()
				if *again.ExpectedVersion != 7 {
					t.Fatal("result aliases output")
				}
			}
			request := pvRequest(t, kind)
			basis := sc.ProjectVariableWriteBasisFields{Request: request, Ref: result.Ref, CredentialVersion: result.Version, Receipt: observed}
			locks, err := sc.ProjectVariableWriteLocks(request, result.Ref)
			if err != nil {
				t.Fatal(err)
			}
			issuer := sc.NewPlanIssuer()
			plan, err := sc.NewProjectVariableWritePlan(issuer, basis, locks)
			if err != nil || !plan.Matches(issuer, request) {
				t.Fatal("historical basis requires live canonical")
			}
			for _, change := range []func(*sc.ProjectVariableWriteBasisFields){
				func(v *sc.ProjectVariableWriteBasisFields) { v.VariableVersion = 1 },
				func(v *sc.ProjectVariableWriteBasisFields) { v.CredentialVersion++ },
				func(v *sc.ProjectVariableWriteBasisFields) {
					r := request.Fields()
					r.VariableID = pvID[i.ProjectVariable](t, 99)
					v.Request, _ = sc.NewProjectVariableWriteRequest(r)
				},
			} {
				v := basis
				change(&v)
				if _, err := sc.NewProjectVariableWritePlan(issuer, v, locks); err == nil {
					t.Fatal("wrong historical basis accepted")
				}
			}
		})
	}
	zero := sc.ProjectVariableWriteObservation{}
	none := sc.ProjectVariableWriteNotObserved()
	if zero.Validate() == nil || none.Validate() != nil || none.Observed() {
		t.Fatal("missing and unobserved conflated")
	}
	if _, err := none.Result(); err == nil {
		t.Fatal("unobserved fabricated result")
	}
	if !sc.ProjectVariableReceiptRead.Valid() || !sc.ProjectVariableNewWrite.Valid() || sc.ProjectVariableWriteStage("").Valid() || sc.ProjectVariableWriteStage("allow").Valid() {
		t.Fatal("stage not closed")
	}
}

func TestProjectVariableResultEffectsAndExternalVersionAreDistinct(t *testing.T) {
	valid := pvResult(t, sc.Update)
	valid.Effect = sc.ProjectVariableReplaced
	valid.Version = 2
	if _, err := sc.NewProjectVariableWriteObservation(valid); err != nil {
		t.Fatal("external version7 need not equal internal version2")
	}
	for _, change := range []func(*sc.ProjectVariableWriteResultFields){
		func(v *sc.ProjectVariableWriteResultFields) { v.Effect = sc.ProjectVariableCreated },
		func(v *sc.ProjectVariableWriteResultFields) { v.Effect = "unknown" },
		func(v *sc.ProjectVariableWriteResultFields) { v.Version = 1 },
		func(v *sc.ProjectVariableWriteResultFields) { v.Deleted = true },
		func(v *sc.ProjectVariableWriteResultFields) { v.ExpectedVersion = nil },
		func(v *sc.ProjectVariableWriteResultFields) { v.ReceiptID = sc.ProjectVariableReceiptID{} },
		func(v *sc.ProjectVariableWriteResultFields) { v.ProjectID = pvID[i.Project](t, 9) },
		func(v *sc.ProjectVariableWriteResultFields) {
			v.Identity, _ = f.NewCommandIdentity("secret", []string{v.ProjectID.String()}, "update", "key")
		},
	} {
		v := valid
		change(&v)
		if _, err := sc.NewProjectVariableWriteObservation(v); err == nil {
			t.Fatal("invalid result accepted")
		}
	}
	request := pvRequest(t, sc.Create)
	prep, err := sc.NewProjectVariablePreparation(sc.ProjectVariablePreparationFields{Request: request, ReceiptID: pvID[sc.ProjectVariableReceipt](t, 8), Ref: pvRef(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prep.Fields(); err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(`null`), &prep) == nil {
		t.Fatal("prepared projection deserialized")
	}
	if _, err := (sc.ProjectVariablePreparation{}).Fields(); err == nil {
		t.Fatal("zero preparation accepted")
	}
}
