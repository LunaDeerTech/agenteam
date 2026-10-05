#!/usr/bin/env python3
"""Verify archived bytes and materialize a source overlay, without running tests."""
import argparse
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def digest(data):
    return hashlib.sha256(data).hexdigest()


def final_sources():
    manifest = json.loads((ROOT / 'candidate/manifest.json').read_text())
    result = {}
    for path, expected in manifest['files'].items():
        data = (ROOT / 'candidate/files' / (path + '.txt')).read_bytes()
        assert digest(data) == expected, path
        result[path] = data
    return result


def sources(variant):
    final = final_sources()
    if variant == 'final':
        return final
    history = json.loads((ROOT / 'history/source-deltas.json').read_text())
    spec = history['variants'][variant]
    original = ROOT / spec['original_manifest']
    assert digest(original.read_bytes()) == spec['original_manifest_sha256']
    result = sources(spec['inherits']) if 'inherits' in spec else {}
    for path, entry in spec['files'].items():
        lines = final[path].decode('utf-8').splitlines(keepends=True)
        for edit in reversed(entry['edits']):
            lines[edit['start']:edit['end']] = edit['lines']
        data = ''.join(lines).encode('utf-8')
        assert digest(data) == entry['sha256'], (variant, path)
        result[path] = data
    return result


def verify():
    index = json.loads((ROOT / 'SHA256SUMS.json').read_text())
    for path, entry in index['files'].items():
        data = (ROOT / path).read_bytes()
        assert digest(data) == entry['sha256'], path
        assert len(data) == entry['bytes'], path
    raw = json.loads((ROOT / 'source-map.json').read_text())['raw_files']
    for path, entry in raw.items():
        assert digest((ROOT / path).read_bytes()) == entry['sha256'], path
    history = json.loads((ROOT / 'history/source-deltas.json').read_text())
    counts = {name: len(sources(name)) for name in ['final', *history['variants']]}
    return {'indexed_files': len(index['files']), 'raw_files': len(raw),
            'source_variants': counts, 'result': 'PASS',
            'execution': 'SHA checks and source reconstruction only; no Go/Docker/network'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--out', type=Path)
    parser.add_argument('--variant', default='final')
    parser.add_argument('--with-foreign-probe', action='store_true')
    args = parser.parse_args()
    result = verify()
    if args.out:
        output = args.out.resolve()
        assert not output.exists(), 'output must be a new directory'
        source = sources(args.variant)
        if args.with_foreign_probe:
            assert args.variant == 'final', 'foreign probe uses final review09'
            path = 'tests/objects/artifact_independent_foreign_result_test.go'
            source[path] = (ROOT / 'independent/foreign/real/artifact_independent_foreign_result_test.go.txt').read_bytes()
        for path, data in source.items():
            target = output / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        result['materialized'] = {'variant': args.variant, 'files': len(source), 'output': str(output)}
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
