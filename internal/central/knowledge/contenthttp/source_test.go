package contenthttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type sourceBody struct {
	io.Reader
	reads, closes int
	closeErr      error
}

func (b *sourceBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *sourceBody) Close() error               { b.closes++; return b.closeErr }

type sourcePort struct {
	*testPorts
	body  *sourceBody
	opens int
}

func (p *sourcePort) OpenCanonical(_ context.Context, a id.Actor, project id.ProjectID, document kc.DocumentID, span *oc.ByteRange) (kc.CanonicalRead, error) {
	p.opens++
	if !a.Equal(testActor()) || project != p.content.Document.ProjectID || document != p.content.Document.ID || span != nil {
		return kc.CanonicalRead{}, badProjection()
	}
	if p.err != nil {
		return kc.CanonicalRead{}, p.err
	}
	d := p.content.Document
	scope, err := id.InProject(d.ProjectID)
	if err != nil {
		return kc.CanonicalRead{}, err
	}
	r, err := oc.NewObjectReader(oc.ObjectMeta{ID: d.ObjectID, Scope: scope, MediaType: d.MediaType, ByteSize: 4, SHA256: f.Digest("sha256:" + strings.Repeat("0", 64)), State: oc.Available, Version: 1, CreatedAt: d.CreatedAt}, nil, p.body)
	if err != nil {
		return kc.CanonicalRead{}, err
	}
	return kc.NewCanonicalRead(d, r)
}
func sourcePath(version string) string {
	return strings.TrimSuffix(testPath(), "content") + "revisions/" + version + "/source"
}

func TestCanonicalSourceHTTPOriginalBytesAndClose(t *testing.T) {
	for _, media := range []string{kc.PlainText, kc.Markdown, kc.PDF, kc.DOCX} {
		h, _, p := testHandler()
		p.content.Document.MediaType = media
		if media == kc.PDF || media == kc.DOCX {
			p.content.Document.SourceKind = kc.File
		}
		raw := "\x00\xffexact\r\n"
		body := &sourceBody{Reader: strings.NewReader(raw)}
		source := &sourcePort{testPorts: p, body: body}
		h.reader = source
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", sourcePath("2"), nil), w) || w.Code != 200 || body.closes != 1 || source.opens != 1 {
			t.Fatal("source read did not complete original Close", media, w.Code)
		}
		var out struct {
			Document map[string]any `json:"document"`
			Bytes    string         `json:"bytes_base64"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("bad JSON")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(out.Bytes)
		if err != nil || string(decoded) != raw || out.Document["content_version"] != "2" || out.Document["media_type"] != media {
			t.Fatal("canonical source or revision altered")
		}
		if _, ok := out.Document["object_id"]; ok {
			t.Fatal("private object identity exposed")
		}
	}
}
func TestCanonicalSourceHTTPRejectsStaleOrFailedRead(t *testing.T) {
	for _, mode := range []string{"stale", "oversize", "close", "version-shape", "query", "read"} {
		h, _, p := testHandler()
		body := &sourceBody{Reader: strings.NewReader("data")}
		source := &sourcePort{testPorts: p, body: body}
		h.reader = source
		path := sourcePath("2")
		status := 400
		switch mode {
		case "stale":
			p.content.Document.ContentVersion = 3
			status = 409
		case "oversize":
			body.Reader = strings.NewReader(strings.Repeat("x", sourceBytesLimit+1))
			status = 413
		case "close":
			body.closeErr = f.NewFault(f.DependencyUnavailable, f.NotStarted)
			status = 503
		case "version-shape":
			path = sourcePath("01")
		case "query":
			path += "?version=1"
		case "read":
			body.Reader = errorReader{}
			status = 500
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", path, nil), w) || w.Code != status || strings.Contains(w.Body.String(), "bytes_base64") {
			t.Fatal("failed source exposed bytes", mode, w.Code)
		}
		if source.opens > 0 && body.closes != 1 {
			t.Fatal("original reader not closed", mode)
		}
		if mode == "stale" && body.reads != 0 {
			t.Fatal("stale source read before version check")
		}
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("private read cause") }
