//go:build integration && linux

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// This observer surrounds the real DeviceHandler's hijacked TLS connection.
// It neither substitutes a peer/Handler nor changes any write result. Only
// bytes actually accepted by the original Write enter the bounded decoder;
// request identities are retained, while payload equality is a private bool.
type operationReadFileWire struct {
	mu       sync.Mutex
	expected p.ReadFilePayload
	requests []operationReadFileFrame
	invalid  bool
}

type operationReadFileFrame struct {
	request, operation, execution, project, agent, mount, workspace p.ID
}

func (o *operationReadFileWire) wrap(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(&operationReadFileResponse{ResponseWriter: w, observer: o}, r)
	})
}

func (o *operationReadFileWire) snapshot() ([]operationReadFileFrame, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.requests), !o.invalid
}

func (o *operationReadFileWire) reject() {
	o.mu.Lock()
	o.invalid = true
	o.mu.Unlock()
}

func (o *operationReadFileWire) message(raw []byte) {
	message, err := p.Decode(raw)
	if err != nil || message.AllowedFrom(p.Central) != nil {
		o.reject()
		return
	}
	request, ok := message.Payload().(p.Request)
	if !ok {
		return
	}
	payload, err := p.DecodeReadFilePayload(request.Payload)
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil || request.OperationName != p.ReadFileOperation || request.OperationRevision != p.ReadFileRevision || payload != o.expected {
		o.invalid = true
	}
	o.requests = append(o.requests, operationReadFileFrame{
		request: request.RequestID, operation: request.OperationID,
		execution: request.ExecutionID, project: request.ProjectID, agent: request.AgentID,
		mount: request.Mount.MountID, workspace: request.Mount.WorkspaceID,
	})
}

type operationReadFileResponse struct {
	http.ResponseWriter
	observer *operationReadFileWire
}

func (w *operationReadFileResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *operationReadFileResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffered, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return conn, buffered, err
	}
	// The standard server has no buffered response before Upgrade. Preserve
	// its original reader (including already buffered request bytes), and put
	// both subsequent buffered and direct writes through the same observer.
	if buffered == nil || buffered.Writer == nil || buffered.Writer.Buffered() != 0 {
		w.observer.reject()
		return conn, buffered, nil // Observation failure never changes native I/O.
	}
	observed := &operationReadFileConnection{Conn: conn, observer: w.observer}
	return observed, bufio.NewReadWriter(buffered.Reader, bufio.NewWriter(observed)), nil
}

type operationReadFileConnection struct {
	net.Conn
	observer *operationReadFileWire
	mu       sync.Mutex // Native Gorilla writers already serialize frame writes.
	header   bool
	invalid  bool
	buffer   []byte
	message  []byte
	fragment bool
}

func (c *operationReadFileConnection) Write(raw []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Conn.Write(raw)
	if n > 0 {
		c.consume(raw[:n])
	}
	return n, err
}

func (c *operationReadFileConnection) Close() error {
	err := c.Conn.Close()
	c.mu.Lock()
	if len(c.buffer) != 0 || c.fragment {
		c.reject()
	}
	c.mu.Unlock()
	return err
}

func (c *operationReadFileConnection) reject() {
	c.invalid = true
	clear(c.buffer)
	clear(c.message)
	c.buffer, c.message = nil, nil
	c.observer.reject()
}

func (c *operationReadFileConnection) consume(raw []byte) {
	if c.invalid {
		return
	}
	if len(c.buffer)+len(raw) > p.MaxMessageBytes+4096 {
		c.reject()
		return
	}
	c.buffer = append(c.buffer, raw...)
	if !c.header {
		end := bytes.Index(c.buffer, []byte("\r\n\r\n"))
		if end < 0 {
			if len(c.buffer) > 4096 {
				c.reject()
			}
			return
		}
		if !bytes.HasPrefix(c.buffer, []byte("HTTP/1.1 101 ")) {
			c.reject()
			return
		}
		c.buffer = c.buffer[end+4:]
		c.header = true
	}
	for len(c.buffer) >= 2 {
		first, second := c.buffer[0], c.buffer[1]
		final, opcode := first&128 != 0, first&15
		if first&112 != 0 || second&128 != 0 {
			c.reject() // No extensions or server masking in the real adapter.
			return
		}
		header, length := 2, uint64(second&127)
		if length == 126 {
			if len(c.buffer) < 4 {
				return
			}
			header, length = 4, uint64(binary.BigEndian.Uint16(c.buffer[2:4]))
		} else if length == 127 {
			if len(c.buffer) < 10 {
				return
			}
			header, length = 10, binary.BigEndian.Uint64(c.buffer[2:10])
		}
		if length > p.MaxMessageBytes || opcode >= 8 && (!final || length > 125) {
			c.reject()
			return
		}
		if uint64(len(c.buffer)-header) < length {
			return
		}
		payload := c.buffer[header : header+int(length)]
		switch opcode {
		case 1:
			if c.fragment {
				c.reject()
				return
			}
			c.message = append(c.message[:0], payload...)
			c.fragment = !final
		case 0:
			if !c.fragment || len(c.message)+len(payload) > p.MaxMessageBytes {
				c.reject()
				return
			}
			c.message = append(c.message, payload...)
			c.fragment = !final
		case 8, 9, 10:
		default:
			c.reject()
			return
		}
		c.buffer = c.buffer[header+int(length):]
		if opcode < 8 && final {
			c.observer.message(c.message)
			clear(c.message)
			c.message = c.message[:0]
		}
	}
}

type operationReadFileFixture struct {
	facts             *toolFactsFixture
	device            *runnerhttp.DeviceHandler
	wire              *operationReadFileWire
	expected          p.ReadFileResult
	expectedCanonical []byte
	checkpoint        toolFactsIdentity
}

func (v *operationReadFileFixture) requireReceipt(t *testing.T, receipt ec.DirectTextReceipt) {
	t.Helper()
	got, err := v.facts.factsFor(ctxFor(t), v.facts.capture.v.base.raw, v.expectedCanonical, 0)
	readFileAdapterOK(t, err, "original ToolCall facts before any Operation")
	if receipt.ExecutionID != v.facts.capture.created.ID || receipt.Status != ec.Running || receipt.TranscriptThrough != 2 || int64(receipt.Version) != got.Version || receipt.SnapshotID.String() != got.Snapshot || receipt.StartID.String() != got.Start || receipt.RoundID.String() != got.Round || receipt.CallID.String() != got.Logical {
		t.Fatal("public receipt differs from the original nonterminal checkpoint")
	}
	_, _, begin, results, closes, joined := v.facts.model.observed()
	if begin != 1 || results != 1 || closes != 1 || !joined {
		t.Fatal("original Model call or actual handle close differs")
	}
	v.facts.checkBinding(t, got)
	v.checkpoint = got
}

func (v *operationReadFileFixture) requireJoined(t *testing.T, before operationReadFileSQL, success bool) {
	t.Helper()
	v.facts.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := v.facts.driver.Drain(ctx); err != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
		t.Fatal("original ToolCall driver/Model handle did not actually join", err)
	}
	// The still-running ToolCall and its protected arguments/references must
	// remain unchanged. This stage has exactly one Operation; count alone does
	// not replace the original Operation/Attempt/result/wire postconditions.
	got, err := v.facts.factsFor(ctxFor(t), v.facts.capture.v.base.raw, v.expectedCanonical, 1)
	readFileAdapterOK(t, err, "original ToolCall facts after Operation and actual join")
	if got != v.checkpoint {
		t.Fatal("Operation or join changed the original ToolCall identity")
	}
	after := v.sql(t)
	requireOperationReadFileIdentity(t, before, after)
	v.requireTerminal(t, after, success)
	wireCount := 0
	if success {
		wireCount = 1
	}
	v.requireWire(t, wireCount, after)
}

// The only controlled business port is the original Model JSON caller. Its
// answer is consumed by the actual ToolCall driver; no SQL inserts an Execution,
// protected argument, Mount, Operation or Attempt into this fixture.
func newOperationReadFileFixture(t *testing.T, fullResult bool) *operationReadFileFixture {
	t.Helper()
	directory := readFileAdapterPrivateDirectory(t)
	rootPath := filepath.Join(directory, "root")
	readFileAdapterOK(t, os.Mkdir(rootPath, 0700), "owned Runner root")
	var runners *runnerservice.Service
	var enrollment rc.EnrollmentMaterial
	facts := newToolFactsFixtureWithRunner(t, rootPath, func(service *runnerservice.Service, material rc.EnrollmentMaterial) {
		runners, enrollment = service, material
	})
	defer func() { enrollment = rc.EnrollmentMaterial{} }()
	if runners == nil || !enrollment.Token.Valid() {
		t.Fatal("formal Runner creation did not hand off its original enrollment")
	}
	length := 64
	content := "original read-file result\r\nsecond line\n"
	if fullResult {
		length = p.MaxReadFileBytes
		content = strings.Repeat("λ\r\n", p.MaxReadFileBytes/4)
	}
	arguments := p.ReadFileArguments{Path: "private-tool-call-argument.txt", Offset: 0, Length: length, Encoding: "utf8"}
	// This is input to the sole declared Model JSON substitute, set before its
	// one BeginChat. The original producer seals and owns the actual bytes.
	facts.model.arguments = []byte(fmt.Sprintf(`{ "path":%q, "offset":"0", "mount_id":%q, "length":%d, "encoding":"utf8" }`, arguments.Path, facts.mount.MountID.String(), length))
	completion := facts.startHeld(t)
	facts.model.unhold()
	completed := toolFactsWait(t, completion)
	readFileAdapterOK(t, completed.err, "actual ToolCall producer")
	v := &operationReadFileFixture{
		facts:             facts,
		expected:          p.ReadFileResult{Path: arguments.Path, BytesRead: len(content), EOF: true, Encoding: "utf8", Data: content},
		expectedCanonical: []byte(fmt.Sprintf(`{"encoding":"utf8","length":%d,"mount_id":%q,"offset":"0","path":"private-tool-call-argument.txt"}`, length, facts.mount.MountID.String())),
	}
	v.requireReceipt(t, completed.receipt)
	root, err := os.Open(rootPath)
	readFileAdapterOK(t, err, "trusted root descriptor")
	manager, managerErr := workspace.New(root)
	closeErr := root.Close()
	readFileAdapterOK(t, managerErr, "real Workspace manager")
	readFileAdapterOK(t, closeErr, "borrowed root descriptor close")
	// The server cleanup is registered before Manager/Client below, so actual
	// Tool owner -> Client.Run -> Manager -> server/Runner retirement is joined.
	wire := &operationReadFileWire{expected: p.ReadFilePayload{WorkspaceName: facts.mount.Workspace, Arguments: arguments}}
	device, err := runnerhttp.NewDeviceHandler(runners)
	readFileAdapterOK(t, err, "real device HTTP handler")
	v.device, v.wire = device, wire
	server := newReadFileAdapterServer(t, wire.wrap(device), runners)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Drain(ctx); err != nil {
			t.Error("original Workspace manager did not actually join")
		}
	})
	project, agentID := facts.capture.v.base.project.ID, facts.capture.v.agentID
	handle, err := manager.Ensure(ctxFor(t), workspace.Identity{ProjectID: p.ID(project.String()), AgentID: p.ID(agentID.String()), Name: facts.mount.Workspace})
	readFileAdapterOK(t, err, "real Workspace Ensure")
	closeContext, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr = handle.Drain(closeContext)
	cancelClose()
	readFileAdapterOK(t, closeErr, "original initial Workspace handle close")
	readFileAdapterOK(t, os.WriteFile(filepath.Join(rootPath, project.String(), agentID.String(), facts.mount.Workspace, arguments.Path), []byte(content), 0600), "owned read-file contents")
	handler, err := readfile.New(manager)
	readFileAdapterOK(t, err, "real read-file Handler")
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(facts.runner.ID.String()), RootPath: rootPath}
	states := make(chan control.ConnectionState, 16)
	client, err := control.NewReadFile(control.Options{IdentityFile: filepath.Join(directory, "identity.json"), Configuration: &configuration, EnrollmentToken: enrollment.Token, RunnerVersion: "read-file-operation-fixture", Roots: server.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
		select {
		case states <- state:
		case <-ctx.Done():
		}
	}}, handler)
	readFileAdapterOK(t, err, "real authenticated Runner control client")
	enrollment = rc.EnrollmentMaterial{}
	runDone := make(chan struct{})
	var runErr error
	go func() { runErr = client.Run(context.Background()); close(runDone) }()
	t.Cleanup(func() {
		client.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Drain(ctx); err != nil {
			t.Error("original control client Drain did not join")
		}
		select {
		case <-runDone:
			if !errors.Is(runErr, context.Canceled) {
				t.Error("original control Run outcome differs")
			}
		case <-ctx.Done():
			t.Error("original control Run return missing")
		}
		if _, valid := wire.snapshot(); !valid {
			t.Error("actual wire observation was incomplete or invalid")
		}
	})
	connected, cancelConnect := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelConnect()
	for {
		select {
		case state := <-states:
			if state == control.Connected {
				return v
			}
		case <-runDone:
			t.Fatal("original Runner returned before authenticated Hello")
		case <-connected.Done():
			t.Fatal("original Runner did not complete authenticated Hello")
		}
	}
}

// Only the original Store's return boundary is controlled. The Attempt is
// physically Committed before the barrier; no acknowledgement is fabricated.
// The final-loss mode projects Unknown only after that final real COMMIT.
type operationReadFileStore struct {
	fixtureStore
	execution                      i.ExecutionID
	mu                             sync.Mutex
	entered, release               chan struct{}
	releaseOnce                    sync.Once
	held, loss                     bool
	loseFinal                      bool
	physicalAttempt, physicalFinal f.CommitResult
	unknown                        f.ID[f.TransactionAttempt]
	problem                        bool
}

func (s *operationReadFileStore) resume() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *operationReadFileStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	details := cause.Details()
	owners := details.Primary.OwnerIDs()
	ours := details.Kind == f.CommandsCause && details.Primary.Namespace() == "tool.operation" && details.Primary.Command() == "prepare" && len(owners) == 1 && owners[0] == s.execution.String()
	var phase, attempt, request string
	var version int64
	var retired bool
	observed := false
	result := s.fixtureStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if !ours {
			return nil
		}
		x, err := s.fixtureStore.InTx(tx)
		if err == nil {
			err = x.QueryRow(ctx, `SELECT o.phase,o.version,COALESCE(a.attempt_id,''),COALESCE(a.backend_request_id,''),COALESCE(a.retired,false) FROM agenteam_tool.operations o LEFT JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id WHERE o.execution_id=$1 AND o.operation_kind='runner.read-file'`, s.execution.String()).Scan(&phase, &version, &attempt, &request, &retired)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			s.mu.Lock()
			s.problem = true
			s.mu.Unlock()
			return nil
		}
		observed = true
		return nil
	})
	if !ours || !observed || result.State() != f.Committed {
		return result
	}
	s.mu.Lock()
	if !s.held && phase == "running" && version == 2 && attempt != "" && p.ID(request).Valid() && !retired {
		s.held = true
		s.physicalAttempt = result
		close(s.entered)
		s.mu.Unlock()
		select {
		case <-s.release:
		case <-ctx.Done():
			s.mu.Lock()
			s.problem = true
			s.mu.Unlock()
		}
		return result
	}
	if s.loseFinal && !s.loss && phase == "succeeded" && version == 3 && retired {
		s.loss = true
		s.physicalFinal = result
		s.mu.Unlock()
		return f.UnknownResult(s.unknown, cause)
	}
	s.mu.Unlock()
	return result
}

type operationReadFileSQL struct {
	operation, attempt, request, project, agent, execution, process string
	phase, attemptPhase, outcome, fingerprint, inputDigest          string
	version, ordinal, fence                                         int64
	retired                                                         bool
	input, terminal, attemptOutcome, result                         []byte
	resultDigest                                                    string
	resultBytes, operations, attempts, results                      int64
}

func (v *operationReadFileFixture) sql(t *testing.T) operationReadFileSQL {
	t.Helper()
	var out operationReadFileSQL
	e := v.facts.capture.created.ID.String()
	err := v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.operation_id,o.active_attempt_id,a.backend_request_id,o.project_id,o.agent_id,o.execution_id,a.process_id,o.phase,a.phase,COALESCE(o.outcome,''),o.fingerprint,o.input_digest,o.version,a.ordinal,a.fence,a.retired,o.input_data,COALESCE(o.terminal_data,''::bytea),COALESCE(a.outcome_data,''::bytea),COALESCE(r.body_data,''::bytea),COALESCE(r.body_sha256,''),COALESCE(r.body_bytes,0),(SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),(SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1),(SELECT count(*) FROM agenteam_tool.read_file_results WHERE execution_id=$1) FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id LEFT JOIN agenteam_tool.read_file_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.execution_id=o.execution_id AND r.project_id=o.project_id AND r.agent_id=o.agent_id WHERE o.execution_id=$1 AND o.operation_kind='runner.read-file'`, e).Scan(&out.operation, &out.attempt, &out.request, &out.project, &out.agent, &out.execution, &out.process, &out.phase, &out.attemptPhase, &out.outcome, &out.fingerprint, &out.inputDigest, &out.version, &out.ordinal, &out.fence, &out.retired, &out.input, &out.terminal, &out.attemptOutcome, &out.result, &out.resultDigest, &out.resultBytes, &out.operations, &out.attempts, &out.results)
	readFileAdapterOK(t, err, "independent durable Operation/Attempt observation")
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original ProcessGuard identity")
	if !p.ID(out.operation).Valid() || !p.ID(out.attempt).Valid() || !p.ID(out.request).Valid() || out.project != v.facts.capture.v.base.project.ID.String() || out.agent != v.facts.capture.v.agentID.String() || out.execution != e || out.process != process.String() || out.operations != 1 || out.attempts != 1 || out.ordinal != 1 || out.fence != 1 || out.fingerprint == "" || out.inputDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(out.input)) {
		t.Fatal("durable original identity or scope differs")
	}
	if bytes.Contains(out.input, []byte(v.expected.Path)) || bytes.Contains(out.input, []byte(v.expected.Data)) {
		t.Fatal("Operation metadata duplicated protected input or file data")
	}
	return out
}

func (v *operationReadFileFixture) executor(t *testing.T, loseFinal bool) (*toolruntime.ReadFileExecutor, *toolruntime.ReadFileAuthority, *operationReadFileStore) {
	t.Helper()
	b := v.facts.capture.v.base
	store := &operationReadFileStore{fixtureStore: b.tracked, execution: v.facts.capture.created.ID, entered: make(chan struct{}), release: make(chan struct{}), loseFinal: loseFinal, unknown: id[f.TransactionAttempt](t)}
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original process identity")
	authority, err := toolruntime.NewReadFileAuthority(store, v.facts.capture.v.agent.guard, process, v.facts.driver)
	readFileAdapterOK(t, err, "real Execution-backed Tool read-file authority")
	resolver, err := mount.NewReadFileResolver(store, v.facts.capture.configuration, authority)
	readFileAdapterOK(t, err, "real current Mount resolver")
	dispatcher, err := runnerhttp.NewReadFileDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "real WSS dispatcher")
	schemas, err := toolruntime.NewRegistrySchemaValidator(store, v.facts.registry)
	readFileAdapterOK(t, err, "actual historical Registry schema validator")
	executor, err := toolruntime.NewReadFileExecutor(authority, resolver, dispatcher, schemas)
	readFileAdapterOK(t, err, "explicit single read-file executor")
	t.Cleanup(func() {
		store.resume()
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := authority.Drain(ctx); err != nil || !authority.Joined() {
			t.Error("original Tool owner did not actually retire")
		}
		store.mu.Lock()
		problem := store.problem
		store.mu.Unlock()
		if problem {
			t.Error("original Store return barrier observation failed")
		}
	})
	return executor, authority, store
}

type operationReadFileCompletion struct {
	result toolruntime.ReadFileOutcome
	err    error
}

func startOperationReadFile(t *testing.T, executor *toolruntime.ReadFileExecutor, execution i.ExecutionID, store *operationReadFileStore) <-chan operationReadFileCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	done := make(chan operationReadFileCompletion, 1)
	go func() {
		out, err := executor.ExecuteReadFile(ctx, execution)
		done <- operationReadFileCompletion{out, err}
	}()
	select {
	case <-store.entered:
	case <-done:
		t.Fatal("original read-file returned before the committed Attempt barrier")
	case <-ctx.Done():
		t.Fatal("original read-file did not reach committed Attempt barrier")
	}
	return done
}

func waitOperationReadFile(t *testing.T, done <-chan operationReadFileCompletion) operationReadFileCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		t.Fatal("original ExecuteReadFile actual return missing")
	}
	return operationReadFileCompletion{}
}

func requireOperationReadFileIdentity(t *testing.T, before, after operationReadFileSQL) {
	t.Helper()
	if before.operation != after.operation || before.attempt != after.attempt || before.request != after.request || before.fingerprint != after.fingerprint || before.inputDigest != after.inputDigest || !bytes.Equal(before.input, after.input) {
		t.Fatal("the original Operation/Attempt/RID or logical input identity changed")
	}
}

func (v *operationReadFileFixture) requireWire(t *testing.T, count int, row operationReadFileSQL) {
	t.Helper()
	frames, valid := v.wire.snapshot()
	if !valid || len(frames) != count {
		t.Fatal("actual successful WSS request writes differ")
	}
	if count == 1 {
		frame := frames[0]
		if string(frame.request) != row.request || string(frame.operation) != row.operation || string(frame.execution) != row.execution || string(frame.project) != row.project || string(frame.agent) != row.agent || frame.mount != p.ID(v.facts.mount.MountID.String()) || frame.workspace != p.ID(v.facts.mount.WorkspaceID.String()) {
			t.Fatal("actual wire identities differ from the original durable Attempt")
		}
	}
}

func (v *operationReadFileFixture) requireTerminal(t *testing.T, row operationReadFileSQL, success bool) {
	t.Helper()
	var terminal struct {
		Format                                     int
		Operation, Attempt, Request, Outcome, Code string
		Attempted                                  bool
		Result                                     *struct {
			Operation, Attempt, SHA256 string
			Bytes                      int64
		}
	}
	if json.Unmarshal(row.terminal, &terminal) != nil || !bytes.Equal(row.terminal, row.attemptOutcome) || len(row.terminal) > 32768 || terminal.Format != 1 || terminal.Operation != row.operation || terminal.Attempt != row.attempt || terminal.Request != row.request || !row.retired || row.version != 3 {
		t.Fatal("final metadata did not bind one original retired Attempt")
	}
	if success {
		want, err := p.EncodeReadFileResult(v.expected)
		readFileAdapterOK(t, err, "independently expected full read-file result")
		if len(want) <= 32768 || row.phase != "succeeded" || row.attemptPhase != "succeeded" || row.outcome != "success" || terminal.Outcome != "success" || terminal.Code != "" || !terminal.Attempted || terminal.Result == nil || row.results != 1 || !bytes.Equal(row.result, want) || row.resultBytes != int64(len(want)) || row.resultDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(want)) || terminal.Result.Operation != row.operation || terminal.Result.Attempt != row.attempt || terminal.Result.SHA256 != row.resultDigest || terminal.Result.Bytes != row.resultBytes {
			t.Fatal("original full result was truncated, replaced or not atomically retired")
		}
	} else if row.phase != "failed" || row.attemptPhase != "failed" || row.outcome != "error" || terminal.Outcome != "failure" || terminal.Code != "not_dispatched" || terminal.Attempted || terminal.Result != nil || row.results != 0 || len(row.result) != 0 {
		t.Fatal("current Mount denial was not recorded as a retired unsent failure")
	}
}

func TestRunnerReadFileOperation(t *testing.T) {
	t.Run("result-commit-loss-and-observation", func(t *testing.T) {
		v := newOperationReadFileFixture(t, true)
		executor, authority, store := v.executor(t, true)
		execution := v.facts.capture.created.ID
		done := startOperationReadFile(t, executor, execution, store)
		pending := v.sql(t)
		if pending.phase != "running" || pending.attemptPhase != "running" || pending.version != 2 || pending.retired || pending.results != 0 || len(pending.terminal) != 0 {
			t.Fatal("Attempt was not physically committed before its held original return")
		}
		v.requireWire(t, 0, pending)
		store.resume()
		completed := waitOperationReadFile(t, done)
		requireCode(t, completed.err, f.CommitUnknown)
		if completed.result.OperationID().Validate() == nil {
			t.Fatal("lost final acknowledgement published a successful outcome")
		}
		store.mu.Lock()
		physicalAttempt, physicalFinal, loss, problem := store.physicalAttempt, store.physicalFinal, store.loss, store.problem
		store.mu.Unlock()
		if physicalAttempt.State() != f.Committed || physicalFinal.State() != f.Committed || !loss || problem {
			t.Fatal("controlled loss did not follow both real original COMMIT results")
		}
		final := v.sql(t)
		requireOperationReadFileIdentity(t, pending, final)
		v.requireTerminal(t, final, true)
		v.requireWire(t, 1, final)
		// Public Lookup is a new current-authorized call and must begin before
		// Stop. It observes only this original unresolved publication/result.
		if authority.Joined() {
			t.Fatal("Unknown original publication reported a premature join")
		}
		for n := 0; n < 2; n++ {
			out, found, err := executor.LookupReadFile(ctxFor(t), execution)
			readFileAdapterOK(t, err, "original durable read-file lookup")
			phase, outcome, attempted := out.Status()
			result, hasResult := out.Result()
			if !found || out.OperationID().String() != final.operation || out.AttemptID().String() != final.attempt || string(out.RequestID()) != final.request || phase != "succeeded" || outcome != "success" || !attempted || !hasResult || result != v.expected {
				t.Fatal("lookup did not return the complete original successful result")
			}
			after := v.sql(t)
			requireOperationReadFileIdentity(t, final, after)
			v.requireTerminal(t, after, true)
			v.requireWire(t, 1, after)
		}
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := authority.Drain(ctx); err != nil || !authority.Joined() {
			t.Fatal("original Unknown owner did not join after exact durable observation")
		}
		v.requireJoined(t, final, true)
		t.Log("read_file_operation full_result=65536 wire_requests=1 original_commit_then_unknown=true repeated_lookup_no_send=true")
	})
	t.Run("current-mount-clear-rejects-unclaimed", func(t *testing.T) {
		v := newOperationReadFileFixture(t, false)
		executor, authority, store := v.executor(t, false)
		execution := v.facts.capture.created.ID
		done := startOperationReadFile(t, executor, execution, store)
		pending := v.sql(t)
		if pending.phase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
			t.Fatal("old unclaimed ticket was not held after actual Attempt commit")
		}
		v.requireWire(t, 0, pending)
		b := v.facts.capture.v.base
		current, err := v.facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.facts.capture.v.agentID)
		readFileAdapterOK(t, err, "actual Owner current Agent read")
		before := current.Fields().Core.Version
		empty := []ac.RunnerMountDeclaration{}
		change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &empty})
		readFileAdapterOK(t, err, "formal empty Runner declarations")
		cleared, err := v.facts.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "read-file-operation-clear", &before), b.project.ID, v.facts.capture.v.agentID, change)
		readFileAdapterOK(t, err, "formal Owner Agent Mount clear")
		if cleared.Validate() != nil || cleared.Fields().Agent.Fields().Core.Version != before+1 || len(cleared.Fields().Agent.Fields().AllowedMountIDs) != 0 || cleared.Fields().RunnerMounts == nil || len(*cleared.Fields().RunnerMounts) != 0 {
			t.Fatal("formal Agent clear did not commit its exact empty configuration")
		}
		v.requireWire(t, 0, pending)
		store.resume()
		completed := waitOperationReadFile(t, done)
		readFileAdapterOK(t, completed.err, "original current-denied call retirement")
		phase, outcome, attempted := completed.result.Status()
		_, hasResult := completed.result.Result()
		if phase != "failed" || outcome != "failure" || attempted || hasResult || completed.result.OperationID().String() != pending.operation || completed.result.AttemptID().String() != pending.attempt || string(completed.result.RequestID()) != pending.request {
			t.Fatal("old ticket was dispatched or its original identity discarded after clear")
		}
		final := v.sql(t)
		requireOperationReadFileIdentity(t, pending, final)
		v.requireTerminal(t, final, false)
		v.requireWire(t, 0, final)
		// A fresh business call rechecks current permission; no second attempt,
		// replacement RID or old current-data lookup is permitted after clear.
		_, err = executor.ExecuteReadFile(ctxFor(t), execution)
		requireCode(t, err, f.Forbidden)
		_, found, err := executor.LookupReadFile(ctxFor(t), execution)
		requireCode(t, err, f.Forbidden)
		if found {
			t.Fatal("denied current lookup exposed an old result")
		}
		after := v.sql(t)
		requireOperationReadFileIdentity(t, final, after)
		v.requireTerminal(t, after, false)
		v.requireWire(t, 0, after)
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := authority.Drain(ctx); err != nil || !authority.Joined() {
			t.Fatal("original unsent call did not actually join")
		}
		v.requireJoined(t, after, false)
		t.Log("read_file_operation current_mount_clear=true unclaimed_ticket_denied=true wire_requests=0 original_identities_retained=true")
	})
}
