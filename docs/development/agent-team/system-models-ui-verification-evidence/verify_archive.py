#!/usr/bin/env python3
"""Check saved bytes and local fixed Git objects; never execute saved commands."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]
INDEX = json.loads((ROOT / 'archive-index.json').read_bytes())
ENV = {**os.environ, 'GIT_NO_LAZY_FETCH': '1'}
BASE = INDEX['product_base']
ACCEPTED = INDEX['accepted_commit']
BATCH = subprocess.Popen(
    ['git', 'cat-file', '--batch'], cwd=REPOSITORY, env=ENV,
    stdin=subprocess.PIPE, stdout=subprocess.PIPE,
)
CACHE = {}


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def git(*args):
    return subprocess.check_output(['git', *args], cwd=REPOSITORY, env=ENV)


def blob(commit, path):
    key = commit + ':' + path
    if key not in CACHE:
        BATCH.stdin.write((key + '\n').encode())
        BATCH.stdin.flush()
        head = BATCH.stdout.readline().decode().split()
        assert len(head) == 3 and head[1] == 'blob', key
        CACHE[key] = BATCH.stdout.read(int(head[2]))
        assert BATCH.stdout.read(1) == b'\n'
    return CACHE[key]


def archived(path):
    target = (ROOT / path).resolve()
    assert target.is_relative_to(ROOT), path
    return target.read_bytes()


ENTRIES = {}
PHYSICAL = set()
for entry in INDEX['originals']:
    logical = entry['logical']
    assert logical not in ENTRIES
    raw = archived(entry['archive'])
    assert (sha(raw), len(raw)) == (entry['sha256'], entry['bytes']), logical
    ENTRIES[logical] = entry
    PHYSICAL.add(entry['archive'])
assert INDEX['format'] == 1
assert len(ENTRIES) == INDEX['counts']['logical']
assert len(PHYSICAL) == INDEX['counts']['physical']
assert sum(len(archived(p)) for p in PHYSICAL) == INDEX['counts']['physical_bytes']
assert {str(p.relative_to(ROOT)) for p in ROOT.rglob('*') if p.is_file()} == PHYSICAL | {
    'archive-index.json', 'README.md', 'verify_archive.py',
}


def original(logical):
    return archived(ENTRIES[logical]['archive'])


def document(logical):
    return json.loads(original(logical))


def filemap(rows):
    if isinstance(rows, list):
        return {r['path']: r['sha256'] for r in rows}
    return {p: (v['sha256'] if isinstance(v, dict) else v) for p, v in rows.items()}


def source_hash(path, expected):
    matches = [e for e in ENTRIES.values() if e['source'] == os.path.normpath(path)]
    assert matches and all(e['sha256'] == expected for e in matches), path


AUTHOR = document('author/author-index.json')
VERIFIER = document('verifier/verification-report.json')
assert AUTHOR['active_author_commands'] == 0 and AUTHOR['real_window_released']
assert VERIFIER['frozen'] and not VERIFIER['active_resources'] and VERIFIER['no_further_test_runs']
assert 'PASS' in VERIFIER['status']
INPUTS = {}
for number in (1, 2, 3):
    name = f'input{number:02d}'
    m = document('author/' + name + '/manifest.json')
    INPUTS[name] = m
    assert m['product_base'] == BASE and len(m['files']) == 25 and len(m['dist']) == 29
    for path, expected in m['files'].items():
        assert sha(original('author/' + name + '/source/' + path)) == expected
    for path, expected in m['dependencies'].items():
        assert sha(blob(BASE, path)) == expected
    for record in m['checks']:
        source_hash(record['result_path'], record['result_sha256'])
    source_hash(m['observer_probe_path'], m['observer_probe_sha256'])
    for record in m.get('serializer_probe', {}).values():
        source_hash(record['path'], record['sha256'])
FINAL = INPUTS['input03']
assert INPUTS['input01']['dist'] == INPUTS['input02']['dist'] == FINAL['dist']
for earlier, later, changed in (
    ('input01', 'input02', {'tests/account-captcha-web/e2e/system-models.spec.ts'}),
    ('input02', 'input03', {'tests/account/system_models_web_fixture_test.go'}),
):
    assert {p for p, h in INPUTS[later]['files'].items() if INPUTS[earlier]['files'][p] != h} == changed
BASELINE = filemap(FINAL['accepted_baseline']['files'])
assert len(BASELINE) == INDEX['counts']['baseline_git_paths'] == 1030
assert BASELINE == filemap(INPUTS['input02']['accepted_baseline']['files'])
for path, expected in BASELINE.items():
    assert sha(blob(BASE, path)) == expected, path
    assert sha(blob(ACCEPTED, path)) == expected, path
for stage in ('api-stage01', 'owner-stage01', 'web-stage01'):
    m = document('author/' + stage + '/manifest.json')
    for row in m['files']:
        path, expected = row['path'], row['sha256']
        raw = blob(BASE, path) if row.get('role') == 'accepted_dependency' else original('author/' + stage + '/source/' + path)
        assert sha(raw) == expected, path
    for path, expected in filemap(m.get('accepted_dependencies', {})).items():
        assert sha(blob(BASE, path)) == expected

# Exact historical check inputs are saved, including original failing test sources.
for name, binding in AUTHOR['offline_runs_original_results'].items():
    source_hash(binding['path'], binding['sha256'])
    prefix = 'author/' + name + '/'
    result = document(prefix + 'result.json')
    assert sha(original(prefix + 'raw.log')) == result['raw_sha256']
    if prefix + 'input.json' in ENTRIES:
        for path, expected in filemap(document(prefix + 'input.json').get('files', {})).items():
            assert sha(original(prefix + 'source/' + path)) == expected
for path in INDEX['early_check_results_without_env_field']:
    e = next(e for e in ENTRIES.values() if e['source'] == path)
    assert 'env' not in document(e['logical'])
for prefix in ('api/api-stage01-report', 'owner/owner-stage01-report', 'page/web-stage01-report'):
    d = document(prefix + '.json')
    source_hash(d['author_manifest'], d['author_manifest_sha256'])
    root = os.path.dirname(ENTRIES[prefix + '.json']['source'])
    for path, expected in d['artifact_hashes'].items():
        source_hash(os.path.join(root, path), expected)
for path, expected in VERIFIER['artifact_hashes'].items():
    source_hash(path, expected)

README = document('author/final-delivery02/manifest.json')
DELIVERY = filemap(README['files'])
assert len(DELIVERY) == INDEX['counts']['delivered_sources'] == 26
assert README['source_count'] == 25 and README['documentation_count'] == 1
assert DELIVERY == {**FINAL['files'], 'docs/development/frontend/README.md': README['readme']['sha256']}
assert README['dist'] == FINAL['dist'] and README['writes_stopped']
assert not README['checks']['new_tests_or_resources'] and not README['checks']['business_checks_repeated']
for path, expected in DELIVERY.items():
    assert sha(blob(ACCEPTED, path)) == expected, path
for path, expected in (
    ('README.md', README['readme']['sha256']),
    ('README-before.md', README['readme']['before_sha256']),
    ('README-delta.diff', README['readme']['delta_sha256']),
    ('README-full.diff', README['readme']['full_diff_sha256']),
    ('26-paths.sha256', README['delivery_list']['sha256']),
):
    assert sha(original('author/final-delivery02/' + path)) == expected
assert original('author/final-delivery02/README-before.md') == original('author/final-delivery/README.md')
assert original('author/final-delivery/README-before.md') == blob(BASE, 'docs/development/frontend/README.md')
source_hash(README['predecessor_delivery']['path'], README['predecessor_delivery']['sha256'])
source_hash(README['tested_input03']['path'], README['tested_input03']['sha256'])
assert document('verifier/commit-source-manifest.json')['files25'] == FINAL['files']
assert set(git('diff-tree', '--no-commit-id', '--name-only', '-r', ACCEPTED).decode().splitlines()) == set(DELIVERY)
assert git('diff', '--name-only', INDEX['backend_base'], ACCEPTED, '--', 'api', 'internal', 'cmd', 'db', 'go.mod', 'go.sum') == b''
migrations = git('ls-tree', '--name-only', ACCEPTED, 'db/migrations/').decode().splitlines()
assert len([p for p in migrations if p.endswith('.sql')]) == 19
for path, expected in INDEX['prior_document_sha256'].items():
    assert sha(blob(ACCEPTED, path)) == expected
card = blob(ACCEPTED, 'docs/development/work-items/d27-system-model-management-ui.md')
assert sha(b'## 1.' + card.split(b'## 1.', 1)[1]) == INDEX['card_technical_sha256']


def check_real(prefix, input_name, exit_code, seconds, pids, waits_count, pass_count, fail_count):
    command = document(prefix + 'command.json')
    result = document(prefix + 'result.json')
    assert command['argv'] and command['cwd'] and command['env']
    assert command['actual_wait_completed'] and command['exit'] == exit_code
    assert command['seconds'] == seconds and result['driver_exit'] == exit_code
    assert result['source_files_unchanged'] and result['verification_inputs_unchanged']
    assert result['accepted_baseline_before'] and result['accepted_baseline_after']
    before = document(prefix + 'input-before.json')
    assert before == document(prefix + 'input-after.json')
    for key in ('product_base', 'files', 'dist', 'dependencies'):
        assert before[key] == INPUTS[input_name][key]
    baseline = before['accepted_baseline']
    assert baseline['accepted'] and baseline['accepted_count'] == 1030 and not baseline['errors']
    assert baseline['files'] == BASELINE
    assert sha(original(prefix + 'driver.py.txt')) == document(prefix + 'verification-input.json')['driver_sha256']
    frozen = document(prefix + 'frozen-input.json')
    if prefix.startswith('author/'):
        assert frozen == INPUTS[input_name]
        assert sha(original(prefix + 'driver.py.txt')) == frozen['driver_sha256']
    else:
        assert frozen == document('verifier/execution01.json')
        assert frozen['product_manifest_sha256'] == sha(original('author/input03/manifest.json'))
        assert before['private'] == frozen['private']
        for path, expected in frozen['private'].items():
            source_hash(path, expected)
    resources = document(prefix + 'observed-resources.json')
    processes = document(prefix + 'observed-processes.json')
    waits = document(prefix + 'adopted-waits.json')
    assert len(resources) == result['observed_resources'] == 7
    assert len(processes) == result['observed_processes'] == pids
    assert len(waits) == result['adopted_waits'] == waits_count and all(w['actual_wait'] for w in waits)
    assert document(prefix + 'monitor-errors.json') == [] and result['monitor_errors'] == 0
    baseline = document(prefix + 'baseline.json')
    assert baseline == document('author/runs/new01/baseline.json')
    assert sum(r['kind'] == 'container' for r in baseline.values()) == 2
    assert sum(r['kind'] == 'network' for r in baseline.values()) == 4
    assert result['double_cleanup'] and result['browser_runtime_removed']
    scans = document(prefix + 'cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        for key in ('owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'):
            assert scan[key] == []
    tops = re.findall(r'^--- (PASS|FAIL): (\S+) \(([0-9.]+)s\)$', original(prefix + 'raw.log').decode(), re.M)
    assert [[state, test] for state, test, _ in tops] == result['top_levels']
    assert sum(state == 'PASS' for state, _, _ in tops) == pass_count
    assert sum(state == 'FAIL' for state, _, _ in tops) == fail_count
    assert result['selected_tests_passed'] == (exit_code == 0)
    return {test for state, test, _ in tops if state == 'PASS'}


new01 = check_real('author/runs/new01/', 'input02', 1, 201.587, 188, 24, 2, 4)
new02 = check_real('author/runs/new02/', 'input03', 0, 124.100, 124, 16, 4, 0)
assert not new01 & new02 and len(new01 | new02) == 6
check_real('author/runs/old-core01/', 'input03', 0, 125.801, 169, 28, 7, 0)
check_real('author/runs/old-provider01/', 'input03', 0, 88.181, 112, 12, 3, 0)
check_real('verifier/runs/independent01/', 'input03', 0, 155.994, 212, 8, 2, 0)
for name, record in AUTHOR['real_runs'].items():
    for binding in record['evidence'].values():
        source_hash(binding['path'], binding['sha256'])
assert len(AUTHOR['visuals']['images']) == 8
for binding in AUTHOR['visuals']['images'].values():
    source_hash(binding['path'], binding['sha256'])
BATCH.stdin.close()
assert BATCH.wait() == 0
print(f"PASS archive bytes/local Git only: {len(ENTRIES)} logical originals / {len(PHYSICAL)} objects / "
      f"{INDEX['counts']['physical_bytes']} bytes; 26 delivery paths at {ACCEPTED}; "
      '1030 baseline paths; input01–03; new 2+4 / old 7+3 / independent 2 recorded PASS; '
      'five recorded actual exits and double cleanup. No product or archived command executed.')
