package knowledge

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeInvocationArgumentsBindVersionAndClosedInput(t *testing.T) {
	valid := []byte(`{"document_id":"018f0000-0000-7000-8000-000000000001","document_version":"2"}`)
	parsed, err := parseReadArguments(valid)
	if err != nil || parsed.Version != 2 || parsed.Request.MaxBytes != 8192 || parsed.Request.ByteOffset != 0 {
		t.Fatal("valid reference/defaults rejected", err)
	}
	v := &invocationRead{document: parsed.Document, version: parsed.Version, read: parsed.Request}
	if err = v.matchArguments(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"document_id":"018f0000-0000-7000-8000-000000000001","document_version":"3"}`,
		`{"document_id":"018f0000-0000-7000-8000-000000000002","document_version":"2"}`,
		`{"document_id":"018f0000-0000-7000-8000-000000000001","document_version":"2","document_version":"2"}`,
		`{"document_id":"018f0000-0000-7000-8000-000000000001","document_version":"2","project_id":"018f0000-0000-7000-8000-000000000002"}`,
		`{"document_id":"018f0000-0000-7000-8000-000000000001","document_version":"2","max_bytes":null}`,
	} {
		if err = v.matchArguments(context.Background(), []byte(raw)); err == nil {
			t.Fatal("changed/unbound argument accepted")
		}
	}
}

func TestKnowledgeAgentActorAloneCannotRead(t *testing.T) {
	p := newID[id.Project](t)
	a, err := id.NewAgentRun(p, newID[id.Agent](t), newID[id.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	if err = readInput(context.Background(), a, p); err == nil {
		t.Fatal("actor shape replaced invocation authority")
	}
	var service *Service
	_, err = service.ReadForInvocation(context.Background(), kc.Invocation{}, newID[kc.Document](t), f.Version(1), kc.ReadRequest{MaxBytes: 8192})
	if err == nil {
		t.Fatal("unbound service/invocation read")
	}
}
