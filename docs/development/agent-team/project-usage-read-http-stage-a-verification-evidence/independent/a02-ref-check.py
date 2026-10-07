import pathlib,json,hashlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02')
doc=json.loads((base/'api/openapi/project-usage.json').read_text());common=json.loads((base/'api/openapi/common.json').read_text())
url='https://usage-independent.invalid/project-usage.json';curl='https://usage-independent.invalid/common.json'
registry=Registry().with_resources([(url,Resource.from_contents(doc,default_specification=DRAFT202012)),(curl,Resource.from_contents(common,default_specification=DRAFT202012))])
refs=[]
def walk(x,where):
 if isinstance(x,dict):
  if '$ref' in x:refs.append((where,x['$ref']))
  for k,v in x.items():walk(v,where+'/'+k)
 elif isinstance(x,list):
  for k,v in enumerate(x):walk(v,where+'/'+str(k))
walk(doc,'')
fail=[]
for where,ref in refs:
 try:registry.resolver(url).lookup(ref)
 except Exception as e:fail.append({'location':where,'ref':ref,'error':type(e).__name__+': '+str(e)})
print(json.dumps({'refs':len(refs),'failed':len(fail),'failures':fail},indent=2))
schema=doc['paths']['/api/v1/projects/{id}/model-usage']['get']['parameters'][9]['schema']
print('from parameter direct standard validation:',schema)
try:
 list(Draft202012Validator({'$id':url,**schema},registry=registry).iter_errors('2026-10-07T00:00:00Z'))
except Exception as e:print(type(e).__name__+': '+str(e))
sys.exit(1 if fail else 0)
