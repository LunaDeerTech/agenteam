//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type stopBlockedBody struct {
	entered, release chan struct{}
	once             sync.Once
	closes           atomic.Int64
}

func (b *stopBlockedBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}
func (b *stopBlockedBody) Close() error { b.closes.Add(1); <-b.release; return nil }

func TestObjectProjectStopArchivePreservesPublishedReads(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	body := strings.Repeat("published payload\n", 10000)
	stored := f.put(t, "archive-body", body)
	reader, err := s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	stopUntilSettled(t, s, actor, cause)
	got, err := io.ReadAll(reader)
	if err != nil || digest(got) != stored.Meta.SHA256 {
		t.Fatal("archive interrupted a legal current reader", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	provider := newSourceAuthority(t, f, s, stored)
	downloads := stopDownloads(t, s, &stopDownloadProvider{source: provider})
	url, err := downloads.IssueDownload(contextFor(t), f.actor, provider.source.Details().Reference, oc.DownloadAttachment, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := url.ForHuman(f.actor)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, readErr, out := downloadHTTP(t, downloads, f.actor, strings.TrimPrefix(wire.URL, "/api/object-downloads/"), "", 0, nil)
	if readErr != nil || out.err != nil || digest(payload) != stored.Meta.SHA256 {
		t.Fatal("archive download changed SHA", readErr, out.err)
	}
	var references int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1 AND kind='canonical'`, stored.Meta.ID.String()).Scan(&references); err != nil || references != 1 {
		t.Fatal("archive removed canonical reference", err)
	}
}

func TestObjectProjectStopDeleteWaitsForActualIO(t *testing.T) {
	t.Run("small_body_already_joined_before_return", func(t *testing.T) {
		f := newFixture(t, false)
		a := newObjectStopAuthority(t, f, f.store)
		planner := &recoveryItemPlanner{base: f.authority}
		process := id[oc.Process](t)
		s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner, process: process})
		f.service = s
		stored := f.put(t, "small-reader", "body")
		var released atomic.Int64
		planner.set(func(d oc.AccessRequestDetails) error {
			if d.Operation == oc.ReleaseReaderAccess {
				released.Add(1)
			}
			return nil
		})
		reader, err := s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if released.Load() != 1 {
			t.Fatal("small body did not synchronously release before ReadObject returned")
		}
		assertStopReaderLifetime(t, f, stored.Meta.ID, process, false)
		actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
		stopUntilSettled(t, s, actor, cause)
	})
	for _, phase := range []string{"body_read_close", "reader_release_callback"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t, false)
			a := newObjectStopAuthority(t, f, f.store)
			planner := &recoveryItemPlanner{base: f.authority}
			proxy := newStorageProxy(t, f)
			process := id[oc.Process](t)
			s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner, endpoint: proxy.server.URL, process: process})
			f.service = s
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			done := make(chan error, 1)
			if phase == "body_read_close" {
				body := &stopBlockedBody{entered: entered, release: release}
				go func() {
					_, err := s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 1, nil, body)
					done <- err
				}()
			} else {
				stored := f.put(t, "release-tail", strings.Repeat("body", oc.StreamBufferSize))
				proxy.mode.Store(proxyHoldReadBody)
				reader, err := s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				assertStopBodyHeld(t, proxy)
				assertStopReaderLifetime(t, f, stored.Meta.ID, process, true)
				var hit sync.Once
				planner.set(func(d oc.AccessRequestDetails) error {
					if d.Operation == oc.ReleaseReaderAccess {
						hit.Do(func() { close(entered) })
						<-release
					}
					return nil
				})
				go func() { done <- reader.Close() }()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("release callback did not enter its actual barrier")
				}
				assertStopReaderLifetime(t, f, stored.Meta.ID, process, true)
			}
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("actual lifetime barrier not reached")
			}
			actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
			report, err := s.RequestProjectStop(contextFor(t), actor, cause)
			if err != nil || report.Details().State != oc.ProjectStopPending {
				t.Fatal("cancel was mistaken for actual join", err, report.Details())
			}
			select {
			case <-done:
				t.Fatal("blocked I/O/callback was forgotten")
			default:
			}
			once.Do(func() { close(release) })
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("actual work did not return")
			}
			planner.set(nil)
			stopUntilSettled(t, s, actor, cause)
		})
	}
}

type stopDownloadProvider struct {
	source           *sourceAuthority
	entered, release chan struct{}
	once             sync.Once
}

func (p *stopDownloadProvider) ResolveDownload(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef) (oc.DownloadTarget, error) {
	if p.entered != nil {
		p.once.Do(func() { close(p.entered) })
		select {
		case <-p.release:
		case <-ctx.Done():
			return oc.DownloadTarget{}, ctx.Err()
		}
	}
	s, err := p.source.Resolve(ctx, actor, ref)
	if err != nil {
		return oc.DownloadTarget{}, err
	}
	return oc.NewDownloadTarget(oc.DownloadTargetDetails{Source: s, Filename: "payload.txt"})
}
func (p *stopDownloadProvider) ValidateDownloadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, target oc.DownloadTarget, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	return p.source.ValidateInTx(ctx, tx, actor, target.Details().Source, plan, locked)
}
func (p *stopDownloadProvider) AppendDownloadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, target oc.DownloadTarget, event oc.DownloadEvent, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if err := event.Validate(); err != nil {
		return err
	}
	return p.ValidateDownloadInTx(ctx, tx, actor, target, plan, locked)
}
func stopDownloads(t *testing.T, s *object.Service, provider oc.DownloadProvider) *object.Downloads {
	t.Helper()
	material := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	keys, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":%q}]}`, material(31)))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, material(32)), keys)
	if err != nil {
		t.Fatal(err)
	}
	download, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":%q}]}`, material(33)), keys, secrets)
	if err != nil {
		t.Fatal(err)
	}
	d, err := object.NewDownloads(s, provider, download)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestObjectProjectStopRegistersBeforeResolver(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	stored := f.put(t, "resolver-source", "body")
	provider := &stopDownloadProvider{source: newSourceAuthority(t, f, s, stored), entered: make(chan struct{}), release: make(chan struct{})}
	d := stopDownloads(t, s, provider)
	done := make(chan error, 1)
	go func() {
		_, err := d.IssueDownload(contextFor(t), f.actor, provider.source.source.Details().Reference, oc.DownloadAttachment, time.Minute)
		done <- err
	}()
	select {
	case <-provider.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("resolver did not begin")
	}
	if projectWorkCount(t, f, "download", false) != 1 {
		t.Fatal("resolver preceded durable admission")
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
	_, err := s.RequestProjectStop(contextFor(t), actor, cause)
	if err != nil && codeOf(err) != foundation.ResourceBusy {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked resolver issued URL")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registered resolver was not cancelled")
	}
	stopUntilSettled(t, s, actor, cause)
}

func TestObjectProjectStopUnopenedSourceAndCrossProjectRelations(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	stored := f.put(t, "source-relation", "body")
	provider := newSourceAuthority(t, f, s, stored)
	reads, err := object.NewSourceReads(s, provider)
	if err != nil {
		t.Fatal(err)
	}
	lease, result := acquireSource(t, f, s, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if projectWorkCount(t, f, "source", false) != 1 {
		t.Fatal("unopened source missing persistent work")
	}
	otherProject := id[identity.Project](t)
	otherOwner, _ := oc.NewObjectOwner(oc.Artifact, id[struct{}](t).String(), otherProject.String())
	f.sql(t, `INSERT INTO object_fixture.projects(id,owner_id) VALUES($1,$2)`, otherProject.String(), f.actor.Details().UserID)
	f.sql(t, `INSERT INTO object_fixture.owners(id,kind,project_id,user_id,existence) VALUES($1,'artifact',$2,$3,'existing')`, otherOwner.Details().ID, otherProject.String(), f.actor.Details().UserID)
	prepared, err := s.PreparePayload(contextFor(t), f.actor, otherOwner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("copy")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DiscardPrepared(prepared)
	var projects int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(DISTINCT project_id) FROM agenteam_object.project_work WHERE joined_at IS NULL`).Scan(&projects); err != nil || projects != 2 {
		t.Fatal("source and destination overwrote one relation", err)
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
	stopUntilSettled(t, s, actor, cause)
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease); err == nil {
		t.Fatal("unopened revoked source performed GET")
	}
	cmd := command(t, "independent-target")
	plan := ownerPlan(t, s, f.actor, otherOwner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared})
	result = plannedTx(f.store, s, contextFor(t), causeForStop(t), plan, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
		_, err := s.ReserveUploadInTx(ctx, tx, f.actor, otherOwner, cmd, prepared, p, l)
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal("source stop cancelled independent target", result.Fault())
	}
}
func causeForStop(t *testing.T) foundation.TransactionCause { return cause(t) }

func TestObjectProjectStopVerificationOwnsItsLifetime(t *testing.T) {
	for _, action := range []string{"archive", "delete", "discard"} {
		t.Run("standalone_writer_tail_"+action, func(t *testing.T) { testStopStandaloneWriterTail(t, action) })
	}
	f := newFixture(t, true)
	a := newObjectStopAuthority(t, f, f.store)
	planner := &recoveryItemPlanner{base: f.authority}
	s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner})
	f.service = s
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.FinishWriterAccess {
			return fault(foundation.ResourceBusy)
		}
		return nil
	})
	_, err := s.PutObject(contextFor(t), f.actor, f.owner, command(t, "verify-lifetime"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	requireCode(t, err, foundation.ResourceBusy)
	entered, release := make(chan struct{}), make(chan struct{})
	var hit, once sync.Once
	defer once.Do(func() { close(release) })
	calls := 0
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.RecoverAttemptAccess {
			calls++
			if calls == 2 {
				hit.Do(func() { close(entered) })
				<-release
			}
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- s.Recover(contextFor(t)) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("independent real verification tail not reached")
	}
	if projectWorkCount(t, f, "verification", false) != 1 {
		t.Fatal("original uploader join substituted for verifier lifetime")
	}
	var closed bool
	if err = f.store.QueryRow(contextFor(t), `SELECT io_closed FROM agenteam_object.upload_attempts`).Scan(&closed); err != nil || !closed {
		t.Fatal("original upload was not already joined", err)
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	report, err := s.RequestProjectStop(contextFor(t), actor, cause)
	if err != nil || report.Details().State != oc.ProjectStopPending {
		t.Fatal("archive ignored verifier tail", err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("verifier failed to join")
	}
	planner.set(nil)
	stopUntilSettled(t, s, actor, cause)
}

func TestObjectProjectStopCleanupWaitsForActualIOAndCallbacks(t *testing.T) {
	for _, phase := range []string{"zero", "remove", "callback"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t, true)
			a := newObjectStopAuthority(t, f, f.store)
			network := newStorageProxy(t, f)
			planner := &recoveryItemPlanner{base: f.authority}
			entered, release := make(chan struct{}), make(chan struct{})
			var hit, once sync.Once
			var armed atomic.Bool
			target, err := url.Parse(network.server.URL)
			if err != nil {
				t.Fatal(err)
			}
			forward := httputil.NewSingleHostReverseProxy(target)
			barrier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				matches := strings.Contains(r.URL.Path, "/candidate/") && (phase == "zero" && r.Method == http.MethodPut || phase == "remove" && r.Method == http.MethodDelete)
				if armed.Load() && matches {
					hit.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				forward.ServeHTTP(w, r)
			}))
			defer barrier.Close()
			defer once.Do(func() { close(release) })
			process := id[oc.Process](t)
			s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner, endpoint: barrier.URL, process: process})
			// The uploader has already closed/joined, and belongs to a different
			// Service process. It cannot account for the later cleanup worker.
			uploader, _ := f.on(t, f.store, network.server.URL, nil)
			f.service = uploader
			if phase == "zero" {
				network.mode.Store(proxyLosePutResponse)
			}
			f.put(t, "cleanup-lifetime", "body")
			network.mode.Store(proxyPass)
			if phase == "callback" {
				planner.set(func(d oc.AccessRequestDetails) error {
					if d.Operation == oc.CheckpointCleanupAccess {
						hit.Do(func() { close(entered) })
						<-release
					}
					return nil
				})
			}
			armed.Store(true)
			done := make(chan error, 1)
			caller, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			go func() { _, err := s.CancelUpload(caller, f.actor, f.owner, "cleanup-lifetime"); done <- err }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("real cleanup did not reach", phase)
			}
			if projectWorkCount(t, f, "cleanup", false) != 1 {
				t.Fatal("cleanup claim did not retain its independent work")
			}
			var exact bool
			err = f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work w JOIN agenteam_object.cleanup_operations c ON c.worker_id=w.id AND c.id=w.resource_id AND c.fence=w.cleanup_claim_fence JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id WHERE w.kind='cleanup' AND c.phase='applying' AND w.process_id=$1 AND a.process_id<>w.process_id AND a.io_closed AND w.joined_at IS NULL AND c.mode=$2)`, process.String(), map[bool]string{true: "zero_marker", false: "delete"}[phase == "zero"]).Scan(&exact)
			if err != nil || !exact {
				t.Fatal("cleanup work not tied to actual worker/fence/process", err)
			}
			// Both caller cancellation and project cancellation must leave the
			// independently bounded storage call/callback in the active lifetime.
			cancel()
			actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
			report, err := s.RequestProjectStop(contextFor(t), actor, cause)
			if err != nil || report.Details().State != oc.ProjectStopPending {
				t.Fatal("cleanup I/O/callback was not joined", phase, err)
			}
			select {
			case <-done:
				t.Fatal("cancel guessed actual cleanup join", phase)
			default:
			}
			once.Do(func() { close(release) })
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("cleanup did not return", phase)
			}
			if phase == "zero" && network.markers.Load() != 1 || phase != "zero" && network.deletes.Load() != 1 {
				t.Fatal("held operation did not execute exactly once against MinIO", phase, network.markers.Load(), network.deletes.Load())
			}
			planner.set(nil)
			stopUntilSettled(t, s, actor, cause)
		})
	}
}

// A separately prepared payload owns its operation without an enclosing
// PutObject defer. The PUT body is already closed when FinishWriter is entered;
// its actual DB/Audit tail still owns the writer and the service lifetime.
func testStopStandaloneWriterTail(t *testing.T, action string) {
	f := newFixture(t, true)
	a := newObjectStopAuthority(t, f, f.store)
	planner := &recoveryItemPlanner{base: f.authority}
	network := newStorageProxy(t, f)
	s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner, endpoint: network.server.URL})
	payload := strings.Repeat("actual standalone writer body\n", 10000)
	prepared, err := s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", int64(len(payload)), nil, io.NopCloser(strings.NewReader(payload)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DiscardPrepared(prepared)
	meta := command(t, "standalone-tail")
	plan := ownerPlan(t, s, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &meta, Prepared: prepared})
	var attempt oc.UploadAttempt
	commit := plannedTx(f.store, s, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var err error
		attempt, err = s.ReserveUploadInTx(ctx, tx, f.actor, f.owner, meta, prepared, plan, locked)
		return err
	})
	if commit.State() != foundation.Committed {
		t.Fatal(commit.Fault())
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var hit, once sync.Once
	defer once.Do(func() { close(release) })
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.FinishWriterAccess {
			hit.Do(func() { close(entered) })
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.UploadPrepared(contextFor(t), f.actor, f.owner, prepared, attempt); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("actual standalone writer never reached its tail")
	}
	if network.puts.Load() != 1 || network.gets.Load() != 1 {
		t.Fatal("tail barrier preceded real PUT/verification", network.puts.Load(), network.gets.Load())
	}
	assertActive := func() {
		t.Helper()
		var active bool
		err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a JOIN agenteam_object.object_leases l ON l.object_id=a.object_id AND l.owner_kind='writer' JOIN agenteam_object.project_work w ON w.resource_id=a.id AND w.kind='preparation' WHERE a.id=$1 AND NOT a.io_closed AND a.phase='sending' AND l.state='active' AND w.joined_at IS NULL)`, attempt.Details().ID.String()).Scan(&active)
		if err != nil || !active {
			t.Error("live standalone writer was falsely checkpointed as joined", err)
		}
	}
	assertActive()
	var actor identity.Actor
	var stop oc.ProjectStopCause
	if action != "discard" {
		actor, stop = activateObjectStop(t, f, oc.ProjectStopAction(action))
		report, err := s.RequestProjectStop(contextFor(t), actor, stop)
		if err != nil || report.Details().State != oc.ProjectStopPending {
			t.Error("actual writer tail did not keep first stop pending", err, report.Details())
		}
	}
	// This is after actual spool.Close and before the native writer's tail.
	// ResourceBusy must cover the full writer, not merely the file body.
	if err = s.DiscardPrepared(prepared); codeOf(err) != foundation.ResourceBusy {
		t.Error("DiscardPrepared released an actual live writer", err)
	}
	if action != "discard" {
		for range 6 {
			report, err := s.RequestProjectStop(contextFor(t), actor, stop)
			if err != nil || report.Details().State != oc.ProjectStopPending {
				t.Error("stop inferred join from discarded spool", err, report.Details())
				break
			}
		}
	}
	assertActive()
	ctx, cancel := context.WithTimeout(contextFor(t), 100*time.Millisecond)
	err = s.Drain(ctx)
	cancel()
	if err == nil {
		t.Error("service/guard drain lost actual standalone writer")
	}
	select {
	case <-done:
		t.Fatal("blocked real writer returned before release")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("actual writer did not return")
	}
	planner.set(nil)
	if action == "discard" {
		if err = s.DiscardPrepared(prepared); err != nil {
			t.Fatal("joined writer could not discard", err)
		}
	} else {
		// The cancellation AfterFunc fired while the writer was active. The
		// writer's actual return must complete that deferred discard itself.
		stopUntilSettled(t, s, actor, stop)
	}
	ctx, cancel = context.WithTimeout(contextFor(t), 300*time.Millisecond)
	err = s.Drain(ctx)
	cancel()
	if err != nil {
		t.Fatal("actual joined writer left operation/guard drain pending", err)
	}
}
