#!/usr/bin/env python3
"""One exact D11 PG top, actual child wait, then bounded host TCP tail.

Example: python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
 --driver /absolute/pg-only-driver --binary /absolute/work.test \
 --run '^TestTaskPlanningMigration$' --output /absolute/existing/output-dir
The output directory is reusable; each run/nonce directory must be new.
"""
import argparse
from collections import deque
import ctypes
import hashlib
import importlib.util
from itertools import islice
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import uuid


def budgets(root_chain):
    # Root: original Go test 360s + readiness 75s + fixture cleanup 55s +
    # build/scheduling allowance 50s. The separate 60s TERM grace allows the
    # original three owners' bounded cleanup; it does not extend a Go test.
    return (540, 60) if root_chain else (123, 3)


def root_adapter(driver):
    expected = Path(__file__).resolve().parents[2] / '.agent-state/work-owner-http/root_chain_driver.py'
    if driver.resolve() != expected:
        raise ValueError('root mode requires the exact task adapter')
    spec = importlib.util.spec_from_file_location('work_owner_root_adapter', expected)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def root_record(directory):
    path = directory / 'owned.json'
    if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600:
        raise ValueError('invalid owned root manifest permissions')
    raw = path.read_bytes()
    if len(raw) > 16384:
        raise ValueError('owned root manifest too large')
    record = json.loads(raw)
    if (set(record) != {'kind', 'resources', 'directories'}
            or record['kind'] != 'work-owner-root-chain'
            or len(record['resources']) != 7 or len(record['directories']) != 3):
        raise ValueError('incomplete seven-resource manifest')
    wanted = {'agenteam.d05.objectfixture': ['container', 'network'],
              'agenteam.d04.networkfixture': ['container', 'network'],
              'agenteam.d03.fixture': ['container', 'container', 'network']}
    seen, groups = set(), {}
    for item in record['resources']:
        if (set(item) != {'kind', 'id', 'label', 'nonce'}
                or item['label'] not in wanted or item['kind'] not in ('container', 'network')
                or re.fullmatch('[0-9a-f]{64}', item['id']) is None
                or re.fullmatch('[0-9a-f]{32}', item['nonce']) is None or item['id'] in seen):
            raise ValueError('invalid owned root resource identity')
        seen.add(item['id'])
        groups.setdefault(item['label'], []).append(item)
    for label, kinds in wanted.items():
        items = groups.get(label, [])
        if sorted(v['kind'] for v in items) != kinds or len({v['nonce'] for v in items}) != 1:
            raise ValueError('root resource nonce/group mismatch')
    runtime = (directory / 'runtime').resolve()
    for name in record['directories']:
        path = Path(name)
        if not path.is_absolute() or not path.resolve().is_relative_to(runtime) or path.resolve() == runtime:
            raise ValueError('private directory is outside owned runtime')
    return record


def exact_absent(item, timeout):
    # An unavailable daemon is not absence. Require Docker's exact missing-ID
    # diagnostic; never remove, prune or infer ownership from a name prefix.
    args = ['docker', item['kind'], 'inspect', item['id']]
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, timeout=timeout)
    missing = re.compile(r'(?:No such (?:object|container|network):\s*' + re.escape(item['id'])
                         + r'\b|network\s+' + re.escape(item['id']) + r'\s+not found)', re.I)
    return result.returncode != 0 and missing.search(result.stderr) is not None


def observe_root_chain(directory, log, log_path, selector):
    good = True
    try:
        record = root_record(directory)
    except (OSError, ValueError, TypeError, KeyError):
        record = None
        good = False
        log.write('STOP missing or invalid seven-resource ownership record\n')
    # A failed setup never manufactures seven resource retirements. It still
    # reports the actual runtime state and retains the original failure.
    deadline = time.monotonic() + 20
    for round in (1, 2):
        if record is not None:
            for item in record['resources']:
                absent = False
                try:
                    remaining = deadline - time.monotonic()
                    if remaining > 0:
                        absent = exact_absent(item, min(3, remaining))
                except (OSError, subprocess.TimeoutExpired):
                    pass
                log.write(f"ROOT resource_observation={round} kind={item['kind']} id={item['id']} nonce={item['nonce']} absent={absent}\n")
                if not absent: good = False
            private_absent = all(not Path(p).exists() and not Path(p).is_symlink()
                                 for p in record['directories'])
            log.write(f'ROOT private_observation={round} absent={private_absent}\n')
            if not private_absent: good = False
        runtime = directory / 'runtime'
        try:
            empty = runtime.is_dir() and not runtime.is_symlink() and not any(runtime.iterdir())
        except OSError:
            empty = False
        log.write(f'ROOT runtime_observation={round} empty={empty}\n')
        if not empty: good = False
        if round == 1: time.sleep(.1)
    log.flush()
    output = log_path.read_text()
    expected = {
        '^TestWorkOwnerRootActual(Command|Reader)Join$': {'TestWorkOwnerRootActualCommandJoin', 'TestWorkOwnerRootActualReaderJoin'},
        '^TestWorkOwnerHTTPProcessRoutingAndPersistence$': {'TestWorkOwnerHTTPProcessRoutingAndPersistence'},
        '^TestIndependentWorkOwnerRootConfirmationJoin$': {'TestIndependentWorkOwnerRootConfirmationJoin'},
        '^TestKnowledgeB02DirectPublication$': {'TestKnowledgeB02DirectPublication'},
        '^TestKnowledgeB02BusinessPublication$': {'TestKnowledgeB02BusinessPublication'},
        '^TestKnowledgeB02Cleanup$': {'TestKnowledgeB02Cleanup'},
        '^TestKnowledgeB02Runtime$': {'TestKnowledgeB02Runtime'},
        '^TestKnowledgeB02CommitUnknown$': {'TestKnowledgeB02CommitUnknown'},
        '^TestKnowledgeB02Concurrency$': {'TestKnowledgeB02Concurrency'},
        '^TestKnowledgeB02CleanupCommitUnknown$': {'TestKnowledgeB02CleanupCommitUnknown'},
        '^TestKnowledgeB02ProcessRecovery$': {'TestKnowledgeB02ProcessRecovery'},
        '^TestKnowledgeB02(Cleanup|CommitUnknown|Concurrency|CleanupCommitUnknown|ProcessRecovery)$': {'TestKnowledgeB02Cleanup', 'TestKnowledgeB02CommitUnknown', 'TestKnowledgeB02Concurrency', 'TestKnowledgeB02CleanupCommitUnknown', 'TestKnowledgeB02ProcessRecovery'},
    }.get(selector, set())
    actual = set(re.findall(r'^=== RUN   (Test\w+)$', output, re.M))
    waited = re.search(r'^D03 explicit test actual_wait pid=[1-9][0-9]* code=-?[0-9]+ selector='
                       + re.escape(selector) + r'$', output, re.M) is not None
    log.write(f'ROOT exact_tops={actual == expected} actual_test_wait={waited}\n')
    return good and actual == expected and waited


def tcp():
    rows = set()
    for name in ('tcp', 'tcp6'):
        for line in Path('/proc/net/' + name).read_text().splitlines()[1:]:
            fields = line.split()
            rows.add((name, fields[1], fields[2], fields[3], fields[9]))
    return rows


def tcp_rows_evidence(rows, limit=4096):
    # Evidence limits never apply to the original set used by the TCP gate.
    ordered = sorted(rows)
    return {'count': len(rows), 'truncated': len(rows) > limit,
            'rows': [dict(zip(('family', 'local', 'remote', 'state', 'inode'), row))
                     for row in ordered[:limit]]}


def tcp_process_identity(path):
    fields = (path / 'stat').read_text().rsplit(')', 1)[1].split()
    # Exclude comm, argv, environment, maps and every application value.
    return {'pid': int(path.name), 'state': fields[0], 'ppid': int(fields[1]),
            'pgid': int(fields[2]), 'sid': int(fields[3]),
            'start_ticks': int(fields[19])}


def tcp_executable_identity(path, deadline):
    link = path / 'exe'
    try:
        if time.monotonic() >= deadline:
            return None
        target = os.readlink(link)
        if time.monotonic() >= deadline:
            return None
        stat = link.stat()
        if time.monotonic() >= deadline:
            return None
        return {'path': target, 'device': stat.st_dev, 'inode': stat.st_ino}
    except (OSError, ValueError):
        return None


def tcp_entries_before_deadline(entries, deadline):
    while time.monotonic() < deadline:
        try:
            entry = next(entries)
        except StopIteration:
            return
        if time.monotonic() >= deadline:
            return
        yield entry


def tcp_owner_evidence(rows, deadline):
    # No owner is inferred from a port, process name, absent fd or inode zero.
    # A match means only that this stable PID held this inode during this scan;
    # its observation time is distinct from the preceding TCP sample.
    result = {'complete': False, 'reason': 'budget', 'processes': 0, 'fds': 0,
              'unreadable': 0, 'vanished': 0, 'unstable': 0,
              'input_rows': len(rows), 'rows_truncated': len(rows) > 4096,
              'inode_zero_considered_rows': 0, 'matches': []}
    if time.monotonic() >= deadline:
        return result
    considered = list(islice(rows, 4096))
    inodes = {row[4] for row in considered if row[4] != '0'}
    result['inode_zero_considered_rows'] = sum(row[4] == '0' for row in considered)
    if time.monotonic() >= deadline:
        return result
    if not inodes:
        result.update(complete=not result['rows_truncated'], reason='no_nonzero_inode')
        return result
    if time.monotonic() >= deadline:
        return result
    try:
        with os.scandir('/proc') as processes:
            for entry in tcp_entries_before_deadline(processes, deadline):
                if time.monotonic() >= deadline:
                    return result
                if not entry.name.isdecimal():
                    continue
                if result['processes'] >= 2048:
                    result['reason'] = 'limit'
                    return result
                result['processes'] += 1
                path = Path(entry.path)
                try:
                    before = tcp_process_identity(path)
                    if time.monotonic() >= deadline:
                        return result
                    with os.scandir(path / 'fd') as descriptors:
                        for descriptor in tcp_entries_before_deadline(descriptors, deadline):
                            if time.monotonic() >= deadline:
                                return result
                            if result['fds'] >= 8192 or len(result['matches']) >= 256:
                                result['reason'] = 'limit'
                                return result
                            result['fds'] += 1
                            try:
                                link = os.readlink(descriptor.path)
                                if time.monotonic() >= deadline:
                                    return result
                                match = re.fullmatch(r'socket:\[([0-9]+)\]', link)
                                if match is None or match[1] not in inodes:
                                    continue
                                executable = tcp_executable_identity(path, deadline)
                                if time.monotonic() >= deadline:
                                    return result
                                if executable is None:
                                    result['unreadable'] += 1
                                    continue
                                after = tcp_process_identity(path)
                                if time.monotonic() >= deadline:
                                    return result
                                second_link = os.readlink(descriptor.path)
                                if time.monotonic() >= deadline:
                                    return result
                                second_executable = tcp_executable_identity(path, deadline)
                                if time.monotonic() >= deadline:
                                    return result
                                if second_executable is None:
                                    result['unreadable'] += 1
                                    continue
                                if (before['start_ticks'] != after['start_ticks']
                                        or second_link != link
                                        or second_executable != executable):
                                    result['unstable'] += 1
                                    continue
                                if time.monotonic() >= deadline:
                                    return result
                                result['matches'].append({**after, 'fd': int(descriptor.name),
                                    'socket_inode': match[1], 'executable': executable,
                                    'observed_ns': time.monotonic_ns()})
                            except (FileNotFoundError, ProcessLookupError):
                                result['vanished'] += 1
                            except (OSError, ValueError, IndexError):
                                result['unreadable'] += 1
                except (FileNotFoundError, ProcessLookupError):
                    result['vanished'] += 1
                except (OSError, ValueError, IndexError):
                    result['unreadable'] += 1
    except OSError:
        result['reason'] = 'proc_unavailable'
        return result
    if time.monotonic() >= deadline:
        return result
    complete = (result['unreadable'] == 0 and result['vanished'] == 0
                and result['unstable'] == 0 and not result['rows_truncated'])
    result.update(complete=complete, reason='scanned' if complete else 'partial')
    return result


def tcp_evidence_pause(sample, tail_deadline):
    # Pay observation work from the existing 100ms inter-sample pause. Never
    # add a scan after the original tail deadline or wait for an owner to appear.
    pause_end = time.monotonic() + .1
    scan_end = min(pause_end, tail_deadline, time.monotonic() + .02)
    sample['owners'] = tcp_owner_evidence(sample['delta'], scan_end)
    remaining = pause_end - time.monotonic()
    if remaining > 0:
        time.sleep(remaining)


def save_tcp_failure(log_path, selector, baseline, baseline_times, samples, reread, log):
    def projection(sample):
        return {'started_ns': sample['started_ns'], 'ended_ns': sample['ended_ns'],
                'rows': tcp_rows_evidence(sample['rows']),
                'delta': tcp_rows_evidence(sample['delta']),
                'owners': sample.get('owners', {'complete': False, 'reason': 'not_scanned'})}
    value = {'version': 1, 'selector': selector, 'gate_unchanged': True,
             'owner_semantics': 'same-inode-fd-observed-later-no-ownership-classification',
             'baseline': {**tcp_rows_evidence(baseline), **baseline_times},
             'last_two_loop_samples': [projection(sample) for sample in samples],
             'failure_reread': projection(reread)}
    path = log_path.with_suffix('.tcp-tail-failure.json')
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, sort_keys=True)
            stream.write('\n')
        log.write('HOST_TCP failure_evidence_saved=True\n')
    except (OSError, ValueError, TypeError):
        # This path is entered only after the original gate has already failed.
        log.write('HOST_TCP failure_evidence_saved=False\n')


def descendants(root):
    parents = {}
    for stat in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = stat.read_text().rsplit(')', 1)[1].split()
            parents[int(stat.parent.name)] = int(fields[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    result = {root}
    changed = True
    while changed:
        added = {pid for pid, parent in parents.items() if parent in result} - result
        changed = bool(added)
        result |= added
    return result - {root}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--driver', required=True, type=Path)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--run', required=True)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--root-chain', action='store_true',
                        help='exact Work root adapter; 540s chain budget and seven-resource observations')
    args = parser.parse_args()
    driver_timeout, term_grace = budgets(args.root_chain)
    adapter = root_adapter(args.driver) if args.root_chain else None
    if adapter is not None and args.run not in adapter.TARGETS:
        parser.error('root mode requires one exact Work root selector')
    args.output.mkdir(parents=True, exist_ok=True)
    stem = 'pg-' + uuid.uuid4().hex
    directory = args.output.resolve() / stem
    log_path = args.output / (stem + '.log')
    # Adopt only this supervisor's own descendants, so any unexpected survivor
    # can be actually waited and reported rather than inferred dead from ps.
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), 'PR_SET_CHILD_SUBREAPER')
    inputs = {str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest()
              for p in (args.driver, args.binary)}
    if adapter is not None:
        inputs = {str(p): adapter.sha(p) for p in adapter.input_paths(args.binary)}
    baseline_times = {'started_ns': time.monotonic_ns()}
    baseline = tcp()
    baseline_times['ended_ns'] = time.monotonic_ns()
    started = time.monotonic()
    child = None
    code = 1
    interrupted = False
    def stop(signum, frame):
        nonlocal interrupted
        interrupted = True
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
    old = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
    with log_path.open('w', buffering=1) as log:
        try:
            child = subprocess.Popen([str(args.driver.resolve()), '--test-binary',
                str(args.binary.resolve()), '--run', args.run, '--directory', str(directory)],
                stdout=log, stderr=subprocess.STDOUT)
            nonroot_reap_deadline = None
            try:
                code = child.wait(timeout=driver_timeout)
            except subprocess.TimeoutExpired:
                if not args.root_chain:
                    # Share the existing three-second retirement allowance
                    # between TERM, direct SIGKILL wait and adopted waits.
                    nonroot_reap_deadline = time.monotonic() + term_grace
                child.terminate()
                try:
                    code = child.wait(timeout=term_grace if args.root_chain else min(1, term_grace / 3))
                except subprocess.TimeoutExpired:
                    child.kill()
                    if args.root_chain:
                        try:
                            code = child.wait(timeout=3)
                        except subprocess.TimeoutExpired:
                            log.write('STOP root driver still not waited after bounded SIGKILL tail\n')
                    else:
                        try:
                            code = child.wait(timeout=max(0, nonroot_reap_deadline - time.monotonic()))
                        except subprocess.TimeoutExpired:
                            log.write('STOP driver still not waited within retirement deadline\n')
                code = 1
                log.write('STOP driver exceeded runtime budget\n')
            if args.root_chain:
                log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} actual={child.returncode is not None} code={code}\n')
            else:
                log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} actual={child.returncode is not None} actual_exit={child.returncode} code={code}\n')
            survivors = descendants(os.getpid())
            if survivors:
                code = 1
                log.write(f'STOP owned descendants survived driver: {sorted(survivors)}\n')
                for pid in survivors:
                    try: os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError: pass
            reap_deadline = (time.monotonic() + 5 if args.root_chain else
                             nonroot_reap_deadline if nonroot_reap_deadline is not None else
                             time.monotonic() + term_grace)
            while True:
                if not args.root_chain and child.returncode is None:
                    # Popen still owns the direct child. Do not steal its
                    # eventual status with generic waitpid and claim a join.
                    code = 1
                    log.write('STOP direct wait incomplete; adopted wait not claimed\n')
                    break
                try:
                    pid, status = os.waitpid(-1, os.WNOHANG)
                    if pid == 0:
                        if time.monotonic() >= reap_deadline:
                            code = 1
                            kind = 'root ' if args.root_chain else ''
                            log.write(f'STOP owned {kind}descendants not joined within bounded reap tail\n')
                            break
                        time.sleep(.02)
                        continue
                    if args.root_chain and pid == child.pid:
                        child.returncode = os.waitstatus_to_exitcode(status)
                    log.write(f'SUPERVISOR adopted_actual_wait pid={pid} status={status}\n')
                except ChildProcessError:
                    break
            for round in (1, 2):
                remaining = descendants(os.getpid())
                log.write(f'OWNED runtime_observation={round} descendants={sorted(remaining)}\n')
                if remaining: code = 1
            if args.root_chain and not observe_root_chain(directory, log, log_path, args.run):
                code = 1
            # The tail is a host delta, not an assertion that every short
            # connection in this shared host was owned by this invocation.
            tail_deadline = time.monotonic() + 75
            empty = 0
            tcp_samples = deque(maxlen=2)
            while time.monotonic() < tail_deadline and empty < 2:
                sampled_at = time.monotonic_ns()
                rows = tcp()
                delta = rows - baseline
                sample = {'started_ns': sampled_at, 'ended_ns': time.monotonic_ns(),
                          'rows': rows, 'delta': delta}
                tcp_samples.append(sample)
                if not delta:
                    empty += 1
                    log.write(f'HOST_TCP delta_empty_observation={empty}\n')
                else:
                    empty = 0
                if empty < 2: tcp_evidence_pause(sample, tail_deadline)
            if empty != 2:
                code = 1
                sampled_at = time.monotonic_ns()
                rows = tcp()
                failure_delta = rows - baseline
                reread = {'started_ns': sampled_at, 'ended_ns': time.monotonic_ns(),
                          'rows': rows, 'delta': failure_delta}
                log.write(f'STOP host TCP delta tail not empty: {len(failure_delta)} rows\n')
                save_tcp_failure(log_path, args.run, baseline, baseline_times, tcp_samples, reread, log)
            same = all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest
                       for p, digest in inputs.items())
            if not same: code = 1
            if interrupted: code = 1
            log.write(f'SUPERVISOR inputs_unchanged={same} terminal={code} elapsed={time.monotonic()-started:.3f}s\n')
        finally:
            for s, handler in old.items(): signal.signal(s, handler)
    print(json.dumps({'selector': args.run, 'exit': code, 'log': str(log_path),
                      'owned_directory': str(directory), 'actual_driver_wait': child is not None and child.returncode is not None}))
    return code


if __name__ == '__main__':
    sys.exit(main())
