import datetime
import hashlib
import json
import pathlib
import time

root = pathlib.Path(__file__).resolve().parent
repository = pathlib.Path('/workspace/agenteam')
requests = []
for name in ('jina-openapi', 'jina-reranker-page', 'github-jina-sdk-search', 'jina-reranker-readme-main', 'jina-reranker-head'):
    meta_path = root / 'evidence' / (name + '.meta.json')
    meta = json.loads(meta_path.read_text())
    assert meta['exit_code'] != 0 and 'ended_ns' in meta
    url = meta.get('url', meta['argv'][2])
    files = []
    for relative in (pathlib.Path('sources') / (name + '.raw'), pathlib.Path('evidence') / (name + '.headers'), pathlib.Path('evidence') / (name + '.stderr'), pathlib.Path('evidence') / (name + '.raw'), pathlib.Path('evidence') / (name + '.meta.json')):
        path = root / relative
        if path.exists():
            files.append({'path': str(relative), 'bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
    requests.append({
        'name': name, 'requested_url': url,
        'started_utc': datetime.datetime.fromtimestamp(meta['started_ns'] / 1e9, datetime.timezone.utc).isoformat(),
        'exit_code': meta['exit_code'], 'http_observation': meta.get('http_observation'),
        'result': 'CONNECT proxy 403; no origin document acquired' if meta['exit_code'] == 56 else ('HTTP 404 error body only; candidate repository validity not established' if meta['exit_code'] == 22 else 'Remote repository reference discovery failed; no commit obtained'),
        'official_field_evidence': False, 'verified_repository': None, 'commit': None, 'license': None,
        'files': files,
    })
manifest = {
    'scope': 'Five bounded public-source requests, no authentication supplied by this task, no inference service invocation, no SDK installation/execution.',
    'authorization': 'New explicit root assignment authorized public read-only evidence collection despite the historical D09 section 6 stop of network research in its earlier stage.',
    'network_stopped': True,
    'stop_basis': 'After three CONNECT-proxy 403 failures and the final candidate README/reference discovery failures, root explicitly confirmed no more requests or proxy changes.',
    'requests': requests,
    'successful_official_documents': 0,
    'valid_official_repository_commit_license_established': False,
    'no_third_party_compatibility_inference': True,
}
(root / 'sources-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')

additional = []
for name in ('internal/central/model/authority.go', 'internal/central/model/adapter/openai_chat.go', 'internal/central/app/account.go', 'internal/central/app/project_usage.go'):
    path = repository / name
    additional.append({'path': name, 'bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
(root / 'evidence/repository-inputs-addendum.json').write_text(json.dumps(additional, indent=2) + '\n')
inputs = json.loads((root / 'evidence/repository-inputs.json').read_text())
for value in inputs:
    assert hashlib.sha256((repository / value['path']).read_bytes()).hexdigest() == value['sha256']
accepted = json.loads((root / 'evidence/accepted-dependency-match.json').read_text())
for value in accepted['files']:
    assert value['same']
    assert hashlib.sha256((repository / value['path']).read_bytes()).hexdigest() == value['accepted_sha256']
c0 = json.loads((root / 'evidence/c0-accepted-match.json').read_text())
for value in c0:
    assert value['same'] and value['exit_code'] == 0
    assert hashlib.sha256((repository / value['path']).read_bytes()).hexdigest() == value['accepted_sha256']
remaining = []
for request in requests:
    meta = json.loads((root / 'evidence' / (request['name'] + '.meta.json')).read_text())
    if pathlib.Path('/proc', str(meta['pid'])).exists():
        remaining.append(meta['pid'])
result = {
    'recorded_ns': time.time_ns(),
    'status': 'Research preparation only; official Jina field evidence missing; implementation card must not be frozen.',
    'source_attempt_count': 5,
    'source_attempt_failures': 5,
    'official_jina_field_facts_established': 0,
    'official_repository_commit_license_established': False,
    'upstream_c0_source_matches': 2,
    'accepted_embedding_shared_wire_source_matches': 6,
    'repository_inputs_unchanged_since_initial_read': True,
    'wire_preparation_dependencies': 'C0 and accepted D04/text/structured/embedding wire foundations exist; six selected shared wire files and two C0 files match accepted fingerprints. Full future compiler/runtime/fixture closure was not enumerated or executed.',
    'unmet_prerequisites': ['Official Jina API field/endpoint/auth/usage/error/cap evidence', 'Future complete bounded wire specification and independent review', 'Future implementation and controlled conformance/resource windows', 'For business Nonchat.Rerank only: Runtime/Invocation/consumer/production root binding'],
    'product_decision_required_now': False,
    'product_decision_note': 'Current blocker is evidence, not an unresolved user product choice. A conflict discovered in future official evidence must be escalated then.',
    'network_stopped': True,
    'no_sdk_install_or_execution': True,
    'no_live_model_request_or_resource_probe': True,
    'no_production_contract_or_lock_changes': True,
    'current_pid_observation': {'recorded_request_pids_present': remaining, 'scope': 'Current observation only. Original exit metadata retained; no new historical wait or machine-restart conclusion inferred.'},
    'scope_boundary': 'Does not revive Object runtime join, OpenAI tools independent verification, or SPA concurrent-publication stops.',
}
(root / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'sources': 5, 'official_field_facts': 0, 'accepted_source_matches': 8, 'recorded_pids_present': remaining, 'implementation_card_frozen': False}))
