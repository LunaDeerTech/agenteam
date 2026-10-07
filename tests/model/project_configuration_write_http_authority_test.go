//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestModelProjectConfigurationWriteHTTPCurrentAuthorityAndFacts(t *testing.T) {
	configurationWriteTop(t)
	v := newConfigurationWriteFixture(t)
	credential := v.create(t, "authority-credential", "authority-private-material")
	create := projectUpdateJSON(t, map[string]any{"input": configurationProviderBody(credential)})
	call := func(method, path, key string, body []byte) systemHTTPResponse {
		return v.command(t, v.ownerBrowser, method, path, key, body)
	}
	first := call("POST", v.base()+"model-providers", "authority-provider", create)
	receipt := configurationWriteReceipt(t, first, "provider.create")
	before := v.snapshot(t)
	for _, b := range []systemHTTPBrowser{v.adminBrowser, v.otherBrowser} {
		v.command(t, b, "POST", v.base()+"model-providers", "outsider", create).problem(t, 404, f.NotFound)
		v.command(t, b, "POST", v.base()+"model-commands/lookup", "authority-provider", projectUpdateJSON(t, map[string]any{"command": "provider.create"})).problem(t, 404, f.NotFound)
	}
	v.sameSnapshot(t, before)
	session := v.login(t, v.ownerBrowser.email)
	if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: session.actor, Key: "configuration-formal-logout"}); e != nil {
		t.Fatal(e)
	}
	// Formal Login retains System response material; that legitimate setup is
	// outside the rejected Model command. Bridge its baseline without allowing
	// any Model row or Model-owned Secret reference to change.
	afterSessionSetup := v.snapshot(t)
	var beforeFields, afterFields map[string]json.RawMessage
	if json.Unmarshal(before, &beforeFields) != nil || json.Unmarshal(afterSessionSetup, &afterFields) != nil || len(beforeFields) != 6 || len(afterFields) != 6 {
		t.Fatal("private setup snapshot shape")
	}
	for _, bucket := range []string{"providers", "models", "commands", "audits", "events"} {
		if len(beforeFields[bucket]) == 0 || !bytes.Equal(beforeFields[bucket], afterFields[bucket]) {
			t.Fatal("account setup changed Model bucket", bucket)
		}
	}
	modelRefs := func(raw json.RawMessage) []json.RawMessage {
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil {
			t.Fatal("private reference snapshot shape")
		}
		var selected []json.RawMessage
		for _, row := range rows {
			var ref struct {
				Consumer string `json:"consumer"`
			}
			if json.Unmarshal(row, &ref) != nil || ref.Consumer == "" {
				t.Fatal("private reference row shape")
			}
			if ref.Consumer == "model" {
				selected = append(selected, row)
			}
		}
		return selected
	}
	beforeRefs, afterRefs := modelRefs(beforeFields["refs"]), modelRefs(afterFields["refs"])
	if len(beforeRefs) != len(afterRefs) {
		t.Fatal("account setup changed Model reference count")
	}
	for i := range beforeRefs {
		if !bytes.Equal(beforeRefs[i], afterRefs[i]) {
			t.Fatal("account setup changed Model reference row")
		}
	}
	before = afterSessionSetup
	v.command(t, session, "POST", v.base()+"model-providers", "authority-provider", create).problem(t, 401, f.SessionRevoked)
	v.sameSnapshot(t, before)
	projectCredentialCanonical(t, v.projectCredentialFixture, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, v.project.ID.String(), v.otherBrowser.actor.Details().UserID)
	call("POST", v.base()+"model-providers", "authority-provider", create).problem(t, 404, f.NotFound)
	call("POST", v.base()+"model-commands/lookup", "authority-provider", projectUpdateJSON(t, map[string]any{"command": "provider.create"})).problem(t, 404, f.NotFound)
	projectCredentialCanonical(t, v.projectCredentialFixture, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, v.project.ID.String(), v.ownerBrowser.actor.Details().UserID)
	v.sameSnapshot(t, before)
	t.Run("cross_project_system", func(t *testing.T) {
		other := v.createProject(t, v.owner)
		system := v.provider(t, mc.OpenAIChat, nil)
		for _, provider := range []string{receipt.ResourceID, system.ID.String()} {
			path := projectConfigurationHTTPPath(other.ID, "models")
			if provider == system.ID.String() {
				path = v.base() + "models"
			}
			v.command(t, v.ownerBrowser, "POST", path, "scope-isolation", projectUpdateJSON(t, map[string]any{"provider_id": provider, "input": configurationModelBody()})).problem(t, 404, f.NotFound)
		}
		v.sameSnapshot(t, before)
	})
	for _, stage := range []string{"secret", "audit", "event", "preparation_lifecycle_change"} {
		t.Run("rollback_"+stage, func(t *testing.T) {
			d := v.deps
			old := v.handler
			defer func() { v.handler = old }()
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
			if stage != "preparation_lifecycle_change" {
				v.installConfiguration(t, v.withDeps(t, d))
				stable := v.snapshot(t)
				call("POST", v.base()+"model-providers", "rollback-"+stage, create).problem(t, 409, f.InvalidState)
				v.sameSnapshot(t, stable)
				return
			}
			// Isolate the real accepted lifecycle operation from the main Project.
			original, scope := v.project, v.scope
			v.project = v.createProject(t, v.owner)
			v.scope, _ = id.InProject(v.project.ID)
			defer func() { v.project, v.scope = original, scope }()
			credential := v.create(t, "preparation-lifecycle-credential", "preparation-lifecycle-private")
			create := projectUpdateJSON(t, map[string]any{"input": configurationProviderBody(credential)})
			gate := &observedEvents{inner: v.events, entered: make(chan struct{}), release: make(chan struct{})}
			d.Events = gate
			v.installConfiguration(t, v.withDeps(t, d))
			var once sync.Once
			release := func() { once.Do(func() { close(gate.release) }) }
			done := make(chan systemHTTPResponse, 1)
			joined := make(chan struct{})
			t.Cleanup(func() { release(); <-joined })
			stable := v.snapshot(t)
			go func() {
				defer close(joined)
				done <- call("POST", v.base()+"model-providers", "gate-before-final", create)
			}()
			waitSignal(t, gate.entered)
			v.gate(t, pc.Archiving)
			release()
			response := <-done
			response.problem(t, 409, f.ProjectNotActive)
			v.sameSnapshot(t, stable)
		})
	}

	t.Run("terminal_project_gate_binding_rollback", func(t *testing.T) {
		stable := v.snapshot(t)
		old := v.handler
		defer func() { v.handler = old }()
		identity := configurationCommandIdentity(t, v.ownerBrowser, v.project.ID, "provider.create", "terminal-project-gate")
		tap := &projectModelEventTap{Appender: v.events}
		reached, originalValid, foreignRejected := false, false, false
		tap.check = func(ctx context.Context, tx f.Tx, actor id.Actor, event ec.Event, plan oc.AppendPlan) error {
			x, e := v.raw.InTx(tx)
			if e != nil {
				t.Fatal(e)
			}
			var complete bool
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.commands c JOIN agenteam_model.providers p ON p.id=c.resource_id AND p.project_id=c.project_id WHERE c.command_identity=$1 AND c.phase='prepared' AND c.safe_receipt IS NULL AND EXISTS(SELECT 1 FROM agenteam_secret.secret_references r WHERE r.credential_id=$2 AND r.consumer='model' AND r.owner_id=p.id) AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.producer='model' AND a.action='provider.create' AND a.project_id=c.project_id AND a.resource_id=p.id) AND NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id))`, identity.Canonical(), credential).Scan(&complete)
			if e != nil || !complete {
				t.Fatal("terminal gate lacks same Tx canonical/ref/Audit/prepared-command proof", e)
			}
			reached = true
			request, e := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: v.project.ID, Actor: actor, Stage: oc.NewFact, Event: event.Summary()})
			if e != nil {
				t.Fatal("original gate request", e)
			}
			deps := plan.Details().Project
			if e = v.projects.ValidateInTx(ctx, tx, request, deps); e != nil {
				t.Fatal("original prepared gate rejected", e)
			}
			originalValid = true
			foreign, e := oc.NewDependencies(oc.NewPlanIssuer(), deps.Binding(), deps.Locks(), deps.Opaque())
			if e != nil {
				t.Fatal("valid foreign issuer construction", e)
			}
			e = v.projects.ValidateInTx(ctx, tx, request, foreign)
			var fault *f.Fault
			if !errors.As(e, &fault) || fault.Code != f.Forbidden {
				t.Fatal("real gate did not reject foreign issuer", e)
			}
			foreignRejected = true
			return e
		}
		d := v.deps
		d.Events = tap
		v.installConfiguration(t, v.withDeps(t, d))
		call("POST", v.base()+"model-providers", "terminal-project-gate", create).problem(t, 403, f.Forbidden)
		if !reached || !originalValid || !foreignRejected {
			t.Fatal("terminal gate proof did not complete")
		}
		v.sameSnapshot(t, stable)
		t.Log("same_tx_prepared_provider_ref_typedAudit=true event_and_receipt_absent=true original_NewFact_gate_valid=true real_foreign_issuer_Forbidden=true full_snapshot_rollback=true")
	})
	t.Run("writer_poison", func(t *testing.T) {
		command := configurationCommandIdentity(t, v.ownerBrowser, v.project.ID, "provider.create", "held-writer")
		lock, _ := f.CommandLock(command)
		release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, lock, f.Exclusive)
		defer release()
		stable := v.snapshot(t)
		r := call("POST", v.base()+"model-providers", "held-writer", create)
		r.problem(t, 500, f.InternalError)
		var problem struct {
			CommitState f.CommitState `json:"commit_state"`
		}
		if json.Unmarshal(r.body, &problem) != nil || problem.CommitState != f.NotCommitted {
			t.Fatal("real writer poison lost NotCommitted")
		}
		v.sameSnapshot(t, stable)
	})
	t.Run("unbound_references_and_empty_replacement", func(t *testing.T) {
		provider := v.projectProvider(t, nil)
		replacement := v.model(t, v.provider(t, mc.OpenAIChat, nil), mc.ChatModel)
		for _, kind := range []string{"agent", "project_summary"} {
			m := v.projectModel(t, provider)
			v.addReference(t, m.ID, kind)
			stable := v.snapshot(t)
			for _, next := range []any{nil, replacement.ID.String()} {
				call("DELETE", v.base()+"models/"+m.ID.String(), "unbound-"+kind, projectUpdateJSON(t, map[string]any{"expected_version": "1", "replacement": next})).problem(t, 503, f.DependencyUnbound)
				v.sameSnapshot(t, stable)
			}
		}
		m := v.projectModel(t, provider)
		r := call("DELETE", v.base()+"models/"+m.ID.String(), "empty-system-replacement", projectUpdateJSON(t, map[string]any{"expected_version": "1", "replacement": replacement.ID.String()}))
		configurationWriteReceipt(t, r, "model.delete")
	})
	t.Run("secret_release_deletion_writer", func(t *testing.T) {
		target := v.create(t, "reference-race-credential", "reference-race-private")
		ref := credentialRefProject(t, v.projectCredentialFixture, target)
		provider := v.projectProvider(t, &ref)
		tap := &projectModelAuditTap{inner: v.aud}
		held, releaseCh := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(releaseCh) }) }
		tap.change = func(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
			x, err := v.raw.InTx(tx)
			if err != nil {
				return ctx, e, k
			}
			var refs int
			if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1`, target).Scan(&refs); err != nil || refs != 0 {
				t.Error("same Tx reference release missing")
			}
			close(held)
			select {
			case <-releaseCh:
			case <-ctx.Done():
			}
			return ctx, e, k
		}
		d := v.deps
		d.Audit = tap
		old := v.handler
		v.installConfiguration(t, v.withDeps(t, d))
		defer func() { v.handler = old }()
		done := make(chan systemHTTPResponse, 1)
		joined := make(chan struct{})
		t.Cleanup(func() { release(); <-joined })
		body := projectUpdateJSON(t, map[string]any{"expected_version": "1", "input": configurationProviderBody(nil)})
		go func() {
			defer close(joined)
			done <- call("PUT", v.base()+"model-providers/"+provider.ID.String(), "release-reference-http", body)
		}()
		waitSignal(t, held)
		v.request(t, v.ownerBrowser, "DELETE", v.collection()+"/"+target, "secret-delete-blocked", projectUpdateJSON(t, map[string]any{"expected_version": "1"})).problem(t, 500, f.InternalError)
		release()
		configurationWriteReceipt(t, <-done, "provider.update")
		v.request(t, v.ownerBrowser, "DELETE", v.collection()+"/"+target, "secret-delete-after", projectUpdateJSON(t, map[string]any{"expected_version": "1"})).want(t, 200)
	})
	for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived} {
		t.Run("six_history_"+string(state), func(t *testing.T) {
			// Separate real Project in the same formal identity chain, with the
			// already accepted fixture's formal lifecycle acceptance setup.
			original, scope := v.project, v.scope
			v.project = v.createProject(t, v.owner)
			v.scope, _ = id.InProject(v.project.ID)
			defer func() { v.project, v.scope = original, scope }()
			a := v.create(t, "archive-first-"+string(state), "archive-first-private")
			b := v.create(t, "archive-second-"+string(state), "archive-second-private")
			history := configurationWriteSix(t, v, "history-"+string(state), a, b, call, false)
			// Existing gate uses formal Archive acceptance; Archived then supplies
			// explicit terminal Authority input, not participant stop execution.
			v.gate(t, state)
			configurationReplayHistory(t, v, history, call)
			stable := v.snapshot(t)
			for _, h := range history {
				call(h.method, h.path, h.key+"-new", h.body).problem(t, 409, f.ProjectNotActive)
			}
			v.sameSnapshot(t, stable)
		})
	}
	t.Log("current Owner/Session/gate and original Read history; Model prepared checker retained; real Secret/Audit/Event/gate failures rolled back full private state; writer poison, missing adapters, reference release/delete serialized")
}
