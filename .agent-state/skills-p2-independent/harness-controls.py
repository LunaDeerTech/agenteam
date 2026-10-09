#!/usr/bin/env python3
"""Exercise the exact P2 root adapter offline; no process, Docker or TCP calls."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DRIVER = ROOT / '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestSkillIndependentP2ConfirmationAndPackage$'
TOP = 'TestSkillIndependentP2ConfirmationAndPackage'
BASE = '29b252c7'
FILES = {'admission_unknown_test.go', 'commit_recovery_test.go', 'fixture_test.go',
         'initialization_test.go', 'lifecycle_stop_test.go', 'migration_test.go',
         'object_publication_test.go', 'owner_read_test.go', 'p2_independent_test.go'}
SUBS = ('same_store_confirmation_and_private_plan',
        'original_plan_rechecks_current_project_gate',
        'owner_package_current_session_and_foreign_denials',
        'original_d05_read_close_returns_and_skill_accounting')
checks = []


def check(label, condition):
    assert condition, label
    checks.append(label)


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def baseline(path):
    return subprocess.check_output(
        ['git', 'show', BASE + ':' + str(path.relative_to(ROOT))], cwd=ROOT).decode()


driver = load(DRIVER, 'independent_p2_driver')
supervisor = load(SUPERVISOR, 'independent_p2_supervisor')
old_driver = types.ModuleType('independent_p2_old_driver')
old_driver.__file__ = str(DRIVER)
exec(compile(baseline(DRIVER), str(DRIVER), 'exec'), old_driver.__dict__)
check('only one new exact target', driver.TARGETS == {
    **old_driver.TARGETS, SELECTOR: 'tests/skills'})
check('root and PG budgets unchanged', supervisor.budgets(True) == (540, 60)
      and supervisor.budgets(False) == (123, 3))

# Mechanical inverse comparisons preserve every pre-existing branch, including
# timeout/reap/TCP behavior. These are configuration checks, not real tail proof.
source = DRIVER.read_text()
source = source.replace("    '" + SELECTOR + "': 'tests/skills',\n", '', 1)
source = source.replace('def input_paths(binary, selector=None):', 'def input_paths(binary):', 1)
start = source.index("    if selector == '" + SELECTOR + "':\n")
end = source.index('    return sorted(paths)', start)
source = source[:start] + source[end:]
check('driver inverse byte equal', source == baseline(DRIVER))
source = SUPERVISOR.read_text().replace(
    "        '" + SELECTOR + "': {'" + TOP + "'},\n", '', 1)
source = source.replace(
    "        paths = (adapter.input_paths(args.binary, args.run)\n"
    "                 if args.run == '" + SELECTOR + "' else adapter.input_paths(args.binary))\n"
    "        inputs = {str(p): adapter.sha(p) for p in paths}\n",
    "        inputs = {str(p): adapter.sha(p) for p in adapter.input_paths(args.binary)}\n", 1)
check('supervisor inverse byte equal', source == baseline(SUPERVISOR))

output = ROOT / 'output/ai/skills-p2-independent/harness-controls'
output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='offline-', dir=output) as temporary:
    directory = Path(temporary)
    binary = directory / 'unexecuted.test'
    binary.write_text('never executed\n')
    binary.chmod(0o700)
    original = set(old_driver.input_paths(binary))
    check('default closure identical', set(driver.input_paths(binary)) == original)
    for selector in old_driver.TARGETS:
        check('old closure ' + selector,
              set(driver.input_paths(binary, selector)) == original)
    added = {ROOT / 'tests/skills' / name for name in FILES}
    check('exact complete package closure',
          set(driver.input_paths(binary, SELECTOR)) == original | added
          and {p.name for p in (ROOT / 'tests/skills').glob('*.go')} == FILES
          and all(p.is_file() and not p.is_symlink() for p in added))
    test_source = (ROOT / 'tests/skills/p2_independent_test.go').read_text()
    check('fixed four subcase source present',
          all('t.Run("' + sub + '"' in test_source for sub in SUBS)
          and test_source.count('t.Run(') == 4)

    # Only verification of installed tool/dependency identity is doubled here;
    # production configuration validates paths and selector without execution.
    with patch.object(driver, 'GO', binary), \
         patch.object(driver, 'sha', return_value=driver.MINIO_SHA):
        for selector in [*old_driver.TARGETS, SELECTOR]:
            fresh = directory / ('config-' + str(len(checks)))
            plan = driver.configuration(binary, selector, fresh)
            check('configuration ' + selector,
                  plan['cwd'] == str(ROOT / driver.TARGETS[selector])
                  and plan['test_timeout'] == '6m' and plan['resources'] == 7
                  and plan['runtime'] == str(fresh / 'runtime')
                  and not fresh.exists())
        for bad in (SELECTOR[:-1], SELECTOR[1:], '^TestSkillIndependentP2.*$',
                    SELECTOR + '/same_store_confirmation_and_private_plan',
                    '^TestSkillIndependentP2ConfirmationAndPackageExtra$',
                    '^TestSkillIndependentP2ConfirmationAndPackage|TestSkillMigration$'):
            try:
                driver.configuration(binary, bad, directory / 'bad')
            except ValueError:
                check('selector rejected ' + bad, not (directory / 'bad').exists())
            else:
                raise AssertionError('broad selector accepted')
        for invalid in (directory, directory / 'link'):
            if invalid != directory:
                invalid.symlink_to(directory / 'absent')
            try:
                driver.configuration(binary, SELECTOR, invalid)
            except ValueError:
                check('fresh directory rejected ' + invalid.name, True)
            else:
                raise AssertionError('used directory accepted')

    # Invoke the actual supervisor main/record decoder/observer. Substitute only
    # external resource boundaries: fake already-waited child, exact Docker
    # absence, process table and TCP. Never call real exec/kill/prctl/network.
    cases = (
        ('exact', [TOP], SELECTOR, 0, False, False, False, 0),
        ('missing', [], SELECTOR, 0, False, False, False, 1),
        ('extra', [TOP, 'TestSkillMigration'], SELECTOR, 0, False, False, False, 1),
        ('wrong-wait', [TOP], '^TestSkillMigration$', 0, False, False, False, 1),
        ('child-failed', [TOP], SELECTOR, 1, False, False, False, 1),
        ('input-changed', [TOP], SELECTOR, 0, True, False, False, 1),
        ('resource-present', [TOP], SELECTOR, 0, False, True, False, 1),
        ('runtime-present', [TOP], SELECTOR, 0, False, False, True, 1),
        ('legacy', ['TestSkillObjectInitializationPublication'],
         '^TestSkillObjectInitializationPublication$', 0, False, False, False, 0),
    )
    for name, tops, waited, child_code, mutate, resource_live, runtime_live, expected in cases:
        selector = waited if name == 'legacy' else SELECTOR
        run_output = directory / name
        calls = {'wait': [], 'tcp': 0, 'absent': 0, 'paths': [], 'sha': {}}
        actual_paths = driver.input_paths

        def paths(*args):
            calls['paths'].append(args)
            return actual_paths(*args)

        def sha(path):
            key = str(path)
            count = calls['sha'].get(key, 0)
            calls['sha'][key] = count + 1
            return 'changed' if mutate and count > 0 and key.endswith('/p2_independent_test.go') else 'fixed'

        class Child:
            pid = 424242
            returncode = None

            def __init__(self, argv, stdout, stderr):
                check('exact child argv ' + name,
                      argv[:-1] == [str(DRIVER), '--test-binary', str(binary),
                                    '--run', selector, '--directory'])
                owned = Path(argv[-1])
                runtime = owned / 'runtime'
                runtime.mkdir(parents=True)
                if runtime_live:
                    (runtime / 'still-present').write_text('owned test marker')
                resources = []
                for label, kinds in (
                    ('agenteam.d05.objectfixture', ('container', 'network')),
                    ('agenteam.d04.networkfixture', ('container', 'network')),
                    ('agenteam.d03.fixture', ('container', 'container', 'network'))):
                    for kind in kinds:
                        resources.append({'kind': kind, 'label': label,
                                          'id': f'{len(resources) + 1:064x}', 'nonce': 'a' * 32})
                manifest = owned / 'owned.json'
                manifest.write_text(json.dumps({'kind': 'work-owner-root-chain',
                    'resources': resources,
                    'directories': [str(runtime / ('retired-' + str(i))) for i in range(3)]}))
                manifest.chmod(0o600)
                stdout.write(''.join('=== RUN   ' + top + '\n' for top in tops))
                if name == 'exact':
                    stdout.write(''.join('=== RUN   ' + TOP + '/' + sub + '\n' for sub in SUBS))
                stdout.write(f'D03 explicit test actual_wait pid=424241 code={child_code} selector={waited}\n')
                stdout.flush()

            def wait(self, timeout):
                calls['wait'].append(timeout)
                self.returncode = child_code
                return child_code

        class Libc:
            def prctl(self, *args):
                return 0

        def tcp():
            calls['tcp'] += 1
            return set()

        def absent(item, timeout):
            calls['absent'] += 1
            assert 0 < timeout <= 3
            return not resource_live

        argv = ['supervisor', '--root-chain', '--driver', str(DRIVER),
                '--binary', str(binary), '--run', selector, '--output', str(run_output)]
        with patch.object(sys, 'argv', argv), \
             patch.object(supervisor, 'root_adapter', return_value=driver), \
             patch.object(driver, 'input_paths', paths), patch.object(driver, 'sha', sha), \
             patch.object(supervisor.ctypes, 'CDLL', return_value=Libc()), \
             patch.object(supervisor.subprocess, 'Popen', Child), \
             patch.object(supervisor, 'descendants', return_value=set()), \
             patch.object(supervisor.os, 'waitpid', side_effect=ChildProcessError), \
             patch.object(supervisor.os, 'kill', side_effect=AssertionError('unexpected kill')), \
             patch.object(supervisor, 'tcp', tcp), \
             patch.object(supervisor, 'exact_absent', absent), \
             patch.object(supervisor.time, 'sleep', return_value=None), \
             contextlib.redirect_stdout(io.StringIO()):
            code = supervisor.main()
        log = next(run_output.glob('*.log')).read_text()
        check('original full tail ' + name,
              code == expected and calls['wait'] == [540]
              and calls['tcp'] == 3 and calls['absent'] == 14
              and calls['paths'] == ([(binary,)] if name == 'legacy' else [(binary, SELECTOR)])
              and f'actual_driver_wait pid=424242 actual=True code={child_code}' in log
              and log.count('ROOT resource_observation=') == 14
              and log.count('ROOT private_observation=') == 2
              and log.count('ROOT runtime_observation=') == 2
              and log.count('OWNED runtime_observation=') == 2
              and 'HOST_TCP delta_empty_observation=1' in log
              and 'HOST_TCP delta_empty_observation=2' in log
              and f'inputs_unchanged={not mutate} terminal={expected}' in log)

print(json.dumps({'passed': len(checks), 'controls': checks}))
