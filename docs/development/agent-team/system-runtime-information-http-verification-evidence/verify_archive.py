#!/usr/bin/env python3
"""Verify archived bytes, fixed Git and original records; execute no product code."""
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
I = json.loads((HERE / 'index.json').read_bytes())
PRODUCT = '9b074f809df4603923faa68d963cb30a5fe4d7eb'
BASE = 'b124650aee095b26191bc181dc8cf5d6e0f977f5'
MAIN = 'aa9ea0b63de0c395525f3084ef514752ce2f1dfd'
TECH = 'aeb4222baf0e2d4547abf13277dc7789f4157e8c3d98843c73377e00d31d2bab'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(argv, data=None):
    return subprocess.run(['git', *argv], cwd=ROOT, env=ENV, check=True, input=data,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


assert I['accepted_product_commit'] == PRODUCT and I['real_dependency_git'] == BASE
assert I['main_compile_head'] == MAIN and I['technical_sha256'] == TECH
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
            end = raw.index(b'\n', offset)
            header = raw[offset:end].split()
            assert len(header) == 3 and header[1] == b'blob', (key, header)
            size = int(header[2])
            b = raw[end + 1:end + 1 + size]
            offset = end + size + 2
            assert len(b) == size and raw[offset - 1:offset] == b'\n'
            G[key] = b
        assert offset == len(raw)
    for key, h in requests.items():
        assert h is None or digest(G[key]) == h, key


def blob(name):
    m = I['artifacts'][name]
    return G[(m['git']['commit'], m['git']['path'])] if 'git' in m else O[m['sha256']]


def item(name):
    return json.loads(blob(name))


def sha(value):
    return value if isinstance(value, str) else value['sha256']


read_git({(m['git']['commit'], m['git']['path']): m['sha256']
          for m in I['artifacts'].values() if 'git' in m})
for name, m in I['artifacts'].items():
    b = blob(name)
    assert digest(b) == m['sha256'] and len(b) == m['bytes'], name
    assert not any(p in Path(m['origin']).parts for p in ['gocache', 'modcache', 'node_modules', 'runtime'])
known = set(O) | {digest(v) for v in G.values()}
delivery = item('author/document-stage01/delivery14.json')
files14 = {p: m['sha256'] for p, m in delivery['files'].items()}
assert len(files14) == 14 and files14 == I['delivery14']
assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT]).decode().splitlines()) == set(files14)
requests = {(PRODUCT, p): h for p, h in files14.items()}
for rec in I['git_manifest_refs']:
    value = item(rec['artifact'])
    for field in rec['fields']:
        value = value[field]
    for p, meta in value.items():
        requests[(rec['commit'], p)] = sha(meta)
for rec in I['plan_fixed_git_refs']:
    requests[(rec['commit'], rec['path'])] = rec['sha256']
for name in I['author_check_inputs']:
    for p, h in item(name)['files'].items():
        if h not in known:
            assert not p.startswith('/'), (name, p)
            requests[(BASE, p)] = h
main_input = item('author/main-integration01/input.json')
local = {p.removeprefix('/workspace/agenteam/'): h for p, h in main_input['files'].items()
         if p.startswith('/workspace/agenteam/')}
assert len(local) == len(main_input['local_paths']) == 622
for p, h in local.items():
    requests[(PRODUCT if p in files14 else MAIN, p)] = h
doc_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
             'docs/development/agent-team/recovery-2026-10-06-continuation.md', I['card']]
for p in doc_paths:
    requests[(PRODUCT, p)] = None
read_git(requests)
known |= {digest(v) for v in G.values()}
for name in I['author_check_inputs']:
    assert set(item(name)['files'].values()) <= known, name
for p, versions in I['source_versions'].items():
    for h, meta in versions.items():
        assert digest(blob(meta['artifact'])) == h, p
for stage, count in [('service-stage01', 12), ('service-stage02', 12),
                     ('http-stage01', 246), ('app-stage01', 343), ('harness-stage01', 446)]:
    m = item('author/' + stage + '/manifest.json')
    assert len(m['dependencies']) == count
    assert {sha(v) for v in m['files'].values()} <= known

# Bind raw/commands/results while retaining original failures and format mutations.
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
for run, code in [('crossfield01', 1), ('crossfield02', 0), ('terminal01', 0)]:
    prefix = 'service-independent/runs/' + run
    c, r, clean = (item(prefix + '/' + n + '.json') for n in ['command', 'result', 'cleanup'])
    assert c['actual_exit'] == r['actual_exit'] == code and c['actual_wait'] and r['actual_wait']
    assert r['input_unchanged'] and r['cleanup_clean'] and r['adopted_wait_count'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert clean['clean'] and clean['child_actual_wait'] and not clean['adopted_waits']
    assert not any(clean['scan1'].values()) and not any(clean['scan2'].values())
probe = digest(blob('service-independent/probe01/crossfield_test.go'))
assert probe == 'a5b4172b070b1ce56448343b31fabeaf6e8be9c437717a84f6767b4dccc45bbc'
for n in ['execution01.json', 'execution02.json']:
    assert probe in item('service-independent/' + n)['files'].values()
assert item('author/runs/app-pure01/result.json')['exit'] == 1
assert item('author/runs/app-pure02/result.json')['exit'] == 0
assert item('author/runs/app-race01/result.json')['exit'] == 0
assert len(I['historical_format_preimage_limits']) == 2
for row in I['historical_format_preimage_limits']:
    assert row['before_sha256'] not in known and row['after_sha256'] in known
assert item('author/preparation/runtime-closure01/tool-result.json')['actual_exit'] == 1
assert item('author/diagnostic-native01/outer-tool-result.json')['actual_exit'] == 1
for n in I['native01_absent_original_records']:
    assert 'author/runs/native01/' + n not in I['artifacts']
for n in ['01', '02']:
    r = item('author/document-stage01/preparation-check' + n + '/result.json')
    assert r['actual_exit'] == 1 and not r['test_or_resource_started'] and r['candidate_unchanged']

# Four complete real rounds; the original prelaunch native01 is not a fifth round.
specs = {
    'author/runs/native02': (7.502, 0, 5, 4),
    'author/runs/new-account01': (104.547, 7, 104, 1),
    'author/runs/new-root01': (57.201, 7, 74, 1),
    'real-independent/runs/independent01': (48.663, 7, 74, 2),
}
for prefix, (seconds, resource_count, pid_count, top_count) in specs.items():
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert c['actual_wait_completed'] and c['exit'] == r['driver_exit'] == 0 and c['seconds'] == seconds
    assert c['argv'] and c['cwd'] and isinstance(c['env'], dict)
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL|SKIP): ([^\s(]+)', blob(prefix + '/raw.log'), re.M)]
    assert tops == r['top_levels'] and len(tops) == top_count and all(a == 'PASS' for a, _ in tops)
    assert r['selected_tests_passed'] and all(r[k] for k in ['accepted_baseline_before', 'accepted_baseline_after',
        'double_cleanup', 'source_files_unchanged', 'verification_inputs_unchanged', 'resource_topology_matches'])
    assert r['browser_processes'] == r['node_processes'] == r['monitor_errors'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    assert before['product_base'] == BASE and len(before['files']) == 13
    assert before['files'] == {p: h for p, h in files14.items() if p != 'docs/development/backend/README.md'}
    assert len(before['accepted_baseline']['files']) == 904
    for p, h in before['accepted_baseline']['files'].items():
        assert digest(G[(BASE, p)]) == sha(h)
    assert set(before.get('private_files', {}).values()) <= known
    vi = item(prefix + '/verification-input.json')
    assert vi['driver_sha256'] == digest(blob(prefix + '/driver.py.txt'))
    assert vi['frozen_input_sha256'] == digest(blob(prefix + '/frozen-input.json'))
    resources, processes = item(prefix + '/observed-resources.json'), item(prefix + '/observed-processes.json')
    assert len(resources) == r['observed_resources'] == resource_count
    assert len(processes) == r['observed_processes'] == pid_count
    assert all(k == f'{v["pid"]}:{v["starttime"]}' for k, v in processes.items())
    assert item(prefix + '/adopted-waits.json') == [] and r['adopted_waits'] == 0
    assert item(prefix + '/monitor-errors.json') == []
    baseline = item(prefix + '/baseline.json')
    assert set(resources).isdisjoint(baseline)
    if resource_count:
        assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    else:
        assert baseline == {} and r['native_only']
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert all(scan[k] == [] for k in ['owned_processes', 'remaining_new', 'runtime_entries'])
author = item('author/driver-stage01/input.json')
independent = item('real-independent/execution01.json')
assert len(author['runtime']['files']) == 3501 and len(independent['runtime']['files']) == 3504
assert all(independent['runtime']['files'][p] == h for p, h in author['runtime']['files'].items())
assert {p: h for p, h in independent['runtime']['files'].items() if p not in author['runtime']['files']} == independent['private_files']
assert independent['accepted_baseline'] == author['accepted_baseline'] and independent['files'] == author['files']
env_a, env_b = author['runtime']['discovery_env'], independent['runtime']['discovery_env']
assert {k for k in set(env_a) | set(env_b) if env_a.get(k) != env_b.get(k)} == {'GOFLAGS', 'TMPDIR', 'GOTMPDIR'}

# Independent preparation is original offline execution, never a business rerun.
ready = item('real-independent/preparation-ready01.json')
for name, record in ready['checks'].items():
    prefix = 'real-independent/runs/' + name
    for filename, h in record['files'].items():
        assert digest(blob(prefix + '/' + filename)) == h
    c, r, clean = (item(prefix + '/' + n + '.json') for n in ['command', 'result', 'cleanup'])
    assert r == record['result'] and c['actual_exit'] == r['actual_exit'] == 0
    assert c['actual_wait'] and r['actual_wait'] and not r['timeout']
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert clean['child_actual_wait'] and clean['clean'] and clean['tmp_removed'] and not clean['adopted_waits']
    assert not any(clean['scan1'].values()) and not any(clean['scan2'].values())
    assert not clean['services_or_listeners_started'] and not clean['docker_or_browser_started']
    assert b'=== RUN ' not in blob(prefix + '/raw.log')

# Main five-command compatibility preserves its actual pre-product HEAD.
mr = item('author/main-integration01/report.json')
assert mr['main_head'] == MAIN and not mr['test_bodies_executed'] and not mr['listeners_or_fixture_started']
assert mr['closure']['local_paths'] == 622 and mr['closure']['external_and_provenance_files'] == 3499
assert mr['closure']['packages'] == 396 and mr['closure']['modules'] == 31
assert mr['owned_pid_starttime_union'] == 27 and mr['adopted_waits'] == 0
assert mr['closure']['noncandidate_production_delta_from_b124650'] == []
assert digest(blob('author/main-integration01/input.json')) == mr['input_sha256']
assert digest(blob('author/document-stage01/delivery14.json')) == mr['delivery14_sha256']
assert len(mr['checks']) == 5
for rec in mr['checks']:
    prefix = 'author/main-integration01/' + rec['name']
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert c['main_head'] == MAIN and c['limit_seconds'] == 45 and c['actual_wait_completed'] and r['actual_wait_completed']
    assert c['exit'] == r['exit'] == rec['exit'] == 0 and c['seconds'] == r['seconds'] == rec['seconds']
    assert r['input_unchanged'] and r['double_cleanup'] and r['adopted_waits'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert item(prefix + '/adopted-waits.json') == []
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and all(s['owned_processes'] == s['runtime_entries'] == [] for s in scans)
    assert b'=== RUN ' not in blob(prefix + '/raw.log')
assert all(v['deleted_after_discovery'] for v in mr['binaries'].values())
outer = item('author/main-integration01/outer-result.json')
assert outer['actual_exit'] == 0 and outer['actual_wait_completed']

# Administrative changes are append-only; technical and historical bytes stay fixed.
old = {p: G[(PRODUCT, p)] for p in doc_paths}
new = {p: (DOCS / p).read_bytes() for p in doc_paths}
card, marker = I['card'], b'## 1. '
assert new[card][new[card].index(marker):] == old[card][old[card].index(marker):]
assert digest(new[card][new[card].index(marker):]) == TECH
continuation = doc_paths[2]
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].lstrip().startswith(b'## 33. ')
for p in doc_paths[:2]:
    at = old[p].index(b'The [System Runtime Information HTTP specification]') if p == 'AGENTS.md' else old[p].index(b'\n\n') + 2
    assert new[p].startswith(old[p][:at]) and new[p].endswith(old[p][at:])
links = 0
for p in doc_paths + [I['report']]:
    b = (DOCS / p).read_bytes()
    text = b.decode('utf-8')
    assert b'\r' not in b and b.endswith(b'\n') and not any(line.endswith((b' ', b'\t')) for line in b.splitlines())
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0
    previous = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old.get(p, b'').decode()))
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
print(json.dumps({'result': 'PASS; archived bytes/fixed Git/original records only; no product execution',
    'accepted_product_commit': PRODUCT, 'logical_artifacts': len(I['artifacts']), 'objects': len(O),
    'object_bytes': counts['object_bytes'], 'git_requests': len(G), 'product_paths': 14,
    'source_versions': sum(map(len, I['source_versions'].values())), 'complete_real_rounds': 4,
    'main_offline_commands': 5, 'raw_result_pairs': raw_pairs, 'recorded_actual_waits': waits,
    'new_local_links_checked': links, 'unconsumed_format_preimage_limits': 2}, ensure_ascii=False))
