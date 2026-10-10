//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

// Only the already disclosed persistent test Skills initializer is substituted.
// Account identities, current Owner, Model configuration, Secret writes and both
// metadata ports use their real services and one Store. No Agent is created.
type agentMetadataFixture struct {
	*secretOwnerFixture
	models    *model.Service
	selection *model.AgentConfiguration
	directory *pv.SecretDirectory
}

func newAgentMetadataFixture(t *testing.T) *agentMetadataFixture {
	t.Helper()
	v := &agentMetadataFixture{secretOwnerFixture: newSecretOwnerFixture(t)}
	store := v.tracked
	ma, err := model.NewAuthority(store, model.Authorizations{Sessions: v.accounts, System: v.accounts, Projects: v.projectAuthority})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, v.keys, audit.Authorizations{Sessions: v.accounts, System: v.accounts, Accounts: v.accounts, Projects: v.projectAuthority, Models: ma})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(ma, v.accounts)
	if err != nil {
		t.Fatal(err)
	}
	projectSecrets, err := project.NewSecretAuthority(v.projectAuthority)
	if err != nil {
		t.Fatal(err)
	}
	// Same fixed, task-owned test key as the existing fixture; no external
	// credential input and no Model credential or provider request is created.
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), v.keys)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(store, keys, aud, secret.Authorizations{Sessions: v.accounts, System: v.accounts, Projects: projectSecrets, Usage: usage, AccountWrites: v.accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	types, err := model.DefineEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{model.ModelProducer: ma}, Sessions: v.accounts, System: v.accounts, Projects: v.projectAuthority, Audit: aud, Cursors: v.keys, Processes: fixtureProcess{id[oc.Process](t)}})
	if err != nil {
		t.Fatal(err)
	}
	v.models, err = model.New(store, ma, model.Dependencies{Secret: secrets, Audit: aud, Events: box, ConfigurationEvents: types, Cursors: v.keys})
	if err != nil {
		t.Fatal(err)
	}
	if err = v.models.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	v.selection, err = model.NewAgentConfiguration(store, ma)
	if err != nil {
		t.Fatal(err)
	}
	v.directory, err = pv.NewSecretDirectory(store, v.projectAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.raw.Exec(ctxFor(t), `CREATE TABLE variable_fixture.agent_metadata_probe (id uuid PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *agentMetadataFixture) createModel(t *testing.T, system bool) mc.ModelView {
	t.Helper()
	a, scope := v.ownerBrowser.actor, func() i.Scope { s, _ := i.InProject(v.project.ID); return s }()
	if system {
		a, scope = v.adminBrowser.actor, i.SystemScope()
	}
	meta := func() mc.CommandMeta {
		return mc.CommandMeta{Actor: a, Scope: scope, Key: f.IdempotencyKey(id[struct{}](t).String())}
	}
	provider, err := v.models.CreateProvider(ctxFor(t), mc.CreateProviderRequest{CommandMeta: meta(), Input: mc.ProviderInput{Name: "metadata-provider", Protocol: mc.OpenAIChat, BaseURL: "https://metadata-provider.example/v1", Enabled: true, Options: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatal("formal Model Provider create", err)
	}
	pid, err := f.ParseID[mc.Provider](provider.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := v.models.CreateModel(ctxFor(t), mc.CreateModelRequest{CommandMeta: meta(), ProviderID: pid, Input: mc.ModelInput{Name: "metadata-chat", ProviderModelID: "metadata-chat", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Reasoning: true, ReasoningEfforts: []string{"medium"}}}})
	if err != nil {
		t.Fatal("formal Model create", err)
	}
	mid, err := f.ParseID[mc.Model](created.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	var view mc.ModelView
	if system {
		view, err = v.models.GetModel(ctxFor(t), a, mid)
	} else {
		view, err = v.models.GetProjectModel(ctxFor(t), a, v.project.ID, mid)
	}
	if err != nil || view.Version != 1 {
		t.Fatal("formal Model current view", err)
	}
	return view
}

type agentMetadataPlan struct {
	modelRequest  mc.ConfigurationSelectionRequest
	modelPlan     mc.ConfigurationSelectionPlan
	secretRequest vc.SecretDirectoryRequest
	secretPlan    vc.SecretDirectoryPlan
}

func (v *agentMetadataFixture) discover(t *testing.T, actor i.Actor, modelID mc.ModelID, purpose mc.ConfigurationPurpose, ids []vc.VariableID) agentMetadataPlan {
	t.Helper()
	command, err := f.NewCommandIdentity("project", []string{v.project.ID.String()}, "agent.create", f.IdempotencyKey(id[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b vc.VariableID) int { return bytes.Compare([]byte(a.String()), []byte(b.String())) })
	p := agentMetadataPlan{
		modelRequest:  mc.ConfigurationSelectionRequest{Actor: actor, ProjectID: v.project.ID, Command: command, ModelID: modelID, Purpose: purpose},
		secretRequest: vc.SecretDirectoryRequest{Actor: actor, ProjectID: v.project.ID, Command: command, IDs: ids},
	}
	if purpose == mc.AgentModelConfiguration {
		effort := "medium"
		p.modelRequest.ReasoningEffort = &effort
	}
	p.modelPlan, err = v.selection.DiscoverConfigurationSelection(ctxFor(t), p.modelRequest)
	if err != nil {
		t.Fatal("Model Discover", err)
	}
	p.secretPlan, err = v.directory.DiscoverSecretVariables(ctxFor(t), p.secretRequest)
	if err != nil {
		t.Fatal("Secret Discover", err)
	}
	return p
}

// The consumer performs the sole lock acquisition before either final port.
// Final checks are synchronous calls on the original caller-owned live Tx.
func (v *agentMetadataFixture) inTx(t *testing.T, p agentMetadataPlan, body func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
	t.Helper()
	locks, err := oc.NormalizeLocks(append(p.modelPlan.RequiredLocks(), p.secretPlan.RequiredLocks()...))
	if err != nil {
		t.Fatal(err)
	}
	commandCause, err := f.NewCommandsCause(p.modelRequest.Command)
	if err != nil {
		t.Fatal(err)
	}
	return v.tracked.WithinTx(ctxFor(t), commandCause, func(ctx context.Context, tx f.Tx) error {
		if err := v.tracked.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		return body(ctx, tx, x)
	})
}

func (v *agentMetadataFixture) require(ctx context.Context, tx f.Tx, p agentMetadataPlan) (mc.ConfigurationSelectionFacts, vc.SecretDirectoryFacts, error) {
	m, err := v.selection.RequireConfigurationSelectionInTx(ctx, tx, p.modelRequest, p.modelPlan)
	if err != nil {
		return mc.ConfigurationSelectionFacts{}, vc.SecretDirectoryFacts{}, err
	}
	s, err := v.directory.RequireSecretVariablesInTx(ctx, tx, p.secretRequest, p.secretPlan)
	return m, s, err
}

// Compare actual persistent side effects, not only returned metadata. No read
// should publish references, runtime state, commands, Audit or Outbox facts.
func (v *agentMetadataFixture) counts(t *testing.T) [12]int64 {
	t.Helper()
	var n [12]int64
	err := v.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_model.references),
 (SELECT count(*) FROM agenteam_projectvariable.secret_references),
 (SELECT count(*) FROM agenteam_model.snapshots),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings),
 (SELECT count(*) FROM agenteam_model.invocations),
 (SELECT count(*) FROM agenteam_secret.secret_leases),
 (SELECT count(*) FROM agenteam_model.providers),
 (SELECT count(*) FROM agenteam_model.models),
 (SELECT count(*) FROM agenteam_model.commands),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands),
 (SELECT count(*) FROM agenteam_audit.audit_records),
 (SELECT count(*) FROM agenteam_outbox.events)`).Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5], &n[6], &n[7], &n[8], &n[9], &n[10], &n[11])
	if err != nil {
		t.Fatal("metadata durable counts", err)
	}
	return n
}

func TestAgentConfigurationMetadata(t *testing.T) {
	v := newAgentMetadataFixture(t)
	a, projectID := v.ownerBrowser.actor, v.project.ID
	systemModel, projectModel := v.createModel(t, true), v.createModel(t, false)
	create := func(projectID vc.ProjectID, name string) vc.SecretVariable {
		in := secretCreateInput(t, name, []byte("task-owned-metadata-secret"))
		r, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), projectID, in)
		if err != nil {
			t.Fatal("formal Secret seed", err)
		}
		return r.Fields().Variable
	}
	valid := create(projectID, "METADATA_VALID")
	removed := create(projectID, "METADATA_REMOVED")
	version := removed.Fields().Version
	if _, err := v.owner.DeleteSecretVariable(ctxFor(t), a, meta(t, "metadata-delete", &version), projectID, removed.Fields().ID); err != nil {
		t.Fatal("formal Secret delete", err)
	}
	foreignProject, _, _ := v.createProject(t, a, "metadata-foreign")
	foreign := create(foreignProject.ID, "METADATA_FOREIGN")
	ordinary := v.createVariable(t, "METADATA_ORDINARY", "ordinary")
	missing := id[i.ProjectVariable](t)
	ids := []vc.VariableID{valid.Fields().ID, removed.Fields().ID, foreign.Fields().ID, ordinary.Fields().ID, missing}
	want := map[vc.VariableID]vc.SecretDirectoryStatus{valid.Fields().ID: vc.SecretDirectoryValid, removed.Fields().ID: vc.SecretDirectoryRemoved, foreign.Fields().ID: vc.SecretDirectoryNotInScope, ordinary.Fields().ID: vc.SecretDirectoryNotInScope, missing: vc.SecretDirectoryNotInScope}
	initial := v.counts(t)
	if initial[0] != 0 || initial[1] != 0 || initial[2] != 0 || initial[3] != 0 || initial[4] != 0 {
		t.Fatal("metadata fixture unexpectedly has Agent references or runtime facts")
	}

	t.Run("normal-metadata", func(t *testing.T) {
		before := v.counts(t)
		for _, selected := range []struct {
			view    mc.ModelView
			purpose mc.ConfigurationPurpose
		}{{systemModel, mc.AgentModelConfiguration}, {projectModel, mc.AgentModelConfiguration}, {projectModel, mc.ApprovalModelConfiguration}} {
			p := v.discover(t, a, selected.view.ID, selected.purpose, ids)
			result := v.inTx(t, p, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
				m, directory, err := v.require(ctx, tx, p)
				if err != nil {
					return err
				}
				if m.ModelID != selected.view.ID || m.ProviderID != selected.view.ProviderID || m.ModelVersion != selected.view.Version || m.ProviderVersion != 1 || !m.Scope.Equal(selected.view.Scope) || !slices.Equal(m.Capabilities.ReasoningEfforts, []string{"medium"}) {
					t.Error("Model facts lost exact scope/identity/version/capabilities")
				}
				if len(directory.Entries()) != 5 {
					t.Error("Secret directory omitted an original ID")
				}
				for _, entry := range directory.Entries() {
					if entry.Status != want[entry.ID] {
						t.Error("Secret directory changed scope/tombstone classification")
					}
					if entry.ID == valid.Fields().ID {
						if entry.Variable == nil || entry.Variable.Fields() != valid.Fields() {
							t.Error("valid Secret metadata changed")
						}
					} else if entry.Variable != nil {
						t.Error("unavailable entry disclosed metadata")
					}
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal("caller metadata Tx", result.Fault())
			}
		}
		if v.counts(t) != before {
			t.Fatal("read-only metadata chain changed durable facts")
		}
	})

	t.Run("current-and-stale", func(t *testing.T) {
		p := v.discover(t, a, projectModel.ID, mc.AgentModelConfiguration, ids)
		before := v.counts(t)
		for _, other := range []i.Actor{v.otherBrowser.actor, v.adminBrowser.actor} {
			mr, sr := p.modelRequest.Clone(), p.secretRequest.Clone()
			mr.Actor, sr.Actor = other, other
			_, err := v.selection.DiscoverConfigurationSelection(ctxFor(t), mr)
			requireCode(t, err, f.NotFound)
			_, err = v.directory.DiscoverSecretVariables(ctxFor(t), sr)
			requireCode(t, err, f.NotFound)
		}
		if v.counts(t) != before {
			t.Fatal("non-Owner metadata read published")
		}
		fresh := v.login(t, v.ownerBrowser.email)
		revoked := v.discover(t, fresh.actor, systemModel.ID, mc.AgentModelConfiguration, ids)
		v.revoke(t, fresh)
		before = v.counts(t)
		for _, isModel := range []bool{true, false} {
			result := v.inTx(t, revoked, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
				if isModel {
					facts, err := v.selection.RequireConfigurationSelectionInTx(ctx, tx, revoked.modelRequest, revoked.modelPlan)
					if facts.ModelID != (mc.ModelID{}) {
						t.Error("revoked Model access returned facts")
					}
					return err
				}
				facts, err := v.directory.RequireSecretVariablesInTx(ctx, tx, revoked.secretRequest, revoked.secretPlan)
				if facts.Validate() == nil {
					t.Error("revoked Secret access returned facts")
				}
				return err
			})
			if result.State() != f.NotCommitted {
				t.Fatal("revoked caller Tx was not rejected")
			}
			requireCode(t, result.Fault(), f.SessionRevoked)
		}
		if v.counts(t) != before {
			t.Fatal("revoked metadata checks published")
		}
		// A real configuration command advances the original Model mapping.
		input := projectModel.Input.Clone()
		input.Name = "metadata-chat-updated"
		_, err := v.models.UpdateModel(ctxFor(t), mc.UpdateModelRequest{CommandMeta: mc.CommandMeta{Actor: a, Scope: projectModel.Scope, Key: "metadata-model-update"}, ID: projectModel.ID, ExpectedVersion: projectModel.Version, Input: input})
		if err != nil {
			t.Fatal("formal Model update", err)
		}
		before = v.counts(t)
		result := v.inTx(t, p, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			_, _, err := v.require(ctx, tx, p)
			return err
		})
		if result.State() != f.NotCommitted {
			t.Fatal("stale Model plan committed")
		}
		requireCode(t, result.Fault(), f.ResourceBusy)
		if v.counts(t) != before {
			t.Fatal("stale Model plan changed facts")
		}
		p = v.discover(t, a, projectModel.ID, mc.AgentModelConfiguration, ids)
		description, version := "updated safe metadata", valid.Fields().Version
		_, err = v.owner.UpdateSecretVariable(ctxFor(t), a, meta(t, "metadata-secret-update", &version), projectID, valid.Fields().ID, secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description}))
		if err != nil {
			t.Fatal("formal Secret metadata update", err)
		}
		before = v.counts(t)
		result = v.inTx(t, p, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			_, _, err := v.require(ctx, tx, p)
			return err
		})
		if result.State() != f.NotCommitted {
			t.Fatal("stale Secret plan committed")
		}
		requireCode(t, result.Fault(), f.ResourceBusy)
		if v.counts(t) != before {
			t.Fatal("stale Secret plan changed facts")
		}
	})

	t.Run("caller-rollback", func(t *testing.T) {
		p := v.discover(t, a, projectModel.ID, mc.AgentModelConfiguration, ids)
		before := v.counts(t)
		marker := id[struct{}](t)
		checked := false
		result := v.inTx(t, p, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if _, _, err := v.require(ctx, tx, p); err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `INSERT INTO variable_fixture.agent_metadata_probe(id) VALUES($1)`, marker.String())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return f.NewFault(f.InternalError, f.NotStarted)
			}
			checked = true
			return f.NewFault(f.InvalidState, f.NotStarted)
		})
		if !checked || result.State() != f.NotCommitted {
			t.Fatal("original caller rollback was not reached")
		}
		requireCode(t, result.Fault(), f.InvalidState)
		var count int
		if err := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM variable_fixture.agent_metadata_probe WHERE id=$1`, marker.String()).Scan(&count); err != nil || count != 0 {
			t.Fatal("caller marker survived rollback", err)
		}
		if v.counts(t) != before {
			t.Fatal("caller rollback changed domain facts")
		}
	})
	t.Log("metadata only: original Owner/Session + both Discover plans + same-Store caller Tx; no Agent creation, references, runtime invocation or production initializer acceptance")
}
