#!/usr/bin/env python3
"""Check immutable evidence and recover source overlays from accepted Git objects."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git_sources(repo, commit, entries):
    result = {}
    for path, expected in entries.items():
        data = subprocess.check_output(['git', 'show', commit + ':' + path], cwd=repo)
        assert sha(data) == expected, (commit, path)
        result[path] = data
    return result


def variant(final, name):
    if name == 'final':
        return dict(final)
    spec = json.loads((ROOT / 'history/source-deltas.json').read_text())['variants'][name]
    assert sha((ROOT / spec['input_metadata']).read_bytes()) == spec['input_metadata_sha256']
    result = {}
    for path, entry in spec['files'].items():
        lines = final[path].decode().splitlines(keepends=True)
        for edit in reversed(entry['edits']):
            lines[edit['start']:edit['end']] = edit['lines']
        data = ''.join(lines).encode()
        assert sha(data) == entry['sha256'], (name, path)
        result[path] = data
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=Path.cwd())
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--out', type=Path)
    parser.add_argument('--variant', default='final')
    parser.add_argument('--with-probe', action='store_true')
    args = parser.parse_args()
    index = json.loads((ROOT / 'SHA256SUMS.json').read_text())['files']
    for path, entry in index.items():
        data = (ROOT / path).read_bytes()
        assert sha(data) == entry['sha256'] and len(data) == entry['bytes'], path
    raw = json.loads((ROOT / 'source-map.json').read_text())['raw_files']
    for path, entry in raw.items():
        assert sha((ROOT / path).read_bytes()) == entry['sha256'], path
    accepted = json.loads((ROOT / 'accepted-input.json').read_text())
    final = git_sources(args.repo, accepted['accepted_commit'], accepted['files'])
    dependency = json.loads((ROOT / 'author/accepted-secret-delta.json').read_text())
    deps = git_sources(args.repo, dependency['commit'], {x['path']:x['sha256'] for x in dependency['files']})
    assert len(deps) == 20 and not set(deps).intersection(final)
    names = ['final', *json.loads((ROOT / 'history/source-deltas.json').read_text())['variants']]
    counts = {name: len(variant(final, name)) for name in names}
    for mapping in json.loads((ROOT / 'history/omitted-duplicate-source-map.json').read_text()):
        data = variant(final, mapping['reconstruct_variant'])[mapping['path']]
        assert sha(data) == mapping['sha256']
    result = {'result':'PASS', 'indexed_files':len(index), 'raw_files':len(raw),
              'source_variants':counts, 'accepted_secret_dependency_files':len(deps),
              'scope':'SHA/source recovery using read-only git show; no Go/Docker/network/Git writes'}
    if args.out:
        output = args.out.resolve()
        assert not output.exists(), 'output must not exist'
        files = {**deps, **variant(final, args.variant)}
        if args.with_probe:
            assert args.variant == 'final'
            files['tests/model/independent_system_http_test.go'] = (ROOT / 'verification/probe/independent_system_http_test.go.txt').read_bytes()
        for path, data in files.items():
            target = output / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        (output / 'reconstruction.json').write_text(json.dumps({'base':accepted['baseline'], 'variant':args.variant,
            'scope':'Overlay only. Historical variants cover exactly their declared source inputs; production01 is a static production-only input.',
            'not_yet_present_in_unit01_unit02': sorted(set(final)-set(variant(final,args.variant))) if args.variant in ('unit01','unit02') else []},indent=2)+'\n')
        result['materialized_files'] = len(files)
    print(json.dumps(result,ensure_ascii=False,indent=2))


if __name__ == '__main__':
    main()
