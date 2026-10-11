"""Offline exact-selector controls; imports no service and starts no fixture."""
import ast
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '5196b88adaf0250dea4fb9edc0e6275be23ebd44'
SELECTOR = '^TestRunnerReadFileOperation$'
FILES = (
    '.agent-state/work-owner-http/root_chain_driver.py',
    '.agent-state/task-planning-recovery/pg_only_supervisor.py',
)


def stripped(source):
    tree = ast.parse(source)
    removed = 0
    for node in ast.walk(tree):
        if isinstance(node, ast.Dict):
            for n in range(len(node.keys) - 1, -1, -1):
                key = node.keys[n]
                if isinstance(key, ast.Constant) and key.value == SELECTOR:
                    del node.keys[n]
                    del node.values[n]
                    removed += 1
    return ast.dump(tree, include_attributes=False), removed


def load(source, path):
    scope = {'__file__': str(ROOT / path), '__name__': 'read_file_profile_control'}
    exec(compile(source, path, 'exec'), scope)
    return scope


scopes = []
preservation = []
for path in FILES:
    old = subprocess.run(['git', 'show', f'{BASE}:{path}'], cwd=ROOT,
                         check=True, capture_output=True, text=True).stdout
    new = (ROOT / path).read_text()
    assert stripped(old)[1] == 0 and stripped(new)[1] == 1
    assert stripped(old)[0] == stripped(new)[0], path
    before, after = load(old, path), load(new, path)
    names = ('METADATA_INPUTS', 'TARGETS') if path == FILES[0] else ('METADATA_GROUPS',)
    for name in names:
        assert {k: v for k, v in after[name].items() if k != SELECTOR} == before[name]
    scopes.append(after)
    preservation.append({'path': path, 'all_other_ast_and_effective_maps_equal': True})

assert scopes[0]['TARGETS'][SELECTOR] == 'tests/projectvariable'
assert all((ROOT / name).is_file() and not (ROOT / name).is_symlink()
           for name in scopes[0]['METADATA_INPUTS'][SELECTOR])
expected = ('TestRunnerReadFileOperation',
            'TestRunnerReadFileOperation/result-commit-loss-and-observation',
            'TestRunnerReadFileOperation/current-mount-clear-rejects-unclaimed')
assert scopes[1]['METADATA_GROUPS'][SELECTOR] == frozenset(expected)
source = (ROOT / 'tests/projectvariable/runner_read_file_operation_test.go').read_text()
assert re.findall(r'^func (Test\w+)\(', source, re.M) == [expected[0]]
assert re.findall(r't\.Run\("([^"]+)"', source) == [case.split('/')[1] for case in expected[1:]]
good = ''.join('=== RUN   ' + case + '\n' for case in expected)
good += ''.join('--- PASS: ' + case + ' (1.00s)\n' for case in expected)
good += 'PASS\nD03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n'
gate = scopes[1]['metadata_results']
assert gate(good, SELECTOR)
bad = [
    good.replace('=== RUN   ' + expected[1] + '\n', ''),
    good.replace('--- PASS:', '--- FAIL:'),
    good.replace('code=0', 'code=1'),
    good + '=== RUN   TestRunnerReadFileOperation/extra\n--- PASS: TestRunnerReadFileOperation/extra (0.01s)\n',
    good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    good.replace(SELECTOR, '^TestOther$'),
    good + 'FAIL\n',
]
assert all(not gate(value, SELECTOR) for value in bad)
print(json.dumps({'selector': SELECTOR, 'effective_target': 'tests/projectvariable',
                  'base': BASE, 'preservation': preservation,
                  'positive': 1, 'rejected': len(bad)}))
