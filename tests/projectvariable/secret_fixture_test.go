//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The existing fixture supplies real Account and Project creation, including
// its explicitly controlled persistent Skills initialization receipt. This
// additional assembly uses real D10/D04/Audit/Outbox ports, not that fixture's
// ordinary-variable appender. No HTTP/defaultroot production binding is added.
type secretOwnerFixture struct {
	*variableHTTPFixture
	owner   *pv.SecretService
	deps    pv.SecretDependencies
	secrets *secret.Service
}

func newSecretOwnerFixture(t *testing.T) *secretOwnerFixture {
	t.Helper()
	return assembleSecretOwnerFixture(t, newVariableHTTPFixture(t))
}

func assembleSecretOwnerFixture(t *testing.T, base *variableHTTPFixture) *secretOwnerFixture {
	t.Helper()
	store := base.tracked
	nativeFacts, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{
		Sessions: base.accounts, Routes: base.accounts,
		AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{
			ac.ProjectVariableProducer: base.authority, ac.SecretProducer: nativeFacts,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writes, err := pv.NewSecretWriteAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, base.keys, audit.Authorizations{
		Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: projects,
	})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`,
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), base.keys)
	if err != nil {
		t.Fatal(err)
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(store, keys, aud, secret.Authorizations{
		Sessions: base.accounts, System: base.accounts, Projects: projectSecrets,
		Usage: base.accounts, AccountWrites: base.accounts, ProjectVariables: writes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	types, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{vc.SecretVariableProducer: base.authority},
		Sessions:  base.accounts, System: base.accounts, Projects: projects, Audit: aud,
		Cursors: base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	v := &secretOwnerFixture{variableHTTPFixture: base, secrets: secrets,
		deps: pv.SecretDependencies{Authority: base.authority, Writes: writes, Secrets: secrets,
			Projects: projects, Events: box, VariableEvents: types, Audit: aud,
			Activity: base.accounts, Cursors: base.keys}}
	v.owner = v.newOwner(t)
	return v
}

func (v *secretOwnerFixture) newOwner(t *testing.T) *pv.SecretService {
	t.Helper()
	s, err := pv.NewSecret(v.tracked, v.deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("Secret Owner actual Drain", err)
		}
	})
	return s
}

func secretCreateInput(t *testing.T, name string, value []byte) vc.SecretVariableCreate {
	t.Helper()
	material, err := sc.NewSecretMaterial(value)
	if err != nil {
		t.Fatal("owned material", err)
	}
	defer material.Destroy()
	in, err := vc.NewSecretVariableCreate(vc.SecretVariableCreateFields{
		ID: id[i.ProjectVariable](t), Name: name, Description: "safe metadata", Value: material,
	})
	if err != nil {
		t.Fatal("Secret create request", err)
	}
	t.Cleanup(in.Destroy)
	return in
}

func secretUpdateInput(t *testing.T, fields vc.SecretVariableUpdateFields) vc.SecretVariableUpdate {
	t.Helper()
	in, err := vc.NewSecretVariableUpdate(fields)
	if err != nil {
		t.Fatal("Secret update request", err)
	}
	t.Cleanup(in.Destroy)
	return in
}

func secretLookup(t *testing.T, project vc.ProjectID, target vc.VariableID, command vc.SecretCommandName, meta f.CommandMeta) vc.SecretVariableCommandLookupRequest {
	t.Helper()
	q, err := vc.NewSecretVariableCommandLookupRequest(vc.SecretVariableCommandLookupFields{
		ProjectID: project, TargetID: target, Command: command,
		IdempotencyKey: meta.IdempotencyKey, ExpectedVersion: meta.ExpectedVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func sameSecretReceipt(t *testing.T, left, right vc.SecretVariableMutation) {
	t.Helper()
	if !bytes.Equal(jsonBytes(t, left), jsonBytes(t, right)) {
		t.Fatal("original safe Secret receipt changed")
	}
}

// Counts are D10's exact new action/event family plus the D04 receipts for this
// Project, excluding Account setup and ordinary-variable side effects.
func (v *secretOwnerFixture) secretCounts(t *testing.T) [6]int64 {
	t.Helper()
	var n [6]int64
	err := v.raw.QueryRow(ctxFor(t), `SELECT
 coalesce((SELECT query_generation FROM agenteam_projectvariable.secret_project_generations WHERE project_id=$1),1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.secret_variable_changed'),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1)`, v.project.ID.String()).Scan(
		&n[0], &n[1], &n[2], &n[3], &n[4], &n[5])
	if err != nil {
		t.Fatal("Secret fact counts", err)
	}
	return n
}
