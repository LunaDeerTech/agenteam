from __future__ import annotations

import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

root = Path(__file__).parent
run = root / sys.argv[1]
run.mkdir(exist_ok=False)
node = '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node'
command = [node, '/workspace/agenteam/web/node_modules/vitest/vitest.mjs',
           'run', '--config', str(root / 'vitest.config.mjs'), '--reporter=json',
           '--outputFile=' + str(run / 'vitest.json')]
paths = [p for p in root.rglob('*') if p.is_file() and not p.is_relative_to(run)
         and not any(v.startswith('.') or v.startswith('run0') for v in p.relative_to(root).parts)
         and 'node_modules' not in p.parts]
paths += [Path(node), Path('/workspace/agenteam/web/package-lock.json'),
          Path('/workspace/agenteam/web/node_modules/vitest/vitest.mjs'),
          Path('/workspace/agenteam/web/node_modules/vitest/package.json'),
          Path('/workspace/agenteam/web/node_modules/vite/package.json')]


def fingerprints():
    return {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}


def processes():
    result = {}
    for path in Path('/proc').glob('[0-9]*/stat'):
        try:
            raw = path.read_text()
            values = raw[raw.rindex(')') + 2:].split()
            result[int(path.parent.name)] = {
                'ppid': int(values[1]), 'pgrp': int(values[2]),
                'state': values[0], 'starttime': values[19],
            }
        except (OSError, ValueError, IndexError):
            pass
    return result


assert ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0
before = fingerprints()
owned = {}
started = time.monotonic()
environment = {k: v for k, v in os.environ.items() if not k.startswith('AGENTEAM_')}
environment['CI'] = '1'
timed_out = False
with (run / 'stdout.raw').open('wb') as stdout, (run / 'stderr.raw').open('wb') as stderr:
    process = subprocess.Popen(command, cwd=root, env=environment, stdout=stdout, stderr=stderr, start_new_session=True)
    while process.poll() is None:
        current = processes()
        for pid, info in current.items():
            if info['pgrp'] == process.pid or info['ppid'] == os.getpid():
                owned[(pid, info['starttime'])] = info
        if time.monotonic() - started >= 45:
            timed_out = True
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
            break
        time.sleep(0.02)
    direct_exit = process.wait()
adopted = []
while True:
    try:
        pid, status = os.waitpid(-1, os.WNOHANG)
    except ChildProcessError:
        break
    if pid:
        adopted.append({'pid': pid, 'wait_status': status})
        continue
    # A surviving adopted process is failure evidence; join it rather than leak it.
    current = processes()
    for pid, info in current.items():
        if info['ppid'] == os.getpid():
            owned[(pid, info['starttime'])] = info
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
    time.sleep(0.05)
    if time.monotonic() - started > 49:
        for pid, info in processes().items():
            if info['ppid'] == os.getpid():
                os.kill(pid, signal.SIGKILL)

cleanup = []
for _ in range(2):
    current = processes()
    cleanup.append([
        {'pid': pid, 'starttime': start, **current[pid]}
        for pid, start in owned
        if pid in current and current[pid]['starttime'] == start
    ])
    time.sleep(0.03)
after = fingerprints()
report = json.loads((run / 'vitest.json').read_text()) if (run / 'vitest.json').exists() else {}
result = {
    'argv': command, 'cwd': str(root), 'timeout_seconds': 45,
    'elapsed_seconds': time.monotonic() - started,
    'timed_out': timed_out, 'direct_actual_wait_exit': direct_exit,
    'adopted_actual_waits': adopted,
    'owned_observed': [{'pid': pid, 'starttime': start, **info} for (pid, start), info in owned.items()],
    'owned_cleanup_two_scans': cleanup, 'inputs_before': before, 'inputs_after': after,
    'inputs_identical': before == after,
    'test_success': report.get('success'), 'total_tests': report.get('numTotalTests'),
    'passed_tests': report.get('numPassedTests'), 'failed_tests': report.get('numFailedTests'),
    'browser_pg_listener_started': False,
}
result['pass'] = direct_exit == 0 and not timed_out and not any(cleanup) and before == after and report.get('success') is True
(run / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({key: result[key] for key in ['pass', 'direct_actual_wait_exit', 'elapsed_seconds',
      'total_tests', 'passed_tests', 'failed_tests', 'inputs_identical', 'owned_cleanup_two_scans']}, indent=2))
raise SystemExit(0 if result['pass'] else 1)
