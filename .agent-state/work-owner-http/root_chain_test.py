#!/usr/bin/env python3
"""Offline controls only; mocked Wait/inspect never prove real retirement."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest import mock

REPOSITORY = Path(__file__).resolve().parents[2]


def load_source(relative, name):
    path = REPOSITORY / relative
    module = types.ModuleType(name)
    module.__file__ = str(path)
    # No import cache or subprocess: compile the exact source under review.
    exec(compile(path.read_text(), str(path), 'exec'), module.__dict__)
    return module


class RootChainControls(unittest.TestCase):
    def setUp(self):
        self.supervisor = load_source('.agent-state/task-planning-recovery/pg_only_supervisor.py', 'supervisor')
        self.adapter = load_source('.agent-state/work-owner-http/root_chain_driver.py', 'adapter')
        self.tmp = tempfile.TemporaryDirectory(dir=REPOSITORY / 'output/ai/work-owner-http/implementation/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_budgets_keep_default_and_bound_root_retirement(self):
        self.assertEqual(self.supervisor.budgets(False), (123, 3))
        self.assertEqual(self.supervisor.budgets(True), (360 + 75 + 55 + 50, 60))

    def test_target_set_includes_independent_confirmation_without_broad_selection(self):
        self.assertEqual(self.adapter.TARGETS, {
            '^TestWorkOwnerRootActual(Command|Reader)Join$': 'internal/central/app',
            '^TestWorkOwnerHTTPProcessRoutingAndPersistence$': 'tests/process',
            '^TestIndependentWorkOwnerRootConfirmationJoin$': 'internal/central/app',
        })

    def test_unjoined_root_remains_failure_after_bounded_kill_and_reap(self):
        s = self.supervisor
        driver, binary = self.root / 'driver', self.root / 'test'
        driver.write_bytes(b'driver')
        binary.write_bytes(b'test')
        waits = []

        class Child:
            pid, returncode = 43210, None

            def wait(self, timeout=None):
                waits.append(timeout)
                raise subprocess.TimeoutExpired('owned', timeout)

            def terminate(self): pass
            def kill(self): pass

        child = Child()
        clock = iter(range(0, 1000, 2))
        adapter = types.SimpleNamespace(TARGETS=self.adapter.TARGETS,
                                        input_paths=lambda _: [driver, binary], sha=self.adapter.sha)
        argv = ['supervisor', '--driver', str(driver), '--binary', str(binary),
                '--run', '^TestIndependentWorkOwnerRootConfirmationJoin$',
                '--output', str(self.root / 'out'), '--root-chain']
        output = io.StringIO()
        with mock.patch.object(s.sys, 'argv', argv), \
                mock.patch.object(s.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *a: 0)), \
                mock.patch.object(s, 'tcp', return_value=set()), \
                mock.patch.object(s, 'descendants', return_value=set()), \
                mock.patch.object(s.subprocess, 'Popen', return_value=child), \
                mock.patch.object(s.os, 'waitpid', return_value=(0, 0)), \
                mock.patch.object(s.time, 'sleep'), \
                mock.patch.object(s.time, 'monotonic', side_effect=lambda: next(clock)), \
                mock.patch.object(s, 'root_adapter', return_value=adapter), \
                mock.patch.object(s, 'observe_root_chain', return_value=False), \
                contextlib.redirect_stdout(output):
            code = s.main()
        self.assertEqual(code, 1)
        self.assertEqual(waits, [540, 60, 3])
        terminal = json.loads(output.getvalue())
        self.assertFalse(terminal['actual_driver_wait'])
        trace = Path(terminal['log']).read_text()
        self.assertIn('STOP owned root descendants not joined within bounded reap tail', trace)

    def test_actual_main_default_success_and_both_timeout_modes_with_mock_children(self):
        s = self.supervisor
        driver, binary = self.root / 'driver', self.root / 'test'
        driver.write_bytes(b'driver')
        binary.write_bytes(b'test')
        for root_mode, timeout_case in [(False, False), (False, True), (True, True)]:
            with self.subTest(root=root_mode, timeout=timeout_case):
                waits, commands = [], []

                class Child:
                    pid, returncode = 43210, None

                    def wait(self, timeout=None):
                        waits.append(timeout)
                        if len(waits) == 1 and timeout_case:
                            raise subprocess.TimeoutExpired('owned', timeout)
                        self.returncode = 0
                        return 0

                    def terminate(self):
                        self.terminated = True

                    def kill(self):
                        self.killed = True

                child = Child()

                def popen(command, **kwargs):
                    commands.append(command)
                    return child

                adapter = types.SimpleNamespace(TARGETS=self.adapter.TARGETS,
                                                input_paths=lambda _: [driver, binary], sha=self.adapter.sha)
                selector = '^TestWorkOwnerHTTPProcessRoutingAndPersistence$'
                argv = ['supervisor', '--driver', str(driver), '--binary', str(binary),
                        '--run', selector, '--output', str(self.root / 'out')]
                if root_mode:
                    argv += ['--root-chain']
                with mock.patch.object(s.sys, 'argv', argv), \
                        mock.patch.object(s.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *a: 0)), \
                        mock.patch.object(s, 'tcp', return_value=set()), \
                        mock.patch.object(s, 'descendants', return_value=set()), \
                        mock.patch.object(s.subprocess, 'Popen', side_effect=popen), \
                        mock.patch.object(s.os, 'waitpid', side_effect=ChildProcessError), \
                        mock.patch.object(s.time, 'sleep'), \
                        mock.patch.object(s, 'root_adapter', return_value=adapter), \
                        mock.patch.object(s, 'observe_root_chain', return_value=True), \
                        contextlib.redirect_stdout(io.StringIO()):
                    code = s.main()
                self.assertEqual(code, 1 if timeout_case else 0)
                self.assertEqual(waits, [540, 60] if root_mode else [123, 3] if timeout_case else [123])
                self.assertEqual(commands[0][:5], [str(driver), '--test-binary', str(binary), '--run', selector])
                self.assertEqual(child.returncode, 0)

    def record(self):
        directory = self.root / 'owned'
        (directory / 'runtime').mkdir(parents=True)
        resources = []
        groups = [('agenteam.d05.objectfixture', ['container', 'network']),
                  ('agenteam.d04.networkfixture', ['container', 'network']),
                  ('agenteam.d03.fixture', ['container', 'container', 'network'])]
        for n, (label, kinds) in enumerate(groups, 1):
            for kind in kinds:
                resources.append(dict(kind=kind, id=f'{len(resources)+1:064x}', label=label, nonce=f'{n:032x}'))
        record = dict(kind='work-owner-root-chain', resources=resources,
                      directories=[str(directory / 'runtime' / x) for x in ['object', 'outbound', 'pg']])
        path = directory / 'owned.json'
        path.write_text(json.dumps(record))
        path.chmod(0o600)
        return directory, path, record

    def test_projection_requires_all_seven_owned_identities(self):
        directory, path, record = self.record()
        self.assertEqual(len(self.supervisor.root_record(directory)['resources']), 7)
        for change in ['two-only', 'duplicate', 'private-escape', 'unknown-label']:
            with self.subTest(change=change):
                bad = json.loads(json.dumps(record))
                if change == 'two-only': bad['resources'] = bad['resources'][:2]
                if change == 'duplicate': bad['resources'][1]['id'] = bad['resources'][0]['id']
                if change == 'private-escape': bad['directories'][0] = '/tmp/unowned'
                if change == 'unknown-label': bad['resources'][0]['label'] = 'foreign'
                path.write_text(json.dumps(bad))
                with self.assertRaises(ValueError):
                    self.supervisor.root_record(directory)

    def test_docker_failure_is_not_absence(self):
        _, _, record = self.record()
        item = record['resources'][0]
        for result, want in [
                (subprocess.CompletedProcess([], 1, '', 'Error: No such object: ' + item['id']), True),
                (subprocess.CompletedProcess([], 1, '', 'Cannot connect to Docker daemon'), False),
                (subprocess.CompletedProcess([], 0, '{}', ''), False)]:
            with self.subTest(want=want, status=result.returncode), \
                    mock.patch.object(self.supervisor.subprocess, 'run', return_value=result) as run:
                self.assertEqual(self.supervisor.exact_absent(item, 1), want)
                self.assertEqual(run.call_args.args[0], ['docker', 'container', 'inspect', item['id']])

    def test_adapter_rejects_unrelated_selector_before_exec(self):
        binary = self.root / 'test'
        binary.write_bytes(b'test')
        binary.chmod(0o500)
        with self.assertRaises(ValueError):
            self.adapter.configuration(binary, '^TestUnrelated$', self.root / 'fresh')

    def test_adapter_execs_original_chain_with_fresh_private_environment(self):
        adapter = self.adapter
        directory = self.root / 'fresh'
        selector = '^TestWorkOwnerHTTPProcessRoutingAndPersistence$'
        plan = dict(directory=str(directory), runtime=str(directory / 'runtime'),
                    binary='/owned/test', cwd=str(REPOSITORY / 'tests/process'))

        class ExecObserved(Exception):
            pass

        argv = ['adapter', '--test-binary', plan['binary'], '--run', selector,
                '--directory', str(directory)]
        inherited = {name: 'prior-unowned-descriptor' for name in
                     ('AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_OUTBOUND_FIXTURE',
                      'AGENTEAM_PG_FIXTURE', 'AGENTEAM_PG_UNSUPPORTED_FIXTURE')}
        with mock.patch.object(adapter.sys, 'argv', argv), \
                mock.patch.object(adapter, 'configuration', return_value=plan), \
                mock.patch.dict(adapter.os.environ, inherited), \
                mock.patch.object(adapter.os, 'chdir'), \
                mock.patch.object(adapter.os, 'execve', side_effect=ExecObserved) as execute:
            with self.assertRaises(ExecObserved):
                adapter.main()
        executable, command, env = execute.call_args.args
        self.assertEqual(executable, '/bin/sh')
        self.assertEqual(command, ['/bin/sh', 'scripts/test-objects.sh', '--run', selector])
        self.assertTrue(all(name not in env for name in inherited))
        self.assertEqual(env['TMPDIR'], plan['runtime'])
        self.assertEqual(env['GOTMPDIR'], plan['runtime'])
        self.assertEqual(env['AGENTEAM_FIXTURE_TEST_CWD'], plan['cwd'])
        self.assertEqual(env['AGENTEAM_FIXTURE_TEST_BINARY'], plan['binary'])
        self.assertEqual(env['GOFLAGS'], '-mod=readonly -p=2')

    def test_dual_observations_and_missing_manifest_fail_without_cleanup(self):
        directory, path, record = self.record()
        selector = '^TestWorkOwnerHTTPProcessRoutingAndPersistence$'
        log_path = self.root / 'trace.log'
        prefix = ('=== RUN   TestWorkOwnerHTTPProcessRoutingAndPersistence\n'
                  'D03 explicit test actual_wait pid=123 code=0 selector=' + selector + '\n')
        for missing in [False, True]:
            if missing:
                path.unlink()
            with log_path.open('w+') as log, \
                    mock.patch.object(self.supervisor, 'exact_absent', return_value=True) as inspect, \
                    mock.patch.object(self.supervisor.time, 'sleep'):
                log.write(prefix)
                result = self.supervisor.observe_root_chain(directory, log, log_path, selector)
                self.assertEqual(result, not missing)
                self.assertEqual(inspect.call_count, 0 if missing else 14)
            trace = log_path.read_text()
            self.assertIn('ROOT runtime_observation=1 empty=True', trace)
            self.assertIn('ROOT runtime_observation=2 empty=True', trace)
            if missing:
                self.assertIn('STOP missing or invalid seven-resource ownership record', trace)


if __name__ == '__main__':
    unittest.main(verbosity=2)
