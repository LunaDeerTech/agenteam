package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type closeFailureTextBody struct {
	*bytes.Reader
	closed bool
}

func (r *closeFailureTextBody) Close() error {
	r.closed = true
	return errors.New("owned reader close failed")
}

type textReadObjects struct {
	oc.Objects
	reader *oc.ObjectReader
	calls  int
}

func (o *textReadObjects) ReadObject(context.Context, id.Actor, oc.ObjectOwner, oc.ObjectID, *oc.ByteRange) (*oc.ObjectReader, error) {
	o.calls++
	return o.reader, nil
}

func TestMCPTextProjectionDoesNotPublishBeforeOriginalReaderClose(t *testing.T) {
	body := []byte("canonical text")
	meta, ref := textProjectionFixture(t, body)
	readerBody := &closeFailureTextBody{Reader: bytes.NewReader(body)}
	reader, err := oc.NewObjectReader(meta, nil, readerBody)
	if err != nil {
		t.Fatal("invalid canonical reader fixture")
	}
	objects := &textReadObjects{reader: reader}
	a := &ContentAuthority{direct: &Service{}}
	store := &ContentStore{authority: a, objects: objects}
	w := &contentUse{owner: a}
	w.active.Store(true)
	if _, err = store.projectCanonicalText(context.Background(), w, oc.ObjectOwner{}, meta, ref, "repo://owned/text"); err == nil || objects.calls != 0 {
		t.Fatal("projection read without its actual private callback context")
	}
	ctx := context.WithValue(context.Background(), contentUseKey{}, w)
	part, err := store.projectCanonicalText(ctx, w, oc.ObjectOwner{}, meta, ref, "repo://owned/text")
	if err == nil || part.Text != nil || !readerBody.closed || objects.calls != 1 {
		t.Fatal("reader Close failure was published as a successful text projection")
	}
}

func textProjectionFixture(t *testing.T, body []byte) (oc.ObjectMeta, oc.BusinessFileRef) {
	t.Helper()
	project, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000001")
	object, _ := f.ParseID[oc.StoredObject]("01960aa5-0286-7000-8000-000000000002")
	scope, _ := id.InProject(project)
	created, _ := f.NewInstant(time.Unix(1000, 0).UTC())
	sum := sha256.Sum256(body)
	meta := oc.ObjectMeta{ID: object, Scope: scope, MediaType: "text/plain", ByteSize: f.Progress(len(body)), SHA256: f.Digest("sha256:" + hex.EncodeToString(sum[:])), State: oc.Available, Version: 1, CreatedAt: created}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: "01960aa5-0286-7000-8000-000000000003"})
	if err != nil || meta.Validate() != nil {
		t.Fatal("invalid canonical projection fixture")
	}
	return meta, ref
}

func TestMCPTextProjectionReadsCanonicalDigestAndBoundsUTF8(t *testing.T) {
	body := []byte(strings.Repeat("x", maxInlineResourceText-1) + "🙂tail")
	meta, ref := textProjectionFixture(t, body)
	part, err := canonicalTextProjection(meta, ref, "repo://owned/text", body)
	if err != nil || part.Text == nil || part.File != nil {
		t.Fatal("canonical text projection unavailable to text Tool profile")
	}
	var projection struct {
		File struct {
			Kind    string `json:"kind"`
			Project string `json:"project_id"`
			ID      string `json:"file_id"`
		} `json:"file_ref"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
		SHA       string `json:"sha256"`
		Size      string `json:"byte_size"`
	}
	if json.Unmarshal([]byte(part.Text.Text), &projection) != nil || !projection.Truncated || projection.Text != strings.Repeat("x", maxInlineResourceText-1) || !utf8.ValidString(projection.Text) {
		t.Fatal("UTF8 projection exceeded its byte budget or hid truncation")
	}
	if projection.File.Kind != string(oc.MCPFile) || projection.File.Project != ref.Details().ProjectID.String() || projection.File.ID != ref.Details().FileID || projection.SHA != meta.SHA256.String() || projection.Size != meta.ByteSize.String() {
		t.Fatal("text projection lost the formal canonical MCPFile identity")
	}
	body[0] = 'z'
	if _, err = canonicalTextProjection(meta, ref, "repo://owned/text", body); err == nil {
		t.Fatal("text not matching the canonical Object digest was published")
	}
}

func TestMCPTextProjectionRejectsBinaryAndForeignCanonicalScope(t *testing.T) {
	for _, body := range [][]byte{[]byte{0xff, 0xfe}, []byte("image bytes")} {
		meta, ref := textProjectionFixture(t, body)
		if utf8.Valid(body) {
			meta.MediaType = "image/png"
		}
		if _, err := canonicalTextProjection(meta, ref, "repo://owned/text", body); err == nil {
			t.Fatal("nontext canonical content was projected as text")
		}
	}
	body := []byte("text")
	meta, ref := textProjectionFixture(t, body)
	other, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000004")
	meta.Scope, _ = id.InProject(other)
	if _, err := canonicalTextProjection(meta, ref, "repo://owned/text", body); err == nil {
		t.Fatal("foreign canonical scope was published with a local reference")
	}
}
