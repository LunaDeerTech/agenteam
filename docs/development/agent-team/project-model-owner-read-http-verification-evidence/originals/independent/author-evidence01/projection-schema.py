from pathlib import Path
import json,hashlib,collections
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
B=Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01');R=Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v03/runs/new-projection01');S=Path('/workspace/agenteam/api/openapi');sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p):return json.loads(Path(p).read_bytes())
assert sha(R/'result.json')=='91986512f29fe93dab627acee40ad51dfa383871660ea79b4c5158e94167b5be'
r=read(R/'result.json');v=read(R/'verification-input.json');f=read(R/'frozen-input.json');assert sha(R/'frozen-input.json')==v['frozen_input_sha256'];assert sha(R/'driver.py.txt')==v['driver_sha256']==f['driver_sha256']
assert all(r[k] for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','selected_tests_passed','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined'])
assert r['driver_exit']==0 and r['forced_tail_actions']==0 and r['monitor_errors']==0 and r['observed_resources']==7
assert read(R/'input-before.json')==read(R/'input-after.json');assert read(R/'input-before.json')['candidate_sha256']=='dabbf4a609ac8dc1d817650cab4afa7515fbf9853d1c440f71ada9e8e5769c91'
cleanup=read(R/'cleanup.json');assert len(cleanup)>=2
for c in cleanup[-2:]:assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries'] and len(c['exact_absent'])==7 and all(x['absent'] for x in c['exact_absent'].values())
tail=read(R/'owned-tail-retirement.json');assert tail['actual_owned_completion'] and not tail['actions'] and not tail['remaining']
raw=(R/'raw.log').read_text();assert '--- PASS: TestModelProjectConfigurationHTTPProjection ' in raw and '--- FAIL:' not in raw
base='https://independent-project-model-evidence.invalid/';registry=Registry()
for name in ['common.json','project-models.json']:registry=registry.with_resource(base+name,Resource.from_contents(read(S/name),default_specification=DRAFT202012))
def validate(name,value):Draft202012Validator({'$ref':base+'project-models.json#/components/schemas/'+name},registry=registry,format_checker=FormatChecker()).validate(value)
def unique_object(pairs):
 d={}
 for k,v in pairs:
  assert k not in d,'duplicate JSON member';d[k]=v
 return d
mapping={'providers':'ProviderPage','provider':'Provider','models':'ModelPage','model':'Model','available':'AvailableChatModelPage','empty-models':'ModelPage'};evidence={};ids=set()
for name,schema in mapping.items():
 body=R/'safe-http-evidence'/(name+'.json');source=R/'safe-http-evidence'/(name+'-source.json');meta=read(source);original=body.read_bytes();value=json.loads(original,object_pairs_hook=unique_object)
 assert meta['body_sha256']==sha(body) and meta['schema_sha256']==sha(S/'project-models.json')
 assert meta['run']==r['run']=='new-projection01' and meta['input_sha256']==v['frozen_input_sha256']
 assert meta['method']=='GET' and meta['status']==200 and meta['content_type']=='application/json' and int(meta['content_length'])==len(original)
 requestID=meta['response_headers']['X-Request-ID'];assert FormatChecker().conforms(requestID,'uuid') and requestID not in ids;ids.add(requestID)
 validate(schema,value)
 if schema in ['Provider','Model']:assert value['id']==meta['target'].split('/')[-1]
 if schema=='AvailableChatModelPage':
  assert len(value['items'])==4
  for row in value['items']:assert set(row)=={'id','provider_id','scope','name','provider_name','version','capabilities'}
  assert b'directory-' not in original and b'credential_ref' not in original
 if name=='empty-models':assert original==b'{"items":[],"next_cursor":null}'
 evidence[name]={'schema':schema,'body_sha256':sha(body),'source_sha256':sha(source),'bytes':len(original),'request_id':requestID,'source_status_type_length':True}
result={'status':'PROJECTION_ORIGINAL_HANDLER_BYTES_SCHEMA_PASS','source_run':str(R),'author_result_sha256':sha(R/'result.json'),'frozen_input_sha256':v['frozen_input_sha256'],'schema_sha256':sha(S/'project-models.json'),'common_schema_sha256':sha(S/'common.json'),'bodies':evidence,'run_terminal_checked':True,'owned_cleanup_checked':True,'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in r['new_pid1_zombies_not_owned_or_joined'])),'limits':['actual Account/Model/Project/PostgreSQL, controlled in-process HTTP writer for this author top; not native HTTP transport','offline Python consumes original body bytes and actual metadata, no new HTTP/DB request','no independent native/PG window started; other active author runs not read','no whole-machine zero claim']};(B/'projection-result.json').write_text(json.dumps(result,indent=2)+'\n');print('PASS six original handler responses/schema; actual metadata/whole-run wait/input/seven-resource cleanup bound; native HTTP transport not claimed')
