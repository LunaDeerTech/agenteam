from __future__ import annotations

import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

root = Path(__file__).parent
repo = Path('/workspace/agenteam')
frozen = Path('/workspace/scratch/owner-ui-recovery/ui-v1')
node = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node')
freeze = json.loads((frozen / 'freeze.json').read_text())
assert hashlib.sha256((frozen / 'freeze.json').read_bytes()).hexdigest() == 'b7a9ef34a10d0230b541d0daffb19d765d023aa27469a9ca144952f5d1bd9f64'
run = root / sys.argv[1]
run.mkdir(exist_ok=False)
paths = {root / 'run_checks.py', root / 'vitest.config.mjs', frozen / 'freeze.json', node,
         repo / 'web/package-lock.json', repo / 'web/package.json',
         repo / 'web/node_modules/vitest/vitest.mjs', repo / 'web/node_modules/vitest/package.json',
         repo / 'web/node_modules/vite/package.json', repo / 'web/node_modules/jsdom/package.json',
         repo / 'web/node_modules/vue/package.json', repo / 'web/node_modules/@vue/test-utils/package.json'}
paths.update(repo / name for name in freeze)
seeds = list(root.glob('*.ts'))
closure = set()
patterns = [r'\bfrom\s+[\'"]([^\'"]+)[\'"]', r'\bimport\s*(?:\(\s*)?[\'"]([^\'"]+)[\'"]']
while seeds:
    path = seeds.pop()
    if path in closure:
        continue
    closure.add(path)
    contents = path.read_text()
    for pattern in patterns:
        for spec in re.findall(pattern, contents):
            if not (spec.startswith('.') or spec.startswith('/workspace/')):
                continue
            base = (path.parent / spec).resolve()
            choices = [base, *(Path(str(base) + ext) for ext in ['.ts', '.vue', '.js']), base / 'index.ts']
            found = next((entry for entry in choices if entry.is_file()), None)
            if found is None:
                raise RuntimeError(f'Unresolved local import {path}: {spec}')
            seeds.append(found)
paths.update(closure)


def hashes():
    return {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(paths)}


before = hashes()
binding = []
for path in sorted(paths):
    if not path.is_relative_to(repo / 'web/src'):
        continue
    name = str(path.relative_to(repo))
    if name in freeze:
        expected = freeze[name]
        assert hashlib.sha256((frozen / name).read_bytes()).hexdigest() == expected
        origin = 'ui-v1/freeze.json'
    else:
        raw = subprocess.check_output(['git', 'show', '7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2:' + name], cwd=repo)
        expected = hashlib.sha256(raw).hexdigest()
        origin = '7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2:' + name
    assert before[str(path)] == expected, f'Unfrozen source {name}'
    binding.append({'path': name, 'source': origin, 'sha256': expected})
(run / 'source-binding.json').write_text(json.dumps(binding, indent=2) + '\n')
command = [str(node), str(repo / 'web/node_modules/vitest/vitest.mjs'), 'run',
           '--config', str(root / 'vitest.config.mjs'), '--reporter=json', '--reporter=default',
           '--outputFile.json=' + str(run / 'vitest.json')]
(run / 'command.json').write_text(json.dumps({'argv': command, 'cwd': str(root), 'timeout_seconds': 45}, indent=2) + '\n')


def processes():
    result = {}
    for path in Path('/proc').glob('[0-9]*/stat'):
        try:
            raw = path.read_text()
            values = raw[raw.rindex(')') + 2:].split()
            result[int(path.parent.name)] = {'ppid': int(values[1]), 'pgrp': int(values[2]), 'state': values[0], 'starttime': values[19]}
        except (OSError, ValueError, IndexError):
            pass
    return result


assert ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0
owned = {}
started = time.monotonic()
environment = {key: value for key, value in os.environ.items() if not key.startswith('AGENTEAM_')}
environment['CI'] = '1'
timed_out = False
with (run / 'stdout.raw').open('wb') as stdout, (run / 'stderr.raw').open('wb') as stderr:
    process = subprocess.Popen(command, cwd=root, env=environment, stdout=stdout, stderr=stderr, start_new_session=True)
    while process.poll() is None:
        for pid, info in processes().items():
            if info['pgrp'] == process.pid or info['ppid'] == os.getpid():
                owned[(pid, info['starttime'])] = info
        if time.monotonic() - started >= 45:
            timed_out = True
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=1)
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
    for pid, info in processes().items():
        if info['ppid'] == os.getpid():
            owned[(pid, info['starttime'])] = info
            try:
                os.kill(pid, signal.SIGTERM if time.monotonic() - started < 47 else signal.SIGKILL)
            except ProcessLookupError:
                pass
    time.sleep(0.03)
scans = []
for _ in range(2):
    current = processes()
    scans.append([{'pid': pid, 'starttime': start, **current[pid]} for pid, start in owned if pid in current and current[pid]['starttime'] == start])
    time.sleep(0.03)
after = hashes()
report = json.loads((run / 'vitest.json').read_text()) if (run / 'vitest.json').exists() else {}
result = {
    'argv': command, 'cwd': str(root), 'timeout_seconds': 45,
    'elapsed_seconds': time.monotonic() - started, 'timed_out': timed_out,
    'direct_actual_wait_exit': direct_exit, 'adopted_actual_waits': adopted,
    'owned_observed': [{'pid': pid, 'starttime': start, **info} for (pid, start), info in owned.items()],
    'owned_cleanup_two_scans': scans, 'inputs_before': before, 'inputs_after': after,
    'inputs_identical': before == after, 'source_bindings': len(binding),
    'test_success': report.get('success'), 'total_tests': report.get('numTotalTests'),
    'passed_tests': report.get('numPassedTests'), 'failed_tests': report.get('numFailedTests'),
    'browser_pg_listener_started': False,
}
result['pass'] = direct_exit == 0 and not timed_out and not any(scans) and before == after and report.get('success') is True
(run / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({key: result[key] for key in ['pass', 'direct_actual_wait_exit', 'elapsed_seconds', 'total_tests', 'passed_tests', 'failed_tests', 'inputs_identical', 'source_bindings', 'owned_cleanup_two_scans']}, indent=2))
raise SystemExit(0 if result['pass'] else 1)
