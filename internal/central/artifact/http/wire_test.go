package artifacthttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func metadataWith(t *testing.T, edit func(*ac.MetadataDetails)) ac.Metadata {
	t.Helper()
	d := testMetadata().Details()
	edit(&d)
	m, err := ac.NewMetadata(d)
	if err != nil {
		t.Fatal("invalid test metadata", err)
	}
	return m
}
func readWith(t *testing.T, d ac.ReadDetails) ac.ReadResult {
	t.Helper()
	v, err := ac.NewReadResult(d)
	if err != nil {
		t.Fatal("invalid test result", err)
	}
	return v
}

func TestArtifactHTTPBoundedMetadataProjection(t *testing.T) {
	ctx := context.Background()
	p := testReference().ProjectID
	items := make([]ac.Metadata, 200)
	for i := range items {
		items[i] = metadataWith(t, func(d *ac.MetadataDetails) {
			d.Reference.ArtifactID = testID[ac.ArtifactEntity](400 - i)
			d.Name = strings.Repeat("<", 255)
			d.Description = strings.Repeat("\x01", 4096)
		})
	}
	raw, err := encodeList(ctx, p, ac.ListFilter{}, f.PageRequest{Limit: 200}, items, strings.Repeat("a", 8192))
	if err != nil || len(raw) <= 4<<20 || len(raw) > maxListRepresentationBytes {
		t.Fatal("maximum legal escaped page was truncated or rejected", len(raw), err)
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil || len(body["items"].([]any)) != 200 {
		t.Fatal("page count")
	}
	first := body["items"].([]any)[0].(map[string]any)
	object := first["object"].(map[string]any)
	if len(object) != 3 || object["byte_size"] != "4" || object["sha256"] != "sha256:"+strings.Repeat("a", 64) || first["version"] != "3" || first["created_at"] != "2026-10-13T01:02:03.000000Z" {
		t.Fatal("safe explicit DTO")
	}
	if strings.Contains(string(raw), testMetadata().Details().Object.ID.String()) {
		t.Fatal("storage identity leaked")
	}
	for _, mode := range []string{"foreign-project", "filter", "duplicate", "order", "oversized-page", "cursor", "empty-metadata"} {
		t.Run(mode, func(t *testing.T) {
			rows := []ac.Metadata{testMetadata()}
			filter := ac.ListFilter{}
			page := f.PageRequest{Limit: 1}
			cursor := ""
			switch mode {
			case "foreign-project":
				rows[0] = metadataWith(t, func(d *ac.MetadataDetails) {
					d.Reference.ProjectID = testID[id.Project](10)
					d.Object.Scope, _ = id.InProject(d.Reference.ProjectID)
				})
			case "filter":
				filter.NameQuery = "unmatched"
			case "duplicate":
				rows = append(rows, rows[0])
				page.Limit = 2
			case "order":
				rows = append(rows, metadataWith(t, func(d *ac.MetadataDetails) { d.Reference.ArtifactID = testID[ac.ArtifactEntity](10) }))
				page.Limit = 2
			case "oversized-page":
				rows = append(rows, rows[0])
			case "cursor":
				cursor = "\x00"
			case "empty-metadata":
				rows[0] = ac.Metadata{}
			}
			if _, err := encodeList(ctx, p, filter, page, rows, cursor); err == nil {
				t.Fatal("invalid result exposed")
			}
		})
	}
}

func TestArtifactHTTPReadProjectionBounds(t *testing.T) {
	ctx := context.Background()
	ref := testReference()
	base := testRead().Details()
	for _, mode := range []string{"text", "empty-window", "image", "binary", "invalid-utf8", "max-escaped"} {
		t.Run(mode, func(t *testing.T) {
			d := base
			req := ac.PreviewRequest{Limit: 8192}
			switch mode {
			case "empty-window":
				d.Text = ""
				d.NextOffset = 0
				d.Truncated = true
				req.Limit = 1
			case "image", "binary", "invalid-utf8":
				d.Text = ""
				d.NextOffset = 0
				d.Projection = ac.FileProjection
				d.InvalidUTF8 = mode == "invalid-utf8"
				if mode == "image" {
					d.Projection = ac.ImageProjection
				}
			case "max-escaped":
				req.Limit = 65536
				d.Text = strings.Repeat("\x01", 65536)
				d.NextOffset = 65536
				d.Artifact = metadataWith(t, func(m *ac.MetadataDetails) { m.Object.ByteSize = 65536; m.Description = strings.Repeat("\x01", 4096) })
			}
			raw, err := encodeRead(ctx, ref, req, readWith(t, d))
			if err != nil || len(raw) > maxReadRepresentationBytes {
				t.Fatal("legal read projection", err)
			}
			if mode == "max-escaped" && len(raw) < 384<<10 {
				t.Fatal("escaped text lost")
			}
		})
	}
	for _, mode := range []string{"wrong-file", "wrong-offset", "over-limit", "request-outside-size", "empty-result"} {
		t.Run(mode, func(t *testing.T) {
			r := ref
			req := ac.PreviewRequest{Limit: 8192}
			result := testRead()
			switch mode {
			case "wrong-file":
				r.FileID = testID[ac.File](10)
			case "wrong-offset":
				req.Offset = 1
			case "over-limit":
				req.Limit = 3
			case "request-outside-size":
				req.Offset = 5
			case "empty-result":
				result = ac.ReadResult{}
			}
			if _, err := encodeRead(ctx, r, req, result); err == nil {
				t.Fatal("invalid read escaped")
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := encodeRead(cancelled, ref, ac.PreviewRequest{Limit: 8192}, testRead()); err == nil {
		t.Fatal("cancelled projection succeeded")
	}
}
