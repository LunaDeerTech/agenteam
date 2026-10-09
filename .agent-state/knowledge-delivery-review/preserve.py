#!/usr/bin/env python3
"""Check the shared merge and confined initialization fixture adaptation."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
MAIN = 'e94077ebe1810d9d61918d5fe4a4d932d8bbfba2'
KNOWLEDGE = 'aaa408c8'
PREFIX = 'internal/central/project/'


def original(ref, name):
    return subprocess.check_output(['git', 'show', ref+':'+PREFIX+name], cwd=ROOT, text=True)


def remove_once(text, value):
    if text.count(value) != 1:
        raise AssertionError('expected exact single merge fragment')
    return text.replace(value, '', 1)


facts = (ROOT/PREFIX/'audit_facts.go').read_text()
old = original(KNOWLEDGE, 'audit_facts.go')
marker = '\nfunc (a *Authority) checkKnowledgeAuditInTx('
function = old[old.index(marker):]
facts = remove_once(facts, function)
facts = facts.replace('audit.SecretProducer, audit.ProjectVariableProducer, audit.KnowledgeProducer, audit.ObjectProducer:', 'audit.SecretProducer, audit.ProjectVariableProducer:', 1)
facts = facts.replace('\t\t\tfacts[producer] = provider\n', '\t\t\tfacts[producer] = provider\n\t\tcase audit.ObjectProducer:\n\t\t\t// Reserved for the separately verified Object fact authority.\n\t\t\treturn nil, fault(foundation.DependencyUnbound)\n', 1)
for producer in ('Knowledge', 'Object'):
    facts = remove_once(facts, f'\tif k.Producer == audit.{producer}Producer {{\n\t\treturn a.check{producer}AuditInTx(ctx, tx, entry, key)\n\t}}\n')
assert facts == original(MAIN, 'audit_facts.go'), 'main Audit branches changed'

events = (ROOT/PREFIX/'events.go').read_text()
events = events.replace('// lifecycle facts, plus the Model, Work, ProjectVariable and Knowledge gates.\n// Other delivery remains unbound; each producer proves its own facts.', '// lifecycle facts, plus the Model-only Project gate. Other delivery remains unbound.', 1)
for call in ('return a.discoverKnowledgeEvent(request)', 'return a.validateKnowledgeEventInTx(ctx, tx, request, deps)'):
    events = remove_once(events, '\tif d.Kind == oc.AppendProject && d.Event.Producer == "knowledge" {\n\t\t'+call+'\n\t}\n')
assert events == original(MAIN, 'events.go'), 'main event branches changed'

tests = (ROOT/PREFIX/'audit_facts_test.go').read_text()
marker = '\nfunc TestSecretVariableAuditReadShapeDoesNotAuthorizeProjectFacts('
assert tests.count(marker) == 1
tests = tests[:tests.index(marker)]
tests = tests.replace('\t\t{"object-nil", ac.ObjectProducer, nil, foundation.DependencyUnbound},\n\t\t{"object-typed-nil", ac.ObjectProducer, typedNil, foundation.DependencyUnbound},', '\t\t{"object-reserved", ac.ObjectProducer, checker, foundation.DependencyUnbound},', 1)
assert tests == original(MAIN, 'audit_facts_test.go'), 'main Audit tests changed'
before = original(MAIN, 'initialization_audit_test.go')
after = (ROOT/PREFIX/'initialization_audit_test.go').read_text()
start = 'func TestInitializationAuditAuthorityDelegation(t *testing.T) {'
end = '\tt.Run("ordinary-producer", func(t *testing.T) {'
assert before[:before.index(start)] == after[:after.index(start)], 'Service initialization tests changed'
assert before[before.index(end, before.index(start)):] == after[after.index(end, after.index(start)):], 'other initialization delegation tests changed'
print('PASS main branches preserved, Knowledge checker exact, initialization delta confined to two ordinary delegate cases')
