#!/usr/bin/env python3
"""Independent Runner/main merge inverse and combined production-method probes."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=Path('/workspace/agenteam-runner-control-delivery'))
parser.add_argument('--go', action='store_true')
args = parser.parse_args()
repo, here = args.repo.resolve(), Path(__file__).resolve().parent
prefix = 'internal/central/'
paths = [prefix+p for p in ('app/account.go', 'audit/contract/metadata.go', 'audit/contract/types.go')]
paths += ['docs/development/backend/README.md', 'docs/development/work-items/d15-runner-control.md']
initial = {p: (repo/p).read_bytes() for p in paths}


def old(path):
    return subprocess.check_output(['git', 'show', '4c1db71:'+path], cwd=repo, text=True)


def remove(source, block):
    assert source.count(block) == 1, block[:90]
    return source.replace(block, '')


p = prefix+'app/account.go'
source = initial[p].decode()
for block in (
    '\trunners      accountWork\n',
    '\t// Runner owns hijacked sockets and generation/Audit transactions; HTTP\n\t// shutdown alone cannot retire them. Account and DB must remain until join.\n\tif a.runners != nil {\n\t\twork = append(work, a.runners)\n\t}\n',
    '\trunnerAuthority, err := createRunnerAuthority(db, authority)\n\tif err != nil {\n\t\treturn err\n\t}\n',
    '\trunners, err := createRunnerControl(runnerAuthority, auditor)\n\tif err != nil {\n\t\treturn err\n\t}\n\tif !accounts.install(ctx, func() { accounts.runners = runners }) {\n\t\treturn context.Canceled\n\t}\n',
    '\trunnerAdmin, runnerDevice, err := runnerControlHandlers(runners, core, cfg.PublicOrigin())\n\tif err != nil {\n\t\treturn err\n\t}\n',
    '\t\taccounts.handler = runnerControlRoutes(accounts.handler, runnerAdmin, runnerDevice)\n',
):
    source = remove(source, block)
source = source.replace('createSecurityWithRunners(cfg, db, authority, modelAuthority, projectUsage.projects, runnerAuthority)',
                        'createSecurity(cfg, db, authority, modelAuthority, projectUsage.projects)')
assert source == old(p), p
p = prefix+'audit/contract/metadata.go'
source = remove(initial[p].decode(), '\tif RunnerAction(action) {\n\t\treturn decodeRunnerMetadata(action, raw)\n\t}\n')
assert source == old(p), p
p = prefix+'audit/contract/types.go'
source = initial[p].decode().replace('RunnerAction(a) || ', '').replace('ProjectVariableResource, RunnerResource:', 'ProjectVariableResource:')
source = source.replace('p == RunnerProducer || ', '')
for block in (
    '\tif (a.Kind == identity.Service && a.ServiceName == identity.RunnerIdentity || r.Kind == RunnerResource) && !RunnerAction(f.Action) {\n\t\treturn Entry{}, invalid("runner_entry")\n\t}\n\tif RunnerAction(f.Action) {\n\t\tif err := validateRunnerEntry(f); err != nil {\n\t\t\treturn Entry{}, err\n\t\t}\n\t}\n',
    '\tif RunnerAction(action) {\n\t\treturn RunnerProducer\n\t}\n',
    '\tif producer == RunnerProducer && (!validID(causeRef) || ordinal > 1) {\n\t\treturn AppendKey{}, invalid("append_key")\n\t}\n',
):
    source = remove(source, block)
assert source == old(p), p
for p in ('app/security.go', 'app/runner_control.go', 'audit/service.go', 'audit/query.go'):
    path = prefix+p
    assert (repo/path).read_bytes() == subprocess.check_output(['git', 'show', '101a7:'+path], cwd=repo)
protected = ['internal/central/object', 'internal/central/project', 'internal/central/projectvariable',
             'internal/central/audit/http', 'api/openapi/audit.json', 'api/openapi/project-audit.json']
assert not subprocess.check_output(['git', 'diff', '--name-only', '4c1db71', '--', *protected], cwd=repo)
p = 'docs/development/backend/README.md'
marker = '## D05 B01 对象库\n'
assert initial[p].decode().split(marker, 1)[1] == old(p).split(marker, 1)[1]
print(json.dumps({'three_shared_inverse': True, 'runner_adjacent_unchanged': True,
                  'protected_main_domains': True, 'backend_result_sections_unchanged': True}), flush=True)

if args.go:
    stat = os.statvfs(repo)
    available = stat.f_bavail*stat.f_frsize
    print(json.dumps({'utc': datetime.now(timezone.utc).isoformat(), 'fresh_available': available}), flush=True)
    if available < 5368709120:
        raise SystemExit(78)
    output = here.parents[1]/'output/ai/runner-delivery-review'
    output.mkdir(parents=True, exist_ok=True)
    tmp = output/'tmp'
    tmp.mkdir(exist_ok=True)
    overlay = output/'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo/prefix/'app/independent_runner_variable_merge_test.go'): str(here/'app_test.go'),
        str(repo/prefix/'audit/contract/independent_runner_variable_merge_test.go'): str(here/'audit_test.go'),
    }}))
    env = {k: v for k, v in os.environ.items() if not k.startswith('AGENTEAM_')}
    go = env.get('REVIEW_GO', '/workspace/toolchains/go1.27.1/bin/go')
    env.update(PATH=str(Path(go).parent)+':'+env['PATH'], GOENV='off', GOWORK='off', GOTOOLCHAIN='local',
               GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly -p=1',
               GOCACHE=env.get('REVIEW_GOCACHE', str(here.parents[1]/'output/ai/project-variables-ui/implementation/gocache')),
               GOMODCACHE=env.get('REVIEW_GOMODCACHE', '/workspace/agenteam-runner-control/output/ai/runner-control/go-mod'),
               GOTMPDIR=str(tmp), TMPDIR=str(tmp))
    command = [go, 'test', '-json', '-mod=readonly', '-p=1', '-race', '-count=1', '-timeout=45s', '-vet=off',
               '-overlay='+str(overlay), '-run', '^TestIndependentRunner(Variables|VariableAudit)',
               './internal/central/app', './internal/central/audit/contract']
    with (output/'go.jsonl').open('w') as log:
        result = subprocess.run(command, cwd=repo, env=env, stdout=log, stderr=subprocess.STDOUT)
    events = []
    for line in (output/'go.jsonl').read_text().splitlines():
        try: events.append(json.loads(line))
        except json.JSONDecodeError: print(line)
    expected = {'TestIndependentRunnerVariablesCombinedRetirement', 'TestIndependentRunnerVariablesRouteComposition', 'TestIndependentRunnerVariableAuditPartition'}
    tops = {e['Test'] for e in events if e.get('Action') == 'pass' and '/' not in e.get('Test', '/')}
    print(json.dumps({'actual_wait': result.returncode, 'top_pass': sorted(tops),
                      'child_pass': sum(e.get('Action') == 'pass' and '/' in e.get('Test', '') for e in events),
                      'frozen_inputs_unchanged': all((repo/p).read_bytes() == v for p, v in initial.items())}), flush=True)
    if result.returncode:
        print(''.join(e.get('Output', '') for e in events if e.get('Action') == 'output')[-6000:])
    assert all((repo/p).read_bytes() == v for p, v in initial.items())
    assert result.returncode == 0 and tops == expected
    assert not any(e.get('Action') in ('skip', 'fail') for e in events)
