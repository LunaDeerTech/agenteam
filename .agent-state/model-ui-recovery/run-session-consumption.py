#!/usr/bin/env python3
"""One fixed eight-case Session probe; requires the assigned local resource window."""
import argparse
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
SOURCE = Path(__file__).resolve().parent
ROOT = SOURCE.parents[1]
OUTPUT = ROOT / 'output/ai/model-ui-session-probe'
spec = importlib.util.spec_from_file_location('session_owned_cleanup', SOURCE / 'run-shared-components.py')
owned = importlib.util.module_from_spec(spec)
spec.loader.exec_module(owned)


def write(path, value):
    with path.open('x') as output:
        json.dump(value, output, indent=2)
        output.write('\n')


def digest(path):
    assert path.is_file() and not path.is_symlink()
    return hashlib.sha256(path.read_bytes()).hexdigest()


def retire_runtime(runtime, expected_identity, allowed):
    """Remove only the observed Chromium singleton residue, after real joins.

    No contents are read. Unknown entries, aliases, or changed identities remain
    for explicit inspection. All unlink/rmdir operations stay relative to the
    opened owned directory and its fixed /tmp parent, with no recursive removal.
    """
    if not allowed or runtime.parent != Path('/tmp') or re.fullmatch(r'ms-[0-9a-f]{12}', runtime.name) is None:
        return None
    parent_fd = runtime_fd = None
    try:
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
        parent_fd = os.open('/tmp', flags)
        runtime_fd = os.open(runtime.name, flags, dir_fd=parent_fd)
        directory = os.fstat(runtime_fd)
        if (directory.st_dev, directory.st_ino) != expected_identity or directory.st_uid != os.getuid() or stat.S_IMODE(directory.st_mode) != 0o700:
            return None
        names = os.listdir(runtime_fd)
        if len(names) > 1:
            return None
        if names:
            name = names[0]
            if re.fullmatch(r'\.org\.chromium\.Chromium\.[A-Za-z0-9]{6}', name) is None:
                return None
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=runtime_fd)
            try:
                opened = os.fstat(fd)
                if not stat.S_ISREG(opened.st_mode) or stat.S_IMODE(opened.st_mode) != 0o600 or opened.st_uid != os.getuid() or opened.st_nlink != 1:
                    return None
                current = os.stat(name, dir_fd=runtime_fd, follow_symlinks=False)
                if (current.st_dev, current.st_ino) != (opened.st_dev, opened.st_ino):
                    return None
                os.unlink(name, dir_fd=runtime_fd)
            finally:
                os.close(fd)
        current_directory = os.stat(runtime.name, dir_fd=parent_fd, follow_symlinks=False)
        if (current_directory.st_dev, current_directory.st_ino) != expected_identity:
            return None
        os.rmdir(runtime.name, dir_fd=parent_fd)
        return len(names)
    except OSError:
        return None
    finally:
        if runtime_fd is not None:
            os.close(runtime_fd)
        if parent_fd is not None:
            os.close(parent_fd)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--run', action='store_true', required=True)
    parser.parse_args()
    os.umask(0o077)
    assert OUTPUT.is_dir() and not OUTPUT.is_symlink()
    # Cross-task scheduling remains the responsible agent's job. These known
    # retained-failure markers additionally reject an accidental overlap.
    for marker in (OUTPUT / 'active.json', ROOT / 'output/ai/model-ui-recovery/shared-focus-active.json', ROOT / 'output/ai/model-ui-regression/active-run.json'):
        assert not marker.exists() and not marker.is_symlink()
    prepared_path = OUTPUT / 'prepared.json'
    prepared = json.loads(prepared_path.read_text())
    assert prepared['network_started'] is False and prepared['cases'] == 8
    assert prepared['playwright'] == '1.56.1' and prepared['browser_version'] == '151.0.7922.173'
    assert prepared['bundle_sha256'] == digest(OUTPUT / 'client.js')
    assert all(digest(Path(path)) == value for path, value in prepared['inputs'].items())
    frozen = dict(prepared['inputs'], **{str(prepared_path): digest(prepared_path), str(OUTPUT / 'client.js'): digest(OUTPUT / 'client.js')})
    nonce = secrets.token_hex(8)
    directory = OUTPUT / ('run-' + nonce)
    directory.mkdir(mode=0o700)
    runtime = Path('/tmp') / ('ms-' + nonce[:12])
    runtime.mkdir(mode=0o700)
    runtime_stat = runtime.lstat()
    runtime_identity = (runtime_stat.st_dev, runtime_stat.st_ino)
    marker = OUTPUT / 'active.json'
    write(marker, {'directory': str(directory), 'runtime': str(runtime), 'nonce': nonce, 'supervisor_pid': os.getpid()})
    marker_identity = (marker.stat().st_dev, marker.stat().st_ino)
    facts = {'exit': 1, 'runtime': str(runtime), 'body_seconds': 30, 'cleanup_seconds': 15, 'direct_actual_wait': False,
             'direct_exit': None, 'adopted_actual_waits': [], 'normal_adopted_settlement': False,
             'inputs_unchanged': False, 'retirement_complete': False}
    child = None
    started = time.monotonic()
    deadline = started + 45
    cleanup_deadline = None
    ports = set()
    assert ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0
    prior_handlers = {}

    def interrupted(_signum, _frame):
        raise InterruptedError('owned Session probe interrupted')

    for name in (signal.SIGTERM, signal.SIGINT):
        prior_handlers[name] = signal.signal(name, interrupted)
    try:
        environment = dict(os.environ, SESSION_PROBE_SUPERVISED=nonce, TMPDIR=str(runtime), DEBUG='', PWDEBUG='')
        child = subprocess.Popen(['node', str(SOURCE / 'session-consumption-probe.mjs'), '--worker', str(directory)], cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        facts['worker_pid'] = child.pid
        try:
            child.wait(timeout=max(.01, started + 30 - time.monotonic()))
        except subprocess.TimeoutExpired:
            facts['failure'] = 'SESSION_PROBE_BODY_BUDGET_EXCEEDED'
        facts.update(direct_actual_wait=child.returncode is not None, direct_exit=child.returncode)
    except Exception:
        facts.setdefault('failure', 'SESSION_PROBE_SUPERVISOR_FAILED')
    finally:
        # One absolute cleanup deadline for TERM, KILL, and all adopted waits.
        cleanup_deadline = min(deadline, time.monotonic() + 15)
        for name in prior_handlers:
            signal.signal(name, signal.SIG_IGN)
        if child is not None and child.returncode is None:
            child.terminate()
            try:
                child.wait(timeout=max(.01, min(5, cleanup_deadline - time.monotonic())))
            except subprocess.TimeoutExpired:
                owned.signal_owned(signal.SIGKILL)
                try:
                    child.wait(timeout=max(.01, cleanup_deadline - time.monotonic()))
                except subprocess.TimeoutExpired:
                    facts['failure'] = 'SESSION_PROBE_TERMINATION_INCOMPLETE'
            facts.update(direct_actual_wait=child.returncode is not None, direct_exit=child.returncode)
        if child is None or facts['direct_actual_wait']:
            facts['normal_adopted_settlement'] = owned.settle(facts['adopted_actual_waits'], cleanup_deadline)
        else:
            # A generic reap must never steal the still-owned Popen child's exit.
            owned.signal_owned(signal.SIGKILL)
        try:
            for name in ('http-owned.json', 'browser-owned.json'):
                path = directory / name
                if path.exists():
                    port = json.loads(path.read_text())['port']
                    assert isinstance(port, int) and 0 < port < 65536
                    ports.add(port)
            result = json.loads((directory / 'result.json').read_text())
            facts['worker_retirement_complete'] = result['retirement_complete'] is True
            facts['worker_failure'] = result['fail_code']
            facts['completed_cases'] = len(result['rows'])
            facts['all_observers_joined'] = len(result['rows']) == 8 and all(row['observer_joined_after_close'] for row in result['rows'])
            facts['inputs_unchanged'] = all(digest(Path(path)) == value for path, value in frozen.items())
        except Exception:
            result = None
            facts.setdefault('failure', 'SESSION_PROBE_TERMINAL_INCOMPLETE')
        facts['retirement_observations'] = [{'descendants': owned.descendants(), 'owned_listeners': owned.listening(ports)} for _ in range(2)]
        safe_runtime_cleanup = bool(child is not None and facts['direct_actual_wait'] and facts['direct_exit'] == 0
                                    and facts['normal_adopted_settlement'] and all(row['status'] == 0 for row in facts['adopted_actual_waits'])
                                    and facts.get('worker_retirement_complete') and facts.get('all_observers_joined')
                                    and facts['inputs_unchanged'] and facts.get('worker_failure') is None and 'failure' not in facts
                                    and all(not row['descendants'] and not row['owned_listeners'] for row in facts['retirement_observations']))
        removed = retire_runtime(runtime, runtime_identity, safe_runtime_cleanup)
        facts['runtime_empty'] = removed is not None
        facts['runtime_residue_removed'] = removed
        if removed is None:
            facts.setdefault('failure', 'SESSION_PROBE_RUNTIME_NOT_RETIRED')
        facts['retirement_complete'] = bool(child is not None and facts['direct_actual_wait'] and facts.get('worker_retirement_complete') and facts['runtime_empty'] and all(not row['descendants'] and not row['owned_listeners'] for row in facts['retirement_observations']))
        facts['exit'] = 0 if facts['retirement_complete'] and facts['normal_adopted_settlement'] and facts['direct_exit'] == 0 and facts['inputs_unchanged'] and facts.get('all_observers_joined') and facts.get('worker_failure') is None and 'failure' not in facts else 1
        if facts['retirement_complete']:
            assert (marker.stat().st_dev, marker.stat().st_ino) == marker_identity
            marker.unlink()
        facts['elapsed_seconds'] = time.monotonic() - started
        write(directory / 'terminal.json', facts)
        for name, handler in prior_handlers.items():
            signal.signal(name, handler)
    print(json.dumps({'directory': str(directory), **facts}))
    return facts['exit']


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception:
        print('SESSION_PROBE_SUPERVISOR_REJECTED', file=sys.stderr)
        raise SystemExit(1)
