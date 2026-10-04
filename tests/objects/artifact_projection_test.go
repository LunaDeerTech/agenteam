//go:build integration

package objects_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestArtifactListLiteralCursorBindingAndCurrentPermission(t *testing.T) {
	f := newArtifactFixture(t)
	for i, name := range []string{"literal%_one.txt", "literal%_two.txt", "literal-other.txt"} {
		_, err := f.artifact.CreateFromContent(contextFor(t), f.invocation(t), command(t, "literal-"+foundation.Progress(i).String()), art.Display{Name: name}, "text/plain", "safe")
		if err != nil {
			t.Fatal(err)
		}
	}
	filter := art.ListFilter{NameQuery: "%_"}
	page, err := f.artifact.ListArtifacts(contextFor(t), f.invocation(t), filter, foundation.PageRequest{Limit: 1})
	if err != nil || len(page.Items()) != 1 || page.NextCursor() == "" {
		t.Fatal("literal first page", err)
	}
	last, err := f.artifact.ListArtifacts(contextFor(t), f.invocation(t), filter, foundation.PageRequest{Limit: 1, Cursor: page.NextCursor()})
	if err != nil || len(last.Items()) != 1 || last.NextCursor() != "" || last.Items()[0].Details().Reference == page.Items()[0].Details().Reference {
		t.Fatal("literal second page", err)
	}
	_, err = f.artifact.ListArtifacts(contextFor(t), f.invocation(t), art.ListFilter{}, foundation.PageRequest{Cursor: page.NextCursor()})
	requireCode(t, err, foundation.CursorInvalid)
	other := id[identity.Project](t)
	f.sql(t, `INSERT INTO object_fixture.projects(id,owner_id)VALUES($1,$2)`, other.String(), f.actor.Details().UserID)
	v, _ := art.NewInvocation(art.InvocationDetails{Actor: f.actor, ProjectID: other, AttemptID: id[art.CallAttempt](t)})
	_, err = f.artifact.ListArtifacts(contextFor(t), v, filter, foundation.PageRequest{Cursor: page.NextCursor()})
	requireCode(t, err, foundation.CursorInvalid)
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false WHERE id=$1`, f.project.String())
	denied, err := f.artifact.ListArtifacts(contextFor(t), f.invocation(t), filter, foundation.PageRequest{Cursor: page.NextCursor()})
	if err == nil || len(denied.Items()) != 0 || denied.NextCursor() != "" {
		t.Fatal("failed list Audit disclosed results")
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=true,owner_id=$2 WHERE id=$1`, f.project.String(), id[identity.User](t).String())
	_, err = f.artifact.ListArtifacts(contextFor(t), f.invocation(t), filter, foundation.PageRequest{Cursor: page.NextCursor()})
	requireCode(t, err, foundation.Forbidden)
}

func TestArtifactReadUTF8BinaryImageAndAuditBoundary(t *testing.T) {
	f := newArtifactFixture(t)
	m := f.create(t, "utf8", "text/plain", "a界é🙂z")
	preview, err := f.artifact.ReadArtifact(contextFor(t), f.invocation(t), m.Details().Reference, art.PreviewRequest{Offset: 1, Limit: 4})
	if err != nil || preview.Details().Text != "界" || preview.Details().NextOffset != 4 || !preview.Details().Truncated {
		t.Fatal("UTF8 partial preview", err)
	}
	_, err = f.artifact.ReadArtifact(contextFor(t), f.invocation(t), m.Details().Reference, art.PreviewRequest{Offset: 2, Limit: 4})
	requireCode(t, err, foundation.InvalidArgument)
	var imageBytes bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err = png.Encode(&imageBytes, picture); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		body       []byte
		media      string
		projection art.Projection
		invalid    bool
	}{
		{[]byte{'t', 0xff, 'z'}, "text/plain", art.FileProjection, true},
		{[]byte("%PDF-1.7\nfile"), "application/pdf", art.FileProjection, false},
		{imageBytes.Bytes(), "image/png", art.ImageProjection, false},
	} {
		key := "projection-" + foundation.Progress(i).String()
		display := art.Display{Name: "projection.dat"}
		target, e := f.artifact.BeginUpload(contextFor(t), f.invocation(t), command(t, key+"-upload"), display, tc.media, int64(len(tc.body)), nil)
		if e != nil {
			t.Fatal(e)
		}
		put, e := f.artifact.UploadPayload(contextFor(t), f.invocation(t), target, io.NopCloser(bytes.NewReader(tc.body)))
		if e != nil {
			t.Fatal(e)
		}
		m, e := f.artifact.CreateFromUpload(contextFor(t), f.invocation(t), command(t, key+"-create"), display, put.Receipt)
		if e != nil {
			t.Fatal(e)
		}
		got, e := f.artifact.ReadArtifact(contextFor(t), f.invocation(t), m.Details().Reference, art.PreviewRequest{})
		if e != nil || got.Details().Projection != tc.projection || got.Details().InvalidUTF8 != tc.invalid || got.Details().Text != "" {
			t.Fatal("binary/image canonical projection", i, e)
		}
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false`)
	result, err := f.artifact.ReadArtifact(contextFor(t), f.invocation(t), m.Details().Reference, art.PreviewRequest{})
	if err == nil || result.Details().Text != "" {
		t.Fatal("failed Read Audit disclosed preview")
	}
}

func TestArtifactSourceUploadReceiptAndUnboundDownloadProviders(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	display := art.Display{Name: "prospective.txt"}
	target, err := f.artifact.BeginUpload(ctx, f.invocation(t), command(t, "source-upload"), display, "text/plain", 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	put, err := f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("uploaded")))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.UploadedObject, Receipt: put.Receipt})
	if err != nil {
		t.Fatal(err)
	}
	d := f.downloads(t, f.sources)
	gets, puts := f.proxy.gets.Load(), f.proxy.puts.Load()
	if _, err = d.IssueDownload(ctx, f.actor, ref, oc.DownloadAttachment, 0); err == nil {
		t.Fatal("prospective receipt acquired browser URL")
	}
	if f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts {
		t.Fatal("rejected prospective download reached storage")
	}
	cmd := command(t, "copy-uploaded")
	copy, err := f.artifact.CreateFromSource(ctx, f.invocation(t), cmd, art.Display{Name: "copy.txt"}, ref)
	if err != nil || copy.Details().Object.ID == put.Meta.ID || copy.Details().Object.SHA256 != put.Meta.SHA256 {
		t.Fatal("receipt source copy", err)
	}
	consumed, err := f.artifact.CreateFromUpload(ctx, f.invocation(t), command(t, "consume-original"), display, put.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	// The original receipt has now been consumed, but completed copy replay is
	// target-only and must neither revalidate that receipt nor reopen its object.
	f.resolver.reject.Store(true)
	gets, puts = f.proxy.gets.Load(), f.proxy.puts.Load()
	resolves := f.resolver.resolves.Load()
	_, err = f.artifact.CreateFromSource(ctx, f.invocation(t), cmd, art.Display{Name: "copy.txt"}, ref)
	if err != nil || f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts || f.resolver.resolves.Load() != resolves {
		t.Fatal("completed copied receipt revisited consumed source", err)
	}
	signed, err := d.IssueDownload(ctx, f.actor, ref, oc.DownloadAttachment, 0)
	if err != nil {
		t.Fatal("real Artifact canonical Uploaded provider", err)
	}
	response, err := signed.ForHuman(f.actor)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err, out := downloadHTTP(t, d, f.actor, strings.TrimPrefix(response.URL, "/api/object-downloads/"), "", 0, nil)
	if err != nil || out.err != nil || string(body) != "uploaded" {
		t.Fatal("canonical Uploaded download", err, out.err)
	}
	var originalAudit int
	err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='artifact.download' AND resource_id=$1`, consumed.Details().Reference.ArtifactID.String()).Scan(&originalAudit)
	if err != nil || originalAudit != 3 {
		t.Fatal("Uploaded download forged resource identity", originalAudit, err)
	}
	knowledge, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: f.project, DocumentID: id[struct{}](t).String(), Revision: 1})
	_, err = d.IssueDownload(ctx, f.actor, knowledge, oc.DownloadAttachment, 0)
	requireCode(t, err, foundation.DependencyUnbound)
}
