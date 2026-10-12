#!/usr/bin/python3
"""Offline exact-result and original TCP-gate controls; no socket/Go execution."""
import ast
import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def module(path):
    spec = importlib.util.spec_from_file_location('run_command_method_control', path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def tcp_loop(source):
    tree = ast.parse(source)
    for node in ast.walk(tree):
        for field in ('body', 'orelse', 'finalbody'):
            body = getattr(node, field, [])
            if not isinstance(body, list):
                continue
            for index, statement in enumerate(body):
                if (isinstance(statement, ast.Assign) and any(isinstance(t, ast.Name) and
                        t.id == 'tail_deadline' for t in statement.targets)):
                    segment = body[index:index + 3]
                    if len(segment) == 3 and isinstance(segment[2], ast.While):
                        return ast.dump(ast.Module(body=segment, type_ignores=[]), include_attributes=False)
    raise AssertionError('original automatic TCP loop not found')


def main():
    gate = module(HERE / 'run-native-checks.py')
    shared = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
    assert tcp_loop(shared.read_text()) == tcp_loop((HERE / 'run-native-checks.py').read_text())
    # Merely build the original helper functions; do not invoke tcp/descendants.
    assert {'tcp', 'tcp_failure_sample', 'descendants'} <= gate.original_helpers().keys()
    top = 'TestRunCommandWSS'
    sub = top + '/exit-and-cancel'
    good = (f'=== RUN   {top}\n=== RUN   {sub}\n'
            f'    fixture.go:1: {gate.RESOURCE_MARKER}\n'
            f'--- PASS: {top} (1.00s)\n    --- PASS: {sub} (1.00s)\nPASS\n')
    assert gate.result_complete(good, 0)
    bad = [
        (good.replace(f'=== RUN   {sub}\n', ''), 0),
        (good.replace(f'--- PASS: {sub}', f'--- FAIL: {sub}'), 0),
        (good, 1),
        (good + f'=== RUN   {top}/extra\n--- PASS: {top}/extra (0.01s)\n', 0),
        (good.replace(gate.RESOURCE_MARKER, 'owned_resources_joined=false'), 0),
        (good + f'--- PASS: {sub} (0.01s)\n', 0),
        (good + 'FAIL\n', 0),
    ]
    assert all(not gate.result_complete(text, code) for text, code in bad)
    checks = module(HERE / 'run-checks.py')
    assert len(checks.TESTS) == 3
    assert sum(len(names) for names in checks.TESTS.values()) == 20
    assert checks.SELECTOR == gate.SELECTOR
    print(json.dumps({'positive': 1, 'negative_rejected': len(bad),
                      'basic_new_top': 14, 'basic_affected_old_top': 6,
                      'native_top': 1, 'native_sub': 1,
                      'original_tcp_loop_ast_equal': True,
                      'original_helpers_loaded_without_invocation': True}))


if __name__ == '__main__':
    main()
