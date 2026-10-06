#!/usr/bin/env python3
"""Offline original-byte/Git/recorded-result checks; never run archived code."""
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
PRODUCT = '213cf5c3f552e6b05b541ce02afc1dd65ce9db93'
BASE = '628612cdfc730cc1d89cad4e24a5d20f36cc6812'
TECH = 'bee4f7ddb2b480b8c57b7d1b6b81be36bfce9f726222ebc10c8b468b90a54cda'


def digest(data):
    return hashlib.sha256(data).hexdigest()


assert INDEX['accepted_product_commit'] == PRODUCT
assert INDEX['real_product_base'] == BASE and INDEX['technical_sha256'] == TECH
OBJECTS = {}
for value, meta in INDEX['objects'].items():
    assert re.fullmatch(r'[0-9a-f]{64}', value)
    assert meta['path'] == 'objects/' + value
    path = HERE / meta['path']
    assert not path.is_symlink()
    data = path.read_bytes()
    assert digest(data) == value and len(data) == meta['bytes'], path
    OBJECTS[value] = data
assert {p.name for p in (HERE / 'objects').iterdir()} == set(OBJECTS)
assert {v['sha256'] for v in INDEX['artifacts'].values() if 'object' in v} == set(OBJECTS)
GIT = {}


def blob(name):
    meta = INDEX['artifacts'][name]
    if 'git' in meta:
        return GIT[(meta['git']['commit'], meta['git']['path'])]
    return OBJECTS[meta['sha256']]


def item(name):
    return json.loads(blob(name))


delivery = item('author/final17/manifest.json')
assert delivery['files'] == INDEX['delivery'] and len(delivery['files']) == 17
stages = {n: item('author/' + n + '/manifest.json') for n in ['input01', 'input02', 'input03', 'input04']}
final = stages['input04']
assert {p: s for p, s in delivery['files'].items() if p != 'docs/development/frontend/README.md'} == final['files']
assert item('independent-real/delivery16.json')['files'] == final['files']
requests = {(PRODUCT, p): s for p, s in delivery['files'].items()}
document_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
                  'docs/development/agent-team/recovery-2026-10-06-continuation.md',
                  INDEX['card'], 'docs/development/frontend/README.md']
for p in document_paths:
    requests.setdefault((PRODUCT, p), None)
for stage in stages.values():
    assert len(stage['files']) == 16 and len(stage['dist']) == 36
    assert stage['product_base'] == BASE
    assert len(stage['accepted_baseline']['files']) == 1083
    for p, meta in stage['accepted_baseline']['files'].items():
        requests[(BASE, p)] = meta['sha256']
for p, meta in INDEX['additional_stage_git'].items():
    requests[(meta['commit'], p)] = meta['sha256']
for meta in INDEX['artifacts'].values():
    if 'git' in meta:
        requests[(meta['git']['commit'], meta['git']['path'])] = meta['sha256']
env = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')
changed = subprocess.run(['git', 'diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT],
                         cwd=ROOT, env=env, check=True, stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE).stdout.decode().splitlines()
assert set(changed) == set(delivery['files'])
order = list(requests)
raw = subprocess.run(['git', 'cat-file', '--batch'], cwd=ROOT, env=env, check=True,
                     input=''.join(f'{c}:{p}\n' for c, p in order).encode(),
                     stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout
offset = 0
for key in order:
    end = raw.index(b'\n', offset)
    header = raw[offset:end].split()
    assert len(header) == 3 and header[1] == b'blob', (key, header)
    size = int(header[2])
    data = raw[end + 1:end + 1 + size]
    offset = end + size + 2
    assert len(data) == size and raw[offset - 1:offset] == b'\n'
    if requests[key]:
        assert digest(data) == requests[key], key
    GIT[key] = data
assert offset == len(raw)
for name, meta in INDEX['artifacts'].items():
    data = blob(name)
    assert digest(data) == meta['sha256'] and len(data) == meta['bytes'], name
    assert '/runtime/' not in meta['origin'] and '/node_modules/' not in meta['origin']
known = set(OBJECTS) | {digest(v) for v in GIT.values()}
for path, variants in INDEX['source_versions'].items():
    for value, meta in variants.items():
        assert digest(blob(meta['artifact'])) == value and meta['consumers'], path
for stage in stages.values():
    assert set(stage['files'].values()) <= known
assert len(INDEX['missing_historical_sources']) == 4
assert all(v['sha256'] not in known and v['run'] == 'page-type01' for v in INDEX['missing_historical_sources'])
assert stages['input02']['accepted_baseline'] == stages['input03']['accepted_baseline'] == final['accepted_baseline']
assert [p for p, s in stages['input03']['files'].items() if s != stages['input02']['files'][p]] == ['tests/account-captcha-web/e2e/system-smtp-delivery.spec.ts']
assert set(p for p, s in final['files'].items() if s != stages['input03']['files'][p]) == {
    'web/src/composables/useSystemSMTPDelivery.ts', 'web/src/tests/system-smtp-delivery.spec.ts'}

for prefix, expected in INDEX['real_runs'].items():
    command, result = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert command['actual_wait_completed'] and command['exit'] == result['driver_exit'] == expected['actual_exit']
    assert command['seconds'] == expected['seconds']
    assert command['argv'] and command['cwd'] and isinstance(command['env'], dict)
    output = blob(prefix + '/raw.log')
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL): ([^\s(]+)', output, re.M)]
    assert tops == result['top_levels'] == expected['top_levels'], prefix
    assert result['selected_tests_passed'] == all(s == 'PASS' for s, _ in tops)
    for key in ['accepted_baseline_before', 'accepted_baseline_after', 'double_cleanup',
                'source_files_unchanged', 'verification_inputs_unchanged', 'browser_runtime_removed']:
        assert result[key] is True, (prefix, key)
    assert result['monitor_errors'] == 0
    before = item(prefix + '/input-before.json')
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert len(before['files']) == 16 and len(before['dist']) == 36
    assert set(before['files'].values()) <= known
    for p, value in before['accepted_baseline']['files'].items():
        assert digest(GIT[(BASE, p)]) == value
    assert item(prefix + '/verification-input.json')['driver_sha256'] == digest(blob(prefix + '/driver.py.txt'))
    resources, processes = item(prefix + '/observed-resources.json'), item(prefix + '/observed-processes.json')
    assert len(resources) == result['observed_resources'] == expected['exact_resources']
    assert len(processes) == result['observed_processes'] == expected['owned_pid_starttime']
    for key, value in processes.items():
        assert key == f'{value["pid"]}:{value["starttime"]}'
    baseline = item(prefix + '/baseline.json')
    assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    assert set(resources).isdisjoint(baseline)
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert all(scan[k] == [] for k in ['owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'])
    waits = item(prefix + '/adopted-waits.json')
    assert len(waits) == result['adopted_waits'] == expected['adopted_waits']
    assert all(v['actual_wait'] for v in waits)
    assert item(prefix + '/monitor-errors.json') == []
assert len(INDEX['real_runs']) == 11

nonzero = item('author/author-result01/manifest.json')['all_original_nonzero_author_runs']
assert nonzero == INDEX['original_author_nonzero'] and len(nonzero) == 14
for name, rec in nonzero.items():
    prefix = 'author/runs/' + name
    result = item(prefix + '/result.json')
    value = result.get('exit_code', result.get('driver_exit'))
    assert value == rec['exit_code'] != 0, name
    for fname, value in rec['evidence'].items():
        assert digest(blob(prefix + '/' + fname)) == value['sha256']

offline_records = 0
for name in INDEX['artifacts']:
    if not name.endswith('/result.json'):
        continue
    result = item(name)
    if not isinstance(result, dict):
        continue
    prefix = name[:-12]
    if prefix + '/raw.log' in INDEX['artifacts'] and 'raw_sha256' in result:
        assert digest(blob(prefix + '/raw.log')) == result['raw_sha256'], name
        offline_records += 1
    for key, fname in [('input_before_sha256', 'input-before.json'),
                       ('input_after_sha256', 'input-after.json'),
                       ('command_sha256', 'command.json')]:
        if key in result:
            assert digest(blob(prefix + '/' + fname)) == result[key], name
    if name.startswith('author/runs/') and 'actual_wait' in result:
        assert result['actual_wait'] and result['argv'] and result['cwd'] and isinstance(result['env'], dict)
        if not result['input_unchanged']:
            assert 'format-write' in name, name

prefix = 'independent-real/runs/independent-real01'
assert 'exit' not in item(prefix + '/command.json') and 'actual_wait_completed' not in item(prefix + '/command.json')
for name in INDEX['original_real01_missing_files']:
    assert prefix + '/' + name not in INDEX['artifacts']
snapshot = item('independent-real/recovery01/snapshot01.json')
assert snapshot['original_completion_files_missing'] == INDEX['original_real01_missing_files']
for name, value in snapshot['original_files'].items():
    assert digest(blob(prefix + '/' + name)) == value
assert re.findall(rb'^--- PASS: (TestIndependentSMTPDelivery\w+)', blob(prefix + '/raw.log'), re.M) == [
    b'TestIndependentSMTPDeliveryTestReplay', b'TestIndependentSMTPDeliveryRetryRecovery']
assert len(snapshot['baseline_missing']) == 1 and snapshot['original_actual_wait_recorded'] is False
recovery_prefix = 'independent-real/recovery01/'
commands = item(recovery_prefix + 'cleanup-commands01.json')
assert len(commands) == 7 and all(v['actual_wait_completed'] and v['exit'] == 0 for v in commands)
assert item(recovery_prefix + 'cleanup-result01.json')['recovery_cleanup_actual_exit'] == 1
confirmation = item(recovery_prefix + 'verification-command02.json')
assert confirmation['exit'] == 0 and confirmation['actual_wait_completed']
scans = item(recovery_prefix + 'verification-scans02.json')
assert len(scans) == 2
for scan in scans:
    assert len(scan['exact_owned_ids']) == 7 and all(v['absent'] for v in scan['exact_owned_ids'].values())
    assert len(scan['associated_anonymous_volumes']) == 2
    assert all(v['absent'] and v['absent_from_successful_volume_list'] for v in scan['associated_anonymous_volumes'].values())
assert item(recovery_prefix + 'verification-result02.json')['original_real01_not_completed_or_backfilled']
for n in ['independent-real01', 'independent-real02']:
    p = 'independent-real/runs/' + n
    assert blob(p + '/frozen-input.json') == blob('independent-real/execution01.json')
    assert blob(p + '/driver.py.txt') == blob('independent-real/driver01.py')
assert blob(prefix + '/input-before.json') == blob('independent-real/runs/independent-real02/input-before.json')

old = {p: GIT[(PRODUCT, p)] for p in document_paths}
new = {p: (DOCS / p).read_bytes() for p in document_paths}
assert digest(old[INDEX['card']][old[INDEX['card']].index(b'## 1.'):]) == TECH
assert digest(new[INDEX['card']][new[INDEX['card']].index(b'## 1.'):]) == TECH
op, np = old[INDEX['card']].split(b'\n\n'), new[INDEX['card']].split(b'\n\n')
assert op[:1] == np[:1] and op[2:] == np[2:]
continuation = 'docs/development/agent-team/recovery-2026-10-06-continuation.md'
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].startswith(b'\n## 26.')
for path, marker in [('AGENTS.md', b'The [System SMTP delivery UI specification]'),
                     ('docs/development/agent-team/tasks.md', b'## ')]:
    point = old[path].index(marker)
    assert new[path].startswith(old[path][:point]) and new[path].endswith(old[path][point:])
readme = 'docs/development/frontend/README.md'
addition = ' 完整来源、分版本复用和恢复边界见[SMTP投递验收记录](../agent-team/system-smtp-delivery-ui-verification.md)。'.encode()
assert new[readme].count(addition) == 1 and new[readme].replace(addition, b'', 1) == old[readme]

checked_links = 0
report_path = INDEX['report']
for path in document_paths + [report_path]:
    data = (DOCS / path).read_bytes()
    assert b'\r' not in data and data.endswith(b'\n')
    assert not any(line.endswith((b' ', b'\t')) for line in data.splitlines())
    text = data.decode()
    # Check newly introduced links only in historical append-only documents.
    old_links = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old.get(path, b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', text):
        if target in old_links or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
            continue
        target = target.strip('<>')
        filename, _, fragment = target.partition('#')
        relative = (Path(path).parent / unquote(filename)).as_posix() if filename else path
        candidate = (DOCS / relative).resolve()
        if not candidate.exists():
            candidate = (ROOT / relative).resolve()
        assert candidate.is_file(), (path, target)
        if fragment:
            headings = re.findall(r'^#+\s+(.+?)\s*#*$', candidate.read_text(), re.M)
            slugs = {re.sub(r'[^\w\-\s]', '', h.lower()).replace(' ', '-') for h in headings}
            assert unquote(fragment) in slugs, (path, target)
        checked_links += 1
assert len(INDEX['artifacts']) == INDEX['counts']['logical_artifacts']
assert len(OBJECTS) == INDEX['counts']['objects']
print(json.dumps({'result': 'PASS; immutable bytes/Git/recorded outcomes only; no product execution',
                  'logical_artifacts': len(INDEX['artifacts']), 'objects': len(OBJECTS),
                  'git_requests': len(GIT), 'product_paths': 17, 'accepted_dependencies': 1083,
                  'dist_fingerprints_only': 36, 'complete_real_commands': 11,
                  'interrupted_original_real_commands': 1, 'raw_result_pairs': offline_records,
                  'new_local_links_checked': checked_links}, ensure_ascii=False))
