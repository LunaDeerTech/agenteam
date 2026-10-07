# Frozen parser for real-body post-validation only; no synthetic HTTP substitution.
import hashlib,json,sys
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
body_path,source_path,schema_dir,kind,expected_run,expected_candidate=sys.argv[1:]
assert kind in ('patch','lookup')
raw=Path(body_path).read_bytes(); source=json.loads(Path(source_path).read_bytes())
assert source['status']==200 and source['content_type']=='application/json'
assert source['content_length']==str(len(raw))
assert source['body_sha256']==hashlib.sha256(raw).hexdigest()
assert source['run']==expected_run and source['candidate']==expected_candidate
assert source['producer_test']=='TestModelProjectOwnerUpdateIndependentRootAndHistory'
basepath='/api/v1/projects/'+source['target']
assert source['method']==('PATCH' if kind=='patch' else 'POST')
assert source['path']==basepath+('' if kind=='patch' else '/commands/lookup')
root=Path(schema_dir)
assert source['schema_sha256']==hashlib.sha256((root/'project-owner.json').read_bytes()).hexdigest()
def no_network(uri):
    raise RuntimeError('unfrozen/nonlocal reference')
registry=Registry(retrieve=no_network)
base='https://independent-owner-update.invalid/'
for name in ('project-owner.json','common.json'):
    doc=json.loads((root/name).read_bytes())
    registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
# Names confirmed against candidate06 schema.
name='Project' if kind=='patch' else 'UpdateLookupResult'
schema={'$ref':base+'project-owner.json#/components/schemas/'+name}
value=json.loads(raw)
errors=list(Draft202012Validator(schema,registry=registry,format_checker=FormatChecker()).iter_errors(value))
assert not errors, 'actual response schema mismatch'
if kind=='lookup':
    assert value['state']=='committed' and value['result']['command']=='update'
    value=value['result']['project']
assert value['id']==source['target']
print(json.dumps({'result':'ACTUAL_BODY_SCHEMA_PASS','kind':kind,'body_sha256':source['body_sha256'],'schema_sha256':source['schema_sha256'],'run':expected_run,'candidate':expected_candidate}))
