#!/usr/bin/env python3
"""One fixed eight-case Session probe; requires the assigned local resource window."""
import argparse
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
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
        try:
            runtime.rmdir()
            facts['runtime_empty'] = True
        except OSError:
            facts['runtime_empty'] = False
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
