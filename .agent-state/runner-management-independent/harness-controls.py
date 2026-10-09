#!/usr/bin/env python3
"""Closed Runner management selector and unchanged PG tail; no real resources.

Optional --driver tests the separately authorized build with an absent parent,
so every invocation stops at input validation/Statfs before mkdir or Docker.
"""
import argparse
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DRIVER_SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go'
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BINARY = ROOT / 'output/ai/runner-control-independent/management.test'
SELECTOR = '^TestIndependentRunnerManagement(Concurrent|LogoutOrder|CommitUnknown)$'
TOPS = ['TestIndependentRunnerManagementConcurrent',
        'TestIndependentRunnerManagementLogoutOrder',
        'TestIndependentRunnerManagementCommitUnknown']
BASE = '1ce69576e497297c6dd77d21781738627ea84562'
spec = importlib.util.spec_from_file_location('runner_management_supervisor', SUPERVISOR)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
parser = argparse.ArgumentParser()
parser.add_argument('--driver', type=Path)
args = parser.parse_args()
checks = []


def check(label, condition):
    assert condition, label
    checks.append(label)


def lines(tops):
    return ''.join('=== RUN   ' + top + '\n' for top in tops).encode()


def baseline(path):
    return subprocess.check_output(
        ['git', 'show', BASE + ':' + str(path.relative_to(ROOT))], cwd=ROOT).decode()


# The driver and all original timeout/resource/Wait code are byte unchanged
# after removing precisely the fixed literal acceptance and observer addition.
driver_source = DRIVER_SOURCE.read_text().replace(
    'const independentRunnerManagementSelector = "' + SELECTOR + '"\n\n', '', 1)
driver_source = driver_source.replace(
    '(!regexp.MustCompile(`^\\^Test[A-Za-z0-9]+\\$$`).MatchString(*selector) && *selector != independentRunnerManagementSelector)',
    '!regexp.MustCompile(`^\\^Test[A-Za-z0-9]+\\$$`).MatchString(*selector)', 1)
check('driver inverse byte equal', driver_source == baseline(DRIVER_SOURCE))
supervisor_source = SUPERVISOR.read_text()
start = supervisor_source.index('RUNNER_MANAGEMENT_SELECTOR = ')
end = supervisor_source.index('def budgets(root_chain):', start)
supervisor_source = supervisor_source[:start] + supervisor_source[end:]
supervisor_source = supervisor_source.replace(
    '            if (not args.root_chain and args.run == RUNNER_MANAGEMENT_SELECTOR\n'
    '                    and not observe_runner_management(log, log_path)):\n'
    '                code = 1\n', '', 1)
check('supervisor inverse byte equal', supervisor_source == baseline(SUPERVISOR))
check('budgets unchanged', module.budgets(False) == (123, 3) and module.budgets(True) == (540, 60))
with BINARY.open('rb') as stream:
    binary_sha = hashlib.file_digest(stream, 'sha256').hexdigest()
check('fixed original candidate', BINARY.stat().st_size == 32284105
      and binary_sha == 'afdaa5dcd0b24d52c5efdbc27210dd33617d5b8fd055ca771e5e98d8251a3630')

output = ROOT / 'output/ai/runner-management-independent/harness-controls'
output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='offline-', dir=output) as temporary:
    directory = Path(temporary)
    matrices = [
        ('exact', lines(TOPS), True),
        ('reordered-with-child', lines(list(reversed(TOPS))) + lines([TOPS[0] + '/same_key']), True),
        ('empty', b'', False), ('missing', lines(TOPS[:-1]), False),
        ('extra', lines(TOPS + ['TestRunnerControlManagement']), False),
        ('duplicate', lines(TOPS + [TOPS[0]]), False),
        ('only-child', lines([TOPS[0] + '/same_key']), False),
        ('invalid-utf8', lines(TOPS) + b'\xff\n', False),
    ]
    path = directory / 'observed.log'
    for name, content, expected in matrices:
        path.write_bytes(content)
        check('actual observer ' + name,
              module.observe_runner_management(io.StringIO(), path) is expected)
    path.unlink()
    check('actual observer missing log',
          module.observe_runner_management(io.StringIO(), path) is False)

    # Actual supervisor main with only external process/TCP boundaries doubled.
    # Observer failure must still reach original descendant/TCP/input/final tail.
    fake_driver = directory / 'not-executed-driver'
    fake_driver.write_bytes(b'controlled child only\n')
    for name, content, selector, child_code, expected in [
        ('exact', lines(TOPS), SELECTOR, 0, 0),
        ('missing', lines(TOPS[:2]), SELECTOR, 0, 1),
        ('duplicate', lines(TOPS + [TOPS[0]]), SELECTOR, 0, 1),
        ('invalid-utf8', lines(TOPS) + b'\xff\n', SELECTOR, 0, 1),
        ('child-failed', lines(TOPS), SELECTOR, 1, 1),
        ('old-single-unchanged', b'\xff\n', '^TestIndependentRunnerManagementConcurrent$', 0, 0),
    ]:
        run_output = directory / name
        tcp_calls, wait_calls = [], []

        class Child:
            pid = 424242
            returncode = None

            def __init__(self, argv, stdout, stderr):
                check('exact argv ' + name, argv[:-1] == [str(fake_driver),
                      '--test-binary', str(BINARY), '--run', selector, '--directory'])
                stdout.flush()
                stdout.buffer.write(content)
                stdout.flush()

            def wait(self, timeout):
                wait_calls.append(timeout)
                self.returncode = child_code
                return child_code

        class Libc:
            def prctl(self, *args):
                return 0

        def tcp():
            tcp_calls.append(True)
            return set()

        argv = ['supervisor', '--driver', str(fake_driver), '--binary', str(BINARY),
                '--run', selector, '--output', str(run_output)]
        with patch.object(sys, 'argv', argv), \
             patch.object(module.ctypes, 'CDLL', return_value=Libc()), \
             patch.object(module.subprocess, 'Popen', Child), \
             patch.object(module, 'descendants', return_value=set()), \
             patch.object(module.os, 'waitpid', side_effect=ChildProcessError), \
             patch.object(module.os, 'kill', side_effect=AssertionError('no actual kill')), \
             patch.object(module, 'tcp', tcp), \
             patch.object(module.time, 'sleep', return_value=None), \
             contextlib.redirect_stdout(io.StringIO()):
            code = module.main()
        raw = next(run_output.glob('*.log')).read_bytes()
        check('actual main complete tail ' + name, code == expected and wait_calls == [123]
              and len(tcp_calls) == 3
              and f'actual=True actual_exit={child_code} code={child_code}'.encode() in raw
              and b'OWNED runtime_observation=1 descendants=[]' in raw
              and b'OWNED runtime_observation=2 descendants=[]' in raw
              and b'HOST_TCP delta_empty_observation=1' in raw
              and b'HOST_TCP delta_empty_observation=2' in raw
              and f'inputs_unchanged=True terminal={expected}'.encode() in raw)

    if args.driver is not None:
        driver = args.driver.resolve()
        for selector, accepted in (
            (SELECTOR, True), ('^TestIndependentRunnerManagementConcurrent$', True),
            ('^TestWorkHTTPNativeDeadlines$', True),
            ('^TestIndependentRunnerManagement.*$', False), (SELECTOR[:-1], False),
            ('^TestIndependentRunnerManagement(LogoutOrder|Concurrent|CommitUnknown)$', False),
            ('^TestIndependentRunnerManagement(Concurrent|LogoutOrder)$', False),
            (SELECTOR + '/same_key', False),
            ('^TestIndependentRunnerManagement(Concurrent|LogoutOrder|CommitUnknown|Extra)$', False),
        ):
            absent = directory / 'absent-parent' / 'run'
            result = subprocess.run([str(driver), '--test-binary', str(BINARY),
                                     '--run', selector, '--directory', str(absent)],
                                    capture_output=True, text=True, timeout=5)
            message = ('fresh disk below 5 GiB' if accepted else
                       'exact binary, directory and one anchored top are required')
            check('actual driver selector ' + selector,
                  result.returncode == 1 and result.stderr.strip() == message and not absent.parent.exists())

print(json.dumps({'passed': len(checks), 'actual_driver_checked': args.driver is not None,
                  'controls': checks}))
