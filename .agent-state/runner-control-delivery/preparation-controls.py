#!/usr/bin/env python3
"""Offline exact transplant controls; no main, child, /proc, Docker or socket."""
import ast
import json
import os
from pathlib import Path
import subprocess
import tempfile
import types
import unittest

ROOT = Path(__file__).resolve().parents[2]
BASE = '85832bd9353478b1ff945c714955a2344e30c8e3'
AUTHOR = 'ed709367'
SELECTOR = '^TestRunnerControlDefaultProcesses$'
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
OUTPUT = ROOT / 'output/ai/runner-control'
OUTPUT.mkdir(parents=True, exist_ok=True)


def blob(commit, path):
    return subprocess.check_output(['git', 'show', commit + ':' + path], cwd=ROOT, text=True)


def module(path, source=None):
    scope = {'__file__': str(ROOT / path), '__name__': 'preparation_control'}
    exec(compile((ROOT / path).read_text() if source is None else source, path, 'exec'), scope)
    return scope


def method(source, name):
    node = next(v for v in ast.parse(source).body if isinstance(v, ast.FunctionDef) and v.name == name)
    return ast.get_source_segment(source, node)


# Only the already-accepted C observer is reused; its methods remain exact.
fixture = 'tests/process/runner_control_test.go'
old = blob(BASE, fixture)
a = old.index('// This is a deployment-style TLS terminator, not a device/control replacement.\n')
b = old.index('func runnerRootDirectory', a)
expected = old[:a] + old[b:]
for item in ['log', 'net', 'net/http/httptest', 'net/http/httputil', 'net/url', 'sync/atomic']:
    expected = expected.replace('\t"' + item + '"\n', '')
expected = expected.replace('newRunnerRootTransport(t, v.address)', 'newRunnerFailureTransport(t, v.address)')
expected = expected.replace('proxy.retired(t)', 'proxy.controlRetired(t)')
assert (ROOT / fixture).read_text() == expected
assert (ROOT / 'tests/process/runner_failure_test.go').read_text() == blob(BASE, 'tests/process/runner_failure_test.go')

new_driver = (ROOT / DRIVER).read_text()
old_driver = blob(BASE, DRIVER)
addition = "    '" + SELECTOR + "': 'tests/process',\n"
assert new_driver.count(addition) == 1 and new_driver.replace(addition, '') == old_driver
new_sup = (ROOT / SUP).read_text()
author_sup = blob(AUTHOR, SUP)
for name in ('tcp_rows_evidence', 'tcp_process_identity', 'tcp_executable_identity',
             'tcp_entries_before_deadline', 'tcp_owner_evidence', 'tcp_evidence_pause', 'save_tcp_failure'):
    assert method(new_sup, name) == method(author_sup, name), name

# Reuse the accepted 18 TCP controls against this real transplanted source.
# Remove only the separately checked selector line for the full inverse check.
source = blob(AUTHOR, '.agent-state/runner-control/tcp-evidence-controls.py')
ns = {'__file__': str(ROOT / '.agent-state/runner-control-delivery/accepted-tcp-controls.py'),
      '__name__': 'accepted_controls'}
exec(compile(source, '<accepted-tcp-controls>', 'exec'), ns)
ns['BASELINE'] = BASE
Gate = ns['GateControls']
original_setup = Gate.setUpClass


@classmethod
def setup(cls):
    original_setup()
    added = "        '" + SELECTOR + "': {'TestRunnerControlDefaultProcesses'},\n"
    assert cls.new.count(added) == 1
    cls.new = cls.new.replace(added, '')


Gate.setUpClass = setup
suite = unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(ns[name])
                           for name in ('OwnerControls', 'EvidenceControls', 'GateControls'))
result = unittest.TextTestRunner(verbosity=1).run(suite)
assert result.wasSuccessful()

driver, original_driver, sup = module(DRIVER), module(DRIVER, old_driver), module(SUP)
assert sup['budgets'](False) == (123, 3) and sup['budgets'](True) == (540, 60)
assert set(driver['TARGETS']) == set(original_driver['TARGETS']) | {SELECTOR}
with tempfile.TemporaryDirectory(prefix='preparation-', dir=OUTPUT) as tmp:
    base = Path(tmp)
    candidate = base / 'candidate'; candidate.write_bytes(b'exact offline artifact'); candidate.chmod(0o700)
    minio = base / 'fixed-minio'; minio.write_bytes(b'controlled shape only')
    fresh = base / 'not-created'
    for scope in (driver, original_driver):
        scope['MINIO'] = minio
        scope['MINIO_SHA'] = scope['sha'](minio)
    for selector in original_driver['TARGETS']:
        assert driver['configuration'](candidate, selector, fresh) == original_driver['configuration'](candidate, selector, fresh)
    assert driver['input_paths'](candidate) == original_driver['input_paths'](candidate)
    plan = driver['configuration'](candidate, SELECTOR, fresh)
    assert plan['cwd'] == str(ROOT / 'tests/process') and plan['resources'] == 7 and plan['test_timeout'] == '6m'
    assert not fresh.exists()
    for selector in [SELECTOR[1:], SELECTOR[:-1], '^TestRunnerControl.*$', SELECTOR + '/^eof$',
                     SELECTOR + '|^TestExtra$', '^TestRunnerControl(DefaultProcesses|DefaultFailures)$']:
        try:
            driver['configuration'](candidate, selector, fresh)
        except ValueError:
            pass
        else:
            raise AssertionError('nonexact selector accepted')
    sup['time'] = types.SimpleNamespace(monotonic=lambda: 0, sleep=lambda _: None)
    cases = [(['TestRunnerControlDefaultProcesses'], True, False, False, True),
             ([], True, False, False, False), (['TestExtra'], True, False, False, False),
             (['TestRunnerControlDefaultProcesses', 'TestExtra'], True, False, False, False),
             (['TestRunnerControlDefaultProcesses'], False, False, False, False),
             (['TestRunnerControlDefaultProcesses'], True, True, False, False),
             (['TestRunnerControlDefaultProcesses'], True, False, True, False)]
    for number, (names, waited, live, private, want) in enumerate(cases):
        directory = base / str(number); directory.mkdir(); runtime = directory / 'runtime'; runtime.mkdir()
        resources = []
        for group, (label, kinds) in enumerate([('agenteam.d05.objectfixture', ['container', 'network']),
                                               ('agenteam.d04.networkfixture', ['container', 'network']),
                                               ('agenteam.d03.fixture', ['container', 'container', 'network'])], 1):
            for kind in kinds:
                resources.append({'kind': kind, 'id': format(len(resources) + 1, '064x'), 'label': label, 'nonce': format(group, '032x')})
        directories = [str(runtime / name) for name in ('object', 'outbound', 'pg')]
        record = directory / 'owned.json'
        record.write_text(json.dumps({'kind': 'work-owner-root-chain', 'resources': resources, 'directories': directories}))
        record.chmod(0o600)
        if private:
            Path(directories[0]).mkdir()
        calls = []
        def absent(item, timeout):
            calls.append(item['id']); return not live
        sup['exact_absent'] = absent
        logpath = directory / 'test.log'
        with logpath.open('w+') as log:
            for name in names:
                log.write('=== RUN   ' + name + '\n')
            if waited:
                log.write('D03 explicit test actual_wait pid=123 code=0 selector=' + SELECTOR + '\n')
            assert sup['observe_root_chain'](directory, log, logpath, SELECTOR) == want
        assert len(calls) == 14 and all(calls.count(item['id']) == 2 for item in resources)
        text = logpath.read_text()
        assert text.count('ROOT private_observation=') == 2 and text.count('ROOT runtime_observation=') == 2
print('PASS exact fixture reuse; 18 accepted TCP controls; byte-exact old paths; config positive/6 negatives; 7 observer controls x14 resource observations; no resources started')
