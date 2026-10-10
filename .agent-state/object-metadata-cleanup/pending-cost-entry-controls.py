#!/usr/bin/env python3
"""Check the exact two-case recovery entry without starting Go or resources."""
import ast
import json
from pathlib import Path
import subprocess
import tempfile
import types

ROOT = Path(__file__).resolve().parents[2]
BASE = 'e3cbe882'
SELECTOR = '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|PendingHistoryAndCausePlans)$'
PREVIOUS = '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans)$'
NAMES = ['TestObjectMetadataCleanupLiveTransferAndDownloadPlans', 'TestObjectMetadataCleanupPendingHistoryAndCausePlans']
PATHS = ['.agent-state/work-owner-http/root_chain_driver.py', '.agent-state/task-planning-recovery/pg_only_supervisor.py']
OUTPUT = ROOT / 'output/ai/object-metadata-cleanup/pending-entry-controls'
OUTPUT.mkdir(parents=True, exist_ok=True)
modules, originals = [], []
for index, path in enumerate(PATHS):
    source = (ROOT / path).read_text()
    old = subprocess.check_output(['git', 'show', BASE + ':' + path], cwd=ROOT, text=True)
    lines = source.splitlines(keepends=True)
    added = [line for line in lines if SELECTOR in line and line.lstrip().startswith("'^Test")]
    assert len(added) == 1
    inverse = source.replace(added[0], '')
    if index == 1:
        current_condition = f"if args.run in ({PREVIOUS!r}, {SELECTOR!r}):"
        assert inverse.count(current_condition) == 1
        inverse = inverse.replace(current_condition, f"if args.run == {PREVIOUS!r}:")
        conditions = [node.test for node in ast.walk(ast.parse(source)) if isinstance(node, ast.If)
                      and isinstance(node.test, ast.Compare)
                      and any(isinstance(child, ast.Constant) and child.value == SELECTOR for child in ast.walk(node.test))]
        assert len(conditions) == 1
        condition = compile(ast.Expression(conditions[0]), path, 'eval')
        for selector, expected in ((SELECTOR, True), (PREVIOUS, True), ('^TestObjectMetadataCleanupOldAttemptsAndStopHistory$', False)):
            assert eval(condition, {'args': types.SimpleNamespace(run=selector)}) == expected
    assert inverse == old, 'existing entry behavior changed beyond the exact mapping'
    scopes = []
    for value in (source, old):
        scope = {'__file__': str(ROOT / path), '__name__': 'pending_entry_control'}
        exec(compile(value, str(ROOT / path), 'exec'), scope)
        scopes.append(scope)
    modules.append(scopes[0])
    originals.append(scopes[1])
driver, supervisor = modules
old_driver, old_supervisor = originals
assert supervisor['budgets'](True) == old_supervisor['budgets'](True) == (540, 60)
assert driver['metadata_remaining_cost_inputs']() == old_driver['metadata_remaining_cost_inputs']()
binary = ROOT / 'output/ai/object-metadata-cleanup/metadata-cleanup-restored-race.test'
# The old executable is only a configuration input. It is never executed and
# is not presented as a compiled candidate for the repaired SQL.
assert driver['input_paths'](binary) == old_driver['input_paths'](binary)
with tempfile.TemporaryDirectory(prefix='exact-', dir=OUTPUT) as temporary:
    base = Path(temporary)
    fresh = base / 'fresh'
    config = driver['configuration'](binary, SELECTOR, fresh)
    assert config['resources'] == 7 and config['test_timeout'] == '6m' and config['cwd'] == str(ROOT / 'tests/objects')
    for selector in old_driver['TARGETS']:
        assert driver['configuration'](binary, selector, fresh) == old_driver['configuration'](binary, selector, fresh)
    for selector in (SELECTOR[1:], SELECTOR[:-1], '^TestObjectMetadataCleanup.*$', SELECTOR + '|^TestExtra$'):
        try:
            driver['configuration'](binary, selector, fresh)
        except ValueError:
            pass
        else:
            raise AssertionError('nonexact selector accepted')
    supervisor['time'] = types.SimpleNamespace(monotonic=lambda: 0, sleep=lambda _: None)
    for label, names, waited, resource_left, private_left, expected in (
        ('normal', NAMES, True, False, False, True),
        ('reordered', NAMES[::-1], True, False, False, True),
        ('missing', NAMES[:1], True, False, False, False),
        ('extra', NAMES + ['TestExtra'], True, False, False, False),
        ('missing-wait', NAMES, False, False, False, False),
        ('resource-left', NAMES, True, True, False, False),
        ('private-left', NAMES, True, False, True, False),
    ):
        directory = base / label
        runtime = directory / 'runtime'
        runtime.mkdir(parents=True)
        resources = []
        for group, (tag, kinds) in enumerate((('agenteam.d05.objectfixture', ('container', 'network')),
                                            ('agenteam.d04.networkfixture', ('container', 'network')),
                                            ('agenteam.d03.fixture', ('container', 'container', 'network'))), 1):
            for kind in kinds:
                resources.append({'kind': kind, 'id': format(len(resources) + 1, '064x'), 'label': tag, 'nonce': format(group, '032x')})
        directories = [str(runtime / name) for name in ('object', 'outbound', 'pg')]
        manifest = directory / 'owned.json'
        manifest.write_text(json.dumps({'kind': 'work-owner-root-chain', 'resources': resources, 'directories': directories}))
        manifest.chmod(0o600)
        if private_left:
            Path(directories[0]).mkdir()
        calls = []
        def absent(item, timeout):
            calls.append(item['id'])
            return not resource_left
        supervisor['exact_absent'] = absent
        log_path = directory / 'test.log'
        with log_path.open('w+') as log:
            for name in names:
                log.write('=== RUN   ' + name + '\n')
            if waited:
                log.write('D03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n')
            assert supervisor['observe_root_chain'](directory, log, log_path, SELECTOR) == expected, label
        assert len(calls) == 14 and all(calls.count(item['id']) == 2 for item in resources)
        output = log_path.read_text()
        assert output.count('ROOT private_observation=') == output.count('ROOT runtime_observation=') == 2
print('PASS: exact two-case mapping; unchanged 12 original configurations/input closures/budgets; original seven-resource observer controls; no Go, Docker, socket or product run')
