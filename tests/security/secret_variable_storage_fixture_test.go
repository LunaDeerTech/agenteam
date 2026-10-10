//go:build integration

package security_test

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This controlled D10 boundary is test-only. It checks persisted audit_fixture
// Session/Project gates and the actual Store's live Tx/locks, but supplies the
// prepared mapping/version from test inputs. It cannot prove D10 canonical,
// history/completed receipts, shared names or production Owner authorization.
// Secret, Audit, their private fact checker, Migrator and SQL are real services.
type secretVariableStorageAuthority struct {
	store  *postgres.Store
	ports  *secretProjectAuditPorts
	issuer sc.PlanIssuer
}

func (a *secretVariableStorageAuthority) Discover(context.Context, sc.ProjectVariableWriteRequest) (sc.ProjectVariableWritePlan, error) {
	return sc.ProjectVariableWritePlan{}, deny(f.DependencyUnbound)
}
func (a *secretVariableStorageAuthority) CheckPlan(request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan) error {
	if !plan.Matches(a.issuer, request) {
		return deny(f.InvalidArgument)
	}
	return nil
}
func (a *secretVariableStorageAuthority) CheckInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	if err := a.CheckPlan(request, plan); err != nil {
		return err
	}
	if _, err := a.store.InTx(tx); err != nil {
		return err
	}
	locks, err := plan.RequiredLocks()
	if err != nil {
		return err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return err
	}
	intent := i.Read
	if stage == sc.ProjectVariableNewWrite {
		intent = i.Mutate
	} else if stage != sc.ProjectVariableReceiptRead {
		return deny(f.InvalidArgument)
	}
	r := request.Fields()
	_, err = a.ports.AuthorizeProject(ctx, tx, r.Actor, r.ProjectID, intent)
	return err
}

type secretVariableStorageFixture struct {
	*secretFixture
	ports     *secretProjectAuditPorts
	authority *secretVariableStorageAuthority
	variable  i.ProjectVariableID
}

func newSecretVariableStorageFixture(t *testing.T) *secretVariableStorageFixture {
	t.Helper()
	base := newSecretFixture(t)
	checker, err := secret.NewProjectAuditAuthority(base.store)
	if err != nil {
		t.Fatal(err)
	}
	ports := &secretProjectAuditPorts{secretAuthority: base.usage, checker: checker}
	aud, err := audit.New(base.store, auditKeys(t), audit.Authorizations{Sessions: ports, System: ports, Projects: &secretProjectAuditAppendPorts{ports}})
	if err != nil {
		t.Fatal(err)
	}
	authority := &secretVariableStorageAuthority{store: base.store, ports: ports, issuer: sc.NewPlanIssuer()}
	s, err := secret.New(base.store, masterKeys(t, 1, 1), aud, secret.Authorizations{Sessions: ports, System: ports, Projects: base.usage, ProjectVariables: authority})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopMaintenance)
	base.secret = s
	base.service = aud
	return &secretVariableStorageFixture{secretFixture: base, ports: ports, authority: authority, variable: newID[i.ProjectVariable](t)}
}
func (v *secretVariableStorageFixture) intent(t *testing.T, kind sc.MutationKind, key string, expected f.Version, value *string) sc.ProjectVariableIntent {
	t.Helper()
	command, err := pv.SecretVariableCommandIdentity(v.project, pv.SecretCommandName("project.secret_variable."+string(kind)), f.IdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	r := sc.ProjectVariableWriteFields{Actor: v.actor, ProjectID: v.project, VariableID: v.variable, Identity: command, Kind: kind}
	if kind != sc.Create {
		r.ExpectedVersion = &expected
	}
	request, err := sc.NewProjectVariableWriteRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	name, description := "TOKEN", "storage integration fixture"
	fields := sc.ProjectVariableIntentFields{Request: request}
	if kind != sc.Delete {
		fields.Description = &description
	}
	if kind == sc.Create {
		fields.Name = &name
	}
	if value != nil {
		material, err := sc.NewSecretMaterial([]byte(*value))
		if err != nil {
			t.Fatal(err)
		}
		defer material.Destroy()
		fields.Value = &material
	}
	intent, err := sc.NewProjectVariableIntent(fields)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(intent.Destroy)
	return intent
}
func (v *secretVariableStorageFixture) prepare(t *testing.T, intent sc.ProjectVariableIntent, previous sc.ProjectVariableWriteObservation, history bool) sc.PreparedProjectVariableWrite {
	t.Helper()
	r := intent.Fields().Request.Fields()
	basis := sc.ProjectVariableWriteBasisFields{Request: intent.Fields().Request, Receipt: sc.ProjectVariableWriteNotObserved()}
	if previous.Observed() {
		result, err := previous.Result()
		if err != nil {
			t.Fatal(err)
		}
		basis.Ref, basis.CredentialVersion = result.Ref, result.Version
		if history {
			basis.Receipt = previous
		} else {
			basis.VariableVersion = *r.ExpectedVersion
		}
	} else {
		if r.Kind != sc.Create {
			t.Fatal("new fixture update needs an actual prior result")
		}
		basis.Ref, _ = sc.NewCredentialRef(newID[sc.Credential](t), v.scope)
	}
	locks, err := sc.ProjectVariableWriteLocks(basis.Request, basis.Ref)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := sc.NewProjectVariableWritePlan(v.authority.issuer, basis, locks)
	if err != nil {
		t.Fatal(err)
	}
	p, err := v.secret.PrepareProjectVariableWrite(auditContext(t), intent, plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Destroy)
	return p
}
func (v *secretVariableStorageFixture) apply(t *testing.T, p sc.PreparedProjectVariableWrite, after func(context.Context, f.Tx) error) (sc.ProjectVariableWriteObservation, f.CommitResult) {
	t.Helper()
	safe, err := p.Preparation()
	if err != nil {
		t.Fatal(err)
	}
	fields, err := safe.Fields()
	if err != nil {
		t.Fatal(err)
	}
	locks, err := p.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(fields.Request.Fields().Identity)
	if err != nil {
		t.Fatal(err)
	}
	var result sc.ProjectVariableWriteObservation
	commit := v.store.WithinTx(auditContext(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		var err error
		result, err = v.secret.ApplyProjectVariableWriteInTx(ctx, tx, p)
		if err != nil {
			return err
		}
		if after != nil {
			return after(ctx, tx)
		}
		return nil
	})
	if commit.State() != f.Committed {
		result = sc.ProjectVariableWriteObservation{}
	}
	return result, commit
}

type secretVariableStorageCounts struct{ values, receipts, payloads, audits int }

func (v *secretVariableStorageFixture) counts(t *testing.T) secretVariableStorageCounts {
	t.Helper()
	var c secretVariableStorageCounts
	err := v.store.QueryRow(auditContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.secret_payloads WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='secret')`, v.project.String()).Scan(&c.values, &c.receipts, &c.payloads, &c.audits)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
