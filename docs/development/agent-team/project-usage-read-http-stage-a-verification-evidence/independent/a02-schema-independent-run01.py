import json,copy,pathlib,hashlib,subprocess,sys,time,importlib.metadata
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path('/workspace/scratch/usage-http-verification/a02'); started=time.monotonic()
doc=json.loads((base/'api/openapi/project-usage.json').read_text());common=json.loads((base/'api/openapi/common.json').read_text())
url='https://usage-independent.invalid/project-usage.json'; curl='https://usage-independent.invalid/common.json'
registry=Registry().with_resources([(url,Resource.from_contents(doc,default_specification=DRAFT202012)),(curl,Resource.from_contents(common,default_specification=DRAFT202012))])
cases=[]
def add(label,schema,value,valid):cases.append({'label':label,'schema':schema,'value':copy.deepcopy(value),'valid':valid})
uid='01900000-0000-7000-8000-000000000001'; stamp='2026-10-07T01:02:03.123456Z'
counts=['input_tokens','output_tokens','total_tokens','cached_input_tokens','cache_write_tokens','reasoning_tokens']
unknown={k:None for k in counts};unknown['source']='unknown'
row=dict(id=uid,call_id=uid,attempt_index='9007199254740993',project_id=uid,consumer_kind='agent',purpose='agent_generation',agent_id=uid,execution_id=uid,meeting_id=None,provider_id=uid,model_id=uid,provider_name='Historical😀',model_name='Historical<&>',provider_model_id='native',protocol='openai-chat-completions',model_type='chat',live_provider_id=None,live_model_id=None,dispatch='reserved',started_at=stamp,dispatched_at=None,final=None,usage=unknown,provider_request_id=None)
pairs={'agent':['agent_generation','agent_compaction'],'meeting':['meeting_summary_initial','meeting_summary_update'],'knowledge':['knowledge_embedding','rerank'],'memory':['memory_embedding','memory_extraction','memory_consolidation','memory_reflection','rerank'],'tool':['approval_auto','image_generation']}
for kind,purposes in pairs.items():
 for purpose in purposes:
  x=copy.deepcopy(row);x.update(consumer_kind=kind,purpose=purpose,agent_id=uid if kind in ['agent','memory'] else None,execution_id=uid if kind=='agent' else None,meeting_id=uid if kind=='meeting' else None)
  if purpose in ['knowledge_embedding','memory_embedding']:x.update(protocol='openai-embeddings',model_type='embedding')
  elif purpose=='rerank':x.update(protocol='jina-rerank',model_type='reranker')
  elif purpose=='image_generation':x.update(protocol='openai-images-generations',model_type='image_generation')
  add(kind+'/'+purpose,'Invocation',x,True)
  for wrong in ['agent_generation','rerank','image_generation']:
   if wrong not in purposes:y=copy.deepcopy(x);y['purpose']=wrong;add(kind+'/wrong-purpose/'+wrong,'Invocation',y,False)
  if kind in ['agent','memory']:y=copy.deepcopy(x);y['agent_id']=None;add(kind+'/missing-agent/'+purpose,'Invocation',y,False)
  else:y=copy.deepcopy(x);y['agent_id']=uid;add(kind+'/unexpected-agent/'+purpose,'Invocation',y,False)
  if kind=='agent':y=copy.deepcopy(x);y['meeting_id']=uid;add('agent/optional-meeting/'+purpose,'Invocation',y,True)
  if kind=='tool':y=copy.deepcopy(x);y['execution_id']=uid;add('tool/optional-execution/'+purpose,'Invocation',y,True)
x=copy.deepcopy(row);x['protocol']='anthropic-messages';add('anthropic','Invocation',x,True)
for protocol in ['openai-chat-completions','anthropic-messages','openai-embeddings','jina-rerank','openai-images-generations']:
 x=copy.deepcopy(row);x.update(protocol=protocol,model_type='alien');add(protocol+'/wrong-type','Invocation',x,False)
for dispatch in ['reserved','authorized','sent','not_sent','unknown']:
 x=copy.deepcopy(row);x.update(dispatch=dispatch,dispatched_at=stamp if dispatch=='sent' else None);add('dispatch/'+dispatch,'Invocation',x,True)
 y=copy.deepcopy(x);y['dispatched_at']=None if dispatch=='sent' else stamp;add('dispatch/bad-time/'+dispatch,'Invocation',y,False)
 y=copy.deepcopy(x);y['final']={'status':'succeeded','finished_at':stamp,'error_category':None};add('final/succeeded/'+dispatch,'Invocation',y,dispatch=='sent')
 for status in ['failed','cancelled','unknown']:
  y=copy.deepcopy(x);y['final']={'status':status,'finished_at':stamp,'error_category':None};add('final/'+dispatch+'/'+status,'Invocation',y,True)
for category in ['invalid_request','authentication','permission','model_not_found','rate_limited','context_too_large','provider_unavailable','timeout','network','cancelled','content_filter','unsupported_feature','provider_error','unknown']:
 x=copy.deepcopy(row);x['final']={'status':'failed','finished_at':stamp,'error_category':category};add('error/'+category,'Invocation',x,True)
 x['dispatch']='sent';x['dispatched_at']=stamp;x['final']['status']='succeeded';add('succeeded/error/'+category,'Invocation',x,False)
for name in counts:
 u=copy.deepcopy(unknown);u[name]='0';add('unknown-known/'+name,'Usage',u,False);u['source']='provider';add('provider-zero/'+name,'Usage',u,True)
 u[name]='9223372036854775807';add('provider-max/'+name,'Usage',u,True)
u=copy.deepcopy(unknown);u['source']='provider';add('provider-all-null','Usage',u,False)
field={'sum':'0','known_count':'0','unknown_count':'0'}
summary={k:'0' for k in ['confirmed_invocations','dispatch_unknown','succeeded','failed','cancelled','unknown']};summary.update({k:copy.deepcopy(field) for k in ['input','output','total','cached_input','cache_write','reasoning']});summary['as_of']=stamp
for by in ['consumer','agent','model','provider','execution','meeting','purpose','day']:
 key={'by':by,'id':None if by=='day' else ('agent' if by=='consumer' else 'agent_generation' if by=='purpose' else uid),'day':'2026-10-07' if by=='day' else None};add('group/'+by,'GroupKey',key,True)
 key['id']=None;add('group/null/'+by,'GroupKey',key,by in ['day','agent','execution','meeting'])
 for bad in [dict(by=by,id=uid,day='2026-10-07'),dict(by=by,id='alien',day=None)]:add('group/bad/'+by,'GroupKey',bad,False)
for sumv,known,unk,valid in [('0','0','0',True),(None,'0','0',False),(None,'0','1',True),('0','0','1',False),('0','1','0',True),(None,'1','0',False),('9223372036854775807','1','0',True)]:add('field/'+str((sumv,known,unk)),'FieldSummary',dict(sum=sumv,known_count=known,unknown_count=unk),valid)
project=dict(id=uid,owner_user_id=uid,name='Project',normalized_name='project',description='line\n\ttab',lifecycle='active',version='9223372036854775807',current_sprint_id=None,created_at=stamp,updated_at=stamp,archived_at=None)
for state in ['active','archiving','archived','deleting']:
 for archived in [None,stamp]:
  x=copy.deepcopy(project);x.update(lifecycle=state,archived_at=archived);add('project/'+state+'/'+str(archived),'Project',x,(archived is not None if state=='archived' else archived is None if state in ['active','archiving'] else True))
objects={'Usage':unknown,'Final':{'status':'failed','finished_at':stamp,'error_category':None},'Invocation':row,'List':{'items':[row],'next_cursor':None},'FieldSummary':field,'Summary':summary,'GroupKey':{'by':'agent','id':None,'day':None},'Group':{'key':{'by':'agent','id':None,'day':None},'summary':summary},'Aggregate':{'items':[],'next_cursor':None,'as_of':stamp},'Project':project}
for name,v in objects.items():
 add('object/'+name,name,v,True)
 for k in v:
  x=copy.deepcopy(v);del x[k];add('required/'+name+'/'+k,name,x,False)
 x=copy.deepcopy(v);x['internal_secret']='never';add('closed/'+name,name,x,False)
for n in ['0','1','9007199254740993','9223372036854775806','9223372036854775807','9223372036854775808','9999999999999999999','18446744073709551615','01','+1','-1','1e2',1,None,'１２','1\n','1\r','1\u2028','1\u2029','1\x00']:
 for schema in ['NonnegativeInt64String','PositiveInt64String']:
  valid=isinstance(n,str) and n.isascii() and n.isdigit() and str(int(n))==n and (0 if schema.startswith('Nonnegative') else 1)<=int(n)<=9223372036854775807
  add('scalar/'+schema+'/'+repr(n),'common:'+schema,n,valid)
for schema,good,bad in [('Name',['😀'*128,'<>&\u2028\u2029','\n'],['😀'*129,'','a\x00']),('ProjectName',['A-._9','a'*64],['.','..','a'*65,'a/','a\\','a%','é']),('Username',['admin','AdMin','A--z','a'*32],['ab','a'*33,'-admin','admin-','a.b','ééé']),('ProviderRequestID',['_.:-09AZaz','x'*256],['x'*257,'x/y','x y','😀']),('common:Instant',[stamp],['2026-10-07T01:02:03Z','2026-10-07T01:02:03.1234567Z']),('common:InstantInput',['2026-10-07T01:02:03Z',stamp,'2026-10-07T01:02:03+02:00'],['2026-10-07T01:02:03.1234567Z'])]:
 for v in good:add('scalar/'+schema+'/'+repr(v[:20]),schema,v,True)
 for v in bad:add('scalar-bad/'+schema+'/'+repr(v[:20]),schema,v,False)
 # NUL/newline suffix rules apply to name/path/token/time; Name itself permits newline.
 if schema!='Name':
  for suffix in ['\n','\r','\u2028','\u2029','\x00']:add('scalar-suffix/'+schema+'/'+repr(suffix),schema,good[0]+suffix,False)
errors=[]
for c in cases:
 target=curl if c['schema'].startswith('common:') else url; name=c['schema'].split(':')[-1]
 schema={'$id':target,'$ref':'#/components/schemas/'+name}
 try:issues=list(Draft202012Validator(schema,registry=registry).iter_errors(c['value']));passed=not issues
 except Exception as e:errors.append({'label':c['label'],'exception':repr(e)});continue
 if passed!=c['valid']:errors.append({'label':c['label'],'schema':c['schema'],'wanted':c['valid'],'actual':passed,'errors':[x.message for x in issues]})
for document in [doc,common]:
 for schema in document['components']['schemas'].values():Draft202012Validator.check_schema(schema)
# JSON Schema's format annotation and UTF-8 byte extension are deliberately not custom-implemented.
(base/'independent-cases.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n')
result={'cases':len(cases),'failures':errors,'elapsed_seconds':time.monotonic()-started,'jsonschema':importlib.metadata.version('jsonschema'),'referencing':importlib.metadata.version('referencing'),'format_assertion':False,'custom_byte_extension_assertion':False}
(base/'schema-result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2));sys.exit(bool(errors))
