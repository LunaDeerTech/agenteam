#!/usr/bin/env python3
"""Offline original-byte/Git/recorded-outcome checks. Never execute archived code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote


HERE = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', type=Path, default=HERE.parents[3])
parser.add_argument('--documents', type=Path)
args = parser.parse_args()
ROOT = args.repo.resolve()
DOCS = (args.documents or ROOT).resolve()
INDEX = json.loads((HERE / 'index.json').read_bytes())
PRODUCT = '1ff044264a54c948e512db9aafad24ec5e0aa3c2'
BASE = '213cf5c3f552e6b05b541ce02afc1dd65ce9db93'
TECH = '343f09b6facd95c000df2f2b35bceeb73f3a7e93d2974ce18304703266d0ab6b'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(argv, data=None):
    return subprocess.run(['git', *argv], cwd=ROOT, env=ENV, check=True,
                          input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


assert INDEX['accepted_product_commit'] == PRODUCT
assert INDEX['real_product_base'] == BASE and INDEX['technical_sha256'] == TECH
OBJECTS = {}
for value, meta in INDEX['objects'].items():
    assert re.fullmatch(r'[0-9a-f]{64}', value) and meta['path'] == 'objects/' + value
    path = HERE / meta['path']
    assert not path.is_symlink()
    data = path.read_bytes()
    assert digest(data) == value and len(data) == meta['bytes'], path
    OBJECTS[value] = data
assert {p.name for p in (HERE / 'objects').iterdir()} == set(OBJECTS)
assert {v['sha256'] for v in INDEX['artifacts'].values() if 'object' in v} == set(OBJECTS)
GIT = {}


def read_git(requests):
    order = [key for key in requests if key not in GIT]
    raw = git(['cat-file', '--batch'], ''.join(f'{c}:{p}\n' for c, p in order).encode())
    offset = 0
    for key in order:
        end = raw.index(b'\n', offset)
        header = raw[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', (key, header)
        size = int(header[2])
        data = raw[end + 1:end + 1 + size]
        offset = end + size + 2
        assert len(data) == size and raw[offset - 1:offset] == b'\n'
        GIT[key] = data
    assert offset == len(raw)
    for key, expected in requests.items():
        if expected is not None:
            assert digest(GIT[key]) == expected, key


def blob(name):
    meta = INDEX['artifacts'][name]
    if 'git' in meta:
        return GIT[(meta['git']['commit'], meta['git']['path'])]
    assert meta['object'] == 'objects/' + meta['sha256']
    return OBJECTS[meta['sha256']]


def item(name):
    return json.loads(blob(name))


def sha(value):
    return value if isinstance(value, str) else value['sha256']


requests = {(m['git']['commit'], m['git']['path']): m['sha256']
            for m in INDEX['artifacts'].values() if 'git' in m}
read_git(requests)
for name, meta in INDEX['artifacts'].items():
    data = blob(name)
    assert digest(data) == meta['sha256'] and len(data) == meta['bytes'], name
    assert not any(p in Path(meta['origin']).parts for p in ['node_modules', 'runtime', 'gocache', 'modcache'])
delivery = item('author/final34/manifest.json')
assert delivery['files'] == INDEX['delivery'] and len(delivery['files']) == 34
stages = {name: item('author/' + name + '/manifest.json')
          for name in ['input01', 'input02', 'input03', 'input04', 'input05']}
final = stages['input05']
assert {p: h for p, h in delivery['files'].items() if p != 'docs/development/frontend/README.md'} == final['files']
assert {v['path']: v['sha256'] for v in item('independent-real/delivery33.json')['files']} == final['files']
assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT]).decode().splitlines()) == set(delivery['files'])
requests = {(PRODUCT, p): h for p, h in delivery['files'].items()}
for rec in INDEX['git_input_manifests']:
    value = item(rec['artifact'])
    for field in rec['field']:
        value = value[field]
    for path, meta in value.items():
        requests[(rec['commit'], path)] = sha(meta)
readme = INDEX['readme_before_git']
requests[(readme['commit'], readme['path'])] = readme['sha256']
assert readme['commit'] == '2cefd225bf179d166d4fa8b912262712226dd571'
assert readme['sha256'] == delivery['readme_before']['sha256']
document_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
                  'docs/development/agent-team/recovery-2026-10-06-continuation.md', INDEX['card']]
for path in document_paths:
    requests[(PRODUCT, path)] = None
read_git(requests)
known = set(OBJECTS) | {digest(b) for b in GIT.values()}
for path, versions in INDEX['source_versions'].items():
    for expected, rec in versions.items():
        assert digest(blob(rec['artifact'])) == expected, path
assert len(INDEX['source_versions']) == 34
assert sum(map(len, INDEX['source_versions'].values())) == 70
assert INDEX['missing_historical_sources'] == []
for name, stage in stages.items():
    assert len(stage['files']) == 33 and len(stage['dist']) == 38
    assert stage['product_base'] == BASE and set(stage['files'].values()) <= known
    assert stage['dist'] == final['dist']
    for path, meta in stage['accepted_baseline']['files'].items():
        assert digest(GIT[(BASE, path)]) == sha(meta), (name, path)
assert len(final['accepted_baseline']['files']) == 1014
assert stages['input02']['files'] == stages['input03']['files']
browser = 'tests/account-captcha-web/e2e/system-outbound-policy.spec.ts'
for old_name, new_name in [('input03', 'input04'), ('input04', 'input05')]:
    before, after = stages[old_name], stages[new_name]
    assert {p for p in after['files'] if before['files'][p] != after['files'][p]} == {browser}
    assert before['driver_sha256'] == after['driver_sha256']
web = item('author/web-stage01/manifest.json')
assert len(web['files']) == 22 and all(final['files'][p] == h for p, h in web['files'].items())
assert {'web/dist/' + p: h for p, h in web['dist'].items()} == final['dist']
for name, field in [('api-stage01', 'files'), ('owner-stage01', 'candidate_sources')]:
    stage = item('author/' + name + '/manifest.json')
    assert all(final['files'][p] == h for p, h in stage[field].items())
for name in ['harness-stage01', 'harness-stage02']:
    assert set(item('author/' + name + '/manifest.json')['files'].values()) <= known

# Recorded outcomes, not new product execution. The read01 outer failure is preserved.
for prefix, expected in INDEX['real_runs'].items():
    command, result = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert command['actual_wait_completed'] and command['exit'] == result['driver_exit'] == expected['command_exit']
    assert command['seconds'] == expected['seconds']
    assert command['argv'] and command['cwd'] and isinstance(command['env'], dict)
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL): ([^\s(]+)', blob(prefix + '/raw.log'), re.M)]
    assert tops == result['top_levels'] == expected['top_levels'], prefix
    assert result['selected_tests_passed'] == all(s == 'PASS' for s, _ in tops)
    for key in ['accepted_baseline_before', 'accepted_baseline_after', 'source_files_unchanged',
                'verification_inputs_unchanged', 'browser_runtime_removed', 'resource_ownership_valid']:
        assert result[key] is True, (prefix, key)
    assert result['monitor_errors'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    assert before['product_base'] == BASE and len(before['files']) == 33 and before['dist'] == final['dist']
    assert set(before['files'].values()) <= known
    assert set(before.get('private_files', {}).values()) <= known
    for path, meta in before['accepted_baseline']['files'].items():
        assert digest(GIT[(BASE, path)]) == sha(meta)
    assert item(prefix + '/verification-input.json')['driver_sha256'] == digest(blob(prefix + '/driver.py.txt'))
    resources, processes = item(prefix + '/observed-resources.json'), item(prefix + '/observed-processes.json')
    assert len(resources) == result['observed_resources'] == expected['exact_resources']
    assert len(processes) == result['observed_processes'] == expected['owned_pid_start']
    assert all(k == f'{v["pid"]}:{v["starttime"]}' for k, v in processes.items())
    baseline = item(prefix + '/baseline.json')
    assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    assert set(resources).isdisjoint(baseline)
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    is_read = prefix == 'author/runs/read01'
    assert [s['baseline_unchanged'] for s in scans] == ([False, True] if is_read else [True, True])
    assert result['double_cleanup'] == expected['double_cleanup'] == (not is_read)
    assert expected['outer_driver_exit'] == (1 if is_read else command['exit'])
    for scan in scans:
        assert set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert all(scan[k] == [] for k in ['owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'])
    waits = item(prefix + '/adopted-waits.json')
    assert len(waits) == result['adopted_waits'] == expected['adopted_actual_waits']
    assert all(v['actual_wait'] for v in waits)
    assert item(prefix + '/monitor-errors.json') == []
assert len(INDEX['real_runs']) == 12
for name, expected in [('read01-cleanup-followup01', 1), ('read01-cleanup-followup02', 0)]:
    result = item('author/runs/' + name + '/result.json')
    assert result['exit'] == expected and result['read_commands_actual_wait_completed']
    assert result['original_command_exit'] == 0 and result['original_driver_tool_exit'] == 1
    assert result['original_evidence_unchanged']
for scan in item('author/runs/read01-cleanup-followup02/sweeps.json'):
    assert all(scan[k] for k in ['all_100_pid_starttime_absent', 'baseline_exact_six_unchanged',
                                'original_seven_absent', 'runtime_empty_or_absent'])

for prefix, expected in INDEX['independent_offline'].items():
    command, result, cleanup = (item(prefix + '/' + name + '.json') for name in ['command', 'result', 'cleanup'])
    assert command['actual_wait'] and result['actual_wait'] and cleanup['child_actual_wait']
    assert command['actual_exit'] == result['actual_exit'] == expected['actual_exit']
    assert command['seconds'] == result['seconds'] == expected['seconds']
    assert command['argv'] and command['cwd'] and isinstance(command['env_overrides'], dict)
    assert command['resource_execution'] is False and result['timeout'] is False
    assert all(result[k] for k in ['input_before_matches', 'input_after_matches', 'input_unchanged', 'cleanup_clean'])
    assert digest(blob(prefix + '/raw.log')) == result['raw_sha256']
    assert digest(blob(prefix + '/cleanup.json')) == result['cleanup_sha256']
    assert result['owned_processes_observed'] == expected['owned_processes']
    assert result['adopted_wait_count'] == len(cleanup['adopted_waits']) == 0
    for name in ['scan1', 'scan2']:
        assert cleanup[name]['known_pid_starttime_remaining'] == cleanup[name]['owned_descendants_remaining'] == []
    assert cleanup['clean'] and cleanup['tmp_removed']
    assert not cleanup['services_or_listeners_started'] and not cleanup['docker_or_browser_started']
assert len(INDEX['independent_offline']) == 14
raw_pairs = 0
for name in INDEX['artifacts']:
    if not name.endswith('/result.json'):
        continue
    value = item(name)
    if not isinstance(value, dict):
        continue
    prefix = name[:-12]
    for key, filename in [('raw_sha256', 'raw.log'), ('input_before_sha256', 'input-before.json'),
                          ('input_after_sha256', 'input-after.json'), ('command_sha256', 'command.json')]:
        target = prefix + '/' + filename
        if key in value and target in INDEX['artifacts']:
            assert digest(blob(target)) == value[key], name
            raw_pairs += key == 'raw_sha256'
    if name.startswith('author/runs/') and 'actual_wait' in value:
        assert value['actual_wait'] and value['argv'] and value['cwd'] and isinstance(value['env'], dict)
for name, code in INDEX['original_author_nonzero'].items():
    value = item(name)
    assert value.get('actual_exit', value.get('exit_code', value.get('exit'))) == code != 0
before = item('author/runs/page-state01/input-before.json')
after = item('author/runs/page-state02/input-before.json')
assert {p for p in after if before[p] != after[p]} == {
    'web/src/composables/useSystemOutboundPolicy.ts', 'web/src/views/system/SystemOutboundPolicyView.vue'}
assert set(before.values()) <= known and set(after.values()) <= known
assert before['web/src/tests/system-outbound-policy.spec.ts'] == after['web/src/tests/system-outbound-policy.spec.ts']
preservation = item('independent-real/repair02/source-preservation.json')
old_spec = blob('independent-real/repair02/original.spec.ts')
new_spec = blob('independent-real/browser/independent.spec.ts')
assert digest(old_spec) == preservation['sha256'] and digest(new_spec) == preservation['new_spec_sha256']
common = 0
while common < min(len(old_spec), len(new_spec)) and old_spec[common] == new_spec[common]:
    common += 1
# The archived prefix is an exact leading segment, ending before the B-only edit.
assert preservation['A_and_all_helpers_byte_identical'] and preservation['fixture_go_config_driver_unchanged']
assert preservation['matches_independent01_input_before_after'] and preservation['no_reload_or_goto_added']
assert common > 0

# Only append/header changes to historical documents. Never compare active product sources.
old = {p: GIT[(PRODUCT, p)] for p in document_paths}
new = {p: (DOCS / p).read_bytes() for p in document_paths}
card = INDEX['card']
marker = b'## 1. '
assert new[card][new[card].index(marker):] == old[card][old[card].index(marker):]
assert digest(new[card][new[card].index(marker):]) == TECH
continuation = 'docs/development/agent-team/recovery-2026-10-06-continuation.md'
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].lstrip().startswith('## 29. '.encode())
for path in ['AGENTS.md', 'docs/development/agent-team/tasks.md']:
    point = (old[path].index(b'The [System Audit management HTTP specification]')
             if path == 'AGENTS.md' else old[path].index(b'\n\n') + 2)
    assert new[path].startswith(old[path][:point]) and new[path].endswith(old[path][point:])
checked_links = 0
for path in document_paths + [INDEX['report']]:
    data = (DOCS / path).read_bytes()
    assert b'\r' not in data and data.endswith(b'\n')
    assert not any(line.endswith((b' ', b'\t')) for line in data.splitlines())
    text = data.decode('utf-8')
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0, path
    old_links = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old.get(path, b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', text):
        if target in old_links or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
            continue
        filename, _, fragment = target.strip('<>').partition('#')
        relative = (Path(path).parent / unquote(filename)).as_posix() if filename else path
        candidate = (DOCS / relative).resolve()
        if not candidate.is_file():
            candidate = (ROOT / relative).resolve()
        assert candidate.is_file(), (path, target)
        if fragment:
            headings = re.findall(r'^#+\s+(.+?)\s*#*$', candidate.read_text(), re.M)
            slugs = {re.sub(r'[^\w\-\s]', '', h.lower()).replace(' ', '-') for h in headings}
            assert unquote(fragment) in slugs, (path, target)
        checked_links += 1
counts = INDEX['counts']
assert len(INDEX['artifacts']) == counts['logical_artifacts'] == 946
assert len(OBJECTS) == counts['objects'] == 622
assert sum(map(len, OBJECTS.values())) == counts['object_bytes'] == 42360348
assert sum('git' in v for v in INDEX['artifacts'].values()) == counts['git_backed_artifacts'] == 117
print(json.dumps({'result': 'PASS; immutable bytes/Git/recorded outcomes only; no product execution',
                  'logical_artifacts': len(INDEX['artifacts']), 'objects': len(OBJECTS),
                  'object_bytes': counts['object_bytes'], 'git_requests': len(GIT),
                  'product_paths': 34, 'source_versions': 70, 'accepted_dependencies': 1014,
                  'dist_fingerprints_only': 38, 'real_commands': 12,
                  'read_original_outer_exit': 1, 'independent_preparation_commands': 14,
                  'raw_result_pairs': raw_pairs, 'new_local_links_checked': checked_links}, ensure_ascii=False))
