#!/usr/bin/env python3
"""Closed configuration-metadata entry controls; no Go, Docker or sockets.

The strict inverse removes only this entry delta back to main 728cd45a.
Importing the module neither loads another entry nor runs a control.
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
SELECTOR = '^TestAgentConfigurationMetadata$'
TOP = 'TestAgentConfigurationMetadata'
CASES = frozenset({TOP, TOP + '/normal-metadata', TOP + '/current-and-stale', TOP + '/caller-rollback'})
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': '405c22124124a2890ec80605745a415e4c473d6e13d1110f9359358b54f8afc1',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': '2cfa7a4d86bcc7fa9846eefc8693e8fea0d38ad4465af458b606e83894231b25'}
BLOCKS = {'.agent-state/work-owner-http/root_chain_driver.py': ('def metadata_inputs(binary):',
                                                       'def guard_inputs(binary):',
                                                       'bda4e3af4554e9a259cee93c610ffffb3e5b23263748183d5f899703d787d17e'),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': ('METADATA_ROOT = '
                                                               "'^TestAgentConfigurationMetadata$'",
                                                               'GUARD_ROOT = '
                                                               "'^TestProjectLifecycleStopBatchRealGuard$'",
                                                               'ce3c80d689bef38cc7ee45afcc2d4eb216abf076aa8b63966ae0f2f40fcf3718')}
SOURCE_HUNKS = {
    '.agent-state/work-owner-http/root_chain_driver.py': [
        ('TARGETS = {\n', "TARGETS = {\n    '^TestAgentConfigurationMetadata$': 'tests/projectvariable',\n"),
        ("    if args.run == '^TestProjectLifecycleStopBatchRealGuard$':\n", "    if args.run == '^TestAgentConfigurationMetadata$':\n        env.pop('AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD', None)\n        prepare_history_go_environment(directory, env)\n    if args.run == '^TestProjectLifecycleStopBatchRealGuard$':\n"),
    ],
    '.agent-state/task-planning-recovery/pg_only_supervisor.py': [
        ('    if selector in (GUARD_ROOT, MODEL_RUNTIME, PARSER_PG,', '    if selector in (METADATA_ROOT, GUARD_ROOT, MODEL_RUNTIME, PARSER_PG,'),
        ('    expected = {\n        GUARD_ROOT:', "    expected = {\n        METADATA_ROOT: {'TestAgentConfigurationMetadata'},\n        GUARD_ROOT:"),
        ('    if selector == GUARD_ROOT:\n', "    if selector == METADATA_ROOT:\n        complete = metadata_results(output)\n        log.write(f'ROOT metadata_exact_run_pass_wait={complete}\\n')\n        good = good and complete\n    if selector == GUARD_ROOT:\n"),
        ("    if 'ProjectLifecycleStopBatchRealGuard' in args.run", "    if 'AgentConfigurationMetadata' in args.run and (args.run != METADATA_ROOT or not args.root_chain):\n        parser.error('configuration metadata requires one exact original root-chain entry')\n    if 'ProjectLifecycleStopBatchRealGuard' in args.run"),
        ('    if args.run == GUARD_ROOT:\n        inputs =', '    if args.run == METADATA_ROOT:\n        inputs = {str(p): adapter.sha(p) for p in adapter.metadata_inputs(args.binary)}\n    if args.run == GUARD_ROOT:\n        inputs ='),
        ('            if args.run == GUARD_ROOT:\n', '            if args.run == METADATA_ROOT:\n                same = same and metadata_same(inputs, args, adapter)\n            if args.run == GUARD_ROOT:\n'),
    ],
}

def schema_projection(name, source):
    if "'^TestAgentConfigurationSchema$'" in source:
        schema = load('formal_system_inverse', '.agent-state/agent-system-integration/schema-entry-controls.py')
        return schema.inverse(name, source)
    return source


def installation_projection(name, source):
    source = schema_projection(name, source)
    if "'^TestSkillInstallationPersistentObject$'" in source:
        install = load('metadata_install_inverse', '.agent-state/skill-installation/entry-controls.py')
        source = install.inverse(name, source)
    return source


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    source = installation_projection(name, source)
    start, end, digest = BLOCKS[name]
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('missing or duplicated metadata block')
    first, last = source.index(start), source.index(end)
    if first >= last or hashlib.sha256(source[first:last].encode()).hexdigest() != digest:
        raise ValueError('unknown metadata block')
    source = source[:first] + source[last:]
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous metadata hunk')
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


class MetadataEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('metadata_entry_driver', DRIVER)
        cls.sup = load('metadata_entry_supervisor', SUP)

    def test_fixed_inverse_and_original_scopes(self):
        for name in BASE_SHA:
            source = installation_projection(name, (ROOT / name).read_text())
            ast.parse(source)
            restored = inverse(name, source)
            ast.parse(restored)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown change\n')
            for _, added in SOURCE_HUNKS[name]:
                self.assertEqual(source.count(added), 1)
                with self.assertRaises(ValueError):
                    inverse(name, source.replace(added, '', 1))
                with self.assertRaises(ValueError):
                    inverse(name, source + added)
            start, end, _ = BLOCKS[name]
            with self.assertRaises(ValueError):
                inverse(name, source[source.index(start):source.index(end)] + source)
        source = installation_projection(SUP, (ROOT / SUP).read_text())
        for old, new in (
                ('(540, 60) if root_chain', '(541, 60) if root_chain'),
                ("('0', METADATA_ROOT)", "('1', METADATA_ROOT)"),
                ('same = same and metadata_same(inputs, args, adapter)', 'same = True'),
                ("('0', GUARD_ROOT)", "('1', GUARD_ROOT)"),
                ("('0', MODEL_RUNTIME)", "('1', MODEL_RUNTIME)"),
                ('good = good and complete', 'good = True')):
            self.assertEqual(source.count(old), 1) if old != 'good = good and complete' else self.assertIn(old, source)
            with self.assertRaises(ValueError):
                inverse(SUP, source.replace(old, new, 1))
        baseline = {'__file__': str(ROOT / DRIVER), '__name__': 'metadata_baseline'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), baseline)
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/projectvariable')
        self.assertEqual({k: v for k, v in self.driver.TARGETS.items() if k not in self.driver.METADATA_INPUTS}, baseline['TARGETS'])
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))
        for path, constant in (
                ('.agent-state/project-variable-lifecycle/guard-entry-controls.py', 'BASE_SHA'),
                ('.agent-state/model-text-runtime/entry-controls.py', 'BASE_SHA'),
                ('.agent-state/d13-plain-text-parser/entry-controls.py', 'BASE_SHA'),
                ('.agent-state/knowledge-owner-ui/entry-controls.py', 'BASE'),
                ('.agent-state/secret-owner-http/root-entry-controls.py', 'BASE')):
            previous = load('metadata_previous_' + path.split('/')[1].replace('-', '_'), path)
            for name in BASE_SHA:
                restored = previous.inverse(name, (ROOT / name).read_text())
                self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), getattr(previous, constant)[name])

    def test_exact_cases_and_original_wait(self):
        good = original_log()
        self.assertEqual(self.sup.METADATA_ROOT, SELECTOR)
        self.assertEqual(self.sup.METADATA_CASES, CASES)
        self.assertTrue(self.sup.metadata_results(good))
        for name in CASES:
            run, passed = '=== RUN   ' + name + '\n', '--- PASS: ' + name + ' (0.01s)\n'
            for bad in (good.replace(run, ''), good.replace(passed, ''), good + run, good + passed,
                        good.replace(passed, passed.replace('PASS', 'SKIP')),
                        good.replace(passed, passed.replace('PASS', 'FAIL'))):
                self.assertFalse(self.sup.metadata_results(bad))
        wait = good.splitlines(True)[-1]
        for bad in (good + '=== RUN   TestOther\n', good + wait, good.replace(wait, ''),
                    good.replace('code=0', 'code=1'), good.replace('pid=42', 'pid=0'),
                    good.replace('selector=' + SELECTOR, 'selector=' + SELECTOR + 'x'),
                    good + 'D03 explicit test actual_wait malformed\n', good + 'FAIL\n'):
            self.assertFalse(self.sup.metadata_results(bad))

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
        with tempfile.TemporaryDirectory(prefix='metadata-entry-inputs-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'production.go',
                     'tests/projectvariable/agent_configuration_metadata_test.go',
                     'tests/projectvariable/original_helper_test.go',
                     'internal/other/other_test.go', 'internal/other/assets/NOTICE',
                     'tests/testsupport/postgres/original.go',
                     'tests/testsupport/outbound/original_test.go',
                     '.agent-state/project-variables-independent/commitproxy/proxy.go',
                     '.agent-state/agent-configuration-metadata/README.md',
                     '.agent-state/agent-configuration-metadata/metadata-entry-controls.py')
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('controlled source\n')
            binary = root / 'candidate.test'
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'input_paths', return_value=[binary, root / 'production.go']):
                paths = self.driver.metadata_inputs(binary)
                self.assertEqual(paths, sorted(root / name for name in names))
                inputs = {str(p): self.driver.sha(p) for p in paths}
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver))
                for relative in ('tests/projectvariable/later_helper_test.go',
                                 'internal/other/later_test.go', 'internal/other/assets/later.txt'):
                    added = root / relative
                    added.write_text('later source')
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver))
                    added.unlink()
                for name in names[2:]:
                    path = root / name
                    original = path.read_text()
                    path.write_text('changed source')
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver))
                    path.unlink()
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver))
                    path.symlink_to(binary)
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver))
                    path.unlink()
                    path.write_text(original)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver))

    def test_actual_observer_preserves_all_resource_tails(self):
        with tempfile.TemporaryDirectory(prefix='metadata-entry-observer-') as tmp:
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
                for value in (original_log().replace('--- PASS: ' + TOP + '/caller-rollback (0.01s)\n', ''),
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
        with tempfile.TemporaryDirectory(prefix='metadata-entry-exec-') as tmp:
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
