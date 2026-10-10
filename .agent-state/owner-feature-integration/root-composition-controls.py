#!/usr/bin/env python3
"""Exact single-root entry controls; explicit OS/resource doubles, no sockets."""
import ast
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestKnowledgeSkillsDefaultRootComposition$'
TOP = SELECTOR[1:-1]
def load(path):
    spec = importlib.util.spec_from_file_location(Path(path).stem, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
union = load('.agent-state/owner-feature-integration/entry_union.py')
sup, driver = load(union.SUP), load(union.DRIVER)
checks = 0
def check(value, label):
    global checks
    assert value, label
    checks += 1

# Independently remove only the exact new namespace from the actual source.
class Previous(ast.NodeTransformer):
    def visit_FunctionDef(self, node):
        if node.name in ('root_composition_inputs', 'root_composition_results', 'root_composition_same'):
            return None
        return self.generic_visit(node)
    def visit_If(self, node):
        if any(isinstance(item, ast.Constant) and item.value == SELECTOR for item in ast.walk(node.test)):
            assert not node.orelse
            return None
        return self.generic_visit(node)
    def visit_Dict(self, node):
        kept = [(key, value) for key, value in zip(node.keys, node.values)
                if not (isinstance(key, ast.Constant) and key.value == SELECTOR)]
        node.keys, node.values = [item[0] for item in kept], [item[1] for item in kept]
        return self.generic_visit(node)
for path in union.PATHS['root_composition']:
    check(ast.dump(Previous().visit(ast.parse((ROOT / path).read_text())), include_attributes=False)
          == ast.dump(ast.parse(union.baseline_for(path, 'root_composition')), include_attributes=False),
          'entire prior six-domain source preserved')
check(sup.budgets(True) == (540, 60) and sup.budgets(False) == (123, 3), 'original budgets')
base_log = f'=== RUN   {TOP}\n--- PASS: {TOP} (1.00s)\nD03 explicit test actual_wait pid=123 code=0 selector={SELECTOR}\n'
cases = {'pass': base_log, 'duplicate-run': base_log + f'=== RUN   {TOP}\n',
         'duplicate-pass': base_log + f'--- PASS: {TOP} (1.00s)\n',
         'missing-run': base_log.replace(f'=== RUN   {TOP}\n', ''),
         'missing-pass': base_log.replace(f'--- PASS: {TOP} (1.00s)\n', ''),
         'skip': base_log.replace('--- PASS:', '--- SKIP:'),
         'fail': base_log.replace('--- PASS:', '--- FAIL:'),
         'child': base_log + f'=== RUN   {TOP}/child\n--- PASS: {TOP}/child (0.1s)\n',
         'extra-top': base_log + '=== RUN   TestOther\n--- PASS: TestOther (0.1s)\n',
         'no-wait': base_log.split('D03 explicit')[0],
         'bad-wait': base_log.replace('code=0', 'code=1'),
         'duplicate-wait': base_log + base_log.splitlines(True)[-1],
         'global-fail': base_log + 'FAIL\n'}
for label, raw in cases.items():
    check(sup.root_composition_results(raw) == (label == 'pass'), label)
check(set(driver.root_composition_inputs()) == set((ROOT / 'internal/central/app').glob('*.go')),
      'all same-package source and fixture inputs')
with tempfile.TemporaryDirectory(prefix='root-entry-', dir=ROOT / 'output/ai/owner-feature-integration') as temporary:
    base = Path(temporary)
    binary = base / 'never-executed-double'; binary.write_text('controlled'); binary.chmod(0o700)
    with patch.object(driver, 'MINIO', binary), patch.object(driver, 'MINIO_SHA', driver.sha(binary)):
        plan = driver.configuration(binary, SELECTOR, base / 'fresh')
        check((plan['cwd'], plan['resources'], plan['test_timeout']) == (str(ROOT / 'internal/central/app'), 7, '6m'), 'fixed actual root configuration')
        for selector in (SELECTOR[1:], SELECTOR[:-1], SELECTOR + '/child', '^TestKnowledgeSkills.*$', SELECTOR + '|TestOther'):
            try: driver.configuration(binary, selector, base / 'fresh')
            except ValueError: check(True, 'nonexact rejected')
            else: raise AssertionError('nonexact accepted')
    # Initial input closure is exercised from the real public main AST.
    main = next(node for node in ast.parse((ROOT / union.SUP).read_text()).body if isinstance(node, ast.FunctionDef) and node.name == 'main')
    freeze = next(node for node in main.body if isinstance(node, ast.If) and isinstance(node.test, ast.Compare)
                  and any(isinstance(item, ast.Attribute) and item.attr == 'root_composition_inputs' for item in ast.walk(node)))
    scope = {'args': types.SimpleNamespace(run=SELECTOR), 'adapter': driver, 'inputs': {}}
    exec(compile(ast.Module(body=[freeze], type_ignores=[]), union.SUP, 'exec'), scope)
    check(scope['inputs'] == {str(p): driver.sha(p) for p in driver.root_composition_inputs()}, 'actual initial app source freeze')
    # Real same-input method, tiny declared input stand-in, mutation/addition fail.
    args = types.SimpleNamespace(binary=binary)
    with patch.object(driver, 'input_paths', return_value=[binary]), patch.object(driver, 'root_composition_inputs', return_value=[]):
        frozen = {str(binary): driver.sha(binary)}
        check(sup.root_composition_same(frozen, args, driver), 'stable input')
        binary.write_text('changed')
        check(not sup.root_composition_same(frozen, args, driver), 'mutated input')
        binary.write_text('controlled')
        with patch.object(driver, 'root_composition_inputs', return_value=[base / 'new.go']):
            check(not sup.root_composition_same(frozen, args, driver), 'closure addition')
    argv = ['review', '--driver', str(ROOT / union.DRIVER), '--binary', str(binary), '--run', SELECTOR, '--output', str(base / 'uncreated')]
    with patch.object(sys, 'argv', argv), patch.object(Path, 'mkdir', side_effect=AssertionError('resource setup')), contextlib.redirect_stderr(io.StringIO()):
        try: sup.main()
        except SystemExit as stopped: check(stopped.code == 2, 'nonroot mode rejects before setup')
        else: raise AssertionError('nonroot mode accepted')
    # Exercise original seven-resource observer, including both retirement rounds.
    directory = base / 'owned'; runtime = directory / 'runtime'; runtime.mkdir(parents=True)
    resources = []
    for group, (label, kinds) in enumerate((('agenteam.d05.objectfixture', ('container', 'network')), ('agenteam.d04.networkfixture', ('container', 'network')), ('agenteam.d03.fixture', ('container', 'container', 'network'))), 1):
        for kind in kinds:
            resources.append(dict(kind=kind, id=format(len(resources)+1,'064x'), label=label, nonce=format(group,'032x')))
    manifest = directory / 'owned.json'
    manifest.write_text(json.dumps(dict(kind='work-owner-root-chain',resources=resources,directories=[str(runtime/name) for name in ('object','outbound','pg')]))); manifest.chmod(0o600)
    log_path = directory / 'test.log'
    for raw, live, residue, accepted in ((base_log,False,False,True),(cases['bad-wait'],False,False,False),(base_log,True,False,False),(base_log,False,True,False)):
        if residue:(runtime/'residue').write_text('controlled')
        observed=[]
        def absent(item, timeout):observed.append(item['id']);return not live
        with log_path.open('w+') as log, patch.object(sup,'exact_absent',absent), patch.object(sup.time,'sleep'):
            log.write(raw);log.flush()
            check(sup.observe_root_chain(directory,log,log_path,SELECTOR)==accepted,'original observer result')
        check(len(observed)==14 and all(observed.count(item['id'])==2 for item in resources),'seven resources twice')
        raw_tail=log_path.read_text()
        check(raw_tail.count('ROOT runtime_observation=')==raw_tail.count('ROOT private_observation=')==2,'private/runtime twice')
print(json.dumps(dict(checks=checks,exit=0,scope='single root exact entry and original tails; offline doubles only')))
