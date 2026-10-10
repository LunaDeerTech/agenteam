#!/usr/bin/env python3
"""Offline checks of the saved wrapper; no subprocesses, sockets or TCP reads."""
import importlib.util
import io
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch

path = Path(__file__).with_name('final_check.py')
spec = importlib.util.spec_from_file_location('final_check', path)
wrapper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wrapper)


class FinalCheckControls(unittest.TestCase):
    def test_audit_repair_keeps_original_header_and_both_whole_package_stages(self):
        source = (wrapper.ROOT / 'scripts/check-go.sh').read_text()
        actual = wrapper.audit_repair_script(source)
        header = source.split('"$AGENTEAM_GO" test ./...\n')[0]
        self.assertEqual(actual, header + '"$AGENTEAM_GO" test ./internal/central/audit/http\n'
                         '"$AGENTEAM_GO" test -race ./internal/central/audit/http\n')
        self.assertIn('set -eu\n', actual)

    def test_audit_repair_rejects_changed_original_stages(self):
        source = (wrapper.ROOT / 'scripts/check-go.sh').read_text()
        for changed in (source.replace('test ./...', 'test ./other'),
                        source + '"$AGENTEAM_GO" test -race ./...\n',
                        source.replace('vet ./...', 'vet ./other')):
            with self.assertRaises(ValueError):
                wrapper.audit_repair_script(changed)

    def test_fixed_modes_are_mutually_exclusive_before_resources(self):
        with patch.object(sys, 'argv', ['final_check', '--remaining', '--audit-repair',
                                        '--source', 'control', '--output', '/not-created']), \
                patch('sys.stderr', io.StringIO()):
            with self.assertRaises(SystemExit) as stopped:
                wrapper.main()
            self.assertEqual(stopped.exception.code, 2)

    def test_remaining_only_removes_original_ordinary_test(self):
        source = (wrapper.ROOT / 'scripts/check-go.sh').read_text()
        before, after = source.split('"$AGENTEAM_GO" test ./...\n')
        remaining = wrapper.remaining_script(source)
        self.assertEqual(remaining, before + after)
        self.assertIn('set -eu\n', remaining)
        self.assertTrue(remaining.endswith('"$AGENTEAM_GO" vet ./...\n'
            '# Analyze the explicit integration suite without starting Docker or a database.\n'
            '"$AGENTEAM_GO" vet -tags=integration ./...\n'
            '"$AGENTEAM_GO" test -race ./...\nsh scripts/build-go.sh\n'))

    def test_remaining_requires_unique_original_anchor(self):
        for source in ('', '"$AGENTEAM_GO" test ./...\n' * 2):
            with self.assertRaisesRegex(ValueError, 'anchor changed'):
                wrapper.remaining_script(source)

    def test_schema_inventory(self):
        env = wrapper.schema_environment()
        self.assertEqual(len(env), 12)
        self.assertEqual(env['AGENTEAM_USAGE_SCHEMA_NODE'], str(wrapper.NODE))
        self.assertTrue(all(env[name] == str(wrapper.PYTHON) for name in wrapper.SCHEMA_SOURCES))

    def test_unknown_schema_mapping_rejected(self):
        key, value = wrapper.SCHEMA_SOURCES.popitem()
        try:
            with self.assertRaisesRegex(ValueError, 'inventory changed'):
                wrapper.schema_environment()
        finally:
            wrapper.SCHEMA_SOURCES[key] = value

    def test_metadata_keeps_failure_and_ignores_loop_name(self):
        wrapper.round = 2
        try:
            result = wrapper.final_record({}, 1, wrapper.time.monotonic(), 1)
            self.assertEqual((result['terminal'], result['script_exit']), (1, 1))
            self.assertIsInstance(result['seconds'], float)
        finally:
            del wrapper.round

    def test_metadata_success_requires_passed_code(self):
        result = wrapper.final_record({}, 0, wrapper.time.monotonic(), 0)
        self.assertEqual((result['terminal'], result['script_exit']), (0, 0))

    def reap(self, survivor):
        killed, observations = [], []
        def descendants(pid):
            observations.append(pid)
            return {42} if survivor and len(observations) == 1 else set()
        def waitpid(*unused):
            raise ChildProcessError()
        scope = dict(code=0, os=SimpleNamespace(getpid=lambda: 10, WNOHANG=1,
                         kill=lambda pid, sig: killed.append(pid), waitpid=waitpid),
                     signal=SimpleNamespace(SIGKILL=9), descendants=descendants,
                     args=SimpleNamespace(root_chain=True, run='check-go.sh'),
                     time=wrapper.time, log=io.StringIO(), child=SimpleNamespace(pid=11, returncode=0))
        exec(compile(wrapper.reap_fragment(), str(wrapper.SUPERVISOR), 'exec'), scope)
        return scope, killed, observations

    def test_original_clean_reap_two_observations(self):
        scope, killed, observations = self.reap(False)
        self.assertEqual((scope['code'], killed, len(observations)), (0, [], 3))
        self.assertEqual(scope['log'].getvalue().count('OWNED runtime_observation='), 2)

    def test_original_survivor_rejection_not_repaired_by_later_empty(self):
        scope, killed, observations = self.reap(True)
        self.assertEqual((scope['code'], killed, len(observations)), (1, [42], 3))
        self.assertIn('STOP owned descendants survived driver:', scope['log'].getvalue())

    def tcp(self, rows, code=0):
        ticks, samples = [0], []
        def monotonic():
            ticks[0] += 1
            return ticks[0]
        def tcp():
            samples.append(True)
            return rows
        scope = dict(code=code, tcp=tcp, baseline=set(), log=io.StringIO(),
                     time=SimpleNamespace(monotonic=monotonic, sleep=lambda unused: None))
        fragment = wrapper.tcp_fragment()
        self.assertIn('time.monotonic() + 75', fragment)
        exec(compile(fragment, str(wrapper.SUPERVISOR), 'exec'), scope)
        return scope, samples

    def test_original_tcp_two_empty(self):
        scope, samples = self.tcp(set())
        self.assertEqual((scope['code'], len(samples), scope['empty']), (0, 2, 2))

    def test_original_tcp_retains_prior_failure(self):
        scope, samples = self.tcp(set(), code=1)
        self.assertEqual((scope['code'], len(samples)), (1, 2))

    def test_original_tcp_busy_fails_at_original_budget(self):
        scope, samples = self.tcp({'row'})
        self.assertEqual(scope['code'], 1)
        self.assertEqual(scope['tail_deadline'], 76)
        self.assertIn('STOP host TCP delta tail not empty: 1 rows', scope['log'].getvalue())


if __name__ == '__main__':
    unittest.main()
