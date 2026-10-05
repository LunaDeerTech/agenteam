//go:build integration

package project_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type projectClaimChildInput struct {
	Database, Bucket, Spool, Process, Project, User, Session string
}

func TestProjectB02ClaimChild(t *testing.T) {
	path := os.Getenv("AGENTEAM_PROJECT_CLAIM_CHILD")
	if path == "" {
		t.Skip("owned subprocess only")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned child input")
	}
	data, err := os.ReadFile(path)
	var input projectClaimChildInput
	if err != nil || json.Unmarshal(data, &input) != nil {
		t.Fatal("invalid child input")
	}
	pg, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	db := &pgfixture.Database{Fixture: pg, Name: input.Database}
	raw := openStore(t, db.Config(t, nil))
	f := assembleFixture(t, db, raw, raw, false)
	storage, _ := projectStorage(t, input.Bucket)
	process, err := foundation.ParseID[objectc.Process](input.Process)
	if err != nil {
		t.Fatal(err)
	}
	runtime, objects, guard := f.processRuntime(t, storage, input.Spool, process)
	if err = runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	user, err := foundation.ParseID[identity.User](input.User)
	if err != nil {
		t.Fatal(err)
	}
	session, err := foundation.ParseID[identity.Session](input.Session)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	target, err := foundation.ParseID[identity.Project](input.Project)
	if err != nil {
		t.Fatal(err)
	}
	f.process = projectGuardProcesses{guard: guard, process: process}
	blocked := &blockedInitializer{ProjectSkillInitializer: f.skills, entered: make(chan struct{}), release: make(chan struct{})}
	service := f.newService(t, blocked, f.aud, f.events)
	done := make(chan error, 1)
	go func() {
		_, err := service.CreateProject(context.Background(), actor, meta(t, "killed-create", nil), c.CreateProjectRequest{ProjectID: target, Name: "KilledWorker"})
		done <- err
	}()
	select {
	case <-blocked.entered:
	case err := <-done:
		t.Fatal("creation returned before live external-work barrier", err)
	case <-time.After(10 * time.Second):
		t.Fatal("creation did not durably claim before external work")
	}
	// Release only the spool-directory lock. The independently held real
	// ProcessGuard remains live while the Project initializer has not joined.
	objects.StopAdmission()
	if err = objects.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	fmt.Println("PROJECT_ACTIVE_GUARD_HELD")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
	close(blocked.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("unexpected parent shutdown failed to join")
	}
}

func TestProjectB02CreationClaimRequiresActualDeathAndFairRecovery(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	storage, bucket := projectStorage(t, "")
	old := id[objectc.Process](t)
	target := id[identity.Project](t)
	spool := filepath.Join(t.TempDir(), "spool")
	input := projectClaimChildInput{Database: f.db.Name, Bucket: bucket, Spool: spool, Process: old.String(), Project: target.String(), User: owner.Details().UserID, Session: owner.Details().SessionID}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "child.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProjectB02ClaimChild$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "AGENTEAM_PROJECT_CLAIM_CHILD="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	lines := make(chan string, 32)
	readDone := make(chan struct{})
	go func() {
		defer close(lines)
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("owned Project worker did not actually join")
		}
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Error("owned child output reader did not join")
		}
	})
	select {
	case line := <-lines:
		if line != "PROJECT_ACTIVE_GUARD_HELD" {
			t.Fatal("child did not reach the exact durable-work barrier", line)
		}
	case <-done:
		t.Fatal("owned child exited before barrier", waitErr, stderr.String())
	case <-ctx.Done():
		t.Fatal("owned child barrier expired")
	}
	var original, originalAttempt, process, phase string
	var fence int64
	claim := func() {
		t.Helper()
		if err = f.raw.QueryRow(ctxFor(t), `SELECT c.id::text,w.attempt_id::text,w.process_id::text,w.fence,w.phase FROM agenteam_project.creations c JOIN agenteam_project.work_claims w ON w.work_kind='creation' AND w.work_id=c.id WHERE c.project_id=$1`, target.String()).Scan(&original, &originalAttempt, &process, &fence, &phase); err != nil {
			t.Fatal(err)
		}
	}
	claim()
	if process != old.String() || fence != 1 || phase != "running" {
		t.Fatal("child lacks exact live persistent claim")
	}
	creationID, err := foundation.ParseID[c.Creation](original)
	if err != nil {
		t.Fatal(err)
	}
	oldAttempt := originalAttempt
	newProcess := id[objectc.Process](t)
	runtime, _, guard := f.processRuntime(t, storage, spool, newProcess)
	if err = runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	requireCode(t, guard.ConfirmStopped(ctxFor(t), old), foundation.ResourceBusy)
	f.process = projectGuardProcesses{guard: guard, process: newProcess}
	f.service = f.newService(t, f.skills, f.aud, f.events)
	f.skills.setMode("pending")
	later, err := f.service.CreateProject(ctxFor(t), owner, meta(t, "later-create", nil), c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "LaterWork"})
	if err != nil || later.Operation == nil || later.Operation.ID.String() <= original {
		t.Fatal("later pending work does not follow the live prefix", err)
	}
	f.skills.setMode("")
	first, err := f.service.RecoverCreations(ctxFor(t), nil, 1)
	if err != nil || first.Visited != 1 || first.Pending != 1 || first.Completed != 0 || first.Next == nil || *first.Next != creationID {
		t.Fatal("live prefix was taken over or lost continuation", first, err)
	}
	claim()
	if process != old.String() || originalAttempt != oldAttempt || fence != 1 || phase != "running" {
		t.Fatal("live worker claim mutated before actual death")
	}
	second, err := f.service.RecoverCreations(ctxFor(t), first.Next, 1)
	if err != nil || second.Completed != 1 || second.Pending != 0 {
		t.Fatal("busy prefix starved later work", second, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("actual kill did not join")
	}
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) {
		t.Fatal("child did not actually die", waitErr)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("child exit was not SIGKILL")
	}
	if err = guard.ConfirmStopped(ctxFor(t), old); err != nil {
		t.Fatal("exact joined OS-death proof unavailable", err)
	}
	recovered, err := f.service.RecoverCreations(ctxFor(t), nil, 1)
	if err != nil || recovered.Completed != 1 || recovered.Pending != 0 {
		t.Fatal("dead claim did not recover", recovered, err)
	}
	claim()
	if original != creationID.String() || originalAttempt == oldAttempt || process != newProcess.String() || fence != 2 || phase != "terminal" {
		t.Fatal("recovery replaced identity or failed to fence exact old attempt")
	}
	var skills, accepted, completed, events int
	if err = f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM project_fixture.skills WHERE project_id=$1 AND protected AND published),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.accepted'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.completed'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.created')`, target.String()).Scan(&skills, &accepted, &completed, &events); err != nil || skills != 1 || accepted != 1 || completed != 1 || events != 1 {
		t.Fatal("death recovery duplicated or lost committed facts", skills, accepted, completed, events, err)
	}
}

func TestProjectB02OwnerSessionNamePathAndPaging(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	admin := f.human(t, "admin", "admin")
	p, _, _ := f.create(t, owner, "Demo")
	if _, e := f.service.GetProject(ctxFor(t), admin, p.ID); e == nil {
		t.Fatal("admin bypassed Owner")
	} else {
		requireCode(t, e, foundation.NotFound)
	}
	ref, e := f.service.ResolveProjectPath(ctxFor(t), owner, "OWNER-ALPHA", "DEMO")
	if e != nil || ref.ID != p.ID {
		t.Fatal("current path", e)
	}
	if _, e = f.service.ResolveProjectPath(ctxFor(t), owner, "admin", "Demo"); e == nil {
		t.Fatal("other route resolved")
	}
	request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "demo"}
	_, e = f.service.CreateProject(ctxFor(t), owner, meta(t, "name-collision", nil), request)
	requireCode(t, e, foundation.ResourceBusy)
	version := p.Version
	name := "dEMo"
	updated, e := f.service.UpdateProject(ctxFor(t), owner, meta(t, "case-rename", &version), p.ID, c.UpdateProjectRequest{Name: &name})
	if e != nil || updated.Version != 2 || updated.NormalizedName != "demo" {
		t.Fatal("case-only rename", e)
	}
	f.create(t, owner, "Second")
	first, e := f.service.ListOwnedProjects(ctxFor(t), owner, c.ListOwnedProjectsRequest{}, foundation.PageRequest{Limit: 1})
	if e != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal("first page", e)
	}
	second, e := f.service.ListOwnedProjects(ctxFor(t), owner, c.ListOwnedProjectsRequest{}, foundation.PageRequest{Limit: 2, Cursor: first.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("cursor continuation", e)
	}
	if _, e = f.service.ListOwnedProjects(ctxFor(t), admin, c.ListOwnedProjectsRequest{}, foundation.PageRequest{Limit: 2, Cursor: first.NextCursor}); e == nil {
		t.Fatal("cursor crossed Owner")
	}
	if _, e = f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.users SET username='owner-new',version=version+1 WHERE id=$1`, owner.Details().UserID); e != nil {
		t.Fatal(e)
	}
	_, e = f.service.ResolveProjectPath(ctxFor(t), owner, "owner-alpha", "demo")
	requireCode(t, e, foundation.NotFound)
	if _, e = f.service.ResolveProjectPath(ctxFor(t), owner, "owner-new", "demo"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID); e != nil {
		t.Fatal(e)
	}
	_, e = f.service.GetProject(ctxFor(t), owner, p.ID)
	requireCode(t, e, foundation.SessionRevoked)
	_, e = f.service.LookupCommand(ctxFor(t), owner, c.CommandLookupRequest{ProjectID: p.ID, Command: c.UpdateCommand, Key: "case-rename"})
	requireCode(t, e, foundation.SessionRevoked)
}
func TestProjectB02TargetIdentityAndDeletionTombstone(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	other := f.human(t, "owner-beta", "admin")
	p, m, r := f.create(t, owner, "One")
	got, e := f.service.CreateProject(ctxFor(t), owner, m, r)
	if e != nil || got.Project.ID != p.ID {
		t.Fatal("same target replay", e)
	}
	changed := r
	changed.Description = "different"
	_, e = f.service.CreateProject(ctxFor(t), owner, m, changed)
	requireCode(t, e, foundation.IdempotencyKeyReused)
	_, e = f.service.CreateProject(ctxFor(t), owner, meta(t, "another-key", nil), r)
	requireCode(t, e, foundation.ResourceBusy)
	_, e = f.service.CreateProject(ctxFor(t), other, m, r)
	requireCode(t, e, foundation.NotFound)
	newIntent := r
	newIntent.ProjectID = id[identity.Project](t)
	newIntent.Name = "Two"
	got, e = f.service.CreateProject(ctxFor(t), owner, m, newIntent)
	if e != nil || got.Project.ID != newIntent.ProjectID {
		t.Fatal("raw key different target", e)
	}
	dead := id[identity.Project](t)
	operation := id[c.Operation](t)
	hash, _ := c.DeletionCommandKeyHash(dead, p.OwnerUserID, "delete-key")
	if _, e = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_project.deletion_receipts(operation_id,deleted_project_id,original_owner_user_id,command_key_hash,request_digest,completed_at) VALUES($1,$2,$3,$4,$4,clock_timestamp())`, operation.String(), dead.String(), p.OwnerUserID.String(), hash.String()); e != nil {
		t.Fatal(e)
	}
	stale := c.CreateProjectRequest{ProjectID: dead, Name: "OldName"}
	_, e = f.service.CreateProject(ctxFor(t), owner, meta(t, "old-key", nil), stale)
	requireCode(t, e, foundation.ResourceDeleted)
	_, e = f.service.CreateProject(ctxFor(t), other, meta(t, "any-key", nil), stale)
	requireCode(t, e, foundation.NotFound)
	var count int
	if e = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.projects WHERE id=$1`, dead.String()).Scan(&count); e != nil || count != 0 {
		t.Fatal("deleted target revived", e)
	}
}
func TestProjectB02InitializerFailureUnknownUnboundAndReceiptOrder(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	unbound := f.newService(t, nil, f.aud, f.events)
	r := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "NoBinding"}
	_, e := unbound.CreateProject(ctxFor(t), owner, meta(t, "unbound", nil), r)
	requireCode(t, e, foundation.DependencyUnbound)
	var count int
	if e = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.projects`).Scan(&count); e != nil || count != 0 {
		t.Fatal("unbound left reservation", e)
	}
	for _, mode := range []string{"failed", "unknown", "pending"} {
		t.Run(mode, func(t *testing.T) {
			f.skills.setMode(mode)
			r := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: mode}
			m := meta(t, mode, nil)
			result, e := f.service.CreateProject(ctxFor(t), owner, m, r)
			if mode == "failed" {
				var failure *foundation.Fault
				if !errors.As(e, &failure) || failure.Code != foundation.DependencyUnavailable || failure.CommitState != foundation.Committed {
					t.Fatal("confirmed failure lost accepted commit or safe fault", e)
				}
				lookup, err := f.service.LookupCommand(ctxFor(t), owner, c.CommandLookupRequest{ProjectID: r.ProjectID, Command: c.CreateCommand, Key: m.IdempotencyKey})
				if err != nil || lookup.State != c.LookupCommitted || lookup.Result == nil || lookup.Result.Creation == nil {
					t.Fatal("failed creation lost its original acceptance", err)
				}
				result = *lookup.Result.Creation
				if result.Operation == nil || result.Operation.State != c.CreationFailed || result.Operation.SafeReason != c.ReasonOperationFailed || failure.CauseID != result.Operation.ID.String() {
					t.Fatal("failed status mismatch", result.Operation)
				}
				e = nil
			}
			if e != nil || result.State != c.CreationPending || result.Operation == nil {
				t.Fatal("accepted operation", e)
			}
			if mode != "failed" {
				wantReason := c.ReasonWorkPending
				if mode == "unknown" {
					wantReason = c.ReasonOutcomeUnknown
				}
				if result.Operation.State != c.CreationInitializing || result.Operation.SafeReason != wantReason {
					t.Fatal("pending/unknown became a confirmed failure", result.Operation)
				}
			}
			creation := result.Operation.ID
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "initialization_key") || strings.Contains(string(raw), "request_description") {
				t.Fatal("private operation input leaked")
			}
			_, e = f.service.GetProject(ctxFor(t), owner, r.ProjectID)
			requireCode(t, e, foundation.ProjectNotActive)
			_, e = unbound.CreateProject(ctxFor(t), owner, m, r)
			requireCode(t, e, foundation.DependencyUnbound)
			f.skills.setMode("")
			result, e = f.service.CreateProject(ctxFor(t), owner, m, r)
			if e != nil || result.State != c.CreationReady || result.Project.Version != 1 {
				t.Fatal("same intent recovery", e)
			}
			var stored string
			if e = f.raw.QueryRow(ctxFor(t), `SELECT creation_id::text FROM agenteam_project.projects WHERE id=$1`, r.ProjectID.String()).Scan(&stored); e != nil || stored != creation.String() {
				t.Fatal("creation identity changed", e)
			}
			replay, e := unbound.CreateProject(ctxFor(t), owner, m, r)
			if e != nil || replay.State != c.CreationReady {
				t.Fatal("completed replay required current initializer", e)
			}
		})
	}
}

type failAudit struct {
	ac.Appender
	action ac.Action
}

func (f failAudit) AppendInTx(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	receipt, err := f.Appender.AppendInTx(ctx, tx, e, k)
	if err != nil {
		return receipt, err
	}
	if e.Fields().Action == f.action {
		return ac.AppendReceipt{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	return receipt, nil
}

type failEvent struct{ oc.Appender }

func (f failEvent) AppendEventInTx(ctx context.Context, tx foundation.Tx, a identity.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	_, err := f.Appender.AppendEventInTx(ctx, tx, a, e, p)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	return oc.AppendReceipt{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
}
func TestProjectB02AuditEventReceiptTouchAtomicityAndNoOp(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "Atomic")
	version := p.Version
	description := "changed"
	m := meta(t, "update-atomic", &version)
	for _, where := range []string{"audit", "event"} {
		t.Run(where, func(t *testing.T) {
			var aud ac.Appender = f.aud
			var events oc.Appender = f.events
			if where == "audit" {
				aud = failAudit{f.aud, ac.ProjectUpdate}
			} else {
				events = failEvent{f.events}
			}
			service := f.newService(t, f.skills, aud, events)
			if _, e := f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET last_activity_at=clock_timestamp()-interval '90 seconds' WHERE id=$1`, owner.Details().SessionID); e != nil {
				t.Fatal(e)
			}
			var before time.Time
			if e := f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, owner.Details().SessionID).Scan(&before); e != nil {
				t.Fatal(e)
			}
			if _, e := service.UpdateProject(ctxFor(t), owner, m, p.ID, c.UpdateProjectRequest{Description: &description}); e == nil {
				t.Fatal("injected failure succeeded")
			}
			var after time.Time
			var actualVersion int64
			var audits, eventsCount, receipts int
			if e := f.raw.QueryRow(ctxFor(t), `SELECT p.version,(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated'),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND state='completed') FROM agenteam_project.projects p WHERE p.id=$1`, p.ID.String(), owner.Details().SessionID).Scan(&actualVersion, &after, &audits, &eventsCount, &receipts); e != nil {
				t.Fatal(e)
			}
			if actualVersion != 1 || audits != 0 || eventsCount != 0 || receipts != 0 || !after.Equal(before) {
				t.Fatal("partial update/Audit/Event/receipt/touch committed")
			}
		})
	}
	updated, e := f.service.UpdateProject(ctxFor(t), owner, m, p.ID, c.UpdateProjectRequest{Description: &description})
	if e != nil || updated.Version != 2 {
		t.Fatal("planned retry", e)
	}
	replay, e := f.service.UpdateProject(ctxFor(t), owner, m, p.ID, c.UpdateProjectRequest{Description: &description})
	if e != nil || replay.Version != 2 {
		t.Fatal("replay rechecked old version", e)
	}
	version = updated.Version
	noop := meta(t, "no-op", &version)
	if _, e = f.service.UpdateProject(ctxFor(t), owner, noop, p.ID, c.UpdateProjectRequest{Description: &description}); e != nil {
		t.Fatal(e)
	}
	var audits, events int
	if e = f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated')`, p.ID.String()).Scan(&audits, &events); e != nil || audits != 1 || events != 1 {
		t.Fatal("no-op/replay added facts", audits, events, e)
	}
}

func TestProjectB02OwnerPortRequiresCallerTransactionLocks(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "Locks")
	_, e := f.authority.RequireOwnerInTx(ctxFor(t), foundation.Tx{}, owner, p.ID, identity.Read)
	requireCode(t, e, foundation.InvalidArgument)
	projectKey, _ := foundation.ProjectLock(p.ID.String())
	userKey, _ := foundation.UserLock(owner.Details().UserID)
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Shared}}); e != nil {
			return e
		}
		_, e := f.authority.RequireOwnerInTx(ctx, tx, owner, p.ID, identity.Read)
		if e == nil {
			t.Error("missing User lock authorized Owner")
		}
		// The actual Store must stay poisoned even if a caller ignores the
		// narrow-port error; no fake transaction result is injected here.
		return nil
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("ignored missing-lock error committed", result.State())
	}
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: userKey, Mode: foundation.Shared}, {Key: projectKey, Mode: foundation.Shared}}); e != nil {
			return e
		}
		access, e := f.authority.RequireOwnerInTx(ctx, tx, owner, p.ID, identity.Read)
		if e != nil {
			return e
		}
		if !access.Matches(owner, p.ID) {
			t.Error("Owner access mismatched actual current identity")
		}
		return nil
	})
	if result.State() != foundation.Committed {
		t.Fatal("complete read locks denied", result.Fault())
	}
}

type blockedInitializer struct {
	c.ProjectSkillInitializer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockedInitializer) InitializeProjectSkills(ctx context.Context, actor identity.Actor, request c.InitializationRequest) (c.InitializationResult, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-ctx.Done():
		return c.InitializationResult{}, ctx.Err()
	case <-b.release:
		return b.ProjectSkillInitializer.InitializeProjectSkills(ctx, actor, request)
	}
}

type createReply struct {
	result c.CreationResult
	err    error
}

func TestProjectB02ConcurrentCreateKeepsOneReservationAndOriginalIdentity(t *testing.T) {
	for _, sameTarget := range []bool{false, true} {
		name := "same-owner-name"
		if sameTarget {
			name = "same-target-key"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			owner := f.human(t, "owner-alpha", "user")
			block := &blockedInitializer{ProjectSkillInitializer: f.skills, entered: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(block.release) }) }
			defer release()
			service := f.newService(t, block, f.aud, f.events)
			requests := []c.CreateProjectRequest{{ProjectID: id[identity.Project](t), Name: "Concurrent"}, {ProjectID: id[identity.Project](t), Name: "CONCURRENT"}}
			metas := []foundation.CommandMeta{meta(t, "first", nil), meta(t, "second", nil)}
			if sameTarget {
				requests[1] = requests[0]
				metas[1] = metas[0]
			}
			start := make(chan struct{})
			replies := make(chan createReply, 2)
			ctx := ctxFor(t)
			for i := range 2 {
				go func(i int) {
					<-start
					r, e := service.CreateProject(ctx, owner, metas[i], requests[i])
					replies <- createReply{r, e}
				}(i)
			}
			close(start)
			await(t, block.entered)
			var first createReply
			select {
			case first = <-replies:
			case <-ctx.Done():
				t.Fatal("competing create did not reach serialized result")
			}
			if sameTarget {
				if first.err != nil || first.result.State != c.CreationPending || first.result.Operation == nil {
					t.Fatal("same target failed to preserve accepted creation", first.err)
				}
			} else {
				requireCode(t, first.err, foundation.ResourceBusy)
				var failure *foundation.Fault
				if !errors.As(first.err, &failure) || len(failure.FieldErrors) != 1 || failure.FieldErrors[0] != (foundation.FieldError{Path: "/name", Code: "NAME_TAKEN"}) {
					t.Fatal("name collision field", first.err)
				}
			}
			release()
			var second createReply
			select {
			case second = <-replies:
			case <-ctx.Done():
				t.Fatal("accepted creation failed to join")
			}
			if second.err != nil || second.result.State != c.CreationReady {
				t.Fatal("accepted creation did not complete", second.err)
			}
			var creations, projects, skills, accepted, completed, events int
			var creation string
			if err := f.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_project.projects),(SELECT count(*) FROM agenteam_project.creations),(SELECT count(*) FROM project_fixture.skills),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='project.create.accepted'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='project.create.completed'),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='project.created'),(SELECT creation_id::text FROM agenteam_project.projects WHERE id=$1)`, second.result.Project.ID.String()).Scan(&projects, &creations, &skills, &accepted, &completed, &events, &creation); err != nil {
				t.Fatal(err)
			}
			if projects != 1 || creations != 1 || skills != 1 || accepted != 1 || completed != 1 || events != 1 {
				t.Fatal("concurrent creation duplicated durable facts", projects, creations, skills, accepted, completed, events)
			}
			if sameTarget && creation != first.result.Operation.ID.String() {
				t.Fatal("same key replaced original creation")
			}
		})
	}
}

func TestProjectB02ConcurrentUpdateRejectsOldVersionWithoutExtraFacts(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "VersionRace")
	metas := []foundation.CommandMeta{meta(t, "left", &p.Version), meta(t, "right", &p.Version)}
	start := make(chan struct{})
	replies := make(chan error, 2)
	ctx := ctxFor(t)
	for i := range 2 {
		go func(i int) {
			<-start
			description := []string{"left", "right"}[i]
			_, err := f.service.UpdateProject(ctx, owner, metas[i], p.ID, c.UpdateProjectRequest{Description: &description})
			replies <- err
		}(i)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		select {
		case err := <-replies:
			if err == nil {
				success++
			} else {
				requireCode(t, err, foundation.VersionConflict)
				conflict++
			}
		case <-ctx.Done():
			t.Fatal("update race did not join")
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("version race admitted two mutations", success, conflict)
	}
	var version, audits, events, receipts int
	if err := f.raw.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='project.update' AND project_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='project.updated' AND project_id=$1),(SELECT count(*) FROM agenteam_project.commands WHERE command_name='update' AND state='completed' AND project_id=$1) FROM agenteam_project.projects WHERE id=$1`, p.ID.String()).Scan(&version, &audits, &events, &receipts); err != nil || version != 2 || audits != 1 || events != 1 || receipts != 1 {
		t.Fatal("version/Audit/Event/receipt diverged", version, audits, events, receipts, err)
	}
}

type plannedRenameBarrier struct {
	oc.Appender
	mu      sync.Mutex
	want    int
	seen    int
	ready   chan struct{}
	release chan struct{}
}

func (b *plannedRenameBarrier) PrepareAppend(ctx context.Context, actor identity.Actor, value event.Event) (oc.AppendPlan, error) {
	plan, err := b.Appender.PrepareAppend(ctx, actor, value)
	if err != nil || value.Header().EventType != c.UpdatedEventName {
		return plan, err
	}
	b.mu.Lock()
	b.seen++
	if b.seen == b.want {
		close(b.ready)
	}
	b.mu.Unlock()
	select {
	case <-b.release:
		return plan, nil
	case <-ctx.Done():
		return oc.AppendPlan{}, ctx.Err()
	}
}

func requireNameTaken(t *testing.T, err error) {
	t.Helper()
	var failure *foundation.Fault
	if !errors.As(err, &failure) || failure.Code != foundation.ResourceBusy || failure.CommitState != foundation.NotCommitted || len(failure.FieldErrors) != 1 || failure.FieldErrors[0] != (foundation.FieldError{Path: "/name", Code: "NAME_TAKEN"}) {
		t.Fatal("name reservation did not return its exact safe rollback fault", err)
	}
}

func TestProjectB02TwoPlannedTargetsCompeteForOneCanonicalName(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	left, _, _ := f.create(t, owner, "Left")
	right, _, _ := f.create(t, owner, "Right")
	projects := []c.ProjectRef{left, right}
	barrier := &plannedRenameBarrier{Appender: f.events, want: 2, ready: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	defer release()
	service := f.newService(t, f.skills, f.aud, barrier)
	type reply struct {
		index int
		err   error
	}
	results := make(chan reply, 2)
	start := make(chan struct{})
	ctx := ctxFor(t)
	for i := range 2 {
		go func(i int) {
			<-start
			name := []string{"SharedName", "SHAREDNAME"}[i]
			_, err := service.UpdateProject(ctx, owner, meta(t, "planned-rename", &projects[i].Version), projects[i].ID, c.UpdateProjectRequest{Name: &name})
			results <- reply{i, err}
		}(i)
	}
	close(start)
	await(t, barrier.ready)
	var plans int
	if err := f.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_project.commands WHERE command_name='update' AND state='planned' AND project_id IN ($1,$2)`, left.ID.String(), right.ID.String()).Scan(&plans); err != nil || plans != 2 {
		t.Fatal("both canonical plans did not commit before the write race", plans, err)
	}
	release()
	winner, loser := -1, -1
	for range 2 {
		select {
		case result := <-results:
			if result.err == nil {
				if winner != -1 {
					t.Fatal("both Projects acquired the same Owner name")
				}
				winner = result.index
			} else {
				requireNameTaken(t, result.err)
				loser = result.index
			}
		case <-ctx.Done():
			t.Fatal("planned rename race did not actually join")
		}
	}
	if winner == -1 || loser == -1 {
		t.Fatal("name race did not have exactly one winner and loser")
	}
	for i, old := range projects {
		var name, normalized, description string
		var version, audits, events, receipts int
		if err := f.raw.QueryRow(ctx, `SELECT name,normalized_name,description,version,(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated'),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND state='completed') FROM agenteam_project.projects WHERE id=$1`, old.ID.String()).Scan(&name, &normalized, &description, &version, &audits, &events, &receipts); err != nil {
			t.Fatal(err)
		}
		if i == winner {
			if normalized != "sharedname" || version != 2 || audits != 1 || events != 1 || receipts != 1 {
				t.Fatal("winner facts did not commit once", normalized, version, audits, events, receipts)
			}
		} else if name != old.Name || normalized != old.NormalizedName || version != 1 || audits != 0 || events != 0 || receipts != 0 {
			t.Fatal("losing Project, Audit, Event or receipt mutated", name, version, audits, events, receipts)
		}
		if description != old.Description {
			t.Fatal("rename changed unrelated private contents")
		}
	}
}

func TestProjectB02CreateReservesNameAfterAnotherTargetWasPlanned(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	old, _, _ := f.create(t, owner, "Original")
	barrier := &plannedRenameBarrier{Appender: f.events, want: 1, ready: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	defer release()
	service := f.newService(t, f.skills, f.aud, barrier)
	ctx := ctxFor(t)
	done := make(chan error, 1)
	go func() {
		name := "SharedName"
		_, err := service.UpdateProject(ctx, owner, meta(t, "rename-before-create", &old.Version), old.ID, c.UpdateProjectRequest{Name: &name})
		done <- err
	}()
	await(t, barrier.ready)
	var plans int
	if err := f.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND state='planned'`, old.ID.String()).Scan(&plans); err != nil || plans != 1 {
		t.Fatal("rename was not durably planned", plans, err)
	}
	created, _, _ := f.create(t, owner, "SHAREDNAME")
	release()
	select {
	case err := <-done:
		requireNameTaken(t, err)
	case <-ctx.Done():
		t.Fatal("losing planned rename did not join")
	}
	var name, description string
	var version, audits, events, receipts, reserved int
	if err := f.raw.QueryRow(ctx, `SELECT name,description,version,(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated'),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND state='completed'),(SELECT count(*) FROM agenteam_project.projects WHERE owner_user_id=$2 AND normalized_name='sharedname') FROM agenteam_project.projects WHERE id=$1`, old.ID.String(), owner.Details().UserID).Scan(&name, &description, &version, &audits, &events, &receipts, &reserved); err != nil {
		t.Fatal(err)
	}
	if name != old.Name || description != old.Description || version != 1 || audits != 0 || events != 0 || receipts != 0 || reserved != 1 || created.NormalizedName != "sharedname" {
		t.Fatal("Create/rename reservation or losing atomic facts diverged", name, version, audits, events, receipts, reserved)
	}
}

func TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, m, r := f.create(t, owner, "SessionReplay")
	if _, err := f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.CreateProject(ctxFor(t), owner, m, r)
	requireCode(t, err, foundation.SessionRevoked)
	_, err = f.service.LookupCommand(ctxFor(t), owner, c.CommandLookupRequest{ProjectID: p.ID, Command: c.CreateCommand, Key: m.IdempotencyKey})
	requireCode(t, err, foundation.SessionRevoked)
	session := id[identity.Session](t)
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,decode(repeat('ab',32),'hex'),'a',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '2 minutes',3600,clock_timestamp()+interval '1 hour')`, session.String(), owner.Details().UserID); err != nil {
		t.Fatal(err)
	}
	user, _ := foundation.ParseID[identity.User](owner.Details().UserID)
	renewed, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	var before, after time.Time
	if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, session.String()).Scan(&before); err != nil {
		t.Fatal(err)
	}
	replay, err := f.newService(t, nil, f.aud, f.events).CreateProject(ctxFor(t), renewed, m, r)
	if err != nil || replay.State != c.CreationReady || replay.Project.ID != p.ID {
		t.Fatal("new current session lost stable-user receipt", err)
	}
	if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, session.String()).Scan(&after); err != nil || !after.Equal(before) {
		t.Fatal("receipt replay touched activity", err)
	}
}

type invalidInitializer struct {
	*skillFixture
	mode string
}

func (i invalidInitializer) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	result, err := i.skillFixture.InitializeProjectSkills(ctx, actor, r)
	if err != nil {
		return result, err
	}
	if i.mode == "wrong-result-project" {
		result.ProjectID, _ = foundation.NewID[identity.Project]()
	}
	if i.mode == "unprotected" || i.mode == "unpublished" {
		column := "protected"
		if i.mode == "unpublished" {
			column = "published"
		}
		_, err = i.store.Exec(ctx, `UPDATE project_fixture.skills SET `+column+`=false WHERE creation_id=$1`, r.CreationID.String())
	}
	return result, err
}
func (i invalidInitializer) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationConfirmationPlan, error) {
	plan, err := i.skillFixture.DiscoverConfirmation(ctx, actor, r)
	if err != nil {
		return plan, err
	}
	if i.mode == "wrong-issuer" {
		return c.NewInitializationPlanIssuer().Plan(actor, r, plan.ProposedReceipt(), plan.RequiredLocks())
	}
	if i.mode == "missing-plan" {
		return c.InitializationConfirmationPlan{}, nil
	}
	return plan, nil
}
func (i invalidInitializer) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r c.InitializationRequest, plan c.InitializationConfirmationPlan) (c.InitializationReceipt, error) {
	receipt, err := i.skillFixture.ConfirmInitializedInTx(ctx, tx, actor, r, plan)
	if err == nil && i.mode == "changed-receipt" {
		receipt.AddSkillsID, _ = foundation.NewID[c.Skill]()
	}
	return receipt, err
}
func TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts(t *testing.T) {
	for _, mode := range []string{"wrong-result-project", "wrong-issuer", "missing-plan", "unprotected", "unpublished", "changed-receipt"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			owner := f.human(t, "owner-alpha", "user")
			request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "InvalidInitialization"}
			m := meta(t, mode, nil)
			service := f.newService(t, invalidInitializer{f.skills, mode}, f.aud, f.events)
			_, err := service.CreateProject(ctxFor(t), owner, m, request)
			var failure *foundation.Fault
			if !errors.As(err, &failure) || failure.CommitState != foundation.Committed {
				t.Fatal("invalid completion was accepted or lost committed reservation", err)
			}
			var initialized bool
			var state, creation string
			var completed, events int
			if err = f.raw.QueryRow(ctxFor(t), `SELECT p.initialized_at IS NOT NULL,c.state,c.id::text,(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='project.create.completed' AND project_id=p.id),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='project.created' AND project_id=p.id) FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id WHERE p.id=$1`, request.ProjectID.String()).Scan(&initialized, &state, &creation, &completed, &events); err != nil || initialized || state != "failed" || completed != 0 || events != 0 {
				t.Fatal("invalid initialization escaped atomic gate", initialized, state, completed, events, err)
			}
			if _, err = f.raw.Exec(ctxFor(t), `UPDATE project_fixture.skills SET protected=true,published=true WHERE creation_id=$1`, creation); err != nil {
				t.Fatal(err)
			}
			ready, err := f.service.CreateProject(ctxFor(t), owner, m, request)
			if err != nil || ready.State != c.CreationReady || ready.Project.ID != request.ProjectID {
				t.Fatal("same original initialization could not recover", err)
			}
			var count int
			if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM project_fixture.skills WHERE creation_id=$1`, creation).Scan(&count); err != nil || count != 1 {
				t.Fatal("initialization recovery duplicated Skill", count, err)
			}
		})
	}
}
func TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock(t *testing.T) {
	f := newFixture(t)
	owner := f.human(t, "owner-alpha", "user")
	extra, err := foundation.SystemConfigLock("fixture-project-skill")
	if err != nil {
		t.Fatal(err)
	}
	f.skills.extraLock = extra
	p, _, _ := f.create(t, owner, "InitializationLocks")
	var creation, initKey string
	if err = f.raw.QueryRow(ctxFor(t), `SELECT id::text,initialization_key FROM agenteam_project.creations WHERE project_id=$1`, p.ID.String()).Scan(&creation, &initKey); err != nil {
		t.Fatal(err)
	}
	creationID, err := foundation.ParseID[c.Creation](creation)
	if err != nil {
		t.Fatal(err)
	}
	request := c.InitializationRequest{CreationID: creationID, ProjectID: p.ID, InitializationKey: foundation.IdempotencyKey(initKey)}
	registration, _ := identity.RegisterService(identity.ProjectInitialization)
	scope, _ := identity.InProject(p.ID)
	actor, err := registration.Actor(creation, scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.skills.DiscoverConfirmation(ctxFor(t), actor, request)
	if err != nil {
		t.Fatal(err)
	}
	projectKey, _ := foundation.ProjectLock(p.ID.String())
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
		if _, err := f.skills.ConfirmInitializedInTx(ctx, tx, actor, request, plan); err == nil {
			t.Error("missing provider lock authorized confirmation")
		}
		return nil
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("ignored missing initializer lock committed", result.State())
	}
}

func TestProjectB02CreateAuditEventFailuresRespectAcceptanceBoundary(t *testing.T) {
	for _, phase := range []string{"accepted-audit", "completed-audit", "created-event"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			owner := f.human(t, "owner-alpha", "user")
			var aud ac.Appender = f.aud
			var events oc.Appender = f.events
			if phase == "accepted-audit" {
				aud = failAudit{f.aud, ac.ProjectCreateAccepted}
			} else if phase == "completed-audit" {
				aud = failAudit{f.aud, ac.ProjectCreateCompleted}
			} else {
				events = failEvent{f.events}
			}
			request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "AtomicCreate"}
			m := meta(t, "atomic-create", nil)
			service := f.newService(t, f.skills, aud, events)
			_, err := service.CreateProject(ctxFor(t), owner, m, request)
			var failure *foundation.Fault
			want := foundation.Committed
			if phase == "accepted-audit" {
				want = foundation.NotCommitted
			}
			if !errors.As(err, &failure) || failure.CommitState != want {
				t.Fatal("creation failure misrepresented acceptance", err, want)
			}
			var projects, ready, accepted, completed, eventsCount int
			if err = f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_project.projects WHERE id=$1),(SELECT count(*) FROM agenteam_project.projects WHERE id=$1 AND initialized_at IS NOT NULL),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.accepted'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.completed'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.created')`, request.ProjectID.String()).Scan(&projects, &ready, &accepted, &completed, &eventsCount); err != nil {
				t.Fatal(err)
			}
			wantAccepted := 1
			if phase == "accepted-audit" {
				wantAccepted = 0
			}
			if projects != wantAccepted || accepted != wantAccepted || ready != 0 || completed != 0 || eventsCount != 0 {
				t.Fatal("creation atomic facts diverged", projects, ready, accepted, completed, eventsCount)
			}
			if r, err := f.service.CreateProject(ctxFor(t), owner, m, request); err != nil || r.State != c.CreationReady {
				t.Fatal("same-key creation recovery", err)
			}
		})
	}
}
