#!/usr/bin/env python3
"""Pure exact-selector and original observer controls; no child/resources."""
import ast
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestKnowledgeOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$'
TOPS = {'TestKnowledgeOwnerReadHTTPMetadata', 'TestKnowledgeOwnerReadHTTPCurrentAuthority', 'TestKnowledgeOwnerReadHTTPTransactions', 'TestKnowledgeOwnerReadHTTPCommitUnknown'}
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
BASE = 'bb98b6cd'


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    out = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(out)
    return out


for path in (DRIVER, SUPERVISOR):
    source = (ROOT / path).read_text()
    ast.parse(source)
    assert sum(SELECTOR in line for line in source.splitlines()) == 1
    before = subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT, check=True, capture_output=True, text=True).stdout
    restored = ''.join(line for line in source.splitlines(keepends=True)
                       if SELECTOR not in line and not any(marker in line for marker in (
                           'SCHEMA_PYTHON =', 'SCHEMA_PYTHON, REPOSITORY',
                           "REPOSITORY / 'api/openapi/knowledge-owner.json'",
                           "'AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON':")))
    assert restored == before

driver = module(DRIVER, 'knowledge_http_driver_controls')
supervisor = module(SUPERVISOR, 'knowledge_http_supervisor_controls')
assert supervisor.budgets(True) == (540, 60)
assert supervisor.budgets(False) == (123, 3)
assert driver.TARGETS[SELECTOR] == 'tests/knowledge'
assert len(driver.TARGETS) == 8
assert driver.SCHEMA_PYTHON.is_absolute() and driver.SCHEMA_PYTHON.is_file()
actual_inputs = set(driver.input_paths(ROOT / 'output/ai/knowledge-owner-read/knowledge-owner-read-http-race.test'))
required_runtime = {driver.SCHEMA_PYTHON, ROOT / '.agent-state/knowledge-owner-read/schema-controls.py',
                    ROOT / 'api/openapi/knowledge-owner.json', ROOT / 'api/openapi/common.json'}
assert required_runtime <= actual_inputs
# Fake fixed-file material isolates the selector/configuration branch only;
# this never claims actual MinIO binary approval or invokes its main function.
with tempfile.TemporaryDirectory(prefix='knowledge-selector-') as name:
    temp = Path(name)
    binary, minio = temp / 'candidate', temp / 'controlled-minio'
    binary.write_text('not executed')
    binary.chmod(0o700)
    minio.write_bytes(b'configuration-only')
    driver.MINIO = minio
    driver.MINIO_SHA = hashlib.sha256(minio.read_bytes()).hexdigest()
    cfg = driver.configuration(binary, SELECTOR, temp / 'fresh')
    assert cfg['test_timeout'] == '6m' and cfg['resources'] == 7
    assert cfg['cwd'] == str(ROOT / 'tests/knowledge')
    bad = [SELECTOR[1:], SELECTOR[:-1], SELECTOR + 'x', '^TestKnowledgeOwnerReadHTTP.*$', '^TestKnowledgeOwnerReadHTTPMetadata$', '^TestKnowledgeOwnerReadHTTP(Metadata|Transactions|CurrentAuthority|CommitUnknown)$']
    for selector in bad:
        try:
            driver.configuration(binary, selector, temp / 'fresh')
        except ValueError:
            pass
        else:
            raise AssertionError('non-exact selector accepted')
    directory = temp / 'observer'
    directory.mkdir()
    runtime = directory / 'runtime'
    runtime.mkdir()
    resources = [dict(kind='container' if i % 2 else 'network', id=f'{i+1:064x}', nonce='a' * 32) for i in range(7)]
    record = {'resources': resources, 'directories': [str(runtime / str(i)) for i in range(3)]}
    supervisor.root_record = lambda _: record
    supervisor.time.sleep = lambda _: None
    observations = []
    supervisor.exact_absent = lambda item, timeout: observations.append(item['id']) is None
    log_path = temp / 'log'
    controls = 0
    tops = sorted(TOPS)
    # Every proper subset must fail, regardless of successful resource tails.
    cases = [(set(tops[i] for i in range(4) if mask & (1 << i)), SELECTOR, True, mask == 15) for mask in range(16)]
    cases += [(TOPS | {'TestKnowledgeB02Runtime'}, SELECTOR, True, False), (TOPS, SELECTOR + 'x', True, False), (TOPS, SELECTOR, False, False)]
    for actual, wait_selector, waited, expected in cases:
        log_path.write_text(''.join('=== RUN   ' + top + '\n' for top in sorted(actual)) + (f'D03 explicit test actual_wait pid=123 code=0 selector={wait_selector}\n' if waited else ''))
        observations.clear()
        result = supervisor.observe_root_chain(directory, io.StringIO(), log_path, SELECTOR)
        assert result == expected and len(observations) == 14
        controls += 1
    log_path.write_text(''.join('=== RUN   ' + top + '\n' for top in tops) + f'D03 explicit test actual_wait pid=123 code=0 selector={SELECTOR}\n')
    for failure in ('resource', 'private', 'runtime'):
        observations.clear()
        supervisor.exact_absent = lambda item, timeout: (observations.append(item['id']) is None) and failure != 'resource'
        leftover = runtime / ('0' if failure == 'private' else 'leftover')
        if failure != 'resource':
            leftover.mkdir()
        assert not supervisor.observe_root_chain(directory, io.StringIO(), log_path, SELECTOR)
        assert len(observations) == 14
        if leftover.exists(): leftover.rmdir()
        controls += 1
print(f'PASS inverse old tools + 4 runtime inputs; config 1+6 negatives; observer {controls} controls x14 exact resource substitutes; no main/PG/socket')
