#!/usr/bin/env python3
"""Check frozen specification evidence and its 31 Git inputs without running product code."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, required=True)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    repo = args.repo.resolve()
    mapping = json.loads((here / 'original-map.json').read_text())
    by_original = {e['original']: e for e in mapping}
    for e in mapping:
        raw = (here / e['archive']).read_bytes()
        assert digest(raw) == e['sha256'] and len(raw) == e['bytes'], e
    inputs = json.loads((here / 'spec/inputs.json').read_text())
    assert inputs['baseline'] == 'c54f73f3324caa11608d84e5d207141985eb6074'
    assert len(inputs['sources']) == 31
    output = subprocess.check_output(
        ['git', 'cat-file', '--batch'], cwd=repo,
        input=''.join(e['git_locator'] + '\n' for e in inputs['sources']).encode(),
    )
    offset = 0
    for e in inputs['sources']:
        end = output.index(b'\n', offset)
        header = output[offset:end].split()
        offset = end + 1
        assert header[1] == b'blob' and header[0].decode() == e['blob'], e
        size = int(header[2])
        raw = output[offset:offset + size]
        offset += size + 1
        assert size == e['bytes'] and digest(raw) == e['sha256'], e
    references = 0
    for name in ('review/index.json', 'plan/index.json'):
        index = json.loads((here / name).read_text())
        for e in index['payloads']:
            stored = by_original[e['path']]
            assert stored['sha256'] == e['sha256'], e
            references += 1
    original = (here / 'spec/rev1.md').read_text()
    accepted = (here / 'acceptance/after.md').read_text()
    active = (repo / 'docs/development/work-items/d26-public-account-entry.md').read_text()
    technical = lambda s: s[s.index('## 1.'):s.index('## 10.')]
    assert technical(original) == technical(accepted) == technical(active)
    adopted = subprocess.check_output([
        'git', 'show', 'e5a5ccf5343fe9f17aced6e9ee0d633fbfe3c5aa:docs/development/work-items/d26-public-account-entry.md',
    ], cwd=repo)
    assert adopted == accepted.encode()
    print(json.dumps({
        'status': 'PASS', 'original_locations': len(mapping),
        'original_payloads': len({e['archive'] for e in mapping}),
        'git_inputs': 31, 'declared_payload_references': references,
        'accepted_card_commit_matches': True, 'technical_sections_1_to_9_unchanged': True,
        'technical_bytes_slice': 'inclusive ## 1. through exclusive ## 10.',
        'technical_bytes_sha256': digest(technical(original).encode()),
        'business_or_resource_execution': False,
    }, ensure_ascii=False))


if __name__ == '__main__':
    main()
