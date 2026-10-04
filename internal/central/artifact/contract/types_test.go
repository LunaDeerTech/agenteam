package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func testID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func testMetadata(t *testing.T) Metadata {
	t.Helper()
	project := testID[identity.Project](t)
	scope, _ := identity.InProject(project)
	at, _ := foundation.NewInstant(time.Now())
	m, e := NewMetadata(MetadataDetails{Reference: ArtifactRef{project, testID[ArtifactEntity](t), testID[File](t)}, Kind: Generated, Name: "name-canary.txt", Description: "description-canary", Object: oc.ObjectMeta{ID: testID[oc.StoredObject](t), Scope: scope, MediaType: "text/plain", ByteSize: 9, SHA256: foundation.Digest("sha256:" + strings.Repeat("a", 64)), State: oc.Available, Version: 1, CreatedAt: at}, CreatedByUserID: testID[identity.User](t).String(), CreatedAt: at, Version: 1})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestArtifactInputsUseUTF8ByteBoundsAndClosedKinds(t *testing.T) {
	if (Display{Name: strings.Repeat("é", 127) + "x", Description: strings.Repeat("界", 1365) + "x"}).Validate() != nil {
		t.Fatal("exact byte bounds rejected")
	}
	for _, d := range []Display{{Name: ""}, {Name: "a/b"}, {Name: "a\\b"}, {Name: "a\n"}, {Name: strings.Repeat("é", 128)}, {Name: "ok", Description: strings.Repeat("x", 4097)}, {Name: "ok", Description: string([]byte{0xff})}} {
		if d.Validate() == nil {
			t.Fatal("invalid display accepted")
		}
	}
	if ValidateInline(strings.Repeat("x", MaxInlineBytes)) != nil || ValidateInline("") != nil {
		t.Fatal("inline exact/empty bound rejected")
	}
	if ValidateInline(strings.Repeat("x", MaxInlineBytes+1)) == nil || ValidateInline(string([]byte{0xff})) == nil {
		t.Fatal("invalid inline accepted")
	}
	for _, filter := range []ListFilter{{Kind: "future"}, {NameQuery: strings.Repeat("界", 86)}, {MediaType: "Text/Plain"}, {ExecutionID: "not-an-id"}} {
		if filter.Validate() == nil {
			t.Fatal("invalid filter accepted")
		}
	}
	if (ListFilter{NameQuery: "100%_literal\\path"}).Validate() != nil {
		t.Fatal("literal search rejected")
	}
	preview, e := (PreviewRequest{}).Normalize()
	if e != nil || preview.Limit != DefaultPreviewBytes {
		t.Fatal("preview default")
	}
	if _, e = (PreviewRequest{Limit: MaxPreviewBytes + 1}).Normalize(); e == nil {
		t.Fatal("unbounded preview")
	}
}
func TestArtifactTrustedInvocationAndResultProjection(t *testing.T) {
	project := testID[identity.Project](t)
	agent := testID[identity.Agent](t)
	execution := testID[identity.Execution](t)
	actor, _ := identity.NewAgentRun(project, agent, execution)
	d := InvocationDetails{Actor: actor, ProjectID: project, ExecutionID: execution.String(), AttemptID: testID[CallAttempt](t)}
	call, e := NewInvocation(d)
	if e != nil {
		t.Fatal(e)
	}
	d.ExecutionID = testID[identity.Execution](t).String()
	if _, e = NewInvocation(d); e == nil {
		t.Fatal("actor execution forgery")
	}
	m := testMetadata(t)
	dm := m.Details()
	dm.Name = "changed"
	if m.Details().Name == dm.Name {
		t.Fatal("mutable metadata")
	}
	result, e := NewReadResult(ReadDetails{Artifact: m, Projection: TextProjection, Text: "界", Offset: 0, NextOffset: 3, Truncated: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []ReadDetails{{Artifact: m, Projection: TextProjection, Text: "界", NextOffset: 2, Truncated: true}, {Artifact: m, Projection: FileProjection, Text: "secret"}, {Artifact: m, Projection: ImageProjection, InvalidUTF8: true}, {Artifact: m, Projection: TextProjection, Text: string([]byte{0xff}), NextOffset: 1, Truncated: true}} {
		if _, e = NewReadResult(d); e == nil {
			t.Fatal("inconsistent projection")
		}
	}
	var log bytes.Buffer
	for _, v := range []any{m, result, call, struct {
		metadata Metadata
		result   ReadResult
		call     Invocation
	}{m, result, call}} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&log, f, v)
		}
		slog.New(slog.NewTextHandler(&log, nil)).Info("value", "value", v)
		slog.New(slog.NewJSONHandler(&log, nil)).Info("value", "value", v)
	}
	for _, canary := range []string{"name-canary", "description-canary", "界", agent.String(), execution.String()} {
		if strings.Contains(log.String(), canary) {
			t.Fatal("private value leaked via fmt/slog")
		}
	}
	for _, v := range []any{new(Metadata), new(Invocation), new(ReadResult)} {
		if json.Unmarshal([]byte(`{}`), v) == nil {
			t.Fatal("JSON manufactured trusted object")
		}
	}
	ref, e := m.Details().Reference.BusinessFile()
	if e != nil || ref.Details().Kind != oc.ArtifactFile || ref.Details().FileID != m.Details().Reference.FileID.String() {
		t.Fatal("business ref lost exact file")
	}
}
