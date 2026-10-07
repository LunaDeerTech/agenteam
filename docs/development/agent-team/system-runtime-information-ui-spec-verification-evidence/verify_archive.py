#!/usr/bin/env python3
"""Read archived original bytes/fixed Git/documents; execute no product or saved checks."""
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
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', type=Path, default=HERE.parents[3])
parser.add_argument('--documents', type=Path)
args = parser.parse_args()
ROOT = args.repo.resolve()
DOCS = (args.documents or ROOT).resolve()
I = json.loads((HERE / 'index.json').read_bytes())
BASE = '2aceadfef6e7a214bc631822317ccf4283bcabc0'
FIXED = 'a0e73bd8fc7fa40e1f424f5817e7b1b3b281def1'
TECH = '85b3d003c8f257e12fa29e0becd2f23b1f77ea4f64efcc521179567c50f27400'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')
digest = lambda b: hashlib.sha256(b).hexdigest()
body = lambda b: b[b.index(b'## 1. '):]
assert I['formal_spec_commit'] == BASE and I['fixed_product'] == FIXED and I['technical_sha256'] == TECH
O, G = {}, {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    p = HERE / meta['path']; assert p.is_file() and not p.is_symlink()
    b = p.read_bytes(); assert digest(b) == h and len(b) == meta['bytes']
    O[h] = b
assert {p.name for p in (HERE / 'objects').iterdir()} == set(O)
assert {x['sha256'] for x in I['artifacts'].values() if 'object' in x} == set(O)

def read_git(requests):
    order = [k for k in requests if k not in G]
    if order:
        raw = subprocess.check_output(['git', 'cat-file', '--batch'], cwd=ROOT, env=ENV,
                                      input=''.join(f'{c}:{p}\n' for c, p in order).encode())
        at = 0
        for key in order:
            end = raw.index(b'\n', at); fields = raw[at:end].split()
            assert len(fields) == 3 and fields[1] == b'blob', (key, fields)
            n = int(fields[2]); b = raw[end + 1:end + 1 + n]; at = end + n + 2
            assert len(b) == n and raw[at - 1:at] == b'\n'; G[key] = b
        assert at == len(raw)
    for key, h in requests.items():
        assert h is None or digest(G[key]) == h, key

read_git({(r['git']['commit'], r['git']['path']): r['sha256'] for r in I['artifacts'].values() if 'git' in r})
def blob(name):
    r = I['artifacts'][name]
    return G[(r['git']['commit'], r['git']['path'])] if 'git' in r else O[r['sha256']]
def item(name):
    return json.loads(blob(name))

for name, r in I['artifacts'].items():
    b = blob(name); assert digest(b) == r['sha256'] and len(b) == r['bytes'], name
    assert not any(p in Path(r['origin']).parts for p in ('gocache', 'node_modules', 'modcache', 'runtime'))
for prefix in ('rev2', 'formal'):
    for name, r in item(prefix + '/freeze.json')['files'].items():
        b = blob(prefix + '/' + name)
        assert digest(b) == r['sha256'] and len(b) == r['bytes']
origin = blob('original/d27-system-runtime-information-ui.draft.md')
assert origin == blob('rev2/original-rev0.1.draft.md')
rev2 = blob('rev2/d27-system-runtime-information-ui.rev2.draft.md')
formal = blob('formal/d27-system-runtime-information-ui.md')
assert digest(rev2) == I['rev2_sha256'] == 'f3d22a8d416c60410f00c28745b985196c72ccb31b10599cbc58a70b29bebe5e'
assert digest(formal) == I['full_formal_sha256'] == '284d00a91772697b6c9a3b7917c9d2caf877f71ae1c0f73da0966d9a0816c4e3'
assert body(rev2) == body(formal) and digest(body(formal)) == TECH
assert digest(body(origin)) == I['original_technical_sha256']
for n in (2, 3):
    start, end = f'## {n}. '.encode(), f'## {n+1}. '.encode()
    assert origin[origin.index(start):origin.index(end)] == rev2[rev2.index(start):rev2.index(end)]
for name, before, after in [('rev2/rev0.1-to-rev2.diff', origin, rev2), ('formal/private-rev2-to-formal.diff', rev2, formal)]:
    lines = blob(name).decode().splitlines(True)
    actual = ''.join(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                                        fromfile=lines[0][4:].rstrip('\n'), tofile=lines[1][4:].rstrip('\n')))
    assert actual == ''.join(lines), name
for prefix in ('rev2', 'formal'):
    d = item(prefix + '/checks.json')['diff_check']
    assert d['actual_exit'] == 1 and d['stdout'] == d['stderr'] == ''

review = item('independent/review.json')
assert digest(blob('independent/review.md')) == 'ec63c9b2521974faec1244d9b0350f342bdb0afa5acdb035660b7f0363090f07'
assert review['verdict'] == 'STATIC PASS' and not review['blocking_findings'] and not review['mandatory_revisions']
assert review['spec']['sha256'] == digest(rev2) and review['spec']['technical_sha256'] == TECH
assert review['input_check']['sha256'] == digest(blob('independent/input-check01.json'))
assert review['author_inputs']['sha256'] == digest(blob('rev2/inputs.json'))
scope = review['candidate_scope']
assert len(scope) == len({x['path'] for x in scope}) == I['counts']['candidate_paths'] == 35
assert review['candidate_breakdown'] == I['candidate_breakdown'] == {'core': 16, 'legacy_pure': 10, 'legacy_browser': 9}
inputs = item('rev2/inputs.json'); refs = inputs['fixed_git_inputs']
assert len(refs) == review['verified_input_count'] == 55
check = item('independent/input-check01.json')
assert {r['path']: {k: r[k] for k in ('bytes', 'git', 'sha256')} for r in check['fixed_git_inputs']} == refs
assert all(r['git'] == FIXED for r in refs.values())
assert digest(blob('original/inputs.json')) == inputs['original_inputs']['sha256']
requests = {(r['git'], p): r['sha256'] for p, r in refs.items()}
doc_paths = ['AGENTS.md', 'docs/development/agent-team/tasks.md',
             'docs/development/agent-team/recovery-2026-10-06-continuation.md', I['card']]
requests.update({(BASE, p): None for p in doc_paths})
read_git(requests)
assert all(len(G[(r['git'], p)]) == r['bytes'] for p, r in refs.items())
binding = item('independent-plan/formal-binding01.json')
assert binding['formal_git'] == BASE and binding['full_sha256'] == digest(formal) and binding['technical_sha256'] == TECH
assert binding['plan01_sha256'] == digest(blob('independent-plan/plan01.md'))
assert binding['technical_bytes_equal_reviewed_rev2'] and not binding['active_implementation_read']
assert not binding['independent_execution_authorized']
state = I['coordination_only']
assert state['frontend_A_API_three_paths_started'] and state['independent_private_plan_started']
assert not state['owner_page_harness_README_started'] and not state['implementation_independently_accepted']
assert not state['resources_authorized'] and not I['implementation_accepted'] and not I['resources_run_by_this_archive']

old = {p: G[(BASE, p)] for p in doc_paths}
new = {p: (DOCS / p).read_bytes() for p in doc_paths}
assert body(new[I['card']]) == body(old[I['card']]) and digest(body(new[I['card']])) == TECH
assert b'\n\n## 1. ' in new[I['card']]
cont = doc_paths[2]
assert new[cont].startswith(old[cont]) and new[cont][len(old[cont]):].lstrip().startswith(b'## 35. ')
for p in doc_paths[:2]:
    at = old[p].index(b'The [System Runtime Information HTTP result]') if p == 'AGENTS.md' else old[p].index(b'\n\n') + 2
    assert new[p].startswith(old[p][:at]) and new[p].endswith(old[p][at:])
links = 0
for p in doc_paths + [I['report']]:
    b = (DOCS / p).read_bytes(); text = b.decode('utf-8')
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
assert len(I['artifacts']) == I['counts']['logical_artifacts'] == 24 and len(O) == I['counts']['objects'] == 22
assert sum(map(len, O.values())) == I['counts']['object_bytes']
assert I['gaps'] == [] and I['counts']['missing_required_originals'] == 0
print(json.dumps({'result': 'PASS; static originals/fixed Git/documents only', 'formal_spec_commit': BASE,
                  'logical_artifacts': len(I['artifacts']), 'objects': len(O), 'object_bytes': I['counts']['object_bytes'],
                  'fixed_contract_git_inputs': 55, 'git_blobs_read': len(G), 'candidate_paths': 35,
                  'new_local_links_checked': links, 'implementation_accepted': False, 'product_or_resource_execution': False}, ensure_ascii=False))
