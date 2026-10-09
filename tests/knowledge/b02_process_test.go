//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Both interfaces name the same UUID but use different phantom types. This
// adapter delegates every death decision to the exact real D05 ProcessGuard.
type knowledgeGuardProcess struct {
	process ob.ProcessID
	guard   *object.ProcessGuard
}

func (p knowledgeGuardProcess) CurrentProcess() ob.ProcessID { return p.process }
func (p knowledgeGuardProcess) ConfirmStopped(ctx context.Context, process ob.ProcessID) error {
	old, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, old)
}

type publicationProcessRuntime struct {
	spool, bucket string
	guard         *object.ProcessGuard
	runtime       *object.Runtime
}

func (p *publicationProcessRuntime) initialize(t *testing.T, objects *object.Service, config object.StorageConfig) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if p.runtime == nil {
			// Constructor failure only: no initialized guard or death proof.
			objects.StopAdmission()
			if err := objects.Drain(ctx); err != nil {
				t.Error("owned construction resources did not drain", err)
			}
			if err := p.guard.Close(); err != nil {
				t.Error("unbound constructor guard did not close", err)
			}
			return
		}
		p.runtime.StopAdmission()
		if err := p.runtime.Drain(ctx); err != nil {
			_ = p.runtime.Force(ctx)
			t.Error("owned Runtime actual drain", err)
		}
	})
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, config)
	if err != nil {
		t.Fatal("owned transfer endpoint unavailable")
	}
	transfers, err := object.NewTransferService(objects, nil, endpoint)
	if err != nil {
		t.Fatal("owned transfer construction failed")
	}
	p.runtime, err = object.NewRuntime(objects, p.guard, transfers)
	if err != nil {
		t.Fatal("owned Runtime construction failed")
	}

	if err = p.runtime.Initialize(knowledgeContext(t)); err != nil {
		t.Fatal("owned Runtime initialization", err)
	}
}

// This hook stops before delegated PreparePayload; all later calls still use
// the real service. It cannot authorize, manufacture a payload or alter SQL.
type processPrepareUploads struct {
	oc.Uploads
	before func(context.Context) error
}

func (u processPrepareUploads) PreparePayload(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	if err := u.before(ctx); err != nil {
		return oc.PreparedPayload{}, err
	}
	return u.Uploads.PreparePayload(ctx, actor, owner, media, size, digest, body)
}

const processChildEnv = "AGENTEAM_KNOWLEDGE_PROCESS_CHILD"
const processBody = "owned process recovery 世界\n"

type knowledgeProcessInput struct {
	Database, Spool, Process, User, Session string
	Request                                 kc.CreateRequest
	Meta                                    f.CommandMeta
}

type knowledgeProcessReady struct {
	Stage, Bucket string
}

func processChild(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned child input")
	}
	rawInput, err := os.ReadFile(path)
	var input knowledgeProcessInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()
	if err != nil || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("invalid owned child input")
	}
	ready, control := os.NewFile(3, "knowledge-ready"), os.NewFile(4, "knowledge-control")
	if ready == nil || control == nil {
		t.Fatal("private child pipes unavailable")
	}
	defer ready.Close()
	defer control.Close()
	pg, err := pgfixture.Load()
	if err != nil {
		t.Fatal("owned PostgreSQL fixture unavailable")
	}
	config, err := pg.Config(input.Database, nil)
	if err != nil {
		t.Fatal("owned PostgreSQL config unavailable")
	}
	store, err := postgres.Open(knowledgeContext(t), config)
	if err != nil {
		t.Fatal("owned PostgreSQL Store unavailable")
	}
	x := newOwnerTreeFixtureOnStore(t, store)
	process, err := f.ParseID[ob.Process](input.Process)
	if err != nil {
		t.Fatal("invalid owned child process")
	}
	x.deps.Processes = treeProcess{process}
	runtime := &publicationProcessRuntime{spool: input.Spool}
	publicationFixtureWithRuntime(t, x, runtime)
	user, userErr := f.ParseID[id.User](input.User)
	session, sessionErr := f.ParseID[id.Session](input.Session)
	actor, actorErr := id.NewHuman(user, session)
	if userErr != nil || sessionErr != nil || actorErr != nil {
		t.Fatal("invalid owned child actor")
	}
	objects := x.deps.Objects.(*object.Service)
	child := publicationService(t, x, func(deps *knowledge.Dependencies) {
		deps.Uploads = processPrepareUploads{Uploads: deps.Uploads, before: func(ctx context.Context) error {
			// Knowledge has confirmed its durable work claim before calling
			// this wrapper. No real D05 Prepare has begun. Release only the
			// spool directory through Service.Drain, retaining the bound
			// Runtime's independent claim flock until actual process exit.
			// Neither this drain nor an unbound Close is a death proof.
			objects.StopAdmission()
			if err := objects.Drain(ctx); err != nil {
				return err
			}
			if err := json.NewEncoder(ready).Encode(knowledgeProcessReady{"prepare_blocked_guard_held", runtime.bucket}); err != nil {
				return err
			}
			var signal [1]byte
			_, err := io.ReadFull(control, signal[:])
			if err != nil {
				return err
			}
			return errors.New("unexpected child barrier release")
		}}
	})
	_, err = child.CreateDocument(knowledgeContext(t), actor, input.Meta, input.Request, publicationText(t, processBody))
	t.Fatal("owned child unexpectedly returned before SIGKILL", err)
}

type knowledgeOwnedChild struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	ready   *os.File
	control *os.File
}

func startProcessChild(t *testing.T, ctx context.Context, input knowledgeProcessInput) *knowledgeOwnedChild {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal("owned child input encoding failed")
	}
	path := filepath.Join(t.TempDir(), "process-input.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal("owned child input write failed")
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal("owned ready pipe unavailable")
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		_ = readyRead.Close()
		_ = readyWrite.Close()
		t.Fatal("owned control pipe unavailable")
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestKnowledgeB02ProcessRecovery$", "-test.timeout=45s")
	cmd.Env = append(os.Environ(), processChildEnv+"="+path)
	cmd.ExtraFiles = []*os.File{readyWrite, controlRead}
	// Child diagnostics cannot accidentally print fixture credentials or a
	// command key. The private pipe transports only a fixed stage and bucket.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	child := &knowledgeOwnedChild{cmd: cmd, done: make(chan struct{}), ready: readyRead, control: controlWrite}
	err = cmd.Start()
	_ = readyWrite.Close()
	_ = controlRead.Close()
	if err != nil {
		_ = readyRead.Close()
		_ = controlWrite.Close()
		t.Fatal("owned child start failed")
	}
	go func() { child.waitErr = cmd.Wait(); close(child.done) }()
	return child
}

func (c *knowledgeOwnedChild) stop(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	default:
		_ = c.cmd.Process.Kill()
	}
	_ = c.control.Close()
	_ = c.ready.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Error("owned child actual Wait did not finish")
	}
}

func (c *knowledgeOwnedChild) awaitReady(t *testing.T, ctx context.Context) string {
	t.Helper()
	type reply struct {
		ready knowledgeProcessReady
		err   error
	}
	result, joined := make(chan reply, 1), make(chan struct{})
	go func() {
		defer close(joined)
		var r reply
		d := json.NewDecoder(io.LimitReader(c.ready, 4096))
		d.DisallowUnknownFields()
		r.err = d.Decode(&r.ready)
		result <- r
	}()
	defer func() {
		_ = c.ready.Close()
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Error("private pipe reader did not join")
		}
	}()
	select {
	case r := <-result:
		if r.err != nil || r.ready.Stage != "prepare_blocked_guard_held" || !strings.HasPrefix(r.ready.Bucket, "d05-") {
			t.Fatal("owned child did not reach exact Prepare barrier")
		}
		return r.ready.Bucket
	case <-c.done:
		t.Fatal("owned child exited before Prepare barrier")
	case <-ctx.Done():
		t.Fatal("owned child Prepare barrier timed out")
	}
	return ""
}

type knowledgeProcessWork struct {
	command, state, publication, process, attempt, phase string
	fence                                                int64
	noSource                                             bool
}

func processWork(t *testing.T, x *ownerTreeFixture, p id.ProjectID, key f.IdempotencyKey) knowledgeProcessWork {
	t.Helper()
	var w knowledgeProcessWork
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT c.id::text,c.state,p.phase,w.process_id::text,w.attempt_id::text,w.fence,w.phase,w.source_project_id IS NULL
 FROM agenteam_knowledge.commands c JOIN agenteam_knowledge.publications p ON p.command_id=c.id
 JOIN agenteam_knowledge.work_claims w ON w.command_id=c.id WHERE c.project_id=$1 AND c.command_name='create' AND c.command_key=$2`, p.String(), string(key)).Scan(&w.command, &w.state, &w.publication, &w.process, &w.attempt, &w.fence, &w.phase, &w.noSource)
	if err != nil {
		t.Fatal("durable work observation failed", err)
	}
	return w
}

func processFacts(t *testing.T, x *ownerTreeFixture, p id.ProjectID) [9]int {
	t.Helper()
	var counts [9]int
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.objects),
 (SELECT count(*) FROM agenteam_object.uploads),
 (SELECT count(*) FROM agenteam_object.object_references),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.object_cleanup WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.object_leases)`, p.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7], &counts[8])
	if err != nil {
		t.Fatal("durable publication facts observation failed", err)
	}
	return counts
}

// The original intent persists its fixed event header in commands.plan.
// command_events is staged only after the real payload has been prepared and
// uploaded, so the pre-Prepare barrier must still have zero such rows.
func processEventPlan(t *testing.T, x *ownerTreeFixture, p id.ProjectID, key f.IdempotencyKey, document kc.DocumentID) string {
	t.Helper()
	var raw string
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT plan::text FROM agenteam_knowledge.commands
 WHERE project_id=$1 AND command_name='create' AND command_key=$2 AND document_id=$3
 AND state='planned' AND receipt IS NULL`, p.String(), string(key), document.String()).Scan(&raw)
	if err != nil {
		t.Fatal("original planned command header unavailable")
	}
	header, err := ev.DecodeHeader([]byte(raw))
	if err != nil || header.EventType != kc.ContentChangedEvent || header.SchemaVersion != 1 || header.AggregateType != kc.KnowledgeAggregate || header.AggregateID.String() != document.String() || header.AggregateVersion == nil || *header.AggregateVersion != 1 || header.AggregateSequence != nil || header.Scope.Kind != ev.ProjectScope || header.Scope.ProjectID.String() != p.String() {
		t.Fatal("original planned command header does not match the exact creation")
	}
	return raw
}

type processObservedGuard struct {
	ob.ProcessAuthority
	checked   []ob.ProcessID
	confirmed []ob.ProcessID
}

func (g *processObservedGuard) ConfirmStopped(ctx context.Context, process ob.ProcessID) error {
	g.checked = append(g.checked, process)
	err := g.ProcessAuthority.ConfirmStopped(ctx, process)
	if err == nil {
		g.confirmed = append(g.confirmed, process)
	}
	return err
}

func TestKnowledgeB02ProcessRecovery(t *testing.T) {
	if path := os.Getenv(processChildEnv); path != "" {
		processChild(t, path)
		return
	}
	db := pgfixture.NewDatabase(t)
	knowledgeMigrate(t, db, knowledgeMigrationSource(t, knowledgeMigrationFiles(t, "00025")))
	raw, err := postgres.Open(knowledgeContext(t), db.Config(t, nil))
	if err != nil {
		t.Fatal("owned Store unavailable")
	}
	x := newOwnerTreeFixtureOnStore(t, raw)
	actor := x.human(t)
	p := x.project(t, actor, true)
	activity := x.activity(t, actor)
	old := treeID[ob.Process](t)
	input := knowledgeProcessInput{Database: db.Name, Spool: filepath.Join(t.TempDir(), "spool"), Process: old.String(), User: actor.Details().UserID, Session: actor.Details().SessionID,
		Request: kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "process recovery"}, Meta: treeMeta(t)}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	child := startProcessChild(t, ctx, input)
	defer child.stop(t)
	bucket := child.awaitReady(t, ctx)
	before := processWork(t, x, p, input.Meta.IdempotencyKey)
	if before.state != "planned" || before.publication != "planned" || before.process != old.String() || before.phase != "active" || before.fence != 1 || !before.noSource || before.attempt == "" {
		t.Fatal("old child did not commit exact active work before Prepare")
	}
	if counts := processFacts(t, x, p); counts != [9]int{} || !x.activity(t, actor).Equal(activity) {
		t.Fatal("blocked child produced publication facts or Activity")
	}
	commands, stagedEvents := x.count(t, p)
	if commands != 1 || stagedEvents != 0 {
		t.Fatalf("original command planning counts: commands=%d staged_events=%d", commands, stagedEvents)
	}
	eventPlan := processEventPlan(t, x, p, input.Meta.IdempotencyKey, input.Request.DocumentID)
	current := x.deps.Processes.CurrentProcess()
	runtime := &publicationProcessRuntime{spool: input.Spool, bucket: bucket}
	publicationFixtureWithRuntime(t, x, runtime)
	var sameClaims bool
	err = raw.QueryRow(knowledgeContext(t), `SELECT a.state='claimed' AND b.state='claimed' AND a.stopped_at IS NULL AND b.stopped_at IS NULL
 AND a.store_id=b.store_id AND a.deployment_id=b.deployment_id AND a.spool_identity=b.spool_identity
 AND a.spool_device=b.spool_device AND a.spool_inode=b.spool_inode AND a.host_identity=b.host_identity AND a.boot_id=b.boot_id
 FROM agenteam_object.process_claims a CROSS JOIN agenteam_object.process_claims b WHERE a.process_id=$1 AND b.process_id=$2`, old.String(), current.String()).Scan(&sameClaims)
	if err != nil || !sameClaims {
		t.Fatal("Runtime did not bind same Store/host/spool claims", err)
	}
	guard := &processObservedGuard{ProcessAuthority: x.deps.Processes}
	prepares := 0
	var replacement knowledgeProcessWork
	s := publicationService(t, x, func(deps *knowledge.Dependencies) {
		deps.Processes = guard
		deps.Uploads = processPrepareUploads{Uploads: deps.Uploads, before: func(context.Context) error {
			prepares++
			replacement = processWork(t, x, p, input.Meta.IdempotencyKey)
			return nil
		}}
	})
	_, err = s.CreateDocument(knowledgeContext(t), actor, input.Meta, input.Request, publicationText(t, processBody))
	treeCode(t, err, f.ResourceBusy)
	if prepares != 0 || len(guard.checked) != 1 || guard.checked[0] != old || len(guard.confirmed) != 0 || processWork(t, x, p, input.Meta.IdempotencyKey) != before || processFacts(t, x, p) != [9]int{} || !x.activity(t, actor).Equal(activity) {
		t.Fatal("live exact Guard allowed work takeover or publication side effects")
	}
	if commands, stagedEvents := x.count(t, p); commands != 1 || stagedEvents != 0 {
		t.Fatalf("live-child retry planning counts: commands=%d staged_events=%d", commands, stagedEvents)
	}
	if processEventPlan(t, x, p, input.Meta.IdempotencyKey, input.Request.DocumentID) != eventPlan {
		t.Fatal("live-child retry changed the original fixed event header")
	}
	digest, err := kc.CreateDigest(actor, input.Meta, input.Request, publicationText(t, processBody))
	if err != nil {
		t.Fatal("original command digest", err)
	}
	lookupRequest := kc.LookupRequest{ProjectID: p, Command: kc.Create, Key: input.Meta.IdempotencyKey, SemanticDigest: digest}
	lookup, err := s.LookupCommand(knowledgeContext(t), actor, lookupRequest)
	if err != nil || lookup.State != kc.InProgress || lookup.Receipt != nil {
		t.Fatal("live child command was not in progress", err)
	}
	// This exact owned OS process, still blocked in the Knowledge call, must
	// actually exit from SIGKILL. PID absence or context cancellation is not
	// accepted as its Wait result or as the ProcessGuard's decision.
	if err = child.cmd.Process.Kill(); err != nil {
		t.Fatal("owned child SIGKILL failed")
	}
	select {
	case <-child.done:
	case <-ctx.Done():
		t.Fatal("owned child actual Wait timed out")
	}
	var exit *exec.ExitError
	if !errors.As(child.waitErr, &exit) {
		t.Fatal("owned child lacked actual killed exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("owned child did not actually exit by SIGKILL")
	}
	var oldClaimed bool
	if err = raw.QueryRow(knowledgeContext(t), `SELECT state='claimed' AND stopped_at IS NULL FROM agenteam_object.process_claims WHERE process_id=$1`, old.String()).Scan(&oldClaimed); err != nil || !oldClaimed {
		t.Fatal("SIGKILL was replaced by a graceful stopped checkpoint", err)
	}
	oldObjectProcess, _ := f.ParseID[oc.Process](old.String())
	if err = runtime.guard.ConfirmStopped(knowledgeContext(t), oldObjectProcess); err != nil {
		t.Fatal("real exact ProcessGuard death proof rejected", err)
	}
	doc, err := s.CreateDocument(knowledgeContext(t), actor, input.Meta, input.Request, publicationText(t, processBody))
	if err != nil {
		t.Fatal("same-command recovery failed", err)
	}
	if prepares != 1 || len(guard.checked) != 2 || guard.checked[1] != old || len(guard.confirmed) != 1 || guard.confirmed[0] != old || replacement.command != before.command || replacement.process != current.String() || replacement.attempt == before.attempt || replacement.fence != before.fence+1 || replacement.phase != "active" {
		t.Fatal("recovery did not consume exact death proof and new attempt/fence once")
	}
	publicationFacts(t, x, doc, []byte(processBody))
	publicationRead(t, x, actor, doc, []byte(processBody))
	audits, events := publicationCount(t, x, p)
	if audits != 1 || events != 1 || !x.activity(t, actor).After(activity) {
		t.Fatal("recovery did not publish exactly one Audit/Event and Activity")
	}
	lookup, err = s.LookupCommand(knowledgeContext(t), actor, lookupRequest)
	if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || lookup.Receipt.Document == nil {
		t.Fatal("recovered canonical receipt unavailable", err)
	}
	titleSameDocument(t, doc, *lookup.Receipt.Document)
	after, facts, changedAt := processWork(t, x, p, input.Meta.IdempotencyKey), processFacts(t, x, p), x.activity(t, actor)
	if after.phase != "joined" || after.attempt != replacement.attempt || after.fence != replacement.fence || after.state != "completed" || after.publication != "published" {
		t.Fatal("recovered exact attempt was not durably joined")
	}
	replay, err := s.CreateDocument(knowledgeContext(t), actor, input.Meta, input.Request, publicationText(t, processBody))
	if err != nil {
		t.Fatal("same-command completed replay", err)
	}
	titleSameDocument(t, doc, replay)
	if prepares != 1 || len(guard.checked) != 2 || guard.checked[1] != old || len(guard.confirmed) != 1 || processWork(t, x, p, input.Meta.IdempotencyKey) != after || processFacts(t, x, p) != facts || !x.activity(t, actor).Equal(changedAt) {
		t.Fatal("completed replay repeated work, facts or Activity")
	}
}
