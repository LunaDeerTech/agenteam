#!/usr/bin/env python3
"""Verify archived evidence and recover an overlay from read-only Git objects."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def sha(data):
    return hashlib.sha256(data).hexdigest()


def accepted(repo):
    specification = json.loads((ROOT / 'accepted-input.json').read_text())
    result = {}
    for path, expected in specification['files'].items():
        command = ['git', 'show', specification['accepted_commit'] + ':' + path]
        data = subprocess.check_output(command, cwd=repo)
        assert sha(data) == expected, path
        result[path] = data
    return result


def variant_sources(final, name):
    if name == 'final':
        return final, []
    history = json.loads((ROOT / 'history/source-deltas.json').read_text())['variants'][name]
    assert sha((ROOT / history['input_metadata']).read_bytes()) == history['input_metadata_sha256']
    result = {}
    for path, entry in history['files'].items():
        lines = final[path].decode().splitlines(keepends=True)
        for edit in reversed(entry['edits']):
            lines[edit['start']:edit['end']] = edit['lines']
        data = ''.join(lines).encode()
        assert sha(data) == entry['sha256'], (name, path)
        result[path] = data
    return result, history['absent_final_paths'] + history['additional_absent_baseline_paths']


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
    originals = json.loads((ROOT / 'source-map.json').read_text())['raw_files']
    for path, entry in originals.items():
        assert sha((ROOT / path).read_bytes()) == entry['sha256'], path
    final = accepted(args.repo.resolve())
    names = ['final', *json.loads((ROOT / 'history/source-deltas.json').read_text())['variants']]
    counts = {name: len(variant_sources(final, name)[0]) for name in names}
    summary = {'result': 'PASS', 'indexed_files': len(index), 'raw_files': len(originals), 'source_variants': counts,
               'scope': 'SHA verification and read-only git show; no Go/Docker/network/Git writes'}
    if args.out:
        output = args.out.resolve()
        assert not output.exists(), 'output must not exist'
        files, absent = variant_sources(final, args.variant)
        files = dict(files)
        if args.with_probe:
            assert args.variant == 'final', 'independent probe uses accepted final20'
            path = 'tests/security/secret_model_independent_test.go'
            files[path] = (ROOT / 'verification/src' / (path + '.txt')).read_bytes()
        for path, data in files.items():
            target = output / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        (output / 'reconstruction.json').write_text(json.dumps({'variant': args.variant, 'absent_paths': absent,
            'apply_to_baseline': '4cc4726b5707127f4b50c7da7023a3cd1f0b7f2d',
            'note': 'This is an overlay. Preserve the listed historical absences when reconstructing a failed input.'}, indent=2)+'\n')
        summary['materialized_files'] = len(files)
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
