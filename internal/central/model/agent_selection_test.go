package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// This controlled SQL adapter exercises the production row decoders and
// authority/Tx flow. It is not PostgreSQL or a production Agent authority.
type selectionFixture struct {
	store                              *projectScopeStore
	service                            *AgentConfiguration
	request                            mc.ConfigurationSelectionRequest
	provider                           mc.ProviderID
	projectScoped, absent              bool
	modelEnabled, providerEnabled      bool
	modelVersion, providerVersion      f.Version
	modelType                          mc.ModelType
	caps                               mc.Capabilities
	ownerError, sessionError           error
	ownerCalls, sessionCalls, rowCalls int
	intents                            []id.AccessIntent
}

func newSelectionFixture(t *testing.T, projectScoped bool) *selectionFixture {
	t.Helper()
	x := &selectionFixture{store: &projectScopeStore{}, provider: mustID[mc.Provider](t),
		projectScoped: projectScoped, modelEnabled: true, providerEnabled: true,
		modelVersion: 2, providerVersion: 3, modelType: mc.ChatModel,
		caps: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
	x.request = mc.ConfigurationSelectionRequest{Actor: testActor(t), ProjectID: mustID[id.Project](t), ModelID: mustID[mc.Model](t), Purpose: mc.AgentModelConfiguration}
	x.request.Command, _ = f.NewCommandIdentity("project", []string{x.request.ProjectID.String()}, "agent.create", "selection-test")
	a, err := NewAuthority(x.store, Authorizations{
		Sessions: sessionFunc(func(_ context.Context, tx f.Tx, actor id.Actor) error {
			x.sessionCalls++
			if tx != x.store.tx || !actor.Equal(x.request.Actor) {
				t.Fatal("changed Session transaction or Actor")
			}
			return x.sessionError
		}),
		System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
			t.Fatal("System catalog selection bypassed current Project Owner")
			return id.AccessGrant{}, nil
		}),
		Projects: projectGrantFunc(func(_ context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
			x.ownerCalls++
			x.intents = append(x.intents, intent)
			if tx != x.store.tx || !actor.Equal(x.request.Actor) || project != x.request.ProjectID {
				t.Fatal("changed Owner, Project or transaction")
			}
			if x.ownerError != nil {
				return id.AccessGrant{}, x.ownerError
			}
			return projectGrant(actor, project, intent)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	x.service, err = NewAgentConfiguration(x.store, a)
	if err != nil {
		t.Fatal(err)
	}
	x.store.row = func(q string, args ...any) postgres.Row {
		x.rowCalls++
		if x.ownerCalls == 0 || x.ownerError != nil || x.sessionError != nil {
			t.Fatal("catalog read before current authority")
		}
		isModel := strings.Contains(q, "agenteam_model.models m")
		isProject := strings.Contains(q, "scope='project'")
		if isProject && (len(args) != 2 || args[1] != x.request.ProjectID.String()) {
			t.Fatal("Project query lost exact scope")
		}
		wantID := x.provider.String()
		if isModel {
			wantID = x.request.ModelID.String()
		}
		if len(args) == 0 || args[0] != wantID {
			t.Fatal("query lost exact catalog identity")
		}
		return scanFunc(func(out ...any) error {
			if x.absent || isProject != x.projectScoped {
				return pgx.ErrNoRows
			}
			now := time.Unix(1700000000, 0).UTC()
			if isModel {
				*out[0].(*string), *out[1].(*string) = x.request.ModelID.String(), x.provider.String()
				*out[2].(*string), *out[3].(*string) = "configuration model", "provider-model"
				*out[4].(*mc.ModelType), *out[5].(*bool) = x.modelType, x.modelEnabled
				*out[6].(*json.RawMessage), *out[7].(*json.RawMessage) = json.RawMessage(`{}`), json.RawMessage(`{}`)
				*out[8].(*[]byte) = []byte(`{}`)
				*out[9].(*[]byte), _ = json.Marshal(x.caps)
				*out[10].(*f.Version) = x.modelVersion
				*out[11].(*time.Time), *out[12].(*time.Time) = now, now
			} else {
				*out[0].(*string), *out[1].(*string) = x.provider.String(), "configuration provider"
				*out[2].(*mc.Protocol), *out[3].(*string) = mc.OpenAIChat, "https://provider.example/v1"
				*out[4].(*bool), *out[5].(**string) = x.providerEnabled, nil
				*out[6].(*json.RawMessage), *out[7].(*f.Version) = json.RawMessage(`{}`), x.providerVersion
				*out[8].(*time.Time), *out[9].(*time.Time) = now, now
			}
			return nil
		})
	}
	return x
}

func (x *selectionFixture) discover(t *testing.T) mc.ConfigurationSelectionPlan {
	t.Helper()
	p, err := x.service.DiscoverConfigurationSelection(context.Background(), x.request)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgentConfigurationSelectionDiscoverAndRequireCallerTx(t *testing.T) {
	for _, projectScoped := range []bool{false, true} {
		name := "system"
		if projectScoped {
			name = "same-project"
		}
		t.Run(name, func(t *testing.T) {
			x := newSelectionFixture(t, projectScoped)
			p := x.discover(t)
			if x.store.transactions != 1 || x.store.acquisitions != 1 || len(p.RequiredLocks()) != 6 {
				t.Fatal("incomplete discovery transaction/locks")
			}
			// These are the caller's already-held final locks, not late locks
			// acquired by RequireConfigurationSelectionInTx.
			x.store.tx, x.store.locks = f.NewTx(), p.RequiredLocks()
			facts, err := x.service.RequireConfigurationSelectionInTx(context.Background(), x.store.tx, x.request, p)
			if err != nil {
				t.Fatal(err)
			}
			if facts.ModelID != x.request.ModelID || facts.ProviderID != x.provider || facts.ModelVersion != 2 || facts.ProviderVersion != 3 {
				t.Fatal("lost original catalog facts")
			}
			if projectScoped && facts.Scope.Details().ProjectID != x.request.ProjectID.String() || !projectScoped && facts.Scope.Details().Kind != id.System {
				t.Fatal("changed catalog scope")
			}
			if x.store.transactions != 1 || x.store.acquisitions != 1 || x.ownerCalls != 2 || x.sessionCalls != 2 || x.intents[0] != id.Read || x.intents[1] != id.Mutate {
				t.Fatal("final check did not reuse caller Tx and current Owner")
			}
			facts.Capabilities.InputModalities[0] = "image"
			if p.Details().Candidate.Capabilities.InputModalities[0] != "text" {
				t.Fatal("facts alias issued plan")
			}
		})
	}
}

func TestAgentConfigurationSelectionRejectsStaleOrForeignFinalUse(t *testing.T) {
	for _, name := range []string{"foreign-tx", "foreign-issuer", "changed-command", "changed-session", "missing-provider-lock", "weak-user-lock", "owner-revoked", "session-revoked", "model-version", "provider-version", "model-disabled", "provider-disabled", "missing-model", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			x := newSelectionFixture(t, false)
			p := x.discover(t)
			x.store.tx, x.store.locks = f.NewTx(), p.RequiredLocks()
			tx, r, service, ctx := x.store.tx, x.request, x.service, context.Background()
			want := f.Forbidden
			switch name {
			case "foreign-tx":
				tx, want = f.NewTx(), f.InvalidArgument
			case "foreign-issuer":
				service, _ = NewAgentConfiguration(x.store, x.service.state().authority)
			case "changed-command":
				r.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "agent.create", "different-key")
			case "changed-session":
				user, _ := f.ParseID[id.User](r.Actor.Details().UserID)
				r.Actor, _ = id.NewHuman(user, mustID[id.Session](t))
			case "missing-provider-lock", "weak-user-lock":
				want = f.InvalidState
				provider := aggregateLock(f.ProviderAggregate, x.provider.String(), f.Shared).Key.Canonical()
				user := userLock(r.Actor.Details().UserID).Key.Canonical()
				for i, l := range x.store.locks {
					if name == "missing-provider-lock" && l.Key.Canonical() == provider {
						x.store.locks = append(x.store.locks[:i], x.store.locks[i+1:]...)
						break
					}
					if name == "weak-user-lock" && l.Key.Canonical() == user {
						x.store.locks[i].Mode = f.Shared
						break
					}
				}
			case "owner-revoked":
				x.ownerError = fault(f.Forbidden)
			case "session-revoked":
				x.sessionError, want = fault(f.Unauthenticated), f.Unauthenticated
			case "model-version":
				x.modelVersion++
				want = f.ResourceBusy
			case "provider-version":
				x.providerVersion++
				want = f.ResourceBusy
			case "model-disabled":
				x.modelEnabled, want = false, f.InvalidState
			case "provider-disabled":
				x.providerEnabled, want = false, f.InvalidState
			case "missing-model":
				x.absent, want = true, f.NotFound
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = f.DependencyUnavailable
			}
			facts, err := service.RequireConfigurationSelectionInTx(ctx, tx, r, p)
			requireCode(t, err, want)
			if facts.ModelID != (mc.ModelID{}) || facts.ProviderID != (mc.ProviderID{}) || facts.ModelVersion != 0 {
				t.Fatal("refusal returned selection facts")
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation cause")
			}
			if x.store.transactions != 1 || x.store.acquisitions != 1 {
				t.Fatal("final refusal opened transaction or acquired locks")
			}
		})
	}
}

func TestAgentConfigurationSelectionRoleAndEffort(t *testing.T) {
	for _, name := range []string{"primary-none", "primary-choice", "primary-missing", "primary-unsupported", "primary-unadvertised", "approval-none", "approval-reasoning-model", "approval-effort-rejected", "non-chat"} {
		t.Run(name, func(t *testing.T) {
			x := newSelectionFixture(t, false)
			var want f.Code
			if strings.Contains(name, "approval") {
				x.request.Purpose = mc.ApprovalModelConfiguration
			}
			if name == "primary-choice" || name == "primary-missing" || name == "primary-unsupported" || name == "approval-reasoning-model" {
				x.caps.Reasoning, x.caps.ReasoningEfforts = true, []string{"medium"}
			}
			choice := "medium"
			switch name {
			case "primary-choice":
				x.request.ReasoningEffort = &choice
			case "primary-missing":
				want = f.CapabilityUnsupported
			case "primary-unsupported":
				choice = "high"
				x.request.ReasoningEffort, want = &choice, f.CapabilityUnsupported
			case "primary-unadvertised":
				x.request.ReasoningEffort, want = &choice, f.CapabilityUnsupported
			case "approval-effort-rejected":
				x.request.ReasoningEffort, want = &choice, f.InvalidArgument
			case "non-chat":
				x.modelType, want = mc.EmbeddingModel, f.CapabilityUnsupported
			}
			p, err := x.service.DiscoverConfigurationSelection(context.Background(), x.request)
			if want != "" {
				requireCode(t, err, want)
				if p.Validate() == nil {
					t.Fatal("refusal issued plan")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			x.store.tx, x.store.locks = f.NewTx(), p.RequiredLocks()
			if _, err = x.service.RequireConfigurationSelectionInTx(context.Background(), x.store.tx, x.request, p); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentConfigurationSelectionConstructionAndEarlyRefusal(t *testing.T) {
	store := &noIOStore{}
	a := pureAuthority(t, store)
	_, err := NewAgentConfiguration(store, a)
	requireCode(t, err, f.DependencyUnbound)
	x := newSelectionFixture(t, false)
	_, err = NewAgentConfiguration(&noIOStore{}, x.service.state().authority)
	requireCode(t, err, f.DependencyUnbound)
	var nilStore *projectScopeStore
	_, err = NewAgentConfiguration(nilStore, nil)
	requireCode(t, err, f.DependencyUnbound)
	x.ownerError = fault(f.Forbidden)
	p, err := x.service.DiscoverConfigurationSelection(context.Background(), x.request)
	requireCode(t, err, f.Forbidden)
	if p.Validate() == nil || x.rowCalls != 0 {
		t.Fatal("Owner refusal exposed catalog")
	}
	var unbound *AgentConfiguration
	_, err = unbound.DiscoverConfigurationSelection(context.Background(), x.request)
	requireCode(t, err, f.DependencyUnbound)
}
