#!/usr/bin/env python3
"""Read-only fixed-input and dependency-delta checks; executes no product code."""
from pathlib import Path
import hashlib
import json
import subprocess

ROOT = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/core-review-01')
OUT = Path(__file__).parent
REPO = Path('/workspace/agenteam')
BASE = '457b1979c9d6563740543b2011eedc06cce34c71'
EXPECTED = 'd58f18be1558b9e1493a40ec848070a32af92ed05037fb1c734aeff76b5aafbc'

def sha(data):
    return hashlib.sha256(data).hexdigest()

commands = []
def git(path, commit=BASE):
    argv = ['git', 'show', commit + ':' + path]
    p = subprocess.run(argv, cwd=REPO, capture_output=True, check=False)
    commands.append({'argv': argv, 'cwd': str(REPO), 'exit': p.returncode,
                     'stdout_sha256': sha(p.stdout), 'stderr': p.stderr.decode()})
    assert p.returncode == 0
    return p.stdout

assert sha((ROOT / 'manifest.json').read_bytes()) == EXPECTED
manifest = json.loads((ROOT / 'manifest.json').read_text())
assert manifest['baseline'] == BASE
items = []
for item in manifest['files'] + manifest['logs']:
    p = Path(item['path'])
    if not p.is_absolute():
        p = ROOT / p
    raw = p.read_bytes()
    assert sha(raw) == item['sha256'], p
    items.append({'path': str(p), 'sha256': sha(raw), 'bytes': len(raw)})

before_pkg = json.loads(git('web/package.json'))
after_pkg = json.loads((ROOT / 'web/package.json').read_text())
expected_pkg = json.loads(json.dumps(before_pkg))
expected_pkg['dependencies']['go-captcha-vue'] = '2.0.7'
assert after_pkg == expected_pkg
before_lock = json.loads(git('web/package-lock.json'))
after_lock = json.loads((ROOT / 'web/package-lock.json').read_text())
old = before_lock['packages']
new = after_lock['packages']
assert set(new) - set(old) == {'node_modules/go-captcha-vue'}
assert set(old) - set(new) == set()
for key in old:
    if key:
        assert old[key] == new[key], key
expected_root = json.loads(json.dumps(old['']))
expected_root['dependencies']['go-captcha-vue'] = '2.0.7'
assert new[''] == expected_root
assert {k: v for k, v in before_lock.items() if k != 'packages'} == {
    k: v for k, v in after_lock.items() if k != 'packages'}
assert new['node_modules/go-captcha-vue']['version'] == '2.0.7'
old_captcha = json.loads(git('tests/account-captcha-web/package-lock.json'))
assert new['node_modules/go-captcha-vue'] == old_captcha['packages']['node_modules/go-captcha-vue']

selected = [
    'api/openapi/account.json', 'api/openapi/common.json',
    'internal/central/account/http_auth.go',
    'internal/central/account/http_wire.go',
    'internal/central/account/validation.go',
    'internal/central/account/contract/profile.go',
    'internal/central/account/contract/identity.go',
]
for p in selected:
    git(p)
card = git('docs/development/work-items/d26-account-authentication.md',
           '64e47fbc180a1fb2da385e7d4fabcc880699b0d5')
assert sha(card) == manifest['card_sha256']
result = {
    'kind': 'static-input-and-locked-dependency-check-only',
    'baseline': BASE, 'manifest_sha256': EXPECTED,
    'files_and_author_logs_matched': items,
    'lock': {'only_added': 'go-captcha-vue@2.0.7',
             'unchanged_existing_nonroot_entries': len(old) - 1,
             'old_harness_go_captcha_entry_identical': True,
             'engines_scripts_and_other_declared_dependencies_unchanged': True},
    'git_reads': commands,
    'not_run': ['product JavaScript', 'npm', 'browser', 'Go', 'Docker', 'network'],
}
(OUT / 'checks.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'pass': True, 'candidate_files': len(manifest['files']),
                  'author_logs': len(manifest['logs']), 'git_materials': len(commands),
                  'existing_lock_entries_unchanged': len(old) - 1}, ensure_ascii=False))
