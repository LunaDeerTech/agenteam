package object

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// These tests control an already issued original handle and Store, as the
// existing source-stop fixture does. Actual public acquisition/Project issuance
// remains the integration boundary; no Artifact loan authorizes Knowledge.
type knowledgeSourcePure struct {
	*sourceStopFixture
	request oc.KnowledgeProjectSourceStopRequest
}
type knowledgeSourcePureLoan struct{}

func (v *knowledgeSourcePure) WithKnowledgeProjectSourceStop(ctx context.Context, r oc.KnowledgeProjectSourceStopRequest, fn func(context.Context) error) error {
	if !r.Equal(v.request) {
		return failure(f.Forbidden, nil)
	}
	return fn(context.WithValue(ctx, knowledgeSourcePureLoan{}, v))
}
func (v *knowledgeSourcePure) RequireKnowledgeProjectSourceStopInTx(ctx context.Context, tx f.Tx, r oc.KnowledgeProjectSourceStopRequest) error {
	v.checks++
	if v.deny || ctx.Value(knowledgeSourcePureLoan{}) != v || !r.Equal(v.request) {
		return failure(f.Forbidden, nil)
	}
	locks, err := r.RequiredLocks()
	if err != nil {
		return err
	}
	return v.store.RequireHeldLocks(ctx, tx, locks)
}
func (v *knowledgeSourcePure) stop(ctx context.Context, observe bool) (complete bool, err error) {
	err = v.reads.WithKnowledgeProjectSourceStop(ctx, v.request, func(run context.Context) error {
		if observe {
			complete, err = v.reads.InspectKnowledgeProjectSource(run, v.request)
		} else {
			complete, err = v.reads.StopKnowledgeProjectSource(run, v.request)
		}
		return err
	})
	return complete, err
}
func newKnowledgeSourcePure(t *testing.T) *knowledgeSourcePure {
	t.Helper()
	b := &sourceStopFixture{leaseState: "active"}
	v := &knowledgeSourcePure{sourceStopFixture: b}
	b.store = &sourceStopStore{joinControlStore: &joinControlStore{active: map[f.Tx]bool{}}, fixture: b, locks: map[f.Tx][]f.LockRequest{}}
	st := &serviceState{store: b.store, process: auditID[oc.Process](t), accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[f.Tx]bool{}, metadataTransactions: map[f.Tx]bool{}, projectWork: map[string]*projectWorkHandle{}, projectWriters: map[*projectWorkWriter]bool{}, projectJoins: map[string]*projectWorkHandle{}, closedLeases: map[oc.LeaseID]oc.ObjectID{}, operations: map[*operation]bool{}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{}), auth: Authorizations{Planner: b}}
	b.service = &Service{data: func() *serviceState { return st }}
	var err error
	b.reads, err = NewSourceReadsWithKnowledgeProjectStop(b.service, sourceStopResolver{}, v)
	if err != nil {
		t.Fatal(err)
	}
	d := downloadVectorClaims(t).target.Details().Source.Details()
	p := d.Reference.Details().ProjectID
	document := auditID[struct{}](t).String()
	d.Reference, err = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: p, DocumentID: document, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	d.Owner, err = oc.NewObjectOwner(oc.Knowledge, document, p.String())
	if err != nil {
		t.Fatal(err)
	}
	d.Revision = 1
	source, err := oc.NewResolvedSource(d)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := i.NewHuman(auditID[i.User](t), auditID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	h := &sourceLeaseState{owner: b.reads.data(), id: auditID[oc.Lease](t), origin: f.NewTx(), actor: actor, source: source, process: st.process}
	b.h = h
	h.work = &projectWorkHandle{source: h, work: projectWork{id: h.id.String(), resource: h.id.String(), kind: "source", project: p, process: st.process, object: d.Meta.ID}, origin: h.origin, registered: true}
	st.projectWork[h.work.work.id] = h.work
	b.reads.data().leases[h.id] = h
	lease, err := oc.NewSourceLease(b.reads.data().issuer, h.id)
	if err != nil {
		t.Fatal(err)
	}
	target := auditID[i.Project](t)
	command, err := f.NewCommandIdentity("knowledge", []string{target.String()}, "create", "knowledge-source")
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: p, OperationID: auditID[oc.ProjectStopOperation](t), Action: oc.ProjectStopDelete, ProjectVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(p)
	owner, err := reg.Actor(cause.Details().OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	v.request, err = oc.NewKnowledgeProjectSourceStopRequest(oc.KnowledgeProjectSourceStopDetails{Actor: owner, Cause: cause, Command: command, Lease: lease, SourceProjectID: p, TargetProjectID: target, ObjectID: d.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The old planner fixture exposes only the same two lock keys. Its Artifact
	// request is never passed to the Knowledge stop authority or runtime.
	artifactCommand, _ := f.NewCommandIdentity("artifact", []string{target.String()}, "create", "artifact-source")
	b.request, err = oc.NewProjectSourceStopRequest(oc.ProjectSourceStopDetails{Actor: owner, Cause: cause, Command: artifactCommand, Lease: lease, SourceProjectID: p, TargetProjectID: target, ObjectID: d.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	locks, _ := v.request.RequiredLocks()
	txCause, _ := f.NewCommandsCause(command)
	result := b.reads.WithinKnowledgeProjectSourceAcquisition(context.Background(), txCause, func(ctx context.Context, tx f.Tx) error {
		h.origin = tx
		h.work.origin = tx
		if err := b.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		return b.reads.BindKnowledgeProjectSourceInTx(ctx, tx, lease, target, command)
	})
	if result.State() != f.Committed || !h.acquireReturned {
		t.Fatal("Knowledge acquisition actual return absent", result.Fault())
	}
	t.Cleanup(func() {
		b.store.after = nil
		v.deny = false
		for range 3 {
			complete, err := v.stop(context.Background(), false)
			if err != nil {
				t.Error("original Knowledge source cleanup", err)
				break
			}
			if complete {
				break
			}
		}
		if err := b.reads.ForgetProjectSource(lease); err != nil {
			t.Error("original Knowledge handle still retained", err)
		}
	})
	return v
}

func TestKnowledgeProjectSourceSeparateIssuer(t *testing.T) {
	for _, mode := range []string{"exact", "artifact-port", "no-loan", "foreign-issuer", "foreign-command"} {
		t.Run(mode, func(t *testing.T) {
			v := newKnowledgeSourcePure(t)
			var complete bool
			var err error
			switch mode {
			case "exact":
				complete, err = v.stop(context.Background(), false)
			case "artifact-port":
				complete, err = v.reads.StopProjectSource(context.Background(), v.sourceStopFixture.request)
			case "no-loan":
				complete, err = v.reads.StopKnowledgeProjectSource(context.Background(), v.request)
			default:
				d := v.request.Details()
				if mode == "foreign-issuer" {
					d.Lease, _ = oc.NewSourceLease(oc.NewSourceIssuer(), d.Lease.ID())
				} else {
					d.Command, _ = f.NewCommandIdentity("knowledge", []string{d.TargetProjectID.String()}, "update", "other-command")
				}
				bad, e := oc.NewKnowledgeProjectSourceStopRequest(d)
				if e != nil {
					t.Fatal(e)
				}
				err = v.reads.WithKnowledgeProjectSourceStop(context.Background(), bad, func(ctx context.Context) error {
					complete, err = v.reads.StopKnowledgeProjectSource(ctx, bad)
					return err
				})
			}
			if mode == "exact" {
				if err != nil || !complete || v.releases != 1 || v.joins != 1 {
					t.Fatal("original Knowledge release/join missing", complete, err)
				}
				return
			}
			if err == nil || complete || v.releases != 0 || v.joins != 0 {
				t.Fatal("foreign family/issuer reached mutation", mode, complete, err)
			}
		})
	}
}

func TestKnowledgeProjectSourceUnknownConfirmationReturn(t *testing.T) {
	for _, phase := range []string{"knowledge-source-release", "knowledge-source-join"} {
		t.Run(phase, func(t *testing.T) {
			v := newKnowledgeSourcePure(t)
			attempt := auditID[f.TransactionAttempt](t)
			var original f.TransactionCause
			masked := false
			v.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if c.Details().Owner == phase && !masked && r.State() == f.Committed {
					masked = true
					original = c
					return f.UnknownResult(attempt, c)
				}
				return r
			}
			complete, err := v.stop(context.Background(), false)
			if complete || !masked {
				t.Fatal("original Knowledge physical Unknown not reached", complete, err)
			}
			assertCleanupUnknown(t, err, attempt)
			writes := [2]int{v.releases, v.joins}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				open()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("original Knowledge confirmation did not return")
					}
				}
			})
			v.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				got, want := c.Details(), original.Details()
				if got.Owner != want.Owner || got.RecoveryRunID != want.RecoveryRunID || got.CheckpointRef != want.CheckpointRef {
					t.Error("confirmation replaced original cause")
				}
				close(entered)
				<-release
				return r
			}
			go func() {
				complete, err := v.stop(context.Background(), true)
				if complete && err == nil {
					err = errors.New("readonly confirmation advanced")
				}
				done <- err
			}()
			joinWait(t, entered)
			v.h.mu.Lock()
			retained, got := v.h.retained, v.h.original
			v.h.mu.Unlock()
			if !retained || got.AttemptID() != attempt || writes != [2]int{v.releases, v.joins} {
				t.Fatal("callback result retired original owner or wrote")
			}
			open()
			err = <-done
			joined = true
			if err != nil || v.h.retained || writes != [2]int{v.releases, v.joins} {
				t.Fatal("readonly confirmation did not preserve boundary", err)
			}
			v.store.after = nil
			complete, err = v.stop(context.Background(), false)
			if err != nil || !complete {
				t.Fatal("next explicit Knowledge stop did not settle", complete, err)
			}
		})
	}
}
