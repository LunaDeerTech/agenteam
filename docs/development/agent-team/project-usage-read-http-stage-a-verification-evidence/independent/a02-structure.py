import json,pathlib,subprocess
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02');doc=json.loads((base/'api/openapi/project-usage.json').read_text())
expected={'/api/v1/projects/resolve':{'username','project_name'},'/api/v1/projects/{id}/model-usage':{'id','consumer_kind','agent_id','execution_id','meeting_id','purpose','provider_id','model_id','status','from','to','limit','cursor'},'/api/v1/projects/{id}/model-usage/summary':{'id','consumer_kind','agent_id','execution_id','meeting_id','purpose','provider_id','model_id','status','from','to','limit','cursor','group_by'}}
assert set(doc['paths'])==set(expected)
for path,keys in expected.items():
 ops=doc['paths'][path];assert set(ops)=={'get','head'}
 for method,op in ops.items():
  assert 'requestBody' not in op and op['security']==[{'browserSession':[]}]
  actual=[x['name'] for x in op['parameters']];assert len(actual)==len(set(actual)) and set(actual)==keys
  required={x['name'] for x in op['parameters'] if x.get('required')};assert required==({'username','project_name'} if 'resolve' in path else {'id','group_by'} if path.endswith('summary') else {'id'})
  for code,response in op['responses'].items():
   assert ('content' in response)==(method=='get')
  assert op['responses']['405']['headers']['Allow']['schema']['const']=='GET, HEAD'
for name,s in doc['components']['schemas'].items():
 if s.get('type')=='object':assert s['additionalProperties'] is False and set(s['required'])==set(s['properties']),name
source=(base/'internal/central/account/http_boundary.go').read_text();old=subprocess.check_output(['git','show','6fa2ee72:internal/central/account/http_boundary.go'],cwd='/workspace/agenteam').decode()
def original_methods(v):return v[v.index('func (b *HTTPBoundary) RequireSystem'):]
assert original_methods(source)==original_methods(old)
print('PASS: exact3 paths/6 operations/parameter sets, required query/path inputs, HEAD no bodies, closed object fields; original RequireSystem and later methods byte-identical.')
