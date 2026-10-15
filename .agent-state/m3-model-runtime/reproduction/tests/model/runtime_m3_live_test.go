//go:build integration

package model_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type m3EmbeddingExportRow struct {
	Dataset   string `json:"dataset_revision"`
	Source    string `json:"source_id"`
	Query     string `json:"query_id"`
	IndexText string `json:"index_text"`
	Text      string `json:"text"`
}
type m3EmbeddingVector struct {
	Source string    `json:"source_id,omitempty"`
	Query  string    `json:"query_id,omitempty"`
	Values []float64 `json:"embedding"`
}

func m3ReadEmbeddingExport(t *testing.T, path string, count int) ([]m3EmbeddingExportRow, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	requireTextRuntime(t, err)
	if len(raw) > 4<<20 {
		t.Fatal("embedding export exceeds bound")
	}
	digest := sha256.Sum256(raw)
	file, err := os.Open(path)
	requireTextRuntime(t, err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var rows []m3EmbeddingExportRow
	for scanner.Scan() {
		var row m3EmbeddingExportRow
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Dataset == "" || row.Source == "" && row.Query == "" {
			t.Fatal("invalid unlabelled export")
		}
		rows = append(rows, row)
	}
	requireTextRuntime(t, scanner.Err())
	if len(rows) != count {
		t.Fatal("fixed embedding corpus count mismatch")
	}
	return rows, hex.EncodeToString(digest[:])
}

func m3LiveRuntimeModel(t *testing.T, v *textRuntimeFixture, kind string, embedding bool) mc.ModelID {
	t.Helper()
	base, key, name := os.Getenv("DEV_"+kind+"_BASE_URL"), os.Getenv("DEV_"+kind+"_API_KEY"), os.Getenv("DEV_"+kind+"_MODEL")
	if base == "" || key == "" || name == "" {
		t.Fatal("authorized model environment unavailable")
	}
	material, err := sc.NewSecretMaterial([]byte(key))
	requireTextRuntime(t, err)
	defer material.Destroy()
	identity, err := f.NewCommandIdentity("secret", []string{v.base.admin.Details().UserID}, string(sc.Create), f.IdempotencyKey(newID[struct{}](t).String()))
	requireTextRuntime(t, err)
	credential, err := v.base.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: v.base.admin, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	requireTextRuntime(t, err)
	provider := projectProviderInput()
	provider.Name = "M3 authorized " + kind
	provider.BaseURL = base
	provider.CredentialRef = &credential.Metadata.CredentialRef
	if embedding {
		provider.Protocol = mc.OpenAIEmbeddings
	}
	p, err := v.base.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), Input: provider})
	requireTextRuntime(t, err)
	pid, _ := f.ParseID[mc.Provider](p.ResourceID)
	input := projectModelInput()
	input.Name = "M3 " + kind
	input.ProviderModelID = name
	input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, StructuredOutputModes: []string{"text", "json_schema"}, Streaming: true}
	max := mc.TokenCount(128)
	input.Capabilities.MaxOutput = &max
	if embedding {
		input.Type = mc.EmbeddingModel
		input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}
	}
	result, err := v.base.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ProviderID: pid, Input: input})
	requireTextRuntime(t, err)
	mid, err := f.ParseID[mc.Model](result.ResourceID)
	requireTextRuntime(t, err)
	return mid
}

// This opt-in accepts only the explicitly supplied DEV model origins. Every
// dispatch uses real Model/Secret/Usage/outbound/TLS and the original handles;
// the synthetic fixture owns only benchmark consumer facts, not domain proof.
func TestModelM3AuthorizedRuntimeEmbeddings(t *testing.T) {
	if os.Getenv("AGENTEAM_M3_LIVE_RUNTIME") != "1" {
		t.Skip("explicit live Runtime opt-in required")
	}
	inputRoot, outputRoot := os.Getenv("AGENTEAM_M3_EMBEDDING_EXPORT"), os.Getenv("AGENTEAM_M3_EMBEDDING_OUTPUT")
	if !filepath.IsAbs(inputRoot) || !filepath.IsAbs(outputRoot) {
		t.Fatal("absolute owned export/output required")
	}
	corpus, corpusDigest := m3ReadEmbeddingExport(t, filepath.Join(inputRoot, "corpus.jsonl"), 64)
	queries, queriesDigest := m3ReadEmbeddingExport(t, filepath.Join(inputRoot, "queries.jsonl"), 72)
	var origins []outbound.Origin
	seenOrigins := map[string]bool{}
	var embeddingOrigin outbound.Origin
	for _, kind := range []string{"CHAT", "EMBEDDING"} {
		target, err := outbound.ParseTarget(os.Getenv("DEV_" + kind + "_BASE_URL"))
		requireTextRuntime(t, err)
		if target.Origin().Scheme() != "https" {
			t.Fatal("authorized TLS origin required")
		}
		origin := target.Origin()
		if kind == "EMBEDDING" {
			embeddingOrigin = origin
		}
		if !seenOrigins[origin.String()] {
			origins = append(origins, origin)
			seenOrigins[origin.String()] = true
		}
	}
	routes, err := outbound.NewEnvironmentProxyRoutes(origins)
	requireTextRuntime(t, err)
	trust, err := outbound.LoadTrustStore("")
	requireTextRuntime(t, err)
	resolver, err := outbound.SystemResolver()
	requireTextRuntime(t, err)
	v := newTextRuntimeFixtureWithTransport(t, nil, nil, func(transport wire.Transport) wire.Transport {
		transport.Trust, transport.Resolver, transport.ProxyRoutes = trust, resolver, routes
		return transport
	})
	em, chat := m3LiveRuntimeModel(t, v, "EMBEDDING", true), m3LiveRuntimeModel(t, v, "CHAT", false)
	m3RuntimeSelection(t, v, em, chat)
	type callEvidence struct {
		CallID       string `json:"call_id"`
		InvocationID string `json:"invocation_id"`
		InputDigest  string `json:"input_digest"`
		Items        int    `json:"items"`
	}
	var calls []callEvidence
	var snapshot *mc.ConfigSnapshot
	var corpusVectors, queryVectors []m3EmbeddingVector
	groups := [][]m3EmbeddingExportRow{corpus, queries[:64], queries[64:]}
	for group, rows := range groups {
		texts := make([]string, len(rows))
		for i, row := range rows {
			texts[i] = row.IndexText
			if texts[i] == "" {
				texts[i] = row.Text
			}
		}
		var serving *mc.SnapshotID
		if snapshot != nil {
			serving = &snapshot.ID
		}
		_, request := m3RuntimeRequestInput(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, serving, texts, 4096)
		if snapshot == nil {
			copy := request.Model.Snapshot.Clone()
			snapshot = &copy
		} else if request.Model.Snapshot.ID != snapshot.ID {
			t.Fatal("embedding snapshot changed across batches")
		}
		ctx, cancel := context.WithTimeout(testContext(t), 30*time.Second)
		handle, err := v.core.BeginEmbed(ctx, request)
		if handle == nil {
			cancel()
			t.Fatal("authorized embedding admission failed", err)
		}
		if err != nil {
			cancel()
			closeCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			closeErr := handle.Close(closeCtx)
			stop()
			t.Fatalf("authorized embedding start failed: %v cleanup=%v joined=%t", err, closeErr, handle.Joined())
		}
		result, callErr := handle.Result(ctx)
		cancel()
		closeCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		closeErr := handle.Close(closeCtx)
		stop()
		if callErr != nil || closeErr != nil || !handle.Joined() || result.ValidateFor(len(rows), 4096) != nil {
			var modelError *mc.ModelError
			if errors.As(callErr, &modelError) {
				t.Logf("authorized Runtime failure category=%s code=%s dispatched=%t retryable=%t partial_output=%t", modelError.Category, modelError.Code, modelError.Dispatched, modelError.Retryable, modelError.PartialOutput)
			}
			t.Fatalf("authorized Runtime batch failed: call=%v cleanup=%v joined=%t", callErr, closeErr, handle.Joined())
		}
		m3LiveTerminal(t, v, request.CallID)
		vectors := make([]m3EmbeddingVector, len(rows))
		for _, item := range result.Items {
			row := rows[item.Index]
			vectors[item.Index] = m3EmbeddingVector{Source: row.Source, Query: row.Query, Values: item.Values}
		}
		if group == 0 {
			corpusVectors = vectors
		} else {
			queryVectors = append(queryVectors, vectors...)
		}
		calls = append(calls, callEvidence{request.CallID.String(), result.InvocationID.String(), request.Input.Digest.String(), len(rows)})
		t.Logf("authorized Runtime embedding batch=%d items=%d dimensions=4096 http_status=200 joined=true", group+1, len(rows))
	}
	requireTextRuntime(t, os.MkdirAll(outputRoot, 0700))
	dir, err := os.MkdirTemp(outputRoot, "runtime-embeddings-")
	requireTextRuntime(t, err)
	proxy, err := routes.UsesProxy(embeddingOrigin)
	requireTextRuntime(t, err)
	result := struct {
		Snapshot                      map[string]any      `json:"snapshot"`
		Corpus                        []m3EmbeddingVector `json:"corpus"`
		Queries                       []m3EmbeddingVector `json:"queries"`
		Inputs                        map[string]string   `json:"inputs_sha256"`
		Calls                         []callEvidence      `json:"calls"`
		TLS, Proxy                    bool
		ProviderResponseModelObserved bool `json:"provider_response_model_observed"`
	}{
		map[string]any{"id": snapshot.ID.String(), "provider": snapshot.Identity.ProviderID.String(), "model": snapshot.Identity.ProviderModelID, "dimensions": 4096, "adapter_revision": snapshot.Identity.AdapterRevision}, corpusVectors, queryVectors, map[string]string{"corpus.jsonl": corpusDigest, "queries.jsonl": queriesDigest}, calls, true, proxy, false}
	raw, err := json.Marshal(result)
	requireTextRuntime(t, err)
	requireTextRuntime(t, os.WriteFile(filepath.Join(dir, "vectors.json"), raw, 0600))
	v.core.StopAdmission()
	requireTextRuntime(t, v.core.Drain(testContext(t)))
	if !v.core.Joined() {
		t.Fatal("authorized Runtime not joined")
	}
	t.Log("authorized Runtime vectors:", filepath.Join(dir, "vectors.json"))
}

func m3LiveTerminal(t *testing.T, v *textRuntimeFixture, call mc.CallID) {
	t.Helper()
	var ok bool
	err := v.base.raw.QueryRow(testContext(t), `SELECT c.phase='succeeded' AND c.retired AND l.released AND i.final_status='succeeded' AND i.dispatch='sent' AND (SELECT count(*) FROM agenteam_model.runtime_attempts WHERE call_id=c.id)=1 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls WHERE call_id=c.id AND phase<>'business_closed') FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call.String()).Scan(&ok)
	requireTextRuntime(t, err)
	if !ok {
		t.Fatal("authorized Runtime ledger/lease/Usage ownership not terminal")
	}
	v.reader.mu.Lock()
	defer v.reader.mu.Unlock()
	for _, material := range v.reader.materials {
		used := false
		if err := material.Use(func([]byte) error { used = true; return nil }); err == nil || used {
			t.Fatal("authorized credential not destroyed")
		}
	}
}
