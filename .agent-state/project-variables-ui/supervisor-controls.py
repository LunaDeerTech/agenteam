#!/usr/bin/env python3
"""Offline controls for the exact Variables UI pre-reap and unchanged survivor gate.

No process, socket, browser or Docker fixture is created. The exact main-gate
AST is executed with controlled wait/descendant observations.
"""
import ast
import importlib.util
import io
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('variables_supervisor', SOURCE)
supervisor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(supervisor)
tree = ast.parse(SOURCE.read_text())
main = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
blocks = [node.body for node in ast.walk(main) if isinstance(node, ast.Try)]
block = next(nodes for nodes in blocks if any(isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'survivors' for t in node.targets) for node in nodes))
position = next(i for i, node in enumerate(block) if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'survivors' for t in node.targets))
assert isinstance(block[position-1], ast.If) and isinstance(block[position-1].test, ast.Name) and block[position-1].test.id == 'ui'
gate = compile(ast.fix_missing_locations(ast.Module(body=block[position-1:position+2], type_ignores=[])), str(SOURCE), 'exec')
assert supervisor.budgets(True) == (540, 60) and supervisor.budgets(False) == (123, 3)
assert len(supervisor.VARIABLE_UI_TOPS) == 6
assert not supervisor.variable_ui_selector('^TestAccountProjectVariablesWeb.*$')


def case(name, snapshot, waits, remaining, expected, ui=True):
    log, events = io.StringIO(), []
    def waited(pid, flags):
        assert pid == -1 and flags == supervisor.os.WNOHANG
        events.append('wait')
        value = waits.pop(0)
        if isinstance(value, Exception):
            raise value
        return value
    snapshots = [set(snapshot), set(remaining)] if ui else [set(remaining)]
    with patch.object(supervisor, 'descendants', side_effect=snapshots), patch.object(supervisor, 'observe_ui_descendant_states', side_effect=lambda log: events.append('state')), patch.object(supervisor.os, 'waitpid', side_effect=waited) as wait, patch.object(supervisor.os, 'kill') as kill, patch.object(supervisor.time, 'sleep', side_effect=AssertionError('no extra wait budget')):
        env = {'ui': ui, 'code': 0, 'log': log, 'os': supervisor.os, 'signal': supervisor.signal,
               'descendants': supervisor.descendants, 'observe_ui_descendant_states': supervisor.observe_ui_descendant_states, 'reap_ui_exited': supervisor.reap_ui_exited}
        exec(gate, env)
        assert env['code'] == expected, name
        assert kill.call_count == len(remaining), name
        assert wait.call_count <= len(snapshot), name
        assert not waits, name
    if ui:
        assert events[0] == 'state'
    else:
        assert not events
    if remaining:
        assert 'STOP owned descendants survived driver:' in log.getvalue()
    print(name + ': PASS')

case('Z exit0 actual wait', [101,102], [(101,0),(102,0)], [], 0)
case('Z nonzero preserves failure', [101], [(101,512)], [], 1)
case('live preserves STOP and kill', [101], [(0,0)], [101], 1)
case('not waitable preserves STOP and kill', [101], [ChildProcessError()], [101], 1)
case('new child after finite snapshot preserves STOP', [101], [(101,0)], [102], 1)
case('non UI skips prereap and keeps old gate', [], [], [101], 1, ui=False)
with patch.object(supervisor, 'descendants', return_value={101}), patch.object(Path, 'read_text', return_value='Name:\towned\nState:\tZ (zombie)\n'):
    log = io.StringIO()
    supervisor.observe_ui_descendant_states(log)
    assert log.getvalue() == 'UI descendant_before_reap pid=101 state=Z\n'
print('owned state projection and unchanged budgets: PASS')
