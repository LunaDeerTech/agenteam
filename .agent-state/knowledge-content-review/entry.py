#!/usr/bin/env python3
"""Independent pure controls; no executable, process, PG or socket is started."""
import argparse
import hashlib
import importlib.util
import inspect
import io
import json
import os
from pathlib import Path
import tempfile
import types
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--root', type=Path, required=True)
parser.add_argument('--expect-interpreter-gap', action='store_true')
args = parser.parse_args()
source = args.root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
spec = importlib.util.spec_from_file_location('content_entry_independent', source)
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
checks = 0


def events(selector):
    nodes = [name for parent, children in sup.CONTENT_GROUPS[selector].items()
             for name in (parent, *(parent + '/' + child for child in children))]
    return ''.join('=== RUN   ' + name + '\n--- PASS: ' + name + ' (0.01s)\n' for name in nodes)


def inputs(selector):
    return sup.content_inputs(selector) if inspect.signature(sup.content_inputs).parameters else sup.content_inputs()


with tempfile.TemporaryDirectory(prefix='content-entry-independent-') as name:
    root = Path(name)
    path, manifest = root / 'log', root / 'owned.json'
    selector = sup.CONTENT_NATIVE
    tail = (f'CHILD pid=123 selector={selector} kind=native-http\n'
            'CHILD actual_wait pid=123 state=exit status 0\n'
            'NATIVE runtime_empty=true actual_child_wait=true\n'
            'DRIVER terminal exit=0 elapsed=0.010s child_started=true actual_child_wait=true private_removed=true\n')
    manifest.write_text(json.dumps({'kind': 'work-http-native', 'child_pid': 123}))
    manifest.chmod(0o600)
    good = events(selector) + tail
    for label, value, wanted in (
        ('valid', good, True),
        ('wrong-start-pid', good.replace('CHILD pid=123 ', 'CHILD pid=124 '), False),
        ('wrong-wait-pid', good.replace('actual_wait pid=123 ', 'actual_wait pid=124 '), False),
        ('duplicate-wait', good + 'CHILD actual_wait pid=123 state=exit status 0\n', False),
        ('missing-runtime', good.replace('NATIVE runtime_empty=true actual_child_wait=true\n', ''), False),
        ('missing-driver-terminal', good[:good.index('DRIVER terminal')], False),
    ):
        path.write_text(value)
        assert sup.content_native(root, io.StringIO(), path, selector) is wanted, label
        checks += 1
    path.write_text(good)
    (root / 'tmp').symlink_to(root / 'absent-private-target')
    assert not sup.content_native(root, io.StringIO(), path, selector)
    (root / 'tmp').unlink()
    manifest.chmod(0o644)
    assert not sup.content_native(root, io.StringIO(), path, selector)
    checks += 2

    selector = sup.CONTENT_PG
    wait = f'D03 explicit test actual_wait pid=123 code=0 selector={selector}\n'
    # This group isolates the additional gate; original resource observations
    # are explicitly substituted, not claimed as actual clean resources.
    with patch.object(sup, 'observe_root_chain', return_value=True):
        for label, line, wanted in (
            ('valid', wait, True), ('missing', '', False),
            ('duplicate', wait + wait, False),
            ('nonzero', wait.replace('code=0', 'code=1'), False),
            ('wrong-selector', wait.replace(selector, '^TestOther$'), False),
        ):
            path.write_text(events(selector) + line)
            assert sup.content_root(root, io.StringIO(), path, selector) is wanted, label
            checks += 1

    python, replacement = root / 'schema-python', root / 'other-schema-python'
    driver, binary = root / 'never-executed-driver', root / 'never-executed-candidate'
    for item in (python, replacement, driver, binary):
        item.write_bytes(b'controlled executable identity; never run\n')
        item.chmod(0o700)
    config = types.SimpleNamespace(driver=driver, binary=binary, run=selector,
                                   content_schema_python=str(python))
    adapter = types.SimpleNamespace(input_paths=lambda _: [driver, binary])
    with patch.dict(os.environ, {'AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON': str(python)}):
        paths = {p.resolve() for p in inputs(selector)} | {driver, binary}
        snapshot = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
        assert sup.content_same(snapshot, config, adapter)
        python.write_bytes(b'changed actual schema interpreter identity\n')
        accepted_changed_bytes = sup.content_same(snapshot, config, adapter)
        python.write_bytes(b'controlled executable identity; never run\n')
        os.environ['AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON'] = str(replacement)
        accepted_changed_path = sup.content_same(snapshot, config, adapter)
    assert accepted_changed_bytes is args.expect_interpreter_gap
    assert accepted_changed_path is args.expect_interpreter_gap
    checks += 3
print(f'PASS {checks} independent pure controls; interpreter_gap={args.expect_interpreter_gap}; no Go/process/PG/socket')
