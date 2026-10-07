from pathlib import Path
import collections, hashlib, json, re
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

B = Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01')
R = Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v05/runs/new-bounded-rerun02')
O = Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v04/runs/new-bounded-rerun01/safe-http-evidence')
S = Path('/workspace/agenteam/api/openapi')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p): return json.loads(Path(p).read_bytes())
assert sha(R/'result.json') == 'f52f67304214be6aa887ba44b05252a60f66da571d1d434dae8b38ca0013fc03'
r, v = read(R/'result.json'), read(R/'verification-input.json')
assert r['driver_exit'] == 0 and r['selected_tests_passed']
assert all(r[k] for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined'])
assert r['forced_tail_actions'] == r['monitor_errors'] == 0 and r['observed_resources'] == 7
assert sha(R/'frozen-input.json') == v['frozen_input_sha256']
assert sha(R/'driver.py.txt') == v['driver_sha256']
assert read(R/'input-before.json') == read(R/'input-after.json')
assert read(R/'input-before.json')['candidate_sha256'] == 'fd56d60def39af459246a3405eeea1fc6e34df4e1baadfc88bce3774ceb0ba72'
for c in read(R/'cleanup.json')[-2:]:
    assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries']
    assert len(c['exact_absent']) == 7 and all(x['absent'] for x in c['exact_absent'].values())
tail = read(R/'owned-tail-retirement.json')
assert tail['actual_owned_completion'] and not tail['actions'] and not tail['remaining']
raw = (R/'raw.log').read_text()
assert '--- PASS: TestModelProjectConfigurationHTTPBoundedRepresentation ' in raw and '--- FAIL:' not in raw
traces = []
method = None
for line in raw.splitlines():
    for name in ['GET','HEAD']:
        if line.startswith('=== RUN   TestModelProjectConfigurationHTTPBoundedRepresentation/oversize-original-Unknown/'+name): method = name
    if 'safe PG frame events after actual join:' not in line: continue
    assert method is not None
    events = line.split('after actual join: [', 1)[1].removesuffix(']').split()
    drops = [(i,event.split(':',1)[0]) for i,event in enumerate(events) if event.endswith(':commit-idle-ack-dropped')]
    assert len(drops) == 1
    drop_index, connection = drops[0]
    seq = [event.split(':',1)[1] for event in events[:drop_index+1] if event.startswith(connection+':')]
    expected = ['forwarded-large-D:9437584','send-COMMIT','tag-COMMIT','ready-I-before-drop','commit-idle-ack-dropped']
    indices = [seq.index(item) for item in expected]
    assert indices == sorted(indices) and len(set(indices)) == len(indices)
    later = [event for event in events[drop_index+1:] if ':forwarded-large-D:9437584' in event]
    assert len(later) == 1 and later[0].split(':',1)[0] != connection
    later_connection = later[0].split(':',1)[0]
    assert later_connection+':ready-I' in events[drop_index+1:]
    traces.append({'method':method,'drop_connection':connection,'actual_large_D_bytes':9437584,'ordered_events':expected,'later_new_read_connection':later_connection,'logged_after_actual_join':True})
assert [x['method'] for x in traces] == ['GET','HEAD']
base = 'https://independent-project-model-evidence.invalid/'
registry = Registry().with_resource(base+'common.json', Resource.from_contents(read(S/'common.json'), default_specification=DRAFT202012))
validator = Draft202012Validator({'$ref':base+'common.json#/components/schemas/Problem'}, registry=registry, format_checker=FormatChecker())
evidence = {}
for name in ['oversize-model','oversize-models','oversize-available','read-unknown']:
    body, source = R/'safe-http-evidence'/(name+'.json'), R/'safe-http-evidence'/(name+'-source.json')
    meta, wire = read(source), body.read_bytes()
    value = json.loads(wire)
    assert sha(body) == meta['body_sha256'] and sha(S/'project-models.json') == meta['schema_sha256']
    assert meta['run'] == r['run'] == 'new-bounded-rerun02' and meta['input_sha256'] == v['frozen_input_sha256']
    assert meta['method'] == 'GET' and meta['status'] == 503 and meta['content_type'] == 'application/problem+json'
    assert int(meta['content_length']) == len(wire) and meta['response_headers']['X-Request-ID'] == value['request_id']
    assert value['status'] == 503
    if name == 'read-unknown': assert value['code'] == 'COMMIT_UNKNOWN' and value['commit_state'] == 'unknown'
    else: assert value['code'] == 'DEPENDENCY_UNAVAILABLE' and value['commit_state'] == 'not_started'
    assert not set(value) & {'input','items','capabilities','reasoning_efforts','cause_id','attempt_id'}
    validator.validate(value)
    comparison = None
    if (O/body.name).exists():
        old = read(O/body.name)
        comparison = {'same_raw_bytes':sha(O/body.name)==sha(body),'changed_top_level_keys':sorted(k for k in set(old)|set(value) if old.get(k)!=value.get(k))}
    evidence[name] = {'body_sha256':sha(body),'source_sha256':sha(source),'bytes':len(wire),'schema':'common Problem','old_failure_body_comparison':comparison}
result = {'status':'AUTHOR_BOUNDED_PASS_ORIGINAL_BODY_AND_SAME_CONNECTION_EVIDENCE_CHECKED', 'author_result_sha256':sha(R/'result.json'),'raw_sha256':sha(R/'raw.log'),'frozen_input_sha256':v['frozen_input_sha256'],'bodies':evidence,'actual_safe_traces':traces,'wait_and_seven_resource_cleanup_verified':True,'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in r['new_pid1_zombies_not_owned_or_joined'])),'limits':['Independent offline consumption of completed author run; no new native/PG execution','Actual PostgreSQL with controlled handler writer, not native transport','Paired HEAD emptiness and original cause/attempt checks are unchanged author assertions bound by fixed candidate05 and passing raw','Both earlier failed bounded rounds stay failed with original input/raw preserved','Other active author root/old rounds excluded']}
(B/'bounded-pass-result.json').write_text(json.dumps(result, indent=2)+'\n')
print('PASS four original Problem bodies/schema and GET+HEAD same-connection largeD/C/Z/drop evidence; author run actualwait/seven-resource cleanup bound; no independent PG execution')
