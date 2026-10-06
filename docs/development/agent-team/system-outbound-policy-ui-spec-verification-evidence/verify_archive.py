#!/usr/bin/env python3
"""Read saved bytes and fixed Git only; never execute archived or product code."""
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
REPO = args.repo.resolve()
DOCS = args.documents.resolve()
INDEX = json.loads((HERE / 'index.json').read_bytes())
BASE = '213cf5c3f552e6b05b541ce02afc1dd65ce9db93'
SPEC = 'f843506d9ec991be1c334a81b88d5cbacc277467'
TECH = '343f09b6facd95c000df2f2b35bceeb73f3a7e93d2974ce18304703266d0ab6b'
CARD = 'docs/development/work-items/d27-system-outbound-policy-ui.md'
TEAM = 'docs/development/agent-team/'
NAME = 'system-outbound-policy-ui-spec-verification'
EVIDENCE = TEAM + NAME + '-evidence'
REPORT = TEAM + NAME + '.md'
ENV = dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def body(data):
    match = re.search(rb'^## 1\.', data, re.M)
    assert match, 'missing exact first technical heading'
    return data[match.start():]


def sections(data):
    matches = list(re.finditer(rb'^## ([1-7])\.', data, re.M))
    assert [int(m[1]) for m in matches] == list(range(1, 8))
    return {int(m[1]): data[m.start():matches[i + 1].start() if i + 1 < len(matches) else len(data)] for i, m in enumerate(matches)}


assert INDEX['product_base'] == BASE and INDEX['spec_commit'] == SPEC
assert INDEX['card'] == CARD and INDEX['technical_sha256'] == TECH
assert INDEX['administrative_base'] == SPEC
objects = {}
for digest, item in INDEX['objects'].items():
    assert re.fullmatch('[0-9a-f]{64}', digest)
    assert item['path'] == 'objects/' + digest
    path = HERE / item['path']
    assert path.is_file() and not path.is_symlink()
    raw = path.read_bytes()
    assert sha(raw) == digest and len(raw) == item['bytes'], path
    objects[digest] = raw
assert {p.name for p in (HERE / 'objects').iterdir()} == set(objects)
assert {v['sha256'] for v in INDEX['artifacts'].values() if 'object' in v} == set(objects)
git_cache = {}


def git_many(requests):
    requests = sorted(set(requests) - set(git_cache))
    if not requests:
        return
    completed = subprocess.run(
        ['git', 'cat-file', '--batch'], cwd=REPO, env=ENV,
        input=''.join(f'{c}:{p}\n' for c, p in requests).encode(),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
    )
    raw, offset = completed.stdout, 0
    for key in requests:
        end = raw.index(b'\n', offset)
        header = raw[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', key
        size = int(header[2])
        data = raw[end + 1:end + 1 + size]
        assert raw[end + 1 + size:end + 2 + size] == b'\n'
        git_cache[key] = (header[0].decode(), data)
        offset = end + 2 + size
    assert offset == len(raw)


refs = INDEX['fixed_git_references']
assert len({(r['commit'], r['path']) for r in refs}) == len(refs)
git_many([(r['commit'], r['path']) for r in refs] + [(SPEC, p) for p in INDEX['administrative_insertions']])
for item in refs:
    blob, data = git_cache[(item['commit'], item['path'])]
    assert blob == item['git_blob'] and sha(data) == item['sha256']
    assert len(data) == item['bytes']


def artifact(name):
    item = INDEX['artifacts'][name]
    if 'object' in item:
        assert item['object'] == 'objects/' + item['sha256']
        data = objects[item['sha256']]
    else:
        assert set(item['git']) == {'commit', 'path'}
        data = git_cache[(item['git']['commit'], item['git']['path'])][1]
    assert sha(data) == item['sha256'] and len(data) == item['bytes']
    return data


def record(name):
    return json.loads(artifact(name))


for name in INDEX['artifacts']:
    artifact(name)
basis = record('rev2-basis-json')
assert basis['baseline'] == BASE
paths = basis['candidate_paths']
assert len(paths) == 34 and len({p['path'] for p in paths}) == 34
assert [p['number'] for p in paths] == list(range(1, 35))
assert sum(p['new'] for p in paths) == 12
for item in paths:
    if not item['new']:
        assert sha(git_cache[(BASE, item['path'])][1]) == item['baseline_sha256']
missing_queries = [f'{BASE}:{p["path"]}' for p in paths if p['new']]
absence = subprocess.run(['git', 'cat-file', '--batch-check'], cwd=REPO, env=ENV,
                         input=('\n'.join(missing_queries) + '\n').encode(),
                         capture_output=True, check=True).stdout.decode().splitlines()
assert absence == [q + ' missing' for q in missing_queries]
scope = [(int(n), path) for n, path in re.findall(rb'^\| (\d+) \| `([^`]+)`', artifact('formal-card'), re.M)]
assert scope == [(p['number'], p['path'].encode()) for p in paths]
for mapping in (basis['fixed_read_basis'], record('rev3-basis-json')['fixed_read_basis'], record('rev2-static-check')['extra_read_source_hashes']):
    for path, digest in mapping.items():
        assert sha(git_cache[(BASE, path)][1]) == digest

versions = {
    'draft-rev1': ('7fa69924facaceaa9944364a68fdf53cdb26655eddf2bf89b48c94e19feb1a71', '1ac9677f62485d7db759da79c8420f452617475e6786a3d2113d4d77174fa877'),
    'rev2-card': ('a6b698b72f4e213c8decb360c171e3c27444f39e9df0540aa431d56597080d2a', '66c3653b01f1cb6a5ac81ddd3f8985ace0761957f89b1c8f58410ea6a947208f'),
    'rev3-card': ('efc6c8cb8c87c058c5e3f7db0d68bd3c33d6eef13a42fdcbc56fe0196d0699b0', TECH),
    'formal-card': ('2f7f3dbed8cc4d263f1c1db3576404589c0cedb7c2aec217e84a223d9880579b', TECH),
}
for name, (full, technical) in versions.items():
    assert sha(artifact(name)) == full and sha(body(artifact(name))) == technical
for left, right, diff, fromfile, tofile in [
    ('draft-rev1', 'rev2-card', 'rev1-to-rev2-diff', 'private-draft-rev1.md', 'd27-system-outbound-policy-ui.rev2.md'),
    ('rev2-card', 'rev3-card', 'rev2-to-rev3-diff', 'ui-spec-rev2/d27-system-outbound-policy-ui.md', 'ui-spec-rev3/d27-system-outbound-policy-ui.md'),
    ('rev3-card', 'formal-card', 'formal-header-diff', 'ui-spec-rev3/d27-system-outbound-policy-ui.md', 'formal-header01/d27-system-outbound-policy-ui.md'),
]:
    actual = ''.join(difflib.unified_diff(artifact(left).decode().splitlines(True), artifact(right).decode().splitlines(True), fromfile=fromfile, tofile=tofile)).encode()
    assert actual == artifact(diff), diff
rev2_sections, rev3_sections = sections(artifact('rev2-card')), sections(artifact('rev3-card'))
assert [i for i in range(1, 8) if rev2_sections[i] != rev3_sections[i]] == [4, 5, 7]
assert body(artifact('formal-card')) == body(artifact('rev3-card'))

r2, r3 = record('rev2-review-json'), record('rev3-review-json')
assert r2['result'] == 'STATIC_BLOCKED_PENDING_NARROW_SPEC_REPAIR'
assert [x['id'] for x in r2['blocking_findings']] == ['OUI-SPEC-01']
assert r2['other_substantive_blockers'] == [] and not r2['dynamic_tests_run']
assert r2['review_sha256'] == sha(artifact('rev2-review-md'))
assert r2['static_check_sha256'] == sha(artifact('rev2-static-check'))
assert r3['result'] == 'STATIC_PASS' and r3['closed_finding'] == 'OUI-SPEC-01'
assert r3['review_sha256'] == sha(artifact('rev3-review-md'))
assert r3['technical_sha256'] == TECH and r3['scope_count'] == 34
assert r3['scope_and_budgets_and_real_selectors_unchanged'] and r3['inactive_navigation_is_not_owner_release']
assert not any(r3[k] for k in ['dynamic_tests', 'resources_started', 'product_or_git_mutation'])
static = record('rev2-static-check')
assert static['structural_response_upper_bound_bytes'] == 414763
assert static['upper_bound_is_not_a_valid_max_rule_fixture'] is True
assert static['product_or_git_mutation'] is False and static['resources_started'] is False
for diff in (static['diff_check'], r3['diff_check']):
    assert diff['actual_exit'] == 1 and diff['stdout'] == diff['stderr'] == ''
prep = record('preparation-error')
assert prep['actual_python_exit'] == prep['original_diff_actual_exit'] == 1
assert prep['original_diff_stdout'] == prep['original_diff_stderr'] == ''
assert 'AssertionError: (1, \'\', \'\')' in prep['raw'] and prep['no_business_resources']
formal = record('formal-header-check')
assert formal['technical_sha256'] == TECH and formal['card_sha256'] == sha(artifact('formal-card'))
assert formal['header_diff_sha256'] == sha(artifact('formal-header-diff'))
assert formal['checks']['technical_1_7_byte_identical']
plan = record('independent-plan-json')
assert plan['baseline'] == BASE and plan['technical_sha256'] == TECH
assert plan['plan_sha256'] == sha(artifact('independent-plan-md'))
assert plan['stage_a_expected_frozen_paths'] == [paths[n - 1]['path'] for n in [1, 2, 3, 11, 12]]
assert all(plan[k] == 2 for k in ['planned_api_combinations', 'planned_owner_combinations', 'planned_app_combinations', 'planned_real_representatives'])
assert not any(plan[k] for k in ['candidate_available', 'actual_tests_executed', 'implementation_probe_written', 'resources_started', 'main_or_git_mutation'])
assert plan['requires_separate_execution_authorization'] is True

stats = INDEX['statistics']
assert stats == {
    'candidate_paths': 34, 'existing_candidate_paths': 22, 'new_candidate_paths': 12,
    'logical_artifacts': len(INDEX['artifacts']), 'objects': len(objects),
    'object_bytes': sum(len(b) for b in objects.values()), 'git_references': len(refs),
    'unique_git_blobs': len({r['git_blob'] for r in refs}),
}
assert stats['logical_artifacts'] == 21 and stats['objects'] == 19

changed_text = {}
for path, item in INDEX['administrative_insertions'].items():
    old = git_cache[(SPEC, path)][1]
    new = (DOCS / path).read_bytes()
    offset, length = item['offset'], item['added_bytes']
    assert sha(old) == item['baseline_sha256']
    assert new[:offset] + new[offset + length:] == old, path
    addition = new[offset:offset + length]
    assert sha(addition) == item['added_sha256']
    changed_text[path] = addition
continuation = TEAM + 'recovery-2026-10-06-continuation.md'
assert INDEX['administrative_insertions'][continuation]['offset'] == len(git_cache[(SPEC, continuation)][1])
assert changed_text[continuation].startswith(b'\n## 27. ')
assert len(re.findall(rb'^## \d+\.', changed_text[continuation], re.M)) == 1
card = (DOCS / CARD).read_bytes()
assert body(card) == body(artifact('formal-card')) and sha(body(card)) == TECH
changed_text[CARD] = card[:len(card) - len(body(card))]
assert b'objects/525832e8a7a08ecddf1b09a0ae641e19003c1aa1fe69c60f54886c00a95ae011' in changed_text[CARD]
for path in [REPORT, EVIDENCE + '/README.md']:
    changed_text[path] = (DOCS / path).read_bytes()


def anchors(data):
    found, seen = set(), {}
    for title in re.findall(r'^#{1,6}\s+(.+?)\s*#*$', data.decode(), re.M):
        title = re.sub(r'\[([^]]+)\]\([^)]*\)', r'\1', title)
        title = re.sub(r'<[^>]*>', '', title).lower().replace('`', '')
        slug = re.sub(r'[^\w\-\s]', '', title).replace(' ', '-')
        count = seen.get(slug, 0)
        seen[slug] = count + 1
        found.add(slug if count == 0 else f'{slug}-{count}')
    return found


links = []
for path, data in changed_text.items():
    text = data.decode('utf-8')
    assert b'\r' not in data and data.endswith(b'\n')
    assert not re.search(rb'[ \t]+$', data, re.M), path
    assert len(re.findall(r'^```', text, re.M)) % 2 == 0, path
    for target in re.findall(r'\[[^\]\n]*\]\(([^)\s]+)\)', text):
        parsed = urlsplit(target)
        if parsed.scheme or parsed.netloc:
            continue
        rel = posixpath.normpath(str(PurePosixPath(path).parent / unquote(parsed.path))) if parsed.path else path
        assert not rel.startswith('../') and not rel.startswith('/'), (path, target)
        links.append((path, target, rel, unquote(parsed.fragment)))
# Check the unchanged formal technical links against their already recorded fixed basis.
for target in basis['links']:
    data = git_cache[(BASE, target['baseline_path'])][1]
    fragment = unquote(urlsplit(target['target']).fragment)
    assert not fragment or fragment in anchors(data), target
git_many([(SPEC, rel) for _, _, rel, _ in links if not (DOCS / rel).is_file()])
for source, target, rel, fragment in links:
    data = (DOCS / rel).read_bytes() if (DOCS / rel).is_file() else git_cache[(SPEC, rel)][1]
    assert not fragment or fragment in anchors(data), (source, target)
for path in [HERE / 'index.json', HERE / 'verify_archive.py']:
    data = path.read_bytes()
    data.decode('utf-8')
    assert b'\r' not in data and data.endswith(b'\n')
    assert not re.search(rb'[ \t]+$', data, re.M), path

print(json.dumps({
    'result': 'PASS', 'kind': 'offline bytes/fixed Git only', **stats,
    'exact_original_diffs': 3, 'local_link_occurrences': len(links),
    'preserved_history': 'administrative originals and continuation 1-26 unchanged',
    'technical_sha256': TECH, 'product_or_archived_code_executed': False,
}, ensure_ascii=False, sort_keys=True))
