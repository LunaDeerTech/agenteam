#!/usr/bin/env python3
"""Verify immutable originals, fixed Git and document deltas; execute no product code."""
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
PRODUCT = '2debfdde347d8c9262ab83d3fe5e18e947a983e1'
BASE = 'c1427fa4fb7fa118b12b2fb1f5d5f8917113ce2c'
TECH = '1320107dd2bebba97188c976780a111d0ec12e2fb11ae866c9ad3473b5e53133'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(argv, data=None):
    return subprocess.run(['git', *argv], cwd=ROOT, env=ENV, input=data,
                          check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


O, G = {}, {}
assert I['accepted_product_commit'] == PRODUCT and I['fixed_running_product_base'] == BASE
assert I['technical_sha256'] == TECH and I['continuation_new_section'] == 38
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    p = HERE / meta['path']
    assert p.is_file() and not p.is_symlink()
    b = p.read_bytes()
    assert digest(b) == h and len(b) == meta['bytes'], h
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
            G[key] = raw[end + 1:end + 1 + size]
            offset = end + size + 2
            assert len(G[key]) == size and raw[offset - 1:offset] == b'\n'
        assert offset == len(raw)
    for key, h in requests.items():
        assert h is None or digest(G[key]) == h, key


def blob(name):
    v = I['artifacts'][name]
    return G[(v['git']['commit'], v['git']['path'])] if 'git' in v else O[v['sha256']]


def item(name):
    return json.loads(blob(name))


def sha(value):
    return value if isinstance(value, str) else value['sha256']


read_git({(v['git']['commit'], v['git']['path']): v['sha256']
          for v in I['artifacts'].values() if 'git' in v})
for name, v in I['artifacts'].items():
    assert ('git' in v) != ('object' in v)
    assert digest(blob(name)) == v['sha256'] and len(blob(name)) == v['bytes'], name
    assert not any(p in Path(v['origin']).parts for p in ('gocache', 'modcache', 'node_modules'))
    assert 'runtime' not in Path(v['origin']).parts or name == 'capacity-stdlib/runtime/mstats.go'

files16 = {p: sha(v) for p, v in item('author/readme-stage01/delivery16.json')['files'].items()}
assert files16 == I['delivery16'] and len(files16) == 16
assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT]).decode().splitlines()) == set(files16)
requests = {(PRODUCT, p): h for p, h in files16.items()}
for reference in I['git_manifest_refs']:
    values = item(reference['artifact'])
    for field in reference['fields']:
        values = values[field]
    assert len(values) == reference['count']
    for commit in reference['commits']:
        requests.update({(commit, p): sha(v) for p, v in values.items()})
requests.update({(PRODUCT, p): v['base_sha256'] for p, v in I['administrative_base'].items()})
read_git(requests)
for p, versions in I['source_versions'].items():
    assert files16[p] in versions
    for h, v in versions.items():
        assert digest(blob(v['artifact'])) == h

files15 = {p: h for p, h in files16.items() if p != 'docs/development/backend/README.md'}
assert {p: sha(v) for p, v in item('author/author-final01/delivery15.json')['files'].items()} == files15
assert item('real-independent/verification-report.json')['source15_sha256'] == files15
integration = item('root-integration/verification.json')
assert integration['commit'] == PRODUCT and integration['source_paths'] == 16
assert integration['all_delivered_main_sha_equal'] and integration['all_fixed_dependencies_main_sha_equal']
assert integration['fixed_dependencies'] == 914 and integration['no_main_dynamic_rerun']
assert integration['push_actual_exit'] == 0 and integration['commit_exit'] == 0 and integration['clean']
assert integration['remote_main'].split()[0] == PRODUCT
assert len(integration['three_modified_source_preimages_exact_base']) == 3
preimages = item('author/author-final01/delivery15.json')['files']
read_git({(BASE, p): preimages[p]['baseline_sha256']
          for p in integration['three_modified_source_preimages_exact_base']})
read_git({(I['previous_main'], p): preimages[p]['baseline_sha256']
          for p in integration['three_modified_source_preimages_exact_base']})

# Explicit stage identities keep old failed inputs separate from later accepted ones.
a2, a3 = item('author/candidate02/manifest.json'), item('author/candidate03/manifest.json')
assert len(a2['files']) == len(a3['files']) == 5
changed = {p for p in a2['files'] if sha(a2['files'][p]) != sha(a3['files'][p])}
assert changed == set(a3['changed_paths']) == {'internal/central/model/adapter/embedding_json_test.go'}
assert digest(blob('author/candidate02-to03.diff')) == a3['diff']['sha256']
for manifest, fields in [('author/candidate02/manifest.json', ['files']),
                         ('author/candidate03/manifest.json', ['files']),
                         ('author/b-candidate01/manifest.json', ['files']),
                         ('author/d-candidate01/manifest.json', ['D_files'])]:
    values = item(manifest)
    for field in fields:
        values = values[field]
    for p, v in values.items():
        assert sha(v) in I['source_versions'][p], (manifest, p)
assert item('d-independent/startup-review01.json')['status'] == 'BLOCKED'
assert item('d-independent/startup-review01.json')['no_dynamic_execution']
assert item('d-independent/startup-review02.json')['status'] == 'STATIC PASS'
one = item('author/driver-stage01/input.json')
two = item('author/driver-stage02/input.json')
ind = item('real-independent/execution01.json')
assert one['files'] == two['files'] == ind['files'] == files15
assert len(one['accepted_baseline']['files']) == 907 and len(two['accepted_baseline']['files']) == 914
assert all(two['accepted_baseline']['files'][p] == h for p, h in one['accepted_baseline']['files'].items())
assert len(two['runtime']['files']) == 3513 and len(one['runtime']['files']) == 3510
assert len(one['runtime']['packages']) == 455 and len(two['runtime']['packages']) == 459
assert all(two['runtime']['files'][p] == h for p, h in one['runtime']['files'].items())
assert ind['accepted_baseline'] == two['accepted_baseline']
assert set(ind['runtime']) == set(two['runtime'])
assert {k for k in two['runtime'] if two['runtime'][k] != ind['runtime'][k]} == {'discovery_env'}
aenv, ienv = two['runtime']['discovery_env'], ind['runtime']['discovery_env']
assert {k for k in set(aenv) | set(ienv) if aenv.get(k) != ienv.get(k)} == {'TMPDIR', 'GOTMPDIR', 'GOFLAGS'}
assert ienv['GOFLAGS'] == aenv['GOFLAGS'] + ' -overlay=/workspace/scratch/agenteam-embeddings-real-independent-y4ny6v95/overlay02.json'
assert len(ind['private_inputs']) == 5
host = two['runtime']['host_cgo1_build_discovery']
assert host['CGO_ENABLED'] == '1' and not host['race'] and not host['integration_tag']
assert set(host['argv'][-5:]) == {'./cmd/agenteam', './cmd/agenteam-runner',
                                './tests/testsupport/objectstore/cmd/fixture',
                                './tests/testsupport/outbound/cmd/fixture',
                                './tests/testsupport/postgres/cmd/fixture'}

# Only recorded offline commands are checked. Preparation narratives stay narratives.
failures, raw_links = {}, 0
for record in I['recorded_offline_results']:
    p = record['artifact_prefix']
    c, r, cleanup = item(p + '/command.json'), item(p + '/result.json'), item(p + '/cleanup.json')
    assert c['actual_exit'] == r['actual_exit'] == record['actual_exit']
    assert c['actual_wait'] and r['actual_wait'] and c['seconds'] == r['seconds'] == record['seconds']
    assert not r['timeout'] and not c['resource_execution'] and c['subreaper_before_child']
    assert c['argv'] and c['cwd'] and isinstance(c['env_overrides'], dict)
    assert r['input_unchanged'] and r['input_before_matches'] and r['input_after_matches'] and r['cleanup_clean']
    assert blob(p + '/input-before.json') == blob(p + '/input-after.json')
    before = item(p + '/input-before.json')
    assert before['matches_frozen'] and len(before['files']) == record['input_count']
    assert c['execution_sha256'] == before['execution_sha256']
    assert c['execution_sha256'] in {v['sha256'] for v in I['artifacts'].values()}
    assert digest(blob(p + '/raw.log')) == r['raw_sha256']
    assert digest(blob(p + '/cleanup.json')) == r['cleanup_sha256']
    raw_links += 1
    assert cleanup['clean'] and cleanup['child_actual_wait'] and cleanup['tmp_removed']
    assert len(cleanup['observed_owned_pid_starttime']) == record['owned_pid_starttime'] == r['owned_processes_observed']
    assert not cleanup['adopted_waits'] and r['adopted_wait_count'] == 0
    assert not any(cleanup['scan1'].values()) and not any(cleanup['scan2'].values())
    assert not cleanup['services_or_listeners_started'] and not cleanup['docker_or_browser_started']
    if r['actual_exit']:
        failures[p] = r['actual_exit']
assert failures == {'author/runs/race01': 1}
red = blob('author/runs/race01/raw.log')
assert len(re.findall(rb'^--- PASS:', red, re.M)) == 7 and len(re.findall(rb'^--- FAIL:', red, re.M)) == 1
assert b'22072128' in red and b'DATA RACE' not in red
counts_by_run = {r['artifact_prefix']: r['input_count'] for r in I['recorded_offline_results']}
assert counts_by_run['author/runs/b-format01'] == 1806
for n in ('b-graph01', 'b-pure01', 'b-race01', 'b-old-pure01', 'b-vet01'):
    assert counts_by_run['author/runs/' + n] == 1805
assert counts_by_run['b-independent/runs/pure01'] == 1825
assert counts_by_run['a-independent/runs/pure01'] == 1823
for n in ('format01', 'compile-list01', 'vet01', 'gate01'):
    assert counts_by_run['real-independent/runs/' + n] == 4452

# Actual resources: preserve original full-Mount comparison results and ownership.
all_tops = 0
for record in I['real_rounds']:
    p = record['artifact_prefix']
    c, r, cleanup = item(p + '/command.json'), item(p + '/result.json'), item(p + '/cleanup.json')
    assert c['actual_wait_completed'] and c['exit'] == r['driver_exit'] == record['actual_exit'] == 0
    assert c['seconds'] == record['seconds'] and c['argv'] and c['cwd'] and isinstance(c['env'], dict)
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL|SKIP): ([^\s(]+)', blob(p + '/raw.log'), re.M)]
    assert tops == record['top_levels'] == r['top_levels'] and all(t[0] == 'PASS' for t in tops)
    all_tops += len(tops)
    assert r['selected_tests_passed'] and r['double_cleanup'] and r['resource_topology_matches']
    assert r['fixture_resources_started'] and r['monitor_errors'] == 0 and r['adopted_waits'] == 0
    assert r['observed_resources'] == record['exact_ids'] == 7
    assert r['observed_processes'] == record['pid_starttime_count']
    for field in ('accepted_baseline_before', 'accepted_baseline_after', 'source_files_unchanged', 'verification_inputs_unchanged'):
        assert r[field]
    assert blob(p + '/input-before.json') == blob(p + '/input-after.json')
    before = item(p + '/input-before.json')
    frozen = two if p.startswith('author/') else ind
    assert before['files'] == files15 and before['runtime'] == frozen['runtime']
    assert before['accepted_baseline']['accepted'] and before['accepted_baseline']['files'] == {path: sha(v) for path, v in two['accepted_baseline']['files'].items()}
    observed = item(p + '/observed-resources.json')
    assert len(observed) == 7 and len(item(p + '/observed-processes.json')) == record['pid_starttime_count']
    assert item(p + '/monitor-errors.json') == [] and item(p + '/adopted-waits.json') == []
    assert len(cleanup) == 2
    for scan in cleanup:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(observed)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert not scan['owned_processes'] and not scan['remaining_new'] and not scan['runtime_entries']
    vi = item(p + '/verification-input.json')
    assert digest(blob(p + '/driver.py.txt')) == vi['driver_sha256']
    assert digest(blob(p + '/frozen-input.json')) == vi['frozen_input_sha256']
    frozen = two if p.startswith('author/') else ind
    assert item(p + '/frozen-input.json') == frozen
    for disk in ('disk-preflight.json', 'disk-before-spawn.json'):
        d = item(p + '/' + disk)
        assert d['free_bytes'] >= d['minimum_free_bytes'] == 5 * 1024 ** 3
assert all_tops == 11
iraw = blob('real-independent/runs/independent01/raw.log')
assert len(re.findall(rb'^    --- PASS:', iraw, re.M)) == 2
assert len(I['real_rounds']) == 6 and len(I['recorded_offline_results']) == 36

old_docs = {p: G[(PRODUCT, p)] for p in I['administrative_base']}
assert len(old_docs) == 5
for p, v in I['administrative_base'].items():
    old, new = old_docs[p], (DOCS / p).read_bytes()
    assert digest(old) == v['base_sha256'] and digest(new) == v['new_sha256']
    assert v['base_commit'] == PRODUCT
    if v['mode'] == 'insert':
        at = v['insertion_offset']
        assert new.startswith(old[:at]) and new.endswith(old[at:]) and len(new) > len(old)
    elif v['mode'] == 'append':
        assert new.startswith(old) and new[len(old):].lstrip().startswith(b'## 38. ')
    else:
        assert v['mode'] == 'card_header_only' and p == I['card']
        marker = b'## 1. '
        assert new[new.index(marker):] == old[old.index(marker):]
        assert digest(new[new.index(marker):]) == TECH and b'\n\n## 1. ' in new

links = 0
for p in list(old_docs) + [I['report']]:
    b = (DOCS / p).read_bytes()
    text = b.decode('utf-8')
    assert b'\r' not in b and b.endswith(b'\n')
    assert not any(line.endswith((b' ', b'\t')) for line in b.splitlines())
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
assert len(I['artifacts']) == counts['logical_artifacts'] == 604
assert len(O) == counts['new_objects'] == 427
assert sum(map(len, O.values())) == counts['new_object_bytes'] == 38093184
assert sum('git' in v for v in I['artifacts'].values()) == counts['git_reused_artifact_paths'] == 44
assert len(I['source_versions']) == 16 and sum(map(len, I['source_versions'].values())) == 27
print(json.dumps({'result': 'PASS; immutable bytes/fixed Git/recorded results only',
                  'product_commit': PRODUCT, 'logical_artifacts': 604, 'objects': 427,
                  'object_bytes': 38093184, 'Git_reads': len(G), 'delivery_paths': 16,
                  'fixed_Git_dependencies': 914, 'saved_source_versions': 27,
                  'offline_rounds': 36, 'original_offline_nonzero': 1,
                  'real_rounds': 6, 'real_top_passes': 11, 'independent_sub_passes': 2,
                  'new_links': links, 'administrative_files': 5,
                  'no_product_execution': True}, ensure_ascii=False))
