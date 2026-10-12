"""Offline controls for the Scheduler Project Stop PG selector; starts no resources."""
import ast
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = 'e4cf974eacd303c46e763cf9524db8ad61cec1b1'
SELECTOR = '^TestSchedulerProjectStop$'
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
    scope = {'__file__': str(ROOT / path), '__name__': 'scheduler_project_stop_profile_control'}
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
    names = ('METADATA_INPUTS', 'TARGETS') if path == FILES[0] else ('METADATA_GROUPS',)
    for name in names:
        assert {key: value for key, value in after[name].items()
                if key != SELECTOR} == before[name], (path, name)
    scopes.append(after)
    results.append({'path': path, 'old_ast_and_effective_maps_unchanged': list(names)})

assert scopes[0]['TARGETS'][SELECTOR] == 'tests/projectvariable'
assert scopes[0]['METADATA_INPUTS'][SELECTOR] == ('tests/projectvariable/scheduler_project_stop_test.go', 'tests/projectvariable/scheduler_execution_test.go', 'tests/projectvariable/execution_first_round_test.go', 'tests/projectvariable/execution_model_environment_capture_test.go', 'tests/projectvariable/project_runners_test.go', 'tests/projectvariable/scheduler_pending_visit_test.go', 'tests/projectvariable/project_lifecycle_fixture_test.go', 'tests/projectvariable/execution_capture_providers_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/scheduler_project_runner_test.go', 'tests/projectvariable/scheduler_claim_test.go', 'internal/central/project/scheduler_stop.go', 'internal/central/project/agent_retirement_preparation.go', 'internal/central/scheduler/project_stop.go', 'internal/central/scheduler/project_runners.go', 'internal/central/project/scheduler_projects.go', 'internal/central/project/lifecycle_stop_round.go', 'internal/central/project/lifecycle_stop_claim.go', 'internal/central/project/lifecycle_authority.go', 'db/migrations/embed.go', 'db/migrations/00075_project_variable_call_ownership.sql', '.agent-state/scheduler-project-stop/profile-controls.py')
expected = ('TestSchedulerProjectStop', 'TestSchedulerProjectStop/committed-stop-isolates-project-and-discards-stale-page', 'TestSchedulerProjectStop/driver-scope-and-authorization-unknown')
assert scopes[1]['METADATA_GROUPS'][SELECTOR] == frozenset(expected)
good = ''.join('=== RUN   ' + case + '\n' for case in expected)
good += ''.join('--- PASS: ' + case + ' (1.00s)\n' for case in expected)
good += 'PASS\nD03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n'
gate = scopes[1]['metadata_results']
assert gate(good, SELECTOR)
bad = {
    'missing_sub_run': good.replace('=== RUN   ' + expected[1] + '\n', ''),
    'failed_case': good.replace('--- PASS:', '--- FAIL:'),
    'nonzero_wait': good.replace('code=0', 'code=1'),
    'extra_case': good + '=== RUN   TestSchedulerProjectStop/extra\n--- PASS: TestSchedulerProjectStop/extra (0.01s)\n',
    'duplicate_wait': good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    'wrong_wait_selector': good.replace(SELECTOR, '^TestOther$'),
    'overall_failure': good + 'FAIL\n',
}
for name, output in bad.items():
    assert not gate(output, SELECTOR), name
print(json.dumps({'selector': SELECTOR, 'baseline': BASE, 'preservation': results,
                  'positive': 1, 'rejected': list(bad)}))
