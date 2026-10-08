#!/usr/bin/env python3
"""Check extracted gate functions against an in-memory filesystem; never import a driver."""
import ast
import copy
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parent
BASE = ROOT.parent / 'pg-driver-v01'


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def functions(text):
    return {node.name: ast.get_source_segment(text, node)
            for node in ast.parse(text).body if isinstance(node, (ast.FunctionDef, ast.ClassDef))}


driver_text = (ROOT / 'driver.py').read_text()
launcher_text = (ROOT / 'launch.py').read_text()
driver, launcher = functions(driver_text), functions(launcher_text)
old_driver = functions((BASE / 'driver.py').read_text())
accepted = functions(Path('/workspace/scratch/owner-ui-backend/resource-driver-v05/driver.py').read_text())
reused = ['sha', 'load_bound', 'digest', 'processes', 'is_main', 'observe_processes',
          'reap_adopted', 'write_retirement', 'retire_owned', 'watch_output', 'utc',
          'write', 'command', 'resources', 'resource_topology', 'tcp_snapshot',
          'tcp_identity', 'tcp_delta', 'retire_tcp_observation']
assert all(driver[name] == old_driver[name] == accepted[name] for name in reused)
assert driver['TopWatchdog'] == old_driver['TopWatchdog']
assert driver['runtime_environment'] == old_driver['runtime_environment']
assert driver['actual_graph_input'] == old_driver['actual_graph_input']
gate_names = list(functions((ROOT / 'gate-block.py.txt').read_text()))
assert all(driver[name] == launcher[name] for name in gate_names)
syntax_files = ['driver.py', 'launch.py', 'prepare.py', 'check_gates.py', 'check_filegate.py']
for name in syntax_files:
    compile((ROOT / name).read_text(), str(ROOT / name), 'exec')
freeze = json.loads((ROOT / 'freeze.prepared.json').read_bytes())
assert freeze['root_authorized_resources'] is False and freeze['pending_inputs']
assert freeze['offline_readiness'] is None and freeze['driver_runtime_files'] == {}
assert digest(driver_text.encode()) == freeze['driver_sha256']
assert digest(launcher_text.encode()) == freeze['launcher_sha256']
assert freeze['permitted_run_name'] is None and freeze['permitted_groups'] == []
groups_row = freeze['groups']
groups_bytes = Path(groups_row['path']).read_bytes()
assert digest(groups_bytes) == groups_row['sha256']
groups = json.loads(groups_bytes)['groups']
assert len(groups) == 7 and len(set(sum(groups.values(), []))) == 14
assert ast.literal_eval(next(n.value for n in ast.parse(driver_text).body
                             if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'GROUPS' for t in n.targets))) == groups


class MemoryPath:
    files = {}
    modes = {}
    dirs = set()

    def __init__(self, value):
        self.value = str(value)

    def __truediv__(self, value):
        return MemoryPath(str(PurePosixPath(self.value) / value))

    def __str__(self):
        return self.value

    @property
    def name(self):
        return PurePosixPath(self.value).name

    def exists(self):
        return self.value in self.files or self.value in self.dirs

    def is_dir(self):
        return self.value in self.dirs

    def is_file(self):
        return self.value in self.files

    def iterdir(self):
        values = sorted(set(self.files) | self.dirs)
        return [MemoryPath(value) for value in values if str(PurePosixPath(value).parent) == self.value and value != self.value]

    def read_bytes(self):
        return self.files[self.value]

    def stat(self):
        return SimpleNamespace(st_mode=self.modes[self.value])

    @classmethod
    def put(cls, value, raw, mode=0o400):
        path = PurePosixPath(value)
        cls.files[str(path)] = raw
        cls.modes[str(path)] = mode
        cls.dirs.update(str(parent) for parent in path.parents)


scope = {'re': re, 'json': json, 'hashlib': hashlib, 'Path': MemoryPath,
         'ROOT': MemoryPath('/memory'), 'os': SimpleNamespace(getppid=lambda: 4100)}
tree = ast.parse(driver_text)
nodes = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in gate_names]
exec(compile(ast.Module(body=nodes, type_ignores=[]), '<isolated immutable/history gates>', 'exec'), scope)
scope['process_starttime'] = lambda pid: 701 if pid == 4100 else None
cases = []


def expect(name, call, allowed):
    try:
        value = call()
    except (RuntimeError, KeyError, TypeError, ValueError) as error:
        assert not allowed, (name, str(error))
        cases.append({'case': name, 'expected': 'BLOCK', 'actual': 'BLOCK', 'reason': str(error)})
    else:
        assert allowed, (name, value)
        cases.append({'case': name, 'expected': 'ALLOW', 'actual': 'ALLOW'})


def new_grant(name='next01'):
    return {'root_authorized_resources': True, 'permitted_run_name': name,
            'permitted_groups': ['new-selection'], 'pending_inputs': [],
            'driver_sha256': digest(b'driver source'), 'launcher_sha256': digest(b'launcher source')}


def reset():
    MemoryPath.files, MemoryPath.modes, MemoryPath.dirs = {}, {}, set()
    MemoryPath.put('/memory/driver.py', b'driver source')
    MemoryPath.put('/memory/launch.py', b'launcher source')


def validate(value=None, mode=0o400, override_raw=None, group='new-selection', name='next01'):
    value = new_grant() if value is None else value
    raw = encoded(value)
    MemoryPath.put('/grant', raw if override_raw is None else override_raw, mode)
    return scope['validate_execution_grant'](MemoryPath('/grant'), raw, name, group)


reset()
expect('immutable exact boolean grant', lambda: validate(), True)
for value in [False, 1, 'true', None]:
    grant = new_grant()
    grant['root_authorized_resources'] = value
    expect('reject authorization ' + repr(value), lambda grant=grant: validate(grant), False)
for mode in [0o600, 0o420, 0o402]:
    expect('reject writable grant ' + oct(mode), lambda mode=mode: validate(mode=mode), False)
expect('reject changed grant bytes', lambda: validate(override_raw=b'changed'), False)
expect('reject wrong exact name', lambda: validate(name='next02'), False)
expect('reject invalid invocation name', lambda: validate(name='../next01'), False)
expect('reject wrong group', lambda: validate(group='old-resolver'), False)
for key, value in [('pending_inputs', ['incomplete']), ('permitted_groups', ['new-selection', 'new-replay']),
                   ('driver_sha256', 'different'), ('launcher_sha256', 'different')]:
    grant = new_grant()
    grant[key] = value
    expect('reject changed ' + key, lambda grant=grant: validate(grant), False)


def add_current(grant=None, changes=None):
    grant = new_grant() if grant is None else grant
    raw = encoded(grant)
    intent = {'state': 'PREPARED_TO_SPAWN', 'run': 'next01', 'group': 'new-selection',
              'root_grant_sha256': digest(raw), 'driver_sha256': grant['driver_sha256'],
              'launcher_sha256': grant['launcher_sha256'], 'launcher_pid': 4100,
              'launcher_starttime_ticks': 701}
    intent.update(changes or {})
    MemoryPath.put('/memory/launches/next01/root-grant.json', raw)
    MemoryPath.put('/memory/launches/next01/intent.json', encoded(intent))


def history(inside=False, grant=None):
    grant = new_grant() if grant is None else grant
    return scope['check_invocation_history']('next01', 'new-selection', grant, encoded(grant), inside)


reset()
expect('first launcher before own directory exists', lambda: history(), True)
expect('direct driver with no outer launcher is blocked', lambda: history(True), False)
add_current()
expect('only correctly bound current launch is exempt', lambda: history(True), True)
expect('launcher cannot reuse an existing name', lambda: history(), False)
for key, value in [('launcher_pid', 99), ('launcher_starttime_ticks', 99), ('root_grant_sha256', 'different'),
                   ('driver_sha256', 'different'), ('launcher_sha256', 'different'),
                   ('run', 'wrong01'), ('group', 'old-summary'), ('state', 'ENDED')]:
    reset()
    add_current(changes={key: value})
    expect('current intent rejects ' + key, lambda: history(True), False)
for filename in ['root-grant.json', 'intent.json']:
    reset()
    add_current()
    MemoryPath.modes['/memory/launches/next01/' + filename] = 0o600
    expect('current copy rejects writable ' + filename, lambda: history(True), False)
reset()
add_current()
MemoryPath.dirs.add('/memory/runs/next01')
expect('current launch cannot exempt a started run', lambda: history(True), False)
reset()
add_current()
MemoryPath.put('/memory/launches/next01/result.json', b'{}')
expect('current launch cannot exempt an outer terminal', lambda: history(True), False)


def add_prior(failed=False, inner_changes=None, launch_changes=None, outer_changes=None, cleanup_changes=None):
    grant = new_grant('prior01')
    raw = encoded(grant)
    intent = {'run': 'prior01', 'group': 'new-selection', 'state': 'PREPARED_TO_SPAWN',
              'root_grant_sha256': digest(raw), 'driver_sha256': grant['driver_sha256'],
              'launcher_sha256': grant['launcher_sha256'], 'launcher_pid': 3000, 'launcher_starttime_ticks': 602}
    intent_raw = encoded(intent)
    inner = {'run': 'prior01', 'actual_wait_completed': True, 'watchdog_thread_joined': True,
             'double_cleanup': True, 'driver_exit': 1 if failed else 0, 'accepted': not failed}
    inner.update(inner_changes or {})
    inner_raw = encoded(inner)
    launch = {'run': 'prior01', 'group': 'new-selection', 'spawned': True,
              'root_grant_sha256': digest(raw), 'intent_sha256': digest(intent_raw),
              'driver_sha256': grant['driver_sha256'], 'launcher_sha256': grant['launcher_sha256'],
              'actual_direct_wait_completed': True, 'driver_pid': 3001, 'driver_starttime_ticks': 603,
              'exit_code': 1 if failed else 0}
    launch.update(launch_changes or {})
    outer = {**launch, 'root_grant_bytes_unchanged': True, 'driver_result_sha256': digest(inner_raw),
             'driver_accepted': not failed, 'accepted': not failed}
    outer.update(outer_changes or {})
    cleanup = [{'baseline_unchanged': True, 'remaining_new': [], 'owned_processes': [], 'runtime_entries': [],
                'exact_absent': {'fixed-resource-id': {'absent': True}}} for _ in range(2)]
    cleanup[1].update(cleanup_changes or {})
    originals = {'root-grant.json': raw, 'intent.json': intent_raw, 'launch.json': encoded(launch),
                 'outer-result.json': encoded(outer), 'inner-result.json': inner_raw, 'cleanup.json': encoded(cleanup)}
    paths = {'root-grant.json': '/memory/launches/prior01/root-grant.json',
             'intent.json': '/memory/launches/prior01/intent.json', 'launch.json': '/memory/launches/prior01/launch.json',
             'outer-result.json': '/memory/launches/prior01/result.json',
             'inner-result.json': '/memory/runs/prior01/result.json', 'cleanup.json': '/memory/runs/prior01/cleanup.json'}
    for key, original in originals.items():
        MemoryPath.put(paths[key], original)
    return paths, {key: digest(raw) for key, raw in originals.items()}


reset()
add_prior()
expect('complete prior PASS permits next launcher', lambda: history(), True)
add_current()
expect('complete prior PASS plus current bound launch permits driver', lambda: history(True), True)
for missing in ['root-grant.json', 'intent.json', 'launch.json', 'outer-result.json', 'inner-result.json', 'cleanup.json']:
    reset()
    paths, _ = add_prior()
    del MemoryPath.files[paths[missing]]
    expect('missing prior original blocks: ' + missing, lambda: history(), False)
reset()
MemoryPath.put('/memory/launches/prior01/root-grant.json', encoded(new_grant('prior01')))
expect('pre-Popen launch without run blocks next', lambda: history(), False)
reset()
MemoryPath.put('/memory/runs/prior01/result.json', encoded({'accepted': True}))
expect('inner PASS without outer launch blocks next', lambda: history(), False)
for changes in [{'accepted': False}, {'exit_code': 1}, {'actual_direct_wait_completed': False},
                {'driver_result_sha256': 'stale'}, {'root_grant_bytes_unchanged': False}, {'driver_pid': 999}]:
    reset()
    add_prior(outer_changes=changes)
    expect('outer invalid ' + next(iter(changes)), lambda: history(), False)
for changes in [{'actual_direct_wait_completed': False}, {'spawned': False}, {'exit_code': 1}, {'intent_sha256': 'stale'}]:
    reset()
    add_prior(launch_changes=changes)
    expect('launch invalid ' + next(iter(changes)), lambda: history(), False)
for changes in [{'actual_wait_completed': False}, {'watchdog_thread_joined': False}, {'double_cleanup': False}, {'accepted': False}]:
    reset()
    add_prior(inner_changes=changes)
    expect('inner invalid ' + next(iter(changes)), lambda: history(), False)
for changes in [{'remaining_new': ['id']}, {'owned_processes': [33]}, {'runtime_entries': ['file']},
                {'baseline_unchanged': False}, {'exact_absent': {}}, {'exact_absent': {'id': {'absent': False}}}]:
    reset()
    add_prior(cleanup_changes=changes)
    expect('cleanup invalid ' + next(iter(changes)), lambda: history(), False)


def disposition(hashes, mode=0o400, changes=None, bound_sha=None):
    value = {'root_authorized_continue': True, 'state': 'ROOT_DISPOSED_RETIRED_INVOCATION',
             'run': 'prior01', 'original_hashes': hashes, 'reason': 'Root reviewed exact retired failure originals.'}
    value.update(changes or {})
    raw = encoded(value)
    MemoryPath.put('/disposition', raw, mode)
    grant = new_grant()
    grant['prior_failure_dispositions'] = {'prior01': {'path': '/disposition', 'sha256': bound_sha or digest(raw)}}
    return grant


reset()
_, hashes = add_prior(failed=True)
expect('fully retired failure without disposition remains blocked', lambda: history(), False)
grant = disposition(hashes)
expect('exact immutable root disposition allows continuation but preserves failed result', lambda: history(grant=grant), True)
assert history(grant=grant)[0]['prior_original_accepted'] is False
for kwargs in [{'mode': 0o600}, {'bound_sha': 'stale'}, {'changes': {'root_authorized_continue': 1}},
               {'changes': {'original_hashes': {}}}, {'changes': {'run': 'other01'}}, {'changes': {'reason': ''}}]:
    grant = disposition(hashes, **kwargs)
    expect('reject disposition ' + str(kwargs), lambda grant=grant: history(grant=grant), False)
reset()
paths, hashes = add_prior(failed=True)
grant = disposition(hashes)
del MemoryPath.files[paths['outer-result.json']]
expect('disposition never substitutes missing outer original', lambda: history(grant=grant), False)
reset()
_, hashes = add_prior(failed=True, inner_changes={'actual_wait_completed': False})
grant = disposition(hashes)
expect('disposition never substitutes missing actual wait', lambda: history(grant=grant), False)
reset()
_, hashes = add_prior(failed=True, cleanup_changes={'owned_processes': [33]})
grant = disposition(hashes)
expect('disposition never substitutes cleanup', lambda: history(grant=grant), False)

result = {'state': 'V02_ISOLATED_GATE_CHECKS_PASS', 'case_count': len(cases),
          'allow_cases': sum(row['actual'] == 'ALLOW' for row in cases),
          'block_cases': sum(row['actual'] == 'BLOCK' for row in cases), 'cases': cases,
          'identical_gate_functions_in_both_entrypoints': gate_names,
          'unchanged_accepted_retirement_helpers': reused,
          'unchanged_watchdog_runtime_environment_and_actual_graph_reader': True,
          'source_syntax_checked': syntax_files, 'selector_groups': 7, 'exact_tops': 14,
          'root_authorized_resources': False, 'driver_imported_or_executed': False,
          'actual_process_or_resource_retirement_tested': False,
          'scope': 'AST-extracted gate functions using memory-only paths and a synthetic parent identity. No Go, process, Docker, listener, provider or test body.'}
(ROOT / 'gate-checks.json').write_bytes(encoded(result))
print(json.dumps({key: value for key, value in result.items() if key != 'cases'}, indent=2))
