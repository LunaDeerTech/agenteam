//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// These tests retain the strict owned consumer fixture: only consumer business
// facts are synthetic. Resolution, snapshots, leases, Runtime, Usage, outbound
// policy, TLS, Secret destruction and ProcessGuard are the real components.
func m3RuntimeModel(t *testing.T, v *textRuntimeFixture, key string, embedding bool) mc.ModelID {
	t.Helper()
	credential := v.base.credential(t, sc.Model)
	provider := projectProviderInput()
	provider.BaseURL, provider.CredentialRef = "https://fixture.test:8443/case/"+key, &credential.CredentialRef
	if embedding {
		provider.Protocol = mc.OpenAIEmbeddings
	}
	p, err := v.base.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), Input: provider})
	requireTextRuntime(t, err)
	pid, _ := f.ParseID[mc.Provider](p.ResourceID)
	input := projectModelInput()
	input.ProviderModelID = "fixture-native-model"
	input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Streaming: true, StructuredOutputModes: []string{"text", "json_schema"}}
	if embedding {
		input.Type = mc.EmbeddingModel
		input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}
	}
	m, err := v.base.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ProviderID: pid, Input: input})
	requireTextRuntime(t, err)
	mid, err := f.ParseID[mc.Model](m.ResourceID)
	requireTextRuntime(t, err)
	return mid
}

func m3RuntimeSelection(t *testing.T, v *textRuntimeFixture, embedding, memory mc.ModelID) {
	t.Helper()
	state, err := v.base.service.GetPlatformSelection(testContext(t), v.base.admin)
	requireTextRuntime(t, err)
	_, err = v.base.service.UpdatePlatformSelection(testContext(t), mc.UpdatePlatformSelectionRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ExpectedVersion: state.Version, Selection: mc.PlatformSelection{ID: state.ID, Version: state.Version, Embedding: embedding, Memory: memory}})
	requireTextRuntime(t, err)
}

func m3RuntimeRequest(t *testing.T, v *textRuntimeFixture, purpose mc.Purpose, format mc.ResponseFormat, serving *mc.SnapshotID) (mc.ModelRequest, mc.EmbeddingRequest) {
	return m3RuntimeRequestInput(t, v, purpose, format, serving, []string{textRuntimeInputCanary}, 2)
}

func m3RuntimeRequestInput(t *testing.T, v *textRuntimeFixture, purpose mc.Purpose, format mc.ResponseFormat, serving *mc.SnapshotID, texts []string, expectedDimensions int) (mc.ModelRequest, mc.EmbeddingRequest) {
	return m3RuntimeGenerationRequest(t, v, purpose, format, serving, "", texts, expectedDimensions)
}

func m3RuntimeGenerationRequest(t *testing.T, v *textRuntimeFixture, purpose mc.Purpose, format mc.ResponseFormat, serving *mc.SnapshotID, generation string, texts []string, expectedDimensions int) (mc.ModelRequest, mc.EmbeddingRequest) {
	t.Helper()
	call := newID[mc.Call](t)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
	requireTextRuntime(t, err)
	consumer := mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: v.base.project.ID, OperationID: newID[struct{}](t).String(), Purpose: purpose}
	agent := newID[id.Agent](t)
	consumer.AgentID = &agent
	selector := mc.MemorySelector
	embedding := purpose == mc.KnowledgeEmbedding
	if embedding {
		consumer.Kind, consumer.AgentID, selector = mc.KnowledgeConsumer, nil, mc.EmbeddingSelector
	}
	resolve := mc.ResolveRequest{Actor: v.base.owner, Consumer: consumer, Purpose: purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: selector}, LeaseOwner: owner}
	if serving != nil {
		if generation == "" {
			generation = newID[struct{}](t).String()
		}
		resolve.Source, resolve.Selection, resolve.ServingSnapshotID, resolve.ServingGenerationID = mc.ServingSnapshotSource, nil, serving, generation
	}
	messages := []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: texts[0]}}}}}
	digest, err := model.StructuredInputDigest(messages, format)
	var dimensions any
	var textData any
	if embedding {
		digest, err = model.EmbeddingInputDigest(texts, expectedDimensions)
		dimensions = expectedDimensions
		textData = resolutionJSON(texts)
	}
	requireTextRuntime(t, err)
	identity := mc.InputIdentity{OperationID: consumer.OperationID, Digest: digest, SchemaVersion: 1}
	deadline, _ := f.NewInstant(time.Now().Add(30 * time.Second))
	attempts := f.Sequence(1)
	policy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &attempts}
	_, err = v.base.raw.Exec(testContext(t), `INSERT INTO model_runtime_fixture.operations(id,project_id,meeting_id,call_id,initiator,messages,input_data,policy_data,resolve_data,version,enabled,consumer_data,response_format,texts,dimensions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,true,$10,$11,$12,$13)`, consumer.OperationID, consumer.ProjectID.String(), newID[struct{}](t).String(), call.String(), resolve.Actor.Details().UserID, resolutionJSON(messages), resolutionJSON(identity), resolutionJSON(policy), resolutionStable(resolve), resolutionJSON(consumer), resolutionJSON(format), textData, dimensions)
	if err != nil {
		t.Fatal("M3 consumer seed insert", err)
	}
	if _, err := v.consumer.facts(testContext(t), v.base.raw, consumer.OperationID); err != nil {
		t.Fatal("M3 consumer seed round trip", purpose, err)
	}
	plan, err := v.base.service.DiscoverResolve(testContext(t), resolve)
	if err != nil {
		t.Fatal("M3 resolution discovery", err)
	}
	var resolved mc.ResolvedModel
	outcome := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		resolved, err = v.base.service.ResolveModelInTx(ctx, tx, resolve, plan)
		if err != nil {
			return err
		}
		x, err := v.base.raw.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE model_runtime_fixture.operations SET snapshot_id=$2,lease_id=$3,version=version+1 WHERE id=$1`, consumer.OperationID, resolved.Snapshot.ID.String(), resolved.CredentialLease.LeaseID.String())
		return err
	})
	if outcome.State() != f.Committed {
		t.Fatal("M3 resolution transaction", outcome.Fault())
	}
	chat := mc.ModelRequest{Actor: v.base.owner, CallID: call, Consumer: consumer, Model: resolved, Input: identity, Messages: messages, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: format, RetryClass: mc.BoundedRetry}
	return chat, mc.EmbeddingRequest{NonchatCall: mc.NonchatCall{Actor: chat.Actor, CallID: call, Consumer: consumer, Model: resolved, Input: identity}, Texts: texts, ExpectedDimensions: expectedDimensions}
}

func m3RuntimeTerminal(t *testing.T, v *textRuntimeFixture, call mc.CallID, expected string) {
	t.Helper()
	var phase, ledger, dispatch string
	var retired, released bool
	var attempts int
	var tokens *int64
	err := v.base.raw.QueryRow(testContext(t), `SELECT c.phase,c.retired,l.released,i.final_status,i.dispatch,i.input_tokens,(SELECT count(*) FROM agenteam_model.runtime_attempts WHERE call_id=c.id) FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call.String()).Scan(&phase, &retired, &released, &ledger, &dispatch, &tokens, &attempts)
	requireTextRuntime(t, err)
	if phase != expected || ledger != expected || !retired || !released || attempts != 1 || dispatch != "sent" || tokens == nil || *tokens != 3 {
		t.Fatal("M3 Runtime/Usage/lease terminal mismatch")
	}
	v.reader.mu.Lock()
	defer v.reader.mu.Unlock()
	for _, material := range v.reader.materials {
		used := false
		if err := material.Use(func([]byte) error { used = true; return nil }); err == nil || used {
			t.Fatal("credential material remained live")
		}
	}
}

func TestModelM3RuntimeEmbeddingsServingAndStructured(t *testing.T) {
	v := newTextRuntimeFixture(t)
	v.wire.allow(t, true)
	usage := json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`)
	embeddingKey := embeddingWireScenario(t, v.wire, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{0.25, -0.5}}, json.RawMessage(`{"prompt_tokens":3,"total_tokens":3,"completion_tokens":0}`))))
	chatKey := v.wire.scenario(t, wireJSON(wireReply(`{"v":1}`, "stop", usage, nil)))
	em, chat := m3RuntimeModel(t, v, embeddingKey, true), m3RuntimeModel(t, v, chatKey, false)
	m3RuntimeSelection(t, v, em, chat)
	_, first := m3RuntimeRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, nil)
	result, err := v.core.Embed(testContext(t), first)
	if err != nil || result.ValidateFor(1, 2) != nil || result.Items[0].Values[0] != 0.25 {
		t.Fatal("real embedding Runtime", err)
	}
	m3RuntimeTerminal(t, v, first.CallID, "succeeded")
	// Drift the live selector, then reuse the immutable serving snapshot under
	// a fresh call and lease. The current model must never replace its identity.
	alternate := m3RuntimeModel(t, v, embeddingKey, true)
	m3RuntimeSelection(t, v, alternate, chat)
	// Reuse the immutable serving snapshot under a fresh call and lease.
	_, second := m3RuntimeRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, &first.Model.Snapshot.ID)
	if second.Model.Snapshot.ID != first.Model.Snapshot.ID || second.Model.CredentialLease.LeaseID == first.Model.CredentialLease.LeaseID {
		t.Fatal("serving binding lost snapshot or reused lease")
	}
	result, err = v.core.Embed(testContext(t), second)
	if err != nil || result.ValidateFor(1, 2) != nil {
		t.Fatal("serving embedding Runtime", err)
	}
	m3RuntimeTerminal(t, v, second.CallID, "succeeded")
	format := mc.ResponseFormat{Kind: "json_schema", Name: "memory_result", Schema: json.RawMessage(structuredScalarSchema)}
	structured, _ := m3RuntimeRequest(t, v, mc.MemoryExtraction, format, nil)
	response, err := v.core.Chat(testContext(t), structured)
	if err != nil || response.Message.Parts[0].Text.Text != `{"v":1}` {
		t.Fatal("Memory structured Runtime", err)
	}
	m3RuntimeTerminal(t, v, structured.CallID, "succeeded")
	reflected, _ := m3RuntimeRequest(t, v, mc.MemoryReflection, mc.ResponseFormat{Kind: "text"}, nil)
	if _, err = v.core.Chat(testContext(t), reflected); err != nil {
		t.Fatal("Memory text reflection", err)
	}
	m3RuntimeTerminal(t, v, reflected.CallID, "succeeded")
	for _, key := range []string{embeddingKey, chatKey} {
		v.wire.settled(t, key)
		if len(v.wire.state(t, key).Requests) != 2 {
			t.Fatal("unexpected retries")
		}
	}
}

func TestModelM3RuntimeStructuredStreamValidation(t *testing.T) {
	v := newTextRuntimeFixture(t)
	v.wire.allow(t, true)
	embeddingKey := embeddingWireScenario(t, v.wire, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, nil)))
	em := m3RuntimeModel(t, v, embeddingKey, true)
	for _, valid := range []bool{true, false} {
		value := `{"v":1}`
		expected := "succeeded"
		if !valid {
			value = `{"v":"wrong"}`
			expected = "failed"
		}
		raw := wireChunk(map[string]any{"content": value}, nil, nil, nil, false)
		raw = append(raw, wireChunk(map[string]any{}, "stop", nil, nil, false)...)
		raw = append(raw, wireChunk(nil, nil, json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`), nil, true)...)
		raw = append(raw, []byte("data: [DONE]\n\n")...)
		key := v.wire.scenario(t, wireSSE(raw))
		chat := m3RuntimeModel(t, v, key, false)
		m3RuntimeSelection(t, v, em, chat)
		request, _ := m3RuntimeRequest(t, v, mc.MemoryConsolidation, mc.ResponseFormat{Kind: "json_schema", Name: "memory", Schema: json.RawMessage(structuredScalarSchema)}, nil)
		stream, err := v.core.Stream(testContext(t), request)
		requireTextRuntime(t, err)
		ends, failures := 0, 0
		for {
			frame, err := stream.Next(testContext(t))
			if errors.Is(err, io.EOF) {
				break
			}
			requireTextRuntime(t, err)
			if frame.Kind == "message_end" {
				ends++
			}
			if frame.Kind == "call_failed" {
				failures++
			}
		}
		if valid && (ends != 1 || failures != 0) || !valid && (ends != 0 || failures != 1) {
			t.Fatal("unvalidated structured completion escaped")
		}
		requireTextRuntime(t, stream.Close(testContext(t)))
		if !stream.Joined() {
			t.Fatal("structured stream not joined")
		}
		m3RuntimeTerminal(t, v, request.CallID, expected)
		v.wire.settled(t, key)
	}
}

func TestModelM3RuntimePreAdmissionRetirement(t *testing.T) {
	v := newTextRuntimeFixture(t)
	v.wire.allow(t, true)
	key := v.wire.scenario(t, wireJSON(wireReply(`{"v":1}`, "stop", json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`), nil)))
	embeddingKey := embeddingWireScenario(t, v.wire, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, nil)))
	m3RuntimeSelection(t, v, m3RuntimeModel(t, v, embeddingKey, true), m3RuntimeModel(t, v, key, false))
	request, _ := m3RuntimeRequest(t, v, mc.MemoryExtraction, mc.ResponseFormat{Kind: "json_schema", Name: "memory", Schema: json.RawMessage(structuredScalarSchema)}, nil)
	facts, err := v.consumer.facts(testContext(t), v.base.raw, request.Consumer.OperationID)
	requireTextRuntime(t, err)
	// Retain the original private actor/owner values. The stored stable proof
	// is a comparison projection, not a constructor for authority-bearing ports.
	original := mc.ResolveRequest{Actor: request.Actor, Consumer: request.Consumer, Purpose: request.Consumer.Purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MemorySelector}, LeaseOwner: request.Model.LeaseOwner}
	observed, found, err := v.base.service.LookupResolution(testContext(t), original)
	if err != nil || !found || observed.Snapshot.ID != request.Model.Snapshot.ID || observed.CredentialLease.LeaseID != request.Model.CredentialLease.LeaseID {
		t.Fatal("read-only resolution confirmation", err)
	}
	// Invalid input never creates an admitted owner, but its previously acquired
	// lease still belongs to the consumer until the formal terminal transaction.
	bad := request.Clone()
	bad.Input.Digest = resolutionDigest("changed")
	handle, err := v.core.BeginChat(testContext(t), bad)
	if err == nil || handle != nil {
		t.Fatal("invalid digest entered Runtime")
	}
	_, err = v.base.raw.Exec(testContext(t), `UPDATE model_runtime_fixture.operations SET enabled=false,version=version+1 WHERE id=$1`, request.Consumer.OperationID)
	requireTextRuntime(t, err)
	retirement := mc.ModelCallRetirementRequest{Actor: v.base.owner, Model: request.Model, TerminalVersion: f.Version(facts.Version + 1)}
	plan, err := v.retirement.DiscoverModelCallRetirement(testContext(t), retirement)
	requireTextRuntime(t, err)
	result := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		return v.retirement.ReleaseModelCallInTx(ctx, tx, retirement, plan)
	})
	if result.State() != f.Committed {
		t.Fatal("pre-admission lease release", result.Fault())
	}
	var released bool
	result = v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		released, err = v.retirement.ModelCallRetiredInTx(ctx, tx, retirement, plan)
		return err
	})
	if result.State() != f.Committed || !released {
		t.Fatal("read-only release confirmation", result.Fault())
	}
	var calls, leases int
	err = v.base.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_model.calls WHERE id=$1),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$2 AND released)`, request.CallID.String(), request.Model.CredentialLease.LeaseID.String()).Scan(&calls, &leases)
	requireTextRuntime(t, err)
	if calls != 0 || leases != 1 || len(v.wire.state(t, key).Requests) != 0 {
		t.Fatal("cleanup dispatched or lost its lease")
	}
}
