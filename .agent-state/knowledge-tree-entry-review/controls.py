#!/usr/bin/env python3
"""Independent entry controls; native driver rejects before any child/resource."""
import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--repo', type=Path, default=Path('/workspace/agenteam-knowledge-tree-http'))
args = parser.parse_args()
repo = args.repo.resolve()
spec = importlib.util.spec_from_file_location('tree_entry_review', repo / '.agent-state/task-planning-recovery/pg_only_supervisor.py')
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
checks = 0


def check(value):
    global checks
    assert value
    checks += 1


owned = Path(__file__).resolve().parents[2] / 'output/ai/knowledge-tree-entry-review'
owned.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(dir=owned) as tmp:
    tmp = Path(tmp)
    log_path = tmp / 'original.log'
    for selector in (sup.TREE_COMMAND_PG, sup.TREE_COMMAND_NATIVE):
        groups = sup.TREE_COMMAND_GROUPS[selector]
        names = list(groups) + [parent + '/' + child for parent, children in groups.items() for child in children]
        check(len(groups) == (4 if selector == sup.TREE_COMMAND_PG else 3))
        check(len(names) == (17 if selector == sup.TREE_COMMAND_PG else 9))
        runs = ''.join('=== RUN   ' + name + '\n' for name in names)
        passes = ''.join('--- PASS: ' + name + ' (0.01s)\n' for name in reversed(names))
        log_path.write_text(runs + passes)
        check(sup.tree_commands_exact(log_path, selector))
        # Same count and all RUNs present do not allow a missing/duplicate PASS.
        log_path.write_text(runs + passes.replace(names[0] + ' (', names[1] + ' ('))
        check(not sup.tree_commands_exact(log_path, selector))
        log_path.write_text(runs.replace(names[0] + '\n', names[1] + '\n') + passes)
        check(not sup.tree_commands_exact(log_path, selector))
        for marker in ('--- SKIP: TestForeign (0.01s)\n', 'FAIL\ttest-package\t0.01s\n',
                       '--- FAIL: TestForeign (0.01s)\n'):
            log_path.write_text(runs + passes + marker)
            check(not sup.tree_commands_exact(log_path, selector))
        if selector == sup.TREE_COMMAND_PG:
            log_path.write_text(runs + passes)
            for result in (False, UnicodeDecodeError('utf8', b'\xff', 0, 1, 'invalid'), OSError('unreadable')):
                with patch.object(sup, 'observe_root_chain', side_effect=result if isinstance(result, Exception) else None,
                                  return_value=result if isinstance(result, bool) else None) as original:
                    check(not sup.tree_commands_root(tmp, io.StringIO(), log_path, selector))
                    check(original.call_count == 1)

    # Actual fixed driver, with no existing parent and no candidate to execute.
    # The accepted selector must fail at mkdir; every nonexact variant earlier.
    driver = repo / 'output/ai/knowledge-tree-http/tree-command-native-driver'
    missing = tmp / 'no-parent' / 'runtime'
    selector = sup.TREE_COMMAND_NATIVE
    env = {k: v for k, v in os.environ.items() if not k.startswith('AGENTEAM_')}
    for candidate, accepted in ((selector, True), (selector[1:], False), (selector[:-1], False),
                                (selector.replace('ReadDeadlines|KeepAliveAndClose', 'KeepAliveAndClose|ReadDeadlines'), False),
                                ('^TestTreeCommandsHTTPNativeReadDeadlines$', False),
                                (selector + '/original_two_seconds', False),
                                ('^TestTreeCommandsHTTPNative.*$', False)):
        result = subprocess.run([str(driver), '--test-binary', str(tmp / 'no-binary'), '--run', candidate,
                                 '--directory', str(missing)], env=env, capture_output=True, text=True, timeout=5)
        expected = ('STOP runtime directory must be new\n' if accepted else
                    'STOP exact native binary, selector and directory required\n')
        check(result.returncode == 1 and result.stdout == '' and result.stderr == expected)
        check(not missing.parent.exists())

native = (repo / 'internal/central/knowledge/commandhttp/native_test.go').read_text()
condition = 'n != 0 || !(errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET))'
check(native.count(condition) == 2)
print(json.dumps({'passed': checks, 'native_test_started': False, 'pg_socket_started': False}))
