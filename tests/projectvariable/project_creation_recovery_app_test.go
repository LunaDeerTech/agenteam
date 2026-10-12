//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// A test-owned PostgreSQL SHARE relation lock blocks only the first real Skill
// initialization INSERT. The original HTTP request is then cancelled before
// any Skill work or Object operation has been registered. No network fault
// proxy, backend cancellation, CommitResult replacement or ready-row write is
// involved. This is known failed-attempt recovery, not COMMIT Unknown recovery.
func TestProjectCreationRecoveryApp(t *testing.T) {
	t.Run("cancelled-create-restarts-original-intent", func(t *testing.T) {
		agentProjectAppHandoff(t, func(first *agentProjectApp) {
			project := id[i.Project](t)
			key := f.IdempotencyKey("cancelled-app-project-create")
			input := pc.CreateProjectRequest{ProjectID: project, Name: "Recovered_App_Project", Description: "Resume the original cancelled creation."}
			body := jsonBytes(t, input)
			failed := projectCreationCancelAtInitialization(t, first, input, key, body)

			// Neither client cancellation nor the terminal database checkpoint
			// proves the original App has returned. Observe its actual Run and
			// guard release before constructing the replacement public App.
			first.requestStop()
			select {
			case <-first.done:
			case <-first.ctx.Done():
				t.Fatal("cancelled creation App did not actually return")
			}
			agentAppRequire(t, first.runErr)
			if !first.logs.drained() {
				t.Fatal("cancelled creation App did not complete graceful shutdown")
			}
			var stopped bool
			agentAppRequire(t, first.setup.p2.base.raw.QueryRow(first.ctx, `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, first.process).Scan(&stopped))
			if !stopped {
				t.Fatal("cancelled creation App did not retire its original process")
			}
			agentAppRequire(t, syscall.Flock(int(first.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
			agentAppRequire(t, syscall.Flock(int(first.claim.Fd()), syscall.LOCK_UN))
			projectCreationAppReturned(t, first.setup, first.ctx, project, first.process, 0, 0)

			withAgentProjectApp(t, first.setup, first.process, func(second *agentProjectApp) {
				if second.process == first.process {
					t.Fatal("restarted App reused the original process identity")
				}
				owner := second.setup.p2.base.ownerBrowser
				statusPath := "/api/v1/project-creations/" + failed.creation
				status := second.http.request(t, owner, http.MethodGet, statusPath, nil, "", false)
				var pending projectCreationAppResult
				agentAppRequire(t, agentWebDecode(status.body, &pending))
				if status.status != http.StatusOK || pending.State != pc.CreationPending || pending.Project != nil || pending.Operation == nil || pending.Operation.ID.String() != failed.creation || pending.Operation.ProjectID != project || pending.Operation.State != pc.CreationFailed || pending.Operation.SafeReason != pc.ReasonDependencyUnavailable || int64(pending.Operation.Version) != failed.version || pending.StatusURL != statusPath {
					t.Fatal("restarted App did not expose the original failed creation to its Owner")
				}
				if observed := projectCreationRecoveryRead(t, second, input, key); observed != failed {
					t.Fatal("restart or status read changed the original failed intent")
				}

				// The explicit retry uses the original P/key/body, after the old
				// owner actually returned. No new creation or initialization key.
				response := second.http.request(t, owner, http.MethodPost, "/api/v1/projects", body, key, false)
				ready := projectCreationAppReady(t, response, http.StatusCreated, input, owner.actor)
				publication := projectCreationAppPublished(t, second.setup, second.ctx, project, key, second.process)
				finished := projectCreationRecoveryRead(t, second, input, key)
				if finished.creation != failed.creation || finished.initKey != failed.initKey || finished.identity != failed.identity || finished.state != "completed" || finished.reason != "" || !finished.initialized || finished.phase != "terminal" || finished.process != second.process || finished.attempt == failed.attempt || finished.fence != failed.fence+1 || finished.version <= failed.version || publication.creation != failed.creation {
					t.Fatal("retry replaced original intent or omitted its new terminal attempt")
				}
				var unique bool
				agentAppRequire(t, second.setup.p2.base.raw.QueryRow(second.ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.creations WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_project.work_claims WHERE project_id=$1 AND work_kind='creation')=1
 AND (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1 AND creation_id::text=$2 AND initialization_key=$3 AND phase='published')=1
 AND (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1)=1
 AND (SELECT count(*) FROM agenteam_object.uploads WHERE project_id=$1)=1`, project.String(), failed.creation, failed.initKey).Scan(&unique))
				if !unique {
					t.Fatal("recovered original intent duplicated Skill or Object publication")
				}
				projectCreationAppPackage(t, second, ready, publication)
				projectCreationAppReady(t, second.http.request(t, owner, http.MethodGet, statusPath, nil, "", false), http.StatusOK, input, owner.actor)
				second.requestStop()
				select {
				case <-second.done:
				case <-second.ctx.Done():
					t.Fatal("recovered creation App did not actually return")
				}
				agentAppRequire(t, second.runErr)
				projectCreationAppReturned(t, second.setup, second.ctx, project, second.process, 0, 0)
			})
			t.Log("PROJECT_CREATION_RECOVERY exact_skill_insert_wait=true client_cancel_returned=true original_sql_gone=true failed_terminal_checkpoint=true old_app_and_guard_returned=true same_creation_and_init_key=true unique_real_publication=true")
		})
	})
}

type projectCreationRecoveryFacts struct {
	creation, initKey, identity, projectSnapshot string
	state, reason, process, attempt, phase       string
	version, fence                               int64
	initialized, retainedInput, noPublication    bool
}

func projectCreationRecoveryRead(t *testing.T, v *agentProjectApp, input pc.CreateProjectRequest, key f.IdempotencyKey) projectCreationRecoveryFacts {
	t.Helper()
	var out projectCreationRecoveryFacts
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT c.id::text,c.initialization_key,
 jsonb_build_object('id',c.id,'project',c.project_id,'owner',c.owner_user_id,'key',c.command_key,
 'digest',c.semantic_digest,'init',c.initialization_key,'created',c.created_at,'event',c.event_id)::text,
 to_jsonb(p)::text,c.state,COALESCE(c.safe_reason,''),w.process_id::text,w.attempt_id::text,w.phase,c.version,w.fence,
 p.initialized_at IS NOT NULL,
 COALESCE(c.request_name=$2 AND c.request_description=$3 AND c.command_key=$4
 AND c.owner_user_id::text=$5 AND p.owner_user_id=c.owner_user_id
 AND p.name=$2 AND p.description=$3 AND p.version=1 AND p.lifecycle='active'
 AND c.safe_result IS NULL AND c.protected_skill_id IS NULL AND c.protected_revision IS NULL,false),
 NOT EXISTS(SELECT 1 FROM agenteam_skill.initializations WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.skills WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.revisions WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.object_attempts WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_object.objects WHERE project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_object.uploads WHERE project_id=p.id)
 FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id
 JOIN agenteam_project.work_claims w ON w.work_kind='creation' AND w.work_id=c.id AND w.project_id=p.id
 WHERE p.id=$1`, input.ProjectID.String(), input.Name, input.Description, string(key), v.setup.p2.base.ownerBrowser.actor.Details().UserID).Scan(
		&out.creation, &out.initKey, &out.identity, &out.projectSnapshot, &out.state, &out.reason, &out.process, &out.attempt, &out.phase, &out.version, &out.fence, &out.initialized, &out.retainedInput, &out.noPublication))
	return out
}

type projectCreationInsertWaiter struct {
	PID     int32     `json:"pid"`
	Backend time.Time `json:"backend"`
	Started time.Time `json:"started"`
}

func projectCreationWaitForInsert(t *testing.T, v *agentProjectApp, project i.ProjectID, holder int32, returned <-chan struct{}) projectCreationInsertWaiter {
	t.Helper()
	key, err := f.ProjectLock(project.String())
	agentAppRequire(t, err)
	bits := uint64(key.AdvisoryKey())
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var raw []byte
		agentAppRequire(t, v.setup.p2.base.raw.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('pid',a.pid,'backend',a.backend_start,'started',a.xact_start)),'[]'::jsonb)
 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.relation=w.relation
 JOIN pg_stat_activity a ON a.pid=w.pid
 WHERE a.datname=current_database() AND a.application_name='agenteam' AND a.state='active'
 AND a.query LIKE 'INSERT INTO agenteam_skill.initializations(%'
 AND w.locktype='relation' AND w.relation='agenteam_skill.initializations'::regclass
 AND w.mode='RowExclusiveLock' AND NOT w.granted
 AND h.pid=$1 AND h.mode='ShareLock' AND h.granted AND $1::int=ANY(pg_blocking_pids(w.pid))
 AND EXISTS(SELECT 1 FROM pg_locks p WHERE p.pid=w.pid AND p.database=w.database
 AND p.locktype='advisory' AND p.classid::bigint=$2 AND p.objid::bigint=$3
 AND p.objsubid=1 AND p.mode='ExclusiveLock' AND p.granted)`, holder, int64(bits>>32), int64(bits&0xffffffff)).Scan(&raw))
		var rows []projectCreationInsertWaiter
		agentAppRequire(t, json.Unmarshal(raw, &rows))
		if len(rows) > 1 {
			t.Fatal("original Skill initialization INSERT waiter is not unique")
		}
		if len(rows) == 1 && rows[0].PID > 0 && !rows[0].Backend.IsZero() && !rows[0].Started.IsZero() {
			return rows[0]
		}
		select {
		case <-tick.C:
		case <-returned:
			t.Fatal("original Create returned before the exact Skill INSERT lock gate")
		case <-ctx.Done():
			t.Fatal("exact Skill INSERT gate did not arrive before the original lock timeout")
		}
	}
}

func projectCreationCancelAtInitialization(t *testing.T, v *agentProjectApp, input pc.CreateProjectRequest, key f.IdempotencyKey, body []byte) projectCreationRecoveryFacts {
	t.Helper()
	conn, err := v.setup.p2.base.db.Fixture.Connect(v.ctx, v.setup.p2.base.db.Name)
	agentAppRequire(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		agentAppRequire(t, conn.Close(ctx))
	}()
	tx, err := conn.Begin(v.ctx)
	agentAppRequire(t, err)
	var releaseOnce sync.Once
	var releaseErr error
	release := func() {
		releaseOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			releaseErr = tx.Rollback(ctx)
		})
	}
	defer func() { release(); agentAppRequire(t, releaseErr) }()
	_, err = tx.Exec(v.ctx, `LOCK TABLE agenteam_skill.initializations IN SHARE MODE`)
	agentAppRequire(t, err)
	holder := int32(conn.PgConn().PID())

	requestCtx, cancelRequest := context.WithCancel(v.ctx)
	returned := make(chan struct{})
	var requestErr error
	var verified atomic.Bool
	go func() {
		defer close(returned)
		requestErr = projectCreationCancelledPost(requestCtx, v.http, v.setup.p2.base.ownerBrowser, body, key, &verified)
	}()
	defer func() {
		cancelRequest()
		release()
		select {
		case <-returned:
		case <-v.ctx.Done():
			t.Error("cancelled Create client has not returned; retaining its App and fixture")
			<-returned // The original owner, not elapsed time, permits resource cleanup.
		}
	}()

	waiter := projectCreationWaitForInsert(t, v, input.ProjectID, holder, returned)
	before := projectCreationRecoveryRead(t, v, input, key)
	if before.state != "initializing" || before.reason != "" || before.initialized || !before.retainedInput || !before.noPublication || before.phase != "running" || before.process != v.process || before.fence != 1 || before.version != 2 || before.initKey != "project-init-"+before.creation {
		t.Fatal("blocked real INSERT lacks the original accepted creation and running claim")
	}
	select {
	case <-returned:
		t.Fatal("original Create returned before intentional client cancellation")
	default:
	}
	v.requireGuardHeld(t)
	cancelRequest()
	select {
	case <-returned:
	case <-v.ctx.Done():
		t.Fatal("original cancelled HTTP client did not actually return")
	}
	if !errors.Is(requestErr, context.Canceled) || !verified.Load() {
		t.Fatal("original verified TLS client did not return its own cancellation")
	}
	select {
	case result := <-v.http.done:
		if result.err == nil || result.cut {
			t.Fatal("original cancelled TLS bridge did not return its real failed upstream call")
		}
	case <-v.ctx.Done():
		t.Fatal("original cancelled TLS bridge did not return")
	}

	// The bridge can return before the backend finishes. Independently wait
	// for the original transaction to disappear and the real WithoutCancel
	// checkpoint to commit failed/terminal. No row below is written by tests.
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var settled bool
		agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND backend_start=$2 AND xact_start=$3)
 AND NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='relation' AND relation='agenteam_skill.initializations'::regclass AND NOT granted)
 AND EXISTS(SELECT 1 FROM agenteam_project.creations c JOIN agenteam_project.work_claims w ON w.work_id=c.id AND w.work_kind='creation'
 WHERE c.id::text=$4 AND c.state='failed' AND c.safe_reason='dependency_unavailable'
 AND w.process_id::text=$5 AND w.attempt_id::text=$6 AND w.fence=$7 AND w.phase='terminal')`, waiter.PID, waiter.Backend, waiter.Started, before.creation, before.process, before.attempt, before.fence).Scan(&settled))
		if settled {
			break
		}
		select {
		case <-tick.C:
		case <-v.ctx.Done():
			t.Fatal("original cancelled SQL and failed creation checkpoint did not settle")
		}
	}
	failed := projectCreationRecoveryRead(t, v, input, key)
	if failed.creation != before.creation || failed.initKey != before.initKey || failed.identity != before.identity || failed.projectSnapshot != before.projectSnapshot || failed.version != before.version+1 || failed.initialized || !failed.retainedInput || !failed.noPublication || failed.process != before.process || failed.attempt != before.attempt || failed.fence != before.fence || failed.phase != "terminal" || failed.state != "failed" || failed.reason != "dependency_unavailable" {
		t.Fatal("cancelled initialization changed intent or fabricated publication instead of a terminal failed checkpoint")
	}
	v.requireGuardHeld(t)
	release()
	agentAppRequire(t, releaseErr)
	var holding bool
	agentAppRequire(t, conn.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='relation' AND relation='agenteam_skill.initializations'::regclass AND mode='ShareLock' AND granted)`).Scan(&holding))
	if holding {
		t.Fatal("test-owned relation lock survived its original Rollback return")
	}
	return failed
}

// This client uses the original httptest CA and production Session/CSRF
// headers. Its only interruption is its own context cancellation after SQL
// readiness. It does not retry a request or cancel a PostgreSQL backend.
func projectCreationCancelledPost(ctx context.Context, h *agentAppHTTP, owner variableHTTPBrowser, body []byte, key f.IdempotencyKey, verified *atomic.Bool) error {
	trace := &httptrace.ClientTrace{TLSHandshakeDone: func(state tls.ConnectionState, err error) {
		verified.Store(err == nil && len(state.VerifiedChains) > 0)
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, h.server.URL+"/api/v1/projects", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: owner.cookie, Secure: true, HttpOnly: true, Path: "/"})
	request.Header.Set("Origin", h.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", owner.csrf)
	request.Header.Set("Idempotency-Key", string(key))
	transport := h.server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	return errors.New("Create returned an HTTP response before its intentional cancellation")
}
