#!/usr/bin/env python3
"""Lightweight source/controlled-main checks; no Go build, PG or socket.

Artifact metadata/content and process/proc/TCP operations are explicit stand-ins.
Use --artifacts only after a separately completed real candidate/driver build;
that optional step performs exact listing and pre-resource rejection only.
"""
import argparse
import ast
import contextlib
import importlib.util
import io
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER_SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go'
OUTPUT = ROOT / 'output/ai/secret-variable-storage'
spec = importlib.util.spec_from_file_location('secret_recovery_entry_review', SOURCE)
supervisor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(supervisor)
WRITE = '^TestSecretVariableStorageSQL(CommitUnknown|NonceUnknown)$'
STATE = '^TestSecretVariableStorageSQL(MaintenanceUnknown|Concurrency)$'
EXPECTED = {
    WRITE: {'TestSecretVariableStorageSQLCommitUnknown',
            'TestSecretVariableStorageSQLCommitUnknown/before',
            'TestSecretVariableStorageSQLCommitUnknown/after',
            'TestSecretVariableStorageSQLCommitUnknown/pending',
            'TestSecretVariableStorageSQLNonceUnknown'},
    STATE: {'TestSecretVariableStorageSQLMaintenanceUnknown',
            'TestSecretVariableStorageSQLMaintenanceUnknown/rotation-refresh',
            'TestSecretVariableStorageSQLMaintenanceUnknown/rotation-no-refresh',
            'TestSecretVariableStorageSQLMaintenanceUnknown/cleanup',
            'TestSecretVariableStorageSQLConcurrency',
            'TestSecretVariableStorageSQLConcurrency/same-intent',
            'TestSecretVariableStorageSQLConcurrency/changed-value',
            'TestSecretVariableStorageSQLConcurrency/stale-credential-version'},
}
NEW_FILES = {
    'secret_variable_storage_recovery_fixture_test.go',
    'secret_variable_storage_recovery_test.go',
    'secret_variable_storage_recovery_nonce_test.go',
    'secret_variable_storage_recovery_concurrency_test.go',
    'secret_variable_storage_recovery_maintenance_test.go',
    'audit_proxy_test.go', 'outbound_reload_test.go',
}
OLD_DRIVER = OUTPUT / 'pg-only-driver'
OLD_BINARY = OUTPUT / 'secret-variable-storage-sql-reviewed.test'
DRIVER = OUTPUT / 'pg-only-recovery-driver'
BINARY = OUTPUT / 'secret-variable-storage-recovery.test'
ARTIFACTS = {OLD_DRIVER, OLD_BINARY, DRIVER, BINARY}
checks = 0


def require(value, label):
    global checks
    assert value, label
    checks += 1


require(supervisor.SECRET_STORAGE_RECOVERY_CASES == EXPECTED, 'exact top/subcase mapping')
require(supervisor.budgets(False) == (123, 3), 'original PG budget')
require(supervisor.budgets(True) == (540, 60), 'original root budget')

# Inverse proof covers the entire old source, not only selected budget strings.
projection_spec = importlib.util.spec_from_file_location('accepted_entry_union', ROOT / '.agent-state/owner-feature-integration/entry_union.py')
projection = importlib.util.module_from_spec(projection_spec)
projection_spec.loader.exec_module(projection)

def old(path):
    return projection.baseline_for(path, 'secret_storage', replacement='22c9d85f')


old_supervisor = old('.agent-state/task-planning-recovery/pg_only_supervisor.py')
old_tree = ast.parse(old_supervisor)
old_inputs = next(node for node in old_tree.body if isinstance(node, ast.FunctionDef) and node.name == 'secret_storage_inputs')
old_artifact_guard = next(node for node in old_inputs.body if isinstance(node, ast.If))


class RemoveRecovery(ast.NodeTransformer):
    def visit_Assign(self, node):
        if any(isinstance(target, ast.Name) and (target.id.startswith('SECRET_STORAGE_RECOVERY_') or target.id in ('secret_recovery', 'expected_driver', 'expected_binary')) for target in node.targets):
            return None
        return self.generic_visit(node)

    def visit_FunctionDef(self, node):
        if node.name == 'secret_storage_inputs':
            node.args.args.pop()
            node.args.defaults.pop()
        return self.generic_visit(node)

    def visit_If(self, node):
        if isinstance(node.test, ast.Name) and node.test.id == 'recovery':
            return None
        if any(isinstance(part, ast.Name) and part.id == 'expected_driver' for part in ast.walk(node.test)):
            node.test = old_artifact_guard.test
        return self.generic_visit(node)

    def visit_BinOp(self, node):
        if isinstance(node.op, ast.BitOr) and isinstance(node.right, ast.Name) and node.right.id == 'SECRET_STORAGE_RECOVERY_CASES':
            return node.left
        return self.generic_visit(node)

    def visit_BoolOp(self, node):
        if isinstance(node.op, ast.Or) and isinstance(node.values[-1], ast.Name) and node.values[-1].id == 'secret_recovery':
            return self.visit(node.values[0])
        return self.generic_visit(node)

    def visit_Call(self, node):
        if isinstance(node.func, ast.Name) and node.func.id == 'secret_storage_inputs' and len(node.args) == 3:
            node.args.pop()
        return self.generic_visit(node)


require(ast.dump(RemoveRecovery().visit(ast.parse(SOURCE.read_text())), include_attributes=False)
        == ast.dump(old_tree, include_attributes=False), 'entire old supervisor AST preserved')
driver_source = DRIVER_SOURCE.read_text()
inverse = re.sub(r'^const secretVariableStorageRecovery(?:Write|State)Selector = `[^`]+`\n', '', driver_source, flags=re.M)
for suffix in ('Write', 'State'):
    inverse = inverse.replace(' && *selector != secretVariableStorageRecovery' + suffix + 'Selector', '')
require(inverse == old('.agent-state/task-planning-recovery/pg_only_driver.go'), 'entire old driver bytes preserved')
for suffix, selector in (('Write', WRITE), ('State', STATE)):
    require(re.search(r'const secretVariableStorageRecovery' + suffix + r'Selector = `([^`]+)`', driver_source).group(1) == selector, 'Go selector ' + suffix)

real_resolve, real_stat, real_read = Path.resolve, Path.stat, Path.read_bytes
mutated = False
mutation_target = ROOT / 'tests/security/secret_variable_storage_recovery_test.go'


def resolve(path, *args, **kwargs):
    return path if path in ARTIFACTS else real_resolve(path, *args, **kwargs)


def file_stat(path, *args, **kwargs):
    if path in ARTIFACTS:
        return type('ControlledRegularFile', (), {'st_mode': stat.S_IFREG | 0o600})()
    return real_stat(path, *args, **kwargs)


def read(path):
    if path in ARTIFACTS:
        return b'controlled artifact, not a built candidate'
    if mutated and path == mutation_target:
        return b'controlled late source mutation'
    return real_read(path)


with patch.object(Path, 'resolve', resolve), patch.object(Path, 'stat', file_stat), patch.object(Path, 'read_bytes', read):
    old_inputs_set = set(supervisor.secret_storage_inputs(OLD_DRIVER, OLD_BINARY))
    new_inputs_set = set(supervisor.secret_storage_inputs(DRIVER, BINARY, True))
    require(new_inputs_set - old_inputs_set == {DRIVER, BINARY} | {ROOT / 'tests/security' / name for name in NEW_FILES}, 'only required recovery/proxy inputs added')
    require(old_inputs_set - new_inputs_set == {OLD_DRIVER, OLD_BINARY}, 'new candidate replaces only artifact pair')
    for driver, binary, recovery in ((OLD_DRIVER, OLD_BINARY, True), (DRIVER, BINARY, False), (OLD_DRIVER, BINARY, True), (DRIVER, OLD_BINARY, True)):
        try:
            supervisor.secret_storage_inputs(driver, binary, recovery)
        except ValueError:
            require(True, 'cross-bound artifact pair rejected')
        else:
            require(False, 'cross-bound artifact pair accepted')

    # Original main runs against controlled process/metadata/TCP observations.
    # No actual prctl, waitpid, signal registration, Popen or socket runs here.
    with tempfile.TemporaryDirectory(dir=OUTPUT, prefix='recovery-entry-controls-') as temporary:
        base = Path(temporary)
        for selector, names in EXPECTED.items():
            original = ''.join(f'=== RUN   {name}\n--- PASS: {name} (0.01s)\n' for name in sorted(names))
            first = sorted(name for name in names if '/' in name)[0]
            variants = {
                'pass': original,
                'missing-subcase': original.replace(f'=== RUN   {first}\n', '', 1),
                'duplicate': original + f'=== RUN   {first}\n--- PASS: {first} (0.01s)\n',
                'skip': original.replace(f'--- PASS: {first}', f'--- SKIP: {first}', 1),
                'fail': original.replace(f'--- PASS: {first}', f'--- FAIL: {first}', 1),
                'extra': original + '=== RUN   TestExtra\n--- PASS: TestExtra (0.01s)\n',
                'source-change': original,
            }
            for name, log in variants.items():
                mutated = False
                invocation = []

                class Child:
                    pid = 987654
                    returncode = None

                    def __init__(self, argv, stdout, stderr):
                        invocation.append(argv)
                        stdout.write(log)

                    def wait(self, timeout):
                        global mutated
                        require(timeout == 123, 'actual main original wait budget')
                        self.returncode = 0
                        mutated = name == 'source-change'
                        return 0

                argv = ['review', '--driver', str(DRIVER), '--binary', str(BINARY), '--run', selector, '--output', str(base / (str(len(names)) + '-' + name))]
                library = type('ControlledPrctl', (), {'prctl': staticmethod(lambda *args: 0)})()
                with patch.object(sys, 'argv', argv), patch.object(supervisor.ctypes, 'CDLL', return_value=library), patch.object(supervisor, 'tcp', return_value=set()), patch.object(supervisor, 'descendants', return_value=set()), patch.object(supervisor.subprocess, 'Popen', Child), patch.object(supervisor.os, 'waitpid', side_effect=ChildProcessError), patch.object(supervisor.signal, 'signal'), patch.object(supervisor.time, 'sleep'), contextlib.redirect_stdout(io.StringIO()):
                    code = supervisor.main()
                require(code == (0 if name == 'pass' else 1), 'actual controlled main ' + name)
                require(invocation == [[str(DRIVER), '--test-binary', str(BINARY), '--run', selector, '--directory', invocation[0][-1]]], 'actual fixed invocation mapping')

        # These fail in the actual public parser before mkdir or resource work.
        for selector, root_chain in ((WRITE + '/', False), (WRITE[1:], False), ('^TestSecretVariableStorageSQL.*$', False), ('^TestSecretVariableStorageSQLCommitUnknown$', False), (STATE.replace('Concurrency', 'ClosedConstraints'), False), (WRITE, True)):
            argv = ['review', '--driver', str(DRIVER), '--binary', str(BINARY), '--run', selector, '--output', str(base / 'never-created')]
            if root_chain:
                argv.append('--root-chain')
            with patch.object(sys, 'argv', argv), patch.object(Path, 'mkdir', side_effect=AssertionError('unexpected mkdir')), contextlib.redirect_stderr(io.StringIO()):
                try:
                    supervisor.main()
                except SystemExit as stopped:
                    require(stopped.code == 2, 'bad selector/root mode rejected before resources')
                else:
                    require(False, 'bad selector accepted')
mutated = False

parser = argparse.ArgumentParser()
parser.add_argument('--artifacts', action='store_true')
args = parser.parse_args()
if args.artifacts:
    supervisor.secret_storage_inputs(DRIVER, BINARY, True)  # Actual ordinary artifacts, no metadata stand-in.
    for selector, expected in EXPECTED.items():
        result = subprocess.run([str(BINARY), '-test.list', selector], capture_output=True, text=True, timeout=10)
        require(result.returncode == 0 and set(result.stdout.splitlines()) == {name for name in expected if '/' not in name}, 'actual recovery top listing')
    for selector in (WRITE + '/', WRITE[1:], '^TestSecretVariableStorageSQL.*$', '^TestSecretVariableStorageSQLCommitUnknown$', STATE.replace('Concurrency', 'ClosedConstraints')):
        result = subprocess.run([str(DRIVER), '--test-binary', str(BINARY), '--run', selector, '--directory', '/nonexistent-secret-recovery-control-parent/never-created'], capture_output=True, text=True, timeout=5)
        require(result.returncode != 0 and 'fresh disk' not in result.stderr, 'actual driver rejects before stat/mkdir')
print(f'{checks} recovery entry checks passed; actual_main=controlled; artifacts={args.artifacts}; resources=0')
