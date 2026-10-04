//go:build integration

package objects_test

import (
	"io"
	"strings"
	"testing"

	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestArtifactThreeCreationPathsAndCompletedSourceReplay(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	v := f.invocation(t)
	content := "你好，artifact\n"
	inline, err := f.artifact.CreateFromContent(ctx, v, command(t, "inline"), art.Display{Name: "inline.txt", Description: "display-only"}, "text/plain; charset=utf-8", content)
	if err != nil {
		t.Fatal("inline", err)
	}
	ref, err := inline.Details().Reference.BusinessFile()
	if err != nil {
		t.Fatal(err)
	}
	copyCommand := command(t, "copy")
	display := art.Display{Name: "copied.txt"}
	copied, err := f.artifact.CreateFromSource(ctx, f.invocation(t), copyCommand, display, ref)
	if err != nil {
		t.Fatal("copy", err)
	}
	if copied.Details().Object.ID == inline.Details().Object.ID || copied.Details().Object.SHA256 != inline.Details().Object.SHA256 {
		t.Fatal("source reused object or changed canonical content")
	}
	resolveCount, gets, puts := f.resolver.resolves.Load(), f.proxy.gets.Load(), f.proxy.puts.Load()
	f.resolver.reject.Store(true)
	replayed, err := f.artifact.CreateFromSource(ctx, f.invocation(t), copyCommand, display, ref)
	if err != nil {
		t.Fatal("completed copy replay", err)
	}
	if replayed.Details().Reference != copied.Details().Reference || f.resolver.resolves.Load() != resolveCount || f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts {
		t.Fatal("completed replay touched original source/storage")
	}
	_, err = f.artifact.CreateFromSource(ctx, f.invocation(t), copyCommand, art.Display{Name: "different.txt"}, ref)
	requireCode(t, err, foundation.IdempotencyKeyReused)
	f.resolver.reject.Store(false)
	uploadDisplay := art.Display{Name: "human.txt"}
	uploadCommand := command(t, "human-bytes")
	target, err := f.artifact.BeginUpload(ctx, f.invocation(t), uploadCommand, uploadDisplay, "text/plain", 5, nil)
	if err != nil {
		t.Fatal("intent", err)
	}
	stored, err := f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("human")))
	if err != nil {
		t.Fatal("human upload", err)
	}
	puts = f.proxy.puts.Load()
	_, err = f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("other")))
	requireCode(t, err, foundation.IdempotencyKeyReused)
	if f.proxy.puts.Load() != puts {
		t.Fatal("different completed upload body emitted PUT")
	}
	replayUpload, err := f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("human")))
	if err != nil || replayUpload.Receipt.Details().ID != stored.Receipt.Details().ID || f.proxy.puts.Load() != puts {
		t.Fatal("same uploaded receipt replay", err)
	}
	human, err := f.artifact.CreateFromUpload(ctx, f.invocation(t), command(t, "human-create"), uploadDisplay, stored.Receipt)
	if err != nil {
		t.Fatal("consume", err)
	}
	if human.Details().Kind != art.UserUpload || human.Details().Object.ID != stored.Meta.ID || f.proxy.puts.Load() != puts {
		t.Fatal("receipt path retransmitted/replaced object")
	}
	var artifacts, canonical, creates int64
	if err = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts),(SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical' AND owner_id IN(SELECT id FROM agenteam_artifact.artifacts)),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='artifact.create')`).Scan(&artifacts, &canonical, &creates); err != nil || artifacts != 3 || canonical != 3 || creates != 3 {
		t.Fatal("business/reference/Audit atomic facts", err, artifacts, canonical, creates)
	}
	preview, err := f.artifact.ReadArtifact(ctx, f.invocation(t), inline.Details().Reference, art.PreviewRequest{})
	if err != nil {
		t.Fatal("preview", err)
	}
	if preview.Details().Text != content || preview.Details().Truncated {
		t.Fatal("UTF8 canonical preview changed")
	}
	page, err := f.artifact.ListArtifacts(ctx, f.invocation(t), art.ListFilter{}, foundation.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal("list", err)
	}
	if len(page.Items()) != 2 || page.NextCursor() == "" {
		t.Fatal("first cursor page")
	}
	last, err := f.artifact.ListArtifacts(ctx, f.invocation(t), art.ListFilter{}, foundation.PageRequest{Limit: 2, Cursor: page.NextCursor()})
	if err != nil || len(last.Items()) != 1 || last.NextCursor() != "" {
		t.Fatal("second cursor page", err)
	}
}
