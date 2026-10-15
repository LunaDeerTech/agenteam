package artifacthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type metadataTestReader struct {
	result      ac.Metadata
	err         error
	invocations []ac.Invocation
	artifacts   []ac.ArtifactID
}

func (p *metadataTestReader) ReadArtifactMetadata(_ context.Context, v ac.Invocation, artifact ac.ArtifactID) (ac.Metadata, error) {
	p.invocations, p.artifacts = append(p.invocations, v), append(p.artifacts, artifact)
	return p.result, p.err
}
func metadataTestPath() string { return testPath(false) + "/" + testReference().ArtifactID.String() }

func TestArtifactMetadataHTTPGETHEADOriginalReference(t *testing.T) {
	p, boundary := &metadataTestReader{result: testMetadata()}, &testBoundary{}
	h := &handler{metadata: p, boundary: boundary}
	var encoded string
	for _, method := range []string{"GET", "HEAD"} {
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest(method, metadataTestPath(), nil), w) || w.Code != 200 {
			t.Fatal("metadata read failed", w.Code, boundary.problem)
		}
		if method == "GET" {
			encoded = w.Body.String()
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(len(encoded)) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("metadata representation headers")
		}
		w.cleared(t, true)
	}
	var out struct {
		Artifact struct {
			Reference referenceDTO `json:"artifact_ref"`
		} `json:"artifact"`
	}
	if json.Unmarshal([]byte(encoded), &out) != nil || out.Artifact.Reference != (referenceDTO{testReference().ProjectID, testReference().ArtifactID, testReference().FileID}) || strings.Contains(encoded, testID[oc.StoredObject](999).String()) || strings.Contains(encoded, "object_id") {
		t.Fatal("metadata lost canonical ref or exposed storage identity")
	}
	if len(p.invocations) != 2 || boundary.checks != 2 || boundary.auths != 2 || p.artifacts[0] != testReference().ArtifactID || p.artifacts[1] != testReference().ArtifactID {
		t.Fatal("metadata bypassed original boundary or changed identity")
	}
	a, b := p.invocations[0].Details(), p.invocations[1].Details()
	if !a.Actor.Equal(testActor()) || a.ProjectID != testReference().ProjectID || a.AttemptID == b.AttemptID || a.ExecutionID != "" || a.OperationID != "" || a.ToolID != "" || a.ToolCallID != "" {
		t.Fatal("metadata borrowed caller provenance or an old attempt")
	}
}

func TestArtifactMetadataHTTPRejectsInputAndForeignProjection(t *testing.T) {
	for _, mode := range []string{"query", "empty-query", "body", "range", "method", "bad-id", "session", "owner", "foreign-project", "foreign-artifact", "empty-metadata"} {
		t.Run(mode, func(t *testing.T) {
			p, boundary := &metadataTestReader{result: testMetadata()}, &testBoundary{}
			h := &handler{metadata: p, boundary: boundary}
			path, method, body := metadataTestPath(), "GET", ""
			status, calls := 400, 0
			switch mode {
			case "query":
				path += "?file_id=" + testReference().FileID.String()
			case "empty-query":
				path += "?"
			case "body":
				body = "{}"
			case "range":
			case "method":
				method, status = "POST", 405
			case "bad-id":
				path = testPath(false) + "/not-an-id"
			case "session":
				boundary.auth, status = f.NewFault(f.Unauthenticated, f.NotStarted), 401
			case "owner":
				p.err, status, calls = f.NewFault(f.NotFound, f.NotCommitted), 404, 1
			case "foreign-project", "foreign-artifact":
				m := testMetadata().Details()
				if mode == "foreign-project" {
					m.Reference.ProjectID = testID[id.Project](300)
					m.Object.Scope, _ = id.InProject(m.Reference.ProjectID)
				} else {
					m.Reference.ArtifactID = testID[ac.ArtifactEntity](301)
				}
				var err error
				p.result, err = ac.NewMetadata(m)
				if err != nil {
					t.Fatal(err)
				}
				status, calls = 503, 1
			case "empty-metadata":
				p.result, status, calls = ac.Metadata{}, 503, 1
			}
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			if mode == "range" {
				r.Header.Set("Range", "bytes=0-1")
			}
			w := newTestWriter()
			if serveTest(h, r, w) || w.Code != status || len(p.invocations) != calls || strings.Contains(w.Body.String(), "artifact_ref") {
				t.Fatal("invalid input/result escaped", w.Code, status, len(p.invocations), calls, boundary.problem)
			}
			if mode == "method" && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("metadata method contract changed")
			}
		})
	}
	if !HandlesPath(metadataTestPath()) || HandlesPath(metadataTestPath()+"/") {
		t.Fatal("metadata route was not exact")
	}
}

var _ http.Handler = (*handler)(nil)
