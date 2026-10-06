#!/usr/bin/env python3
"""Offline bytes, fixed Git provenance and original results; never execute a product test."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[3]
PRODUCT = 'debbb28deb7c883fd0b6b77a75354b9b5d7ece0b'
BASE = 'fd32120eba4c76f67b649248377f4825a78d5d79'
SPEC = 'e4848151fabf50ee94087d5577fd948341379766'
TECH = 'f1ee727039ff8724ed83764e6149e6df052950e1a26500a36762c4817376fbe6'


def sha(data):
    return hashlib.sha256(data).hexdigest()


index = json.loads((HERE / 'index.json').read_bytes())
assert index['accepted_product_commit'] == PRODUCT and index['product_base'] == BASE
assert index['spec_commit'] == SPEC and index['technical_sha256'] == TECH
objects = {}
assert {p.name for p in (HERE / 'objects').iterdir()} == set(index['objects'])
for value, row in index['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', value)
    raw = (HERE / 'objects' / value).read_bytes()
    assert sha(raw) == value and len(raw) == row['bytes'], value
    objects[value] = raw
for name, row in index['artifacts'].items():
    assert len(objects[row['sha256']]) == row['bytes'], name


def original(name):
    return objects[index['artifacts'][name]['sha256']]


def record(name):
    return json.loads(original(name))


manifest = record('author/input01/manifest.json')
stage = record('author/web-stage01/manifest.json')
author = record('author/author-report01/index.json')
independent = record('independent/verification-report-v2.json')
assert manifest['files'] == author['candidate_files'] == independent['delivery_paths'] == index['delivery']
assert len(manifest['files']) == 4 and len(manifest['dist']) == 31
assert all(manifest['files'][p] == h for p, h in stage['files'].items())
baseline = manifest['accepted_baseline']['files']
assert len(baseline) == index['git_baseline']['count'] == 1033
paths = sorted(set(baseline) | set(index['git_baseline']['extra_paths']))
refs = [BASE + ':' + p for p in paths] + [PRODUCT + ':' + p for p in manifest['files']] + [SPEC + ':' + index['card']]
env = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_TERMINAL_PROMPT='0')
raw = subprocess.run(['git', 'cat-file', '--batch'], input=('\n'.join(refs) + '\n').encode(),
                     cwd=REPO, env=env, check=True, capture_output=True).stdout
git = {}
offset = 0
for ref in refs:
    end = raw.index(b'\n', offset)
    head = raw[offset:end].decode().split()
    assert len(head) == 3 and head[1] == 'blob', ref
    size = int(head[2])
    offset = end + 1
    git[ref] = raw[offset:offset + size]
    offset += size + 1
assert offset == len(raw)
for path, row in baseline.items():
    expected = row['sha256'] if isinstance(row, dict) else row
    assert sha(git[BASE + ':' + path]) == expected, path
for path, expected in manifest['files'].items():
    assert sha(git[PRODUCT + ':' + path]) == expected
    assert sha(original('author/input01/source/' + path)) == expected
assert original('author/card.md') == git[SPEC + ':' + index['card']]
for card in (original('author/card.md'), (REPO / index['card']).read_bytes()):
    assert sha(card[card.index(b'## 1.'):]) == TECH

for prefix, row in index['runs'].items():
    result = record(prefix + '/result.json')
    log = original(prefix + '/raw.log')
    kind = row['kind']
    if kind == 'author-offline':
        assert result['actual_wait'] and result['inputs_unchanged'] and result['account_input04_unchanged']
        assert result['resource_execution'] is False
        assert result['exit_code'] == row['exit'] and result['seconds'] == row['seconds']
        assert result['argv'] and result['cwd'] and isinstance(result['env'], dict)
        inputs = record(prefix + '/input.json')
        overrides = index['author_source_overrides'][prefix]
        for path, expected in inputs.items():
            if path in overrides:
                assert overrides[path]['sha256'] == expected and sha(objects[expected]) == expected
            else:
                assert sha(git[BASE + ':' + path]) == expected
        source = author['checks'][prefix.rsplit('/', 1)[1]]['hashes']
        for name, expected in source.items():
            assert sha(original(prefix + '/' + name)) == expected
    elif kind in ('independent-pure', 'independent-preparation'):
        command = record(prefix + '/command.json')
        assert command['argv'] and command['cwd']
        assert result['actual_wait'] and result['inputs_unchanged'] and result['no_real_resources']
        assert result['raw_sha256'] == sha(log)
        assert result.get('exit_code', result.get('exit')) == row['exit']
        assert result.get('elapsed_seconds', result.get('seconds')) == row['seconds']
        if kind == 'independent-pure':
            assert result['before'] == result['after']
            assert result['adopted_waits'] == []
            name = prefix.rsplit('/', 1)[1]
            declared = record('independent/web-stage01-report.json')['runs'][name]
            assert declared['node_pid_absent_at_review']
            assert sha(original(prefix + '/command.json')) == declared['command_sha256']
        else:
            assert result['private_before'] == result['private_after']
    else:
        assert kind == 'real'
        command = record(prefix + '/command.json')
        assert command['actual_wait_completed'] and command['exit'] == result['driver_exit'] == row['exit'] == 0
        assert command['seconds'] == row['seconds']
        assert result['selected_tests_passed'] and result['top_levels'] == row['top_levels']
        assert all(result[k] for k in ('source_files_unchanged', 'verification_inputs_unchanged',
                                      'accepted_baseline_before', 'accepted_baseline_after', 'double_cleanup'))
        assert result['monitor_errors'] == 0 and not record(prefix + '/monitor-errors.json')
        before = record(prefix + '/input-before.json')
        assert before == record(prefix + '/input-after.json')
        for key in ('files', 'dist', 'dependencies', 'locked_runtime', 'preserved_account'):
            assert before[key] == manifest[key], (prefix, key)
        assert before['accepted_baseline']['accepted'] and before['accepted_baseline']['accepted_count'] == 1033
        assert before['accepted_baseline']['files'] == {p: v['sha256'] if isinstance(v, dict) else v for p, v in baseline.items()}
        resources = record(prefix + '/observed-resources.json')
        processes = record(prefix + '/observed-processes.json')
        waits = record(prefix + '/adopted-waits.json')
        cleanup = record(prefix + '/cleanup.json')
        assert len(resources) == result['observed_resources'] == 7 and len(cleanup) == 2
        assert len(processes) == result['observed_processes'] == row['owned_processes']
        assert len(waits) == result['adopted_waits'] == row['adopted_waits']
        assert all(x['actual_wait'] for x in waits)
        for scan in cleanup:
            assert set(scan['exact_absent']) == set(resources) and all(x['absent'] for x in scan['exact_absent'].values())
            assert scan['baseline_unchanged']
            assert not any(scan[k] for k in ('owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'))
        if prefix.startswith('author/'):
            assert record(prefix + '/frozen-input.json') == manifest
            assert sha(original(prefix + '/driver.py.txt')) == manifest['driver_sha256']
        else:
            execution = record('independent/real01/execution01.json')
            assert record(prefix + '/frozen-input.json') == execution
            assert sha(original(prefix + '/driver.py.txt')) == execution['driver_sha256']
            assert before['private_inputs'] == execution['private_inputs']

# Rebuild the bounded independent pure closure from fixed Git plus exact private bytes.
for name in ('input01.json', 'input02.json'):
    source = record('independent/' + name)
    for item in source['files']:
        path, expected = item['path'], item['sha256']
        actual = manifest['files'][path] if path in manifest['files'] else sha(git[BASE + ':' + path])
        assert actual == expected
    for path, expected in source['private_files'].items():
        assert sha(original('independent/private/' + path)) == expected
execution = record('independent/real01/execution01.json')
for path, expected in execution['private_inputs']['files'].items():
    marker = '/agenteam-selection-cancel-independent-4zz3f7ut/'
    if marker in path:
        assert sha(original('independent/' + path.split(marker, 1)[1])) == expected

assert b'1044 passed (1044)' in original('author/runs/check02/raw.log')
assert b'2 failed | 17 passed (19)' in original('author/runs/pure-red01/raw.log')
assert sha(original('independent/verification-report-v2.md')) == independent['markdown_sha256']
for origin, expected in independent['supersedes'].items():
    assert sha(original('independent/' + Path(origin).name)) == expected
assert b'Root read that report but did not view' in original('independent/verification-report-v2.json')
visual = record('author/runs/navigation01/visual-review.json')
assert visual['inspected_all_eight'] and len(visual['images']) == 8
for name, expected in visual['images'].items():
    assert sha(original('author/runs/navigation01/images/' + name)) == expected
for name, expected in [('oldselection01', 1), ('oldselection02', 0)]:
    prefix = 'history/account/' + name
    assert record(prefix + '/command.json')['exit'] == record(prefix + '/result.json')['driver_exit'] == expected
    assert record(prefix + '/command.json')['actual_wait_completed']
counts = index['counts']
assert counts['logical_artifacts'] == len(index['artifacts'])
assert counts['unique_objects'] == len(objects)
assert counts['object_bytes'] == sum(len(x) for x in objects.values())
assert counts['original_checks'] == len(index['runs']) == 26
assert sum(x['kind'] == 'real' for x in index['runs'].values()) == counts['real_rounds'] == 3
print(f"PASS: {len(index['artifacts'])} logical originals / {len(objects)} SHA objects / 4 Git delivery paths / 1033 fixed baseline files / 26 original checks including 3 real rounds; bytes and original cleanup only, no product execution")
