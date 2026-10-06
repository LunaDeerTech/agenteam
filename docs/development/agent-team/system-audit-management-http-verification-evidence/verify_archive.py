#!/usr/bin/env python3
"""Verify immutable evidence, fixed Git, and recorded results; execute no product code."""
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
PRODUCT = 'b124650aee095b26191bc181dc8cf5d6e0f977f5'
BASE = 'f843506d9ec991be1c334a81b88d5cbacc277467'
MAIN = '2742cf785e2f9379c2046172556ead70c29b48dc'
TECH = '687c85d4a41880828d038251bf6321accc48092aa043a10addb1606ab02c6f21'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(args, data=None):
    return subprocess.run(['git', *args], cwd=ROOT, env=ENV, check=True, input=data,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


assert I['accepted_product_commit'] == PRODUCT and I['technical_sha256'] == TECH
assert I['real_dependency_git'] == BASE and I['main_compile_head'] == MAIN
O, G = {}, {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    p = HERE / meta['path']
    assert p.is_file() and not p.is_symlink()
    b = p.read_bytes()
    assert digest(b) == h and len(b) == meta['bytes']
    O[h] = b
assert {p.name for p in (HERE / 'objects').iterdir()} == set(O)
assert {m['sha256'] for m in I['artifacts'].values() if 'object' in m} == set(O)


def read_git(requests):
    order = [key for key in requests if key not in G]
    raw = git(['cat-file', '--batch'], ''.join(f'{c}:{p}\n' for c, p in order).encode())
    offset = 0
    for key in order:
        end = raw.index(b'\n', offset); header = raw[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', (key, header)
        size = int(header[2]); b = raw[end + 1:end + 1 + size]
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


read_git({(m['git']['commit'], m['git']['path']): m['sha256'] for m in I['artifacts'].values() if 'git' in m})
for name, meta in I['artifacts'].items():
    b = blob(name)
    assert digest(b) == meta['sha256'] and len(b) == meta['bytes'], name
    assert not any(p in Path(meta['origin']).parts for p in ['gocache', 'modcache', 'runtime', 'node_modules'])
known = set(O) | {digest(b) for b in G.values()}
delivery = item('author/author-delivery14.json')
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
for name in I['author_check_inputs']:
    for p, h in item(name).items():
        if h not in known:
            requests[(BASE, p)] = h
main_input = item('author/main-integration01/input.json')
local = {p.removeprefix('/workspace/agenteam/'): h for p, h in main_input['files'].items() if p.startswith('/workspace/agenteam/')}
assert len(local) == 614
for p, h in local.items():
    requests[(PRODUCT if p in files14 else MAIN, p)] = h
for p, h in main_input['outbound_1ff0442_files'].items():
    requests[('1ff044264a54c948e512db9aafad24ec5e0aa3c2', p)] = h
fix = I['mime_fix_commit']
if fix:
    fix_files = item('mime-author/delivery2.json')['files']
    assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', fix]).decode().splitlines()) == set(fix_files)
    for p, meta in fix_files.items():
        requests[(fix, p)] = meta['sha256']
doc_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
             'docs/development/agent-team/recovery-2026-10-06-continuation.md', I['card']]
for p in doc_paths:
    requests[(PRODUCT, p)] = None
read_git(requests)
known |= {digest(b) for b in G.values()}
for p, versions in I['source_versions'].items():
    for h, meta in versions.items():
        assert digest(blob(meta['artifact'])) == h, p
for name in I['author_check_inputs']:
    assert set(item(name).values()) <= known, name
final13 = item('author/input03/manifest.json')
assert {p: sha(v) for p, v in final13['files'].items()} == {p: h for p, h in files14.items() if p != 'docs/development/backend/audit.md'}
assert len(final13['dependencies']) == 467
for n in ['input01', 'input02', 'input03']:
    value = item('author/' + n + '/manifest.json')
    assert len(value['files']) == 13 and {sha(v) for v in value['files'].values()} <= known
    assert value['dependencies'] == final13['dependencies']
for n in ['query-stage01', 'http-stage01']:
    assert {sha(v) for v in item('author/stages/' + n + '/manifest.json')['files'].values()} <= known

# Original six native/PG commands remain distinct, including native01's exit1.
real_specs = {
    'author/runs/native01': (1, 4.405, 0, 3),
    'author/runs/native02': (0, 5.186, 0, 3),
    'author/runs/new-account01': (0, 103.558, 7, 111),
    'author/runs/new-root01': (0, 55.982, 7, 73),
    'author/runs/old01': (0, 55.256, 7, 70),
    'real-independent/runs/independent01': (0, 105.717, 7, 122),
}
for prefix, expected in real_specs.items():
    code, seconds, id_count, pid_count = expected
    command, result = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert command['actual_wait_completed'] and command['exit'] == result['driver_exit'] == code
    assert command['seconds'] == seconds and command['argv'] and command['cwd'] and isinstance(command['env'], dict)
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL): ([^\s(]+)', blob(prefix + '/raw.log'), re.M)]
    assert tops == result['top_levels'] and result['selected_tests_passed'] == all(v[0] == 'PASS' for v in tops)
    assert all(result[k] for k in ['accepted_baseline_before', 'accepted_baseline_after', 'double_cleanup',
                                  'source_files_unchanged', 'verification_inputs_unchanged', 'resource_topology_matches'])
    assert result['browser_processes'] == result['node_processes'] == result['monitor_errors'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    assert before['product_base'] == BASE and len(before['files']) == 13 and set(before['files'].values()) <= known
    assert len(before['accepted_baseline']['files']) == 893
    for p, h in before['accepted_baseline']['files'].items():
        assert digest(G[(BASE, p)]) == sha(h)
    assert set(before.get('private_files', {}).values()) <= known
    assert item(prefix + '/verification-input.json')['driver_sha256'] == digest(blob(prefix + '/driver.py.txt'))
    resources, processes = item(prefix + '/observed-resources.json'), item(prefix + '/observed-processes.json')
    assert len(resources) == result['observed_resources'] == id_count
    assert len(processes) == result['observed_processes'] == pid_count
    assert all(k == f'{v["pid"]}:{v["starttime"]}' for k, v in processes.items())
    assert item(prefix + '/adopted-waits.json') == [] and result['adopted_waits'] == 0
    assert item(prefix + '/monitor-errors.json') == []
    baseline = item(prefix + '/baseline.json')
    if id_count:
        assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    else:
        assert baseline == {} and result['native_only']
    assert set(resources).isdisjoint(baseline)
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and set(scan['exact_absent']) == set(resources)
        assert all(v['absent'] for v in scan['exact_absent'].values())
        assert all(scan[k] == [] for k in ['owned_processes', 'remaining_new', 'runtime_entries'])
author_runtime = item('author/driver-stage02/input.json')['runtime']['files']
independent_runtime = item('real-independent/execution01.json')['runtime']['files']
assert len(author_runtime) == 3501 and len(independent_runtime) == 3504
assert len(author_runtime.keys() & independent_runtime.keys()) == 3500
assert all(author_runtime[p] == independent_runtime[p] for p in author_runtime.keys() & independent_runtime.keys())
assert set(author_runtime) - set(independent_runtime) == {'/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12'}
assert len(set(independent_runtime) - set(author_runtime)) == 4

# Hash-bind every original recorded result without inventing absent cleanup/input-after records.
raw_pairs, actual_wait_records = 0, 0
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
    if value.get('actual_wait') or value.get('actual_wait_completed'):
        actual_wait_records += 1
original_nonzero = {'author/checks/query-pure01/result.json': 1, 'author/checks/query-pure02/result.json': 1,
                   'author/checks/http-pure01/result.json': 1, 'query-independent/runs/query01/result.json': 1}
for name, code in original_nonzero.items():
    value = item(name)
    assert value.get('exit_code', value.get('actual_exit')) == code, name

# Main compatibility is original graph/compile/discovery only, on 2742cf7 + copied14.
main_report = item('author/main-integration01/report.json')
assert main_report['main_head'] == MAIN and not main_report['business_tests_executed']
assert main_report['local_paths'] == 614 and main_report['runtime_files'] == 3496
assert main_report['packages'] == 394 and main_report['modules'] == 31
assert main_report['observed_pid_starttime_union'] == 44 and main_report['adopted_waits'] == 0
assert digest(blob('author/main-integration01/input.json')) == main_report['input_sha256']
for check in main_report['commands']:
    prefix = 'author/main-integration01/' + check['name']
    command, result = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert command['actual_wait_completed'] and result['actual_wait_completed']
    assert command['exit'] == result['exit'] == check['exit'] == 0
    assert command['seconds'] == result['seconds'] == check['seconds']
    assert command['main_head'] == MAIN and command['limit_seconds'] == 45
    assert result['input_unchanged'] and result['double_cleanup'] and result['adopted_waits'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert all(scan['owned_processes'] == scan['runtime_entries'] == [] for scan in item(prefix + '/cleanup.json'))
    assert item(prefix + '/adopted-waits.json') == []
assert all(v['deleted_after_discovery'] for v in main_report['binaries'].values())
# MIME author originals preserve old RED and later affected checks; no browser/DB claim.
mime = item('mime-author/report.json')
for check in mime['checks']:
    prefix = 'mime-author/runs/' + check['name']
    command, result = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert result['actual_wait_completed'] and result['exit'] == check['exit']
    assert result['seconds'] == check['seconds'] and result['double_cleanup'] and result['source_unchanged']
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert item(prefix + '/adopted-waits.json') == []
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and all(s['owned_processes'] == s['runtime_entries'] == [] for s in scans)
assert [c['exit'] for c in mime['checks']] == [1, 1, 0, 0, 0, 0, 0]
assert mime['observed_pid_starttime_union'] == 451 and mime['adopted_waits'] == 0

# The later MIME correction is a separate two-file, offline-only accepted result.
assert fix == 'fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5'
mime_independent = item('mime-independent/verification-report.json')
assert mime_independent['status'] == 'PASS' and mime_independent['baseline'] == PRODUCT
assert not mime_independent['real_resources_started'] and not mime_independent['product_modified']
for name, h in mime_independent['files'].items():
    assert digest(blob('mime-independent/' + name)) == h
mime_static = item('mime-independent/static-delta01.json')
assert mime_static['two_candidates_verified'] and mime_static['dependencies_verified'] == 252
assert len(mime_static['schema_changes']) == 9
assert all(set(v['changed_keys']) == {'pattern', 'not'} for v in mime_static['schema_changes'])
assert all(mime_static[k] for k in ['other_schema_structure_unchanged',
                                  'old_test_bytes_after_removing_one_added_function_equal',
                                  'production_go_unchanged'])
for run, owned in [('go01', 5), ('node01', 1)]:
    prefix = 'mime-independent/runs/' + run
    command, result, cleanup = (item(prefix + '/' + n + '.json') for n in ['command', 'result', 'cleanup'])
    execution_name = 'mime-independent/execution-' + run + '.json'
    execution = item(execution_name)
    assert execution['outer_limit_seconds'] == 45 and execution['source_base'] == PRODUCT
    assert command['execution_sha256'] == digest(blob(execution_name))
    assert command['argv'] == execution['argv'] and command['actual_wait'] and result['actual_wait']
    assert command['actual_exit'] == result['actual_exit'] == 0 and not result['timeout']
    assert command['seconds'] == result['seconds'] and result == mime_independent['results'][run]
    assert all(result[k] for k in ['input_before_matches', 'input_after_matches', 'input_unchanged', 'cleanup_clean'])
    assert result['owned_processes_observed'] == owned and result['adopted_wait_count'] == 0
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    assert before['matches_frozen'] and before['execution_sha256'] == digest(blob(execution_name))
    assert before['files'] == execution['files']
    for p, h in execution['files'].items():
        if p.startswith(I['origins']['mime-independent'] + '/'):
            assert h in known, p
        elif p.startswith(I['origins']['mime-author'] + '/workspace/'):
            rel = p[len(I['origins']['mime-author'] + '/workspace/'):]
            assert digest(G[(fix if rel in fix_files else PRODUCT, rel)]) == h
    assert cleanup['child_actual_wait'] and cleanup['clean'] and cleanup['tmp_removed']
    assert len(cleanup['observed_owned_pid_starttime']) == owned and cleanup['adopted_waits'] == []
    assert not cleanup['services_or_listeners_started'] and not cleanup['docker_or_browser_started']
    for n in ['scan1', 'scan2']:
        assert cleanup[n]['known_pid_starttime_remaining'] == cleanup[n]['owned_descendants_remaining'] == []
assert len(item('mime-independent/runs/go01/oracle.json')) == 298
assert b'formal_vectors=298 full_record_and_exact_branch_checks=378 nine_branches=true' in blob('mime-independent/runs/go01/raw.log')
node = item('mime-independent/runs/node01/raw.log')
assert node['formalVectors'] == 298 and node['branches'] == 9 and node['checks'] == 5364
assert node['flags'] == ['', 'u'] and len(node['endings']) == 5
assert all(not e[k] for e in node['endings'] for k in ['bareDollar', 'bareDollarUnicode', 'oldFullField', 'currentFullField'])
assert node['old257PatternMatches'] and node['old257FullFieldRejects'] and node['current257FullFieldRejects']

# Only the new report, header and append-only administrative changes are checked for format.
old = {p: G[(PRODUCT, p)] for p in doc_paths}
new = {p: (DOCS / p).read_bytes() for p in doc_paths}
card = I['card']; marker = b'## 1. '
assert new[card][new[card].index(marker):] == old[card][old[card].index(marker):]
assert digest(new[card][new[card].index(marker):]) == TECH
continuation = doc_paths[2]
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].lstrip().startswith(b'## 30. ')
for p in doc_paths[:2]:
    point = old[p].index(b'The [System Audit management HTTP specification]') if p == 'AGENTS.md' else old[p].index(b'\n\n') + 2
    assert new[p].startswith(old[p][:point]) and new[p].endswith(old[p][point:])
links = 0
for p in doc_paths + [I['report']]:
    data = (DOCS / p).read_bytes(); text = data.decode('utf-8')
    assert b'\r' not in data and data.endswith(b'\n') and not any(line.endswith((b' ', b'\t')) for line in data.splitlines())
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0
    prior = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old.get(p, b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', text):
        if target in prior or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
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
c = I['counts']
assert len(I['artifacts']) == c['logical_artifacts'] and len(O) == c['objects']
assert sum(map(len, O.values())) == c['object_bytes']
print(json.dumps({'result': 'PASS; archived bytes/Git/recorded results only; no product execution',
                  'mime_fix_commit': fix, 'logical_artifacts': len(I['artifacts']), 'objects': len(O),
                  'object_bytes': c['object_bytes'], 'git_requests': len(G), 'old_product_paths': 14,
                  'source_versions': sum(map(len, I['source_versions'].values())),
                  'original_native_pg_rounds': 6, 'main_compatibility_commands': 5, 'mime_independent_commands': 2,
                  'raw_result_pairs': raw_pairs, 'recorded_actual_waits': actual_wait_records,
                  'new_local_links_checked': links}, ensure_ascii=False))
