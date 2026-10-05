from pathlib import Path
import difflib
import hashlib
import json

OUT = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence')
OLD = ROOT / 'ui-review-01'
NEW = ROOT / 'ui-review-02'
CORE = ROOT / 'core-review-02'
records = []

def check(path, expected=None):
    path = path.resolve()
    data = path.read_bytes()
    actual = hashlib.sha256(data).hexdigest()
    if expected is not None:
        assert actual == expected, (str(path), actual, expected)
    records.append({'path': str(path), 'sha256': actual, 'bytes': len(data),
                    'expected': expected, 'matched': expected is None or actual == expected})
    return data

old = json.loads(check(OLD / 'manifest.json', '6e08def62ab40b7d4f6916467eeab1e3c425c07c05ebc30c7fe519555d7c669b'))
new = json.loads(check(NEW / 'manifest.json', 'e9fab84272bef87ad3438733fb2190d88b82b7a6211abdbb9e0399f9b55686e0'))
core = json.loads(check(CORE / 'manifest.json', '0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece'))
assert old['base'] == new['base'] == core['base'] == '457b1979c9d6563740543b2011eedc06cce34c71'
assert old['files'].keys() == new['files'].keys()
changed = []
diffs = []
for path, digest in new['files'].items():
    before = check(OLD / path, old['files'][path])
    after = check(NEW / path, digest)
    if before != after:
        changed.append(path)
        diffs.extend(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                                        fromfile='ui01/' + path, tofile='ui02/' + path))
assert set(changed) == {
    'web/src/components/account/RotateChallenge.vue',
    'web/src/views/auth/LoginView.vue',
    'web/src/tests/authentication.spec.ts',
    'web/src/tests/rotate-challenge.spec.ts',
}
derived = ''.join(diffs).encode()
supplied = check(NEW / 'delta.patch')
assert derived == supplied, 'derived diff differs from supplied delta'
for path, digest in new['logs'].items():
    check(Path(path), digest)
red = []
for entry in new['red_inputs']:
    manifest_path = (NEW / entry).resolve()
    manifest = json.loads(check(manifest_path))
    for path, digest in manifest['files'].items():
        check(manifest_path.parent / path, digest)
    red.append(manifest)
assert red[0]['files']['web/src/views/auth/LoginView.vue'] == old['files']['web/src/views/auth/LoginView.vue']
assert red[1]['files']['web/src/views/auth/LoginView.vue'] == red[2]['files']['web/src/views/auth/LoginView.vue']
for entry in core['files']:
    check(CORE / entry['path'], entry['sha256'])

result = {
    'result': 'PASS',
    'base': new['base'],
    'scope': 'read fixed text and SHA-256 only; no product/runtime execution',
    'ui02_files': len(new['files']),
    'prior_ui01_files': len(old['files']),
    'author_logs': len(new['logs']),
    'red_manifests': len(red),
    'red_payloads': sum(len(item['files']) for item in red),
    'fixed_core02_entries': len(core['files']),
    'changed_paths': changed,
    'unchanged_ui_paths': [p for p in new['files'] if p not in changed],
    'exact_delta_sha256': hashlib.sha256(derived).hexdigest(),
    'exact_delta_matches': True,
    'records': records,
}
(OUT / 'checks.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({k:v for k,v in result.items() if k != 'records'}, ensure_ascii=False))
