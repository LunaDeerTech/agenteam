import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

BASE = Path(__file__).parent
source = json.loads((BASE / 'authority-unknown-input.json').read_text())
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
for path, expected in source['files'].items():
    assert sha(path) == expected, 'frozen evidence changed'
checks = []
for row in source['runs']:
    run = Path(row['path'])
    result = json.loads((run / 'result.json').read_text())
    assert result['driver_exit'] == 0 and result['selected_tests_passed']
    for key in ['actual_wait_completed', 'double_cleanup', 'source_files_unchanged', 'verification_inputs_unchanged', 'resource_topology_matches', 'top_watchdog_complete', 'watchdog_thread_joined']:
        assert result[key], key
    assert result['observed_resources'] == 7 and result['forced_tail_actions'] == 0
    assert result['top_levels'] == [['PASS', row['top']]]
    command = json.loads((run / 'command.json').read_text())
    assert command['exit'] == 0 and command['actual_wait_completed'] and command['watchdog_thread_joined']
    tail = json.loads((run / 'owned-tail-retirement.json').read_text())
    assert tail['actual_owned_completion'] and not tail['actions'] and not tail['remaining']
    cleanup = json.loads((run / 'cleanup.json').read_text())
    assert len(cleanup) == 2
    for check in cleanup:
        assert check['baseline_unchanged'] and not check['owned_processes']
        assert len(check['exact_absent']) == 7 and all(x['absent'] for x in check['exact_absent'].values())
    watchdog = json.loads((run / 'watchdog.json').read_text())
    assert watchdog['complete'] and not watchdog['active'] and not watchdog['failures']
    input_sha = sha(run / 'frozen-input.json')
    assert json.loads((run / 'frozen-input.json').read_text())['candidate_sha256'] == source['candidate_sha256']
    verification = json.loads((run / 'verification-input.json').read_text())
    assert verification['frozen_input_sha256'] == input_sha and verification['driver_sha256'] == sha(run / 'driver.py.txt')
    raw = (run / 'raw.log').read_text()
    sublines = [x for x in raw.splitlines() if x.startswith('    --- PASS: ' + row['top'] + '/')]
    assert len(sublines) == row['subcases'] and '--- FAIL:' not in raw
    if row['name'] == 'new-authority03':
        assert 'original_NewFact_gate_valid=true real_foreign_issuer_Forbidden=true full_snapshot_rollback=true' in raw
        assert not list((run / 'safe-http-evidence').iterdir())
    else:
        expected = 'safe_proxy_stages=target-final-commit-intercepted,send-{tag},tag-{tag},ready-I-before-drop,terminal-idle-ack-dropped targeted_backend=true original_unknown=true same_context_confirmation=true'
        assert raw.count(expected.format(tag='COMMIT')) == 3 and raw.count(expected.format(tag='ROLLBACK')) == 1
    checks.append({'run': row['name'], 'top': row['top'], 'subcases': row['subcases'], 'result_sha256': sha(run / 'result.json'), 'seconds': command['seconds'], 'input_sha256': input_sha, 'owned_actual_wait_doublecleanup': True, 'owned_processes': result['observed_processes'], 'new_nonowned_shims_not_waited': len(result['new_pid1_zombies_not_owned_or_joined'])})

schema_dir = Path('/workspace/agenteam/api/openapi')
registry = Registry()
for name in ['project-models.json', 'common.json']:
    p = schema_dir / name
    registry = registry.with_resource(p.as_uri(), Resource.from_contents(json.loads(p.read_text()), default_specification=DRAFT202012))

def unique_pairs(pairs):
    out = {}
    for key, value in pairs:
        assert key not in out, 'duplicate response member'
        out[key] = value
    return out

run = Path(source['runs'][1]['path'])
input_sha = sha(run / 'frozen-input.json')
bodies = []
for name in source['body_names']:
    p = run / 'safe-http-evidence' / (name + '.json')
    raw = p.read_bytes()
    meta = json.loads(p.with_name(name + '-source.json').read_text())
    assert meta['run'] == 'new-unknown01' and meta['input_sha256'] == input_sha
    assert meta['body_sha256'] == sha(p) and meta['schema_sha256'] == sha(schema_dir / 'project-models.json')
    assert int(meta['content_length']) == len(raw) <= 1024 and meta['method'] == 'POST'
    assert meta['target'] == meta['path'] and meta['path'].startswith('/api/v1/projects/')
    assert set(meta['response_headers']) == {'X-Request-ID'} and meta['response_headers']['X-Request-ID']
    data = json.loads(raw, object_pairs_hook=unique_pairs)
    if name == 'confirmed-write':
        filename, schema = 'project-models.json', 'ConfigurationReceipt'
        assert meta['status'] == 200 and meta['content_type'] == 'application/json'
        assert data['kind'] == 'provider.create' and data['version'] == '1' and data['affected_references'] == '0'
    else:
        filename, schema = 'common.json', 'Problem'
        assert meta['status'] == 503 and meta['content_type'] == 'application/problem+json'
        assert data['status'] == 503 and data['code'] == 'COMMIT_UNKNOWN' and data['commit_state'] == 'unknown' and data['retry_hint'] == 'lookup'
        assert data['request_id'] == meta['response_headers']['X-Request-ID']
        assert not ({'found', 'receipt', 'kind', 'resource_id'} & data.keys())
    assert meta['path'].endswith('/model-commands/lookup' if name == 'lookup-read-unknown' else '/model-providers')
    validator = Draft202012Validator({'$ref': (schema_dir / filename).as_uri() + '#/components/schemas/' + schema}, registry=registry, format_checker=FormatChecker())
    assert not list(validator.iter_errors(data)), 'raw response schema mismatch'
    bodies.append({'name': name, 'schema': schema, 'body_sha256': sha(p), 'source_sha256': sha(p.with_name(name + '-source.json')), 'bytes': len(raw), 'origin': 'controlled result after real read, not physical read Unknown' if name == 'lookup-read-unknown' else 'real targeted writer ACK loss; held writer released to actual terminal'})
print(json.dumps({'status': 'PASS', 'runs': checks, 'bodies': bodies, 'independent_PG': False, 'limitations': ['Writer proxy closes client before forwarding held COMMIT/ROLLBACK; real terminal C+Z later observed, not terminal-first.', 'lookup-read-unknown is a controlled terminal substitution after real read, separate from physical writer Unknown.', 'Original authority01/02 and CRUD01 failures and independent preliminary correction remain; no whole-product PASS or whole-machine zero.']}, indent=2))
