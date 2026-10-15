package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type embeddingProfileOwner struct {
	store                         *projectScopeStore
	request                       mc.EmbeddingProfileRequest
	issuer                        mc.PlanIssuer
	discoverDenied, requireDenied bool
	discovered, required          int
}

func (o *embeddingProfileOwner) DiscoverEmbeddingProfile(_ context.Context, r mc.EmbeddingProfileRequest) (mc.ConsumerDependencies, error) {
	o.discovered++
	if o.discoverDenied {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	b, _ := mc.EmbeddingProfileBinding(r)
	return mc.NewConsumerDependencies(o.issuer, mc.ConsumerDependencyDetails{Binding: b, Mapping: f.Digest("sha256:" + strings.Repeat("a", 64)), Locks: []f.LockRequest{projectLock(r.Consumer.ProjectID.String())}})
}
func (o *embeddingProfileOwner) RequireEmbeddingProfileInTx(ctx context.Context, tx f.Tx, r mc.EmbeddingProfileRequest, p mc.ConsumerDependencies) error {
	o.required++
	b, _ := mc.EmbeddingProfileBinding(o.request)
	if o.requireDenied || !r.Actor.Equal(o.request.Actor) || !r.Consumer.Equal(o.request.Consumer) || !p.Matches(o.issuer, b, p.Details().Mapping) {
		return fault(f.Forbidden)
	}
	return o.store.RequireHeldLocks(ctx, tx, p.RequiredLocks())
}

type embeddingProfileFixture struct {
	store           *projectScopeStore
	reader          *EmbeddingProfileReader
	owner           *embeddingProfileOwner
	model           mc.ModelID
	provider        mc.ProviderID
	selection       string
	reads           int
	drift, disabled bool
}

func newEmbeddingProfileFixture(t *testing.T) *embeddingProfileFixture {
	t.Helper()
	x := &embeddingProfileFixture{store: &projectScopeStore{}, model: mustID[mc.Model](t), provider: mustID[mc.Provider](t), selection: mustID[struct{}](t).String()}
	project, agent := mustID[id.Project](t), mustID[id.Agent](t)
	scope, _ := id.InProject(project)
	reg, _ := id.RegisterService(id.MemoryRuntime)
	op := mustID[struct{}](t).String()
	actor, _ := reg.Actor(op, scope)
	x.owner = &embeddingProfileOwner{store: x.store, issuer: mc.NewPlanIssuer(), request: mc.EmbeddingProfileRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: project, AgentID: &agent, Purpose: mc.MemoryEmbedding, OperationID: op}}}
	a, err := NewAuthority(x.store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error {
		t.Fatal("profile used Human Session authority")
		return nil
	}), System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
		t.Fatal("profile used Human System authority")
		return id.AccessGrant{}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	x.reader, err = NewEmbeddingProfileReader(a, x.owner)
	if err != nil {
		t.Fatal(err)
	}
	x.store.row = func(q string, args ...any) postgres.Row {
		x.reads++
		if x.owner.discovered == 0 {
			t.Fatal("profile read before private owner discovery")
		}
		return scanFunc(func(out ...any) error {
			at := time.Unix(1700000000, 0).UTC()
			if strings.Contains(q, "platform_selection") {
				*out[0].(*string), *out[1].(*f.Version), *out[2].(*bool) = x.selection, 1, true
				selected := x.model.String()
				*out[3].(**string) = &selected
				for n := 4; n < 7; n++ {
					*out[n].(**string) = nil
				}
				return nil
			}
			if strings.Contains(q, "agenteam_model.models m") {
				if len(args) != 1 || args[0] != x.model.String() {
					t.Fatal("profile lost system Model identity")
				}
				*out[0].(*string), *out[1].(*string) = x.model.String(), x.provider.String()
				*out[2].(*string), *out[3].(*string) = "profile model", "embedding-model"
				*out[4].(*mc.ModelType), *out[5].(*bool) = mc.EmbeddingModel, true
				*out[6].(*json.RawMessage), *out[7].(*json.RawMessage) = json.RawMessage(`{}`), json.RawMessage(`{}`)
				*out[8].(*[]byte) = []byte(`{}`)
				*out[9].(*[]byte), _ = json.Marshal(mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}})
				*out[10].(*f.Version) = 2
				*out[11].(*time.Time), *out[12].(*time.Time) = at, at
				return nil
			}
			if len(args) != 1 || args[0] != x.provider.String() {
				t.Fatal("profile lost system Provider identity")
			}
			*out[0].(*string), *out[1].(*string) = x.provider.String(), "profile provider"
			*out[2].(*mc.Protocol), *out[3].(*string) = mc.OpenAIEmbeddings, "https://provider.example/v1"
			*out[4].(*bool), *out[5].(**string) = !x.disabled, nil
			*out[6].(*json.RawMessage), *out[7].(*f.Version) = json.RawMessage(`{}`), 3
			if x.drift && x.store.tx.Valid() {
				*out[7].(*f.Version) = 4
			}
			*out[8].(*time.Time), *out[9].(*time.Time) = at, at
			return nil
		})
	}
	return x
}
func TestEmbeddingProfileReadRevalidatesPrivateOwnerAndFullLocks(t *testing.T) {
	x := newEmbeddingProfileFixture(t)
	p, err := x.reader.ReadEmbeddingProfile(context.Background(), x.owner.request)
	if err != nil {
		t.Fatal(err)
	}
	if p.ModelID != x.model || p.ProviderID != x.provider || p.SelectionVersion != 1 || p.ModelVersion != 2 || p.ProviderVersion != 3 || p.Validate() != nil || x.reads != 6 || x.owner.required != 1 || x.store.transactions != 1 {
		t.Fatal("profile did not return exact revalidated current facts")
	}
}
func TestEmbeddingProfileDriftAndPermissionFailuresReturnNoProfile(t *testing.T) {
	for _, name := range []string{"discovery", "current authority", "drift", "disabled"} {
		t.Run(name, func(t *testing.T) {
			x := newEmbeddingProfileFixture(t)
			want := f.Forbidden
			switch name {
			case "discovery":
				x.owner.discoverDenied = true
			case "current authority":
				x.owner.requireDenied = true
				x.disabled = true
			case "drift":
				x.drift = true
				want = f.ResourceBusy
			case "disabled":
				x.disabled = true
				want = f.InvalidState
			}
			p, err := x.reader.ReadEmbeddingProfile(context.Background(), x.owner.request)
			requireCode(t, err, want)
			if p != (mc.EmbeddingProfile{}) {
				t.Fatal("denied or stale profile escaped")
			}
			if name == "discovery" && x.reads != 0 {
				t.Fatal("denied owner queried Model catalog")
			}
		})
	}
}
