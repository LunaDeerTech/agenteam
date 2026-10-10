#!/usr/bin/env python3
"""Installer data-profile extension of the accepted metadata PG family.
No Go, Docker or sockets; exact inverse preserves the schema/metadata donor.
"""
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
SELECTOR = '^TestSkillInstallationPersistentObject$'
TOP = 'TestSkillInstallationPersistentObject'
CASES = frozenset({TOP, TOP + '/install-read-replay', TOP + '/ordinary-cleanup'})
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': 'a2dafc36700f4caa8a2b0f7d068afc771354068903d3c1d9feea86dd2d33603d',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'e66109118867c3c477aaae481b5f34f9ba52a39de34b3800048927f3e879daee'}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [['TARGETS = {\n'
                                                        "    '^TestAgentConfigurationSchema$': 'tests/projectvariable',\n"
                                                        "    '^TestAgentConfigurationMetadata$': 'tests/projectvariable',\n",
                                                        '# One closed same-package PG family: adding a scenario changes required\n'
                                                        '# inputs and expected test data, never the resource/Wait/tail implementation.\n'
                                                        'METADATA_INPUTS = {\n'
                                                        "    '^TestAgentConfigurationMetadata$': (\n"
                                                        "        'tests/projectvariable/agent_configuration_metadata_test.go',\n"
                                                        "        '.agent-state/agent-configuration-metadata/README.md',\n"
                                                        "        '.agent-state/agent-configuration-metadata/metadata-entry-controls.py'),\n"
                                                        "    '^TestAgentConfigurationSchema$': (\n"
                                                        "        'tests/projectvariable/agent_configuration_schema_test.go',\n"
                                                        "        'tests/testsupport/agentconfiguration/assembly.go'),\n"
                                                        "    '^TestSkillInstallationPersistentObject$': (\n"
                                                        "        'tests/projectvariable/skill_installation_test.go',),\n"
                                                        '}\n'
                                                        'TARGETS = {\n'
                                                        "    **dict.fromkeys(METADATA_INPUTS, 'tests/projectvariable'),\n"],
                                                       ["    if selector not in ('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n",
                                                        '    if selector not in METADATA_INPUTS:\n'],
                                                       ["    if selector == '^TestAgentConfigurationSchema$':\n"
                                                        '        # Runtime/compiled source only. Task records and pure controls do not\n'
                                                        "        # alter this run's inputs or force another candidate build.\n"
                                                        '        paths.update(REPOSITORY / name for name in (\n'
                                                        "            'tests/projectvariable/agent_configuration_schema_test.go',\n"
                                                        "            'tests/testsupport/agentconfiguration/assembly.go'))\n"
                                                        '    else:\n'
                                                        '        paths.update(REPOSITORY / name for name in (\n'
                                                        "            'tests/projectvariable/agent_configuration_metadata_test.go',\n"
                                                        "            '.agent-state/agent-configuration-metadata/README.md',\n"
                                                        '            '
                                                        "'.agent-state/agent-configuration-metadata/metadata-entry-controls.py'))\n",
                                                        '    # Only the explicitly retained legacy metadata profile includes its\n'
                                                        '    # historical records. New profiles contain actual compiled/runtime inputs.\n'
                                                        '    paths.update(REPOSITORY / name for name in METADATA_INPUTS[selector])\n'],
                                                       ["    if args.run in ('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n",
                                                        '    if args.run in METADATA_INPUTS:\n']],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [['METADATA_GROUPS = {METADATA_ROOT: METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES}\n',
                                                                'METADATA_GROUPS = {\n'
                                                                '    METADATA_ROOT: METADATA_CASES,\n'
                                                                '    SCHEMA_ROOT: SCHEMA_CASES,\n'
                                                                "    '^TestSkillInstallationPersistentObject$': frozenset({\n"
                                                                "        'TestSkillInstallationPersistentObject',\n"
                                                                "        'TestSkillInstallationPersistentObject/install-read-replay',\n"
                                                                "        'TestSkillInstallationPersistentObject/ordinary-cleanup',\n"
                                                                '    }),\n'
                                                                '}\n'],
                                                               ["        METADATA_ROOT: {'TestAgentConfigurationMetadata'},\n"
                                                                "        SCHEMA_ROOT: {'TestAgentConfigurationSchema'},\n",
                                                                "        **{key: {name for name in cases if '/' not in name}\n"
                                                                '           for key, cases in METADATA_GROUPS.items()},\n'],
                                                               ["    if any(name in args.run for name in ('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema')) and (args.run not in METADATA_GROUPS or not "
                                                                'args.root_chain):\n',
                                                                '    if any(selector[1:-1] in args.run for selector in METADATA_GROUPS) '
                                                                'and (args.run not in METADATA_GROUPS or not args.root_chain):\n']]}


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous Installer data-profile delta')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown shared baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def output(selector=SELECTOR, cases=CASES):
    return (''.join('=== RUN   ' + name + '\n--- PASS: ' + name + ' (0.01s)\n' for name in sorted(cases))
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + selector + '\n')


class InstallEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver, cls.sup = load('install_driver', DRIVER), load('install_supervisor', SUP)

    def test_only_closed_profile_delta(self):
        schema = load('install_schema_donor', '.agent-state/agent-system-integration/schema-entry-controls.py')
        metadata = load('install_metadata_control', '.agent-state/agent-configuration-metadata/metadata-entry-controls.py')
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            restored = inverse(name, source)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            schema.inverse(name, restored)
            metadata.inverse(name, source)
            for bad in (source + '\n# unknown change\n', source.replace('540', '541', 1)):
                if bad != source:
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
            for old, new in SOURCE_HUNKS[name]:
                for bad in (source.replace(new, old, 1), source + new):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        self.assertEqual(set(self.driver.METADATA_INPUTS), set(self.sup.METADATA_GROUPS))
        self.assertEqual(self.sup.METADATA_GROUPS[SELECTOR], CASES)
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.budgets(True), (540, 60))

    def test_common_results_and_mode_reject_missing_or_wrong_case(self):
        for selector, cases in self.sup.METADATA_GROUPS.items():
            good = output(selector, cases)
            self.assertTrue(self.sup.metadata_results(good, selector))
            for name in cases:
                run, passed = '=== RUN   ' + name + '\n', '--- PASS: ' + name + ' (0.01s)\n'
                for bad in (good.replace(run, ''), good.replace(passed, ''), good + run, good + passed,
                            good.replace(passed, passed.replace('PASS', 'SKIP')),
                            good.replace(passed, passed.replace('PASS', 'FAIL'))):
                    self.assertFalse(self.sup.metadata_results(bad, selector))
            wait = good.splitlines(True)[-1]
            for bad in (good.replace('code=0', 'code=1'), good.replace(wait, ''), good + wait,
                        good.replace('pid=42', 'pid=0'), good + 'FAIL\n'):
                self.assertFalse(self.sup.metadata_results(bad, selector))
        self.assertFalse(self.sup.metadata_results(output(), self.sup.SCHEMA_ROOT))
        base = ['supervisor', '--driver', '/unused/driver', '--binary', '/unused/candidate', '--output', '/unused/out', '--run']
        for selector, root in ((SELECTOR, False), (SELECTOR + 'x', True), (TOP, True),
                               ('^' + TOP + '(Extra)?$', True)):
            with patch.object(sys, 'argv', base + [selector] + (['--root-chain'] if root else [])), \
                    patch.object(self.sup, 'budgets') as budget, patch.object(self.sup.subprocess, 'Popen') as spawn, \
                    patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit):
                    self.sup.main()
                budget.assert_not_called(); spawn.assert_not_called()

    def test_actual_collector_required_sources_and_reenumeration(self):
        with tempfile.TemporaryDirectory(prefix='install-entry-input-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'tests/projectvariable/skill_installation_test.go',
                     'tests/projectvariable/other_test.go', 'tests/testsupport/original.go',
                     'internal/central/skill/installation.go')
            for name in names:
                path = root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('source')
            binary = root / names[0]
            with patch.object(self.driver, 'REPOSITORY', root), patch.object(self.driver, 'input_paths', return_value=[binary]):
                inputs = {str(p): self.driver.sha(p) for p in self.driver.metadata_inputs(binary, SELECTOR)}
                self.assertEqual(set(inputs), {str(root / name) for name in names})
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                with self.assertRaises(ValueError):
                    self.driver.metadata_inputs(binary, self.sup.SCHEMA_ROOT)
                added = root / 'tests/projectvariable/later_test.go'; added.write_text('later')
                self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR)); added.unlink()
                required = root / names[1]; required.unlink()
                self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                required.symlink_to(binary)
                self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))

    def test_actual_observer_preserves_original_tails(self):
        with tempfile.TemporaryDirectory(prefix='install-entry-observer-') as tmp:
            root = Path(tmp); runtime = root / 'runtime'; runtime.mkdir(); private = root / 'private'
            record = {'resources': [{'kind': 'container', 'id': str(n), 'nonce': 'controlled'} for n in range(7)],
                      'directories': [str(private)]}
            log = root / 'case.log'; log.write_text(output())
            with patch.object(self.sup, 'root_record', return_value=record), \
                    patch.object(self.sup, 'exact_absent', return_value=True) as absent, patch.object(self.sup.time, 'sleep'):
                self.assertTrue(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                self.assertEqual(absent.call_count, 14)
                log.write_text(output().replace('--- PASS: ' + TOP + '/ordinary-cleanup (0.01s)\n', ''))
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR)); log.write_text(output())
                private.mkdir()
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR)); private.rmdir()
                absent.return_value = False
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR)); absent.return_value = True
                (runtime / 'pending').write_text('held')
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))


if __name__ == '__main__':
    unittest.main()
