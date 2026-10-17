package model

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestExecutionToolFactsCaptureProfileIsExplicitAndClosed(t *testing.T) {
	p := providerRecord{Input: providerInput{Protocol: mc.OpenAIChat, Enabled: true, Options: json.RawMessage(`{}`)}}
	m := modelRecord{Input: mc.ModelInput{Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{ToolCalls: true, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}}
	sel := mc.SelectionRef{Kind: "direct"}
	before, _ := json.Marshal(m)
	revision, err := toolFactsResolutionProfile(&p, &m, sel)
	if err != nil || revision != toolFactsCaptureRevision || revision == wire.OpenAIChatTextRevision {
		t.Fatal("distinct configuration-only profile", err)
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		t.Fatal("capture changed configured capabilities")
	}
	_, err = resolutionProfile(&p, &m, sel)
	requireCode(t, err, f.CapabilityUnsupported)
	for name, change := range map[string]func(*providerRecord, *modelRecord, *mc.SelectionRef){
		"missing-tools": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) { m.Input.Capabilities.ToolCalls = false },
		"parallel": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) {
			m.Input.Capabilities.ParallelToolCalls = true
		},
		"streaming": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) { m.Input.Capabilities.Streaming = true },
		"reasoning": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) { m.Input.Capabilities.Reasoning = true },
		"schema": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) {
			m.Input.Capabilities.StructuredOutputModes = []string{"json_schema"}
		},
		"protocol": func(p *providerRecord, _ *modelRecord, _ *mc.SelectionRef) { p.Input.Protocol = mc.AnthropicMessages },
		"parameters": func(_ *providerRecord, m *modelRecord, _ *mc.SelectionRef) {
			m.Input.Parameters = json.RawMessage(`{"temperature":1}`)
		},
		"selection": func(_ *providerRecord, _ *modelRecord, s *mc.SelectionRef) {
			*s = mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector}
		},
	} {
		t.Run(name, func(t *testing.T) {
			cp, cm, cs := p, m, sel
			cm.Input = m.Input.Clone()
			change(&cp, &cm, &cs)
			_, err := toolFactsResolutionProfile(&cp, &cm, cs)
			requireCode(t, err, f.CapabilityUnsupported)
		})
	}
	old, _, owner, _ := modelCaptureFixture(t)
	explicit, err := NewToolCallFactsCapture(old.models, owner)
	if err != nil || explicit.profile != toolFactsResolutionCapture || old.profile != defaultResolutionCapture || explicit.models != old.models || explicit.owner != old.owner {
		t.Fatal("explicit capture replaced original store/owner or changed default", err)
	}
	if _, err = NewToolCallFactsCapture(nil, owner); err == nil {
		t.Fatal("unbound capture accepted")
	}
}

func TestExecutionToolFactsCaptureKeepsDefaultRuntimeClosed(t *testing.T) {
	r := resolutionTestRequest(t)
	if err := toolFactsResolutionCapture.requireRequest(r); err != nil {
		t.Fatal(err)
	}
	requireCode(t, toolFactsResolutionCapture.requireRequest(meetingResolutionTestRequest(t)), f.CapabilityUnsupported)
	draft := resolutionTestDraft(t, r)
	old := draft
	draft.Snapshot.Identity.AdapterRevision = toolFactsCaptureRevision
	draft.Snapshot.Capabilities.ToolCalls = true
	if err := toolFactsResolutionCapture.requireDraft(r, draft); err != nil {
		t.Fatal(err)
	}
	requireCode(t, defaultResolutionCapture.requireDraft(r, draft), f.CapabilityUnsupported)
	requireCode(t, toolFactsResolutionCapture.requireDraft(r, old), f.CapabilityUnsupported)
	// The real Runtime gate rejects this revision even if a caller strips the
	// capability bit. No transport/adapter is constructed by this test.
	request := runtimePureRequest(t)
	for _, tools := range []bool{false, true} {
		request.Model.Snapshot.Identity.AdapterRevision = toolFactsCaptureRevision
		request.Model.Snapshot.Capabilities.ToolCalls = tools
		_, err := prepareRuntimeInput(context.Background(), request, wire.JSONResponse)
		requireCode(t, err, f.CapabilityUnsupported)
	}
	identity, _ := resolutionIdentity(r)
	// Mapping requires a real, structurally valid issued consumer plan. A zero
	// plan has an invalid empty digest and cannot be an encoding positive case.
	consumer, err := mc.NewConsumerDependencies(mc.NewPlanIssuer(), mc.ConsumerDependencyDetails{Binding: hash([]byte("mapping-test-binding")), Mapping: hash([]byte("mapping-test-consumer")), Locks: []f.LockRequest{projectLock(r.Consumer.ProjectID.String())}})
	if err != nil {
		t.Fatal(err)
	}
	c := &resolutionCandidate{request: r, identity: identity, draft: old, version: 1, consumer: consumer}
	raw, err := encoded(struct {
		Format   int
		Identity string
		Semantic f.Digest
		Version  f.Version
		Draft    resolutionDraft
		Consumer f.Digest
	}{1, identity.Canonical(), resolutionSemantic(r), 1, old, c.consumer.Details().Mapping})
	if err != nil {
		t.Fatal("valid original mapping encoding", err)
	}
	if c.mapping() != hash(raw) {
		t.Fatal("default mapping bytes changed")
	}
	previous := c.mapping()
	c.profile, c.draft = toolFactsResolutionCapture, draft
	if c.mapping() == previous {
		t.Fatal("plan mapping failed to bind the distinct frozen revision")
	}
}

func TestExecutionToolFactsCaptureUnknownCannotCrossProfiles(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "read-only"}[readOnly], func(t *testing.T) {
			old, store, owner, request := modelCaptureFixture(t)
			capture, err := NewToolCallFactsCapture(old.models, owner)
			if err != nil {
				t.Fatal(err)
			}
			source, err := readExecutionModelSource(context.Background(), store, request)
			if err != nil {
				t.Fatal(err)
			}
			resolve, err := executionModelResolveRequest(request, source)
			if err != nil {
				t.Fatal(err)
			}
			identity, _ := resolutionIdentity(resolve)
			draft := resolutionTestDraft(t, resolve)
			draft.Snapshot.Identity.AdapterRevision = toolFactsCaptureRevision
			draft.Snapshot.Capabilities.ToolCalls = true
			row := &resolutionPreparation{ID: mustID[struct{}](t).String(), Identity: identity.Canonical(), Project: request.ProjectID.String(), OwnerKind: string(resolve.LeaseOwner.Details().Kind), OwnerID: request.ExecutionID.String(), Semantic: resolutionSemantic(resolve), Request: resolutionRequest(resolve), SnapshotID: draft.Snapshot.ID, Phase: "prepared", Version: 1, Draft: draft}
			candidate := &resolutionCandidate{profile: toolFactsResolutionCapture, authority: capture.models.state().authority, request: resolve, identity: identity, row: row, draft: draft, version: 1, locks: executionModelLocks(request)}
			cause, _ := f.NewCommandsCause(identity)
			original := resolutionDiscoveryOutcome(f.UnknownResult(store.attempt, cause), candidate, readOnly)
			var discovery *resolutionDiscoveryUnknown
			if !errors.As(original, &discovery) {
				t.Fatal("missing original Unknown")
			}
			unknown := &executionCaptureUnknown{owner: capture, request: request, attempt: owner.facts.Scope.AttemptBinding, source: source, original: original, discovery: discovery, locks: executionModelLocks(request)}
			stored := *row
			store.row = &stored
			want := mc.ModelCapturePrepared
			if readOnly {
				want = mc.ModelCaptureReadOnly
			}
			state, err := capture.ObserveExecutionModelDiscovery(context.Background(), request, unknown)
			if err != nil || state != want || store.writes != 0 {
				t.Fatal("original profile observation failed", state, err)
			}
			candidate.profile = defaultResolutionCapture
			state, err = capture.ObserveExecutionModelDiscovery(context.Background(), request, unknown)
			if err == nil || state != mc.ModelCapturePending {
				t.Fatal("foreign candidate profile settled")
			}
			candidate.profile = toolFactsResolutionCapture
			candidate.draft.Snapshot.Identity.AdapterRevision = wire.OpenAIChatTextRevision
			state, err = capture.ObserveExecutionModelDiscovery(context.Background(), request, unknown)
			if err == nil || state != mc.ModelCapturePending {
				t.Fatal("different frozen revision settled")
			}
			candidate.draft = draft
			state, err = old.ObserveExecutionModelDiscovery(context.Background(), request, unknown)
			if err == nil || state != mc.ModelCapturePending {
				t.Fatal("default capture settled explicit owner's Unknown")
			}
			if store.writes != 0 {
				t.Fatal("observer wrote")
			}
		})
	}
}
