#!/usr/bin/env python3
"""One exact D11 PG top, actual child wait, then bounded host TCP tail.

Example: python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
 --driver /absolute/pg-only-driver --binary /absolute/work.test \
 --run '^TestTaskPlanningMigration$' --output /absolute/existing/output-dir
The output directory is reusable; each run/nonce directory must be new.
"""
import argparse
import ctypes
import hashlib
import importlib.util
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
        '^TestProjectVariablesRootActualCallJoin$': {'TestProjectVariablesRootActualCallJoin'},
        '^TestProjectVariablesHTTPProcessRoutingAndPersistence$': {'TestProjectVariablesHTTPProcessRoutingAndPersistence'},
        '^TestIndependentProjectVariablesProcessConfirmationExit$': {'TestIndependentProjectVariablesProcessConfirmationExit'},
        '^TestIndependentProjectVariablesRootConfirmationForce$': {'TestIndependentProjectVariablesRootConfirmationForce'},
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



SKILL_HTTP_PG = '^TestSkillOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$'
SKILL_HTTP_NATIVE = '^TestSkillOwnerHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$'
SKILL_HTTP_CASES = {
    SKILL_HTTP_PG: {
        'TestSkillOwnerReadHTTPMetadata': ('same_current_directory_and_detail_get_head', 'strict_request_and_real_browser_boundary'),
        'TestSkillOwnerReadHTTPCurrentAuthority': ('foreign_owner_and_admin_no_bypass', 'project_gate_and_missing_publication_are_distinct', 'real_new_session_and_logout', 'current_owner_mapping_rechecked', 'archived_read_then_deleting_gate'),
        'TestSkillOwnerReadHTTPTransactions': ('reader_first', 'writer_first', 'actual_query_cancel_and_original_tx_join'),
        'TestSkillOwnerReadHTTPCommitUnknown': ('not_forwarded', 'committed_ack_lost'),
    },
    SKILL_HTTP_NATIVE: {
        'TestSkillOwnerHTTPNativeDeadlines': ('read-natural', 'earlier-parent'),
        'TestSkillOwnerHTTPNativeKeepAliveAndClose': ('cleared-deadline-keeps-real-connection', 'real-body-close-error-aborts-before-response'),
        'TestSkillOwnerHTTPNativeBackpressureAndDisconnect': ('summary-write-natural-deadline', 'disconnect-cancels-actual-library-tail'),
    },
}


def skill_http_inputs(driver, binary, selector):
    # Exact local test inputs plus the runtime Schema producer/JSON/interpreter.
    # The immutable precompiled binary represents its other build dependencies;
    # this is not a repository-wide hash or another build during supervision.
    root = Path(__file__).resolve().parents[2]
    paths = {driver.resolve(), binary.resolve(), Path(__file__).resolve(),
             root / 'go.mod', root / 'go.sum'}
    paths.update(root / 'internal/central/skill/http' / name for name in ('handler.go', 'wire.go', 'io.go', 'native_test.go'))
    paths.update((root / 'internal/central/skill/http').glob('*.go'))
    if selector == SKILL_HTTP_PG:
        paths.add(root / '.agent-state/task-planning-recovery/pg_only_driver.go')
        paths.update(root / 'tests/skills' / name for name in ('owner_http_fixture_test.go', 'owner_http_test.go', 'owner_http_transactions_test.go'))
        paths.update((root / 'tests/skills').glob('*.go'))
        paths.update((root / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
        paths.update((root / 'tests/testsupport/postgres').glob('*.go'))
        paths.update({root / 'internal/central/skill/http/testdata/schema.py',
                      root / 'api/openapi/skill-owner.json', root / 'api/openapi/common.json',
                      Path(sys.executable).resolve()})
    elif selector == SKILL_HTTP_NATIVE:
        paths.add(root / '.agent-state/work-owner-http/native_driver.go')
    else:
        raise ValueError('unknown Skill HTTP selector')
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def skill_http_spawn(driver, binary, selector, directory, log):
    environment = dict(os.environ)
    if selector == SKILL_HTTP_PG:
        environment['AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON'] = str(Path(sys.executable).resolve())
    return subprocess.Popen([str(driver.resolve()), '--test-binary', str(binary.resolve()),
                             '--run', selector, '--directory', str(directory)],
                            stdout=log, stderr=subprocess.STDOUT, env=environment)


def observe_skill_http(directory, log, log_path, selector):
    try:
        output = log_path.read_text()
        expected = {top for top in SKILL_HTTP_CASES[selector]}
        expected.update(top + '/' + sub for top, subs in SKILL_HTTP_CASES[selector].items() for sub in subs)
        runs = re.findall(r'^=== RUN   (Test[^\s]+)$', output, re.M)
        passes = re.findall(r'^\s*--- PASS: (Test[^\s]+) \([^\r\n]*\)$', output, re.M)
        good = (set(runs) == expected and len(runs) == len(expected)
                and set(passes) == expected and len(passes) == len(expected))
        started = re.findall(r'^CHILD pid=([1-9][0-9]*) selector=' + re.escape(selector)
                             + (r' kind=native-http' if selector == SKILL_HTTP_NATIVE else '') + r'$', output, re.M)
        waited = re.findall(r'^CHILD actual_wait pid=([1-9][0-9]*) state=exit status 0$', output, re.M)
        good = (good and len(started) == 1 and waited == started
                and len(re.findall(r'^CHILD pid=', output, re.M)) == 1
                and len(re.findall(r'^CHILD actual_wait ', output, re.M)) == 1
                and len(re.findall(r'^DRIVER terminal ', output, re.M)) == 1
                and re.search(r'^\s*--- (?:FAIL|SKIP): ', output, re.M) is None)
        manifest = directory / 'owned.json'
        if manifest.is_symlink() or manifest.stat().st_mode & 0o777 != 0o600 or manifest.stat().st_size > 16384:
            raise ValueError('invalid Skill HTTP ownership record')
        record = json.loads(manifest.read_bytes())
        if selector == SKILL_HTTP_PG:
            if (set(record) != {'nonce', 'network_id', 'container_id'}
                    or re.fullmatch('[0-9a-f]{32}', record['nonce']) is None
                    or any(re.fullmatch('[0-9a-f]{64}', record[k]) is None for k in ('network_id', 'container_id'))):
                raise ValueError('incomplete Skill HTTP PG identities')
            expected_retire = [(str(n), record['container_id'], record['network_id']) for n in (1, 2)]
            retired = re.findall(r'^RETIRE observation=([12]) exact_container=([0-9a-f]{64}) exact_network=([0-9a-f]{64}) clean=true$', output, re.M)
            owned = re.findall(r'^OWNED nonce=([0-9a-f]{32}) container=([0-9a-f]{64}) network=([0-9a-f]{64}) port=[0-9]+ PostgreSQL=[0-9]+ vector=0\.8\.1$', output, re.M)
            terminal = re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true cleanup=true$', output, re.M)
            good = good and len(re.findall(r'^RETIRE observation=', output, re.M)) == 2 and len(re.findall(r'^OWNED nonce=', output, re.M)) == 1 and retired == expected_retire and owned == [(record['nonce'], record['container_id'], record['network_id'])] and len(terminal) == 1
        else:
            if set(record) != {'kind', 'child_pid'} or record['kind'] != 'work-http-native' or type(record['child_pid']) is not int:
                raise ValueError('invalid Skill HTTP native identity')
            good = (good and started == [str(record['child_pid'])]
                    and len(re.findall(r'^NATIVE runtime_empty=true actual_child_wait=true$', output, re.M)) == 1
                    and len(re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true private_removed=true$', output, re.M)) == 1)
        good = good and not directory.is_symlink() and {p.name for p in directory.iterdir()} == {'owned.json'}
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        good = False
    log.write(f'SKILL_HTTP exact_cases_wait_private={good}\n')
    return good

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
    skill_selected = not args.root_chain and args.run in SKILL_HTTP_CASES
    if skill_selected:
        inputs = skill_http_inputs(args.driver, args.binary, args.run)
    baseline = tcp()
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
            if skill_selected:
                child = skill_http_spawn(args.driver, args.binary, args.run, directory, log)
            else:
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
            if skill_selected and not observe_skill_http(directory, log, log_path, args.run):
                code = 1
            # The tail is a host delta, not an assertion that every short
            # connection in this shared host was owned by this invocation.
            tail_deadline = time.monotonic() + 75
            empty = 0
            while time.monotonic() < tail_deadline and empty < 2:
                delta = tcp() - baseline
                if not delta:
                    empty += 1
                    log.write(f'HOST_TCP delta_empty_observation={empty}\n')
                else:
                    empty = 0
                if empty < 2: time.sleep(.1)
            if empty != 2:
                code = 1
                log.write(f'STOP host TCP delta tail not empty: {len(tcp() - baseline)} rows\n')
            if skill_selected:
                try:
                    same = skill_http_inputs(args.driver, args.binary, args.run) == inputs
                except (OSError, ValueError):
                    same = False
            else:
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
