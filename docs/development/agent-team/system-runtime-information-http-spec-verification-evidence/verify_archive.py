#!/usr/bin/env python3
"""Verify frozen specification bytes and fixed Git; run no product or resource checks."""
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
BASE = 'ddb5044109d0c701b99a60f8f9ab0af7c937469d'
FORMAL = '1cf08c70db1b88e7e8dd5f635193b5752fef4452'
BACKEND = 'b124650aee095b26191bc181dc8cf5d6e0f977f5'
MIME = 'fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5'
TECH = 'aeb4222baf0e2d4547abf13277dc7789f4157e8c3d98843c73377e00d31d2bab'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def digest(b):
    return hashlib.sha256(b).hexdigest()


assert I['administrative_base'] == BASE and I['formal_commit'] == FORMAL
assert I['backend_git'] == BACKEND and I['unchanged_sources_at_mime_git'] == MIME
assert I['technical_sha256'] == TECH
requests = {key: meta['sha256'] for key, meta in I['git_refs'].items()}
for meta in I['git_refs'].values():
    if meta['role'] == 'bounded-static-source':
        requests[MIME + ':' + meta['path']] = meta['sha256']
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
    assert digest(data) == requests[key], key
    G[key] = data
assert offset == len(r.stdout)
O = {}
for h, meta in I['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', h) and meta['path'] == 'objects/' + h
    f = HERE / meta['path']; assert f.is_file() and not f.is_symlink()
    b = f.read_bytes(); assert digest(b) == h and len(b) == meta['bytes']
    O[h] = b
assert set(O) == {p.name for p in (HERE / 'objects').iterdir()}
assert set(O) == {v['sha256'] for v in I['artifacts'].values() if 'object' in v}


def blob(name):
    meta = I['artifacts'][name]
    return G[meta['git_ref']] if 'git_ref' in meta else O[meta['sha256']]


def item(name):
    return json.loads(blob(name))


for name, meta in I['artifacts'].items():
    data = blob(name)
    assert digest(data) == meta['sha256'] and len(data) == meta['bytes'], name
assert len(I['artifacts']) == 16 and len(O) == 13 and len(I['git_refs']) == 32
assert sum(map(len, O.values())) == I['counts']['object_bytes'] == 122015
filename = 'd28-system-runtime-information-http.md'
v1 = blob('author/rev1/' + filename)
v2 = blob('author/rev2/' + filename)
f1 = blob('author/formal-header01/' + filename)
f2 = blob('author/formal-header02/' + filename)
assert digest(v1) == 'a2956d5491f5cce1409cff0400571aa031df61bdb0d66ca858872e5193aa451d'
assert digest(v2) == 'bed0c30c025003460d1237f30329ffbfa9b3d9bd95029a751b14c3a794acff7b'
assert digest(f2) == I['formal_full_sha256'] == 'e4cbf79d922c1b1456024e7cbf64fefa73937c75b5916e29c4e18fe66868bdfb'
assert f2 == G[FORMAL + ':' + I['card']]
technical = v2[v2.index(b'## 1. '):]
assert digest(technical) == TECH
assert all(b[b.index(b'## 1. '):] == technical for b in [f1, f2])
for before, after, old_name, new_name, artifact in [
    (v1, v2, 'rev1/' + filename, 'rev2/' + filename, 'author/rev1-to-rev2.diff'),
    (v2, f1, 'rev2/' + filename, 'formal-header01/' + filename, 'author/formal-header01/header.diff'),
    (f1, f2, 'formal-header01/' + filename, 'formal-header02/' + filename, 'author/formal-header02/header01-to-header02.diff'),
]:
    expected = ''.join(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                                           fromfile=old_name, tofile=new_name)).encode()
    assert blob(artifact) == expected, artifact
assert blob('review/spec/rev2/' + filename) == v2
assert blob('review/spec/rev1-to-rev2.diff') == blob('author/rev1-to-rev2.diff')
sections = lambda b: {int(n): text for n, text in re.findall(rb'^## ([1-7])\. (.*?)(?=^## [1-7]\. |\Z)', b, re.M | re.S)}
old_sections, new_sections = sections(v1), sections(v2)
assert set(old_sections) == set(new_sections) == set(range(1, 8))
assert [n for n in range(1, 8) if old_sections[n] != new_sections[n]] == [2, 7]
review = item('review/review.json')
assert review['verdict'] == 'STATIC PASS' and review['blocking_findings'] == []
assert review['spec_full'] == digest(v2) and review['spec_technical'] == TECH
assert review['baseline'] == BACKEND and review['source_count'] == 31 and review['paths_count'] == 14
assert not any(review[k] for k in ['product_compilation_or_tests', 'real_resources', 'product_or_git_mutations'])
for name, h in review['files'].items():
    assert digest(blob('review/' + name)) == h
prior = {}
for version, source_count in [('inputs01.json', 17), ('inputs02.json', 29), ('inputs03.json', 31)]:
    data = item('review/' + version)
    assert data['mode'] == 'STATIC only' and data['baseline'] == BACKEND
    assert len(data['files']) == source_count + 2
    assert all(data['files'].get(k) == v for k, v in prior.items())
    prior = data['files']
    for path, meta in data['files'].items():
        if path.startswith('spec/'):
            assert digest(blob('review/' + path)) == meta['sha256']
        else:
            assert meta['commit'] == BACKEND
            assert digest(G[BACKEND + ':' + path]) == meta['sha256']
            assert G[BACKEND + ':' + path] == G[MIME + ':' + path]
note = item('review/preparation-note01.json')
assert note['git_show_exit'] == 128 and note['outer_read_command_exit'] == 1
assert not note['product_or_test_execution']
assert note['initial_missing_fixed_path'] == 'internal/central/app/diagnostics_test.go'
assert note['actual_path_discovered_by_fixed_tree'] == 'internal/central/app/app_test.go'
checks = item('review/static-checks01.json')
assert checks['full_sha256'] == digest(v2) and checks['tech_sha256'] == TECH
assert not checks['compiled_or_executed_product']
paths = [[n.decode(), path.decode()] for n, path in re.findall(rb'^\|\s*(\d+)\s*\|\s*`([^`]+)`', technical, re.M)]
assert paths == checks['candidate_paths'] and len(paths) == len({v[1] for v in paths}) == 14
assert [int(v[0]) for v in paths] == list(range(1, 15))
assert paths[-1][1] == 'docs/development/backend/README.md'
assert checks['max_fixed_shell_bytes'] == 618 and checks['max_version_escape_bytes'] == 2 * 256 * 6
assert checks['upper_bound_bytes'] == 3690 and checks['success_limit_bytes'] == 16384
assert checks['max_fixed_shell_bytes'] + checks['max_version_escape_bytes'] == checks['upper_bound_bytes'] < checks['success_limit_bytes']
stage = I['root_confirmed_stage']
assert stage['actual_start'] and stage['authorized_implementation_sources'] == 13
assert stage['checks_completed'] and stage['author_service_frozen'] and stage['independent_stage_started']
assert stage['independent_implementation_verification']
assert stage['independent_reproduction_final']
assert not any(stage[k] for k in ['repair_accepted', 'http_stage_started', 'root_stage_started',
                                 'resources_started', 'product_accepted'])

# Administrative additions must retain historical bytes and the entire technical body.
old = {path: G[BASE + ':' + path] for path in I['baseline']}
new = {path: (DOCS / path).read_bytes() for path in old}
card = I['card']
assert new[card][new[card].index(b'## 1. '):] == technical
continuation = 'docs/development/agent-team/recovery-2026-10-06-continuation.md'
assert new[continuation].startswith(old[continuation])
assert new[continuation][len(old[continuation]):].lstrip().startswith(b'## 32. ')
for path in ['AGENTS.md', 'docs/development/agent-team/tasks.md']:
    point = old[path].index(b'The [System Audit read-only UI specification]') if path == 'AGENTS.md' else old[path].index(b'\n\n') + 2
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
print(json.dumps({'result': 'PASS; fixed specification/Git/links only; no product execution',
                  'logical_originals': 16, 'objects': 13, 'object_bytes': 122015,
                  'git_refs': 32, 'static_sources': 31, 'input03_entries': 33,
                  'candidate_paths': 14, 'original_diffs_verified': 3,
                  'unchanged_technical_sha256': TECH, 'new_local_links_checked': links}, ensure_ascii=False))
