#!/usr/bin/env python3
"""Run frozen D04 supervisor main with controlled process/TCP/OS boundaries.

No Docker, socket, /proc sampling, Go compilation or product source mutation.
The real log parser/input comparison/main are used; resource retirement remains
the unchanged Go driver's responsibility and requires its actual PG run.
"""
import contextlib
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest.mock import patch

ROOT = Path('/workspace/agenteam-secret-variable-storage')
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
OUTPUT = ROOT / 'output/ai/secret-variable-storage'
TMP = Path('/workspace/agenteam-skills/output/ai/skills/compile/tmp')


def load():
    spec = importlib.util.spec_from_file_location('independent_d04_sql_entry', SOURCE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run_case(label, maintenance=False):
    module = load()
    selector = module.SECRET_STORAGE_RECOVERY_STATE if maintenance else module.SECRET_STORAGE_RECOVERY_WRITE
    expected = sorted(module.SECRET_STORAGE_RECOVERY_CASES[selector], reverse=True)
    trace = {'tcp': 0, 'desc': 0, 'wait': [], 'inputs': 0}
    original_inputs = module.secret_storage_inputs
    original_read = Path.read_text
    child_code = 2 if label == 'driver-exit2' else 0

    def inputs(driver, binary, recovery):
        assert recovery is True
        trace['inputs'] += 1
        if label == 'input-unreadable' and trace['inputs'] == 2:
            raise FileNotFoundError('controlled final input disappearance')
        return original_inputs(driver, binary, recovery)

    def read(path, *args, **kwargs):
        if label == 'log-oserror' and path.name.startswith('pg-') and path.suffix == '.log':
            raise OSError('controlled log failure')
        return original_read(path, *args, **kwargs)

    def tcp():
        trace['tcp'] += 1
        return set()

    def descendants(pid):
        assert pid == os.getpid()
        trace['desc'] += 1
        return set()

    def waited(*args):
        raise ChildProcessError

    class Child:
        pid = 12345
        returncode = None

        def __init__(self, args, stdout, stderr):
            assert args[:5] == [str(OUTPUT / 'pg-only-recovery-driver'), '--test-binary',
                               str(OUTPUT / 'secret-variable-storage-recovery.test'),
                               '--run', selector]
            assert args[5] == '--directory' and stderr == subprocess.STDOUT
            for name in expected:
                stdout.write(f'=== RUN   {name}\n')
                if label == 'missing-pass' and name == expected[0]:
                    continue
                stdout.write(f'--- PASS: {name} (0.01s)\n')
            if label == 'duplicate-pass':
                stdout.write(f'--- PASS: {expected[0]} (0.01s)\n')
            stdout.flush()
            if label == 'invalid-utf8':
                os.write(stdout.fileno(), b'\xff\n')

        def wait(self, timeout):
            trace['wait'].append(timeout)
            self.returncode = child_code
            return child_code

    class Libc:
        @staticmethod
        def prctl(*args):
            assert args == (36, 1, 0, 0, 0)
            return 0

    with tempfile.TemporaryDirectory(dir=TMP, prefix='d04-sql-entry-review-') as directory:
        argv = ['review', '--driver', str(OUTPUT / 'pg-only-recovery-driver'), '--binary',
                str(OUTPUT / 'secret-variable-storage-recovery.test'), '--run', selector,
                '--output', directory]
        with contextlib.ExitStack() as stack:
            stack.enter_context(patch.object(sys, 'argv', argv))
            stack.enter_context(patch.object(module.ctypes, 'CDLL', return_value=Libc()))
            stack.enter_context(patch.object(module.signal, 'signal', return_value=None))
            stack.enter_context(patch.object(module.subprocess, 'Popen', Child))
            stack.enter_context(patch.object(module.os, 'waitpid', waited))
            stack.enter_context(patch.object(module.time, 'sleep', lambda _: None))
            stack.enter_context(patch.object(module, 'tcp', tcp))
            stack.enter_context(patch.object(module, 'descendants', descendants))
            stack.enter_context(patch.object(module, 'secret_storage_inputs', inputs))
            stack.enter_context(patch.object(Path, 'read_text', read))
            stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            code = module.main()
        log_path, = Path(directory).glob('*.log')
        log = log_path.read_bytes().decode('utf-8', errors='replace')
        wanted = 0 if label == 'valid' else 2 if label == 'driver-exit2' else 1
        assert code == wanted, (label, code)
        assert trace == {'tcp': 3, 'desc': 3, 'wait': [123], 'inputs': 2}, trace
        for marker in ('actual_driver_wait pid=12345 actual=True',
                       'OWNED runtime_observation=1 descendants=[]',
                       'OWNED runtime_observation=2 descendants=[]',
                       'HOST_TCP delta_empty_observation=1',
                       'HOST_TCP delta_empty_observation=2',
                       f'terminal={wanted}'):
            assert marker in log, (label, marker)
        assert ('inputs_unchanged=False' in log) == (label == 'input-unreadable')
        assert ('exact_cases=True' in log) == (label in ('valid', 'driver-exit2', 'input-unreadable'))
    print(f'{label} maintenance={maintenance}: pass; no real child/resources')


if __name__ == '__main__':
    # New selector/artifact closure with the original main. No old business run.
    for maintenance in (False, True):
        for label in ('valid', 'driver-exit2', 'invalid-utf8', 'input-unreadable'):
            run_case(label, maintenance=maintenance)
    print('8 recovery actual-main delta controls passed; resources=0')
