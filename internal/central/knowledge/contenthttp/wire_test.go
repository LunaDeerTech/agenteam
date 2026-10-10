package contenthttp

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestContentProjectionStrictUnionAndByteIdentity(t *testing.T) {
	request := kc.DefaultReadRequest()
	for _, mode := range []string{"cross-project", "cross-target", "missing-object", "wrong-next", "too-many-bytes", "invalid-utf8", "union", "future-unavailable", "file-reference", "overflow"} {
		content := testContent()
		q := request
		switch mode {
		case "cross-project":
			content.Document.ProjectID = testID[id.Project](7)
		case "cross-target":
			content.Document.ID = testID[kc.Document](7)
		case "missing-object":
			content.Document.ObjectID = oc.ObjectID{}
		case "wrong-next":
			content.Text.NextByteOffset = 3
		case "too-many-bytes":
			q.MaxBytes = 3
		case "invalid-utf8":
			content.Text.Text = "\xff"
			content.Text.NextByteOffset = 1
		case "union":
			x := kc.ReadableUnbound
			content.Unavailable = &x
		case "future-unavailable":
			x := kc.ReadableProcessing
			content.Document.SourceKind = kc.File
			content.Document.MediaType = kc.PDF
			content.Text = nil
			content.Unavailable = &x
		case "file-reference":
			content.Text = nil
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: content.Document.ProjectID, DocumentID: content.Document.ID.String(), Revision: content.Document.ContentVersion})
			if err != nil {
				t.Fatal(err)
			}
			content.File = &ref
		case "overflow":
			q.ByteOffset = f.Progress(math.MaxInt64)
		}
		if raw, err := encodeContent(context.Background(), testID[id.Project](4), testID[kc.Document](3), q, content); err == nil || len(raw) != 0 {
			t.Fatal("bad content produced candidate", mode)
		}
	}
	for _, media := range []string{kc.PDF, kc.DOCX} {
		content := testContent()
		x := kc.ReadableUnbound
		content.Document.SourceKind = kc.File
		content.Document.MediaType = media
		content.Text = nil
		content.Unavailable = &x
		raw, err := encodeContent(context.Background(), content.Document.ProjectID, content.Document.ID, request, content)
		var actual map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &actual) != nil || len(actual) != 2 || string(actual["unavailable"]) != `"dependency_unbound"` {
			t.Fatal("unbound file fabricated readable content", err)
		}
	}
	content := testContent()
	content.Text = &kc.TextContent{Text: strings.Repeat("\x01", 1<<20), NextByteOffset: 1 << 20}
	request.MaxBytes = 1 << 20
	raw, err := encodeContent(context.Background(), content.Document.ProjectID, content.Document.ID, request, content)
	if err != nil || len(raw) <= 6<<20 || len(raw) > 7<<20 {
		t.Fatal("maximum legal escaped text did not remain bounded", err)
	}
	for _, tt := range []struct {
		text         string
		offset, next f.Progress
		limit        int
		truncated    bool
	}{{"", 0, 0, 1, true}, {"", 3, 3, 1, false}, {"a", 3, 4, 1, true}} {
		content := testContent()
		content.Text = &kc.TextContent{Text: tt.text, NextByteOffset: tt.next, Truncated: tt.truncated}
		if _, err := encodeContent(context.Background(), content.Document.ProjectID, content.Document.ID, kc.ReadRequest{ByteOffset: tt.offset, MaxBytes: tt.limit}, content); err != nil {
			t.Fatal("valid byte boundary projection rejected", err)
		}
	}
}
