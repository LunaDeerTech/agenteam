#!/usr/bin/env python3
"""Offline controls: actual adapter/supervisor, explicit process/OS doubles.

No Go, PG, Docker, sockets or child test is started. Git is used read-only for
inverse comparison against the fixed pre-increment tools.
"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = ROOT / '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BASE = '58073b6768f6192aa2506323b46b4a9a1d687739'
projection_path = ROOT / '.agent-state/owner-feature-integration/entry_union.py'
projection_spec = importlib.util.spec_from_file_location('owner_entry_union', projection_path)
projection = importlib.util.module_from_spec(projection_spec)
projection_spec.loader.exec_module(projection)
SELECTOR = '^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$'
PERSIST = 'TestSkillLifecycleCleanupPersistence'
RECOVER = 'TestSkillLifecycleCleanupCommitRecovery'
HISTORY = 'TestSkillLifecycleCleanupHistoricalAttempts'
HISTORY_SELECTOR = '^' + HISTORY + '$'
NODES = [PERSIST, PERSIST + '/current_gate_before_irreversible_release',
         PERSIST + '/actual_physical_audit_and_bounded_history',
         PERSIST + '/last_object_and_skill_anchors_share_original_transaction',
         RECOVER, RECOVER + '/gate', RECOVER + '/last_two_domain_anchors']
HISTORY_NODES = [HISTORY, HISTORY + '/native_retry_preserves_abandoned_cause',
                 HISTORY + '/seeded_retained_mapping_history_batches_and_fk_rollback']
INPUTS = {'tests/skills/' + n for n in (
    'fixture_test.go', 'object_publication_test.go', 'lifecycle_stop_test.go',
    'owner_read_test.go', 'commit_recovery_test.go',
    'lifecycle_cleanup_fixture_test.go', 'lifecycle_cleanup_test.go',
    'lifecycle_cleanup_unknown_test.go', 'lifecycle_cleanup_history_test.go',
    'lifecycle_cleanup_history_proxy_test.go')}
INPUTS.add('.agent-state/project-variables-independent/commitproxy/proxy.go')


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def prior(path):
    return projection.baseline_for(path, 'skills_cleanup')


def prior_module(path, name):
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(prior(path), str(path), 'exec'), module.__dict__)
    return module


def output(nodes=NODES, selector=SELECTOR):
    return ''.join('=== RUN   ' + n + '\n' for n in nodes) + ''.join(
        '--- PASS: ' + n + ' (0.01s)\n' for n in nodes) + (
        'D03 explicit test actual_wait pid=321 code=0 selector=' + selector + '\n')


class Controls(unittest.TestCase):
    def setUp(self):
        self.driver = load(DRIVER, 'cleanup_driver')
        self.sup = load(SUPERVISOR, 'cleanup_supervisor')
        base = ROOT / 'output/ai/skills-cleanup'
        base.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix='entry-controls-', dir=base)
        self.addCleanup(self.temp.cleanup)
        self.tmp = Path(self.temp.name)
        self.binary = self.tmp / 'test-binary'
        self.binary.write_bytes(b'explicit configuration double\n')
        self.binary.chmod(0o700)

    def test_inverse_and_original_budgets(self):
        driver = DRIVER.read_text().replace("    '" + SELECTOR + "': 'tests/skills',\n", '')
        driver = driver.replace("    '" + HISTORY_SELECTOR + "': 'tests/skills',\n", '')
        begin = driver.index('def prepare_history_go_environment(')
        end = driver.index('def main():', begin)
        driver = driver[:begin] + driver[end:]
        driver = driver.replace("    if args.run == '" + HISTORY_SELECTOR + "':\n"
                                '        prepare_history_go_environment(directory, env)\n', '')
        begin = driver.index('    # The cleanup fixture invokes these exact shared helpers')
        end = driver.index('    return sorted(paths)', begin)
        driver = driver[:begin] + driver[end:]
        self.assertEqual(driver, prior(DRIVER))
        sup = SUPERVISOR.read_text()
        begin = sup.index('def survivor_identity(')
        end = sup.index('def main():', begin)
        sup = sup[:begin] + sup[end:]
        sup = sup.replace("                    if args.run == '" + HISTORY_SELECTOR + "':\n"
                          "                        log.write('OWNED survivor_identity=' + survivor_identity(pid) + '\\n')\n", '')
        begin = sup.index('def skill_cleanup_results(')
        end = sup.index('def observe_root_chain(', begin)
        sup = sup[:begin] + sup[end:]
        begin = sup.index("    if selector in ('" + SELECTOR + "',\n")
        end = sup.index('    expected = {', begin)
        sup = sup[:begin] + '    output = log_path.read_text()\n' + sup[end:]
        sup = sup.replace("        '" + SELECTOR + "': {'" + PERSIST + "', '" + RECOVER + "'},\n", '')
        sup = sup.replace("        '" + HISTORY_SELECTOR + "': {'" + HISTORY + "'},\n", '')
        begin = sup.index("    if selector in ('" + SELECTOR + "',\n")
        end = sup.index('    return good and actual == expected and waited', begin)
        sup = sup[:begin] + sup[end:]
        self.assertEqual(sup, prior(SUPERVISOR))
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))

    def test_exact_configuration_and_eleven_inputs(self):
        old = prior_module(DRIVER, 'old_driver')
        with patch.object(self.driver, 'MINIO', self.binary), patch.object(self.driver, 'MINIO_SHA', self.driver.sha(self.binary)):
            plan = self.driver.configuration(self.binary, SELECTOR, self.tmp / 'new')
            self.assertEqual((plan['resources'], plan['test_timeout'], plan['cwd']),
                             (7, '6m', str(ROOT / 'tests/skills')))
            history = self.driver.configuration(self.binary, HISTORY_SELECTOR, self.tmp / 'history')
            self.assertEqual((history['resources'], history['test_timeout'], history['cwd']),
                             (7, '6m', str(ROOT / 'tests/skills')))
            for wrong in ('', PERSIST, '^' + PERSIST + '$', SELECTOR + '/gate',
                          '^TestSkillLifecycleCleanup.*$', '^TestSkillLifecycleCleanup(CommitRecovery|Persistence)$',
                          HISTORY, HISTORY_SELECTOR + '/native_retry_preserves_abandoned_cause'):
                with self.subTest(selector=wrong), self.assertRaises(ValueError):
                    self.driver.configuration(self.binary, wrong, self.tmp / 'bad')
            for selector, cwd in old.TARGETS.items():
                with self.subTest(legacy=selector):
                    self.assertEqual(self.driver.configuration(self.binary, selector, self.tmp / 'old')['cwd'], str(ROOT / cwd))
        self.assertEqual(self.driver.TARGETS, old.TARGETS | {SELECTOR: 'tests/skills', HISTORY_SELECTOR: 'tests/skills'})
        before = set(old.input_paths(self.binary))
        after = set(self.driver.input_paths(self.binary))
        self.assertEqual(after - before, {ROOT / p for p in INPUTS})
        self.assertTrue(before <= after)
        self.assertTrue(all((ROOT / p).is_file() for p in INPUTS))
        self.assertFalse((self.tmp / 'new').exists())
        self.assertFalse((self.tmp / 'history').exists())

    def test_history_mode_is_local_and_precedes_exec(self):
        outside = self.tmp / 'unrelated-config'
        outside.mkdir()
        (outside / 'mode').write_text('on\n')
        inherited = {'XDG_CONFIG_HOME': str(outside), 'TEST_TELEMETRY_DIR': str(outside),
                     'GO_TELEMETRY_CHILD': '1', 'GO_TELEMETRY_CHILD_UPLOAD': '1',
                     'GOTELEMETRY': 'off'}
        original_open = Path.open
        for selector, fail_mode in ((HISTORY_SELECTOR, False), (HISTORY_SELECTOR, True),
                                    (SELECTOR, False)):
            with self.subTest(selector=selector, fail_mode=fail_mode):
                directory = self.tmp / ('mode-failure' if fail_mode else 'mode-history' if selector == HISTORY_SELECTOR else 'mode-legacy')
                argv = ['adapter', '--test-binary', str(self.binary), '--run', selector,
                        '--directory', str(directory)]
                boundary = []
                def execute(binary, args, env):
                    boundary.append(env.copy())
                    self.assertEqual((binary, args[:2]), ('/bin/sh', ['/bin/sh', 'scripts/test-objects.sh']))
                    if selector == HISTORY_SELECTOR:
                        config = directory / 'go-config'
                        mode = config / 'go/telemetry/mode'
                        self.assertEqual(env['XDG_CONFIG_HOME'], str(config))
                        self.assertEqual(mode.read_bytes(), b'off\n')
                        self.assertEqual(mode.stat().st_mode & 0o777, 0o600)
                        self.assertTrue(all(name not in env for name in inherited if name.startswith(('TEST_', 'GO_TELEMETRY_'))))
                    else:
                        self.assertTrue(all(env[name] == value for name, value in inherited.items()))
                        self.assertFalse((directory / 'go-config').exists())
                    self.assertFalse(any((directory / 'runtime').iterdir()))
                def opening(path, *args, **kwargs):
                    if fail_mode and path.name == 'mode':
                        raise PermissionError('explicit owned mode write failure')
                    return original_open(path, *args, **kwargs)
                with patch.object(sys, 'argv', argv), patch.dict(os.environ, inherited), \
                        patch.object(self.driver, 'MINIO', self.binary), \
                        patch.object(self.driver, 'MINIO_SHA', self.driver.sha(self.binary)), \
                        patch.object(self.driver.os, 'chdir'), \
                        patch.object(self.driver.os, 'execve', execute), patch.object(Path, 'open', opening):
                    if fail_mode:
                        with self.assertRaises(PermissionError):
                            self.driver.main()
                        self.assertEqual(boundary, [])
                    else:
                        self.driver.main()
                        self.assertEqual(len(boundary), 1)
                self.assertEqual((outside / 'mode').read_text(), 'on\n')

    def test_survivor_identity_has_no_sensitive_process_material(self):
        # Exact /proc shape, including spaces and a closing parenthesis in comm.
        raw = '567 (go ) worker) Z 432 ' + '0 ' * 17 + '987654 0\n'
        with patch.object(Path, 'read_text', return_value=raw), \
                patch.object(os, 'readlink', return_value='/private/secret-path/bin/go'):
            got = json.loads(self.sup.survivor_identity(567))
        self.assertEqual(got, {'pid': 567, 'comm': 'go ) worker', 'state': 'Z',
                               'ppid': 432, 'starttime': 987654, 'exe_name': 'go'})
        for error in (FileNotFoundError(), UnicodeError(), ValueError(), IndexError()):
            with self.subTest(error=type(error).__name__), \
                    patch.object(Path, 'read_text', side_effect=error), \
                    patch.object(os, 'readlink', side_effect=OSError()):
                got = json.loads(self.sup.survivor_identity(567))
                self.assertEqual(got, {'pid': 567, 'comm': None, 'state': None,
                                       'ppid': None, 'starttime': None, 'exe_name': None})

    def test_closed_body_positive_and_negative(self):
        for selector, nodes in ((SELECTOR, NODES), (HISTORY_SELECTOR, HISTORY_NODES)):
            with self.subTest(selector=selector):
                baseline = output(nodes, selector)
                self.assertTrue(self.sup.skill_cleanup_results(baseline, selector))
                self.assertTrue(self.sup.skill_cleanup_results(output(list(reversed(nodes)), selector), selector))
                # Missing any parent/child, duplicate RUN/PASS and failed/skipped
                # output must not be upgraded by one successful parent line.
                for omitted in nodes:
                    with self.subTest(omitted=omitted):
                        self.assertFalse(self.sup.skill_cleanup_results(output([n for n in nodes if n != omitted], selector), selector))
                for extra in ('=== RUN   ' + nodes[0] + '\n', '--- PASS: ' + nodes[-1] + ' (0s)\n',
                              '--- FAIL: ' + nodes[0] + ' (0s)\n', '--- SKIP: ' + nodes[-1] + ' (0s)\n',
                              '=== RUN   TestForeign\n--- PASS: TestForeign (0s)\n'):
                    with self.subTest(extra=extra):
                        self.assertFalse(self.sup.skill_cleanup_results(baseline + extra, selector))
                for text in (baseline.replace('pid=321', 'pid=0'), baseline.replace('code=0', 'code=1'),
                             baseline.replace('--- PASS: ' + nodes[-1], '--- PASS: ' + nodes[-1] + '_wrong'),
                             baseline.replace('=== RUN   ' + nodes[-1] + '\n', ''),
                             baseline + baseline.splitlines()[-1] + '\n'):
                    self.assertFalse(self.sup.skill_cleanup_results(text, selector))
        self.assertFalse(self.sup.skill_cleanup_results(output(NODES, SELECTOR), HISTORY_SELECTOR))
        self.assertFalse(self.sup.skill_cleanup_results(output(HISTORY_NODES, HISTORY_SELECTOR), SELECTOR))
        self.assertFalse(self.sup.skill_cleanup_results(output(), '^' + PERSIST + '$'))

    def actual_main(self, mode, selector, nodes):
        sup, driver = self.sup, self.driver
        events, waits, observed = [], [], []
        output_dir = self.tmp / ('main-' + mode)
        text = output(nodes, selector)
        if mode == 'missing_child':
            text = output(nodes[:-1], selector)
        original_read = Path.read_text
        original_readlink = os.readlink
        class Child:
            pid = 432
            returncode = None
            def wait(self, timeout):
                waits.append(timeout)
                self.returncode = 2 if mode == 'driver_exit' else 0
                if mode == 'changed_input':
                    self_outer.binary.write_bytes(b'changed')
                return self.returncode
            def poll(self):
                return self.returncode
        self_outer = self
        def popen(args, stdout, stderr):
            events.append('popen_double')
            self.assertEqual(args[args.index('--run') + 1], selector)
            directory = Path(args[args.index('--directory') + 1])
            runtime = directory / 'runtime'
            runtime.mkdir(parents=True)
            if mode == 'runtime_left':
                (runtime / 'survivor').write_text('owned residual')
            groups = [('agenteam.d05.objectfixture', ('container', 'network')),
                      ('agenteam.d04.networkfixture', ('container', 'network')),
                      ('agenteam.d03.fixture', ('container', 'container', 'network'))]
            resources = []
            for label, kinds in groups:
                for kind in kinds:
                    resources.append({'kind': kind, 'label': label,
                                      'id': format(len(resources) + 1, '064x'), 'nonce': 'a' * 32})
            record = {'kind': 'work-owner-root-chain', 'resources': resources,
                      'directories': [str(runtime / n) for n in ('objects', 'outbound', 'pg')]}
            (directory / 'owned.json').write_text(json.dumps(record))
            (directory / 'owned.json').chmod(0o600)
            stdout.write(text)
            stdout.flush()
            if mode == 'invalid_utf8':
                os.write(stdout.fileno(), b'\xff\n')
            return Child()
        def read(path, *args, **kwargs):
            if mode == 'log_oserror' and path.suffix == '.log':
                raise OSError('explicit read boundary fault')
            if mode == 'owned_survivor' and str(path) == '/proc/567/stat':
                raise UnicodeError('explicit failed identity observation')
            return original_read(path, *args, **kwargs)
        def readlink(path, *args, **kwargs):
            if mode == 'owned_survivor' and str(path) == '/proc/567/exe':
                raise OSError('explicit unavailable executable')
            return original_readlink(path, *args, **kwargs)
        def absent(item, timeout):
            observed.append(item['id'])
            return not (mode == 'resource_left' and item['id'] == format(1, '064x'))
        def tcp():
            events.append('tcp_double')
            return set()
        def descendants(_):
            events.append('descendants_double')
            if mode == 'owned_survivor' and events.count('descendants_double') == 1:
                return {567}
            return set()
        def kill(pid, sig):
            self.assertEqual((pid, sig), (567, sup.signal.SIGKILL))
            events.append('kill_double')
        def reaped(*_):
            events.append('reap_double')
            raise ChildProcessError
        argv = ['supervisor', '--root-chain', '--driver', str(DRIVER),
                '--binary', str(self.binary), '--run', selector, '--output', str(output_dir)]
        with contextlib.ExitStack() as stack:
            for obj, name, replacement in (
                (sys, 'argv', argv), (sup, 'root_adapter', lambda _: driver),
                (driver, 'input_paths', lambda _: [self.binary, DRIVER, SUPERVISOR]),
                (sup.ctypes, 'CDLL', lambda *_args, **_kwargs: types.SimpleNamespace(prctl=lambda *_: 0)),
                (sup.signal, 'signal', lambda *_: None), (sup.subprocess, 'Popen', popen),
                (sup, 'tcp', tcp), (sup, 'descendants', descendants),
                (sup.os, 'kill', kill), (sup.os, 'readlink', readlink),
                (sup.os, 'waitpid', reaped), (sup.time, 'sleep', lambda _: None),
                (sup, 'exact_absent', absent), (Path, 'read_text', read)):
                stack.enter_context(patch.object(obj, name, replacement))
            stream = stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            result = sup.main()
            terminal = json.loads(stream.getvalue())
        log = Path(terminal['log']).read_bytes().decode('utf-8', errors='replace')
        self.assertEqual(waits, [540])
        self.assertEqual(len(observed), 14)
        self.assertEqual(len(set(observed)), 7)
        self.assertEqual(events.count('tcp_double'), 3)
        self.assertEqual(events.count('descendants_double'), 3)
        self.assertEqual(events.count('reap_double'), 1)
        self.assertIn('ROOT private_observation=2 absent=True', log)
        self.assertIn('ROOT runtime_observation=2', log)
        self.assertIn('HOST_TCP delta_empty_observation=2', log)
        self.assertIn('SUPERVISOR inputs_unchanged=', log)
        if mode == 'owned_survivor':
            self.assertEqual(events.count('kill_double'), 1)
            self.assertIn('STOP owned descendants survived driver: [567]', log)
            self.assertEqual('OWNED survivor_identity=' in log, selector == HISTORY_SELECTOR)
        self.assertTrue(terminal['actual_driver_wait'])
        wanted = 0 if mode == 'good' else 2 if mode == 'driver_exit' else 1
        self.assertEqual(result, wanted)
        self.assertEqual(terminal['exit'], wanted)
        if mode == 'changed_input':
            self.assertIn('inputs_unchanged=False', log)
        self.binary.write_bytes(b'explicit configuration double\n')

    def test_actual_main_retains_all_tails(self):
        for selector, nodes in ((SELECTOR, NODES), (HISTORY_SELECTOR, HISTORY_NODES)):
            for mode in ('good', 'missing_child', 'invalid_utf8', 'log_oserror',
                         'driver_exit', 'resource_left', 'runtime_left', 'changed_input',
                         'owned_survivor'):
                with self.subTest(selector=selector, mode=mode):
                    self.actual_main(mode, selector, nodes)


if __name__ == '__main__':
    unittest.main(verbosity=2)
