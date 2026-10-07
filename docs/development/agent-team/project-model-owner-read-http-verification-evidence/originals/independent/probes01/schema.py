from pathlib import Path
import copy,json
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
B=Path('/workspace/scratch/summary-verification/project-model-owner-read-http-probes01');R=Path('/workspace/agenteam');base='https://agenteam.invalid/api/openapi/'
registry=Registry()
for name in ['common.json','project-models.json']:
 value=json.loads((R/'api/openapi'/name).read_bytes());registry=registry.with_resource(base+name,Resource.from_contents(value,default_specification=DRAFT202012))
samples={}
for line in (B/'controlled03/stdout.log').read_text().splitlines():
 if line.startswith('INDEPENDENT_SCHEMA '):
  _,name,hexbytes=line.split();samples[name]=json.loads(bytes.fromhex(hexbytes))
assert set(samples)=={'Provider','Model','AvailableChatModelPage'}
def validate(name,value):
 return not list(Draft202012Validator({'$ref':base+'project-models.json#/components/schemas/'+name},registry=registry,format_checker=FormatChecker()).iter_errors(value))
for name,value in samples.items():assert validate(name,value),name
assert set(samples['AvailableChatModelPage']['items'][0])=={'id','provider_id','scope','name','provider_name','version','capabilities'}
negatives=[]
for field in ['base_url','provider_model_id','options','header_overwrite','credential_ref']:
 v=copy.deepcopy(samples['AvailableChatModelPage']);v['items'][0][field]='private';negatives.append(('AvailableChatModelPage',v))
for name in ['Provider','Model']:
 v=copy.deepcopy(samples[name]);del v['input'];negatives.append((name,v))
 v=copy.deepcopy(samples[name]);v['scope']={'kind':'system'};negatives.append((name,v))
for name,value in negatives:assert not validate(name,value),name
print('PRIVATE_SCHEMA_PASS production_encoder_samples=3 negative_variants='+str(len(negatives))+' real_HTTP=false')
