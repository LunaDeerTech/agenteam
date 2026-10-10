package secret

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Extend only the existing controlled SQL fixture. The real acquisition writer
// creates the original lease; no successful retirement is manually seeded.
type leaseRetirementTestStore struct {
	*environmentLeaseTestStore
	released        bool
	at              *time.Time
	retireWrites    int
	wrongOwner      bool
	reads           int
	afterRetirement func()
}

func (s *leaseRetirementTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *leaseRetirementTestStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT execution_id,project_id,agent_id,variable_id::text") {
		s.reads++
		if len(args) != 1 || args[0] != s.lease {
			return projectAuditUnitRow{err: errors.New("wrong original lease")}
		}
		project := s.r.ProjectID.String()
		if s.wrongOwner {
			project = "01900000-0000-7000-8000-000000000099"
		}
		return projectAuditUnitRow{values: []any{s.r.ExecutionID.String(), project, s.r.AgentID.String(), s.r.VariableID.String(), int64(s.r.VariableVersion), s.r.Ref.Details().ID.String(), int64(s.r.CredentialVersion), int64(s.r.AgentVersion), string(s.r.AttemptBinding), s.released, s.at}}
	}
	if strings.HasPrefix(q, "SELECT lease_id::text,project_id,agent_id,variable_id::text") {
		if len(args) != 2 || args[0] != s.r.ExecutionID.String() || args[1] != s.r.Ref.Details().ID.String() {
			return projectAuditUnitRow{err: errors.New("wrong reacquisition scope")}
		}
		return projectAuditUnitRow{values: []any{s.lease, s.r.ProjectID.String(), s.r.AgentID.String(), s.r.VariableID.String(), int64(s.r.VariableVersion), int64(s.r.CredentialVersion), int64(s.r.AgentVersion), string(s.r.AttemptBinding), s.released}}
	}
	return s.environmentLeaseTestStore.QueryRow(ctx, q, args...)
}
func (s *leaseRetirementTestStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(q, "UPDATE agenteam_secret.project_variable_execution_leases SET released=true") || len(args) != 9 || args[0] != s.lease || args[1] != s.r.ExecutionID.String() || args[2] != s.r.ProjectID.String() || args[3] != s.r.AgentID.String() || args[4] != s.r.VariableID.String() || args[5] != int64(s.r.VariableVersion) || args[6] != s.r.Ref.Details().ID.String() || args[7] != int64(s.r.AgentVersion) || args[8] != string(s.r.AttemptBinding) || s.released {
		return pgconn.CommandTag{}, errors.New("unexpected retirement write")
	}
	s.retireWrites++
	s.released = true
	now := time.Date(2026, 10, 11, 0, 0, 0, 123000, time.UTC)
	s.at = &now
	if s.afterRetirement != nil {
		s.afterRetirement()
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type leaseRetirementTestOwner struct {
	store   *leaseRetirementTestStore
	request sc.ProjectVariableLeaseRetirementRequest
	reject  bool
}

func (o *leaseRetirementTestOwner) CheckProjectVariableLeaseRetirementPlan(_ context.Context, r sc.ProjectVariableLeaseRetirementRequest) error {
	if o.reject || !sameEnvironmentRetirement(o.request, r) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (o *leaseRetirementTestOwner) CheckProjectVariableLeaseRetirementInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest) error {
	if tx != o.store.tx || o.store.checks == 0 {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return o.CheckProjectVariableLeaseRetirementPlan(ctx, r)
}
func (o *leaseRetirementTestOwner) CheckProjectVariableLeaseRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest) error {
	return o.CheckProjectVariableLeaseRetirementInTx(ctx, tx, r)
}

func leaseRetirementFixture(t *testing.T) (*ProjectVariableLeaseRetirementService, *leaseRetirementTestStore, *leaseRetirementTestOwner, sc.ProjectVariableLeaseRetirementRequest, *ProjectVariableLeaseService, sc.ProjectVariableLeasePlan) {
	t.Helper()
	acquire, original, _, request := environmentLeaseFixture(t)
	plan, err := acquire.DiscoverProjectVariableLease(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := acquire.AcquireProjectVariableLeaseInTx(context.Background(), original.tx, request, plan)
	if err != nil || original.writes != 1 {
		t.Fatal("original acquisition", err)
	}
	store := &leaseRetirementTestStore{environmentLeaseTestStore: original}
	// Keep the same controlled Tx/locks and actual acquired row, adding the
	// retirement SQL shape to that fixture's executor, not a new business grant.
	acquire.state().service.state().store = store
	r := sc.ProjectVariableLeaseRetirementRequest{ProjectID: request.ProjectID, AgentID: request.AgentID, ExecutionID: request.ExecutionID, VariableID: request.VariableID, VariableVersion: request.VariableVersion, AgentVersion: request.AgentVersion, Ref: request.Ref, LeaseID: lease.LeaseID, AttemptBinding: request.AttemptBinding}
	owner := &leaseRetirementTestOwner{store: store, request: r}
	retire, err := NewProjectVariableLeaseRetirement(acquire.state().service, owner)
	if err != nil {
		t.Fatal(err)
	}
	return retire, store, owner, r, acquire, plan
}

func TestProjectVariableLeaseRetirementIsExactAndCannotBeReacquired(t *testing.T) {
	p, store, _, request, acquire, originalPlan := leaseRetirementFixture(t)
	plan, err := p.DiscoverProjectVariableLeaseRetirement(context.Background(), request)
	if err != nil || store.reads != 0 {
		t.Fatal("planning performed SQL", err)
	}
	store.locks = plan.RequiredLocks()
	retired, err := p.ProjectVariableLeaseRetiredInTx(context.Background(), store.tx, request, plan)
	if err != nil || retired || store.retireWrites != 0 {
		t.Fatal("observer wrote or invented retirement", err)
	}
	// Current credential rotation must not prevent retiring the captured lease.
	store.version++
	currentReads := store.queries
	for n := 0; n < 2; n++ {
		if err = p.RetireProjectVariableLeaseInTx(context.Background(), store.tx, request, plan); err != nil {
			t.Fatal("retirement/replay", err)
		}
	}
	retired, err = p.ProjectVariableLeaseRetiredInTx(context.Background(), store.tx, request, plan)
	if err != nil || !retired || store.retireWrites != 1 || store.writes != 1 || store.queries != currentReads {
		t.Fatal("retirement changed history/material, read current or replay wrote again", err)
	}
	store.version = int64(store.r.CredentialVersion)
	store.locks = originalPlan.RequiredLocks()
	lease, err := acquire.AcquireProjectVariableLeaseInTx(context.Background(), store.tx, store.r, originalPlan)
	if err == nil || lease.LeaseID.Validate() == nil || store.writes != 1 {
		t.Fatal("released original tuple was reacquired")
	}

	for _, mode := range []string{"owner", "held", "tx", "issuer", "request", "row-owner", "malformed-release", "cancel"} {
		p, store, owner, request, _, _ := leaseRetirementFixture(t)
		plan, err := p.DiscoverProjectVariableLeaseRetirement(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		store.locks = plan.RequiredLocks()
		ctx, tx := context.Background(), store.tx
		switch mode {
		case "owner":
			owner.reject = true
		case "held":
			store.locks = nil
		case "tx":
			tx = f.NewTx()
		case "issuer":
			p, err = NewProjectVariableLeaseRetirement(p.service, owner)
			if err != nil {
				t.Fatal(err)
			}
		case "request":
			request.AgentVersion++
		case "row-owner":
			store.wrongOwner = true
		case "malformed-release":
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			store.at = &at // a timestamp without released=true is corrupt
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if err = p.RetireProjectVariableLeaseInTx(ctx, tx, request, plan); err == nil || store.retireWrites != 0 {
			t.Fatal("unproven retirement wrote", mode, err)
		}
		if retired, err = p.ProjectVariableLeaseRetiredInTx(ctx, tx, request, plan); err == nil || retired {
			t.Fatal("invalid observation reported retired", mode, err)
		}
	}
	p, store, owner, request, _, _ := leaseRetirementFixture(t)
	if _, err = NewProjectVariableLeaseRetirement(&Service{}, owner); err == nil {
		t.Fatal("zero Secret service accepted")
	}
	if _, err = NewProjectVariableLeaseRetirement(p.service, (*leaseRetirementTestOwner)(nil)); err == nil {
		t.Fatal("nil owner accepted")
	}
	wrongScope := request
	project := projectAuditID[i.Project](t)
	// The fixture's IDs are fresh; a different Project without a matching
	// CredentialRef scope must fail before an owner callback or SQL.
	wrongScope.ProjectID = project
	if _, err = p.DiscoverProjectVariableLeaseRetirement(context.Background(), wrongScope); err == nil {
		t.Fatal("cross-scope reference planned")
	}
	plan, err = p.DiscoverProjectVariableLeaseRetirement(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	store.locks = plan.RequiredLocks()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.afterRetirement = cancel
	if err = p.RetireProjectVariableLeaseInTx(ctx, store.tx, request, plan); !errors.Is(err, context.Canceled) || store.retireWrites != 1 {
		t.Fatal("post-write cancellation returned success", err)
	}
	// This controlled write is not a claim of physical rollback: the real
	// caller must roll back its original outer Tx when the callback cancels.
}
