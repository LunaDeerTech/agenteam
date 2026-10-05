import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

root = Path(__file__).resolve().parent
mode = sys.argv[1]
go = '/workspace/toolchains/go1.27.1/bin/go'
commands = {
    'compile-02': [go, 'test', '-c', '-race', '-tags=integration', '-o', str(root / 'bin/process-independent-02.test'), './tests/process'],
    'vet-02': [go, 'vet', '-tags=integration', './tests/process'],
}
argv = commands[mode]
fixed_env = {
    'GOENV': 'off', 'GOWORK': 'off', 'GOTOOLCHAIN': 'local',
    'GOPROXY': 'off', 'GOSUMDB': 'off',
    'GOCACHE': '/workspace/agenteam-secret-model-v-thmobool/gocache',
    'GOMODCACHE': '/workspace/agenteam-dependency-cache/modcache',
    'TMPDIR': str(root / 'gotmp'), 'GOTMPDIR': str(root / 'gotmp'),
    'GOFLAGS': '-mod=readonly -buildvcs=false -p=2', 'GOMAXPROCS': '4', 'CGO_ENABLED': '1',
    'AGENTEAM_GO': go,
}
env = os.environ.copy()
env.update(fixed_env)
sources = ['go.mod', 'go.sum', 'internal/central/app/account.go',
           'internal/central/app/security.go', 'internal/central/app/model.go',
           'tests/process/model_root_independent_test.go']
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
record = {
    'argv': argv, 'cwd': str(root / 'src'), 'environment': fixed_env,
    'baseline': 'ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7',
    'input': {p: sha(root / 'src' / p) for p in sources},
    'tool_sha256': sha(Path(go)), 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
    'docker_authorized': False,
}
meta = root / 'logs' / (mode + '.json')
log = root / 'logs' / (mode + '.log')
with meta.open('x') as out:
    json.dump(record, out, indent=2)
    out.write('\n')
started = time.monotonic()
with log.open('xb') as out:
    result = subprocess.run(argv, cwd=root / 'src', env=env, stdout=out, stderr=subprocess.STDOUT)
record.update(exit_code=result.returncode, elapsed_seconds=round(time.monotonic()-started, 3),
              ended_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), log=str(log), log_sha256=sha(log))
meta.write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps({'mode': mode, 'exit_code': result.returncode, 'elapsed_seconds': record['elapsed_seconds'], 'log': str(log), 'metadata': str(meta)}))
sys.exit(result.returncode)
