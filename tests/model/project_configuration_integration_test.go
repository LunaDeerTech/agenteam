//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"strings"
	"sync"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func TestModelProjectCRUDScopeAndCanonicalReceipts(t *testing.T) {
	v := newProjectConfigurationFixture(t)
	ctx := testContext(t)
	r := mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "provider-create"), Input: projectProviderInput()}
	before := v.modelCounts(t)
	for _, actor := range []id.Actor{v.admin, v.human(t, "user")} {
		other := r
		other.Actor = actor
		_, err := v.service.CreateProvider(ctx, other)
		requireCode(t, err, f.NotFound)
		v.unchanged(t, before)
	}
	bad := r
	bad.Input.Protocol = mc.OpenAIEmbeddings
	_, err := v.service.CreateProvider(ctx, bad)
	requireCode(t, err, f.InvalidArgument)
	first, err := v.service.CreateProvider(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := f.ParseID[mc.Provider](first.ResourceID)
	p, err := v.service.GetProjectProvider(ctx, v.owner, v.project.ID, pid)
	if err != nil || !p.Scope.Equal(v.scope) || p.Version != 1 {
		t.Fatal("exact Project Provider", err)
	}
	uid, _ := f.ParseID[id.User](v.owner.Details().UserID)
	r.Actor = v.session(t, uid)
	replayed, err := v.service.CreateProvider(ctx, r)
	if err != nil || replayed != first {
		t.Fatal("stable User receipt after Session renewal", err)
	}
	lookup, err := v.service.LookupCommand(ctx, model.LookupCommandRequest{Meta: r.CommandMeta, Command: "provider.create"})
	if err != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != first {
		t.Fatal("Project lookup", err)
	}
	different := r
	different.Input.Name = "different semantic request"
	_, err = v.service.CreateProvider(ctx, different)
	requireCode(t, err, f.IdempotencyKeyReused)
	otherProject := v.createProject(t, v.owner)
	otherScope, _ := id.InProject(otherProject.ID)
	other := r
	other.Scope = otherScope
	otherReceipt, err := v.service.CreateProvider(ctx, other)
	if err != nil || otherReceipt.ResourceID == first.ResourceID {
		t.Fatal("cross Project key isolation", err)
	}
	otherID, _ := f.ParseID[mc.Provider](otherReceipt.ResourceID)
	_, err = v.service.GetProjectProvider(ctx, v.owner, v.project.ID, otherID)
	requireCode(t, err, f.NotFound)
	system := v.provider(t, mc.OpenAIChat, nil)
	_, err = v.service.GetProjectProvider(ctx, v.owner, v.project.ID, system.ID)
	requireCode(t, err, f.NotFound)
	_, err = v.service.GetProvider(ctx, v.admin, pid)
	requireCode(t, err, f.NotFound)
	for _, provider := range []mc.ProviderID{system.ID, otherID} {
		_, err = v.service.CreateModel(ctx, mc.CreateModelRequest{CommandMeta: v.projectMeta(t, newID[struct{}](t).String()), ProviderID: provider, Input: projectModelInput()})
		requireCode(t, err, f.NotFound)
	}
	changed := p.Input.Clone()
	changed.Protocol = mc.AnthropicMessages
	_, err = v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "immutable-protocol"), ID: p.ID, ExpectedVersion: 1, Input: changed})
	requireCode(t, err, f.InvalidArgument)
	changed = p.Input.Clone()
	changed.Name = "Changed Project Provider"
	update := mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "provider-update"), ID: p.ID, ExpectedVersion: 1, Input: changed}
	updated, err := v.service.UpdateProvider(ctx, update)
	if err != nil || updated.Version != 2 {
		t.Fatal(err)
	}
	stale, err := v.service.UpdateProvider(ctx, update)
	if err != nil || stale != updated {
		t.Fatal("receipt before stale ExpectedVersion", err)
	}
	update.Key = "new-stale-command"
	_, err = v.service.UpdateProvider(ctx, update)
	requireCode(t, err, f.VersionConflict)
	m := v.projectModel(t, p)
	wrongType := projectModelInput()
	wrongType.Type = mc.EmbeddingModel
	_, err = v.service.CreateModel(ctx, mc.CreateModelRequest{CommandMeta: v.projectMeta(t, "not-chat"), ProviderID: p.ID, Input: wrongType})
	requireCode(t, err, f.InvalidArgument)
	changedModel := m.Input.Clone()
	changedModel.Name = "Changed Project model"
	mr, err := v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.projectMeta(t, "model-update"), ID: m.ID, ExpectedVersion: 1, Input: changedModel})
	if err != nil || mr.Version != 2 {
		t.Fatal(err)
	}
	_, err = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.projectMeta(t, "nonempty-provider"), ID: p.ID, ExpectedVersion: 2})
	requireCode(t, err, f.InvalidState)
	_, err = v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.projectMeta(t, "model-delete"), ID: m.ID, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.projectMeta(t, "provider-delete"), ID: p.ID, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.service.GetProjectModel(ctx, v.owner, v.project.ID, m.ID)
	requireCode(t, err, f.NotFound)
	_, err = v.service.GetProjectProvider(ctx, v.owner, v.project.ID, p.ID)
	requireCode(t, err, f.NotFound)
	var commands, audits, events int
	err = v.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_model.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='model' AND project_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='model' AND project_id=$1)`, v.project.ID.String()).Scan(&commands, &audits, &events)
	if err != nil || commands != 6 || audits != 6 || events != 6 {
		t.Fatal("six atomic configuration results", commands, audits, events, err)
	}
}

func (v *projectConfigurationFixture) deleteProjectCredential(t *testing.T, metadata sc.Metadata) error {
	t.Helper()
	command, err := f.NewCommandIdentity("secret", []string{v.project.ID.String(), v.owner.Details().UserID}, string(sc.Delete), f.IdempotencyKey(newID[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: v.owner, Scope: v.scope, Identity: command, Kind: sc.Delete, Purpose: metadata.Purpose, Ref: metadata.CredentialRef, ExpectedVersion: metadata.Version})
	return err
}

type projectSecretReject struct{ sc.UsageOperations }

func (s projectSecretReject) ApplyUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, d sc.UsageDependencies) (sc.UsageResult, error) {
	_, err := s.UsageOperations.ApplyUsageInTx(ctx, tx, r, d)
	if err != nil {
		return sc.UsageResult{}, err
	}
	return sc.UsageResult{}, f.NewFault(f.InvalidState, f.NotStarted)
}

func TestModelProjectSecretReferencesAndAtomicEffects(t *testing.T) {
	v := newProjectConfigurationFixture(t)
	ctx := testContext(t)
	first := v.projectCredential(t, sc.Model)
	second := v.projectCredential(t, sc.Model)
	p := v.projectProvider(t, &first.CredentialRef)
	requireCode(t, v.deleteProjectCredential(t, first), f.ResourceBusy)
	input := p.Input.Clone()
	input.CredentialRef = &second.CredentialRef
	_, err := v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "change-reference"), ID: p.ID, ExpectedVersion: 1, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if err = v.deleteProjectCredential(t, first); err != nil {
		t.Fatal("old exact reference was not released", err)
	}
	requireCode(t, v.deleteProjectCredential(t, second), f.ResourceBusy)
	_, err = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.projectMeta(t, "delete-provider-release"), ID: p.ID, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	var stillPresent bool
	if err = v.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1)`, second.CredentialRef.Details().ID.String()).Scan(&stillPresent); err != nil || !stillPresent {
		t.Fatal("release must keep Secret material", err)
	}
	for _, stage := range []string{"secret", "audit", "event"} {
		t.Run(stage, func(t *testing.T) {
			credential := v.projectCredential(t, sc.Model)
			before := v.modelCounts(t)
			d := v.deps
			switch stage {
			case "secret":
				d.Secret = projectSecretReject{v.secrets}
			case "audit":
				a := &rejectingAudit{inner: v.aud}
				a.enabled.Store(true)
				d.Audit = a
			case "event":
				e := &observedEvents{inner: v.events}
				e.enabled.Store(true)
				d.Events = e
			}
			input := projectProviderInput()
			input.CredentialRef = &credential.CredentialRef
			_, err := v.withDeps(t, d).CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "reject-"+stage), Input: input})
			requireCode(t, err, f.InvalidState)
			v.unchanged(t, before)
			if err = v.deleteProjectCredential(t, credential); err != nil {
				t.Fatal("rolled back reference remained", err)
			}
		})
	}
	wrong := v.projectCredential(t, sc.MCP)
	input = projectProviderInput()
	input.CredentialRef = &wrong.CredentialRef
	before := v.modelCounts(t)
	_, err = v.service.CreateProvider(ctx, mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "wrong-purpose"), Input: input})
	requireCode(t, err, f.Forbidden)
	v.unchanged(t, before)
	system := v.credential(t, sc.Model)
	input.CredentialRef = &system.CredentialRef
	_, err = v.service.CreateProvider(ctx, mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "wrong-scope"), Input: input})
	requireCode(t, err, f.InvalidArgument)
	v.unchanged(t, before)
}

func TestModelProjectSecretReleaseAndDeletionShareRealWriterLock(t *testing.T) {
	v := newProjectConfigurationFixture(t)
	ctx := testContext(t)
	credential := v.projectCredential(t, sc.Model)
	p := v.projectProvider(t, &credential.CredentialRef)
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	tap := &projectModelAuditTap{inner: v.aud}
	tap.change = func(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
		// This callback follows the real canonical update and real Secret
		// reference release, while their complete transaction locks are held.
		x, err := v.raw.InTx(tx)
		if err != nil {
			t.Fatal(err)
		}
		var retained bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1)`, credential.CredentialRef.Details().ID.String()).Scan(&retained); err != nil || retained {
			t.Fatal("release was not executed before barrier", err)
		}
		close(held)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return ctx, entry, key
	}
	d := v.deps
	d.Audit = tap
	input := p.Input.Clone()
	input.CredentialRef = nil
	request := mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "actual-held-reference-release"), ID: p.ID, ExpectedVersion: 1, Input: input}
	service := v.withDeps(t, d)
	done := make(chan error, 1)
	go func() { _, err := service.UpdateProvider(ctx, request); done <- err }()
	waitSignal(t, held)
	requireProjectLockTimeout(t, v.deleteProjectCredential(t, credential))
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("reference release did not join")
	}
	if err := v.deleteProjectCredential(t, credential); err != nil {
		t.Fatal("released exact reference remained", err)
	}
	got, err := v.service.GetProjectProvider(ctx, v.owner, v.project.ID, p.ID)
	if err != nil || got.Version != 2 || got.Input.CredentialRef != nil {
		t.Fatal("release did not commit canonical Provider", err)
	}
}

func TestModelProjectReceiptReadGateAndLegacySystemPlan(t *testing.T) {
	for _, state := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run(string(state), func(t *testing.T) {
			v := newProjectConfigurationFixture(t)
			ctx := testContext(t)
			request := mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "before-gate"), Input: projectProviderInput()}
			receipt, err := v.service.CreateProvider(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			v.gate(t, state)
			before := v.modelCounts(t)
			lookup, lookupErr := v.service.LookupCommand(ctx, model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
			again, replayErr := v.service.CreateProvider(ctx, request)
			if state == c.Deleting {
				requireCode(t, lookupErr, f.ProjectNotActive)
				requireCode(t, replayErr, f.ProjectNotActive)
			} else {
				if lookupErr != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != receipt || replayErr != nil || again != receipt {
					t.Fatal("Read gate must preserve original receipt", lookupErr, replayErr)
				}
			}
			request.Key = "new-after-gate"
			_, err = v.service.CreateProvider(ctx, request)
			requireCode(t, err, f.ProjectNotActive)
			v.unchanged(t, before)
		})
	}
	t.Run("uninitialized", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		v.skills.setMode("pending")
		target := newID[id.Project](t)
		result, err := v.projectService.CreateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(target.String())}, c.CreateProjectRequest{ProjectID: target, Name: "pending-" + target.String()[24:]})
		if err != nil || result.State != c.CreationPending {
			t.Fatal("pending Skills setup", result.State, err)
		}
		scope, _ := id.InProject(target)
		meta := v.projectMeta(t, "pending-config")
		meta.Scope = scope
		before := v.modelCounts(t)
		_, err = v.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: meta, Input: projectProviderInput()})
		requireCode(t, err, f.ProjectNotActive)
		v.unchanged(t, before)
	})
	t.Run("legacy-system", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		ctx := testContext(t)
		request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "legacy-system"), Input: projectProviderInput()}
		receipt, err := v.service.CreateProvider(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		identity, _ := f.NewCommandIdentity("model.system", []string{v.admin.Details().UserID}, "provider.create", request.Key)
		var raw []byte
		var canonical string
		if err = v.raw.QueryRow(ctx, `SELECT mutation_plan,command_identity FROM agenteam_model.commands WHERE command_identity=$1 AND scope='system' AND project_id IS NULL`, identity.Canonical()).Scan(&raw, &canonical); err != nil {
			t.Fatal(err)
		}
		var plan map[string]json.RawMessage
		if err = json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		if _, ok := plan["Project"]; ok || canonical != identity.Canonical() {
			t.Fatal("new code changed legacy System representation")
		}
		v.sql(t, `UPDATE agenteam_model.commands SET mutation_plan=mutation_plan-'Project' WHERE command_identity=$1`, identity.Canonical())
		before := v.modelCounts(t)
		again, err := v.service.CreateProvider(ctx, request)
		if err != nil || again != receipt {
			t.Fatal("legacy scope-less plan receipt", err)
		}
		v.unchanged(t, before)
	})
	t.Run("project-cannot-adopt-scope-less-plan", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		ctx := testContext(t)
		request := mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "lost-project-discriminator"), Input: projectProviderInput()}
		if _, err := v.service.CreateProvider(ctx, request); err != nil {
			t.Fatal(err)
		}
		command, err := f.NewCommandIdentity("model.project", []string{v.project.ID.String(), v.owner.Details().UserID}, "provider.create", request.Key)
		if err != nil {
			t.Fatal(err)
		}
		v.sql(t, `UPDATE agenteam_model.commands SET mutation_plan=mutation_plan-'Project' WHERE command_identity=$1`, command.Canonical())
		before := v.modelCounts(t)
		lookup, err := v.service.LookupCommand(ctx, model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
		requireCode(t, err, f.DependencyUnavailable)
		if lookup.Found || lookup.Receipt != nil {
			t.Fatal("Project row adopted a legacy System plan")
		}
		_, err = v.service.CreateProvider(ctx, request)
		requireCode(t, err, f.DependencyUnavailable)
		v.unchanged(t, before)
	})
}

func TestModelProjectPreparedAuthorizationAndReferenceMapping(t *testing.T) {
	for _, scenario := range []string{"session", "owner", "archiving", "secret-deleted", "reference-added"} {
		t.Run(scenario, func(t *testing.T) {
			v := newProjectConfigurationFixture(t)
			ctx := testContext(t)
			request := mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "prepared-"+scenario), Input: projectProviderInput()}
			var invoke func(*model.Service) error
			var intervene func()
			want := f.SessionRevoked
			switch scenario {
			case "session":
				intervene = func() {
					v.revokeSession(t, v.owner)
				}
			case "owner":
				want = f.NotFound
				next := v.human(t, "user")
				intervene = func() {
					v.sql(t, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, v.project.ID.String(), next.Details().UserID)
				}
			case "archiving":
				want = f.ProjectNotActive
				intervene = func() { v.gate(t, c.Archiving) }
			case "secret-deleted":
				want = f.NotFound
				credential := v.projectCredential(t, sc.Model)
				request.Input.CredentialRef = &credential.CredentialRef
				intervene = func() {
					if err := v.deleteProjectCredential(t, credential); err != nil {
						t.Fatal(err)
					}
				}
			case "reference-added":
				want = f.ResourceBusy
				m := v.projectModel(t, v.projectProvider(t, nil))
				invoke = func(s *model.Service) error {
					_, err := s.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: request.CommandMeta, ID: m.ID, ExpectedVersion: 1})
					return err
				}
				intervene = func() { v.addReference(t, m.ID, "agent") }
			}
			if invoke == nil {
				invoke = func(s *model.Service) error { _, err := s.CreateProvider(ctx, request); return err }
			}
			gate := &observedEvents{inner: v.events, entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(gate.release) }) }
			t.Cleanup(release)
			d := v.deps
			d.Events = gate
			s := v.withDeps(t, d)
			before := v.modelCounts(t)
			done := make(chan error, 1)
			go func() { done <- invoke(s) }()
			waitSignal(t, gate.entered)
			intervene()
			release()
			select {
			case err := <-done:
				requireCode(t, err, want)
			case <-ctx.Done():
				t.Fatal("prepared command did not join")
			}
			v.unchanged(t, before)
		})
	}
}

func (v *projectConfigurationFixture) addReference(t *testing.T, modelID mc.ModelID, kind string) {
	t.Helper()
	owner := newID[struct{}](t).String()
	role := "agent_model"
	if kind == "project_summary" {
		owner = v.project.ID.String()
		role = "meeting_summary"
	}
	result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.SystemConfigLock("model-references")
		if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Shared}}); err != nil {
			return err
		}
		x, err := v.raw.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES($1,$2,$3,$4,$5,1)`, kind, owner, role, v.project.ID.String(), modelID.String())
		return err
	})
	if result.State() != f.Committed {
		t.Fatal(result.Fault())
	}
}

func TestModelProjectUnboundReferencesAndDeleteBarrier(t *testing.T) {
	for _, kind := range []string{"agent", "project_summary"} {
		t.Run(kind, func(t *testing.T) {
			v := newProjectConfigurationFixture(t)
			ctx := testContext(t)
			p := v.projectProvider(t, nil)
			m := v.projectModel(t, p)
			next := v.model(t, v.provider(t, mc.OpenAIChat, nil), mc.ChatModel)
			v.addReference(t, m.ID, kind)
			before := v.modelCounts(t)
			for _, replacement := range []*mc.ModelID{nil, &next.ID} {
				_, err := v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.projectMeta(t, newID[struct{}](t).String()), ID: m.ID, ExpectedVersion: 1, Replacement: replacement})
				requireCode(t, err, f.DependencyUnbound)
				v.unchanged(t, before)
			}
			var exact int
			if err := v.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_model.references WHERE owner_kind=$1 AND model_id=$2 AND owner_version=1`, kind, m.ID.String()).Scan(&exact); err != nil || exact != 1 {
				t.Fatal("unbound canonical owner/index changed", err)
			}
		})
	}
	t.Run("empty-index-system-replacement", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		m := v.projectModel(t, v.projectProvider(t, nil))
		next := v.model(t, v.provider(t, mc.OpenAIChat, nil), mc.ChatModel)
		_, err := v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.projectMeta(t, "empty-replace"), ID: m.ID, ExpectedVersion: 1, Replacement: &next.ID})
		if err != nil {
			t.Fatal(err)
		}
		var affected int
		if err = v.raw.QueryRow(testContext(t), `SELECT (metadata->>'affected_count')::int FROM agenteam_audit.audit_records WHERE producer='model' AND action='model.delete' AND resource_id=$1`, m.ID.String()).Scan(&affected); err != nil || affected != 0 {
			t.Fatal("empty index cannot invent rewrites", err)
		}
	})
	t.Run("shared-builder-excludes-delete", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		ctx := testContext(t)
		m := v.projectModel(t, v.projectProvider(t, nil))
		held, release := make(chan struct{}), make(chan struct{})
		done := make(chan f.CommitResult, 1)
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		t.Cleanup(unblock)
		cause := recoveryCause(t)
		go func() {
			done <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				key, _ := f.SystemConfigLock("model-references")
				if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Shared}}); err != nil {
					return err
				}
				close(held)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		waitSignal(t, held)
		before := v.modelCounts(t)
		_, err := v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.projectMeta(t, "blocked-by-builder"), ID: m.ID, ExpectedVersion: 1})
		requireProjectLockTimeout(t, err)
		v.unchanged(t, before)
		unblock()
		select {
		case result := <-done:
			if result.State() != f.Committed {
				t.Fatal(result.Fault())
			}
		case <-ctx.Done():
			t.Fatal("builder did not join")
		}
	})
}

// Pause only the actual optimistic Project command lookup. SQL and all current
// authorization remain delegated; the row can become committed while the first
// caller still believes discovery was absent.
type projectDiscoveryStore struct {
	*postgres.Store
	target           string
	entered, release chan struct{}
	once             sync.Once
}
type projectDiscoveryRow struct {
	postgres.Row
	store *projectDiscoveryStore
	ctx   context.Context
}

func (r projectDiscoveryRow) Scan(out ...any) error {
	err := r.Row.Scan(out...)
	if errors.Is(err, pgx.ErrNoRows) {
		r.store.once.Do(func() {
			close(r.store.entered)
			select {
			case <-r.store.release:
			case <-r.ctx.Done():
			}
		})
	}
	return err
}
func (s *projectDiscoveryStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	row := s.Store.QueryRow(ctx, q, args...)
	if strings.Contains(q, "FROM agenteam_model.commands") && len(args) > 0 && args[0] == s.target {
		return projectDiscoveryRow{row, s, ctx}
	}
	return row
}

func TestModelProjectPreparationFailureRechecksReadReceipt(t *testing.T) {
	for _, scenario := range []string{"active-update", "active-delete", "archiving-update", "archiving-delete"} {
		t.Run(scenario, func(t *testing.T) {
			operation := strings.Split(scenario, "-")[1]
			v := newProjectConfigurationFixture(t)
			ctx := testContext(t)
			m := v.projectModel(t, v.projectProvider(t, nil))
			meta := v.projectMeta(t, "concurrent-"+operation)
			identity, _ := f.NewCommandIdentity("model.project", []string{v.project.ID.String(), v.owner.Details().UserID}, "model."+operation, meta.Key)
			raw := openStore(t, v.db.Config(t, nil))
			store := &projectDiscoveryStore{Store: raw, target: identity.Canonical(), entered: make(chan struct{}), release: make(chan struct{})}
			other := assembleProjectConfiguration(t, v.db, raw, store)
			var once sync.Once
			release := func() { once.Do(func() { close(store.release) }) }
			t.Cleanup(release)
			invoke := func(s *model.Service) (mc.CommandReceipt, error) {
				if operation == "delete" {
					return s.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: meta, ID: m.ID, ExpectedVersion: 1})
				}
				input := m.Input.Clone()
				input.Name = "concurrent updated"
				return s.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: meta, ID: m.ID, ExpectedVersion: 1, Input: input})
			}
			type response struct {
				receipt mc.CommandReceipt
				err     error
			}
			done := make(chan response, 1)
			go func() { r, e := invoke(other.service); done <- response{r, e} }()
			waitSignal(t, store.entered)
			first, err := invoke(v.service)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(scenario, "archiving") {
				v.gate(t, c.Archiving)
			}
			before := v.modelCounts(t)
			release()
			select {
			case r := <-done:
				if r.err != nil || r.receipt != first {
					t.Fatal("preparation/gate failure hid committed Read receipt", r.err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent replay did not join")
			}
			v.unchanged(t, before)
		})
	}
}

// The typed verifier is reached through the real Appender. Every corruption
// remains a structurally legal entry/key and must fail against the original
// Model private command and actual resulting rows.
type projectModelAuditTap struct {
	inner    ac.Appender
	change   func(context.Context, f.Tx, ac.Entry, ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey)
	captured context.Context
	entry    ac.Entry
	key      ac.AppendKey
	innerErr error
}

type projectModelEventTap struct {
	oc.Appender
	check   func(context.Context, f.Tx, id.Actor, ec.Event, oc.AppendPlan) error
	ctx     context.Context
	actor   id.Actor
	summary ec.Summary
	plan    oc.AppendPlan
}

func (e *projectModelEventTap) AppendEventInTx(ctx context.Context, tx f.Tx, actor id.Actor, event ec.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	e.ctx, e.actor, e.summary, e.plan = ctx, actor, event.Summary(), plan
	if e.check != nil {
		if err := e.check(ctx, tx, actor, event, plan); err != nil {
			return oc.AppendReceipt{}, err
		}
	}
	return e.Appender.AppendEventInTx(ctx, tx, actor, event, plan)
}

func TestModelProjectProducerFactsCannotBeReplayedOrRebound(t *testing.T) {
	for _, scenario := range []string{"summary", "actor", "issuer", "context"} {
		t.Run(scenario, func(t *testing.T) {
			v := newProjectConfigurationFixture(t)
			uid, _ := f.ParseID[id.User](v.owner.Details().UserID)
			renewed := v.session(t, uid)
			tap := &projectModelEventTap{Appender: v.events}
			tap.check = func(ctx context.Context, tx f.Tx, actor id.Actor, event ec.Event, plan oc.AppendPlan) error {
				summary, deps := event.Summary(), plan.Details().Producer
				switch scenario {
				case "summary":
					summary.PayloadDigest = f.Digest("sha256:" + strings.Repeat("f", 64))
				case "actor":
					actor = renewed
				case "issuer":
					var err error
					deps, err = oc.NewDependencies(oc.NewPlanIssuer(), deps.Binding(), deps.Locks(), deps.Opaque())
					if err != nil {
						t.Fatal(err)
					}
				case "context":
					ctx = context.Background()
				}
				err := v.authority.ValidateAppendInTx(ctx, tx, actor, summary, deps, oc.NewFact)
				requireCode(t, err, f.Forbidden)
				return err
			}
			d := v.deps
			d.Events = tap
			before := v.modelCounts(t)
			_, err := v.withDeps(t, d).CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "producer-"+scenario), Input: projectProviderInput()})
			requireCode(t, err, f.Forbidden)
			v.unchanged(t, before)
		})
	}
	t.Run("real-store-lock-and-transaction-boundary", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		tap := &projectModelEventTap{Appender: v.events}
		d := v.deps
		d.Events = tap
		_, err := v.withDeps(t, d).CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "capture-committed-producer"), Input: projectProviderInput()})
		if err != nil || tap.ctx == nil {
			t.Fatal("real prepared producer capture", err)
		}
		before := v.modelCounts(t)
		for _, mode := range []string{"new-transaction", "missing-locks", "foreign-store"} {
			t.Run(mode, func(t *testing.T) {
				store := v.raw
				if mode == "foreign-store" {
					store = openStore(t, v.db.Config(t, nil))
				}
				var callback error
				// The new transaction starts from a fresh context. Only the fact
				// checker receives the captured private command, so Nested cannot
				// accidentally stand in for the boundary under test.
				result := store.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					if mode != "missing-locks" {
						if err := store.AcquireAll(ctx, tx, tap.plan.Locks()); err != nil {
							return err
						}
					}
					callback = v.authority.ValidateAppendInTx(context.WithoutCancel(tap.ctx), tx, tap.actor, tap.summary, tap.plan.Details().Producer, oc.NewFact)
					if mode != "new-transaction" {
						return nil
					} // ignored primitive failure must still poison
					return callback
				})
				if result.State() != f.NotCommitted {
					t.Fatal("rejected fact check did not roll back", result.State())
				}
				if mode == "new-transaction" {
					requireCode(t, callback, f.Forbidden)
					requireCode(t, result.Fault(), f.Forbidden)
				} else {
					requireCode(t, callback, f.DependencyUnavailable)
					want := postgres.LockNotHeld
					if mode == "foreign-store" {
						want = postgres.InvalidTransaction
					}
					if postgres.CodeOf(callback) != want || postgres.CodeOf(result.Fault()) != want {
						t.Fatal("wrong concrete callback/poison boundary", postgres.CodeOf(callback), postgres.CodeOf(result.Fault()))
					}
				}
				v.unchanged(t, before)
			})
		}
	})
}

func (a *projectModelAuditTap) AppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	if ac.ModelAction(e.Fields().Action) {
		a.captured = ctx
		a.entry = e
		a.key = k
		if a.change != nil {
			ctx, e, k = a.change(ctx, tx, e, k)
		}
	}
	r, err := a.inner.AppendInTx(ctx, tx, e, k)
	a.innerErr = err
	return r, err
}

func TestModelProjectAuditPreparedFactsAndProjectEventStages(t *testing.T) {
	for _, scenario := range []string{"cause", "ordinal", "session", "scope", "resource", "version", "fields", "missing-context", "canonical-row"} {
		t.Run(scenario, func(t *testing.T) {
			v := newProjectConfigurationFixture(t)
			ctx := testContext(t)
			uid, _ := f.ParseID[id.User](v.owner.Details().UserID)
			renewed := v.session(t, uid)
			var original mc.ProviderView
			if scenario == "fields" {
				original = v.projectProvider(t, nil)
			}
			tap := &projectModelAuditTap{inner: v.aud}
			tap.change = func(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
				fields := e.Fields()
				var err error
				switch scenario {
				case "cause":
					k, err = ac.NewAppendKey(ac.ModelProducer, "sha256:"+strings.Repeat("f", 64), 0)
				case "ordinal":
					details := k.Details()
					k, err = ac.NewAppendKey(details.Producer, details.CauseRef, 1)
				case "session":
					fields.Actor = renewed
				case "scope":
					fields.Scope = id.SystemScope()
				case "resource":
					resource := newID[mc.Provider](t).String()
					fields.Resource, err = ac.NewResource(ac.ModelProviderResource, resource)
					if err != nil {
						t.Fatal(err)
					}
					metadata, metadataErr := fields.Metadata.ModelFields()
					if metadataErr != nil {
						t.Fatal(metadataErr)
					}
					metadata.ProviderID = resource
					fields.Metadata, err = ac.ModelMetadata(fields.Action, metadata)
				case "version", "fields":
					var metadata ac.ModelMetadataFields
					if err = json.Unmarshal(fields.Metadata.JSON(), &metadata); err != nil {
						t.Fatal(err)
					}
					if scenario == "version" {
						metadata.Version++
					} else {
						metadata.ChangedFields = []string{"enabled"}
					}
					fields.Metadata, err = ac.ModelMetadata(fields.Action, metadata)
				case "missing-context":
					ctx = context.Background()
				case "canonical-row":
					x, xerr := v.raw.InTx(tx)
					if xerr != nil {
						t.Fatal(xerr)
					}
					_, err = x.Exec(ctx, `UPDATE agenteam_model.providers SET version=version+1 WHERE id=$1`, fields.Resource.Details().ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				e, err = ac.NewEntry(fields)
				if err != nil {
					t.Fatal(err)
				}
				return ctx, e, k
			}
			d := v.deps
			d.Audit = tap
			before := v.modelCounts(t)
			service := v.withDeps(t, d)
			var err error
			if scenario == "fields" {
				input := original.Input.Clone()
				input.Name = "actual-name-change"
				_, err = service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "tamper-fields"), ID: original.ID, ExpectedVersion: 1, Input: input})
			} else {
				_, err = service.CreateProvider(ctx, mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "tamper-"+scenario), Input: projectProviderInput()})
			}
			requireCode(t, tap.innerErr, f.Forbidden)
			requireCode(t, err, f.Forbidden)
			v.unchanged(t, before)
			if scenario == "fields" {
				actual, readErr := v.service.GetProjectProvider(ctx, v.owner, v.project.ID, original.ID)
				if readErr != nil {
					t.Fatal("read Provider after rejected update", readErr)
				}
				beforeJSON, beforeErr := json.Marshal(original)
				afterJSON, afterErr := json.Marshal(actual)
				if beforeErr != nil || afterErr != nil || !bytes.Equal(beforeJSON, afterJSON) {
					t.Fatal("rejected typed Audit did not restore the complete Provider view, including input/version/timestamps")
				}
			}
		})
	}
	t.Run("two-stages-and-closed-summary", func(t *testing.T) {
		v := newProjectConfigurationFixture(t)
		ctx := testContext(t)
		p := v.projectProvider(t, nil)
		var rawHeader, rawPayload []byte
		if err := v.raw.QueryRow(ctx, `SELECT header,payload FROM agenteam_outbox.events WHERE producer='model' AND aggregate_id=$1`, p.ID.String()).Scan(&rawHeader, &rawPayload); err != nil {
			t.Fatal(err)
		}
		var header ec.Header
		if err := json.Unmarshal(rawHeader, &header); err != nil {
			t.Fatal(err)
		}
		payloadDigest, err := cursor.Digest(rawPayload)
		if err != nil {
			t.Fatal(err)
		}
		summary := ec.Summary{Producer: model.ModelProducer, Header: header, PayloadDigest: payloadDigest}
		details := oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: v.project.ID, Actor: v.owner, Stage: oc.CurrentAccess, Event: summary}
		request, err := oc.NewProjectRequest(details)
		if err != nil {
			t.Fatal(err)
		}
		deps, err := v.projects.Discover(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		check := func(stage oc.Stage, subject oc.ProjectRequestDetails, authority *project.Authority, locks bool) error {
			subject.Stage = stage
			r, err := oc.NewProjectRequest(subject)
			if err != nil {
				return err
			}
			var callback error
			result := v.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if locks {
					if err := v.raw.AcquireAll(ctx, tx, deps.Locks()); err != nil {
						return err
					}
				}
				callback = authority.ValidateInTx(ctx, tx, r, deps)
				if !locks {
					return nil
				} // ignored missing-lock error must still poison
				return callback
			})
			if callback != nil && result.State() != f.NotCommitted {
				t.Fatal("authorization failure did not roll back")
			}
			if !locks {
				requireCode(t, callback, f.DependencyUnavailable)
				requireCode(t, result.Fault(), f.InternalError)
				if result.State() != f.NotCommitted || postgres.CodeOf(callback) != postgres.LockNotHeld || postgres.CodeOf(result.Fault()) != postgres.LockNotHeld {
					t.Fatal("exact ignored missing-lock callback/poison boundary", callback, result.Fault())
				}
				t.Log("Project callback DependencyUnavailable/LOCK_NOT_HELD; ignored error still NotCommitted InternalError/LOCK_NOT_HELD")
				return callback
			}
			if result.State() != f.Committed {
				return result.Fault()
			}
			return nil
		}
		if err = check(oc.CurrentAccess, details, v.projects, true); err != nil {
			t.Fatal(err)
		}
		if err = check(oc.NewFact, details, v.projects, true); err != nil {
			t.Fatal(err)
		}
		changed := details
		changed.Event.Header.AggregateID, _ = f.ParseID[ec.Aggregate](p.ID.String())
		changed.Event.PayloadDigest = f.Digest("sha256:" + strings.Repeat("f", 64))
		requireCode(t, check(oc.CurrentAccess, changed, v.projects, true), f.Forbidden)
		foreign, err := project.NewAuthority(v.store, project.AuthorityDependencies{Sessions: v.accounts})
		if err != nil {
			t.Fatal(err)
		}
		requireCode(t, check(oc.CurrentAccess, details, foreign, true), f.Forbidden)
		requireCode(t, check(oc.CurrentAccess, details, v.projects, false), f.DependencyUnavailable)
		v.gate(t, c.Archived)
		before := v.modelCounts(t)
		if err = check(oc.CurrentAccess, details, v.projects, true); err != nil {
			t.Fatal(err)
		}
		requireCode(t, check(oc.NewFact, details, v.projects, true), f.ProjectNotActive)
		v.unchanged(t, before)
	})
}
