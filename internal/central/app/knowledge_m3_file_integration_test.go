//go:build integration && linux

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func knowledgeM3PDFEnvironment(base []string) []string {
	return append(base,
		"AGENTEAM_CENTRAL_KNOWLEDGE_PDFTOTEXT_PATH=/usr/bin/pdftotext",
		"AGENTEAM_CENTRAL_KNOWLEDGE_PDFTOTEXT_SHA256=7fe3559cac3ba61dd93650a1ba90499b9fc488184ef8d0f2f1305ddf52c0378b",
		"AGENTEAM_CENTRAL_KNOWLEDGE_PRLIMIT_PATH=/usr/bin/prlimit",
		"AGENTEAM_CENTRAL_KNOWLEDGE_PRLIMIT_SHA256=06275e616ca2b721482aeb564260489be3fdc6d5f86836b4a78e96485845f060")
}

// Validate the actual generated file bytes before acquiring a PG fixture. The
// PDF explicitly declares its encoding; the DOCX is a complete OPC package.
func TestKnowledgeM3FileParserFixtures(t *testing.T) {
	ctx := context.Background()
	pdf, err := document.NewPoppler(ctx, document.PopplerConfig{
		PDFToTextPath: "/usr/bin/pdftotext", PDFToTextSHA256: "7fe3559cac3ba61dd93650a1ba90499b9fc488184ef8d0f2f1305ddf52c0378b",
		PrlimitPath: "/usr/bin/prlimit", PrlimitSHA256: "06275e616ca2b721482aeb564260489be3fdc6d5f86836b4a78e96485845f060",
	})
	workAppCheck(t, "knowledge-files-native-parser", err)
	registry, err := document.NewRegistry(pdf)
	workAppCheck(t, "knowledge-files-native-registry", err)
	source := parser.SourceIdentity{ProjectID: guardID[id.Project](t), DocumentID: guardID[kc.Document](t), ContentVersion: 1, ObjectID: guardID[oc.StoredObject](t)}
	for _, media := range []string{kc.PDF, kc.DOCX} {
		text := "Mercury's orbital period is 88 Earth days."
		raw, expected, page := knowledgeM3DOCX(t, text), text, 0
		if media == kc.PDF {
			raw, expected, page = knowledgeM3PDF(text), text+"\n\n\f", 1
		}
		parsed, err := registry.ParseBytes(ctx, source, "Public file fixture", media, document.PopplerRegistryProfile, raw)
		workAppCheck(t, "knowledge-files-native-parse", err)
		if parsed.Text != expected {
			t.Fatalf("fixture projection differs: media=%s actual_bytes=%d expected_bytes=%d", media, len(parsed.Text), len(expected))
		}
		chunks, err := chunker.Split(ctx, parsed, chunker.Config{SoftBytes: 2048, HardBytes: 4096, ForcedOverlapBytes: 256})
		workAppCheck(t, "knowledge-files-native-chunks", err)
		if len(chunks) != 1 || chunks[0].RawText != text || chunks[0].Locator.Page != page || chunks[0].Locator.ByteStart != 0 || chunks[0].Locator.ByteEnd != len(text) {
			t.Fatal("fixture exact chunk/page differs")
		}
	}
}

// All document mutations enter the original Owner HTTP boundary. No Rebuild,
// RunPending, direct index insert or manual execution Advance is used.
func TestKnowledgeM3AppOwnerFiles(t *testing.T) {
	knowledgeM3OwnerFiles(t, nil)
}

func knowledgeM3OwnerFiles(t *testing.T, browser *knowledgeM3FilesWeb) {
	t.Helper()
	network, err := netfixture.Load()
	workAppCheck(t, "knowledge-files-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "knowledge-files-address", err)
	var transport *outboundRuntime
	trace := &knowledgeM3CaptureTrace{}
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		origin := "http://localhost:8080"
		if browser != nil {
			origin = browser.origin
		}
		v.root.cfg = knowledgeM3RetentionConfiguration(t, knowledgeM3PDFEnvironment(guardEnvironment(t, v.root.db)), network.CAFile, origin)
		v.store.observeCommit = trace.observe
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("original outbound missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents.index == nil {
		t.Fatal("original index missing")
	}
	defer func() {
		if t.Failed() {
			trace.diagnose(t)
			knowledgeM3IndexDiagnostic(t, v, nil)
			knowledgeM3AutomaticDiagnostic(t, v)
		}
	}()
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "knowledge-retention", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	embeddingCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	workAppCheck(t, "knowledge-files-embedding-peer", err)
	_, credential := knowledgeM3RetentionModels(t, v, embeddingCase)
	var pdf kc.DocumentRef
	const revised = "Mercury's orbital period is 88 Earth days."
	if browser != nil {
		pdf = browser.documents(t, v, documents, credential)
	} else {
		for _, seed := range []struct {
			title, media, text string
			raw                []byte
			page               int
		}{
			{"Venus.txt", "text/plain", "Venus turns slowly.", []byte("Venus turns slowly."), 0},
			{"Earth.md", "text/markdown", "Earth has one moon.", []byte("Earth has one moon."), 0},
			{"Mars.docx", kc.DOCX, "Mars has two moons.", knowledgeM3DOCX(t, "Mars has two moons."), 0},
			{"Mercury.pdf", "application/pdf", "Mercury's orbital period is 87 Earth days.", knowledgeM3PDF("Mercury's orbital period is 87 Earth days."), 1},
		} {
			target := guardID[kc.Document](t)
			receipt := knowledgeM3FileCall(t, v, http.MethodPost, target.String()+"/create-file", map[string]any{"parent_document_id": nil, "title": seed.title, "media_type": seed.media, "bytes_base64": base64.StdEncoding.EncodeToString(seed.raw)}, http.StatusOK)
			var decoded struct {
				Document struct {
					ID      kc.DocumentID `json:"id"`
					Version f.Version     `json:"content_version"`
					Media   string        `json:"media_type"`
				} `json:"document"`
			}
			workAppCheck(t, "knowledge-file-create-wire", json.Unmarshal(receipt, &decoded))
			if decoded.Document.ID != target || decoded.Document.Version != 1 || decoded.Document.Media != seed.media || bytes.Contains(receipt, []byte("object_id")) {
				t.Fatal("file receipt lost target/version/media boundary")
			}
			head, err := documents.pair.Service().GetDocument(v.ctx, v.actor, v.a, target)
			workAppCheck(t, "knowledge-file-canonical-head", err)
			if head.Active == nil || head.Active.Title != seed.title {
				t.Fatal("HTTP write did not publish canonical document")
			}
			doc := *head.Active
			profile := knowledgeM3AwaitAutomatic(t, v, doc, "", seed.text)
			knowledgeM3RetentionFacts(t, v, profile, "active", "retained", credential)
			var parserProfile string
			workAppCheck(t, "knowledge-file-parser-profile", v.admin.QueryRow(v.ctx, `SELECT parser_profile FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, v.a.String(), profile).Scan(&parserProfile))
			if parserProfile != document.PopplerRegistryProfile {
				t.Fatal("actual App parser was not explicitly bound")
			}
			expected := seed.text
			if seed.page != 0 {
				expected += "\n\n\f"
				pdf = doc
			}
			knowledgeM3FileProjection(t, v, doc, seed.raw, expected, seed.page)
		}
		updated := knowledgeM3PDF(revised)
		knowledgeM3FileCall(t, v, http.MethodPost, pdf.ID.String()+"/replace-file", map[string]any{"expected_version": pdf.ContentVersion, "media_type": "application/pdf", "bytes_base64": base64.StdEncoding.EncodeToString(updated)}, http.StatusOK)
		old := pdf.ContentVersion
		head, err := documents.pair.Service().GetDocument(v.ctx, v.actor, v.a, pdf.ID)
		workAppCheck(t, "knowledge-file-replaced-head", err)
		if head.Active == nil || head.Active.ContentVersion != old+1 {
			t.Fatal("file replacement version did not advance")
		}
		pdf = *head.Active
		knowledgeM3AwaitAutomatic(t, v, pdf, "", revised)
		knowledgeM3FileProjection(t, v, pdf, updated, revised+"\n\n\f", 1)
		knowledgeM3FileCall(t, v, http.MethodGet, fmt.Sprintf("%s/revisions/%d/source", pdf.ID, old), nil, http.StatusConflict)
	}
	// Three controlled replies still traverse the real Model, Scheduler and
	// original Tool witnesses. The PDF page and byte locator remain assertions.
	tools := map[string]id.ToolID{}
	for _, key := range []string{"builtin:query-doc", "builtin:read-doc"} {
		var raw string
		workAppCheck(t, "knowledge-files-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tools[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "knowledge-files-tool-id", err)
	}
	sequence := []netfixture.WireScenario{
		knowledgeM3AutoToolReply(t, tools["builtin:query-doc"], map[string]any{"query": "Mercury orbital period", "limit": 5}, 1),
		knowledgeM3AutoToolReply(t, tools["builtin:read-doc"], map[string]any{"document_id": pdf.ID, "document_version": pdf.ContentVersion}, 2),
		knowledgeM3AutoReply(t, map[string]any{"role": "assistant", "content": "Mercury takes 88 Earth days. [document_id=" + pdf.ID.String() + "; document_version=2; page=1]"}, "stop", 3),
	}
	chatCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "knowledge-files-chat-peer", err)
	chatCredential := knowledgeM3RetentionCredential(t, v, "owned-knowledge-files-chat")
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Knowledge files Agent", "protocol": mc.OpenAIChat, "base_url": "https://fixture.test:8443/case/" + chatCase, "enabled": true, "credential_ref": chatCredential, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "knowledge-files-chat-provider", err)
	knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(knowledgeM3ModelInput(t, "agent", "knowledge-auto"))}, &receipt)
	agentModel, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "knowledge-files-chat-model", err)
	knowledgeM3AgentSources(t, v, agentModel, pdf, revised+"\n\n\f", knowledgeM3SourceLocation{Snippet: revised, Page: 1})
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "knowledge-files-app-return")
	knowledgeM3FileExitDiagnostic(t, v)
	workAppCheck(t, "knowledge-files-app-exit", v.root.err)
	if !documents.index.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("file original owners did not join")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	for scenario, want := range map[string]int{embeddingCase: 6, chatCase: 3} {
		peer, err := network.State(context.Background(), scenario)
		workAppCheck(t, "knowledge-files-peer-state", err)
		if len(peer.Requests) != want || peer.CompletedHandlers != want || peer.ActiveHandlers != 0 {
			t.Fatalf("file peer requests=%d completed=%d active=%d expected=%d", len(peer.Requests), peer.CompletedHandlers, peer.ActiveHandlers, want)
		}
	}
	t.Log("M3_KNOWLEDGE_FILES owner_http_four_formats=true automatic_index=true source_bytes=true current_projection=true pdf_replace=true pdf_page_agent_citation=true original_app_join=true controlled_model=true")
}

// Observe only after the original run.done publication. No errors are formatted:
// lifecycle/fault enums, context predicates and owner join flags are sufficient
// to distinguish an App drain failure from a retained concrete domain owner.
func knowledgeM3FileExitDiagnostic(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	knowledgeM3FileSafeError(t, "app", v.root.err)
	t.Logf("M3_KNOWLEDGE_FILE_EXIT runtime_joined=%t account_joined=%t producers_joined=%t", v.runtime.Joined(), v.assembly.Joined(), v.root.owned.producersJoined())
	for ordinal, owner := range v.assembly.works() {
		t.Logf("M3_KNOWLEDGE_FILE_ACCOUNT_OWNER ordinal=%d kind=%T joined=%t", ordinal, owner, owner.Joined())
	}
	owners, runs, constructing := v.runtime.snapshot()
	t.Logf("M3_KNOWLEDGE_FILE_RUNTIME constructing=%t owners=%d runs=%d", constructing, len(owners), len(runs))
	for ordinal, owner := range owners {
		t.Logf("M3_KNOWLEDGE_FILE_RUNTIME_OWNER ordinal=%d joined=%t", ordinal, owner.joined())
	}
	for ordinal, run := range runs {
		select {
		case <-run.done:
			knowledgeM3FileSafeError(t, fmt.Sprintf("runtime-run-%d", ordinal), run.err)
		default:
			t.Logf("M3_KNOWLEDGE_FILE_RUNTIME_RUN ordinal=%d returned=false", ordinal)
		}
	}
	manager := v.runtime.manager.Observation()
	t.Logf("M3_KNOWLEDGE_FILE_MANAGER started=%t stopped=%t projects=%d", manager.Started, manager.Stopped, len(manager.Projects))
	knowledgeM3FileSafeError(t, "manager-discovery", manager.DiscoveryError)
	for ordinal, project := range manager.Projects {
		t.Logf("M3_KNOWLEDGE_FILE_MANAGER_PROJECT ordinal=%d running=%t retained=%t stopping=%t", ordinal, project.Running, project.Retained, project.Stopping)
		knowledgeM3FileSafeError(t, fmt.Sprintf("manager-project-%d", ordinal), project.Err)
	}
}
func knowledgeM3FileSafeError(t *testing.T, stage string, err error) {
	t.Helper()
	var life *lifecycle.Failure
	var fault *f.Fault
	var lifecycleCode, faultCode, commitState string
	if errors.As(err, &life) {
		lifecycleCode = string(life.Code())
	}
	if errors.As(err, &fault) {
		faultCode, commitState = string(fault.Code.Safe()), string(fault.CommitState.Safe())
	}
	t.Logf("M3_KNOWLEDGE_FILE_SAFE_ERROR stage=%s present=%t lifecycle=%s fault=%s commit=%s canceled=%t deadline=%t", stage, err != nil, lifecycleCode, faultCode, commitState, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
}
func knowledgeM3FileCall(t *testing.T, v *executionSchedulerApp, method, suffix string, body any, status int) []byte {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/projects/"+v.a.String()+"/knowledge/documents/"+suffix, raw, guardID[struct{}](t).String())
	clear(raw)
	workAppCheck(t, "knowledge-file-http", err)
	if response.status != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("knowledge file status=%d expected=%d code=%s", response.status, status, problem.Code.Safe())
	}
	return response.body
}
func knowledgeM3FileProjection(t *testing.T, v *executionSchedulerApp, doc kc.DocumentRef, raw []byte, text string, page int) {
	t.Helper()
	var source struct {
		Bytes    string `json:"bytes_base64"`
		Document struct {
			Version f.Version `json:"content_version"`
		} `json:"document"`
	}
	body := knowledgeM3FileCall(t, v, http.MethodGet, fmt.Sprintf("%s/revisions/%d/source", doc.ID, doc.ContentVersion), nil, http.StatusOK)
	workAppCheck(t, "knowledge-file-source-wire", json.Unmarshal(body, &source))
	clear(body)
	decoded, err := base64.StdEncoding.Strict().DecodeString(source.Bytes)
	workAppCheck(t, "knowledge-file-source-bytes", err)
	if !bytes.Equal(decoded, raw) || source.Document.Version != doc.ContentVersion {
		t.Fatal("current source bytes changed")
	}
	clear(decoded)
	var content struct {
		Document struct {
			Version f.Version `json:"content_version"`
		} `json:"document"`
		Text *struct {
			Text      string `json:"text"`
			Truncated bool   `json:"truncated"`
		} `json:"text"`
	}
	body = knowledgeM3FileCall(t, v, http.MethodGet, doc.ID.String()+"/content?byte_offset=0&max_bytes=8192", nil, http.StatusOK)
	workAppCheck(t, "knowledge-file-projection-wire", json.Unmarshal(body, &content))
	clear(body)
	if content.Document.Version != doc.ContentVersion || content.Text == nil || content.Text.Text != text || content.Text.Truncated {
		t.Fatal("current parsed projection differs")
	}
	var locatorRaw []byte
	workAppCheck(t, "knowledge-file-locator", v.admin.QueryRow(v.ctx, `SELECT c.locator FROM agenteam_knowledge.index_chunks c JOIN agenteam_knowledge.index_generations g ON g.id=c.generation_id JOIN agenteam_knowledge.index_state st ON st.project_id=g.project_id AND st.serving_profile_id=g.profile_id WHERE g.project_id=$1 AND g.document_id=$2 AND g.document_version=$3 AND g.state='ready' ORDER BY c.ordinal LIMIT 1`, v.a.String(), doc.ID.String(), int64(doc.ContentVersion)).Scan(&locatorRaw))
	var locator chunker.Locator
	workAppCheck(t, "knowledge-file-locator-wire", json.Unmarshal(locatorRaw, &locator))
	if locator.Page != page || locator.ByteStart < 0 || locator.ByteEnd > len(text) || locator.ByteStart >= locator.ByteEnd {
		t.Fatal("file locator invented a page or lost current range")
	}
}
func knowledgeM3DOCX(t *testing.T, text string) []byte {
	t.Helper()
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	for _, entry := range []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`},
	} {
		part, err := w.Create(entry.name)
		workAppCheck(t, "knowledge-file-docx-entry", err)
		_, err = part.Write([]byte(entry.body))
		workAppCheck(t, "knowledge-file-docx-bytes", err)
	}
	workAppCheck(t, "knowledge-file-docx-close", w.Close())
	return raw.Bytes()
}
func knowledgeM3PDF(text string) []byte {
	stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET\n"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream)}
	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for n, obj := range objects {
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(out.String())
}
