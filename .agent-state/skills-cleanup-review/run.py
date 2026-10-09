#!/usr/bin/env python3
"""Run only the independent Skills consumer controls; no native/PG fixture."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=Path('/workspace/agenteam-skills-cleanup'))
args = parser.parse_args()
repo = args.repo.resolve()
source = Path(__file__).resolve().parent / 'independent_test.go'
v = os.statvfs(repo)
available = v.f_bavail * v.f_frsize
print('UTC', datetime.now(timezone.utc).isoformat(), 'available', available, flush=True)
if available < 5368709120:
    raise SystemExit(78)
go = '/workspace/toolchains/go1.27.1/bin/go'
env = os.environ.copy()
env.update(PATH='/workspace/toolchains/go1.27.1/bin:' + env.get('PATH', ''),
           GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off',
           GOSUMDB='off', GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=1',
           GOMAXPROCS='2',
           GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
           GOCACHE='/workspace/agenteam-knowledge/output/ai/knowledge/go-cache')
assert subprocess.check_output([go, 'version'], env=env, text=True).strip() == 'go version go1.27.1 linux/amd64'
with tempfile.TemporaryDirectory(prefix='skills-consumer-review-') as directory:
    overlay = Path(directory) / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo / 'internal/central/skill/zz_knowledge_skills_cleanup_independent_test.go'): str(source),
    }}))
    command = ['timeout', '--signal=TERM', '--kill-after=3s', '120s', go,
               'test', '-overlay=' + str(overlay), '-race', '-count=1', '-p=1',
               '-mod=readonly', '-timeout=60s', '-v',
               '-run=^TestIndependentSkillCleanup', './internal/central/skill']
    result = subprocess.run(command, cwd=repo, env=env)
    print('independent_actual_wait', result.returncode, flush=True)
v = os.statvfs(repo)
print('available_end', v.f_bavail * v.f_frsize, 'overlay_tmp_removed=True', flush=True)
raise SystemExit(result.returncode)
