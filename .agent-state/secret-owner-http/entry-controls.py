#!/usr/bin/env python3
"""Offline controls for the two exact Secret HTTP entries; no real resources."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
PG = '.agent-state/task-planning-recovery/pg_only_driver.go'
NATIVE = '.agent-state/work-owner-http/native_driver.go'
# Actual accepted bacbb28d bytes before this new domain; no historical Git ref
# is required to run the inverse controls in a fresh clone.
BASE = {PG: 'b8878854e1f54319611c05796a575eb1f2bd4ac1561846170deaf39cf5776bba',
        SUP: '4e52d7628c43391bbe184ae179097c42ed201185a703cef372bb7ad7bc9fa4e7',
        NATIVE: '7daba590af7fa255f9a84d82ae9be2e9569fdcd94d9746e1f5e7917a7da2592b'}
spec = importlib.util.spec_from_file_location('secret_http_supervisor', ROOT / SUP)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
root_spec = importlib.util.spec_from_file_location('secret_root_entry_controls',
    ROOT / '.agent-state/secret-owner-http/root-entry-controls.py')
root_entry = importlib.util.module_from_spec(root_spec)
root_spec.loader.exec_module(root_entry)


def remove_once(source, old, new=''):
    if source.count(old) != 1:
        raise ValueError('changed inverse anchor')
    return source.replace(old, new, 1)


def inverse(name, source):
    if name == PG:
        source = remove_once(source, 'const secretHTTPSelector = `^TestSecretVariableHTTPBoundary$`\n\n')
        source = remove_once(source, '\tif strings.Contains(*selector, "SecretVariableHTTP") && *selector != secretHTTPSelector {\n'
            '\t\treturn fail("Secret HTTP requires its exact PG-only top")\n\t}\n')
    elif name == NATIVE:
        source = remove_once(source, 'const secretHTTPNativeSelector = `^TestSecretHTTPNativeTransport$`\n\n')
        source = remove_once(source, ' && *selector != secretHTTPNativeSelector) {', ') {')
        source = remove_once(source, '\t} else if *selector == secretHTTPNativeSelector {\n'
            '\t\tnativeGate = "AGENTEAM_SECRET_VARIABLE_HTTP_NATIVE"\n')
    elif name == SUP:
        # The later root entry is a separately accepted exact increment. Remove
        # it with its real inverse before projecting this older HTTP domain.
        # That inverse checks the complete intermediate source, so unknown
        # changes cannot disappear while removing the HTTP helper block below.
        try:
            source = root_entry.inverse(SUP, source)
        except ValueError:
            return False
        begin, end = source.index('SECRET_HTTP_PG = '), source.index('def survivor_identity(')
        source = source[:begin] + source[end:]
        source = remove_once(source,
            "    if any(name in args.run for name in ('SecretVariableHTTP', 'SecretHTTPNative')) and (args.root_chain or args.run not in SECRET_HTTP_CASES):\n"
            "        parser.error('Secret HTTP requires one exact PG/native top without root mode')\n"
            "    secret_http_selected = not args.root_chain and args.run in SECRET_HTTP_CASES\n")
        source = remove_once(source, '    if secret_http_selected:\n'
            '        inputs = secret_http_inputs(args.driver, args.binary, args.run)\n')
        source = remove_once(source, '            if secret_http_selected and not observe_secret_http(directory, log, log_path, args.run):\n'
            '                code = 1\n')
        source = remove_once(source, '            if secret_http_selected:\n'
            '                try:\n'
            '                    same = secret_http_inputs(args.driver, args.binary, args.run) == inputs\n'
            '                except (OSError, ValueError):\n'
            '                    same = False\n'
            '            elif secret_owner:\n', '            if secret_owner:\n')
    else:
        raise ValueError('unknown shared source')
    return hashlib.sha256(source.encode()).hexdigest() == BASE[name]


class EntryControls(unittest.TestCase):
    def test_three_actual_sources_inverse_to_accepted_union(self):
        for name in BASE:
            with self.subTest(name=name):
                source = (ROOT / name).read_text()
                self.assertTrue(inverse(name, source))
                self.assertFalse(inverse(name, source + '# unknown shared change\n'))
        self.assertFalse(inverse(SUP, (ROOT / SUP).read_text().replace(
            "all(state == 'PASS'", "all(state != 'FAIL'", 1)))

    def test_exact_cases(self):
        self.assertEqual(sup.SECRET_HTTP_CASES, {
            '^TestSecretVariableHTTPBoundary$': {'TestSecretVariableHTTPBoundary': (
                'real-owner-protocol-and-safe-history', 'authenticated-session-revoked-before-domain')},
            '^TestSecretHTTPNativeTransport$': {'TestSecretHTTPNativeTransport': (
                'deadlines-and-keepalive', 'backpressure-and-disconnect-join')},
        })

    def test_actual_parser_rejects_alias_and_root_mode_before_resources(self):
        for selector, root_mode in [(sup.SECRET_HTTP_PG + 'x', False),
                                   ('^TestSecretVariableHTTPBoundaryExtra$', False),
                                   ('^TestSecretHTTPNativeTransportExtra$', False),
                                   (sup.SECRET_HTTP_PG, True), (sup.SECRET_HTTP_NATIVE, True)]:
            with self.subTest(selector=selector, root_mode=root_mode):
                argv = ['supervisor', '--driver', '/no-driver', '--binary', '/no-binary',
                        '--output', '/not-created', '--run', selector]
                if root_mode:
                    argv.append('--root-chain')
                with patch.object(sys, 'argv', argv), patch('sys.stderr', io.StringIO()):
                    with self.assertRaises(SystemExit) as stopped:
                        sup.main()
                    self.assertEqual(stopped.exception.code, 2)

    def fixture(self, directory, selector):
        top, subs = next(iter(sup.SECRET_HTTP_CASES[selector].items()))
        cases = [top] + [top + '/' + sub for sub in subs]
        lines = ['=== RUN   ' + name for name in cases]
        lines += ['--- PASS: ' + name + ' (0.01s)' for name in cases]
        lines += ['CHILD pid=42 selector=' + selector + (' kind=native-http' if selector == sup.SECRET_HTTP_NATIVE else ''),
                  'CHILD actual_wait pid=42 state=exit status 0']
        if selector == sup.SECRET_HTTP_PG:
            record = dict(nonce='a' * 32, container_id='b' * 64, network_id='c' * 64)
            lines += ['OWNED nonce=' + record['nonce'] + ' container=' + record['container_id'] +
                      ' network=' + record['network_id'] + ' port=11111 PostgreSQL=17 vector=0.8.1']
            lines += ['RETIRE observation=' + str(n) + ' exact_container=' + record['container_id'] +
                      ' exact_network=' + record['network_id'] + ' clean=true' for n in (1, 2)]
            lines += ['DRIVER terminal exit=0 elapsed=1s child_started=true actual_child_wait=true cleanup=true']
        else:
            record = dict(kind='work-http-native', child_pid=42)
            lines += ['NATIVE runtime_empty=true actual_child_wait=true',
                      'DRIVER terminal exit=0 elapsed=1s child_started=true actual_child_wait=true private_removed=true']
        owned = directory / 'owned.json'
        owned.write_text(json.dumps(record))
        owned.chmod(0o600)
        return '\n'.join(lines) + '\n'

    def test_observer_positive_and_bounded_negative_cases(self):
        for selector in sup.SECRET_HTTP_CASES:
            with tempfile.TemporaryDirectory(prefix='secret-http-control-') as tmp:
                directory, log = Path(tmp) / 'owned', Path(tmp) / 'output.log'
                directory.mkdir()
                original = self.fixture(directory, selector)
                def observe(value):
                    log.write_text(value)
                    return sup.observe_secret_http(directory, io.StringIO(), log, selector)
                self.assertTrue(observe(original))
                changes = [original.replace('--- PASS:', '--- SKIP:', 1),
                           original.replace('--- PASS:', '--- FAIL:', 1),
                           original + '=== RUN   TestUnexpected\n',
                           original + original.splitlines()[0] + '\n',
                           original.replace('CHILD actual_wait pid=42 state=exit status 0\n', ''),
                           original.replace('actual_wait pid=42', 'actual_wait pid=43'),
                           original.replace('DRIVER terminal exit=0', 'DRIVER terminal exit=1')]
                if selector == sup.SECRET_HTTP_PG:
                    changes += [original.replace('RETIRE observation=2', 'RETIRE observation=1'),
                                original.replace('clean=true', 'clean=false')]
                else:
                    changes += [original.replace('runtime_empty=true', 'runtime_empty=false'),
                                original.replace('private_removed=true', 'private_removed=false')]
                for number, value in enumerate(changes):
                    with self.subTest(selector=selector, mutation=number):
                        self.assertFalse(observe(value))
                (directory / 'leftover').write_text('private')
                self.assertFalse(observe(original))
                (directory / 'leftover').unlink()
                (directory / 'owned.json').chmod(0o644)
                self.assertFalse(observe(original))

    def test_input_artifacts_and_reenumeration(self):
        with tempfile.TemporaryDirectory(prefix='secret-http-input-') as tmp:
            root = Path(tmp).resolve()
            sup_path = root / SUP
            names = [SUP, PG, NATIVE, 'go.mod', 'go.sum',
                     'internal/central/projectvariable/http/secret_native_test.go',
                     'internal/central/account/assets/weak-passwords.json',
                     'tests/projectvariable/secret_http_fixture_test.go',
                     'tests/projectvariable/secret_http_test.go']
            output = root / 'output/ai/secret-owner-http/candidate-01'
            names += [str((output / name).relative_to(root)) for name in
                      ('pg-only-driver', 'secret-http-pg.test', 'native-driver', 'secret-http-native.test')]
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('fixed')
            with patch.object(sup, '__file__', str(sup_path)):
                for selector, driver, binary in [(sup.SECRET_HTTP_PG, 'pg-only-driver', 'secret-http-pg.test'),
                                                (sup.SECRET_HTTP_NATIVE, 'native-driver', 'secret-http-native.test')]:
                    with self.subTest(selector=selector):
                        before = sup.secret_http_inputs(output / driver, output / binary, selector)
                        added = root / 'internal/central/projectvariable/http/additional.go'
                        added.write_text('new')
                        self.assertNotEqual(before, sup.secret_http_inputs(output / driver, output / binary, selector))
                        added.unlink()
                        with self.assertRaises(ValueError):
                            sup.secret_http_inputs(output / 'other-driver', output / binary, selector)
                        needed = root / 'internal/central/projectvariable/http/secret_native_test.go'
                        needed.unlink()
                        with self.assertRaises(FileNotFoundError):
                            sup.secret_http_inputs(output / driver, output / binary, selector)
                        needed.symlink_to(root / 'go.mod')
                        with self.assertRaises(ValueError):
                            sup.secret_http_inputs(output / driver, output / binary, selector)
                        needed.unlink()
                        needed.write_text('fixed')


if __name__ == '__main__':
    unittest.main()
