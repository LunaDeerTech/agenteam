package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMCPLeaseObservationRequiresOriginalProofAndExactLease(t *testing.T) {
	r := unitModelRequest(t)
	r.Purpose, r.Action, r.RequestID = sc.MCP, sc.ReleaseLeaseUsage, ""
	binding, err := sc.UsageBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := f.AggregateLock(f.CredentialRefAggregate, r.Ref.Details().ID.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Exclusive}}
	provider, err := sc.NewUsageDependencies(sc.NewPlanIssuer(), binding, binding, locks)
	if err != nil {
		t.Fatal(err)
	}
	issuer := sc.NewPlanIssuer()
	plan, err := sc.WrapUsageDependencies(issuer, provider, locks)
	if err != nil {
		t.Fatal(err)
	}
	store := &projectAuditUnitStore{tx: f.NewTx(), locks: locks}
	denied, released, mismatched, cancelRead := false, false, false, false
	proofCalls := 0
	usage := retirementUsageControl{validate: func(_ context.Context, tx f.Tx, request sc.UsageRequest, _ sc.UsageDependencies) error {
		proofCalls++
		if denied || tx != store.tx || request.LeaseID != r.LeaseID {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
		return nil // controlled original domain proof; not an Execution fixture
	}}
	s := &Service{data: func() *serviceState {
		return &serviceState{store: store, auth: Authorizations{Usage: usage}, usageIssuer: issuer}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.read = func(sql string, args []any) postgres.Row {
		if !strings.HasPrefix(sql, "SELECT credential_id::text") || len(args) != 1 || args[0] != r.LeaseID.String() {
			t.Fatal("observer did not read exact original lease")
		}
		if cancelRead {
			cancel()
		}
		purpose := "mcp"
		if mismatched {
			purpose = "model"
		}
		return projectAuditUnitRow{values: []any{r.Ref.Details().ID.String(), "system", "", string(sc.ExecutionOwner), r.LeaseOwner.Details().ID, purpose, released}}
	}
	got, err := s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, r, plan)
	if err != nil || got || proofCalls != 1 || store.queries != 1 {
		t.Fatal("live lease appeared retired", err)
	}
	released = true
	got, err = s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, r, plan)
	if err != nil || !got || proofCalls != 2 || store.queries != 2 {
		t.Fatal("original release was not observed", err)
	}
	denied = true
	got, err = s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, r, plan)
	if err == nil || got || store.queries != 2 {
		t.Fatal("retired authority read lease")
	}
	denied, mismatched = false, true
	got, err = s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, r, plan)
	if err == nil || got {
		t.Fatal("different consumer lease accepted")
	}
	mismatched = false
	for _, kind := range []sc.LeaseOwnerKind{sc.ModelCallOwner, sc.MCPConnectionRequestOwner} {
		other := r
		other.LeaseOwner, _ = sc.NewCredentialLeaseOwner(kind, r.LeaseOwner.Details().ID)
		before := store.queries
		got, err = s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, other, plan)
		if err == nil || got || store.queries != before {
			t.Fatal("non-Execution owner reached observer")
		}
	}
	cancelRead = true
	got, err = s.MCPExecutionLeaseReleasedInTx(ctx, store.tx, r, plan)
	if !errors.Is(err, context.Canceled) || got {
		t.Fatal("cancelled read published observation")
	}
}
