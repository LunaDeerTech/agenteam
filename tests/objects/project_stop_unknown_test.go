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
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Arming is based on the real same-Tx delta, not a brittle transaction number.
// The existing protocol proxy still intercepts the actual PostgreSQL COMMIT.
type stopCommitStore struct {
	*postgres.Store
	proxy     *commitProxy
	authority *objectStopAuthority
	mu        sync.Mutex
	phase     string
	hit       string
	identity  string
	armed     atomic.Bool
}
type stopTxFacts struct {
	stops, stopped, joined                                       int64
	preparation, reader, source, download, verification, cleanup int64
}

func (s *stopCommitStore) arm(phase string) {
	s.mu.Lock()
	s.phase = phase
	s.hit = ""
	s.identity = ""
	s.mu.Unlock()
	s.armed.Store(true)
}
func stopTransactionFacts(ctx context.Context, e postgres.SQLExecutor) (stopTxFacts, error) {
	var f stopTxFacts
	err := e.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_object.project_stops),(SELECT count(*) FROM agenteam_object.project_stops WHERE state='stopped'),count(*) FILTER(WHERE joined_at IS NOT NULL),count(*) FILTER(WHERE kind='preparation'),count(*) FILTER(WHERE kind='reader'),count(*) FILTER(WHERE kind='source'),count(*) FILTER(WHERE kind='download'),count(*) FILTER(WHERE kind='verification'),count(*) FILTER(WHERE kind='cleanup') FROM agenteam_object.project_work`).Scan(&f.stops, &f.stopped, &f.joined, &f.preparation, &f.reader, &f.source, &f.download, &f.verification, &f.cleanup)
	return f, err
}
func (s *stopCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		e, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		before, err := stopTransactionFacts(ctx, e)
		if err != nil {
			return err
		}
		previous, err := e.Query(ctx, `SELECT id::text FROM agenteam_object.project_work`)
		if err != nil {
			return err
		}
		var beforeIDs []string
		for previous.Next() {
			var id string
			if err = previous.Scan(&id); err != nil {
				previous.Close()
				return err
			}
			beforeIDs = append(beforeIDs, id)
		}
		err = previous.Err()
		previous.Close()
		if err != nil {
			return err
		}

		if err = fn(ctx, tx); err != nil {
			return err
		}
		after, err := stopTransactionFacts(ctx, e)
		if err != nil {
			return err
		}
		var ownStops, ownStopped, ownJoined int
		if err = e.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_object.project_stops WHERE xmin::text=pg_current_xact_id_if_assigned()::text),(SELECT count(*) FROM agenteam_object.project_stops WHERE xmin::text=pg_current_xact_id_if_assigned()::text AND state='stopped'),(SELECT count(*) FROM agenteam_object.project_work WHERE xmin::text=pg_current_xact_id_if_assigned()::text AND joined_at IS NOT NULL)`).Scan(&ownStops, &ownStopped, &ownJoined); err != nil {
			return err
		}
		phase := ""
		switch {
		case ownStopped > 0 && after.stopped > before.stopped:
			phase = "stopped"
		case ownStops > 0 && after.stops > before.stops:
			phase = "gate"
		case ownJoined > 0:
			phase = "join"
		}
		if phase == "" {
			var kind *string
			if err = e.QueryRow(ctx, `SELECT min(kind) FROM agenteam_object.project_work WHERE xmin::text=pg_current_xact_id_if_assigned()::text AND NOT(id=ANY($1::uuid[]))`, beforeIDs).Scan(&kind); err != nil {
				return err
			}
			if kind != nil {
				phase = *kind
			}
		}

		if phase == "" && s.authority != nil {
			s.authority.mu.Lock()
			_, valid := s.authority.validated[tx]
			s.authority.mu.Unlock()
			if valid && before == after && ownStops == 0 && ownJoined == 0 {
				phase = "preflight"
			}
		}
		s.mu.Lock()
		wanted := s.phase
		s.mu.Unlock()
		if phase == wanted && s.armed.CompareAndSwap(true, false) {
			var identity string
			if phase == "verification" || phase == "cleanup" {
				if err = e.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,project_id,process_id,kind,resource_id,object_id,cleanup_claim_fence,admission_version,admission_operation) ORDER BY id),'[]'::jsonb)::text FROM agenteam_object.project_work WHERE kind=$1 AND xmin::text=pg_current_xact_id_if_assigned()::text AND NOT(id=ANY($2::uuid[]))`, phase, beforeIDs).Scan(&identity); err != nil {
					return err
				}
				if phase == "cleanup" {
					var exact bool
					if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work w JOIN agenteam_object.cleanup_operations c ON c.id=w.resource_id AND c.worker_id=w.id AND c.fence=w.cleanup_claim_fence WHERE w.kind='cleanup' AND w.xmin::text=pg_current_xact_id_if_assigned()::text AND c.xmin=w.xmin)`).Scan(&exact); err != nil || !exact {
						return fault(foundation.InvalidState)
					}
				}
			}
			s.mu.Lock()
			s.hit = phase
			s.identity = identity
			s.mu.Unlock()
			s.proxy.armed.Store(1)
		}
		return nil
	})
}
func (s *stopCommitStore) assertHit(t *testing.T, want string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hit != want {
		t.Fatal("intercepted the wrong semantic COMMIT", s.hit, want)
	}
	t.Log("actual PostgreSQL COMMIT intercepted:", s.hit)
}

type measuredStopBody struct{ reads atomic.Int64 }

func (b *measuredStopBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	copy(p, "body")
	return 4, io.EOF
}
func (*measuredStopBody) Close() error { return nil }

func TestObjectProjectStopUnknownAdmissionNeverStartsIO(t *testing.T) {
	for _, kind := range []string{"preparation", "reader", "source", "download"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, false)
			stored := f.put(t, "admission-source", "body")
			raw, proxy := proxyStore(t, f, true)
			authority := newObjectStopAuthority(t, f, raw)
			wrapped := &stopCommitStore{Store: raw, proxy: proxy, authority: authority}
			httpProxy := newStorageProxy(t, f)
			s := newObjectStopService(t, f, authority, stopServiceOptions{store: raw, wrapper: wrapped, endpoint: httpProxy.server.URL})
			body := &measuredStopBody{}
			provider := newSourceAuthority(t, f, s, stored)
			provider.store = raw
			downloadProvider := &stopDownloadProvider{source: provider, entered: make(chan struct{}), release: make(chan struct{})}
			downloads := stopDownloads(t, s, downloadProvider)
			reads, err := object.NewSourceReads(s, provider)
			if err != nil {
				t.Fatal(err)
			}
			wrapped.arm(kind)
			done := make(chan error, 1)
			go func() {
				switch kind {
				case "preparation":
					_, err = s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, body)
				case "reader":
					var r *oc.ObjectReader
					r, err = s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
					if r != nil {
						_ = r.Close()
					}
				case "source":
					_, result := acquireSourceOn(t, f, s, reads, provider.source, wrapped, nil)
					err = result.Fault()
				case "download":
					_, err = downloads.IssueDownload(contextFor(t), f.actor, provider.source.Details().Reference, oc.DownloadAttachment, time.Minute)
				}
				done <- err
			}()
			select {
			case <-proxy.reached:
			case <-time.After(3 * time.Second):
				t.Fatal("actual admission COMMIT not intercepted")
			}
			wrapped.assertHit(t, kind)
			if body.reads.Load() != 0 || httpProxy.gets.Load() != 0 || httpProxy.puts.Load() != 0 {
				t.Fatal("external I/O preceded confirmed work admission")
			}
			select {
			case <-downloadProvider.entered:
				t.Fatal("resolver ran before confirmed admission")
			default:
			}
			close(proxy.release)
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("unknown admission returned success")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("unknown admission failed to return")
			}
			if body.reads.Load() != 0 || httpProxy.gets.Load() != 0 || httpProxy.puts.Load() != 0 {
				t.Fatal("unknown admission speculated external I/O")
			}
		})
	}
}

func TestObjectProjectStopUnknownPreflightNeverCancels(t *testing.T) {
	f := newFixture(t, false)
	raw, proxy := proxyStore(t, f, true)
	a := newObjectStopAuthority(t, f, raw)
	wrapped := &stopCommitStore{Store: raw, proxy: proxy, authority: a}
	s := newObjectStopService(t, f, a, stopServiceOptions{store: raw, wrapper: wrapped})
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	body := &stopBlockedBody{entered: entered, release: release}
	workDone := make(chan error, 1)
	go func() {
		_, err := s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 1, nil, body)
		workDone <- err
	}()
	<-entered
	actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
	wrapped.arm("preflight")
	done := make(chan error, 1)
	go func() { _, err := s.RequestProjectStop(contextFor(t), actor, cause); done <- err }()
	select {
	case <-proxy.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("preflight COMMIT not intercepted")
	}
	wrapped.assertHit(t, "preflight")
	close(proxy.release)
	requireCode(t, <-done, foundation.CommitUnknown)
	var gates int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_stops`).Scan(&gates); err != nil || gates != 0 {
		t.Fatal("unknown read-only preflight created gate", err)
	}
	if body.closes.Load() != 0 {
		t.Fatal("unknown preflight invoked cancellation callback")
	}
	select {
	case <-workDone:
		t.Fatal("unknown preflight cancelled the original work")
	default:
	}
	once.Do(func() { close(release) })
	<-workDone
}

func TestObjectProjectStopUnknownCheckpointWaitsForOriginalWriter(t *testing.T) {
	for _, phase := range []string{"gate", "join", "stopped"} {
		for _, late := range []bool{false, true} {
			t.Run(fmtStopPhase(phase, late), func(t *testing.T) {
				f := newFixture(t, false)
				raw, proxy := proxyStore(t, f, false)
				proxy.late.Store(late)
				a := newObjectStopAuthority(t, f, raw)
				wrapped := &stopCommitStore{Store: raw, proxy: proxy, authority: a}
				s := newObjectStopService(t, f, a, stopServiceOptions{store: raw, wrapper: wrapped})
				var prepared oc.PreparedPayload
				var err error
				if phase == "join" {
					prepared, err = s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
					if err != nil {
						t.Fatal(err)
					}
				}
				actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
				wrapped.arm(phase)
				done := make(chan error, 1)
				go func() {
					if phase == "join" {
						done <- s.DiscardPrepared(prepared)
					} else {
						ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
						defer cancel()
						_, err := s.RequestProjectStop(ctx, actor, cause)
						done <- err
					}
				}()
				select {
				case <-proxy.reached:
				case <-time.After(3 * time.Second):
					t.Fatal("target writer COMMIT not intercepted")
				}
				wrapped.assertHit(t, phase)
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
				report, err := s.RequestProjectStop(ctx, actor, cause)
				cancel()
				if err == nil || report.Details().State == oc.ProjectStopped {
					t.Fatal("unresolved original writer was inferred terminal", err)
				}
				close(proxy.release)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("unknown original phase did not return")
				}
				if late {
					select {
					case <-proxy.committed:
					case <-time.After(3 * time.Second):
						t.Fatal("original late COMMIT never became terminal")
					}
				}
				stopUntilSettled(t, s, actor, cause)
				var n int
				if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_stops WHERE project_id=$1`, f.project.String()).Scan(&n); err != nil || n != 1 {
					t.Fatal("unknown escaped through a replacement stop cause", err)
				}
			})
		}
	}
}
func fmtStopPhase(phase string, late bool) string {
	if late {
		return phase + "_late_commit"
	}
	return phase + "_rollback"
}

func TestObjectProjectStopUnknownMaintenanceNeverStartsIO(t *testing.T) {
	for _, kind := range []string{"verification", "cleanup"} {
		for _, outcome := range []string{"lost_response", "late_commit", "rollback"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				f := newFixture(t, true)
				raw, proxy := proxyStore(t, f, outcome == "lost_response")
				proxy.late.Store(outcome == "late_commit")
				a := newObjectStopAuthority(t, f, raw)
				wrapped := &stopCommitStore{Store: raw, proxy: proxy, authority: a}
				httpProxy := newStorageProxy(t, f)
				planner := &recoveryItemPlanner{base: &authority{store: raw}}
				process := id[oc.Process](t)
				s := newObjectStopService(t, f, a, stopServiceOptions{store: raw, wrapper: wrapped, endpoint: httpProxy.server.URL, planner: planner, process: process})
				f.service = s
				if kind == "verification" {
					planner.set(func(d oc.AccessRequestDetails) error {
						if d.Operation == oc.FinishWriterAccess {
							return fault(foundation.ResourceBusy)
						}
						return nil
					})
					_, err := s.PutObject(contextFor(t), f.actor, f.owner, command(t, "maintenance-unknown"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
					requireCode(t, err, foundation.ResourceBusy)
					planner.set(nil)
				} else {
					f.put(t, "maintenance-unknown", "body")
				}
				gets, markers, deletes := httpProxy.gets.Load(), httpProxy.markers.Load(), httpProxy.deletes.Load()
				wrapped.arm(kind)
				done := make(chan error, 1)
				go func() {
					if kind == "verification" {
						done <- s.Recover(contextFor(t))
					} else {
						_, err := s.CancelUpload(contextFor(t), f.actor, f.owner, "maintenance-unknown")
						done <- err
					}
				}()
				select {
				case <-proxy.reached:
				case <-time.After(3 * time.Second):
					t.Fatal("maintenance admission COMMIT not intercepted")
				}
				wrapped.assertHit(t, kind)
				wrapped.mu.Lock()
				original := wrapped.identity
				wrapped.mu.Unlock()
				if original == "" || original == "[]" {
					t.Fatal("intercepted transaction lacked exact work identity")
				}
				if httpProxy.gets.Load() != gets || httpProxy.markers.Load() != markers || httpProxy.deletes.Load() != deletes {
					t.Fatal("maintenance I/O preceded confirmed claim/work")
				}
				if outcome != "lost_response" {
					// The original transaction still owns the complete native writer
					// locks even if the client has already observed Unknown.
					ctx, cancel := context.WithTimeout(contextFor(t), 120*time.Millisecond)
					key, _ := foundation.ProjectLock(f.project.String())
					r := f.store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
						return f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}})
					})
					cancel()
					if r.State() == foundation.Committed {
						t.Fatal("original maintenance writer was not still holding project lock")
					}
				}
				close(proxy.release)
				select {
				case err := <-done:
					requireCode(t, err, foundation.CommitUnknown)
				case <-time.After(3 * time.Second):
					t.Fatal("maintenance unknown did not return")
				}
				if outcome == "late_commit" {
					select {
					case <-proxy.committed:
					case <-time.After(3 * time.Second):
						t.Fatal("original maintenance COMMIT never became terminal")
					}
				}
				if httpProxy.gets.Load() != gets || httpProxy.markers.Load() != markers || httpProxy.deletes.Load() != deletes {
					t.Fatal("unknown maintenance claim started I/O")
				}
				// Serialize once more on the same project lock, so absence means a
				// confirmed rollback rather than an original writer hidden by MVCC.
				var identity string
				key, _ := foundation.ProjectLock(f.project.String())
				r := f.store.WithinTx(contextFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
					e, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					return e.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,project_id,process_id,kind,resource_id,object_id,cleanup_claim_fence,admission_version,admission_operation) ORDER BY id),'[]'::jsonb)::text FROM agenteam_object.project_work WHERE kind=$1`, kind).Scan(&identity)
				})
				if r.State() != foundation.Committed {
					t.Fatal("could not confirm original maintenance writer", r.Fault())
				}
				if outcome == "rollback" {
					if identity != "[]" {
						t.Fatal("rolled-back maintenance identity survived", identity)
					}
				} else if identity != original {
					t.Fatal("Unknown changed original worker/work/fence/process", original, identity)
				}
				t.Log("confirmed original maintenance writer:", outcome, identity)
			})
		}
	}
}
