import hashlib
import json
from pathlib import Path
import subprocess

root = Path(__file__).parent
author = Path('/workspace/scratch/owner-ui-recovery')
repo = Path('/workspace/agenteam')
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
freeze = json.loads((author / 'ui-v1/freeze.json').read_text())
assert sha(author / 'ui-v1/freeze.json') == 'b7a9ef34a10d0230b541d0daffb19d765d023aa27469a9ca144952f5d1bd9f64'
assert all(sha(repo / name) == value == sha(author / 'ui-v1' / name) for name, value in freeze.items())
rounds = []
for name in ['integrated-unit-all01', 'integrated-build01', 'integrated-format-check01']:
    folder = author / name
    before = json.loads((folder / 'inputs-before.json').read_text())
    after = json.loads((folder / 'inputs-after.json').read_text())
    result = json.loads((folder / 'result.json').read_text())
    command = json.loads((folder / 'command.json').read_text())
    assert before == after
    assert all(before.get(path) == value for path, value in freeze.items())
    assert all(sha(repo / path) == value for path, value in before.items())
    assert result['exit'] == 0 and result['timeout'] is False and result['inputs_same'] is True
    assert result['owned_scan_1'] == result['owned_scan_2'] == {}
    assert result['direct_actual_wait'] in [int(pid) for pid in result['owned_pids']]
    baseline = 0
    for path, value in before.items():
        if path in freeze:
            continue
        raw = subprocess.check_output(['git', 'show', '7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2:' + path], cwd=repo)
        assert hashlib.sha256(raw).hexdigest() == value, path
        baseline += 1
    raw = (folder / 'raw.log').read_text()
    if name == 'integrated-unit-all01':
        assert '52 passed (52)' in raw and '2077 passed (2077)' in raw
    if name == 'integrated-build01':
        assert '> vue-tsc --noEmit' in raw and 'built in 1.18s' in raw
    rounds.append({
        'name': name, 'argv': command['argv'], 'cwd': command['cwd'],
        'exit': result['exit'], 'elapsed_seconds': result['elapsed_seconds'],
        'inputs_count': len(before), 'all_19_frozen_inputs_match': True,
        'remaining_inputs_match_7db_count': baseline, 'current_and_before_after_all_match': True,
        'original_result_has_direct_actual_wait_and_double_empty': True,
        'evidence': {str(path): sha(path) for path in sorted(folder.iterdir()) if path.is_file()},
    })
manifest = author / 'ui-v1/assets.json'
assert sha(manifest) == '00bd636e8446c8e75690bcf07745975bd7a4f9b906172de4d3ae55ab10393752'
assets = json.loads(manifest.read_text())
dist = author / 'dist-ui01'
actual = {str(path.relative_to(dist)): sha(path) for path in dist.rglob('*') if path.is_file()}
assert actual == assets and len(actual) == 53
debug_hits = [name for name in assets if 'Debug' in name or (name.endswith('.js') and ('DebugView' in (dist / name).read_text() or 'views/debug/' in (dist / name).read_text()))]
assert not debug_hits
check = subprocess.run(['git', 'diff', '--check', '7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2', '--', *freeze], cwd=repo, capture_output=True)
assert check.returncode == 0
whitespace_errors = []
for name in freeze:
    raw = (repo / name).read_bytes()
    if not raw.endswith(b'\n') or b'\r' in raw:
        whitespace_errors.append(name + ': line endings')
    for number, line in enumerate(raw.decode().splitlines(), 1):
        if line != line.rstrip():
            whitespace_errors.append(f'{name}:{number}: trailing whitespace')
assert not whitespace_errors
payload = {
    'rounds': rounds,
    'frozen_sources_current_snapshot_all_match': True,
    'freeze_sha256': sha(author / 'ui-v1/freeze.json'),
    'assets_manifest_sha256': sha(manifest), 'assets_count': len(assets), 'assets_all_match': True,
    'debug_chunk_or_import_hits': debug_hits,
    'git_diff_check_exit': check.returncode,
    'all_19_including_untracked_whitespace_errors': whitespace_errors,
    'reused_not_independently_rerun': ['all 2077 unit cases', 'product fullgraph types', 'Vite production build', '19-source Prettier'],
}
(root / 'author-binding.json').write_text(json.dumps(payload, indent=2) + '\n')
print(json.dumps({'rounds': len(rounds), 'source_freeze_match': True, 'assets_count': len(assets), 'all_pass': True}))
