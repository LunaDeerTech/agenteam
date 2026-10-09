#!/usr/bin/env python3
"""Pure closed native group checks. All process/OS/TCP effects are doubles."""
import ast
import contextlib
import importlib.util
import io
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
BASE = '391f0cb7'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/native_driver.go'
SELECTOR = '^TestKnowledgeHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$'
GROUPS = {
    'TestKnowledgeHTTPNativeDeadlines': ('read-natural', 'earlier-parent'),
    'TestKnowledgeHTTPNativeKeepAliveAndClose': ('cleared-deadline-keeps-real-connection', 'real-body-close-error-aborts-before-response'),
    'TestKnowledgeHTTPNativeBackpressureAndDisconnect': ('summary-write-natural-deadline', 'disconnect-cancels-actual-library-tail'),
}


def original(path):
    return subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT, check=True, capture_output=True, text=True).stdout


source = (ROOT / SUP).read_text()
ast.parse(source)
start = source.index('KNOWLEDGE_NATIVE_SELECTOR = ')
end = source.index('def root_adapter(driver):', start)
inverse = source[:start] + source[end:]
start = inverse.index('            if not args.root_chain and args.run == KNOWLEDGE_NATIVE_SELECTOR:')
end = inverse.index('            # The tail is a host delta', start)
inverse = inverse[:start] + inverse[end:]
assert inverse == original(SUP)
driver = (ROOT / DRIVER).read_text()
start = driver.index('func knowledgeNative(selector string) bool {')
end = driver.index('func run() int {', start)
inverse = driver[:start] + driver[end:]
inverse = inverse.replace(' && !knowledgeNative(*selector)', '').replace(' else if knowledgeNative(*selector) {\n\t\tnativeGate = "AGENTEAM_KNOWLEDGE_HTTP_NATIVE"\n\t}', '')
assert inverse == original(DRIVER)
assert driver.count('"' + SELECTOR + '"') == 1
assert '"-test.timeout=90s"' in driver and '105*time.Second' in driver
assert 'cmd.Wait()' in driver and 'NATIVE runtime_empty=%t actual_child_wait=true' in driver
native = (ROOT / 'internal/central/knowledge/http/native_test.go').read_text()
assert set(re.findall(r'^func (TestKnowledgeHTTPNative\w+)\(', native, re.M)) == set(GROUPS)
assert set(re.findall(r't.Run\("([^"\n]+)"', native)) | {'read-natural', 'earlier-parent'} == {x for values in GROUPS.values() for x in values}
assert 'os.Getenv("AGENTEAM_KNOWLEDGE_HTTP_NATIVE") != "1"' in native
assert 't.Parallel(' not in native

spec = importlib.util.spec_from_file_location('knowledge_native_supervisor_controls', ROOT / SUP)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
assert sup.KNOWLEDGE_NATIVE_SELECTOR == SELECTOR
assert sup.budgets(False) == (123, 3) and sup.budgets(True) == (540, 60)
PARENTS = tuple(GROUPS)
CHILDREN = tuple(parent + '/' + child for parent, names in GROUPS.items() for child in names)


def log(children=CHILDREN):
    return ''.join('=== RUN   ' + parent + '\n' + ''.join('=== RUN   ' + child + '\n    --- PASS: ' + child + ' (0.01s)\n' for child in children if child.startswith(parent + '/')) + '--- PASS: ' + parent + ' (0.02s)\n' for parent in PARENTS)


checks = 0
with tempfile.TemporaryDirectory(prefix='knowledge-native-controls-') as name:
    temp = Path(name)
    path = temp / 'child.log'
    for mask in range(64):
        path.write_text(log(tuple(child for i, child in enumerate(CHILDREN) if mask & (1 << i))))
        assert sup.knowledge_native_exact(path) == (mask == 63)
        checks += 1
    good = log()
    bad = [good.replace('--- PASS: ' + CHILDREN[0], '--- SKIP: ' + CHILDREN[0]),
           good.replace('--- PASS: ' + PARENTS[0] + ' ', '--- FAIL: ' + PARENTS[0] + ' '),
           good + '=== RUN   TestForeign\n--- PASS: TestForeign (0.01s)\n',
           good + '--- PASS: ' + CHILDREN[0] + ' (0.00s)\n',
           good + '=== RUN   ' + CHILDREN[0] + '\n',
           good.replace('=== RUN   ' + PARENTS[0] + '\n', ''),
           good.encode() + b'\xff\n']
    for value in bad:
        path.write_bytes(value if isinstance(value, bytes) else value.encode())
        assert not sup.knowledge_native_exact(path)
        checks += 1
    path.unlink()
    assert not sup.knowledge_native_exact(path)
    checks += 1
    for mode in ('valid', 'missing-child', 'extra-top', 'driver-exit2'):
        binary, adapter = temp / 'binary', temp / 'adapter'
        binary.write_text('not-executed')
        adapter.write_text('not-executed')
        trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0}

        class Child:
            pid = 434343
            returncode = None

            def __init__(self, args, stdout, stderr):
                assert args[:5] == [str(adapter), '--test-binary', str(binary), '--run', SELECTOR]
                output = log(CHILDREN[:-1]) if mode == 'missing-child' else good
                if mode == 'extra-top': output += '=== RUN   TestForeign\n--- PASS: TestForeign (0.01s)\n'
                stdout.write(output)
                stdout.flush()

            def wait(self, timeout):
                trace['wait'].append(timeout)
                self.returncode = 2 if mode == 'driver-exit2' else 0
                return self.returncode

        def desc(_):
            trace['desc'] += 1
            return set()

        def tcp():
            trace['tcp'] += 1
            return set()

        def reap(*_):
            trace['reap'] += 1
            raise ChildProcessError

        output = temp / mode
        argv = ['probe', '--driver', str(adapter), '--binary', str(binary), '--run', SELECTOR, '--output', str(output)]
        with patch.object(sys, 'argv', argv), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', desc), patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        expected = 0 if mode == 'valid' else 2 if mode == 'driver-exit2' else 1
        assert code == expected and trace == {'wait': [123], 'desc': 3, 'tcp': 3, 'reap': 1}
        actual = next(output.glob('*.log')).read_text()
        for marker in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2', 'delta_empty_observation=1', 'delta_empty_observation=2', 'inputs_unchanged=True terminal=' + str(expected)):
            assert marker in actual
        checks += 1
print(f'PASS native entry controls={checks}; inverse defaults exact; 3 parents/6 children; 90/105/123+3/TCP75 preserved; no Go/socket/PG')
