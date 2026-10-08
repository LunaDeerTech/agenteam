#!/usr/bin/env python3
"""One root-granted invocation; retain the actual outer wait for driver.py."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parent
PYTHON = '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3'


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def grant_errors(value, name, group, mode, driver_sha, launcher_sha, same_bytes):
    errors = []
    if value.get('root_authorized_resources') is not True:
        errors.append('resource authorization must be boolean true')
    if mode & 0o222 or not same_bytes:
        errors.append('grant must be immutable and unchanged')
    if re.fullmatch(r'[a-z0-9][a-z0-9-]{0,19}', name) is None:
        errors.append('invalid invocation name')
    if value.get('permitted_run_name') != name or value.get('permitted_groups') != [group]:
        errors.append('grant must bind one exact name and group')
    if value.get('pending_inputs') != []:
        errors.append('preparation is still pending')
    if value.get('driver_sha256') != driver_sha or value.get('launcher_sha256') != launcher_sha:
        errors.append('both execution source hashes must match')
    return errors


def validate_execution_grant(path, raw, name, group):
    value = json.loads(raw)
    errors = grant_errors(value, name, group, path.stat().st_mode,
                          hashlib.sha256((ROOT / 'driver.py').read_bytes()).hexdigest(),
                          hashlib.sha256((ROOT / 'launch.py').read_bytes()).hexdigest(),
                          path.read_bytes() == raw)
    if errors:
        raise RuntimeError('; '.join(errors))
    return value


def process_starttime(pid):
    return int(Path(f'/proc/{pid}/stat').read_text().rsplit(') ', 1)[1].split()[19])


def current_launch_errors(intent, grant_sha, name, group, driver_sha, launcher_sha,
                          parent_pid, parent_starttime, copy_sha, copy_mode, intent_mode,
                          run_exists, terminal_exists):
    errors = []
    expected = {'run': name, 'group': group, 'root_grant_sha256': grant_sha,
                'driver_sha256': driver_sha, 'launcher_sha256': launcher_sha,
                'launcher_pid': parent_pid, 'launcher_starttime_ticks': parent_starttime,
                'state': 'PREPARED_TO_SPAWN'}
    if any(intent.get(key) != value for key, value in expected.items()):
        errors.append('current launch intent is not bound to this grant and actual parent')
    if copy_sha != grant_sha or copy_mode & 0o222 or intent_mode & 0o222:
        errors.append('current launch grant/intent copy is not immutable and exact')
    if run_exists or terminal_exists:
        errors.append('current invocation has already started or ended')
    return errors


def prior_terminal_errors(name, outer, launch, inner, intent, grant, hashes, disposition=None):
    errors = []
    if (intent.get('run') != name or grant.get('root_authorized_resources') is not True or
            grant.get('permitted_run_name') != name or grant.get('permitted_groups') != [intent.get('group')] or
            grant.get('driver_sha256') != intent.get('driver_sha256') or
            grant.get('launcher_sha256') != intent.get('launcher_sha256')):
        errors.append('prior intent/grant identity mismatch')
    for value in (outer, launch):
        if (value.get('run') != name or value.get('group') != intent.get('group') or
                value.get('root_grant_sha256') != hashes['root-grant.json'] or
                value.get('driver_sha256') != intent.get('driver_sha256') or
                value.get('launcher_sha256') != intent.get('launcher_sha256') or
                value.get('intent_sha256') != hashes['intent.json'] or
                value.get('actual_direct_wait_completed') is not True or value.get('spawned') is not True):
            errors.append('prior outer identity or actual wait is incomplete')
    if (outer.get('driver_pid') != launch.get('driver_pid') or
            outer.get('driver_starttime_ticks') != launch.get('driver_starttime_ticks') or
            type(outer.get('driver_pid')) is not int or type(outer.get('driver_starttime_ticks')) is not int):
        errors.append('prior outer process identity mismatch')
    if (outer.get('driver_result_sha256') != hashes['inner-result.json'] or
            outer.get('root_grant_bytes_unchanged') is not True or
            intent.get('root_grant_sha256') != hashes['root-grant.json'] or
            inner.get('run') != name or inner.get('actual_wait_completed') is not True or
            inner.get('watchdog_thread_joined') is not True or inner.get('double_cleanup') is not True):
        errors.append('prior inner result binding or retirement is incomplete')
    passed = (type(outer.get('exit_code')) is int and outer.get('exit_code') == 0 and
              type(launch.get('exit_code')) is int and launch.get('exit_code') == 0 and
              outer.get('accepted') is True and outer.get('driver_accepted') is True and
              type(inner.get('driver_exit')) is int and inner.get('driver_exit') == 0 and inner.get('accepted') is True)
    if not passed:
        # Disposition does not replace missing originals or actual retirement.
        expected_hashes = {key: hashes[key] for key in
                           ('root-grant.json', 'intent.json', 'launch.json', 'outer-result.json', 'inner-result.json', 'cleanup.json')}
        if (not disposition or disposition.get('root_authorized_continue') is not True or
                disposition.get('state') != 'ROOT_DISPOSED_RETIRED_INVOCATION' or
                disposition.get('run') != name or disposition.get('original_hashes') != expected_hashes or
                not isinstance(disposition.get('reason'), str) or not disposition['reason'].strip()):
            errors.append('prior failed invocation has no exact root disposition')
    return errors


def check_invocation_history(name, group, grant, raw, inside_driver):
    launch_root, run_root = ROOT / 'launches', ROOT / 'runs'
    launch_names = {p.name for p in launch_root.iterdir()} if launch_root.exists() else set()
    run_names = {p.name for p in run_root.iterdir()} if run_root.exists() else set()
    if inside_driver:
        current = launch_root / name
        if name not in launch_names or not current.is_dir():
            raise RuntimeError('resource driver requires its bound active outer launcher')
        intent_path, copy_path = current / 'intent.json', current / 'root-grant.json'
        intent = json.loads(intent_path.read_bytes())
        errors = current_launch_errors(intent, hashlib.sha256(raw).hexdigest(), name, group,
                                       grant['driver_sha256'], grant['launcher_sha256'],
                                       os.getppid(), process_starttime(os.getppid()),
                                       hashlib.sha256(copy_path.read_bytes()).hexdigest(),
                                       copy_path.stat().st_mode, intent_path.stat().st_mode,
                                       name in run_names, (current / 'result.json').exists())
        if errors:
            raise RuntimeError('; '.join(errors))
    elif name in launch_names or name in run_names:
        raise RuntimeError('invocation name already exists; no overwrite/retry')
    records = []
    for previous in sorted((launch_names | run_names) - ({name} if inside_driver else set())):
        launch_dir, run_dir = launch_root / previous, run_root / previous
        paths = {'root-grant.json': launch_dir / 'root-grant.json', 'intent.json': launch_dir / 'intent.json',
                 'launch.json': launch_dir / 'launch.json', 'outer-result.json': launch_dir / 'result.json',
                 'inner-result.json': run_dir / 'result.json', 'cleanup.json': run_dir / 'cleanup.json'}
        if not all(path.is_file() for path in paths.values()):
            raise RuntimeError('prior invocation has missing originals: ' + previous)
        values, hashes = {}, {}
        for key, path in paths.items():
            original = path.read_bytes()
            values[key] = json.loads(original)
            hashes[key] = hashlib.sha256(original).hexdigest()
        disposition = None
        bound = grant.get('prior_failure_dispositions', {}).get(previous)
        if bound:
            path = Path(bound['path'])
            original = path.read_bytes()
            if path.stat().st_mode & 0o222 or hashlib.sha256(original).hexdigest() != bound['sha256']:
                raise RuntimeError('root disposition is not immutable and exact')
            disposition = json.loads(original)
        errors = prior_terminal_errors(previous, values['outer-result.json'], values['launch.json'],
                                       values['inner-result.json'], values['intent.json'],
                                       values['root-grant.json'], hashes, disposition)
        cleanup = values['cleanup.json']
        if (not isinstance(cleanup, list) or len(cleanup) != 2 or
                any(row.get('baseline_unchanged') is not True or row.get('remaining_new') or
                    row.get('owned_processes') or row.get('runtime_entries') or
                    not row.get('exact_absent') or any(item.get('absent') is not True for item in row['exact_absent'].values())
                    for row in cleanup)):
            errors.append('prior two original cleanup records are not complete')
        if errors:
            raise RuntimeError(previous + ': ' + '; '.join(errors))
        records.append({'run': previous, 'original_hashes': hashes,
                        'root_disposition': bound, 'prior_original_accepted': values['outer-result.json'].get('accepted') is True})
    return records



def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--name', required=True)
    parser.add_argument('--group', required=True)
    parser.add_argument('--frozen', type=Path, required=True)
    args = parser.parse_args()
    if re.fullmatch(r'[a-z0-9][a-z0-9-]{0,19}', args.name) is None:
        raise ValueError('invalid run name')
    frozen_bytes = args.frozen.read_bytes()
    frozen = validate_execution_grant(args.frozen, frozen_bytes, args.name, args.group)
    prior_history = check_invocation_history(args.name, args.group, frozen, frozen_bytes, False)
    # The driver checks the complete input gate before any business resource.
    out = ROOT / 'launches' / args.name
    out.mkdir(parents=True, mode=0o700)
    (out / 'root-grant.json').write_bytes(frozen_bytes)
    (out / 'root-grant.json').chmod(0o400)
    intent = {'state': 'PREPARED_TO_SPAWN', 'run': args.name, 'group': args.group,
              'root_grant_sha256': hashlib.sha256(frozen_bytes).hexdigest(),
              'driver_sha256': frozen['driver_sha256'], 'launcher_sha256': frozen['launcher_sha256'],
              'launcher_pid': os.getpid(), 'launcher_starttime_ticks': process_starttime(os.getpid())}
    write(out / 'intent.json', intent)
    (out / 'intent.json').chmod(0o400)
    write(out / 'prior-invocation-history.json', prior_history)
    argv = [PYTHON, '-I', '-B', str(ROOT / 'driver.py'), '--name', args.name,
            '--group', args.group, '--frozen', str(args.frozen), '--execute-authorized']
    process = None
    cancellations = []

    def cancel(signum, _frame):
        cancellations.append({'time': utc(), 'signal': signum})
        if process is not None:
            try:
                process.send_signal(signal.SIGTERM)
            except ProcessLookupError:
                pass

    signal.signal(signal.SIGTERM, cancel)
    signal.signal(signal.SIGINT, cancel)
    started = time.monotonic()
    record = {'run': args.name, 'group': args.group, 'spawned': False, 'intent_sha256': hashlib.sha256((out / 'intent.json').read_bytes()).hexdigest(), 'argv': argv, 'cwd': str(ROOT), 'started_utc': utc(),
              'root_grant_sha256': hashlib.sha256(frozen_bytes).hexdigest(),
              'driver_sha256': frozen['driver_sha256'],
              'launcher_sha256': frozen['launcher_sha256'],
              'outer_parent_pid': os.getpid(), 'actual_direct_wait_completed': False}
    with (out / 'raw.stdout').open('wb') as stdout, (out / 'raw.stderr').open('wb') as stderr:
        try:
            if args.frozen.read_bytes() != frozen_bytes:
                raise RuntimeError('root grant changed before driver launch')
            process = subprocess.Popen(argv, cwd=ROOT, stdout=stdout, stderr=stderr, start_new_session=True)
            record['spawned'] = True
            record['driver_pid'] = process.pid
            try:
                stat = Path(f'/proc/{process.pid}/stat').read_text().rsplit(') ', 1)[1].split()
                record['driver_starttime_ticks'] = int(stat[19])
            except FileNotFoundError:
                record['driver_starttime_ticks'] = None
            write(out / 'launch.json', record)
            if cancellations:
                cancel(signal.SIGTERM, None)
        except BaseException as exc:
            record['owner_error'] = {'type': type(exc).__name__, 'message': str(exc)}
            cancel(signal.SIGTERM, None)
        finally:
            if process is not None:
                while True:
                    try:
                        record['exit_code'] = process.wait()
                        break
                    except InterruptedError:
                        continue
                record['actual_direct_wait_completed'] = True
                record['ended_utc'] = utc()
                record['elapsed_seconds'] = time.monotonic() - started
                record['cancellations'] = cancellations
                write(out / 'launch.json', record)
            else:
                record['exit_code'] = None
                record['ended_utc'] = utc()
                record['elapsed_seconds'] = time.monotonic() - started
                record['cancellations'] = cancellations
                write(out / 'launch.json', record)
    record['root_grant_bytes_unchanged'] = args.frozen.read_bytes() == frozen_bytes
    result_path = ROOT / 'runs' / args.name / 'result.json'
    if result_path.is_file():
        record['driver_result_sha256'] = hashlib.sha256(result_path.read_bytes()).hexdigest()
        record['driver_accepted'] = json.loads(result_path.read_bytes()).get('accepted') is True
    else:
        record['driver_accepted'] = False
    record['accepted'] = (record.get('exit_code') == 0 and record['actual_direct_wait_completed']
                          and record['driver_accepted'] and not cancellations and not record.get('owner_error') and record['root_grant_bytes_unchanged'])
    write(out / 'result.json', record)
    print(json.dumps(record), flush=True)
    return 0 if record['accepted'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
