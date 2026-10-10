#!/usr/bin/env python3
"""Offline controls for the frozen D12 entry increment, never browser/PG."""
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
SELECTOR = '^TestKnowledgeOwnerReadWeb$'
# Actual main 3a7a3fb5 bytes, including the accepted Secret HTTP/root entries.
# Removing only D12 must recover this complete source, never a generated base.
BASE = {DRIVER: 'e784286b7e9debaf47aaac2f50e4a6c89a883231cff1c4e4063e46fdad91421c',
        SUP: '4335ca66ac391db8cd20c67850a35b79ae8f286f7d65ab2ada7e66d6d8e2ad29'}
# Only these exact new helper blocks may be removed during inverse projection.
BLOCKS = {DRIVER: ('def metadata_cost_inputs(', '73695a584b7ae85c4f302dc1f130306aa0d2fa503090ec4cf76c7a49bc2766a4'),
          SUP: ('def root_composition_results(', '1621a08b71d67bbac397b7dbccd838a15bc4f6bb2f3e41989f85eaabd514dd3b')}


def inverse(name, source):
    if "'^TestProjectLifecycleStopBatchRealGuard$'" in source:
        guard = load('legacy_guard_inverse', '.agent-state/project-variable-lifecycle/guard-entry-controls.py')
        source = guard.inverse(name, source)
    if name not in BASE:
        raise ValueError('unknown source')
    # The later Parser entry has its own exact inverse to accepted main
    # 04455194. It imports only adapters, so this projection remains acyclic.
    if "'^TestKnowledgePlainTextParserIntegration$'" in source:
        source = parser_entry.inverse(name, source)
    end, digest = BLOCKS[name]
    first, last = source.index('KNOWLEDGE_UI ='), source.index(end)
    if hashlib.sha256(source[first:last].encode()).hexdigest() != digest:
        raise ValueError('unknown helper change')
    source = source[:first] + source[last:]
    changes = [
        ("    '^TestKnowledgeOwnerReadWeb$': 'internal/central/app',\n", ''),
        ("    plan = {'binary': str(binary.resolve()), 'selector': selector,\n",
         "    return {'binary': str(binary.resolve()), 'selector': selector,\n"),
        ("    if selector == KNOWLEDGE_UI:\n"
         "        plan['knowledge_ui'] = knowledge_ui_configuration(directory)\n"
         "    return plan\n", ''),
        ("    if args.run == KNOWLEDGE_UI:\n"
         "        prepare_history_go_environment(directory, env)\n"
         "        ui = plan['knowledge_ui']\n"
         "        Path(ui['AGENTEAM_KNOWLEDGE_OWNER_WEB_EVIDENCE']).mkdir(mode=0o700)\n"
         "        env.update(ui)\n"
         "        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),\n"
         "                    'AGENTEAM_KNOWLEDGE_OWNER_WEB_INPUT_HASH': knowledge_ui_input_hash(args.test_binary),\n"
         "                    'PATH': str(KNOWLEDGE_NODE.parent) + os.pathsep + env.get('PATH', '')})\n", ''),
    ] if name == DRIVER else [
        ("        KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'},\n", ''),
        ("    if selector == KNOWLEDGE_UI:\n"
         "        complete = knowledge_ui_results(output)\n"
         "        log.write(f'ROOT knowledge_ui_exact_run_pass_wait={complete}\\n')\n"
         "        good = good and complete\n", ''),
        ("    if 'KnowledgeOwnerReadWeb' in args.run and (args.run != KNOWLEDGE_UI or not args.root_chain):\n"
         "        parser.error('Knowledge UI requires its exact original root-chain entry')\n", ''),
        ("    stem = ('ui-' + uuid.uuid4().hex[:16]) if args.run == KNOWLEDGE_UI else ('pg-' + uuid.uuid4().hex)\n"
         "    directory = args.output.resolve() / stem\n"
         "    if args.run == KNOWLEDGE_UI:\n"
         "        try:\n"
         "            adapter.knowledge_ui_configuration(directory)\n"
         "            args.knowledge_ui_environment = adapter.knowledge_ui_environment()\n"
         "        except (OSError, ValueError):\n"
         "            parser.error('exact frozen Knowledge assets, interpreters and fresh evidence required')\n"
         "    args.output.mkdir(parents=True, exist_ok=True)\n",
         "    args.output.mkdir(parents=True, exist_ok=True)\n"
         "    stem = 'pg-' + uuid.uuid4().hex\n"
         "    directory = args.output.resolve() / stem\n"),
        ("    if args.run == KNOWLEDGE_UI:\n"
         "        inputs = {str(p): adapter.sha(p) for p in adapter.knowledge_ui_inputs(args.binary)}\n", ''),
        ("            if args.run == KNOWLEDGE_UI and child.returncode is not None:\n"
         "                if not knowledge_ui_reap_exited(log):\n"
         "                    code = 1\n", ''),
        ("            if args.run == KNOWLEDGE_UI:\n"
         "                same = same and knowledge_ui_same(inputs, args, adapter)\n", ''),
    ]
    for added, old in changes:
        if source.count(added) != 1:
            raise ValueError('changed inverse anchor')
        source = source.replace(added, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE[name]:
        raise ValueError('unknown baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


driver, sup = load('knowledge_ui_driver', DRIVER), load('knowledge_ui_supervisor', SUP)
parser_entry = load('knowledge_ui_parser_entry',
                    '.agent-state/d13-plain-text-parser/entry-controls.py')


class EntryControls(unittest.TestCase):
    def test_actual_inverse_and_unknown_changes(self):
        for name in BASE:
            source = (ROOT / name).read_text()
            ast.parse(source)
            original = inverse(name, source)
            ast.parse(original)
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown\n')
            with self.assertRaises(ValueError):
                inverse(name, source.replace('KNOWLEDGE_UI =', '# unknown\nKNOWLEDGE_UI =', 1))
        source = (ROOT / SUP).read_text()
        begin, end = source.index('def secret_root_results('), source.index('KNOWLEDGE_UI =')
        secret_block = source[begin:end]
        self.assertEqual(secret_block.count("all(state == 'PASS'"), 1)
        with self.assertRaises(ValueError):
            inverse(SUP, source[:begin] + secret_block.replace(
                "all(state == 'PASS'", "all(state != 'FAIL'", 1) + source[end:])
        for old, new in (("('0', PARSER_PG)", "('1', PARSER_PG)"),
                         ("SECRET_ROOT: {'TestProjectSecretVariablesDefaultRoot'}",
                          "SECRET_ROOT: set()"),
                         ("KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'}",
                          "KNOWLEDGE_UI: set()")):
            self.assertEqual(source.count(old), 1)
            with self.assertRaises(ValueError):
                inverse(SUP, source.replace(old, new, 1))

    def test_target_and_old_budgets(self):
        self.assertEqual(driver.TARGETS[SELECTOR], 'internal/central/app')
        old = {'__file__': str(ROOT / DRIVER), '__name__': 'baseline'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), old)
        self.assertEqual(driver.TARGETS[parser_entry.SELECTOR], 'tests/knowledge')
        self.assertEqual({k: v for k, v in driver.TARGETS.items()
                          if k not in (SELECTOR, parser_entry.SELECTOR,
                                       '^TestProjectLifecycleStopBatchRealGuard$')}, old['TARGETS'])
        self.assertEqual(sup.budgets(True), (540, 60))
        self.assertEqual(sup.budgets(False), (123, 3))

    def test_one_top_node_and_go_actual_waits(self):
        output = ('=== RUN   TestKnowledgeOwnerReadWeb\n'
                  '    knowledge_owner_web_test.go:547: Knowledge Node actual_wait pid=43 success=true\n'
                  '--- PASS: TestKnowledgeOwnerReadWeb (1.0s)\n'
                  'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n')
        self.assertTrue(sup.knowledge_ui_results(output))
        for changed in (output.replace('--- PASS:', '--- SKIP:'), output.replace('--- PASS:', '--- FAIL:'),
                        output.replace('success=true', 'success=false'), output.replace('code=0', 'code=1'),
                        output.replace('pid=43', 'pid=0'), output + 'FAIL\n',
                        output + '=== RUN   TestKnowledgeOwnerReadWeb/extra\n',
                        output + output.splitlines()[0] + '\n', output + output.splitlines()[1] + '\n',
                        output + output.splitlines()[-1] + '\n', '\n'.join(output.splitlines()[:-1])):
            self.assertFalse(sup.knowledge_ui_results(changed))

    def test_mode_and_alias_rejected_before_resources(self):
        for selector, root in ((SELECTOR, False), (SELECTOR + 'x', True), ('^TestKnowledgeOwnerReadWebExtra$', False)):
            args = ['supervisor', '--driver', '/absent', '--binary', '/absent', '--output', '/not-created', '--run', selector]
            if root:
                args.append('--root-chain')
            with patch.object(sys, 'argv', args), patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit) as stopped:
                    sup.main()
                self.assertEqual(stopped.exception.code, 2)

    def test_actual_root_observer_composes_exact_results_and_resource_gates(self):
        output = ('=== RUN   TestKnowledgeOwnerReadWeb\n'
                  '    knowledge_owner_web_test.go:547: Knowledge Node actual_wait pid=43 success=true\n'
                  '--- PASS: TestKnowledgeOwnerReadWeb (1.0s)\n'
                  'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n')
        with tempfile.TemporaryDirectory(prefix='d12-observer-') as tmp:
            directory = Path(tmp)
            runtime = directory / 'runtime'; runtime.mkdir()
            private = [runtime / str(n) for n in range(3)]
            resources = []
            groups = {'agenteam.d05.objectfixture': ('container', 'network'),
                      'agenteam.d04.networkfixture': ('container', 'network'),
                      'agenteam.d03.fixture': ('container', 'container', 'network')}
            for index, (label, kinds) in enumerate(groups.items()):
                for kind in kinds:
                    resources.append({'kind': kind, 'label': label, 'nonce': format(index + 1, '032x'),
                                      'id': format(len(resources) + 1, '064x')})
            owned = directory / 'owned.json'
            owned.write_text(json.dumps({'kind': 'work-owner-root-chain', 'resources': resources,
                                         'directories': [str(p) for p in private]})); owned.chmod(0o600)
            log = directory / 'test.log'; log.write_text(output)
            with patch.object(sup, 'exact_absent', return_value=True) as absent, patch.object(sup.time, 'sleep'):
                self.assertTrue(sup.observe_root_chain(directory, io.StringIO(), log, SELECTOR))
                self.assertEqual(absent.call_count, 14)
                self.assertFalse(sup.skill_cleanup_results(output, SELECTOR))
                log.write_text(output + '=== RUN   TestUnexpected\n')
                self.assertFalse(sup.observe_root_chain(directory, io.StringIO(), log, SELECTOR))
                log.write_text(output.replace('code=0', 'code=1'))
                self.assertFalse(sup.observe_root_chain(directory, io.StringIO(), log, SELECTOR))
                log.write_text(output)
                private[0].mkdir()
                self.assertFalse(sup.observe_root_chain(directory, io.StringIO(), log, SELECTOR))
                private[0].rmdir()
                absent.return_value = False
                self.assertFalse(sup.observe_root_chain(directory, io.StringIO(), log, SELECTOR))

    def fixture(self, root):
        owned = root / 'output/ai/knowledge-owner-ui'
        dist = owned / 'dist-read-01'
        dist.mkdir(parents=True)
        (dist / 'index.html').write_text('frozen')
        env = dict(zip(driver.KNOWLEDGE_ENV, (str(dist), str(owned / 'evidence-01'), str(driver.KNOWLEDGE_PYTHON), 'read')))
        return owned, dist, env

    def test_fresh_evidence_short_runtime_and_exact_environment(self):
        with tempfile.TemporaryDirectory(prefix='d12-control-') as tmp:
            root = Path(tmp)
            owned, dist, env = self.fixture(root)
            with patch.object(driver, 'REPOSITORY', root), patch.dict(os.environ, env):
                self.assertEqual(driver.knowledge_ui_configuration(Path('/tmp/ku/ui-123')), env)
                with self.assertRaises(ValueError):
                    driver.knowledge_ui_configuration(Path('/tmp/' + 'x' * 45))
                evidence = Path(env[driver.KNOWLEDGE_ENV[1]])
                evidence.mkdir()
                with self.assertRaises(ValueError):
                    driver.knowledge_ui_configuration(Path('/tmp/ku/ui-123'))
                evidence.rmdir()
                with patch.dict(os.environ, {driver.KNOWLEDGE_ENV[3]: 'other'}):
                    with self.assertRaises(ValueError): driver.knowledge_ui_environment()
                with patch.dict(os.environ, {driver.KNOWLEDGE_ENV[2]: '/usr/bin/python3'}):
                    with self.assertRaises(ValueError): driver.knowledge_ui_environment()
                (dist / 'alias.js').symlink_to(dist / 'index.html')
                with self.assertRaises(ValueError): driver.knowledge_ui_assets()

    def test_real_input_enumerator_closed_runtime_sources(self):
        with tempfile.TemporaryDirectory(prefix='d12-input-') as tmp:
            root = Path(tmp)
            _, dist, env = self.fixture(root)
            harness = root / 'tests/account-captcha-web'
            names = ['candidate.test', 'internal/central/app/knowledge_owner_web_test.go',
                     'web/src/knowledge.ts', 'web/package.json', 'web/package-lock.json',
                     'web/node_modules/typescript/package.json', 'web/node_modules/typescript/lib/typescript.js']
            names += ['api/openapi/' + name for name in ('common.json', 'knowledge-owner.json', 'knowledge-content.json')]
            names += ['tests/account-captcha-web/' + name for name in ('knowledge-owner-read.config.js', 'package.json', 'package-lock.json',
                      'e2e/knowledge-owner-read.spec.ts', 'e2e/knowledge-owner-read.native.ts', 'node_modules/@playwright/test/cli.js')]
            for name in names:
                path = root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('fixed')
            for name in ('@playwright/test', 'playwright', 'playwright-core'):
                path = harness / 'node_modules' / name / 'package.json'
                path.parent.mkdir(parents=True, exist_ok=True); path.write_text(json.dumps({'version': '1.56.1'}))
            binary = root / 'candidate.test'
            with patch.object(driver, 'REPOSITORY', root), patch.object(driver, 'input_paths', return_value=[binary]), patch.dict(os.environ, env):
                before = set(driver.knowledge_ui_inputs(binary))
                self.assertTrue({root / n for n in names} <= before)
                extra = harness / 'node_modules/playwright/new.js'; extra.write_text('new')
                self.assertEqual(set(driver.knowledge_ui_inputs(binary)) - before, {extra})
                extra.unlink()
                required = harness / 'e2e/knowledge-owner-read.native.ts'; required.unlink()
                with self.assertRaises(ValueError): driver.knowledge_ui_inputs(binary)

    def test_same_inputs_bytes_paths_and_environment(self):
        with tempfile.TemporaryDirectory(prefix='d12-same-') as tmp:
            path = Path(tmp) / 'source'; path.write_text('original')
            env = ('dist', 'evidence', 'python', 'read')
            adapter = SimpleNamespace(knowledge_ui_inputs=lambda _: [path], knowledge_ui_environment=lambda: env, sha=driver.sha)
            args = SimpleNamespace(binary=path, knowledge_ui_environment=env)
            frozen = {str(path): driver.sha(path)}
            self.assertTrue(sup.knowledge_ui_same(frozen, args, adapter))
            path.write_text('changed'); self.assertFalse(sup.knowledge_ui_same(frozen, args, adapter))
            path.write_text('original'); args.knowledge_ui_environment = ('wrong',)
            self.assertFalse(sup.knowledge_ui_same(frozen, args, adapter))
            args.knowledge_ui_environment = env; frozen['extra'] = 'unrecognized'
            self.assertFalse(sup.knowledge_ui_same(frozen, args, adapter))

    def test_finite_actual_adopted_wait_preserves_failure(self):
        for returns, expected in (([(42, 0), (43, 0)], True), ([(42, 256), (43, 0)], False), ([(0, 0)], True)):
            log = io.StringIO()
            with patch.object(sup, 'descendants', return_value={42, 43}), patch.object(sup.os, 'waitpid', side_effect=returns) as wait:
                self.assertEqual(sup.knowledge_ui_reap_exited(log), expected)
                self.assertLessEqual(wait.call_count, 2)
                for call in wait.call_args_list: self.assertEqual(call.args, (-1, os.WNOHANG))


if __name__ == '__main__':
    unittest.main()
