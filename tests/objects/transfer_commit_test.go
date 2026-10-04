//go:build integration

package objects_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestTransferUnknownIssueWaitsForOriginalWriterTerminal(t *testing.T) {
	f := newTransferFixture(t)
	network := newTransferProxy(t, f.fixture)
	store, proxy := proxyStore(t, f.fixture, false)
	proxy.late.Store(true)
	through := transferOn(t, f, store, network.config(t, f.fixture))
	spec := f.spec(t, []byte("original writer still owns command"))
	cmd := command(t, "late-transfer")
	proxy.armed.Store(1)
	var once sync.Once
	defer once.Do(func() { close(proxy.release) })
	returned := make(chan error, 1)
	go func() { _, err := through.transfers.IssueTransfer(contextFor(t), f.actor, cmd, spec); returned <- err }()
	select {
	case <-proxy.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("original COMMIT not held")
	}
	select {
	case err := <-returned:
		requireCode(t, err, foundation.CommitUnknown)
	case <-time.After(3 * time.Second):
		t.Fatal("disconnected Issue did not return unknown")
	}
	var count int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_transfers`).Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture did not retain uncommitted writer", count, err)
	}
	type issued struct {
		grant oc.TransferGrant
		err   error
	}
	replayed := make(chan issued, 1)
	go func() {
		g, e := through.transfers.IssueTransfer(contextFor(t), f.actor, cmd, spec)
		replayed <- issued{g, e}
	}()
	// Lock ordering chooses the existing upload command first; this is the same
	// exact lock held by the original, still uncommitted Issue transaction.
	c, _ := foundation.NewCommandIdentity("object", []string{f.project.String(), f.owner.Details().ID}, "put."+string(f.owner.Details().Kind), spec.Details().UploadCommand.IdempotencyKey)
	key, _ := foundation.CommandLock(c)
	waitAdvisory(t, f.fixture, key)
	select {
	case <-replayed:
		t.Fatal("unconfirmed writer was treated as absence")
	default:
	}
	if network.stagePUT.Load() != 0 || network.stageGET.Load() != 0 || network.candidatePUT.Load() != 0 {
		t.Fatal("unknown emitted speculative I/O")
	}
	once.Do(func() { close(proxy.release) })
	select {
	case <-proxy.committed:
	case <-time.After(3 * time.Second):
		t.Fatal("original COMMIT did not actually finish")
	}
	select {
	case r := <-replayed:
		requireCode(t, r.err, foundation.ResourceBusy)
		if _, e := r.grant.Material.ForRunner(f.runner, r.grant.Status.ID); e == nil {
			t.Fatal("changed plan produced material")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replay did not roll back changed mapping")
	}
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, cmd, spec)
	if err != nil {
		t.Fatal("explicit recollection failed", err)
	}
	var durable string
	if err = f.store.QueryRow(contextFor(t), `SELECT id::text FROM agenteam_object.object_transfers`).Scan(&durable); err != nil || durable != grant.Status.ID.String() {
		t.Fatal("late writer identity changed", err)
	}
	d := spec.Details()
	d.ExpiresInSeconds++
	changed, _ := oc.NewTransferSpec(d)
	_, err = through.transfers.IssueTransfer(contextFor(t), f.actor, cmd, changed)
	requireCode(t, err, foundation.IdempotencyKeyReused)
}

func TestTransferIssueUnknownNeverPublishesMaterial(t *testing.T) {
	for _, commit := range []int64{1, 2} {
		t.Run(map[int64]string{1: "grant", 2: "material"}[commit], func(t *testing.T) {
			f := newTransferFixture(t)
			network := newTransferProxy(t, f.fixture)
			store, proxy := proxyStore(t, f.fixture, true)
			through := transferOn(t, f, store, network.config(t, f.fixture))
			spec := f.spec(t, []byte("unknown grant"))
			command := command(t, "unknown-issue")
			proxy.armed.Store(commit)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			type result struct {
				grant oc.TransferGrant
				err   error
			}
			returned := make(chan result, 1)
			go func() {
				grant, err := through.transfers.IssueTransfer(ctx, f.actor, command, spec)
				returned <- result{grant, err}
			}()
			select {
			case <-proxy.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("real COMMIT response not intercepted")
			}
			cancel()
			var got result
			select {
			case got = <-returned:
			case <-time.After(3 * time.Second):
				t.Fatal("unknown Issue did not return")
			}
			requireCode(t, got.err, foundation.CommitUnknown)
			if _, err := got.grant.Material.ForRunner(f.runner, got.grant.Status.ID); err == nil {
				t.Fatal("unknown handed out signed material")
			}
			if network.stagePUT.Load() != 0 || network.stageGET.Load() != 0 || network.candidatePUT.Load() != 0 {
				t.Fatal("unconfirmed Issue emitted payload I/O")
			}
			close(proxy.release)
			var count int
			if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_transfers`).Scan(&count); err != nil || count != 1 {
				t.Fatal("actual committed grant absent", err)
			}
			replay, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command, spec)
			if err != nil {
				t.Fatal("confirmed replay", err)
			}
			changed := spec.Details()
			changed.ExpiresInSeconds++
			other, _ := oc.NewTransferSpec(changed)
			_, err = through.transfers.IssueTransfer(contextFor(t), f.actor, command, other)
			requireCode(t, err, foundation.IdempotencyKeyReused)
			var durable string
			if err = f.store.QueryRow(contextFor(t), `SELECT id::text FROM agenteam_object.object_transfers`).Scan(&durable); err != nil || durable != replay.Status.ID.String() {
				t.Fatal("replay changed durable grant", err)
			}
		})
	}
}

func TestTransferCaptureUnknownHasZeroSourceGETAndResumes(t *testing.T) {
	f := newTransferFixture(t)
	network := newTransferProxy(t, f.fixture)
	store, proxy := proxyStore(t, f.fixture, true)
	through := transferOn(t, f, store, network.config(t, f.fixture))
	body := []byte("one fixed staging read after capture confirmation")
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "capture-unknown"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, material, body); status != 200 {
		t.Fatal("PUT setup", status)
	}
	evidence := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	proxy.armed.Store(1)
	ctx, cancel := context.WithCancel(contextFor(t))
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := through.transfers.CompleteTransfer(ctx, f.actor, grant.Status.ID, evidence)
		returned <- err
	}()
	select {
	case <-proxy.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("capture COMMIT not intercepted")
	}
	cancel()
	select {
	case err = <-returned:
	case <-time.After(4 * time.Second):
		t.Fatal("capture did not join")
	}
	requireCode(t, err, foundation.CommitUnknown)
	if network.stageGET.Load() != 0 || network.candidatePUT.Load() != 0 {
		t.Fatal("unknown capture opened source")
	}
	close(proxy.release)
	var active int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='active'`, grant.Status.ObjectID.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("zero-I/O source cleanup did not converge", err)
	}
	state, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence)
	if err != nil || state.State != oc.TransferComplete {
		t.Fatal("confirmed capture retry", err)
	}
	// One GET reads the original nonempty staging stream; the second is the
	// required full empty-marker verification, not a second payload source.
	if network.stageGET.Load() != 2 || network.candidatePUT.Load() != 1 {
		t.Fatalf("physical I/O GET=%d candidatePUT=%d", network.stageGET.Load(), network.candidatePUT.Load())
	}
	beforeGET, beforePUT := network.stageGET.Load(), network.candidatePUT.Load()
	if _, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence); err != nil {
		t.Fatal(err)
	}
	if network.stageGET.Load() != beforeGET || network.candidatePUT.Load() != beforePUT {
		t.Fatal("completed replay repeated storage I/O")
	}
}

func TestTransferCandidateAndPublishUnknownKeepExactFacts(t *testing.T) {
	for _, stage := range []struct {
		name   string
		commit int64
	}{{"candidate", 4}, {"publish", 7}} {
		t.Run(stage.name, func(t *testing.T) {
			f := newTransferFixture(t)
			network := newTransferProxy(t, f.fixture)
			store, proxy := proxyStore(t, f.fixture, true)
			through := transferOn(t, f, store, network.config(t, f.fixture))
			body := []byte("candidate reservation must join before returning unknown")
			grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "unknown-candidate"), f.spec(t, body))
			if err != nil {
				t.Fatal(err)
			}
			material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
			if status, _ := transferRequest(t, through, material, body); status != 200 {
				t.Fatal(status)
			}
			proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
			proxy.armed.Store(stage.commit)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			returned := make(chan error, 1)
			go func() {
				_, e := through.transfers.CompleteTransfer(ctx, f.actor, grant.Status.ID, proof)
				returned <- e
			}()
			select {
			case <-proxy.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("target COMMIT not intercepted")
			}
			cancel()
			select {
			case err = <-returned:
			case <-time.After(4 * time.Second):
				t.Fatal("unknown candidate did not return")
			}
			requireCode(t, err, foundation.CommitUnknown)
			close(proxy.release)
			var phase string
			var active int
			if err = f.store.QueryRow(contextFor(t), `SELECT t.phase,(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=t.object_id AND owner_kind='writer' AND state='active') FROM agenteam_object.object_transfers t WHERE id=$1`, grant.Status.ID.String()).Scan(&phase, &active); err != nil {
				t.Fatal(err)
			}
			if stage.name == "candidate" {
				if network.candidatePUT.Load() != 0 || active != 0 || phase != "completing" {
					t.Fatalf("unconfirmed candidate: PUT=%d active_local_writer=%d phase=%s", network.candidatePUT.Load(), active, phase)
				}
				if _, err = through.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, command(t, "cancel-reserved-candidate")); err != nil {
					t.Fatal(err)
				}
				terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
				if _, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal); err != nil {
					t.Fatal(err)
				}
				if err = through.service.Recover(contextFor(t)); err != nil {
					t.Fatal(err)
				}
			} else {
				if phase != "complete" || active != 0 || network.candidatePUT.Load() != 1 {
					t.Fatal("not the actual publication COMMIT", phase, active)
				}
				get, put := network.stageGET.Load(), network.candidatePUT.Load()
				state, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
				if err != nil || state.State != oc.TransferComplete || network.stageGET.Load() != get || network.candidatePUT.Load() != put {
					t.Fatal("publication replay emitted I/O", err)
				}
			}
		})
	}
}
