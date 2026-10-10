#!/usr/bin/env python3
"""Closed Guard root-entry controls; no Go, sockets or Docker execution.

Importing only defines functions/tests. The inverse removes this exact Guard
increment before the accepted legacy inverse chain, never unknown changes.
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
SELECTOR = '^TestProjectLifecycleStopBatchRealGuard$'
TOP = 'TestProjectLifecycleStopBatchRealGuard'
CASES = {TOP}
# Fixed main 69c13e5d bytes, including Model; no unshipped UI is imported.
BASE_SHA = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': '0082ae5c627c1102a7f3ac97bbd7ffb4fbb9e7a2424e0c4f46b869bf351a7f06',
 '.agent-state/work-owner-http/root_chain_driver.py': 'fd8872cebd99102ccbbe1212382e081616ecf6075d4b790e3a8ab473ccf18d85'}
BLOCKS = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': ('GUARD_ROOT = '
                                                               "'^TestProjectLifecycleStopBatchRealGuard$'\n",
                                                               'MODEL_RUNTIME '
                                                               '= '
                                                               "'^TestModelTextRuntimePersistentWire$'\n",
                                                               '2a465b14a0aa67aba2db78ecdd5986f8d0926ce9a6b6229a1e08de7949b9936a'),
 '.agent-state/work-owner-http/root_chain_driver.py': ('def '
                                                       'guard_inputs(binary):\n',
                                                       'def '
                                                       'model_runtime_inputs(binary):\n',
                                                       'f34bb841b8459ec740642f3171b0480e34b93600919a2689b68719fb9739b52a')}
SOURCE_HUNKS = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': [('        if '
                                                                'round == 1: '
                                                                'time.sleep(.1)\n'
                                                                '    '
                                                                'log.flush()\n'
                                                                '    if '
                                                                'selector in '
                                                                '(MODEL_RUNTIME, '
                                                                'PARSER_PG, '
                                                                "'^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n"
                                                                '                    '
                                                                "'^TestSkillLifecycleCleanupHistoricalAttempts$'):\n"
                                                                '        '
                                                                'try:\n',
                                                                '        if '
                                                                'round == 1: '
                                                                'time.sleep(.1)\n'
                                                                '    '
                                                                'log.flush()\n'
                                                                '    if '
                                                                'selector in '
                                                                '(GUARD_ROOT, '
                                                                'MODEL_RUNTIME, '
                                                                'PARSER_PG, '
                                                                "'^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',\n"
                                                                '                    '
                                                                "'^TestSkillLifecycleCleanupHistoricalAttempts$'):\n"
                                                                '        '
                                                                'try:\n'),
                                                               ('        '
                                                                'output = '
                                                                'log_path.read_text()\n'
                                                                '    expected '
                                                                '= {\n'
                                                                '        '
                                                                'MODEL_RUNTIME: '
                                                                "{'TestModelTextRuntimePersistentWire'},\n"
                                                                '        '
                                                                'PARSER_PG: '
                                                                "{'TestKnowledgePlainTextParserIntegration'},\n",
                                                                '        '
                                                                'output = '
                                                                'log_path.read_text()\n'
                                                                '    expected '
                                                                '= {\n'
                                                                '        '
                                                                'GUARD_ROOT: '
                                                                "{'TestProjectLifecycleStopBatchRealGuard'},\n"
                                                                '        '
                                                                'MODEL_RUNTIME: '
                                                                "{'TestModelTextRuntimePersistentWire'},\n"
                                                                '        '
                                                                'PARSER_PG: '
                                                                "{'TestKnowledgePlainTextParserIntegration'},\n"),
                                                               ('                       '
                                                                '+ '
                                                                're.escape(selector) '
                                                                "+ r'$', "
                                                                'output, re.M) '
                                                                'is not None\n'
                                                                '    '
                                                                "log.write(f'ROOT "
                                                                'exact_tops={actual '
                                                                '== expected} '
                                                                "actual_test_wait={waited}\\n')\n"
                                                                '    if '
                                                                'selector == '
                                                                'PARSER_PG:\n'
                                                                '        '
                                                                'complete = '
                                                                'parser_results(output)\n',
                                                                '                       '
                                                                '+ '
                                                                're.escape(selector) '
                                                                "+ r'$', "
                                                                'output, re.M) '
                                                                'is not None\n'
                                                                '    '
                                                                "log.write(f'ROOT "
                                                                'exact_tops={actual '
                                                                '== expected} '
                                                                "actual_test_wait={waited}\\n')\n"
                                                                '    if '
                                                                'selector == '
                                                                'GUARD_ROOT:\n'
                                                                '        '
                                                                'complete = '
                                                                'guard_results(output)\n'
                                                                '        '
                                                                "log.write(f'ROOT "
                                                                "guard_exact_run_pass_child_wait={complete}\\n')\n"
                                                                '        good '
                                                                '= good and '
                                                                'complete\n'
                                                                '    if '
                                                                'selector == '
                                                                'PARSER_PG:\n'
                                                                '        '
                                                                'complete = '
                                                                'parser_results(output)\n'),
                                                               ('                        '
                                                                "help='exact "
                                                                'Work root '
                                                                'adapter; 540s '
                                                                'chain budget '
                                                                'and '
                                                                'seven-resource '
                                                                "observations')\n"
                                                                '    args = '
                                                                'parser.parse_args()\n'
                                                                '    if '
                                                                "'ModelTextRuntimePersistentWire' "
                                                                'in args.run '
                                                                'and (args.run '
                                                                '!= '
                                                                'MODEL_RUNTIME '
                                                                'or not '
                                                                'args.root_chain):\n'
                                                                '        '
                                                                "parser.error('Model "
                                                                'Runtime '
                                                                'requires one '
                                                                'exact '
                                                                'original '
                                                                'root-chain '
                                                                "entry')\n",
                                                                '                        '
                                                                "help='exact "
                                                                'Work root '
                                                                'adapter; 540s '
                                                                'chain budget '
                                                                'and '
                                                                'seven-resource '
                                                                "observations')\n"
                                                                '    args = '
                                                                'parser.parse_args()\n'
                                                                '    if '
                                                                "'ProjectLifecycleStopBatchRealGuard' "
                                                                'in args.run '
                                                                'and (args.run '
                                                                '!= GUARD_ROOT '
                                                                'or not '
                                                                'args.root_chain):\n'
                                                                '        '
                                                                "parser.error('lifecycle "
                                                                'guard '
                                                                'requires one '
                                                                'exact '
                                                                'original '
                                                                'root-chain '
                                                                "entry')\n"
                                                                '    if '
                                                                "'ModelTextRuntimePersistentWire' "
                                                                'in args.run '
                                                                'and (args.run '
                                                                '!= '
                                                                'MODEL_RUNTIME '
                                                                'or not '
                                                                'args.root_chain):\n'
                                                                '        '
                                                                "parser.error('Model "
                                                                'Runtime '
                                                                'requires one '
                                                                'exact '
                                                                'original '
                                                                'root-chain '
                                                                "entry')\n"),
                                                               ('    if '
                                                                'args.run in '
                                                                "('^TestKnowledgeSkillsDefaultRootComposition$', "
                                                                'SECRET_ROOT):\n'
                                                                '        '
                                                                'inputs.update({str(p): '
                                                                'adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.root_composition_inputs()})\n'
                                                                '    if '
                                                                'args.run == '
                                                                'MODEL_RUNTIME:\n'
                                                                '        '
                                                                'inputs = '
                                                                '{str(p): '
                                                                'adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.model_runtime_inputs(args.binary)}\n',
                                                                '    if '
                                                                'args.run in '
                                                                "('^TestKnowledgeSkillsDefaultRootComposition$', "
                                                                'SECRET_ROOT):\n'
                                                                '        '
                                                                'inputs.update({str(p): '
                                                                'adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.root_composition_inputs()})\n'
                                                                '    if '
                                                                'args.run == '
                                                                'GUARD_ROOT:\n'
                                                                '        '
                                                                'inputs = '
                                                                '{str(p): '
                                                                'adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.guard_inputs(args.binary)}\n'
                                                                '    if '
                                                                'args.run == '
                                                                'MODEL_RUNTIME:\n'
                                                                '        '
                                                                'inputs = '
                                                                '{str(p): '
                                                                'adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.model_runtime_inputs(args.binary)}\n'),
                                                               ('            '
                                                                'if args.run '
                                                                'in '
                                                                "('^TestKnowledgeSkillsDefaultRootComposition$', "
                                                                'SECRET_ROOT):\n'
                                                                '                '
                                                                'same = same '
                                                                'and '
                                                                'root_composition_same(inputs, '
                                                                'args, '
                                                                'adapter)\n'
                                                                '            '
                                                                'if args.run '
                                                                '== '
                                                                'MODEL_RUNTIME:\n'
                                                                '                '
                                                                'same = same '
                                                                'and '
                                                                'model_runtime_same(inputs, '
                                                                'args, '
                                                                'adapter)\n',
                                                                '            '
                                                                'if args.run '
                                                                'in '
                                                                "('^TestKnowledgeSkillsDefaultRootComposition$', "
                                                                'SECRET_ROOT):\n'
                                                                '                '
                                                                'same = same '
                                                                'and '
                                                                'root_composition_same(inputs, '
                                                                'args, '
                                                                'adapter)\n'
                                                                '            '
                                                                'if args.run '
                                                                '== '
                                                                'GUARD_ROOT:\n'
                                                                '                '
                                                                'same = same '
                                                                'and '
                                                                'guard_same(inputs, '
                                                                'args, '
                                                                'adapter)\n'
                                                                '            '
                                                                'if args.run '
                                                                '== '
                                                                'MODEL_RUNTIME:\n'
                                                                '                '
                                                                'same = same '
                                                                'and '
                                                                'model_runtime_same(inputs, '
                                                                'args, '
                                                                'adapter)\n')],
 '.agent-state/work-owner-http/root_chain_driver.py': [('MINIO_SHA = '
                                                        "'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'\n"
                                                        'TARGETS = {\n'
                                                        '    '
                                                        "'^TestModelTextRuntimePersistentWire$': "
                                                        "'tests/model',\n"
                                                        '    '
                                                        "'^TestKnowledgePlainTextParserIntegration$': "
                                                        "'tests/knowledge',\n",
                                                        'MINIO_SHA = '
                                                        "'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'\n"
                                                        'TARGETS = {\n'
                                                        '    '
                                                        "'^TestProjectLifecycleStopBatchRealGuard$': "
                                                        "'tests/projectvariable',\n"
                                                        '    '
                                                        "'^TestModelTextRuntimePersistentWire$': "
                                                        "'tests/model',\n"
                                                        '    '
                                                        "'^TestKnowledgePlainTextParserIntegration$': "
                                                        "'tests/knowledge',\n"),
                                                       ('    if args.run == '
                                                        "'^TestSkillLifecycleCleanupHistoricalAttempts$':\n"
                                                        '        '
                                                        'prepare_history_go_environment(directory, '
                                                        'env)\n'
                                                        '    if args.run == '
                                                        "'^TestModelTextRuntimePersistentWire$':\n"
                                                        '        '
                                                        'prepare_history_go_environment(directory, '
                                                        'env)\n',
                                                        '    if args.run == '
                                                        "'^TestSkillLifecycleCleanupHistoricalAttempts$':\n"
                                                        '        '
                                                        'prepare_history_go_environment(directory, '
                                                        'env)\n'
                                                        '    if args.run == '
                                                        "'^TestProjectLifecycleStopBatchRealGuard$':\n"
                                                        '        '
                                                        "env.pop('AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD', "
                                                        'None)\n'
                                                        '        '
                                                        'prepare_history_go_environment(directory, '
                                                        'env)\n'
                                                        '    if args.run == '
                                                        "'^TestModelTextRuntimePersistentWire$':\n"
                                                        '        '
                                                        'prepare_history_go_environment(directory, '
                                                        'env)\n')]}

def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    start, end, digest = BLOCKS[name]
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('missing or duplicated Guard block')
    first, last = source.index(start), source.index(end)
    if first >= last or hashlib.sha256(source[first:last].encode()).hexdigest() != digest:
        raise ValueError('unknown Guard block')
    source = source[:first] + source[last:]
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous Guard hunk')
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
    return ('=== RUN   ' + TOP + '\n'
            + '    project_phase_recovery_guard_test.go:497: ProjectStopBatch child actual_wait '
              'pid=43 signal=SIGKILL stdout_joined=true\n'
            + '--- PASS: ' + TOP + ' (0.01s)\n'
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n')


class GuardEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('guard_entry_driver', DRIVER)
        cls.sup = load('guard_entry_supervisor', SUP)

    def test_fixed_inverse_and_original_scopes(self):
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            ast.parse(source)
            restored = inverse(name, source)
            ast.parse(restored)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown source change\n')
            for _, added in SOURCE_HUNKS[name]:
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
                ("('0', GUARD_ROOT)", "('1', GUARD_ROOT)"),
                ("GUARD_ROOT: {'TestProjectLifecycleStopBatchRealGuard'}", 'GUARD_ROOT: set()'),
                ('same = same and guard_same(inputs, args, adapter)', 'same = True'),
                ('same = same and root_composition_same(inputs, args, adapter)', 'same = True'),
                ('same = same and parser_same(inputs, args, adapter)', 'same = True'),
                ("('0', MODEL_RUNTIME)", "('1', MODEL_RUNTIME)"),
                ('same = same and model_runtime_same(inputs, args, adapter)', 'same = True'),
                ("('0', SECRET_ROOT)", "('1', SECRET_ROOT)"),
                ("KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'}", 'KNOWLEDGE_UI: set()'),
                ('good = good and complete', 'good = True')):
            self.assertIn(old, source)
            with self.assertRaises(ValueError):
                inverse(SUP, source.replace(old, new, 1))
        baseline = {'__file__': str(ROOT / DRIVER), '__name__': 'guard_entry_baseline'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), baseline)
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/projectvariable')
        for unshipped in ('^TestSkillOwnerReadWeb$', '^TestKnowledgeOwnerRenameWeb$'):
            self.assertNotIn(unshipped, self.driver.TARGETS)
            with self.assertRaises(ValueError):
                inverse(DRIVER, (ROOT / DRIVER).read_text().replace(
                    'TARGETS = {\n', 'TARGETS = {\n    ' + repr(unshipped)
                    + ": 'internal/central/app',\n", 1))
        self.assertEqual(self.driver.TARGETS['^TestModelTextRuntimePersistentWire$'], 'tests/model')
        self.assertFalse(hasattr(self.sup, 'OWNER_UI_TOPS'))
        self.assertEqual({k: v for k, v in self.driver.TARGETS.items() if k != SELECTOR}, baseline['TARGETS'])
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))
        # This follows the actual known inverse chain, without rerunning the
        # historical domains' business/method matrices or requiring artifacts.
        parser = load('guard_parser_inverse', '.agent-state/d13-plain-text-parser/entry-controls.py')
        knowledge = load('guard_knowledge_inverse', '.agent-state/knowledge-owner-ui/entry-controls.py')
        secret = load('guard_secret_inverse', '.agent-state/secret-owner-http/root-entry-controls.py')
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            self.assertEqual(hashlib.sha256(parser.inverse(name, source).encode()).hexdigest(), parser.BASE_SHA[name])
            self.assertEqual(hashlib.sha256(knowledge.inverse(name, source).encode()).hexdigest(), knowledge.BASE[name])
            self.assertEqual(hashlib.sha256(secret.inverse(name, source).encode()).hexdigest(), secret.BASE[name])

    def test_exact_cases_and_original_wait(self):
        good = original_log()
        self.assertEqual(self.sup.GUARD_ROOT, SELECTOR)
        self.assertEqual(self.sup.GUARD_CASES, CASES)
        self.assertTrue(self.sup.guard_results(good))
        lines = good.splitlines(True)
        for line in lines:
            self.assertFalse(self.sup.guard_results(good.replace(line, '')))
            self.assertFalse(self.sup.guard_results(good + line))
        for before, after in (
                ('--- PASS:', '--- SKIP:'), ('--- PASS:', '--- FAIL:'),
                ('code=0', 'code=1'), ('pid=42', 'pid=0'), ('pid=43', 'pid=42'),
                ('pid=43', 'pid=0'), ('signal=SIGKILL', 'signal=SIGTERM'),
                ('stdout_joined=true', 'stdout_joined=false'),
                ('project_phase_recovery_guard_test.go:497:', 'untrusted.go:497:'),
                ('selector=' + SELECTOR, 'selector=' + SELECTOR + 'x')):
            self.assertFalse(self.sup.guard_results(good.replace(before, after)))
        for extra in ('=== RUN   TestOther\n', '=== RUN   ' + TOP + '/extra\n',
                      'D03 explicit test actual_wait malformed\n',
                      'ProjectStopBatch child actual_wait malformed\n', 'FAIL\n'):
            self.assertFalse(self.sup.guard_results(good + extra))
        # The fixture's child marker is not a native-provider lifecycle event.
        self.assertFalse(self.sup.guard_results(good.replace(lines[1],
            'D03 native-provider event=stopped pid=43 actual_wait=true\n')))

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
        with tempfile.TemporaryDirectory(prefix='guard-entry-inputs-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'production.go',
                     'tests/projectvariable/project_phase_recovery_guard_test.go',
                     'tests/projectvariable/project_phase_stop_fixture_test.go',
                     'tests/projectvariable/project_lifecycle_fixture_test.go',
                     'tests/projectvariable/original_helper_test.go',
                     'tests/testsupport/accountenv/environment_test.go',
                     'tests/testsupport/objectstore/cmd/fixture/main.go',
                     '.agent-state/project-variables-independent/commitproxy/proxy_test.go',
                     '.agent-state/project-variable-lifecycle/guard-entry-controls.py')
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('controlled source\n')
            binary = root / 'candidate.test'
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'input_paths', return_value=[binary, root / 'production.go']):
                paths = self.driver.guard_inputs(binary)
                self.assertEqual(paths, sorted(root / name for name in names))
                inputs = {str(p): self.driver.sha(p) for p in paths}
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.guard_same(inputs, args, self.driver))
                for new in ('tests/projectvariable/later_test.go', 'tests/testsupport/new/helper.go'):
                    added = root / new
                    added.parent.mkdir(parents=True, exist_ok=True)
                    added.write_text('later source')
                    self.assertFalse(self.sup.guard_same(inputs, args, self.driver))
                    added.unlink()
                for name in names[2:]:
                    path = root / name
                    original = path.read_text()
                    path.write_text('changed source')
                    self.assertFalse(self.sup.guard_same(inputs, args, self.driver))
                    path.unlink()
                    self.assertFalse(self.sup.guard_same(inputs, args, self.driver))
                    path.symlink_to(binary)
                    self.assertFalse(self.sup.guard_same(inputs, args, self.driver))
                    path.unlink()
                    path.write_text(original)
                self.assertTrue(self.sup.guard_same(inputs, args, self.driver))

    def test_actual_observer_preserves_all_resource_tails(self):
        with tempfile.TemporaryDirectory(prefix='guard-entry-observer-') as tmp:
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
                for value in (original_log().replace('stdout_joined=true', 'stdout_joined=false'),
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
        with tempfile.TemporaryDirectory(prefix='guard-entry-exec-') as tmp:
            root = Path(tmp)
            directory = root / 'owned'
            plan = {'binary': str(root / 'candidate.test'), 'cwd': str(root / 'tests/projectvariable'),
                    'directory': str(directory), 'runtime': str(directory / 'runtime')}
            args = ['driver', '--test-binary', plan['binary'], '--run', SELECTOR, '--directory', str(directory)]
            def original_exec(path, argv, environment):
                self.assertEqual(path, '/bin/sh')
                self.assertEqual(argv, ['/bin/sh', 'scripts/test-objects.sh', '--run', SELECTOR])
                config = directory / 'go-config'
                self.assertEqual(environment['XDG_CONFIG_HOME'], str(config))
                self.assertEqual((config / 'go/telemetry/mode').read_text(), 'off\n')
                self.assertEqual(environment['AGENTEAM_FIXTURE_TEST_BINARY'], plan['binary'])
                for name in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD',
                             'AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD'):
                    self.assertNotIn(name, environment)
                raise OriginalExecBoundary()
            with patch.object(sys, 'argv', args), patch.object(self.driver, 'configuration', return_value=plan), \
                    patch.object(self.driver.os, 'chdir'), patch.object(self.driver.os, 'execve', side_effect=original_exec), \
                    patch.dict(os.environ, {name: 'must-be-removed' for name in (
                        'TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD',
                             'AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD')}):
                with self.assertRaises(OriginalExecBoundary):
                    self.driver.main()


if __name__ == '__main__':
    unittest.main()
