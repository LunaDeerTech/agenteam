package agent

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// These constructors are real; the Store cannot supply a successful live Tx or
// SQL fact. Full initialized configuration/clone/version checks use the real PG
// issuer test, not a public fake preparation authority in this package.
type retirementPreparationStore struct {
	Store
	borrows int
}

func (s *retirementPreparationStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.borrows++
	return nil, fault(f.Forbidden)
}

type retirementPreparationProcesses struct {
	oc.ProcessAuthority
	id oc.ProcessID
}

func (p *retirementPreparationProcesses) CurrentProcess() oc.ProcessID { return p.id }

func retirementPreparationID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func retirementPreparationOwner(t *testing.T, s *retirementPreparationStore) *project.AgentRetirementPreparation {
	t.Helper()
	manifest, err := pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object", "artifact"}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret", "lease"}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", ReferenceKinds: []pc.ReferenceKind{"event", "delivery"}, CleanupAfter: []pc.ParticipantName{pc.SecretParticipant, pc.ArtifactObjectParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", ReferenceKinds: []pc.ReferenceKind{"record"}, CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := project.NewLifecycleAuthority(s, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := project.NewLifecycleStopDriver(s, a, &retirementPreparationProcesses{id: retirementPreparationID[oc.Process](t)}, func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error {
		t.Error("construction/read invoked stop driver")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := project.NewAgentRetirementPreparation(driver)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
func retirementPreparationRequest(t *testing.T) pc.AgentRetirementPreparationRequest {
	t.Helper()
	p := retirementPreparationID[i.Project](t)
	op := retirementPreparationID[pc.Operation](t)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	r := pc.AgentRetirementPreparationRequest{Actor: actor, ProjectID: p, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 2}, AgentID: retirementPreparationID[i.Agent](t), ExpectedConfigVersion: 1}
	if err := r.Validate(); err != nil {
		t.Fatal("invalid controlled request", err)
	}
	return r
}

func TestRetirementPreparationUsesConcreteProjectIssuer(t *testing.T) {
	s := &retirementPreparationStore{}
	projects := &executionConfigurationProjects{check: func(context.Context, f.Tx, i.Actor, i.ProjectID, i.AccessIntent) (pc.ProjectAccess, error) {
		t.Error("preparation used Human owner authority")
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}}
	agents, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	owner := retirementPreparationOwner(t, s)
	reader, err := NewRetirementPreparation(agents, owner)
	if err != nil {
		t.Fatal(err)
	}
	if s.borrows != 0 {
		t.Fatal("pure construction borrowed a transaction")
	}
	for _, bad := range []*Authority{nil, {}} {
		if _, err := NewRetirementPreparation(bad, owner); err == nil {
			t.Fatal("unbound Agent authority accepted")
		}
	}
	if _, err := NewRetirementPreparation(agents, nil); err == nil {
		t.Fatal("missing concrete Project owner accepted")
	}
	for _, name := range []string{"public-request", "cancelled", "nil-reader", "zero-owner"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			r := reader
			switch name {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-reader":
				r = nil
			case "zero-owner":
				var err error
				r, err = NewRetirementPreparation(agents, &project.AgentRetirementPreparation{})
				if err != nil {
					t.Fatal(err)
				}
			}
			value, err := r.ReadRetirementPreparationInTx(ctx, f.NewTx(), retirementPreparationRequest(t))
			if err == nil || value.Validate() == nil || value.Fields().Core.ID.Validate() == nil || s.borrows != 0 {
				t.Fatal("caller DTO exposed a config or crossed issuer boundary", err, s.borrows)
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("original cancellation was replaced", err)
			}
		})
	}
}
