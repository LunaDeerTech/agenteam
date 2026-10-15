package runtime

import (
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This tests the additional closed handler and exact private owner boundary.
// Real old-process flock proof remains the child-process integration test.
func TestSkillPrepareColdProofCannotRebindOriginalLookup(t *testing.T) {
	for _, name := range []string{"valid", "execute", "expired", "process", "attempt", "request", "local", "handler", "revision"} {
		t.Run(name, func(t *testing.T) {
			e, call, proof := coldProofFixture(t)
			call.binding.Binding = tc.BuiltinBinding{HandlerID: string(sc.PrepareSkill), ContractRevision: 1}
			call.record.Input = bindingProjection(call.binding)
			raw, err := encode(call.record.Input)
			if err != nil {
				t.Fatal(err)
			}
			call.record.InputDigest, call.record.Fingerprint = digest(raw), digest(raw)
			proof.binding, proof.record = call.binding, call.record
			switch name {
			case "execute":
				call.lookup = false
			case "expired":
				proof.active.Store(false)
			case "process":
				proof.process = e.authority.process
			case "attempt":
				call.record.Attempt = newOperationID[tc.Attempt](t)
			case "request":
				call.record.Request = "changed"
			case "local":
				e.calls[call.binding.Identity()] = &builtinCall{}
			case "handler":
				call.binding.Binding.HandlerID = "skill.install"
			case "revision":
				call.binding.Binding.ContractRevision++
			}
			if proof.permits(e.authority.process, call.record) != (name == "valid") {
				t.Fatal("cold prepare proof crossed original lookup boundary")
			}
			if proof.process == (oc.ProcessID{}) {
				t.Fatal("fixture omitted prior process")
			}
		})
	}
}
