//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This is an owning-domain test provider, backed by real fixture facts. It does
// not replace object lease/storage behavior or any production authorization.
type sourceAuthority struct {
	f           *fixture
	service     *object.Service
	source      oc.ResolvedSource
	validations atomic.Int64
	store       *postgres.Store
	referenceID string
}

func (a *sourceAuthority) Resolve(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	if ref.Details().Kind != a.source.Details().Reference.Details().Kind {
		return oc.ResolvedSource{}, fault(foundation.Forbidden)
	}
	if _, err := a.f.authority.AuthorizeOwner(ctx, actor, a.source.Details().Owner, identity.Read); err != nil {
		return oc.ResolvedSource{}, err
	}
	return a.source, nil
}
func (a *sourceAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	a.validations.Add(1)
	d := plan.Details().Request.Details()
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: d.Operation, Actor: actor, Source: source})
	if err != nil {
		return err
	}
	if err = a.service.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return err
	}
	if _, err = (&authority{store: a.store}).AuthorizeOwnerInTx(ctx, tx, actor, source.Details().Owner, identity.Read); err != nil {
		return err
	}
	e, err := a.store.InTx(tx)
	if err != nil {
		return err
	}
	var allowed bool
	err = e.QueryRow(ctx, `SELECT active AND object_id=$2 AND revision=$3 FROM object_fixture.source_refs WHERE id=$1`, a.referenceID, source.Details().Meta.ID.String(), int64(source.Details().Revision)).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return fault(foundation.Forbidden)
	}
	return nil
}
func newSourceAuthority(t *testing.T, f *fixture, service *object.Service, stored oc.PutResult) *sourceAuthority {
	t.Helper()
	file := id[struct{}](t)
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: f.project, ArtifactID: f.owner.Details().ID, FileID: file.String()})
	if err != nil {
		t.Fatal(err)
	}
	source, err := oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: f.owner, Meta: stored.Meta, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `CREATE TABLE object_fixture.source_refs(id uuid PRIMARY KEY,object_id uuid NOT NULL,revision bigint NOT NULL,active boolean NOT NULL); CREATE TABLE object_fixture.copy_commands(id uuid PRIMARY KEY,object_id uuid NOT NULL,source_lease uuid NOT NULL,revision bigint NOT NULL)`)
	f.sql(t, `INSERT INTO object_fixture.source_refs VALUES($1,$2,1,true)`, file.String(), stored.Meta.ID.String())
	return &sourceAuthority{f: f, service: service, source: source, store: f.store, referenceID: file.String()}
}
func acquireSource(t *testing.T, f *fixture, service *object.Service, reads *object.SourceReads, source oc.ResolvedSource, callback func(context.Context, foundation.Tx, oc.SourceLease) error) (oc.SourceLease, foundation.CommitResult) {
	t.Helper()
	return acquireSourceOn(t, f, service, reads, source, f.store, callback)
}
func acquireSourceOn(t *testing.T, f *fixture, service *object.Service, reads *object.SourceReads, source oc.ResolvedSource, store object.Store, callback func(context.Context, foundation.Tx, oc.SourceLease) error) (oc.SourceLease, foundation.CommitResult) {
	t.Helper()
	ctx := contextFor(t)
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.AcquireSourceAccess, Actor: f.actor, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.DiscoverAccess(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	objectKey, _ := foundation.AggregateLock(foundation.ObjectAggregate, source.Details().Meta.ID.String())
	var exclusive bool
	for _, lock := range plan.Details().Locks {
		if lock.Key.Canonical() == objectKey.Canonical() && lock.Mode == foundation.Exclusive {
			exclusive = true
		}
	}
	if !exclusive {
		t.Fatal("source acquisition omitted exact Object EX")
	}
	var lease oc.SourceLease
	result := store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
		locked, err := service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		lease, err = reads.AcquireSourceInTx(ctx, tx, f.actor, source, plan, locked)
		if err != nil {
			return err
		}
		e, err := store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `INSERT INTO object_fixture.copy_commands VALUES($1,$2,$3,$4)`, id[struct{}](t).String(), source.Details().Meta.ID.String(), lease.ID().String(), int64(source.Details().Revision))
		if err != nil {
			return err
		}
		if callback != nil {
			return callback(ctx, tx, lease)
		}
		return nil
	})
	return lease, result
}
func TestObjectSourceLeaseAtomicCommitAndSingleOpen(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "source-atomic", strings.Repeat("copy", 40000))
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	provider := newSourceAuthority(t, f, service, stored)
	reads, err := object.NewSourceReads(service, provider)
	if err != nil {
		t.Fatal(err)
	}
	lease, result := acquireSource(t, f, service, reads, provider.source, func(ctx context.Context, tx foundation.Tx, lease oc.SourceLease) error {
		_, err := reads.OpenLeasedSource(ctx, f.actor, lease)
		requireCode(t, err, foundation.ResourceBusy)
		if proxy.gets.Load() != 0 {
			t.Fatal("GET inside origin Tx")
		}
		return nil
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	var n int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM object_fixture.copy_commands c JOIN agenteam_object.object_leases l ON l.id=c.source_lease AND l.object_id=c.object_id WHERE l.id=$1 AND l.state='active' AND l.owner_kind='source' AND l.owner_id=l.id`, lease.ID().String()).Scan(&n); err != nil || n != 1 {
		t.Fatal("source checkpoint/lease not atomic", err)
	}
	foreign, _ := object.NewSourceReads(service, provider)
	if _, err = foreign.OpenLeasedSource(contextFor(t), f.actor, lease); err == nil {
		t.Fatal("foreign adapter accepted handle")
	}
	fabricated, _ := oc.NewSourceLease(oc.NewSourceIssuer(), lease.ID())
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, fabricated); err == nil {
		t.Fatal("caller fabricated permission from LeaseID")
	}
	reader, err := reads.OpenLeasedSource(contextFor(t), f.actor, lease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease); err == nil {
		t.Fatal("second GET accepted")
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(body) != strings.Repeat("copy", 40000) {
		t.Fatal("source contents", err)
	}
	if proxy.gets.Load() != 1 || provider.validations.Load() < 2 {
		t.Fatal("single stream or current source revalidation missing")
	}
	if err = reads.CancelSourceLease(contextFor(t), lease); err != nil {
		t.Fatal(err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE id=$1 AND state='active'`, lease.ID().String()).Scan(&n); err != nil || n != 0 {
		t.Fatal("joined source left active lease", err)
	}
	rolled, result := acquireSource(t, f, service, reads, provider.source, func(context.Context, foundation.Tx, oc.SourceLease) error { return fault(foundation.Forbidden) })
	if result.State() != foundation.NotCommitted {
		t.Fatal("rollback not confirmed")
	}
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, rolled); err == nil {
		t.Fatal("rolled-back handle opened")
	}
	if proxy.gets.Load() != 1 {
		t.Fatal("rollback produced GET")
	}
	if err = reads.CancelSourceLease(contextFor(t), rolled); err != nil {
		t.Fatal(err)
	}
}
func TestObjectSourceLeaseCurrentAuthorityAndCancelJoins(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "source-cancel", strings.Repeat("stream", 40000))
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	provider := newSourceAuthority(t, f, service, stored)
	reads, err := object.NewSourceReads(service, provider)
	if err != nil {
		t.Fatal(err)
	}
	lease, result := acquireSource(t, f, service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	f.sql(t, `UPDATE object_fixture.source_refs SET active=false`)
	_, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease)
	requireCode(t, err, foundation.Forbidden)
	if proxy.gets.Load() != 0 {
		t.Fatal("stale source opened")
	}
	if err = reads.CancelSourceLease(contextFor(t), lease); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `UPDATE object_fixture.source_refs SET active=true`)
	lease, result = acquireSource(t, f, service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := reads.OpenLeasedSource(contextFor(t), f.actor, lease)
	if err != nil {
		t.Fatal(err)
	}
	<-proxy.began
	readDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); readDone <- err }()
	ctx, cancel := context.WithTimeout(contextFor(t), time.Second)
	defer cancel()
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			if e := reads.CancelSourceLease(ctx, lease); e != nil {
				t.Error(e)
			}
		})
	}
	group.Wait()
	select {
	case e := <-readDone:
		if e == nil {
			t.Fatal("cancelled stream completed")
		}
	case <-ctx.Done():
		t.Fatal("actual source Read did not join")
	}
	select {
	case <-proxy.finished:
	case <-ctx.Done():
		t.Fatal("actual remote request not closed")
	}
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease); err == nil {
		t.Fatal("cancelled handle reopened")
	}
	var active bool
	if err = f.store.QueryRow(contextFor(t), `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || active {
		t.Fatal("cancel did not release after join", err)
	}
}

func TestObjectSourceLeaseUploadedReceiptAndSharedPlanRejection(t *testing.T) {
	f := newFixture(t, true)
	stored := f.put(t, "prospective-source", "reserved-payload")
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	provider := newSourceAuthority(t, f, service, stored)
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.UploadedObject, Receipt: stored.Receipt})
	if err != nil {
		t.Fatal(err)
	}
	provider.source, err = oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: f.owner, Meta: stored.Meta, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	reads, err := object.NewSourceReads(service, provider)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextFor(t)
	request, _ := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.ValidateSourceAccess, Actor: f.actor, Source: provider.source})
	plan, err := service.DiscoverAccess(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	result := f.store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
		locked, err := service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		_, err = reads.AcquireSourceInTx(ctx, tx, f.actor, provider.source, plan, locked)
		return err
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("shared source plan acquired technical lease")
	}
	lease, result := acquireSource(t, f, service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	reader, err := reads.OpenLeasedSource(ctx, f.actor, lease)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != "reserved-payload" {
		t.Fatal("exact receipt source", err)
	}
	if _, err = service.ReadObject(ctx, f.actor, f.owner, stored.Meta.ID, nil); err == nil {
		t.Fatal("source lease granted ordinary prospective read")
	}
	lease, result = acquireSource(t, f, service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	service.StopAdmission()
	_, err = reads.OpenLeasedSource(ctx, f.actor, lease)
	requireCode(t, err, foundation.ShuttingDown)
	if proxy.gets.Load() != 1 {
		t.Fatal("late Open after StopAdmission reached storage")
	}
	if err = reads.CancelSourceLease(ctx, lease); err != nil {
		t.Fatal(err)
	}
}
