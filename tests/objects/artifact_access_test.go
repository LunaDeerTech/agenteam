//go:build integration

package objects_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type afterArtifactUpload struct {
	oc.Uploads
	after func() error
	once  sync.Once
}

func (u *afterArtifactUpload) UploadPrepared(ctx context.Context, a identity.Actor, o oc.ObjectOwner, p oc.PreparedPayload, attempt oc.UploadAttempt) (oc.UploadAttempt, error) {
	v, err := u.Uploads.UploadPrepared(ctx, a, o, p, attempt)
	if err == nil {
		u.once.Do(func() { err = u.after() })
	}
	return v, err
}

type measuredArtifactSource struct {
	oc.SourceReads
	opens atomic.Int64
}

func (r *measuredArtifactSource) OpenLeasedSource(ctx context.Context, actor identity.Actor, lease oc.SourceLease) (*oc.ObjectReader, error) {
	reader, err := r.SourceReads.OpenLeasedSource(ctx, actor, lease)
	if err == nil {
		r.opens.Add(1)
	}
	return reader, err
}

func (f *artifactFixture) withUploads(t *testing.T, uploads oc.Uploads) (*artifact.Service, *measuredArtifactSource) {
	t.Helper()
	owners, err := artifact.NewOwnerProvider(f.store, f.auth)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := object.NewSourceReads(f.objects, f.resolver)
	if err != nil {
		t.Fatal(err)
	}
	measured := &measuredArtifactSource{SourceReads: reads}
	s, err := artifact.New(f.store, owners, f.objects, uploads, f.objects, measured, f.resolver, f.auditing, f.keys)
	if err != nil {
		t.Fatal(err)
	}
	return s, measured
}

func TestArtifactSourceCurrentRevocationBlocksPublishAndRecoveryKeepsOriginal(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	sourceProject := id[identity.Project](t)
	f.sql(t, `INSERT INTO object_fixture.projects(id,owner_id) VALUES($1,$2)`, sourceProject.String(), f.actor.Details().UserID)
	sourceInvocation, err := art.NewInvocation(art.InvocationDetails{Actor: f.actor, ProjectID: sourceProject, AttemptID: id[art.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	original, err := f.artifact.CreateFromContent(ctx, sourceInvocation, command(t, "source-original"), art.Display{Name: "source.txt"}, "text/plain", strings.Repeat("source", 20000))
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := original.Details().Reference.BusinessFile()
	projectKey, _ := foundation.ProjectLock(sourceProject.String())
	revocation := cause(t)
	foreignOwner := id[identity.User](t)
	uploads := &afterArtifactUpload{Uploads: f.objects, after: func() error {
		result := f.store.WithinTx(ctx, revocation, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			e, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			_, err = e.Exec(ctx, `UPDATE object_fixture.projects SET owner_id=$2 WHERE id=$1`, sourceProject.String(), foreignOwner.String())
			return err
		})
		if result.State() != foundation.Committed {
			return result.Fault()
		}
		return nil
	}}
	s, sourceReads := f.withUploads(t, uploads)
	cmd := command(t, "recover-source-permission")
	display := art.Display{Name: "independent-copy.txt"}
	beforeGet, beforePut := f.proxy.gets.Load(), f.proxy.puts.Load()
	_, err = s.CreateFromSource(ctx, f.invocation(t), cmd, display, ref)
	requireCode(t, err, foundation.Forbidden)
	if sourceReads.opens.Load() != 1 || f.proxy.gets.Load() <= beforeGet || f.proxy.puts.Load() <= beforePut {
		t.Fatal("revocation did not follow the real source stream and destination upload")
	}
	var published, canonical int
	var captured, target, state string
	err = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1),(SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical' AND object_id IN(SELECT target_object_id FROM agenteam_artifact.commands WHERE project_id=$1))`, f.project.String()).Scan(&published, &canonical)
	if err != nil || published != 0 || canonical != 0 {
		t.Fatal("current source denial leaked publication", published, canonical, err)
	}
	err = f.store.QueryRow(ctx, `SELECT source::text,target_object_id::text,state FROM agenteam_artifact.commands WHERE command_key=$1`, string(cmd.IdempotencyKey)).Scan(&captured, &target, &state)
	if err != nil || state != "pending" || !strings.Contains(captured, original.Details().Object.ID.String()) {
		t.Fatal("original pending source fact lost", err, state)
	}
	// Recomposition loses all local handles. The committed source lease has
	// actually closed, so a new service may recover only the captured source.
	restarted := f.bindOn(t, f.store, nil, nil)
	beforeGet, beforePut = f.proxy.gets.Load(), f.proxy.puts.Load()
	_, err = restarted.artifact.CreateFromSource(ctx, restarted.invocation(t), cmd, display, ref)
	requireCode(t, err, foundation.Forbidden)
	if f.proxy.gets.Load() != beforeGet || f.proxy.puts.Load() != beforePut {
		t.Fatal("revoked recovery performed storage I/O")
	}
	f.sql(t, `UPDATE object_fixture.projects SET owner_id=$2 WHERE id=$1`, sourceProject.String(), f.actor.Details().UserID)
	result, err := restarted.artifact.CreateFromSource(ctx, restarted.invocation(t), cmd, display, ref)
	if err != nil || result.Details().Object.ID.String() != target || result.Details().Object.ID == original.Details().Object.ID || result.Details().Object.SHA256 != original.Details().Object.SHA256 {
		t.Fatal("original fact recovery", err)
	}
	var after string
	if err = f.store.QueryRow(ctx, `SELECT source::text FROM agenteam_artifact.commands WHERE command_key=$1`, string(cmd.IdempotencyKey)).Scan(&after); err != nil || captured != after {
		t.Fatal("recovery replaced captured source facts", err)
	}
	// Completed replay no longer depends on current source ownership.
	f.sql(t, `UPDATE object_fixture.projects SET owner_id=$2 WHERE id=$1`, sourceProject.String(), foreignOwner.String())
	restarted.resolver.reject.Store(true)
	beforeGet, beforePut = f.proxy.gets.Load(), f.proxy.puts.Load()
	resolves := restarted.resolver.resolves.Load()
	_, err = restarted.artifact.CreateFromSource(ctx, restarted.invocation(t), cmd, display, ref)
	if err != nil || f.proxy.gets.Load() != beforeGet || f.proxy.puts.Load() != beforePut || restarted.resolver.resolves.Load() != resolves {
		t.Fatal("completed replay revisited revoked source", err)
	}
}

// Start a real exclusive identity/Project gate and change the fixture's current
// authority facts only after the caller proves the exact waiter in pg_locks.
func artifactGate(t *testing.T, f *artifactFixture, key foundation.LockKey, query string, args ...any) func() {
	t.Helper()
	ctx := contextFor(t)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan foundation.CommitResult, 1)
	var once sync.Once
	c := cause(t)
	go func() {
		done <- f.store.WithinTx(ctx, c, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			close(held)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			e, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			_, err = e.Exec(ctx, query, args...)
			return err
		})
	}()
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("gate not acquired")
	}
	return func() {
		once.Do(func() { close(release) })
		select {
		case result := <-done:
			if result.State() != foundation.Committed {
				t.Fatal("gate update", result.Fault())
			}
		case <-ctx.Done():
			t.Fatal("gate update did not finish")
		}
	}
}

func TestArtifactDownloadCurrentAuthorizationWinsGateRace(t *testing.T) {
	for _, stage := range []string{"issue_owner", "open_session", "open_owner"} {
		t.Run(stage, func(t *testing.T) {
			f := newArtifactFixture(t)
			m := f.create(t, "authorization-race", "text/plain", "guarded")
			d := f.downloads(t, f.sources)
			ref, _ := m.Details().Reference.BusinessFile()
			var token string
			if stage != "issue_owner" {
				token = f.downloadToken(t, d, m, oc.DownloadAttachment)
			}
			key, _ := foundation.ProjectLock(f.project.String())
			query := `UPDATE object_fixture.projects SET owner_id=$1 WHERE id=$2`
			args := []any{id[identity.User](t).String(), f.project.String()}
			if stage == "open_session" {
				key, _ = foundation.UserLock(f.actor.Details().UserID)
				query = `UPDATE object_fixture.sessions SET active=false WHERE id=$1`
				args = []any{f.actor.Details().SessionID}
			}
			release := artifactGate(t, f, key, query, args...)
			before := f.proxy.gets.Load()
			done := make(chan error, 1)
			go func() {
				if stage == "issue_owner" {
					_, err := d.IssueDownload(contextFor(t), f.actor, ref, oc.DownloadAttachment, 0)
					done <- err
					return
				}
				stream, err := d.OpenDownload(contextFor(t), f.actor, token, "")
				if stream != nil {
					_ = stream.Close()
				}
				done <- err
			}()
			waitAdvisory(t, f.fixture, key)
			if f.proxy.gets.Load() != before {
				t.Fatal("bytes crossed held authorization gate")
			}
			release()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stale authority accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("authority waiter stuck")
			}
			if f.proxy.gets.Load() != before {
				t.Fatal("revoked request reached storage")
			}
			var attempts int
			if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_download.attempts`).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatal("denied request retained attempt", attempts, err)
			}
		})
	}
}

func TestArtifactAgentExecutionCurrentGateAndProvenance(t *testing.T) {
	f := newArtifactFixture(t)
	agent, execution := executionFacts(t, f.fixture)
	actor, _ := identity.NewAgentRun(f.project, agent, execution)
	operation, tool, call := id[struct{}](t), id[struct{}](t), id[struct{}](t)
	f.sql(t, `INSERT INTO object_fixture.operations(id,execution_id,tool_id,tool_call_id)VALUES($1,$2,$3,$4)`, operation.String(), execution.String(), tool.String(), call.String())
	v, err := art.NewInvocation(art.InvocationDetails{Actor: actor, ProjectID: f.project, ExecutionID: execution.String(), OperationID: operation.String(), ToolID: tool.String(), ToolCallID: call.String(), AttemptID: id[art.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	cmd := command(t, "agent-artifact")
	m, err := f.artifact.CreateFromContent(contextFor(t), v, cmd, art.Display{Name: "agent.txt"}, "text/plain", "real-agent")
	if err != nil || m.Details().CreatedByAgentID != agent.String() || m.Details().ExecutionID != execution.String() || m.Details().OperationID != operation.String() {
		t.Fatal("trusted provenance", err)
	}
	var exact bool
	err = f.store.QueryRow(contextFor(t), `SELECT execution_id=$1 AND operation_id=$2 AND tool_id=$3 AND tool_call_id=$4 FROM agenteam_audit.audit_records WHERE action='artifact.create'`, execution.String(), operation.String(), tool.String(), call.String()).Scan(&exact)
	if err != nil || !exact {
		t.Fatal("actual Audit provenance", exact, err)
	}
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, execution.String())
	release := artifactGate(t, f, key, `UPDATE object_fixture.executions SET active=false WHERE id=$1`, execution.String())
	before := f.proxy.puts.Load()
	done := make(chan error, 1)
	go func() {
		_, err := f.artifact.CreateFromContent(contextFor(t), v, cmd, art.Display{Name: "agent.txt"}, "text/plain", "real-agent")
		done <- err
	}()
	waitAdvisory(t, f.fixture, key)
	release()
	select {
	case err = <-done:
		requireCode(t, err, foundation.Forbidden)
	case <-time.After(3 * time.Second):
		t.Fatal("execution waiter stuck")
	}
	if f.proxy.puts.Load() != before {
		t.Fatal("failed completed authorization wrote storage")
	}
}
