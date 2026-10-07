from pathlib import Path
import collections, hashlib, json
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

B = Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01')
R = Path('/workspace/scratch/summary-verification/project-model-owner-read-http-runtime-prep01/pg-driver-v06/runs')
S = Path('/workspace/agenteam/api/openapi')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p): return json.loads(Path(p).read_bytes())
def unique(pairs):
    obj = {}
    for k,v in pairs:
        assert k not in obj, 'duplicate JSON member'
        obj[k] = v
    return obj
base = 'https://independent-project-model-evidence.invalid/'
registry = Registry()
for name in ['common.json','project-models.json']:
    registry = registry.with_resource(base+name, Resource.from_contents(read(S/name), default_specification=DRAFT202012))
mapping = {
    'independent-a01': {'independent-catalog-continuation':('project-models.json','AvailableChatModelPage',200), 'independent-read-unknown':('common.json','Problem',503)},
    'independent-b01': {'independent-root-provider':('project-models.json','Provider',200), 'independent-root-model':('project-models.json','Model',200), 'independent-root-directory':('project-models.json','AvailableChatModelPage',200)},
}
results = {}
for name, cases in mapping.items():
    run = R/name
    result, verification = read(run/'result.json'), read(run/'verification-input.json')
    assert result['driver_exit'] == 0 and all(result[k] for k in ['actual_wait_completed','double_cleanup','selected_tests_passed','source_files_unchanged','verification_inputs_unchanged','resource_topology_matches','watchdog_thread_joined','top_watchdog_complete'])
    assert result['observed_resources'] == 7 and result['forced_tail_actions'] == result['monitor_errors'] == 0
    assert read(run/'input-before.json') == read(run/'input-after.json')
    assert read(run/'input-before.json')['candidate_sha256'] == 'f2dc9c81c478cf85f09695d955ebb7e8ec56050eff919bb931b0493d2d794e4b'
    assert sha(run/'frozen-input.json') == verification['frozen_input_sha256']
    assert sha(run/'driver.py.txt') == verification['driver_sha256']
    for c in read(run/'cleanup.json')[-2:]:
        assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries']
        assert len(c['exact_absent']) == 7 and all(x['absent'] for x in c['exact_absent'].values())
    tail = read(run/'owned-tail-retirement.json')
    assert tail['actual_owned_completion'] and not tail['actions'] and not tail['remaining']
    bodies = {}
    for body_name,(schema_file,schema,status) in cases.items():
        body, sidecar = run/'safe-http-evidence'/(body_name+'.json'), run/'safe-http-evidence'/(body_name+'-source.json')
        original, source = body.read_bytes(), read(sidecar)
        value = json.loads(original, object_pairs_hook=unique)
        assert source['body_sha256'] == sha(body) and source['schema_sha256'] == sha(S/'project-models.json')
        assert source['status'] == status and source['method'] == 'GET' and source['run'] == name
        assert source['input_sha256'] == verification['frozen_input_sha256'] and int(source['content_length']) == len(original)
        assert source['content_type'] == ('application/problem+json' if status == 503 else 'application/json')
        assert FormatChecker().conforms(source['response_headers']['X-Request-ID'],'uuid')
        Draft202012Validator({'$ref':base+schema_file+'#/components/schemas/'+schema}, registry=registry, format_checker=FormatChecker()).validate(value)
        if schema in ['Provider','Model']: assert value['id'] == source['target'].rsplit('/',1)[1]
        if schema == 'AvailableChatModelPage':
            assert len(value['items']) == 1 and value['next_cursor'] is None
            for row in value['items']: assert set(row) == {'id','provider_id','scope','name','provider_name','version','capabilities'}
        if status == 503:
            assert value['code'] == 'COMMIT_UNKNOWN' and value['commit_state'] == 'unknown'
            assert source['response_headers']['X-Request-ID'] == value['request_id']
            assert not set(value) & {'input','items','capabilities','attempt_id','cause_id'}
        bodies[body_name] = {'body_sha256':sha(body),'source_sha256':sha(sidecar),'bytes':len(original),'schema':schema_file+'#'+schema,'actual_header_and_input_binding':True}
    results[name] = {'result_sha256':sha(run/'result.json'),'frozen_input_sha256':verification['frozen_input_sha256'],'bodies':bodies,'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in result['new_pid1_zombies_not_owned_or_joined']))}
out = {'status':'PASS_INDEPENDENT_A_B_ORIGINAL_FIVE_BODY_SCHEMA','runs':results,'schema_sha256':sha(S/'project-models.json'),'common_schema_sha256':sha(S/'common.json'),'limits':['A uses actual PostgreSQL plus controlled HTTP writer; B uses native default app.Run HTTP transport','Offline validation preserves original bytes/status/headers, no new network requests','No current read is treated as confirmation of prior Unknown','Daemon/PID1 zombies outside ownership were not waited; no whole-machine zero claim']}
(B/'independent-five-result.json').write_text(json.dumps(out,indent=2)+'\n')
print('PASS five original independent A/B response bodies through local-ref standard schemas; actual run/header/input bindings and full wait/seven-resource cleanup verified')
