#!/usr/bin/python3
"""One precompiled real TLS/WSS test; original automatic host TCP tail semantics."""
import argparse
import ast
import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
EXPECTED = {'TestRunCommandWSS', 'TestRunCommandWSS/exit-and-cancel'}
SELECTOR = '^TestRunCommandWSS$/^exit-and-cancel$'
RESOURCE_MARKER = 'RUN_COMMAND_WSS owned_resources_joined=true'


def original_helpers():
    # Reuse the exact original implementations without invoking the PG CLI,
    # adding a PG resource declaration, or duplicating a modified TCP classifier.
    tree = ast.parse(SUPERVISOR.read_text())
    names = {'tcp', 'tcp_failure_sample', 'descendants'}
    nodes = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in names]
    if {node.name for node in nodes} != names:
        raise ValueError('original tail helpers missing')
    scope = {'Path': Path, 'time': time}
    exec(compile(ast.Module(body=nodes, type_ignores=[]), str(SUPERVISOR), 'exec'), scope)
    return scope


def result_complete(text, actual_exit):
    runs = re.findall(r'^=== RUN\s+(\S+)\s*$', text, re.M)
    passed = re.findall(r'^\s*--- PASS: (\S+) \([^\n]+\)\s*$', text, re.M)
    bad = re.search(r'^\s*--- (?:FAIL|SKIP):|^FAIL(?:\s|$)|^panic:|^WARNING: DATA RACE', text, re.M)
    return (actual_exit == 0 and not bad and len(runs) == 2 and set(runs) == EXPECTED
            and len(passed) == 2 and set(passed) == EXPECTED
            and len(re.findall(r'^PASS\s*$', text, re.M)) == 1
            and text.count(RESOURCE_MARKER) == 1)


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--request', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    request, directory = args.request.resolve(), args.output
    plan = json.loads((request / 'plan.json').read_text())
    inputs = json.loads((request / 'input-hashes.json').read_text())
    if (str(directory) != plan['namespace'] or not re.fullmatch(r'/tmp/rcwss[0-9]{2}', str(directory))
            or directory.exists() or directory.is_symlink() or plan['selector'] != SELECTOR
            or plan['test_timeout_seconds'] != 60 or plan['outer_wait_seconds'] != 70
            or plan['tcp_tail_seconds'] != 75 or plan['tcp_empty_observations'] != 2):
        raise ValueError('fresh exact native plan required')
    if any(sha(path) != expected for path, expected in inputs.items()):
        raise ValueError('frozen runtime inputs changed before invocation')
    binary = plan['binary']
    if binary['path'] not in inputs or inputs[binary['path']] != binary['sha256']:
        raise ValueError('binary not bound')
    helpers = original_helpers()
    tcp, descendants = helpers['tcp'], helpers['descendants']
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:  # Same PR_SET_CHILD_SUBREAPER ownership as original supervisor.
        raise OSError(ctypes.get_errno(), 'subreaper setup failed')
    directory.mkdir(mode=0o700)
    temporary = directory / 'tmp'
    temporary.mkdir(mode=0o700)
    baseline = tcp()
    save(directory / 'tcp-baseline.json', sorted(baseline))
    started = time.monotonic()
    command = [binary['path'], '-test.v', '-test.count=1', '-test.timeout=60s', '-test.run', SELECTOR]
    save(directory / 'invocation.json', {'command': command, 'cwd': str(ROOT), 'supervisor_pid': os.getpid()})
    child, interrupted = None, False

    def stop(signum, frame):
        nonlocal interrupted
        interrupted = True
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)

    old = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
    code, actual = 1, None
    with (directory / 'native.log').open('x', buffering=1) as log:
        with (directory / 'test.log').open('xb') as testlog:
            child = subprocess.Popen(command, cwd=ROOT, env=dict(os.environ, TMPDIR=str(temporary)),
                                     stdout=testlog, stderr=subprocess.STDOUT)
            save(directory / 'process.json', {'pid': child.pid, 'supervisor_pid': os.getpid()})
            print(json.dumps({'test_pid': child.pid, 'directory': str(directory)}), flush=True)
            try:
                actual = child.wait(timeout=70)
                code = int(actual != 0)
            except subprocess.TimeoutExpired:
                log.write('STOP test exceeded runtime budget\n')
                child.terminate()
                try:
                    actual = child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    child.kill()
                    actual = child.wait(timeout=3)
                code = 1
            testlog.flush()
            os.fsync(testlog.fileno())
        log.write(f'WSS actual_test_wait pid={child.pid} code={actual}\n')
        complete = result_complete((directory / 'test.log').read_text(), actual)
        log.write(f'WSS exact_run_pass_and_owned_resources={complete}\n')
        code = int(code or not complete or interrupted)
        survivors = descendants(os.getpid())
        if survivors:
            code = 1
            log.write(f'STOP owned descendants survived test: {sorted(survivors)}\n')
            for pid in survivors:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        deadline = time.monotonic() + 3
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
                if pid == 0:
                    if time.monotonic() >= deadline:
                        code = 1
                        log.write('STOP owned descendants not actually joined\n')
                        break
                    time.sleep(.02)
                    continue
                log.write(f'WSS adopted_actual_wait pid={pid} status={status}\n')
                code = 1  # Any adopted child means the original test did not reap its command.
            except ChildProcessError:
                break
        for observation in (1, 2):
            remaining = descendants(os.getpid())
            private = sorted(str(p.relative_to(temporary)) for p in temporary.rglob('*'))
            log.write(f'OWNED observation={observation} descendants={sorted(remaining)} private={private}\n')
            if remaining or private:
                code = 1
        # Verbatim original 75-second automatic host delta/two-empty gate.
        tail_deadline = time.monotonic() + 75
        empty, delta = 0, set()
        while time.monotonic() < tail_deadline and empty < 2:
            delta = tcp() - baseline
            if not delta:
                empty += 1
                log.write(f'HOST_TCP delta_empty_observation={empty}\n')
            else:
                empty = 0
            if empty < 2:
                time.sleep(.1)
        if empty != 2:
            code = 1
            log.write(f'STOP host TCP delta tail not empty: {len(delta)} rows\n')
            sample = helpers['tcp_failure_sample'](delta, 'supervisor')
            log.write('HOST_TCP failure_sample=' + json.dumps(sample, sort_keys=True) + '\n')
            print(json.dumps({'host_tcp_failure': sample}, sort_keys=True), flush=True)
        unchanged = all(sha(path) == expected for path, expected in inputs.items())
        code = int(code or not unchanged)
        log.write(f'inputs_unchanged={unchanged} terminal={code}\n')
    for sig, handler in old.items():
        signal.signal(sig, handler)
    result = {'supervisor_pid': os.getpid(), 'test_pid': child.pid, 'actual_test_wait_exit': actual,
              'exact_test_and_resource_proof': complete, 'inputs_unchanged': unchanged,
              'tcp_empty_observations': empty, 'exit_code': code,
              'elapsed_seconds': round(time.monotonic() - started, 3)}
    save(request / 'result.json', result)
    print(json.dumps(result), flush=True)
    return code


if __name__ == '__main__':
    raise SystemExit(main())
