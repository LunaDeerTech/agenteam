#!/usr/bin/env python3
"""Verify archived bytes and reconstruct fixed inputs from read-only Git objects.

This does not run Go, Docker or the recorded historical commands.
"""
from pathlib import Path
from io import BytesIO
import gzip
import hashlib
import json
import subprocess

root = Path(__file__).resolve().parent
repo = Path(subprocess.check_output(
    ['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())


def sha(data):
    return hashlib.sha256(data).hexdigest()


def read_json(path):
    return json.loads((root / path).read_text(encoding='utf-8'))


checksums = read_json('CHECKSUMS.json')
actual = {str(p.relative_to(root)) for p in root.rglob('*') if p.is_file()
          and p.name != 'CHECKSUMS.json'}
assert actual == set(checksums), ('archive membership mismatch', actual ^ set(checksums))
for path, expected in checksums.items():
    raw = (root / path).read_bytes()
    assert len(raw) == expected['bytes'] and sha(raw) == expected['sha256'], path

provenance = read_json('provenance.json')
for path, record in provenance['artifacts'].items():
    raw = (root / path).read_bytes()
    if record['encoding'] == 'gzip-original-bytes':
        raw = gzip.decompress(raw)
    assert len(raw) == record['source_bytes'] and sha(raw) == record['source_sha256'], path

delivery = provenance['delivery_commit']
inputs = [(item, read_json(item['manifest'])) for item in provenance['source_inputs']]
core = read_json('independent/core01/input.json')
requests = set()
for item, data in inputs:
    owned = data[item['owned_key']]
    for path in owned:
        requests.add(f'{delivery}:{path}')
    for path in data.get('closure', {}):
        if path not in owned:
            revision = data['upstream_commit'] if path in data.get('upstream_overlay', {}) else data['baseline']
            requests.add(f'{revision}:{path}')
for value in core['source_files'].values():
    if value['source'] != 'core-freeze-01':
        requests.add(value['source'])
ordered = sorted(requests)
result = subprocess.run(['git', 'cat-file', '--batch'], cwd=repo,
                        input=('\n'.join(ordered) + '\n').encode(),
                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
stream, objects = BytesIO(result.stdout), {}
for key in ordered:
    header = stream.readline().decode().strip().split()
    assert len(header) == 3 and header[1] == 'blob', ('missing Git blob', key, header)
    raw = stream.read(int(header[2]))
    assert stream.read(1) == b'\n', ('invalid Git batch boundary', key)
    objects[key] = (raw, sha(raw))
assert stream.read() == b''


def owned_bytes(path, expected):
    current, digest = objects[f'{delivery}:{path}']
    if digest == expected:
        return current
    historical = root / 'objects' / f'{expected}.txt'
    raw = historical.read_bytes()
    assert sha(raw) == expected, ('historical input mismatch', path)
    return raw


verified_owned = verified_closure = 0
for item, data in inputs:
    owned = data[item['owned_key']]
    for path, expected in owned.items():
        owned_bytes(path, expected)
        verified_owned += 1
    for path, expected in data.get('closure', {}).items():
        if path in owned:
            assert sha(owned_bytes(path, owned[path])) == expected, (item['manifest'], path)
        else:
            revision = data['upstream_commit'] if path in data.get('upstream_overlay', {}) else data['baseline']
            assert objects[f'{revision}:{path}'][1] == expected, (item['manifest'], path)
        verified_closure += 1
    for path, expected in data.get('upstream_overlay', {}).items():
        assert objects[f"{data['upstream_commit']}:{path}"][1] == expected, path

core_owned = read_json('inputs/author-core-freeze-01.json')['files']
for path, record in core['source_files'].items():
    if record['source'] == 'core-freeze-01':
        assert sha(owned_bytes(path, core_owned[path])) == record['sha256'], path
    else:
        assert objects[record['source']][1] == record['sha256'], path

final = read_json('inputs/author-input13.json')
assert len(final['owned']) == 20
for path, expected in final['owned'].items():
    assert objects[f'{delivery}:{path}'][1] == expected, ('delivered 20 differs', path)
combined = read_json('inputs/combined-input01.json')
assert combined['owned'] == final['owned'] and len(combined['upstream_overlay']) == 13

probe_hashes = {sha(p.read_bytes()) for p in (root / 'independent/probes').iterdir()}
for run, input_path in provenance['independent_run_inputs'].items():
    command = read_json(f'independent/{run}/command.json')
    assert sha((root / input_path).read_bytes()) == command['manifest_sha256'], run
    assert set(command['probe_sha256'].values()) <= probe_hashes, run

expected_runs = {'final01': 11, 'combined01': 3}
for run, count in expected_runs.items():
    result = read_json(f'independent/{run}/result-summary.json')
    assert result['driver_exit'] == 0 and len(result['tops_passed']) == count
    assert result['failures'] == 0 and result['skips'] == 0
    assert result['two_cleanup_passes'] and all(x['absent'] for x in result['coordinator_exact_id_recheck'].values())

print(json.dumps({'status': 'PASS', 'archived_files': len(actual),
                  'original_artifacts': len(provenance['artifacts']),
                  'fixed_input_manifests': len(inputs), 'owned_revision_entries': verified_owned,
                  'closure_revision_entries': verified_closure, 'core_dependency_files': len(core['source_files']),
                  'unique_read_only_git_blobs': len(objects), 'delivered_owned_paths': 20,
                  'combined_upstream_paths': 13, 'independent_top_level_passes': [11, 3]}, indent=2))
