#!/usr/bin/env python3
"""Read-only source composition and independent Project dispatch controls."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-knowledge-delivery')
BASE = 'e94077eb'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')


def old(revision, path):
    return subprocess.check_output(['git', 'show', revision + ':' + path], cwd=ROOT).decode()


def replace_once(text, before, after=''):
    assert text.count(before) == 1, before
    return text.replace(before, after, 1)


def sources():
    paths = sorted(str(p.relative_to(ROOT)) for p in (ROOT / 'internal/central/knowledge').glob('*.go'))
    paths += ['internal/central/knowledge/contract/events.go', 'internal/central/knowledge/contract/events_test.go', 'db/migrations/00025_knowledge.sql']
    paths += ['internal/central/project/' + name for name in ('knowledge_audit_test.go', 'knowledge_event_authority.go', 'knowledge_event_authority_test.go', 'object_audit_facts.go', 'object_audit_facts_test.go')]
    paths += sorted(str(p.relative_to(ROOT)) for p in (ROOT / 'tests/knowledge').glob('*.go'))
    assert len(paths) == 44
    for name in paths:
        origin = '924d5627' if Path(name).name.startswith('b02_independent_') else 'aaa408c8'
        assert (ROOT / name).read_text() == old(origin, name), name

    name = 'internal/central/project/audit_facts.go'
    original = (ROOT / name).read_text()
    current = replace_once(original,
        '\t\tcase audit.SecretProducer, audit.ProjectVariableProducer, audit.KnowledgeProducer, audit.ObjectProducer:',
        '\t\tcase audit.SecretProducer, audit.ProjectVariableProducer:')
    current = replace_once(current, '\t\t\tfacts[producer] = provider\n',
        '\t\t\tfacts[producer] = provider\n\t\tcase audit.ObjectProducer:\n\t\t\t// Reserved for the separately verified Object fact authority.\n\t\t\treturn nil, fault(foundation.DependencyUnbound)\n')
    current = replace_once(current,
        '\tif k.Producer == audit.KnowledgeProducer {\n\t\treturn a.checkKnowledgeAuditInTx(ctx, tx, entry, key)\n\t}\n\tif k.Producer == audit.ObjectProducer {\n\t\treturn a.checkObjectAuditInTx(ctx, tx, entry, key)\n\t}\n')
    marker = '\nfunc (a *Authority) checkKnowledgeAuditInTx('
    start = current.index(marker)
    author = old('aaa408c8', name)
    assert current[start:] == author[author.index(marker):]
    assert current[:start] == old(BASE, name)

    name = 'internal/central/project/events.go'
    current = (ROOT / name).read_text()
    current = replace_once(current,
        '// lifecycle facts, plus the Model, Work, ProjectVariable and Knowledge gates.\n// Other delivery remains unbound; each producer proves its own facts.',
        '// lifecycle facts, plus the Model-only Project gate. Other delivery remains unbound.')
    for statement in ('return a.discoverKnowledgeEvent(request)', 'return a.validateKnowledgeEventInTx(ctx, tx, request, deps)'):
        current = replace_once(current, '\tif d.Kind == oc.AppendProject && d.Event.Producer == "knowledge" {\n\t\t' + statement + '\n\t}\n')
    assert current == old(BASE, name)
    name = 'internal/central/project/initialization_audit_test.go'
    before, after = old(BASE, name), (ROOT / name).read_text()
    start = 'func TestInitializationAuditAuthorityDelegation(t *testing.T) {'
    end = '\tt.Run("ordinary-producer", func(t *testing.T) {'
    assert before[:before.index(start)] == after[:after.index(start)]
    assert before[before.index(end, before.index(start)):] == after[after.index(end, after.index(start)):]
    for name in ('initialization_audit.go', 'authority.go', 'audit_authority.go',
                 'projectvariable_event_authority.go', 'external_event_authority.go', 'work_event_authority.go'):
        path = 'internal/central/project/' + name
        assert (ROOT / path).read_text() == old(BASE, path), path
    assert not subprocess.check_output(['git', 'diff', BASE, '--', 'internal/central/audit'], cwd=ROOT)
    print('PASS 44 exact origins; shared source inverse equals main; adjacent Audit/Project dispatch retained', flush=True)


def main():
    sources()
    names = ('audit_facts.go', 'audit_facts_test.go', 'events.go', 'initialization_audit_test.go')
    frozen = {name: (ROOT / 'internal/central/project' / name).read_bytes() for name in names}
    env = os.environ.copy()
    env.update({
        'PATH': str(GO.parent) + os.pathsep + env.get('PATH', ''),
        'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTELEMETRY': 'off',
        'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly',
        'GOMODCACHE': '/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
        'GOCACHE': '/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache',
        'GOTMPDIR': '/workspace/agenteam-skills/output/ai/skills/compile/tmp',
    })
    with tempfile.TemporaryDirectory(prefix='knowledge-delivery-review-', dir=env['GOTMPDIR']) as directory:
        overlay = Path(directory) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(ROOT / 'internal/central/project/independent_knowledge_delivery_test.go'): str(HERE / 'shared_test.go'),
        }}))
        result = subprocess.run([str(GO), 'test', '-race', '-count=1', '-p=1',
                                 '-overlay=' + str(overlay), '-timeout=30s', '-v',
                                 '-run=^TestIndependentKnowledgeDelivery', './internal/central/project'],
                                cwd=ROOT, env=env, timeout=90)
        assert all((ROOT / 'internal/central/project' / name).read_bytes() == value for name, value in frozen.items()), 'shared input changed during control'
        if result.returncode:
            raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
