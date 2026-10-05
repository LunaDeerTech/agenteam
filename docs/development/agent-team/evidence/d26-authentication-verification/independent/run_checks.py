from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import json, os, subprocess, time

root = Path(__file__).resolve().parent
cwd = root / 'input'
go = '/workspace/toolchains/go1.27.1/bin/go'
explicit = {
    'AGENTEAM_GO': go, 'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
    'GOPROXY': 'off', 'GOFLAGS': '-mod=readonly',
    'GOMODCACHE': '/workspace/agenteam-dependency-cache/modcache',
    'GOCACHE': '/workspace/agenteam-d26-auth-author-jrfhr6h1/go-cache',
    'TMPDIR': str(root / 'tmp'),
}
commands = [
    ('compile-01', [go, 'test', '-race', '-tags=integration', '-c', '-o', str(root / 'bin/account.test'), './tests/account']),
    ('vet-01', [go, 'vet', '-tags=integration', './tests/account']),
    ('typescript-01', [str(cwd / 'web/node_modules/.bin/tsc'), '--noEmit', '--strict', '--target', 'ES2022', '--module', 'ESNext', '--moduleResolution', 'Bundler', '--lib', 'ES2022,DOM,DOM.Iterable', '--skipLibCheck', '--types', 'node', '--typeRoots', 'web/node_modules/@types', 'tests/account-captcha-web/e2e/independent.spec.ts']),
    ('config-syntax-01', ['node', '--check', str(cwd / 'tests/account-captcha-web/independent.config.js')]),
]

def run(item):
    name, argv = item
    start = time.monotonic()
    with (root / 'logs' / (name + '.log')).open('wb') as out:
        proc = subprocess.run(argv, cwd=cwd, env=dict(os.environ, **explicit), stdout=out, stderr=subprocess.STDOUT, timeout=360)
    result = {'argv': argv, 'cwd': str(cwd), 'env': explicit, 'exit': proc.returncode, 'seconds': time.monotonic() - start}
    (root / 'logs' / (name + '.json')).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'name': name, 'exit': proc.returncode, 'seconds': result['seconds']}), flush=True)
    return proc.returncode

with ThreadPoolExecutor(max_workers=3) as pool:
    statuses = list(pool.map(run, commands))
raise SystemExit(0 if all(v == 0 for v in statuses) else 1)
