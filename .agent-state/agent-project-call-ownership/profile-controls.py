"""Offline controls for the Agent Project Call Ownership PG selector; starts no resources."""
import ast
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '4f76ef40eb0b8129940a21e2e36c55d0e03eaa83'
SELECTOR = '^TestAgentProjectCallOwnership$'
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
    scope = {'__file__': str(ROOT / path), '__name__': 'agent_project_call_ownership_profile_control'}
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
assert scopes[0]['METADATA_INPUTS'][SELECTOR] == ('tests/projectvariable/agent_project_call_ownership_test.go', 'tests/projectvariable/agent_project_call_ownership_fixture_test.go', 'tests/projectvariable/agent_project_call_ownership_process_test.go', 'tests/projectvariable/agent_project_lifecycle_test.go', 'tests/projectvariable/agent_project_lifecycle_fixture_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/project_phase_stop_fixture_test.go', 'tests/projectvariable/project_phase_recovery_guard_test.go', 'tests/projectvariable/fixture_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'internal/central/agent/project_call_ownership.go', 'internal/central/agent/project_call_repository.go', 'internal/central/agent/project_call_observer.go', 'internal/central/agent/project_lifecycle.go', 'internal/central/agent/calls.go', 'internal/central/agent/service.go', 'internal/central/agent/commands.go', 'internal/central/agent/command_plan.go', 'internal/central/agent/command_store.go', 'internal/central/agent/reader.go', 'internal/central/agent/runner_mounts.go', 'internal/central/agent/directory.go', 'internal/central/object/process.go', 'internal/central/object/process_binding.go', 'internal/central/project/lifecycle_stop_round.go', 'db/migrations/00073_execution_round_model_claims.sql', 'db/migrations/00074_agent_project_call_ownership.sql', '.agent-state/project-variables-independent/commitproxy/proxy.go', '.agent-state/agent-project-call-ownership/profile-controls.py')
expected = ('TestAgentProjectCallOwnership',
            'TestAgentProjectCallOwnership/local-original-return-and-terminal-fence',
            'TestAgentProjectCallOwnership/foreign-death-before-original-sql-terminal')
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
    'extra_case': good + '=== RUN   TestAgentProjectCallOwnership/extra\n--- PASS: TestAgentProjectCallOwnership/extra (0.01s)\n',
    'duplicate_wait': good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    'wrong_wait_selector': good.replace(SELECTOR, '^TestOther$'),
    'overall_failure': good + 'FAIL\n',
}
for name, output in bad.items():
    assert not gate(output, SELECTOR), name
print(json.dumps({'selector': SELECTOR, 'baseline': BASE, 'preservation': results,
                  'positive': 1, 'rejected': list(bad)}))
