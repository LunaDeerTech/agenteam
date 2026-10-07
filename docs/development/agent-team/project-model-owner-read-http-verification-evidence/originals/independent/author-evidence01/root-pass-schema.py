from pathlib import Path
import json,hashlib,collections
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
B=Path('/workspace/scratch/summary-verification/project-model-owner-read-http-author-evidence01');R=Path('/workspace/scratch/project-model-owner-read-http-author/pg-driver-v06/runs/new-root-rerun01');S=Path('/workspace/agenteam/api/openapi');sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
def read(p):return json.loads(Path(p).read_bytes())
assert sha(R/'result.json')=='83bc38dc432c083f7eccafa544fc55de0b3c546f8bc0aa1b34723431806de9ba'
r=read(R/'result.json');v=read(R/'verification-input.json');f=read(R/'frozen-input.json');assert sha(R/'frozen-input.json')==v['frozen_input_sha256'];assert sha(R/'driver.py.txt')==v['driver_sha256']==f['driver_sha256']
assert all(r[k] for k in ['actual_wait_completed','double_cleanup','resource_topology_matches','selected_tests_passed','source_files_unchanged','top_watchdog_complete','verification_inputs_unchanged','watchdog_thread_joined'])
assert r['driver_exit']==0 and r['forced_tail_actions']==0 and r['monitor_errors']==0 and r['observed_resources']==7
assert read(R/'input-before.json')==read(R/'input-after.json');assert read(R/'input-before.json')['candidate_sha256']=='f2dc9c81c478cf85f09695d955ebb7e8ec56050eff919bb931b0493d2d794e4b'
cleanup=read(R/'cleanup.json');assert len(cleanup)>=2
for c in cleanup[-2:]:assert c['baseline_unchanged'] and not c['owned_processes'] and not c['remaining_new'] and not c['runtime_entries'] and len(c['exact_absent'])==7 and all(x['absent'] for x in c['exact_absent'].values())
tail=read(R/'owned-tail-retirement.json');assert tail['actual_owned_completion'] and not tail['actions'] and not tail['remaining']
raw=(R/'raw.log').read_text();assert '--- PASS: TestModelProjectConfigurationHTTPDefaultRoot ' in raw and '--- FAIL:' not in raw
base='https://independent-project-model-evidence.invalid/';registry=Registry()
for name in ['common.json','project-models.json']:registry=registry.with_resource(base+name,Resource.from_contents(read(S/name),default_specification=DRAFT202012))
def validate(name,value):Draft202012Validator({'$ref':base+'project-models.json#/components/schemas/'+name},registry=registry,format_checker=FormatChecker()).validate(value)
def unique_object(pairs):
 d={}
 for k,v in pairs:
  assert k not in d,'duplicate JSON member';d[k]=v
 return d
mapping={'root-providers':'ProviderPage','root-provider':'Provider','root-models':'ModelPage','root-model':'Model','root-available':'AvailableChatModelPage'};evidence={};ids=set()
for name,schema in mapping.items():
 body=R/'safe-http-evidence'/(name+'.json');source=R/'safe-http-evidence'/(name+'-source.json');meta=read(source);original=body.read_bytes();value=json.loads(original,object_pairs_hook=unique_object)
 assert meta['body_sha256']==sha(body) and meta['schema_sha256']==sha(S/'project-models.json')
 assert meta['run']==r['run']=='new-root-rerun01' and meta['input_sha256']==v['frozen_input_sha256']
 assert meta['method']=='GET' and meta['status']==200 and meta['content_type']=='application/json' and int(meta['content_length'])==len(original)
 requestID=meta['response_headers']['X-Request-ID'];assert FormatChecker().conforms(requestID,'uuid') and requestID not in ids;ids.add(requestID)
 validate(schema,value)
 if schema in ['Provider','Model']:assert value['id']==meta['target'].split('/')[-1]
 if schema=='AvailableChatModelPage':
  assert len(value['items'])==1
  for row in value['items']:assert set(row)=={'id','provider_id','scope','name','provider_name','version','capabilities'}
  assert b'directory-' not in original and b'credential_ref' not in original
 assert b'credential_ref' not in original if schema=='AvailableChatModelPage' else True
 evidence[name]={'schema':schema,'body_sha256':sha(body),'source_sha256':sha(source),'bytes':len(original),'request_id':requestID,'source_status_type_length':True}
result={'status':'AUTHOR_DEFAULT_ROOT_ORIGINAL_HTTP_BYTES_SCHEMA_PASS','source_run':str(R),'author_result_sha256':sha(R/'result.json'),'frozen_input_sha256':v['frozen_input_sha256'],'schema_sha256':sha(S/'project-models.json'),'common_schema_sha256':sha(S/'common.json'),'bodies':evidence,'run_terminal_checked':True,'owned_cleanup_checked':True,'new_nonowned_nonwaited_PID1':dict(collections.Counter(x['name'] for x in r['new_pid1_zombies_not_owned_or_joined'])),'limits':['actual default app.Run nil hooks and native HTTP client over PostgreSQL; author execution only, not independent PG execution','offline Python consumes original body bytes and actual metadata; top PASS and fixed source bind startup, prior Update, drain/restart assertions; no new HTTP/DB request','no independent native/PG window started; active author old group excluded; earlier root failure retained','no whole-machine zero claim']};(B/'root-pass-result.json').write_text(json.dumps(result,indent=2)+'\n');print('PASS five original native default-root responses/schema; actual metadata and whole author run wait/input/seven-resource cleanup bound; not independent PG execution')
