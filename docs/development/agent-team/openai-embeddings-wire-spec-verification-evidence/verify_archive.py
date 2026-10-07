#!/usr/bin/env python3
"""Check archived bytes, fixed Git and documentation only; execute no saved source scripts."""
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
I = json.loads((HERE/'index.json').read_bytes())
BASE = '15d7e08fd72eb2c05f7ef64c14cc0de4f5a87597'
FIXED = 'c1427fa4fb7fa118b12b2fb1f5d5f8917113ce2c'
TECH = '1320107dd2bebba97188c976780a111d0ec12e2fb11ae866c9ad3473b5e53133'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')
digest = lambda b: hashlib.sha256(b).hexdigest()
body = lambda b: b[b.index(b'## 1. '):]
assert I['formal_spec_commit'] == I['administrative_base'] == BASE
assert I['fixed_product'] == FIXED and I['technical_sha256'] == TECH
O, G = {}, {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/'+h
    p = HERE/meta['path']; assert p.is_file() and not p.is_symlink()
    data = p.read_bytes(); assert digest(data) == h and len(data) == meta['bytes']
    O[h] = data
assert {p.name for p in (HERE/'objects').iterdir()} == set(O)
assert {r['sha256'] for r in I['artifacts'].values() if 'object' in r} == set(O)

def read_git(requests):
    order = [key for key in requests if key not in G]
    if order:
        raw = subprocess.check_output(['git','cat-file','--batch'], cwd=ROOT, env=ENV,
            input=''.join(f'{c}:{p}\n' for c,p in order).encode())
        at = 0
        for key in order:
            end = raw.index(b'\n',at); fields = raw[at:end].split()
            assert len(fields) == 3 and fields[1] == b'blob', (key,fields)
            n = int(fields[2]); data = raw[end+1:end+1+n]; at = end+n+2
            assert len(data) == n and raw[at-1:at] == b'\n'; G[key] = data
        assert at == len(raw)
    for key,h in requests.items():
        assert h is None or digest(G[key]) == h, key

read_git({(r['git']['commit'],r['git']['path']):r['sha256'] for r in I['artifacts'].values() if 'git' in r})
def blob(name):
    r = I['artifacts'][name]
    return G[(r['git']['commit'],r['git']['path'])] if 'git' in r else O[r['sha256']]
def item(name):
    return json.loads(blob(name))
for name,r in I['artifacts'].items():
    data = blob(name); assert digest(data) == r['sha256'] and len(data) == r['bytes'], name
    assert not any(part in Path(r['origin']).parts for part in ('gocache','node_modules','modcache','runtime'))
for prefix, manifest in [('author','freeze.json'),('formal','delivery.json')]:
    for name,r in item(prefix+'/'+manifest)['files'].items():
        data = blob(prefix+'/'+name)
        assert digest(data) == r['sha256'] and len(data) == r['bytes'], name
original = blob('author/d09-openai-embeddings-wire.draft.md')
formal = blob('formal/d09-openai-embeddings-wire.md')
assert digest(original) == I['draft_sha256'] == '91767b4685c37209648fca103672db33de537031ca30c51c1e40cc4de28d36f0'
assert digest(formal) == I['full_formal_sha256'] == '9154874ac03b0e6b4563b1eb994f5dcde0b11a66ce305953c6cf550d28585b15'
assert body(original) == body(formal) and digest(body(formal)) == TECH
delta = blob('formal/header.diff').decode().splitlines(True)
assert ''.join(difflib.unified_diff(original.decode().splitlines(True), formal.decode().splitlines(True),
    fromfile=delta[0][4:].rstrip('\n'), tofile=delta[1][4:].rstrip('\n'))) == ''.join(delta)
formal_check = item('formal/checks.json')
assert formal_check['header_only'] and formal_check['technical_bytes_equal']
assert formal_check['technical_sha256'] == TECH and formal_check['card_sha256'] == digest(formal)
checks = item('author/checks.json')
assert checks['source_count'] == 31 and checks['candidate_count'] == 16
assert checks['official_source_count'] == 6 and checks['failed_public_GET_records'] == 2
assert not checks['product_checks_run'] and not checks['resources_started']
capacity = checks['parser_capacity_arithmetic']
assert sum(capacity[k] for k in ('raw_max_bytes','vectors_max_bytes','return_copy_max_bytes','aux_max_bytes')) == capacity['sum_bytes']
assert capacity['sum_bytes'] == 21037057 < capacity['bound_bytes_exclusive'] == 22020096
original_check = item('author/self-check-result.json')
assert original_check['actual_exit'] == 0
assert json.loads(original_check['stdout'])['checks'] == 'PASS: static self-check only'
assert not {'actual_wait','actual_wait_completed','raw_sha256','env'} & set(original_check)
review = item('independent/review.json')
assert review['status'] == 'STATIC PASS' and not review['blocking_findings']
assert review['review_md_sha256'] == digest(blob('independent/review.md')) == '208d90a678e22364debe5f59b86abd9b21a0449566815cf1d28203c828456f51'
assert review['full_sha256'] == digest(original) and review['technical_sha256'] == TECH
assert review['no_product_execution'] and review['no_network'] and review['no_resources']
assert review['input_check']['sha256'] == digest(blob('independent/input-check01.json'))
assert review['additional_input']['sha256'] == digest(blob('independent/additional-input01.json'))

inputs = item('author/inputs.json'); checked = item('independent/input-check01.json')
extra = item('independent/additional-input01.json')
assert inputs['fixed_git'] == checked['fixed_git'] == extra['fixed_git'] == FIXED
assert len(inputs['files']) == len(checked['fixed_files']) == review['input_check']['git_files'] == 31
assert extra['path'] == 'internal/central/outbound/client.go' and review['additional_input']['git_files'] == 1
refs = [{'commit':FIXED,'path':p,'bytes':r['bytes'],'sha256':r['sha256'],'scope':'author-original-31'} for p,r in inputs['files'].items()]
refs.append({'commit':FIXED,'path':extra['path'],'bytes':extra['bytes'],'sha256':extra['sha256'],'scope':'independent-additional-1'})
assert refs == I['fixed_git_inputs'] and len({r['path'] for r in refs}) == 32
for p,r in inputs['files'].items():
    c = checked['fixed_files'][p]
    assert c['git'] == FIXED and c['bytes'] == r['bytes'] and c['sha256'] == r['sha256']
for p,r in item('author/freeze.json')['files'].items():
    c = checked['private_frozen_files'][p]
    assert c['bytes'] == r['bytes'] and c['sha256'] == r['sha256']
requests = {(r['commit'],r['path']):r['sha256'] for r in refs}
admin = ['AGENTS.md','docs/development/agent-team/tasks.md',
         'docs/development/agent-team/recovery-2026-10-06-continuation.md',I['card']]
requests.update({(BASE,p):None for p in admin})
read_git(requests)
assert all(len(G[(r['commit'],r['path'])]) == r['bytes'] for r in refs)
scope = inputs['candidate_paths']
assert scope == I['candidate_scope'] and len(scope) == len({r['path'] for r in scope}) == 16
assert sum(r['existing'] for r in scope) == 4
out = subprocess.check_output(['git','cat-file','--batch-check'],cwd=ROOT,env=ENV,
    input=''.join(FIXED+':'+r['path']+'\n' for r in scope).encode()).splitlines()
assert len(out) == 16
for row,meta in zip(out,scope):
    if meta['existing']:
        assert row.split()[1] == b'blob' and digest(G[(FIXED,meta['path'])]) == meta['base_sha256']
    else:
        assert row == (FIXED+':'+meta['path']+' missing').encode() and meta['base_sha256'] is None

records = item('author/official/fetch.json')['records'] + item('author/official/fetch-extra.json')['records']
records.append(item('author/official/fetch-cookbook.json'))
successful = [r for r in records if r.get('status') == 200]
failed = [r for r in records if 'error' in r]
assert len(successful) == 6 and len(failed) == 2
assert all('Tunnel connection failed: 403 Forbidden' in r['error'] for r in failed)
official = {p:r for p,r in inputs['official'].items() if r.get('actual_status') == 200}
assert len(official) == 6
for p,r in official.items():
    assert r['revision'] == 'becc1d20eed83c1b8d85e15dc131a372d9dc7813'
    assert digest(blob('author/'+p)) == r['sha256'] and len(blob('author/'+p)) == r['bytes']
    match = [q for q in successful if q['url'] == r['url']]
    assert len(match) == 1 and match[0]['sha256'] == r['sha256'] and match[0]['bytes'] == r['bytes']
assert not any(name.endswith(('embeddings-guide.html','Embedding_long_inputs.ipynb')) for name in I['artifacts'])
state = I['coordination_only']
assert state['backend_A_five_paths_started']
assert not state['constructor_Start_handle_implemented'] and not state['budget_transport_fixture_resources_started']
assert not state['implementation_independently_accepted'] and not I['implementation_accepted'] and not I['resources_run_by_this_archive']
assert state['runtime_UI_author_C_frozen_1875'] and state['runtime_UI_independent_C_started']
assert not state['runtime_UI_product_accepted'] and not state['runtime_UI_real_resources_started']

old = {p:G[(BASE,p)] for p in admin}
new = {p:(DOCS/p).read_bytes() for p in admin}
assert body(new[I['card']]) == body(old[I['card']]) and digest(body(new[I['card']])) == TECH
assert b'\n\n## 1. ' in new[I['card']]
assert b'/workspace/scratch/' not in new[I['card']][:new[I['card']].index(b'## 1. ')]
assert new[admin[2]].startswith(old[admin[2]])
assert new[admin[2]][len(old[admin[2]]):].lstrip().startswith(b'## 36. ')
for p in admin[:2]:
    at = old[p].index(b'The [System Runtime Information UI specification]') if p == 'AGENTS.md' else old[p].index(b'\n\n')+2
    assert new[p].startswith(old[p][:at]) and new[p].endswith(old[p][at:])
links = 0
for p in admin + [I['report']]:
    data = (DOCS/p).read_bytes(); text = data.decode('utf-8')
    assert b'\r' not in data and data.endswith(b'\n') and not any(line.endswith((b' ',b'\t')) for line in data.splitlines())
    assert len(re.findall(r'^```',text,re.M)) % 2 == 0
    previous = set(re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)',old.get(p,b'').decode()))
    for target in re.findall(r'(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)',text):
        if target in previous or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:',target):
            continue
        filename,_,fragment = target.strip('<>').partition('#')
        rel = (Path(p).parent/unquote(filename)).as_posix() if filename else p
        dest = (DOCS/rel).resolve()
        if not dest.is_file():
            dest = (ROOT/rel).resolve()
        assert dest.is_file(),(p,target)
        if fragment:
            headings = re.findall(r'^#+\s+(.+?)\s*#*$',dest.read_text(),re.M)
            assert unquote(fragment) in {re.sub(r'[^\w\-\s]','',h.lower()).replace(' ','-') for h in headings}
        links += 1
assert len(I['artifacts']) == I['counts']['logical_artifacts'] == 27
assert len(O) == I['counts']['objects'] == 26
assert sum(map(len,O.values())) == I['counts']['object_bytes'] == 122343
assert I['gaps'] == [] and I['counts']['missing_required_originals'] == 0
print(json.dumps({'result':'PASS; static originals/fixed Git/documents only','formal_spec_commit':BASE,
    'logical_artifacts':len(I['artifacts']),'objects':len(O),'object_bytes':sum(map(len,O.values())),
    'author_fixed_git_inputs':31,'independent_additional_git_inputs':1,'git_blobs_read':len(G),
    'candidate_paths':16,'official_sources':6,'proxy403_records':2,'new_local_links_checked':links,
    'implementation_accepted':False,'product_or_resource_execution':False},ensure_ascii=False))
