import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path('/workspace/scratch/project-model-owner-read-http-spec')
REPO = Path('/workspace/agenteam')


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


draft = ROOT / 'draft01.md'
source = ROOT / 'inputs02.json'
body = draft.read_text(encoding='utf-8')
inputs = json.loads(source.read_text())
errors = []
links = []
for label, target in re.findall(r'\[([^\]]+)\]\(([^)]+)\)', body):
    path = Path(target) if target.startswith('/') else draft.parent / target
    links.append({'target': target, 'exists': path.exists()})
    if not path.exists():
        errors.append('missing link: ' + target)
if '\r' in body or not body.endswith('\n') or '\x00' in body:
    errors.append('UTF8/LF/final-newline/NUL')
if any(line.rstrip() != line for line in body.splitlines()):
    errors.append('trailing whitespace')
scope = re.findall(r'^\| (\d+) \| `([^`]+)`', body, re.M)
if [int(n) for n, _ in scope] != list(range(1, 15)):
    errors.append('scope numbering')
for number, rel in scope:
    if int(number) not in (9, 14) and (REPO / rel).exists():
        errors.append('new candidate already exists: ' + rel)
unchanged = []
for item in inputs['files']:
    path = Path(item['read_path'])
    actual = sha(path)
    unchanged.append({'logical_path': item['logical_path'], 'sha256': actual,
                      'matches': actual == item['sha256']})
    if actual != item['sha256']:
        errors.append('input drift: ' + item['logical_path'])
    expected = item['expected_sha256']
    if expected is not None and expected != actual:
        errors.append('accepted fingerprint mismatch: ' + item['logical_path'])
    if item['logical_path'] in inputs['active_update_paths_excluded']:
        if path == REPO / item['logical_path']:
            errors.append('active Update implementation used as source')
for item in inputs['inherited_indexes']:
    if sha(Path(item['path'])) != item['sha256']:
        errors.append('inherited index drift: ' + item['path'])
assert 1 + 12 * 1_048_576 == 12_582_913 > 8 * 1024 * 1024
result = {
    'status': 'AUTHOR_DOCUMENT_SELF_CHECK_PASS' if not errors else 'FAIL',
    'not_independent_static': True,
    'draft_sha256': sha(draft), 'inputs_sha256': sha(source),
    'source_count': len(unchanged),
    'accepted_fingerprint_count': sum(x['expected_sha256'] is not None for x in inputs['files']),
    'fingerprint_only_corroboration': [x['logical_path'] for x in inputs['files'] if x['expected_sha256'] is None],
    'source_hashes_rechecked_unchanged': all(x['matches'] for x in unchanged),
    'links': links, 'candidate_technical_paths': 13, 'README_last': 1,
    'new_candidate_paths_absent': len(scope) - 2,
    'errors': errors,
    'large_legal_capabilities_example': {
        'tokens': 'r00000000 through r000fffff', 'unique_token_count': 1_048_576,
        'token_ascii_bytes': 9, 'array_json_bytes_by_arithmetic': 12_582_913,
        'http_limit_bytes': 8_388_608, 'product_execution': False,
    },
    'no_git_go_network_tests_or_resources': True,
    'repository_writes': [],
}
(ROOT / 'check-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(result, ensure_ascii=False, indent=2))
sys.exit(bool(errors))
