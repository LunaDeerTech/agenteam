package adapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestReasoningPreparedRequestOwnedTLSTransport(t *testing.T) {
	// Exercise the prepared wire request over an owned TLS connection. The
	// outbound policy, credential lease and database lifecycle are tested elsewhere.
	bodies := make(chan []byte, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil || r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- raw
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	for _, profile := range []struct {
		name  string
		input func(*testing.T) (Request, CallOptions)
	}{{"text", unitInput}, {"structured", structuredInput}, {"tools", jsonToolsInput}} {
		for _, selection := range []string{"custom", "default", "fixed"} {
			t.Run(profile.name+"/"+selection, func(t *testing.T) {
				r, o := profile.input(t)
				r.Snapshot.Endpoint = server.URL
				r.Snapshot.Capabilities.Reasoning = true
				r.Snapshot.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "深入思考", Value: "custom:deep"}}
				if selection == "custom" {
					r.Snapshot.ReasoningEffort = "custom:deep"
					r.Snapshot.Capabilities.InputModalities = []string{"text", "image", "file"}
				} else if selection == "fixed" {
					r.Snapshot.Capabilities.ReasoningEfforts = nil
				}
				req, _, err := prepare(r, o)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					t.Fatalf("owned server status %d", response.StatusCode)
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(<-bodies, &body); err != nil {
					t.Fatal(err)
				}
				effort, present := body["reasoning_effort"]
				if selection == "custom" {
					if string(effort) != `"custom:deep"` {
						t.Fatalf("provider effort got %s; wanted declared value, not display name", effort)
					}
				} else if present {
					t.Fatalf("unselected effort must be omitted, got %s", effort)
				}
				if profile.name == "structured" && body["response_format"] == nil || profile.name == "tools" && body["tools"] == nil {
					t.Fatal("reasoning changed the selected request profile")
				}
			})
		}
	}
}

func TestReasoningUnsupportedSelectionRejectsBeforeAdmission(t *testing.T) {
	for _, input := range []func(*testing.T) (Request, CallOptions){unitInput, structuredInput, jsonToolsInput} {
		r, o := input(t)
		t.Run(r.Snapshot.Identity.AdapterRevision, func(t *testing.T) {
			r.Snapshot.Capabilities.Reasoning = true
			r.Snapshot.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "深入思考", Value: "custom:deep"}}
			r.Snapshot.ReasoningEffort = "undeclared"
			budget := NewBudget()
			budget.StopAdmission()
			// Invalid selection must fail before the stopped admission check;
			// no transport or asynchronous outbound work may be created.
			x, err := (&OpenAIChat{budget: budget}).Start(context.Background(), r, o)
			if x != nil || !budget.Joined() {
				t.Fatal("invalid reasoning selection admitted outbound work")
			}
			requireFault(t, err, f.InvalidArgument)
		})
	}
}
