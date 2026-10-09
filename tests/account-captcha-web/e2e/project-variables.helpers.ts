import { expect, type Page, type Locator, type Request, type Response as PWResponse } from '@playwright/test';
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from 'node:fs';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { AccountFailure, uuid7 } from '../../../web/src/api/client';
import { createProjectVariablesAPI, captureProjectVariableCommand, type ProjectVariableCommand, type ProjectVariable } from '../../../web/src/api/project-variables';
export const directory=process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const repository=resolve(dirname(fileURLToPath(import.meta.url)),'../../..');
export type Credential={email:string;password:string;user_id:string;username:string};
export type Material={owner:Credential;admin:Credential;other:Credential;ids:Record<string,string>;projects:Record<string,{normalized_name:string;id:string}>;variables:Record<string,ProjectVariable>;targets:Record<string,string>};
export const material=():Material=>JSON.parse(readFileSync(join(directory,'project-variables-material.json'),'utf8'));
export const button=(page:Page|Locator,name:string)=>page.getByRole('button',{name,exact:true});
export const path=(data:Material,key='main')=>`/${data.owner.username}/${data.projects[key]!.normalized_name}/settings/variables`;
export const editor=(page:Page)=>page.locator('.variable-editor');
export const field=(page:Page,name:string)=>editor(page).getByRole('textbox',{name,exact:true});
export const history=(page:Page)=>page.getByRole('region',{name:'原操作与历史回执',exact:true});
export const table=(page:Page)=>page.getByRole('table',{name:'普通变量目录（摘要不含值）',exact:true});
export async function login(page:Page,who:Credential){
  await expect(page.locator('#login-email')).toBeVisible();
  try {await page.locator('#login-email').fill(who.email);await page.locator('#login-password').fill(who.password)} catch {throw new Error('PRIVATE_VARIABLE_LOGIN_INPUT_FAILED')}
  await button(page,'登录').click();await expect(button(page,'退出登录')).toBeEnabled();await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
export async function enter(page:Page,data:Material,key='main') {const response=await page.goto(path(data,key));expect(response?.status()).toBe(200);await login(page,data.owner);await ready(page)}
export async function ready(page:Page) {await expect(page.getByRole('heading',{name:'Variables',exact:true})).toBeVisible();await expect(button(page,'从第一页重新读取')).toBeEnabled();await expect(table(page)).toBeVisible()}
export async function select(page:Page,name='CUSTOM_VALUE') {await button(page,`查看变量 ${name}`).click();await expect(field(page,'值')).toBeVisible();await expect(button(page,'读取当前变量')).toBeEnabled()}
export async function endTracking(page:Page) {await button(page,'结束本地追踪').click();const dialog=page.getByRole('dialog',{name:'结束本地操作追踪？',exact:true});await expect(dialog).toBeVisible();await button(dialog,'结束追踪').click();await expect(history(page)).toHaveCount(0)}
export async function confirmed(page:Page) {await expect(history(page)).toContainText('原操作已确认');await expect(button(page,'查询原操作')).toBeEnabled()}
let sequence=0;
const acknowledgementFields={ 'arm-loss':[], 'hold-read':[], 'hold-status':['started','finished','canceled'],'release-read':[],'fail-session':[], update:['receipt'],archive:['fact_only'],observe:['commands','history','audits','events']} as const;
export async function ipc(action:keyof typeof acknowledgementFields,fields:Record<string,string>={}) {
 const next=++sequence,request=join(directory,'project-variables-ipc.json'),ack=join(directory,`project-variables-ack-${next}.json`);
 writeFileSync(request+'.tmp',JSON.stringify({sequence:next,action,project:'main',...fields}),{mode:0o600});renameSync(request+'.tmp',request);
 await expect.poll(()=>existsSync(ack),{timeout:4000,intervals:[20,40,100]}).toBe(true);
 const value=JSON.parse(readFileSync(ack,'utf8'));const keys=['sequence',...acknowledgementFields[action]];
 expect(value && typeof value==='object' && Object.keys(value).length===keys.length && keys.every(k=>Object.hasOwn(value,k)) && value.sequence===next).toBe(true);
 if(action==='hold-status') expect(['started','finished','canceled'].every(k=>typeof value[k]==='boolean')).toBe(true);
 if(action==='observe') expect(['commands','history','audits','events'].every(k=>Number.isSafeInteger(value[k])&&value[k]>=0)).toBe(true);
 if(action==='archive') expect(value.fact_only===true).toBe(true);
 return value as Record<string,any>;
}
export function complete(value:Record<string,unknown>){writeFileSync(join(directory,'project-variables-result.json'),JSON.stringify({completed:true,...value}),{mode:0o600})}
let step=0;
export function checkpoint(){step++;writeFileSync(join(directory,'project-variables-failure.json'),JSON.stringify({phase:process.env.AGENTEAM_PROJECT_VARIABLE_WEB_CASE,step}),{mode:0o600})}
export function protect(page:Page){
 let rejected=0, pageErrors=0;
 page.on('pageerror',()=>pageErrors++);
 return {async install(){await page.addInitScript(()=>{(window as any).__variableUnhandled=0;window.addEventListener('unhandledrejection',()=>{(window as any).__variableUnhandled++})})},async check(){rejected=await page.evaluate(()=>(window as any).__variableUnhandled);expect(pageErrors===0&&rejected===0).toBe(true)}}
}
type Entry={request:Request;method:string;url:URL;body:string|null;response?:PWResponse;failed:boolean;finished:boolean;expected?:'cut'|'cancel'};
type Declaration={method:string;project:string;target:string|null;consumed:boolean};
export function observe(page:Page){
 const entries:Entry[]=[],declarations:Declaration[]=[],tails:Promise<void>[]=[];let error=false;
 const add=(task:Promise<void>)=>{tails.push(task.catch(()=>{error=true}))};
 const selected=(request:Request)=>/^\/api\/v1\/projects\/[^/]+\/variables(?:\/|$)/.test(new URL(request.url()).pathname);
 page.on('request',request=>{
  if(!selected(request))return;
  const e:Entry={request,method:request.method(),url:new URL(request.url()),body:request.postData(),failed:false,finished:false};
  const d=declarations.find(d=>!d.consumed&&e.method===d.method&&e.url.pathname.startsWith(`/api/v1/projects/${d.project}/variables`)&&!e.url.pathname.endsWith('/commands/lookup'));
  if(d){
   const target=e.method==='POST'?JSON.parse(e.body??'{}').request?.variable_id:e.url.pathname.split('/').at(-1);
   if(!uuid7.test(target??'')||(d.target!==null&&target!==d.target)){error=true}else {d.consumed=true;e.expected='cut'}
  }
  entries.push(e);
 });
 page.on('response',response=>{const e=entries.find(e=>e.request===response.request());if(e){if(e.response)error=true;e.response=response}});
 page.on('requestfailed',request=>{const e=entries.find(e=>e.request===request);if(e){if(e.failed||e.finished||!e.expected)error=true;e.failed=true}});
 page.on('requestfinished',request=>{const e=entries.find(e=>e.request===request);if(e){if(e.failed||e.finished||e.expected)error=true;e.finished=true;add((async()=>{expect(e.response!==undefined && await e.response.finished()===null).toBe(true)})())}});
 const cut=(method:string,project:string,target:string|null)=>{if(declarations.length>=3)throw new Error('VARIABLE_CUT_BOUND');declarations.push({method,project,target,consumed:false})};
 const cancel=(project:string,target:string)=>{const active=entries.filter(e=>e.method==='GET'&&e.url.pathname===`/api/v1/projects/${project}/variables/${target}`&&!e.finished&&!e.failed);expect(active.length===1&&!active[0]!.expected).toBe(true);active[0]!.expected='cancel'};
 return {entries,cut,cancel,async finish(){
  await expect.poll(()=>entries.every(e=>e.finished||e.failed),{timeout:4000}).toBe(true);await Promise.all(tails);
  expect(!error&&declarations.every(d=>d.consumed)&&entries.length>0).toBe(true);
  for(const e of entries)expect(e.expected?e.failed&&!e.finished:e.finished&&!e.failed).toBe(true);
  await validateOriginalBodies(entries);
  return {requests:entries.length,completed:entries.filter(e=>e.finished).length,expected_incomplete:entries.filter(e=>e.failed).length};
 }};
}
function command(e:Entry):ProjectVariableCommand {
 const body=JSON.parse(e.body??'{}'),projectID=e.url.pathname.split('/')[4]!;
 const kind=e.url.pathname.endsWith('/commands/lookup')?body.command.replace('project.variable.',''):e.method==='POST'?'create':e.method==='PATCH'?'update':'delete';
 return captureProjectVariableCommand({kind,projectID,...(kind==='create'?{request:body.request}:{targetID:body.target_id??e.url.pathname.split('/').at(-1),expectedVersion:body.expected_version,...(kind==='update'?{request:body.request}:{})})});
}
const schemaProgram=String.raw`
import sys,json,pathlib,base64
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path(sys.argv[1])/'api/openapi'
registry=Registry()
for path in base.glob('*.json'):
 registry=registry.with_resource(path.as_uri(),Resource.from_contents(json.loads(path.read_text()),default_specification=DRAFT202012))
doc=json.loads((base/'project-variables.json').read_text()); count=0
for item in json.loads(pathlib.Path(sys.argv[2]).read_text()):
 schema={'$id':(base/'project-variables.json').as_uri(),'$ref':item['schema']}
 Draft202012Validator(schema,registry=registry).validate(json.loads(base64.b64decode(item['raw'])))
 count+=1
print(count)
`;
async function validateOriginalBodies(entries:Entry[]){
 const records=readdirSync(directory).filter(n=>/^project-variables-response-\d+\.json$/.test(n)).map(n=>JSON.parse(readFileSync(join(directory,n),'utf8')));
 const vectors:{schema:string;raw:string}[]=[];let success=0;
 for(const e of entries){
  if(e.expected==='cancel')continue;
  expect(e.response!==undefined).toBe(true);const response=e.response!;
  const requestID=await response.headerValue('x-request-id');expect(uuid7.test(requestID??'')).toBe(true);
  const matches=records.filter(r=>r.request_id===requestID);expect(matches.length===1).toBe(true);const record=matches[0];
  expect(record.method===e.method&&record.path===e.url.pathname&&record.query===e.url.search.slice(1)&&record.status===response.status()&&record.content_type===await response.headerValue('content-type')).toBe(true);
  expect(Buffer.from(record.request_b64,'base64').toString('utf8')===(e.body??'')).toBe(true);
  const raw=Buffer.from(record.body_b64,'base64');
  if(!e.expected)expect((await response.body()).equals(raw)).toBe(true);
  const c=e.method==='GET'?null:command(e),isLookup=e.url.pathname.endsWith('/commands/lookup');
  const api=createProjectVariablesAPI(async()=>new Response(new Uint8Array(raw).buffer,{status:record.status,headers:{'Content-Type':record.content_type,'X-Request-ID':requestID!}}));
  const project=e.url.pathname.split('/')[4]!,signal=new AbortController().signal;
  let schema='./common.json#/components/schemas/Problem';
  if(record.status===200){
   schema='#/components/schemas/'+(e.method==='GET'?(e.url.pathname.split('/').length===6?'VariableSummaryPage':'Variable'):isLookup?'VariableCommandLookup':'VariableMutation');
  }
  // Validate the exact complete upstream bytes, then the public decoder with
  // the original captured request. A cut is not declared browser success.
  vectors.push({schema,raw:record.body_b64});
  try{
   if(e.method==='GET'){
    if(e.url.pathname.split('/').length===6)await api.list(project,{limit:Number(e.url.searchParams.get('limit')??'50'),...(e.url.searchParams.has('cursor')?{cursor:e.url.searchParams.get('cursor')!}:{})},signal);
    else await api.get(project,e.url.pathname.split('/').at(-1)!,signal);
   }else if(isLookup)await api.lookup(c!,'private-csrf','private-key',signal);
   else if(c!.kind==='create')await api.create(project,c!.request as any,'private-csrf','private-key',signal);
   else if(c!.kind==='update')await api.update(project,c!.targetID,c!.expectedVersion,c!.request,'private-csrf','private-key',signal);
   else if(c!.kind==='delete')await api.delete(project,c!.targetID,c!.expectedVersion,'private-csrf','private-key',signal);
   expect(record.status===200).toBe(true);success++;
  }catch(error){expect(record.status!==200&&error instanceof AccountFailure&&error.kind==='problem').toBe(true)}
 }
 expect(success>0).toBe(true);
 const file=join(directory,'variable-schema-vectors.json');writeFileSync(file,JSON.stringify(vectors),{mode:0o600});
 const checked=spawnSync('/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3',['-c',schemaProgram,repository,file],{encoding:'utf8',timeout:6000,maxBuffer:4096});
 expect(checked.status===0&&Number(checked.stdout.trim())===vectors.length).toBe(true);
}
