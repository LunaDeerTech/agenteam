#!/bin/sh
# Real HTTP projection, in-memory only: no server, browser or database fixture.
set -eu
probe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 - "$probe_dir" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

probe = Path(sys.argv[1])
source = probe.parents[2]
delivery = Path('/workspace/agenteam-delivery')
for name in ('project_owner_models_web_fixture_test.go', 'project_owner_models_web_test.go'):
    rel = Path('tests/account') / name
    if (source / rel).read_bytes() != (delivery / rel).read_bytes():
        raise SystemExit('Frozen Model Go input differs from delivery input')
output = source / 'output/ai/resource-identity-recovery/model-problem'
output.mkdir(parents=True, exist_ok=True)
run = Path(tempfile.mkdtemp(prefix='run-', dir=output))
for name in ('tmp', 'config', 'cache'):
    (run / name).mkdir()
env = os.environ.copy()
env.update(GOTOOLCHAIN='local', GOENV='off', GOPROXY='off', GOSUMDB='off', GOWORK='off',
           GOFLAGS='', GOMAXPROCS='2', CGO_ENABLED='1', GOCACHE=str(output / 'go-cache'),
           GOMODCACHE='/home/agent/go/pkg/mod', TMPDIR=str(run / 'tmp'), GOTMPDIR=str(run / 'tmp'),
           XDG_CONFIG_HOME=str(run / 'config'), XDG_CACHE_HOME=str(run / 'cache'), GOTELEMETRY='off')
go = '/workspace/toolchains/go1.27.1/bin/go'
version = subprocess.run([go, 'version'], cwd=delivery, env=env, text=True, capture_output=True, timeout=5)
if version.returncode or not version.stdout.startswith('go version go1.27.1 '):
    raise SystemExit('Go 1.27.1 required')
overlay = run / 'overlay.json'
overlay.write_text(json.dumps({'Replace': {
    str(delivery / 'tests/account/zz_independent_model_problem_test.go'): str(probe / 'probe_test.go')
}}) + '\n')
cmd = [go, 'test', '-tags=integration', '-mod=readonly', '-race', '-count=1', '-p=2', '-timeout=30s', '-json',
       '-overlay=' + str(overlay), '-run=^TestIndependentModelProblem', './tests/account']
print(version.stdout.strip(), flush=True)
print(' '.join(cmd), flush=True)
print('evidence:', run, flush=True)
p = subprocess.Popen(cmd, env=env, cwd=delivery, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                     text=True, start_new_session=True)
timed_out = False
try:
    log, _ = p.communicate(timeout=300)
except subprocess.TimeoutExpired:
    timed_out = True
    os.killpg(p.pid, signal.SIGTERM)
    try:
        log, _ = p.communicate(timeout=1)
    except subprocess.TimeoutExpired:
        os.killpg(p.pid, signal.SIGKILL)
        log, _ = p.communicate(timeout=1)
(run / 'go-test.jsonl').write_text(log)
(run / 'exit.txt').write_text(f'wait_returncode={p.returncode}\ntimeout={timed_out}\n')
events = []
for line in log.splitlines():
    try:
        events.append(json.loads(line))
    except json.JSONDecodeError:
        pass
top = {'TestIndependentModelProblem' + suffix for suffix in ('Boundary', 'ClosedBody', 'ResponseBinding')}
children = set()
for suffix, names in {
    'Boundary': 'INVALID_STATE RESOURCE_BUSY FORBIDDEN NOT_FOUND PROJECT_NOT_ACTIVE VERSION_CONFLICT COMMIT_UNKNOWN CURSOR_STALE',
    'ClosedBody': 'resource-instance query-instance escaped-instance fragment-instance detail title type unknown-code different-code status request-id invalid-request-id state retry-hint extra field-errors duplicate trailing',
    'ResponseBinding': 'cache media missing-id different-id status',
}.items():
    children.update('TestIndependentModelProblem' + suffix + '/' + name for name in names.split())
ran = {e.get('Test') for e in events if e.get('Action') == 'run'}
passed = {e.get('Test') for e in events if e.get('Action') == 'pass'}
bad = any(e.get('Action') in ('fail', 'skip') for e in events)
package_passed = any(e.get('Action') == 'pass' and not e.get('Test') for e in events)
print(f'actual wait returncode={p.returncode}; timeout={timed_out}; '
      f'top run/pass={len(top & ran)}/{len(top & passed)} of {len(top)}; '
      f'subtests run/pass={len(children & ran)}/{len(children & passed)} of {len(children)}', flush=True)
if timed_out or p.returncode or bad or not package_passed or not (top | children) <= (ran & passed):
    print(log[-16000:])
    raise SystemExit(1)
print('PASS: independent production-boundary Problem probes executed under race; no browser case claim')
PY
