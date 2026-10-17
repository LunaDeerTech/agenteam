package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestReasoningTogglePreparedRequestOwnedTLS(t *testing.T) {
	server, client, bodies := reasoningTestTransport(t)
	for _, profile := range []struct {
		name  string
		input func(*testing.T) (Request, CallOptions)
	}{{"text", unitInput}, {"structured", structuredInput}, {"tools", jsonToolsInput}} {
		for _, mapping := range []string{"enable_thinking", "thinking_type"} {
			for _, selection := range []string{"on", "off", "default"} {
				t.Run(profile.name+"/"+mapping+"/"+selection, func(t *testing.T) {
					r, o := profile.input(t)
					r.Snapshot.Endpoint = server.URL
					r.Snapshot.Capabilities.Reasoning = true
					r.Snapshot.Capabilities.ReasoningToggleMode = &mapping
					if selection != "default" {
						enabled := selection == "on"
						r.Snapshot.ReasoningEnabled = &enabled
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
						t.Fatal("owned TLS server rejected the prepared request")
					}
					var body map[string]json.RawMessage
					if err := json.Unmarshal(<-bodies, &body); err != nil {
						t.Fatal(err)
					}
					if body["reasoning_effort"] != nil {
						t.Fatal("toggle selection acquired a reasoning effort")
					}
					if selection == "default" {
						if body["enable_thinking"] != nil || body["thinking"] != nil {
							t.Fatal("provider default must omit both toggle fields")
						}
					} else if mapping == "enable_thinking" {
						want := "false"
						if selection == "on" {
							want = "true"
						}
						if string(body["enable_thinking"]) != want || body["thinking"] != nil {
							t.Fatal("enable_thinking must preserve the explicit boolean")
						}
					} else {
						want := `{"type":"disabled"}`
						if selection == "on" {
							want = `{"type":"enabled"}`
						}
						if string(body["thinking"]) != want || body["enable_thinking"] != nil {
							t.Fatal("thinking type must match the explicit toggle")
						}
					}
					if profile.name == "structured" && body["response_format"] == nil || profile.name == "tools" && body["tools"] == nil {
						t.Fatal("reasoning toggle changed the request profile")
					}
				})
			}
		}
	}
}

func TestReasoningToggleMappingRejectsBeforeAdmission(t *testing.T) {
	for _, input := range []func(*testing.T) (Request, CallOptions){unitInput, structuredInput, jsonToolsInput} {
		for _, mapping := range []string{"", "arbitrary_field"} {
			r, o := input(t)
			t.Run(r.Snapshot.Identity.AdapterRevision+"/"+mapping, func(t *testing.T) {
				r.Snapshot.Capabilities.Reasoning = true
				if mapping != "" {
					r.Snapshot.Capabilities.ReasoningToggleMode = &mapping
				}
				enabled := false
				r.Snapshot.ReasoningEnabled = &enabled
				budget := NewBudget()
				budget.StopAdmission()
				x, err := (&OpenAIChat{budget: budget}).Start(context.Background(), r, o)
				if x != nil || !budget.Joined() {
					t.Fatal("invalid toggle mapping admitted outbound work")
				}
				requireFault(t, err, f.InvalidArgument)
			})
		}
	}
}
