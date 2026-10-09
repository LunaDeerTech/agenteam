#!/usr/bin/env python3
"""P2-only actual pre-reap code and original survivor gate, with no processes."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import textwrap
from types import SimpleNamespace
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('independent_p2_reaper', SOURCE)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
SELECTOR = '^TestSkillIndependentP2ConfirmationAndPackage$'
PARENT = 100
checks = []


def check(label, good):
    assert good, label
    checks.append(label)


def identity(pid=101, ppid=PARENT, state='Z', start=1001):
    return {'pid': pid, 'ppid': ppid, 'state': state, 'start_ticks': start}


def stat(pid=101, ppid=PARENT, state='Z', start=1001):
    fields = [state, str(ppid), '1', '1'] + ['0'] * 15 + [str(start)]
    return str(pid) + ' (PRIVATE-COMMAND-CANARY)) ' + ' '.join(fields) + '\n'


# Exercise the actual /proc parser. Neither comm nor an arbitrary exe filename
# is returned; malformed identity never becomes eligible.
class StatPath:
    value = ''

    def __init__(self, path):
        assert path == '/proc/101/stat'

    def read_text(self):
        return self.value


with patch.object(module, 'Path', StatPath):
    StatPath.value = stat()
    check('actual stat fields and comm excluded', module.p2_process_identity(101) == identity())
    for name, raw in (
        ('wrong PID', stat(pid=102)), ('no parent', stat(ppid=0)),
        ('no start', stat(start=0)), ('negative start', stat(start=-1)),
        ('multi-state', stat(state='RS')), ('invalid state', stat(state='Q')),
        ('truncated', '101 (canary) Z 100\n'), ('malformed', 'not-stat'),
    ):
        StatPath.value = raw
        try:
            module.p2_process_identity(101)
        except (ValueError, IndexError):
            check('stat rejects ' + name, True)
        else:
            raise AssertionError(name)

for name, value, expected in (
    ('known', '/private/path/fixture', 'fixture'),
    ('deleted known', '/private/path/fixture (deleted)', 'fixture'),
    ('candidate', '/private/skills-p2-independent-race-01.test', 'skills-p2-independent-race-01.test'),
    ('private name', '/private/SECRET-CANARY', None),
    ('private newline', '/private/fixture\nSECRET-CANARY', None),
    ('unavailable', FileNotFoundError(), None),
):
    options = {'side_effect': value} if isinstance(value, Exception) else {'return_value': value}
    with patch.object(module.os, 'readlink', **options):
        check('exe projection ' + name, module.p2_executable_basename(101) == expected)


def run(label, snapshots, identities, waits, expected, *, original_code=0,
        selector=SELECTOR, root_chain=True, driver_waited=True, expected_kills=()):
    # Execute the actual new branch AND unchanged original survivor/kill block.
    # The remaining original timeout/resource/TCP tail has a full inverse proof
    # plus actual controlled main coverage in harness-controls.py.
    source = SOURCE.read_text()
    start = source.index("            if args.root_chain and args.run == '" + SELECTOR + "':\n")
    end = source.index('            reap_deadline = ', start)
    block = compile(textwrap.dedent(source[start:end]), str(SOURCE), 'exec')
    log = io.StringIO()
    calls = {'desc': [], 'identity': [], 'wait': [], 'kill': [], 'exe': []}
    snapshot_iter, identity_iter, wait_iter = iter(snapshots), iter(identities), iter(waits)

    def descendants(parent):
        assert parent == PARENT
        calls['desc'].append(parent)
        return next(snapshot_iter)

    def read_identity(pid):
        calls['identity'].append(pid)
        value = next(identity_iter)
        if isinstance(value, Exception):
            raise value
        return value

    def exe(pid):
        calls['exe'].append(pid)
        return None  # An unavailable zombie executable does not invent a name.

    def waitpid(pid, flags):
        assert flags == module.os.WNOHANG and pid > 0
        calls['wait'].append(pid)
        value = next(wait_iter)
        if isinstance(value, Exception):
            raise value
        return value

    def kill(pid, sig):
        assert sig == module.signal.SIGKILL
        calls['kill'].append(pid)

    env = {'args': SimpleNamespace(root_chain=root_chain, run=selector),
           'child': SimpleNamespace(returncode=0 if driver_waited else None),
           'code': original_code, 'log': log, 'os': module.os, 'signal': module.signal,
           'descendants': descendants, 'reap_p2_exited': module.reap_p2_exited}
    with patch.object(module, 'descendants', descendants), \
         patch.object(module, 'p2_process_identity', read_identity), \
         patch.object(module, 'p2_executable_basename', exe), \
         patch.object(module.os, 'getpid', return_value=PARENT), \
         patch.object(module.os, 'waitpid', waitpid), patch.object(module.os, 'kill', kill), \
         patch.object(module.time, 'sleep', side_effect=AssertionError('no new sleep')), \
         patch.object(module.time, 'monotonic', side_effect=AssertionError('no new deadline')):
        exec(block, env)
    check(label, env['code'] == expected and calls['kill'] == list(expected_kills))
    check(label + ' bounded exact wait', len(calls['wait']) <= len(identities) // 2
          and all(pid in calls['identity'] for pid in calls['wait']))
    for remaining in (snapshot_iter, identity_iter, wait_iter):
        assert list(remaining) == [], label + ' unused stimulus'
    assert 'SECRET-CANARY' not in log.getvalue() and 'PRIVATE-COMMAND-CANARY' not in log.getvalue()
    return log.getvalue(), calls


z = identity()
raw, calls = run('stable owned Z actual0', [{101}, set()], [z, z], [(101, 0)], 0)
sample = json.loads(raw.split('P2 descendant_before_reap ', 1)[1].splitlines()[0])
check('pre-wait complete safe identity', sample == {**z, 'exe_basename': None, 'identity_stable': True}
      and raw.index('descendant_before_reap') < raw.index('p2_adopted_actual_wait')
      and calls['wait'] == [101])
run('empty owned set', [set(), set()], [], [], 0)
for name, result in (('nonzero exit', (101, 256)), ('signal exit', (101, 9)),
                     ('not waitable', (0, 0)), ('wrong returned PID', (102, 0)),
                     ('ECHILD', ChildProcessError()), ('other wait error', PermissionError())):
    run(name + ' sticky after disappearance', [{101}, set()], [z, z], [result], 1)
for state in ('S', 'R'):
    live = identity(state=state)
    run('live ' + state + ' never prereaped', [{101}, set()], [live, live], [], 1)
for name, observations in (
    ('first gone', [FileNotFoundError()]),
    ('second gone', [z, FileNotFoundError()]),
    ('first unknown', [PermissionError()]),
    ('PID reuse', [z, identity(start=2002)]),
    ('ppid changed', [z, identity(ppid=999)]),
    ('state changed', [z, identity(state='R')]),
    ('not direct child', [identity(ppid=999), identity(ppid=999)]),
):
    run(name + ' sticky after disappearance', [{101}, set()], observations, [], 1)
run('one Z0 cannot hide unknown peer', [{101, 102}, set()],
    [z, z, FileNotFoundError()], [(101, 0)], 1)
run('one Z0 cannot hide live peer', [{101, 102}, set()],
    [z, z, identity(pid=102, state='S'), identity(pid=102, state='S')], [(101, 0)], 1)
run('new PID remains original STOP and kill', [{101}, {102}], [z, z], [(101, 0)], 1, expected_kills=(102,))
run('Z0 cannot override original driver FAIL', [{101}, set()], [z, z], [(101, 0)], 1, original_code=1)
run('driver not actually waited no prereap', [set()], [], [], 1, driver_waited=False)
run('old root selector skips new method', [{101}], [], [], 1,
    selector='^TestSkillObjectInitializationPublication$', expected_kills=(101,))
run('nonroot selector skips new method', [set()], [], [], 0, root_chain=False)

print(json.dumps({'passed': len(checks), 'controls': checks}))
