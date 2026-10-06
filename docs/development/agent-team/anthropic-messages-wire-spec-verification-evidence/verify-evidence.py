#!/usr/bin/env python3
"""Verify this specification archive offline; never import SDK or run subprocesses."""
import base64
import difflib
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parent


def require(ok, reason):
    if not ok:
        raise SystemExit('FAIL: ' + reason)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def object_id(kind, raw):
    return hashlib.sha1(kind.encode() + b' ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()


def read(relative):
    path = ROOT / relative
    require(path.resolve().is_relative_to(ROOT), 'archive path escapes root')
    return path.read_bytes()


def load(relative):
    return json.loads(read(relative))


manifest = load('archive-manifest.json')
records = manifest['artifacts'] + manifest['generated_artifacts']
require(len({r['path'] for r in records}) == len(records), 'duplicate artifact path')
for record in records:
    raw = read(record['path'])
    require(len(raw) == record['bytes'] and digest(raw) == record['sha256'], record['path'])
expected = {r['path'] for r in records} | set(manifest['excluded_self_and_run_evidence'])
actual = {str(p.relative_to(ROOT)) for p in ROOT.rglob('*') if p.is_file()}
require(not (actual - expected), 'unexpected archive files: ' + repr(actual - expected))

origin_map = manifest['origin_map']
author = manifest['original_roots']['author']
independent = manifest['original_roots']['independent']


def original(path):
    return read(origin_map[path])


def author_json(name):
    return json.loads(original(author + '/' + name))


def independent_json(name):
    return json.loads(original(independent + '/' + name))


proof = load('git-proofs.json')
commits = {}
trees = {}
for oid, encoded in proof['commits'].items():
    raw = base64.b64decode(encoded, validate=True)
    require(object_id('commit', raw) == oid, 'commit object ' + oid)
    commits[oid] = raw
for oid, encoded in proof['trees'].items():
    raw = base64.b64decode(encoded, validate=True)
    require(object_id('tree', raw) == oid, 'tree object ' + oid)
    entries = {}
    pos = 0
    while pos < len(raw):
        stop = raw.index(b'\0', pos)
        mode, name = raw[pos:stop].split(b' ', 1)
        require(stop + 21 <= len(raw), 'truncated tree')
        label = name.decode('utf-8')
        require(label not in entries, 'duplicate tree name')
        entries[label] = (mode.decode(), raw[stop + 1:stop + 21].hex())
        pos = stop + 21
    trees[oid] = entries


def member(commit, path, blob):
    first = commits[commit].splitlines()[0]
    require(first.startswith(b'tree '), 'missing root tree')
    tree = first[5:].decode()
    parts = path.split('/')
    for number, part in enumerate(parts):
        mode, oid = trees[tree][part]
        if number == len(parts) - 1:
            require(mode in ('100644', '100755') and oid == blob, 'Git path ' + path)
        else:
            require(mode == '40000', 'non-directory Git ancestor')
            tree = oid


source = author_json('source-manifest.json')
require(source['commit'] == manifest['official_commit'], 'official commit identity')
commit_raw = original(author + '/commit-object.txt')
require(commit_raw == commits[source['commit']], 'original official commit bytes')
require(digest(commit_raw) == source['commit_object_sha256'], 'official commit SHA256')
for item in source['files']:
    raw = original(author + '/source/' + item['path'])
    require(digest(raw) == item['sha256'] and len(raw) == item['bytes'], item['path'])
    require(object_id('blob', raw) == item['git_blob'], 'official blob ' + item['path'])
    member(source['commit'], item['path'], item['git_blob'])
    expected_url = source['official_repository'] + '/blob/' + source['commit'] + '/' + item['path']
    require(item['url'] == expected_url, 'official fixed URL')
require(len(source['files']) == 66 and sum(i['bytes'] for i in source['files']) == 297810,
        'official file coverage')
license_raw = original(author + '/source/LICENSE')
require(b'Permission is hereby granted, free of charge' in license_raw, 'MIT license text')

baseline = author_json('baseline-inputs.json')
fixed = {}
for commit, key in [(baseline['baseline'], 'sources'), (baseline['status_commit'], 'status_sources')]:
    for path, item in baseline[key].items():
        fixed[(commit, path)] = item
require(len(fixed) == len(manifest['fixed_git_inputs']) == 27, 'Git input coverage')
for item in manifest['fixed_git_inputs']:
    raw = read(item['archive_path'])
    require(digest(raw) == item['sha256'] and len(raw) == item['bytes'], 'fixed Git bytes')
    require(object_id('blob', raw) == item['git_blob'], 'fixed Git blob')
    require(fixed[(item['commit'], item['repo_path'])] ==
            {'sha256': item['sha256'], 'bytes': item['bytes']}, 'original baseline locator')
    member(item['commit'], item['repo_path'], item['git_blob'])

facts = author_json('field-manifest.json')
line_count = 0
for fact in facts['facts']:
    for item in fact['files']:
        raw = original(author + '/source/' + item['path'])
        require(digest(raw) == item['sha256'], 'field source SHA')
        lines = raw.decode('utf-8').splitlines()
        for match in item['matches']:
            require(lines[match['line'] - 1].strip() == match['text'], 'field line match')
            line_count += 1
require(len(facts['facts']) == 9 and line_count == 75, 'field evidence coverage')

versions = ['spec-freeze01', 'spec-freeze02', 'spec-freeze03', 'spec-adopted01']
specs = {}
for number, version in enumerate(versions):
    meta = author_json(version + '/manifest.json')
    raw = original(author + '/' + version + '/spec.md')
    specs[version] = raw
    require(digest(raw) == meta['sha256'] and len(raw) == meta['bytes'], 'spec ' + version)
    require(meta['implementation_authorized'] is False, 'spec is not implementation authorization')
    if number:
        prior = versions[number - 1]
        diff = ''.join(difflib.unified_diff(specs[prior].decode().splitlines(True),
                       raw.decode().splitlines(True), fromfile=prior + '/spec.md',
                       tofile=version + '/spec.md')).encode()
        recorded = original(author + '/' + version + '/delta.patch')
        require(diff == recorded and digest(diff) == meta['delta_sha256'], 'exact delta ' + version)
        checks = original(author + '/' + version + '/checks.json')
        require(digest(checks) == meta['checks_sha256'], 'revision checks ' + version)
for name, item in author_json('spec-freeze01/manifest.json')['documents'].items():
    raw = original(author + '/' + name)
    require(digest(raw) == item['sha256'] and len(raw) == item['bytes'], 'frozen metadata ' + name)

marker = '## 1. 完整结果、前置与后续责任\n'.encode()
reviewed_body = specs['spec-freeze03'].split(marker, 1)[1]
delivered_body = specs['spec-adopted01'].split(marker, 1)[1]
require(reviewed_body == delivered_body, 'technical sections 1-11 changed after review')
technical_sha = digest(marker + reviewed_body)
require(technical_sha == author_json('spec-adopted01/checks.json')['technical_sections_sha256'],
        'technical body identity')
paths = re.findall(r'^\| \d+ \| `([^`]+)`', specs['spec-freeze03'].decode(), re.M)
require(len(paths) == len(set(paths)) == 19, 'candidate path coverage')
delivery = manifest['delivery']
require(read(delivery['archive_path']) == specs['spec-adopted01'], 'delivery original')
require(digest(specs['spec-adopted01']) == delivery['sha256'], 'delivery SHA')
require(object_id('blob', specs['spec-adopted01']) == delivery['git_blob'], 'delivery blob')
member(delivery['commit'], delivery['repo_path'], delivery['git_blob'])

index = independent_json('evidence-index.json')
for item in index['files']:
    raw = original(independent + '/' + item['path'])
    require(digest(raw) == item['sha256'] and len(raw) == item['bytes'], 'independent ' + item['path'])
final = independent_json('final-checks.json')
require(final['result'] == 'STATIC PASS' and not final['remaining_blocking_spec_findings'],
        'independent static conclusion')
require(final['accepted_spec_sha256'] == digest(specs['spec-freeze03']), 'reviewed spec identity')
require(digest(original(final['report']['path'])) == final['report']['sha256'], 'independent report')
for version, item in zip(versions, final['chain']):
    require(item['spec']['sha256'] == digest(specs[version]), 'independent revision chain')
require(independent_json('static-check01.json')['exit'] == 1, 'preserved first metadata failure')
require(independent_json('static-check02.json')['exit'] == 0, 'corrected metadata check')
require(independent_json('final-check-run.json')['exit_code'] == 0, 'final static command')

print(json.dumps({'result': 'PASS (archive integrity only)', 'original_artifacts': len(manifest['artifacts']),
                  'official_files': 66, 'official_bytes': 297810, 'git_baseline_files': 27,
                  'git_commit_objects': len(commits), 'git_tree_objects': len(trees),
                  'field_fact_groups': 9, 'exact_field_lines': line_count, 'spec_versions': versions,
                  'exact_revision_deltas': 3, 'candidate_paths_not_authorized': len(paths),
                  'technical_sections_sha256': technical_sha, 'spec_delivery': delivery['commit'],
                  'independent_conclusion': 'STATIC PASS', 'implementation_authorized': False,
                  'network_sdk_go_docker_or_blocked_task_execution': False}, indent=2))
