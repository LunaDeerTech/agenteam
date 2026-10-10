#!/usr/bin/env python3
"""Exact CLI entry controls; external processes, proc and TCP are never used."""
import ast
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
BASE = '545d2b33bb4c3d2a57d99f24a2b3119b014ec3d2'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = '.agent-state/work-owner-http/native_driver.go'
SELECTOR = '^Test(CLIScopeAndSafeFailures|RunnerRealSIGTERMAndSIGINT|RunnerAndNeutralDependencyBoundaries)$'
TOPS = ('TestCLIScopeAndSafeFailures', 'TestRunnerRealSIGTERMAndSIGINT', 'TestRunnerAndNeutralDependencyBoundaries')

def original(path):
    return subprocess.check_output(['git', 'show', BASE + ':' + path], cwd=ROOT, text=True)

source = (ROOT / SUP).read_text()
a = source.index('\nRUNNER_CLI_SELECTOR = ')
b = source.index('\ndef main():', a)
inverse = source[:a] + source[b:]
input_addition = '''    if not args.root_chain and args.run == RUNNER_CLI_SELECTOR:
        inputs = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in runner_cli_inputs(args.binary, args.driver)}
'''
observe_addition = '''            if not args.root_chain and args.run == RUNNER_CLI_SELECTOR:
                if not observe_runner_cli(directory, log, log_path):
                    code = 1
'''
assert inverse.count(input_addition) == inverse.count(observe_addition) == 1
assert inverse.replace(input_addition, '').replace(observe_addition, '') == original(SUP)
driver = (ROOT / DRIVER).read_text()
inverse = driver.replace('const runnerCLISelector = "' + SELECTOR + '"\n\n', '')
inverse = inverse.replace(' && *selector != runnerCLISelector', '')
inverse = inverse.replace('''	if *selector == runnerCLISelector {
		// The existing process TestMain resolves its repository from ../.. and
		// builds the default commands; the supervisor fixes this repository cwd.
		cmd.Dir = filepath.Join("tests", "process")
	}
''', '')
inverse = inverse.replace(' && (*selector != runnerCLISelector || key != "GOTMPDIR")', '')
inverse = inverse.replace('''	if *selector == runnerCLISelector {
		cmd.Env = append(cmd.Env, "GOTMPDIR="+tmp)
	}
''', '')
assert inverse == original(DRIVER)
assert driver.count('"' + SELECTOR + '"') == 1
assert '"-test.timeout=90s"' in driver and '105*time.Second' in driver
scope = {'__file__': str(ROOT / SUP), '__name__': 'runner_cli_controls'}
exec(compile(source, SUP, 'exec'), scope)
assert scope['budgets'](False) == (123, 3) and scope['budgets'](True) == (540, 60)
# All existing Default dynamic inputs remain unchanged. The CLI only adds its
# actual compiled adapter and source; the original list carries cmd/embed/Go.
adapter = scope['root_adapter'](ROOT / '.agent-state/work-owner-http/root_chain_driver.py')
inputs = scope['runner_cli_inputs'](ROOT / 'candidate', ROOT / 'native-adapter')
assert set(inputs) == set(adapter.input_paths(ROOT / 'candidate')) | {ROOT / 'native-adapter', ROOT / DRIVER}
assert all(ROOT / p in inputs for p in ('go.mod', 'go.sum', 'cmd/agenteam/main.go', 'cmd/agenteam-runner/main.go', 'db/migrations/00026_runner_control.sql'))
checks = 0
with tempfile.TemporaryDirectory(prefix='runner-cli-controls-') as name:
    root = Path(name)
    (root / 'tests/process').mkdir(parents=True)
    scope['__file__'] = str(root / SUP)
    directory = root / 'owned'; directory.mkdir()
    path = root / 'test.log'
    record = directory / 'owned.json'
    record.write_text(json.dumps({'kind': 'work-http-native', 'child_pid': 123}))
    record.chmod(0o600)
    good = ''.join('=== RUN   ' + t + '\n--- PASS: ' + t + ' (0.01s)\n' for t in TOPS)
    good += 'CHILD pid=123 selector=' + SELECTOR + ' kind=native-http\nCHILD actual_wait pid=123 state=exit status 0\nNATIVE runtime_empty=true actual_child_wait=true\nDRIVER terminal exit=0 elapsed=0.123s child_started=true actual_child_wait=true private_removed=true\n'
    cases = [(good, True)]
    for t in TOPS:
        cases.extend([(good.replace('=== RUN   ' + t + '\n', ''), False),
                      (good.replace('--- PASS: ' + t, '--- SKIP: ' + t), False)])
    cases += [(good + '=== RUN   ' + TOPS[0] + '\n', False),
              (good + '--- PASS: ' + TOPS[0] + ' (0.01s)\n', False),
              (good + '=== RUN   TestForeign\n', False),
              (good.replace('actual_wait pid=123', 'actual_wait pid=124'), False),
              (good.replace('state=exit status 0', 'state=exit status 1'), False),
              (good.replace('runtime_empty=true', 'runtime_empty=false'), False),
              (good.replace('private_removed=true', 'private_removed=false'), False),
              (good.encode() + b'\xff', False)]
    for value, want in cases:
        path.write_bytes(value if isinstance(value, bytes) else value.encode())
        assert scope['observe_runner_cli'](directory, io.StringIO(), path) == want
        checks += 1
    path.unlink()
    assert not scope['observe_runner_cli'](directory, io.StringIO(), path); checks += 1
    path.write_text(good)
    for leftover in (directory / 'tmp', root / 'tests/process/.runner-process-owned'):
        leftover.mkdir()
        assert not scope['observe_runner_cli'](directory, io.StringIO(), path)
        leftover.rmdir(); checks += 1
    record.chmod(0o644)
    assert not scope['observe_runner_cli'](directory, io.StringIO(), path); checks += 1
    record.chmod(0o600)
    record.write_text(json.dumps({'kind': 'work-http-native', 'child_pid': True}))
    assert not scope['observe_runner_cli'](directory, io.StringIO(), path); checks += 1

    # Execute the actual supervisor main with effect doubles, including a
    # failed observer and changed source. The original Wait/reap/TCP/input tail
    # must still run and must retain failure.
    for mode in ('valid', 'bad-log', 'source-changed', 'driver-failed'):
        out = root / mode
        artifact = root / ('candidate-' + mode); artifact.write_text('frozen')
        native = root / ('driver-' + mode); native.write_text('frozen')
        dynamic = root / ('source-' + mode); dynamic.write_text('frozen')
        trace = {'wait': [], 'tcp': 0, 'desc': 0, 'reap': 0}
        class Child:
            pid = 456
            returncode = None
            def __init__(self, args, stdout, stderr):
                d = Path(args[-1]); d.mkdir()
                r = d / 'owned.json'; r.write_text(json.dumps({'kind': 'work-http-native', 'child_pid': 123})); r.chmod(0o600)
                stdout.write(good if mode != 'bad-log' else good.replace('--- PASS:', '--- SKIP:', 1))
                stdout.flush()
                if mode == 'source-changed': dynamic.write_text('changed')
            def wait(self, timeout):
                trace['wait'].append(timeout)
                self.returncode = 1 if mode == 'driver-failed' else 0
                return self.returncode
        def tcp():
            trace['tcp'] += 1; return set()
        def desc(_):
            trace['desc'] += 1; return set()
        def waitpid(*_):
            trace['reap'] += 1; raise ChildProcessError()
        fake_os = types.SimpleNamespace(getpid=lambda: 999, waitpid=waitpid, WNOHANG=1)
        argv = ['supervisor', '--driver', str(native), '--binary', str(artifact), '--run', SELECTOR, '--output', str(out)]
        replacements = {'subprocess': types.SimpleNamespace(Popen=Child, STDOUT=-2, TimeoutExpired=subprocess.TimeoutExpired),
                        'ctypes': types.SimpleNamespace(CDLL=lambda *a, **k: types.SimpleNamespace(prctl=lambda *a: 0)),
                        'signal': types.SimpleNamespace(SIGINT=2, SIGTERM=15, signal=lambda *a: None),
                        'os': fake_os, 'tcp': tcp, 'descendants': desc,
                        'time': types.SimpleNamespace(monotonic=lambda: 1, monotonic_ns=lambda: 1, sleep=lambda _: None),
                        'tcp_evidence_pause': lambda *a: None,
                        'runner_cli_inputs': lambda *a: [artifact, native, dynamic]}
        with patch.dict(scope, replacements), patch.object(sys, 'argv', argv), contextlib.redirect_stdout(io.StringIO()):
            code = scope['main']()
        assert code == (0 if mode == 'valid' else 1)
        assert trace == {'wait': [123], 'tcp': 3, 'desc': 3, 'reap': 1}, trace
        text = next(out.glob('*.log')).read_text()
        assert 'HOST_TCP delta_empty_observation=2' in text and 'SUPERVISOR inputs_unchanged=' in text
        checks += 1
print(f'PASS {checks} observer/main controls; exact old source inverse; original dynamic cmd inputs; budgets and complete tail unchanged; no resources started')
