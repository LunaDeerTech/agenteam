#!/usr/bin/env python3
"""Only the independent PG selector/log gate; process/OS/TCP are doubles."""
import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('independent_project_supervisor', SOURCE)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
TOP = 'TestProjectSkillsCleanupIndependentRevalidation'
SELECTOR = '^' + TOP + '$'
NAMES = [TOP] + [TOP + '/' + suffix for suffix in (
    'progress_revoked_and_restored_in_original_tx',
    'owner_reread_after_prior_grant',
    'cancelled_original_call_cannot_reuse_prior_grant')]
LOG = ''.join(f'=== RUN   {name}\n--- PASS: {name} (0.00s)\n' for name in NAMES)

# Exact inverse projection also keeps the inherited author's three single tops.
source = SOURCE.read_text()
line = "    '^TestProjectSkillsCleanupIndependentRevalidation$': 'TestProjectSkillsCleanupIndependentRevalidation',\n"
assert source.count(line) == 1
inverse = source.replace(line, '')
start = inverse.index("    if selector == '^TestProjectSkillsCleanupIndependentRevalidation$':")
end = inverse.index("    runs = re.findall(r'^=== RUN   (Test\\w+)$'", start)
inverse = inverse[:start] + inverse[end:]
old = subprocess.run(['git', 'show', 'feff9e0c:.agent-state/task-planning-recovery/pg_only_supervisor.py'], cwd=ROOT, capture_output=True, text=True, check=True).stdout
assert inverse == old
driver = '.agent-state/task-planning-recovery/pg_only_driver.go'
assert (ROOT / driver).read_bytes() == subprocess.run(['git', 'show', 'feff9e0c:' + driver], cwd=ROOT, capture_output=True, check=True).stdout
assert sup.budgets(False) == (123, 3) and sup.budgets(True) == (540, 60)

variants = {
    'valid': LOG,
    'zero-child': ''.join(f'=== RUN   {TOP}\n--- PASS: {TOP} (0.01s)\n'),
    'wrong-child': LOG.replace(NAMES[1], TOP + '/wrong'),
    'missing-pass': LOG.replace(f'--- PASS: {NAMES[1]} (0.00s)\n', ''),
    'duplicate-pass': LOG + f'--- PASS: {NAMES[1]} (0.01s)\n',
    'duplicate-run': LOG + f'=== RUN   {TOP}\n',
    'extra-child': LOG + f'=== RUN   {TOP}/extra\n--- PASS: {TOP}/extra (0.00s)\n',
    'skip-child': LOG.replace(f'--- PASS: {NAMES[1]}', f'--- SKIP: {NAMES[1]}'),
    'fail-parent': LOG.replace(f'--- PASS: {TOP} ', f'--- FAIL: {TOP} '),
    'utf8': LOG.encode() + b'\xff\n',
}
checks = 0
with tempfile.TemporaryDirectory() as temporary:
    base = Path(temporary)
    log_path = base / 'go.log'
    for label, log in variants.items():
        log_path.write_bytes(log if isinstance(log, bytes) else log.encode())
        assert sup.skills_cleanup_exact_top(log_path, SELECTOR) == (label == 'valid'), label
        checks += 1
    log_path.unlink()
    assert not sup.skills_cleanup_exact_top(log_path, SELECTOR)
    checks += 1
    log_path.write_text(LOG)
    for selector in (TOP, SELECTOR + '/.*', '^TestProjectSkillsCleanupIndependent.*$', SELECTOR[:-1]):
        assert not sup.skills_cleanup_exact_top(log_path, selector)
        checks += 1

    # Actual supervisor main continues through all old tails on gate failure.
    for label in ('valid', 'zero-child', 'utf8', 'driver-exit2'):
        data = variants.get(label, LOG)
        driver_path, binary = base / 'driver', base / 'binary'
        driver_path.write_text('controlled fixed driver')
        binary.write_text('controlled fixed candidate')
        trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0}

        class Child:
            pid = 424242
            returncode = None

            def __init__(self, args, stdout, stderr):
                assert args[:5] == [str(driver_path), '--test-binary', str(binary), '--run', SELECTOR]
                stdout.flush()
                stdout.buffer.write(data if isinstance(data, bytes) else data.encode())
                stdout.buffer.flush()

            def wait(self, timeout):
                trace['wait'].append(timeout)
                self.returncode = 2 if label == 'driver-exit2' else 0
                return self.returncode

        def reap(*args):
            trace['reap'] += 1
            raise ChildProcessError

        def descendants(*args):
            trace['desc'] += 1
            return set()

        def tcp():
            trace['tcp'] += 1
            return set()

        output = base / label
        argv = ['review', '--driver', str(driver_path), '--binary', str(binary), '--run', SELECTOR, '--output', str(output)]
        with patch.object(sys, 'argv', argv), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', descendants), patch.object(sup.os, 'waitpid', reap), patch.object(sup, 'tcp', tcp), patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        want = 0 if label == 'valid' else 2 if label == 'driver-exit2' else 1
        assert code == want, (label, code)
        assert trace == {'wait': [123], 'desc': 3, 'tcp': 3, 'reap': 1}, trace
        log = next(output.glob('*.log')).read_bytes()
        for marker in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2', 'delta_empty_observation=1', 'delta_empty_observation=2', 'inputs_unchanged=True terminal=' + str(want)):
            assert marker.encode() in log, (label, marker)
        checks += 1
print(f'{checks} independent selector controls passed; old tools preserved; resources=0')
