#!/usr/bin/env python3
"""Check fixed specification originals and Git references; execute no product code."""
import argparse
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote

HERE = Path(__file__).resolve().parent
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--repo', type=Path, default=HERE.parents[3])
p.add_argument('--documents', type=Path)
args = p.parse_args()
ROOT = args.repo.resolve()
DOCS = (args.documents or ROOT).resolve()
I = json.loads((HERE / 'index.json').read_bytes())
BASE = '94eda044b06104f740a98367e05eab096c374caa'
FORMAL = 'a7a29c34acbe392d9a1375967309123b9a74ec16'
HTTP = 'b124650aee095b26191bc181dc8cf5d6e0f977f5'
MIME = 'fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5'
TECH = 'e7c96b588eed6218030ef6ce42154c7967667ff156bd8c343d0cdd14cf2bd8d5'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(b):
    return hashlib.sha256(b).hexdigest()


assert I['administrative_base'] == BASE and I['formal_specification_commit'] == FORMAL
assert I['technical_sha256'] == TECH and I['original_http_git'] == HTTP
assert I['subsequent_mime_fix_git'] == MIME
requests = {key: meta['sha256'] for key, meta in I['git_refs'].items()}
for path, meta in I['baseline'].items():
    requests[BASE + ':' + path] = meta['sha256']
order = list(requests)
r = subprocess.run(['git', 'cat-file', '--batch'], cwd=ROOT, env=ENV, check=True,
                   input=''.join(k + '\n' for k in order).encode(), stdout=subprocess.PIPE,
                   stderr=subprocess.PIPE)
G, offset = {}, 0
for key in order:
    end = r.stdout.index(b'\n', offset)
    header = r.stdout[offset:end].split()
    assert len(header) == 3 and header[1] == b'blob', key
    size = int(header[2]); data = r.stdout[end + 1:end + 1 + size]
    offset = end + size + 2
    assert len(data) == size and digest(data) == requests[key], key
    G[key] = data
assert offset == len(r.stdout)
O = {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    target = HERE / meta['path']
    assert not target.is_symlink()
    data = target.read_bytes()
    assert digest(data) == h and len(data) == meta['bytes']
    O[h] = data
assert set(O) == {p.name for p in (HERE / 'objects').iterdir()}
assert set(O) == {v['sha256'] for v in I['artifacts'].values() if 'object' in v}


def blob(name):
    meta = I['artifacts'][name]
    return G[meta['git_ref']] if 'git_ref' in meta else O[meta['sha256']]


def item(name):
    return json.loads(blob(name))


for name, meta in I['artifacts'].items():
    b = blob(name)
    assert digest(b) == meta['sha256'] and len(b) == meta['bytes'], name
assert len(I['artifacts']) == 13 and len(O) == 10 and len(I['git_refs']) == 54
assert sum(map(len, O.values())) == I['counts']['object_bytes'] == 100047
draft = blob('author/d27-system-audit-ui.md')
formal = G[FORMAL + ':' + I['card']]
assert formal == blob('author/formal-header01/d27-system-audit-ui.md') == blob('plan/accepted-card.md')
assert digest(formal) == I['formal_full_sha256'] == 'a22e10e8ba0531b043afdf5aae214a66dcda58a37088ae220c2654ee1077cac1'
assert draft == blob('review/draft01.md')
assert digest(draft) == '096b18e3e50d1612d3ce03e92073164eff60db35b3732f785297f52aa421b010'
technical = formal[formal.index(b'## 1. '):]
assert technical == draft[draft.index(b'## 1. '):] and digest(technical) == TECH
expected_diff = ''.join(difflib.unified_diff(draft.decode().splitlines(True), formal.decode().splitlines(True),
                                           fromfile='private-rev1/d27-system-audit-ui.md',
                                           tofile='formal-rev1/d27-system-audit-ui.md')).encode()
assert blob('author/formal-header01/header.diff') == expected_diff
review = item('review/review.json')
assert review['verdict'] == 'STATIC PASS' and review['blockers'] == []
assert not review['dynamic_executed'] and not review['implementation_authorized']
assert not review['main_or_git_mutations'] and review['resource_actions'] == []
for key, name in [('report', 'review.md'), ('bounded_sources', 'inputs02.json'), ('static_checks', 'static-checks01.json')]:
    assert review[key]['sha256'] == digest(blob('review/' + name))
inputs1, inputs2 = item('review/inputs01.json'), item('review/inputs02.json')
assert len(inputs1['fixed_source_files']) == 44 and len(inputs2['fixed_source_files']) == 46
old_inputs = {(v['path'], v['sha256']) for v in inputs1['fixed_source_files']}
new_inputs = {(v['path'], v['sha256']) for v in inputs2['fixed_source_files']}
assert old_inputs < new_inputs and len(new_inputs - old_inputs) == 2
for v in inputs2['fixed_source_files']:
    key = v.get('source_git', HTTP) + ':' + v['path']
    assert digest(G[key]) == v['sha256'] and len(G[key]) == v['bytes']
checks = item('review/static-checks01.json')
assert checks['source_fingerprints_checked'] == 46 and checks['source_fingerprints_unchanged']
assert checks['spec_sha256'] == digest(draft) and checks['technical_sha256'] == TECH
paths = [{'number': int(n), 'path': path} for n, path in re.findall(rb'^\|\s*(\d+)\s*\|\s*`([^`]+)`', technical, re.M)]
paths = [{'number': v['number'], 'path': v['path'].decode()} for v in paths]
assert paths == checks['scope36'] and len(paths) == len({v['path'] for v in paths}) == 36
assert [v['number'] for v in paths] == list(range(1, 37))
assert paths[18]['path'] == 'docs/development/frontend/README.md'
assert checks['enum_counts'] == {'filters': 14, 'filter_action': 53, 'filter_resource': 25,
                                'filter_actor': 3, 'filter_outcome': 4, 'output_action': 37,
                                'output_resource': 17, 'output_service': 13, 'output_associations': 5}
assert checks['exact_paths'] == ['/api/v1/system/audit', '/api/v1/system/audit/{id}']
assert checks['read_success_budget_bytes'] == 1048576
assert checks['unchanged_problem_budget_bytes'] == 600000 and checks['unchanged_provider_list_budget_bytes'] == 2097152
assert len(checks['old_browser_count_sites']) == 8
assert sum(v['link8_sites'] for v in checks['old_browser_count_sites']) == 14
assert sum(v['group3_sites'] for v in checks['old_browser_count_sites']) == 4
assert (checks['old_pure_menu_files'], checks['old_pure_group_assertions'], checks['old_exact_return_set']) == (8, 7, 1)
plan, plan_input = item('plan/plan01.json'), item('plan/inputs01.json')
assert plan['plan']['sha256'] == digest(blob('plan/plan01.md'))
assert plan['inputs']['sha256'] == digest(blob('plan/inputs01.json'))
assert not any(plan[k] for k in ['author_candidates_read', 'product_tests_run', 'resources_started', 'private_probe_implementation_written'])
assert plan['planned_groups'] == {'A': 2, 'B': 2, 'C': 2, 'final_real_maximum': 2}
assert plan_input['formal_card']['sha256'] == digest(formal) and plan_input['formal_card']['technical_sha256'] == TECH
assert not plan_input['execution_closure'] and not plan_input['source_snapshots_recopied']
for v in plan_input['accepted_http_rebinding']:
    assert v['equal_Q_input03'] and digest(G[HTTP + ':' + v['path']]) == v['sha256']
assert len(plan_input['accepted_http_rebinding']) == 5
for name, alias in [('static_review', 'review/review.md'), ('bounded_46_sources', 'review/inputs02.json'),
                    ('static_contract_checks', 'review/static-checks01.json')]:
    assert plan_input['references'][name]['sha256'] == digest(blob(alias))
for name, alias in [('accepted_http_independent', 'real-independent/verification-report.md'),
                    ('query_controlled', 'query-independent/verification-report.md'),
                    ('http_controlled', 'http-independent/verification-report.md')]:
    assert plan_input['references'][name]['sha256'] == digest(G[I['reused_http_artifacts'][alias]])
manifest = json.loads(G[I['reused_http_artifacts']['author/input03/manifest.json']])
for v in plan_input['accepted_http_rebinding']:
    assert manifest['files'][v['path']]['sha256'] == v['sha256']
stage = I['root_confirmed_current_stage']
assert not stage['ui_product_accepted'] and stage['resources'] == 'none'

# Only four administrative files change; all old content or technical body is retained.
old = {path: G[BASE + ':' + path] for path in I['baseline']}
new = {path: (DOCS / path).read_bytes() for path in old}
card = I['card']
assert new[card][new[card].index(b'## 1. '):] == technical
continuation = 'docs/development/agent-team/recovery-2026-10-06-continuation.md'
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].lstrip().startswith(b'## 31. ')
for path in ['AGENTS.md', 'docs/development/agent-team/tasks.md']:
    point = old[path].index(b'The [System Audit management HTTP result]') if path == 'AGENTS.md' else old[path].index(b'\n\n') + 2
    assert new[path].startswith(old[path][:point]) and new[path].endswith(old[path][point:])
links = 0
for path in list(old) + [I['report']]:
    data = (DOCS / path).read_bytes(); text = data.decode()
    assert b'\r' not in data and data.endswith(b'\n') and not any(v.endswith((b' ', b'\t')) for v in data.splitlines())
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0
    prior = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', old.get(path, b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)', text):
        if target in prior or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
            continue
        filename, _, fragment = target.strip('<>').partition('#')
        rel = (Path(path).parent / unquote(filename)).as_posix() if filename else path
        dest = (DOCS / rel).resolve()
        if not dest.is_file():
            dest = (ROOT / rel).resolve()
        assert dest.is_file(), (path, target)
        if fragment:
            headings = re.findall(r'^#+\s+(.+?)\s*#*$', dest.read_text(), re.M)
            assert unquote(fragment) in {re.sub(r'[^\w\-\s]', '', h.lower()).replace(' ', '-') for h in headings}, target
        links += 1
print(json.dumps({'result': 'PASS; specification originals/Git/links only; no product execution',
                  'logical_originals': 13, 'objects': 10, 'object_bytes': 100047,
                  'git_refs': 54, 'static_inputs': 46, 'plan_http_rebinding': 5,
                  'candidate_paths': 36, 'unchanged_technical_sha256': TECH,
                  'new_local_links_checked': links}, ensure_ascii=False))
