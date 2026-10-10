#!/usr/bin/env python3
"""Offline controls for the exact Secret default-root increment; no sockets."""
import ast
import hashlib
import importlib.util
import io
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestProjectSecretVariablesDefaultRoot$'
# Actual d61fe981 shared bytes, after the accepted main/Secret HTTP union.
BASE = {DRIVER: '5c79e5edb34d88fac531dd6c8357c4307bb5c2123a74b706b92ebc622ed594d0',
        SUP: '819c3672eae51f5f656b7b79384f61cc7e8e01db926a235cf58becff6d3c8c0a'}
RESULTS = r'''SECRET_ROOT = '^TestProjectSecretVariablesDefaultRoot$'


def secret_root_results(output):
    top = 'TestProjectSecretVariablesDefaultRoot'
    expected = {top, top + '/existing-project-protocol-and-routing',
                top + '/stop-drain-original-calls'}
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    return (len(runs) == len(expected) and set(runs) == expected
            and len(results) == len(expected)
            and all(state == 'PASS' for state, _ in results)
            and {name for _, name in results} == expected
            and len(waits) == 1 and waits[0][1:] == ('0', SECRET_ROOT)
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


'''


def inverse(name, source):
    changes = [("    '^TestProjectSecretVariablesDefaultRoot$': 'internal/central/app',\n", '')] if name == DRIVER else [
        (RESULTS, ''),
        ("        SECRET_ROOT: {'TestProjectSecretVariablesDefaultRoot'},\n", ''),
        ("    if selector == SECRET_ROOT:\n"
         "        complete = secret_root_results(output)\n"
         "        log.write(f'ROOT secret_exact_run_pass_wait={complete}\\n')\n"
         "        good = good and complete\n", ''),
        ("    if 'ProjectSecretVariablesDefaultRoot' in args.run and (args.run != SECRET_ROOT or not args.root_chain):\n"
         "        parser.error('Secret default root requires its exact original root-chain entry')\n", ''),
        ("    if args.run in ('^TestKnowledgeSkillsDefaultRootComposition$', SECRET_ROOT):\n"
         "        inputs.update({str(p): adapter.sha(p) for p in adapter.root_composition_inputs()})\n",
         "    if args.run == '^TestKnowledgeSkillsDefaultRootComposition$':\n"
         "        inputs.update({str(p): adapter.sha(p) for p in adapter.root_composition_inputs()})\n"),
        ("            if args.run in ('^TestKnowledgeSkillsDefaultRootComposition$', SECRET_ROOT):\n"
         "                same = same and root_composition_same(inputs, args, adapter)\n",
         "            if args.run == '^TestKnowledgeSkillsDefaultRootComposition$':\n"
         "                same = same and root_composition_same(inputs, args, adapter)\n"),
    ]
    if name not in BASE:
        raise ValueError('unknown shared source')
    for added, original in changes:
        if source.count(added) != 1:
            raise ValueError('unknown or missing increment')
        source = source.replace(added, original, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE[name]:
        raise ValueError('unrecognized baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


sup, driver = load('secret_root_supervisor', SUP), load('secret_root_driver', DRIVER)


class RootEntryControls(unittest.TestCase):
    def test_actual_inverse_and_unknown_refusal(self):
        for name in BASE:
            source = (ROOT / name).read_text()
            ast.parse(source)
            original = inverse(name, source)
            ast.parse(original)
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown change\n')
        with self.assertRaises(ValueError):
            inverse(SUP, (ROOT / SUP).read_text().replace("all(state == 'PASS'", "all(state != 'FAIL'", 1))

    def test_exact_target_and_original_budgets(self):
        self.assertEqual(driver.TARGETS[SELECTOR], 'internal/central/app')
        self.assertNotIn(SELECTOR + 'x', driver.TARGETS)
        self.assertEqual(sup.budgets(True), (540, 60))
        self.assertEqual(sup.budgets(False), (123, 3))
        baseline = {}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'),
             {'__file__': str(ROOT / DRIVER), '__name__': 'baseline_driver'}, baseline)
        self.assertEqual({k: v for k, v in driver.TARGETS.items() if k != SELECTOR}, baseline['TARGETS'])

    def test_exact_three_cases_and_original_wait(self):
        top = 'TestProjectSecretVariablesDefaultRoot'
        cases = [top, top + '/existing-project-protocol-and-routing', top + '/stop-drain-original-calls']
        run = ''.join('=== RUN   ' + name + '\n' for name in cases)
        passed = ''.join('--- PASS: ' + name + ' (0.01s)\n' for name in reversed(cases))
        wait = 'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n'
        original = run + passed + wait
        self.assertTrue(sup.secret_root_results(original))
        for mutated in (original.replace('--- PASS:', '--- SKIP:', 1),
                        original.replace('--- PASS:', '--- FAIL:', 1),
                        original.replace('code=0', 'code=1'), original.replace(wait, ''),
                        original + wait, original + 'FAIL\n', original + run.splitlines()[0] + '\n',
                        original + '=== RUN   TestOther\n', original + '--- PASS: TestOther (0.1s)\n',
                        original.replace('/stop-drain-original-calls', '/alias'),
                        original.replace('pid=42', 'pid=0'),
                        original + wait.replace(SELECTOR, '^TestOther$')):
            with self.subTest(log=mutated):
                self.assertFalse(sup.secret_root_results(mutated))

    def test_parser_rejects_unknown_and_wrong_mode_before_resources(self):
        for selector, root in ((SELECTOR, False), (SELECTOR + 'x', True),
                               ('^TestProjectSecretVariablesDefaultRootExtra$', False)):
            args = ['supervisor', '--driver', '/absent', '--binary', '/absent',
                    '--output', '/not-created', '--run', selector] + (['--root-chain'] if root else [])
            with patch.object(sys, 'argv', args), patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit) as stopped:
                    sup.main()
                self.assertEqual(stopped.exception.code, 2)

    def test_actual_input_reenumeration_and_bytes(self):
        with tempfile.TemporaryDirectory(prefix='secret-root-input-') as tmp:
            root = Path(tmp)
            app = root / 'internal/central/app'
            app.mkdir(parents=True)
            source = app / 'root_test.go'
            source.write_text('package app\n')
            binary = root / 'candidate.test'
            binary.write_text('fixed candidate')
            args = SimpleNamespace(binary=binary)
            with patch.object(driver, 'REPOSITORY', root), patch.object(driver, 'input_paths', return_value=[binary]):
                inputs = {str(p): driver.sha(p) for p in [binary] + driver.root_composition_inputs()}
                self.assertTrue(sup.root_composition_same(inputs, args, driver))
                source.write_text('changed')
                self.assertFalse(sup.root_composition_same(inputs, args, driver))
                source.write_text('package app\n')
                added = app / 'another_test.go'
                added.write_text('extra')
                self.assertFalse(sup.root_composition_same(inputs, args, driver))
                added.unlink()
                source.unlink()
                self.assertFalse(sup.root_composition_same(inputs, args, driver))
                source.symlink_to(binary)
                self.assertFalse(sup.root_composition_same(inputs, args, driver))


if __name__ == '__main__':
    unittest.main()
