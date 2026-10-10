package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// These ports control only call ownership. The request uses the existing real
// typed decoder fixture; none of these tests creates an Execution grant, a
// persisted Round/Transcript or a successful Model/Usage transaction.
type loopJSONCaller struct {
	begin func(context.Context, mc.ModelRequest) (mc.JSONCall, error)
}

func (c *loopJSONCaller) BeginChat(ctx context.Context, request mc.ModelRequest) (mc.JSONCall, error) {
	return c.begin(ctx, request)
}

type loopJSONCall struct {
	result func(context.Context) (mc.ModelResponse, error)
	close  func(context.Context) error
	joined func() bool
}

func (c *loopJSONCall) Result(ctx context.Context) (mc.ModelResponse, error) { return c.result(ctx) }
func (c *loopJSONCall) Close(ctx context.Context) error                      { return c.close(ctx) }
func (c *loopJSONCall) Joined() bool                                         { return c.joined() }

type loopOwnerKey struct{}

func loopCandidate(t *testing.T) DirectTextRequest {
	t.Helper()
	return requestCandidate(t, requestContext(t, requestFields(t, "task/work")))
}

func loopResponse(t *testing.T, request DirectTextRequest) mc.ModelResponse {
	t.Helper()
	response := mc.ModelResponse{CallID: request.Request().CallID, InvocationID: requestID[mc.Invocation](t, 120), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "private-assistant-text"}}}}, FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}}
	requestOK(t, response.Validate())
	return response
}

func loopCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var value *f.Fault
	if !errors.As(err, &value) || value.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestDirectTextControllerAcceptsAndCompletesOwnedTurn(t *testing.T) {
	request := loopCandidate(t)
	response := loopResponse(t, request)
	var begin, result, closeCount int
	joined := false
	owner := &struct{}{}
	call := &loopJSONCall{
		result: func(ctx context.Context) (mc.ModelResponse, error) {
			result++
			if ctx.Value(loopOwnerKey{}) != owner {
				t.Fatal("Result lost original owner context")
			}
			return response, nil
		},
		close: func(ctx context.Context) error {
			closeCount++
			if ctx.Value(loopOwnerKey{}) != owner {
				t.Fatal("Close lost original owner context")
			}
			joined = true
			return nil
		},
		joined: func() bool { return joined },
	}
	caller := &loopJSONCaller{begin: func(ctx context.Context, got mc.ModelRequest) (mc.JSONCall, error) {
		begin++
		if ctx.Value(loopOwnerKey{}) != owner || got.CallID != request.Request().CallID || got.Input.Digest != request.Request().Input.Digest {
			t.Fatal("Begin changed owner or exact request identity")
		}
		got.Messages[0].Parts[0].Text.Text = "port-mutated-copy"
		return call, nil
	}}
	for _, invalid := range []mc.JSONCaller{nil, (*loopJSONCaller)(nil)} {
		_, err := NewDirectTextController(invalid)
		loopCode(t, err, f.DependencyUnbound)
	}
	controller, err := NewDirectTextController(caller)
	requestOK(t, err)
	_, err = controller.Accept(DirectTextRequest{})
	loopCode(t, err, f.InvalidArgument)
	session, err := controller.Accept(request)
	requestOK(t, err)
	if begin != 0 || session.Joined() || controller.Joined() {
		t.Fatal("Accept invoked Model or prematurely joined")
	}
	_, err = controller.Accept(request)
	loopCode(t, err, f.ResourceBusy)
	ctx := context.WithValue(context.Background(), loopOwnerKey{}, owner)
	got, err := session.Result(ctx)
	requestOK(t, err)
	if !session.Joined() || controller.Joined() || begin != 1 || result != 1 || closeCount != 1 || got.CallID != response.CallID || got.InvocationID != response.InvocationID {
		t.Fatal("normal response published before exact call joined")
	}
	got.Message.Parts[0].Text.Text = "caller-mutated-copy"
	response.Message.Parts[0].Text.Text = "dependency-mutated-copy"
	again, err := session.Result(context.Background())
	requestOK(t, err)
	if again.Message.Parts[0].Text.Text != "private-assistant-text" || begin != 1 || result != 1 || closeCount != 1 || strings.Contains(request.Request().Messages[0].Parts[0].Text.Text, "port-mutated-copy") {
		t.Fatal("cached result/request alias or repeated invocation")
	}
	for _, value := range []any{controller, session, struct{ Value any }{session}} {
		raw, err := json.Marshal(value)
		requestOK(t, err)
		if strings.Contains(string(raw)+fmt.Sprintf("%#v", value), "private-assistant-text") {
			t.Fatal("default session projection exposed response")
		}
	}
	controller.Stop()
	requestOK(t, controller.Drain(context.Background()))
	if !controller.Joined() {
		t.Fatal("stopped controller retained joined session")
	}
	_, err = controller.Accept(request)
	loopCode(t, err, f.ShuttingDown)
}

func TestDirectTextControllerKeepsUnknownHandleWithoutRedispatch(t *testing.T) {
	for _, duringBegin := range []bool{false, true} {
		request := loopCandidate(t)
		response := loopResponse(t, request)
		cause := errors.New("private-original-unknown-material")
		unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
		var begins, results, closes int
		canJoin, joined := false, false
		owner := &struct{}{}
		call := &loopJSONCall{
			result: func(context.Context) (mc.ModelResponse, error) {
				results++
				if duringBegin {
					t.Fatal("begin failure gained a first dispatch")
				}
				if results == 1 {
					return mc.ModelResponse{}, unknown
				}
				if !joined {
					t.Fatal("Unknown response reread before original Close/Joined")
				}
				return response.Clone(), nil
			},
			close: func(ctx context.Context) error {
				closes++
				if ctx.Value(loopOwnerKey{}) != owner {
					t.Fatal("recovery borrowed a replacement owner")
				}
				if !canJoin {
					return unknown
				}
				joined = true
				return nil
			},
			joined: func() bool { return joined },
		}
		controller, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
			begins++
			if duringBegin {
				return call, unknown
			}
			return call, nil
		}})
		requestOK(t, err)
		session, err := controller.Accept(request)
		requestOK(t, err)
		_, err = session.Result(context.WithValue(context.Background(), loopOwnerKey{}, owner))
		loopCode(t, err, f.CommitUnknown)
		if !errors.Is(err, cause) || strings.Contains(fmt.Sprint(err), cause.Error()) || session.Joined() || closes != 0 || begins != 1 {
			t.Fatal("first Unknown lost original failure or ownership")
		}
		_, err = controller.Accept(request)
		loopCode(t, err, f.ResourceBusy)
		_, err = session.Result(context.Background())
		loopCode(t, err, f.CommitUnknown)
		if begins != 1 || session.Joined() || closes != 1 {
			t.Fatal("unconfirmed recovery repeated admission or retired owner")
		}
		canJoin = true
		got, err := session.Result(context.Background())
		if duringBegin {
			loopCode(t, err, f.CommitUnknown)
			if results != 0 {
				t.Fatal("unstarted response was invoked")
			}
		} else {
			requestOK(t, err)
			if got.InvocationID != response.InvocationID || results != 2 {
				t.Fatal("confirmed response did not retain original invocation")
			}
		}
		if begins != 1 || !session.Joined() || closes != 2 {
			t.Fatal("exact handle did not join")
		}
		controller.Stop()
		requestOK(t, controller.Drain(context.Background()))
		if !controller.Joined() {
			t.Fatal("controller did not release observed original owner")
		}
	}
}

func TestDirectTextControllerCancellationWaitsForActualModelReturn(t *testing.T) {
	request := loopCandidate(t)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var closes atomic.Int32
	var joined atomic.Bool
	owner := &struct{}{}
	call := &loopJSONCall{
		result: func(ctx context.Context) (mc.ModelResponse, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release // cancellation is deliberately not actual return
			return mc.ModelResponse{}, fmt.Errorf("private cancellation: %w", ctx.Err())
		},
		close: func(ctx context.Context) error {
			closes.Add(1)
			if ctx.Value(loopOwnerKey{}) != owner || ctx.Err() != nil {
				return errors.New("cleanup lost original owner or inherited canceled wait")
			}
			joined.Store(true)
			return nil
		},
		joined: joined.Load,
	}
	controller, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) { return call, nil }})
	requestOK(t, err)
	session, err := controller.Accept(request)
	requestOK(t, err)
	returned := make(chan error, 1)
	go func() {
		_, err := session.Result(context.WithValue(context.Background(), loopOwnerKey{}, owner))
		returned <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Result did not enter controlled call")
	}
	session.Stop()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not reach original call")
	}
	budget, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = session.Drain(budget)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || session.Joined() || closes.Load() != 0 {
		t.Fatal("Drain replaced actual result return with cancellation")
	}
	_, err = controller.Accept(request)
	loopCode(t, err, f.ResourceBusy)
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-returned:
		if err != context.Canceled {
			t.Fatal("cancellation leaked dependency text or changed identity", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original Result did not return")
	}
	requestOK(t, session.Drain(context.Background()))
	if !session.Joined() || closes.Load() != 1 {
		t.Fatal("returned call not actually closed/joined")
	}
	controller.Stop()
	requestOK(t, controller.Drain(context.Background()))
	if !controller.Joined() {
		t.Fatal("controller retained canceled, joined original call")
	}

	// A stopped, accepted session that never ran has no external call to join.
	unused, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
		t.Fatal("stopped admission invoked Model")
		return nil, nil
	}})
	requestOK(t, err)
	unstarted, err := unused.Accept(request)
	requestOK(t, err)
	unused.Stop()
	requestOK(t, unused.Drain(context.Background()))
	if !unstarted.Joined() || !unused.Joined() {
		t.Fatal("unstarted stopped session retained an imaginary call")
	}
}
