#!/usr/bin/env python3
"""One Secret UI launch; the existing supervisor owns the seven-resource chain."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import shutil
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
OWNED = ROOT / 'output/ai/secret-owner-ui'
PYTHON = '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3'
SELECTOR = '^TestProjectSecretOwnerWeb$'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--attempt', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9]{2}', args.attempt):
        parser.error('one two-digit fresh attempt required')
    os.umask(0o077)
    control = OWNED / f'native-{args.attempt}-control'
    output = Path('/tmp') / f'psu{args.attempt}'
    evidence = OWNED / f'evidence-owner-{args.attempt}'
    if any(p.exists() or p.is_symlink() for p in (control, output, evidence)):
        parser.error('fresh owned attempt required')
    control.mkdir()
    result = {'selector': SELECTOR, 'exit': 1, 'stage': 'preflight',
              'outer_pid': os.getpid(), 'actual_supervisor_wait': False}
    child = None
    interrupted = False
    handlers = {}
    started = time.monotonic()
    try:
        result['fresh_bytes'] = shutil.disk_usage(ROOT).free
        if result['fresh_bytes'] < 5 * 1024 ** 3:
            raise ValueError('capacity')
        config = control / 'toolconfig'
        (config / 'go/telemetry').mkdir(parents=True)
        (config / 'go/telemetry/mode').write_text('off\n')
        docker = config / 'docker'
        docker.mkdir()
        env = os.environ.copy()
        for key in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD',
                    'GO_TELEMETRY_CHILD_UPLOAD', 'AGENTEAM_OBJECT_FIXTURE',
                    'AGENTEAM_OUTBOUND_FIXTURE', 'AGENTEAM_PG_FIXTURE',
                    'AGENTEAM_PG_UNSUPPORTED_FIXTURE',
                    'AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD'):
            env.pop(key, None)
        env.update(PYTHONDONTWRITEBYTECODE='1', GOTOOLCHAIN='local',
                   GOPROXY='off', GOSUMDB='off', GOFLAGS='-mod=readonly -p=2',
                   GOCACHE='/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build',
                   GOMODCACHE='/workspace/shared/agenteam-deps/go-mod',
                   XDG_CONFIG_HOME=str(config), DOCKER_CONFIG=str(docker),
                   GOTELEMETRY='off',
                   PATH='/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin:/usr/local/bin:/usr/bin:/bin',
                   AGENTEAM_SECRET_OWNER_WEB_DIST=str(OWNED / 'web-dist-01'),
                   AGENTEAM_SECRET_OWNER_WEB_EVIDENCE=str(evidence),
                   AGENTEAM_SECRET_OWNER_WEB_CASE='owner',
                   AGENTEAM_SECRET_OWNER_WEB_SCHEMA_PYTHON=PYTHON)
        if shutil.which('docker', path=env['PATH']) != '/usr/local/bin/docker':
            raise ValueError('docker')
        os.environ.clear()
        os.environ.update(env)
        binary = OWNED / 'candidate-02/secret-owner-ui-race.test'
        driver = ROOT / '.agent-state/work-owner-http/root_chain_driver.py'
        supervisor_path = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
        adapter = runpy.run_path(str(driver))
        supervisor = runpy.run_path(str(supervisor_path))
        adapter['configuration'](str(binary), SELECTOR, str(output / 'ui-preflight'))
        paths = adapter['knowledge_ui_inputs'](binary, SELECTOR)
        inputs = {str(p): adapter['sha'](p) for p in paths}
        digest = hashlib.sha256()
        for path, value in inputs.items():
            digest.update(path.encode() + b'\0' + value.encode() + b'\n')
        result.update(inputs=len(inputs), input_hash=digest.hexdigest())
        (control / 'inputs.json').write_text(json.dumps(inputs, sort_keys=True) + '\n')
        if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
            raise OSError('subreaper')
        baseline = supervisor['tcp']()
        command = [PYTHON, str(supervisor_path), '--root-chain', '--driver',
                   str(driver), '--binary', str(binary), '--run', SELECTOR,
                   '--output', str(output)]

        def stop(signum, frame):
            nonlocal interrupted
            interrupted = True
            if child is not None and child.poll() is None:
                child.send_signal(signal.SIGTERM)

        handlers = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
        with (control / 'supervisor.log').open('w') as log:
            child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                                     stderr=subprocess.STDOUT, start_new_session=True)
            result.update(stage='running', supervisor_pid=child.pid)
            print(json.dumps(result), flush=True)
            # The original supervisor retains all driver and resource budgets.
            code = child.wait()
        result.update(supervisor_exit=child.returncode,
                      actual_supervisor_wait=child.returncode is not None)
        survivors = supervisor['descendants'](os.getpid())
        if survivors:
            code = 1
            for pid in survivors:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        adopted = []
        deadline = time.monotonic() + 5
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
                if pid == 0:
                    if time.monotonic() >= deadline:
                        code = 1
                        break
                    time.sleep(.02)
                    continue
                adopted.append([pid, status])
                code = 1
            except ChildProcessError:
                break
        # Convert at observation time, before serialization; do not re-sample
        # after a failed record write and call that the original observation.
        descendants = [sorted(supervisor['descendants'](os.getpid())) for _ in (1, 2)]
        if any(descendants):
            code = 1
        empty = 0
        deadline = time.monotonic() + 75
        while empty < 2 and time.monotonic() < deadline:
            empty = empty + 1 if not supervisor['tcp']() - baseline else 0
            if empty < 2:
                time.sleep(.1)
        if empty != 2 or interrupted:
            code = 1
        result.update(exit=code, stage='complete', outer_descendants=descendants,
                      outer_tcp_empty_observations=empty, adopted=adopted,
                      survivors=sorted(survivors))
    except Exception as error:
        if child is not None and child.returncode is None:
            child.send_signal(signal.SIGTERM)
            child.wait()
            result.update(actual_supervisor_wait=True,
                          supervisor_exit=child.returncode)
        result.update(exit=1, error_type=type(error).__name__)
    finally:
        for signum, handler in handlers.items():
            signal.signal(signum, handler)
        result['elapsed'] = round(time.monotonic() - started, 3)
        (control / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result), flush=True)
    return result['exit']


if __name__ == '__main__':
    sys.exit(main())
