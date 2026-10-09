#!/usr/bin/env python3
"""Pure parser and actual witness wiring controls; no child, proc, or socket."""
import ast
import importlib.util
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

SOURCE = Path(__file__).with_name('runner-os-signals.py')
spec = importlib.util.spec_from_file_location('os_probe_diagnostic_controls', SOURCE)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)
checks = 0


def check(value):
    global checks
    assert value
    checks += 1


raw = '0 0x0 0xSECRETADDRESS 0x81 0x0 0x0 0x0 0xPRIVATESTACK 0xPRIVATEPC'.split()
for wchan in ('pipe_read', 'anon_pipe_read', 'fifo_pipe_read'):
    value = probe.safe_thread_sample('123', raw, wchan, raw)
    check(value['before'] == {'state': 'sampled', 'number': 0, 'fd0': True})
    check(value['wchan'] == wchan and value['same_syscall_sample'])
    check('SECRET' not in json.dumps(value) and 'PRIVATE' not in json.dumps(value))
for bad in ('private-material', '9' * 1000, '-1', ''):
    check(probe.safe_number(bad, 65535) is None)
for fields, state in (([], 'unavailable'), (['running'], 'running'), (['-1'], 'not_in_syscall'),
                      (['PRIVATE'], 'unavailable')):
    check(probe.safe_syscall(fields)['state'] == state)
check(probe.safe_thread_sample('private-tid', raw, 'PRIVATE-WCHAN', ['running']) == {
    'tid': None, 'before': {'state': 'sampled', 'number': 0, 'fd0': True},
    'after': {'state': 'running', 'number': None, 'fd0': None},
    'same_syscall_sample': False, 'wchan': 'other'})


class FakePath:
    values = {}
    tasks = []
    reads = []

    def __init__(self, value):
        self.value = str(value)

    @property
    def name(self):
        return self.value.rsplit('/', 1)[-1]

    def __truediv__(self, name):
        return FakePath(self.value + '/' + name)

    def read_text(self):
        self.reads.append(self.value)
        value = self.values[self.value]
        if isinstance(value, Exception):
            raise value
        if isinstance(value, list):
            return value.pop(0)
        return value

    def iterdir(self):
        return iter(self.tasks)


def witness(wchan='pipe_read', error=None, task_count=1, syscalls=None, identity_error_at=0):
    child = probe.Child.__new__(probe.Child)
    child.proc = SimpleNamespace(pid=100)
    child.start_ticks = 200
    child.pipe_inode = 300
    identity_calls = 0

    def identity():
        nonlocal identity_calls
        identity_calls += 1
        if identity_calls == identity_error_at:
            raise probe.Failure('child_identity_changed')

    child.live_identity = identity  # No proc access; both original checks remain.
    FakePath.tasks = [FakePath('/proc/100/task/' + str(400 + n)) for n in range(task_count)]
    FakePath.values = {'/proc/100/fdinfo/0': 'flags:\t00\n'}
    for task in FakePath.tasks:
        FakePath.values[task.value + '/syscall'] = error or syscalls or ' '.join(raw)
        FakePath.values[task.value + '/wchan'] = wchan
    FakePath.reads = []
    with patch.object(probe, 'Path', FakePath), patch.object(probe.os, 'readlink', return_value='pipe:[300]'):
        try:
            result = child.blocked_read()
        except Exception as caught:
            result = caught
    return child, result, tuple(FakePath.reads)


# Only the old name and diagnostic02's exact anonymous-pipe name are accepted.
for wchan in ('pipe_read', 'anon_pipe_read', 'fifo_pipe_read', 'PRIVATE-WCHAN'):
    child, result, reads = witness(wchan)
    check(bool(result) == (wchan in ('pipe_read', 'anon_pipe_read')))
    check(len(reads) == 4)  # fdinfo plus syscall/wchan/syscall, no diagnostic re-read.
    check(child.read_snapshot['pid'] == 100 and child.read_snapshot['start_ticks'] == 200)
    check(child.read_snapshot['fd0_inode'] == 300 and child.read_snapshot['fd0_flags'] == 0)
    check('PRIVATE' not in json.dumps(child.read_snapshot))

# Alias compatibility cannot weaken any other part of the original witness.
for wchan in ('anon_pipe_read_more', 'xanon_pipe_read', 'Anon_pipe_read', 'ep_poll'):
    _, result, _ = witness(wchan)
    check(result is None)
for field, value in ((0, '19'), (1, '0x1'), (3, '0x0')):
    changed = raw.copy()
    changed[field] = value
    _, result, _ = witness('anon_pipe_read', syscalls=' '.join(changed))
    check(result is None)
changed = raw.copy()
changed[2] = '0xDIFFERENT_PRIVATE_ADDRESS'
_, result, _ = witness('anon_pipe_read', syscalls=[' '.join(raw), ' '.join(changed)])
check(result is None)  # Equal safe projections cannot replace raw equality.
for at in (1, 2):
    child, result, _ = witness('anon_pipe_read', identity_error_at=at)
    check(isinstance(result, probe.Failure) and str(result) == 'child_identity_changed')
    check(child.read_snapshot['error'] == 'witness_condition')
for error, category in ((PermissionError('PRIVATE'), 'permission'),
                        (OSError('PRIVATE'), 'io'), (ValueError('PRIVATE'), 'format')):
    child, result, reads = witness(error=error)
    check(result is error and child.read_snapshot['error'] == category)
    check(child.read_snapshot['current_tid'] == 400 and len(reads) == 2)
    check(not child.read_snapshot['scan_complete'] and 'PRIVATE' not in json.dumps(child.read_snapshot))
child, result, reads = witness(error=FileNotFoundError('PRIVATE'))
check(result is None and child.read_snapshot['threads'] == [{'tid': 400, 'error': 'missing'}])
child, result, reads = witness(task_count=129)
check(isinstance(result, probe.Failure) and str(result) == 'task_scan_limit')
check(child.read_snapshot['threads'] == [] and not child.read_snapshot['scan_complete'])
check(len(reads) == 1)

# A diagnostic sample is never a late witness or an extension of original time.
child, _, _ = witness('anon_pipe_read')
child.pump = lambda timeout: None
with patch.object(probe.time, 'monotonic', side_effect=[0, 0, 2]):
    try:
        child.until(lambda: {'not_authority': True}, 1, 'original_deadline')
    except probe.Failure as error:
        check(str(error) == 'original_deadline')
    else:
        raise AssertionError('late result accepted')

# Actual run_case exception path: cleanup first; evidence errors do not replace
# the original failure. No original Child.start or native process is invoked.
trace = []
original = probe.Failure('initial_read_not_observed')


class FailingChild:
    read_snapshot = {'error': 'permission'}
    emit_failed_read_snapshot = probe.Child.emit_failed_read_snapshot

    def __init__(self, *args):
        pass

    def start(self, *args):
        raise original

    def cleanup(self):
        trace.append('cleanup')

    def emit(self, *args, **kwargs):
        trace.append('diagnostic')
        raise PermissionError('PRIVATE')


with patch.object(probe, 'Child', FailingChild), patch('builtins.print') as printed:
    try:
        probe.run_case(None, SimpleNamespace(mkdir=lambda **kwargs: None), 'eof', None)
    except probe.Failure as error:
        check(error is original)
    else:
        raise AssertionError('original failure lost')
    check(trace == ['cleanup', 'diagnostic'])
    check(printed.call_args.args == ('runner_os_read_snapshot_unavailable',))

# The original phase, signal, Wait, cleanup, and strict read expressions remain.
tree = ast.parse(SOURCE.read_text())
functions = {node.name: node for node in ast.walk(tree) if isinstance(node, ast.FunctionDef)}
strict = ast.unparse(functions['_blocked_read'])
check("wchan in ('pipe_read', 'anon_pipe_read')" in strict and "before == after" in strict)
check('len(tasks) <= 128' in strict and 'self.live_identity()' in strict)
check('time.monotonic() < deadline' in ast.unparse(functions['until']))
check('deadline = time.monotonic() + 3' in ast.unparse(functions['cleanup']))
check('stopped_at + 5' in ast.unparse(functions['run_case']))
print(f'PASS {checks} parser/privacy/original-witness/deadline/cleanup controls; no child/proc/socket')
