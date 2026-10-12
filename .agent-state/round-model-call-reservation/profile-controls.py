"""Offline controls for the Round Model Call Reservation PG selector; starts no resources."""
import ast
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '6c843253ef8366a4080f3f7d1a2da8420db66fba'
SELECTOR = '^TestRoundModelCallReservation$'
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
    scope = {'__file__': str(ROOT / path), '__name__': 'round_model_call_reservation_profile_control'}
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
assert scopes[0]['METADATA_INPUTS'][SELECTOR] == ('tests/projectvariable/round_model_call_reservation_test.go', 'tests/projectvariable/next_round_model_request_test.go', 'tests/projectvariable/run_command_result_consumption_test.go', 'tests/projectvariable/run_command_operation_test.go', 'db/migrations/00066_execution_tool_result_consumptions.sql', 'db/migrations/00068_execution_round_inputs.sql', 'db/migrations/00071_execution_run_command_results.sql', 'db/migrations/00072_execution_round_model_requests.sql', 'db/migrations/00073_execution_round_model_claims.sql', 'internal/central/execution/contract/round_model_call.go', 'internal/central/execution/round_model_call.go', 'internal/central/execution/round_model_call_owner.go', 'internal/central/execution/round_model_call_repository.go', 'internal/central/execution/contract/next_round_request_codec.go', 'internal/central/execution/contract/round_model_request.go', 'internal/central/execution/round_model_request.go', 'internal/central/execution/round_model_request_owner.go', 'internal/central/execution/round_model_request_repository.go', 'internal/central/agentloop/next_round_request.go', 'internal/central/object/process_binding.go', '.agent-state/round-model-call-reservation/profile-controls.py')
expected = ('TestRoundModelCallReservation',
            'TestRoundModelCallReservation/committed-request-reservation-and-history',
            'TestRoundModelCallReservation/reservation-rollback-and-commit-loss-lookup')
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
    'extra_case': good + '=== RUN   TestRoundModelCallReservation/extra\n--- PASS: TestRoundModelCallReservation/extra (0.01s)\n',
    'duplicate_wait': good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    'wrong_wait_selector': good.replace(SELECTOR, '^TestOther$'),
    'overall_failure': good + 'FAIL\n',
}
for name, output in bad.items():
    assert not gate(output, SELECTOR), name
print(json.dumps({'selector': SELECTOR, 'baseline': BASE, 'preservation': results,
                  'positive': 1, 'rejected': list(bad)}))
