#!/usr/bin/env python3
"""Offline app-check controls; no subprocess, socket, /proc or TCP sampling."""
import ast
import contextlib
import hashlib
import importlib.util
import inspect
import io
from pathlib import Path
import signal
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/owner-feature-integration/final_check.py'
spec = importlib.util.spec_from_file_location('app_final_check', SOURCE)
entry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(entry)
BASE_SHA = 'ef28fbf4cd18a4c87165fa6340c3bbb73842a087c5c07393f833d793f7c15566'
TCP_SHA = '6ee4b0336bd834595497462643fed11467b1f1984f80d57084758ffe51b9c5f9'
REAP_SHA = '1c802fa53278657454621f7b431d909f8d8acc308a4c684358286871d2e638d6'
APP_FUNCTION = '''def app_repair_script(source):
    # Freeze the original Go/version/cwd header; audit_repair_script verifies
    # both unique test lines and the complete original remaining-stage tail.
    header = source.split('"$AGENTEAM_GO" test ./...\\n', 1)[0]
    if hashlib.sha256(header.encode()).hexdigest() != 'b9bc2651561468ff83a9af6447cf47c56e984210821c2c4adcea24b9707d485e':
        raise ValueError('original check header changed')
    return audit_repair_script(source).replace('./internal/central/audit/http', './internal/central/app')


'''
MODE = '''    mode.add_argument('--app-repair', action='store_true',
                      help='run the original ordinary/race stages for the entire app package')
'''
DISPATCH = '''    elif args.app_repair:
        command = ['sh', '-c', app_repair_script((ROOT / 'scripts/check-go.sh').read_text()),
                   'scripts/check-go.sh']
'''


def inverse(source):
    changes = (
        (APP_FUNCTION, ''), (MODE, ''), (DISPATCH, ''),
        ('audit_repair=args.audit_repair, app_repair=args.app_repair, outer_pid=os.getpid()',
         'audit_repair=args.audit_repair, outer_pid=os.getpid()'),
        ("'            if secret_http_selected:\\n                try:\\n                    same ='",
         "'            if secret_owner:\\n                try:\\n                    same ='"),
    )
    for new, old in changes:
        if source.count(new) != 1:
            raise ValueError('unknown app-check increment')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA:
        raise ValueError('unknown existing wrapper change')
    return source


class StopBeforePreflight(Exception):
    pass


class AppCheckControls(unittest.TestCase):
    def setUp(self):
        self.script = (ROOT / 'scripts/check-go.sh').read_text()
        self.ordinary = '"$AGENTEAM_GO" test ./...\n'
        self.race = '"$AGENTEAM_GO" test -race ./...\n'
        self.header = self.script.split(self.ordinary)[0]

    def dispatch(self, modes):
        commands = []
        def stop():
            # Exercise the actual main parser and mode branch, then stop at the
            # first preflight, before executable identity checks or resources.
            commands.append(inspect.currentframe().f_back.f_locals['command'])
            raise StopBeforePreflight()
        argv = ['final_check.py', '--output',
                str(ROOT / 'output/ai/owner-feature-integration/app-control'),
                '--source', 'controlled-source'] + modes
        with patch.object(sys, 'argv', argv), patch.object(entry.os, 'chdir'), \
                patch.object(entry, 'schema_environment', new=stop):
            with self.assertRaises(StopBeforePreflight):
                entry.main()
        return commands[0]

    def test_actual_dispatch_and_old_mode_scripts(self):
        self.assertEqual(self.dispatch([]), ['sh', 'scripts/check-go.sh'])
        self.assertEqual(self.dispatch(['--remaining']),
                         ['sh', '-c', self.script.replace(self.ordinary, '', 1), 'scripts/check-go.sh'])
        for flag, package in (('--audit-repair', 'audit/http'), ('--app-repair', 'app')):
            expected = self.header + (self.ordinary + self.race).replace('./...', './internal/central/' + package)
            self.assertEqual(self.dispatch([flag]), ['sh', '-c', expected, 'scripts/check-go.sh'])

    def test_actual_parser_rejects_conflicting_or_unknown_modes_before_preflight(self):
        for modes in (['--app-repair', '--remaining'], ['--app-repair', '--audit-repair'],
                      ['--audit-repair', '--remaining'], ['--app-repair', '--package', './other']):
            argv = ['final_check.py', '--output', '/not-used', '--source', 'controlled'] + modes
            with patch.object(sys, 'argv', argv), patch.object(entry, 'schema_environment') as preflight, \
                    patch.object(entry.os, 'chdir'), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    entry.main()
                preflight.assert_not_called()

    def test_script_drift_is_rejected(self):
        mutations = (
            self.script.replace('set -eu', 'set -u'),
            self.script.replace('go1.27.1', 'go1.27.2'),
            self.script.replace(self.ordinary, ''),
            self.script.replace(self.ordinary, self.ordinary * 2),
            self.script.replace(self.race, ''),
            self.script.replace(self.race, self.race * 2),
            self.script.replace('vet -tags=integration', 'vet -tags=other'),
            self.script.replace('sh scripts/build-go.sh\n', 'true\n'),
            self.script + 'true\n',
        )
        for source in mutations:
            with self.assertRaises(ValueError):
                entry.app_repair_script(source)

    def test_exact_inverse_and_fragment_identity(self):
        source = SOURCE.read_text()
        ast.parse(source)
        inverse(source)
        for changed in (source.replace('code = child.wait()', 'code = 0'),
                        source + '\n# unknown wrapper hunk\n'):
            with self.assertRaises(ValueError):
                inverse(changed)
        tcp, reap = entry.tcp_fragment(), entry.reap_fragment()
        self.assertEqual(len(tcp.encode()), 407)  # Original raw block is 563 B.
        self.assertEqual(hashlib.sha256(tcp.encode()).hexdigest(), TCP_SHA)
        self.assertEqual(hashlib.sha256(reap.encode()).hexdigest(), REAP_SHA)
        for fragment in (tcp, reap):
            ast.parse(fragment)
        original = entry.SUPERVISOR.read_text()
        anchor = '            if secret_http_selected:\n                try:\n                    same ='
        for changed in (original.replace(anchor, 'changed', 1), original + '\n' + anchor):
            with patch.object(Path, 'read_text', return_value=changed):
                with self.assertRaises(ValueError):
                    entry.tcp_fragment()

    def test_actual_tcp_fragment_keeps_two_empty_and_failure(self):
        for nonempty in (False, True):
            ticks = iter(range(1000))
            log = io.StringIO()
            samples = []
            def tcp():
                samples.append(True)
                return {'controlled socket'} if nonempty else set()
            scope = dict(time=SimpleNamespace(monotonic=lambda: next(ticks), sleep=lambda _: None),
                         tcp=tcp, baseline=set(), log=log, code=0)
            exec(compile(entry.tcp_fragment(), str(entry.SUPERVISOR), 'exec'), scope)
            self.assertEqual(scope['code'], int(nonempty))
            if nonempty:
                self.assertIn('STOP host TCP delta tail not empty:', log.getvalue())
            else:
                self.assertEqual(len(samples), 2)
                self.assertEqual(log.getvalue().count('HOST_TCP delta_empty_observation='), 2)

    def test_actual_reap_fragment_never_upgrades_survivor_failure(self):
        for survived in (False, True):
            observations = iter(({77} if survived else set(), set(), set()))
            killed, log = [], io.StringIO()
            def waitpid(*_):
                raise ChildProcessError()
            scope = dict(
                descendants=lambda _: next(observations),
                os=SimpleNamespace(getpid=lambda: 42, kill=lambda pid, sig: killed.append((pid, sig)),
                                   waitpid=waitpid, WNOHANG=1),
                signal=signal, time=SimpleNamespace(monotonic=lambda: 0, sleep=lambda _: None),
                args=SimpleNamespace(root_chain=True, run='check-go.sh'), log=log, code=0,
                child=SimpleNamespace(pid=43, returncode=0), nonroot_reap_deadline=None, term_grace=60)
            exec(compile(entry.reap_fragment(), str(entry.SUPERVISOR), 'exec'), scope)
            self.assertEqual(scope['code'], int(survived))
            self.assertEqual(killed, [(77, signal.SIGKILL)] if survived else [])
            self.assertEqual(log.getvalue().count('OWNED runtime_observation='), 2)


if __name__ == '__main__':
    unittest.main()
