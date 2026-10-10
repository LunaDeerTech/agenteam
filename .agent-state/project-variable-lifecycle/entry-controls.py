#!/usr/bin/env python3
"""Offline controls: actual supervisor orchestration with explicit OS doubles.

No subprocess, Docker, socket, /proc or host TCP observation is executed.
"""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('lifecycle_entry', Path(__file__).with_name('run.py'))
entry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(entry)


def fake_tree(root):
    names = set(entry.REQUIRED) | {entry.DRIVER, entry.BINARY}
    names.update('internal/central/' + name + '/fixture.go' for name in entry.CENTRAL)
    names.update(name + '/fixture.go' for name in entry.GO_TREES)
    names.add('db/migrations/00001_fixture.sql')
    for name in names:
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('controlled input\n')


def transcript(directory):
    record = {'nonce': 'a' * 32, 'container_id': 'b' * 64, 'network_id': 'c' * 64}
    directory.mkdir()
    owned = directory / 'owned.json'
    owned.write_text(json.dumps(record))
    owned.chmod(0o600)
    top, subs = next(iter(entry.CASES.items()))
    cases = [top] + [top + '/' + sub for sub in subs]
    lines = ['=== RUN   ' + name for name in cases]
    lines += ['--- PASS: ' + name + ' (0.01s)' for name in cases]
    lines += ['CHILD pid=77 selector=' + entry.SELECTOR,
              'CHILD actual_wait pid=77 state=exit status 0',
              'OWNED nonce=' + record['nonce'] + ' container=' + record['container_id'] +
              ' network=' + record['network_id'] + ' port=54321 PostgreSQL=170010 vector=0.8.1']
    lines += ['RETIRE observation=' + str(n) + ' exact_container=' + record['container_id'] +
              ' exact_network=' + record['network_id'] + ' clean=true' for n in (1, 2)]
    lines += ['DRIVER terminal exit=0 elapsed=1s child_started=true actual_child_wait=true cleanup=true']
    return '\n'.join(lines) + '\n'


class LifecycleEntryControls(unittest.TestCase):
    def test_closed_cli_before_supervisor_load(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(entry, 'ROOT', Path(tmp)):
            args = ['--driver', str(entry.ROOT / entry.DRIVER), '--binary', str(entry.ROOT / entry.BINARY),
                    '--run', entry.SELECTOR, '--output', str(entry.ROOT / 'output/ai/project-variable-lifecycle/pg-author-control')]
            entry.arguments(args)
            invalid = [args + ['--root-chain'], args + ['--native'], args + ['--unknown'],
                       args + ['--run', entry.SELECTOR], args[:-2],
                       [x.replace(entry.SELECTOR, entry.SELECTOR + 'x') for x in args],
                       [x.replace(entry.SELECTOR, '^TestSecretVariableHTTPBoundary$') for x in args],
                       [x.replace(entry.SELECTOR, '^TestSecretHTTPNativeTransport$') for x in args],
                       [x.replace(entry.BINARY, 'wrong.test') for x in args],
                       [x.replace('--driver', '--driv') for x in args]]
            for vector in invalid:
                with self.subTest(vector_number=invalid.index(vector)), patch.object(sys, 'argv', ['run.py'] + vector), patch.object(entry, 'load_supervisor') as load, contextlib.redirect_stderr(io.StringIO()):
                    with self.assertRaises(SystemExit): entry.main()
                    load.assert_not_called()

    def test_namespace_and_original_budgets(self):
        sup = entry.load_supervisor()
        self.assertEqual(sup['SECRET_HTTP_PG'], entry.SELECTOR)
        self.assertEqual(sup['SECRET_HTTP_CASES'], {entry.SELECTOR: entry.CASES})
        self.assertEqual(sup['budgets'](False), (123, 3))
        self.assertEqual(sup['budgets'](True), (540, 60))
        self.assertIs(sup['secret_http_inputs'], entry.inputs)
        self.assertEqual(sup['observe_secret_http'].__code__.co_filename, str(entry.ROOT / entry.SUPERVISOR))

    def test_actual_observer_accepts_only_complete_original_case_set(self):
        sup = entry.load_supervisor()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            directory, log = root / 'owned', root / 'output.log'
            original = transcript(directory)
            def observe(value):
                log.write_text(value)
                return sup['observe_secret_http'](directory, io.StringIO(), log, entry.SELECTOR)
            self.assertTrue(observe(original))
            lines = original.splitlines(True)
            negative = [original.replace('--- PASS:', '--- FAIL:', 1), original.replace('--- PASS:', '--- SKIP:', 1),
                        original + '=== RUN   TestUnexpected\n', original + lines[0], original.replace(lines[1], ''),
                        original.replace('actual_wait pid=77', 'actual_wait pid=78'),
                        original.replace('state=exit status 0', 'state=exit status 1'),
                        original.replace('RETIRE observation=2', 'RETIRE observation=1'),
                        original.replace('clean=true', 'clean=false'),
                        original.replace('DRIVER terminal exit=0', 'DRIVER terminal exit=1')]
            for value in negative: self.assertFalse(observe(value))
            (directory / 'private.key').write_text('controlled')
            self.assertFalse(observe(original))
            (directory / 'private.key').unlink()
            (directory / 'owned.json').chmod(0o644)
            self.assertFalse(observe(original))

    def test_inputs_reenumerate_and_reject_missing_aliases_or_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(entry, 'ROOT', Path(tmp).resolve()):
            fake_tree(entry.ROOT)
            driver, binary = entry.ROOT / entry.DRIVER, entry.ROOT / entry.BINARY
            first = entry.inputs(driver, binary, entry.SELECTOR)
            self.assertIn(str(entry.ROOT / entry.PROBE), first)
            self.assertIn(str(entry.ROOT / entry.OWN / 'entry-controls.py'), first)
            added = entry.ROOT / 'tests/projectvariable/new_probe.go'
            added.write_text('new')
            self.assertNotEqual(first, entry.inputs(driver, binary, entry.SELECTOR))
            added.unlink()
            for bad_driver, bad_binary, selector in [(driver, binary, entry.SELECTOR+'x'), (driver.with_name('wrong'), binary, entry.SELECTOR), (driver, binary.with_name('wrong'), entry.SELECTOR)]:
                with self.assertRaises(ValueError): entry.inputs(bad_driver, bad_binary, selector)
            probe = entry.ROOT / entry.PROBE
            saved = probe.read_bytes()
            probe.unlink()
            with self.assertRaises(FileNotFoundError): entry.inputs(driver, binary, entry.SELECTOR)
            probe.symlink_to(driver)
            with self.assertRaises(ValueError): entry.inputs(driver, binary, entry.SELECTOR)
            probe.unlink()
            probe.write_bytes(saved)
            self.assertEqual(first, entry.inputs(driver, binary, entry.SELECTOR))

    def exercise_main(self, mutation=None, change_input=False):
        # Load the real frozen supervisor before replacing the root with an
        # explicitly synthetic input tree; only OS/resource edges are doubled.
        sup = entry.load_supervisor()
        original_observer = sup['observe_secret_http']
        observations, input_reads, wait_budgets = [], [], []
        with tempfile.TemporaryDirectory() as tmp, patch.object(entry, 'ROOT', Path(tmp).resolve()):
            fake_tree(entry.ROOT)
            output = entry.ROOT / 'output/ai/project-variable-lifecycle/pg-author-control'
            argv = ['run.py', '--driver', str(entry.ROOT / entry.DRIVER), '--binary', str(entry.ROOT / entry.BINARY), '--run', entry.SELECTOR, '--output', str(output)]
            def observed(directory, log, path, selector):
                observations.append(selector)
                return original_observer(directory, log, path, selector)
            def inputs(driver, binary, selector):
                input_reads.append(selector)
                return entry.inputs(driver, binary, selector)
            class Child:
                pid, returncode = 42, None
                def __init__(self, command, stdout, stderr):
                    self.command = command
                    directory = Path(command[command.index('--directory')+1])
                    value = transcript(directory)
                    stdout.write(mutation(value) if mutation else value)
                    stdout.flush()
                def wait(self, timeout):
                    wait_budgets.append(timeout)
                    self.returncode = 0
                    if change_input: (entry.ROOT / 'tests/projectvariable/new_during_run.go').write_text('changed')
                    return 0
            sup['observe_secret_http'], sup['secret_http_inputs'] = observed, inputs
            sup['descendants'], sup['tcp'] = lambda _: set(), lambda: set()
            libc = Mock()
            libc.prctl.return_value = 0
            with patch.object(entry, 'load_supervisor', return_value=sup), patch.object(sys, 'argv', argv), \
                 patch.object(sup['ctypes'], 'CDLL', return_value=libc), \
                 patch.object(sup['subprocess'], 'Popen', Child), \
                 patch.object(sup['os'], 'waitpid', side_effect=ChildProcessError), \
                 patch.object(sup['signal'], 'signal'), patch.object(sup['time'], 'sleep'), \
                 contextlib.redirect_stdout(io.StringIO()):
                code = entry.main()
            self.assertEqual(observations, [entry.SELECTOR])
            self.assertEqual(input_reads, [entry.SELECTOR, entry.SELECTOR])
            self.assertEqual(wait_budgets, [123])
            log = next(output.glob('*.log')).read_text()
            self.assertIn('SUPERVISOR actual_driver_wait pid=42 actual=True actual_exit=0', log)
            self.assertEqual(log.count('HOST_TCP delta_empty_observation='), 2)
            self.assertEqual(log.count('OWNED runtime_observation='), 2)
            return code

    def test_actual_main_calls_pg_observer_and_both_input_passes(self):
        self.assertEqual(self.exercise_main(), 0)

    def test_actual_main_fails_missing_sub_and_changed_input(self):
        self.assertEqual(self.exercise_main(lambda s: s.replace('--- PASS:', '--- SKIP:', 1)), 1)
        self.assertEqual(self.exercise_main(change_input=True), 1)


if __name__ == '__main__':
    unittest.main()
