#!/usr/bin/env python3
"""Read-only archive/source checks; optional reconstruction into an empty owned directory."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile


def digest(data):
    return hashlib.sha256(data).hexdigest()


def load(path):
    return json.loads(path.read_text())


def git_blobs(repo, objects):
    data = subprocess.run(
        ['git', 'cat-file', '--batch'], cwd=repo,
        input=''.join(c + ':' + p + '\n' for c, p in objects).encode(),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
    ).stdout
    result = {}
    offset = 0
    for key in objects:
        end = data.index(b'\n', offset)
        header = data[offset:end]
        offset = end + 1
        assert not header.endswith(b' missing'), key
        size = int(header.split()[-1])
        result[key] = data[offset:offset + size]
        offset += size + 1
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--materialize', type=Path)
    args = parser.parse_args()
    archive = Path(__file__).resolve().parent
    repo = args.repo.resolve()
    mapping = load(archive / 'original-map.json')
    for entry in mapping:
        assert digest((archive / entry['archive']).read_bytes()) == entry['sha256'], entry
    sources = load(archive / 'source-locators.json')
    available = {(s['path'], s['sha256']): s for s in sources['sources']}
    objects = sorted({(s['commit'], s['path']) for s in available.values() if s['kind'] == 'git'})
    blobs = git_blobs(repo, objects)
    by_pair = {}
    for pair, source in available.items():
        if source['kind'] == 'git':
            data = blobs[(source['commit'], source['path'])]
        elif source['kind'] == 'archived-source':
            data = (archive / source['archive']).read_bytes()
        else:
            continue
        assert digest(data) == source['sha256'], source
        by_pair[pair] = data
    final = load(archive / 'author/evidence/fixture-input-01/manifest.json')
    assert final['baseline'] == sources['base']
    assert len(final['files']) == 24
    final_objects = [(sources['accepted'], p) for p in final['files']]
    final_bytes = git_blobs(repo, final_objects)
    for p, h in final['files'].items():
        assert digest(final_bytes[(sources['accepted'], p)]) == h, p
    ready = load(archive / 'independent/evidence/ready-input.json')
    checked = 0
    assets = 0
    for field in ('files', 'fixed_dependencies_and_dist', 'probe_files'):
        for p, h in ready[field].items():
            item = available[(p, h)]
            if p.startswith('web/dist/'):
                assert item['kind'] == 'generated-dist-hash-only'
                assert item['original_bytes_verified'] is True
                assets += 1
            else:
                assert (p, h) in by_pair, (field, p, h)
                checked += 1
    pure_counts = {}
    for stage in ('ui01', 'ui02'):
        pure = load(archive / ('reviews/' + stage + '/input.json'))
        for source in pure['fixed_files']:
            assert (source['path'], source['sha256']) in by_pair, source
        for name in ('probe', 'config'):
            source = pure[name]
            p = source['path'].split('/tree/', 1)[1]
            assert (p, source['sha256']) in by_pair, source
        pure_counts[stage] = len(pure['fixed_files']) + 2
    web = load(archive / 'author/evidence/web-check-02.json')
    web_input = [s for s in web['input'] if s['path'].startswith('web/')]
    assert len(web_input) == 19
    assert {s['path']: s['sha256'] for s in web_input} == {
        p: h for p, h in final['files'].items() if p.startswith('web/')
    }
    for source in web_input:
        assert (source['path'], source['sha256']) in by_pair
    # Intermediate input-only records remain listed, never treated as verified bytes.
    limitations = load(archive / 'source-limitations.json')
    absent = [s for s in sources['sources'] if s['kind'] == 'missing']
    assert {(s['path'], s['sha256']) for s in absent} == {
        (s['path'], s['sha256']) for s in limitations['intermediate_source_hashes_only']
    }
    if args.materialize:
        target = args.materialize.resolve()
        assert not target.exists() or not any(target.iterdir()), 'Use a new empty owned directory'
        target.mkdir(parents=True, exist_ok=True)
        archive_bytes = subprocess.check_output([
            'git', 'archive', sources['base'], 'go.mod', 'go.sum', 'api', 'cmd', 'db',
            'internal', 'scripts', 'tests', 'web', 'docs/frontend-design/styles/colors-and-themes.md',
        ], cwd=repo)
        with tarfile.open(fileobj=io.BytesIO(archive_bytes)) as tar:
            for member in tar:
                rel = Path(member.name)
                assert not rel.is_absolute() and '..' not in rel.parts
                if member.isdir():
                    (target / rel).mkdir(parents=True, exist_ok=True)
                elif member.isfile():
                    dest = target / rel
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    dest.write_bytes(tar.extractfile(member).read())
                    dest.chmod(member.mode & 0o777)
                else:
                    raise AssertionError('Non-regular Git entry: ' + member.name)
        for p, h in final['files'].items():
            dest = target / p
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(final_bytes[(sources['accepted'], p)])
        for p, h in ready['probe_files'].items():
            dest = target / p
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(by_pair[(p, h)])
    print(json.dumps({
        'status': 'PASS', 'original_mappings': len(mapping),
        'located_source_versions': len(by_pair), 'git_objects': len(objects),
        'final_accepted_sources': 24, 'web_check_final_sources': 19,
        'independent_fixed_source_entries': checked, 'independent_dist_hashes_only': assets,
        'pure_source_entries': pure_counts,
        'intermediate_source_versions_not_reconstructed': len(absent),
        'dist_rebuilt': False, 'business_tests_run': False,
        'materialized': str(args.materialize) if args.materialize else None,
    }, ensure_ascii=False))


if __name__ == '__main__':
    main()
