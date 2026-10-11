#!/usr/bin/env python3
"""Offline checks for the new PG profile; never start Go, Docker or sockets."""
import ast
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from types import ModuleType
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
BASE = '806e0a68'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestExecutionMountCapture$'
TOP = 'TestExecutionMountCapture'
CASES = (
    TOP,
    TOP + '/nonempty-unknown-and-historical-material',
    TOP + '/final-rollback-keeps-no-partial-capture',
)


def load(source, relative, name):
    module = ModuleType(name)
    module.__file__ = str(ROOT / relative)
    exec(compile(source, module.__file__, 'exec'), module.__dict__)
    return module


def remove_profile(source, mapping):
    tree = ast.parse(source)
    removed = 0
    for statement in tree.body:
        if not isinstance(statement, ast.Assign):
            continue
        if not any(isinstance(target, ast.Name) and target.id == mapping
                   for target in statement.targets):
            continue
        assert isinstance(statement.value, ast.Dict)
        keep = []
        for key, value in zip(statement.value.keys, statement.value.values):
            if isinstance(key, ast.Constant) and key.value == SELECTOR:
                removed += 1
            else:
                keep.append((key, value))
        statement.value.keys = [key for key, _ in keep]
        statement.value.values = [value for _, value in keep]
    assert removed == 1
    return ast.dump(tree, include_attributes=False)


def main():
    current, baseline, sources = {}, {}, {}
    for relative, mapping in ((DRIVER, 'METADATA_INPUTS'),
                              (SUPERVISOR, 'METADATA_GROUPS')):
        text = (ROOT / relative).read_text()
        original = subprocess.check_output(
            ['git', 'show', BASE + ':' + relative], cwd=ROOT, text=True)
        assert remove_profile(text, mapping) == ast.dump(
            ast.parse(original), include_attributes=False)
        current[relative] = load(text, relative, 'current_' + mapping)
        baseline[relative] = load(original, relative, 'base_' + mapping)
        sources[relative] = hashlib.sha256(text.encode()).hexdigest()

    driver, supervisor = current[DRIVER], current[SUPERVISOR]
    old_driver, old_supervisor = baseline[DRIVER], baseline[SUPERVISOR]
    assert {k: v for k, v in driver.METADATA_INPUTS.items()
            if k != SELECTOR} == old_driver.METADATA_INPUTS
    assert {k: v for k, v in driver.TARGETS.items()
            if k != SELECTOR} == old_driver.TARGETS
    assert {k: v for k, v in supervisor.METADATA_GROUPS.items()
            if k != SELECTOR} == old_supervisor.METADATA_GROUPS
    assert driver.TARGETS[SELECTOR] == 'tests/projectvariable'
    assert supervisor.METADATA_GROUPS[SELECTOR] == frozenset(CASES)
    assert supervisor.budgets(True) == (540, 60)
    assert supervisor.budgets(False) == old_supervisor.budgets(False)
    assert set(old_driver.METADATA_INPUTS[
        '^TestExecutionModelEnvironmentCapture$']).issubset(
            driver.METADATA_INPUTS[SELECTOR])
    for relative in driver.METADATA_INPUTS[SELECTOR]:
        path = ROOT / relative
        assert path.is_file() and not path.is_symlink()
    fixture = (ROOT / 'tests/projectvariable/execution_mount_capture_test.go').read_text()
    assert re.findall(r'^func (Test\w+)\(t \*testing.T\)', fixture, re.M) == [TOP]
    assert tuple(TOP + '/' + value for value in re.findall(
        r'\bt\.Run\("([^"]+)"', fixture)) == CASES[1:]

    lines = ['=== RUN   ' + case for case in CASES]
    lines += ['--- PASS: ' + case + ' (0.01s)' for case in reversed(CASES)]
    lines += ['PASS', 'D03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR]
    good = '\n'.join(lines) + '\n'
    assert supervisor.metadata_results(good, SELECTOR)
    rejected = {}
    for case in CASES:
        rejected['missing run ' + case] = good.replace('=== RUN   ' + case + '\n', '')
        rejected['missing pass ' + case] = good.replace('--- PASS: ' + case + ' (0.01s)\n', '')
        rejected['failed ' + case] = good.replace('--- PASS: ' + case, '--- FAIL: ' + case)
        rejected['skipped ' + case] = good.replace('--- PASS: ' + case, '--- SKIP: ' + case)
    rejected.update({
        'duplicate run': good + lines[0] + '\n',
        'duplicate pass': good + lines[3] + '\n',
        'missing wait': good.replace(lines[-1] + '\n', ''),
        'duplicate wait': good + lines[-1] + '\n',
        'nonzero wait': good.replace('code=0 selector=', 'code=1 selector='),
        'wrong waited selector': good.replace('selector=' + SELECTOR, 'selector=^TestOther$'),
        'standalone fail': good + 'FAIL\n',
        'foreign case': good + '=== RUN   TestOther\n--- PASS: TestOther (0.01s)\n',
    })
    for name, sample in rejected.items():
        assert not supervisor.metadata_results(sample, SELECTOR), name

    # Only exercise configuration parsing with an inert temporary file. No Go
    # executable is launched and the MinIO checksum check is locally stubbed.
    invalid = ('TestExecutionMountCapture', '^TestExecutionMountCapture',
               '^TestExecutionMountCapture$/nonempty-unknown-and-historical-material')
    with tempfile.TemporaryDirectory(prefix='execution-mount-profile-') as temporary:
        binary = Path(temporary) / 'inert.test'
        binary.write_bytes(b'profile parsing only\n')
        binary.chmod(0o700)
        directory = Path(temporary) / 'not-created'
        with patch.object(driver, 'sha', return_value=driver.MINIO_SHA):
            plan = driver.configuration(binary, SELECTOR, directory)
            assert plan['cwd'] == str(ROOT / 'tests/projectvariable')
            assert plan['resources'] == 7 and plan['test_timeout'] == '6m'
            for selector in invalid:
                try:
                    driver.configuration(binary, selector, directory)
                except ValueError:
                    pass
                else:
                    raise AssertionError('non-exact selector accepted')
                assert not supervisor.metadata_results(good, selector)
        assert not directory.exists()

    record = {
        'base': BASE, 'source_sha256': sources,
        'old_full_ast_unchanged_after_removing_new_mapping': True,
        'old_effective_targets_inputs_groups_unchanged': True,
        'selector': SELECTOR, 'target': driver.TARGETS[SELECTOR],
        'cases': CASES, 'explicit_inputs': driver.METADATA_INPUTS[SELECTOR],
        'positive_result_controls': 1, 'negative_result_controls': len(rejected),
        'negative_exact_selector_controls': len(invalid),
        'original_root_budget': [540, 60], 'test_timeout': '6m', 'resources': 7,
        'native_executed': False,
    }
    print(json.dumps(record, indent=2))


if __name__ == '__main__':
    main()
