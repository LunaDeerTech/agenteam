"""Offline controls for the Work Agent Assignee References PG selector; starts no resources."""
import ast
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '987ca6c42fb2c316fe8ce6564711f2afdedfbd75'
SELECTOR = '^TestWorkAgentAssigneeReferences$'
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
    scope = {'__file__': str(ROOT / path), '__name__': 'work_agent_assignee_references_profile_control'}
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
assert scopes[0]['METADATA_INPUTS'][SELECTOR] == ('tests/projectvariable/work_agent_assignee_references_test.go', 'tests/projectvariable/task_transition_scheduler_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'internal/central/work/contract/agent_assignee_references.go', 'internal/central/work/agent_assignee_references.go', 'internal/central/work/service.go', 'internal/central/work/contract/structure.go', 'internal/central/work/repository.go', 'internal/central/work/reader.go', 'internal/central/work/task_repository.go', 'internal/central/agent/current.go', 'internal/central/agent/contract/reference.go', 'internal/central/project/authority.go', 'db/migrations/embed.go', 'db/migrations/00022_task_planning.sql', 'db/migrations/00041_task_transitions.sql', 'db/migrations/00045_task_scheduler_claims.sql', 'db/migrations/00075_project_variable_call_ownership.sql', '.agent-state/work-agent-assignee-references/profile-controls.py')
expected = ('TestWorkAgentAssigneeReferences', 'TestWorkAgentAssigneeReferences/same-transaction-current-and-terminal-references', 'TestWorkAgentAssigneeReferences/authority-and-transaction-boundaries', 'TestWorkAgentAssigneeReferences/caller-lock-release-before-original-return')
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
    'extra_case': good + '=== RUN   TestWorkAgentAssigneeReferences/extra\n--- PASS: TestWorkAgentAssigneeReferences/extra (0.01s)\n',
    'duplicate_wait': good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
    'wrong_wait_selector': good.replace(SELECTOR, '^TestOther$'),
    'overall_failure': good + 'FAIL\n',
}
for name, output in bad.items():
    assert not gate(output, SELECTOR), name
print(json.dumps({'selector': SELECTOR, 'baseline': BASE, 'preservation': results,
                  'positive': 1, 'rejected': list(bad)}))
