from pathlib import Path
import ctypes
import datetime
import hashlib
import json
import os
import shutil
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parent
EVIDENCE = ROOT / 'evidence'
RUNTIME = ROOT / 'runtime'
NODE = '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node'
PLAN = Path('/tmp/agenteam-d26-dom-focus-plan-u5cuud7j')

def write(name, value):
    (EVIDENCE / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')

def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()

def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def processes():
    found = {}
    for directory in Path('/proc').iterdir():
        if not directory.name.isdecimal():
            continue
        try:
            raw = (directory / 'stat').read_text()
            fields = raw[raw.rfind(')')+2:].split()
            found[int(directory.name)] = {
                'pid': int(directory.name), 'state': fields[0],
                'ppid': int(fields[1]), 'pgrp': int(fields[2]),
                'starttime': int(fields[19]),
                'comm': (directory / 'comm').read_text().strip(),
            }
        except (OSError, ValueError, IndexError):
            pass
    return found

def browser_baseline(found):
    return [p for p in found.values() if p['comm'].startswith(('chromium', 'chrome_crashpad'))]

assert digest(PLAN / 'plan.md') == 'de5b09fbb1a821779897e69e94369ba0a4e2bf05851d088fcee34a123633150e'
assert digest(PLAN / 'inputs.json') == 'ec6429edec037905003ccbce803b69336d76783bda478306875d73e99d1fd7c2'
dependencies = json.loads((PLAN / 'inputs.json').read_text())
for entry in [dependencies['package_lock'], dependencies['chromium'], *dependencies['packages']]:
    assert digest(entry['path']) == entry['sha256'], entry['path']
write('inputs.json', {
    'source': [{'path': str(ROOT / name), 'sha256': digest(ROOT / name)} for name in ('probe.cjs', 'run.py')],
    'plan': {'path': str(PLAN / 'plan.md'), 'sha256': digest(PLAN / 'plan.md')},
    'dependency_identity': dependencies,
})

for name in ('tmp', 'config', 'cache'):
    (RUNTIME / name).mkdir(exist_ok=True)
env = {
    'PATH': os.environ['PATH'],
    'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'TZ': 'UTC',
    'TMPDIR': str(RUNTIME / 'tmp'),
    'XDG_CONFIG_HOME': str(RUNTIME / 'config'),
    'XDG_CACHE_HOME': str(RUNTIME / 'cache'),
    'NODE_DISABLE_COMPILE_CACHE': '1',
    'PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD': '1',
    'DEBUG': 'pw:browser',
}
preflight = []
for argv in ([NODE, '--version'], [NODE, '--check', str(ROOT / 'probe.cjs')]):
    run = subprocess.run(argv, cwd=ROOT, env=env, text=True, capture_output=True, timeout=10)
    preflight.append({'argv': argv, 'cwd': str(ROOT), 'env': env, 'exit': run.returncode,
                      'stdout': run.stdout, 'stderr': run.stderr})
    write('preflight.json', preflight)
    assert run.returncode == 0, preflight[-1]

before = processes()
write('process-before.json', {'at': stamp(), 'browser_processes': browser_baseline(before)})
# Reap only descendants of this driver if Chromium's intermediate parent exits.
assert ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0
owned = {}
argv = [NODE, str(ROOT / 'probe.cjs')]
command = {'argv': argv, 'cwd': str(ROOT), 'env': env, 'started_at': stamp(),
           'hard_timeout_seconds': 45, 'attempt': 1}
write('command.json', command)
started = time.monotonic()
timed_out = False
cleanup_signals = []
with (EVIDENCE / 'stdout.log').open('w') as stdout, (EVIDENCE / 'stderr.log').open('w') as stderr:
    child = subprocess.Popen(argv, cwd=ROOT, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
    def observe():
        current = processes()
        parents = {child.pid, os.getpid()}
        parents.update(pid for (pid, starttime) in owned if pid in current and current[pid]['starttime'] == starttime)
        while True:
            additions = {pid for pid, item in current.items() if item['ppid'] in parents}
            if additions.issubset(parents):
                break
            parents.update(additions)
        for pid in parents:
            if pid == os.getpid() or pid not in current:
                continue
            item = dict(current[pid])
            key = (pid, item['starttime'])
            if key not in owned:
                try:
                    item['argv'] = (Path('/proc') / str(pid) / 'cmdline').read_bytes().split(b'\0')
                    item['argv'] = [arg.decode(errors='replace') for arg in item['argv'] if arg]
                except OSError:
                    item['argv'] = []
                owned[key] = item
        return current
    def signal_owned(signum):
        current = observe()
        for (pid, starttime), item in reversed(list(owned.items())):
            if pid in current and current[pid]['starttime'] == starttime and current[pid]['state'] != 'Z':
                try:
                    os.kill(pid, signum)
                    cleanup_signals.append({'pid': pid, 'starttime': starttime, 'signal': signum})
                except ProcessLookupError:
                    pass
    while child.poll() is None:
        observe()
        if time.monotonic() - started > 45:
            timed_out = True
            signal_owned(signal.SIGTERM)
            time.sleep(0.2)
            signal_owned(signal.SIGKILL)
            break
        time.sleep(0.05)
    exit_code = child.wait(timeout=5)
    current = observe()
    survivors = [item for key, item in owned.items() if key[0] in current and current[key[0]]['starttime'] == key[1] and current[key[0]]['state'] != 'Z']
    if survivors:
        signal_owned(signal.SIGTERM)
        time.sleep(0.2)
        signal_owned(signal.SIGKILL)

reaped = []
while True:
    try:
        pid, status = os.waitpid(-1, os.WNOHANG)
        if pid == 0:
            break
        reaped.append({'pid': pid, 'wait_status': status})
    except ChildProcessError:
        break
after = processes()
remaining = [after[pid] for (pid, starttime) in owned if pid in after and after[pid]['starttime'] == starttime]
live_remaining = [item for item in remaining if item['state'] != 'Z']
command.update({'ended_at': stamp(), 'elapsed_seconds': round(time.monotonic() - started, 6),
                'exit': exit_code, 'timed_out': timed_out})
write('result.json', command)
resource = {'at': stamp(), 'owned': list(owned.values()), 'cleanup_signals': cleanup_signals, 'reaped': reaped,
            'remaining_owned': remaining, 'live_remaining_owned': live_remaining,
            'browser_processes_before': browser_baseline(before),
            'browser_processes_after': browser_baseline(after)}
write('process-final.json', resource)
assert not live_remaining, live_remaining

runtime_entries = []
for path in RUNTIME.rglob('*'):
    if path.is_file() and not path.is_symlink():
        runtime_entries.append({'path': str(path.relative_to(RUNTIME)), 'bytes': path.stat().st_size, 'sha256': digest(path)})
    elif path.is_symlink():
        runtime_entries.append({'path': str(path.relative_to(RUNTIME)), 'symlink': os.readlink(path)})
write('runtime-before-cleanup.json', runtime_entries)
for path in list(RUNTIME.iterdir()):
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink()
write('runtime-cleanup.json', {'path': str(RUNTIME), 'remaining': [p.name for p in RUNTIME.iterdir()], 'at': stamp()})
for entry in json.loads((EVIDENCE / 'inputs.json').read_text())['source']:
    assert digest(entry['path']) == entry['sha256']
print(json.dumps({'root': str(ROOT), 'exit': exit_code, 'timed_out': timed_out,
                  'owned_processes_observed': len(owned), 'owned_remaining': len(remaining),
                  'owned_live_remaining': len(live_remaining), 'runtime_empty': not any(RUNTIME.iterdir())}))
raise SystemExit(exit_code if not timed_out else 124)
