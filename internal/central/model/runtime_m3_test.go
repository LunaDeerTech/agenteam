package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestRuntimeStructuredDigestAndPreparation(t *testing.T) {
	r := runtimePureRequest(t)
	format := mc.ResponseFormat{Kind: "json_schema", Name: "summary", Schema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`)}
	r.ResponseFormat = format
	r.Model.Snapshot.Identity.AdapterRevision = wire.OpenAIChatStructuredRevision
	r.Model.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
	digest, err := StructuredInputDigest(r.Messages, format)
	if err != nil {
		t.Fatal(err)
	}
	r.Input.Digest = digest
	// PostgreSQL JSONB reorders object keys and adds insignificant whitespace.
	// The same durable schema must keep its identity after that round trip.
	reordered := format.Clone()
	reordered.Schema = json.RawMessage(`{"required": ["summary"], "properties": {"summary": {"type": "string"}}, "type": "object", "additionalProperties": false}`)
	if got, err := StructuredInputDigest(r.Messages, reordered); err != nil || got != digest {
		t.Fatal("JSONB schema ordering changed input identity", err)
	}
	for _, mode := range []wire.ResponseMode{wire.JSONResponse, wire.SSEResponse} {
		copy, err := prepareRuntimeInput(context.Background(), r, mode)
		if err != nil || copy.Input.Digest != digest {
			t.Fatal("structured runtime preparation", err)
		}
		copy.ResponseFormat.Schema[0] = ' '
		if r.ResponseFormat.Schema[0] != '{' {
			t.Fatal("schema aliases")
		}
	}
	r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","properties":{"changed":{"type":"string"}},"required":["changed"],"additionalProperties":false}`)
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err == nil {
		t.Fatal("changed schema accepted with prior digest")
	}
	r.ResponseFormat = mc.ResponseFormat{Kind: "text"}
	r.Input.Digest, err = TextInputDigest(r.Messages)
	if err != nil {
		t.Fatal(err)
	}
	same, err := StructuredInputDigest(r.Messages, r.ResponseFormat)
	if err != nil || same != r.Input.Digest {
		t.Fatal("text digest compatibility")
	}
	if _, err = prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err != nil {
		t.Fatal("structured snapshot reflection", err)
	}
}

func TestRuntimeEmbeddingDigestAndPreparation(t *testing.T) {
	chat := runtimePureRequest(t)
	c := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: chat.Consumer.ProjectID, OperationID: chat.Consumer.OperationID, Purpose: mc.KnowledgeEmbedding}
	m := chat.Model.Clone()
	m.Consumer = c
	m.Snapshot.Identity.Protocol, m.Snapshot.Identity.Profile, m.Snapshot.Identity.ModelType, m.Snapshot.Identity.AdapterRevision = mc.OpenAIEmbeddings, mc.OpenAIEmbeddingsV1, mc.EmbeddingModel, wire.OpenAIEmbeddingsFloatRevision
	m.Snapshot.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}, Embedding: &mc.EmbeddingCapabilities{Dimensions: 3, DimensionsMode: mc.EmbeddingFixedDimensions}}
	r := mc.EmbeddingRequest{NonchatCall: mc.NonchatCall{Actor: chat.Actor, CallID: chat.CallID, Consumer: c, Model: m, Input: chat.Input}, Texts: []string{"first", "second"}, ExpectedDimensions: 3}
	var err error
	r.Input.Digest, err = EmbeddingInputDigest(r.Texts, r.ExpectedDimensions)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := prepareRuntimeEmbedding(context.Background(), r)
	if err != nil {
		t.Fatal("embedding preparation", err)
	}
	copy.Texts[0] = "changed"
	if r.Texts[0] != "first" {
		t.Fatal("texts alias")
	}
	changed := r.Clone()
	changed.ExpectedDimensions++
	if _, err = prepareRuntimeEmbedding(context.Background(), changed); err == nil {
		t.Fatal("changed dimensions accepted")
	}
	changed = r.Clone()
	changed.Texts[0], changed.Texts[1] = changed.Texts[1], changed.Texts[0]
	if _, err = prepareRuntimeEmbedding(context.Background(), changed); err == nil {
		t.Fatal("changed order accepted")
	}
	for _, texts := range [][]string{nil, {""}, {"nul\x00"}, {strings.Repeat("x", (1<<20)+1)}, {string([]byte{0xff})}} {
		if _, err = EmbeddingInputDigest(texts, 3); err == nil {
			t.Fatal("invalid embedding text accepted")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = prepareRuntimeEmbedding(cancelled, r); err == nil {
		t.Fatal("cancelled embedding accepted")
	}
}

func TestServingResolutionIdentityIncludesGeneration(t *testing.T) {
	r := platformEmbeddingTestRequest(t, mc.KnowledgeEmbedding)
	snapshot := mustID[mc.Snapshot](t)
	r.Source, r.Selection, r.ServingSnapshotID, r.ServingGenerationID = mc.ServingSnapshotSource, nil, &snapshot, mustID[struct{}](t).String()
	if err := resolutionVariant(r); err != nil {
		t.Fatal(err)
	}
	if err := resolutionRequest(r).validate(); err != nil {
		t.Fatal("durable serving request", err)
	}
	first := resolutionSemantic(r)
	r.ServingGenerationID = mustID[struct{}](t).String()
	if resolutionSemantic(r) == first {
		t.Fatal("generation not included in semantic")
	}
	r.ServingSnapshotID = nil
	if resolutionRequest(r).validate() == nil {
		t.Fatal("missing serving identity accepted")
	}
}
