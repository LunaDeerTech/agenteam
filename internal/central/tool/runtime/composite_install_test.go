package runtime

import (
	"testing"

	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestCompositeInstallKeepsOriginalDispatcher(t *testing.T) {
	_, _, _, call, _ := operationFixture(t)
	install := &InstallDispatcher{}
	combined := &CompositeExecutor{install: install, builtins: &BuiltinExecutor{}}
	selected, err := combined.selectCall(call)
	if err != nil || selected != install {
		t.Fatal("install was routed through a different operation owner", err)
	}
	combined.install = nil
	if _, err := combined.selectCall(call); err == nil {
		t.Fatal("unbound original install owner admitted")
	}
	combined.install = install
	call.Binding.ContractRevision++
	if _, err := combined.selectCall(call); err == nil {
		t.Fatal("unimplemented install revision admitted")
	}
	call.Binding = tc.BuiltinBinding{HandlerID: "skill.read", ContractRevision: 1}
	selected, err = combined.selectCall(call)
	if err != nil || selected != combined.builtins {
		t.Fatal("ordinary Skill reads escaped their original builtin owner", err)
	}
}
