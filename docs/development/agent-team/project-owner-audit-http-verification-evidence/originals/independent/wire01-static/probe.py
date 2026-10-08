from pathlib import Path
import json,copy,re,calendar
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
B=Path(__file__).parent
schema=Path('/workspace/scratch/project-owner-audit-http-author/wire01/src/api/openapi/project-audit.json')
common=Path('/workspace/scratch/project-owner-audit-http-spec-verification/rev1/fixed/api/openapi/common.json')
doc=json.loads(schema.read_bytes());base=schema.as_uri();registry=Registry().with_resource(base,Resource.from_contents(doc,default_specification=DRAFT202012)).with_resource((schema.parent/'common.json').as_uri(),Resource.from_contents(json.loads(common.read_bytes()),default_specification=DRAFT202012));checker=FormatChecker()
@checker.checks('date-time')
def instant(v):
 if not isinstance(v,str):return True
 m=re.fullmatch(r'(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?Z',v)
 if not m:return False
 y,mo,d,h,mi,s=map(int,m.groups());return 1<=mo<=12 and 1<=d<=calendar.monthrange(y,mo)[1] and h<24 and mi<60 and s<60
for v in doc['components']['schemas'].values():Draft202012Validator.check_schema(v)
validators={n:Draft202012Validator({'$ref':base+'#/components/schemas/'+n},registry=registry,format_checker=checker) for n in ['ProjectAuditRecord','ProjectAuditPage']}
U='01900000-0000-7000-8000-000000000001';P='01900000-0000-7000-8000-000000000003'
def record(action,kind,metadata):
 return {'audit_id':U,'created_at':'2026-10-08T01:02:03Z','scope':'project','project_id':P,'actor':{'kind':'human','id':U},'action':action,'outcome':'success','resource':{'kind':kind,'id':U},'metadata':metadata,'associations':{},'summary':'Audit event'}
vectors=[]
def add(name,body,want=True,schema='ProjectAuditRecord'):vectors.append({'name':name,'body':body,'want_valid':want,'schema':schema})
def mutate(name,v,path,val):
 v=copy.deepcopy(v);where=v
 for p in path[:-1]:where=where[p]
 if val=='__REMOVE__':where.pop(path[-1],None)
 else:where[path[-1]]=val
 add(name,v,False)
outbox=record('outbox.delivery.requeue','outbox_delivery',{'delivery_id':U,'event_id':U,'handler_id':'a','from_state':'failed','redrive_cycle':'1','reason_code':'operator_retry'})
add('outbox-positive',outbox)
mutate('outbox-handler-trailing-LF',outbox,['metadata','handler_id'],'a\n')
mutate('outbox-handler-CR',outbox,['metadata','handler_id'],'a\r')
mutate('outbox-handler-129',outbox,['metadata','handler_id'],'a'*129)
mutate('outbox-handler-upper',outbox,['metadata','handler_id'],'A')
project=record('project.update','project',{'project_id':P,'initiator_id':U,'project_version':'2','changed_fields':['name']});project['resource']['id']=P
add('project-update-positive',project)
mutate('project-update-forbidden-operation-association',project,['associations','operation_id'],U)
mutate('project-update-version-one',project,['metadata','project_version'],'1')
mutate('project-update-forbidden-transition',project,['metadata','from'],'active')
mutate('project-update-null-changed',project,['metadata','changed_fields'],None)
mutate('project-update-duplicate-changed',project,['metadata','changed_fields'],['name','name'])
artifact=record('artifact.download','artifact',{'artifact_id':U,'object_id':U,'media_type':'text/plain','byte_size':'9','sent_bytes':'9','phase':'sent'})
add('artifact-download-no-source-positive',artifact)
mutate('artifact-source-empty-forbidden',artifact,['metadata','source_kind'],'')
mutate('artifact-source-id-without-kind',artifact,['metadata','source_id'],U)
mutate('artifact-sent-reason-forbidden',artifact,['metadata','reason'],'timeout')
mutate('artifact-sent-failed-outcome',artifact,['outcome'],'failed')
failed=copy.deepcopy(artifact);failed['metadata'].update(phase='failed',reason='timeout');failed['outcome']='failed';add('artifact-failed-positive',failed)
mutate('artifact-failed-no-reason',failed,['metadata','reason'],'__REMOVE__')
for i,mime in enumerate(['text/plain; filename=x','Text/Plain','text/plain\n','text/plain; charset=UTF-8','text/plain; charset="utf-8"']):mutate('artifact-mime-reject-'+str(i),artifact,['metadata','media_type'],mime)
for field in ['actor','metadata','project_id','summary']:
 mutate('record-null-'+field,outbox,[field],None)
 mutate('record-missing-'+field,outbox,[field],'__REMOVE__')
mutate('record-system-scope',outbox,['scope'],'system')
mutate('record-system-action',outbox,['action'],'account.login')
agent=copy.deepcopy(artifact);agent['actor']={'kind':'agent_run','id':U,'project_id':P,'execution_id':U};agent['associations']={'execution_id':U};add('agent-download-positive',agent);mutate('agent-missing-association',agent,['associations','execution_id'],'__REMOVE__')
for stamp in ['0000-02-29T00:00:00Z','9999-12-31T23:59:59.999999Z']:
 v=copy.deepcopy(outbox);v['created_at']=stamp;add('calendar-positive-'+stamp,v)
for stamp in ['0000-02-30T00:00:00Z','2026-02-29T00:00:00Z']:mutate('calendar-negative-'+stamp,outbox,['created_at'],stamp)
add('page-positive',{'items':[outbox],'next_cursor':None},True,'ProjectAuditPage')
results=[]
for v in vectors:
 got=validators[v['schema']].is_valid(v['body']);results.append({'name':v['name'],'expected':v['want_valid'],'actual':got,'match':got==v['want_valid']})
(B/'probe-vectors.json').write_text(json.dumps(vectors,indent=2)+'\n');(B/'probe-results.json').write_text(json.dumps(results,indent=2)+'\n')
print(json.dumps({'vectors':len(results),'disagreements':[v for v in results if not v['match']]},indent=2))
raise SystemExit(1 if any(not v['match'] for v in results) else 0)
