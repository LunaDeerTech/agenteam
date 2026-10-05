package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type countedBody struct {
	reader   io.Reader
	closes   atomic.Int32
	closeErr error
}

func (b *countedBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *countedBody) Close() error               { b.closes.Add(1); return b.closeErr }
func body(text string) *countedBody               { return &countedBody{reader: strings.NewReader(text)} }
func sha() f.Digest                               { return f.Digest("sha256:" + strings.Repeat("a", 64)) }
func TestSourceMetadataAndOwnedClose(t *testing.T) {
	text := must(k.NewTextSource(k.Markdown, "  原始\r\n"))
	d := must(text.Details())
	*d.Text = "mutated"
	if *must(text.Details()).Text != "  原始\r\n" {
		t.Fatal("aliased text")
	}
	requireOK(t, text.Close())
	_, e := text.TakeUploadBody()
	requireError(t, e)
	for _, media := range []string{k.PDF, k.DOCX, "text/plain;charset=utf-8"} {
		_, e := k.NewTextSource(media, "x")
		requireError(t, e)
	}
	_, e = k.NewTextSource(k.PlainText, string([]byte{0xff}))
	requireError(t, e)
	var nilReader *countedBody
	_, e = k.NewUploadSource(k.PDF, 0, sha(), nilReader)
	requireError(t, e)
	failed := body("x")
	_, e = k.NewUploadSource(k.PDF, f.Progress(oc.MaxObjectSize+1), sha(), failed)
	requireError(t, e)
	if failed.closes.Load() != 0 {
		t.Fatal("constructor stole failed input")
	}
	requireOK(t, failed.Close())
	upload := body("payload")
	closeErr := errors.New("underlying-close-did-not-join")
	upload.closeErr = closeErr
	s := must(k.NewUploadSource(k.PDF, 7, sha(), upload))
	copy := s
	d = must(s.Details())
	d.Upload.Length = 0
	if must(s.Details()).Upload.Length != 7 {
		t.Fatal("aliased upload metadata")
	}
	r := must(copy.TakeUploadBody())
	if r == upload {
		t.Fatal("raw body escaped")
	}
	got := must(io.ReadAll(r))
	if string(got) != "payload" {
		t.Fatal(string(got))
	}
	requireError(t, s.Close())
	if !errors.Is(r.Close(), closeErr) || !errors.Is(copy.Close(), closeErr) || upload.closes.Load() != 1 {
		t.Fatal("close error/ownership lost")
	}
	_, e = r.Read(make([]byte, 1))
	requireError(t, e)
	_, e = s.TakeUploadBody()
	requireError(t, e)
	if must(s.Details()).Upload.Length != 7 {
		t.Fatal("close discarded command metadata")
	}
	for _, zero := range []k.SourceInput{{}} {
		requireError(t, zero.Validate())
		_, e := zero.Details()
		requireError(t, e)
		_, e = zero.TakeUploadBody()
		requireError(t, e)
		requireError(t, zero.Close())
	}
}
func TestUploadConcurrentCopiesTransferExactlyOnce(t *testing.T) {
	original := body("private")
	s := must(k.NewUploadSource(k.PlainText, 7, sha(), original))
	var wg sync.WaitGroup
	var won atomic.Int32
	for range 32 {
		wg.Go(func() {
			copy := s
			r, e := copy.TakeUploadBody()
			if e == nil {
				won.Add(1)
				requireOK(t, r.Close())
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 || original.closes.Load() != 1 {
		t.Fatal(won.Load(), original.closes.Load())
	}
	var closes sync.WaitGroup
	for range 16 {
		closes.Go(func() { requireOK(t, s.Close()) })
	}
	closes.Wait()
	if original.closes.Load() != 1 {
		t.Fatal("duplicate Close")
	}
}

type blockingBody struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func (b *blockingBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.EOF
}
func (b *blockingBody) Close() error { b.closes.Add(1); close(b.closed); return nil }
func TestCloseCanInterruptBlockedRead(t *testing.T) {
	b := &blockingBody{entered: make(chan struct{}), closed: make(chan struct{})}
	s := must(k.NewUploadSource(k.PlainText, 1, sha(), b))
	r := must(s.TakeUploadBody())
	done := make(chan struct{})
	go func() { defer close(done); _, _ = r.Read(make([]byte, 1)) }()
	<-b.entered
	requireOK(t, s.Close())
	<-done
	requireOK(t, r.Close())
	if b.closes.Load() != 1 {
		t.Fatal("multiple provider closes")
	}
}
func objectReader(d k.DocumentRef, b io.ReadCloser, rr *oc.ResolvedRange) *oc.ObjectReader {
	m := oc.ObjectMeta{ID: d.ObjectID, Scope: must(id.InProject(d.ProjectID)), MediaType: d.MediaType, ByteSize: 4, SHA256: sha(), State: oc.Available, Version: 1, CreatedAt: d.CreatedAt}
	return must(oc.NewObjectReader(m, rr, b))
}
func TestCanonicalReadOwnsMatchingReaderAndCopiesMetadata(t *testing.T) {
	d := doc(10)
	d.ParentDocumentID = ptr(keyID[k.Document](9))
	provider := body("data")
	provider.closeErr = errors.New("provider-close")
	reader := objectReader(d, provider, &oc.ResolvedRange{Offset: 1, Length: 2, Total: 4})
	stream := must(k.NewCanonicalRead(d, reader))
	copy := stream
	projection := stream.Document()
	*projection.ParentDocumentID = keyID[k.Document](88)
	rr := stream.Range()
	rr.Offset = 0
	if *stream.Document().ParentDocumentID != *d.ParentDocumentID || stream.Range().Offset != 1 {
		t.Fatal("metadata aliased")
	}
	got := must(io.ReadAll(stream))
	if string(got) != "data" {
		t.Fatal("wrapper altered provider bytes")
	}
	if !errors.Is(copy.Close(), provider.closeErr) || !errors.Is(stream.Close(), provider.closeErr) || provider.closes.Load() != 1 {
		t.Fatal("reader lifetime lost")
	}
	_, e := stream.Read(make([]byte, 1))
	requireError(t, e)
	for _, change := range []func(*k.DocumentRef){func(x *k.DocumentRef) { x.ObjectID = keyID[oc.StoredObject](100) }, func(x *k.DocumentRef) { x.ProjectID = keyID[id.Project](100) }, func(x *k.DocumentRef) { x.MediaType = k.Markdown }, func(x *k.DocumentRef) { x.Status = k.Deleted }} {
		wrong := d
		change(&wrong)
		b := body("data")
		r := objectReader(d, b, nil)
		_, e := k.NewCanonicalRead(wrong, r)
		requireError(t, e)
		if b.closes.Load() != 0 {
			t.Fatal("failed constructor closed caller reader")
		}
		requireOK(t, r.Close())
	}
	_, e = k.NewCanonicalRead(d, new(oc.ObjectReader))
	requireError(t, e)
	var zero k.CanonicalRead
	requireError(t, zero.Validate())
	requireError(t, zero.Close())
	_, e = zero.Read(nil)
	requireError(t, e)
	if zero.Range() != nil || zero.Document().ID.Validate() == nil {
		t.Fatal("zero projection")
	}
}
func TestOpaqueSourcesNeverImplicitlyExposeContent(t *testing.T) {
	canary := "NEVER-LOG-RAW-SOURCE-01234"
	source := must(k.NewTextSource(k.PlainText, canary))
	objects := []any{source, must(k.NewCreatorRef(k.CreatorDetails{Kind: id.Human, UserID: keyID[id.User](1)}))}
	for _, v := range objects {
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(verb, v), canary) {
				t.Fatal("format leak")
			}
		}
		raw := must(json.Marshal(v))
		if bytes.Contains(raw, []byte(canary)) {
			t.Fatal("JSON leak")
		}
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		logger.Info("probe", "value", v)
		if strings.Contains(buf.String(), canary) {
			t.Fatal("slog leak")
		}
	}
	var target k.SourceInput
	requireError(t, json.Unmarshal([]byte(`"knowledge_source"`), &target))
	var cr k.CanonicalRead
	requireError(t, json.Unmarshal([]byte("{}"), &cr))
}
func TestCanonicalFactsDoNotRetainDeletedObject(t *testing.T) {
	d := doc(10)
	v := k.CurrentDocumentFact{ProjectID: d.ProjectID, DocumentID: d.ID, ContentVersion: 1, Status: k.Active, ObjectID: &d.ObjectID}
	var decoded k.CurrentDocumentFact
	roundTrip(t, v, &decoded)
	v.Status = k.Deleted
	requireError(t, v.Validate())
	v.ObjectID = nil
	roundTrip(t, v, &decoded)
	if strings.Contains(string(must(json.Marshal(v))), "object_id") {
		t.Fatal("deleted object retained")
	}
}
