#!/usr/bin/env python3
"""Verify this archive or reconstruct one small source overlay; never run tests."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--repo', type=Path, help='read exact Git objects only')
    group.add_argument('--accepted-files', type=Path, help='read an already frozen final20 directory, without Git')
    parser.add_argument('--baseline-root', type=Path, help='optionally verify existing baseline files, without copying')
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--variant')
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    if bool(args.variant) != bool(args.out):
        parser.error('--variant and --out must be supplied together')
    if not args.check and not args.variant:
        parser.error('choose --check or --variant/--out')
    archive = Path(__file__).resolve().parent
    mapping = json.loads((archive / 'source-map.json').read_text())
    checks = {'raw_payloads': 0, 'source_blobs': 0, 'variants': 0, 'baseline_files': 0}
    for item in mapping['raw_files']:
        data = (archive / item['archive']).read_bytes()
        assert digest(data) == item['sha256'] and len(data) == item['bytes'], item['archive']
        checks['raw_payloads'] += 1
    index = archive / 'SHA256SUMS.json'
    if index.exists():
        for item in json.loads(index.read_text())['files']:
            data = (archive / item['path']).read_bytes()
            assert digest(data) == item['sha256'] and len(data) == item['bytes'], item['path']

    def git_blob(commit, path):
        return subprocess.check_output(['git', '-C', str(args.repo), 'show', commit + ':' + path])

    blobs = {}
    for key, item in mapping['source_blobs'].items():
        if 'archive' in item:
            data = (archive / item['archive']).read_bytes()
        elif args.accepted_files:
            data = (args.accepted_files / item['git_path']).read_bytes()
        else:
            data = git_blob(item['git_commit'], item['git_path'])
        assert digest(data) == key == item['sha256'] and len(data) == item['bytes'], item
        blobs[key] = data
        checks['source_blobs'] += 1
    for name, variant in mapping['variants'].items():
        for row in variant['paths']:
            assert row['sha256'] in blobs, (name, row)
        checks['variants'] += 1
    baseline = json.loads((archive / mapping['baseline']['manifest']).read_text())
    if args.baseline_root or args.repo:
        for row in baseline['files']:
            data = ((args.baseline_root / row['path']).read_bytes() if args.baseline_root
                    else git_blob(mapping['baseline']['commit'], row['path']))
            assert digest(data) == row['sha256'], row['path']
            checks['baseline_files'] += 1
        assert checks['baseline_files'] == mapping['baseline']['count']
    if args.variant:
        variant = mapping['variants'][args.variant]
        args.out.mkdir(parents=True, exist_ok=False)
        for row in variant['paths']:
            p = args.out / row['path']
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(blobs[row['sha256']])
        checks['written_overlay'] = {'variant': args.variant, 'count': len(variant['paths'])}
    checks['git_read'] = args.repo is not None
    checks['no_tests_network_or_git_write'] = True
    print(json.dumps(checks, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
