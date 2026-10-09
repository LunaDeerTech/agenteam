#!/usr/bin/env python3
"""One exact D11 PG top, actual child wait, then bounded host TCP tail.

Example: python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
 --driver /absolute/pg-only-driver --binary /absolute/work.test \
 --run '^TestTaskPlanningMigration$' --output /absolute/existing/output-dir
The output directory is reusable; each run/nonce directory must be new.
"""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import uuid


def tcp():
    rows = set()
    for name in ('tcp', 'tcp6'):
        for line in Path('/proc/net/' + name).read_text().splitlines()[1:]:
            fields = line.split()
            rows.add((name, fields[1], fields[2], fields[3], fields[9]))
    return rows


def descendants(root):
    parents = {}
    for stat in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = stat.read_text().rsplit(')', 1)[1].split()
            parents[int(stat.parent.name)] = int(fields[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    result = {root}
    changed = True
    while changed:
        added = {pid for pid, parent in parents.items() if parent in result} - result
        changed = bool(added)
        result |= added
    return result - {root}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--driver', required=True, type=Path)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--run', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    stem = 'pg-' + uuid.uuid4().hex
    directory = args.output.resolve() / stem
    log_path = args.output / (stem + '.log')
    # Adopt only this supervisor's own descendants, so any unexpected survivor
    # can be actually waited and reported rather than inferred dead from ps.
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), 'PR_SET_CHILD_SUBREAPER')
    inputs = {str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest()
              for p in (args.driver, args.binary)}
    baseline = tcp()
    started = time.monotonic()
    child = None
    code = 1
    interrupted = False
    def stop(signum, frame):
        nonlocal interrupted
        interrupted = True
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
    old = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
    with log_path.open('w', buffering=1) as log:
        try:
            child = subprocess.Popen([str(args.driver.resolve()), '--test-binary',
                str(args.binary.resolve()), '--run', args.run, '--directory', str(directory)],
                stdout=log, stderr=subprocess.STDOUT)
            try:
                code = child.wait(timeout=123)
            except subprocess.TimeoutExpired:
                child.terminate()
                try:
                    code = child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    child.kill()
                    code = child.wait()
                code = 1
                log.write('STOP driver exceeded runtime budget\n')
            log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} code={code}\n')
            survivors = descendants(os.getpid())
            if survivors:
                code = 1
                log.write(f'STOP owned descendants survived driver: {sorted(survivors)}\n')
                for pid in survivors:
                    try: os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError: pass
            while True:
                try:
                    pid, status = os.waitpid(-1, 0)
                    log.write(f'SUPERVISOR adopted_actual_wait pid={pid} status={status}\n')
                except ChildProcessError:
                    break
            for round in (1, 2):
                remaining = descendants(os.getpid())
                log.write(f'OWNED runtime_observation={round} descendants={sorted(remaining)}\n')
                if remaining: code = 1
            # The tail is a host delta, not an assertion that every short
            # connection in this shared host was owned by this invocation.
            tail_deadline = time.monotonic() + 75
            empty = 0
            while time.monotonic() < tail_deadline and empty < 2:
                delta = tcp() - baseline
                if not delta:
                    empty += 1
                    log.write(f'HOST_TCP delta_empty_observation={empty}\n')
                else:
                    empty = 0
                if empty < 2: time.sleep(.1)
            if empty != 2:
                code = 1
                log.write(f'STOP host TCP delta tail not empty: {len(tcp() - baseline)} rows\n')
            same = all(hashlib.sha256(Path(p).read_bytes()).hexdigest() == digest
                       for p, digest in inputs.items())
            if not same: code = 1
            if interrupted: code = 1
            log.write(f'SUPERVISOR inputs_unchanged={same} terminal={code} elapsed={time.monotonic()-started:.3f}s\n')
        finally:
            for s, handler in old.items(): signal.signal(s, handler)
    print(json.dumps({'selector': args.run, 'exit': code, 'log': str(log_path),
                      'owned_directory': str(directory), 'actual_driver_wait': child is not None and child.returncode is not None}))
    return code


if __name__ == '__main__':
    sys.exit(main())
