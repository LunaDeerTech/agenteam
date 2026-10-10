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

type retirementUsageControl struct {
	sc.UsageAuthority
	sc.UsagePlanner
	validate func(context.Context, f.Tx, sc.UsageRequest, sc.UsageDependencies) error
}

func (p retirementUsageControl) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	return p.validate(ctx, tx, r, plan)
}

func TestModelExecutionLeaseObservationUsesCurrentProofAndOriginalRead(t *testing.T) {
	request := unitModelRequest(t)
	request.Action, request.RequestID = sc.ReleaseLeaseUsage, ""
	binding, _ := sc.UsageBinding(request)
	key, _ := f.AggregateLock(f.CredentialRefAggregate, request.Ref.Details().ID.String())
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
	proofCalls := 0
	denied := false
	usage := retirementUsageControl{validate: func(_ context.Context, tx f.Tx, r sc.UsageRequest, _ sc.UsageDependencies) error {
		proofCalls++
		if tx != store.tx || r.LeaseID != request.LeaseID || denied {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
		return nil // explicitly controlled upstream terminal proof, not Execution
	}}
	s := &Service{data: func() *serviceState {
		return &serviceState{store: store, auth: Authorizations{Usage: usage}, usageIssuer: issuer}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	released, cancelRead := false, false
	store.read = func(sql string, args []any) postgres.Row {
		if !strings.HasPrefix(sql, "SELECT credential_id::text") || len(args) != 1 || args[0] != request.LeaseID.String() {
			t.Fatal("observer did not use exact original lease read")
		}
		if cancelRead {
			cancel()
		}
		return projectAuditUnitRow{values: []any{request.Ref.Details().ID.String(), "system", "", "execution", request.LeaseOwner.Details().ID, "model", released}}
	}
	got, err := s.ModelExecutionLeaseReleasedInTx(ctx, store.tx, request, plan)
	if err != nil || got || store.queries != 1 || proofCalls != 1 {
		t.Fatal("unreleased lease became a released observation", err)
	}
	released = true
	got, err = s.ModelExecutionLeaseReleasedInTx(ctx, store.tx, request, plan)
	if err != nil || !got || store.queries != 2 || proofCalls != 2 {
		t.Fatal("released observation lost original current proof", err)
	}
	denied = true
	got, err = s.ModelExecutionLeaseReleasedInTx(ctx, store.tx, request, plan)
	if err == nil || got || store.queries != 2 {
		t.Fatal("denied current proof reached credential metadata")
	}
	denied, cancelRead = false, true
	got, err = s.ModelExecutionLeaseReleasedInTx(ctx, store.tx, request, plan)
	if !errors.Is(err, context.Canceled) || got || store.queries != 3 {
		t.Fatal("read cancellation published a partial observation")
	}
}
