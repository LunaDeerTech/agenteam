#!/usr/bin/python3
"""One explicitly authorized capture from the unchanged 448f NRMR codec.

This is a test-only data capture, not a new production oracle or PG run. The
accepted Command-owner process recorder supplies actual Wait and owned-SID
retirement. No result is accepted after a stop request or failed stage.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
BASE = '448f6adea22d62dcb71255a3c834dbb8f1b95d84'
CAPTURE = 'internal/central/agentloop/successor_golden_capture_test.go'
PROCESS = '.agent-state/runner-command-owner/run-checks.py'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
PACKAGE = './internal/central/agentloop'
TOP = 'TestCaptureLegacySuccessorGolden'
CASES = ('read-control', 'write-maximum', 'command-escaped-nonzero')
FIELDS = ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles',
          'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles', 'EmbedFiles')
ENV = dict(GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off',
           GOSUMDB='off', GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=1',
           GOMAXPROCS='1', CGO_ENABLED='1', PYTHONDONTWRITEBYTECODE='1',
           GOCACHE='/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build',
           GOMODCACHE='/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod')


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def save(out, name, value):
    with (out / name).open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def snapshot(paths):
    return {str(path): sha(path) for path in sorted(paths)}


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


def values(raw):
    decoder, offset = json.JSONDecoder(), 0
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        value, offset = decoder.raw_decode(raw, offset)
        yield value


def original_blobs(paths):
    table = {}
    for row in git('ls-tree', '-r', '-z', BASE).split(b'\0'):
        if row:
            header, name = row.split(b'\t', 1)
            mode, kind, oid = header.split()
            if kind == b'blob':
                table[name.decode()] = oid.decode()
    proof = []
    child = subprocess.Popen(['git', 'cat-file', '--batch'], cwd=ROOT,
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE)
    try:
        for path in sorted(paths):
            relative = str(path.relative_to(ROOT))
            if relative == CAPTURE or relative == str(Path(__file__).resolve().relative_to(ROOT)):
                continue
            oid = table[relative]
            child.stdin.write((oid + '\n').encode())
            child.stdin.flush()
            header = child.stdout.readline().decode().strip().split()
            assert header[:2] == [oid, 'blob']
            data = child.stdout.read(int(header[2]))
            assert child.stdout.read(1) == b'\n'
            digest = hashlib.sha256(data).hexdigest()
            assert digest == sha(path), relative
            proof.append(dict(path=relative, base=BASE, git_blob=oid,
                              sha256=digest, size_bytes=len(data)))
    finally:
        child.stdin.close()
        child.stdout.close()
        code = child.wait()
    assert code == 0
    return proof, dict(pid=child.pid, actual_wait_exit=code)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--head', required=True)
    parser.add_argument('--capture-sha256', required=True)
    args = parser.parse_args()
    out = args.output.resolve()
    assert out == ROOT / 'output/ai/model-successor-contract/golden01'
    assert git('rev-parse', 'HEAD').decode().strip() == args.head
    assert sha(ROOT / CAPTURE) == args.capture_sha256
    # Every existing product and test file remains exactly the old version;
    # the separately bound capture test is the sole new package input.
    assert not git('diff', BASE, '--', 'internal', 'db', 'tests/testsupport', 'go.mod', 'go.sum').strip()
    out.mkdir(parents=True, mode=0o700, exist_ok=False)
    # The capture top itself creates the fresh vectors directory (0700).
    (out / 'capture-source.go').write_bytes((ROOT / CAPTURE).read_bytes())
    spec = importlib.util.spec_from_file_location('owned_capture_process', ROOT / PROCESS)
    owner = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(owner)
    stopped = []

    def stop(signum, _frame):
        stopped.append(signum)
        # Do not return while an original Go/compiler process owns the window.
        # The unchanged recorder will Wait and check both same-SID tails; its
        # bounded timeout escalates if TERM does not retire the owned session.
        for session in list(owner.ACTIVE_STAGES):
            members, _ = owner.session_members(session)
            owner.signal_members(members, session, signal.SIGTERM)

    for signum in (signal.SIGTERM, signal.SIGINT):
        signal.signal(signum, stop)
    env = os.environ.copy()
    env.update(ENV)
    env['AGENTEAM_LEGACY_SUCCESSOR_OUTPUT'] = str(out / 'vectors')
    protected = {GO, Path(__file__).resolve(), ROOT / PROCESS, ROOT / CAPTURE}
    candidates = {ROOT / 'go.mod', ROOT / 'go.sum'} | protected
    for prefix in ('internal', 'db', 'tests/testsupport'):
        candidates.update(path for path in (ROOT / prefix).rglob('*') if path.is_file())
    prior = snapshot(candidates)
    save(out, 'discovery-candidate-inputs.json', prior)
    save(out, 'invocation.json', dict(base=BASE, head=args.head, cwd=str(ROOT),
         pid=os.getpid(), environment_overrides=dict(ENV,
         AGENTEAM_LEGACY_SUCCESSOR_OUTPUT=env['AGENTEAM_LEGACY_SUCCESSOR_OUTPUT']),
         protected={str(path): prior[str(path)] for path in sorted(protected)},
         scope='old deterministic synthetic fixtures; no PG, Model, Runner or network; one list and one capture top'))
    started = time.monotonic()
    code = owner.run(out, 'deps', [GO, 'list', '-deps', '-test', '-json', PACKAGE], env, 120)
    if code or stopped:
        raise SystemExit(code or 1)
    paths = {ROOT / 'go.mod', ROOT / 'go.sum'} | protected
    for row in values((out / 'deps.log').read_text()):
        directory = Path(row.get('Dir', '/'))
        if not directory.is_relative_to(ROOT):
            continue
        for field in FIELDS:
            for item in row.get(field, []):
                path = directory / item
                if path.is_relative_to(ROOT):
                    paths.add(path)
    assert all(str(path) in prior for path in paths)
    before = {str(path): prior[str(path)] for path in sorted(paths)}
    assert before == snapshot(paths)
    required = [CAPTURE, 'internal/central/execution/contract/next_round_request_codec.go',
                'internal/central/agentloop/next_round_request.go',
                'internal/central/agentloop/round_projection_test.go',
                'internal/central/agentloop/write_file_round_projection_test.go',
                'internal/central/agentloop/run_command_round_projection_test.go']
    assert all(str(ROOT / name) in before for name in required)
    repo_paths = [path for path in paths if path.is_relative_to(ROOT)]
    provenance, git_wait = original_blobs(repo_paths)
    save(out, 'legacy-inputs.json', dict(base=BASE, entries=provenance, git_process=git_wait,
         sole_new_package_source=dict(path=CAPTURE, sha256=args.capture_sha256)))
    save(out, 'inputs-before.json', before)
    if stopped:
        raise SystemExit(1)
    code = owner.run(out, 'capture', [GO, 'test', '-p', '1', '-count=1',
                     '-timeout=2m', '-json', '-run', '^' + TOP + '$', PACKAGE], env, 180)
    after = snapshot(paths)
    save(out, 'inputs-after.json', after)
    if code or stopped or before != after:
        raise SystemExit(code or 1)
    assert git('rev-parse', 'HEAD').decode().strip() == args.head
    expected = {TOP, *(TOP + '/' + name for name in CASES)}
    run, passed = [], []
    for row in values((out / 'capture.log').read_text()):
        assert row['Action'] not in ('fail', 'skip')
        if row.get('Test'):
            if row['Action'] == 'run': run.append(row['Test'])
            if row['Action'] == 'pass': passed.append(row['Test'])
    assert set(run) == expected == set(passed) and len(run) == len(passed) == 4
    files = sorted(path for path in (out / 'vectors').iterdir())
    assert len(files) == 7 and all(path.is_file() and not path.is_symlink() for path in files)
    save(out, 'output-hashes.json', {path.name: dict(sha256=sha(path), size_bytes=path.stat().st_size) for path in files})
    save(out, 'result.json', dict(actual_capture_exit=0, stopped=False,
         input_count=len(before), inputs_unchanged=True, legacy_base=BASE,
         original_existing_repository_inputs_verified=len(provenance),
         exact_tests=sorted(expected), vectors=3, outputs=7,
         elapsed_seconds=round(time.monotonic() - started, 3),
         source_head_after=git('rev-parse', 'HEAD').decode().strip(),
         scope='capture only; no successor implementation or real dependency acceptance'))
    print(json.dumps(dict(captured=True, vectors=3, inputs=len(before), native_started=False)), flush=True)


if __name__ == '__main__':
    main()
