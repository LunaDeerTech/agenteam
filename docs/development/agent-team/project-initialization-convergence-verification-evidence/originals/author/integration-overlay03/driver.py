#!/usr/bin/env python3
"""Bounded offline contract verification; owns/reaps its entire subprocess tree."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path('/workspace/agenteam')
BASE = Path('/workspace/scratch/project-initialization-convergence-author')
INPUT = Path(sys.argv.pop(1))
INITIAL = json.loads(INPUT.read_text())
FILES = list(INITIAL['files'])

SOURCE_SUFFIXES = {'.go','.s','.S','.c','.h','.cc','.cpp','.cxx','.m','.mm','.f','.F','.for','.f90','.swig','.swigcxx','.syso'}
def fingerprints():
    result = {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() if Path(p).exists() else None for p in FILES}
    for directory,row in INITIAL.get('package_file_sets',{}).items():
        names=sorted(p.name for p in Path(directory).iterdir() if p.is_file() and (p.suffix in SOURCE_SUFFIXES or row['embed_directory']))
        # Effective file-set follows this frozen overlay, including explicit UI deletion.
        effective = set(names)
        for original, replacement in INITIAL.get('overlay_replace', {}).items():
            if str(Path(original).parent) == directory:
                if replacement:
                    effective.add(Path(original).name)
                else:
                    effective.discard(Path(original).name)
        names = sorted(effective)
        result['@set:'+directory]=hashlib.sha256(json.dumps(names).encode()).hexdigest()
    return result

def processes():
    found = {}
    for entry in Path('/proc').iterdir():
        if not entry.name.isdecimal():
            continue
        try:
            raw = (entry / 'stat').read_text()
            fields = raw[raw.rfind(')') + 2:].split()
            found[int(entry.name)] = {'pid': int(entry.name), 'ppid': int(fields[1]), 'pgid': int(fields[2]), 'state': fields[0], 'starttime': fields[19]}
        except (OSError, ValueError, IndexError):
            pass
    return found

def descendants(table):
    owned = {os.getpid()}
    while True:
        added = {p for p, info in table.items() if info['ppid'] in owned} - owned
        if not added:
            break
        owned.update(added)
    return [table[p] for p in sorted(owned - {os.getpid()}) if p in table]

name, *argv = sys.argv[1:]
cwd = ROOT
if len(argv) >= 2 and argv[0] == '--cwd':
    cwd = Path(argv[1])
    argv = argv[2:]
if argv and argv[0] == '--':
    argv = argv[1:]
if not argv or '/' in name:
    raise SystemExit('usage: driver.py unique-name -- command [args]')
run = BASE / name
run.mkdir(exist_ok=False)
libc = ctypes.CDLL(None, use_errno=True)
if libc.prctl(36, 1, 0, 0, 0) != 0:
    raise OSError(ctypes.get_errno(), 'PR_SET_CHILD_SUBREAPER')
subreaper = ctypes.c_int()
if libc.prctl(37, ctypes.byref(subreaper), 0, 0, 0) != 0 or subreaper.value != 1:
    raise RuntimeError('PR_GET_CHILD_SUBREAPER failed')
overrides = {
    'PATH': '/workspace/toolchains/go1.27.1/bin:/usr/local/bin:/usr/bin:/bin',
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
    'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOMODCACHE': '/workspace/go/pkg/mod',
    'GOCACHE': '/workspace/.cache/go-build', 'GOFLAGS': '-mod=readonly -p=1 -overlay=/workspace/scratch/project-initialization-convergence-author/integration-overlay03/overlay.json',
    'TMPDIR': str(BASE / 'tmp'), 'CGO_ENABLED': '1', 'GOMAXPROCS': '2',
    'AGENTEAM_PROJECT_AUDIT_NATIVE': '0',
    'AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON': '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',
    'AGENTEAM_PROJECT_READ_SCHEMA_PYTHON': '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',
    'AGENTEAM_PROJECT_OWNER_READ_NATIVE': '0',
    'AGENTEAM_PROJECT_OWNER_UPDATE_NATIVE': '0',
    'AGENTEAM_PROJECT_CREDENTIAL_NATIVE': '0',
    'AGENTEAM_PROJECT_MODEL_READ_NATIVE': '0',
    'AGENTEAM_PROJECT_MODEL_NATIVE': '0',
    'AGENTEAM_PROJECT_MODEL_SCHEMA_PYTHON': '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',
    'AGENTEAM_PROJECT_CREDENTIAL_SCHEMA_PYTHON': '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',
    'AGENTEAM_USAGE_SCHEMA_PYTHON': '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',
}
env = dict(os.environ)
env.update(overrides)
started = time.monotonic()
meta = {'argv': argv, 'cwd': str(cwd), 'environment_overrides': overrides, 'subreaper': True,
        'driver_pid': os.getpid(), 'driver_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'frozen_manifest_sha256': hashlib.sha256(INPUT.read_bytes()).hexdigest(),
        'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        'total_deadline_seconds': 45, 'child_timeout_seconds': 42, 'inputs_before': fingerprints()}
expected = dict(INITIAL['files'])
expected.update({'@set:'+directory:hashlib.sha256(json.dumps(row['names']).encode()).hexdigest() for directory,row in INITIAL.get('package_file_sets',{}).items()})
if meta['inputs_before'] != expected:
    raise SystemExit('frozen source mismatch')
(run / 'command.json').write_text(json.dumps(meta, indent=2) + '\n')
seen, reaped, actions = {}, [], []
rc, timed_out, proc = None, False, None
with (run / 'stdout.log').open('wb') as out, (run / 'stderr.log').open('wb') as err:
    proc = subprocess.Popen(argv, cwd=cwd, env=env, stdout=out, stderr=err, start_new_session=True)
    while True:
        current = descendants(processes())
        for info in current:
            seen[(info['pid'], info['starttime'])] = info
        rc = proc.poll()  # waitpid joins the direct child on exit.
        elapsed = time.monotonic() - started
        if rc is not None:
            break
        if elapsed >= 42:
            timed_out = True
            os.killpg(proc.pid, signal.SIGTERM)
            actions.append({'elapsed': elapsed, 'action': 'SIGTERM-owned-pgid', 'pgid': proc.pid})
            break
        time.sleep(0.03)
    # Reap direct child first, then adopted grandchildren; terminate only this tree.
    if rc is None:
        try:
            rc = proc.wait(timeout=max(0.01, 43 - (time.monotonic() - started)))
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            actions.append({'elapsed': time.monotonic() - started, 'action': 'SIGKILL-owned-pgid', 'pgid': proc.pid})
            rc = proc.wait(timeout=max(0.01, 44 - (time.monotonic() - started)))
    else:
        proc.wait()
    quiet = 0
    while time.monotonic() - started < 44.5:
        current = descendants(processes())
        for info in current:
            seen[(info['pid'], info['starttime'])] = info
            if info['state'] != 'Z':
                try:
                    os.kill(info['pid'], signal.SIGKILL)
                    actions.append({'elapsed': time.monotonic() - started, 'action': 'SIGKILL-adopted-child', 'pid': info['pid'], 'starttime': info['starttime']})
                except ProcessLookupError:
                    pass
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                break
            if pid == 0:
                break
            reaped.append({'pid': pid, 'wait_status': status})
        if not descendants(processes()):
            quiet += 1
            if quiet == 2:
                break
        else:
            quiet = 0
        time.sleep(0.03)
remaining = descendants(processes())
result = {'command_exit_code': rc, 'timed_out': timed_out, 'elapsed_seconds': time.monotonic() - started,
          'direct_child_pid': proc.pid, 'direct_child_joined': proc.returncode is not None,
          'subreaper': True, 'adopted_reaped': reaped, 'processes_seen': list(seen.values()),
          'actions': actions, 'remaining_descendants': remaining, 'inputs_after': fingerprints()}
result['inputs_match'] = all(value == meta['inputs_before'][path] or path in INITIAL.get('format_write_paths', []) for path,value in result['inputs_after'].items())
result['owned_clear_observations'] = quiet
result['exit_code'] = 124 if timed_out else (125 if remaining or actions or not result['inputs_match'] else rc)
for f in ('stdout.log', 'stderr.log'):
    result[f + '_sha256'] = hashlib.sha256((run / f).read_bytes()).hexdigest()
(run / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'run': name, 'exit_code': result['exit_code'], 'elapsed_seconds': result['elapsed_seconds'], 'direct_child_joined': result['direct_child_joined'], 'remaining_descendants': remaining, 'stdout': str(run / 'stdout.log'), 'stderr': str(run / 'stderr.log')}))
raise SystemExit(result['exit_code'])
