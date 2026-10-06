#!/usr/bin/env python3
"""Verify immutable bytes, fixed Git inputs and recorded execution; no product execution."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[3]
PRODUCT = '40c904c0dd88420fc621f40c0a737d243d514ec1'
BASE = 'debbb28deb7c883fd0b6b77a75354b9b5d7ece0b'
TECH = '35c2f7f00917213642d9897ae37bc9509728f9921f9f71f674b59505bf06a146'
MISSING = '72e3a507ef49871c3b0170942ac43788df52414cf884b4b351965e7bc1840d37'


def sha(data):
    return hashlib.sha256(data).hexdigest()


index = json.loads((HERE / 'index.json').read_bytes())
assert index['accepted_product_commit'] == PRODUCT and index['product_base'] == BASE
assert index['technical_sha256'] == TECH
allowed_parents = {HERE / 'objects'}
for name, row in index['permanent_dependencies'].items():
    path = (HERE / row['index']).resolve()
    assert path == HERE.parent / name / 'index.json'
    assert sha(path.read_bytes()) == row['sha256']
    allowed_parents.add(path.parent / 'objects')
objects = {}
local = set()
for value, row in index['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', value)
    path = (HERE / row['path']).resolve()
    assert path.parent in allowed_parents and path.name == value
    data = path.read_bytes()
    assert sha(data) == value and len(data) == row['bytes']
    objects[value] = data
    if path.parent == HERE / 'objects':
        local.add(value)
assert local == {p.name for p in (HERE / 'objects').iterdir()}
for name, row in index['artifacts'].items():
    assert len(objects[row['sha256']]) == row['bytes'], name


def original(name):
    return objects[index['artifacts'][name]['sha256']]


def record(name):
    return json.loads(original(name))


def hash_value(value):
    return value['sha256'] if isinstance(value, dict) else value


manifests = {name: record(name) for name in index['source_manifests']}
final_input = manifests['author/input06/manifest.json']
delivery = record('author/final-delivery01/manifest.json')
assert len(final_input['files']) == 26 and len(final_input['dist']) == 33
assert len(delivery['files']) == len(index['delivery']) == 27
assert {p: h for p, h in delivery['files'].items() if p != 'docs/development/frontend/README.md'} == final_input['files']
assert delivery['dist'] == final_input['dist'] and delivery['readme_only_since_input06']
assert final_input['product_base'] == BASE and len(final_input['accepted_baseline']['files']) == 1051

# Git supplies baseline bytes; existing original manifests are the only baseline inventories.
refs = {PRODUCT + ':' + path for path in delivery['files']}
refs.add(PRODUCT + ':' + index['card'])
for manifest in manifests.values():
    refs.update(manifest['product_base'] + ':' + p for p in manifest['accepted_baseline']['files'])
for path, row in index['fixed_early_git_references'].items():
    refs.add(row['commit'] + ':' + path)
ordered = sorted(refs)
env = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_TERMINAL_PROMPT='0')
output = subprocess.run(['git', 'cat-file', '--batch'], input=('\n'.join(ordered) + '\n').encode(),
                        cwd=REPO, env=env, check=True, capture_output=True).stdout
git, offset = {}, 0
for ref in ordered:
    end = output.index(b'\n', offset)
    header = output[offset:end].decode().split()
    assert len(header) == 3 and header[1] == 'blob', ref
    size = int(header[2])
    offset = end + 1
    git[ref] = output[offset:offset + size]
    offset += size + 1
assert offset == len(output)
for path, expected in delivery['files'].items():
    assert sha(git[PRODUCT + ':' + path]) == expected
    assert index['delivery'][path]['sha256'] == expected and sha(objects[expected]) == expected
for manifest in manifests.values():
    assert len(manifest['files']) == 26
    for path, expected in manifest['files'].items():
        assert sha(objects[expected]) == expected, path
    for path, value in manifest['accepted_baseline']['files'].items():
        assert sha(git[manifest['product_base'] + ':' + path]) == hash_value(value), path
for path, row in index['fixed_early_git_references'].items():
    assert sha(git[row['commit'] + ':' + path]) == row['sha256']
for data in (git[PRODUCT + ':' + index['card']], (REPO / index['card']).read_bytes()):
    assert sha(data[data.index(b'## 1.'):]) == TECH

missing_seen = []
for name in index['artifacts']:
    if not name.startswith('author/'):
        continue
    if name.endswith('/input.json'):
        source = record(name)
        if isinstance(source, list) and all(isinstance(x, dict) and {'path', 'sha256'} <= set(x) for x in source):
            for row in source:
                if row['sha256'] == MISSING:
                    missing_seen.append((name, row['path']))
                else:
                    assert row['sha256'] in objects, (name, row)
assert missing_seen == [('author/runs/harness-go01/input.json', 'tests/account-captcha-web/e2e/system-account-security.spec.ts')]
assert MISSING not in objects and index['bounded_missingness']['sha256'] == MISSING

for prefix, row in index['runs'].items():
    result = record(prefix + '/result.json')
    raw = original(prefix + '/raw.log')
    if row['kind'] == 'real':
        command = record(prefix + '/command.json')
        assert command['actual_wait_completed'] and command['exit'] == result['driver_exit'] == row['exit']
        assert command['seconds'] == row['seconds']
        assert result['top_levels'] == row['top_levels']
        assert result['selected_tests_passed'] == (row['exit'] == 0)
        for status, name in result['top_levels']:
            assert ('--- ' + status + ': ' + name + ' ').encode() in raw
        assert all(result[k] for k in ('source_files_unchanged', 'verification_inputs_unchanged',
                                      'accepted_baseline_before', 'accepted_baseline_after', 'double_cleanup'))
        assert result['monitor_errors'] == 0 and not record(prefix + '/monitor-errors.json')
        before = record(prefix + '/input-before.json')
        assert before == record(prefix + '/input-after.json')
        frozen = record(prefix + '/frozen-input.json')
        if prefix.startswith('author/'):
            input_manifest = frozen
            assert sha(original(prefix + '/driver.py.txt')) == input_manifest['driver_sha256']
        else:
            input_manifest = json.loads(objects[frozen['author_manifest']['sha256']])
            assert before['private_inputs'] == frozen['private_inputs']
            assert before['runtime_inputs'] == frozen['runtime_inputs']
            assert sha(original(prefix + '/driver.py.txt')) == frozen['driver_sha256']
        for key in ('files', 'dist', 'dependencies'):
            assert before[key] == input_manifest[key], (prefix, key)
        baseline = input_manifest['accepted_baseline']['files']
        assert before['accepted_baseline']['accepted'] and before['accepted_baseline']['accepted_count'] == len(baseline)
        assert before['accepted_baseline']['files'] == {p: hash_value(v) for p, v in baseline.items()}
        resources = record(prefix + '/observed-resources.json')
        processes = record(prefix + '/observed-processes.json')
        waits = record(prefix + '/adopted-waits.json')
        cleanup = record(prefix + '/cleanup.json')
        assert len(resources) == result['observed_resources'] == row['resources'] == 7
        assert len(processes) == result['observed_processes'] == row['owned_processes']
        assert len(waits) == result['adopted_waits'] == row['adopted_waits']
        assert all(x['actual_wait'] for x in waits) and len(cleanup) == 2
        for scan in cleanup:
            assert set(scan['exact_absent']) == set(resources) and all(x['absent'] for x in scan['exact_absent'].values())
            assert scan['baseline_unchanged']
            assert not any(scan[k] for k in ('owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'))
    else:
        assert result.get('actual_wait', result.get('actual_wait_completed'))
        assert result.get('exit_code', result.get('exit')) == row['exit']
        assert result.get('duration_seconds', result.get('elapsed_seconds', result.get('seconds'))) == row['seconds']
        assert result['argv'] and result['cwd']
        assert isinstance(result.get('env', result.get('env_overrides')), dict)
        if 'raw_sha256' in result:
            assert result['raw_sha256'] == sha(raw)
        if 'before' in result:
            assert result['before'] == result['after'] and result['inputs_unchanged']
        if 'resource_execution' in result:
            assert result['resource_execution'] is False
        if prefix + '/input-before.json' in index['artifacts']:
            before, after = record(prefix + '/input-before.json'), record(prefix + '/input-after.json')
            if prefix.endswith('/combination-check06'):
                assert result['dist_change_authorized'] and not result['dist_unchanged']
                assert before['dist'] != after['dist']
                assert {k: v for k, v in before.items() if k != 'dist'} == {k: v for k, v in after.items() if k != 'dist'}
            else:
                assert before == after

assert b'1115 passed (1115)' in original('author/runs/combination-check06/raw.log')
assert b'32 passed (32)' in original('author/runs/combination-check06/raw.log')
assert b'no such file or directory' in original('author/runs/combination-go-list06/raw.log')
failed = record('author/runs/combination-go-list06/result.json')
passed = record('author/runs/combination-go-list06b/result.json')
assert failed['exit_code'] == 1 and passed['exit_code'] == 0
for key in ('argv', 'cwd', 'env'):
    assert failed[key] == passed[key]
review = record('independent/combination06-review/check-result.json')
assert review['outcome'] == 'PASS' and review['candidate_count'] == 26 and review['unchanged_from_input04'] == 24
assert review['manifest_sha256'] == sha(original('author/input06/manifest.json'))
submission = original('independent/combination06-review/submission-26.sha256').decode().splitlines()
assert {x.split(maxsplit=1)[1].strip(): x.split(maxsplit=1)[0] for x in submission} == final_input['files']
old_input = manifests['author/input04/manifest.json']
changed = {p for p, h in final_input['files'].items() if old_input['files'][p] != h}
assert changed == set(review['changed_tests']) and len(changed) == 2
assert record('independent/account/verification-report.json')['status'] == 'PASS_ACCOUNT_DOMAIN'
assert index['runs']['independent/account/runs/independent01']['exit'] == 0
assert index['runs']['independent/account/runs/independent01']['seconds'] == 160.685
assert index['runs']['author/runs/combination-navigation06']['seconds'] == 81.887
visual = record('author/runs/combination-navigation06/visual-review.json')
assert visual['author_actually_viewed_all'] and not visual['root_image_view_claimed'] and len(visual['images']) == 16
for row in visual['images']:
    assert sha(original('author/runs/combination-navigation06/images/' + Path(row['path']).name)) == row['sha256']
checks = record('author/final-delivery01/checks.json')
assert checks['scope_card_exact27'] and checks['frozen26_sources_unchanged'] and checks['frozen33_dist_unchanged']
c = index['counts']
assert c['logical_artifacts'] == len(index['artifacts']) and c['unique_objects'] == len(objects)
assert c['local_objects'] == len(local) and c['reused_objects'] == len(objects) - len(local)
assert c['original_runs'] == len(index['runs']) == 64
assert c['real_rounds'] == sum(x['kind'] == 'real' for x in index['runs'].values()) == 10
print(f"PASS: {c['logical_artifacts']} logical originals / {c['local_objects']} local + {c['reused_objects']} reused SHA objects / 27 Git delivery paths / 1051 final baseline files / 64 original runs including 10 real rounds; one declared historical browser prestate gap retained; no product execution")
