#!/usr/bin/env python3
"""Fixed schema02 metadata-family outer invocation; original supervisor owns its resource tail."""
from pathlib import Path
import ctypes
import datetime
import hashlib
import importlib.util
import json
import os
import shutil
import signal
import stat
import subprocess
import sys
import time


def declared_binary(plan):
    command = plan['command']
    if command.count('--binary') != 1:
        raise ValueError('one binary flag required')
    index = command.index('--binary')
    if index + 1 >= len(command):
        raise ValueError('binary value required')
    binary = Path(command[index + 1])
    if not binary.is_absolute() or str(binary) not in plan['inputs']:
        raise ValueError('declared absolute binary required')
    return binary


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    root = Path(__file__).resolve().parents[3]
    plan_path = root / 'output/ai/agent-system-integration/scheduler-launch-01-inputs.json'
    plan = json.loads(plan_path.read_text())
    control = Path(plan['planned_control'])
    private = Path(plan['planned_private'])
    output = Path(plan['planned_output'])
    assert all(not p.exists() and not p.is_symlink() for p in (control, private, output))
    control.mkdir(parents=True, mode=0o700)
    result = {
        'outer_pid': os.getpid(),
        'started_utc': datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
        'source_ref': '281fc3cb', 'input_hash': plan['input_hash'],
        'launcher_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'command_sha256': hashlib.sha256(json.dumps(plan['command'], separators=(',', ':')).encode()).hexdigest(),
        'launch_input_sha256': hashlib.sha256(plan_path.read_bytes()).hexdigest(),
        'selector': plan['selector'], 'whole_pass': False, 'preflight': [],
    }

    def save():
        (control / 'result.json').write_text(json.dumps(result, indent=2) + '\n')

    sup = load('metadata_outer_supervisor', root / '.agent-state/task-planning-recovery/pg_only_supervisor.py')
    driver = load('metadata_outer_driver', root / '.agent-state/work-owner-http/root_chain_driver.py')
    child, baseline, binary, code = None, None, None, 1
    try:
        libc = ctypes.CDLL(None, use_errno=True)
        if libc.prctl(36, 1, 0, 0, 0) != 0:
            raise OSError(ctypes.get_errno(), 'subreaper')
        fresh = os.statvfs(root)
        result['fresh_available_bytes'] = fresh.f_bavail * fresh.f_frsize
        print(json.dumps({'stage': 'same-process preflight', 'utc': result['started_utc'],
                          'outer_pid': os.getpid(), 'fresh_available_bytes': result['fresh_available_bytes']}), flush=True)
        save()
        if result['fresh_available_bytes'] < 5 * 1024**3:
            raise RuntimeError('fresh disk below 5 GiB; zero resources started')
        env = plan['environment'].copy()
        for name in plan['removed_environment']:
            env.pop(name, None)
        result['removed_environment_absent'] = all(name not in env for name in plan['removed_environment'])
        for path in (private, private / 'go-config', private / 'go-config/go',
                     private / 'go-config/go/telemetry', private / 'docker'):
            path.mkdir(mode=0o700)
        fd = os.open(private / 'go-config/go/telemetry/mode', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, 'w') as stream:
            stream.write('off\n')
        assert not any((private / 'docker').iterdir())
        docker = shutil.which('docker', path=env['PATH'])
        result['docker_path'] = docker
        expected = plan['docker_preflight']
        assert docker == expected['resolved_path']
        path = Path(docker)
        item = path.lstat()
        assert stat.S_ISREG(item.st_mode) and item.st_nlink == 1 and path.resolve(strict=True) == path
        assert item.st_size == expected['bytes'] and driver.sha(path) == expected['sha256']

        # Metadata01 mistakenly used positional command[7] (--run). Require the
        # unique declared flag, then check the actual fixed executable identity.
        binary = declared_binary(plan)
        item = binary.lstat()
        assert stat.S_ISREG(item.st_mode) and item.st_nlink == 1 and binary.resolve(strict=True) == binary
        assert item.st_size == plan['candidate_bytes'] and stat.S_IMODE(item.st_mode) == 0o700
        assert driver.sha(binary) == plan['candidate_sha256']
        inputs = {str(p): driver.sha(p) for p in driver.metadata_inputs(binary, plan['selector'])}
        assert inputs == plan['inputs']
        result['inputs_before_count'] = len(inputs)
        assert hashlib.sha256(json.dumps(inputs, sort_keys=True, separators=(',', ':')).encode()).hexdigest() == plan['input_hash']
        baseline = sup.tcp()
        for label in ('agenteam.d03.fixture', 'agenteam.d04.networkfixture', 'agenteam.d05.objectfixture'):
            for kind in ('containers', 'networks'):
                argv = ([docker, 'ps', '-aq', '--filter', 'label=' + label] if kind == 'containers'
                        else [docker, 'network', 'ls', '-q', '--filter', 'label=' + label])
                process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
                try:
                    stdout, _ = process.communicate(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.communicate()
                    result['preflight'].append({'label': label, 'kind': kind, 'pid': process.pid,
                                                'actual_wait': process.returncode, 'timeout': True})
                    raise RuntimeError('original Docker preflight timed out')
                result['preflight'].append({'label': label, 'kind': kind, 'pid': process.pid,
                                            'actual_wait': process.returncode, 'id_count': len(stdout.splitlines())})
                save()
                if process.returncode != 0 or stdout.strip():
                    raise RuntimeError('original Docker preflight failed or resources present')
        with (control / 'supervisor.log').open('w', buffering=1) as log:
            child = subprocess.Popen(plan['command'], cwd=root, env=env, stdout=log,
                                     stderr=subprocess.STDOUT, start_new_session=True)
            result['supervisor_pid'] = child.pid
            save()
            print(json.dumps({'stage': 'launched', 'utc': datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
                              'outer_pid': os.getpid(), 'supervisor_pid': child.pid,
                              'fresh_available_bytes': result['fresh_available_bytes'],
                              'source_ref': result['source_ref'], 'selector': plan['selector']}), flush=True)
            code = child.wait()
            result['supervisor_actual_wait'] = code
            save()
    except BaseException as error:
        code = 1
        result['error_type'] = type(error).__name__
        result['error'] = str(error) if isinstance(error, (RuntimeError, AssertionError)) else 'outer operation failed'
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
            try:
                child.wait(timeout=60)
            except subprocess.TimeoutExpired:
                child.kill()
                try:
                    child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    result['direct_wait_incomplete'] = True
            result['supervisor_actual_wait'] = child.returncode
    finally:
        survivors = sup.descendants(os.getpid())
        result['outer_survivors_after_wait'] = sorted(survivors)
        if survivors:
            code = 1
            for pid in survivors:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        reaped, deadline = [], time.monotonic() + 5
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
                if pid == 0:
                    if time.monotonic() >= deadline:
                        code = 1
                        result['outer_reap_timeout'] = True
                        break
                    time.sleep(.02)
                    continue
                reaped.append({'pid': pid, 'status': status})
                code = 1
            except ChildProcessError:
                break
        result['outer_adopted_actual_waits'] = reaped
        observations = []
        for observation in (1, 2):
            remaining = sorted(sup.descendants(os.getpid()))
            observations.append(remaining)
            if remaining:
                code = 1
            if observation == 1:
                time.sleep(.1)
        result['outer_descendant_observations'] = observations
        if baseline is not None:
            deadline, empty, delta = time.monotonic() + 75, 0, set()
            while time.monotonic() < deadline and empty < 2:
                delta = sup.tcp() - baseline
                empty = empty + 1 if not delta else 0
                if empty < 2:
                    time.sleep(.1)
            result['outer_tcp_empty_observations'] = empty
            result['outer_tcp_last_delta_count'] = len(delta)
            if empty != 2:
                code = 1
        try:
            final_inputs = {str(p): driver.sha(p) for p in driver.metadata_inputs(binary, plan['selector'])}
            result['inputs_after_count'] = len(final_inputs)
            result['inputs_unchanged'] = final_inputs == plan['inputs']
            if not result['inputs_unchanged']:
                code = 1
        except BaseException:
            result['inputs_unchanged'] = False
            code = 1
        result['whole_pass'] = code == 0 and child is not None and result.get('supervisor_actual_wait') == 0
        result['exit_code'] = code
        result['finished_utc'] = datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
        save()
        print(json.dumps(result), flush=True)
    return 0 if result['whole_pass'] else 1


if __name__ == '__main__':
    sys.exit(main())
