#!/usr/bin/env python3
"""Verify saved bytes and fixed local Git objects; never execute saved commands."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[3]
INDEX = json.loads((ROOT / 'archive-index.json').read_bytes())
BASE, ACCEPTED = INDEX['product_base'], INDEX['accepted_commit']
ENV = {**os.environ, 'GIT_NO_LAZY_FETCH': '1'}
BATCH = subprocess.Popen(['git', 'cat-file', '--batch'], cwd=REPOSITORY, env=ENV,
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE)
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


ENTRIES, PHYSICAL = {}, set()
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


def source_hash(path, expected):
    matches = [e for e in ENTRIES.values() if e['source'] == os.path.normpath(path)]
    assert matches and all(e['sha256'] == expected for e in matches), path


def filemap(rows):
    return {p: (v['sha256'] if isinstance(v, dict) else v) for p, v in rows.items()}


AUTHOR = document('author/author-index.json')
VERIFIER = document('real/verification-report.json')
assert VERIFIER['frozen'] and VERIFIER['all_commands_ended']
assert VERIFIER['owned_real_resources_remaining'] == 0
for path, expected in VERIFIER['artifact_hashes'].items():
    source_hash(path, expected)
INPUTS = {}
for name, pointer in AUTHOR['stages'].items():
    m = document('author/' + name + '/manifest.json')
    assert sha(original('author/' + name + '/manifest.json')) == pointer['sha256']
    INPUTS[name] = m
    for path, expected in m['files'].items():
        assert sha(original('author/' + name + '/source/' + path)) == expected
FINAL = INPUTS['input02']
assert len(FINAL['files']) == 25 and len(FINAL['dist']) == 31
assert FINAL['files'] == AUTHOR['frozen25']
assert FINAL['dist'] == AUTHOR['dist31']
assert INPUTS['harness-stage01']['dist'] == INPUTS['input01']['dist'] == FINAL['dist']
assert {p for p, h in INPUTS['input01']['files'].items() if INPUTS['harness-stage01']['files'][p] != h} == {
    'tests/account-captcha-web/e2e/system-model-selection.spec.ts',
}
assert {p for p, h in FINAL['files'].items() if INPUTS['input01']['files'][p] != h} == {
    'tests/account/system_model_selection_web_fixture_test.go',
}
assert {p for p, h in INPUTS['page-stage03']['files'].items() if INPUTS['page-stage02']['files'][p] != h} == {
    'web/src/composables/useSystemModelSelection.ts', 'web/src/tests/system-model-selection-state.spec.ts',
}
BASELINE = filemap(FINAL['accepted_baseline']['files'])
assert len(BASELINE) == INDEX['counts']['baseline_git_paths'] == 1041
assert BASELINE == filemap(INPUTS['input01']['accepted_baseline']['files'])
for path, expected in BASELINE.items():
    assert sha(blob(BASE, path)) == expected, path
    assert sha(blob(ACCEPTED, path)) == expected, path
for path, expected in FINAL['dependencies'].items():
    if '/node_modules/' not in path:
        assert sha(blob(BASE, path)) == expected, path
for path, expected in INDEX['prior_document_sha256'].items():
    assert sha(blob(ACCEPTED, path)) == expected
card = blob(ACCEPTED, 'docs/development/work-items/d27-system-model-selection-ui.md')
assert sha(b'## 1.' + card.split(b'## 1.', 1)[1]) == INDEX['card_technical_sha256']

DELIVERY = document('author/readme-delivery01/delivery26.json')
assert DELIVERY['stopped_writing'] and len(DELIVERY['files']) == 26
assert DELIVERY['files'] == {**FINAL['files'], 'docs/development/frontend/README.md': '5ddfc8ae082230f788d38bb20acc3eaaad0ad2e84a65e20870ca4e78aee68e11'}
assert DELIVERY['dist31_unchanged'] == FINAL['dist']
assert document('real/delivery25.json')['files'] == FINAL['files']
for path, expected in DELIVERY['files'].items():
    assert sha(blob(ACCEPTED, path)) == expected, path
    assert sha(original('author/readme-delivery01/source/' + path)) == expected
assert original('author/readme-delivery01/README.before.md') == blob(BASE, 'docs/development/frontend/README.md')
assert document('author/readme-delivery01/validation.json')['no_test_or_service_started']
assert set(git('diff-tree', '--no-commit-id', '--name-only', '-r', ACCEPTED).decode().splitlines()) == set(DELIVERY['files'])
assert git('diff', '--name-only', INDEX['backend_base'], ACCEPTED, '--', 'api', 'internal', 'cmd', 'db', 'go.mod', 'go.sum') == b''
assert len([p for p in git('ls-tree', '--name-only', ACCEPTED, 'db/migrations/').decode().splitlines() if p.endswith('.sql')]) == 19

# Explicitly account for missing historical passing/formatting inputs; never infer them.
BINDINGS = {}
for binding in INDEX['historical_source_bindings']:
    key = (binding['path'], binding['sha256'])
    raw = original(binding['original']) if 'original' in binding else blob(*binding['git'].split(':', 1))
    assert sha(raw) == binding['sha256']
    BINDINGS[key] = binding
GAPS = {(x['run'], x['input'], x['path'], x['sha256']): x for x in INDEX['unavailable_historical_versions']}
confirmed = {(x['path'], x['sha256']) for x in GAPS.values() if x['author_confirmed_missing']}
format_only = {(x['path'], x['sha256']) for x in GAPS.values() if not x['author_confirmed_missing']}
assert len(confirmed) == 4 and len(format_only) == 12
assert confirmed == {(x['path'], x['sha256']) for x in AUTHOR['historical_source_limit']['missing']}
assert len(AUTHOR['checks']) == INDEX['counts']['author_check_records'] == 52
for name, info in AUTHOR['checks'].items():
    prefix = 'author/runs/' + name + '/'
    for pointer in info['files'].values():
        source_hash(pointer['path'], pointer['sha256'])
    command, result = document(prefix + 'command.json'), document(prefix + 'result.json')
    assert command['argv'] and command['cwd'] and command['env']
    assert info['actual_wait']
    assert result.get('exit_code', result.get('driver_exit')) == info['actual_exit']
    if 'raw_sha256' in result:
        assert sha(original(prefix + 'raw.log')) == result['raw_sha256']
    if 'real_result' in info:
        continue
    assert result['actual_wait']
    for filename in ('input.json', 'input-before.json', 'input-after.json'):
        if prefix + filename not in ENTRIES:
            continue
        d = document(prefix + filename)
        for path, expected in d.get('files', d).items():
            if (path, expected) in BINDINGS:
                continue
            gap = GAPS[(name, filename, path, expected)]
            assert info['actual_exit'] == 0 and gap['actual_exit'] == 0
            assert gap['author_confirmed_missing'] or 'format' in name
assert original('page_red/logs/observations02/probe.ts.txt') == original('page_repair/logs/original01/probe.ts.txt')
assert document('page_red/page-stage01-report.json')['finding']['safe_observation']['writes'] == 0
for name in ('type01', 'list-a01', 'list-b01'):
    prefix = 'real/runs/' + name + '/'
    failure = document(prefix + 'launch-error.json')
    assert not failure['command_child_started'] and failure['no_post_input_or_child_result_was_written']
    assert original(prefix + 'raw.log') == b'' and prefix + 'result.json' not in ENTRIES


def check_real(prefix, input_name, exit_code, seconds, pids, waits_count, pass_count, fail_count):
    command, result = document(prefix + 'command.json'), document(prefix + 'result.json')
    assert command['argv'] and command['cwd'] and command['env']
    assert command['actual_wait_completed'] and command['exit'] == exit_code and command['seconds'] == seconds
    assert result['driver_exit'] == exit_code and result['source_files_unchanged'] and result['verification_inputs_unchanged']
    assert result['accepted_baseline_before'] and result['accepted_baseline_after']
    before = document(prefix + 'input-before.json')
    assert before == document(prefix + 'input-after.json')
    for key in ('product_base', 'files', 'dist', 'dependencies'):
        assert before[key] == INPUTS[input_name][key], (prefix, key)
    baseline = before['accepted_baseline']
    assert baseline['accepted'] and baseline['accepted_count'] == 1041
    assert not any(baseline[k] for k in ('errors', 'missing', 'unexpected', 'mismatches'))
    verification = document(prefix + 'verification-input.json')
    assert sha(original(prefix + 'driver.py.txt')) == verification['driver_sha256']
    if prefix.startswith('author/'):
        assert document(prefix + 'frozen-input.json') == INPUTS[input_name]
        assert baseline['files'] == BASELINE
        assert before['locked_runtime'] == FINAL['locked_runtime']
    else:
        execution = document(prefix + 'execution.json')
        assert sha(original(prefix + 'execution.json')) == verification['execution_sha256']
        assert verification['author_manifest_sha256'] == sha(original('author/input02/manifest.json'))
        for path, expected in execution['private_files'].items():
            assert sha(original('real/' + path)) == expected
        assert before['private'] == verification['private']
        assert baseline['files_sha256'] == sha(json.dumps(BASELINE, sort_keys=True).encode())
        assert before['locked_runtime']['inventory_sha256'] == sha(json.dumps(FINAL['locked_runtime'], sort_keys=True).encode())
        assert before['locked_runtime']['files_count'] == len(FINAL['locked_runtime']['files']) == 7132
        assert before['locked_runtime']['symlinks_count'] == len(FINAL['locked_runtime']['symlinks']) == 28
    resources, processes = document(prefix + 'observed-resources.json'), document(prefix + 'observed-processes.json')
    waits = document(prefix + 'adopted-waits.json')
    assert len(resources) == result['observed_resources'] == 7
    assert len(processes) == result['observed_processes'] == pids
    assert len(waits) == result['adopted_waits'] == waits_count and all(w['actual_wait'] for w in waits)
    assert document(prefix + 'monitor-errors.json') == [] and result['monitor_errors'] == 0
    baseline = document(prefix + 'baseline.json')
    assert baseline == document('author/runs/new01/baseline.json')
    assert sum(v['kind'] == 'container' for v in baseline.values()) == 2
    assert sum(v['kind'] == 'network' for v in baseline.values()) == 4
    scans = document(prefix + 'cleanup.json')
    assert result['double_cleanup'] and result['browser_runtime_removed']
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert all(scan[k] == [] for k in ('owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'))
    tops = re.findall(r'^--- (PASS|FAIL): (\S+) \(([0-9.]+)s\)$', original(prefix + 'raw.log').decode(), re.M)
    assert [[state, name] for state, name, _ in tops] == result['top_levels']
    assert sum(state == 'PASS' for state, _, _ in tops) == pass_count
    assert sum(state == 'FAIL' for state, _, _ in tops) == fail_count
    assert result['selected_tests_passed'] == (exit_code == 0)
    return {name for state, name, _ in tops if state == 'PASS'}


new01 = check_real('author/runs/new01/', 'input01', 1, 204.111, 178, 20, 5, 1)
new02 = check_real('author/runs/new02/', 'input02', 0, 69.029, 89, 4, 1, 0)
assert not new01 & new02 and len(new01 | new02) == 6
check_real('author/runs/old-core01/', 'input02', 0, 126.425, 168, 28, 7, 0)
check_real('author/runs/old-provider01/', 'input02', 0, 89.833, 111, 12, 3, 0)
check_real('author/runs/old-model01/', 'input02', 0, 107.473, 108, 12, 3, 0)
check_real('real/runs/independent01/', 'input02', 1, 199.715, 203, 8, 0, 2)
b02 = check_real('real/runs/independent02/', 'input02', 1, 77.600, 95, 8, 1, 1)
a03 = check_real('real/runs/independent03/', 'input02', 0, 61.415, 84, 4, 1, 0)
assert not b02 & a03 and len(b02 | a03) == 2
marker = b"test('[independent-b]"
assert original('real/bound04/independent.spec.ts').split(marker, 1)[1] == original('real/bound05/independent.spec.ts').split(marker, 1)[1]
visual = document('author/new02-visual-review.json')
assert len(visual['images']) == 8 and all(i['viewed'] for i in visual['images'])
for image in visual['images']:
    source_hash(image['path'], image['sha256'])
BATCH.stdin.close()
assert BATCH.wait() == 0
print(f"PASS archive bytes/local Git only: {len(ENTRIES)} logical originals / {len(PHYSICAL)} objects / "
      f"{INDEX['counts']['physical_bytes']} bytes; 26 paths at {ACCEPTED}; 1041 baseline paths; "
      '25 final sources / 31 dist fingerprints; eight recorded actual exits/double cleanup; '
      'new5+1 / old7+3+3 / independentB02+A03; 4 confirmed historical +12 formatter source limits retained. '
      'No product or archived command executed.')
