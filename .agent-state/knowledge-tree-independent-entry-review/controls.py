#!/usr/bin/env python3
"""Independent selector-tail controls. All process/resource/TCP effects are doubles."""
import argparse
import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import tempfile
import types
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path, default=Path(__file__).resolve().parents[3] / 'agenteam-knowledge-tree-http')
root = parser.parse_args().source.resolve()
own = Path(__file__).resolve().parents[2]
output = own / 'output/ai/knowledge-independent-entry-review'
output.mkdir(parents=True, exist_ok=True)
path = root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('independent_knowledge_entry', path)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
selector = '^TestKnowledgeTreeCommandHTTPIndependentReceiptOwner$'
top = 'TestKnowledgeTreeCommandHTTPIndependentReceiptOwner'
inputs = set(sup.tree_command_inputs())
assert root / 'tests/knowledge/owner_tree_commands_independent_test.go' in inputs
assert set((root / 'tests/knowledge').glob('*.go')) <= inputs
assert root / '.agent-state/project-variables-independent/commitproxy/proxy.go' in inputs
checks = 3
with tempfile.TemporaryDirectory(dir=output) as temporary:
    base = Path(temporary)
    for mode in ('valid', 'resource-false', 'private-link', 'runtime-leftover', 'wrong-wait-selector', 'source-change', 'driver2'):
        driver, binary, dynamic = (base / (n + '-' + mode) for n in ('driver', 'binary', 'source'))
        for p in (driver, binary, dynamic):
            p.write_bytes(b'explicit controlled input')
        private = base / ('private-' + mode)
        trace = {'wait': [], 'resources': 0, 'desc': 0, 'tcp': 0, 'reap': 0}

        class Child:
            pid = 888888
            returncode = None

            def __init__(self, args, stdout, stderr):
                directory = Path(args[-1]); directory.mkdir()
                runtime = directory / 'runtime'; runtime.mkdir()
                if mode == 'runtime-leftover':
                    (runtime / 'held').write_text('controlled')
                if mode == 'private-link':
                    private.symlink_to(base / 'absent')
                logged = '^TestWrong$' if mode == 'wrong-wait-selector' else selector
                stdout.write(f'=== RUN   {top}\n--- PASS: {top} (0.01s)\nD03 explicit test actual_wait pid=777777 code=0 selector={logged}\n')
                stdout.flush()

            def wait(self, timeout):
                trace['wait'].append(timeout)
                if mode == 'source-change':
                    dynamic.write_bytes(b'changed during original wait')
                self.returncode = 2 if mode == 'driver2' else 0
                return self.returncode

        def absent(item, timeout):
            trace['resources'] += 1
            return not (mode == 'resource-false' and trace['resources'] == 1)

        def descendants(pid):
            trace['desc'] += 1
            return set()

        def tcp():
            trace['tcp'] += 1
            return set()

        def reap(*args):
            trace['reap'] += 1
            raise ChildProcessError()

        record = {'resources': [{'kind': 'container', 'id': f'{n+1:064x}', 'nonce': 'b'*32} for n in range(7)],
                  'directories': [str(private), str(base / 'absent-two'), str(base / 'absent-three')]}
        adapter = types.SimpleNamespace(TARGETS={selector: 'tests/knowledge'}, input_paths=lambda _: [binary, driver], sha=lambda p: __import__('hashlib').sha256(Path(p).read_bytes()).hexdigest())
        argv = ['probe', '--root-chain', '--driver', str(driver), '--binary', str(binary), '--run', selector, '--output', str(base / mode)]
        with patch.object(sys, 'argv', argv), patch.object(sup, 'root_adapter', return_value=adapter), patch.object(sup, 'tree_command_inputs', return_value=[dynamic]), patch.object(sup, 'root_record', return_value=record), patch.object(sup, 'exact_absent', absent), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *args: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', descendants), patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), patch.object(sup.signal, 'signal'), patch.object(sup.time, 'sleep'), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        expected = 0 if mode == 'valid' else 2 if mode == 'driver2' else 1
        assert code == expected, (mode, code)
        assert trace == {'wait': [540], 'resources': 14, 'desc': 3, 'tcp': 3, 'reap': 1}, (mode, trace)
        text = next((base / mode).glob('*.log')).read_text()
        for marker in ('ROOT tree_commands_exact=True', 'ROOT private_observation=2', 'ROOT runtime_observation=2', 'OWNED runtime_observation=2 descendants=[]', 'HOST_TCP delta_empty_observation=2', 'SUPERVISOR inputs_unchanged='):
            assert marker in text, (mode, marker)
        if mode == 'source-change':
            assert 'inputs_unchanged=False terminal=1' in text
        checks += 3
print(f'PASS {checks} independent checks; seven actual-main scenarios with process/resource/TCP doubles; no Go/PG/socket')
