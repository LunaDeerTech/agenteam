#!/usr/bin/env python3
"""Offline source and controlled-main checks; no PG, socket or process fixture.

Only --artifacts executes the separately built test for exact listing and the
real driver for invalid selectors rejected before stat/mkdir. Process, proc,
TCP and artifact stand-ins below are explicit and do not prove retirement.
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
OUTPUT = ROOT / 'output/ai/secret-variable-owner-service'
DRIVER, BINARY = OUTPUT / 'pg-only-owner-driver', OUTPUT / 'secret-variable-owner.test'
ARTIFACTS = {DRIVER, BINARY}
spec = importlib.util.spec_from_file_location('secret_owner_supervisor_control', SOURCE)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
EXPECTED = {
    '^TestSecretVariableOwner(Persistence|CurrentAuthority)$': {
        'TestSecretVariableOwnerPersistence', 'TestSecretVariableOwnerCurrentAuthority',
        'TestSecretVariableOwnerCurrentAuthority/current-owner-and-cross-project',
        'TestSecretVariableOwnerCurrentAuthority/current-owner-loss-hides-original-history',
        'TestSecretVariableOwnerCurrentAuthority/real-archive-after-prepare-rechecks-final-gate',
        'TestSecretVariableOwnerCurrentAuthority/revoked-current-session-before-safe-history'},
    '^TestSecretVariableOwner(AtomicFacts|Concurrency)$': {
        'TestSecretVariableOwnerAtomicFacts', 'TestSecretVariableOwnerConcurrency',
        'TestSecretVariableOwnerAtomicFacts/after-d10-audit', 'TestSecretVariableOwnerAtomicFacts/after-outbox',
        'TestSecretVariableOwnerAtomicFacts/after-activity', 'TestSecretVariableOwnerAtomicFacts/owner-tail',
        'TestSecretVariableOwnerConcurrency/same-key-original-intent',
        'TestSecretVariableOwnerConcurrency/same-key-other-value',
        'TestSecretVariableOwnerConcurrency/two-keys-update-delete-version',
        'TestSecretVariableOwnerConcurrency/ordinary-secret-name'},
    '^TestSecretVariableOwnerCommitRecovery$': {
        'TestSecretVariableOwnerCommitRecovery', 'TestSecretVariableOwnerCommitRecovery/before-forward',
        'TestSecretVariableOwnerCommitRecovery/after-forward',
        'TestSecretVariableOwnerCommitRecovery/pending-outlives-confirmation',
        'TestSecretVariableOwnerCommitRecovery/stop-confirms-actual-join'},
    '^TestSecretVariableOwnerMigration$': {
        'TestSecretVariableOwnerMigration', 'TestSecretVariableOwnerMigration/empty-repeat-and-exact-new-schema',
        'TestSecretVariableOwnerMigration/ordinary-stored-facts-survive-upgrade',
        'TestSecretVariableOwnerMigration/actual-closed-checks-and-deferred-history',
        *('TestSecretVariableOwnerMigration/actual-closed-checks-and-deferred-history/' + name for name in
          ('secret-plaintext', 'secret-null-internal-version', 'ordinary-null-value',
           'completed-without-audit', 'audit-extra-material-field'))},
}
checks = 0


def require(value, label):
    global checks
    assert value, label
    checks += 1


def old(path):
    return subprocess.run(['git', 'show', '26db9680:' + path], cwd=ROOT,
                          text=True, capture_output=True, check=True).stdout


# Whole old driver bytes and supervisor AST inverse; no selected-line-only claim.
class RemoveOwner(ast.NodeTransformer):
    def visit_Import(self, node):
        return None if len(node.names) == 1 and node.names[0].name == 'stat' else node

    def visit_Assign(self, node):
        if any(isinstance(t, ast.Name) and t.id in ('SECRET_OWNER_CASES', 'secret_owner') for t in node.targets):
            return None
        return self.generic_visit(node)

    def visit_FunctionDef(self, node):
        return None if node.name in ('secret_owner_inputs', 'observe_secret_owner') else self.generic_visit(node)

    def visit_If(self, node):
        names = {p.id for p in ast.walk(node.test) if isinstance(p, ast.Name)}
        if names & {'secret_owner', 'SECRET_OWNER_CASES'}:
            return [self.visit(p) for p in node.orelse] if node.orelse else None
        return self.generic_visit(node)


require(ast.dump(RemoveOwner().visit(ast.parse(SOURCE.read_text())), include_attributes=False)
        == ast.dump(ast.parse(old('.agent-state/task-planning-recovery/pg_only_supervisor.py')), include_attributes=False),
        'entire old supervisor AST unchanged')
driver_source = DRIVER_SOURCE.read_text()
inverse = re.sub(r'^const secretOwner\w+Selector = `[^`]+`\n', '', driver_source, flags=re.M)
inverse = re.sub(r'\tif strings.Contains\(\*selector, "SecretVariableOwner"\)[^\n]+\{\n\t\treturn fail\("Secret Owner requires one exact PG-only group"\)\n\t}\n', '', inverse)
for name in ('Read', 'Atomic'):
    inverse = inverse.replace(' && *selector != secretOwner' + name + 'Selector', '')
require(inverse == old('.agent-state/task-planning-recovery/pg_only_driver.go'), 'entire old driver bytes unchanged')
require(sup.SECRET_OWNER_CASES == EXPECTED, 'independent exact cases')
require([len(v) for v in EXPECTED.values()] == [6, 10, 5, 9], 'full node counts')
for name, selector in zip(('Read', 'Atomic', 'Recovery', 'Migration'), EXPECTED):
    require(f'const secretOwner{name}Selector = `{selector}`' in driver_source, 'driver exact selector ' + name)
require(sup.budgets(False) == (123, 3) and sup.budgets(True) == (540, 60), 'unchanged budgets')

real_resolve, real_stat, real_read = Path.resolve, Path.stat, Path.read_bytes
mutation = None
mutation_target = ROOT / 'tests/projectvariable/secret_recovery_test.go'


def resolve(path, *args, **kwargs):
    return path if path in ARTIFACTS else real_resolve(path, *args, **kwargs)


def file_stat(path, *args, **kwargs):
    if path in ARTIFACTS:
        return type('ControlledRegularFile', (), {'st_mode': stat.S_IFREG | 0o600})()
    return real_stat(path, *args, **kwargs)


def read(path):
    if path in ARTIFACTS:
        return b'controlled artifact only; not a real build'
    if path == mutation_target:
        if mutation == 'source-change':
            return b'controlled changed source'
        if mutation == 'source-missing':
            raise FileNotFoundError('controlled missing source')
    return real_read(path)


with patch.object(Path, 'resolve', resolve), patch.object(Path, 'stat', file_stat), patch.object(Path, 'read_bytes', read):
    inputs = set(sup.secret_owner_inputs(DRIVER, BINARY))
    required = {'secret_fixture_test.go', 'secret_persistence_test.go', 'secret_authority_test.go',
                'secret_atomicity_test.go', 'secret_concurrency_test.go', 'secret_recovery_test.go',
                'secret_migration_test.go', 'fixture_test.go', 'recovery_test.go', 'concurrency_test.go'}
    require({ROOT / 'tests/projectvariable' / p for p in required} <= inputs, 'real package helpers and physical proxy included')
    require(ROOT / 'db/migrations/00030_project_secret_variables.sql' in inputs
            and ROOT / 'internal/central/account/assets/weak-passwords.json' in inputs, 'actual embedded data inputs')
    # Independently follow local production imports from every compiled package
    # test/helper. This discovers missing source dependencies without using the
    # supervisor's hand-maintained package list as its own expected value.
    pending, seen, closure = ['tests/projectvariable', 'tests/testsupport/postgres'], set(), set()
    while pending:
        package = pending.pop()
        if package in seen:
            continue
        seen.add(package)
        for path in (ROOT / package).glob('*.go'):
            if package != 'tests/projectvariable' and path.name.endswith('_test.go'):
                continue
            closure.add(path)
            pending.extend(re.findall(r'"github.com/LunaDeerTech/agenteam/([^"\s]+)"', path.read_text()))
    require(closure <= inputs, 'actual local import closure covered')
    for driver, binary in ((DRIVER.with_name('other'), BINARY), (DRIVER, BINARY.with_name('other')),
                           (Path(str(DRIVER) + '/..'), BINARY)):
        try:
            sup.secret_owner_inputs(driver, binary)
        except ValueError:
            require(True, 'wrong artifact pair rejected')
        else:
            require(False, 'wrong artifact pair accepted')
    for kind in ('symlink', 'directory'):
        def bad_resolve(path, *args, **kwargs):
            return mutation_target.parent / 'elsewhere' if path == mutation_target and kind == 'symlink' else resolve(path, *args, **kwargs)
        def bad_stat(path, *args, **kwargs):
            return type('ControlledDirectory', (), {'st_mode': stat.S_IFDIR | 0o700})() if path == mutation_target and kind == 'directory' else file_stat(path, *args, **kwargs)
        with patch.object(Path, 'resolve', bad_resolve), patch.object(Path, 'stat', bad_stat):
            try:
                sup.secret_owner_inputs(DRIVER, BINARY)
            except ValueError:
                require(True, kind + ' input rejected')
            else:
                require(False, kind + ' input accepted')

    with tempfile.TemporaryDirectory(dir=OUTPUT, prefix='owner-entry-controls-') as temporary:
        base = Path(temporary)
        for number, (selector, names) in enumerate(EXPECTED.items()):
            original = ''.join(f'=== RUN   {name}\n--- PASS: {name} (0.01s)\n' for name in sorted(names))
            first = sorted(name for name in names if '/' in name)[0]
            variants = {'pass': original,
                        'missing-subcase': original.replace(f'=== RUN   {first}\n', '', 1),
                        'duplicate': original + f'=== RUN   {first}\n--- PASS: {first} (0.01s)\n',
                        'skip': original.replace(f'--- PASS: {first}', f'--- SKIP: {first}', 1),
                        'fail': original.replace(f'--- PASS: {first}', f'--- FAIL: {first}', 1),
                        'extra': original + '=== RUN   TestExtra\n--- PASS: TestExtra (0.01s)\n',
                        'bad-utf8': original, 'source-change': original, 'source-missing': original,
                        'driver-fail': original}
            for name, log in variants.items():
                mutation = None
                invocation = []
                class Child:
                    pid, returncode = 987654, None
                    def __init__(self, argv, stdout, stderr):
                        invocation.append(argv)
                        stdout.write(log)
                        if name == 'bad-utf8':
                            stdout.flush()
                            stdout.buffer.write(b'\xff')
                            stdout.buffer.flush()
                    def wait(self, timeout):
                        global mutation
                        require(timeout == 123, 'original main Wait budget')
                        mutation = name
                        self.returncode = 2 if name == 'driver-fail' else 0
                        return self.returncode
                argv = ['control', '--driver', str(DRIVER), '--binary', str(BINARY), '--run', selector,
                        '--output', str(base / (str(number) + '-' + name))]
                library = type('ControlledPrctl', (), {'prctl': staticmethod(lambda *args: 0)})()
                with patch.object(sys, 'argv', argv), patch.object(sup.ctypes, 'CDLL', return_value=library), patch.object(sup, 'tcp', return_value=set()), patch.object(sup, 'descendants', return_value=set()), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup.os, 'waitpid', side_effect=ChildProcessError), patch.object(sup.signal, 'signal'), patch.object(sup.time, 'sleep'), contextlib.redirect_stdout(io.StringIO()):
                    code = sup.main()
                require(code == (0 if name == 'pass' else 2 if name == 'driver-fail' else 1), 'controlled actual main ' + name)
                require(invocation == [[str(DRIVER), '--test-binary', str(BINARY), '--run', selector, '--directory', invocation[0][-1]]], 'unchanged invocation')
                saved = next((base / (str(number) + '-' + name)).glob('*.log')).read_text(errors='replace')
                require('actual_driver_wait pid=987654 actual=True' in saved and 'runtime_observation=2 descendants=[]' in saved and 'HOST_TCP delta_empty_observation=2' in saved and 'terminal=' + str(code) in saved, 'actual main reaches original terminal observations')
        for selector, root_chain in ((next(iter(EXPECTED)) + '/', False), ('TestSecretVariableOwnerMigration$', False),
                                     ('^TestSecretVariableOwner.*$', False), ('^TestSecretVariableOwnerPersistence$', False),
                                     ('^TestSecretVariableOwnerMigration$', True)):
            argv = ['control', '--driver', str(DRIVER), '--binary', str(BINARY), '--run', selector, '--output', str(base / 'never-created')]
            if root_chain:
                argv.append('--root-chain')
            with patch.object(sys, 'argv', argv), patch.object(Path, 'mkdir', side_effect=AssertionError('unexpected mkdir')), contextlib.redirect_stderr(io.StringIO()):
                try:
                    sup.main()
                except SystemExit as stopped:
                    require(stopped.code == 2, 'bad selector rejected before resources')
                else:
                    require(False, 'bad selector accepted')
mutation = None
parser = argparse.ArgumentParser()
parser.add_argument('--artifacts', action='store_true')
args = parser.parse_args()
if args.artifacts:
    sup.secret_owner_inputs(DRIVER, BINARY)
    for selector, expected in EXPECTED.items():
        result = subprocess.run([str(BINARY), '-test.list', selector], capture_output=True, text=True, timeout=10)
        require(result.returncode == 0 and set(result.stdout.splitlines()) == {p for p in expected if '/' not in p}, 'actual binary exact listing')
    for selector in ('^TestSecretVariableOwner.*$', '^TestSecretVariableOwnerPersistence$', '^TestSecretVariableOwnerMigration$/'):
        result = subprocess.run([str(DRIVER), '--test-binary', str(BINARY), '--run', selector,
                                 '--directory', '/nonexistent-secret-owner-control-parent/run'], capture_output=True, text=True, timeout=5)
        require(result.returncode != 0 and 'fresh disk' not in result.stderr, 'actual driver rejects before stat/mkdir')
print(f'{checks} Owner entry checks passed; main=controlled; artifacts={args.artifacts}; resources=0')
