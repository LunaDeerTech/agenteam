package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const credentialProject = "01970000-0000-7000-8000-000000000010"
const credentialID = "01970000-0000-7000-8000-000000000011"
const credentialCollectionPath = "/api/v1/projects/" + credentialProject + "/mcp/credentials"
const credentialLookupPath = "/api/v1/projects/" + credentialProject + "/mcp/credential-commands/lookup"

type credentialBoundaryProbe struct {
	actor  id.Actor
	denied error
}

func (b credentialBoundaryProbe) CheckRequest(http.ResponseWriter, *http.Request) error {
	return b.denied
}
func (b credentialBoundaryProbe) RequireHuman(*http.Request) (id.Actor, error) { return b.actor, nil }
func (credentialBoundaryProbe) WriteProblem(http.ResponseWriter, *http.Request, error) {
	panic("unused wire probe")
}

type credentialWriteProbe struct {
	calls    int
	purpose  sc.Purpose
	material sc.SecretMaterial
	failure  error
	lookup   sc.WriteCommandLookupRequest
}

func (s *credentialWriteProbe) ExecuteWrite(_ context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	s.calls++
	s.material = r.Value
	if r.Purpose != sc.MCP || r.Scope.Details().ProjectID != credentialProject || len(r.Identity.OwnerIDs()) != 2 || r.Identity.OwnerIDs()[1] != r.Actor.Details().UserID {
		return sc.MutationResult{}, fault(f.Forbidden)
	}
	if s.failure != nil {
		return sc.MutationResult{}, s.failure
	}
	credential, _ := f.ParseID[sc.Credential](credentialID)
	ref, _ := sc.NewCredentialRef(credential, r.Scope)
	return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: sc.MCP, Version: r.ExpectedVersion + 1}, Deleted: r.Kind == sc.Delete}, nil
}
func (s *credentialWriteProbe) Metadata(_ context.Context, _ id.Actor, ref sc.CredentialRef) (sc.Metadata, error) {
	s.calls++
	return sc.Metadata{CredentialRef: ref, Purpose: s.purpose, Version: 1}, nil
}
func (s *credentialWriteProbe) LookupWriteCommand(_ context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	s.calls++
	s.lookup = r
	if r.Validate() != nil {
		return sc.WriteCommandObservation{}, fault(f.InvalidArgument)
	}
	return sc.WriteCommandObservation{}, nil
}
func credentialProbe(t *testing.T) (*projectCredentialHTTP, *credentialWriteProbe) {
	t.Helper()
	u, _ := f.ParseID[id.User]("01970000-0000-7000-8000-000000000001")
	session, _ := f.ParseID[id.Session]("01970000-0000-7000-8000-000000000002")
	actor, err := id.NewHuman(u, session)
	if err != nil {
		t.Fatal("actor fixture")
	}
	writes := &credentialWriteProbe{purpose: sc.MCP}
	return &projectCredentialHTTP{writes, credentialBoundaryProbe{actor: actor}}, writes
}
func credentialRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "mcp-credential:original")
	return r
}

func TestMCPCredentialPrivateMaterialAndFixedPurpose(t *testing.T) {
	h, s := credentialProbe(t)
	raw, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, credentialCollectionPath, `{"value":"owned-private-material"}`))
	if err != nil || s.calls != 1 || strings.Contains(string(raw), "owned-private-material") {
		t.Fatal("fixed MCP create projection failed")
	}
	if err = s.material.Use(func([]byte) error { return nil }); err == nil {
		t.Fatal("request material outlived original service call")
	}
	var result httpCredentialResult
	if json.Unmarshal(raw, &result) != nil || result.Purpose != sc.MCP || result.Version != 1 || result.Deleted {
		t.Fatal("invalid metadata receipt")
	}
	var value httpCredentialValue
	if json.Unmarshal([]byte(`"owned-private-material"`), &value) != nil {
		t.Fatal("material fixture")
	}
	encoded, _ := json.Marshal(value)
	if strings.Contains(string(encoded), "owned-private-material") || strings.Contains(fmt.Sprintf("%+v", value), "owned-private-material") {
		t.Fatal("material exposed by formatting")
	}
}
func TestMCPCredentialRejectsForeignPurposeAndLookupNeverWrites(t *testing.T) {
	h, s := credentialProbe(t)
	s.purpose = sc.Model
	_, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodGet, credentialCollectionPath+"/"+credentialID, ""))
	var failure *f.Fault
	if !errors.As(err, &failure) || failure.Code != f.NotFound {
		t.Fatal("foreign purpose metadata exposed")
	}
	s.calls = 0
	raw, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, credentialLookupPath, `{"kind":"create"}`))
	if err != nil || s.calls != 1 || s.lookup.Purpose != sc.MCP || string(raw) != `{"observed":false,"result":null}` {
		t.Fatal("MCP original lookup did not remain read-only")
	}
}
func TestMCPCredentialRejectsUnboundedOrAmbiguousInputs(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"POST", credentialCollectionPath, `{"value":"a","purpose":"model"}`},
		{"POST", credentialCollectionPath, `{"value":"a","value":"b"}`},
		{"POST", credentialCollectionPath, `{"value":null}`},
		{"POST", credentialCollectionPath, `{"value":""}`},
		{"GET", credentialCollectionPath, ""},
		{"POST", credentialLookupPath, `{"kind":"create","value":"a"}`},
		{"PATCH", credentialCollectionPath + "/" + credentialID, `{"expected_version":1,"value":"a"}`},
		{"PATCH", credentialCollectionPath + "/" + credentialID, `{"expected_version":"9223372036854775807","value":"a"}`},
		{"POST", credentialCollectionPath + "?purpose=model", `{"value":"a"}`},
	} {
		h, s := credentialProbe(t)
		_, err := h.execute(httptest.NewRecorder(), credentialRequest(tc.method, tc.path, tc.body))
		if err == nil || s.calls != 0 {
			t.Fatal("invalid input reached credential capability")
		}
	}
	for _, path := range []string{"/api/v1/system/mcp/credentials", "/api/v1/projects/p/x/mcp/credentials", credentialCollectionPath + "/id/extra", "/api/v1/projects/p/model-credentials"} {
		if HandlesCredentialPath(path) {
			t.Fatal("credential path escaped exact MCP project surface")
		}
	}
}
func TestMCPCredentialRetainsActualUnknownAndChecksBoundaryFirst(t *testing.T) {
	h, s := credentialProbe(t)
	s.failure = f.NewFault(f.CommitUnknown, f.Unknown)
	_, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, credentialCollectionPath, `{"value":"owned-private-material"}`))
	if err != s.failure || s.calls != 1 {
		t.Fatal("original Unknown was replaced or retried")
	}
	if s.material.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("unknown retained plaintext")
	}
	h, s = credentialProbe(t)
	h.boundary = credentialBoundaryProbe{denied: fault(f.Forbidden)}
	_, err = h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, credentialCollectionPath, `{"value":"owned-private-material"}`))
	if err == nil || s.calls != 0 {
		t.Fatal("browser boundary rejection bypassed")
	}
}
