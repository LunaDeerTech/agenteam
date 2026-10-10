#!/usr/bin/env python3
"""Independent offline entry checks. All child/proc/TCP/artifacts are substitutes."""
import argparse
import contextlib
import importlib.util
import io
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path, default=Path(__file__).resolve().parents[3] / 'agenteam-secret-variable-owner-service')
args = parser.parse_args()
root = args.source.resolve()
own = Path(__file__).resolve().parents[2]
output = own / 'output/ai/secret-variable-owner-review/entry'
output.mkdir(parents=True, exist_ok=True)
source = root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('independent_secret_owner_entry', source)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
count = 0


def check(ok, message):
    global count
    assert ok, message
    count += 1


ddl = root / 'db/migrations/00030_project_secret_variables.sql'
draft = subprocess.check_output(['git', 'show', 'eaf209f5:.agent-state/secret-variable-owner-service/owner-storage.draft.sql'], cwd=root)
installed = ddl.read_bytes()
check(installed.splitlines(keepends=True)[3:] == draft.splitlines(keepends=True)[3:], 'DDL body exactly the reviewed draft')
check(installed.splitlines()[:2] == [b'-- agenteam:transaction tx', b'-- +goose Up'], 'production transactional Up header')
check(list((root / 'db/migrations').glob('00030*')) == [ddl], 'one installed owner for migration 30')

with tempfile.TemporaryDirectory(dir=output) as temp:
    log_path = Path(temp) / 'observer.log'
    for selector, names in sup.SECRET_OWNER_CASES.items():
        names = sorted(names)
        subcase = next(n for n in names if '/' in n)
        valid = ''.join(f'=== RUN   {n}\n' for n in reversed(names))
        valid += ''.join(f'    --- PASS: {n} (0.01s)\n' for n in names)
        variants = {
            'order-independent': valid,
            'same-count-wrong-run': valid.replace('=== RUN   ' + subcase, '=== RUN   TestUnrelated', 1),
            'same-count-duplicate-pass': valid.replace('--- PASS: ' + subcase, '--- PASS: ' + names[0], 1),
            'wrong-nested-parent': valid.replace(subcase, 'TestWrong/' + subcase.split('/', 1)[1]),
            'late-fail-after-passes': valid + f'--- FAIL: {names[0]} (0.1s)\n',
            'additional-skip': valid + '--- SKIP: TestExtra (0.1s)\n',
            'pass-without-run': valid.replace('=== RUN   ' + subcase + '\n', '', 1),
        }
        for name, text in variants.items():
            log_path.write_text(text)
            safe = io.StringIO()
            result = sup.observe_secret_owner(log_path, safe, selector)
            check(result == (name == 'order-independent'), 'observer ' + name)
        for fault in (OSError('private-canary'), UnicodeDecodeError('utf8', b'\xff', 0, 1, 'private-canary')):
            safe = io.StringIO()
            with patch.object(Path, 'read_text', side_effect=fault):
                check(not sup.observe_secret_owner(log_path, safe, selector), 'unreadable fails closed')
            check('private-canary' not in safe.getvalue() and 'log_unreadable=True' in safe.getvalue(), 'safe read-error projection')

artifact_root = root / 'output/ai/secret-variable-owner-service'
driver, binary = artifact_root / 'pg-only-owner-driver', artifact_root / 'secret-variable-owner.test'
artifacts = {driver, binary}
real_read, real_stat, real_resolve, real_glob = Path.read_bytes, Path.stat, Path.resolve, Path.glob
victim = root / 'tests/projectvariable/secret_recovery_test.go'
extra = root / 'tests/projectvariable/entry_review_added.go'
after_wait = False
case = ''


def resolve(path, *a, **kw):
    if after_wait and case == 'late-symlink' and path == victim:
        return victim.with_name('other.go')
    return path if path in artifacts or path == extra else real_resolve(path, *a, **kw)


def file_stat(path, *a, **kw):
    if path in artifacts or path == extra:
        return type('RegularInput', (), {'st_mode': stat.S_IFREG | 0o600})()
    return real_stat(path, *a, **kw)


def read(path):
    if path in artifacts or path == extra:
        return b'explicit controlled input'
    return real_read(path)


def glob(path, pattern):
    found = list(real_glob(path, pattern))
    if after_wait and case == 'late-added-source' and path == victim.parent and pattern == '*.go':
        found.append(extra)
    return iter(found)


selector = '^TestSecretVariableOwnerCommitRecovery$'
names = sup.SECRET_OWNER_CASES[selector]
valid = ''.join(f'=== RUN   {n}\n--- PASS: {n} (0.01s)\n' for n in sorted(names))
with tempfile.TemporaryDirectory(dir=output) as temp, patch.object(Path, 'resolve', resolve), patch.object(Path, 'stat', file_stat), patch.object(Path, 'read_bytes', read), patch.object(Path, 'glob', glob):
    for case in ('pass', 'late-added-source', 'late-symlink', 'nonzero-driver', 'wait-not-owned', 'surviving-descendant', 'tcp-not-empty'):
        after_wait = False
        kills, waits = [], []
        directory = Path(temp) / case
        ticks = [0]
        tcp_calls = [0]
        descendant_calls = [0]

        def monotonic():
            ticks[0] += 1
            return ticks[0]

        def tcp():
            tcp_calls[0] += 1
            return {'safe-owned-tuple'} if case == 'tcp-not-empty' and tcp_calls[0] > 1 else set()

        def descendants(pid):
            descendant_calls[0] += 1
            return {444444} if case == 'surviving-descendant' and descendant_calls[0] == 1 else set()

        class Child:
            pid = 333333
            returncode = None

            def __init__(self, argv, stdout, stderr):
                stdout.write(valid)

            def wait(self, timeout):
                global after_wait
                waits.append(timeout)
                after_wait = True
                if case != 'wait-not-owned':
                    self.returncode = 7 if case == 'nonzero-driver' else 0
                return 7 if case == 'nonzero-driver' else 0

        argv = ['probe', '--driver', str(driver), '--binary', str(binary), '--run', selector, '--output', str(directory)]
        library = type('SubreaperSubstitute', (), {'prctl': staticmethod(lambda *a: 0)})()
        with patch.object(sys, 'argv', argv), patch.object(sup.ctypes, 'CDLL', return_value=library), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup.signal, 'signal'), patch.object(sup.os, 'waitpid', side_effect=ChildProcessError), patch.object(sup.os, 'kill', side_effect=lambda pid, sig: kills.append((pid, sig))), patch.object(sup, 'descendants', descendants), patch.object(sup, 'tcp', tcp), patch.object(sup.time, 'monotonic', monotonic), patch.object(sup.time, 'sleep'), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        log = next(directory.glob('*.log')).read_text()
        check((code == 0) == (case == 'pass'), 'controlled main sticky failure ' + case)
        check(waits == [123], 'original direct Wait budget ' + case)
        check('runtime_observation=2 descendants=[]' in log and 'inputs_unchanged=' in log and 'terminal=' in log, 'original tail reached ' + case)
        check(('HOST_TCP delta_empty_observation=2' in log) == (case != 'tcp-not-empty'), 'TCP condition remains required ' + case)
        if case == 'surviving-descendant':
            check(len(kills) == 1 and kills[0][0] == 444444, 'only original owned descendant killed')
        if case.startswith('late-'):
            check('inputs_unchanged=False' in log, 'late manifest binding failure')

print(f'{count} independent checks passed; children/proc/TCP/artifacts=controlled; no PG/socket/Go')
