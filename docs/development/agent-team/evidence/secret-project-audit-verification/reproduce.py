#!/usr/bin/env python3
"""Rebuild the exact accepted Secret checker verification input without using active sources.

Default only prepares files. --pure runs the independent race test and integration compilation/vet.
--fixture requires an already assigned exclusive fixture window and additionally
runs the original race/count=1/timeout=6m driver with the independent selector.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workspace', type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument('--source-commit', default='7d7c50df0dafcdeaaf700dc2662a6013245bbb6f')
    parser.add_argument('--pure', action='store_true')
    parser.add_argument('--fixture', action='store_true')
    parser.add_argument('--go', default='/workspace/toolchains/go1.27.1/bin/go')
    parser.add_argument('--modcache', default='/workspace/agenteam-dependency-cache/modcache')
    args = parser.parse_args()
    evidence = Path(__file__).resolve().parent
    manifest = json.loads((evidence / 'fixed-inputs.json').read_text())
    results = json.loads((evidence / 'verification-results.json').read_text())
    root = Path(tempfile.mkdtemp(prefix='agenteam-secret-audit-reproduce-'))
    snapshot = root / 'snapshot'; snapshot.mkdir()
    archive = root / 'base.tar'
    with archive.open('wb') as output:
        subprocess.run(['git', '-C', str(args.workspace), 'archive', manifest['base']], stdout=output, check=True)
    expected_archive = json.loads((evidence / 'fixed-inputs.json').read_text())['archive_sha256']
    if sha(archive.read_bytes()) != expected_archive:
        raise RuntimeError('Base archive fingerprint mismatch')
    with tarfile.open(archive) as source:
        source.extractall(snapshot, filter='data')
    for path, expected in manifest['source_hashes'].items():
        data = subprocess.check_output(['git', '-C', str(args.workspace), 'show', args.source_commit + ':' + path])
        if sha(data) != expected:
            raise RuntimeError('Frozen source mismatch: ' + path)
        destination = snapshot / path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data)
    for name in ['go.mod', 'go.sum']:
        if sha((snapshot / name).read_bytes()) != manifest[name.replace('.', '_') + '_sha256']:
            raise RuntimeError('Module input mismatch: ' + name)
    probe = root / 'probe'; probe.mkdir()
    replacements = {}
    for name, expected in results['probe_files'].items():
        data = (evidence / name).read_bytes()
        if sha(data) != expected:
            raise RuntimeError('Probe fingerprint mismatch: ' + name)
        destination = probe / name.removesuffix('.txt')
        destination.write_bytes(data)
        replacements[str(snapshot / results['probe_targets'][name])] = str(destination)
    overlay = root / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': replacements}, indent=2) + '\n')
    for name in ['runtime', 'cache', 'docker-config', 'logs', 'bin']:
        (root / name).mkdir()
    env = os.environ.copy()
    env.update({
        'AGENTEAM_GO': args.go,
        'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
        'GOMODCACHE': args.modcache, 'GOCACHE': str(root / 'cache'),
        'GOTMPDIR': str(root / 'runtime'), 'TMPDIR': str(root / 'runtime'),
        'GOFLAGS': '-mod=readonly -overlay=' + str(overlay) + ' -v',
        'DOCKER_CONFIG': str(root / 'docker-config'), 'DOCKER_HOST': 'unix:///var/run/docker.sock',
    })
    for name in ['DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH', 'DOCKER_API_VERSION',
                 'AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_POSTGRES_FIXTURE', 'AGENTEAM_NETWORK_FIXTURE']:
        env.pop(name, None)
    print('Prepared fixed input: ' + str(root), flush=True)
    if not (args.pure or args.fixture):
        return
    version = subprocess.check_output([args.go, 'env', 'GOVERSION'], env=env, text=True).strip()
    if version != 'go1.27.1':
        raise RuntimeError('Exact Go 1.27.1 required')
    commands = [
        ('pure-race', [args.go, 'test', '-race', '-count=1', '-run', '^TestSecretAuditIndependent', './internal/central/secret']),
        ('pure-vet', [args.go, 'vet', './internal/central/secret']),
        ('compile', [args.go, 'test', '-tags=integration', '-run', '^$', './tests/security']),
        ('integration-vet', [args.go, 'vet', '-tags=integration', './tests/security']),
    ]
    if args.fixture:
        commands.append(('fixture', [args.go, 'run', './tests/testsupport/postgres/cmd/fixture', '-run', '^TestSecretAuditIndependent']))
    records = []
    for name, command in commands:
        log = root / 'logs' / (name + '.log')
        with log.open('wb') as output:
            result = subprocess.run(command, cwd=snapshot, env=env, stdout=output, stderr=subprocess.STDOUT)
        records.append({'argv': command, 'cwd': str(snapshot), 'exit': result.returncode, 'log': str(log)})
        (root / 'commands.json').write_text(json.dumps(records, indent=2) + '\n')
        print(name + ': exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
        if result.returncode:
            raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
