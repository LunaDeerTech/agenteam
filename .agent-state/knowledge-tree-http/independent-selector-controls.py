#!/usr/bin/env python3
"""One independent receipt-owner entrance; no resources or child test body."""
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
BASE = 'fa7fcc6fab8b7ed16ed9f4edeac9f0d3462c0d94'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
TOP = 'TestKnowledgeTreeCommandHTTPIndependentReceiptOwner'
SELECTOR = '^' + TOP + '$'
TEST = ROOT / 'tests/knowledge/owner_tree_commands_independent_test.go'


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def original(path):
    return subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT,
                          check=True, capture_output=True, text=True).stdout


def remove_once(source, piece):
    assert source.count(piece) == 1
    return source.replace(piece, '', 1)


source = (ROOT / SUP).read_text()
ast.parse(source)
for line in (
    "TREE_COMMAND_INDEPENDENT = '" + SELECTOR + "'\n",
    "TREE_COMMAND_GROUPS[TREE_COMMAND_INDEPENDENT] = {'" + TOP + "': ()}\n",
    '        TREE_COMMAND_INDEPENDENT: set(TREE_COMMAND_GROUPS[TREE_COMMAND_INDEPENDENT]),\n',
):
    source = remove_once(source, line)
assert source.count('TREE_COMMAND_RECHECK, TREE_COMMAND_INDEPENDENT)') == 1
source = source.replace('TREE_COMMAND_RECHECK, TREE_COMMAND_INDEPENDENT)', 'TREE_COMMAND_RECHECK)', 1)
assert source == original(SUP)
assert remove_once((ROOT / DRIVER).read_text(), "    '" + SELECTOR + "': 'tests/knowledge',\n") == original(DRIVER)
sup, driver = module(SUP, 'independent_supervisor'), module(DRIVER, 'independent_driver')
assert sup.TREE_COMMAND_GROUPS[SELECTOR] == {TOP: ()}
assert sup.budgets(True) == (540, 60) and sup.budgets(False) == (123, 3)
assert re.findall(r'^func (Test\w+)\(t \*testing.T\)', TEST.read_text(), re.M) == [TOP]
assert 't.Run(' not in TEST.read_text() and 't.Parallel(' not in TEST.read_text()
assert TEST in sup.tree_command_inputs()
assert all(p.is_file() and not p.is_symlink() for p in sup.tree_command_inputs())

GOOD = f'=== RUN   {TOP}\n--- PASS: {TOP} (0.01s)\n'
checks = 0
with tempfile.TemporaryDirectory(prefix='knowledge-independent-selector-') as name:
    temp = Path(name)
    log_path = temp / 'log'
    log_path.write_text(GOOD)
    assert sup.tree_commands_exact(log_path, SELECTOR)
    checks += 1
    bad_logs = [
        '', GOOD.splitlines(keepends=True)[0], GOOD.splitlines(keepends=True)[1],
        GOOD + GOOD, GOOD + GOOD.splitlines(keepends=True)[0],
        GOOD + GOOD.splitlines(keepends=True)[1], GOOD + 'FAIL\n',
        GOOD.replace('--- PASS:', '--- SKIP:'), GOOD.replace('--- PASS:', '--- FAIL:'),
        GOOD + f'=== RUN   {TOP}/unexpected\n', GOOD + '=== RUN   TestForeign\n',
        GOOD.encode() + b'\xff',
    ]
    for raw in bad_logs:
        log_path.write_bytes(raw if isinstance(raw, bytes) else raw.encode())
        assert not sup.tree_commands_exact(log_path, SELECTOR)
        checks += 1
    log_path.unlink()
    assert not sup.tree_commands_exact(log_path, SELECTOR)
    checks += 1

    binary, executable, minio = (temp / n for n in ('candidate', 'driver', 'minio'))
    for p in (binary, executable, minio):
        p.write_bytes(b'controlled-never-executed')
        p.chmod(0o700)
    with patch.object(driver, 'MINIO', minio), patch.object(driver, 'MINIO_SHA', hashlib.sha256(minio.read_bytes()).hexdigest()):
        plan = driver.configuration(binary, SELECTOR, temp / 'fresh')
        assert plan['resources'] == 7 and plan['test_timeout'] == '6m'
        assert plan['cwd'] == str(ROOT / 'tests/knowledge') and plan['selector'] == SELECTOR
        checks += 1
        for wrong in (SELECTOR[1:], SELECTOR[:-1], SELECTOR + 'x', SELECTOR + '/.*',
                      SELECTOR.replace('IndependentReceiptOwner', 'Independent.*'),
                      SELECTOR.replace('IndependentReceiptOwner', '(IndependentReceiptOwner)')):
            try:
                driver.configuration(binary, wrong, temp / 'fresh')
            except ValueError:
                checks += 1
            else:
                raise AssertionError('nonliteral independent selector accepted')

    # Exercise the actual main. Only OS/process/resource boundaries are doubles;
    # observer failure must still traverse all original cleanup observations.
    for mode in ('valid', 'missing_pass', 'utf8', 'read_error', 'exit2'):
        trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0, 'resource': 0}

        class Child:
            pid = 434343
            returncode = None

            def __init__(self, args, stdout, stderr):
                directory = Path(args[args.index('--directory') + 1])
                directory.mkdir()
                (directory / 'runtime').mkdir()
                raw = GOOD if mode != 'missing_pass' else GOOD.splitlines(keepends=True)[0]
                stdout.write(raw + f'D03 explicit test actual_wait pid=123 code=0 selector={SELECTOR}\n')
                stdout.flush()
                if mode == 'utf8':
                    stdout.buffer.write(b'\xff\n')
                    stdout.buffer.flush()

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
            return {'resources': [{'kind': 'container', 'id': f'{i+1:064x}', 'nonce': 'a'*32} for i in range(7)],
                    'directories': [str(directory / 'runtime' / str(i)) for i in range(3)]}

        original_read = Path.read_text

        def read(path, *args, **kwargs):
            if mode == 'read_error' and path.suffix == '.log':
                raise OSError('controlled log read failure')
            return original_read(path, *args, **kwargs)

        adapter = types.SimpleNamespace(TARGETS=driver.TARGETS, sha=driver.sha,
                                        input_paths=lambda _: [executable, binary])
        output = temp / ('output-' + mode)
        args = ['probe', '--root-chain', '--driver', str(executable), '--binary', str(binary),
                '--run', SELECTOR, '--output', str(output)]
        with patch.object(sys, 'argv', args), patch.object(sup, 'root_adapter', return_value=adapter), \
                patch.object(sup, 'root_record', record), patch.object(sup, 'exact_absent', absent), \
                patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), \
                patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', descendants), \
                patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), \
                patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), \
                patch.object(Path, 'read_text', read), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        expected = 0 if mode == 'valid' else 2 if mode == 'exit2' else 1
        assert code == expected, (mode, code, expected)
        assert trace == {'wait': [540], 'desc': 3, 'tcp': 3, 'reap': 1, 'resource': 14}, trace
        tail = next(output.glob('*.log')).read_bytes()
        for marker in ('actual_driver_wait', 'private_observation=1', 'private_observation=2',
                       'runtime_observation=1', 'runtime_observation=2',
                       'OWNED runtime_observation=1 descendants=[]', 'OWNED runtime_observation=2 descendants=[]',
                       'delta_empty_observation=1', 'delta_empty_observation=2',
                       'inputs_unchanged=True terminal=' + str(expected)):
            assert marker.encode() in tail, marker
        checks += 1

assert sys.argv[1:] in ([], ['--artifacts'])
if sys.argv[1:]:
    binary = ROOT / 'output/ai/knowledge-tree-http/independent/knowledge-tree-commands-independent-race-01.test'
    with tempfile.TemporaryDirectory(prefix='knowledge-independent-config-') as directory:
        plan = driver.configuration(binary, SELECTOR, Path(directory) / 'fresh')
        assert plan['resources'] == 7 and plan['selector'] == SELECTOR
    result = subprocess.run([str(binary), '-test.list', SELECTOR], cwd=ROOT,
                            check=True, capture_output=True, text=True)
    assert result.stdout.splitlines() == [TOP]
    checks += 2
print(f'PASS {checks} controls; 5 actual-main boundary doubles; inverse tools exact; no PG/socket/test body')
