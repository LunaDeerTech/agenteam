#!/usr/bin/env python3
"""Check immutable originals, fixed Git and document deltas; execute no product code."""
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
PRODUCT = 'e1f8cefbeaf3203c4e0388f9ea9dbf11e2e2959b'
BASE = 'a0e73bd8fc7fa40e1f424f5817e7b1b3b281def1'
TECH = '85b3d003c8f257e12fa29e0becd2f23b1f77ea4f64efcc521179567c50f27400'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(argv, data=None):
    return subprocess.run(['git', *argv], cwd=ROOT, env=ENV, input=data, check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


assert I['accepted_product_commit'] == PRODUCT and I['fixed_running_product_base'] == BASE
assert I['technical_sha256'] == TECH and I['continuation_new_section'] == 37
O, G = {}, {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    p = HERE / meta['path']
    assert p.is_file() and not p.is_symlink()
    b = p.read_bytes()
    assert digest(b) == h and len(b) == meta['bytes']
    O[h] = b
assert {p.name for p in (HERE / 'objects').iterdir()} == set(O)
assert {r['sha256'] for r in I['artifacts'].values() if 'object' in r} == set(O)


def read_git(requests):
    order = [key for key in requests if key not in G]
    if order:
        raw = git(['cat-file', '--batch'], ''.join(f'{c}:{p}\n' for c, p in order).encode())
        offset = 0
        for key in order:
            end = raw.index(b'\n', offset)
            header = raw[offset:end].split()
            assert len(header) == 3 and header[1] == b'blob', (key, header)
            n = int(header[2])
            G[key] = raw[end + 1:end + 1 + n]
            offset = end + n + 2
            assert len(G[key]) == n and raw[offset - 1:offset] == b'\n'
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


def code(value):
    return next((value[k] for k in ('actual_exit', 'exit_code', 'exit', 'driver_exit') if k in value), None)


read_git({(v['git']['commit'], v['git']['path']): v['sha256']
          for v in I['artifacts'].values() if 'git' in v})
for name, rec in I['artifacts'].items():
    b = blob(name)
    assert digest(b) == rec['sha256'] and len(b) == rec['bytes'], name
    assert ('git' in rec) != ('object' in rec)
    assert not any(p in Path(rec['origin']).parts for p in ('node_modules', 'gocache', 'modcache', 'runtime'))

# Permanent Git identities replace duplicate source bytes, never the old scratch files.
delivery = item('author/delivery35/manifest.json')
files35 = {p: sha(v) for p, v in delivery['sources'].items()}
assert files35 == I['delivery35'] and len(files35) == delivery['source_count'] == 35
assert set(git(['diff-tree', '--no-commit-id', '--name-only', '-r', PRODUCT]).decode().splitlines()) == set(files35)
requests = {(PRODUCT, p): h for p, h in files35.items()}
for rec in I['git_manifest_refs']:
    values = item(rec['artifact'])
    for field in rec['fields']:
        values = values[field]
    assert len(values) == rec['count'] == 957
    requests.update({(rec['commit'], p): sha(v) for p, v in values.items()})
requests.update({(PRODUCT, p): v['base_sha256'] for p, v in I['administrative_base'].items()})
read_git(requests)
known = set(O) | {digest(v) for v in G.values()}
assert len(I['source_versions']) == 35
for p, versions in I['source_versions'].items():
    assert files35[p] in versions, p
    for h, record in versions.items():
        assert digest(blob(record['artifact'])) == h, p

final = item('author/input04/manifest.json')
ind = item('real-independent/execution03.json')
previous_ind = item('real-independent/execution02.json')
files34 = {p: h for p, h in files35.items() if p != 'docs/development/frontend/README.md'}
assert final['files'] == ind['files'] == files34
assert {p: sha(v) for p, v in item('author/author-final01/delivery34.json')['sources'].items()} == files34
assert {p: sha(v) for p, v in item('real-independent/delivery34.json')['files'].items()} == files34
assert final['dist'] == ind['dist'] and len(final['dist']) == 42
assert len(final['dependencies']) == 2892 and len(ind['dependencies']) == 3127
assert len(previous_ind['private_files']) == 44 and len(ind['private_files']) == 47
assert all(ind['private_files'][k] == v for k, v in previous_ind['private_files'].items())
assert len(set(ind['private_files']) - set(previous_ind['private_files'])) == 3
for field in ('dependencies', 'dist', 'driver_sha256', 'accepted_baseline'):
    assert ind[field] == previous_ind[field]
assert {p for p in ind['files'] if ind['files'][p] != previous_ind['files'][p]} == {
    'tests/account-captcha-web/e2e/system-runtime-information.spec.ts'}

# Original failures and formatting mutations retain their original actual exits.
raw_links = 0
for name in I['artifacts']:
    if not name.endswith('/result.json'):
        continue
    value = item(name)
    if not isinstance(value, dict):
        continue
    prefix = name[:-12]
    for field, filename in [('raw_sha256', 'raw.log'), ('command_sha256', 'command.json'),
                            ('input_before_sha256', 'input-before.json'), ('input_after_sha256', 'input-after.json'),
                            ('cleanup_sha256', 'cleanup.json')]:
        target = prefix + '/' + filename
        if field in value and target in I['artifacts']:
            assert digest(blob(target)) == value[field], (name, field)
            raw_links += field == 'raw_sha256'
expected_offline_failures = {
    'author/runs/old-browser-format-ast01': 1,
    'author/runs/old-browser-format-provenance01': 1,
    'author/runs/old-browser-format01': 1,
    'author/runs/old-browser-type01': 2,
    'author/runs/state-pure01': 1,
    'author/runs/state-type01': 2,
}
actual_offline_failures = {}
assert len(I['recorded_offline_results']) == 70
for record in I['recorded_offline_results']:
    prefix = record['artifact_prefix']
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert code(r) == record['result_exit'] and code(c) == record['command_exit'], prefix
    wait = bool(r.get('actual_wait') or r.get('actual_wait_completed') or c.get('actual_wait') or c.get('actual_wait_completed'))
    assert wait and record['actual_wait_recorded'], prefix
    assert r.get('input_unchanged') == record['input_unchanged']
    assert c['argv'] and c['cwd'] and isinstance(c.get('env', c.get('env_overrides')), dict)
    if code(r):
        actual_offline_failures[prefix] = code(r)
    if record['input_unchanged']:
        assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json'), prefix
    if 'owned_processes_double_empty' in r:
        assert r['owned_processes_double_empty'], prefix
assert actual_offline_failures == expected_offline_failures
for prefix, input_count in [('api-independent/runs/node01', 39),
                            ('owner-independent/runs/node01', 81),
                            ('page-independent/runs/app01', 2449),
                            ('real-independent/runs/gate02', 4211)]:
    r, cleanup = item(prefix + '/result.json'), item(prefix + '/cleanup.json')
    assert code(r) == 0 and r['actual_wait'] and r['input_unchanged'] and r['cleanup_clean']
    assert cleanup['clean'] and cleanup['child_actual_wait'] and cleanup['tmp_removed']
    assert len(cleanup['observed_owned_pid_starttime']) == 2 and cleanup['adopted_waits'] == []
    assert not any(cleanup['scan1'].values()) and not any(cleanup['scan2'].values())
    assert not cleanup['services_or_listeners_started'] and not cleanup['docker_or_browser_started']
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    assert len(item(prefix + '/input-before.json')['files']) == input_count


def canonical_resource(value):
    out = dict(value)
    if 'mounts' in out:
        # Sort full serialized objects; duplicate mounts remain duplicate entries.
        out['mounts'] = sorted(out['mounts'], key=lambda v: json.dumps(v, sort_keys=True, separators=(',', ':')))
    return out


# Seven original real runs: two FAIL records are preserved alongside later PASS.
assert len(I['real_rounds']) == 7
failed, mount_order_differences = 0, 0
for record in I['real_rounds']:
    prefix = record['artifact_prefix']
    c, r = item(prefix + '/command.json'), item(prefix + '/result.json')
    assert c['actual_wait_completed'] and c['exit'] == r['driver_exit'] == record['actual_exit']
    assert c['seconds'] == record['seconds'] and c['argv'] and c['cwd'] and isinstance(c['env'], dict)
    failed += c['exit'] != 0
    tops = [[a.decode(), b.decode()] for a, b in re.findall(rb'^--- (PASS|FAIL|SKIP): ([^\s(]+)', blob(prefix + '/raw.log'), re.M)]
    assert tops == record['top_levels'] == r['top_levels']
    assert r['selected_tests_passed'] == (c['exit'] == 0)
    assert r['double_cleanup'] and record['original_double_cleanup'] and r['browser_runtime_removed']
    for field in ('accepted_baseline_before', 'accepted_baseline_after', 'source_files_unchanged', 'verification_inputs_unchanged'):
        assert r[field]
    assert blob(prefix + '/input-before.json') == blob(prefix + '/input-after.json')
    before = item(prefix + '/input-before.json')
    frozen = item(record['frozen_input'])
    assert blob(prefix + '/frozen-input.json') == blob(record['frozen_input'])
    assert len(before['files']) == 34 and len(before['dist']) == 42
    for field in ('files', 'dist', 'dependencies'):
        assert before[field] == frozen[field], (prefix, field)
    assert set(before['files'].values()) <= known
    baseline_input = before['accepted_baseline']
    assert baseline_input['accepted'] and baseline_input['accepted_count'] == 957
    assert baseline_input['files'] == {p: sha(v) for p, v in frozen['accepted_baseline']['files'].items()}
    assert baseline_input['product_base'] == frozen['product_base'] == BASE
    assert baseline_input['git_tree_sha256'] == frozen['accepted_baseline']['git_tree_sha256']
    assert all(not baseline_input[k] for k in ('errors', 'mismatches', 'missing', 'unexpected'))
    assert item(prefix + '/verification-input.json')['driver_sha256'] == digest(blob(prefix + '/driver.py.txt')) == frozen['driver_sha256']
    resources, processes = item(prefix + '/observed-resources.json'), item(prefix + '/observed-processes.json')
    assert len(resources) == r['observed_resources'] == record['resources'] == 7
    assert len(processes) == r['observed_processes'] == record['owned_pid_starttime']
    assert all(k == f'{v["pid"]}:{v["starttime"]}' for k, v in processes.items())
    waits = item(prefix + '/adopted-waits.json')
    assert len(waits) == r['adopted_waits'] == record['adopted_waits']
    assert all(v['actual_wait'] for v in waits)
    assert item(prefix + '/monitor-errors.json') == [] and r['monitor_errors'] == 0
    baseline = item(prefix + '/baseline.json')
    assert set(resources).isdisjoint(baseline)
    assert sorted(v['kind'] for v in baseline.values()) == ['container'] * 2 + ['network'] * 4
    scans = item(prefix + '/cleanup.json')
    assert len(scans) == 2 and scans[0]['time'] < scans[1]['time']
    for scan in scans:
        assert scan['baseline_unchanged'] and scan['baseline_canonical_differences'] == {}
        assert {k: canonical_resource(v) for k, v in scan['current_resources'].items()} == {
            k: canonical_resource(v) for k, v in baseline.items()}
        for difference in scan['baseline_raw_differences'].values():
            assert difference['before'] != difference['after']
            assert canonical_resource(difference['before']) == canonical_resource(difference['after'])
            mount_order_differences += 1
        assert set(scan['exact_absent']) == set(resources) and all(v['absent'] for v in scan['exact_absent'].values())
        assert not any(scan[k] for k in ('owned_processes', 'remaining_new', 'runtime_entries', 'browser_runtime_entries'))
    assert not r['prior_task_owned_unreaped']['reaped_by_this_run']
assert failed == 2 and mount_order_differences > 0

# Image bytes and the actual, narrower review counts are different claims.
assert len(I['images']) == 24
for name, h in I['images'].items():
    data = blob(name)
    assert digest(data) == h and data[:8] == b'\x89PNG\r\n\x1a\n'
    width, height = struct.unpack('>II', data[16:24])
    assert width in (390, 768, 1024, 1440) and height == 900
author_views, root_views = 0, 0
for run in ('navigation01', 'old-audit-navigation01', 'old-outbound-navigation01'):
    prefix = 'author/runs/' + run
    review = item(prefix + '/visual-review.json')
    assert len(review['images']) == 8
    for im in review['images']:
        rec = I['artifacts'][prefix + '/images/' + im['file']]
        assert rec['sha256'] == im['sha256'] and rec['bytes'] == im['bytes']
        author_views += bool(im['author_actually_viewed'])
    root_views += len(review.get('root_actually_viewed', []))
assert author_views == 12 and root_views == 2

# No missing historical execution fingerprints are synthesized from later manifests.
gaps = I['historical_gaps_and_limits']
assert len(gaps) == 5 and all(v['record'] in I['artifacts'] for v in gaps)
assert gaps[0]['execution_before_SHA_not_recorded'] and gaps[0]['not_reconstructed']
assert I['counts']['helper_pre_execution_SHA_gaps'] == 3 and I['counts']['missing_required_source_versions'] == 0

# Administrative insertions retain the entire prior text and the card's technical bytes.
old_docs = {p: G[(PRODUCT, p)] for p in I['administrative_base']}
for p, rec in I['administrative_base'].items():
    old, new = old_docs[p], (DOCS / p).read_bytes()
    assert digest(new) == rec['new_sha256'] and digest(old) == rec['base_sha256']
    assert rec['base_commit'] == PRODUCT
    if rec['mode'] == 'insert':
        at = rec['insertion_offset']
        assert new.startswith(old[:at]) and new.endswith(old[at:]) and len(new) > len(old)
    elif rec['mode'] == 'append':
        assert new.startswith(old) and new[len(old):].lstrip().startswith(b'## 37. ')
    else:
        assert rec['mode'] == 'card_header_only' and p == I['card']
        marker = b'## 1. '
        assert new[new.index(marker):] == old[old.index(marker):]
        assert digest(new[new.index(marker):]) == TECH and b'\n\n## 1. ' in new

links = 0
for p in list(old_docs) + [I['report']]:
    b = (DOCS / p).read_bytes()
    text = b.decode('utf-8')
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
assert len(I['artifacts']) == counts['logical_artifacts'] == 1079
assert len(O) == counts['objects'] == 610 and sum(map(len, O.values())) == counts['object_bytes'] == 36152599
assert sum('git' in v for v in I['artifacts'].values()) == counts['git_backed_artifacts'] == 71
assert sum(len(v['map_aliases']) for v in I['artifacts'].values()) == counts['map_logical_references'] == 1102
assert sum(map(len, I['source_versions'].values())) == counts['source_versions'] == 53
print(json.dumps({'result': 'PASS; original bytes/fixed Git/records only; no product execution',
                  'accepted_product_commit': PRODUCT, 'logical_artifacts': 1079, 'objects': 610,
                  'object_bytes': counts['object_bytes'], 'Git_reads': len(G), 'delivery_paths': 35,
                  'source_versions': 53, 'fixed_Git_dependencies': 957, 'real_rounds': 7,
                  'original_real_failures': failed, 'recorded_offline_results': 70,
                  'original_offline_nonzero': len(actual_offline_failures), 'raw_result_pairs': raw_links,
                  'raw_mount_order_differences_preserved': mount_order_differences,
                  'saved_images': 24, 'author_views': author_views, 'root_views': root_views,
                  'new_local_links_checked': links, 'administrative_files': len(old_docs),
                  'helper_pre_execution_SHA_gaps': 3}, ensure_ascii=False))
