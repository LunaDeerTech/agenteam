#!/usr/bin/env python3
"""Verify preserved bytes, fixed Git sources and original execution records only."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[3]
PRODUCT = 'fd32120eba4c76f67b649248377f4825a78d5d79'
BASE = '870ebbb986f56bb62be34ccdfa2c77819ce995a6'
SPEC = '3c79fd4069837c4cee0b1ed37e00480ea8f1909f'
TECH = 'e1e5606cbb493f5079aa51c72eaa46462ae80d6794059ca4db9904903e4cef75'


def sha(data):
    return hashlib.sha256(data).hexdigest()


index = json.loads((HERE / 'index.json').read_bytes())
assert index['accepted_product_commit'] == PRODUCT
assert index['component_base'] == BASE and index['spec_commit'] == SPEC
objects = {}
assert {p.name for p in (HERE / 'objects').iterdir()} == set(index['objects'])
for value, item in index['objects'].items():
    assert re.fullmatch(r'[0-9a-f]{64}', value)
    data = (HERE / 'objects' / value).read_bytes()
    assert sha(data) == value and len(data) == item['bytes'], value
    objects[value] = data
for name, item in index['artifacts'].items():
    assert len(objects[item['sha256']]) == item['bytes'], name


def original(name):
    return objects[index['artifacts'][name]['sha256']]


def record(name):
    return json.loads(original(name))


env = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_TERMINAL_PROMPT='0')
git_bytes = {}
for ref, item in index['git_references'].items():
    assert item['commit'] in {BASE, SPEC, PRODUCT}
    assert ref == item['commit'] + ':' + item['path']
    proc = subprocess.run(['git', 'cat-file', 'blob', ref], cwd=REPO, env=env, check=True, capture_output=True)
    assert sha(proc.stdout) == item['sha256'] and len(proc.stdout) == item['bytes'], ref
    git_bytes[ref] = proc.stdout

manifest = record('author/input01/manifest.json')
author = record('author/author-report.json')
assert manifest['files'] == author['files'] and len(manifest['files']) == 5
assert set(index['delivery']) == set(manifest['files'])
for path, expected in manifest['files'].items():
    binding = index['delivery'][path]
    assert binding['ref'] == PRODUCT + ':' + path and binding['sha256'] == expected
    assert sha(original('author/input01/source/' + path)) == expected
for path, expected in manifest['full_isolated_web_inputs'].items():
    actual = manifest['files'].get(path) if path in manifest['files'] else sha(git_bytes[BASE + ':' + path])
    assert actual == expected, path
assert len(manifest['full_isolated_web_inputs']) == 127
assert len(manifest['browser_closure']) == 11

expected_exits = {
    **{f'author/runs/{name}': item['result']['exit'] for name, item in author['checks'].items()},
    'independent/runs/pure01': 1, 'independent/runs/pure02': 0,
    'independent/runs/type01': 2, 'independent/runs/type02': 0,
    'independent/runs/list01': 0, 'independent/runs/browser01': 0,
}
assert len(expected_exits) == 16 and set(expected_exits) == set(index['runs'])
for prefix, expected_exit in expected_exits.items():
    before = record(prefix + '/input-before.json')
    after = record(prefix + '/input-after.json')
    assert before == after
    bound = index['run_input_bindings'][prefix]
    assert set(before) == set(bound)
    for path, expected in before.items():
        binding = bound[path]
        kind = binding['kind']
        if kind == 'object':
            assert binding['sha256'] == expected and sha(objects[expected]) == expected
        elif kind == 'git':
            assert binding['sha256'] == expected and sha(git_bytes[binding['ref']]) == expected
        elif kind == 'recorded-runtime-fingerprint':
            assert binding['sha256'] == expected
            assert index['recorded_runtime_fingerprints'][path] == expected
        else:
            assert kind == 'recorded-runtime-count'
            assert binding['value'] == expected
            assert (path, expected) in {('runtime_verified_files', 7094), ('runtime_verified_symlinks', 28)}
    result = record(prefix + '/result.json')
    command = record(prefix + '/command.json')
    cleanup = record(prefix + '/cleanup.json')
    assert result['exit'] == expected_exit == index['runs'][prefix]['exit']
    assert result['actual_wait_completed'] and result['inputs_unchanged']
    assert result['raw_sha256'] == sha(original(prefix + '/raw.log'))
    assert result.get('seconds', result.get('elapsed_seconds')) == index['runs'][prefix]['seconds']
    assert command['argv'] and command['cwd'] and isinstance(command['env'], dict)
    if 'actual_wait_completed' in command:
        assert command['actual_wait_completed'] and command['exit'] == expected_exit
    if 'scans' in cleanup:
        assert cleanup['subreaper_before_spawn'] and cleanup['scans'] == [[], []]
        assert result['owned_two_scans_empty']
        assert len(cleanup['owned']) == result['owned_processes']
        assert len(cleanup['adopted_waits']) == result['adopted_waits']
    else:
        assert cleanup['actual_wait_completed'] and cleanup['subreaper_enabled_before_launch']
        assert cleanup['remaining_first'] == cleanup['remaining_second'] == []
        assert cleanup['tmp_absent'] and cleanup['ports_unchanged']
        if prefix == 'author/runs/old01':
            assert result['clean'] is False and cleanup['server_all_closed'] is False
            assert cleanup['servers'] == []
        else:
            assert result['clean'] is True and cleanup['server_all_closed'] is True

for prefix, count, waits in [('author/runs/old02', 13, 5), ('author/runs/new01', 145, 4),
                             ('independent/runs/browser01', 19, 4)]:
    cleanup = record(prefix + '/cleanup.json')
    assert len(cleanup['owned_processes']) == count and len(cleanup['adopted_waits']) == waits
    server = record(prefix + '/server-0.json')
    assert server['closed'] is True
    expected = record('author/baseline.json')['closure'] if prefix.endswith('/old02') else manifest['browser_closure']
    assert {'web/' + path: value for path, value in server['inputs'].items()} == expected
for prefix, expected, unexpected in [('author/runs/old01', 0, 0), ('author/runs/old02', 0, 1),
                                    ('author/runs/new01', 45, 0), ('independent/runs/browser01', 3, 0)]:
    stats = record(prefix + '/results.json')['stats']
    assert stats['expected'] == expected and stats['unexpected'] == unexpected and stats['flaky'] == 0

pure01 = original('independent/runs/pure01/raw.log').decode()
pure02 = original('independent/runs/pure02/raw.log').decode()
assert '1 failed | 7 passed (8)' in pure01 and '1 passed | 7 skipped (8)' in pure02
assert '114 passed (114)' in original('author/runs/pure02/raw.log').decode()
old_format = record('author/old02-probe-format-equivalence.json')
assert old_format['old_sha256'] != old_format['current_sha256']
assert old_format['canonical_bytes_equal'] is True
assert old_format['old_sha256'] in objects and old_format['current_sha256'] in objects

card_path = 'docs/development/work-items/d27-dialog-fallback-focus.md'
fixed_card = git_bytes[SPEC + ':' + card_path]
current_card = (REPO / card_path).read_bytes()
for data in (fixed_card, current_card):
    assert sha(data[data.index(b'## 1.'):]) == TECH
counts = index['counts']
assert counts['logical_artifacts'] == len(index['artifacts']) == 219
assert counts['unique_objects'] == len(objects) == 139
assert counts['git_references'] == len(git_bytes) == 140
assert counts['delivered_paths'] == 5
print('PASS: 219 logical originals / 139 SHA objects / 140 fixed Git references / 5 delivered paths / 16 original runs; byte and original cleanup verification only, no product execution')
