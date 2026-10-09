#!/usr/bin/env python3
"""Offline controls for the supervisor's diagnostic-only TCP increment.

Imports the actual source but never invokes main, /proc, Docker, or a socket.
The pre-increment gate is read from a fixed local Git blob for differential
execution with the same synthetic TCP observations and simulated clock.
"""
import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
import subprocess
import tempfile
import textwrap
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BASELINE = '3e7fd3bd7aaa661c35ca96b68a103c789118548e'
spec = importlib.util.spec_from_file_location('tcp_supervisor_under_test', SOURCE)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
ROW = ('tcp', '0100007F:8000', '0100007F:9000', '01', '123')
ZERO = ('tcp6', '0:8000', '0:9000', '06', '0')
OLD = ('tcp', '0100007F:7000', '00000000:0000', '0A', '99')


class Clock:
    def __init__(self):
        self.now = 0.0
        self.sleeps = []

    def monotonic(self):
        return self.now

    def monotonic_ns(self):
        return int(self.now * 1_000_000_000)

    def sleep(self, duration):
        assert duration >= 0
        self.sleeps.append(duration)
        self.now += duration


class Entries:
    def __init__(self, entries):
        self.entries = entries

    def __enter__(self):
        return iter(self.entries)

    def __exit__(self, *args):
        pass


IDENTITY = dict(pid=321, state='S', ppid=1, pgid=321, sid=321, start_ticks=44)
EXECUTABLE = dict(path='/test/synthetic-binary', device=5, inode=6)


class OwnerControls(unittest.TestCase):
    def scan(self, *, rows=None, identities=None, links=None, executables=None,
             proc_error=None, fd_error=None, clock=None, deadline=1, processes=1, fds=1):
        clock = clock or Clock()

        def scandir(path):
            if str(path) == '/proc':
                if proc_error:
                    raise proc_error
                return Entries(SimpleNamespace(name=str(321 + n), path=f'/proc/{321+n}')
                               for n in range(processes))
            if fd_error:
                raise fd_error
            return Entries(SimpleNamespace(name=str(7 + n), path=f'{path}/{7+n}')
                           for n in range(fds))

        with patch.object(module.os, 'scandir', side_effect=scandir) as scanned, \
             patch.object(module.os, 'readlink', side_effect=links,
                          return_value='socket:[123]'), \
             patch.object(module, 'tcp_process_identity', side_effect=identities,
                          return_value=IDENTITY), \
             patch.object(module, 'tcp_executable_identity', side_effect=executables,
                          return_value=EXECUTABLE), \
             patch.object(module, 'time', clock):
            result = module.tcp_owner_evidence({ROW} if rows is None else rows, deadline)
            return result, scanned.call_count

    def test_stable_pid_fd_and_executable_are_observed(self):
        result, _ = self.scan()
        self.assertTrue(result['complete'])
        self.assertEqual(result['matches'][0], {**IDENTITY, 'fd': 7,
                         'socket_inode': '123', 'executable': EXECUTABLE, 'observed_ns': 0})
        self.assertNotIn('owner', result)

    def test_pid_reuse_fd_reuse_and_exec_change_cannot_claim_match(self):
        cases = [dict(identities=[IDENTITY, {**IDENTITY, 'start_ticks': 45}]),
                 dict(links=['socket:[123]', 'socket:[999]']),
                 dict(executables=[EXECUTABLE, {**EXECUTABLE, 'inode': 999}])]
        for case in cases:
            with self.subTest(case=case):
                result, _ = self.scan(**case)
                self.assertEqual(result['matches'], [])
                self.assertEqual(result['unstable'], 1)
                self.assertFalse(result['complete'])

    def test_unreadable_vanished_and_unavailable_are_explicit(self):
        for kwargs, field in [(dict(fd_error=PermissionError()), 'unreadable'),
                              (dict(identities=FileNotFoundError()), 'vanished')]:
            result, _ = self.scan(**kwargs)
            self.assertFalse(result['complete'])
            self.assertEqual(result[field], 1)
            self.assertEqual(result['matches'], [])
        result, _ = self.scan(proc_error=PermissionError())
        self.assertEqual(result['reason'], 'proc_unavailable')
        self.assertFalse(result['complete'])

    def test_executable_read_error_is_unknown(self):
        with patch.object(module, 'time', Clock()), \
             patch.object(module.os, 'readlink', side_effect=PermissionError('private-canary')), \
             patch.object(Path, 'stat') as stat:
            self.assertIsNone(module.tcp_executable_identity(Path('/synthetic/321'), 1))
            stat.assert_not_called()

    def test_directory_enumeration_deadline_cannot_report_complete(self):
        for stage in ('open', 'entry', 'eof'):
            clock = Clock()

            class TimedEntries:
                def __enter__(self):
                    if stage == 'open':
                        clock.now = 2
                    return self

                def __exit__(self, *args):
                    pass

                def __next__(self):
                    self.assert_before_deadline()
                    clock.now = 2
                    if stage == 'eof':
                        raise StopIteration
                    return SimpleNamespace(name='321', path='/synthetic/321')

                def assert_before_deadline(self):
                    assert clock.now < 1, 'enumeration started after deadline'

            with patch.object(module, 'time', clock), \
                 patch.object(module.os, 'scandir', return_value=TimedEntries()), \
                 patch.object(module, 'tcp_process_identity') as identity:
                result = module.tcp_owner_evidence({ROW}, 1)
                identity.assert_not_called()
                self.assertEqual(result['reason'], 'budget')
                self.assertFalse(result['complete'])

    def test_missing_first_or_second_executable_is_unknown(self):
        for values in ([None], [EXECUTABLE, None]):
            result, _ = self.scan(executables=values)
            self.assertEqual(result['unreadable'], 1)
            self.assertEqual(result['matches'], [])
            self.assertFalse(result['complete'])
            self.assertEqual(result['reason'], 'partial')

    def test_inode_zero_and_expired_budget_do_not_scan(self):
        result, calls = self.scan(rows={ZERO})
        self.assertEqual(calls, 0)
        self.assertEqual(result['inode_zero_considered_rows'], 1)
        self.assertEqual(result['matches'], [])
        result, calls = self.scan(deadline=0)
        self.assertEqual(calls, 0)
        self.assertEqual(result['reason'], 'budget')
        self.assertFalse(result['complete'])

    def test_deadline_crossed_during_identity_does_not_publish_match(self):
        clock = Clock()
        calls = 0

        def identity(_):
            nonlocal calls
            calls += 1
            if calls == 2:
                clock.now = 2
            return IDENTITY

        result, _ = self.scan(identities=identity, clock=clock)
        self.assertEqual(result['matches'], [])
        self.assertEqual(result['reason'], 'budget')

    def test_each_proc_operation_stops_before_the_next_after_deadline(self):
        order = ['identity1', 'link1', 'exe1', 'identity2', 'link2', 'exe2']
        for trigger in order:
            with self.subTest(trigger=trigger):
                clock, events, counts = Clock(), [], dict(identity=0, link=0, exe=0)

                def observe(kind, value):
                    def run(*args):
                        counts[kind] += 1
                        name = kind + str(counts[kind])
                        events.append(name)
                        if name == trigger:
                            clock.now = 2
                        return value
                    return run

                result, scans = self.scan(clock=clock,
                    identities=observe('identity', IDENTITY),
                    links=observe('link', 'socket:[123]'),
                    executables=observe('exe', EXECUTABLE))
                self.assertEqual(events, order[:order.index(trigger) + 1])
                self.assertEqual(scans, 1 if trigger == 'identity1' else 2)
                self.assertFalse(result['complete'])
                self.assertEqual(result['reason'], 'budget')
                self.assertEqual(result['matches'], [])

    def test_executable_readlink_deadline_prevents_stat(self):
        clock = Clock()

        def readlink(_):
            clock.now = 2
            return '/test/synthetic-binary'

        with patch.object(module, 'time', clock), \
             patch.object(module.os, 'readlink', side_effect=readlink), \
             patch.object(Path, 'stat') as stat:
            self.assertIsNone(module.tcp_executable_identity(Path('/proc/321'), 1))
            stat.assert_not_called()

    def test_directory_iteration_checks_budget_before_and_after_next(self):
        clock, calls = Clock(), []

        def entries():
            calls.append('first')
            clock.now = 2
            yield 'unpublishable'
            calls.append('second')
            yield 'forbidden'

        with patch.object(module, 'time', clock):
            self.assertEqual(list(module.tcp_entries_before_deadline(entries(), 1)), [])
            self.assertEqual(calls, ['first'])
            self.assertEqual(list(module.tcp_entries_before_deadline(entries(), 1)), [])
            self.assertEqual(calls, ['first'])

    def test_scan_caps_do_not_claim_complete(self):
        for kwargs, field, limit in [(dict(processes=2049, fds=0), 'processes', 2048),
                                     (dict(fds=8193, links=lambda _: 'pipe:[123]'), 'fds', 8192),
                                     (dict(fds=257), 'fds', 256)]:
            result, _ = self.scan(**kwargs)
            self.assertFalse(result['complete'])
            self.assertEqual(result['reason'], 'limit')
            self.assertEqual(result[field], limit)
        many = {('tcp', str(n), 'remote', '06', '0') for n in range(4100)}
        result, calls = self.scan(rows=many)
        self.assertTrue(result['rows_truncated'])
        self.assertEqual(result['input_rows'], 4100)
        self.assertEqual(result['inode_zero_considered_rows'], 4096)
        self.assertFalse(result['complete'])
        self.assertEqual(calls, 0)

    def test_pause_pays_scan_from_existing_delay(self):
        for deadline in (1, 0):
            clock = Clock()
            deadlines = []

            def owner(rows, end):
                deadlines.append(end)
                if clock.now < end:
                    clock.now += .005
                return {'complete': False, 'reason': 'budget'}

            with patch.object(module, 'time', clock), \
                 patch.object(module, 'tcp_owner_evidence', side_effect=owner):
                module.tcp_evidence_pause({'delta': {ROW}}, deadline)
            self.assertAlmostEqual(clock.now, .1)
            self.assertEqual(deadlines, [min(deadline, .02)])


class EvidenceControls(unittest.TestCase):
    def test_exact_samples_private_exclusive_and_no_overwrite(self):
        parent = ROOT / 'output/ai/runner-control'
        with tempfile.TemporaryDirectory(prefix='tcp-evidence-control-', dir=parent) as temp:
            log_path = Path(temp) / 'synthetic.log'
            sample = lambda n, rows: dict(started_ns=n, ended_ns=n + 1,
                                         rows=rows, delta=rows - {OLD})
            log = io.StringIO()
            args = (log_path, '^Synthetic$', {OLD}, dict(started_ns=1, ended_ns=2),
                    [sample(3, {ROW, OLD}), sample(5, {ZERO})], sample(7, set()), log)
            module.save_tcp_failure(*args)
            path = log_path.with_suffix('.tcp-tail-failure.json')
            original = path.read_bytes()
            value = json.loads(original)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(value['baseline']['rows'][0]['inode'], '99')
            self.assertEqual(value['last_two_loop_samples'][0]['rows']['count'], 2)
            self.assertEqual(value['last_two_loop_samples'][0]['delta']['count'], 1)
            self.assertEqual(value['last_two_loop_samples'][1]['delta']['rows'][0]['state'], '06')
            self.assertEqual(value['failure_reread']['delta']['count'], 0)
            self.assertEqual(value['failure_reread']['owners']['reason'], 'not_scanned')
            module.save_tcp_failure(*args)
            self.assertEqual(path.read_bytes(), original)
            self.assertTrue(log.getvalue().endswith('failure_evidence_saved=False\n'))
            path.unlink()
            sentinel = Path(temp) / 'sentinel'
            sentinel.write_text('unchanged')
            path.symlink_to(sentinel)
            module.save_tcp_failure(*args)
            self.assertEqual(sentinel.read_text(), 'unchanged')

    def test_evidence_truncation_keeps_original_count_and_set(self):
        rows = {('tcp', str(n), 'remote', '01', str(n)) for n in range(4100)}
        copy = rows.copy()
        result = module.tcp_rows_evidence(rows)
        self.assertEqual(result['count'], 4100)
        self.assertEqual(len(result['rows']), 4096)
        self.assertTrue(result['truncated'])
        self.assertEqual(rows, copy)


def tail(source):
    start = source.index('            tail_deadline = ')
    end = source.index('            same = ', start)
    return compile(textwrap.dedent(source[start:end]), '<actual-tail>', 'exec')


class GateControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.old = subprocess.run(['git', 'show', BASELINE + ':' + str(SOURCE.relative_to(ROOT))],
                                 cwd=ROOT, check=True, capture_output=True, text=True).stdout
        cls.new = SOURCE.read_text()

    def execute(self, source, observations, initial_code=0, owner_match=False):
        clock, log, saved, calls = Clock(), io.StringIO(), [], []

        def tcp():
            index = len(calls)
            calls.append(clock.now)
            return observations(index, clock.now)

        def owner(rows, deadline):
            if clock.now < deadline:
                clock.now += min(.005, deadline - clock.now)
            return {'complete': owner_match, 'reason': 'scanned' if owner_match else 'budget',
                    'matches': [{**IDENTITY, 'socket_inode': '123',
                                 'executable': EXECUTABLE}] if owner_match else []}

        env = {**module.__dict__, 'time': clock, 'tcp': tcp, 'baseline': {OLD},
               'baseline_times': {'started_ns': 0, 'ended_ns': 0}, 'code': initial_code,
               'log': log, 'args': SimpleNamespace(run='^Synthetic$'), 'log_path': Path('unused'),
               'save_tcp_failure': lambda *args: saved.append(args)}
        with patch.object(module, 'time', clock), \
             patch.object(module, 'tcp_owner_evidence', side_effect=owner):
            exec(tail(source), env)
        return env['code'], env['empty'], log.getvalue(), calls, clock.now, saved

    def test_all_other_supervisor_bytes_are_unchanged(self):
        trimmed = self.new.replace('from collections import deque\n', '')
        trimmed = trimmed.replace('from itertools import islice\n', '')
        start, end = trimmed.index('def tcp_rows_evidence('), trimmed.index('def descendants(')
        trimmed = trimmed[:start] + trimmed[end:]
        trimmed = trimmed.replace("    baseline_times = {'started_ns': time.monotonic_ns()}\n", '')
        trimmed = trimmed.replace("    baseline_times['ended_ns'] = time.monotonic_ns()\n", '')
        start, end = trimmed.index('            tail_deadline = '), trimmed.index('            same = ')
        old_start, old_end = self.old.index('            tail_deadline = '), self.old.index('            same = ')
        trimmed = trimmed[:start] + self.old[old_start:old_end] + trimmed[end:]
        self.assertEqual(trimmed, self.old)

    def test_proven_owner_never_exempts_a_tuple(self):
        observations = lambda i, t: {OLD, ROW}
        unknown = self.execute(self.new, observations)
        known = self.execute(self.new, observations, owner_match=True)
        self.assertEqual(unknown[:5], known[:5])
        self.assertEqual(known[0], 1)
        self.assertTrue(known[5][0][4][-1]['owners']['matches'])

    def test_differential_original_gate_and_failure_reread(self):
        cases = [lambda i, t: {OLD},
                 lambda i, t: {OLD, ROW} if i == 0 else {OLD},
                 lambda i, t: {OLD} if i % 2 else {ROW},
                 lambda i, t: {OLD, ROW, ZERO} if t < 75 else {ZERO},
                 lambda i, t: {ROW} if t < 75 else set()]
        for index, observations in enumerate(cases):
            for initial in (0, 1):
                with self.subTest(case=index, initial=initial):
                    old = self.execute(self.old, observations, initial)
                    new = self.execute(self.new, observations, initial)
                    self.assertEqual(old[:3], new[:3])
                    self.assertEqual(len(old[3]), len(new[3]))
                    self.assertAlmostEqual(old[4], new[4], places=9)
                    for before, after in zip(old[3], new[3]):
                        self.assertAlmostEqual(before, after, places=9)
                    if new[1] != 2:
                        self.assertEqual(new[0], 1)
                        self.assertEqual(len(new[5]), 1)
                        saved = new[5][0]
                        self.assertEqual(len(saved[4]), 2)
                        self.assertEqual(saved[5]['rows'], observations(len(new[3]) - 1, new[3][-1]))
                        self.assertNotIn('owners', saved[5])
                    else:
                        self.assertEqual(new[5], [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
