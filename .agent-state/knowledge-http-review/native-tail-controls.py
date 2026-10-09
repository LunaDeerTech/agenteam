#!/usr/bin/env python3
"""Independent native exact-gate failure tail; all OS/process calls are doubles."""
import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path('/workspace/agenteam-knowledge-http')
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('native_tail_review', SOURCE)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
original_gate = module.knowledge_native_exact
checks = []

with tempfile.TemporaryDirectory(prefix='native-tail-review-') as name:
    directory = Path(name)
    binary, driver = directory / 'binary', directory / 'driver'
    binary.write_bytes(b'not-executed')
    driver.write_bytes(b'not-executed')
    for mode in ('invalid-utf8', 'legacy-selector'):
        calls = {'wait': [], 'desc': 0, 'reap': 0, 'tcp': 0, 'gate': 0}
        selector = (module.KNOWLEDGE_NATIVE_SELECTOR if mode == 'invalid-utf8'
                    else '^TestWorkHTTPNativeDeadlines$')

        class Child:
            pid = 717171
            returncode = None

            def __init__(self, args, stdout, stderr):
                assert args[:5] == [str(driver), '--test-binary', str(binary), '--run', selector]
                stdout.flush()
                stdout.buffer.write(b'original-child-output\n' + (b'\xff\n' if mode == 'invalid-utf8' else b''))
                stdout.buffer.flush()

            def wait(self, timeout):
                calls['wait'].append(timeout)
                self.returncode = 0
                return 0

        def descendants(_):
            calls['desc'] += 1
            return set()

        def tcp():
            calls['tcp'] += 1
            return set()

        def reap(pid, flags):
            assert pid == -1 and flags == module.os.WNOHANG
            calls['reap'] += 1
            raise ChildProcessError

        def gate(path):
            assert mode == 'invalid-utf8', 'native gate entered for legacy target'
            calls['gate'] += 1
            return original_gate(path)

        output = directory / mode
        argv = ['review', '--driver', str(driver), '--binary', str(binary),
                '--run', selector, '--output', str(output)]
        with patch.object(sys, 'argv', argv), \
             patch.object(module.ctypes, 'CDLL', return_value=SimpleNamespace(prctl=lambda *_: 0)), \
             patch.object(module.subprocess, 'Popen', Child), \
             patch.object(module, 'descendants', descendants), patch.object(module, 'tcp', tcp), \
             patch.object(module.os, 'waitpid', reap), patch.object(module.time, 'sleep'), \
             patch.object(module.signal, 'signal'), patch.object(module, 'knowledge_native_exact', gate), \
             contextlib.redirect_stdout(io.StringIO()):
            actual = module.main()
        expected = 1 if mode == 'invalid-utf8' else 0
        assert actual == expected
        assert calls == {'wait': [123], 'desc': 3, 'reap': 1, 'tcp': 3,
                         'gate': 1 if mode == 'invalid-utf8' else 0}
        log = next(output.glob('*.log')).read_text(errors='replace')
        for text in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2',
                     'delta_empty_observation=1', 'delta_empty_observation=2',
                     'inputs_unchanged=True terminal=' + str(expected)):
            assert text in log
        checks.append(mode)
print('PASS actual main: invalid UTF8 remains FAIL with complete original tail; legacy skips new gate; no child/proc/socket/PG')
