package runnerhttp

import (
	"context"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"net/http"
	"strings"
	"testing"
	"time"
)

func skillPrepareTestRequest(t *testing.T) p.Request {
	t.Helper()
	id := func() p.ID {
		v, e := p.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	expires, _ := p.NewInstant(time.Now().Add(time.Minute))
	digest := "sha256:" + strings.Repeat("0", 64)
	input := p.SkillPreparePayload{WorkspaceName: "work", SkillID: id(), RevisionID: id(), Revision: "1", ArchiveBytes: "1", ArchiveSHA256: digest, Manifest: p.SkillManifest{Format: p.SkillPackageFormat, EntryPath: "SKILL.md", Files: []p.SkillFile{{Path: "SKILL.md", MediaType: "text/plain", ByteSize: "1", SHA256: digest}}}, Transfer: p.SkillTransfer{ID: id(), Method: "GET", URL: "https://storage.example.invalid/one", Headers: http.Header{}, ExpiresAt: expires}}
	raw, err := p.EncodeSkillPreparePayload(input)
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: id(), OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: p.SkillPrepareOperation, OperationRevision: p.SkillPrepareRevision, Payload: raw}
}
func startSkillPrepareSession(t *testing.T) (*controlSession, *pureSessionSocket, context.CancelFunc, <-chan error) {
	t.Helper()
	socket := newPureSessionSocket()
	s, cancel, done := startPureCentralSession(t, &pureSessionAuthority{}, socket, pureControlGate, false)
	sessionInput(t, socket, p.Hello{RunnerID: s.runner, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{p.SkillPrepareOperation}, FeatureFlags: []string{}})
	if sessionOutput(t, socket).Type() != p.HelloAckType {
		t.Fatal("missing hello")
	}
	sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("missing heartbeat")
	}
	return s, socket, cancel, done
}
func TestSkillPrepareCancellationWaitsOriginalTerminal(t *testing.T) {
	s, socket, _, _ := startSkillPrepareSession(t)
	input := skillPrepareTestRequest(t)
	type answer struct {
		result controlResult
		err    error
	}
	done := make(chan answer, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { r, e := s.dispatchSkillPrepare(ctx, input); done <- answer{r, e} }()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	if wire.RequestID != input.RequestID || wire.OperationID != input.OperationID {
		t.Fatal("original committed request identity replaced")
	}
	cancel()
	if sessionOutput(t, socket).Type() != p.CancelType {
		t.Fatal("missing cancellation")
	}
	select {
	case <-done:
		t.Fatal("caller detached before original handler returned")
	case <-time.After(25 * time.Millisecond):
	}
	code := p.CancelledCode
	message := code.SafeMessage()
	sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &message})
	select {
	case out := <-done:
		if out.err != nil || !out.result.attempted || out.result.terminal == nil {
			t.Fatal("original terminal not joined", out.err)
		}
	case <-time.After(time.Second):
		t.Fatal("original call did not return")
	}
	for _, b := range input.Payload {
		if b != 0 {
			t.Fatal("original private control payload retained")
		}
	}
}
func TestSkillPrepareStatusRemovalRejectsBeforeWire(t *testing.T) {
	s, socket, _, _ := startSkillPrepareSession(t)
	if err := s.receive(context.Background(), pureControlMessage(t, p.RunnerStatus{Capabilities: []string{p.ReadFileOperation}})); err != nil {
		t.Fatal(err)
	}
	out, err := s.dispatchSkillPrepare(context.Background(), skillPrepareTestRequest(t))
	if err != errControlClosed || out.attempted || out.terminal != nil {
		t.Fatal("removed capability admitted", err)
	}
	select {
	case <-socket.out:
		t.Fatal("unadmitted transfer reached wire")
	default:
	}
}
func TestSkillPrepareCopiedCallSharesRetirementState(t *testing.T) {
	call := &SkillPrepareCall{state: &skillPrepareCallState{}}
	copy := *call
	if err := call.CloseUnsent(); err != nil || !copy.Retirement() {
		t.Fatal("copy split original admission", err)
	}
	copy.state.requestsJoined = false
	if call.Retirement() {
		t.Fatal("lost terminal converted to retirement")
	}
}
