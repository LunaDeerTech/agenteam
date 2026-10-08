import hashlib
import json
from pathlib import Path

root = Path(__file__).parent
frozen = Path('/workspace/scratch/owner-ui-recovery/ui-v1')
repo = Path('/workspace/agenteam')
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
freeze = json.loads((frozen / 'freeze.json').read_text())
run = json.loads((root / 'run02/result.json').read_text())
assert run['pass'] is True and run['total_tests'] == run['passed_tests'] == 29
assert all(sha(repo / path) == value == sha(frozen / path) for path, value in freeze.items())
original = json.loads((root / 'run01/result.json').read_text())
probe = root / 'run01/ownership.independent.spec.ts'
assert sha(probe) == original['inputs_before'][str(root / 'ownership.independent.spec.ts')]
result = {
    'status': 'PASS', 'acceptance_scope': 'ui-v1 frozen 19 frontend sources: independent static review and controlled Session/workspace supplement only',
    'unresolved_must_fix': [], 'implementation_participation': False, 'product_files_written': [],
    'source_freeze': str(frozen / 'freeze.json'), 'source_freeze_sha256': sha(frozen / 'freeze.json'),
    'source_sha256': freeze, 'source_current_and_snapshot_match': True,
    'independent_run': {'result': str(root / 'run02/result.json'), 'sha256': sha(root / 'run02/result.json'), 'tests': 29, 'passed': 29, 'exit': 0, 'elapsed_seconds': run['elapsed_seconds'], 'inputs': len(run['inputs_before']), 'source_bindings': run['source_bindings'], 'inputs_same': True, 'owned_cleanup_two_scans': run['owned_cleanup_two_scans']},
    'prior_run_retained': {'result': str(root / 'run01/result.json'), 'sha256': sha(root / 'run01/result.json'), 'passed': 28, 'failed': 1, 'classification': 'independent probe omitted old Selection activeRoute context; not product defect', 'original_probe': str(probe), 'original_probe_sha256': sha(probe)},
    'reused_author_evidence_binding': {'path': str(root / 'author-binding.json'), 'sha256': sha(root / 'author-binding.json')},
    'reused_api': {'product_commit': '7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2', 'independent_tests': 258, 'rerun': False},
    'not_accepted_by_this_result': ['full D27', 'real browser or visual/keyboard/focus', 'real PG/API fixture or lookup state/commit facts', 'production SPA hosting/fallback/publication', 'stopped probes'],
    'review': {'path': str(root / 'review.md'), 'sha256': sha(root / 'review.md')},
    'frozen_evidence': {path.name: sha(path) for path in sorted(root.iterdir()) if path.is_file() and path.suffix in ['.py', '.ts', '.mjs']},
}
(root / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'status': result['status'], 'review_sha256': result['review']['sha256'], 'result_sha256': sha(root / 'result.json'), 'tests': 29}))
