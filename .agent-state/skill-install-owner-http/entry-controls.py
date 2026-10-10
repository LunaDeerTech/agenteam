#!/usr/bin/env python3
"""One HTTP profile in the existing PG family; no Go, Docker or sockets.

Only required source paths and expected cases change in the actual entry.
Reuse the accepted Installer controls for common dispatch and observer gates.
"""
import ast
import hashlib
import importlib.util
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestSkillInstallationOwnerHTTP$'
COMBINED = '^TestSkillInstallation(PersistentObject|OwnerHTTP)$'
TOP = 'TestSkillInstallationOwnerHTTP'
CASES = frozenset({TOP, TOP + '/install-lookup-catalog-and-read', TOP + '/current-owner-and-csrf'})
COMBINED_CASES = CASES | frozenset({'TestSkillInstallationPersistentObject',
    'TestSkillInstallationPersistentObject/install-read-replay',
    'TestSkillInstallationPersistentObject/ordinary-cleanup'})
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': 'd7b4b47d9c39ee72d71b4a87b7a429f3d0a3a2675f1ddf6102d9e961a340e2f4',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': '271eb7bbd814d23e6cf5d92d1e6a94141c3a8a51ec41b2e41970746acf0c5574'}
ADDED = {
    DRIVER: "    '^TestSkillInstallationOwnerHTTP$': (\n"
            "        'tests/projectvariable/skill_installation_http_test.go',\n"
            "        'tests/projectvariable/skill_installation_test.go'),\n"
            "    '^TestSkillInstallation(PersistentObject|OwnerHTTP)$': (\n"
            "        'tests/projectvariable/skill_installation_http_test.go',\n"
            "        'tests/projectvariable/skill_installation_test.go'),\n",
    SUP: "    '^TestSkillInstallationOwnerHTTP$': frozenset({\n"
         "        'TestSkillInstallationOwnerHTTP',\n"
         "        'TestSkillInstallationOwnerHTTP/install-lookup-catalog-and-read',\n"
         "        'TestSkillInstallationOwnerHTTP/current-owner-and-csrf',\n"
         "    }),\n"
         "    '^TestSkillInstallation(PersistentObject|OwnerHTTP)$': frozenset({\n"
         "        'TestSkillInstallationPersistentObject',\n"
         "        'TestSkillInstallationPersistentObject/install-read-replay',\n"
         "        'TestSkillInstallationPersistentObject/ordinary-cleanup',\n"
         "        'TestSkillInstallationOwnerHTTP',\n"
         "        'TestSkillInstallationOwnerHTTP/install-lookup-catalog-and-read',\n"
         "        'TestSkillInstallationOwnerHTTP/current-owner-and-csrf',\n"
         "    }),\n",
}
MODE_GUARD = ("    if 'TestSkillInstallation' in args.run and (args.run not in METADATA_GROUPS or not args.root_chain):\n"
              "        parser.error('Skill installation requires one exact original root-chain profile')\n")


def inverse(name, source):
    if isinstance(source, str) and "'^TestProjectSecretOwnerWeb$'" in source:
        secret = load('http_secret_ui_inverse', '.agent-state/secret-owner-ui/entry-controls.py')
        source = secret.inverse(name, source)
    if name not in BASE_SHA or not isinstance(source, str) or source.count(ADDED[name]) != 1:
        raise ValueError('unknown or duplicated HTTP profile')
    source = source.replace(ADDED[name], '', 1)
    if name == SUP:
        if source.count(MODE_GUARD) != 1:
            raise ValueError('missing or duplicated installation mode guard')
        source = source.replace(MODE_GUARD, '', 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown shared baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class HTTPEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('http_entry_driver', DRIVER)
        cls.sup = load('http_entry_supervisor', SUP)
        cls.install = load('http_entry_install_controls', '.agent-state/skill-installation/entry-controls.py')

    def test_only_closed_profiles_and_existing_inverse_chain(self):
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            ast.parse(source)
            restored = inverse(name, source)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            self.install.inverse(name, source)
            for bad in (source + '\n# unknown change\n', source + ADDED[name],
                        source.replace(ADDED[name], '', 1)):
                with self.assertRaises(ValueError):
                    inverse(name, bad)
        raw = (ROOT / SUP).read_text()
        for old, new in (('(540, 60) if root_chain', '(541, 60) if root_chain'),
                         ("waits[0][1:] == ('0', selector)\n            and sum", "waits[0][1:] == ('1', selector)\n            and sum"),
                         ('same = same and metadata_same(inputs, args, adapter, args.run)', 'same = True')):
            self.assertEqual(raw.count(old), 1)
            with self.assertRaises(ValueError):
                inverse(SUP, raw.replace(old, new))
        self.assertEqual(set(self.driver.METADATA_INPUTS), set(self.sup.METADATA_GROUPS))
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[SELECTOR], CASES)
        self.assertEqual(self.sup.METADATA_GROUPS[COMBINED], COMBINED_CASES)
        self.assertEqual(self.driver.TARGETS[COMBINED], 'tests/projectvariable')
        self.assertEqual(self.sup.budgets(True), (540, 60))

    def test_original_family_results_and_actual_mode_dispatch(self):
        # Includes old metadata/Installer cases and the new HTTP cases.
        with patch.multiple(self.install, SELECTOR=SELECTOR, TOP=TOP, CASES=CASES):
            self.install.InstallEntryControls.test_common_results_and_mode_reject_missing_or_wrong_case(self)
        with patch.multiple(self.install, SELECTOR=COMBINED,
                            TOP='TestSkillInstallation(PersistentObject|OwnerHTTP)', CASES=COMBINED_CASES):
            self.install.InstallEntryControls.test_common_results_and_mode_reject_missing_or_wrong_case(self)

    def test_actual_required_inputs_and_final_reenumeration(self):
        with tempfile.TemporaryDirectory(prefix='skill-http-entry-inputs-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'tests/projectvariable/skill_installation_http_test.go',
                     'tests/projectvariable/skill_installation_test.go',
                     'tests/projectvariable/fixture_test.go', 'tests/testsupport/original.go',
                     'internal/central/skill/http/management.go')
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('source')
            binary = root / names[0]
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'input_paths', return_value=[binary]):
                inputs = {str(p): self.driver.sha(p) for p in self.driver.metadata_inputs(binary, SELECTOR)}
                self.assertEqual(set(inputs), {str(root / name) for name in names})
                self.assertEqual(self.driver.metadata_inputs(binary, COMBINED),
                                 self.driver.metadata_inputs(binary, SELECTOR))
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                for bad in (TOP, SELECTOR + 'x', '^TestOther$'):
                    with self.assertRaises(ValueError):
                        self.driver.metadata_inputs(binary, bad)
                later = root / 'tests/projectvariable/later_test.go'
                later.write_text('new source')
                self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                later.unlink()
                for name in names[1:3]:
                    required = root / name
                    required.write_text('changed')
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    required.unlink()
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    required.symlink_to(binary)
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    required.unlink()
                    required.write_text('source')
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))

    def test_original_observer_requires_cases_wait_and_resource_tails(self):
        original = self.install.output
        # The reused method removes this exact sub result as its negative case.
        source = self.install.InstallEntryControls.test_actual_observer_preserves_original_tails
        # Its only Installer-specific negative name is substituted in a local
        # function body; neither actual entry nor observer implementation changes.
        import inspect
        import textwrap
        body = textwrap.dedent(inspect.getsource(source))
        self.assertEqual(body.count('/ordinary-cleanup'), 1)
        body = body.replace('/ordinary-cleanup', '/current-owner-and-csrf')
        for selector, cases in ((SELECTOR, CASES), (COMBINED, COMBINED_CASES)):
            def output(selector=selector, cases=cases):
                return original(selector, cases)
            namespace = dict(vars(self.install), SELECTOR=selector, TOP=TOP, CASES=cases, output=output)
            exec(compile(body, '<existing-installer-observer-control>', 'exec'), namespace)
            namespace[source.__name__](self)


if __name__ == '__main__':
    unittest.main()
