import datetime
import hashlib
import json
import os
import pathlib
import subprocess
import sys
import time

root = pathlib.Path(__file__).parent
author = pathlib.Path('/workspace/agenteam-current-resolution-author-xtvs4_8b')
name, kind = sys.argv[1:]
go = '/workspace/toolchains/go1.27.1/bin/go'
settings = {
    'GOROOT': '/workspace/toolchains/go1.27.1',
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
    'GOPROXY': 'off', 'GOSUMDB': 'off',
    'GONOPROXY': '', 'GONOSUMDB': '', 'GOPRIVATE': '',
    'GOMODCACHE': '/workspace/agenteam-dependency-cache/modcache',
    'GOCACHE': '/workspace/agenteam-secret-model-v-thmobool/gocache',
    'GOTMPDIR': str(root / 'gotmp'), 'TMPDIR': str(root / 'runtime'),
    'GOFLAGS': '-mod=readonly -buildvcs=false -p=2 -overlay=' + str(root / 'overlay.json'),
    'GOMAXPROCS': '4', 'CGO_ENABLED': '1',
}
env = {k:v for k,v in os.environ.items() if not k.startswith(('AGENTEAM_', 'DOCKER_', 'PG'))}
env.update(settings)
argv = [go, 'test', '-c', '-race', '-tags=integration', '-o', str(root / 'model-independent.test'), './tests/model'] if kind == 'compile' else [go, 'vet', '-tags=integration', './tests/model']
log = root / 'logs' / (name + '.log')
meta = {'argv':argv, 'cwd':str(author / 'snapshot'), 'environment':settings,
        'kind':kind + ' only; no fixture or test binary execution',
        'candidate_manifest_sha256':hashlib.sha256((author / 'candidate-freeze-04/manifest.json').read_bytes()).hexdigest(),
        'probe_sha256':hashlib.sha256((root / 'current_resolution_independent_test.go').read_bytes()).hexdigest(),
        'overlay_sha256':hashlib.sha256((root / 'overlay.json').read_bytes()).hexdigest(),
        'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(root / 'logs' / (name + '.input.json')).write_text(json.dumps(meta, indent=2) + '\n')
start = time.monotonic()
with log.open('wb') as f:
    result = subprocess.run(argv, cwd=author / 'snapshot', env=env, stdout=f, stderr=subprocess.STDOUT)
meta.update({'exit_code':result.returncode, 'elapsed_seconds':round(time.monotonic()-start, 3),
             'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
             'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()})
(root / 'logs' / (name + '.json')).write_text(json.dumps(meta, indent=2) + '\n')
print(json.dumps(meta), flush=True)
sys.exit(result.returncode)
