#!/usr/bin/env python3
"""Closed Model Runtime root entry controls; no sockets, Go or Docker calls.

The exported inverse only removes the fixed Model delta. Importing this module
defines controls; it does not load another entry or execute a test.
"""
import ast
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestModelTextRuntimePersistentWire$'
TOP = 'TestModelTextRuntimePersistentWire'
CASES = {TOP, TOP + '/json_success', TOP + '/policy_deny'}
# Actual a028559a main bytes. Only the Model delta is projected away.
BASE_SHA = {
    DRIVER: '70182f089b5ef3820fc35497c58e154f825ace40d2b6d51b6a09906fcedcbbea',
    SUP: '3b57384499c4c6ce348add8c762b7cf8d7bd70e83f001563d95c6c73b384f24e',
}
# Fixed literals are filled once during source preparation, never at import.
BLOCKS = {'.agent-state/work-owner-http/root_chain_driver.py': ('def model_runtime_inputs(binary):\n',
                                                       'def parser_inputs(binary):\n',
                                                       'a82cb3fff3c45c210e2443b5d78ed45f84ae7ae987e36476bdf2ad7798d06d55'),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': ('MODEL_RUNTIME = '
                                                               "'^TestModelTextRuntimePersistentWire$'\n",
                                                               'PARSER_PG = '
                                                               "'^TestKnowledgePlainTextParserIntegration$'\n",
                                                               '9f58e7c5e4adc0c3ba7a5bb3bf8bea4ee7d68b79dbc9f350666f037b417db018')}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': (('',
                                                        "    '^TestModelTextRuntimePersistentWire$': "
                                                        "'tests/model',\n"),
                                                       ('',
                                                        '    if args.run == '
                                                        "'^TestModelTextRuntimePersistentWire$':\n"
                                                        '        prepare_history_go_environment(directory, '
                                                        'env)\n')),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': (('    if selector in (PARSER_PG, '
                                                                "'^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n",
                                                                '    if selector in (MODEL_RUNTIME, '
                                                                'PARSER_PG, '
                                                                "'^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n"),
                                                               ('',
                                                                '        MODEL_RUNTIME: '
                                                                "{'TestModelTextRuntimePersistentWire'},\n"),
                                                               ('',
                                                                '        good = good and complete\n'
                                                                '    if selector == MODEL_RUNTIME:\n'
                                                                '        complete = '
                                                                'model_runtime_results(output)\n'
                                                                "        log.write(f'ROOT "
                                                                "model_runtime_exact_run_pass_wait={complete}\\n')\n"),
                                                               ('',
                                                                "    if 'ModelTextRuntimePersistentWire' in "
                                                                'args.run and (args.run != MODEL_RUNTIME or '
                                                                'not args.root_chain):\n'
                                                                "        parser.error('Model Runtime "
                                                                'requires one exact original root-chain '
                                                                "entry')\n"),
                                                               ('',
                                                                '    if args.run == MODEL_RUNTIME:\n'
                                                                '        inputs = {str(p): adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.model_runtime_inputs(args.binary)}\n'),
                                                               ('',
                                                                '            if args.run == MODEL_RUNTIME:\n'
                                                                '                same = same and '
                                                                'model_runtime_same(inputs, args, '
                                                                'adapter)\n'))}


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    if "'^TestProjectLifecycleStopBatchRealGuard$'" in source:
        guard = load('model_guard_inverse', '.agent-state/project-variable-lifecycle/guard-entry-controls.py')
        source = guard.inverse(name, source)
    start, end, digest = BLOCKS[name]
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('missing or duplicated Model block')
    first, last = source.index(start), source.index(end)
    if first >= last or hashlib.sha256(source[first:last].encode()).hexdigest() != digest:
        raise ValueError('unknown Model block')
    source = source[:first] + source[last:]
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous Model hunk')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown shared baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def original_log():
    return (''.join('=== RUN   ' + name + '\n' for name in sorted(CASES))
            + ''.join('--- PASS: ' + name + ' (0.01s)\n' for name in sorted(CASES))
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n')


class ModelRuntimeEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('model_entry_driver', DRIVER)
        cls.sup = load('model_entry_supervisor', SUP)

    def test_fixed_inverse_and_original_scopes(self):
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            ast.parse(source)
            restored = inverse(name, source)
            ast.parse(restored)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown source change\n')
            # Guard overlaps the old Model log-read tuple. Mutate each Model
            # hunk only after the complete newer delta has been verified.
            if "'^TestProjectLifecycleStopBatchRealGuard$'" in source:
                guard = load('model_guard_mutation_base', '.agent-state/project-variable-lifecycle/guard-entry-controls.py')
                source = guard.inverse(name, source)
            for _, added in SOURCE_HUNKS[name]:
                self.assertEqual(source.count(added), 1)
                with self.assertRaises(ValueError):
                    inverse(name, source.replace(added, '', 1))
                with self.assertRaises(ValueError):
                    inverse(name, source + added)
            start, end, _ = BLOCKS[name]
            block = source[source.index(start):source.index(end)]
            with self.assertRaises(ValueError):
                inverse(name, block + source)
        source = (ROOT / SUP).read_text()
        for old, new in (
                ('(540, 60) if root_chain', '(541, 60) if root_chain'),
                ("('0', MODEL_RUNTIME)", "('1', MODEL_RUNTIME)"),
                ("MODEL_RUNTIME: {'TestModelTextRuntimePersistentWire'}", 'MODEL_RUNTIME: set()'),
                ('same = same and model_runtime_same(inputs, args, adapter)', 'same = True'),
                ('same = same and root_composition_same(inputs, args, adapter)', 'same = True'),
                ('same = same and parser_same(inputs, args, adapter)', 'same = True'),
                ("('0', SECRET_ROOT)", "('1', SECRET_ROOT)"),
                ("KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'}", 'KNOWLEDGE_UI: set()'),
                ('good = good and complete', 'good = True')):
            self.assertIn(old, source)
            with self.assertRaises(ValueError):
                inverse(SUP, source.replace(old, new, 1))
        baseline = {'__file__': str(ROOT / DRIVER), '__name__': 'model_entry_baseline'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), baseline)
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/model')
        for unshipped in ('^TestSkillOwnerReadWeb$', '^TestKnowledgeOwnerRenameWeb$'):
            self.assertNotIn(unshipped, self.driver.TARGETS)
            with self.assertRaises(ValueError):
                inverse(DRIVER, (ROOT / DRIVER).read_text().replace(
                    'TARGETS = {\n', 'TARGETS = {\n    ' + repr(unshipped)
                    + ": 'internal/central/app',\n", 1))
        self.assertFalse(hasattr(self.sup, 'OWNER_UI_TOPS'))
        self.assertEqual({k: v for k, v in self.driver.TARGETS.items()
                          if k not in (SELECTOR, '^TestProjectLifecycleStopBatchRealGuard$', '^TestAgentConfigurationMetadata$')}, baseline['TARGETS'])
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))
        # This follows the actual known inverse chain, without rerunning the
        # historical domains' business/method matrices or requiring artifacts.
        parser = load('model_parser_inverse', '.agent-state/d13-plain-text-parser/entry-controls.py')
        knowledge = load('model_knowledge_inverse', '.agent-state/knowledge-owner-ui/entry-controls.py')
        secret = load('model_secret_inverse', '.agent-state/secret-owner-http/root-entry-controls.py')
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            self.assertEqual(hashlib.sha256(parser.inverse(name, source).encode()).hexdigest(), parser.BASE_SHA[name])
            self.assertEqual(hashlib.sha256(knowledge.inverse(name, source).encode()).hexdigest(), knowledge.BASE[name])
            self.assertEqual(hashlib.sha256(secret.inverse(name, source).encode()).hexdigest(), secret.BASE[name])

    def test_exact_cases_and_original_wait(self):
        good = original_log()
        self.assertEqual(self.sup.MODEL_RUNTIME, SELECTOR)
        self.assertEqual(self.sup.MODEL_RUNTIME_CASES, CASES)
        self.assertTrue(self.sup.model_runtime_results(good))
        for name in CASES:
            run, passed = '=== RUN   ' + name + '\n', '--- PASS: ' + name + ' (0.01s)\n'
            for bad in (good.replace(run, ''), good.replace(passed, ''), good + run, good + passed,
                        good.replace(passed, passed.replace('PASS', 'SKIP')),
                        good.replace(passed, passed.replace('PASS', 'FAIL'))):
                self.assertFalse(self.sup.model_runtime_results(bad))
        wait = good.splitlines(True)[-1]
        for bad in (good + '=== RUN   TestOther\n', good + wait, good.replace(wait, ''),
                    good.replace('code=0', 'code=1'), good.replace('pid=42', 'pid=0'),
                    good.replace('selector=' + SELECTOR, 'selector=' + SELECTOR + 'x'),
                    good + 'D03 explicit test actual_wait malformed\n', good + 'FAIL\n'):
            self.assertFalse(self.sup.model_runtime_results(bad))

    def test_actual_main_rejects_aliases_before_resources(self):
        base = ['supervisor', '--driver', '/not-used/driver', '--binary', '/not-used/candidate',
                '--output', '/not-used/output', '--run']
        for selector, root in ((SELECTOR, False), (SELECTOR + 'x', True),
                               (TOP, True), ('^' + TOP + '(Extra)?$', True)):
            args = base + [selector] + (['--root-chain'] if root else [])
            with patch.object(sys, 'argv', args), patch.object(self.sup, 'budgets') as budgets, \
                    patch.object(self.sup, 'root_adapter') as adapter, \
                    patch.object(self.sup.subprocess, 'Popen') as spawned, \
                    patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit):
                    self.sup.main()
                budgets.assert_not_called()
                adapter.assert_not_called()
                spawned.assert_not_called()
        class ReachedOriginalBudget(Exception):
            pass
        for selector, root in ((SELECTOR, True), ('^TestOpenAIChatWireJSON$', False),
                               (self.sup.SKILL_HTTP_PG, False), (self.sup.SKILL_HTTP_NATIVE, False)):
            args = base + [selector] + (['--root-chain'] if root else [])
            with patch.object(sys, 'argv', args), \
                    patch.object(self.sup, 'budgets', side_effect=ReachedOriginalBudget) as budgets:
                with self.assertRaises(ReachedOriginalBudget):
                    self.sup.main()
                budgets.assert_called_once_with(root)

    def test_actual_closure_and_reenumeration(self):
        with tempfile.TemporaryDirectory(prefix='model-entry-inputs-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'production.go', 'tests/model/runtime_persistence_test.go',
                     'tests/model/runtime_native_test.go', 'tests/model/original_helper_test.go',
                     '.agent-state/model-text-runtime/first-wire-method.md',
                     '.agent-state/model-text-runtime/entry-controls.py')
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('controlled source\n')
            binary = root / 'candidate.test'
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'input_paths', return_value=[binary, root / 'production.go']):
                paths = self.driver.model_runtime_inputs(binary)
                self.assertEqual(paths, sorted(root / name for name in names))
                inputs = {str(p): self.driver.sha(p) for p in paths}
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.model_runtime_same(inputs, args, self.driver))
                added = root / 'tests/model/later_helper_test.go'
                added.write_text('later source')
                self.assertFalse(self.sup.model_runtime_same(inputs, args, self.driver))
                added.unlink()
                for name in names[2:]:
                    path = root / name
                    original = path.read_text()
                    path.write_text('changed source')
                    self.assertFalse(self.sup.model_runtime_same(inputs, args, self.driver))
                    path.unlink()
                    self.assertFalse(self.sup.model_runtime_same(inputs, args, self.driver))
                    path.symlink_to(binary)
                    self.assertFalse(self.sup.model_runtime_same(inputs, args, self.driver))
                    path.unlink()
                    path.write_text(original)
                self.assertTrue(self.sup.model_runtime_same(inputs, args, self.driver))

    def test_actual_observer_preserves_all_resource_tails(self):
        with tempfile.TemporaryDirectory(prefix='model-entry-observer-') as tmp:
            root = Path(tmp)
            runtime = root / 'runtime'; runtime.mkdir()
            private = root / 'private'
            record = {'resources': [{'kind': 'container', 'id': str(n), 'nonce': 'controlled'} for n in range(7)],
                      'directories': [str(private)]}
            log = root / 'case.log'; log.write_text(original_log())
            with patch.object(self.sup, 'root_record', return_value=record), \
                    patch.object(self.sup, 'exact_absent', return_value=True) as absent, \
                    patch.object(self.sup.time, 'sleep'):
                self.assertTrue(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                self.assertEqual(absent.call_count, 14)
                for value in (original_log().replace('--- PASS: ' + TOP + '/policy_deny (0.01s)\n', ''),
                              original_log().replace('code=0', 'code=1')):
                    log.write_text(value)
                    self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                log.write_text(original_log())
                private.mkdir()
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                private.rmdir()
                absent.return_value = False
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                absent.return_value = True
                (runtime / 'pending').write_text('controlled')
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))

    def test_actual_driver_prepares_private_telemetry_before_exec(self):
        class OriginalExecBoundary(Exception):
            pass
        with tempfile.TemporaryDirectory(prefix='model-entry-exec-') as tmp:
            root = Path(tmp)
            directory = root / 'owned'
            plan = {'binary': str(root / 'candidate.test'), 'cwd': str(root / 'tests/model'),
                    'directory': str(directory), 'runtime': str(directory / 'runtime')}
            args = ['driver', '--test-binary', plan['binary'], '--run', SELECTOR, '--directory', str(directory)]
            def original_exec(path, argv, environment):
                self.assertEqual(path, '/bin/sh')
                self.assertEqual(argv, ['/bin/sh', 'scripts/test-objects.sh', '--run', SELECTOR])
                config = directory / 'go-config'
                self.assertEqual(environment['XDG_CONFIG_HOME'], str(config))
                self.assertEqual((config / 'go/telemetry/mode').read_text(), 'off\n')
                self.assertEqual(environment['AGENTEAM_FIXTURE_TEST_BINARY'], plan['binary'])
                for name in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD'):
                    self.assertNotIn(name, environment)
                raise OriginalExecBoundary()
            with patch.object(sys, 'argv', args), patch.object(self.driver, 'configuration', return_value=plan), \
                    patch.object(self.driver.os, 'chdir'), patch.object(self.driver.os, 'execve', side_effect=original_exec), \
                    patch.dict(os.environ, {name: 'must-be-removed' for name in (
                        'TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD')}):
                with self.assertRaises(OriginalExecBoundary):
                    self.driver.main()


if __name__ == '__main__':
    unittest.main()
