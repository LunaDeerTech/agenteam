from pathlib import Path
import collections, hashlib, json
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

B = Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01')
R = Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v04/runs/new-bounded-rerun01')
S = Path('/workspace/agenteam/api/openapi')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p): return json.loads(Path(p).read_bytes())
assert sha(R/'result.json') == '3a20920ba75ca7e5018de9c5f0a0c10743b2cd1f5aff546de56250091630dce8'
r, v = read(R/'result.json'), read(R/'verification-input.json')
assert r['driver_exit'] == 1 and not r['selected_tests_passed']
assert all(r[k] for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined'])
assert r['forced_tail_actions'] == r['monitor_errors'] == 0 and r['observed_resources'] == 7
assert sha(R/'frozen-input.json') == v['frozen_input_sha256']
assert sha(R/'driver.py.txt') == v['driver_sha256']
assert read(R/'input-before.json') == read(R/'input-after.json')
assert read(R/'input-before.json')['candidate_sha256'] == '44e8691202a6887636264b4298056168ec00710663cd25d1df1b615202acda59'
for c in read(R/'cleanup.json')[-2:]:
    assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries']
    assert len(c['exact_absent']) == 7 and all(x['absent'] for x in c['exact_absent'].values())
raw = (R/'raw.log').read_text()
assert '--- FAIL: TestModelProjectConfigurationHTTPBoundedRepresentation ' in raw
assert 'HTTP=500 want=503 code=INTERNAL_ERROR commit=not_committed' in raw
base = 'https://independent-project-model-evidence.invalid/'
registry = Registry().with_resource(base+'common.json', Resource.from_contents(read(S/'common.json'), default_specification=DRAFT202012))
validator = Draft202012Validator({'$ref':base+'common.json#/components/schemas/Problem'}, registry=registry, format_checker=FormatChecker())
evidence = {}
for name in ['model','models','available']:
    assert '--- PASS: TestModelProjectConfigurationHTTPBoundedRepresentation/'+name+' ' in raw
    body = R/'safe-http-evidence'/('oversize-'+name+'.json')
    source = R/'safe-http-evidence'/('oversize-'+name+'-source.json')
    meta, wire = read(source), body.read_bytes()
    value = json.loads(wire)
    assert sha(body) == meta['body_sha256'] and sha(S/'project-models.json') == meta['schema_sha256']
    assert meta['run'] == r['run'] == 'new-bounded-rerun01' and meta['input_sha256'] == v['frozen_input_sha256']
    assert meta['method'] == 'GET' and meta['status'] == 503 and meta['content_type'] == 'application/problem+json'
    assert int(meta['content_length']) == len(wire) and meta['response_headers']['X-Request-ID'] == value['request_id']
    assert value['status'] == 503 and value['code'] == 'DEPENDENCY_UNAVAILABLE' and value['commit_state'] == 'not_started'
    assert not set(value) & {'input','items','capabilities','reasoning_efforts','cause_id','attempt_id'}
    validator.validate(value)
    evidence[name] = {'body_sha256':sha(body), 'source_sha256':sha(source), 'bytes':len(wire), 'schema':'common Problem'}
result = {'status':'PARTIAL_CAP_BODY_SCHEMA_PASS_WITH_WHOLE_AUTHOR_ROUND_FAILED', 'author_run_result_sha256':sha(R/'result.json'), 'bodies':evidence, 'whole_top':'FAIL original-Unknown GET/HEAD 500 not_committed, not a successful Unknown injection', 'wait_and_seven_resource_cleanup_verified':True, 'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in r['new_pid1_zombies_not_owned_or_joined'])), 'limits':['Only three original GET Problem bytes validated offline; paired HEAD emptiness is the author subcase assertion, not another execution','The run is FAIL; no full bounded or product acceptance','Actual DB with controlled handler writer, not native transport','No real frame-limit observation existed in old raw; static helper capacity conflict is recorded separately']}
(B/'bounded-partial-result.json').write_text(json.dumps(result, indent=2)+'\n')
print('PASS three original cap Problem bodies/schema; source whole round remains FAIL; actualwait and seven-resource doublecleanup bound')
