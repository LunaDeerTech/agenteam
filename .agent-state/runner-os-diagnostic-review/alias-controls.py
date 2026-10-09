#!/usr/bin/env python3
"""Independent exact anon_pipe_read compatibility controls; no real proc/child."""
import argparse
import ast
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
path = args.repo / relative
text = path.read_text()
baseline = subprocess.check_output(['git', 'show', 'e6f541c8:' + relative], cwd=args.repo, text=True)
old = 'and wchan == "pipe_read"):'
new = 'and wchan in ("pipe_read", "anon_pipe_read")):'
assert text.count(new) == 1 and text.replace(new, old) == baseline
spec = importlib.util.spec_from_file_location('reviewed_os_alias', path)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)

# Reuse our stable independent in-memory fixture, not the author's controls.
tree = ast.parse(Path(__file__).with_name('controls.py').read_text())
names = {'check', 'FakePath', 'sample'}
fixture = ast.Module(body=[node for node in tree.body
                          if isinstance(node, (ast.FunctionDef, ast.ClassDef)) and node.name in names],
                     type_ignores=[])
checks = ['exact inverse source bytes; all non-alias gates and budgets unchanged']
raw = '0 0x0 0xPRIVATEBUF 0x80 0x0 0x0 0x0 0xPRIVATESTACK 0xPRIVATEPC'
exec(compile(fixture, str(__file__), 'exec'), globals())

for wchan in ('pipe_read', 'anon_pipe_read'):
    result, snapshot, reads = sample(wchan=wchan)
    check(result == {'tid': 400, 'syscall': 'read', 'fd': 0, 'wchan': wchan, 'blocking': True}
          and reads == 4 and snapshot['identity_checked'], 'exact alias under unchanged original witness')

for wchan in ('fifo_pipe_read', 'ep_poll', 'futex_wait_queue', 'anon_pipe_read_more',
              'xanon_pipe_read', 'Anon_pipe_read', 'anon_pipe_read\x00', 'PRIVATE'):
    result, _, _ = sample(wchan=wchan)
    check(result is None, 'other wait/name not accepted')

for label, kwargs in (
    ('changed raw buffer', {'after': raw.replace('PRIVATEBUF', 'DIFFERENTBUF')}),
    ('wrong fd', {'before': raw.replace('0x0 ', '0x1 ', 1), 'after': raw.replace('0x0 ', '0x1 ', 1)}),
    ('zero count', {'before': raw.replace('0x80', '0'), 'after': raw.replace('0x80', '0')}),
    ('wrong syscall', {'before': '19' + raw[1:], 'after': '19' + raw[1:]}),
    ('short sample', {'before': '0 0', 'after': '0 0'}),
):
    result, _, _ = sample(wchan='anon_pipe_read', **kwargs)
    check(result is None, label + ' rejects alias')

for kwargs, reason in (
    ({'identity_error_at': 1}, 'child_identity_changed'),
    ({'identity_error_at': 2}, 'child_identity_changed'),
    ({'links': ['pipe:[999]']}, 'stdin_pipe_changed'),
    ({'links': ['pipe:[6789]', 'pipe:[999]']}, 'stdin_pipe_changed_after_snapshot'),
    ({'flags': format(os.O_NONBLOCK, 'o')}, 'child_stdin_not_blocking'),
    ({'flags': format(os.O_WRONLY, 'o')}, 'child_stdin_not_blocking'),
):
    result, snapshot, _ = sample(wchan='anon_pipe_read', **kwargs)
    check(isinstance(result, probe.Failure) and str(result) == reason
          and snapshot['error'] == 'witness_condition', 'original identity/fd rejection under alias')

print(json.dumps({'passed': len(checks), 'real_proc_or_child': False, 'scope': 'exact alias only'}))
