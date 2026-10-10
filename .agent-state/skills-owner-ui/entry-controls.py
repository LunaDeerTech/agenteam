#!/usr/bin/env python3
"""Offline controls for two exact Owner UI entries. No sockets or child test runs.

Importing this module only defines its fixed inverse and controlled methods.
The native/browser result schema is owned and checked by each original Go top.
"""
import ast
import hashlib
import importlib.util
import io
import json
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
SELECTORS = ('^TestSkillOwnerReadWeb$', '^TestKnowledgeOwnerRenameWeb$')
BASE = '33903460'
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': '70182f089b5ef3820fc35497c58e154f825ace40d2b6d51b6a09906fcedcbbea',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': '3b57384499c4c6ce348add8c762b7cf8d7bd70e83f001563d95c6c73b384f24e'}
BLOCKS = {'.agent-state/work-owner-http/root_chain_driver.py': ('OWNER_UI = {\n',
                                                       'def metadata_cost_inputs(',
                                                       '81fc245f8f4071f19982e21d3848679438e52373e2ec9ff9b49aa89efeb700ca'),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': ('OWNER_UI_TOPS = {\n',
                                                               'def root_composition_results(',
                                                               'f7228cc2547f0b4fdc543107e832b48e9969e8cf1f6eac5258a1032c0bf55a6f')}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': (('',
                                                        "    '^TestSkillOwnerReadWeb$': "
                                                        "'internal/central/app',\n"
                                                        "    '^TestKnowledgeOwnerRenameWeb$': "
                                                        "'internal/central/app',\n"),
                                                       ('',
                                                        '    if selector in OWNER_UI:\n'
                                                        "        plan['owner_ui'] = "
                                                        'owner_ui_configuration(selector, directory)\n'),
                                                       ('',
                                                        '    if args.run in OWNER_UI:\n'
                                                        '        prepare_history_go_environment(directory, '
                                                        'env)\n'
                                                        "        ui = plan['owner_ui']\n"
                                                        '        prefix = OWNER_UI[args.run][1]\n'
                                                        '        Path(ui[prefix + '
                                                        "'_EVIDENCE']).mkdir(mode=0o700)\n"
                                                        '        env.update(ui)\n'
                                                        "        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': "
                                                        'str(runtime),\n'
                                                        "                    prefix + '_INPUT_HASH': "
                                                        'owner_ui_input_hash(args.test_binary, args.run),\n'
                                                        "                    'PATH': "
                                                        'str(KNOWLEDGE_NODE.parent) + os.pathsep + '
                                                        "env.get('PATH', '')})\n")),
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': (('',
                                                                '        **{selector: {top} for selector, '
                                                                '(top, _) in OWNER_UI_TOPS.items()},\n'),
                                                               ('',
                                                                '    if selector in OWNER_UI_TOPS:\n'
                                                                '        complete = owner_ui_results(output, '
                                                                'selector)\n'
                                                                "        log.write(f'ROOT "
                                                                "owner_ui_exact_run_pass_wait={complete}\\n')\n"
                                                                '        good = good and complete\n'),
                                                               ('',
                                                                '    if any(name in args.run for name in '
                                                                "('SkillOwner', 'KnowledgeOwnerRename')) and "
                                                                '(args.run not in OWNER_UI_TOPS or not '
                                                                'args.root_chain):\n'
                                                                "        parser.error('Owner UI requires one "
                                                                "exact original root-chain entry')\n"),
                                                               ("    stem = ('ui-' + uuid.uuid4().hex[:16]) "
                                                                "if args.run == KNOWLEDGE_UI else ('pg-' + "
                                                                'uuid.uuid4().hex)\n',
                                                                "    stem = ('ui-' + uuid.uuid4().hex[:16]) "
                                                                'if args.run == KNOWLEDGE_UI or args.run in '
                                                                "OWNER_UI_TOPS else ('pg-' + "
                                                                'uuid.uuid4().hex)\n'),
                                                               ('',
                                                                '    if args.run in OWNER_UI_TOPS:\n'
                                                                '        try:\n'
                                                                '            '
                                                                'adapter.owner_ui_configuration(args.run, '
                                                                'directory)\n'
                                                                '            args.owner_ui_environment = '
                                                                'adapter.owner_ui_environment(args.run)\n'
                                                                '        except (OSError, ValueError, '
                                                                'KeyError):\n'
                                                                "            parser.error('exact frozen "
                                                                'Owner UI assets, interpreters and fresh '
                                                                "evidence required')\n"),
                                                               ('',
                                                                '    if args.run in OWNER_UI_TOPS:\n'
                                                                '        inputs = {str(p): adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.owner_ui_inputs(args.binary, '
                                                                'args.run)}\n'),
                                                               ('            if args.run == KNOWLEDGE_UI and '
                                                                'child.returncode is not None:\n',
                                                                '            if (args.run == KNOWLEDGE_UI or '
                                                                'args.run in OWNER_UI_TOPS) and '
                                                                'child.returncode is not None:\n'),
                                                               ('',
                                                                '            if args.run in OWNER_UI_TOPS:\n'
                                                                '                same = same and '
                                                                'owner_ui_same(inputs, args, adapter)\n'))}


def inverse(name, source):
    """Strip only this fixed UI delta; require the entire original main bytes."""
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    start, end, digest = BLOCKS[name]
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('missing or duplicated UI block')
    first, last = source.index(start), source.index(end)
    if first >= last or hashlib.sha256(source[first:last].encode()).hexdigest() != digest:
        raise ValueError('unknown UI block')
    source = source[:first] + source[last:]
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous UI hunk')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown main baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def original_log(selector):
    top, label = {
        SELECTORS[0]: ('TestSkillOwnerReadWeb', 'Skill'),
        SELECTORS[1]: ('TestKnowledgeOwnerRenameWeb', 'KnowledgeRename'),
    }[selector]
    return (f'=== RUN   {top}\n'
            f'    owner_web_test.go:42: {label} Node actual_wait pid=43 success=true\n'
            f'--- PASS: {top} (1.0s)\n'
            f'D03 explicit test actual_wait pid=42 code=0 selector={selector}\n')


class OwnerUIControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('owner_ui_driver', DRIVER)
        cls.sup = load('owner_ui_supervisor', SUP)

    def test_fixed_inverse_and_original_gates(self):
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            ast.parse(source)
            restored = inverse(name, source)
            ast.parse(restored)
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown delta\n')
            for _, new in SOURCE_HUNKS[name]:
                with self.assertRaises(ValueError):
                    inverse(name, source.replace(new, '', 1))
                with self.assertRaises(ValueError):
                    inverse(name, source + new)
            start, end, _ = BLOCKS[name]
            first, last = source.index(start), source.index(end)
            with self.assertRaises(ValueError):
                inverse(name, source[:first] + source[first:last] + source[first:])
        source = (ROOT / SUP).read_text()
        for old, new in (
                ('(540, 60) if root_chain', '(541, 60) if root_chain'),
                ("('0', PARSER_PG)", "('1', PARSER_PG)"),
                ("('0', KNOWLEDGE_UI)", "('1', KNOWLEDGE_UI)"),
                ('same = same and root_composition_same(inputs, args, adapter)', 'same = True'),
                ('same = same and owner_ui_same(inputs, args, adapter)', 'same = True'),
                ("SECRET_ROOT: {'TestProjectSecretVariablesDefaultRoot'}", 'SECRET_ROOT: set()'),
                ('good = good and complete', 'good = True')):
            self.assertIn(old, source)
            with self.assertRaises(ValueError):
                inverse(SUP, source.replace(old, new, 1))
        self.assertEqual(set(self.driver.OWNER_UI), set(SELECTORS))
        self.assertEqual(set(self.sup.OWNER_UI_TOPS), set(SELECTORS))
        old = {'__file__': str(ROOT / DRIVER), '__name__': 'original_driver'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), old)
        self.assertEqual({k: v for k, v in self.driver.TARGETS.items() if k not in SELECTORS}, old['TARGETS'])
        for selector in SELECTORS:
            self.assertEqual(self.driver.TARGETS[selector], 'internal/central/app')
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))

    def test_exact_top_node_and_original_go_wait(self):
        for selector in SELECTORS:
            output = original_log(selector)
            self.assertTrue(self.sup.owner_ui_results(output, selector))
            lines = output.splitlines(keepends=True)
            for changed in (
                    output.replace('--- PASS:', '--- SKIP:'), output.replace('--- PASS:', '--- FAIL:'),
                    output.replace('success=true', 'success=false'), output.replace('code=0', 'code=1'),
                    output.replace('pid=43', 'pid=0'), output.replace('pid=42', 'pid=0'),
                    output.replace('Node actual_wait', 'Node pending'), output.replace('selector=', 'alias='),
                    output + 'FAIL\n', output + '=== RUN   TestUnexpected/extra\n',
                    output + '--- PASS: TestUnexpected (1s)\n',
                    output + lines[0], output + lines[1], output + lines[-1],
                    output + lines[-1].replace('pid=42', 'pid=0'),
                    output + lines[1].replace('pid=43', 'pid=0'),
                    output.replace(lines[1], ''), output.replace(lines[-1], ''),
                    output.replace('Node actual_wait', 'Wrong Node actual_wait')):
                self.assertFalse(self.sup.owner_ui_results(changed, selector))
            self.assertFalse(self.sup.owner_ui_results(output, SELECTORS[1] if selector == SELECTORS[0] else SELECTORS[0]))
            self.assertFalse(self.sup.owner_ui_results(output, selector + 'x'))

    def test_real_cli_rejects_alias_and_nonroot_before_setup(self):
        for selector in SELECTORS:
            for selected, root_mode in ((selector, False), (selector + 'x', True),
                    (selector[1:], True), (selector[:-1], True),
                    (selector + '/extra', True), (selector.replace('Web', '.*'), False)):
                argv = ['supervisor', '--driver', '/absent', '--binary', '/absent',
                        '--output', '/never-created', '--run', selected]
                if root_mode:
                    argv.append('--root-chain')
                with patch.object(sys, 'argv', argv), patch('sys.stderr', io.StringIO()), \
                        patch.object(self.sup, 'root_adapter', side_effect=AssertionError('adapter reached')), \
                        patch.object(self.sup, 'tcp', side_effect=AssertionError('TCP reached')), \
                        patch.object(self.sup.ctypes, 'CDLL', side_effect=AssertionError('process setup reached')), \
                        patch.object(Path, 'mkdir', side_effect=AssertionError('directory creation reached')):
                    with self.assertRaises(SystemExit) as stopped:
                        self.sup.main()
                    self.assertEqual(stopped.exception.code, 2)

    def configuration_fixture(self, root, selector):
        name, prefix, case, _, _ = self.driver.OWNER_UI[selector]
        owned = root / 'output/ai' / name
        dist = owned / 'dist'
        dist.mkdir(parents=True)
        (dist / 'index.html').write_text('frozen asset')
        env = {prefix + '_DIST': str(dist), prefix + '_EVIDENCE': str(owned / 'evidence'),
               prefix + '_SCHEMA_PYTHON': str(self.driver.KNOWLEDGE_PYTHON), prefix + '_CASE': case}
        return owned, dist, env

    def test_closed_environment_owned_assets_and_fresh_evidence(self):
        for selector in SELECTORS:
            with tempfile.TemporaryDirectory(prefix='owner-ui-config-') as tmp:
                root = Path(tmp)
                owned, dist, env = self.configuration_fixture(root, selector)
                prefix = self.driver.OWNER_UI[selector][1]
                with patch.object(self.driver, 'REPOSITORY', root), patch.dict(os.environ, env):
                    self.assertEqual(self.driver.owner_ui_configuration(selector, Path('/tmp/u/ui-123')), env)
                    for key, bad in ((prefix + '_CASE', 'unknown'), (prefix + '_SCHEMA_PYTHON', '/unowned-python'),
                                     (prefix + '_DIST', str(root)), (prefix + '_EVIDENCE', str(dist / 'evidence'))):
                        with patch.dict(os.environ, {key: bad}):
                            with self.assertRaises(ValueError):
                                self.driver.owner_ui_configuration(selector, Path('/tmp/u/ui-123'))
                    with self.assertRaises(ValueError):
                        self.driver.owner_ui_configuration(selector, Path('/tmp/' + 'x' * 45))
                    evidence = Path(env[prefix + '_EVIDENCE'])
                    evidence.mkdir()
                    with self.assertRaises(ValueError):
                        self.driver.owner_ui_configuration(selector, Path('/tmp/u/ui-123'))
                    evidence.rmdir()
                    (dist / 'alias').symlink_to(dist / 'index.html')
                    with self.assertRaises(ValueError):
                        self.driver.owner_ui_assets(selector)

    def test_source_closure_and_input_reenumeration(self):
        for selector in SELECTORS:
            with tempfile.TemporaryDirectory(prefix='owner-ui-input-') as tmp:
                root = Path(tmp)
                _, dist, env = self.configuration_fixture(root, selector)
                _, _, _, stem, schemas = self.driver.OWNER_UI[selector]
                harness = root / 'tests/account-captcha-web'
                names = [harness / n for n in (stem + '.config.js', 'package.json', 'package-lock.json',
                    'e2e/' + stem + '.spec.ts', 'e2e/' + stem + '.native.ts', 'e2e/knowledge-owner-read.native.ts')]
                names += [root / n for n in ('web/src/main.ts', 'web/package.json', 'web/package-lock.json',
                    'web/node_modules/typescript/package.json', 'web/node_modules/typescript/lib/typescript.js',
                    'internal/central/app/original_fixture_test.go', 'candidate.test')]
                names.append(root / 'internal/central/app' / {
                    SELECTORS[0]: 'skill_owner_web_test.go',
                    SELECTORS[1]: 'knowledge_owner_rename_web_test.go',
                }[selector])
                names += [root / 'api/openapi' / n for n in ('common.json', *schemas)]
                for name in names:
                    name.parent.mkdir(parents=True, exist_ok=True)
                    name.write_text('controlled input')
                for name in ('@playwright/test', 'playwright', 'playwright-core'):
                    package = harness / 'node_modules' / name
                    package.mkdir(parents=True)
                    (package / 'package.json').write_text(json.dumps({'version': '1.56.1'}))
                    (package / 'cli.js').write_text('controlled launcher')
                binary = root / 'candidate.test'
                with patch.object(self.driver, 'REPOSITORY', root), patch.dict(os.environ, env), \
                        patch.object(self.driver, 'input_paths', return_value=[binary]):
                    paths = self.driver.owner_ui_inputs(binary, selector)
                    self.assertTrue(set(names).issubset(paths))
                    inputs = {str(p): self.driver.sha(p) for p in paths}
                    args = SimpleNamespace(binary=binary, run=selector, owner_ui_environment=env)
                    self.assertTrue(self.sup.owner_ui_same(inputs, args, self.driver))
                    original_hash = self.driver.owner_ui_input_hash(binary, selector)
                    source = root / 'web/src/main.ts'
                    source.write_text('changed source')
                    self.assertFalse(self.sup.owner_ui_same(inputs, args, self.driver))
                    self.assertNotEqual(self.driver.owner_ui_input_hash(binary, selector), original_hash)
                    source.write_text('controlled input')
                    extra = dist / 'extra.js'; extra.write_text('new asset')
                    self.assertFalse(self.sup.owner_ui_same(inputs, args, self.driver))
                    extra.unlink()
                    source.unlink()
                    self.assertFalse(self.sup.owner_ui_same(inputs, args, self.driver))
                    source.symlink_to(binary)
                    self.assertFalse(self.sup.owner_ui_same(inputs, args, self.driver))
                    source.unlink(); source.write_text('controlled input')
                    prefix = self.driver.OWNER_UI[selector][1]
                    with patch.dict(os.environ, {prefix + '_CASE': 'changed'}):
                        self.assertFalse(self.sup.owner_ui_same(inputs, args, self.driver))

    def test_actual_observer_composes_original_resource_tails(self):
        for selector in SELECTORS:
            with tempfile.TemporaryDirectory(prefix='owner-ui-observer-') as tmp:
                root = Path(tmp)
                runtime = root / 'runtime'; runtime.mkdir()
                private = root / 'private'
                record = {'resources': [{'kind': 'container', 'id': str(n), 'nonce': 'controlled'} for n in range(7)],
                          'directories': [str(private)]}
                output = original_log(selector)
                log = root / 'case.log'; log.write_text(output)
                with patch.object(self.sup, 'root_record', return_value=record), \
                        patch.object(self.sup, 'exact_absent', return_value=True) as absent, \
                        patch.object(self.sup.time, 'sleep'):
                    self.assertTrue(self.sup.observe_root_chain(root, io.StringIO(), log, selector))
                    self.assertEqual(absent.call_count, 14)
                    for changed in (output.replace('code=0', 'code=1'), output.replace('success=true', 'success=false'),
                                    output + '=== RUN   TestExtra\n'):
                        log.write_text(changed)
                        self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, selector))
                    log.write_text(output)
                    private.mkdir()
                    self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, selector))
                    private.rmdir()
                    absent.return_value = False
                    self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, selector))
                    absent.return_value = True
                    (runtime / 'residue').write_text('controlled')
                    self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, selector))


if __name__ == '__main__':
    unittest.main()
