package contract

import "testing"

func TestWorkArtifactBuiltinBindingClosedSet(t *testing.T) {
	for _, name := range []string{"task.read", "task.transfer", "artifact.list", "artifact.create", "artifact.read"} {
		if !WorkArtifactBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 1}) {
			t.Fatalf("missing collaboration binding %s", name)
		}
		if WorkArtifactBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 2}) {
			t.Fatalf("unimplemented revision accepted for %s", name)
		}
	}
	for _, name := range []string{"", "skill.install", "task.delete", "artifact.delete", "artifact.read.other-project"} {
		if WorkArtifactBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 1}) {
			t.Fatalf("unimplemented collaboration binding accepted: %s", name)
		}
	}
}

func TestSkillDomainBindingsKeepInstallOnOriginalLedger(t *testing.T) {
	for _, name := range []string{"skill.list", "skill.read", "skill.assign", "skill.prepare"} {
		if !DomainBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 1}) || DomainBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 2}) {
			t.Fatalf("invalid Skill domain binding revision: %s", name)
		}
	}
	for _, name := range []string{"skill.install", "skill.delete", "skill.list-all"} {
		if DomainBuiltinBinding(BuiltinBinding{HandlerID: name, ContractRevision: 1}) {
			t.Fatalf("unimplemented or original-ledger binding admitted: %s", name)
		}
	}
}
