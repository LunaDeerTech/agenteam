#!/usr/bin/env python3
"""Exact new entrances and original tail controls; no resource or child start."""
import ast
import contextlib
import hashlib
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
BASE = '7fee6d3c'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
ROOT_DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
NATIVE_DRIVER = '.agent-state/work-owner-http/native_driver.go'


def original(path):
    return subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT, check=True, capture_output=True, text=True).stdout


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


source = (ROOT / SUP).read_text()
ast.parse(source)
start, end = source.index('TREE_COMMAND_PG = '), source.index('def root_adapter(driver):')
inverse = source[:start] + source[end:]
inverse = inverse.replace('        TREE_COMMAND_PG: set(TREE_COMMAND_GROUPS[TREE_COMMAND_PG]),\n', '')
inverse = inverse.replace('        TREE_COMMAND_UNKNOWN: set(TREE_COMMAND_GROUPS[TREE_COMMAND_UNKNOWN]),\n', '')
inverse = inverse.replace('    if args.run in TREE_COMMAND_GROUPS:\n        inputs.update({str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in tree_command_inputs()})\n', '')
start = inverse.index('            if args.root_chain and not (tree_commands_root(')
end = inverse.index('            # The tail is a host delta', start)
inverse = inverse[:start] + '            if args.root_chain and not observe_root_chain(directory, log, log_path, args.run):\n                code = 1\n' + inverse[end:]
assert inverse == original(SUP)
sup = module(SUP, 'tree_command_supervisor_controls')
adapter = module(ROOT_DRIVER, 'tree_command_adapter_controls')
pg, native = sup.TREE_COMMAND_PG, sup.TREE_COMMAND_NATIVE
unknown = sup.TREE_COMMAND_UNKNOWN
assert sys.argv[1:] in ([], ['--unknown-only'])
selectors = (unknown,) if sys.argv[1:] else (pg, native, unknown)
root_selectors = (unknown,) if sys.argv[1:] else (pg, unknown)
assert sup.TREE_COMMAND_GROUPS[unknown] == {'TestKnowledgeTreeCommandHTTPUnknown': sup.TREE_COMMAND_GROUPS[pg]['TestKnowledgeTreeCommandHTTPUnknown']}
root_source = (ROOT / ROOT_DRIVER).read_text()
assert root_source.replace("    '" + pg + "': 'tests/knowledge',\n", '').replace("    '" + unknown + "': 'tests/knowledge',\n", '') == original(ROOT_DRIVER)
native_source = (ROOT / NATIVE_DRIVER).read_text()
start, end = native_source.index('func treeCommandNative('), native_source.index('func run() int {')
inverse = (native_source[:start] + native_source[end:]).replace(' && !treeCommandNative(*selector)', '')
inverse = inverse.replace(' else if treeCommandNative(*selector) {\n\t\tnativeGate = "AGENTEAM_KNOWLEDGE_TREE_HTTP_NATIVE"\n\t}', '')
assert inverse == original(NATIVE_DRIVER)
assert native_source.count('"' + native + '"') == 1
assert '105*time.Second' in native_source and '"-test.timeout=90s"' in native_source
assert sup.budgets(True) == (540, 60) and sup.budgets(False) == (123, 3)
inputs = set(sup.tree_command_inputs())
assert {ROOT / SUP, ROOT / ROOT_DRIVER, ROOT / NATIVE_DRIVER,
        ROOT / 'internal/central/knowledge/commandhttp/native_test.go',
        ROOT / 'tests/knowledge/owner_tree_commands_unknown_test.go',
        ROOT / '.agent-state/project-variables-independent/commitproxy/proxy.go'} <= inputs
assert all(p.is_file() and not p.is_symlink() for p in inputs)


def log(selector):
    return ''.join('=== RUN   ' + parent + '\n' + ''.join('=== RUN   ' + parent + '/' + child + '\n    --- PASS: ' + parent + '/' + child + ' (0.01s)\n' for child in children) + '--- PASS: ' + parent + ' (0.02s)\n' for parent, children in sup.TREE_COMMAND_GROUPS[selector].items())


checks = 0
with tempfile.TemporaryDirectory(prefix='tree-command-selector-') as name:
    temp = Path(name)
    path = temp / 'log'
    for selector in selectors:
        good = log(selector)
        path.write_text(good)
        assert sup.tree_commands_exact(path, selector)
        checks += 1
        for line in good.splitlines(keepends=True):
            path.write_text(good.replace(line, '', 1))
            assert not sup.tree_commands_exact(path, selector)
            path.write_text(good + line)
            assert not sup.tree_commands_exact(path, selector)
            checks += 2
        for bad in (good + 'FAIL\n', good.replace('--- PASS:', '--- SKIP:', 1), good.replace('--- PASS:', '--- FAIL:', 1), good + '=== RUN   TestForeign\n', good.encode() + b'\xff'):
            path.write_bytes(bad if isinstance(bad, bytes) else bad.encode())
            assert not sup.tree_commands_exact(path, selector)
            checks += 1
        path.unlink()
        assert not sup.tree_commands_exact(path, selector)
        checks += 1
    binary, driver, minio = temp / 'candidate', temp / 'driver', temp / 'minio'
    for p in (binary, driver, minio):
        p.write_bytes(b'controlled-never-executed')
        p.chmod(0o700)
    adapter.MINIO = minio
    adapter.MINIO_SHA = hashlib.sha256(minio.read_bytes()).hexdigest()
    for selector in root_selectors:
        assert adapter.configuration(binary, selector, temp / 'fresh')['resources'] == 7
    for bad in (pg[1:], pg[:-1], pg + 'x', '^TestKnowledgeTreeCommandHTTP.*$', '^TestKnowledgeTreeCommandHTTPMutations$', pg.replace('Mutations|Authority', 'Authority|Mutations')):
        try:
            adapter.configuration(binary, bad, temp / 'fresh')
        except ValueError:
            checks += 1
        else:
            raise AssertionError('unapproved PG selector')
    for bad in (unknown[1:], unknown[:-1], unknown + 'x', unknown.replace('Unknown', '(Unknown)'), unknown + '/.*', unknown.replace('Unknown', 'Unknown|Authority')):
        try:
            adapter.configuration(binary, bad, temp / 'fresh')
        except ValueError:
            checks += 1
        else:
            raise AssertionError('unapproved Unknown selector')
    for selector in selectors:
        for mode in ('valid', 'missing', 'utf8', 'exit2'):
            trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0, 'resource': 0}
            owned = []
            class Child:
                pid = 434343
                returncode = None
                def __init__(self, args, stdout, stderr):
                    directory = Path(args[args.index('--directory') + 1])
                    directory.mkdir()
                    (directory / 'runtime').mkdir()
                    owned.append(directory)
                    raw = log(selector)
                    if mode == 'missing': raw = raw[raw.index('\n') + 1:]
                    raw += f'D03 explicit test actual_wait pid=123 code=0 selector={selector}\n'
                    stdout.write(raw)
                    stdout.flush()
                    if mode == 'utf8': stdout.buffer.write(b'\xff\n'); stdout.buffer.flush()
                def wait(self, timeout):
                    trace['wait'].append(timeout)
                    self.returncode = 2 if mode == 'exit2' else 0
                    return self.returncode
            def descendants(_):
                trace['desc'] += 1
                return set()
            def tcp():
                trace['tcp'] += 1
                return set()
            def reap(*_):
                trace['reap'] += 1
                raise ChildProcessError
            def absent(item, timeout):
                trace['resource'] += 1
                return True
            def record(directory):
                return {'resources': [{'kind': 'container', 'id': f'{i+1:064x}', 'nonce': 'a'*32} for i in range(7)], 'directories': [str(directory/'runtime'/str(i)) for i in range(3)]}
            fake_adapter = types.SimpleNamespace(TARGETS={pg: 'tests/knowledge', unknown: 'tests/knowledge'}, sha=adapter.sha, input_paths=lambda _: [driver, binary])
            output = temp / ('out-' + str(checks))
            args = ['probe', '--driver', str(driver), '--binary', str(binary), '--run', selector, '--output', str(output)]
            is_pg = selector in (pg, unknown)
            if is_pg: args.append('--root-chain')
            with patch.object(sys, 'argv', args), patch.object(sup, 'root_adapter', return_value=fake_adapter), patch.object(sup, 'root_record', record), patch.object(sup, 'exact_absent', absent), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', descendants), patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), contextlib.redirect_stdout(io.StringIO()):
                code = sup.main()
            expected = 0 if mode == 'valid' else 2 if mode == 'exit2' else 1
            assert code == expected
            assert trace == {'wait': [540 if is_pg else 123], 'desc': 3, 'tcp': 3, 'reap': 1, 'resource': 14 if is_pg else 0}, trace
            raw = next(output.glob('*.log')).read_bytes()
            for marker in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2', 'delta_empty_observation=1', 'delta_empty_observation=2', 'inputs_unchanged=True terminal=' + str(expected)):
                assert marker.encode() in raw
            checks += 1
print(f'PASS controls={checks}; selectors={selectors}; old three tools inverse unchanged; no child/socket/PG')
