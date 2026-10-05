import hashlib
import json
import os
import pathlib
import subprocess
import time

ROOT = pathlib.Path('/workspace/agenteam-object-join-probe-e__5hndp')
SNAPSHOT = ROOT / 'snapshot'
RUN = ROOT / 'normal-lock-01'
RUN.mkdir(exist_ok=False)

def save(name, value):
    (RUN / name).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')

def call(args):
    return subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

def inventory():
    result = {'containers': {}, 'networks': {}}
    for kind, args, inspect in (
        ('containers', ['docker', 'ps', '-aq', '--no-trunc'], ['docker', 'inspect']),
        ('networks', ['docker', 'network', 'ls', '-q', '--no-trunc'], ['docker', 'network', 'inspect']),
    ):
        listed = call(args)
        if listed.returncode:
            raise RuntimeError('inventory listing failed: ' + kind)
        template = '{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Config.Labels}}}' if kind == 'containers' else '{"id":{{json .Id}},"name":{{json .Name}},"labels":{{json .Labels}}}'
        for identity in listed.stdout.split():
            inspected = call(inspect + ['--format', template, identity])
            if inspected.returncode:
                continue  # A normally removed owned object may disappear mid-sample.
            item = json.loads(inspected.stdout)
            result[kind][item['id']] = item
    return result

def process_snapshot():
    found = {}
    for entry in pathlib.Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        try:
            data = (entry / 'stat').read_text()
            tail = data[data.rindex(')') + 2:].split()
            found[int(entry.name)] = {
                'pid': int(entry.name), 'ppid': int(tail[1]), 'pgrp': int(tail[2]),
                'start': tail[19], 'name': data[data.index('(')+1:data.rindex(')')],
                'state': tail[0],
            }
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
    return found

frozen = json.loads((ROOT / 'input.json').read_text())
def source_check():
    checks = []
    for item in frozen['inputs']:
        path = item.get('path')
        expected = item.get('sha256')
        if path is None or expected is None:
            raise RuntimeError('unexpected frozen input format')
        actual = hashlib.sha256((SNAPSHOT / path).read_bytes()).hexdigest()
        checks.append({'path': path, 'sha256': actual, 'matches': actual == expected})
    return {'all_match': all(item['matches'] for item in checks), 'files': checks}

before_sources = source_check()
save('source-before.json', before_sources)
if not before_sources['all_match']:
    raise RuntimeError('fixed input mismatch before driver')

baseline = inventory()
save('baseline.json', baseline)
if len(baseline['containers']) != 2 or len(baseline['networks']) != 4:
    raise RuntimeError('expected root-coordinated baseline 2 containers / 4 networks')

environment = dict(frozen['environment'])
environment.update({
    'AGENTEAM_GO': '/workspace/toolchains/go1.27.1/bin/go',
    'AGENTEAM_MINIO_BINARY': '/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio',
    'GOFLAGS': '-mod=readonly -buildvcs=false -v',
    'TMPDIR': str(ROOT / 'runtime'),
    'GONOSUMDB': '', 'GONOPROXY': '', 'GOPRIVATE': '',
})
argv = ['sh', 'scripts/test-objects.sh', '-run', '^TestObjectProjectWorkPendingJoinKeepsRuntimeGuard$']
save('invocation.json', {
    'authorization': 'root granted the unique fixture window for ordinary owned lock contention only',
    'baseline_commit': frozen['baseline'], 'input_sha256': hashlib.sha256((ROOT / 'input.json').read_bytes()).hexdigest(),
    'probe_sha256': frozen['probe_sha256'], 'cwd': str(SNAPSHOT),
    'argv': argv, 'environment': environment,
    'driver_inner_flags': ['-tags=integration', '-race', '-count=1', '-timeout=6m'],
    'network_fault_probe': 'remains stopped; no new proxy/interruption/abandoned backend preservation',
})
owned = {'containers': {}, 'networks': {}}
observed = {}
started = time.time()
with (RUN / 'driver.log').open('w') as log:
    driver = subprocess.Popen(argv, cwd=SNAPSHOT, env=dict(os.environ, **environment), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    save('started.json', {'pid': driver.pid, 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(started))})
    print('driver started', driver.pid, flush=True)
    while True:
        processes = process_snapshot()
        descendants = {driver.pid}
        while True:
            more = {pid for pid, item in processes.items() if item['ppid'] in descendants}
            if more <= descendants:
                break
            descendants |= more
        for pid in descendants:
            if pid in processes:
                item = processes[pid]
                observed[str(pid) + ':' + item['start']] = item
        current = inventory()
        for kind in owned:
            for identity, item in current[kind].items():
                if identity not in baseline[kind]:
                    owned[kind][identity] = item
        save('owned.json', owned)
        save('processes-observed.json', list(observed.values()))
        if driver.poll() is not None:
            break
        time.sleep(0.4)
    status = driver.wait()

finished = time.time()
save('result.json', {'exit_code': status, 'seconds': round(finished-started, 3), 'finished_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(finished))})

checks = []
for repetition in (1, 2):
    absent = []
    for kind in owned:
        for identity in owned[kind]:
            args = ['docker', 'inspect', identity] if kind == 'containers' else ['docker', 'network', 'inspect', identity]
            inspected = call(args)
            absent.append({'kind': kind, 'id': identity, 'exit_code': inspected.returncode, 'absent': inspected.returncode != 0 and 'No such' in inspected.stderr, 'stderr': inspected.stderr.strip()})
    after = inventory()
    processes = process_snapshot()
    survivors = [item for item in observed.values() if item['pid'] in processes and processes[item['pid']]['start'] == item['start']]
    path_survivors = []
    for pid, item in processes.items():
        if pid == os.getpid():
            continue
        try:
            command = (pathlib.Path('/proc') / str(pid) / 'cmdline').read_bytes()
            cwd = os.readlink(pathlib.Path('/proc') / str(pid) / 'cwd')
            if str(ROOT).encode() in command or cwd.startswith(str(ROOT)):
                path_survivors.append(item)
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
    checks.append({
        'repetition': repetition, 'owned_absence': absent,
        'baseline_unchanged': after == baseline, 'inventory': after,
        'observed_processes_remaining': survivors, 'workspace_processes_remaining': path_survivors,
        'runtime_remaining': sorted(str(p.relative_to(ROOT / 'runtime')) for p in (ROOT / 'runtime').rglob('*')),
        'gotmp_remaining': sorted(str(p.relative_to(ROOT / 'gotmp')) for p in (ROOT / 'gotmp').rglob('*')),
    })
    if repetition == 1:
        time.sleep(0.4)
save('resource-handoff.json', {'owned': owned, 'tracked_processes': len(observed), 'checks': checks})
after_sources = source_check()
save('source-after.json', after_sources)
print(json.dumps({'exit_code': status, 'seconds': round(finished-started,3), 'owned_containers': len(owned['containers']), 'owned_networks': len(owned['networks']), 'tracked_processes': len(observed), 'source_matches': after_sources['all_match'], 'handoff': 'normal-lock-01/resource-handoff.json'}), flush=True)
