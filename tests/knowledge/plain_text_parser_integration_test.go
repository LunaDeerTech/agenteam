//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	parser "github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

// This observes the original D05 reader; it supplies no bytes, metadata,
// authorization, SQL outcome or successful Close. The optional barrier runs
// only after the original Close has actually returned.
type plainTextParserReadProbe struct {
	oc.Objects
	objects *contentHTTPObjects
	body    *contentHTTPReaderBody
	calls   atomic.Int32
	closed  atomic.Int32
}

func (p *plainTextParserReadProbe) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, target oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
	p.calls.Add(1) // Count attempts too, including a delegate that would reject.
	return p.objects.ReadObject(ctx, actor, owner, target, span)
}

func plainTextParserReader(t *testing.T, v *knowledgeOwnerHTTPFixture, doc kc.DocumentRef, text string, afterClose func(error)) (*knowledge.Service, *plainTextParserReadProbe) {
	t.Helper()
	probe := &plainTextParserReadProbe{Objects: v.deps.Objects, objects: &contentHTTPObjects{Objects: v.deps.Objects}}
	probe.objects.afterOpen = func(_ context.Context, reader *oc.ObjectReader) error {
		meta := reader.Meta()
		if meta.ID != doc.ObjectID || meta.ByteSize != f.Progress(len(text)) || meta.SHA256 != independentDigest([]byte(text)) {
			return errors.New("wrong original canonical object identity or bytes")
		}
		return nil
	}
	probe.objects.wrap = func(reader *oc.ObjectReader) io.ReadCloser {
		probe.body = &contentHTTPReaderBody{reader: reader, afterClose: func(err error) {
			if err == nil {
				probe.closed.Add(1)
			}
			if afterClose != nil {
				afterClose(err)
			}
		}}
		return probe.body
	}
	service := publicationService(t, v.ownerTreeFixture, func(d *knowledge.Dependencies) { d.Objects = probe })
	return service, probe
}

func plainTextParserReadJoined(t *testing.T, v *knowledgeOwnerHTTPFixture, service *knowledge.Service, probe *plainTextParserReadProbe, doc kc.DocumentRef) {
	t.Helper()
	if probe.calls.Load() != 1 || probe.objects.opens.Load() != 1 || probe.body == nil || probe.body.reads.Load() == 0 || probe.body.closes.Load() != 1 || probe.closed.Load() != 1 {
		t.Fatal("original read and successful synchronous Close were not observed exactly once")
	}
	if err := service.Drain(knowledgeContext(t)); err != nil {
		t.Fatal("original Knowledge call did not actually retire", err)
	}
	// Short objects can retire their lease during prefetch. This complements
	// actual Close/Drain; it never substitutes for either observation.
	contentHTTPNoReader(t, v, doc)
}

func plainTextParserRejected(t *testing.T, result parser.StructuredDocument, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code || fault.CommitState != f.NotStarted || !reflect.DeepEqual(result, parser.StructuredDocument{}) {
		t.Fatal("parser rejection lost its code, not-started state or zero result")
	}
}

// A real same-Store Account/Project/Knowledge/Object chain followed by the
// public D13 value adapter. Account identities use real Login; only Project
// initialization is the existing explicit upstream SQL fixture. This is not
// default Project creation, HTTP/SPA, index publication or global Object join.
func TestKnowledgePlainTextParserIntegration(t *testing.T) {
	knowledgeOwnerHTTPTop(t) // Original 120s includes all fixture cleanup.
	v := newKnowledgeOwnerHTTPFixture(t)
	old, err := v.service.CreateDocument(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), kc.CreateRequest{
		ProjectID: v.project, DocumentID: treeID[kc.Document](t), Title: "Parser actual source",
	}, publicationText(t, "old content"))
	if err != nil || old.ContentVersion != 1 {
		t.Fatal("real initial publication failed", err)
	}
	const text = "甲。\r\n乙！\r\n\r\nA. B?"
	source := publicationText(t, text)
	doc, err := v.service.UpdateDocument(knowledgeContext(t), v.ownerBrowser.actor, titleMeta(t, 1), v.project, old.ID, kc.UpdateRequest{ReplaceSource: true}, &source)
	if err != nil || doc.ContentVersion != 2 || doc.ID != old.ID || doc.ProjectID != v.project || doc.ObjectID == old.ObjectID {
		t.Fatal("real replacement did not produce the current version and object", err)
	}
	publicationFacts(t, v.ownerTreeFixture, doc, []byte(text))

	t.Run("full_current_bytes_after_actual_close", func(t *testing.T) {
		before := v.facts(t)
		gate, release := knowledgeOwnerHTTPGate()
		defer release()
		closed := make(chan struct{})
		var closeErr error
		service, probe := plainTextParserReader(t, v, doc, text, func(err error) {
			closeErr = err
			close(closed)
			<-gate
		})
		ctx, cancel := context.WithCancel(knowledgeContext(t))
		request := kc.ReadRequest{MaxBytes: kc.MaxReadBytes}
		type outcome struct {
			content kc.DocumentContent
			err     error
		}
		reply, done := make(chan outcome, 1), make(chan struct{})
		var parserCalls atomic.Int32
		go func() {
			defer close(done)
			content, err := service.ReadDocument(ctx, v.ownerBrowser.actor, v.project, doc.ID, request)
			reply <- outcome{content: content, err: err}
		}()
		t.Cleanup(func() { release(); cancel(); runtimeAwait(t, done) })
		runtimeAwait(t, closed)
		if closeErr != nil || probe.closed.Load() != 1 || probe.body == nil || probe.body.bytes.Load() != 23 || probe.body.eof.Load() != 1 || probe.body.closes.Load() != 1 || parserCalls.Load() != 0 {
			t.Fatal("original EOF/Close or pre-parser boundary was not reached")
		}
		select {
		case <-reply:
			t.Fatal("ReadDocument returned before its original consumer Close returned")
		default:
		}
		select {
		case <-done:
			t.Fatal("original caller retired while consumer Close was held")
		default:
		}
		ended, stop := context.WithCancel(context.Background())
		stop()
		if err := service.Drain(ended); !errors.Is(err, context.Canceled) {
			t.Fatal("inner Object Close was mistaken for Knowledge consumer retirement", err)
		}
		release()
		runtimeAwait(t, done) // Join the original ReadDocument caller first.
		read := <-reply
		if read.err != nil || read.content.Validate() != nil || read.content.Text == nil || read.content.Text.Text != text || read.content.Text.Truncated || read.content.Text.NextByteOffset != 23 {
			t.Fatal("actual full D12 read did not return the complete original value", read.err)
		}
		actual := read.content.Document
		if actual.ID != doc.ID || actual.ProjectID != doc.ProjectID || actual.ContentVersion != 2 || actual.ObjectID != doc.ObjectID {
			t.Fatal("actual D12 result lost the replacement's source identity")
		}
		plainTextParserReadJoined(t, v, service, probe, doc)
		parserCalls.Add(1)
		parsed, err := parser.ParseBoundedContent(ctx, request, read.content)
		if err != nil || parserCalls.Load() != 1 || parsed.Source != (parser.SourceIdentity{ProjectID: doc.ProjectID, DocumentID: doc.ID, ContentVersion: 2, ObjectID: doc.ObjectID}) || parsed.ParserProfile != "plain_text:v1" || parsed.SourceBytes != 23 {
			t.Fatal("actual D12 to D13 call lost current identity or profile", err)
		}
		// Independent hand-counted original byte coordinates: 甲。=6,
		// CRLF=2, 乙！=6, CRLF CRLF=4, A. B?=5. No parser builds this oracle.
		want := []parser.StructuredElement{
			{Kind: parser.Paragraph, Ordinal: 1, Text: "甲。\r\n乙！", Bytes: parser.ByteRange{Start: 0, End: 14}, Sentences: []parser.ByteRange{{Start: 0, End: 6}, {Start: 6, End: 14}}},
			{Kind: parser.Paragraph, Ordinal: 2, Text: "A. B?", Bytes: parser.ByteRange{Start: 18, End: 23}, Sentences: []parser.ByteRange{{Start: 18, End: 20}, {Start: 20, End: 23}}},
		}
		if !reflect.DeepEqual(parsed.Elements, want) || v.facts(t) != before {
			t.Fatal("wrong literal byte oracle or read/parse created business facts")
		}
	})

	t.Run("partial_and_nonplain_rejected", func(t *testing.T) {
		before := v.facts(t)
		service, probe := plainTextParserReader(t, v, doc, text, nil)
		request := kc.ReadRequest{MaxBytes: 6}
		partial, err := service.ReadDocument(knowledgeContext(t), v.ownerBrowser.actor, v.project, doc.ID, request)
		if err != nil || partial.Validate() != nil || partial.Text == nil || partial.Text.Text != "甲。" || !partial.Text.Truncated || partial.Text.NextByteOffset != 6 || partial.Document.ContentVersion != 2 || partial.Document.ObjectID != doc.ObjectID {
			t.Fatal("real bounded D12 read did not produce the expected partial page", err)
		}
		plainTextParserReadJoined(t, v, service, probe, doc)
		parsed, err := parser.ParseBoundedContent(knowledgeContext(t), request, partial)
		plainTextParserRejected(t, parsed, err, f.PayloadTooLarge)
		if v.facts(t) != before {
			t.Fatal("partial read/parser rejection changed business facts")
		}

		const markdown = "# A."
		md := contentHTTPCreate(t, v, kc.Markdown, "Actual nonplain source", markdown)
		publicationFacts(t, v.ownerTreeFixture, md, []byte(markdown))
		before = v.facts(t)
		service, probe = plainTextParserReader(t, v, md, markdown, nil)
		request = kc.DefaultReadRequest()
		content, err := service.ReadDocument(knowledgeContext(t), v.ownerBrowser.actor, v.project, md.ID, request)
		if err != nil || content.Validate() != nil || content.Document.MediaType != kc.Markdown || content.Text == nil || content.Text.Text != markdown || content.Text.Truncated || content.Text.NextByteOffset != 4 {
			t.Fatal("real Markdown read did not return its actual complete value", err)
		}
		plainTextParserReadJoined(t, v, service, probe, md)
		parsed, err = parser.ParseBoundedContent(knowledgeContext(t), request, content)
		plainTextParserRejected(t, parsed, err, f.UnsupportedMediaType)
		if v.facts(t) != before {
			t.Fatal("nonplain read/parser rejection changed business facts")
		}
	})

	t.Run("foreign_owner_produces_no_parser_input", func(t *testing.T) {
		before := v.facts(t)
		service, probe := plainTextParserReader(t, v, doc, text, nil)
		request := kc.DefaultReadRequest()
		content, readErr := service.ReadDocument(knowledgeContext(t), v.otherBrowser.actor, v.project, doc.ID, request)
		parsed, parserCalled := parser.StructuredDocument{}, false
		if readErr == nil {
			parserCalled = true
			var err error
			parsed, err = parser.ParseBoundedContent(knowledgeContext(t), request, content)
			if err != nil {
				t.Fatal("unexpected successful foreign read reached parser", err)
			}
		}
		treeCode(t, readErr, f.NotFound)
		if !reflect.DeepEqual(content, kc.DocumentContent{}) || parserCalled || !reflect.DeepEqual(parsed, parser.StructuredDocument{}) || probe.calls.Load() != 0 || probe.objects.opens.Load() != 0 || probe.body != nil {
			t.Fatal("foreign Owner acquired a reader or supplied parser input")
		}
		if err := service.Drain(knowledgeContext(t)); err != nil || v.facts(t) != before {
			t.Fatal("denied call did not retire without business changes", err)
		}
		contentHTTPNoReader(t, v, doc)
	})
}
