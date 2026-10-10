package agentloop

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestDirectTextTurnRejectsUnsafeOrIncompleteResponses(t *testing.T) {
	request := loopCandidate(t)
	for _, kind := range []string{"foreign-call", "invalid-invocation", "length", "filter", "tool-call", "provider-metadata", "empty-content"} {
		response := loopResponse(t, request)
		want := f.CapabilityUnsupported
		switch kind {
		case "foreign-call":
			response.CallID = requestID[mc.Call](t, 121)
			want = f.InvalidState
		case "invalid-invocation":
			response.InvocationID = mc.InvocationID{}
			want = f.InvalidState
		case "length":
			response.FinishReason = "length"
		case "filter":
			response.FinishReason = "content_filter"
		case "tool-call":
			response.Message.Parts = []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "call_1", Name: "invented_tool", Arguments: []byte(`{}`)}}}
		case "provider-metadata":
			response.Message.Parts = append(response.Message.Parts, mc.MessagePart{Metadata: &mc.ProviderMetadataPart{AdapterID: "openai", Version: "v1", OpaqueRef: "continuation"}})
		case "empty-content":
			response.Message.Parts = []mc.MessagePart{}
			want = f.InvalidState
		}
		if kind != "invalid-invocation" && kind != "empty-content" {
			requestOK(t, response.Validate())
		}
		joined, closes := false, 0
		call := &loopJSONCall{
			result: func(context.Context) (mc.ModelResponse, error) { return response, nil },
			close:  func(context.Context) error { closes++; joined = true; return nil },
			joined: func() bool { return joined },
		}
		controller, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) { return call, nil }})
		requestOK(t, err)
		session, err := controller.Accept(request)
		requestOK(t, err)
		got, err := session.Result(context.Background())
		loopCode(t, err, want)
		if got.CallID.Validate() == nil || !session.Joined() || closes != 1 {
			t.Fatal("unsupported response published or original call not closed", kind)
		}
		controller.Stop()
		requestOK(t, controller.Drain(context.Background()))
	}

	// A well-formed response is insufficient while its original I/O/Usage
	// owner is retained. Recovery must close that same handle before publish.
	response := loopResponse(t, request)
	joined, canJoin, begins, results := false, false, 0, 0
	call := &loopJSONCall{
		result: func(context.Context) (mc.ModelResponse, error) { results++; return response, nil },
		close:  func(context.Context) error { joined = canJoin; return nil },
		joined: func() bool { return joined },
	}
	controller, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) { begins++; return call, nil }})
	requestOK(t, err)
	session, err := controller.Accept(request)
	requestOK(t, err)
	got, err := session.Result(context.Background())
	loopCode(t, err, f.ResourceBusy)
	if got.CallID.Validate() == nil || session.Joined() || begins != 1 || results != 1 {
		t.Fatal("response presence replaced actual Joined")
	}
	canJoin = true
	got, err = session.Result(context.Background())
	requestOK(t, err)
	if !session.Joined() || got.InvocationID != response.InvocationID || begins != 1 || results != 2 {
		t.Fatal("original confirmed response not recovered")
	}
	controller.Stop()
	requestOK(t, controller.Drain(context.Background()))

	// An admitted Begin failure still transfers its opaque owner. A rejected
	// Begin without a handle transfers none; neither path manufactures output.
	for _, admitted := range []bool{false, true} {
		var returned mc.JSONCall
		closed := false
		if admitted {
			returned = &loopJSONCall{
				result: func(context.Context) (mc.ModelResponse, error) {
					t.Fatal("failed Begin consumed output")
					return mc.ModelResponse{}, nil
				},
				close:  func(context.Context) error { closed = true; return nil },
				joined: func() bool { return closed },
			}
		}
		controller, err := NewDirectTextController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
			return returned, errors.New("private-begin-failure")
		}})
		requestOK(t, err)
		session, err := controller.Accept(request)
		requestOK(t, err)
		_, err = session.Result(context.Background())
		loopCode(t, err, f.DependencyUnavailable)
		if session.Joined() == admitted {
			t.Fatal("Begin error discarded admitted ownership")
		}
		requestOK(t, session.Drain(context.Background()))
		if !session.Joined() || admitted && !closed {
			t.Fatal("Begin's original call not closed")
		}
		controller.Stop()
		requestOK(t, controller.Drain(context.Background()))
	}
}
