//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Acquisition, source authorization, metadata, bytes and lease cancellation
// remain the actual D05 implementation. Only the declared Open/Close boundary
// is controlled, never a successful permission or fabricated source handle.
type runtimeSourceReads struct {
	oc.SourceReads
	open func(context.Context, id.Actor, oc.SourceLease) (*oc.ObjectReader, error)
}

func (r *runtimeSourceReads) OpenLeasedSource(ctx context.Context, actor id.Actor, lease oc.SourceLease) (*oc.ObjectReader, error) {
	return r.open(ctx, actor, lease)
}

type runtimeCloseGate struct {
	io.ReadCloser
	entered, release chan struct{}
	enter            sync.Once
	returned         atomic.Bool
}

func (r *runtimeCloseGate) Close() error {
	r.enter.Do(func() { close(r.entered) })
	<-r.release
	err := r.ReadCloser.Close()
	r.returned.Store(true)
	return err
}

func runtimeAwait(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatal("declared runtime boundary was not reached")
	}
}

func runtimeNoCanonical(t *testing.T, x *ownerTreeFixture, target id.ProjectID) {
	t.Helper()
	var documents, objects, events int
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.object_references WHERE partition_id=$1 AND owner_kind='knowledge' AND kind='canonical'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, target.String()).Scan(&documents, &objects, &events)
	if err != nil || documents != 0 || objects != 0 || events != 0 {
		t.Fatal("unfinished runtime operation published target facts", err)
	}
}

func runtimeSourceLeaseCounts(t *testing.T, x *ownerTreeFixture, object oc.ObjectID) (total, active int) {
	t.Helper()
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT count(*),count(*) FILTER (WHERE state='active')
 FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source'`, object.String()).Scan(&total, &active)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// These same-process tests exercise Knowledge's admitted-call and exact lease
// ownership against real D05. They provide neither a ProcessGuard death proof
// nor transport COMMIT Unknown or global Object runtime-join acceptance.
func TestKnowledgeB02Runtime(t *testing.T) {
	t.Run("stop_waits_for_actual_delegated_close", func(t *testing.T) {
		x := newPublicationFixture(t)
		actor := x.human(t)
		source := publicationSeedContent(t, x, actor, x.project(t, actor, true), "source for close")
		target := x.project(t, actor, true)
		gate := &runtimeCloseGate{entered: make(chan struct{}), release: make(chan struct{})}
		var release sync.Once
		unblock := func() { release.Do(func() { close(gate.release) }) }
		port := &runtimeSourceReads{SourceReads: x.deps.SourceReads}
		port.open = func(ctx context.Context, actor id.Actor, lease oc.SourceLease) (*oc.ObjectReader, error) {
			reader, err := port.SourceReads.OpenLeasedSource(ctx, actor, lease)
			if err != nil || reader == nil {
				return reader, err
			}
			gate.ReadCloser = reader
			wrapped, err := oc.NewObjectReader(reader.Meta(), reader.Range(), gate)
			if err != nil {
				_ = reader.Close()
			}
			return wrapped, err
		}
		s := publicationService(t, x, func(d *knowledge.Dependencies) { d.SourceReads = port })
		result := make(chan error, 1)
		joined := make(chan struct{})
		request := kc.CreateRequest{ProjectID: target, DocumentID: treeID[kc.Document](t), Title: "target"}
		meta, input := treeMeta(t), publicationBusiness(t, source)
		ctx := knowledgeContext(t)
		go func() {
			defer close(joined)
			_, err := s.CreateDocument(ctx, actor, meta, request, input)
			result <- err
		}()
		// This cleanup precedes the fixture's actual service/Store teardown on
		// every assertion failure. The test cannot abandon its blocked caller.
		t.Cleanup(func() { unblock(); runtimeAwait(t, joined) })
		runtimeAwait(t, gate.entered)
		if gate.returned.Load() {
			t.Fatal("Close gate returned before release")
		}
		runtimeNoCanonical(t, x, target)
		s.Stop()
		cancelled, cancel := context.WithCancel(knowledgeContext(t))
		cancel()
		if err := s.Drain(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatal("Drain accepted an unreturned actual Close", err)
		}
		select {
		case <-joined:
			t.Fatal("cancel completed the blocked public caller")
		default:
		}
		unblock()
		runtimeAwait(t, joined)
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal("stopped operation lost original cancellation", err)
		}
		if !gate.returned.Load() {
			t.Fatal("public caller returned before real delegated Close")
		}
		if err := s.Drain(knowledgeContext(t)); err != nil {
			t.Fatal("actual caller/retirement did not join", err)
		}
		runtimeNoCanonical(t, x, target)
		if total, active := runtimeSourceLeaseCounts(t, x, source.ObjectID); total != 1 || active != 0 {
			t.Fatal("actual source lease was lost or left active")
		}
		_, err := s.CreateDocument(knowledgeContext(t), actor, meta, request, publicationBusiness(t, source))
		treeCode(t, err, f.ShuttingDown)
		// D05 may release a fully read stream at EOF before explicit Close.
		// The assertion above is Knowledge call ownership, not a claim that
		// a source lease necessarily stayed active until the wrapper returned.
	})
	t.Run("cancelled_request_retains_unopened_lease_for_live_retry", func(t *testing.T) {
		x := newPublicationFixture(t)
		actor := x.human(t)
		source := publicationSeedContent(t, x, actor, x.project(t, actor, true), "source for retry")
		target := x.project(t, actor, true)
		request := kc.CreateRequest{ProjectID: target, DocumentID: treeID[kc.Document](t), Title: "target"}
		meta := treeMeta(t)
		requestContext, cancel := context.WithCancel(knowledgeContext(t))
		defer cancel()
		injected := errors.New("declared pre-GET source failure")
		var opens atomic.Int32
		port := &runtimeSourceReads{SourceReads: x.deps.SourceReads}
		port.open = func(ctx context.Context, actor id.Actor, lease oc.SourceLease) (*oc.ObjectReader, error) {
			if opens.Add(1) == 1 {
				if lease.Validate() != nil {
					return nil, errors.New("missing actual source lease")
				}
				cancel()
				return nil, injected
			}
			return port.SourceReads.OpenLeasedSource(ctx, actor, lease)
		}
		s := publicationService(t, x, func(d *knowledge.Dependencies) { d.SourceReads = port })
		_, err := s.CreateDocument(requestContext, actor, meta, request, publicationBusiness(t, source))
		if !errors.Is(err, injected) || opens.Load() != 1 {
			t.Fatal("cleanup replaced original pre-GET failure", err)
		}
		if total, active := runtimeSourceLeaseCounts(t, x, source.ObjectID); total != 1 || active != 1 {
			t.Fatal("cancelled request pretended to retire real unopened lease")
		}
		runtimeNoCanonical(t, x, target)
		if err = s.Drain(knowledgeContext(t)); err != nil {
			t.Fatal("fresh caller failed actual lease retirement", err)
		}
		if total, active := runtimeSourceLeaseCounts(t, x, source.ObjectID); total != 1 || active != 0 {
			t.Fatal("live Drain did not release the exact original lease")
		}
		actual, err := s.CreateDocument(knowledgeContext(t), actor, meta, request, publicationBusiness(t, source))
		if err != nil || actual.ID != request.DocumentID || opens.Load() != 2 {
			t.Fatal("same command did not resume with new authorized source lease", err)
		}
		publicationRead(t, x, actor, actual, []byte("source for retry"))
		publicationSourceRetired(t, x, source.ObjectID, target)
		if total, _ := runtimeSourceLeaseCounts(t, x, source.ObjectID); total != 2 {
			t.Fatal("retry reused stale lease or created extra lease")
		}
		if audits, events := publicationCount(t, x, target); audits != 1 || events != 1 {
			t.Fatal("recovery produced duplicate publication facts")
		}
	})
}
