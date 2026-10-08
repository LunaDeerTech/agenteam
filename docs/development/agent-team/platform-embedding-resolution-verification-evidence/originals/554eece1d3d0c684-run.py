import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
REPO = Path('/workspace/agenteam')
GO = '/workspace/toolchains/go1.27.1/bin/go'
name = sys.argv[1]
if name.startswith('compile'):
    command = [GO, 'test', '-race', '-c', '-tags=integration', '-vet=off', '-work', '-x', '-o', str(ROOT / (name + '.test')), './tests/model']
elif name.startswith('vet'):
    command = [GO, 'vet', '-race', '-tags=integration', '-work', '-x', './tests/model']
elif name.startswith('format'):
    allowed = {row['path'] for row in json.loads((ROOT / 'installation.json').read_bytes())['installed_sources']}
    paths = sys.argv[2:]
    assert paths and all(p in allowed for p in paths)
    command = ['/workspace/toolchains/go1.27.1/bin/gofmt', '-w'] + [str(REPO / p) for p in paths]
else:
    raise SystemExit('unsupported command')

argv = ['/usr/bin/timeout', '--signal=TERM', '--kill-after=4s', '40s'] + command
out = ROOT / name
out.mkdir()
(ROOT / 'tmp').mkdir(exist_ok=True)
safe_env = {
    'GOTOOLCHAIN': 'local', 'GOWORK': 'off', 'GOPROXY': 'off', 'GOSUMDB': 'off',
    'GOFLAGS': '-mod=readonly -p=1', 'GOTELEMETRY': 'off',
    'GOCACHE': '/workspace/.cache/go-build', 'GOMODCACHE': '/workspace/go/pkg/mod',
    'TMPDIR': str(ROOT / 'tmp'),
}
env = os.environ.copy()
env.update(safe_env)
env['PATH'] = '/workspace/toolchains/go1.27.1/bin:' + env.get('PATH', '')
started = datetime.datetime.now(datetime.timezone.utc).isoformat()
start = time.monotonic()
with (out / 'raw.stdout').open('wb') as stdout, (out / 'raw.stderr').open('wb') as stderr:
    process = subprocess.Popen(argv, cwd=REPO, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
    stat = Path(f'/proc/{process.pid}/stat').read_text().rsplit(') ', 1)[1].split()
    owner = {'pid': process.pid, 'pgrp': int(stat[2]), 'starttime_ticks': int(stat[19])}
    launch = {'argv': argv, 'cwd': str(REPO), 'safe_env': safe_env, 'started_utc': started, 'owned_direct': owner, 'outer_limit_seconds': 45}
    (out / 'launch.json').write_text(json.dumps(launch, indent=2) + '\n')
    outer_timeout = False
    try:
        code = process.wait(timeout=45)
    except subprocess.TimeoutExpired:
        outer_timeout = True
        os.killpg(process.pid, signal.SIGKILL)
        code = process.wait()

owned_remaining = []
for path in Path('/proc').iterdir():
    if not path.name.isdigit():
        continue
    try:
        fields = (path / 'stat').read_text().rsplit(') ', 1)[1].split()
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        continue
    if int(fields[2]) == owner['pgrp']:
        owned_remaining.append({'pid': int(path.name), 'state': fields[0], 'starttime_ticks': int(fields[19])})
result = {'exit_code': code, 'elapsed_seconds': time.monotonic() - start, 'outer_timeout': outer_timeout, 'actual_direct_wait_completed': True, 'owned_process_group_remaining': owned_remaining, 'raw': []}
for filename in ['raw.stdout', 'raw.stderr']:
    b = (out / filename).read_bytes()
    result['raw'].append({'path': filename, 'bytes': len(b), 'sha256': hashlib.sha256(b).hexdigest()})
(out / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'command': name, **result}, indent=2))
if code or owned_remaining:
    sys.exit(1)
