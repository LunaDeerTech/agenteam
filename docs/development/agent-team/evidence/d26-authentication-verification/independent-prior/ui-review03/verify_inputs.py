from pathlib import Path
import difflib
import hashlib
import json

OUT = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence')
OLD = ROOT / 'ui-review-02'
NEW = ROOT / 'ui-review-03'
CORE = ROOT / 'core-review-02'
records = []

def check(path, expected=None):
    path = path.resolve()
    data = path.read_bytes()
    sha = hashlib.sha256(data).hexdigest()
    assert expected is None or sha == expected, (str(path), sha, expected)
    records.append({'path': str(path), 'sha256': sha, 'bytes': len(data), 'expected': expected})
    return data

old = json.loads(check(OLD / 'manifest.json', 'e9fab84272bef87ad3438733fb2190d88b82b7a6211abdbb9e0399f9b55686e0'))
new = json.loads(check(NEW / 'manifest.json', 'd9b0472650e1705273cf0e5009dc70855e54893285779181905d0e563fa0ed0b'))
core = json.loads(check(CORE / 'manifest.json', '0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece'))
assert old['base'] == core['base'] == '457b1979c9d6563740543b2011eedc06cce34c71'
assert new['baseline'] == old['base'][:7]
assert old['files'].keys() == new['files'].keys()
changed, parts = [], []
for path, sha in new['files'].items():
    before = check(OLD / path, old['files'][path])
    after = check(NEW / path, sha)
    if before != after:
        changed.append(path)
        parts.extend(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                                         fromfile='ui02/' + path, tofile='ui03/' + path))
assert set(changed) == {'web/src/views/auth/LoginView.vue', 'web/src/tests/authentication.spec.ts'}
derived = ''.join(parts).encode()
assert derived == check(NEW / 'delta.patch')
for path, sha in new['logs'].items():
    check(Path(path), sha)
red_dirs = []
for path in new['red_inputs']:
    manifest_path = (NEW / path).resolve()
    red_dirs.append(manifest_path.parent)
    red = json.loads(check(manifest_path))
    for name, sha in red['files'].items():
        check(manifest_path.parent / name, sha)
    assert red['files']['web/src/views/auth/LoginView.vue'] == old['files']['web/src/views/auth/LoginView.vue']
test = 'web/src/tests/authentication.spec.ts'
for name, before, after in [('red01-to-red02.patch', red_dirs[0], red_dirs[1]), ('red02-to-final.patch', red_dirs[1], NEW)]:
    delta = ''.join(difflib.unified_diff((before / test).read_text().splitlines(True), (after / test).read_text().splitlines(True),
                                       fromfile=before.name + '/' + test, tofile=after.name + '/' + test))
    (OUT / name).write_text(delta)
for item in core['files']:
    check(CORE / item['path'], item['sha256'])

result = {'result': 'PASS', 'scope': 'fixed static SHA/diff only; no product execution',
          'ui_before_files': 10, 'ui_after_files': 10, 'logs': 6, 'red_manifests': 2, 'red_payloads': 4,
          'fixed_core02_entries': 7, 'changed_paths': changed,
          'exact_delta_matches': True, 'delta_sha256': hashlib.sha256(derived).hexdigest(), 'records': records}
(OUT / 'checks.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({k: v for k, v in result.items() if k != 'records'}, ensure_ascii=False))
