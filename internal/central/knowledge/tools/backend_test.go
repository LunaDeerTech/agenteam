package tools

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"testing"
)

func TestKnowledgeBuiltinClosedRegistrationAndNoReissue(t *testing.T) {
	ports, err := NewBackends(&knowledge.IndexService{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 {
		t.Fatal("wrong fixed tool set")
	}
	for _, port := range ports {
		registration := port.Registration()
		if err := registration.Validate(context.Background()); err != nil {
			t.Fatal("invalid Core definition", err)
		}
		if registration.Class != tc.CoreTool || !registration.Definition.Annotations.ReadOnly || registration.Definition.Annotations.Idempotency != tc.Inherent {
			t.Fatal("knowledge must remain inherent read")
		}
		if _, err := port.Execute(context.Background(), tr.BuiltinInvocation{}, []byte(`{}`)); err == nil {
			t.Fatal("naked execution admitted")
		}
		if body, found, err := port.Lookup(context.Background(), tr.BuiltinInvocation{}, nil); err == nil || found || body != nil {
			t.Fatal("recovery reissued an unbound read")
		}
	}
}
