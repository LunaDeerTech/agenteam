#!/usr/bin/env python3
"""Check the actual combined D05 entries; no Go, socket or resource is started."""
import ast
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
helper = ROOT / '.agent-state/owner-feature-integration/entry_union.py'
spec = importlib.util.spec_from_file_location('owner_entry_union', helper)
union = importlib.util.module_from_spec(spec)
spec.loader.exec_module(union)
union.check_actual_union()
checks = 0


def check(value, label):
    global checks
    assert value, label
    checks += 1


def load(path):
    namespace = {'__file__': str(ROOT / path), '__name__': 'entry_union_control'}
    exec(compile((ROOT / path).read_text(), path, 'exec'), namespace)
    return namespace


driver, sup = load(union.DRIVER), load(union.SUP)
cases = {
    '^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$':
        ('BoundedHistoryAndFinalTransaction', 'FinalCommitUnknown'),
    '^TestObjectMetadataCleanupIndexMigration$': ('IndexMigration',),
    '^TestObjectMetadataCleanupOldAttemptsAndStopHistory$': ('OldAttemptsAndStopHistory',),
    '^TestObjectMetadataCleanup(ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans)$':
        ('ProjectHistoryPlans', 'SkillsIndexPlans', 'TransferAndForeignKeyPlans'),
    '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans)$':
        ('LiveTransferAndDownloadPlans', 'FinalAnchorForeignKeyPlans', 'PendingHistoryAndCausePlans'),
    '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|PendingHistoryAndCausePlans)$':
        ('LiveTransferAndDownloadPlans', 'PendingHistoryAndCausePlans'),
}

# Reverse only this domain from the actual four-file union; preserve every
# existing Skills HTTP/Cleanup selector, branch, input and all original tails.
for path in union.PATHS['d05']:
    inverse = (ROOT / path).read_text()
    for selector in cases:
        lines = [line for line in inverse.splitlines(keepends=True)
                 if selector in line and line.lstrip().startswith("'^Test")]
        check(len(lines) == 1, 'one exact mapping')
        inverse = inverse.replace(lines[0], '')
    if path == union.DRIVER:
        start, end = inverse.index('def metadata_cost_inputs():'), inverse.index('def configuration(')
        inverse = inverse[:start] + inverse[end:]
    else:
        start = inverse.index("        if args.run == '^TestObjectMetadataCleanup(ProjectHistoryPlans")
        end = inverse.index('    skill_selected = ', start)
        inverse = inverse[:start] + inverse[end:]
    check(inverse == union.baseline_for(path, 'd05'), 'other domains and entire original source preserved')

check(sup['budgets'](False) == (123, 3) and sup['budgets'](True) == (540, 60), 'fixed budgets')
check('time.monotonic() + 75' in (ROOT / union.SUP).read_text(), 'original TCP budget')
original_read = Path.read_text
def unknown_change(path, *args, **kwargs):
    value = original_read(path, *args, **kwargs)
    return value.replace('return (540, 60)', 'return (541, 60)') if path == ROOT / union.SUP else value
with patch.object(Path, 'read_text', unknown_change):
    try:
        union.check_actual_union()
    except AssertionError:
        check(True, 'unknown budget mutation rejected')
    else:
        raise AssertionError('unknown source was accepted')

output = ROOT / 'output/ai/owner-feature-integration'
output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='entry-', dir=output) as temporary:
    base = Path(temporary)
    binary = base / 'explicit-config-double'
    binary.write_text('never executed\n')
    binary.chmod(0o700)
    driver['MINIO'], driver['MINIO_SHA'] = binary, driver['sha'](binary)
    sup['time'] = types.SimpleNamespace(monotonic=lambda: 0, sleep=lambda _: None)
    for number, (selector, suffixes) in enumerate(cases.items()):
        names = ['TestObjectMetadataCleanup' + name for name in suffixes]
        config = driver['configuration'](binary, selector, base / 'fresh')
        check((config['resources'], config['test_timeout'], config['cwd']) ==
              (7, '6m', str(ROOT / 'tests/objects')), 'exact D05 configuration')
        for bad in (selector[1:], selector[:-1], selector + '|^TestOther$',
                    '^TestObjectMetadataCleanup.*$', selector + '/child'):
            try:
                driver['configuration'](binary, bad, base / 'fresh')
            except ValueError:
                check(True, 'nonexact selector rejected')
            else:
                raise AssertionError('nonexact selector accepted')
        for label, actual, waited, live, residue, expected in (
            ('normal', names, True, False, False, True),
            ('reorder', names[::-1], True, False, False, True),
            ('missing', names[:-1], True, False, False, False),
            ('extra', names + ['TestOther'], True, False, False, False),
            ('wait', names, False, False, False, False),
            ('resource', names, True, True, False, False),
            ('private', names, True, False, True, False),
        ):
            directory = base / (str(number) + '-' + label)
            runtime = directory / 'runtime'
            runtime.mkdir(parents=True)
            resources = []
            for group, (tag, kinds) in enumerate((('agenteam.d05.objectfixture', ('container', 'network')),
                                                  ('agenteam.d04.networkfixture', ('container', 'network')),
                                                  ('agenteam.d03.fixture', ('container', 'container', 'network'))), 1):
                for kind in kinds:
                    resources.append({'kind': kind, 'id': format(len(resources) + 1, '064x'),
                                      'label': tag, 'nonce': format(group, '032x')})
            private = [str(runtime / name) for name in ('object', 'outbound', 'pg')]
            manifest = directory / 'owned.json'
            manifest.write_text(json.dumps({'kind': 'work-owner-root-chain', 'resources': resources, 'directories': private}))
            manifest.chmod(0o600)
            if residue:
                Path(private[0]).mkdir()
            observed = []
            def absent(item, timeout):
                observed.append(item['id'])
                return not live
            sup['exact_absent'] = absent
            log_path = directory / 'test.log'
            with log_path.open('w+') as log:
                log.write(''.join('=== RUN   ' + name + '\n' for name in actual))
                if waited:
                    log.write('D03 explicit test actual_wait pid=123 code=0 selector=' + selector + '\n')
                check(sup['observe_root_chain'](directory, log, log_path, selector) == expected, label)
            check(len(observed) == 14 and all(observed.count(item['id']) == 2 for item in resources), 'seven resources observed twice')
            text = log_path.read_text()
            check(text.count('ROOT private_observation=') == text.count('ROOT runtime_observation=') == 2, 'private/runtime double observations')

# Evaluate the real input-freeze node, including its original indentation.
tree = ast.parse((ROOT / union.SUP).read_text())
main = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
node = next(node for node in main.body if isinstance(node, ast.If) and
            any(isinstance(child, ast.Attribute) and child.attr == 'input_paths' for child in ast.walk(node)))
adapter = types.SimpleNamespace(**driver)
code = compile(ast.Module(body=[node], type_ignores=[]), union.SUP, 'exec')
adapter.sha = lambda path: 'explicit-digest'
for selector in driver['TARGETS']:
    scope = {'adapter': adapter, 'args': types.SimpleNamespace(run=selector, binary=binary), 'inputs': {}}
    exec(code, scope)
    expected = set(driver['input_paths'](binary))
    if selector == list(cases)[3]:
        expected.update(driver['metadata_cost_inputs']())
    if selector in tuple(cases)[4:]:
        expected.update(driver['metadata_remaining_cost_inputs']())
    check(set(scope['inputs']) == {str(path) for path in expected}, 'actual selector input closure')
check(set(driver['metadata_cost_inputs']()) <= set(driver['metadata_remaining_cost_inputs']()), 'earlier cost inputs preserved')
for helper_name, count in (('metadata_cost_inputs', 3), ('metadata_remaining_cost_inputs', 6)):
    actual = driver[helper_name]()
    check(set(path for path in actual if path.suffix == '.go') == set((ROOT / 'tests/objects').glob('*.go')), 'all same-package Go helpers')
    check(len([path for path in actual if path.suffix == '.sql']) == count and all(path.is_file() for path in actual), 'exact embedded SQL and existing sources')
print(json.dumps({'checks': checks, 'exit': 0, 'scope': 'actual union/D05 inverse, six exact entries, original resource/input gates; offline doubles only'}))
