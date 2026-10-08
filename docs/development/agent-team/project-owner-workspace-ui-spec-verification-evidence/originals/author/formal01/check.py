#!/usr/bin/env python3
"""Static formal-placement check; no product commands or resource operations."""
from pathlib import Path
import hashlib
import json
import re

ROOT = Path(__file__).resolve().parent
mapping = json.loads((ROOT / 'administrative-map.json').read_text())
links = json.loads((ROOT / 'link-map.json').read_text())
target = Path(mapping['target'])
source = Path(mapping['source_draft']['path'])
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
assert sha(source) == mapping['source_draft']['sha256']
actual = target.read_text()
assert actual.endswith(mapping['appendix'])
restored = actual[:-len(mapping['appendix'])]
for change in mapping['administrative_replacements']:
    assert restored.count(change['new']) == 1, change['kind']
    restored = restored.replace(change['new'], change['old'], 1)
for link in links:
    new = '[' + link['label'] + '](' + link['formal_target'] + ')'
    old = '[' + link['label'] + '](' + link['scratch_target'] + ')'
    assert restored.count(new) == 1, link['label']
    restored = restored.replace(new, old, 1)
assert restored == source.read_text(), 'Non-administrative byte difference'
found = re.findall(r'\[[^\]]+\]\(([^)]+)\)', actual)
assert len(found) == 77
for value in found:
    assert not value.startswith(('/', 'http://', 'https://'))
    assert '#' not in value
    assert (target.parent / value).resolve().is_file(), value
assert '/workspace/scratch' not in actual
assert 'inputs.json](' not in actual and 'candidate-paths.json](' not in actual
inputs = json.loads((source.parent / 'inputs.json').read_text())
for item in inputs['sources'].values():
    assert sha(Path(item['path'])) == item['sha256']
    assert '`' + item['sha256'] + '`' in actual
candidates = json.loads((source.parent / 'candidate-paths.json').read_text())
for item in candidates['files']:
    assert f"| {item['number']} | `{item['path']}` |" in actual
    if item['existing']:
        assert '`' + item['baseline_sha256'] + '`' in actual
for path in [target, ROOT / 'administrative-map.json', ROOT / 'link-map.json', ROOT / 'check.py']:
    data = path.read_bytes()
    text = data.decode('utf-8')
    assert b'\r' not in data and data.endswith(b'\n')
    assert all(line == line.rstrip(' \t') for line in text.splitlines()), path
print(json.dumps({
    'kind': 'author formal-placement self-check only; independent final STATIC pending',
    'target': str(target),
    'target_sha256': sha(target),
    'fixed_rev2_sha256': sha(source),
    'inverse_administrative_normalization_equals_rev2_all_bytes': True,
    'source_hashes_verified': len(inputs['sources']),
    'candidate_paths': len(candidates['files']),
    'relative_local_links_verified': len(found),
    'json_utf8_lf_whitespace': 'PASS',
    'scratch_dangling_links': 0,
    'technical_changes': [],
    'product_execution': False,
    'resource_operations': False,
    'git_operations': False,
}, ensure_ascii=False, indent=2))
