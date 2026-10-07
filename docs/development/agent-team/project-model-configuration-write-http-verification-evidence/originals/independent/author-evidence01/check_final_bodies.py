import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012
BASE = Path(__file__).parent
source = json.loads((BASE / 'final-body-input.json').read_text())
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
for p, h in source['files'].items(): assert sha(p) == h, 'fixed input drift'
schema_dir = Path('/workspace/agenteam/api/openapi')
registry = Registry()
for filename in ['project-models.json', 'common.json']:
    p = schema_dir / filename
    registry = registry.with_resource(p.as_uri(), Resource.from_contents(json.loads(p.read_text()), default_specification=DRAFT202012))
def unique(pairs):
    d = {}
    for k, v in pairs:
        assert k not in d, 'duplicate response property'
        d[k] = v
    return d
checks = []
for group in source['runs']:
    run = Path(group['path'])
    result = json.loads((run / 'result.json').read_text())
    assert result['driver_exit'] == 0 and result['selected_tests_passed']
    assert all(result[k] for k in ['actual_wait_completed','double_cleanup','source_files_unchanged','verification_inputs_unchanged','resource_topology_matches','top_watchdog_complete','watchdog_thread_joined'])
    assert result['top_levels'] == [['PASS', group['top']]] and result['observed_resources'] == 7 and result['forced_tail_actions'] == 0
    c = json.loads((run / 'cleanup.json').read_text())
    assert len(c) == 2
    assert all(not x['owned_processes'] and x['baseline_unchanged'] and len(x['exact_absent']) == 7 and all(v['absent'] for v in x['exact_absent'].values()) for x in c)
    t = json.loads((run / 'owned-tail-retirement.json').read_text())
    assert t['actual_owned_completion'] and not t['remaining'] and not t['actions']
    command = json.loads((run / 'command.json').read_text())
    assert command['exit'] == 0 and command['actual_wait_completed'] and command['watchdog_thread_joined']
    frozen = json.loads((run / 'frozen-input.json').read_text())
    assert frozen['candidate_sha256'] == source['candidate_sha256']
    verification = json.loads((run / 'verification-input.json').read_text())
    assert verification['frozen_input_sha256'] == sha(run / 'frozen-input.json') and verification['driver_sha256'] == sha(run / 'driver.py.txt')
    bodies = {}
    for name in group['names']:
        p = run / 'safe-http-evidence' / (name + '.json')
        raw = p.read_bytes(); meta = json.loads(p.with_name(name + '-source.json').read_text())
        assert meta['body_sha256'] == sha(p) and meta['schema_sha256'] == sha(schema_dir / 'project-models.json')
        assert meta['run'] == group['name'] and meta['input_sha256'] == sha(run / 'frozen-input.json')
        assert meta['status'] == 200 and meta['content_type'] == 'application/json' and int(meta['content_length']) == len(raw) <= 1024
        assert meta['target'] == meta['path'] and meta['path'].startswith('/api/v1/projects/')
        assert set(meta['response_headers']) == {'X-Request-ID'} and meta['response_headers']['X-Request-ID']
        data = json.loads(raw, object_pairs_hook=unique)
        schema = 'ConfigurationLookup' if 'lookup' in name else 'ConfigurationReceipt'
        validator = Draft202012Validator({'$ref': (schema_dir / 'project-models.json').as_uri() + '#/components/schemas/' + schema}, registry=registry, format_checker=FormatChecker())
        assert not list(validator.iter_errors(data)), 'raw response schema mismatch'
        if schema == 'ConfigurationLookup':
            assert meta['method'] == 'POST' and meta['path'].endswith('/model-commands/lookup') and data['found'] is True
        else:
            assert data['affected_references'] == '0'
            kind = name.removeprefix('root-') if name.startswith('root-') else 'provider.create'
            assert data['kind'] == kind and meta['method'] == {'create':'POST','update':'PUT','delete':'DELETE'}[kind.split('.')[1]]
            if not name.startswith('root-'): assert data['version'] == '1'
        bodies[name] = data
        checks.append({'run': group['name'], 'name': name, 'schema': schema, 'bytes': len(raw), 'body_sha256': sha(p), 'source_sha256': sha(p.with_name(name+'-source.json'))})
    if group['name'] == 'new-root01':
        for kind in ['provider','model']:
            sequence = [bodies['root-'+kind+'.'+action] for action in ['create','update','delete']]
            assert len({r['resource_id'] for r in sequence}) == 1 and [r['version'] for r in sequence] == ['1','2','3']
    elif group['name'] == 'independent-a01': assert bodies['independent-lookup']['receipt'] == bodies['independent-confirmed']
    else: assert bodies['independent-root-historical-lookup']['receipt'] == bodies['independent-root-create']
assert len(checks) == 10
print(json.dumps({'status':'PASS','body_count':len(checks),'checks':checks,'scope':'only new final10 original bytes, prior CRUD8+Unknown4 not rerun; author root6 and independent A2/B2 kept distinct'},indent=2))
