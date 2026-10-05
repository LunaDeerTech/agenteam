from pathlib import Path
import hashlib
import json
import difflib
import re

R = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence')
O = Path(__file__).parent
expected = '0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece'
sha = lambda b: hashlib.sha256(b).hexdigest()
assert sha((R / 'core-review-02/manifest.json').read_bytes()) == expected
m = json.loads((R / 'core-review-02/manifest.json').read_text())
assert m['base'] == '457b1979c9d6563740543b2011eedc06cce34c71'
records = []
def check(p, digest):
    data = p.read_bytes()
    assert sha(data) == digest, p
    records.append({'path': str(p), 'sha256': digest, 'bytes': len(data)})
for item in m['files']:
    check(R / 'core-review-02' / item['path'], item['sha256'])
for item in m['logs']:
    check(Path(item['path']), item['sha256'])
for item in m['original_red_inputs']:
    p = Path(item['path'])
    check(p, item['sha256'])
    rm = json.loads(p.read_text())
    assert rm['base'] == m['base']
    for f in rm['files']:
        check(p.parent / f['path'], f['sha256'])
check(R / 'core-review-02/delta.patch', m['delta_sha256'])
oldm = json.loads((R / 'core-review-01/manifest.json').read_text())
assert sha((R / 'core-review-01/manifest.json').read_bytes()) == 'd58f18be1558b9e1493a40ec848070a32af92ed05037fb1c734aeff76b5aafbc'
for f in oldm['files']:
    check(R / 'core-review-01' / f['path'], f['sha256'])
diff = ''
changed = []
for f in m['files']:
    p = f['path']
    a = (R / 'core-review-01' / p).read_text()
    b = (R / 'core-review-02' / p).read_text()
    if a != b:
        changed.append(p)
    diff += ''.join(difflib.unified_diff(a.splitlines(True), b.splitlines(True),
                                       fromfile='core01/' + p, tofile='core02/' + p))
assert diff == (R / 'core-review-02/delta.patch').read_text()
assert set(changed) == {'web/src/api/client.ts', 'web/src/composables/useSession.ts',
                        'web/src/tests/account-client.spec.ts', 'web/src/tests/session.spec.ts'}
for p in ['web/src/api/client.ts', 'web/src/api/account.ts', 'web/src/composables/useSession.ts']:
    assert (R / 'core-f1-f2-red-01' / p).read_bytes() == (R / 'core-review-01' / p).read_bytes()

# Diagnostic token comparison preserves string values; it does not execute JS.
pattern = re.compile(r'''(?:'(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*"|`(?:\\.|[^`\\])*`|//[^\n]*|/\*[\s\S]*?\*/|[A-Za-z_$][A-Za-z0-9_$]*|[0-9]+|[^\s])''')
def tokens(s):
    return [t for t in pattern.findall(s) if not t.startswith('//') and not t.startswith('/*')]
test_delta = {}
for p in ['web/src/tests/account-client.spec.ts', 'web/src/tests/session.spec.ts']:
    a = tokens((R / 'core-f3-f4-red-01' / p).read_text())
    b = tokens((R / 'core-review-02' / p).read_text())
    rows = []
    for tag, i, j, k, l in difflib.SequenceMatcher(a=a, b=b, autojunk=False).get_opcodes():
        if tag != 'equal':
            rows.append({'kind': tag, 'before': a[i:j], 'after': b[k:l]})
    test_delta[p] = rows
(O / 'test-token-delta.json').write_text(json.dumps(test_delta, ensure_ascii=False, indent=2) + '\n')
(O / 'checks.json').write_text(json.dumps({
    'kind': 'static SHA and fixed-delta checks, no product execution',
    'manifest_sha256': expected, 'matched': records, 'changed_paths': changed,
    'exact_delta_reconstructed': True, 'package_and_account_unchanged': True,
    'original_F1_F2_production_identical_to_core01': True,
    'author_final_tests': 29,
    'not_run': ['JavaScript', 'npm', 'browser', 'Go', 'Docker', 'network'],
}, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'pass': True, 'checked_payloads': len(records), 'changed_paths': len(changed),
                  'exact_delta': True, 'package_and_account_unchanged': True}))
