#!/usr/bin/env python3
"""Actual two tools, configuration only and simulated owned-resource observation."""
import ast
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestKnowledgeB02Independent(Content|TreeReference)$'
TOPS = ['TestKnowledgeB02IndependentContent', 'TestKnowledgeB02IndependentTreeReference']
BINARY = ROOT / 'output/ai/knowledge-independent/knowledge-independent-race.test'
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


for path, added in [
    (DRIVER, "    '^TestKnowledgeB02Independent(Content|TreeReference)$': 'tests/knowledge',\n"),
    (SUPERVISOR, "        '^TestKnowledgeB02Independent(Content|TreeReference)$': {'TestKnowledgeB02IndependentContent', 'TestKnowledgeB02IndependentTreeReference'},\n"),
]:
    before = subprocess.check_output(['git', 'show', '2578a9ef:' + str(path.relative_to(ROOT))], cwd=ROOT, text=True)
    current = path.read_text()
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
check('original budgets', supervisor.budgets(True) == (540, 60) and supervisor.budgets(False) == (123, 3))

with tempfile.TemporaryDirectory(prefix='knowledge-independent-map-') as directory:
    parent = Path(directory)
    fresh = parent / 'not-created'
    plan = driver.configuration(BINARY, SELECTOR, fresh)
    check('actual exact configuration no runtime', plan['cwd'] == str(ROOT / 'tests/knowledge')
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

print(json.dumps({'passed': len(checks), 'controls': checks, 'resources_started': False,
                  'socket': False, 'network': False}))
