#!/usr/bin/env python3
"""Independent CLI entry controls; native child, proc and TCP are substitutes."""
import argparse
import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import types
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path, default=Path(__file__).resolve().parents[3] / 'agenteam-runner-control-delivery')
parser.add_argument('--only', choices=('closure', 'addition', 'observer', 'all'), default='all')
args = parser.parse_args()
source_root = args.source.resolve()
own = Path(__file__).resolve().parents[2]
output = own / 'output/ai/runner-cli-entry-review'
output.mkdir(parents=True, exist_ok=True)
sup_path = source_root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
scope = {'__file__': str(sup_path), '__name__': 'independent_cli_entry'}
exec(compile(sup_path.read_text(), str(sup_path), 'exec'), scope)
selector = '^Test(CLIScopeAndSafeFailures|RunnerRealSIGTERMAndSIGINT|RunnerAndNeutralDependencyBoundaries)$'
tops = ('TestCLIScopeAndSafeFailures', 'TestRunnerRealSIGTERMAndSIGINT', 'TestRunnerAndNeutralDependencyBoundaries')
checks = 0


def check(value, label):
    global checks
    assert value, label
    checks += 1


if args.only in ('closure', 'all'):
    with patch.object(Path, 'cwd', return_value=source_root):
        inputs = set(scope['runner_cli_inputs'](source_root / 'candidate', source_root / 'native-driver'))
    package = set((source_root / 'tests/process').glob('*.go'))
    check(bool(package) and package <= inputs, 'missing process package inputs: ' + ','.join(str(p.relative_to(source_root)) for p in sorted(package - inputs)))


def valid_log(pid=123):
    text = ''.join(f'=== RUN   {top}\n--- PASS: {top} (0.01s)\n' for top in tops)
    return (text + f'CHILD pid={pid} selector={selector} kind=native-http\n'
            + f'CHILD actual_wait pid={pid} state=exit status 0\n'
            + 'NATIVE runtime_empty=true actual_child_wait=true\n'
            + 'DRIVER terminal exit=0 elapsed=0.123s child_started=true actual_child_wait=true private_removed=true\n')


with tempfile.TemporaryDirectory(dir=output) as temporary:
    root = Path(temporary)
    (root / 'tests/process').mkdir(parents=True)
    scope['__file__'] = str(root / '.agent-state/task-planning-recovery/pg_only_supervisor.py')
    if args.only in ('observer', 'all'):
        directory = root / 'observer'; directory.mkdir()
        record = directory / 'owned.json'
        manifest = {'kind': 'work-http-native', 'child_pid': 123}
        record.write_text(json.dumps(manifest)); record.chmod(0o600)
        log = root / 'test.log'
        valid = valid_log()
        variants = {
            'valid': valid,
            'same-count-foreign': valid.replace('=== RUN   ' + tops[0], '=== RUN   TestForeign'),
            'same-count-duplicate-pass': valid.replace('--- PASS: ' + tops[0], '--- PASS: ' + tops[1]),
            'wrong-original-selector': valid.replace('selector=' + selector, 'selector=^TestWrong$'),
            'two-starts': valid + f'CHILD pid=123 selector={selector} kind=native-http\n',
            'two-waits': valid + 'CHILD actual_wait pid=123 state=exit status 0\n',
            'two-terminals': valid + valid.splitlines(keepends=True)[-1],
            'no-original-child-wait': valid.replace('CHILD actual_wait pid=123 state=exit status 0\n', ''),
        }
        for name, text in variants.items():
            log.write_text(text)
            check(scope['observe_runner_cli'](directory, io.StringIO(), log) == (name == 'valid'), 'observer ' + name)
        log.write_text(valid)
        for broken in ({**manifest, 'child_pid': 124}, {**manifest, 'child_pid': 0}, {**manifest, 'child_pid': '123'}, {**manifest, 'extra': 'private-canary'}):
            record.write_text(json.dumps(broken))
            safe = io.StringIO()
            check(not scope['observe_runner_cli'](directory, safe, log), 'manifest identity rejects')
            check('private-canary' not in safe.getvalue(), 'manifest error safe projection')
        record.write_text(json.dumps(manifest))
        for link in (directory / 'tmp', root / 'tests/process/.runner-process-dangling'):
            link.symlink_to(root / 'absent')
            check(not scope['observe_runner_cli'](directory, io.StringIO(), log), 'dangling private link rejects')
            link.unlink()

    if args.only in ('addition', 'all'):
        artifact, native, first, added = (root / name for name in ('candidate', 'driver', 'old.go', 'new.go'))
        for p in (artifact, native, first, added):
            p.write_text('frozen input')
        for mode in ('unchanged', 'new-dynamic-source'):
            after_wait = False
            trace = {'wait': [], 'tcp': 0, 'desc': 0, 'reap': 0, 'input_calls': 0}

            def inputs(*unused):
                trace['input_calls'] += 1
                return [artifact, native, first] + ([added] if after_wait and mode == 'new-dynamic-source' else [])

            class Child:
                pid = 456
                returncode = None

                def __init__(self, argv, stdout, stderr):
                    run = Path(argv[-1]); run.mkdir()
                    record = run / 'owned.json'
                    record.write_text(json.dumps({'kind': 'work-http-native', 'child_pid': 123})); record.chmod(0o600)
                    stdout.write(valid_log()); stdout.flush()

                def wait(self, timeout):
                    global after_wait
                    trace['wait'].append(timeout)
                    after_wait = True
                    self.returncode = 0
                    return 0

            def tcp():
                trace['tcp'] += 1
                return set()

            def descendants(pid):
                trace['desc'] += 1
                return set()

            def reap(*unused):
                trace['reap'] += 1
                raise ChildProcessError()

            directory = root / mode
            argv = ['probe', '--driver', str(native), '--binary', str(artifact), '--run', selector, '--output', str(directory)]
            substitutions = {
                'subprocess': types.SimpleNamespace(Popen=Child, STDOUT=-2),
                'ctypes': types.SimpleNamespace(CDLL=lambda *a, **kw: types.SimpleNamespace(prctl=lambda *a: 0)),
                'signal': types.SimpleNamespace(SIGINT=2, SIGTERM=15, signal=lambda *a: None),
                'os': types.SimpleNamespace(getpid=lambda: 999, waitpid=reap, WNOHANG=1),
                'time': types.SimpleNamespace(monotonic=lambda: 1, monotonic_ns=lambda: 1, sleep=lambda _: None),
                'tcp': tcp, 'descendants': descendants, 'runner_cli_inputs': inputs,
                'tcp_evidence_pause': lambda *a: None,
            }
            with patch.dict(scope, substitutions), patch.object(sys, 'argv', argv), contextlib.redirect_stdout(io.StringIO()):
                code = scope['main']()
            text = next(directory.glob('*.log')).read_text()
            check(trace['wait'] == [123] and trace['tcp'] == 3 and trace['desc'] == 3 and trace['reap'] == 1, 'original Wait/desc/TCP tail')
            check('HOST_TCP delta_empty_observation=2' in text and 'SUPERVISOR inputs_unchanged=' in text, 'terminal input observation reached')
            print(mode, 'actual_exit', code, 'input_calls', trace['input_calls'])
            check((code == 0) == (mode == 'unchanged'), 'new dynamic source must fail original input gate')

print(f'{checks} independent CLI entry checks passed; processes/proc/TCP=controlled; no native resources')
