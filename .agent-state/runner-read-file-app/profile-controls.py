"""Offline Runner profile/descriptor controls; no Go, subprocess or resource startup."""
import argparse
import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
BASE = '3a495115'
SELECTOR = '^TestRunnerReadFileApp$'
FILES = (
    '.agent-state/work-owner-http/root_chain_driver.py',
    '.agent-state/task-planning-recovery/pg_only_supervisor.py',
)
# Captured from these exact pre-edit shared files at BASE. No Git execution is
# needed for this offline check; reviewers can independently reproduce the AST.
BASE_AST_SHA = (
    '6a4ac0af5d5e37dbaa9b6f50a758c36c304eb160fa67472ca8aade30598aac3c',
    '77aefb8ccb467d6dddafb6782b8f9b0fe4a994a1b980ae61ea848695575bfff8',
)
EXPECTED = ('TestRunnerReadFileApp',
            'TestRunnerReadFileApp/explicit-cli-range-and-identity-restart',
            'TestRunnerReadFileApp/default-empty-capability-unsupported',
            'TestRunnerReadFileApp/invalid-root-and-explicit-config-before-connect')
INPUTS = tuple(FILES) + tuple('.agent-state/runner-read-file-app/' + name for name in (
    'run-pg-checks.py', 'run-basic-checks.py', 'profile-controls.py', 'record-native.py', 'README.md'))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def stripped(source):
    tree = ast.parse(source)
    count = {'key': 0, 'helper': 0, 'branch': 0}
    class Original(ast.NodeTransformer):
        def visit_FunctionDef(self, node):
            if node.name == 'runner_read_file_app_inputs':
                count['helper'] += 1
                return None
            return self.generic_visit(node)

        def visit_If(self, node):
            if (isinstance(node.test, ast.Compare) and isinstance(node.test.left, ast.Name)
                    and node.test.left.id == 'selector' and len(node.test.ops) == 1
                    and isinstance(node.test.ops[0], ast.Eq)
                    and len(node.test.comparators) == 1
                    and isinstance(node.test.comparators[0], ast.Constant)
                    and node.test.comparators[0].value == SELECTOR):
                count['branch'] += 1
                return None
            return self.generic_visit(node)

        def visit_Dict(self, node):
            for index in range(len(node.keys) - 1, -1, -1):
                key = node.keys[index]
                if isinstance(key, ast.Constant) and key.value == SELECTOR:
                    del node.keys[index]
                    del node.values[index]
                    count['key'] += 1
            return self.generic_visit(node)
    Original().visit(tree)
    return tree, count


def load(source, path):
    scope = {'__file__': str(ROOT / path), '__name__': 'runner_read_file_app_profile_control'}
    exec(compile(source, path, 'exec'), scope)
    return scope


def profile_checks():
    result, scopes = [], []
    for index, name in enumerate(FILES):
        source = (ROOT / name).read_text()
        original, count = stripped(source)
        expected_count = {'key': 1, 'helper': 1, 'branch': 2} if index == 0 else {'key': 1, 'helper': 0, 'branch': 0}
        assert count == expected_count, (name, count)
        old_sha = hashlib.sha256(ast.dump(original, include_attributes=False).encode()).hexdigest()
        assert old_sha == BASE_AST_SHA[index], (name, 'original AST changed')
        before, after = load(original, name), load(source, name)
        maps = ('METADATA_INPUTS', 'TARGETS') if index == 0 else ('METADATA_GROUPS',)
        for key in maps:
            assert {k: v for k, v in after[key].items() if k != SELECTOR} == before[key]
        scopes.append(after)
        result.append({'path': name, 'baseline_ast_sha256': old_sha, 'maps_unchanged': list(maps)})
    driver, supervisor = scopes
    assert driver['TARGETS'][SELECTOR] == 'tests/projectvariable'
    assert supervisor['METADATA_GROUPS'][SELECTOR] == frozenset(EXPECTED)
    assert supervisor['budgets'](True) == (540, 60)
    good = ''.join('=== RUN   ' + case + '\n' for case in EXPECTED)
    good += ''.join('--- PASS: ' + case + ' (1.00s)\n' for case in EXPECTED)
    good += 'PASS\nD03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n'
    gate = supervisor['metadata_results']
    assert gate(good, SELECTOR)
    bad = {
        'missing_sub_run': good.replace('=== RUN   ' + EXPECTED[1] + '\n', ''),
        'failed_case': good.replace('--- PASS:', '--- FAIL:'),
        'nonzero_wait': good.replace('code=0', 'code=1'),
        'extra_case': good + '=== RUN   TestRunnerReadFileApp/extra\n--- PASS: TestRunnerReadFileApp/extra (0.01s)\n',
        'duplicate_wait': good + 'D03 explicit test actual_wait pid=124 code=0 selector=' + SELECTOR + '\n',
        'wrong_wait_selector': good.replace(SELECTOR, '^TestOther$'),
        'overall_failure': good + 'FAIL\n',
    }
    for name, output in bad.items():
        assert not gate(output, SELECTOR), name
    return driver, {'shared_preservation': result, 'metadata_positive': 1,
                    'metadata_rejected': list(bad)}


def descriptor_checks(driver):
    # Small synthetic files exercise only the descriptor boundary. They are
    # never executed and cannot satisfy original native configuration/MinIO.
    names = ('AGENTEAM_RUNNER_BINARY', 'AGENTEAM_RUNNER_READ_FILE_APP_INPUTS')
    previous = {name: os.environ.get(name) for name in names}
    rejected = []
    try:
        with tempfile.TemporaryDirectory(prefix='runner-read-file-descriptor-') as temp:
            root = Path(temp).resolve()
            source = root / 'cmd/agenteam-runner/main.go'
            pg_source = root / 'tests/projectvariable/runner_read_file_app_linux_test.go'
            for path in (source, pg_source):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'package fixture\n')
            go, runner, pg = root / 'go', root / 'agenteam-runner', root / 'projectvariable.test'
            for path in (go, runner, pg):
                path.write_bytes(path.name.encode())
                path.chmod(0o700)
            descriptor = root / 'runner-read-file-inputs.json'
            value = {'schema': 1, 'selector': SELECTOR,
                     'artifacts': {kind: {'path': str(path), 'sha256': sha(path), 'size_bytes': path.stat().st_size}
                                   for kind, path in (('runner', runner), ('pg', pg))},
                     'source_inputs': {str(path): sha(path) for path in (source, pg_source, go)},
                     'compiler_inputs': {'runner': [str(source)], 'pg': [str(pg_source)]}}
            driver['REPOSITORY'], driver['GO'] = root, go
            check = driver['runner_read_file_app_inputs']
            os.environ.update({names[0]: str(runner), names[1]: str(descriptor)})
            def write(item):
                descriptor.write_text(json.dumps(item))
            def rejects(label, operation):
                try:
                    operation()
                except (ValueError, OSError, TypeError, KeyError):
                    rejected.append(label)
                else:
                    raise AssertionError(label + ' unexpectedly accepted')
            write(value)
            paths, proof = check(pg)
            assert paths == {source, pg_source, go, runner, pg, descriptor}
            assert proof['source_count'] == 3 and proof['compiler_counts'] == {'runner': 1, 'pg': 1}
            mutations = {
                'wrong_selector': lambda v: v.update(selector='^Other$'),
                'wrong_schema': lambda v: v.update(schema=True),
                'missing_artifact': lambda v: v['artifacts'].pop('runner'),
                'unknown_field': lambda v: v.update(extra_inputs=[]),
                'wrong_binary_size': lambda v: v['artifacts']['runner'].update(size_bytes=999),
                'wrong_binary_sha': lambda v: v['artifacts']['pg'].update(sha256='0' * 64),
                'source_changed': lambda v: v['source_inputs'].update({str(source): '0' * 64}),
                'go_omitted': lambda v: v['source_inputs'].pop(str(go)),
                'missing_command_closure': lambda v: v['compiler_inputs'].update(runner=[]),
                'duplicated_closure': lambda v: v['compiler_inputs'].update(pg=[str(pg_source)] * 2),
                'unknown_closure_source': lambda v: v['compiler_inputs'].update(runner=[str(root / 'missing.go')]),
                'compile_go_placeholder': lambda v: v['artifacts'].update(pg={'path': str(go), 'sha256': sha(go), 'size_bytes': go.stat().st_size}),
            }
            for name, mutate in mutations.items():
                changed = copy.deepcopy(value)
                mutate(changed)
                write(changed)
                rejects(name, lambda: check(pg))
            write(value)
            rejects('pg_argument_mismatch', lambda: check(runner))
            os.environ[names[0]] = str(go)
            rejects('runner_environment_mismatch', lambda: check(pg))
            os.environ[names[0]] = str(runner)
            os.environ.pop(names[1])
            rejects('missing_descriptor', lambda: check(pg))
            os.environ[names[1]] = str(descriptor)
            descriptor.write_text('{"schema":1,' + json.dumps(value)[1:])
            rejects('duplicate_json_key', lambda: check(pg))
            write(value)
            source.unlink()
            source.symlink_to(pg_source)
            rejects('symlink_source', lambda: check(pg))
            source.unlink()
            source.write_bytes(b'package fixture\n')
            write(value)
            check(pg)
    finally:
        for name, value in previous.items():
            if value is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = value
    return {'descriptor_positive': 1, 'descriptor_rejected': rejected}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    started = time.monotonic()
    before = {name: sha(ROOT / name) for name in INPUTS}
    def save(name, value):
        with (out / name).open('x') as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
            stream.write('\n')
    save('inputs-before.json', before)
    driver, result = profile_checks()
    result.update(descriptor_checks(driver))
    after = {name: sha(ROOT / name) for name in INPUTS}
    save('inputs-after.json', after)
    assert before == after
    result.update(selector=SELECTOR, baseline=BASE, inputs_unchanged=True,
                  pid=os.getpid(), elapsed_seconds=round(time.monotonic() - started, 3),
                  scope='offline AST, metadata gate and synthetic descriptor only; no product execution')
    save('result.json', result)
    print(json.dumps(result))


if __name__ == '__main__':
    main()
