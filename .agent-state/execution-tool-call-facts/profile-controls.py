"""Offline controls for the first ToolCall PG selector; starts no resources."""
import ast
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '226ea38b'
SELECTOR = '^TestExecutionToolCallFacts$'
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
    scope = {'__file__': str(ROOT / path), '__name__': 'tool_call_profile_control'}
    exec(compile(source, path, 'exec'), scope)
    return scope


results = []
scopes = []
for path in FILES:
    old = subprocess.run(['git', 'show', f'{BASE}:{path}'], cwd=ROOT,
                         check=True, capture_output=True, text=True).stdout
    new = (ROOT / path).read_text()
    assert stripped(old)[1] == 0
    assert stripped(new)[1] == 1
    assert stripped(old)[0] == stripped(new)[0], path
    before, after = load(old, path), load(new, path)
    name = 'TARGETS' if path == FILES[0] else 'METADATA_GROUPS'
    assert {key: value for key, value in after[name].items()
            if key != SELECTOR} == before[name]
    scopes.append(after)
    results.append({'path': path, 'old_ast_and_effective_map_unchanged': True})

assert scopes[0]['TARGETS'][SELECTOR] == 'tests/projectvariable'
expected = ('TestExecutionToolCallFacts',
            'TestExecutionToolCallFacts/held-close-unknown-and-binding',
            'TestExecutionToolCallFacts/current-revocation-and-checkpoint-rollback')
assert scopes[1]['METADATA_GROUPS'][SELECTOR] == frozenset(expected)
good = ''.join('=== RUN   ' + case + '\n' for case in expected)
good += ''.join('--- PASS: ' + case + ' (1.00s)\n' for case in expected)
good += 'PASS\nD03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n'
gate = scopes[1]['metadata_results']
assert gate(good, SELECTOR)
bad = [
    good.replace('=== RUN   TestExecutionToolCallFacts/held-close-unknown-and-binding\n', ''),
    good.replace('--- PASS:', '--- FAIL:'),
    good.replace('code=0', 'code=1'),
    good + '=== RUN   TestExecutionToolCallFacts/extra\n--- PASS: TestExecutionToolCallFacts/extra (0.01s)\n',
    good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    good.replace(SELECTOR, '^TestOther$'),
    good + 'FAIL\n',
]
assert all(not gate(value, SELECTOR) for value in bad)
print(json.dumps({'selector': SELECTOR, 'preservation': results,
                  'positive': 1, 'rejected': len(bad)}))
