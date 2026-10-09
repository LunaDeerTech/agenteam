#!/usr/bin/env python3
"""Offline exact-top controls. No fixture, Docker, network or driver execution."""
import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('project_skills_supervisor', SOURCE)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
NAMES = tuple(sup.SKILLS_CLEANUP_TOPS.values())


def output(name):
    return f'=== RUN   {name}\n=== RUN   {name}/allowed\n--- PASS: {name} (0.01s)\n    --- PASS: {name}/allowed (0.00s)\nPASS\n'


class ExactTop(unittest.TestCase):
    def test_all_three_exact_tops(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'go.log'
            for selector, name in sup.SKILLS_CLEANUP_TOPS.items():
                with self.subTest(name=name):
                    path.write_text(output(name))
                    self.assertTrue(sup.skills_cleanup_exact_top(path, selector))

    def test_log_closed_negative_matrix(self):
        name = NAMES[0]
        original = output(name)
        selector = '^' + name + '$'
        variants = {
            'no_run': original.replace(f'=== RUN   {name}\n', ''),
            'no_pass': original.replace(f'--- PASS: {name} (0.01s)\n', ''),
            'wrong_run': original.replace(f'=== RUN   {name}\n', f'=== RUN   {NAMES[1]}\n'),
            'wrong_pass': original.replace(f'--- PASS: {name} (0.01s)', f'--- PASS: {NAMES[1]} (0.01s)'),
            'duplicate_run': original + f'=== RUN   {name}\n',
            'duplicate_pass': original + f'--- PASS: {name} (0.01s)\n',
            'extra_top': original + output(NAMES[1]),
            'skip_top': original + f'--- SKIP: {name} (0.00s)\n',
            'skip_child': original + f'    --- SKIP: {name}/hidden (0.00s)\n',
            'fail_top': original + f'--- FAIL: {name} (0.00s)\n',
            'fail_child': original + f'    --- FAIL: {name}/hidden (0.00s)\n',
            'empty': '',
            'invalid_utf8': original.encode() + b'\xff',
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'go.log'
            for label, value in variants.items():
                with self.subTest(label=label):
                    path.write_bytes(value if isinstance(value, bytes) else value.encode())
                    self.assertFalse(sup.skills_cleanup_exact_top(path, selector))
            path.unlink()
            self.assertFalse(sup.skills_cleanup_exact_top(path, selector))
            path.mkdir()
            self.assertFalse(sup.skills_cleanup_exact_top(path, selector))

    def test_selector_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'go.log'
            path.write_text(output(NAMES[0]))
            for selector in (NAMES[0], '^'+NAMES[0], NAMES[0]+'$', '^TestProjectSkillsCleanup.*$', '^('+NAMES[0]+'|'+NAMES[1]+')$', '^'+NAMES[0]+'$/^allowed$', '^TestTaskPlanningMigration$'):
                with self.subTest(selector=selector):
                    self.assertFalse(sup.skills_cleanup_exact_top(path, selector))

    def test_real_binary_discovery(self):
        binary = ROOT / 'output/ai/project-skills-cleanup/project-skills-cleanup-race.test'
        self.assertTrue(binary.is_file(), 'build the fixed offline candidate first')
        for name in NAMES:
            with self.subTest(name=name):
                result = subprocess.run([str(binary), '-test.list', '^'+name+'$'], capture_output=True, text=True, timeout=15)
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout.splitlines(), [name])

    def test_original_tools_inverse_projection(self):
        source = SOURCE.read_text()
        start = source.index('SKILLS_CLEANUP_TOPS = {')
        end = source.index('def root_adapter', start)
        source = source[:start] + source[end:]
        branch = """            if not args.root_chain and args.run in SKILLS_CLEANUP_TOPS:
                log.flush()
                exact = skills_cleanup_exact_top(log_path, args.run)
                log.write(f'PROJECT_SKILLS exact_top={exact}\\n')
                if not exact:
                    code = 1
"""
        self.assertEqual(source.count(branch), 1)
        source = source.replace(branch, '')
        baseline = subprocess.run(['git', 'show', '9c60199f:.agent-state/task-planning-recovery/pg_only_supervisor.py'], cwd=ROOT, check=True, capture_output=True, text=True).stdout
        self.assertEqual(source, baseline)
        driver = '.agent-state/task-planning-recovery/pg_only_driver.go'
        prior = subprocess.run(['git', 'show', '9c60199f:'+driver], cwd=ROOT, check=True, capture_output=True).stdout
        self.assertEqual((ROOT / driver).read_bytes(), prior)
        self.assertEqual(sup.budgets(False), (123, 3))
        self.assertEqual(sup.budgets(True), (540, 60))

    def test_main_log_failure_preserves_original_tail(self):
        name = NAMES[0]
        for kind in ('valid', 'utf8', 'oserror', 'old_single'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                base = Path(directory)
                driver, binary = base / 'driver', base / 'test'
                driver.write_text('owned offline driver identity')
                binary.write_text('owned offline binary identity')
                args = ['supervisor', '--driver', str(driver), '--binary', str(binary), '--run', '^'+(name if kind != 'old_single' else 'TestTaskPlanningMigration')+'$', '--output', str(base / 'output')]
                calls = []

                class Process:
                    pid = 424242
                    returncode = None

                    def __init__(self, argv, stdout, **kwargs):
                        calls.append('popen_double')
                        stdout.write(output(name) if kind == 'valid' else 'ordinary old log\n')
                        if kind == 'utf8':
                            stdout.flush()
                            stdout.buffer.write(b'\xff\n')
                            stdout.flush()

                    def wait(self, timeout):
                        calls.append(('actual_wait_double', timeout))
                        self.returncode = 0
                        return 0

                def no_children(*args):
                    calls.append('reap_double')
                    raise ChildProcessError()

                def descendants(*args):
                    calls.append('descendants_double')
                    return set()

                def tcp():
                    calls.append('tcp_double')
                    return set()

                checker = sup.skills_cleanup_exact_top
                if kind == 'oserror':
                    checker = lambda *_, original=checker: original(base / 'absent-log', '^'+name+'$')
                # Main uses doubles for every process/resource operation. Only
                # the original log/input files and Python control flow are real.
                with patch.object(sys, 'argv', args), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Process), patch.object(sup, 'descendants', descendants), patch.object(sup.os, 'waitpid', no_children), patch.object(sup, 'tcp', tcp), patch.object(sup.time, 'sleep', lambda *_: None), patch.object(sup, 'skills_cleanup_exact_top', checker), contextlib.redirect_stdout(io.StringIO()):
                    result = sup.main()
                self.assertEqual(result, 0 if kind in ('valid', 'old_single') else 1)
                log = next((base / 'output').glob('*.log')).read_bytes()
                self.assertIn(b'SUPERVISOR actual_driver_wait', log)
                self.assertEqual(calls.count('reap_double'), 1)
                self.assertEqual(calls.count('descendants_double'), 3)
                self.assertEqual(calls.count('tcp_double'), 3)
                self.assertIn(b'HOST_TCP delta_empty_observation=2', log)
                self.assertIn(b'SUPERVISOR inputs_unchanged=True terminal=', log)
                if kind == 'old_single':
                    self.assertNotIn(b'PROJECT_SKILLS', log)


if __name__ == '__main__':
    unittest.main(verbosity=2)
