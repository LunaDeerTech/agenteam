#!/usr/bin/env python3
"""Offline controls over the actual two Skills PG harness files; no resources."""
import contextlib
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
DRIVER = ROOT / 'output/ai/skills/compile/pg-only-skills-combo-driver'
BINARY = ROOT / 'output/ai/skills/compile/skill-pg-stop.test'
SELECTOR = '^TestSkill(Migration|InitializationAdmissionUnknown|OwnerMetadataCurrentAuthority)$'
TOPS = ['TestSkillMigration', 'TestSkillInitializationAdmissionUnknown',
        'TestSkillOwnerMetadataCurrentAuthority']
spec = importlib.util.spec_from_file_location('skills_combo_supervisor', SUPERVISOR)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
checks = []


def check(label, condition):
    assert condition, label
    checks.append(label)


def lines(tops):
    return ''.join('=== RUN   ' + top + '\n' for top in tops).encode()


with tempfile.TemporaryDirectory(prefix='skills-pg-combo-controls-') as temporary:
    directory = Path(temporary)
    # A recognized selector reaches Statfs on a deliberately absent parent.
    # Every command exits before mkdir, entropy, Docker or child execution.
    selectors = [
        (SELECTOR, True), ('^TestSkillMigration$', True),
        ('^TestWorkHTTPNativeDeadlines$', True),
        ('^TestSkill.*$', False), (SELECTOR[:-1], False),
        ('^TestSkill(Migration|OwnerMetadataCurrentAuthority|InitializationAdmissionUnknown)$', False),
        ('^TestSkill(Migration|InitializationAdmissionUnknown)$', False),
        ('^TestSkill(Migration|InitializationAdmissionUnknown|OwnerMetadataCurrentAuthority|LifecycleStopPersistence)$', False),
        ('^TestSkillMigration$/fresh_and_repeat', False),
    ]
    for selector, recognized in selectors:
        absent = directory / 'absent-parent' / 'run'
        result = subprocess.run([str(DRIVER), '--test-binary', str(BINARY),
                                 '--run', selector, '--directory', str(absent)],
                                capture_output=True, text=True, timeout=5)
        message = ('fresh disk below 5 GiB' if recognized else
                   'exact binary, directory and one anchored top are required')
        check('actual driver selector ' + selector,
              result.returncode == 1 and result.stderr.strip() == message
              and not absent.parent.exists())

    matrices = [
        ('exact', lines(TOPS), True),
        ('reordered-with-child', lines(list(reversed(TOPS))) + b'=== RUN   TestSkillMigration/fresh_and_repeat\n', True),
        ('empty', b'', False), ('missing', lines(TOPS[:-1]), False),
        ('extra', lines(TOPS + ['TestSkillLifecycleStopPersistence']), False),
        ('duplicate', lines(TOPS + [TOPS[0]]), False),
        ('only-child', b'=== RUN   TestSkillMigration/fresh_and_repeat\n', False),
        ('invalid-utf8', lines(TOPS) + b'\xff\n', False),
    ]
    path = directory / 'observed.log'
    for name, content, expected in matrices:
        path.write_bytes(content)
        log = io.StringIO()
        check('actual observer ' + name,
              module.observe_skill_pending_acceptance(log, path) is expected)
    path.unlink()
    check('actual observer missing log',
          module.observe_skill_pending_acceptance(io.StringIO(), path) is False)

    # Run the real supervisor main with a controlled, already-waited driver.
    # The invalid log must not bypass descendant/TCP/input/final observations.
    for name, content, selector, expected in [
        ('exact', lines(TOPS), SELECTOR, 0),
        ('missing', lines(TOPS[:2]), SELECTOR, 1),
        ('invalid-utf8', lines(TOPS) + b'\xff\n', SELECTOR, 1),
        ('old-single-unchanged', b'\xff\n', '^TestSkillMigration$', 0),
    ]:
        output = directory / name
        tcp_calls = []
        wait_calls = []

        class Child:
            pid = 424242
            returncode = None

            def __init__(self, args, stdout, stderr):
                check('supervisor exact argv ' + name,
                      args == [str(DRIVER), '--test-binary', str(BINARY),
                               '--run', selector, '--directory', args[-1]])
                stdout.flush()
                stdout.buffer.write(content)
                stdout.flush()

            def wait(self, timeout):
                wait_calls.append(timeout)
                self.returncode = 0
                return 0

        class Libc:
            def prctl(self, *args):
                return 0

        def tcp():
            tcp_calls.append(True)
            return set()

        argv = ['supervisor', '--driver', str(DRIVER), '--binary', str(BINARY),
                '--run', selector, '--output', str(output)]
        with patch.object(sys, 'argv', argv), \
             patch.object(module.ctypes, 'CDLL', return_value=Libc()), \
             patch.object(module.subprocess, 'Popen', Child), \
             patch.object(module, 'descendants', return_value=set()), \
             patch.object(module.os, 'waitpid', side_effect=ChildProcessError), \
             patch.object(module, 'tcp', tcp), \
             patch.object(module.time, 'sleep', return_value=None), \
             contextlib.redirect_stdout(io.StringIO()):
            code = module.main()
        raw = next(output.glob('*.log')).read_bytes()
        check('actual main tail ' + name,
              code == expected and wait_calls == [123] and len(tcp_calls) == 3
              and b'actual=True actual_exit=0 code=0' in raw
              and b'OWNED runtime_observation=1 descendants=[]' in raw
              and b'OWNED runtime_observation=2 descendants=[]' in raw
              and b'HOST_TCP delta_empty_observation=1' in raw
              and b'HOST_TCP delta_empty_observation=2' in raw
              and f'inputs_unchanged=True terminal={expected}'.encode() in raw)

# Full inverse projection proves no change to original budgets, child Wait,
# resource cleanup, default/single-top paths, root chain or TCP/input gates.
def baseline(path):
    relative = str(path.relative_to(ROOT))
    return subprocess.check_output(['git', 'show', '4442a356:' + relative], cwd=ROOT).decode()

driver = DRIVER_SOURCE.read_text()
driver = driver.replace('const skillPendingAcceptanceSelector = "' + SELECTOR + '"\n\n', '', 1)
new = '(!regexp.MustCompile(`^\\^Test[A-Za-z0-9]+\\$$`).MatchString(*selector) && *selector != skillPendingAcceptanceSelector)'
driver = driver.replace(new, '!regexp.MustCompile(`^\\^Test[A-Za-z0-9]+\\$$`).MatchString(*selector)', 1)
check('driver exact inverse baseline', driver == baseline(DRIVER_SOURCE))
supervisor = SUPERVISOR.read_text()
start = supervisor.index('SKILL_PENDING_ACCEPTANCE_SELECTOR = ')
end = supervisor.index('def budgets(root_chain):', start)
supervisor = supervisor[:start] + supervisor[end:]
invocation = ('            if (not args.root_chain and args.run == SKILL_PENDING_ACCEPTANCE_SELECTOR\n'
              '                    and not observe_skill_pending_acceptance(log, log_path)):\n'
              '                code = 1\n')
supervisor = supervisor.replace(invocation, '', 1)
check('supervisor exact inverse baseline', supervisor == baseline(SUPERVISOR))
print(json.dumps({'passed': len(checks), 'controls': checks}))
