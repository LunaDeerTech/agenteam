from pathlib import Path
import hashlib
import json
import subprocess

OUT = Path(__file__).resolve().parent
ROOT = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1')
BASE = '457b1979c9d6563740543b2011eedc06cce34c71'
inputs = []
commands = []

def read(path, expected=None):
    path = Path(path)
    data = path.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    if expected:
        assert digest == expected, str(path)
    inputs.append({'path': str(path), 'sha256': digest, 'bytes': len(data)})
    return data

before = ROOT / 'evidence/fixture-input-04'
after = ROOT / 'evidence/fixture-input-05'
delta = ROOT / 'evidence/ui-review-05'
a = json.loads(read(before / 'manifest.json', '918ea0883bb77fab0beedcc48cfbbeaa4d38a32230525bdf2fb92090f5af0bdd'))
b = json.loads(read(after / 'manifest.json', '2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966'))
d = json.loads(read(delta / 'manifest.json', '202204f07dc30851044ad34adbe8476e9c9282c5a96f0878dc32393b8deae0dd'))
for directory, manifest in [(before, a), (after, b)]:
    for name, digest in manifest['files'].items():
        read(directory / name, digest)
assert set(a['files']) == set(b['files']) and len(a['files']) == 21
changed = [p for p in a['files'] if a['files'][p] != b['files'][p]]
assert changed == ['web/src/App.vue', 'tests/account-captcha-web/e2e/authentication.spec.ts']
for name, digest in d['files'].items():
    assert a['files'][name] == d['files_before'][name]
    assert b['files'][name] == digest
    read(delta / name, digest)
read(delta / 'delta.patch', '85a84b5108e1b9281e5121331ae36ea56e468123b6b580b306ac994e3bffd767')
read(delta / 'review.md', '14e253076ba47f5faf550af29966c94ae61e1e537a58b62aaf252fd3406dfa2c')
author_commands = []
for path, digest in d['logs'].items():
    raw = read(path, digest)
    if path.endswith('.result.json'):
        result = json.loads(raw)
        assert result['exit'] == 0
        author_commands.append({'metadata_path': path, **result})
for path, digest in d['original_screenshots'].items():
    read(path, digest)
read(d['retained_browser_red']['path'], d['retained_browser_red']['sha256'])
for filename in ['command.json', 'result.json']:
    read(ROOT / 'evidence/auth-revocation-layouts-01' / filename)
plan = json.loads(read(ROOT / 'evidence/fixture-plan-06.json', 'fd9e67ad70ca8b37b8a16a5f1e7b7cd5c629390609e5db6c194cc53661c99821'))
assert plan['groups'][0]['argv'] == ['sh', 'scripts/test-objects.sh', '-run', '^TestAccountAuthenticationWebLayoutsAndProduction$']
assert plan['budgets'] == {'driver': 'original race/count1/6m per package', 'go_top': 'original2m including subcases', 'playwright': '45s workers1 retries0'}
spec = 'tests/account-captcha-web/e2e/authentication.spec.ts'
old, new = (before / spec).read_text(), (after / spec).read_text()
start = 'async function noOverflow(page: Page) {'
end = 'async function noStoredMaterials(page: Page) {'
assert old.split(start)[0] == new.split(start)[0]
assert old.split(end)[1] == new.split(end)[1]
assert 'document.documentElement.scrollWidth <= innerWidth' in new
assert 'document.documentElement.style.zoom = "2";' in new
assert 'expect(fits).toBe(true);' in new
for path in ['web/src/styles/base.css', 'web/src/styles/components.css', 'web/src/styles/tokens.css', 'web/src/components/layout/SystemNav.vue', 'web/src/components/ui/UiButton.vue']:
    argv = ['git', 'show', f'{BASE}:{path}']
    proc = subprocess.run(argv, cwd='/workspace/agenteam', capture_output=True)
    commands.append({'argv': argv, 'cwd': '/workspace/agenteam', 'exit': proc.returncode})
    assert proc.returncode == 0, path
    inputs.append({'git_commit': BASE, 'path': path, 'sha256': hashlib.sha256(proc.stdout).hexdigest(), 'bytes': len(proc.stdout)})
assert {p: h for p, h in a['fixed_dependencies_and_dist'].items() if not p.startswith('web/dist/')} == {p: h for p, h in b['fixed_dependencies_and_dist'].items() if not p.startswith('web/dist/')}
checks = {
    'verdict': 'STATIC PASS; actual post-fix layout remains unverified',
    'source_manifest_checks': {'input04': 21, 'input05': 21, 'ui05': 2, 'changed': changed, 'unchanged': 19},
    'same_e2e_outside_noOverflow': True,
    'original_zoom_and_boolean_assertion_preserved': True,
    'plan_selector_budgets_preserved': True,
    'author_log_fingerprints_checked': len(d['logs']),
    'original_screenshot_fingerprints_checked': len(d['original_screenshots']),
    'non_dist_dependency_declarations_unchanged': True,
    'author_commands': author_commands,
    'limits': ['No npm, browser, Go, Docker or network command executed by this reviewer.', 'No active author input or dist bytes inspected.', 'Author build metadata is reused, not an independent build.', 'Original screenshots precede zoom and cannot prove the overflowing element.', 'Resource cleanup was not reverified by this static review.'],
}
(OUT / 'inputs.json').write_text(json.dumps(inputs, ensure_ascii=False, indent=2) + '\n')
(OUT / 'checks.json').write_text(json.dumps(checks, ensure_ascii=False, indent=2) + '\n')
(OUT / 'read-commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'checked_sources': 44, 'changed': changed, 'author_metadata_exit0': len(author_commands), 'static': 'PASS', 'output': str(OUT)}, ensure_ascii=False))
