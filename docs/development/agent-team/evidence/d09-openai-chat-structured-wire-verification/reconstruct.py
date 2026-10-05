#!/usr/bin/env python3
"""Verify or materialize fixed source bytes only; never run Go, Docker or network."""
import argparse
import hashlib
import json
import pathlib
import subprocess

HERE = pathlib.Path(__file__).resolve().parent
AUTHOR = '/workspace/agenteam-structured-wire-author-2zomqtpx/'
INDEPENDENT = '/workspace/agenteam-structured-wire-independent-v-jmkxen5r/'
BASE = 'ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7'
ACCEPTED = 'be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74'
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', type=pathlib.Path, default=HERE.parents[4])
parser.add_argument('--mode', choices=['final', 'red', 'independent'], default='final')
parser.add_argument('--output', type=pathlib.Path, help='Optional new private directory; omitted means in-memory verification only')
args = parser.parse_args()
records = {x['original']: x for x in json.loads((HERE / 'archive-provenance.json').read_text())['records']}

def git_bytes(commit, name):
    return subprocess.check_output(['git', 'show', commit + ':' + name], cwd=args.repo)

def original(name):
    entry = records[name]
    if 'archive' in entry:
        data = (HERE / entry['archive']).read_bytes()
    else:
        data = git_bytes(entry['git']['commit'], entry['git']['path'])
    assert len(data) == entry['bytes'] and hashlib.sha256(data).hexdigest() == entry['sha256'], name
    return data

if args.mode == 'independent':
    manifest = json.loads(original(INDEPENDENT + 'evidence/ready-input.json'))
    expected = {x['path']: x for x in manifest['files']}
    production = {x['path'] for x in json.loads(original(AUTHOR + 'production-review-02/manifest.json'))['files']}
    probe = manifest['probe']['path']
    def source(name):
        if name == probe:
            return original(INDEPENDENT + 'tree/' + name)
        return git_bytes(ACCEPTED if name in production else BASE, name)
else:
    expected = {x['path']: x for x in json.loads(original(AUTHOR + 'evidence/baseline-source.json'))['files']}
    stage = 'candidate-review-02' if args.mode == 'final' else 'red-f1-f2-01'
    overlay = {x['path']: x for x in json.loads(original(AUTHOR + stage + '/manifest.json'))['files']}
    expected.update(overlay)
    if args.mode == 'final':
        asset = json.loads(original(AUTHOR + 'evidence/baseline-assets-supplement.json'))
        expected[asset['path']] = asset
    def source(name):
        if name in overlay:
            return original(AUTHOR + stage + '/sources/' + name)
        return git_bytes(BASE, name)

if args.output:
    args.output.mkdir(parents=True, exist_ok=False)
count = size = 0
for name, entry in sorted(expected.items()):
    assert not pathlib.PurePosixPath(name).is_absolute() and '..' not in pathlib.PurePosixPath(name).parts
    data = source(name)
    assert len(data) == entry['bytes'] and hashlib.sha256(data).hexdigest() == entry['sha256'], name
    if args.output:
        dest = args.output / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(data)
    count += 1
    size += len(data)
print(json.dumps({'mode': args.mode, 'baseline': BASE, 'accepted': ACCEPTED, 'files': count, 'bytes': size, 'all_hashes_match': True, 'output': str(args.output) if args.output else None, 'source_execution': False}))
