package contract

import (
	"context"
	"encoding/json"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"testing"
)

func TestFirstToolMediaDigestBindsArtifactAndOrder(t *testing.T) {
	project := fresh[id.Project](t)
	ref := func() oc.BusinessFileRef {
		r, e := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: project, ArtifactID: fresh[struct{}](t).String(), FileID: fresh[struct{}](t).String()})
		must(t, e)
		return r
	}
	messages := []Message{{Role: "system", Parts: []MessagePart{{Text: &TextPart{Text: "system"}}}}, {Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "read"}}, {Image: &MediaPart{Ref: ref(), MediaType: "image/png"}}, {File: &MediaPart{Ref: ref(), MediaType: "application/pdf"}}}}}
	tools := []Tool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	digest, e := ToolInputDigest(context.Background(), messages, tools, ToolChoice{Kind: "auto"})
	must(t, e)
	raw, e := EncodeCanonicalMessages(messages)
	must(t, e)
	decoded, e := DecodeCanonicalMessages(raw)
	must(t, e)
	restored, e := ToolInputDigest(context.Background(), decoded, tools, ToolChoice{Kind: "auto"})
	must(t, e)
	if restored != digest {
		t.Fatal("media source lost during replay")
	}
	decoded[1].Parts[1], decoded[1].Parts[2] = decoded[1].Parts[2], decoded[1].Parts[1]
	changed, e := ToolInputDigest(context.Background(), decoded, tools, ToolChoice{Kind: "auto"})
	must(t, e)
	if changed == digest {
		t.Fatal("media order omitted")
	}
	decoded[1].Parts[2].Image.Ref = ref()
	changed2, e := ToolInputDigest(context.Background(), decoded, tools, ToolChoice{Kind: "auto"})
	must(t, e)
	if changed2 == changed {
		t.Fatal("media identity omitted")
	}
	messages[0].Parts = append(messages[0].Parts, messages[1].Parts[1])
	if _, e = ToolInputDigest(context.Background(), messages, tools, ToolChoice{Kind: "auto"}); e == nil {
		t.Fatal("media promoted to system instruction")
	}
}
