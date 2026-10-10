#!/usr/bin/env python3
"""Exact content entries and unchanged supervision; all processes are substitutes."""
import ast
import contextlib
import hashlib
import importlib.util
import io
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
BASE = 'e3145974'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
ADAPTER = '.agent-state/work-owner-http/root_chain_driver.py'
NATIVE = '.agent-state/work-owner-http/native_driver.go'
sys.dont_write_bytecode = True
projection_path = ROOT / '.agent-state/owner-feature-integration/entry_union.py'
projection_spec = importlib.util.spec_from_file_location('owner_entry_union', projection_path)
projection = importlib.util.module_from_spec(projection_spec)
projection_spec.loader.exec_module(projection)


def original(path):
    return projection.baseline_for(path, 'knowledge_content')


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def remove_once(text, value, replacement=''):
    assert text.count(value) == 1, value
    return text.replace(value, replacement, 1)


source = (ROOT / SUP).read_text()
ast.parse(source)
a, b = source.index('CONTENT_PG = '), source.index('def root_adapter(driver):')
inverse = source[:a] + source[b:]
inverse = remove_once(inverse, '        CONTENT_PG: set(CONTENT_GROUPS[CONTENT_PG]),\n')
inverse = remove_once(inverse, "    if args.run in CONTENT_GROUPS and args.root_chain != (args.run == CONTENT_PG):\n        parser.error('exact content selector requires its declared mode')\n")
inverse = remove_once(inverse, "    if args.run == CONTENT_PG:\n        try:\n            content_schema_python()\n            args.content_schema_python = os.environ['AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON']\n        except (OSError, ValueError):\n            parser.error('explicit local content Schema interpreter required')\n")
inverse = remove_once(inverse, '    if args.run in CONTENT_GROUPS:\n        inputs.update({str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest() for p in content_inputs(args.run)})\n')
inverse = remove_once(inverse, '            if args.root_chain and not (content_root(directory, log, log_path, args.run)\n                    if args.run == CONTENT_PG else observe_root_chain(directory, log, log_path, args.run)):\n                code = 1\n            if not args.root_chain and args.run == CONTENT_NATIVE and not content_native(directory, log, log_path, args.run):\n                code = 1\n', '            if args.root_chain and not observe_root_chain(directory, log, log_path, args.run):\n                code = 1\n')
inverse = remove_once(inverse, '                        same = (content_same(inputs, args, adapter) if args.run in CONTENT_GROUPS else\n                                all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest\n                                    for p, digest in inputs.items()))\n', '                        same = all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest\n                                   for p, digest in inputs.items())\n')
assert inverse == original(SUP)
sup = module(SUP, 'content_supervisor_controls')
adapter = module(ADAPTER, 'content_adapter_controls')
pg, native = sup.CONTENT_PG, sup.CONTENT_NATIVE
assert remove_once((ROOT / ADAPTER).read_text(), "    '" + pg + "': 'tests/knowledge',\n") == original(ADAPTER)
native_source = (ROOT / NATIVE).read_text()
inverse = remove_once(native_source, 'func contentNative(selector string) bool {\n\treturn selector == "' + native + '"\n}\n\n')
inverse = remove_once(inverse, ' && !contentNative(*selector)')
inverse = remove_once(inverse, ' else if contentNative(*selector) {\n\t\tnativeGate = "AGENTEAM_KNOWLEDGE_CONTENT_HTTP_NATIVE"\n\t}')
assert inverse == original(NATIVE)
assert sup.budgets(True) == (540, 60) and sup.budgets(False) == (123, 3)
assert native_source.count('"' + native + '"') == 1
assert '105*time.Second' in native_source and '"-test.timeout=90s"' in native_source
inputs = set(sup.content_inputs())
assert {ROOT / SUP, ROOT / ADAPTER, ROOT / NATIVE,
        ROOT / '.agent-state/knowledge-content-http/schema-controls.py',
        ROOT / 'api/openapi/knowledge-content.json', ROOT / 'api/openapi/common.json',
        ROOT / 'tests/knowledge/owner_content_http_reader_test.go',
        ROOT / 'tests/knowledge/owner_content_http_transactions_test.go',
        ROOT / 'tests/knowledge/owner_read_http_fixture_test.go',
        ROOT / 'tests/knowledge/owner_read_http_transactions_test.go',
        ROOT / 'internal/central/knowledge/contenthttp/native_test.go',
        ROOT / '.agent-state/project-variables-independent/commitproxy/proxy.go'} <= inputs
assert set((ROOT / 'tests/knowledge').glob('*.go')) <= inputs
assert all(p.is_file() and not p.is_symlink() for p in inputs)
assert len(sup.CONTENT_GROUPS[pg]) == 4 and sum(map(len, sup.CONTENT_GROUPS[pg].values())) == 14
assert len(sup.CONTENT_GROUPS[native]) == 3 and sum(map(len, sup.CONTENT_GROUPS[native].values())) == 6


def events(selector):
    return ''.join('=== RUN   ' + parent + '\n' + ''.join('=== RUN   ' + parent + '/' + child + '\n    --- PASS: ' + parent + '/' + child + ' (0.01s)\n' for child in children) + '--- PASS: ' + parent + ' (0.02s)\n' for parent, children in sup.CONTENT_GROUPS[selector].items())


def native_tail(selector, pid=123):
    return (f'CHILD pid={pid} selector={selector} kind=native-http\n'
            f'CHILD actual_wait pid={pid} state=exit status 0\n'
            'NATIVE runtime_empty=true actual_child_wait=true\n'
            'DRIVER terminal exit=0 elapsed=0.020s child_started=true actual_child_wait=true private_removed=true\n')


checks = 0
with tempfile.TemporaryDirectory(prefix='content-selector-controls-') as name:
    temp = Path(name)
    path = temp / 'log'
    for selector in (pg, native):
        good = events(selector)
        path.write_text(good)
        assert sup.content_exact(path, selector)
        checks += 1
        for line in good.splitlines(keepends=True):
            for bad in (good.replace(line, '', 1), good + line):
                path.write_text(bad)
                assert not sup.content_exact(path, selector)
                checks += 1
        for bad in (good + 'FAIL\n', good.replace('--- PASS:', '--- SKIP:', 1), good.replace('--- PASS:', '--- FAIL:', 1), good + '=== RUN   TestForeign\n', good.encode() + b'\xff'):
            path.write_bytes(bad if isinstance(bad, bytes) else bad.encode())
            assert not sup.content_exact(path, selector)
            checks += 1
        path.unlink()
        assert not sup.content_exact(path, selector)
        checks += 1
    binary, driver, minio, python = temp / 'candidate', temp / 'driver', temp / 'minio', temp / 'python'
    for p in (binary, driver, minio, python):
        p.write_bytes(b'controlled-never-executed')
        p.chmod(0o700)
    adapter.MINIO = minio
    adapter.MINIO_SHA = hashlib.sha256(minio.read_bytes()).hexdigest()
    assert adapter.configuration(binary, pg, temp / 'fresh')['resources'] == 7
    assert adapter.configuration(binary, pg, temp / 'fresh')['test_timeout'] == '6m'
    for bad in ('', 'relative/python', str(temp), str(temp / 'missing')):
        with patch.dict(sup.os.environ, {'AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON': bad}):
            try:
                sup.content_inputs(pg)
            except (OSError, ValueError):
                checks += 1
            else:
                raise AssertionError('invalid Schema interpreter accepted')
            assert sup.content_inputs(native) == sorted(inputs)
    with patch.dict(sup.os.environ, {'AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON': str(python)}):
        assert set(sup.content_inputs(pg)) == inputs | {python}
        checks += 1
        python.chmod(0o600)
        try:
            sup.content_inputs(pg)
        except ValueError:
            checks += 1
        else:
            raise AssertionError('non-executable Schema interpreter accepted')
        finally:
            python.chmod(0o700)
    for bad in (pg[1:], pg[:-1], pg + 'x', pg + '/.*', '^TestKnowledgeOwnerContentHTTP.*$', '^TestKnowledgeOwnerContentHTTPCurrentBytes$', pg.replace('CurrentBytes|CurrentAuthority', 'CurrentAuthority|CurrentBytes'), native):
        try:
            adapter.configuration(binary, bad, temp / 'fresh')
        except ValueError:
            checks += 1
        else:
            raise AssertionError('unapproved root selector')
    for selector in (pg, native):
        modes = ('valid', 'missing', 'utf8', 'exit2', 'wrong-wait', 'resource', 'private', 'input-add', 'input-delete')
        if selector == pg:
            modes += ('python-change', 'python-env', 'python-link')
        for mode in modes:
            trace = {'wait': [], 'desc': 0, 'tcp': 0, 'reap': 0, 'resource': 0}
            fixture = temp / ('sources-' + str(checks))
            fixture.mkdir()
            fixture_input = fixture / 'old.go'
            fixture_input.write_text('explicit input')
            python.write_bytes(b'controlled interpreter')
            alias = fixture / 'python-link'
            alias.symlink_to(python)
            schema_path = alias if mode == 'python-link' else python
            is_pg = selector == pg

            class Child:
                pid = 434343
                returncode = None

                def __init__(self, args, stdout, stderr):
                    directory = Path(args[args.index('--directory') + 1])
                    directory.mkdir()
                    (directory / ('runtime' if is_pg else 'tmp')).mkdir()
                    if is_pg:
                        if mode == 'private': (directory / 'runtime/left').write_text('owned residue')
                        tail = f'D03 explicit test actual_wait pid=123 code={1 if mode == "wrong-wait" else 0} selector={selector}\n'
                    else:
                        manifest = directory / 'owned.json'
                        manifest.write_text('{"kind":"work-http-native","child_pid":123}')
                        manifest.chmod(0o600)
                        if mode != 'private': (directory / 'tmp').rmdir()
                        tail = native_tail(selector)
                        if mode == 'wrong-wait': tail = tail.replace('state=exit status 0', 'state=exit status 1')
                        if mode == 'resource': manifest.write_text('{"kind":"work-http-native","child_pid":124}')
                    raw = events(selector)
                    if mode == 'missing': raw = raw[raw.index('\n') + 1:]
                    stdout.write(raw + tail)
                    stdout.flush()
                    if mode == 'utf8':
                        stdout.buffer.write(b'\xff\n')
                        stdout.buffer.flush()

                def wait(self, timeout):
                    trace['wait'].append(timeout)
                    if mode == 'input-add': (fixture / 'new.go').write_text('new source')
                    if mode == 'input-delete': fixture_input.unlink()
                    if mode == 'python-change': python.write_bytes(b'changed original interpreter')
                    if mode == 'python-env': sup.os.environ['AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON'] = str(driver)
                    if mode == 'python-link':
                        alias.unlink()
                        alias.symlink_to(driver)
                    self.returncode = 2 if mode == 'exit2' else 0
                    return self.returncode

            def descendants(_):
                trace['desc'] += 1
                return set()

            def tcp():
                trace['tcp'] += 1
                return set()

            def reap(*_):
                trace['reap'] += 1
                raise ChildProcessError

            def absent(item, timeout):
                trace['resource'] += 1
                return mode != 'resource'

            def record(directory):
                return {'resources': [{'kind': 'container', 'id': f'{i+1:064x}', 'nonce': 'a'*32} for i in range(7)], 'directories': [str(directory/'runtime'/str(i)) for i in range(3)]}

            fake_adapter = types.SimpleNamespace(TARGETS={pg: 'tests/knowledge'}, sha=adapter.sha, input_paths=lambda _: [driver, binary])
            output = temp / ('out-' + str(checks))
            args = ['probe', '--driver', str(driver), '--binary', str(binary), '--run', selector, '--output', str(output)]
            if is_pg: args.append('--root-chain')
            with patch.dict(sup.os.environ, {'AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON': str(schema_path)}), patch.object(sys, 'argv', args), patch.object(sup, 'root_adapter', return_value=fake_adapter), patch.object(sup, 'content_inputs', side_effect=lambda selected=None: sorted(fixture.glob('*.go')) + ([sup.content_schema_python()] if selected == pg else [])), patch.object(sup, 'root_record', record), patch.object(sup, 'exact_absent', absent), patch.object(sup.ctypes, 'CDLL', return_value=types.SimpleNamespace(prctl=lambda *_: 0)), patch.object(sup.subprocess, 'Popen', Child), patch.object(sup, 'descendants', descendants), patch.object(sup, 'tcp', tcp), patch.object(sup.os, 'waitpid', reap), patch.object(sup.time, 'sleep'), patch.object(sup.signal, 'signal'), contextlib.redirect_stdout(io.StringIO()):
                code = sup.main()
            expected = 0 if mode == 'valid' else 2 if mode == 'exit2' else 1
            assert code == expected, (selector, mode, code)
            assert trace == {'wait': [540 if is_pg else 123], 'desc': 3, 'tcp': 3, 'reap': 1, 'resource': 14 if is_pg else 0}, trace
            raw = next(output.glob('*.log')).read_bytes()
            for marker in ('actual_driver_wait', 'runtime_observation=1', 'runtime_observation=2', 'delta_empty_observation=1', 'delta_empty_observation=2', 'terminal=' + str(expected)):
                assert marker.encode() in raw
            assert ('inputs_unchanged=False' if mode.startswith(('input-', 'python-')) else 'inputs_unchanged=True').encode() in raw
            checks += 1
print(f'PASS {checks} controls; PG4/14 and native3/6 exact; old three tools byte-inverse; no child/proc/socket/PG')
