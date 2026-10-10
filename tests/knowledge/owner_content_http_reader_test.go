//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Only original call boundaries are observed/held. ReadObject and every byte,
// D05 synchronous Close and lease mutation still run through the real service.
// This does not simulate a blocked MinIO socket or claim global runtime join.
type contentHTTPObjects struct {
	oc.Objects
	afterOpen func(context.Context, *oc.ObjectReader) error
	wrap      func(*oc.ObjectReader) io.ReadCloser
	opens     atomic.Int32
}

func (o *contentHTTPObjects) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, target oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
	reader, err := o.Objects.ReadObject(ctx, actor, owner, target, span)
	if err != nil {
		return nil, err
	}
	o.opens.Add(1)
	if o.afterOpen != nil {
		if err := o.afterOpen(ctx, reader); err != nil {
			return nil, errors.Join(err, reader.Close())
		}
	}
	if o.wrap == nil {
		return reader, nil
	}
	wrapped, err := oc.NewObjectReader(reader.Meta(), reader.Range(), o.wrap(reader))
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return wrapped, nil
}

type contentHTTPReaderBody struct {
	reader                    *oc.ObjectReader
	afterClose                func(error)
	reads, bytes, eof, closes atomic.Int64
}

func (b *contentHTTPReaderBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.reads.Add(1)
	b.bytes.Add(int64(n))
	if err == io.EOF {
		b.eof.Add(1)
	}
	return n, err
}
func (b *contentHTTPReaderBody) Close() error {
	b.closes.Add(1)
	err := b.reader.Close()
	if b.afterClose != nil {
		b.afterClose(err)
	}
	return err
}
func contentHTTPReaderService(t *testing.T, v *knowledgeOwnerHTTPFixture, objects *contentHTTPObjects) *knowledge.Service {
	t.Helper()
	deps := v.deps
	deps.Objects = objects
	service, err := knowledge.New(v.raw, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			t.Error("content reader Service did not actually join", err)
		}
	})
	installContentHTTP(t, v, service)
	return service
}

func TestKnowledgeOwnerContentHTTPReaderOwnership(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	for _, change := range []string{"owner", "deleted"} {
		t.Run("current_"+change+"_after_real_object_open", func(t *testing.T) {
			v := newContentHTTPFixture(t)
			// Small objects may verify EOF and retire the D05 lease before
			// ReadObject returns. Keep this real stream beyond that prefetch.
			text := strings.Repeat("a", 2*oc.StreamBufferSize+1)
			doc := contentHTTPCreate(t, v, kc.PlainText, "Opened canonical", text)
			gate, release := knowledgeOwnerHTTPGate()
			defer release()
			opened := make(chan struct{})
			objects := &contentHTTPObjects{Objects: v.deps.Objects}
			var body *contentHTTPReaderBody
			var originalCtx context.Context
			objects.afterOpen = func(ctx context.Context, reader *oc.ObjectReader) error {
				meta := reader.Meta()
				if meta.ID != doc.ObjectID || meta.ByteSize <= oc.StreamBufferSize || meta.ByteSize != f.Progress(len(text)) || meta.SHA256 != independentDigest([]byte(text)) {
					return errors.New("wrong actual canonical Object identity, size or digest")
				}
				originalCtx = ctx
				close(opened)
				return knowledgeOwnerHTTPWait(ctx, gate)
			}
			objects.wrap = func(reader *oc.ObjectReader) io.ReadCloser {
				body = &contentHTTPReaderBody{reader: reader}
				return body
			}
			contentHTTPReaderService(t, v, objects)
			replies, done := knowledgeOwnerHTTPRun(t, v, knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, ""), "", ""))
			runtimeAwait(t, opened)
			var active int
			if err := v.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, doc.ObjectID.String()).Scan(&active); err != nil || active != 1 {
				t.Fatalf("real reader lease was not established before change: active=%d query_error=%v original_context=%v", active, err, originalCtx.Err())
			}
			want, code := 409, f.VersionConflict
			if change == "owner" {
				// Explicit upstream same-Store/current-lock SQL owner fact only.
				result, writerDone := knowledgeOwnerHTTPOwnerWriter(t, v, nil, nil, nil)
				knowledgeOwnerHTTPCommit(t, result, writerDone)
				want, code = 404, f.NotFound
			} else {
				preview, err := v.service.PrepareDeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, v.project, doc.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := v.service.DeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), v.project, doc.ID, preview.Confirmation); err != nil {
					t.Fatal("actual delete after original reader open", err)
				}
			}
			before := v.facts(t)
			release()
			response := knowledgeOwnerHTTPReply(t, replies, done)
			problem := response.want(t, want)
			if problem["code"] != string(code) || body == nil || body.reads.Load() != 0 || body.closes.Load() != 1 || objects.opens.Load() != 1 {
				t.Fatal("current recheck bypassed, read bytes or lost actual Close")
			}
			contentHTTPNoReader(t, v, doc)
			if v.facts(t) != before {
				t.Fatal("failed canonical read changed business facts")
			}
		})
	}
	t.Run("real_eof_and_d05_close_do_not_finish_held_consumer", func(t *testing.T) {
		v := newContentHTTPFixture(t)
		const text = "文a"
		doc := contentHTTPCreate(t, v, kc.PlainText, "Actual Close", text)
		before := v.facts(t)
		gate, release := knowledgeOwnerHTTPGate()
		defer release()
		closed := make(chan error, 1)
		originalCtx := make(chan context.Context, 1)
		objects := &contentHTTPObjects{Objects: v.deps.Objects}
		objects.afterOpen = func(ctx context.Context, _ *oc.ObjectReader) error { originalCtx <- ctx; return nil }
		var body *contentHTTPReaderBody
		objects.wrap = func(reader *oc.ObjectReader) io.ReadCloser {
			body = &contentHTTPReaderBody{reader: reader, afterClose: func(err error) { closed <- err; <-gate }}
			return body
		}
		service := contentHTTPReaderService(t, v, objects)
		started := time.Now()
		replies, done := knowledgeOwnerHTTPRun(t, v, knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "HEAD", contentHTTPPath(v, doc.ID, ""), "", ""))
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal("original D05 Close did not complete", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("original EOF/Close not observed")
		}
		ctx := <-originalCtx
		runtimeAwait(t, ctx.Done())
		elapsed := time.Since(started)
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) || elapsed < 1500*time.Millisecond || elapsed > 4*time.Second || body.eof.Load() != 1 || body.bytes.Load() != int64(len(text)) || body.closes.Load() != 1 {
			t.Fatal("original total deadline or actual reader observation lost", elapsed)
		}
		select {
		case <-done:
			t.Fatal("HTTP finished while its original typed Close was still held")
		default:
		}
		ended, cancel := context.WithCancel(context.Background())
		cancel()
		if err := service.Drain(ended); !errors.Is(err, context.Canceled) {
			t.Fatal("EOF/inner Close treated as original consumer retirement", err)
		}
		contentHTTPNoReader(t, v, doc)
		release()
		response := knowledgeOwnerHTTPReply(t, replies, done)
		if !response.aborted || len(response.body) != 0 {
			t.Fatal("expired HEAD published after late Close return")
		}
		if err := service.Drain(knowledgeContext(t)); err != nil || v.facts(t) != before {
			t.Fatal("original consumer did not retire without business changes", err)
		}
	})
}
