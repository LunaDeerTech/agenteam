#!/usr/bin/env python3
"""Rebuild the fixed D09 input and optional independent checks without Git writes."""

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import time

BASE = '81fe7427ceb4672247b3d30a51c10a2e2808ba04'
GO_PATHS = ['go.mod', 'go.sum', 'internal', 'db', 'tests', 'scripts', 'cmd']
PROBE = 'tests/model/d09_independent_test.go'
SELECTOR = '^(TestModelProjectIndependentSixHistoricalReceiptsRequireCurrentRead|TestModelProjectIndependentLateGateBindingRollsBackAllFacts)$'
MINIO_SHA256 = 'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--work', type=Path, required=True, help='New empty private directory, preferably on /workspace')
    parser.add_argument('--go', dest='go_binary', default='go')
    parser.add_argument('--modcache', type=Path)
    parser.add_argument('--minio', type=Path)
    parser.add_argument('--compile', action='store_true')
    parser.add_argument('--fixture', action='store_true', help='Run only when the shared fixture window has been assigned')
    args = parser.parse_args()
    evidence = Path(__file__).resolve().parent
    repo, work = args.repo.resolve(), args.work.resolve()
    if work.exists() and any(work.iterdir()):
        parser.error('--work must be absent or empty')
    if args.fixture and (args.minio is None or sha(args.minio) != MINIO_SHA256):
        parser.error('--fixture requires the exact source-built MinIO binary recorded in this evidence')
    work.mkdir(parents=True, exist_ok=True)
    snapshot = work / 'snapshot'
    snapshot.mkdir()
    archive = subprocess.check_output(['git', 'archive', BASE, *GO_PATHS], cwd=repo)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        for member in tar:
            path = snapshot / member.name
            if member.isdir():
                path.mkdir(parents=True, exist_ok=True)
                continue
            if not member.isfile() or path.resolve().is_relative_to(snapshot) is False:
                raise ValueError('unexpected archive member')
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(tar.extractfile(member).read())
            path.chmod(member.mode)
    patch = subprocess.run(['patch', '--batch', '-p1', '-i', str(evidence / 'implementation.patch')], cwd=snapshot, text=True, capture_output=True)
    (work / 'patch.log').write_text(patch.stdout + patch.stderr)
    if patch.returncode:
        raise SystemExit(patch.returncode)
    manifest = json.loads((evidence / 'source-manifest.json').read_text())
    for item in manifest['files']:
        if sha(snapshot / item['path']) != item['sha256']:
            raise ValueError('rebuilt source mismatch: ' + item['path'])
    shutil.copyfile(evidence / 'independent_d09_project_configuration_test.go.txt', snapshot / PROBE)
    probe_manifest = json.loads((evidence / 'independent-probe-manifest.json').read_text())
    if sha(snapshot / PROBE) != probe_manifest['sha256']:
        raise ValueError('probe mismatch')
    result = {'baseline': BASE, 'manifest_sha256': sha(evidence / 'source-manifest.json'), 'source_files_verified': len(manifest['files']), 'probe_sha256': sha(snapshot / PROBE), 'patch_exit': patch.returncode, 'checks': []}
    for directory in ['gocache', 'tmp', 'runtime', 'bin', 'docker-empty']:
        (work / directory).mkdir()
    env = dict(os.environ, GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off', GOSUMDB='off', GOCACHE=str(work / 'gocache'), GOTMPDIR=str(work / 'tmp'), TMPDIR=str(work / 'runtime'), GOFLAGS='-mod=readonly -p=2 -v', GOMAXPROCS='2', AGENTEAM_GO=args.go_binary, DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(work / 'docker-empty'))
    for key in list(env):
        if key.startswith('AGENTEAM_TEST_'):
            del env[key]
    if args.modcache is not None:
        env['GOMODCACHE'] = str(args.modcache.resolve())
    if args.compile or args.fixture:
        version = subprocess.check_output([args.go_binary, 'env', 'GOVERSION'], env=env, text=True).strip()
        if version != 'go1.27.1':
            raise ValueError('Go 1.27.1 is required')
        result['go_version'] = version
    commands = []
    if args.compile:
        commands.append(('compile', [args.go_binary, 'test', '-c', '-race', '-count=1', '-timeout=6m', '-tags=integration', '-o', str(work / 'bin/d09-independent.test'), './tests/model']))
    if args.fixture:
        env['AGENTEAM_MINIO_BINARY'] = str(args.minio.resolve())
        commands.append(('fixture', ['sh', 'scripts/test-objects.sh', '-run', SELECTOR]))
    for name, command in commands:
        start = time.monotonic()
        with (work / (name + '.log')).open('w') as stream:
            completed = subprocess.run(command, cwd=snapshot, env=env, stdout=stream, stderr=subprocess.STDOUT)
        result['checks'].append({'name': name, 'argv': command, 'exit': completed.returncode, 'seconds': round(time.monotonic() - start, 3)})
        if completed.returncode:
            (work / 'reproduce-result.json').write_text(json.dumps(result, indent=2) + '\n')
            raise SystemExit(completed.returncode)
    (work / 'reproduce-result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
