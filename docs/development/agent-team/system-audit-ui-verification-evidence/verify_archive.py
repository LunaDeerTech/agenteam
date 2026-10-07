#!/usr/bin/env python3
"""Verify immutable evidence, fixed Git and document deltas; execute no product code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
from urllib.parse import unquote

HERE = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', type=Path, default=HERE.parents[3])
parser.add_argument('--documents', type=Path)
args = parser.parse_args()
ROOT = args.repo.resolve()
DOCS = (args.documents or ROOT).resolve()
I = json.loads((HERE / 'index.json').read_bytes())
PRODUCT = 'a0e73bd8fc7fa40e1f424f5817e7b1b3b281def1'
BACK = 'b124650aee095b26191bc181dc8cf5d6e0f977f5'
FRONT = '1ff044264a54c948e512db9aafad24ec5e0aa3c2'
MAIN = '86a882472bc128cb4607898b839781c483b13b07'
TECH = 'e7c96b588eed6218030ef6ce42154c7967667ff156bd8c343d0cdd14cf2bd8d5'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(argv, data=None):
    return subprocess.run(['git', *argv], cwd=ROOT, env=ENV, check=True, input=data,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


assert I['accepted_product_commit'] == PRODUCT and I['backend_base'] == BACK
assert I['frontend_base'] == FRONT and I['main_compile_head'] == MAIN and I['technical_sha256'] == TECH
O, G = {}, {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    p = HERE / meta['path']
    assert p.is_file() and not p.is_symlink()
    b = p.read_bytes()
    assert digest(b) == h and len(b) == meta['bytes']
    O[h] = b
assert {p.name for p in (HERE / 'objects').iterdir()} == set(O)
assert {v['sha256'] for v in I['artifacts'].values() if 'object' in v} == set(O)


def read_git(requests):
    order = [key for key in requests if key not in G]
    if order:
        raw = git(['cat-file', '--batch'], ''.join(f'{c}:{p}\n' for c, p in order).encode())
        offset = 0
        for key in order:
            end = raw.index(b'\n', offset); header = raw[offset:end].split()
            assert len(header) == 3 and header[1] == b'blob', (key, header)
            n = int(header[2]); b = raw[end + 1:end + 1 + n]; offset = end + n + 2
            assert len(b) == n and raw[offset - 1:offset] == b'\n'
            G[key] = b
        assert offset == len(raw)
    for key, h in requests.items():
        assert h is None or digest(G[key]) == h, key


def blob(name):
    rec = I['artifacts'][name]
    return G[(rec['git']['commit'], rec['git']['path'])] if 'git' in rec else O[rec['sha256']]


def item(name):
    return json.loads(blob(name))


def sha(value):
    return value if isinstance(value, str) else value['sha256']


read_git({(v['git']['commit'], v['git']['path']): v['sha256'] for v in I['artifacts'].values() if 'git' in v})
for name, rec in I['artifacts'].items():
    b = blob(name)
    assert digest(b) == rec['sha256'] and len(b) == rec['bytes'], name
    assert not any(p in Path(rec['origin']).parts for p in ('node_modules', 'gocache', 'modcache', 'runtime'))
delivery = item('author/delivery36/manifest.json')
files36 = delivery['source']
assert files36 == I['delivery36'] and len(files36) == delivery['source_count'] == 36
assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT]).decode().splitlines()) == set(files36)
requests = {(PRODUCT, p): h for p, h in files36.items()}
for rec in I['git_manifest_refs']:
    values = item(rec['artifact'])
    for field in rec['fields']:
        values = values[field]
    assert len(values) == rec['count']
    for p, v in values.items():
        requests[(rec['commit'], p)] = sha(v)
main_input = item('main-compat/input.json')
local = {p.removeprefix('/workspace/agenteam/'): h for p, h in main_input['files'].items() if p.startswith('/workspace/agenteam/')}
assert len(local) == len(main_input['local_paths']) == 805
for p, h in local.items():
    requests[(PRODUCT if p in files36 else MAIN, p)] = h
doc_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
             'docs/development/agent-team/recovery-2026-10-06-continuation.md', I['card']]
for p in doc_paths:
    requests[(PRODUCT, p)] = None
read_git(requests)
known = set(O) | {digest(v) for v in G.values()}
for p, versions in I['source_versions'].items():
    for h, record in versions.items():
        assert digest(blob(record['artifact'])) == h, p

# Historical source gaps remain explicit and are never inferred from a later same-name file.
gaps = I['historical_gaps_and_limits']
missing = {r['sha256'] for r in gaps if r.get('sha256')}
assert missing == {'1a37607bc06e57148636e02a0556085e7af71678967049ab3475e448b15a8f1c',
                   'db1a27978b18792345b10adef7f753101097eb00a6b816db9a9fd389b1ed30c3'}
assert not missing & known
assert any(r['kind'] == 'historical_unfrozen_preparation_utility' and not r['exact_original_snapshot_available'] for r in gaps)
assert any(r['kind'] == 'tool_only_initial_closure_error' and not r['contemporaneous_raw_result_available'] for r in gaps)

# Original result-to-raw/command/input links, including failed and mutating format rounds.
raw_pairs, waits = 0, 0
for name in I['artifacts']:
    if not name.endswith('/result.json'):
        continue
    value = item(name)
    if not isinstance(value, dict):
        continue
    prefix = name[:-12]
    for key, filename in [('raw_sha256', 'raw.log'), ('command_sha256', 'command.json'),
                          ('input_before_sha256', 'input-before.json'), ('input_after_sha256', 'input-after.json'),
                          ('cleanup_sha256', 'cleanup.json')]:
        target = prefix + '/' + filename
        if key in value and target in I['artifacts']:
            assert digest(blob(target)) == value[key], name
            raw_pairs += key == 'raw_sha256'
    waits += bool(value.get('actual_wait') or value.get('actual_wait_completed'))
expected_author = {'api-pure01': 1, 'api-pure02': 0, 'api-pure03': 0,
                   'owner-state-type01': 2, 'owner-state-pure01': 1, 'owner-state-pure02': 0, 'owner-state-pure03': 0,
                   'page-app01': 1, 'page-app02': 0, 'page-app03': 1, 'page-app04': 0,
                   'page-check01': 0, 'old-browser-type01': 2, 'old-browser-type02': 0, 'old-browser-list01': 0}
for run, code in expected_author.items():
    value = item('author/runs/' + run + '/result.json')
    assert value['exit_code'] == code and value['actual_wait'] and value['owned_processes_double_empty'], run
for prefix, seconds in [('api-independent/runs/oracle01', 2.544462),
                        ('api-independent/runs/node01', 0.217664),
                        ('owner-independent/runs/owner01', 0.293447),
                        ('page-independent/runs/app01', 2.649925825)]:
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    code = r.get('actual_exit', r.get('exit_code', r.get('exit')))
    assert code == 0 and (r.get('actual_wait') or r.get('actual_wait_completed'))
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')

# Original real rounds: seven failures, two original cleanup=false, and separate later recovery.
assert len(I['real_rounds']) == 13
failed = 0
author_round_inputs = {
    'read01': '01', 'read02': '02', 'read03': '03',
    'authority01': '03', 'authority02': '04', 'authority03': '05',
    'navigation01': '05', 'navigation02': '06', 'navigation03': '07',
    'navigation04': '08', 'old-outbound-navigation01': '08', 'old-delivery-navigation01': '08',
}
for record in I['real_rounds']:
    prefix = record['artifact_prefix']
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert c['actual_wait_completed'] and c['exit'] == r['driver_exit'] == record['actual_exit']
    assert c['seconds'] == record['seconds'] and c['argv'] and c['cwd'] and isinstance(c['env'], dict)
    failed += c['exit'] != 0
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL|SKIP): ([^\s(]+)', blob(prefix + '/raw.log'), re.M)]
    assert tops == record['top_levels'] == r['top_levels']
    assert r['selected_tests_passed'] == (c['exit'] == 0)
    assert r['double_cleanup'] == record['original_double_cleanup']
    for k in ('accepted_baseline_before', 'accepted_baseline_after', 'source_files_unchanged', 'verification_inputs_unchanged'):
        assert r[k]
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    assert len(before['files']) == 35 and len(before['dist']) == 40
    assert set(before['files'].values()) <= known
    if prefix.endswith('read01'):
        assert len(before['accepted_baseline']['files']) == 938
    else:
        assert len(before['accepted_baseline']['files']) == 943
    vi = item(prefix + '/verification-input.json')
    assert vi['driver_sha256'] == digest(blob(prefix + '/driver.py.txt'))
    # These original records contain driver_sha256 only; bind the separately saved full input.
    binding = ('author/input' + author_round_inputs[prefix.rsplit('/', 1)[1]] + '/manifest.json'
               if prefix.startswith('author/') else 'real-independent/execution03.json')
    assert blob(prefix + '/frozen-input.json') == blob(binding)
    resources = item(prefix + '/observed-resources.json')
    processes = item(prefix + '/observed-processes.json')
    assert len(resources) == r['observed_resources'] == record['resources']
    assert len(processes) == r['observed_processes'] == record['owned_pid_starttime']
    assert all(key == f'{v["pid"]}:{v["starttime"]}' for key, v in processes.items())
    assert len(item(prefix + '/adopted-waits.json')) == r['adopted_waits'] == record['adopted_waits']
    assert item(prefix + '/monitor-errors.json') == [] and r['monitor_errors'] == 0
    baseline = item(prefix + '/baseline.json')
    assert set(resources).isdisjoint(baseline)
    assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert not scan['owned_processes'] and not scan['remaining_new'] and not scan['runtime_entries']
        if record['original_double_cleanup']:
            assert not scan['browser_runtime_entries']
        else:
            assert scan['browser_runtime_entries'], prefix
assert failed == 7
assert item('author/recovery01/result.json')['exit'] == 1
for n in ('02', '03'):
    r = item('author/recovery' + n + '/result.json')
    assert r['exit'] == 0 and r['original_double_cleanup_remains_false'] and r['original_evidence_unchanged']
    assert r['two_post_recovery_sweeps_pass'] and not r['business_content_read'] and not r['resources_started']

# Final independent binding/erratum and ten original offline prerequisites.
ad = item('author/author-real-stage01/delivery35.json')
ind = item('real-independent/execution03.json')
assert ad['source'] == ind['files'] == {p: h for p, h in files36.items() if p != 'docs/development/frontend/README.md'}
assert ad['dist'] == ind['dist'] == delivery['dist'] and len(ind['private_files']) == 15
assert len(ind['runtime_files']) == 7604 and len(ind['dependencies']) == 18
for n, count in [('01', 13), ('02', 14), ('03', 15)]:
    e = item('real-independent/execution' + n + '.json')
    assert e['driver_sha256'] == ind['driver_sha256']
    assert len(e['private_files']) == count and set(e['private_files'].values()) <= known
    assert e['runtime_files'] == ind['runtime_files'] and e['dependencies'] == ind['dependencies'] and e['budgets'] == ind['budgets']
old = blob('real-independent/verification-report.md').decode()
new = blob('real-independent/verification-report-rev2.md').decode()
change = I['independent_erratum']
assert old.count(change['old']) == 1 and old.replace(change['old'], change['new']) == new
assert digest(new.encode()) == 'ac6e9c16a5495a1b05696e02d0e46a25018e7a0c1ffe56e5976f9efc10c821cb'
for run in ('format01', 'closure01', 'type01', 'compile01', 'list02', 'discover-a01', 'discover-b01', 'gate01', 'gate02', 'gate03'):
    prefix = 'real-independent/runs/' + run
    c, r, cleanup = (item(prefix + '/' + n + '.json') for n in ('command', 'result', 'cleanup'))
    assert c['actual_exit'] == r['actual_exit'] == 0 and c['actual_wait'] and r['actual_wait']
    assert r['input_unchanged'] and r['cleanup_clean'] and cleanup['clean'] and cleanup['child_actual_wait']
    assert not any(cleanup['scan1'].values()) and not any(cleanup['scan2'].values())
    assert not cleanup['services_or_listeners_started'] and not cleanup['docker_or_browser_started']
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
assert not any(n.startswith('real-independent/runs/list01/') for n in I['artifacts'])

# Main compatibility is compile/discovery only, under its historical pre-product HEAD.
mr = item('main-compat/report.json')
assert mr['main_head'] == MAIN and not mr['test_bodies_executed'] and not mr['listeners_or_fixtures_started']
assert mr['owned_pid_starttime_union'] == 6 and mr['adopted_waits'] == 0 and mr['front_non_candidate_count'] == 149
assert mr['front_non_candidates_match'] and mr['runtime13_match'] and not mr['readme_read']
assert mr['binary']['deleted_after_discovery'] and len(mr['binary']['exact_discovered']) == 3
for name, seconds in [('graph01', 0.114), ('account-compile01', 6.462), ('account-list01', 1.021)]:
    prefix = 'main-compat/' + name
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert c['main_head'] == MAIN and c['actual_wait_completed'] and r['actual_wait_completed']
    assert c['exit'] == r['exit'] == 0 and c['seconds'] == r['seconds'] == seconds
    assert c['limit_seconds'] == 45 and r['double_cleanup'] and r['input_unchanged'] and r['adopted_waits'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert all(s['owned_processes'] == s['runtime_entries'] == [] for s in item(prefix + '/cleanup.json'))
    assert b'=== RUN ' not in blob(prefix + '/raw.log')
assert item('author/delivery36/document-check01-actual.json')['actual_exit'] == 1
assert item('author/delivery36/document-check02-actual.json')['actual_exit'] == 0
assert item('author/delivery36/baseline-binding.json')['commit'] == MAIN

assert len(I['images']) == 26
for name, h in I['images'].items():
    data = blob(name)
    assert digest(data) == h and data[:8] == b'\x89PNG\r\n\x1a\n'
    width, height = struct.unpack('>II', data[16:24])
    assert width in (390, 768, 1024, 1440) and height == 900

# Append-only administrative changes and unchanged technical body.
old_docs = {p: G[(PRODUCT, p)] for p in doc_paths}
new_docs = {p: (DOCS / p).read_bytes() for p in doc_paths}
card, marker = I['card'], b'## 1. '
assert new_docs[card][new_docs[card].index(marker):] == old_docs[card][old_docs[card].index(marker):]
assert digest(new_docs[card][new_docs[card].index(marker):]) == TECH
assert b'\n\n## 1. ' in new_docs[card]
cont = doc_paths[2]
assert new_docs[cont].startswith(old_docs[cont]) and new_docs[cont][len(old_docs[cont]):].lstrip().startswith(b'## 34. ')
for p in doc_paths[:2]:
    at = old_docs[p].index(b'The [System Audit read-only UI specification]') if p == 'AGENTS.md' else old_docs[p].index(b'\n\n') + 2
    assert new_docs[p].startswith(old_docs[p][:at]) and new_docs[p].endswith(old_docs[p][at:])
links = 0
for p in doc_paths + [I['report']]:
    b = (DOCS / p).read_bytes(); text = b.decode('utf-8')
    assert b'\r' not in b and b.endswith(b'\n') and not any(line.endswith((b' ', b'\t')) for line in b.splitlines())
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0
    previous = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old_docs.get(p, b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', text):
        if target in previous or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
            continue
        filename, _, fragment = target.strip('<>').partition('#')
        rel = (Path(p).parent / unquote(filename)).as_posix() if filename else p
        dest = (DOCS / rel).resolve()
        if not dest.is_file():
            dest = (ROOT / rel).resolve()
        assert dest.is_file(), (p, target)
        if fragment:
            headings = re.findall(r'^#+\s+(.+?)\s*#*$', dest.read_text(), re.M)
            assert unquote(fragment) in {re.sub(r'[^\w\-\s]', '', h.lower()).replace(' ', '-') for h in headings}
        links += 1
counts = I['counts']
assert len(I['artifacts']) == counts['logical_artifacts'] and len(O) == counts['objects']
assert sum(map(len, O.values())) == counts['object_bytes']
print(json.dumps({'result': 'PASS; original bytes/fixed Git/records only; no product execution',
                  'accepted_product_commit': PRODUCT, 'logical_artifacts': len(I['artifacts']), 'objects': len(O),
                  'object_bytes': counts['object_bytes'], 'git_requests': len(G), 'product_paths': 36,
                  'source_versions': sum(map(len, I['source_versions'].values())), 'real_rounds': 13,
                  'original_real_failures': 7, 'original_cleanup_false': 2, 'main_offline_commands': 3,
                  'saved_images': 26, 'raw_result_pairs': raw_pairs, 'recorded_actual_waits': waits,
                  'new_local_links_checked': links, 'format_preimage_gaps': 2}, ensure_ascii=False))
