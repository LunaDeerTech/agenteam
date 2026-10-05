from pathlib import Path
import hashlib
import json
import difflib
import subprocess

R = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence')
O = Path(__file__).parent
BASE = '457b1979c9d6563740543b2011eedc06cce34c71'
sha = lambda x: hashlib.sha256(x).hexdigest()
m = json.loads((R / 'ui-review-01/manifest.json').read_text())
assert sha((R / 'ui-review-01/manifest.json').read_bytes()) == '6e08def62ab40b7d4f6916467eeab1e3c425c07c05ebc30c7fe519555d7c669b'
old = json.loads((R / 'web-check-01/manifest.json').read_text())
assert sha((R / 'web-check-01/manifest.json').read_bytes()) == '19906aa324b24d5b2a49f84ec0902a33f73d0320c94d5cff00e40bf9d26edf31'
assert m['base'] == old['base'] == BASE
records = []
for root, files in [(R / 'ui-review-01', m['files']), (R / 'web-check-01', old['files'])]:
    for name, wanted in files.items():
        p = root / name
        raw = p.read_bytes()
        assert sha(raw) == wanted, p
        records.append({'path': str(p), 'sha256': wanted, 'bytes': len(raw)})
for name, wanted in m['logs'].items():
    raw = Path(name).read_bytes()
    assert sha(raw) == wanted, name
    records.append({'path': name, 'sha256': wanted, 'bytes': len(raw)})
core = json.loads((R / 'core-review-02/manifest.json').read_text())
assert sha((R / 'core-review-02/manifest.json').read_bytes()) == '0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece'
for entry in core['files']:
    assert sha((R / 'core-review-02' / entry['path']).read_bytes()) == entry['sha256']
    assert old['files'][entry['path']] == entry['sha256']
delta = ''
changed = []
for name in m['files']:
    a = (R / 'web-check-01' / name).read_text()
    b = (R / 'ui-review-01' / name).read_text()
    if a != b:
        changed.append(name)
    delta += ''.join(difflib.unified_diff(a.splitlines(True), b.splitlines(True),
                                       fromfile='web-check-01/' + name, tofile='ui-review-01/' + name))
(O / 'loop-to-ui01.patch').write_text(delta)
git_reads = []
for name in ['web/src/components/layout/SystemNav.vue', 'web/src/components/ui/UiButton.vue',
             'web/src/components/ui/UiInput.vue', 'web/src/components/ui/UiState.vue',
             'web/src/views/NotFoundView.vue', 'web/src/styles/base.css',
             'web/src/styles/components.css', 'web/src/tests/styles.spec.ts',
             'docs/frontend-design/layouts/account-entry.md',
             'docs/frontend-design/styles/colors-and-themes.md']:
    argv = ['git', 'show', BASE + ':' + name]
    p = subprocess.run(argv, cwd='/workspace/agenteam', capture_output=True)
    assert p.returncode == 0
    git_reads.append({'argv': argv, 'cwd': '/workspace/agenteam', 'exit': p.returncode,
                      'stdout_sha256': sha(p.stdout)})
sdk = Path('/workspace/agenteam-d26-auth-author-jrfhr6h1/input/web/node_modules/go-captcha-vue')
assert json.loads((sdk / 'package.json').read_text())['version'] == '2.0.7'
sdk_reads = []
for name in ['package.json', 'dist/go-captcha-vue.es.js',
             'dist/components/rotate/meta/data.d.ts', 'dist/components/rotate/meta/event.d.ts',
             'dist/components/rotate/meta/config.d.ts']:
    p = sdk / name
    sdk_reads.append({'path': str(p), 'sha256': sha(p.read_bytes())})
(O / 'checks.json').write_text(json.dumps({'input_manifest_sha256': sha((R / 'ui-review-01/manifest.json').read_bytes()),
    'matched_payloads': records, 'core02_unchanged_and_same_as_loop17': True,
    'loop_delta_paths': changed, 'git_reads': git_reads, 'locked_sdk_reads': sdk_reads,
    'not_run': ['npm', 'browser', 'Go', 'Docker', 'network', 'product JavaScript']}, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'pass': True, 'matched_payloads': len(records), 'core02_files_matched': len(core['files']),
                  'fixed_git_reads': len(git_reads), 'locked_sdk_reads': len(sdk_reads),
                  'loop_delta_paths': len(changed)}))
