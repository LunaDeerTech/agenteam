#!/usr/bin/env python3
"""Independent frozen dispatch diff and actual Project->D10 rejection controls."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=Path('/workspace/agenteam-secret-variable-owner-service'))
parser.add_argument('--go', action='store_true')
args = parser.parse_args()
repo = args.repo.resolve()
here = Path(__file__).resolve().parent
prefix = 'internal/central/'
paths = [prefix + p for p in (
    'project/audit_facts.go', 'project/projectvariable_event_authority.go', 'project/audit_facts_test.go',
    'project/secret_variable_facts.go', 'project/secret_variable_facts_test.go',
    'projectvariable/authority.go', 'projectvariable/commands.go', 'projectvariable/reader.go',
    'projectvariable/repository.go')]
paths += ['.agent-state/current.md', 'docs/development/work-items/d10-secret-variable-owner-service.md']


def blobs():
    values = subprocess.check_output(['git', 'hash-object', *paths], cwd=repo, text=True).splitlines()
    return dict(zip(paths, values, strict=True))


initial = blobs()
old = lambda p: subprocess.check_output(['git', 'show', 'ce65714a:' + p], cwd=repo, text=True)
removals = {
    'project/audit_facts.go': ['\t\tif audit.ProjectSecretVariableAction(f.Action) {\n\t\t\treturn a.checkSecretVariableAuditInTx(ctx, tx, entry, key)\n\t\t}\n'],
    'project/projectvariable_event_authority.go': [
        '\tif request.Details().Event.Header.EventType == "project.secret_variable_changed" {\n\t\treturn a.discoverSecretVariableEvent(request)\n\t}\n',
        '\tif request.Details().Event.Header.EventType == "project.secret_variable_changed" {\n\t\treturn a.validateSecretVariableEventInTx(ctx, tx, request, deps)\n\t}\n'],
    'projectvariable/authority.go': [
        '\tif summary.Header.EventType == c.SecretVariableChangedName {\n\t\treturn a.discoverSecretAppend(ctx, actor, summary)\n\t}\n',
        '\tif summary.Header.EventType == c.SecretVariableChangedName {\n\t\treturn a.validateSecretAppend(ctx, tx, actor, summary, deps, stage)\n\t}\n',
        '\tif ac.ProjectSecretVariableAction(entry.Fields().Action) {\n\t\treturn a.checkSecretProjectAudit(ctx, tx, entry, key)\n\t}\n'],
}
for name, blocks in removals.items():
    path = prefix + name
    actual = (repo / path).read_text()
    for block in blocks:
        assert actual.count(block) == 1
        actual = actual.replace(block, '')
    assert actual == old(path), path
for name, expected in (('commands.go', 2), ('reader.go', 1), ('repository.go', 1)):
    path = prefix + 'projectvariable/' + name
    actual = (repo / path).read_text()
    assert actual.count(" AND type='variable'") == expected
    actual = actual.replace(" AND type='variable'", '')
    if name == 'repository.go':
        actual = actual.replace('// Package projectvariable owns ordinary values and Secret variable metadata.\n// Secret material and protected intent receipts belong to the Secret producer.',
                                '// Package projectvariable owns ordinary project configuration, not Secrets.')
    assert actual == old(path), path
name_query = 'SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=$1 AND name=$2 AND id<>$3 AND deleted_at IS NULL)'
for name in ('commands.go', 'secret_mutation.go'):
    assert name_query in (repo / prefix / 'projectvariable' / name).read_text()
print(json.dumps({'inverse_dispatch_files': 3, 'ordinary_type_filters': 4,
                  'cross_type_name_checks': 2, 'frozen_blobs': initial}), flush=True)

if args.go:
    stat = os.statvfs(repo)
    available = stat.f_bavail * stat.f_frsize
    print(json.dumps({'utc': datetime.now(timezone.utc).isoformat(), 'fresh_available': available}), flush=True)
    if available < 5368709120:
        raise SystemExit(78)
    output = here.parents[1] / 'output/ai/secret-variable-routing-review'
    output.mkdir(parents=True, exist_ok=True)
    tmp = output / 'tmp'
    tmp.mkdir(exist_ok=True)
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo / prefix / 'project/variables_independent_routing_test.go'): str(here / 'independent_test.go'),
    }}))
    env = {k: v for k, v in os.environ.items() if not k.startswith('AGENTEAM_')}
    go = env.get('REVIEW_GO', '/workspace/toolchains/go1.27.1/bin/go')
    env.update(PATH=str(Path(go).parent)+':'+env['PATH'], GOTOOLCHAIN='local', GOENV='off', GOWORK='off',
               GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly -p=1',
               GOMODCACHE=env.get('REVIEW_GOMODCACHE', '/workspace/agenteam/output/ai/model-ui-recovery/go-mod'),
               GOCACHE=env.get('REVIEW_GOCACHE', str(here.parents[1] / 'output/ai/project-variables-ui/implementation/gocache')),
               TMPDIR=str(tmp), GOTMPDIR=str(tmp))
    command = [go, 'test', '-mod=readonly', '-p=1', '-race', '-count=1', '-timeout=45s', '-vet=off',
               '-overlay='+str(overlay), '-run', '^TestIndependentSecret(Route|Dependencies)', './internal/central/project']
    result = subprocess.run(command, cwd=repo, env=env)
    print(json.dumps({'go_actual_wait': result.returncode, 'eleven_inputs_unchanged': blobs() == initial}), flush=True)
    assert blobs() == initial
    raise SystemExit(result.returncode)
