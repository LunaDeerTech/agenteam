//go:build integration && linux

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5/pgconn"
)

// Only the Model JSON result and original physical transaction-return timing
// are controlled. Command execution, pipes, WSS and canonical facts are real.
type commandResultConsumptionFixture struct {
	operation     *operationCommandFixture
	terminal      operationCommandSQL
	expected      p.RunCommandResult
	bundle        []byte
	outcome, code string
	before        toolResultConsumptionState
	diagnostic    writeResultDiagnostic
}

const (
	commandResultStdout = "stdout ready α\n\tquote=\" slash=\\ nul=\x00\n"
	commandResultStderr = "stderr ready β\r\n\tquote=\" slash=\\ nul=\x00\n"
)

// Synchronous observation of the original TLS connection read return. This is
// transport-read readiness, not dispatcher acceptance; terminal/SQL assertions
// separately require every byte to survive the real dispatcher and final commit.
type commandResultReadiness struct {
	mu                 sync.Mutex
	ready              chan struct{}
	request, operation p.ID
	stdout, stderr     []byte
	sequences          map[string]uint64
	invalid, signalled bool
}

func newCommandResultReadiness() *commandResultReadiness {
	return &commandResultReadiness{ready: make(chan struct{}), sequences: make(map[string]uint64)}
}
func (o *commandResultReadiness) bind(request, operation p.ID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.request != "" || !request.Valid() || !operation.Valid() {
		o.invalid = true
		return
	}
	o.request, o.operation = request, operation
}
func (o *commandResultReadiness) reject() { o.mu.Lock(); o.invalid = true; o.mu.Unlock() }
func (o *commandResultReadiness) complete() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.invalid && o.signalled && bytes.Equal(o.stdout, []byte(commandResultStdout)) && bytes.Equal(o.stderr, []byte(commandResultStderr))
}
func (o *commandResultReadiness) message(raw []byte) {
	message, err := p.Decode(raw)
	if err != nil || message.AllowedFrom(p.Runner) != nil {
		o.reject()
		return
	}
	stream, ok := message.Payload().(p.Stream)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := strconv.ParseUint(string(stream.Sequence), 10, 64)
	if err != nil || stream.RequestID != o.request || stream.OperationID != o.operation || n != o.sequences[stream.Stream]+1 {
		o.invalid = true
		return
	}
	var dst *[]byte
	var want string
	switch stream.Stream {
	case "stdout":
		dst, want = &o.stdout, commandResultStdout
	case "stderr":
		dst, want = &o.stderr, commandResultStderr
	default:
		o.invalid = true
		return
	}
	if len(*dst)+len(stream.Data) > len(want) {
		o.invalid = true
		return
	}
	*dst = append(*dst, stream.Data...)
	o.sequences[stream.Stream] = n
	if !bytes.HasPrefix([]byte(want), *dst) {
		o.invalid = true
		return
	}
	if !o.signalled && bytes.Equal(o.stdout, []byte(commandResultStdout)) && bytes.Equal(o.stderr, []byte(commandResultStderr)) {
		o.signalled = true
		close(o.ready)
	}
}
func (o *commandResultReadiness) wrap(actual http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual.ServeHTTP(&commandResultResponse{ResponseWriter: w, observer: o}, r)
	})
}

type commandResultResponse struct {
	http.ResponseWriter
	observer *commandResultReadiness
}

func (w *commandResultResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *commandResultResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffered, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return conn, buffered, err
	}
	if buffered == nil || buffered.Reader == nil || buffered.Writer == nil || buffered.Writer.Buffered() != 0 {
		w.observer.reject()
		return conn, buffered, nil
	}
	// The original reader retains any bytes buffered by net/http and its
	// original conn. No direct read bypasses it; the returned reader uses the
	// same wrapper for both buffered and subsequent bytes exactly once.
	observed := &commandResultConnection{Conn: conn, source: buffered.Reader, observer: w.observer}
	return observed, bufio.NewReadWriter(bufio.NewReader(observed), buffered.Writer), nil
}

type commandResultConnection struct {
	net.Conn
	source            *bufio.Reader
	observer          *commandResultReadiness
	buffer, message   []byte
	fragment, invalid bool
}

func (c *commandResultConnection) Read(raw []byte) (int, error) {
	n, err := c.source.Read(raw)
	if n > 0 {
		c.consume(raw[:n])
	}
	return n, err
}
func (c *commandResultConnection) reject() {
	c.invalid = true
	clear(c.buffer)
	clear(c.message)
	c.buffer = nil
	c.message = nil
	c.observer.reject()
}
func (c *commandResultConnection) consume(raw []byte) {
	if c.invalid {
		return
	}
	if len(c.buffer)+len(raw) > 2*p.MaxMessageBytes {
		c.reject()
		return
	}
	c.buffer = append(c.buffer, raw...)
	for len(c.buffer) >= 2 {
		first, second := c.buffer[0], c.buffer[1]
		final, opcode := first&128 != 0, first&15
		if first&112 != 0 || second&128 == 0 {
			c.reject()
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
		if len(c.buffer) < header+4 || uint64(len(c.buffer)-header-4) < length {
			return
		}
		mask := c.buffer[header : header+4]
		header += 4
		payload := make([]byte, int(length))
		for n := range payload {
			payload[n] = c.buffer[header+n] ^ mask[n%4]
		}
		switch opcode {
		case 1:
			if c.fragment {
				clear(payload)
				c.reject()
				return
			}
			c.message = append(c.message[:0], payload...)
			c.fragment = !final
		case 0:
			if !c.fragment || len(c.message)+len(payload) > p.MaxMessageBytes {
				clear(payload)
				c.reject()
				return
			}
			c.message = append(c.message, payload...)
			c.fragment = !final
		case 8, 9, 10:
		default:
			clear(payload)
			c.reject()
			return
		}
		clear(payload)
		c.buffer = c.buffer[header+int(length):]
		if opcode < 8 && final {
			c.observer.message(c.message)
			clear(c.message)
			c.message = c.message[:0]
		}
	}
}

// Only the real Runner selects this helper. It is not a behaviour top and its
// default invocation skips. Cancel is delivered to the actual process group.
func TestRunCommandResultConsumptionChild(t *testing.T) {
	at := slices.Index(os.Args, "--")
	if at < 0 || at+1 >= len(os.Args) || os.Args[at+1] != "run-command-result-child" {
		t.Skip("explicit owned result-consumption child only")
	}
	if len(os.Args) != at+3 || (os.Args[at+2] != "result-consumption-exit23-child" && os.Args[at+2] != "result-consumption-cancel-child") {
		os.Exit(91)
	}
	if n, err := os.Stdout.WriteString(commandResultStdout); err != nil || n != len(commandResultStdout) {
		os.Exit(92)
	}
	if n, err := os.Stderr.WriteString(commandResultStderr); err != nil || n != len(commandResultStderr) {
		os.Exit(93)
	}
	if os.Args[at+2] == "result-consumption-exit23-child" {
		os.Exit(23)
	}
	for {
		// No timer chooses the cancel point. The test waits for the original
		// WSS streams; the child remains alive until the Runner kills it.
		_ = syscall.Pause()
	}
}

func startCommandResultOperation(t *testing.T, executor *toolruntime.RunCommandExecutor, executionID i.ExecutionID, store *operationCommandStore) (<-chan operationCommandCompletion, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan operationCommandCompletion, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := executor.ExecuteRunCommand(ctx, executionID)
		done <- operationCommandCompletion{out, err}
	}()
	t.Cleanup(func() {
		store.resume()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("original cancellable command Execute did not actually return")
		}
	})
	select {
	case <-store.entered:
	case <-done:
		t.Fatal("command returned before its real Attempt COMMIT return barrier")
	case <-ctx.Done():
		t.Fatal("command did not reach its original Attempt return barrier")
	}
	return done, cancel
}

func newCommandResultConsumptionFixture(t *testing.T, cancelled bool) *commandResultConsumptionFixture {
	t.Helper()
	mode := "result-consumption-exit23-child"
	if cancelled {
		mode = "result-consumption-cancel-child"
	}
	ready := newCommandResultReadiness()
	v := newOperationCommandFixtureWithChild(t, "TestRunCommandResultConsumptionChild", []string{"run-command-result-child", mode}, ready.wrap)
	v.expectedRequests = 1
	executor, authority, store := v.executor(t, false, false)
	done, cancel := startCommandResultOperation(t, executor, v.facts.capture.created.ID, store)
	pending := v.sql(t)
	if pending.phase != "running" || pending.attemptPhase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
		t.Fatal("real command Attempt was not committed before the original return")
	}
	v.requireWire(t, 0, pending)
	ready.bind(p.ID(pending.request), p.ID(pending.operation))
	store.resume()
	if cancelled {
		select {
		case <-ready.ready:
			if !ready.complete() {
				t.Fatal("original command streams did not establish readiness")
			}
		case <-done:
			t.Fatal("command returned before both real WSS streams were received")
		case <-time.After(10 * time.Second):
			t.Fatal("original command WSS readiness did not arrive")
		}
		cancel() // Only this Execute context. Driver and canonical E remain live.
	}
	completed := waitOperationCommand(t, done)
	readFileAdapterOK(t, completed.err, "actual command final report return")
	terminal := v.sql(t)
	requireOperationCommandIdentity(t, pending, terminal)
	v.requireWire(t, 1, terminal)
	if !ready.complete() {
		t.Fatal("actual complete WSS stdout/stderr were not observed")
	}
	phase, outcome, attempted := completed.result.Status()
	report, stdout, stderr, available := completed.result.Result()
	defer clear(stdout)
	defer clear(stderr)
	x := &commandResultConsumptionFixture{operation: v, terminal: terminal, expected: report, outcome: outcome, code: completed.result.Code()}
	x.requireReport(t, report, stdout, stderr)
	if !attempted || !available || completed.result.OperationID().String() != terminal.operation || completed.result.AttemptID().String() != terminal.attempt || string(completed.result.RequestID()) != terminal.request || phase != terminal.phase {
		t.Fatal("command return lost its known report or original identities")
	}
	if cancelled {
		if phase != "cancelled" || outcome != "cancelled" || x.code != string(p.CancelledCode) || !report.Cancelled || report.TimedOut || report.CompletionCode == nil || *report.CompletionCode != p.CancelledCode || report.Signal <= 0 || report.ExitCode != -1 {
			t.Fatal("actual cancellation was not retained as a known cancelled report")
		}
	} else if phase != "failed" || outcome != "failure" || x.code != "command_exit_nonzero" || report.ExitCode != 23 || report.Signal != 0 || report.Cancelled || report.TimedOut || report.CompletionCode != nil {
		t.Fatal("actual exit23 was not retained as a known Tool error")
	}
	// Construct the expected canonical bundle independently of the reader.
	raw, err := p.EncodeRunCommandResult(report)
	readFileAdapterOK(t, err, "original command report codec")
	defer clear(raw)
	encoded, err := json.Marshal(map[string]any{"report": json.RawMessage(raw), "stdout": string(stdout), "stderr": string(stderr)})
	readFileAdapterOK(t, err, "independent complete command bundle")
	defer clear(encoded)
	x.bundle, err = cursor.CanonicalJSON(encoded)
	readFileAdapterOK(t, err, "independent canonical command bundle")
	t.Cleanup(func() { clear(x.bundle) })
	t.Cleanup(func() {
		if !ready.complete() {
			t.Error("original wire-read stream observation became invalid")
		}
	})
	x.requireTerminal(t, terminal)
	authority.Stop()
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if authority.Drain(ctx) != nil || !authority.Joined() {
		t.Fatal("original command dispatch owner did not actually join")
	}
	x.before, err = x.read(ctxFor(t), v.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent live unconsumed command checkpoint")
	x.requireState(t, x.before, false)
	tracked := v.facts.capture.v.base.tracked
	tracked.mu.Lock()
	tracked.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if x.matches(cause) {
			x.diagnostic.final(result)
		}
	}
	tracked.mu.Unlock()
	t.Cleanup(func() { roundInputClearHooks(tracked) })
	t.Cleanup(func() {
		v.facts.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if v.facts.driver.Drain(ctx) != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
			t.Error("original command ToolCall/Model did not actually join")
		}
		x.requireOriginalWork(t)
	})
	return x
}

func (v *commandResultConsumptionFixture) requireReport(t *testing.T, report p.RunCommandResult, stdout, stderr []byte) {
	t.Helper()
	if report.Validate() != nil || report.State != "completed" || report.StdoutTruncated || report.StderrTruncated || !report.OutputComplete || !report.ProcessGroupTerminationConfirmed || string(report.StdoutBytes) != fmt.Sprint(len(commandResultStdout)) || string(report.StderrBytes) != fmt.Sprint(len(commandResultStderr)) || !bytes.Equal(stdout, []byte(commandResultStdout)) || !bytes.Equal(stderr, []byte(commandResultStderr)) {
		t.Fatal("complete original command report or stream bytes changed")
	}
}

func (v *commandResultConsumptionFixture) requireTerminal(t *testing.T, row operationCommandSQL) {
	t.Helper()
	var terminal struct {
		Format                                     int
		Operation, Attempt, Request, Outcome, Code string
		Attempted                                  bool
		ReportAvailable                            bool `json:"report_available"`
		Result                                     *struct {
			Operation, Attempt, Request, SHA256, StdoutSHA256, StderrSHA256 string
			Bytes, StdoutBytes, StderrBytes                                 f.Progress
		}
	}
	if json.Unmarshal(row.terminal, &terminal) != nil || !bytes.Equal(row.terminal, row.attemptOutcome) || terminal.Format != 1 || terminal.Operation != row.operation || terminal.Attempt != row.attempt || terminal.Request != row.request || terminal.Outcome != v.outcome || terminal.Code != v.code || !terminal.Attempted || !terminal.ReportAvailable || terminal.Result == nil || !row.retired || row.version != 3 || row.results != 1 || row.resultRequest != row.request || row.attemptPhase != row.phase {
		t.Fatal("original retired command report identity or classification changed")
	}
	if (v.outcome == "failure" && (row.phase != "failed" || row.outcome != "error")) || (v.outcome == "cancelled" && (row.phase != "cancelled" || row.outcome != "cancelled")) {
		t.Fatal("known command error/cancellation changed its stored phases")
	}
	report, err := p.DecodeRunCommandResult(row.report)
	readFileAdapterOK(t, err, "original stored command report")
	v.requireReport(t, report, row.stdout, row.stderr)
	if !reflect.DeepEqual(report, v.expected) || row.reportDigest != toolResultConsumptionDigest(row.report) || row.stdoutDigest != toolResultConsumptionDigest(row.stdout) || row.stderrDigest != toolResultConsumptionDigest(row.stderr) || row.reportBytes != int64(len(row.report)) || row.stdoutBytes != int64(len(row.stdout)) || row.stderrBytes != int64(len(row.stderr)) {
		t.Fatal("stored command report or stream references changed")
	}
	ref := terminal.Result
	if ref.Operation != row.operation || ref.Attempt != row.attempt || ref.Request != row.request || ref.SHA256 != row.reportDigest || int64(ref.Bytes) != row.reportBytes || ref.StdoutSHA256 != row.stdoutDigest || int64(ref.StdoutBytes) != row.stdoutBytes || ref.StderrSHA256 != row.stderrDigest || int64(ref.StderrBytes) != row.stderrBytes {
		t.Fatal("original terminal did not bind its complete report and streams")
	}
	var joined bool
	err = v.operation.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.xmin=a.xmin AND a.xmin=r.xmin FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.request_id=a.backend_request_id WHERE o.operation_id=$1 AND o.operation_kind='runner.run-command'`, row.operation).Scan(&joined)
	readFileAdapterOK(t, err, "original command terminal transaction")
	if !joined {
		t.Fatal("command report and retirement were not in the original final transaction")
	}
}

func (v *commandResultConsumptionFixture) consumer(t *testing.T) *execution.ToolResultConsumer {
	t.Helper()
	facts := v.operation.facts
	history := &commandResultObservedHistory{actual: facts.driver, diagnostic: &v.diagnostic}
	reader, err := toolruntime.NewRunCommandResultReader(facts.capture.v.base.tracked, history)
	readFileAdapterOK(t, err, "real historical command result reader")
	consumer, err := execution.NewRunCommandResultConsumer(facts.driver, &commandResultObservedReader{actual: reader, diagnostic: &v.diagnostic})
	readFileAdapterOK(t, err, "explicit command result consumer")
	t.Cleanup(func() { stopToolResultConsumer(t, consumer) })
	return consumer
}

// These are explicitly rollback-only SQL negative controls, not canonical
// producers. Their candidate comes from the actual historical reader and Loop
// projection. No existing consumption/transcript row is modified or unsealed.
func (v *commandResultConsumptionFixture) rejectInvalidSQLCandidates(t *testing.T) {
	t.Helper()
	facts, store := v.operation.facts, v.operation.facts.capture.v.base.tracked
	reader, err := toolruntime.NewRunCommandResultReader(store, facts.driver)
	readFileAdapterOK(t, err, "actual command reader for SQL rejection candidate")
	plan, err := reader.DiscoverRunCommandResult(ctxFor(t), facts.capture.created.ID)
	readFileAdapterOK(t, err, "actual committed command result discovery")
	cause, err := f.NewRecoveryCause("command-result.sql-negative", v.terminal.execution, v.terminal.attempt)
	readFileAdapterOK(t, err, "explicit SQL negative cause")
	var projected agentloop.ToolResultProjection
	loans := 0
	observed := store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		return reader.WithRunCommandResultInTx(ctx, tx, plan, func(ctx context.Context, binding tc.RunCommandResultBinding, raw []byte) error {
			loans++
			expected, actual := plan.Binding(), binding
			actor, expectedActor := binding.Call.Actor.Details(), expected.Call.Actor.Details()
			actual.Call.Actor, expected.Call.Actor = i.Actor{}, i.Actor{}
			if !reflect.DeepEqual(actor, expectedActor) || !reflect.DeepEqual(actual, expected) || !bytes.Equal(raw, v.bundle) {
				return errors.New("SQL candidate changed the actual reader result")
			}
			var err error
			projected, err = agentloop.ProjectRunCommandToolResult(ctx, binding, raw)
			return err
		})
	})
	if observed.State() != f.Committed || loans != 1 || projected.Validate() != nil {
		t.Fatal("original historical SQL candidate read did not actually return")
	}
	rollback := errors.New("command result SQL candidate rollback")
	for _, kind := range []string{"valid-pair", "wrong-is-error", "unknown-report", "old-profile-budget"} {
		var binding map[string]any
		readFileAdapterOK(t, json.Unmarshal(v.before.expectedBinding, &binding), "independent SQL binding candidate")
		message := projected.Message()
		resultBytes := int64(len(v.bundle))
		resultDigest := toolResultConsumptionDigest(v.bundle)
		constraint := "execution_tool_result_pair"
		switch kind {
		case "wrong-is-error":
			message.Parts[0].ToolResult.IsError = false
		case "unknown-report":
			binding["outcome"], binding["code"] = "unknown", ""
		case "old-profile-budget":
			// Only the immediate byte-budget CHECK is under test here; this is
			// not a successful read-file producer or deferred identity proof.
			binding["operation_name"] = "read-file"
			resultBytes = 524289
			binding["result_bytes"] = resultBytes
			message.Parts[0].ToolResult.Parts[0].Text.Text = strings.Repeat("x", int(resultBytes))
			resultDigest = toolResultConsumptionDigest([]byte(message.Parts[0].ToolResult.Parts[0].Text.Text))
			binding["result_digest"] = resultDigest
			constraint = "execution_tool_result_bytes"
		}
		rawBinding, err := json.Marshal(binding)
		readFileAdapterOK(t, err, "negative SQL binding encoding")
		rawBinding, err = cursor.CanonicalJSON(rawBinding)
		readFileAdapterOK(t, err, "negative SQL binding canonical form")
		rawMessage, err := json.Marshal(message)
		readFileAdapterOK(t, err, "negative SQL message encoding")
		rawMessage, err = cursor.CanonicalJSON(rawMessage)
		readFileAdapterOK(t, err, "negative SQL message canonical form")
		reachedInsert, reachedDeferred := false, false
		b := plan.Binding()
		physical := facts.capture.v.base.raw.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := facts.capture.v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return err
			}
			x, err := facts.capture.v.base.raw.InTx(tx)
			if err != nil {
				return err
			}
			var at time.Time
			if err = x.QueryRow(ctx, `SELECT GREATEST(clock_timestamp(),created_at) FROM agenteam_execution.tool_calls WHERE execution_id=$1 AND round_id=$2`, v.terminal.execution, b.Call.RoundID).Scan(&at); err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`, v.terminal.execution, v.before.version, at)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("SQL candidate did not retain its one live checkpoint")
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_execution.transcript_entries(execution_id,sequence,round_id,kind,message,digest,created_at) VALUES($1,3,$2,'tool_result',$3,$4,$5)`, v.terminal.execution, b.Call.RoundID, rawMessage, toolResultConsumptionDigest(rawMessage), at)
			if err != nil {
				return err
			}
			reachedInsert = true
			_, err = x.Exec(ctx, `INSERT INTO agenteam_execution.tool_result_consumptions(execution_id,round_id,invocation_id,call_id,operation_id,attempt_id,request_id,input_digest,fingerprint,terminal_digest,result_digest,result_bytes,binding,binding_digest,message_digest,transcript_sequence,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,3,$16,$17)`, v.terminal.execution, b.Call.RoundID, b.Call.InvocationID.String(), b.Call.CallID, v.terminal.operation, v.terminal.attempt, v.terminal.request, v.terminal.inputDigest, v.terminal.fingerprint, toolResultConsumptionDigest(v.terminal.terminal), resultDigest, resultBytes, rawBinding, toolResultConsumptionDigest(rawBinding), toolResultConsumptionDigest(rawMessage), v.before.version+1, at)
			if err != nil {
				return err
			}
			reachedDeferred = true
			_, err = x.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
			if err != nil {
				return err
			}
			return rollback
		})
		clear(rawBinding)
		clear(rawMessage)
		var pg *pgconn.PgError
		if kind == "valid-pair" {
			if physical.State() != f.NotCommitted || !reachedInsert || !reachedDeferred || !errors.Is(physical.Fault(), rollback) {
				t.Fatal("valid original SQL candidate did not pass exact constraints before actual rollback")
			}
		} else if physical.State() != f.NotCommitted || !reachedInsert || (kind != "old-profile-budget" && !reachedDeferred) || (kind == "old-profile-budget" && reachedDeferred) || !errors.As(physical.Fault(), &pg) || pg.Code != "23514" || pg.ConstraintName != constraint {
			t.Fatal("invalid command SQL candidate did not hit its exact rollback-only constraint")
		}
		if !reflect.DeepEqual(v.requireDurable(t, false), v.before) {
			t.Fatal("negative SQL transaction left partial canonical facts")
		}
		v.requireOriginalWork(t)
	}
	t.Log("command_result_sql valid_rollback_transactions=1 negative_transactions=3 actual_rollback=true exact_constraints=true sealed_rows_unchanged=true")
}

type commandResultObservedHistory struct {
	actual     tc.CommittedRunCommandCallAuthority
	diagnostic *writeResultDiagnostic
}

func (o *commandResultObservedHistory) DiscoverCommittedRunCommandCall(ctx context.Context, e i.ExecutionID) (tc.CommittedRunCommandCallPlan, error) {
	started := time.Now()
	plan, err := o.actual.DiscoverCommittedRunCommandCall(ctx, e)
	o.diagnostic.stage("history_discover_return", ctx, started, err, 0, nil, nil)
	return plan, err
}

func (o *commandResultObservedHistory) WithCommittedRunCommandCallInTx(ctx context.Context, tx f.Tx, plan tc.CommittedRunCommandCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	started := time.Now()
	callbacks := 0
	var callbackErr error
	var callbackCtx context.Context
	err := o.actual.WithCommittedRunCommandCallInTx(ctx, tx, plan, func(inner context.Context, binding tc.ToolCallBinding, raw []byte) error {
		callbacks++
		callbackCtx = inner
		callbackErr = consume(inner, binding, raw)
		return callbackErr
	})
	o.diagnostic.stage("history_loan_return", ctx, started, err, callbacks, callbackErr, callbackCtx)
	return err
}

type commandResultObservedReader struct {
	actual     tc.RunCommandResultAuthority
	diagnostic *writeResultDiagnostic
}

func (o *commandResultObservedReader) DiscoverRunCommandResult(ctx context.Context, e i.ExecutionID) (tc.RunCommandResultPlan, error) {
	started := time.Now()
	plan, err := o.actual.DiscoverRunCommandResult(ctx, e)
	o.diagnostic.stage("result_discover_return", ctx, started, err, 0, nil, nil)
	return plan, err
}

func (o *commandResultObservedReader) WithRunCommandResultInTx(ctx context.Context, tx f.Tx, plan tc.RunCommandResultPlan, consume func(context.Context, tc.RunCommandResultBinding, []byte) error) error {
	started := time.Now()
	o.diagnostic.mu.Lock()
	o.diagnostic.finalLoans++
	o.diagnostic.mu.Unlock()
	callbacks := 0
	var callbackErr error
	var callbackCtx context.Context
	err := o.actual.WithRunCommandResultInTx(ctx, tx, plan, func(inner context.Context, binding tc.RunCommandResultBinding, raw []byte) error {
		callbacks++
		callbackCtx = inner
		callbackErr = consume(inner, binding, raw)
		return callbackErr
	})
	o.diagnostic.stage("result_loan_return", ctx, started, err, callbacks, callbackErr, callbackCtx)
	return err
}

func (v *commandResultConsumptionFixture) revokeDispatch(t *testing.T) {
	t.Helper()
	facts, b := v.operation.facts, v.operation.facts.capture.v.base
	current, err := facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, facts.capture.v.agentID)
	readFileAdapterOK(t, err, "ordinary Owner current configuration before revocation")
	version := current.Fields().Core.Version
	tools, mounts := []i.ToolID{}, []ac.RunnerMountDeclaration{}
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &tools, RunnerMounts: &mounts})
	readFileAdapterOK(t, err, "formal Tool and Mount revocation input")
	changed, err := facts.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "command-result-revoke-dispatch", &version), b.project.ID, facts.capture.v.agentID, change)
	readFileAdapterOK(t, err, "ordinary Owner committed Tool and Mount revocation")
	if changed.Validate() != nil || changed.Fields().Agent.Fields().Core.Version != version+1 || len(changed.Fields().Agent.Fields().AllowedToolIDs) != 0 || len(changed.Fields().Agent.Fields().AllowedMountIDs) != 0 || changed.Fields().RunnerMounts == nil || len(*changed.Fields().RunnerMounts) != 0 {
		t.Fatal("formal revocation left dispatch authorization")
	}
	actual, err := facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, facts.capture.v.agentID)
	readFileAdapterOK(t, err, "independent current Owner read after revocation")
	if actual.Fields().Core.Version != version+1 || len(actual.Fields().AllowedToolIDs) != 0 || len(actual.Fields().AllowedMountIDs) != 0 {
		t.Fatal("current configuration did not retain the committed revocation")
	}
	v.requireOriginalWork(t)
}

func (v *commandResultConsumptionFixture) requireOriginalWork(t *testing.T) {
	t.Helper()
	actual := v.operation.sql(t)
	if !reflect.DeepEqual(actual, v.terminal) {
		t.Fatal("receipt consumption changed original Operation/Attempt/result facts")
	}
	v.requireTerminal(t, actual)
	v.operation.requireWire(t, 1, actual)
	_, _, begin, result, closes, joined := v.operation.facts.model.observed()
	if begin != 1 || result != 1 || closes != 1 || !joined {
		t.Fatal("new consumption or projection repeated Model work")
	}
}

func (v *commandResultConsumptionFixture) matches(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.JobCause && d.JobType == "execution.tool-result.consume" && d.JobID == v.terminal.execution && d.JobAttemptID == v.terminal.attempt
}

// The shared state DTO carries safe counters and in-memory canonical bytes.
// Its SQL here is independent of the old read-only fixture and original
// ToolCall checkpoint-version join, which is no longer true after consumption.
func (v *commandResultConsumptionFixture) read(ctx context.Context, x postgres.SQLExecutor) (toolResultConsumptionState, error) {
	var out toolResultConsumptionState
	err := x.QueryRow(ctx, `SELECT e.status,e.version,r.id,c.call_id,r.tool_pending,r.transcript_through,
 (e.cancel_requested_at IS NULL),(r.terminal_status IS NULL),
 (SELECT count(*) FROM agenteam_execution.rounds z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_calls z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_call_arguments z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_result_consumptions z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events z WHERE z.aggregate_id=e.id::uuid AND z.event_type IN('execution.succeeded','execution.failed','execution.cancelled')),
 (SELECT count(*) FROM agenteam_mount.execution_use_retirements z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings z JOIN agenteam_secret.secret_leases l ON l.id=z.lease_id WHERE z.owner_kind='execution' AND z.owner_id::text=e.id AND NOT l.released),
 (SELECT count(*) FROM agenteam_projectvariable.execution_secret_references z WHERE z.execution_id=e.id),
 row_to_json(s)::text,row_to_json(r)::text,row_to_json(c)::text,row_to_json(a)::text,
 (SELECT json_agg(z ORDER BY z.sequence)::text FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id AND z.sequence IN(1,2)),
 COALESCE(k.operation_id,''),COALESCE(k.attempt_id,''),COALESCE(k.request_id,''),
 COALESCE(k.input_digest,''),COALESCE(k.fingerprint,''),COALESCE(k.terminal_digest,''),COALESCE(k.result_digest,''),COALESCE(k.result_bytes,0),
 COALESCE(k.binding,''::bytea),COALESCE(k.binding_digest,''),COALESCE(k.message_digest,''),COALESCE(k.transcript_sequence,0),COALESCE(k.checkpoint_version,0),COALESCE(k.created_at,TIMESTAMPTZ 'epoch'),
 COALESCE(t.message,''::bytea),COALESCE(t.kind,''),COALESCE(e.xmin=k.xmin AND k.xmin=t.xmin AND k.created_at=t.created_at AND k.message_digest=t.digest
  AND k.round_id=r.id AND k.invocation_id=c.invocation_id AND k.call_id=c.call_id AND t.round_id=r.id,false),
 json_build_object(
 'project_id',e.project_id,'agent_id',e.agent_id,'execution_id',e.id,'round_id',r.id,'snapshot_id',s.id,'input_binding_id',r.input_binding_id,
 'logical_call_id',r.call_id,'invocation_id',c.invocation_id,'call_id',c.call_id,'model_visible_name',c.model_visible_name,'tool_id',c.tool_id,'spec_revision',c.spec_revision,
 'operation_name',c.operation_name,'operation_revision',c.operation_revision,'protocol_major',c.protocol_major,
 'payload_id',a.id,'payload_digest',a.original_digest,'payload_bytes',octet_length(a.original),'canonical_arguments',a.canonical_digest,
 'operation_id',o.operation_id,'attempt_id',u.attempt_id,'request_id',u.backend_request_id,'input_digest',o.input_digest,'fingerprint',o.fingerprint,
 'terminal_digest','sha256:'||encode(sha256(o.terminal_data),'hex'),'outcome',convert_from(o.terminal_data,'UTF8')::jsonb->>'Outcome','code',convert_from(o.terminal_data,'UTF8')::jsonb->>'Code','attempted',true,'result_digest',$2::text,'result_bytes',$3::bigint)::text
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id)=(e.id,s.id)
 JOIN agenteam_execution.tool_calls c ON(c.execution_id,c.round_id)=(e.id,r.id)
 JOIN agenteam_execution.tool_call_arguments a ON(a.execution_id,a.round_id,a.id)=(e.id,r.id,c.payload_id)
 JOIN agenteam_tool.operations o ON o.execution_id=e.id AND o.operation_kind='runner.run-command'
 JOIN agenteam_tool.attempts u ON(u.operation_id,u.attempt_id)=(o.operation_id,o.active_attempt_id)
 JOIN agenteam_tool.command_results z ON(z.operation_id,z.attempt_id)=(o.operation_id,u.attempt_id)
 LEFT JOIN agenteam_execution.tool_result_consumptions k ON k.execution_id=e.id
 LEFT JOIN agenteam_execution.transcript_entries t ON t.execution_id=e.id AND t.sequence=3
 WHERE e.id=$1`, v.terminal.execution, toolResultConsumptionDigest(v.bundle), len(v.bundle)).Scan(
		&out.status, &out.version, &out.round, &out.call, &out.pending, &out.through, &out.live, &out.roundOpen,
		&out.rounds, &out.calls, &out.arguments, &out.transcripts, &out.consumptions, &out.terminals, &out.retirements, &out.modelLeases, &out.environmentLeases,
		&out.immutable[0], &out.immutable[1], &out.immutable[2], &out.immutable[3], &out.immutable[4],
		&out.operation, &out.attempt, &out.request, &out.inputDigest, &out.fingerprint, &out.terminalDigest, &out.resultDigest, &out.resultBytes,
		&out.binding, &out.bindingDigest, &out.messageDigest, &out.sequence, &out.checkpoint, &out.created, &out.message, &out.kind, &out.sameTx, &out.expectedBinding)
	return out, err
}

func (v *commandResultConsumptionFixture) validateState(got toolResultConsumptionState, consumed bool) error {
	beforeVersion := int64(v.operation.facts.capture.created.Version) + 3
	if got.status != "running" || !got.live || !got.roundOpen || !got.pending || got.through != 2 || got.rounds != 1 || got.calls != 1 || got.arguments != 1 || got.terminals != 0 || got.retirements != 0 || got.modelLeases != 1 || got.environmentLeases != 0 {
		return errors.New("result checkpoint changed the pending Round, current execution or live references")
	}
	if v.before.round != "" && (got.round != v.before.round || got.call != v.before.call || got.immutable != v.before.immutable || !bytes.Equal(got.expectedBinding, v.before.expectedBinding)) {
		return errors.New("result consumption changed original Snapshot, Round, ToolCall, protected arguments or earlier transcript")
	}
	if !consumed {
		if got.version != beforeVersion || got.transcripts != 2 || got.consumptions != 0 || got.operation != "" || got.attempt != "" || got.sequence != 0 || got.checkpoint != 0 || len(got.binding) != 0 || len(got.message) != 0 || got.sameTx {
			return errors.New("unconsumed or rolled-back checkpoint contains partial facts")
		}
		return nil
	}
	if got.version != beforeVersion+1 || got.checkpoint != got.version || got.sequence != 3 || got.transcripts != 3 || got.consumptions != 1 || got.kind != "tool_result" || !got.sameTx || got.operation != v.terminal.operation || got.attempt != v.terminal.attempt || got.request != v.terminal.request || got.inputDigest != v.terminal.inputDigest || got.fingerprint != v.terminal.fingerprint || got.terminalDigest != toolResultConsumptionDigest(v.terminal.terminal) || got.resultDigest != toolResultConsumptionDigest(v.bundle) || got.resultBytes != int64(len(v.bundle)) || got.bindingDigest != toolResultConsumptionDigest(got.binding) || got.messageDigest != toolResultConsumptionDigest(got.message) {
		return errors.New("consumption did not bind one original result, transcript and Execution version in the same transaction")
	}
	var binding, expected map[string]any
	if json.Unmarshal(got.binding, &binding) != nil || json.Unmarshal(got.expectedBinding, &expected) != nil || !reflect.DeepEqual(binding, expected) {
		return errors.New("consumption binding differs from original canonical Call, Operation, Attempt or payload facts")
	}
	canonical, err := cursor.CanonicalJSON(got.message)
	if err != nil || !bytes.Equal(canonical, got.message) {
		return errors.New("result transcript is not canonical JSON")
	}
	var message mc.Message
	decoder := json.NewDecoder(bytes.NewReader(got.message))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&message) != nil || message.Validate() != nil || message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
		return errors.New("result transcript is not the exact model-visible tool message")
	}
	result := message.Parts[0].ToolResult
	want := v.bundle
	if result.CallID != got.call || !result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil || result.Parts[0].Text.Text != string(want) {
		return errors.New("result transcript changed the complete known command error bundle")
	}
	decoded, err := tc.DecodeRunCommandResultBundle(context.Background(), want)
	if err != nil || !reflect.DeepEqual(decoded.Report, v.expected) || !bytes.Equal(decoded.Stdout, v.terminal.stdout) || !bytes.Equal(decoded.Stderr, v.terminal.stderr) {
		return errors.New("canonical command bundle did not preserve the actual report and full streams")
	}
	clear(decoded.Stdout)
	clear(decoded.Stderr)
	return nil
}

func (v *commandResultConsumptionFixture) requireState(t *testing.T, got toolResultConsumptionState, consumed bool) {
	t.Helper()
	if err := v.validateState(got, consumed); err != nil {
		t.Fatal(err)
	}
}

func (v *commandResultConsumptionFixture) requireDurable(t *testing.T, consumed bool) toolResultConsumptionState {
	t.Helper()
	got, err := v.read(ctxFor(t), v.operation.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent original consumption observation")
	v.requireState(t, got, consumed)
	return got
}

func (v *commandResultConsumptionFixture) requireReceipt(t *testing.T, receipt ec.ToolResultConsumptionReceipt, actual toolResultConsumptionState) {
	t.Helper()
	if receipt.Validate() != nil || receipt.ExecutionID.String() != v.terminal.execution || receipt.RoundID.String() != actual.round || receipt.CallID != actual.call || receipt.OperationID.String() != actual.operation || receipt.AttemptID.String() != actual.attempt || int64(receipt.Version) != actual.checkpoint || receipt.TranscriptThrough != 3 || string(receipt.MessageDigest) != actual.messageDigest || string(receipt.ResultDigest) != actual.resultDigest || !receipt.CreatedAt.Time().Equal(actual.created) {
		t.Fatal("public consumption receipt differs from the original durable checkpoint")
	}
}

func TestRunCommandResultConsumption(t *testing.T) {
	t.Run("revoked-dispatch-grants-consume-and-project", func(t *testing.T) {
		v := newCommandResultConsumptionFixture(t, false)
		v.revokeDispatch(t)
		v.rejectInvalidSQLCandidates(t)
		if !reflect.DeepEqual(v.requireDurable(t, false), v.before) {
			t.Fatal("formal current revocation changed immutable unconsumed facts")
		}
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		ctx, started := ctxFor(t), time.Now()
		receipt, err := consumer.Consume(ctx, e)
		v.diagnostic.stage("consume_return", ctx, started, err, 0, nil, nil)
		if err != nil {
			v.diagnostic.dump(t)
		}
		readFileAdapterOK(t, err, "first historical command consumption after current revocation")
		committed := v.requireDurable(t, true)
		v.requireReceipt(t, receipt, committed)
		observed, found, err := consumer.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "historical receipt Lookup after current revocation")
		if !found || observed != receipt || !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("receipt Lookup changed the one consumed command checkpoint")
		}
		v.determineAndProject(t, receipt, committed)
		v.requireOriginalWork(t)
		t.Log("command_result_consumption original_exit_code=23 complete_streams_with_nul=true original_wire_requests=1 original_model_calls=1 formal_tool_mount_revoked=true first_consume_sequence=3 original_error_bundle_preserved=true determined_projection_read_only=true")
	})
	t.Run("cancelled-report-commit-loss-lookup-and-project", func(t *testing.T) {
		v := newCommandResultConsumptionFixture(t, true)
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		store := v.operation.facts.capture.v.base.tracked
		var candidate toolResultConsumptionState
		seen, consumeTransactions := false, 0
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !v.matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			candidate, err = v.read(ctx, x)
			if err != nil {
				return err
			}
			if err = v.validateState(candidate, true); err != nil {
				return err
			}
			seen = true
			return nil
		})
		loss := &writeResultCommitLoss{match: v.matches, attempt: id[f.TransactionAttempt](t)}
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if v.matches(cause) {
				consumeTransactions++
				v.diagnostic.final(result)
			}
		}
		store.transformResult = loss.transform
		store.mu.Unlock()
		t.Cleanup(func() {
			roundInputClearHooks(store)
			store.mu.Lock()
			store.transformResult = nil
			store.mu.Unlock()
		})
		ctx, started := ctxFor(t), time.Now()
		absent, err := consumer.Consume(ctx, e)
		v.diagnostic.stage("consume_return", ctx, started, err, 0, nil, nil)
		if writeResultFaultCode(err) != string(f.CommitUnknown) {
			v.diagnostic.dump(t)
		}
		requireCode(t, err, f.CommitUnknown)
		original, retained := execution.UnknownAttempt(err)
		fired, physical := loss.observed()
		if !seen || consumeTransactions != 1 || absent != (ec.ToolResultConsumptionReceipt{}) || !fired || physical.State() != f.Committed || !retained || original.State() != f.Unknown || original.AttemptID() != loss.attempt || !v.matches(original.Cause()) {
			t.Fatal("receipt loss did not follow the original fully written consumption COMMIT")
		}
		committed := v.requireDurable(t, true)
		if !reflect.DeepEqual(candidate, committed) {
			t.Fatal("physical COMMIT differs from the complete original candidate")
		}
		v.revokeDispatch(t)
		// No second Consume or Execute after Unknown. The first Lookup observes
		// the original candidate; a new consumer then reads the durable receipt.
		observed, found, err := consumer.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "original unknown Command consumption Lookup after revocation")
		if !found {
			t.Fatal("original Lookup did not observe the committed candidate")
		}
		v.requireReceipt(t, observed, committed)
		stopToolResultConsumer(t, consumer)
		recovered := v.consumer(t)
		again, found, err := recovered.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "new consumer historical receipt recovery")
		if !found || again != observed || consumeTransactions != 1 || !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("historical recovery repeated or changed the original consumption")
		}
		v.requireReceipt(t, again, committed)
		v.determineAndProject(t, again, committed)
		v.requireOriginalWork(t)
		t.Log("command_result_consumption cancelled_report=true live_execution=true physical_consume_commit_then_ack_loss=true original_and_new_consumer_lookup=true consumption_count=1 transcript_sequence=3 original_operation_attempt_rid_unchanged=true wire_requests=1 model_calls=1")
	})
}

type commandResultRoundState struct {
	version, inputs, controls, dispositions, ingress int64
	id, digest, binding                              string
	through, checkpoint                              int64
	content                                          []byte
	sameTx                                           bool
}

func (v *commandResultConsumptionFixture) roundState(t *testing.T) commandResultRoundState {
	t.Helper()
	var out commandResultRoundState
	b := v.operation.facts.capture.v.base
	err := b.raw.QueryRow(ctxFor(t), `SELECT e.version,
 (SELECT count(*) FROM agenteam_execution.round_inputs x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.control_inputs x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_dispositions x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_ingress x WHERE x.execution_id=e.id),
 COALESCE(q.id,''),COALESCE(q.digest,''),COALESCE(q.skill_input_binding_id,''),
 COALESCE(q.transcript_through,0),COALESCE(q.checkpoint_version,0),COALESCE(q.content,''::bytea),
 COALESCE(q.xmin=e.xmin AND q.project_id=e.project_id AND q.agent_id=e.agent_id
  AND q.snapshot_id=e.snapshot_id AND q.predecessor_round_id=k.round_id
  AND q.checkpoint_version=e.version AND q.checkpoint_version=k.checkpoint_version+1,false)
 FROM agenteam_execution.executions e
 LEFT JOIN agenteam_execution.tool_result_consumptions k ON k.execution_id=e.id
 LEFT JOIN agenteam_execution.round_inputs q ON q.execution_id=e.id
 WHERE e.id=$1`, v.terminal.execution).Scan(&out.version, &out.inputs, &out.controls, &out.dispositions, &out.ingress, &out.id, &out.digest, &out.binding, &out.through, &out.checkpoint, &out.content, &out.sameTx)
	readFileAdapterOK(t, err, "independent Command successor input facts")
	return out
}

func (v *commandResultConsumptionFixture) determineAndProject(t *testing.T, receipt ec.ToolResultConsumptionReceipt, consumed toolResultConsumptionState) {
	t.Helper()
	facts, c := v.operation.facts, v.operation.facts.capture
	before := v.roundState(t)
	if before.version != consumed.version || before.inputs != 0 || before.controls != 0 || before.dispositions != 0 || before.ingress != 0 || before.id != "" || len(before.content) != 0 {
		t.Fatal("Command-only successor fixture already contains extra round/control facts")
	}
	initial, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual historical initial Skill capture reader")
	assignments, err := skill.NewAssignmentValidation(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual current Skill assignment validator")
	determiner, err := execution.NewRoundInputDeterminer(c.v.base.tracked, c.authority, execution.RoundInputDependencies{Projects: c.projectAuthority, Agents: c.configuration, InitialSkills: initial, Assignments: assignments})
	readFileAdapterOK(t, err, "actual successor input determiner")
	t.Cleanup(func() {
		determiner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := determiner.Drain(ctx); err != nil || !determiner.Joined() {
			t.Error("original Command successor input calls did not actually join")
		}
	})
	next := id[ec.Round](t)
	input, err := determiner.Determine(ctxFor(t), c.created.ID, next)
	readFileAdapterOK(t, err, "real determined Command successor input after revocation")
	q := input.Fields()
	initialFields := facts.input.Fields().Skills
	if input.Validate() != nil || q.Source != receipt || q.ExecutionID != c.created.ID || q.RoundID != next || q.PredecessorRoundID.String() != consumed.round || q.SnapshotID.String() != v.operation.checkpoint.Snapshot || q.CheckpointVersion != receipt.Version+1 || q.TranscriptThrough != 3 || q.Control != nil || len(q.AppliedChangeIDs) != 0 || len(q.Dispositions) != 0 || q.CompactionRef != nil || !reflect.DeepEqual(q.Initial, initialFields) || !reflect.DeepEqual(q.Bindings, initialFields.Bindings) || q.AssignmentCutoff != initialFields.AssignmentSequence {
		t.Fatal("successor input changed original receipt/capture or invented pending work")
	}
	committed := v.roundState(t)
	if committed.inputs != 1 || committed.controls != 0 || committed.dispositions != 0 || committed.ingress != 0 || !committed.sameTx || committed.version != consumed.version+1 || committed.checkpoint != committed.version || committed.through != 3 || committed.id != next.String() || committed.binding != q.SkillInputBindingID.String() || committed.digest != string(input.Digest()) || !bytes.Equal(committed.content, input.CanonicalBytes()) {
		t.Fatal("actual next input was not committed with its one Execution version change")
	}
	after, err := v.read(ctxFor(t), c.v.base.raw)
	readFileAdapterOK(t, err, "post-Determine original consumption facts")
	if after.version != consumed.version+1 || after.sameTx {
		t.Fatal("Determine did not use its separate original checkpoint transaction")
	}
	stable := after
	stable.version, stable.sameTx = consumed.version, consumed.sameTx
	if !reflect.DeepEqual(stable, consumed) {
		t.Fatal("Determine changed the original consumption, pending Model round or seq1-3")
	}
	projector, err := agentloop.NewRoundProjector(determiner)
	readFileAdapterOK(t, err, "real committed Command projection reader")
	// Reuse only the existing actual transaction read-only observation helper;
	// it needs facts, not the old read-file producer or its fixture assertions.
	observation := &roundInputFixture{facts: facts}
	project := func() agentloop.RoundProjection {
		var got agentloop.RoundProjection
		nextProjectionReadOnly(t, observation, next, func() error {
			var err error
			got, err = projector.Project(ctxFor(t), c.created.ID, next)
			return err
		})
		v.requireProjection(t, input, got)
		current, err := v.read(ctxFor(t), c.v.base.raw)
		readFileAdapterOK(t, err, "independent facts after historical Command projection")
		if !reflect.DeepEqual(current, after) || !reflect.DeepEqual(v.roundState(t), committed) {
			t.Fatal("read-only projection changed durable facts")
		}
		v.requireOriginalWork(t)
		return got
	}
	first, again := project(), project()
	if !bytes.Equal(first.CanonicalBytes(), again.CanonicalBytes()) || first.Digest() != again.Digest() {
		t.Fatal("same committed Command input changed its historical projection")
	}
}

func (v *commandResultConsumptionFixture) requireProjection(t *testing.T, input ec.RoundInput, got agentloop.RoundProjection) {
	t.Helper()
	q := input.Fields()
	if got.Validate() != nil || got.ExecutionID() != q.ExecutionID || got.RoundID() != q.RoundID || got.SourceDigest() != input.Digest() || string(got.Digest()) != toolResultConsumptionDigest(got.CanonicalBytes()) {
		t.Fatal("projection lost the committed Command input identity")
	}
	var roundRaw []byte
	b := v.operation.facts.capture.v.base
	readFileAdapterOK(t, b.raw.QueryRow(ctxFor(t), `SELECT content FROM agenteam_execution.rounds WHERE execution_id=$1 AND id=$2`, q.ExecutionID.String(), q.PredecessorRoundID.String()).Scan(&roundRaw), "original persisted Command Model round")
	old, err := ec.DecodeToolCallRound(roundRaw)
	readFileAdapterOK(t, err, "original canonical Command Model round")
	messages := got.Messages()
	if len(messages) != 5 || len(got.Tools()) != 1 || q.Control != nil {
		t.Fatal("Command projection changed its no-control message shape")
	}
	if !bytes.Equal(nextProjectionJSON(t, messages[0]), nextProjectionJSON(t, old.Fields().Round.Messages[0])) || !bytes.Equal(nextProjectionJSON(t, got.Tools()[0]), nextProjectionJSON(t, old.Fields().Tool)) {
		t.Fatal("Command projection changed the original system or captured Tool schema")
	}
	for n, role := range []string{"system", "user", "assistant", "tool", "user"} {
		if messages[n].Role != role || messages[n].Validate() != nil {
			t.Fatal("Command projection changed complete message order")
		}
	}
	var prefix []json.RawMessage
	for n := 1; n <= 3; n++ {
		var raw []byte
		readFileAdapterOK(t, b.raw.QueryRow(ctxFor(t), `SELECT message FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence=$2 AND round_id=$3`, q.ExecutionID.String(), n, q.PredecessorRoundID.String()).Scan(&raw), "independent original canonical transcript")
		if !bytes.Equal(nextProjectionJSON(t, messages[n]), raw) {
			t.Fatal("projection replaced an original complete canonical message")
		}
		prefix = append(prefix, json.RawMessage(raw))
	}
	prefixBytes, err := json.Marshal(prefix)
	readFileAdapterOK(t, err, "complete canonical transcript prefix")
	if q.TranscriptPrefixDigest != ec.TriggerInputDigest(prefixBytes) {
		t.Fatal("Command projection prefix no longer binds the complete original args")
	}
	// Original seq2 argv and full transcript prefix stay intact. The new
	// ToolResult preserves the complete report and both streams, including NUL.
	var originalCalls int
	for _, part := range messages[2].Parts {
		if part.ToolCall != nil {
			originalCalls++
			if part.ToolCall.ID != q.Source.CallID || !bytes.Equal(part.ToolCall.Arguments, v.operation.expectedCanonical) {
				t.Fatal("projection changed the original protected Command call arguments")
			}
		}
	}
	if originalCalls != 1 || len(messages[3].Parts) != 1 || messages[3].Parts[0].ToolResult == nil {
		t.Fatal("Command projection lacks its exact original call/result pair")
	}
	result := messages[3].Parts[0].ToolResult
	if result.CallID != q.Source.CallID || !result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil {
		t.Fatal("known command error was recast as Tool success")
	}
	raw := []byte(result.Parts[0].Text.Text)
	report, err := tc.DecodeRunCommandResultBundle(ctxFor(t), raw)
	readFileAdapterOK(t, err, "projected original Command receipt")
	if !reflect.DeepEqual(report.Report, v.expected) || !bytes.Equal(report.Stdout, v.terminal.stdout) || !bytes.Equal(report.Stderr, v.terminal.stderr) || !bytes.Equal(raw, v.bundle) {
		t.Fatal("projection changed original report, flags, optional completion code or complete output")
	}
	catalog := make([]any, 0, len(q.Bindings))
	for _, binding := range q.Bindings {
		catalog = append(catalog, nextProjectionSkill(binding))
	}
	effective := nextProjectionJSON(t, map[string]any{"assignment_cutoff": q.AssignmentCutoff, "skills": catalog})
	wantCatalog := mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "Effective Skill catalog for this round (metadata only; not a current access grant):\n" + string(effective)}}}}
	if !bytes.Equal(nextProjectionJSON(t, messages[4]), nextProjectionJSON(t, wantCatalog)) {
		t.Fatal("projection changed the exact final effective Skill directory")
	}
	want := nextProjectionJSON(t, map[string]any{"projection_version": f.Version(1), "execution_id": q.ExecutionID, "round_id": q.RoundID, "round_input_digest": input.Digest(), "messages": messages, "tools": got.Tools()})
	if !bytes.Equal(got.CanonicalBytes(), want) {
		t.Fatal("Command projection envelope differs from its checked complete content")
	}
}
