#!/usr/bin/env python3
"""Restore the locked, non-secret test dependencies during a root-owned window.

Run each stage explicitly; this never starts a product test or a container.
The shared directory is writable only by the executor assigned by root.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
SHARED = Path('/workspace/shared/agenteam-deps')
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
TREES = ('agenteam', 'agenteam-object-metadata-cleanup',
         'agenteam-secret-variable-owner-service', 'agenteam-skills-owner-http',
         'agenteam-knowledge-content-http', 'agenteam-work-ui')
SERVER = 'github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648'
SERVER_SHA = 'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
IMAGES = ('pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc',
          'pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782')


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def environment(online):
    env = os.environ.copy()
    env.update(GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOAUTH='off',
               GOPROXY='https://proxy.golang.org' if online else 'off',
               GOSUMDB='sum.golang.org' if online else 'off',
               GOPRIVATE='', GONOPROXY='', GONOSUMDB='', GOTELEMETRY='off',
               GOMODCACHE=str(SHARED / 'go-mod'), GOCACHE=str(SHARED / 'go-build'),
               GOFLAGS='-mod=readonly -p=2', GOMAXPROCS='2')
    return env


def run(label, args, cwd, timeout, online=True):
    log = SHARED / ('restore-' + label + '.log')
    start = time.monotonic()
    with log.open('xb') as stream:
        child = subprocess.Popen(args, cwd=cwd, env=environment(online),
                                 stdout=stream, stderr=subprocess.STDOUT,
                                 start_new_session=True)
        print(f'{label}: pid={child.pid} timeout={timeout}s log={log}', flush=True)
        try:
            code = child.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                code = child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                code = child.wait()
            print(f'{label}: timed_out=true', flush=True)
    print(f'{label}: actual_wait={code} elapsed={time.monotonic()-start:.3f}s', flush=True)
    print(log.read_text(errors='replace')[-5000:], flush=True)
    if code:
        raise SystemExit(code if code > 0 else 1)


def prepare():
    SHARED.mkdir(mode=0o700, parents=True, exist_ok=True)
    for name in ('bin', 'docker-config', 'union'):
        (SHARED / name).mkdir(mode=0o700, exist_ok=True)
    if not (SHARED / 'go-mod').exists():
        # This is a public module/build cache, not deployment configuration.
        shutil.copytree('/workspace/go/pkg/mod', SHARED / 'go-mod')
    if not (SHARED / 'go-build').exists():
        shutil.copytree('/workspace/.cache/go-build', SHARED / 'go-build')
    modules, sums, inputs = set(), set(), {}
    for name in TREES:
        root = Path('/workspace') / name
        for filename in ('go.mod', 'go.sum'):
            inputs[str(root / filename)] = digest(root / filename)
        modules.update(re.findall(r'^\s+([^\s]+) (v[^\s]+)', (root / 'go.mod').read_text(), re.M))
        sums.update((root / 'go.sum').read_text().splitlines())
    # The union is a disposable manifest; repository lock files stay intact.
    (SHARED / 'union/go.mod').write_text('module example.invalid/agenteam-dependency-recovery\n\ngo 1.27.1\n\nrequire (\n' +
        ''.join(f'\t{module} {version}\n' for module, version in sorted(modules)) + ')\n')
    (SHARED / 'union/go.sum').write_text('\n'.join(sorted(sums)) + '\n')
    (SHARED / 'repository-inputs.json').write_text(json.dumps(inputs, indent=2) + '\n')
    (SHARED / 'module-versions.json').write_text(json.dumps(sorted(modules), indent=2) + '\n')
    print(f'prepared {len(modules)} locked module versions; no repository lock changed', flush=True)


def modules():
    selected = json.loads((SHARED / 'module-versions.json').read_text())
    run('modules', [str(GO), 'mod', 'download', '-json'] +
        [module + '@' + version for module, version in selected], SHARED / 'union', 180)


def minio_source():
    run('minio-source', [str(GO), 'mod', 'download', '-json', SERVER], SHARED / 'union', 90)
    source = SHARED / 'go-mod' / SERVER
    for filename, expected in (
        ('go.mod', '673f06144e90bc045f0a20050d2874c52e551be5bfbd70dff6e76b66da0db702'),
        ('go.sum', '86e062349c7abdce0465561bb409d410a95b00b0969ba05d5fb5f2e3550a5cd4')):
        if digest(source / filename) != expected:
            raise SystemExit('fixed MinIO ' + filename + ' checksum mismatch')
    run('minio-modules', [str(GO), 'mod', 'download'], source, 180)


def minio_build():
    env = environment(False)
    env.update(GOOS='linux', GOARCH='amd64', GOAMD64='v1', CGO_ENABLED='0')
    # Use the same immutable source and exact flags as D05 research revision 2.
    flags = '-s -w -X github.com/minio/minio/cmd.Version=2025-10-15T17:29:55Z -X github.com/minio/minio/cmd.ReleaseTag=RELEASE.2025-10-15T17-29-55Z -X github.com/minio/minio/cmd.CommitID=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a -X github.com/minio/minio/cmd.ShortCommitID=9e49d5e7a648 -X github.com/minio/minio/cmd.CopyrightYear=2025'
    os.environ.update({key: env[key] for key in ('GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED')})
    run('minio-build', [str(GO), 'build', '-trimpath', '-buildvcs=false', '-ldflags', flags,
                      '-o', str(SHARED / 'bin/minio'), '.'], SHARED / 'go-mod' / SERVER, 300, False)
    binary = SHARED / 'bin/minio'
    print(f'MinIO bytes={binary.stat().st_size} sha256={digest(binary)}', flush=True)
    if digest(binary) != SERVER_SHA or binary.stat().st_size != 109289632:
        raise SystemExit('fixed MinIO binary identity mismatch')


def images():
    docker = ['docker', '--host', 'unix:///var/run/docker.sock', '--config', str(SHARED / 'docker-config')]
    for index, image in enumerate(IMAGES):
        check = subprocess.run(docker + ['image', 'inspect', '--format', '{{.Id}} {{.Os}}/{{.Architecture}}', image],
                               capture_output=True, text=True, timeout=15)
        print(f'image-{index}: inspect_actual_wait={check.returncode} {check.stdout.strip()} {check.stderr.strip()}', flush=True)
        if check.returncode:
            run(f'image-{index}', docker + ['pull', image], SHARED, 180)


def web_harness():
    root = Path('/workspace/agenteam-work-ui/tests/account-captcha-web')
    run('work-browser-modules', ['npm', 'ci', '--ignore-scripts', '--no-audit', '--no-fund',
                               '--cache', str(SHARED / 'npm-cache')], root, 180)


def verify():
    for path, expected in json.loads((SHARED / 'repository-inputs.json').read_text()).items():
        if digest(path) != expected:
            raise SystemExit('repository lock changed: ' + path)
    print('all recorded repository Go locks unchanged', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=('prepare', 'modules', 'minio-source', 'minio-build', 'images', 'web-harness', 'verify'))
    args = parser.parse_args()
    globals()[args.stage.replace('-', '_')]()
