#!/usr/bin/env python3
"""Verify saved bytes and local immutable Git objects; never execute saved commands."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]
INDEX = json.loads((ROOT / 'archive-index.json').read_bytes())
assert INDEX['format'] == 1
ENV = {**os.environ, 'GIT_NO_LAZY_FETCH': '1'}
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
        header = BATCH.stdout.readline().decode().split()
        assert len(header) == 3 and header[1] == 'blob', key
        CACHE[key] = BATCH.stdout.read(int(header[2]))
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
    assert logical not in ENTRIES, logical
    if 'git' in entry:
        raw = blob(entry['git']['commit'], entry['git']['path'])
    else:
        PHYSICAL.add(entry['archive'])
        raw = archived(entry['archive'])
    assert (sha(raw), len(raw)) == (entry['sha256'], entry['bytes']), logical
    ENTRIES[logical] = entry
assert {str(p.relative_to(ROOT)) for p in ROOT.rglob('*') if p.is_file()} == PHYSICAL | {
    'README.md', 'archive-index.json', 'verify_archive.py',
}
assert len(ENTRIES) == INDEX['counts']['logical']
assert len(PHYSICAL) == INDEX['counts']['physical']
assert sum(len(archived(p)) for p in PHYSICAL) == INDEX['counts']['physical_bytes']
assert sum('git' in e for e in ENTRIES.values()) == INDEX['counts']['git_references']


def original(logical):
    e = ENTRIES[logical]
    return blob(e['git']['commit'], e['git']['path']) if 'git' in e else archived(e['archive'])


def document(logical):
    return json.loads(original(logical))


def source_hash(path, expected):
    path = os.path.normpath(path)
    matches = [e for e in ENTRIES.values() if e['source'] == path and e['sha256'] == expected]
    if not matches:
        relocation = INDEX['source_relocations'][path]
        assert relocation['sha256'] == expected
        assert sha(original(relocation['original'])) == expected
    else:
        assert all(sha(original(e['logical'])) == expected for e in matches)


AUTHOR = document('author/author-report.json')
VERIFIER = document('verifier/verification-report.json')
assert VERIFIER['verdict'] == 'INDEPENDENT PASS for fixed Provider input07; root adoption/README/Git outside this verifier'
BASE = INDEX['product_base']
ACCEPTED = INDEX['accepted_commit']
INPUTS = {}
BASELINE = {}
for name in ('preparation.json', 'backend-preparation.json'):
    record = document('author/' + name)
    assert record['base'] == BASE
    BASELINE.update(record['files'])
for i in range(1, 8):
    name = f'input{i:02d}'
    m = document('author/' + name + '.json')
    INPUTS[name] = m
    assert m['product_base'] == BASE and m['backend_accepted'] == INDEX['backend_base']
    assert len(m['files']) == (20 if i == 1 else 21) and len(m['dist']) == 26
    for path, expected in m['files'].items():
        assert sha(original('author/' + name + '/' + path)) == expected
    for path, expected in m['dependencies'].items():
        assert path not in BASELINE or BASELINE[path] == expected
        BASELINE[path] = expected
    if i > 1:
        previous = INPUTS[f'input{i-1:02d}']
        changed = {p for p, h in m['files'].items() if previous['files'].get(p) != h}
        wanted = {'tests/account-captcha-web/e2e/system-providers.spec.ts'}
        if i == 2:
            wanted.add('tests/account-captcha-web/e2e/system-invitations.spec.ts')
        if i >= 6:
            wanted.add('web/src/views/system/SystemProvidersView.vue')
        assert changed == wanted, (name, changed)
        assert (m['dist'] == previous['dist']) == (i < 6)
assert len(BASELINE) == INDEX['counts']['baseline_paths']
for path, expected in BASELINE.items():
    assert sha(blob(BASE, path)) == expected, path
FINAL = INPUTS['input07']
assert len(FINAL['dependencies']) == 1016
assert set(INPUTS['input01']['dependencies']) - set(FINAL['dependencies']) == {
    'tests/account-captcha-web/e2e/system-invitations.spec.ts',
}
assert sha(original('author/input07.json')) == AUTHOR['candidate']['manifest_sha256'] == VERIFIER['product_manifest_sha256']
assert FINAL['files'] == VERIFIER['deliverables_exact_21']
for path, expected in FINAL['dependencies'].items():
    assert sha(blob(ACCEPTED, path)) == expected, path
for path, expected in FINAL['files'].items():
    assert sha(blob(ACCEPTED, path)) == expected, path

README = document('author/readme-final.json')
assert README['product_input_sha256'] == sha(original('author/input07.json'))
assert README['product_sources_unchanged'] and not README['resources_started']
assert README['files'] == {**FINAL['files'], 'docs/development/frontend/README.md': README['readme_sha256']}
assert README['dist'] == FINAL['dist'] and README['dependency_count'] == 1016
for name, key in ((README['readme_snapshot'], 'readme_sha256'), ('readme-before.md', 'readme_before_sha256'), ('readme-final.diff', 'readme_diff_sha256'), ('runs/readme-check01/result.json', 'check_result_sha256')):
    assert sha(original('author/' + name)) == README[key]
assert sha(blob(ACCEPTED, 'docs/development/frontend/README.md')) == README['readme_sha256']
assert sha(blob(BASE, 'docs/development/frontend/README.md')) == README['readme_before_sha256']
assert sha(original('verifier/verification-report.md')) == README['independent_final_report_sha256']
changed = git('diff-tree', '--no-commit-id', '--name-only', '-r', ACCEPTED).decode().splitlines()
assert set(changed) == set(README['files']) and len(changed) == 22
assert git('diff', '--name-only', INDEX['backend_base'], ACCEPTED, '--', 'db', 'internal', 'api', 'cmd', 'go.mod', 'go.sum') == b''
for path, expected in INDEX['prior_document_sha256'].items():
    assert sha(blob(ACCEPTED, path)) == expected
card = blob(ACCEPTED, 'docs/development/work-items/d27-system-provider-management-ui.md')
assert sha(b'## 1.' + card.split(b'## 1.', 1)[1]) == INDEX['card_technical_sha256']

for stage, record in AUTHOR['stage_inputs'].items():
    m = document('author/' + record['manifest'])
    assert sha(original('author/' + record['manifest'])) == record['sha256']
    for path, expected in m['files'].items():
        assert sha(original('author/' + record['snapshot'] + '/' + path)) == expected
reuse = document('author/stage-reuse.json')
for stage, paths in reuse.items():
    m = document('author/' + stage + '.json')
    for path, same in paths.items():
        assert (m['files'][path] == FINAL['files'][path]) == same
for record in AUTHOR['independent_stage_reports']:
    source_hash(record['path'], record['sha256'])
    target = os.path.splitext(record['path'])[0] + '.json'
    e = next(e for e in ENTRIES.values() if e['source'] == target)
    d = document(e['logical'])
    for path, expected in d['artifact_hashes'].items():
        source_hash(os.path.join(os.path.dirname(target), path), expected)
for path, expected in VERIFIER['artifact_hashes'].items():
    source_hash(path, expected)
for collection in ('check_index', 'real_runs'):
    for record in AUTHOR[collection].values():
        for path, expected in record['evidence_sha256'].items():
            assert sha(original('author/' + record['path'] + '/' + path)) == expected
closure = document('verifier/closure-input.json')
assert len(closure['files']) == 40
for path, binding in closure['files'].items():
    raw = original('author/input01/' + path) if binding['origin'] == 'input01' else blob(BASE, path)
    assert sha(raw) == binding['sha256']


def check_real(prefix, expected_exit):
    command = document(prefix + 'command.json')
    result = document(prefix + 'result.json')
    m_raw = original(prefix + 'frozen-input.json')
    manifest = json.loads(m_raw)
    assert any(sha(m_raw) == sha(original('author/' + n + '.json')) for n in INPUTS)
    assert command['argv'] and command['cwd'] and command['env']
    assert command['actual_wait_completed'] and command['exit'] == expected_exit
    assert result['driver_exit'] == expected_exit
    assert result['source_files_unchanged'] and result['verification_inputs_unchanged']
    before = document(prefix + 'input-before.json')
    assert before == document(prefix + 'input-after.json')
    for key in ('product_base', 'files', 'dist', 'dependencies'):
        assert before[key] == manifest[key], (prefix, key)
    private = document(prefix + 'verification-input.json')
    assert sha(original(prefix + 'driver.py.txt')) == private['driver_sha256']
    for path, expected in private.items():
        if path != 'driver_sha256':
            assert sha(original(prefix + 'source/' + path)) == expected
    resources = document(prefix + 'observed-resources.json')
    processes = document(prefix + 'observed-processes.json')
    waits = document(prefix + 'adopted-waits.json')
    assert len(resources) == result['observed_resources'] == 7
    assert len(processes) == result['observed_processes']
    assert len(waits) == result['adopted_waits'] and all(w['actual_wait'] for w in waits)
    assert document(prefix + 'monitor-errors.json') == [] and result['monitor_errors'] == 0
    baseline = document(prefix + 'baseline.json')
    assert len(baseline) == 6
    assert sum(x['kind'] == 'container' for x in baseline.values()) == 2
    assert sum(x['kind'] == 'network' for x in baseline.values()) == 4
    assert baseline == document('author/runs/new01/baseline.json')
    failed_cleanup = prefix == 'author/runs/new01/'
    assert result['double_cleanup'] == (not failed_cleanup)
    assert result['browser_runtime_removed'] == (not failed_cleanup)
    cleanup = document(prefix + 'cleanup.json')
    assert len(cleanup) == 2 and cleanup[0]['time'] < cleanup[1]['time']
    for observation in cleanup:
        assert set(observation['exact_absent']) == set(resources)
        assert all(x['absent'] for x in observation['exact_absent'].values())
        assert observation['baseline_unchanged']
        for key in ('owned_processes', 'remaining_new', 'runtime_entries'):
            assert observation[key] == [], (prefix, key)
        assert observation['browser_runtime_entries'] == (['run-789356966'] if failed_cleanup else [])
    raw = original(prefix + 'raw.log').decode()
    tops = re.findall(r'^--- (PASS|FAIL): (\S+) \(([0-9.]+)s\)$', raw, re.M)
    assert [[state, name] for state, name, seconds in tops] == result['top_levels']
    return command, result


for name, info in AUTHOR['real_runs'].items():
    command, result = check_real('author/' + info['path'] + '/', info['actual_exit'])
    assert command['seconds'] == info['seconds']
    for field in ('top_levels', 'double_cleanup', 'observed_resources', 'observed_processes', 'adopted_waits', 'monitor_errors'):
        assert result[field] == info[field]
followup = document('author/runs/new01/cleanup-followup.json')
assert not followup['original_driver_double_cleanup'] and followup['double_clean_after_followup']
assert len(followup['checks']) == 2
resources = document('author/runs/new01/observed-resources.json')
for observation in followup['checks']:
    assert set(observation['exact_absent']) == set(resources) and all(observation['exact_absent'].values())
    assert observation['baseline_unchanged'] and observation['browser_runtime_removed']
    for key in ('owned_processes', 'remaining_new', 'runtime_entries'):
        assert observation[key] == []
assert sha(original('author/runs/new01/cleanup-followup.json')) == AUTHOR['real_runs']['new01']['separate_cleanup_followup_sha256']
assert [state for state, name in AUTHOR['real_runs']['new02']['top_levels']] == ['PASS'] * 4 + ['FAIL'] * 2
assert [state for state, name in AUTHOR['real_runs']['new03']['top_levels']] == ['PASS'] * 2
assert [state for state, name in AUTHOR['real_runs']['new05']['top_levels']] == ['PASS']
assert [state for state, name in AUTHOR['real_runs']['old01']['top_levels']] == ['PASS'] * 7
for info in VERIFIER['real_runs']:
    prefix = 'verifier/real-probe01/runs/' + info['name'] + '/'
    command, result = check_real(prefix, info['exit'])
    assert command['seconds'] == info['seconds'] and result['top_levels'] == info['tops']
    assert (result['observed_resources'], result['observed_processes'], result['adopted_waits']) == (info['resources'], info['owned_pid_starttimes'], info['adopted_waits'])
    assert not result['static_c_binary_executed']
assert VERIFIER['real_runs'][1]['tops'] == [
    ['FAIL', 'TestAccountSystemProvidersIndependentPartial'],
    ['PASS', 'TestAccountSystemProvidersIndependentReplay'],
]
assert VERIFIER['real_runs'][2]['tops'] == [['PASS', 'TestAccountSystemProvidersIndependentPartial']]
prefix02 = 'verifier/real-probe01/runs/independent02/source/'
prefix03 = 'verifier/real-probe01/runs/independent03/source/'
spec02 = original(prefix02 + 'e2e/independent-providers.spec.ts')
spec03 = original(prefix03 + 'e2e/independent-providers.spec.ts')
needle = b"fact.status === 503 && fact.ended"
assert spec02.count(needle) == 1
assert spec03 == spec02.replace(needle, b'fact.status === 503')
for path in ('e2e/fixed-helpers.ts', 'independent-fixture.go', 'independent-tops.go', 'independent.config.js', 'overlay.json', 'product-binding07.json'):
    assert original(prefix02 + path) == original(prefix03 + path)
for run, images in AUTHOR['images'].items():
    selected = images if run == 'new05' else ('providers-light-1440.png', 'providers-dark-390.png')
    for name in selected:
        raw = original('author/runs/' + run + '/images/' + name)
        assert sha(raw) == images[name]['sha256']
        assert raw.startswith(b'\x89PNG\r\n\x1a\n')
        if run == 'new05':
            assert int.from_bytes(raw[20:24], 'big') == 900
assert len(AUTHOR['images']['new05']) == 8
BATCH.stdin.close()
assert BATCH.wait() == 0
print('PASS: archive bytes and fixed Git only; no product or archived command executed')
print(json.dumps({**INDEX['counts'], 'delivery_paths': 22, 'historical_inputs': 7, 'real_runs': 9, 'final_independent': 'Replay independent02 + Partial independent03', 'new01_original_cleanup': False, 'new01_separate_followup': True}, ensure_ascii=False))
