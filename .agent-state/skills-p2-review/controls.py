#!/usr/bin/env python3
"""Independent P2 prereap proof: real parser/method, synthetic proc and wait only."""
import ast
import io
import json
from pathlib import Path
import subprocess
from types import SimpleNamespace

ROOT = Path('/workspace/agenteam-skills-p2-independent')
REL = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SOURCE = ROOT / REL
BASE = '4530b375'
SELECTOR = '^TestSkillIndependentP2ConfirmationAndPackage$'
source = SOURCE.read_text()
parsed = ast.parse(source)
names = {'p2_process_identity', 'p2_executable_basename', 'reap_p2_exited'}
helpers = [node for node in parsed.body if isinstance(node, ast.FunctionDef) and node.name in names]
assert {node.name for node in helpers} == names
old = subprocess.check_output(['git', 'show', BASE + ':' + REL], cwd=ROOT, text=True)
first = source.index('def p2_process_identity(pid):\n')
last = source.index('def main():\n', first)
branch = (
    "            if args.root_chain and args.run == '" + SELECTOR + "':\n"
    '                if not reap_p2_exited(log, child.returncode is not None):\n'
    '                    code = 1\n'
    "                    log.write('STOP P2 owned descendant retirement unconfirmed\\n')\n"
)
assert source.count(branch) == 1
assert (source[:first] + source[last:]).replace(branch, '', 1) == old
checks = ['whole supervisor inverse exactly baseline, including old budgets and tail']
code = compile(ast.Module(body=helpers, type_ignores=[]), str(SOURCE), 'exec')
PARENT = 4100
CANARY = 'PRIVATE-COMM-DO-NOT-PUBLISH'


def stat(pid, *, parent=PARENT, start=555, state='Z'):
    # Linux fields 3..22, with a comm containing an embedded right parenthesis.
    fields = [state, str(parent)] + ['0'] * 17 + [str(start)]
    return f'{pid} ({CANARY}) embedded)) ' + ' '.join(fields) + '\n'


def run(label, data, expected, wait_results, *, links=None, waited=True, snapshot_error=None):
    events = []
    log = io.StringIO()
    queues = {pid: iter(values) for pid, values in data.items()}
    links = links or {}
    results = iter(wait_results)

    class ProcPath:
        def __init__(self, value):
            self.value = value

        @property
        def name(self):
            return Path(self.value).name

        def read_text(self):
            parts = self.value.split('/')
            assert parts[1] == 'proc' and parts[3] == 'stat'
            pid = int(parts[2])
            events.append(('stat', pid))
            value = next(queues[pid])
            if isinstance(value, Exception):
                raise value
            return value

    def descendants(parent):
        assert parent == PARENT
        events.append(('snapshot', parent))
        if snapshot_error is not None:
            raise snapshot_error
        return set(data)

    def readlink(path):
        parts = path.split('/')
        assert parts[1] == 'proc' and parts[3] == 'exe'
        pid = int(parts[2])
        events.append(('exe', pid))
        value = links.get(pid, FileNotFoundError('PRIVATE-ERROR'))
        if isinstance(value, Exception):
            raise value
        return value

    def waitpid(pid, flags):
        assert flags == 1 and pid > 0
        # This proves the actual helper logged and observed both identities
        # before the exact nonblocking wait, rather than inferring an exit.
        assert events[-3:] == [('stat', pid), ('exe', pid), ('stat', pid)]
        samples = [json.loads(line.split(' ', 2)[2]) for line in log.getvalue().splitlines()
                   if line.startswith('P2 descendant_before_reap ')]
        assert samples[-1]['pid'] == pid and samples[-1]['identity_stable']
        events.append(('wait', pid))
        result = next(results)
        if isinstance(result, Exception):
            raise result
        return result

    env = {'Path': ProcPath, 'json': json, 'descendants': descendants,
           'os': SimpleNamespace(getpid=lambda: PARENT, readlink=readlink,
                                 WNOHANG=1, waitpid=waitpid)}
    exec(code, env)
    assert env['reap_p2_exited'](log, waited) is expected, label
    assert list(results) == [], label + ': missing expected wait'
    assert all(list(values) == [] for values in queues.values()), label + ': unused stat'
    assert CANARY not in log.getvalue() and 'PRIVATE-ERROR' not in log.getvalue()
    samples = [json.loads(line.split(' ', 2)[2]) for line in log.getvalue().splitlines()
               if line.startswith('P2 descendant_before_reap ')]
    assert all(set(item) == {'pid', 'ppid', 'state', 'start_ticks', 'exe_basename',
                             'identity_stable'} for item in samples)
    checks.append(label)
    return events, samples


z = stat(4101)
_, samples = run('exe permission denied is diagnostic null, actual stat authority retained',
                 {4101: [z, z]}, True, [(4101, 0)],
                 links={4101: PermissionError('PRIVATE-ERROR')})
assert samples[0]['exe_basename'] is None
run('known exe does not rescue PID reuse', {4101: [z, stat(4101, start=556)]}, False, [],
    links={4101: '/private/fixture'})
run('actual parser rejects wrong PID without invoking wait', {4101: [stat(4102)]}, False, [])
run('actual parser rejects raw non-UTF8 safely',
    {4101: [UnicodeDecodeError('utf-8', b'\xff', 0, 1, 'PRIVATE-ERROR')]}, False, [])
run('actual parser truncation after first observation is not retirement',
    {4101: [z, '4101 (x) Z 4100\n']}, False, [])
run('stable indirect zombie cannot be reaped',
    {4101: [stat(4101, parent=4999)] * 2}, False, [])
run('changed zombie to live cannot be reaped', {4101: [z, stat(4101, state='S')]}, False, [])
run('exact Z0 then peer nonzero is sticky failure with both waits',
    {4102: [stat(4102)] * 2, 4101: [z, z]}, False, [(4101, 0), (4102, 256)])
run('unexpected wait PID is not owner completion', {4101: [z, z]}, False, [(4999, 0)])
run('owned snapshot error returns safe failure', {}, False, [],
    snapshot_error=PermissionError('PRIVATE-ERROR'))
run('no driver actual Wait means no proc scan', {}, False, [], waited=False)
print(json.dumps({'passed': len(checks), 'controls': checks,
                  'resources_started': False, 'proc_io': 'synthetic', 'waitpid': 'synthetic'}))
