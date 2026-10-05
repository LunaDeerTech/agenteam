#!/usr/bin/env python3
"""Reconstruct fixed R2 inputs. --fixture needs an exclusive local Docker window."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('repository', type=Path)
parser.add_argument('output', type=Path, help='new task-owned directory without whitespace')
parser.add_argument('--fixture', action='store_true', help='build both commands and run the original real fixture')
args = parser.parse_args()
evidence = Path(__file__).resolve().parent
inputs = json.loads((evidence / 'inputs.json').read_text())
repo, root = args.repository.resolve(), args.output.resolve()
if any(c.isspace() for c in str(root)):
    parser.error('output path must not contain whitespace (Go overlay flags)')
root.mkdir(parents=True, exist_ok=False)
snapshot = root / 'snapshot'
snapshot.mkdir()
archive = root / 'base.tar'
with archive.open('wb') as stream:
    subprocess.run(['git', 'archive', inputs['base']], cwd=repo, stdout=stream, check=True)
assert hashlib.sha256(archive.read_bytes()).hexdigest() == inputs['archive_sha256']
with tarfile.open(archive) as stream:
    stream.extractall(snapshot, filter='data')
for line in (evidence / 'source.sha256').read_text().splitlines():
    expected, name = line.split('  ')
    data = subprocess.check_output(['git', 'show', inputs['accepted_source_commit'] + ':' + name], cwd=repo)
    assert hashlib.sha256(data).hexdigest() == expected, name
    (snapshot / name).parent.mkdir(parents=True, exist_ok=True)
    (snapshot / name).write_bytes(data)
for name, expected in inputs['critical_unchanged_inputs'].items():
    assert hashlib.sha256((snapshot / name).read_bytes()).hexdigest() == expected, name
probe = root / 'independent_test.go'
shutil.copyfile(evidence / 'independent-probe.go.txt', probe)
assert hashlib.sha256(probe.read_bytes()).hexdigest() == inputs['independent_probe_sha256']
(root / 'overlay.json').write_text(json.dumps({'Replace': {
    str(snapshot / 'tests/project/independent_r2_verification_test.go'): str(probe)
}}, indent=2) + '\n')
for name in ['runtime', 'docker-config', 'cache', 'builds']:
    (root / name).mkdir()
print('Frozen snapshot:', snapshot, flush=True)
if args.fixture:
    env = os.environ.copy()
    env.update(AGENTEAM_GO='/workspace/toolchains/go1.27.1/bin/go',
               AGENTEAM_MINIO_BINARY='/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio',
               GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off',
               GOMODCACHE='/workspace/agenteam-dependency-cache/modcache',
               GOCACHE=str(root / 'cache'), GOTMPDIR=str(root / 'runtime'),
               TMPDIR=str(root / 'runtime'), DOCKER_CONFIG=str(root / 'docker-config'),
               DOCKER_HOST='unix:///var/run/docker.sock', GOFLAGS='-mod=readonly')
    for key in ['DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH', 'DOCKER_API_VERSION',
                'AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_POSTGRES_FIXTURE', 'AGENTEAM_NETWORK_FIXTURE']:
        env.pop(key, None)
    assert hashlib.sha256(Path(env['AGENTEAM_GO']).read_bytes()).hexdigest() == inputs['toolchain']['go_sha256']
    assert hashlib.sha256(Path(env['AGENTEAM_MINIO_BINARY']).read_bytes()).hexdigest() == inputs['toolchain']['minio_sha256']
    for command in ['agenteam', 'agenteam-runner']:
        with (root / ('build-' + command + '.log')).open('wb') as log:
            subprocess.run([env['AGENTEAM_GO'], 'build', '-o', str(root / 'builds' / command),
                            './cmd/' + command], cwd=snapshot, env=env, stdout=log,
                           stderr=subprocess.STDOUT, check=True)
    env['GOFLAGS'] += ' -overlay=' + str(root / 'overlay.json') + ' -v'
    with (root / 'fixture.log').open('wb') as log:
        result = subprocess.run(['sh', 'scripts/test-objects.sh', '-run', '^TestProjectR2Independent'],
                                cwd=snapshot, env=env, stdout=log, stderr=subprocess.STDOUT)
    print('Fixture exit:', result.returncode, 'log:', root / 'fixture.log', flush=True)
    raise SystemExit(result.returncode)
