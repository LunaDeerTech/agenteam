from pathlib import Path
import json, copy
root=Path('/workspace/agenteam')
schemas={}
def ref(name): return {'$ref':'#/components/schemas/'+name}
def common(name): return {'$ref':'./common.json#/components/schemas/'+name}
def nullable(s): return {'anyOf':[s,{'type':'null'}]}
def obj(props,**kw): return {'type':'object','additionalProperties':False,'required':list(props),'properties':props,**kw}
def enum(v): return {'type':'string','enum':v.split()}
def cond(key,value,props): return {'if':{'properties':{key:{'const':value}},'required':[key]},'then':{'properties':props}}
def exact(pattern): return {'type':'string','pattern':pattern+'(?![\\s\\S])'}
schemas['ConsumerKind']=enum('agent meeting knowledge memory tool')
schemas['Purpose']=enum('agent_generation agent_compaction approval_auto meeting_summary_initial meeting_summary_update knowledge_embedding memory_embedding memory_extraction memory_consolidation memory_reflection rerank image_generation')
schemas['Protocol']=enum('openai-chat-completions anthropic-messages openai-embeddings jina-rerank openai-images-generations')
schemas['ModelType']=enum('chat embedding reranker image_generation')
schemas['Dispatch']=enum('reserved authorized sent not_sent unknown')
schemas['FinalStatus']=enum('succeeded failed cancelled unknown')
schemas['ErrorCategory']=enum('invalid_request authentication permission model_not_found rate_limited context_too_large provider_unavailable timeout network cancelled content_filter unsupported_feature provider_error unknown')
schemas['GroupBy']=enum('consumer agent model provider execution meeting purpose day')
schemas['Cursor']={'type':'string','minLength':1,'maxLength':8192,'not':{'pattern':'\u0000'},'description':'Opaque signed cursor, at most 8192 decoded UTF-8 bytes. Binding remains current stable Actor, Project, filters and ordering; limit, SessionID and names do not join the binding. Invalid tokens, scope/identity mismatches and overlong decoded tokens use CURSOR_INVALID. Never log or display the token.','x-maxUTF8Bytes':8192}
schemas['Name']={'type':'string','minLength':1,'maxLength':128,'not':{'pattern':'\u0000'}}
schemas['ProviderModelID']={'type':'string','minLength':1,'maxLength':256,'not':{'pattern':'\u0000'},'x-maxUTF8Bytes':256,'description':'Frozen provider-native model identifier, at most 256 UTF-8 bytes; server validates the byte bound.'}
schemas['ProviderRequestID']={**exact('^[A-Za-z0-9_.:-]{1,256}'),'maxLength':256}
schemas['ProjectName']={**exact('^[A-Za-z0-9._-]{1,64}'),'not':{'enum':['.','..']},'maxLength':64}
schemas['NormalizedProjectName']={**exact('^[a-z0-9._-]{1,64}'),'not':{'enum':['.','..']},'maxLength':64}
schemas['Username']={**exact('^[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]'),'minLength':3,'maxLength':32,'description':'Current account route spelling. Existing admin is accepted; no creation-time reserved-name rules or second decoding.'}
schemas['Usage']=obj({k:nullable(common('NonnegativeInt64String')) for k in ['input_tokens','output_tokens','total_tokens','cached_input_tokens','cache_write_tokens','reasoning_tokens']}|{'source':enum('provider unknown')})
usageFields=list(schemas['Usage']['properties'])[:-1]
schemas['Usage']['allOf']=[cond('source','unknown',{k:{'type':'null'} for k in usageFields}),{'if':{'properties':{'source':{'const':'provider'}}},'then':{'anyOf':[{'properties':{k:common('NonnegativeInt64String')}} for k in usageFields]}}]
schemas['Final']=obj({'status':ref('FinalStatus'),'finished_at':common('Instant'),'error_category':nullable(ref('ErrorCategory'))},allOf=[cond('status','succeeded',{'error_category':{'type':'null'}})])
p={'id':common('ID'),'call_id':common('ID'),'attempt_index':common('Sequence'),'project_id':common('ID'),'consumer_kind':ref('ConsumerKind'),'purpose':ref('Purpose'),'agent_id':nullable(common('ID')),'execution_id':nullable(common('ID')),'meeting_id':nullable(common('ID')),'provider_id':common('ID'),'model_id':common('ID'),'provider_name':ref('Name'),'model_name':ref('Name'),'provider_model_id':ref('ProviderModelID'),'protocol':ref('Protocol'),'model_type':ref('ModelType'),'live_provider_id':nullable(common('ID')),'live_model_id':nullable(common('ID')),'dispatch':ref('Dispatch'),'started_at':common('Instant'),'dispatched_at':nullable(common('Instant')),'final':nullable(ref('Final')),'usage':ref('Usage'),'provider_request_id':nullable(ref('ProviderRequestID'))}
conditions=[]
for protocol,model in [('openai-chat-completions','chat'),('anthropic-messages','chat'),('openai-embeddings','embedding'),('jina-rerank','reranker'),('openai-images-generations','image_generation')]: conditions.append(cond('protocol',protocol,{'model_type':{'const':model}}))
for purpose in schemas['Purpose']['enum']:
    model='embedding' if purpose in ['knowledge_embedding','memory_embedding'] else 'reranker' if purpose=='rerank' else 'image_generation' if purpose=='image_generation' else 'chat'
    conditions.append(cond('purpose',purpose,{'model_type':{'const':model}}))
null={'type':'null'}
for kind,purposes,related in [('agent','agent_generation agent_compaction',{'agent_id':common('ID'),'execution_id':common('ID')}),('meeting','meeting_summary_initial meeting_summary_update',{'agent_id':null,'execution_id':null,'meeting_id':common('ID')}),('knowledge','knowledge_embedding rerank',{'agent_id':null,'execution_id':null,'meeting_id':null}),('memory','memory_embedding memory_extraction memory_consolidation memory_reflection rerank',{'agent_id':common('ID'),'execution_id':null,'meeting_id':null}),('tool','approval_auto image_generation',{'agent_id':null,'meeting_id':null})]:
    conditions.append(cond('consumer_kind',kind,{'purpose':enum(purposes),**related}))
conditions.append({'if':{'properties':{'dispatch':{'const':'sent'}}},'then':{'properties':{'dispatched_at':common('Instant')}},'else':{'properties':{'dispatched_at':null,'usage':{'properties':{'source':{'const':'unknown'}}},'final':{'not':{'type':'object','properties':{'status':{'const':'succeeded'}},'required':['status']}}}}})
schemas['Invocation']=obj(p,allOf=conditions,description='Safe frozen invocation projection. The server first validates every internal field, including fields omitted here, then requires the exact requested ProjectID. Non-null live references must equal frozen IDs. Started/dispatched/final times retain the validated chronological ordering. No prompts, endpoint, credentials, profile, revision, process, fence, snapshot, operation or internal error code/cause are exposed.')
schemas['List']=obj({'items':{'type':'array','maxItems':100,'items':ref('Invocation')},'next_cursor':nullable(ref('Cursor'))})
schemas['FieldSummary']=obj({'sum':nullable(common('NonnegativeInt64String')),'known_count':common('NonnegativeInt64String'),'unknown_count':common('NonnegativeInt64String')},allOf=[{'if':{'properties':{'known_count':{'const':'0'},'unknown_count':{'const':'0'}}},'then':{'properties':{'sum':{'const':'0'}}}},{'if':{'properties':{'known_count':{'const':'0'},'unknown_count':{'not':{'const':'0'}}}},'then':{'properties':{'sum':null}}},{'if':{'properties':{'known_count':{'not':{'const':'0'}}}},'then':{'properties':{'sum':common('NonnegativeInt64String')}}}])
schemas['Summary']=obj({k:common('NonnegativeInt64String') for k in ['confirmed_invocations','dispatch_unknown','succeeded','failed','cancelled','unknown']}|{k:ref('FieldSummary') for k in ['input','output','total','cached_input','cache_write','reasoning']}|{'as_of':common('Instant')},description='Original validated aggregate, never inferred from a page. Each known_count + unknown_count equals confirmed_invocations; summed statuses do not exceed confirmed_invocations + dispatch_unknown, and additions must fit Int64. Row as_of equals page as_of; all six token fields retain their original denominators.')
schemas['Day']={**exact('^[0-9]{4}-[0-9]{2}-[0-9]{2}'),'format':'date','minLength':10,'maxLength':10,'description':'An actual calendar day under the original GroupKey contract.'}
schemas['GroupKey']=obj({'by':ref('GroupBy'),'id':nullable({'type':'string'}),'day':nullable(ref('Day'))},allOf=[cond('by','day',{'id':null,'day':ref('Day')})]+[cond('by',by,{'day':null,'id':ref('ConsumerKind') if by=='consumer' else ref('Purpose') if by=='purpose' else nullable(common('ID')) if by in ['agent','execution','meeting'] else common('ID')}) for by in ['consumer','agent','model','provider','execution','meeting','purpose']])
schemas['Group']=obj({'key':ref('GroupKey'),'summary':ref('Summary')})
schemas['Aggregate']=obj({'items':{'type':'array','maxItems':100,'items':ref('Group')},'next_cursor':nullable(ref('Cursor')),'as_of':common('Instant')})
schemas['Project']=obj({'id':common('ID'),'owner_user_id':common('ID'),'name':ref('ProjectName'),'normalized_name':ref('NormalizedProjectName'),'description':{'type':'string','maxLength':8192,'x-maxUTF8Bytes':8192,'not':{'pattern':'[\u0000-\u0008\u000b-\u001f\u007f]'},'description':'At most 8192 UTF-8 bytes; server validates this byte bound. Tabs and newlines are allowed.'},'lifecycle':enum('active archiving archived deleting'),'version':common('Version'),'current_sprint_id':nullable(common('ID')),'created_at':common('Instant'),'updated_at':common('Instant'),'archived_at':nullable(common('Instant'))},allOf=[cond('lifecycle','archived',{'archived_at':common('Instant')}),cond('lifecycle','active',{'archived_at':null}),cond('lifecycle','archiving',{'archived_at':null})],description='Current ProjectRef, with full typed validation and owner_user_id equal to the authenticated Human. Names normalize under the formal Project contract; updated_at is at least created_at and archived_at, when present, lies between them. This independent resolve is not authority for a later stable-ID read.')
params={}
def param(name,s,required=False,description=None):
    x={'name':name,'in':'query','required':required,'schema':s,'allowEmptyValue':False,'style':'form','explode':False}
    if description:x['description']=description
    return x
for name,typ in [('consumer_kind','ConsumerKind'),('purpose','Purpose'),('status','FinalStatus'),('cursor','Cursor')]:params[name]=param(name,ref(typ))
for name in ['agent_id','execution_id','meeting_id','provider_id','model_id']:params[name]=param(name,common('ID'))
for name in ['from','to']:params[name]=param(name,common('InstantInput'),description='Both from/to are supplied or both omitted, with from < to. Omitted dates use the original 30-day window; a cursor retains the original window.')
params['limit']=param('limit',{**exact('^(?:[1-9]|[1-9][0-9]|100)'),'default':'50'},description='Unsigned canonical decimal 1–100; no sign, whitespace, leading zero or exponent.')
params['group_by']=param('group_by',ref('GroupBy'),True)
params['username']=param('username',ref('Username'),True)
params['project_name']=param('project_name',ref('ProjectName'),True)
params['id']={'name':'id','in':'path','required':True,'schema':common('ID')}
headers={'Cache-Control':{'schema':{'const':'no-store'}},'X-Request-ID':{'schema':common('ID')},'Content-Length':{'schema':{'type':'integer','minimum':0},'description':'Length of the complete GET representation; HEAD performs the same read, validation and encoding without a body.'}}
paths={}
for path,typ,keys,operation in [('/api/v1/projects/resolve','Project',['username','project_name'],'resolveProject'),('/api/v1/projects/{id}/model-usage','List',['id','consumer_kind','agent_id','execution_id','meeting_id','purpose','provider_id','model_id','status','from','to','limit','cursor'],'listProjectModelUsage'),('/api/v1/projects/{id}/model-usage/summary','Aggregate',['id','consumer_kind','agent_id','execution_id','meeting_id','purpose','provider_id','model_id','status','from','to','limit','cursor','group_by'],'aggregateProjectModelUsage')]:
    methods={}
    for method in ['get','head']:
        responses={}
        for status in ['200','400','401','403','404','405','409','410','422','429','500','503']:
            response={'description':'Complete current-owner read' if status=='200' else 'Safe shared Problem; no candidate result on failure or Unknown','headers':copy.deepcopy(headers)}
            if status=='405':response['headers']['Allow']={'schema':{'const':'GET, HEAD'}}
            if method=='get':response['content']={('application/json' if status=='200' else 'application/problem+json'):{'schema':ref(typ) if status=='200' else common('Problem')}}
            responses[status]=response
        methods[method]={'operationId':operation+('Head' if method=='head' else ''),'parameters':[copy.deepcopy(params[k]) for k in keys],'security':[{'browserSession':[]}],'responses':responses}
    paths[path]=methods
spec={'openapi':'3.1.0','info':{'title':'Project Owner Usage read-only HTTP','version':'0.1.0','description':'Three GET/HEAD resources under current browser Session and Owner authorization, with no administrator cross-Owner exemption. Each stable-ID read authorizes again in its own transaction. Resolve uses current names; old names return 404 without aliases or redirects and name reuse does not restore identity. Strict query: at most 32768 raw bytes before parsing; only documented keys, one nonempty value each, decoded once; reject duplicate decoded keys, malformed escapes/UTF-8/NUL, semicolons, empty segments and bare ?. No request entity, uncertain/nonzero length or Transfer-Encoding. Host/Origin/Fetch Metadata/path checks keep the original Account priority. Exact resources allow only GET/HEAD. The two-second total budget starts before authentication and includes Body.Close, transaction completion, complete default-escaped JSON (at most 1 MiB), write/flush and joined cancellation callbacks. Earlier parent deadlines win. HEAD repeats the full read and encoding and emits no success or Problem body. Safe Problem instance is /api/v1. Unknown retains its original Fault/CommitState; this API offers no receipt or lookup. Only formally persisted Project/Usage records are readable; production Invocations remains nil. No project initialization/lifecycle, Runtime Facts producer, UI or Execution Summary selection is provided.'},'paths':paths,'components':{'securitySchemes':{'browserSession':{'type':'apiKey','in':'cookie','name':'__Host-agenteam_session','description':'Original Account session cookie; configured loopback HTTP uses agenteam_local_session. No bearer or caller-supplied Actor.'}},'schemas':schemas}}
(root/'api/openapi/project-usage.json').write_text(json.dumps(spec,indent=2,ensure_ascii=True)+'\n')
print('wrote',len(schemas),'schemas and',sum(len(x) for x in paths.values()),'operations')
