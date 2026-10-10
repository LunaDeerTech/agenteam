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
    return subprocess.check_output(['git', 'show', BASE + ':' + str(path.relative_to(ROOT))], cwd=ROOT, text=True)


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
        begin = driver.index('    # The cleanup fixture invokes these exact shared helpers')
        end = driver.index('    return sorted(paths)', begin)
        driver = driver[:begin] + driver[end:]
        self.assertEqual(driver, prior(DRIVER))
        sup = SUPERVISOR.read_text()
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
            return original_read(path, *args, **kwargs)
        def absent(item, timeout):
            observed.append(item['id'])
            return not (mode == 'resource_left' and item['id'] == format(1, '064x'))
        def tcp():
            events.append('tcp_double')
            return set()
        def descendants(_):
            events.append('descendants_double')
            return set()
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
                         'driver_exit', 'resource_left', 'runtime_left', 'changed_input'):
                with self.subTest(selector=selector, mode=mode):
                    self.actual_main(mode, selector, nodes)


if __name__ == '__main__':
    unittest.main(verbosity=2)
