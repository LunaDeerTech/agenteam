#!/usr/bin/env python3
"""Read saved specification bytes and fixed Git; execute no archived/product code."""
import argparse
import difflib
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import re
import subprocess
from urllib.parse import unquote, urlsplit

HERE = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', type=Path, default=HERE.parents[3])
parser.add_argument('--documents', type=Path, default=HERE.parents[3])
args = parser.parse_args()
REPO, DOCS = args.repo.resolve(), args.documents.resolve()
INDEX = json.loads((HERE / 'index.json').read_bytes())
FIXED = 'f843506d9ec991be1c334a81b88d5cbacc277467'
PRODUCT = '213cf5c3f552e6b05b541ce02afc1dd65ce9db93'
SPEC = '24da141652bbc4c183cd827e767dbdaf2f2f4051'
TECH = '687c85d4a41880828d038251bf6321accc48092aa043a10addb1606ab02c6f21'
CARD = 'docs/development/work-items/d04-system-audit-management-http.md'
TEAM = 'docs/development/agent-team/'
NAME = 'system-audit-management-http-spec-verification'
REPORT = TEAM + NAME + '.md'
EVIDENCE = TEAM + NAME + '-evidence'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def body(data):
    return data[re.search(rb'^## 1\.', data, re.M).start():]


assert INDEX['fixed_git'] == FIXED and INDEX['product_base'] == PRODUCT
assert INDEX['spec_commit'] == INDEX['administrative_base'] == SPEC
assert INDEX['card'] == CARD and INDEX['technical_sha256'] == TECH
assert INDEX['dynamic_acceptance'] is False
objects = {}
for digest, meta in INDEX['objects'].items():
    assert re.fullmatch(r'[0-9a-f]{64}', digest)
    assert meta['path'] == 'objects/' + digest
    path = HERE / meta['path']
    assert path.is_file() and not path.is_symlink()
    data = path.read_bytes()
    assert sha(data) == digest and len(data) == meta['bytes']
    objects[digest] = data
assert {p.name for p in (HERE / 'objects').iterdir()} == set(objects)
assert {x['sha256'] for x in INDEX['artifacts'].values() if 'object' in x} == set(objects)
git = {}


def git_many(requests):
    requests = sorted(set(requests) - set(git))
    if not requests:
        return
    result = subprocess.run(['git', 'cat-file', '--batch'], cwd=REPO, env=ENV,
                            input=''.join(f'{c}:{p}\n' for c, p in requests).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout
    offset = 0
    for key in requests:
        end = result.index(b'\n', offset)
        header = result[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', key
        length = int(header[2])
        data = result[end + 1:end + 1 + length]
        assert result[end + 1 + length:end + 2 + length] == b'\n'
        git[key] = (header[0].decode(), data)
        offset = end + 2 + length
    assert offset == len(result)


refs = INDEX['fixed_git_references']
assert len(refs) == len({r['path'] for r in refs}) == 52
assert {r['commit'] for r in refs} == {FIXED}
git_many([(FIXED, r['path']) for r in refs] + [(SPEC, CARD)] + [(SPEC, p) for p in INDEX['administrative_insertions']])
for item in refs:
    blob, data = git[(FIXED, item['path'])]
    assert blob == item['git_blob'] and sha(data) == item['sha256'] and len(data) == item['bytes']


def artifact(name):
    meta = INDEX['artifacts'][name]
    if 'object' in meta:
        assert meta['object'] == 'objects/' + meta['sha256']
        data = objects[meta['sha256']]
    else:
        assert meta['git'] == {'commit': SPEC, 'path': CARD}
        data = git[(SPEC, CARD)][1]
    assert sha(data) == meta['sha256'] and len(data) == meta['bytes']
    return data


def record(name):
    return json.loads(artifact(name))


for name in INDEX['artifacts']:
    artifact(name)
draft, formal = artifact('author-draft'), artifact('formal-card')
assert sha(draft) == '91d576400771047c9abda0c5d4fd983d331bfd8c1ff86096701c8edd641756b0'
assert sha(formal) == '5fdc1dde616c89840fa583213377e078c636284b79be57e4234095031ac777f0'
assert body(draft) == body(formal) and sha(body(formal)) == TECH
assert re.findall(rb'^## ([1-7])\.', body(formal), re.M) == [str(n).encode() for n in range(1, 8)]
added_diff = ''.join(difflib.unified_diff([], draft.decode().splitlines(True), fromfile='/dev/null', tofile=CARD))
assert added_diff.encode() == artifact('author-diff')
header_diff = artifact('formal-header_diff').decode()
headers = header_diff.splitlines()[:2]
assert headers[0].startswith('--- ') and headers[1].startswith('+++ ')
assert ''.join(difflib.unified_diff(draft.decode().splitlines(True), formal.decode().splitlines(True), fromfile=headers[0][4:], tofile=headers[1][4:])) == header_diff

basis, inputs = record('author-basis'), record('independent-review-inputs.json')
review, arithmetic = record('independent-review.json'), record('independent-static-arithmetic.json')
assert inputs['fixed_git'] == basis['fixed_git'] == FIXED
assert inputs['product_git'] == basis['product_git'] == PRODUCT
assert inputs['technical_sha256'] == basis['technical_sha256'] == TECH
assert len(inputs['fixed_git_files']) == 47
independent_paths = {x['path'] for x in inputs['fixed_git_files']}
assert {r['path'] for r in refs if r['role'] == 'independent static source'} == independent_paths
for item in inputs['fixed_git_files']:
    data = git[(FIXED, item['path'])][1]
    assert sha(data) == item['sha256'] and len(data) == item['bytes']
assert len(basis['fixed_sources']) == 26
for item in basis['fixed_sources']:
    assert item['path'] in independent_paths
    assert sha(git[(FIXED, item['path'])][1]) == item['sha256']
assert len(basis['link_checks']) == 10
for item in basis['link_checks']:
    assert sha(git[(FIXED, item['fixed_path'])][1]) == item['sha256']

paths = inputs['candidate_paths']
assert paths == basis['candidates'] and len(paths) == 14
assert [p['number'] for p in paths] == list(range(1, 15))
assert len({p['path'] for p in paths}) == 14
table = [(int(n), path.decode()) for n, path in re.findall(rb'^\| (\d+) \| `([^`]+)`', formal, re.M)]
assert table == [(p['number'], p['path']) for p in paths]
queries = [FIXED + ':' + p['path'] for p in paths]
observed = subprocess.run(['git', 'cat-file', '--batch-check'], cwd=REPO, env=ENV,
                          input=('\n'.join(queries) + '\n').encode(), capture_output=True, check=True).stdout.decode().splitlines()
exists = []
for query, item, result in zip(queries, paths, observed):
    if result == query + ' missing':
        continue
    assert len(result.split()) == 3 and result.split()[1] == 'blob'
    exists.append(item['path'])
assert exists == ['internal/central/audit/query.go', 'internal/central/app/account.go', 'docs/development/backend/audit.md']

assert review['decision'] == 'STATIC PASS' and review['blocking_findings'] == []
assert review['report']['sha256'] == sha(artifact('independent-review.md'))
assert review['inputs']['sha256'] == sha(artifact('independent-review-inputs.json'))
assert review['static_arithmetic']['sha256'] == sha(artifact('independent-static-arithmetic.json'))
assert review['spec']['technical_sha256'] == TECH and review['spec']['sha256'] == sha(draft)
assert 'Logout' in review['confirmed_implementation_choice'] and 'no SQL role edits' in review['confirmed_implementation_choice']
assert len(review['new_required_selectors']) == 3
assert review['existing_regression_selector'] == 'TestAuditPaginationBindingsFiltersAndRevocation'
assert 'Two auxiliary inspection failures' in '\n'.join(review['limits'])
assert arithmetic['shell_bytes'] + arithmetic['metadata_limit'] == arithmetic['record_bound'] == 4835
assert arithmetic['record_bound'] * 200 + arithmetic['page_overhead'] == arithmetic['page200_bound'] == 975420
assert arithmetic['page200_bound'] < arithmetic['budget'] == 1048576
assert sum(arithmetic['action_family_counts'].values()) == len(set(arithmetic['system_actions'])) == 37
assert len(set(arithmetic['services'])) == 13 and len(set(arithmetic['system_resources'])) == 17
assert len(set(arithmetic['system_associations'])) == 5
assert basis['json_bound']['page200_bound'] == arithmetic['page200_bound']
assert basis['scope']['test_build_or_resource_execution'] is False
checks = record('formal-checks')
assert checks['technical_bytes_unchanged'] and checks['original_private_bytes_unchanged']
assert checks['candidate_count'] == 14 and checks['technical_sha256'] == TECH
assert checks['independent_report']['sha256'] == sha(artifact('independent-review.md'))
assert record('author-freeze')['diff']['sha256'] == sha(artifact('author-diff'))
assert record('formal-freeze')['header_diff']['sha256'] == sha(artifact('formal-header_diff'))

assert INDEX['statistics'] == {
    'additional_author_link_targets': 5, 'candidate_paths': 14, 'distinct_static_git_paths': 52,
    'existing_candidates': 3, 'formal_git_originals': 1, 'independent_git_sources': 47,
    'logical_artifacts': len(INDEX['artifacts']), 'new_candidates': 11,
    'object_bytes': sum(len(x) for x in objects.values()), 'objects': len(objects),
}
assert len(INDEX['artifacts']) == 12 and len(objects) == 11
changed = {}
for path, item in INDEX['administrative_insertions'].items():
    prior = git[(SPEC, path)][1]
    current = (DOCS / path).read_bytes()
    offset, length = item['offset'], item['added_bytes']
    assert sha(prior) == item['baseline_sha256']
    assert current[:offset] + current[offset + length:] == prior, path
    changed[path] = current[offset:offset + length]
    assert sha(changed[path]) == item['added_sha256']
continuation = TEAM + 'recovery-2026-10-06-continuation.md'
assert INDEX['administrative_insertions'][continuation]['offset'] == len(git[(SPEC, continuation)][1])
assert re.findall(rb'^## (\d+)\.', changed[continuation], re.M) == [b'28']
current_card = (DOCS / CARD).read_bytes()
assert body(current_card) == body(formal)
changed[CARD] = current_card[:len(current_card) - len(body(current_card))]
assert b'objects/ed4cc13288cdb39218e68a53a1b92ac0b622e813d81e4534673136ee42befb79' in changed[CARD]
for path in [REPORT, EVIDENCE + '/README.md']:
    changed[path] = (DOCS / path).read_bytes()


def anchors(data):
    result, seen = set(), {}
    for heading in re.findall(r'^#{1,6}\s+(.+?)\s*#*$', data.decode(), re.M):
        heading = re.sub(r'\[([^]]+)\]\([^)]*\)', r'\1', heading)
        heading = re.sub(r'<[^>]*>', '', heading).lower().replace('`', '')
        slug = re.sub(r'[^\w\-\s]', '', heading).replace(' ', '-')
        count = seen.get(slug, 0)
        result.add(slug if count == 0 else f'{slug}-{count}')
        seen[slug] = count + 1
    return result


links = []
for path, data in changed.items():
    text = data.decode('utf-8')
    assert data.endswith(b'\n') and b'\r' not in data and not re.search(rb'[ \t]+$', data, re.M)
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0
    for target in re.findall(r'\[[^\]\n]*\]\(([^)\s]+)\)', text):
        parsed = urlsplit(target)
        if parsed.scheme or parsed.netloc:
            continue
        rel = posixpath.normpath(str(PurePosixPath(path).parent / unquote(parsed.path))) if parsed.path else path
        assert not rel.startswith('../') and not rel.startswith('/'), (path, target)
        links.append((path, target, rel, unquote(parsed.fragment)))
git_many([(SPEC, rel) for _, _, rel, _ in links if not (DOCS / rel).is_file()])
for source, target, rel, fragment in links:
    data = (DOCS / rel).read_bytes() if (DOCS / rel).is_file() else git[(SPEC, rel)][1]
    assert not fragment or fragment in anchors(data), (source, target)
for item in basis['link_checks']:
    fragment = unquote(urlsplit(item['target']).fragment)
    assert not fragment or fragment in anchors(git[(FIXED, item['fixed_path'])][1])
for path in [HERE / 'index.json', HERE / 'verify_archive.py']:
    data = path.read_bytes()
    data.decode('utf-8')
    assert data.endswith(b'\n') and b'\r' not in data and not re.search(rb'[ \t]+$', data, re.M)
print(json.dumps({'result': 'PASS', 'kind': 'offline bytes/fixed Git only', **INDEX['statistics'],
                  'exact_original_diffs': 2, 'new_local_link_occurrences': len(links),
                  'preserved_history': 'card technical1-7 and continuation1-27',
                  'technical_sha256': TECH, 'product_or_archived_code_executed': False}, ensure_ascii=False, sort_keys=True))
