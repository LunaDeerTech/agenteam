#!/usr/bin/env python3
"""Pure profile controls: no compiler, subprocess test, server, Docker or network."""
import ast
import json
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestProjectUsageWeb$'
BASE = '98d7a02e'
PATHS = ('.agent-state/work-owner-http/root_chain_driver.py', '.agent-state/task-planning-recovery/pg_only_supervisor.py')


def usage_condition(node):
    return (isinstance(node, ast.Compare) and len(node.ops) == 1 and isinstance(node.ops[0], ast.Eq)
            and len(node.comparators) == 1 and isinstance(node.comparators[0], ast.Name)
            and node.comparators[0].id == 'PROJECT_USAGE_UI')


class RemoveOnlyUsage(ast.NodeTransformer):
    def visit_Assign(self, node):
        if any(isinstance(target, ast.Name) and target.id in ('PROJECT_USAGE_UI', 'PROJECT_USAGE_ENV') for target in node.targets):
            return None
        return self.generic_visit(node)

    def visit_Dict(self, node):
        pairs = [(key, value) for key, value in zip(node.keys, node.values) if not (isinstance(key, ast.Constant) and key.value == SELECTOR)]
        node.keys, node.values = [p[0] for p in pairs], [p[1] for p in pairs]
        return self.generic_visit(node)

    def visit_Tuple(self, node):
        node.elts = [value for value in node.elts if not (isinstance(value, ast.Name) and value.id == 'PROJECT_USAGE_UI')]
        return self.generic_visit(node)

    def visit_If(self, node):
        if usage_condition(node.test):
            return None
        return self.generic_visit(node)

    def visit_IfExp(self, node):
        if usage_condition(node.test):
            return self.visit(node.orelse)
        return self.generic_visit(node)


modules = []
for relative in PATHS:
    path = ROOT / relative
    current = path.read_text()
    before = subprocess.check_output(['git', 'show', BASE + ':' + relative], cwd=ROOT, text=True)
    reduced = RemoveOnlyUsage().visit(ast.parse(current))
    assert ast.dump(reduced, include_attributes=False) == ast.dump(ast.parse(before), include_attributes=False), relative
    namespace = {'__file__': str(path), '__name__': 'usage_profile_controls'}
    exec(compile(current, str(path), 'exec'), namespace)
    modules.append(namespace)
driver, supervisor = modules
assert driver['TARGETS'][SELECTOR] == 'tests/model'
assert driver['task_review_ui_profile'](SELECTOR) == (('AGENTEAM_PROJECT_USAGE_WEB_DIST', 'AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE'), 'output/ai/project-usage-read-ui', 'project-usage')
assert 'tests/model/project_usage_web_test.go' in driver['METADATA_INPUTS'][SELECTOR]
assert any(value == frozenset(('TestProjectUsageWeb', 'TestProjectUsageWeb/paginated-current-read')) for value in supervisor.values() if isinstance(value, frozenset)) or any(isinstance(value, dict) and value.get(SELECTOR) == frozenset(('TestProjectUsageWeb', 'TestProjectUsageWeb/paginated-current-read')) for value in supervisor.values())
line = '    project_usage_web_test.go:400: ProjectUsage Node actual_wait pid=123 success=true'
check = supervisor['task_review_browser_waits']
assert check(line, SELECTOR)
for invalid in ('', line + '\n' + line, line.replace('success=true', 'success=false'), line.replace('pid=123', 'pid=0'), line.replace('project_usage_web_test.go', 'project_secrets_web_test.go'), line.replace('ProjectUsage', 'ProjectSecrets')):
    assert not check(invalid, SELECTOR)
print(json.dumps({'old_profiles_and_all_gates_ast_unchanged': True, 'usage_target': 'tests/model', 'positive_node_wait': 1, 'rejected_node_waits': 6, 'services_started': False}))
