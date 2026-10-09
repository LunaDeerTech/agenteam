#!/usr/bin/env python3
"""Independent exact three-top observer controls, no process/resource execution."""
import importlib.util
import io
import itertools
from pathlib import Path
import re
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path('/workspace/agenteam-runner-control-independent')
source = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('independent_management_review', source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
go = (ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go').read_text()
literal = re.search(r'^const independentRunnerManagementSelector = "([^"]+)"$', go, re.M)
assert literal and literal[1] == module.RUNNER_MANAGEMENT_SELECTOR
assert '&& *selector != independentRunnerManagementSelector)' in go
names = ('TestIndependentRunnerManagementConcurrent',
         'TestIndependentRunnerManagementLogoutOrder',
         'TestIndependentRunnerManagementCommitUnknown')
checks = 1
with tempfile.TemporaryDirectory(prefix='runner-management-review-') as temp:
    path = Path(temp) / 'child.log'
    for ordering in itertools.permutations(names):
        path.write_text(''.join('=== RUN   ' + name + '\n' for name in ordering)
                        + '=== RUN   ' + names[0] + '/private-child\n')
        log = io.StringIO()
        assert module.observe_runner_management(log, path)
        assert log.getvalue() == 'RUNNER management_exact_tops=True top_count=3\n'
        checks += 1
    for missing in names:
        for replacement in names:
            if missing == replacement:
                continue
            # Count remains three; equality alone must not accept duplicates
            # replacing a different required top.
            actual = [replacement if name == missing else name for name in names]
            path.write_text(''.join('=== RUN   ' + name + '\n' for name in actual))
            log = io.StringIO()
            assert not module.observe_runner_management(log, path)
            assert log.getvalue() == 'RUNNER management_exact_tops=False top_count=3\n'
            checks += 1
    path.write_bytes(b'PRIVATE-CANARY\xff\n')
    log = io.StringIO()
    assert not module.observe_runner_management(log, path)
    assert log.getvalue() == 'RUNNER management_exact_tops=False log_unreadable=True\n'
    checks += 1
print(f'PASS {checks} independent literal/order/duplicate/invalid-byte controls; no driver/child/proc/PG/socket')
