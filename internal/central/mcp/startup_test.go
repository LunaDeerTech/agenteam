package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func startupCandidate(t *testing.T) (*Service, *startupUse, startupFact) {
	t.Helper()
	process := contentTailID[oc.Process](t, "31")
	s := &Service{owner: &processOwner{process: process}, secretIssuer: sc.NewPlanIssuer()}
	request := contentTailID[f.Request](t, "32").String()
	r := startupFact{process: process.String(), project: contentTailID[id.Project](t, "33").String(), connection: contentTailID[ConnectionIdentity](t, "34").String(), config: contentTailID[ConfigIdentity](t, "35").String(), runtime: contentTailID[struct{}](t, "36").String(), credential: contentTailID[sc.Credential](t, "37").String(), lease: contentTailID[sc.Lease](t, "38").String(), revision: 1, version: 2, phase: "pending"}
	w, err := s.recoveredStartupUse(&processCall{owner: s.owner}, request, r)
	if err != nil {
		t.Fatal(err)
	}
	return s, w, r
}

func TestMCPStartupPersistedTupleDoesNotCreatePrivateAuthority(t *testing.T) {
	s, w, fact := startupCandidate(t)
	if w.active.Load() || w.stopped || !w.matches(fact) {
		t.Fatal("row parser minted a stopped-process proof")
	}
	if _, err := s.startupUse(context.WithValue(context.Background(), startupUseKey{}, w)); err == nil {
		t.Fatal("public persisted tuple became active")
	}
	if err := s.RequireMCPRuntimeInTx(context.Background(), f.NewTx(), w.actor, w.connection.ProjectID); err == nil {
		t.Fatal("registered service actor replaced original call")
	}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), startupUseKey{}, w)
	if got, err := s.startupUse(ctx); err != nil || got != w {
		t.Fatal("original private context lost", err)
	}
	for _, change := range []func(*startupFact){
		func(r *startupFact) { r.process = contentTailID[oc.Process](t, "39").String() },
		func(r *startupFact) { r.project = contentTailID[id.Project](t, "39").String() },
		func(r *startupFact) { r.config = contentTailID[ConfigIdentity](t, "39").String() },
		func(r *startupFact) { r.connection = contentTailID[ConnectionIdentity](t, "39").String() },
		func(r *startupFact) { r.runtime = contentTailID[struct{}](t, "39").String() },
		func(r *startupFact) { r.revision++ }, func(r *startupFact) { r.version++ },
		func(r *startupFact) { r.credential = contentTailID[sc.Credential](t, "39").String() },
		func(r *startupFact) { r.lease = contentTailID[sc.Lease](t, "39").String() },
	} {
		other := fact
		change(&other)
		if w.matches(other) {
			t.Fatal("substituted durable ownership accepted")
		}
	}
	w.active.Store(false)
	if _, err := s.startupUse(ctx); err == nil {
		t.Fatal("returned original context remained a grant")
	}
}

func TestMCPStartupSecretPlanRequiresOriginalUsageAndValidatedTx(t *testing.T) {
	s, w, _ := startupCandidate(t)
	w.active.Store(true)
	use, err := s.newStartupSecret(w, sc.ReadLeaseUsage)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.WithValue(context.Background(), startupUseKey{}, w), startupSecretKey{}, use)
	plan, err := s.discoverStartupSecret(ctx, use.request)
	if err != nil || plan.Validate() != nil {
		t.Fatal("original usage planning failed", err)
	}
	if _, err = s.discoverStartupSecret(context.Background(), use.request); err == nil {
		t.Fatal("bare usage DTO planned a lease")
	}
	changed := use.request
	changed.RequestID = contentTailID[f.Request](t, "39").String()
	if _, err = s.discoverStartupSecret(ctx, changed); err == nil {
		t.Fatal("another backend request reused private usage")
	}
	if _, err = s.authorizeStartupSecret(ctx, f.NewTx(), use.request.Actor, use.request.Ref, use.request.LeaseOwner, sc.ReadLease); err == nil {
		t.Fatal("unvalidated transaction acquired a Secret grant")
	}
	use.close()
	if _, err = s.discoverStartupSecret(ctx, use.request); err == nil {
		t.Fatal("retired usage witness planned again")
	}
	if err = (&Service{}).StartReconciliation(context.Background()); err == nil {
		t.Fatal("unbound process started a worker")
	}
}
