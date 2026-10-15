package texthttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

type uploadService struct {
	*testService
	raw []byte
}

func (s *uploadService) CreateDocument(ctx context.Context, a id.Actor, m f.CommandMeta, r kc.CreateRequest, source kc.SourceInput) (kc.DocumentRef, error) {
	body, err := source.TakeUploadBody()
	if err != nil {
		return kc.DocumentRef{}, err
	}
	s.raw, err = io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		return kc.DocumentRef{}, err
	}
	if closeErr != nil {
		return kc.DocumentRef{}, closeErr
	}
	return s.testService.CreateDocument(ctx, a, m, r, source)
}

func fileCreateBody(media string, raw []byte) string {
	b, _ := json.Marshal(map[string]any{"parent_document_id": nil, "title": " exact title ", "media_type": media, "bytes_base64": base64.StdEncoding.EncodeToString(raw)})
	return string(b)
}

func TestKnowledgeUploadOriginalBytesAndLookup(t *testing.T) {
	for _, media := range []string{kc.PlainText, kc.Markdown, kc.PDF, kc.DOCX} {
		t.Run(media, func(t *testing.T) {
			h, original, _ := fixture()
			original.doc.MediaType = media
			if media == kc.PDF || media == kc.DOCX {
				original.doc.SourceKind = kc.File
			}
			s := &uploadService{testService: original}
			h.service = s
			raw := []byte("original\r\n中文\x00binary")
			body := fileCreateBody(media, raw)
			success(t, h, request("create-file", body))
			d, err := s.source.Details()
			sum := sha256.Sum256(raw)
			if err != nil || !bytes.Equal(s.raw, raw) || d.Kind != kc.InputUpload || d.Upload == nil || d.Upload.Length != f.Progress(len(raw)) || d.Upload.ExpectedSHA256 != f.Digest("sha256:"+hex.EncodeToString(sum[:])) {
				t.Fatal("original upload bytes or measured identity lost")
			}
			expected, err := kc.CreateDigest(testActor(), s.meta, s.create, s.source)
			if err != nil {
				t.Fatal(err)
			}
			lookup, _ := json.Marshal(map[string]any{"command": "create", "document_id": s.doc.ID.String(), "request": json.RawMessage(body)})
			r := request("lookup", string(lookup))
			r.URL.Path = strings.Replace(r.URL.Path, "text-commands", "file-commands", 1)
			success(t, h, r)
			if s.lookupRequest.SemanticDigest != expected || s.lookups != 1 || s.creates != 1 {
				t.Fatal("lookup must bind original upload without another mutation")
			}
		})
	}
}

func TestKnowledgeUploadRejectsAmbiguousOrOverLimitInput(t *testing.T) {
	valid := fileCreateBody(kc.PDF, []byte("a"))
	for _, body := range []string{
		strings.Replace(valid, "YQ==", "YR==", 1),
		strings.Replace(valid, "YQ==", "YQ==\\n", 1),
		strings.Replace(valid, "YQ==", "YQ", 1),
		strings.Replace(valid, "bytes_base64", "text", 1),
		strings.Replace(valid, "application/pdf", "image/png", 1),
		strings.Replace(valid, "\"bytes_base64\":", "\"bytes_base64\":\"YQ==\",\"bytes_base64\":", 1),
		fileCreateBody(kc.PDF, make([]byte, uploadBytesLimit+1)),
	} {
		h, s, _ := fixture()
		w := writer()
		if serve(h, request("create-file", body), w) || (w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge) || s.calls != 0 {
			t.Fatal("invalid upload reached domain or wrong failure class")
		}
	}
	h, s, _ := fixture()
	s.doc.MediaType = kc.PDF
	s.doc.SourceKind = kc.File
	u := &uploadService{testService: s}
	h.service = u
	success(t, h, request("create-file", fileCreateBody(kc.PDF, make([]byte, uploadBytesLimit))))
	if len(u.raw) != uploadBytesLimit {
		t.Fatal("exact boundary truncated")
	}
}
