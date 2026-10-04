//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type finalCleanupBoundary struct {
	oc.Cleaner
	oc.DownloadCleanup
	after func() error
	once  sync.Once
}

func (c *finalCleanupBoundary) PurgeProjectDownloadsInTx(ctx context.Context, tx foundation.Tx, a identity.Actor, cause oc.ProjectCleanupCause, p oc.AccessLockPlan, l oc.LockedAccess) error {
	if err := c.DownloadCleanup.PurgeProjectDownloadsInTx(ctx, tx, a, cause, p, l); err != nil {
		return err
	}
	var err error
	c.once.Do(func() { err = c.after() })
	return err
}
func (f *artifactFixture) withCleaner(t *testing.T, cleaner oc.Cleaner) *artifact.Service {
	t.Helper()
	owners, err := artifact.NewOwnerProvider(f.auth.store, f.auth)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := object.NewSourceReads(f.objects, f.resolver)
	if err != nil {
		t.Fatal(err)
	}
	s, err := artifact.New(f.auth.store, owners, f.objects, f.objects, cleaner, reads, f.resolver, f.auditing, f.keys)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (f *artifactFixture) deleting(t *testing.T) (identity.Actor, oc.ProjectCleanupCause) {
	t.Helper()
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1,version=2 WHERE id=$2`, operation.String(), f.project.String())
	cause, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 2})
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(f.project)
	actor, _ := registration.Actor(operation.String(), scope)
	return actor, cause
}
func (f *artifactFixture) assertPurged(t *testing.T) {
	t.Helper()
	var n int
	err := f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1)+(SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1)+(SELECT count(*) FROM agenteam_artifact.upload_intents WHERE project_id=$1)+(SELECT count(*) FROM agenteam_download.grants WHERE project_id=$1)+(SELECT count(*) FROM agenteam_download.attempts)`, f.project.String()).Scan(&n)
	if err != nil || n != 0 {
		t.Fatal("permanent cleanup retained business facts", n, err)
	}
	var state string
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_artifact.cleanup WHERE project_id=$1`, f.project.String()).Scan(&state); err != nil || state != "completed" {
		t.Fatal("minimal cleanup receipt", state, err)
	}
}

func TestArtifactDownloadPurgeRequiresPlansPhysicalCompletionAndBoundPort(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	m := f.create(t, "purge-preconditions", "text/plain", "guarded")
	d := f.downloads(t, f.sources)
	_ = f.downloadToken(t, d, m, oc.DownloadAttachment)
	actor, cleanup := f.deleting(t)
	request, err := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.FinishProjectAccess, Actor: actor, ProjectCleanup: cleanup})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.objects.DiscoverAccess(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, wrongToken := range []bool{true, false} {
		result := f.store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
			locked, err := f.objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
			if err != nil {
				return err
			}
			if wrongToken {
				locked = oc.LockedAccess{}
			}
			return f.objects.PurgeProjectDownloadsInTx(ctx, tx, actor, cleanup, plan, locked)
		})
		if result.State() != foundation.NotCommitted || result.Fault() == nil {
			t.Fatal("premature/unlocked purge succeeded")
		}
		if !wrongToken {
			requireCode(t, result.Fault(), foundation.ResourceBusy)
		}
	}
	// Hiding the capability is a real unbound composition, never successful
	// deletion. Physical cleanup may finish, but domain recovery facts remain.
	unbound := f.withCleaner(t, struct{ oc.Cleaner }{f.objects})
	out, err := unbound.CleanupProject(ctx, actor, cleanup)
	requireCode(t, err, foundation.DependencyUnbound)
	if out.State != oc.CleanupPending {
		t.Fatal("unbound download cleanup claimed complete")
	}
	var commands, grants int
	if err = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.commands),(SELECT count(*) FROM agenteam_download.grants)`).Scan(&commands, &grants); err != nil || commands != 1 || grants != 1 {
		t.Fatal("unbound port erased recovery facts", commands, grants, err)
	}
	out, err = f.artifact.CleanupProject(ctx, actor, cleanup)
	if err != nil || out.State != oc.CleanupCompleted {
		t.Fatal("bound recovery", out, err)
	}
	f.assertPurged(t)
}

func TestArtifactFinalCleanupAtomicRollbackAndRealCommitUnknown(t *testing.T) {
	for _, mode := range []string{"rollback", "unknown_rollback", "unknown_commit", "late_commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newArtifactFixture(t)
			ctx := contextFor(t)
			m := f.create(t, "final-inline", "text/plain", "private-body")
			target, err := f.artifact.BeginUpload(ctx, f.invocation(t), command(t, "final-intent"), art.Display{Name: "private-intent.txt", Description: "private-description"}, "text/plain", 3, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("raw"))); err != nil {
				t.Fatal(err)
			}
			d := f.downloads(t, f.sources)
			token := f.downloadToken(t, d, m, oc.DownloadAttachment)
			if _, _, e, out := downloadHTTP(t, d, f.actor, token, "", 0, nil); e != nil || out.err != nil {
				t.Fatal(e, out.err)
			}
			on := f
			var proxy *commitProxy
			if mode != "rollback" {
				store, p := proxyStore(t, f.fixture, mode == "unknown_commit")
				proxy = p
				if mode == "late_commit" {
					p.late.Store(true)
				}
				on = f.bindOn(t, store, nil, nil)
			}
			boundary := &finalCleanupBoundary{Cleaner: on.objects, DownloadCleanup: on.objects, after: func() error {
				if mode == "rollback" {
					return fault(foundation.InvalidState)
				}
				proxy.armed.Store(1)
				return nil
			}}
			s := on.withCleaner(t, boundary)
			actor, cause := f.deleting(t)
			type outcome struct {
				value oc.ProjectCleanupResult
				err   error
			}
			done := make(chan outcome, 1)
			go func() { v, e := s.CleanupProject(ctx, actor, cause); done <- outcome{v, e} }()
			if proxy != nil {
				var once sync.Once
				release := func() { once.Do(func() { close(proxy.release) }) }
				defer release()
				select {
				case <-proxy.reached:
				case <-time.After(5 * time.Second):
					t.Fatal("actual final COMMIT not intercepted")
				}
				if mode == "late_commit" {
					select {
					case out := <-done:
						requireCode(t, out.err, foundation.CommitUnknown)
						if out.value.State != oc.CleanupPending {
							t.Fatal("unknown claimed complete")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("unknown did not return")
					}
					wait, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
					out, e := f.artifact.CleanupProject(wait, actor, cause)
					cancel()
					if e == nil || out.State != oc.CleanupPending {
						t.Fatal("cleanup raced past nonterminal original writer")
					}
					release()
					select {
					case <-proxy.committed:
					case <-time.After(3 * time.Second):
						t.Fatal("late actual final COMMIT not terminal")
					}
				} else {
					release()
					select {
					case out := <-done:
						requireCode(t, out.err, foundation.CommitUnknown)
						if out.value.State != oc.CleanupPending {
							t.Fatal("unknown claimed complete")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("unknown did not return")
					}
				}
			} else {
				out := <-done
				requireCode(t, out.err, foundation.InvalidState)
				if out.value.State != oc.CleanupPending {
					t.Fatal("rolled-back cleanup claimed complete")
				}
			}
			if mode == "rollback" || mode == "unknown_rollback" {
				var commands, intents, grants, attempts, objects int
				err = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.commands WHERE name='下载-canary.txt'),(SELECT count(*) FROM agenteam_artifact.upload_intents WHERE name='private-intent.txt'),(SELECT count(*) FROM agenteam_download.grants),(SELECT count(*) FROM agenteam_download.attempts),(SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1)`, f.project.String()).Scan(&commands, &intents, &grants, &attempts, &objects)
				if err != nil || commands != 1 || intents != 1 || grants != 1 || attempts != 1 || objects != 0 {
					t.Fatal("final rollback lost recovery facts or physical cleanup", commands, intents, grants, attempts, objects, err)
				}
			}
			out, err := f.artifact.CleanupProject(ctx, actor, cause)
			if err != nil || out.State != oc.CleanupCompleted {
				t.Fatal("final cleanup recovery", out, err)
			}
			f.assertPurged(t)
		})
	}
}

func TestArtifactFinalCleanupLateDownloadAndNewKeysCannotRevive(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	m := f.create(t, "old-key", "text/plain", "buffered")
	d := f.downloads(t, f.sources)
	token := f.downloadToken(t, d, m, oc.DownloadAttachment)
	// Object's small reader has genuinely reached EOF/join, while the admitted
	// browser attempt has not yet written a terminal checkpoint.
	stream, err := d.OpenDownload(ctx, f.actor, token, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var leases int
	var attempt string
	if err = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'),(SELECT id::text FROM agenteam_download.attempts)`).Scan(&leases, &attempt); err != nil || leases != 0 {
		t.Fatal("reader not actually joined", leases, err)
	}
	actor, cause := f.deleting(t)
	out, err := f.artifact.CleanupProject(ctx, actor, cause)
	if err != nil || out.State != oc.CleanupCompleted {
		t.Fatal(out, err)
	}
	f.assertPurged(t)
	if err = stream.Close(); err == nil {
		t.Fatal("late finalizer ignored removed grant")
	}
	f.assertPurged(t)
	attemptID, err := foundation.ParseID[oc.DownloadAttempt](attempt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.ConfirmDownloadOutcome(ctx, f.actor, attemptID); err == nil {
		t.Fatal("removed attempt recreated")
	}
	gets, puts := f.proxy.gets.Load(), f.proxy.puts.Load()
	// Even an accidentally stale external active Project grant cannot erase the
	// domain's permanent gate. Current authorization is still required first.
	f.sql(t, `UPDATE object_fixture.projects SET state='active' WHERE id=$1`, f.project.String())
	for _, key := range []string{"old-key", "new-key"} {
		_, err = f.artifact.CreateFromContent(ctx, f.invocation(t), command(t, key), art.Display{Name: "new.txt"}, "text/plain", "new")
		requireCode(t, err, foundation.ResourceDeleted)
	}
	_, err = f.artifact.BeginUpload(ctx, f.invocation(t), command(t, "new-intent"), art.Display{Name: "new.txt"}, "text/plain", 3, nil)
	requireCode(t, err, foundation.ResourceDeleted)
	owner, _ := oc.NewObjectOwner(oc.Artifact, id[art.ArtifactEntity](t).String(), f.project.String())
	_, err = f.objects.PreparePayload(ctx, f.actor, owner, "text/plain", 3, nil, io.NopCloser(strings.NewReader("new")))
	requireCode(t, err, foundation.ResourceDeleted)
	if f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts {
		t.Fatal("terminal gate emitted storage I/O")
	}
	f.assertPurged(t)
}
