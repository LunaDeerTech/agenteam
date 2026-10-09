#!/usr/bin/env python3
"""Offline controls: no supervisor main, /proc sampling, sockets or Docker."""
import ast
import importlib.util
import io
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('secret_storage_supervisor_controls', SOURCE)
supervisor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(supervisor)
OUTPUT = ROOT / 'output/ai/secret-variable-storage'
checks = 0


def require(value):
    global checks
    assert value
    checks += 1


require(supervisor.budgets(False) == (123, 3))
require(supervisor.budgets(True) == (540, 60))
require(len(supervisor.SECRET_STORAGE_CASES[supervisor.SECRET_STORAGE_CORE]) == 17)
require(len(supervisor.SECRET_STORAGE_CASES[supervisor.SECRET_STORAGE_MAINTENANCE]) == 1)
with tempfile.TemporaryDirectory(dir=OUTPUT, prefix='entry-controls-') as directory:
    directory = Path(directory)
    log = directory / 'controlled.log'
    for selector, expected in supervisor.SECRET_STORAGE_CASES.items():
        original = ''.join(f'=== RUN   {name}\n--- PASS: {name} (0.01s)\n' for name in sorted(expected))
        first = sorted(expected)[0]
        cases = {
            'valid': original,
            'missing-run': original.replace(f'=== RUN   {first}\n', '', 1),
            'missing-pass': original.replace(f'--- PASS: {first} (0.01s)\n', '', 1),
            'skip': original.replace(f'--- PASS: {first}', f'--- SKIP: {first}', 1),
            'fail': original.replace(f'--- PASS: {first}', f'--- FAIL: {first}', 1),
            'duplicate': original + f'=== RUN   {first}\n--- PASS: {first} (0.01s)\n',
            'extra': original + '=== RUN   TestUnexpected\n--- PASS: TestUnexpected (0.01s)\n',
        }
        for name, raw in cases.items():
            log.write_text(raw)
            require(supervisor.observe_secret_storage(log, io.StringIO(), selector) == (name == 'valid'))
    require(not supervisor.observe_secret_storage(directory / 'missing.log', io.StringIO(), supervisor.SECRET_STORAGE_CORE))

driver = OUTPUT / 'pg-only-driver'
binary = OUTPUT / 'secret-variable-storage-sql-reviewed.test'
inputs = supervisor.secret_storage_inputs(driver, binary)
require(len(inputs) == len(set(inputs)))
for path in (SOURCE, driver, binary, ROOT / 'db/migrations/00029_project_variable_receipts.sql',
             ROOT / 'tests/security/secret_variable_storage_fixture_test.go',
             ROOT / 'tests/security/secret_variable_storage_test.go',
             ROOT / 'tests/security/secret_variable_storage_maintenance_test.go'):
    require(path in inputs)
for changed_driver, changed_binary in ((Path('/tmp/other-driver'), binary), (driver, OUTPUT / 'secret-variable-storage-sql.test')):
    try:
        supervisor.secret_storage_inputs(changed_driver, changed_binary)
    except ValueError:
        require(True)
    else:
        require(False)

# Actual binary rejects these before statvfs/mkdir/certificates/Docker.
# Use a nonexistent parent so a regression cannot create a resource fixture.
for selector in (
    '^TestSecretVariableStorageSQL.*$',
    '^TestSecretVariableStorageSQLReplayAndEffects$',
    '^TestSecretVariableStorageSQLBogus$',
    supervisor.SECRET_STORAGE_CORE + '/',
    supervisor.SECRET_STORAGE_CORE[1:],
):
    result = subprocess.run([str(driver), '--test-binary', str(binary), '--run', selector,
                             '--directory', '/nonexistent-secret-control-parent/never-created'],
                            capture_output=True, text=True, timeout=5)
    require(result.returncode != 0 and 'fresh disk' not in result.stderr)

for selector, wanted in (
    (supervisor.SECRET_STORAGE_CORE, {'TestSecretVariableStorageSQLReplayAndEffects', 'TestSecretVariableStorageSQLAtomicAuditAndOwnerRollback', 'TestSecretVariableStorageSQLClosedConstraints'}),
    (supervisor.SECRET_STORAGE_MAINTENANCE, {'TestSecretVariableStorageSQLRotationDeletedOwnerAndCleanup'}),
):
    result = subprocess.run([str(binary), '-test.list', selector], capture_output=True, text=True, timeout=10)
    require(result.returncode == 0 and set(result.stdout.splitlines()) == wanted)

# The original owning lifecycle/budgets are intentionally not reimplemented.
source = SOURCE.read_text()
driver_source = (ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go').read_text()
for text in ('105*time.Second', '15*time.Second', '120*time.Second', '"-test.timeout=6m"', 'err = cmd.Wait()', 'for round := 1; round <= 2; round++'):
    require(text in driver_source)
require('tail_deadline = time.monotonic() + 75' in source)
require('code = child.wait(timeout=driver_timeout)' in source)
require('network", "create' in driver_source and '"run", "--detach"' in driver_source)
require('objectfixture' not in driver_source and 'netfixture' not in driver_source)
require(re.search(r'const secretVariableStorageCoreSelector = `([^`]+)`', driver_source).group(1) == supervisor.SECRET_STORAGE_CORE)
require(re.search(r'const secretVariableStorageMaintenanceSelector = `([^`]+)`', driver_source).group(1) == supervisor.SECRET_STORAGE_MAINTENANCE)

baseline = 'fde3ecb5'
old_supervisor = subprocess.run(['git', 'show', baseline + ':.agent-state/task-planning-recovery/pg_only_supervisor.py'], cwd=ROOT, capture_output=True, text=True, check=True).stdout


class RemoveStorageExtension(ast.NodeTransformer):
    def visit_FunctionDef(self, node):
        if node.name in ('secret_storage_inputs', 'observe_secret_storage'):
            return None
        return self.generic_visit(node)

    def visit_Assign(self, node):
        if any(isinstance(target, ast.Name) and (target.id.startswith('SECRET_STORAGE_') or target.id == 'secret_storage') for target in node.targets):
            return None
        return self.generic_visit(node)

    def visit_Import(self, node):
        if len(node.names) == 1 and node.names[0].name == 'stat':
            return None
        return node

    def visit_If(self, node):
        if any(isinstance(value, ast.Name) and value.id == 'secret_storage' for value in ast.walk(node.test)):
            return [self.visit(value) for value in node.orelse]
        if any(isinstance(value, ast.Constant) and value.value == 'SecretVariableStorage' for value in ast.walk(node.test)):
            return None
        return self.generic_visit(node)


require(ast.dump(RemoveStorageExtension().visit(ast.parse(source)), include_attributes=False)
        == ast.dump(ast.parse(old_supervisor), include_attributes=False))
old_driver = subprocess.run(['git', 'show', baseline + ':.agent-state/task-planning-recovery/pg_only_driver.go'], cwd=ROOT, capture_output=True, text=True, check=True).stdout
inverse = re.sub(r'^const secretVariableStorage(?:Core|Maintenance)Selector = `[^`]+`\n', '', driver_source, flags=re.M)
inverse = inverse.replace('\tif strings.Contains(*selector, "SecretVariableStorage") && *selector != secretVariableStorageCoreSelector && *selector != secretVariableStorageMaintenanceSelector {\n\t\treturn fail("Secret storage requires an exact core or maintenance group")\n\t}\n', '')
inverse = inverse.replace(' && *selector != secretVariableStorageCoreSelector', '')
require(inverse == old_driver)
print(f'{checks} offline entry assertions passed; resources=0')
