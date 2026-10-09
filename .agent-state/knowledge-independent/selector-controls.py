#!/usr/bin/env python3
"""Actual two tools, configuration only and simulated owned-resource observation."""
import ast
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestKnowledgeB02Independent(Content|TreeReference)$'
TOPS = ['TestKnowledgeB02IndependentContent', 'TestKnowledgeB02IndependentTreeReference']
RECEIPT_TOP = 'TestKnowledgeB02IndependentTreeReference'
RECEIPT_SUB = 'revoked_persisted_public_receipt_identity_and_old_attachment'
RECEIPT_SELECTOR = '^' + RECEIPT_TOP + '$/^' + RECEIPT_SUB + '$'
BINARY = ROOT / 'output/ai/knowledge-independent/knowledge-independent-race.test'
RECEIPT_BINARY = ROOT / 'output/ai/knowledge-independent/knowledge-independent-receipt-fixed-race.test'
DRIVER = ROOT / '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


driver, supervisor = module('independent_driver', DRIVER), module('independent_supervisor', SUPERVISOR)
checks = []


def check(name, condition):
    assert condition, name
    checks.append(name)


receipt_read = r'''    if selector == '^TestKnowledgeB02IndependentTreeReference$/^revoked_persisted_public_receipt_identity_and_old_attachment$':
        try:
            output = log_path.read_text()
        except (OSError, UnicodeDecodeError):
            log.write('KNOWLEDGE independent_exact_receipt_sub=False log_unreadable=True\n')
            return False
    else:
        output = log_path.read_text()
'''
receipt_guard = r'''    if selector == '^TestKnowledgeB02IndependentTreeReference$/^revoked_persisted_public_receipt_identity_and_old_attachment$':
        top = 'TestKnowledgeB02IndependentTreeReference'
        names = [top, top + '/revoked_persisted_public_receipt_identity_and_old_attachment']
        runs = re.findall(r'^=== RUN   ([^\r\n]+)$', output, re.M)
        terminals = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): ([^\s]+)(?:[ \t]|$)', output, re.M)
        exact = sorted(runs) == sorted(names) and sorted(terminals) == sorted(('PASS', name) for name in names)
        good = good and exact
        log.write(f'KNOWLEDGE independent_exact_receipt_sub={exact}\n')
'''
projected = {}
for path, added in [
    (DRIVER, '    ' + repr(RECEIPT_SELECTOR) + ": 'tests/knowledge',\n"),
    (SUPERVISOR, '        ' + repr(RECEIPT_SELECTOR) + ': {' + repr(RECEIPT_TOP) + '},\n'),
]:
    current = path.read_text()
    check('one exact receipt mapping in ' + path.name, current.count(added) == 1)
    reduced = current.replace(added, '', 1)
    if path == SUPERVISOR:
        check('one receipt read boundary and one exact guard',
              reduced.count(receipt_read) == 1 and reduced.count(receipt_guard) == 1)
        reduced = reduced.replace(receipt_read, '    output = log_path.read_text()\n', 1).replace(receipt_guard, '', 1)
    before = subprocess.check_output(['git', 'show', '87898d82:' + str(path.relative_to(ROOT))], cwd=ROOT, text=True)
    check('receipt increment inverse equals original ' + path.name, reduced == before)
    projected[path] = reduced

for path, added in [
    (DRIVER, "    '^TestKnowledgeB02Independent(Content|TreeReference)$': 'tests/knowledge',\n"),
    (SUPERVISOR, "        '^TestKnowledgeB02Independent(Content|TreeReference)$': {'TestKnowledgeB02IndependentContent', 'TestKnowledgeB02IndependentTreeReference'},\n"),
]:
    before = subprocess.check_output(['git', 'show', '2578a9ef:' + str(path.relative_to(ROOT))], cwd=ROOT, text=True)
    current = projected[path]
    check('one exact mapping in ' + path.name, current.count(added) == 1)
    reduced = current.replace(added, '', 1)
    if path == SUPERVISOR:
        count_guard = '''    if selector == '^TestKnowledgeB02Independent(Content|TreeReference)$':
        count = len(re.findall(r'^=== RUN   (Test\\w+)$', output, re.M))
        good = good and count == 2
        log.write(f'KNOWLEDGE independent_exact_top_count={count == 2}\\n')
'''
        check('one new-selector-only cardinality guard', reduced.count(count_guard) == 1)
        reduced = reduced.replace(count_guard, '', 1)
    check('all other source bytes unchanged in ' + path.name, reduced == before)

syntax = ast.parse(SUPERVISOR.read_text())
observer = next(node for node in syntax.body if isinstance(node, ast.FunctionDef) and node.name == 'observe_root_chain')
assignment = next(node for node in observer.body if isinstance(node, ast.Assign)
                  and any(isinstance(target, ast.Name) and target.id == 'expected' for target in node.targets))
expected = ast.literal_eval(assignment.value.func.value)
check('all target tables agree', set(expected) == set(driver.TARGETS))
check('only the exact two business tops', expected[SELECTOR] == set(TOPS))
check('receipt singleton table', expected[RECEIPT_SELECTOR] == {RECEIPT_TOP})
check('original budgets', supervisor.budgets(True) == (540, 60) and supervisor.budgets(False) == (123, 3))

with tempfile.TemporaryDirectory(prefix='knowledge-independent-map-') as directory:
    parent = Path(directory)
    fresh = parent / 'not-created'
    plan = driver.configuration(BINARY, SELECTOR, fresh)
    check('actual exact configuration no runtime', plan['cwd'] == str(ROOT / 'tests/knowledge')
          and plan['resources'] == 7 and plan['test_timeout'] == '6m' and not fresh.exists())
    plan = driver.configuration(RECEIPT_BINARY, RECEIPT_SELECTOR, fresh)
    check('actual receipt configuration no runtime', plan['cwd'] == str(ROOT / 'tests/knowledge')
          and plan['resources'] == 7 and plan['test_timeout'] == '6m' and not fresh.exists())
    for selector in ['^TestKnowledgeB02IndependentContent$', '^TestKnowledgeB02Independent.*$',
                     '^TestKnowledgeB02Independent(TreeReference|Content)$', SELECTOR[:-1],
                     '^TestKnowledgeB02Independent(Content|TreeReference|Runtime)$']:
        try:
            driver.configuration(BINARY, selector, fresh)
        except ValueError:
            rejected = True
        else:
            rejected = False
        check('reject selector ' + selector, rejected and not fresh.exists())
    for selector in ['^' + RECEIPT_TOP + '$', '^' + RECEIPT_TOP + '$/^revoked_original_receipt$',
                     '^' + RECEIPT_TOP + '$/.*', RECEIPT_SELECTOR[:-1],
                     RECEIPT_SELECTOR + '|^TestKnowledgeB02IndependentContent$']:
        try:
            driver.configuration(RECEIPT_BINARY, selector, fresh)
        except ValueError:
            rejected = True
        else:
            rejected = False
        check('reject receipt selector ' + selector, rejected and not fresh.exists())

    runtime = parent / 'runtime'
    runtime.mkdir()
    resources = []
    for label, kinds, nonce in [
        ('agenteam.d05.objectfixture', ['container', 'network'], '1' * 32),
        ('agenteam.d04.networkfixture', ['container', 'network'], '2' * 32),
        ('agenteam.d03.fixture', ['container', 'container', 'network'], '3' * 32),
    ]:
        for kind in kinds:
            resources.append({'kind': kind, 'id': f'{len(resources)+1:064x}', 'label': label, 'nonce': nonce})
    record = {'kind': 'work-owner-root-chain', 'resources': resources,
              'directories': [str(runtime / name) for name in ['objects', 'outbound', 'postgres']]}
    owned = parent / 'owned.json'
    owned.write_text(json.dumps(record))
    owned.chmod(0o600)
    wait_line = 'D03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n'
    for name, tops, waited, accept in [
        ('exact', TOPS, wait_line, True),
        ('missing', TOPS[:1], wait_line, False),
        ('extra', TOPS + ['TestKnowledgeB02Runtime'], wait_line, False),
        ('duplicate', TOPS + TOPS[:1], wait_line, False),
        ('no actual Wait', TOPS, '', False),
        ('foreign selector Wait', TOPS, wait_line.replace(SELECTOR, '^TestKnowledgeB02Runtime$'), False),
    ]:
        log_path = parent / 'observer.log'
        log_path.write_text(''.join('=== RUN   ' + top + '\n' for top in tops) + waited)
        output = io.StringIO()
        with patch.object(supervisor, 'exact_absent', return_value=True) as absent, \
             patch.object(supervisor.time, 'sleep', return_value=None):
            result = supervisor.observe_root_chain(parent, output, log_path, SELECTOR)
        check('observer ' + name, result is accept)
        check('all fourteen resource observations ' + name, absent.call_count == 14)
        text = output.getvalue()
        check('private/runtime double tail ' + name,
              all(f'ROOT private_observation={n} absent=True' in text and
                  f'ROOT runtime_observation={n} empty=True' in text for n in (1, 2)))

    child = RECEIPT_TOP + '/' + RECEIPT_SUB
    exact_runs = '=== RUN   ' + RECEIPT_TOP + '\n=== RUN   ' + child + '\n'
    exact_pass = '--- PASS: ' + RECEIPT_TOP + ' (0.50s)\n    --- PASS: ' + child + ' (0.40s)\n'
    exact_wait = 'D03 explicit test actual_wait pid=123 code=0 selector=' + RECEIPT_SELECTOR + '\n'
    exact = exact_runs + exact_pass + exact_wait
    variants = [
        ('exact receipt', exact, None, True),
        ('empty parent', '=== RUN   ' + RECEIPT_TOP + '\n--- PASS: ' + RECEIPT_TOP + ' (0.01s)\n' + exact_wait, None, False),
        ('wrong child', exact.replace(RECEIPT_SUB, 'wrong_child'), None, False),
        ('extra child', exact + '=== RUN   ' + RECEIPT_TOP + '/other\n    --- PASS: ' + RECEIPT_TOP + '/other (0.01s)\n', None, False),
        ('extra parent', exact + '=== RUN   TestKnowledgeB02IndependentContent\n--- PASS: TestKnowledgeB02IndependentContent (0.01s)\n', None, False),
        ('duplicate child RUN', exact + '=== RUN   ' + child + '\n', None, False),
        ('duplicate parent RUN', exact + '=== RUN   ' + RECEIPT_TOP + '\n', None, False),
        ('duplicate child PASS', exact + '    --- PASS: ' + child + ' (0.01s)\n', None, False),
        ('missing child PASS', exact.replace('    --- PASS: ' + child + ' (0.40s)\n', ''), None, False),
        ('missing parent PASS', exact.replace('--- PASS: ' + RECEIPT_TOP + ' (0.50s)\n', ''), None, False),
        ('child SKIP', exact.replace('    --- PASS:', '    --- SKIP:'), None, False),
        ('child FAIL', exact.replace('    --- PASS:', '    --- FAIL:'), None, False),
        ('late duplicate FAIL', exact + '    --- FAIL: ' + child + ' (0.01s)\n', None, False),
        ('no actual Wait', exact.replace(exact_wait, ''), None, False),
        ('Wait zero PID', exact.replace('pid=123', 'pid=0'), None, False),
        ('foreign selector Wait', exact.replace(exact_wait, wait_line), None, False),
        ('invalid UTF8', b'\xff', None, False),
        ('log OSError', exact, OSError('owned log read unavailable'), False),
    ]
    for name, payload, read_error, accept in variants:
        log_path = parent / 'receipt-observer.log'
        log_path.write_bytes(payload if isinstance(payload, bytes) else payload.encode())
        output = io.StringIO()
        original_read = Path.read_text
        def read(path, *args, **kwargs):
            if path == log_path and read_error is not None:
                raise read_error
            return original_read(path, *args, **kwargs)
        with patch.object(supervisor, 'exact_absent', return_value=True) as absent, \
             patch.object(supervisor.time, 'sleep', return_value=None), \
             patch.object(Path, 'read_text', read):
            result = supervisor.observe_root_chain(parent, output, log_path, RECEIPT_SELECTOR)
        check('receipt observer ' + name, result is accept)
        check('receipt fourteen resources ' + name, absent.call_count == 14)
        text = output.getvalue()
        check('receipt double private/runtime ' + name,
              all(f'ROOT private_observation={n} absent=True' in text and
                  f'ROOT runtime_observation={n} empty=True' in text for n in (1, 2)))

    for name, payload, read_error, want in [
        ('exact', exact, None, 0),
        ('invalid UTF8', b'\xff', None, 1),
        ('log OSError', exact, OSError('owned log read unavailable'), 1),
    ]:
        output_dir = parent / ('main-' + name.replace(' ', '-'))
        waited = []
        class Child:
            pid, returncode = 321, None
            def __init__(self, args, stdout, stderr):
                assert args[args.index('--run') + 1] == RECEIPT_SELECTOR
                directory = Path(args[args.index('--directory') + 1])
                directory.mkdir()
                runtime = directory / 'runtime'
                runtime.mkdir()
                owned = directory / 'owned.json'
                owned.write_text(json.dumps({**record, 'directories': [str(runtime / p) for p in ('objects', 'outbound', 'postgres')]}))
                owned.chmod(0o600)
                if isinstance(payload, bytes):
                    stdout.flush()
                    __import__('os').write(stdout.fileno(), payload)
                else:
                    stdout.write(payload)
            def wait(self, timeout):
                waited.append(timeout)
                self.returncode = 0
                return 0
        def read(path, *args, **kwargs):
            if path.parent == output_dir and path.suffix == '.log' and read_error is not None:
                raise read_error
            return original_read(path, *args, **kwargs)
        def sha(path):
            return hashlib.sha256(Path(path).read_bytes()).hexdigest()
        adapter = SimpleNamespace(TARGETS={RECEIPT_SELECTOR: 'tests/knowledge'},
                                  input_paths=lambda binary: [DRIVER, SUPERVISOR], sha=sha)
        argv = ['supervisor', '--root-chain', '--driver', str(DRIVER), '--binary', str(RECEIPT_BINARY),
                '--run', RECEIPT_SELECTOR, '--output', str(output_dir)]
        with patch.object(sys, 'argv', argv), \
             patch.object(supervisor, 'root_adapter', return_value=adapter), \
             patch.object(supervisor.ctypes, 'CDLL', return_value=SimpleNamespace(prctl=lambda *args: 0)), \
             patch.object(supervisor.subprocess, 'Popen', Child), \
             patch.object(supervisor, 'descendants', return_value=set()) as descendants, \
             patch.object(supervisor.os, 'waitpid', side_effect=ChildProcessError), \
             patch.object(supervisor, 'tcp', return_value=set()) as tcp, \
             patch.object(supervisor, 'exact_absent', return_value=True) as absent, \
             patch.object(supervisor.time, 'sleep', return_value=None), \
             patch.object(Path, 'read_text', read), contextlib.redirect_stdout(io.StringIO()):
            result = supervisor.main()
        text = next(output_dir.glob('*.log')).read_bytes().decode('utf-8', errors='replace')
        check('main receipt return ' + name, result == want)
        check('main original Wait budget ' + name, waited == [540])
        check('main all resources and owned descendants ' + name, absent.call_count == 14 and descendants.call_count == 3)
        check('main baseline and two TCP tails ' + name, tcp.call_count == 3 and
              all(f'HOST_TCP delta_empty_observation={n}' in text for n in (1, 2)))
        check('main private/runtime and input terminal ' + name,
              all(f'ROOT private_observation={n} absent=True' in text and
                  f'ROOT runtime_observation={n} empty=True' in text and
                  f'OWNED runtime_observation={n} descendants=[]' in text for n in (1, 2)) and
              f'SUPERVISOR inputs_unchanged=True terminal={want}' in text and
              'SUPERVISOR actual_driver_wait pid=321 actual=True code=0' in text)

print(json.dumps({'passed': len(checks), 'controls': checks, 'resources_started': False,
                  'socket': False, 'network': False}))
