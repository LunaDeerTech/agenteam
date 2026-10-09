#!/usr/bin/env python3
"""Only run after cache coordination. No TestMain/native/PG/socket tests."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

own = Path(__file__).resolve().parents[2]
target = Path('/workspace/agenteam-knowledge-http')
package = target / 'internal/central/knowledge/http'
stable = {name + '.go' for name in ('handler', 'query', 'wire', 'io',
                                   'handler_test', 'query_test', 'wire_test', 'io_test')}
for name in stable:
    path = package / name
    expected = subprocess.check_output(['git', 'show', '35b62908:' + str(path.relative_to(target))], cwd=target)
    assert path.read_bytes() == expected, 'frozen Go source changed'
    assert b'func TestMain(' not in expected, 'unexpected TestMain'
base = own / 'output/ai/runner-control/tmp/knowledge-http-review'
base.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='pure-', dir=base) as temporary:
    directory = Path(temporary)
    empty = directory / 'excluded.go'
    empty.write_text('package knowledgehttp\n')
    # Exclude currently unreviewed native/schema tests without author writes.
    overlay = {str(path): str(empty) for path in package.glob('*.go') if path.name not in stable}
    overlay[str(package / 'independent_runner_review_test.go')] = str(Path(__file__).with_name('risk_test.go').resolve())
    mapping = directory / 'overlay.json'
    mapping.write_text(json.dumps({'Replace': overlay}))
    env = os.environ.copy()
    env.update({'PATH': '/workspace/toolchains/go1.27.1/bin:' + env['PATH'],
                'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
                'GOSUMDB': 'off', 'GOTELEMETRY': 'off', 'GOMAXPROCS': '2',
                'GOCACHE': str(own / 'output/ai/runner-control/gocache'),
                'GOMODCACHE': str(own / 'output/ai/runner-control/go-mod'),
                'GOTMPDIR': str(directory), 'GOFLAGS': '-mod=readonly -p=1'})
    command = ['/workspace/toolchains/go1.27.1/bin/go', 'test', '-race', '-count=1',
               '-overlay=' + str(mapping), '-timeout=20s', '-run',
               '^TestIndependentKnowledgeHTTP(Projection|HeldBodyAndCallback)$',
               './internal/central/knowledge/http']
    result = subprocess.run(command, cwd=target, env=env, timeout=90)
    raise SystemExit(result.returncode)
