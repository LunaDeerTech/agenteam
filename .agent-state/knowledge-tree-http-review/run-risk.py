#!/usr/bin/env python3
"""Run only two independent pure controls over the frozen adapter; no native/PG."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile

own = Path(__file__).resolve().parents[2]
target = Path('/workspace/agenteam-knowledge-tree-http')
package = target / 'internal/central/knowledge/commandhttp'
stable = {name + '.go' for name in ('handler', 'input', 'wire', 'io', 'handler_test')}
for name in stable:
    path = package / name
    expected = subprocess.check_output(['git', 'show', 'f1a1bd45:' + str(path.relative_to(target))], cwd=target)
    assert path.read_bytes() == expected, 'frozen adapter input changed'
    assert b'func TestMain(' not in expected, 'unexpected TestMain'
space = os.statvfs(own)
available = space.f_bavail * space.f_frsize
print(json.dumps({'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'available_bytes': available}), flush=True)
if available < 5368709120:
    raise SystemExit(78)
base = own / 'output/ai/runner-control/tmp/knowledge-tree-http-review'
base.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='pure-', dir=base) as temporary:
    directory = Path(temporary)
    empty = directory / 'excluded.go'
    empty.write_text('package commandhttp\n')
    overlay = {str(path): str(empty) for path in package.glob('*.go') if path.name not in stable}
    overlay[str(package / 'independent_runner_review_test.go')] = str(Path(__file__).with_name('risk_test.go').resolve())
    mapping = directory / 'overlay.json'
    mapping.write_text(json.dumps({'Replace': overlay}))
    env = os.environ.copy()
    env.update({'PATH': '/workspace/toolchains/go1.27.1/bin:' + env['PATH'],
                'AGENTEAM_GO': '/workspace/toolchains/go1.27.1/bin/go',
                'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
                'GOSUMDB': 'off', 'GOTELEMETRY': 'off', 'GOMAXPROCS': '2',
                'GOCACHE': str(own / 'output/ai/runner-control/gocache'),
                'GOMODCACHE': str(own / 'output/ai/runner-control/go-mod'),
                'GOTMPDIR': str(directory), 'TMPDIR': str(directory), 'GOFLAGS': '-mod=readonly -p=1'})
    result = subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'test', '-race', '-count=1', '-v',
                             '-overlay=' + str(mapping), '-timeout=15s', '-run',
                             '^TestIndependentTreeCommand(BindingAndProjection|ActualCallbackTail)$',
                             './internal/central/knowledge/commandhttp'], cwd=target, env=env, timeout=90)
    raise SystemExit(result.returncode)
