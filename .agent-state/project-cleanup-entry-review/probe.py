#!/usr/bin/env python3
"""Independent entry-only checks. No Go, sockets, subprocess owners or PG."""
import ast
import contextlib
import importlib.util
import io
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

REPO = Path('/workspace/agenteam-project-skills-cleanup-independent')
BASE = 'feff9e0c'
REL = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
TOP = 'TestProjectSkillsCleanupIndependentRevalidation'
SELECTOR = '^' + TOP + '$'
CHILDREN = ('progress_revoked_and_restored_in_original_tx', 'owner_reread_after_prior_grant', 'cancelled_original_call_cannot_reuse_prior_grant')
source = (REPO / REL).read_text()
tree = ast.parse(source)
function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'skills_cleanup_exact_top')
branch = next(node for node in function.body if isinstance(node, ast.If) and SELECTOR in ast.unparse(node.test))
lines = source.splitlines(keepends=True)
removed = set(range(branch.lineno - 1, branch.end_lineno))
added_map = [n for n, line in enumerate(lines) if SELECTOR in line and n not in removed]
assert len(added_map) == 1
removed.update(added_map)
old = subprocess.run(['git', 'show', BASE + ':' + REL], cwd=REPO, check=True, capture_output=True, text=True).stdout
assert ''.join(line for n, line in enumerate(lines) if n not in removed) == old
assert len(removed) == 12
old_driver = subprocess.run(['git', 'show', BASE + ':.agent-state/task-planning-recovery/pg_only_driver.go'], cwd=REPO, check=True, capture_output=True).stdout
assert (REPO / '.agent-state/task-planning-recovery/pg_only_driver.go').read_bytes() == old_driver
actual_test = (REPO / 'tests/project/skills_cleanup_independent_test.go').read_text()
assert re.findall(r't.Run\("([^"\n]+)"', actual_test) == list(CHILDREN)
assert 't.Parallel(' not in actual_test

spec = importlib.util.spec_from_file_location('project_cleanup_independent_entry_probe', REPO / REL)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
assert sup.budgets(False) == (123, 3) and sup.budgets(True) == (540, 60)
assert len(sup.SKILLS_CLEANUP_TOPS) == 4


def go_log(children=CHILDREN):
    # Actual Go ordering: parent's RUN, each child's RUN/result, parent result.
    return ('=== RUN   ' + TOP + '\n' + ''.join('=== RUN   ' + TOP + '/' + child + '\n    --- PASS: ' + TOP + '/' + child + ' (0.01s)\n' for child in children) + '--- PASS: ' + TOP + ' (0.04s)\n')


checks = 0
with tempfile.TemporaryDirectory(prefix='project-cleanup-entry-') as name:
    base = Path(name)
    path = base / 'go.log'
    for mask in range(8):
        path.write_text(go_log(tuple(child for i, child in enumerate(CHILDREN) if mask & (1 << i))))
        assert sup.skills_cleanup_exact_top(path, SELECTOR) == (mask == 7)
        checks += 1
    good = go_log()
    for bad in (good + '    --- PASS: ' + TOP + '/' + CHILDREN[0] + ' (0.01s)\n',
                good.replace('--- PASS: ' + TOP + ' ', '--- SKIP: ' + TOP + ' '),
                good + '=== RUN   TestForeign\n--- PASS: TestForeign (0.00s)\n',
                good.replace(TOP + '/' + CHILDREN[1], TOP + '/other'),
                good.replace('=== RUN   ' + TOP + '\n', '')):
        path.write_text(bad)
        assert not sup.skills_cleanup_exact_top(path, SELECTOR)
        checks += 1
    path.write_text(good)
    for wrong in (TOP, SELECTOR[:-1], SELECTOR + '/.*'):
        assert not sup.skills_cleanup_exact_top(path, wrong)
        checks += 1
    for old_selector, old_top in sup.SKILLS_CLEANUP_TOPS.items():
        if old_selector == SELECTOR:
            continue
        path.write_text(f'=== RUN   {old_top}\n--- PASS: {old_top} (0.01s)\n')
        assert sup.skills_cleanup_exact_top(path, old_selector)
        checks += 1

    # Run the actual main with all process/OS/time effects explicitly replaced.
    # Gate denial, child failure and surviving descendants must still reach tails.
    for mode in ('ok', 'missing-child', 'child-exit', 'survivor', 'changed-input'):
        binary, driver = base / 'binary', base / 'driver'
        binary.write_bytes(b'owned-before')
        driver.write_bytes(b'not-executed')
        trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0, 'kill': []}

        class Child:
            pid = 445566
            returncode = None

            def __init__(self, args, stdout, stderr):
                assert args[:5] == [str(driver), '--test-binary', str(binary), '--run', SELECTOR]
                stdout.write(go_log(CHILDREN[:2]) if mode == 'missing-child' else good)
                stdout.flush()
                if mode == 'changed-input':
                    binary.write_bytes(b'owned-after')

            def wait(self, timeout):
                trace['wait'].append(timeout)
                self.returncode = 4 if mode == 'child-exit' else 0
                return self.returncode

        def desc(_):
            trace['desc'] += 1
            return {998877} if mode == 'survivor' and trace['desc'] == 1 else set()

        def tcp():
            trace['tcp'] += 1
            return set()

        def reap(*_):
            trace['reap'] += 1
            raise ChildProcessError

        output = base / mode
        argv = ['probe', '--driver', str(driver), '--binary', str(binary), '--run', SELECTOR, '--output', str(output)]
        with patch.object(sys, 'argv', argv), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', desc), patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), patch.object(sup.os, 'kill', side_effect=lambda pid, sig: trace['kill'].append((pid, sig))), patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), contextlib.redirect_stdout(io.StringIO()):
            code = sup.main()
        want = 0 if mode == 'ok' else 4 if mode == 'child-exit' else 1
        assert code == want
        assert trace['wait'] == [123] and trace['desc'] == 3 and trace['tcp'] == 3 and trace['reap'] == 1
        assert bool(trace['kill']) == (mode == 'survivor')
        log = next(output.glob('*.log')).read_text()
        for marker in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2', 'delta_empty_observation=1', 'delta_empty_observation=2', 'terminal=' + str(want)):
            assert marker in log
        assert ('inputs_unchanged=False' in log) == (mode == 'changed-input')
        checks += 1
print(f'PASS independent entry controls={checks}; old inverse/driver exact; 123+3/105+15/6m/TCP75 retained; all process/OS effects are doubles')
