import json
from pathlib import Path
root=Path('/workspace/agenteam/api/openapi');old=json.loads((root/'project-models.json').read_text());common='./common.json#/components/schemas/'
ref=lambda s:{'$ref':'#/components/schemas/'+s}
obj=lambda props:{'type':'object','additionalProperties':False,'required':list(props),'properties':props}
schemas={
 'ExpectedVersion':{'allOf':[{'$ref':common+'PositiveInt64String'},{'not':{'const':'9223372036854775807'}}],'description':'Canonical decimal string, 1..9223372036854775806. The service must be able to increment exactly once.'},
 'Material':{'type':'string','minLength':1,'maxLength':65536,'writeOnly':True,'description':'Decoded UTF-8 byte length 1..65536, enforced in Go independently of Unicode character maxLength. No trimming or normalization; never returned. Raw create/update request <=409600 bytes including JSON shell and escapes.'},
 'Create':obj({'value':ref('Material')}),
 'Update':obj({'expected_version':ref('ExpectedVersion'),'value':ref('Material')}),
 'Delete':obj({'expected_version':ref('ExpectedVersion')}),
 'Lookup':{'oneOf':[obj({'kind':{'const':'create'}}),obj({'kind':{'enum':['update','delete']},'credential_id':{'$ref':common+'ID'},'expected_version':ref('ExpectedVersion')})]},
 'Metadata':obj({'credential_id':{'$ref':common+'ID'},'purpose':{'const':'model'},'version':{'$ref':common+'PositiveInt64String'}}),
 'Mutation':obj({'credential_id':{'$ref':common+'ID'},'purpose':{'const':'model'},'version':{'$ref':common+'PositiveInt64String'},'deleted':{'type':'boolean'}}),
 'Observation':{'oneOf':[obj({'observed':{'const':False},'result':{'type':'null'}}),obj({'observed':{'const':True},'result':ref('Mutation')})]}
}
headers={k:v for k,v in old['paths'][next(iter(old['paths']))]['get']['responses']['200']['headers'].items()}
headers['Content-Length']={'schema':{'type':'integer','minimum':0,'maximum':1024},'description':'Exact fully encoded safe representation length. HEAD runs the same read/validation/encoding/tail but sends no body.'}
paths={}
base='/api/v1/projects/{project_id}/'
for path,method,request,response in [(base+'model-credentials','post','Create','Mutation'),(base+'model-credentials/{credential_id}','put','Update','Mutation'),(base+'model-credentials/{credential_id}','delete','Delete','Mutation'),(base+'model-credentials/{credential_id}','get',None,'Metadata'),(base+'model-credentials/{credential_id}','head',None,'Metadata'),(base+'model-credential-commands/lookup','post','Lookup','Observation')]:
 unsafe=method in ['post','put','delete'];budget=2 if request=='Lookup' or not unsafe else 30
 params=[{'in':'path','name':'project_id','required':True,'schema':{'$ref':common+'ID'}}]
 if '{credential_id}' in path:params.append({'in':'path','name':'credential_id','required':True,'schema':{'$ref':common+'ID'}})
 if unsafe:params.append({'in':'header','name':'Idempotency-Key','required':True,'schema':{'$ref':common+'IdempotencyKey'},'description':'Exactly one original key; never changed by the adapter or placed in a URL.'})
 op={'operationId':method+'_project_credential_'+(request or response).lower(),'security':[{'Session':[],**({'CSRF':[]} if unsafe else {})},{'LocalSession':[],**({'CSRF':[]} if unsafe else {})}],
 'description':f'Current Human Project Owner only; no administrator exemption. No query parameters. Strict UTF-8 JSON/presence/unknown/duplicate/case-alias/trailing-value checks. One {budget}s publication/I/O budget starts before authentication; actual Body.Close/callback/transaction retirement remains synchronously owned after cancellation. No late Problem or success. Project passive Read lookup does not authorize mutations: all mutation responses come from exactly one original ExecuteWrite with current Mutate, including committed replays. Unknown is returned without automatic confirmation. No material/nonce/digest or credential value appears in responses.',
 'parameters':params,'responses':{'200':{'description':'Complete safe representation <=1024 bytes; HEAD has no body. Historical lookup result is distinct from current metadata.','headers':headers},'default':{'description':'Existing safe Problem; HEAD has no body. No cause_id wire field/header.','content':{'application/problem+json':{'schema':{'$ref':common+'Problem'}}}}}}
 if method!='head':op['responses']['200']['content']={'application/json':{'schema':ref(response)}}
 if request:op['requestBody']={'required':True,'description':('Raw JSON body <=409600 bytes; decoded material <=65536 bytes.' if request in ['Create','Update'] else 'Raw JSON body <=1024 bytes.'),'content':{'application/json':{'schema':ref(request)}}}
 paths.setdefault(path,{})[method]=op
out={'openapi':'3.1.0','info':{'title':'Project Owner Model Credentials','version':'1.0.0'},'paths':paths,'components':{'securitySchemes':old['components']['securitySchemes'],'schemas':schemas}}
# Reuse the actual common key name rather than adding a second key contract.
common_doc=json.loads((root/'common.json').read_text());assert 'IdempotencyKey' in common_doc['components']['schemas']
(root/'project-model-credentials.json').write_text(json.dumps(out,indent=2)+'\n')
