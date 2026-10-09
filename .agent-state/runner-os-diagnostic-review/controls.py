#!/usr/bin/env python3
"""Independent pure controls for the OS diagnostic delta; never run the probe.

Only source retrieval uses a read-only git subprocess. All proc paths, clocks,
Child methods and native process entrypoints used below are explicit doubles.
"""
import argparse
import ast
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
from types import SimpleNamespace
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=Path('/workspace/agenteam-runner-control'))
args = parser.parse_args()
relative = '.agent-state/runner-control/runner-os-signals.py'
source = (args.repo / relative).read_text()
baseline = subprocess.check_output(['git', 'show', '1367780a:' + relative], cwd=args.repo, text=True)
spec = importlib.util.spec_from_file_location('reviewed_os_probe', args.repo / relative)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)
checks = []


def check(ok, label):
    assert ok, label
    checks.append(label)


class RemoveDiagnostics(ast.NodeTransformer):
    def visit_FunctionDef(self, node):
        if node.name in {'safe_number', 'safe_syscall', 'safe_thread_sample', 'safe_proc_error',
                         'emit_failed_read_snapshot', 'blocked_read'}:
            return None
        if node.name == '_blocked_read':
            node.name = 'blocked_read'
        return self.generic_visit(node)

    def visit_Assign(self, node):
        target = ast.unparse(node.targets[0])
        if target.startswith('self.read_snapshot') or target in {'failed', 'link'}:
            return None
        return self.generic_visit(node)

    def visit_Expr(self, node):
        if ast.unparse(node).startswith('self.read_snapshot['):
            return None
        return self.generic_visit(node)

    def visit_If(self, node):
        if ast.unparse(node.test).startswith('link.startswith(') or ast.unparse(node.test) == 'failed':
            return None
        return self.generic_visit(node)

    def visit_Try(self, node):
        if any(any(isinstance(item, ast.Assign) and ast.unparse(item.targets[0]) == 'failed'
                   for item in handler.body) for handler in node.handlers):
            node.handlers = []
        return self.generic_visit(node)

    def visit_Name(self, node):
        if node.id == 'link':
            return ast.parse("os.readlink(proc / 'fd/0')", mode='eval').body
        return node


recovered = RemoveDiagnostics().visit(copy.deepcopy(ast.parse(source)))
check(ast.dump(recovered, include_attributes=False) == ast.dump(ast.parse(baseline), include_attributes=False),
      'whole source AST inverse: original acceptance, budgets, Wait and cleanup unchanged')

raw = '0 0x0 0xPRIVATEBUF 0x80 0x0 0x0 0x0 0xPRIVATESTACK 0xPRIVATEPC'


class FakePath:
    values = {}
    reads = []
    tasks = []

    def __init__(self, value):
        self.value = str(value)

    def __truediv__(self, name):
        return FakePath(self.value + '/' + name)

    @property
    def name(self):
        return self.value.rsplit('/', 1)[-1]

    def read_text(self):
        self.reads.append(self.value)
        value = self.values[self.value]
        if isinstance(value, list):
            value = value.pop(0)
        if isinstance(value, Exception):
            raise value
        return value

    def iterdir(self):
        return iter(self.tasks)


def sample(before=raw, after=raw, wchan='pipe_read', flags='0', count=1, links=None, identity_error_at=None):
    child = probe.Child.__new__(probe.Child)
    child.proc = SimpleNamespace(pid=321)
    child.start_ticks, child.pipe_inode = 12345, 6789
    identities = []

    def identity():
        identities.append(True)
        if len(identities) == identity_error_at:
            raise probe.Failure('child_identity_changed')

    child.live_identity = identity
    FakePath.tasks = [FakePath('/proc/321/task/' + str(400 + n)) for n in range(count)]
    FakePath.values = {'/proc/321/fdinfo/0': 'flags:\t' + flags + '\n'}
    FakePath.reads = []
    for task in FakePath.tasks:
        FakePath.values[task.value + '/syscall'] = [before, after]
        FakePath.values[task.value + '/wchan'] = wchan
    with patch.object(probe, 'Path', FakePath), patch.object(probe.os, 'readlink', side_effect=links or ['pipe:[6789]'] * 2):
        try:
            result = child.blocked_read()
        except Exception as error:
            result = error
    check('PRIVATE' not in json.dumps(child.read_snapshot), 'private canaries excluded')
    return result, child.read_snapshot, len(FakePath.reads)


for label, kwargs in (
    ('changed raw buffer', {'after': raw.replace('PRIVATEBUF', 'OTHERBUF')}),
    ('wrong fd', {'before': raw.replace('0x0 ', '0x1 ', 1), 'after': raw.replace('0x0 ', '0x1 ', 1)}),
    ('zero length', {'before': raw.replace('0x80', '0'), 'after': raw.replace('0x80', '0')}),
    ('other syscall', {'before': '1' + raw[1:], 'after': '1' + raw[1:]}),
    ('short syscall', {'before': '0 0x0', 'after': '0 0x0'}),
    ('other kernel wchan', {'wchan': 'anon_pipe_read'}),
    ('private wchan', {'wchan': 'PRIVATEPATH'}),
):
    result, snap, reads = sample(**kwargs)
    check(result is None and reads == 4 and snap['scan_complete'], label + ' not accepted')
    if label == 'changed raw buffer':
        thread = snap['threads'][0]
        check(thread['before'] == thread['after'] and not thread['same_syscall_sample'],
              'equal safe projection cannot stand in for raw equality')

for label, kwargs, reason, identity in (
    ('identity first', {'identity_error_at': 1}, 'child_identity_changed', False),
    ('identity changed', {'identity_error_at': 2}, 'child_identity_changed', True),
    ('fd swapped first', {'links': ['pipe:[999]']}, 'stdin_pipe_changed', True),
    ('fd swapped last', {'links': ['pipe:[6789]', 'pipe:[999]']}, 'stdin_pipe_changed_after_snapshot', True),
    ('nonblocking', {'flags': format(os.O_NONBLOCK, 'o')}, 'child_stdin_not_blocking', True),
    ('write only', {'flags': format(os.O_WRONLY, 'o')}, 'child_stdin_not_blocking', True),
    ('129 tasks', {'count': 129}, 'task_scan_limit', True),
):
    result, snap, _ = sample(**kwargs)
    check(isinstance(result, probe.Failure) and str(result) == reason, label + ' preserves rejection')
    check(snap['identity_checked'] is identity and snap['error'] == 'witness_condition', label + ' safe failure')

result, snap, reads = sample(count=128, wchan='anon_pipe_read')
check(result is None and len(snap['threads']) == 128 and reads == 385, 'bounded 128 task snapshot with original reads')
for fields in (['65536'] + ['0'] * 8, ['PRIVATE'], ['-1'], ['running'], []):
    output = probe.safe_syscall(fields)
    check(output['number'] is None and 'PRIVATE' not in json.dumps(output), 'closed syscall number/state')

for cleanup_throws in (False, True):
    trace = []
    original, cleanup_error = probe.Failure('initial_read_not_observed'), probe.Failure('cleanup_failed')

    class FailureChild:
        def __init__(self, *unused):
            pass

        def start(self, *unused):
            raise original

        def cleanup(self):
            trace.append('cleanup')
            if cleanup_throws:
                raise cleanup_error

        def emit_failed_read_snapshot(self):
            trace.append('diagnostic')

    with patch.object(probe, 'Child', FailureChild):
        try:
            probe.run_case(None, SimpleNamespace(mkdir=lambda **unused: None), 'eof', None)
        except probe.Failure as error:
            check(error is (cleanup_error if cleanup_throws else original), 'original cleanup exception precedence')
        else:
            raise AssertionError('failure changed to success')
    check(trace == (['cleanup'] if cleanup_throws else ['cleanup', 'diagnostic']),
          'no snapshot before or after incomplete cleanup')

print(json.dumps({'passed': len(checks), 'real_proc_or_child': False, 'scope': 'diagnostic only'}))
