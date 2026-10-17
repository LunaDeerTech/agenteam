//go:build integration

package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Real Model configuration/PG resolution snapshots feed the real TLS adapter.
// The existing current-resolution consumer owns synthetic consumer business
// rows; this is not an assertion about a complete Agent execution assembly.
func TestModelCapabilityReasoningReviewSnapshotWire(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	w := newWireFixture(t)
	w.allow(t, true)
	for _, tc := range []struct {
		name   string
		choice string
		empty  bool
	}{
		{name: "selected-value-is-frozen", choice: "vendor-depth"},
		{name: "unselected-adjustable-model-omits-field"},
		{name: "unselected-fixed-model-omits-field", empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := w.scenario(t, wireJSON(wireReply("reasoning review answer", "stop", nil, nil)))
			providerReceipt, err := v.service.CreateProvider(testContext(t), mc.CreateProviderRequest{
				CommandMeta: v.meta(t, newID[struct{}](t).String()),
				Input: mc.ProviderInput{Name: "Reasoning review provider", Protocol: mc.OpenAICompatible,
					BaseURL: "https://fixture.test:8443/case/" + key, Enabled: true, Options: json.RawMessage(`{}`)},
			})
			if err != nil {
				t.Fatal("create controlled provider", err)
			}
			providerID, err := f.ParseID[mc.Provider](providerReceipt.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			input := mc.ModelInput{Notes: "Reasoning review model", ProviderModelID: "reasoning-review-chat", Type: mc.ChatModel,
				Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`),
				Capabilities: mc.Capabilities{Reasoning: true, InputModalities: []string{"text", "image", "file"}, OutputModalities: []string{"text"}},
			}
			if !tc.empty {
				input.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "Deep display label", Value: "vendor-depth"}}
			}
			created, err := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ProviderID: providerID, Input: input})
			if err != nil {
				t.Fatal("create typed model", err)
			}
			modelID, err := f.ParseID[mc.Model](created.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			m, err := v.service.GetModel(testContext(t), v.admin, modelID)
			if err != nil {
				t.Fatal("read typed model", err)
			}
			if !tc.empty && (len(m.Input.Capabilities.ReasoningEfforts) != 1 || m.Input.Capabilities.ReasoningEfforts[0].Name != "Deep display label" || m.Input.Capabilities.ReasoningEfforts[0].Value != "vendor-depth") {
				t.Fatal("persisted name/value changed")
			}
			r := v.request(t, m, false)
			r.ReasoningEffort = tc.choice
			v.bind(t, r)
			resolved := v.resolve(t, r)
			if resolved.Snapshot.ReasoningEffort != tc.choice || resolved.Snapshot.Endpoint != inputEndpoint(key) {
				t.Fatal("snapshot lost selected value or configured endpoint")
			}
			if tc.choice != "" {
				changed := m.Input.Clone()
				changed.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "New display label", Value: "new-depth"}}
				_, err = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: m.ID, ExpectedVersion: m.Version, Input: changed})
				if err != nil {
					t.Fatal("change live effort declaration", err)
				}
				replayed := v.resolve(t, r)
				if replayed.Snapshot.ID != resolved.Snapshot.ID || string(resolutionJSON(replayed.Snapshot)) != string(resolutionJSON(resolved.Snapshot)) {
					t.Fatal("live configuration rewrote the frozen snapshot")
				}
				fresh := v.request(t, m, false)
				fresh.ReasoningEffort = "vendor-depth"
				v.bind(t, fresh)
				before := v.count(t, "snapshots")
				refused, refusal := v.resolving.ResolveModel(testContext(t), fresh)
				resolutionNoResult(t, refused, refusal)
				requireCode(t, refusal, f.CapabilityUnsupported)
				if v.count(t, "snapshots") != before {
					t.Fatal("undeclared effort created a snapshot")
				}
			}
			req, opts := w.input(t, key, wire.JSONResponse)
			req.Snapshot = resolved.Snapshot.Clone()
			x := w.start(t, testContext(t), w.adapter, req, opts)
			result, err := x.Result(testContext(t))
			if err != nil || result.Text != "reasoning review answer" {
				t.Fatal("real TLS chat result", err)
			}
			wireClose(t, x)
			w.settled(t, key)
			state := w.state(t, key)
			if len(state.Requests) != 1 {
				t.Fatal("expected exactly one controlled provider call")
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal([]byte(state.Requests[0].Body), &body); err != nil {
				t.Fatal("decode captured wire body", err)
			}
			effort, present := body["reasoning_effort"]
			if tc.choice == "" {
				if present || len(body) != 3 {
					t.Fatal("provider default was replaced by a reasoning field")
				}
			} else if !present || string(effort) != `"vendor-depth"` || len(body) != 4 {
				t.Fatal("fixed reasoning_effort field did not receive only the declared value")
			}
			if strings.Contains(state.Requests[0].Body, "display label") || strings.Contains(state.Requests[0].Body, "reasoning_efforts") {
				t.Fatal("display metadata leaked into the protocol request")
			}
			if tc.choice != "" {
				ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: opts.ProjectID, ArtifactID: newID[struct{}](t).String(), FileID: newID[struct{}](t).String()})
				if err != nil {
					t.Fatal(err)
				}
				req.Messages[0].Parts[0] = mc.MessagePart{Image: &mc.MediaPart{Ref: ref, MediaType: "image/png"}}
				if err := req.Messages[0].Validate(); err != nil {
					t.Fatal("media negative case is structurally invalid", err)
				}
				handle, err := w.adapter.Start(testContext(t), req, opts)
				if handle != nil {
					wireClose(t, handle)
					t.Fatal("media STOP admitted an exchange")
				}
				wireModelError(t, err, "unsupported_feature", "wire_unsupported_feature", false, false)
				if len(w.state(t, key).Requests) != 1 {
					t.Fatal("unsupported media made an additional provider request")
				}
			}
		})
	}
}

func inputEndpoint(key string) string { return "https://fixture.test:8443/case/" + key }
