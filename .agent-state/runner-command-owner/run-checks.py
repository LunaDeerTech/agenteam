#!/usr/bin/python3
"""One authorized Command-owner race/vet run, or inert offline method checks.

No PG, network fixture, retry, global kill, or second supervisor. A failed
actual-Wait/owned-session tail means the resource window remains unreleased.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
OUTPUT = ROOT / 'output/ai/runner-command-owner'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
PREFIX = 'github.com/LunaDeerTech/agenteam/'
TESTS = {
    './internal/runner/command': (
        'TestCommandRequestAndDefaultRedaction',
        'TestCommandOwnerWaitsForOriginalReturnAndClose',
        'TestCommandOwnerStopBeforeStartAndCloseError',
        'TestCommandDirectExecKeepsArgvCwdAndMinimalEnvironment',
        'TestCommandOutputBoundsAndActualNonzeroExit',
        'TestCommandStartFailureAndSinkErrorKeepSafeFacts',
        'TestCommandCancellationWaitsForActualChildAndGroup',
        'TestCommandHeldSinkKeepsOwnerUntilActualReturn'),
    './internal/runner/filesystem': (
        'TestFilesystemCommandUsesContainedPinnedCWD',
        'TestFilesystemCommandRejectsInvalidAndEscapingCWD',
        'TestFilesystemCommandRetainsAdmissionThroughOwnerAndLeaseClose',
        'TestFilesystemCommandConstructorFailureJoinsBeforeLeaseClose'),
    './internal/runner/workspace': (
        'TestHandleCommandUsesActualPinnedWorkspace',
        'TestHandleCommandStopJoinsOriginalReturnBeforeRetiring'),
}
HELPERS = {
    './internal/runner/command': 'TestCommandProcessHelper',
    './internal/runner/filesystem': 'TestFilesystemCommandCWDChild',
    './internal/runner/workspace': 'TestWorkspaceCommandChild',
}
ENV = dict(GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off',
           GOSUMDB='off', GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=1',
           GOMAXPROCS='1', CGO_ENABLED='1', PYTHONDONTWRITEBYTECODE='1',
           GOCACHE='/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build',
           GOMODCACHE='/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod')
FIELDS = ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles',
          'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles', 'EmbedFiles',
          'TestGoFiles', 'XTestGoFiles', 'TestEmbedFiles', 'XTestEmbedFiles')
ACTIVE_STAGES = {}


def save(out, name, value):
    with (out / name).open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def sha(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def snapshot(paths):
    return {str(path): sha(path) for path in sorted(paths)}


def parse_stat(text):
    # comm may contain spaces or ')'; numeric fields begin after the last ')'.
    left, right = text.index('('), text.rindex(')')
    pid = int(text[:left].strip())
    fields = text[right + 1:].split()
    return {'pid': pid, 'comm': text[left + 1:right], 'state': fields[0],
            'ppid': int(fields[1]), 'pgrp': int(fields[2]),
            'session': int(fields[3]), 'starttime': int(fields[19])}


def identity(pid):
    return parse_stat(Path('/proc', str(pid), 'stat').read_text())


def session_members(session):
    result, unreadable = [], []
    for entry in Path('/proc').iterdir():
        if not entry.name.isdecimal():
            continue
        try:
            item = parse_stat((entry / 'stat').read_text())
        except (FileNotFoundError, ProcessLookupError):
            continue
        except (OSError, ValueError, IndexError) as exc:
            unreadable.append({'pid': int(entry.name), 'error': type(exc).__name__})
            continue
        if item['session'] == session:
            result.append(item)
    return sorted(result, key=lambda item: item['pid']), unreadable


def signal_members(members, session, sig):
    """Signal only a verified member through its stable pidfd, never a PID kill."""
    attempts = []
    for original in members:
        fd = None
        result = {'identity': original, 'signal': int(sig)}
        try:
            fd = os.pidfd_open(original['pid'])
            current = identity(original['pid'])
            if (current['session'] != session or
                    current['starttime'] != original['starttime']):
                result['sent'] = False
                result['reason'] = 'identity changed'
            else:
                signal.pidfd_send_signal(fd, sig)
                result['sent'] = True
        except (ProcessLookupError, FileNotFoundError):
            result['sent'] = False
            result['reason'] = 'original process absent'
        except OSError as exc:
            result['sent'] = False
            result['error'] = type(exc).__name__
            result['errno'] = exc.errno
        finally:
            if fd is not None:
                os.close(fd)
        attempts.append(result)
    return attempts


class Unreleased(RuntimeError):
    pass


def run(out, stage, command, env, budget):
    command = [str(value) for value in command]
    save(out, stage + '-invocation.json', {'command': command, 'cwd': str(ROOT),
         'outer_budget_seconds': budget, 'new_session': True})
    started = time.monotonic()
    events = (out / (stage + '-ownership.jsonl')).open('x')
    seen, observation_errors = {}, []

    def observe(session, label):
        members, unreadable = session_members(session)
        for item in members:
            seen[(item['pid'], item['starttime'])] = item
        if unreadable:
            observation_errors.extend(unreadable)
        events.write(json.dumps({'elapsed': round(time.monotonic() - started, 4),
                     'phase': label, 'members': members, 'unreadable': unreadable}) + '\n')
        events.flush()
        return members, unreadable

    with (out / (stage + '.log')).open('xb') as log:
        child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log,
                                 stderr=subprocess.STDOUT, start_new_session=True)
        ACTIVE_STAGES[child.pid] = stage
        save(out, stage + '-process.json', {'pid': child.pid, 'session': child.pid,
                                           'wrapper_pid': os.getpid()})
        # Popen's child is a session leader. Its original identity is captured
        # before our first Wait; even very short exits remain unreaped here.
        original = identity(child.pid)
        if original['session'] != child.pid:
            raise Unreleased('new session identity not established')
        save(out, stage + '-identity.json', original)
        print(json.dumps({'stage': stage, 'pid': child.pid, 'session': child.pid}), flush=True)
        code, timed_out, cleanup = None, False, []
        while code is None and time.monotonic() - started < budget:
            observe(child.pid, 'running')
            try:
                code = child.wait(timeout=0.025)
            except subprocess.TimeoutExpired:
                pass
        if code is None:
            timed_out = True
            # Failure cleanup is bounded and cannot turn the stage into PASS.
            # All known fixture subprocesses remain in this private session;
            # moving process group does not bypass membership observation.
            for sig in (signal.SIGTERM, signal.SIGKILL):
                members, _ = observe(child.pid, 'timeout-before-signal')
                cleanup.extend(signal_members(members, child.pid, sig))
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline:
                    if code is None:
                        try:
                            code = child.wait(timeout=0.025)
                        except subprocess.TimeoutExpired:
                            pass
                    members, unreadable = observe(child.pid, 'timeout-after-signal')
                    if code is not None and not members and not unreadable:
                        break
                    time.sleep(0.025)
        # Actual leader Wait and two independently read empty session tails
        # are separate facts. Reparented descendants keep the same SID.
        tails = []
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            members, unreadable = observe(child.pid, 'tail')
            tails.append({'members': members, 'unreadable': unreadable})
            if (len(tails) >= 2 and
                    all(not row['members'] and not row['unreadable'] for row in tails[-2:])):
                break
            time.sleep(0.1)
        log.flush()
        os.fsync(log.fileno())
    events.close()
    released = (code is not None and not observation_errors and len(tails) >= 2 and
                all(not row['members'] and not row['unreadable'] for row in tails[-2:]))
    result = {'pid': child.pid, 'actual_wait': code is not None,
              'actual_wait_exit': code, 'timed_out': timed_out,
              'elapsed_seconds': round(time.monotonic() - started, 3),
              'owned_session': child.pid, 'seen_identities': list(seen.values()),
              'unreadable_observations': observation_errors,
              'final_two_observations': tails[-2:], 'failure_signals': cleanup,
              'resource_window_released': released}
    result['stage_pass'] = released and code == 0 and not timed_out and not observation_errors
    save(out, stage + '-result.json', result)
    print(json.dumps({'stage': stage, 'actual_wait_exit': code,
                     'resource_window_released': released, 'stage_pass': result['stage_pass']}), flush=True)
    if not released:
        raise Unreleased(stage + ': actual Wait or owned session not retired')
    del ACTIVE_STAGES[child.pid]
    return 0 if result['stage_pass'] else 1


def json_values(path):
    text, decoder, offset = path.read_text(), json.JSONDecoder(), 0
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        value, offset = decoder.raw_decode(text, offset)
        yield value


def discover(out, when, env):
    imports = {PREFIX + package[2:] for package in TESTS}
    paths = {ROOT / 'go.mod', ROOT / 'go.sum', GO, Path(__file__).resolve()}
    declared, generated = set(), set()
    for mode, flags in (('race', ['-race']), ('plain', [])):
        label = 'deps-' + when + '-' + mode
        code = run(out, label, [GO, 'list', *flags, '-deps', '-test', '-json', *TESTS], env, 120)
        if code:
            return code, paths, declared, generated
        for row in json_values(out / (label + '.log')):
            if row.get('Name') == 'main' and row['ImportPath'].endswith('.test'):
                generated.add(row['ImportPath'])
                continue
            directory = Path(row['Dir'])
            for field in FIELDS:
                if field.startswith(('Test', 'XTest')) and row['ImportPath'] not in imports:
                    continue
                for name in row.get(field, []):
                    path = directory / name
                    paths.add(path)
                    if field in ('TestGoFiles', 'XTestGoFiles') and row['ImportPath'] in imports:
                        declared.update((row['ImportPath'], test) for test in
                                        re.findall(r'^func (Test[A-Za-z0-9_]+)\(', path.read_text(), re.M))
            if row.get('Module', {}).get('GoMod'):
                paths.add(Path(row['Module']['GoMod']))
    return 0, paths, declared, generated


def expected_tests():
    behavior = {(PREFIX + package[2:], name) for package, names in TESTS.items() for name in names}
    helpers = {(PREFIX + package[2:], name) for package, name in HELPERS.items()}
    return behavior, helpers


def selection(events):
    behavior, helpers = expected_tests()
    run_top, pass_top, skipped, passed_sub, failures = [], [], [], [], []
    for event in events:
        if event.get('Action') == 'fail':
            failures.append(event)
        if not event.get('Test'):
            continue
        pair = (event['Package'], event['Test'])
        if event['Action'] == 'skip':
            skipped.append(pair)
        if '/' in pair[1]:
            if event['Action'] == 'pass':
                passed_sub.append(pair)
            continue
        if event['Action'] == 'run':
            run_top.append(pair)
        if event['Action'] == 'pass':
            pass_top.append(pair)
    exact = (not failures and set(run_top) == behavior | helpers and
             len(run_top) == len(behavior | helpers) and set(pass_top) == behavior and
             len(pass_top) == len(behavior) and set(skipped) == helpers and
             len(skipped) == len(helpers))
    return {'exact': exact, 'behavior_pass_count': len(pass_top),
            'subtest_pass_count': len(passed_sub), 'helper_skip_count': len(skipped),
            'passed_behavior': sorted(pass_top), 'passed_subtests': sorted(passed_sub),
            'skipped': sorted(skipped), 'failures': failures}


def checks(out):
    if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
        raise ValueError('stable owned-process cleanup requires Python pidfd support')
    tmp = out / 'tmp'
    tmp.mkdir(mode=0o700)
    overrides = dict(ENV, TMPDIR=str(tmp), GOTMPDIR=str(tmp))
    env = dict(os.environ, **overrides)
    save(out, 'invocation.json', {'packages': list(TESTS), 'environment': overrides,
         'source_head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
         'script_sha256': sha(__file__), 'wrapper_pid': os.getpid(),
         'scope': '14 new behavior tops + 3 default-Skip helpers; three-package vet; no PG/network'})
    code, paths, declared, generated = discover(out, 'before', env)
    if code:
        return code
    behavior, helpers = expected_tests()
    if not (behavior | helpers) <= declared:
        raise ValueError('exact test declarations are missing')
    before = snapshot(paths)
    save(out, 'inputs-before.json', before)
    save(out, 'selection.json', {'behavior': sorted(behavior), 'helpers_default_skip': sorted(helpers),
         'generated_test_mains_excluded': sorted(generated),
         'scope': 'actual race/plain go-list union, package test sources, embeds, modules, fixed Go and method'})
    selector = '^(' + '|'.join(sorted(name for _, name in behavior | helpers)) + ')$'
    code = run(out, 'unit', [GO, 'test', '-race', '-count=1', '-p=1', '-timeout=60s',
                           '-json', '-run', selector, *TESTS], env, 300)
    events = []
    for line in (out / 'unit.log').read_text().splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass  # Original compiler diagnostics remain unmodified in unit.log.
    selected = selection(events)
    save(out, 'unit-selection.json', selected)
    after_unit = snapshot(paths)
    save(out, 'inputs-after-unit.json', after_unit)
    code = int(code or not selected['exact'] or before != after_unit)
    if not code:
        code = run(out, 'vet', [GO, 'vet', '-p=1', *TESTS], env, 180)
    # Re-discover actual file sets to detect added/removed relevant inputs.
    listed, after_paths, _, _ = discover(out, 'after', env)
    after = snapshot(after_paths)
    save(out, 'inputs-after.json', after)
    unchanged = listed == 0 and before == after
    code = int(code or listed or not unchanged)
    save(out, 'result.json', {'actual_checks_exit': code, 'inputs_unchanged': unchanged,
         'input_count_before': len(before), 'input_count_after': len(after),
         'behavior_top_count': 14, 'helpers_are_default_skip_not_pass': 3,
         'resource_window_released': True})
    return code


def self_check(out):
    """In-memory parser controls only: no subprocess, proc read, Go or service."""
    behavior, helpers = expected_tests()
    assert len(behavior) == 14 and len(helpers) == 3
    def event(action, pair):
        return {'Action': action, 'Package': pair[0], 'Test': pair[1]}
    positive = [event('run', pair) for pair in sorted(behavior | helpers)]
    positive += [event('pass', pair) for pair in sorted(behavior)]
    positive += [event('skip', pair) for pair in sorted(helpers)]
    assert selection(positive)['exact']
    helper, test = min(helpers), min(behavior)
    cases = [
        [e for e in positive if not (e['Action'] == 'pass' and (e['Package'], e['Test']) == test)],
        positive + [event('pass', helper)],
        [e for e in positive if not (e['Action'] == 'skip' and (e['Package'], e['Test']) == helper)],
        positive + [event('skip', (test[0], test[1] + '/unexpected-skip'))],
        positive + [event('fail', test)],
        positive + [event('run', (test[0], 'TestUnexpected')), event('pass', (test[0], 'TestUnexpected'))],
        positive + [event('pass', test)],
    ]
    assert all(not selection(case)['exact'] for case in cases)
    sample = '123 (name ) with space) ' + ' '.join(['S', '1', '123', '123'] + ['0'] * 15 + ['456'])
    stat = parse_stat(sample)
    assert stat['pid'] == 123 and stat['session'] == 123 and stat['starttime'] == 456
    save(out, 'controls.json', {'actual_exit': 0, 'selection_positive': 1,
         'selection_negative': 7, 'stat_parser': 1,
         'scope': 'pure parsing only; not Go, child lifecycle, signal or resource verification'})
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('basic', 'self-check'))
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    out = args.output if args.output.is_absolute() else ROOT / args.output
    if out.resolve() != out or not out.is_relative_to(OUTPUT):
        parser.error('fresh canonical task-owned output required')
    out.mkdir(parents=True, exist_ok=False)
    started, released = time.monotonic(), True
    try:
        if args.action == 'self-check':
            inputs = {Path(__file__).resolve(), Path(sys.executable).resolve()}
            before = snapshot(inputs)
            save(out, 'inputs-before.json', before)
            code = self_check(out)
            after = snapshot(inputs)
            save(out, 'inputs-after.json', after)
            code = int(code or before != after)
        else:
            code = checks(out)
    except Unreleased as exc:
        released, code = False, 1
        save(out, 'exception.json', {'type': type(exc).__name__, 'message': str(exc),
                                    'resource_window_released': False})
    except Exception as exc:
        code = 1
        # An unexpected exception during a started phase cannot prove retirement.
        unfinished = [p.name for p in out.glob('*-process.json')
                      if not p.with_name(p.name.replace('-process.json', '-result.json')).exists()]
        released = not unfinished and not ACTIVE_STAGES
        save(out, 'exception.json', {'type': type(exc).__name__, 'message': str(exc),
                                    'unfinished_stages': unfinished, 'active_stages': ACTIVE_STAGES,
                                    'resource_window_released': released})
    save(out, 'outer-terminal.json', {'wrapper_pid': os.getpid(), 'exit_code': code,
         'elapsed_seconds': round(time.monotonic() - started, 3),
         'resource_window_released': released,
         'source': 'wrapper return; outer tool actual Wait must be recorded separately'})
    print(json.dumps({'exit_code': code, 'resource_window_released': released}), flush=True)
    return code


if __name__ == '__main__':
    raise SystemExit(main())
