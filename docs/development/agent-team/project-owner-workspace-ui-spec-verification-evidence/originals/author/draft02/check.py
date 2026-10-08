#!/usr/bin/env python3
"""Read-only checks for this scratch specification; never runs product tools."""
from pathlib import Path
import hashlib
import json
import re

ROOT = Path(__file__).resolve().parent
REPO = Path('/workspace/agenteam')

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

inputs = json.loads((ROOT / 'inputs.json').read_text())
candidates = json.loads((ROOT / 'candidate-paths.json').read_text())
draft = (ROOT / 'draft.md').read_text()
errors = []
for key, source in inputs['sources'].items():
    path = Path(source['path'])
    if not path.is_file() or sha(path) != source['sha256']:
        errors.append('source mismatch: ' + key)
assert candidates['count'] == len(candidates['files']) == 23
assert [x['number'] for x in candidates['files']] == list(range(1, 24))
assert len({x['path'] for x in candidates['files']}) == 23
for entry in candidates['files']:
    path = REPO / entry['path']
    if entry['existing']:
        if not path.is_file() or sha(path) != entry['baseline_sha256']:
            errors.append('candidate old source mismatch: ' + entry['path'])
    elif path.exists():
        errors.append('candidate new path already exists: ' + entry['path'])
    if f"| {entry['number']} | `{entry['path']}` |" not in draft:
        errors.append('candidate missing in table: ' + entry['path'])

links = re.findall(r'\[[^\]]+\]\(([^)]+)\)', draft)
for target in links:
    if target.startswith(('http://', 'https://')):
        errors.append('unexpected network link: ' + target)
    elif '#' in target:
        errors.append('fragment needs explicit checking: ' + target)
    elif not (ROOT / target).resolve().is_file():
        errors.append('missing local link: ' + target)

for name in ('draft.md', 'inputs.json', 'candidate-paths.json', 'check.py'):
    data = (ROOT / name).read_bytes()
    text = data.decode('utf-8')
    if b'\r' in data or not data.endswith(b'\n'):
        errors.append('newline: ' + name)
    if any(line != line.rstrip(' \t') for line in text.splitlines()):
        errors.append('trailing whitespace: ' + name)

tops = re.findall(r'^\d+\. `(TestAccount[^`]+)`$', draft, flags=re.M)
top_sources = {}
for source in inputs['sources'].values():
    p = Path(source['path'])
    if p.parent == REPO / 'tests/account' and p.suffix == '.go':
        for top in re.findall(r'^func (TestAccount\w+)\(', p.read_text(), re.M):
            top_sources[top] = str(p)
for top in tops:
    if top not in top_sources:
        errors.append('missing old top: ' + top)
assert len(tops) == 16

stopped = {
    'internal/central/webassets/handler.go',
    'internal/central/webassets/handler_test.go',
    'internal/central/webassets/bundle.go',
    'internal/central/webassets/bundle_test.go',
    'internal/central/webassets/bundle_release.go',
    'internal/central/webassets/bundle_backend.go',
    'internal/central/webassets/bundle/.gitignore',
    'internal/central/app/app.go',
    'internal/central/app/webassets.go',
    'internal/central/app/webassets_test.go',
    'scripts/build-central-web.mjs',
    'scripts/build-central-web.test.mjs',
    'tests/process/central_web_test.go',
    'tests/account-captcha-web/central-spa.config.js',
    'tests/account-captcha-web/e2e/central-spa.spec.ts',
    'docs/development/backend/frontend-hosting.md',
}
overlap = sorted(stopped & {x['path'] for x in candidates['files']})
if overlap:
    errors.append('SPA stopped path overlap: ' + ','.join(overlap))
bound = 100 * (6 * 8192 + 1024) + 6 * 8192 + 256
assert bound == 5067008 and bound < 5 * 1024 * 1024
result = {
    'kind': 'author scratch-document self-check only; not independent STATIC or product PASS',
    'files': {name: sha(ROOT / name) for name in ('draft.md', 'inputs.json', 'candidate-paths.json', 'check.py')},
    'source_count': len(inputs['sources']),
    'source_hash_mismatches': [],
    'candidate_count': 23,
    'technical_count': 22,
    'readme_last_number': 22,
    'candidate_old_count': sum(x['existing'] for x in candidates['files']),
    'candidate_new_count': sum(not x['existing'] for x in candidates['files']),
    'local_link_count': len(links),
    'old_top_count': len(tops),
    'old_tops': {top: top_sources.get(top) for top in tops},
    'spa_stopped_path_intersection': overlap,
    'complete_list_conservative_bytes': bound,
    'complete_list_cap_bytes': 5 * 1024 * 1024,
    'errors': errors,
    'product_commands_resources_git_executed': False,
}
print(json.dumps(result, ensure_ascii=False, indent=2))
raise SystemExit(bool(errors))
