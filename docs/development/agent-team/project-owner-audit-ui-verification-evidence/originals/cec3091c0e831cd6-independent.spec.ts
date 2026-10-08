import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { setImmediate as turn } from 'node:timers/promises'
import { AccountFailure, type Fetch } from './src/api/client'
import { createAccountAPI } from './src/api/account'
import { createSystemAccountAPI } from './src/api/system-account'
import { createSystemInvitationAPI } from './src/api/system-invitations'
import { createSystemProviderAPI } from './src/api/system-providers'
import { createSystemModelAPI } from './src/api/system-models'
import { createSystemModelSelectionAPI } from './src/api/system-model-selection'
import { createSystemAccountSecurityAPI } from './src/api/system-account-security'
import { createSystemSMTPSettingsAPI } from './src/api/system-smtp-settings'
import { createSystemSMTPDeliveryAPI } from './src/api/system-smtp-delivery'
import { createSystemOutboundPolicyAPI } from './src/api/system-outbound-policy'
import { createSystemAuditAPI } from './src/api/system-audit'
import { createSystemRuntimeInformationAPI } from './src/api/system-runtime-information'
import { createProjectOwnerAPI } from './src/api/project-owner'
import { createProjectAuditAPI } from './src/api/project-audit'
import { createSessionController } from './src/composables/useSession'
import { createProjectWorkspace } from './src/composables/useProjectWorkspace'
import { useProjectAudit } from './src/composables/useProjectAudit'
import { projectRoute, safeReturnTarget } from './src/router/auth'

const id=(n:number)=>`01970000-0000-7000-8000-${n.toString(16).padStart(12,'0')}`
const projectID=id(10),auditID=id(80),time='2026-10-08T12:34:56.123456Z'
function session(role:'user'|'admin'='user',sid=id(2),csrf='S'.repeat(43)) {return {user:{id:id(1),email:'reader@example.test',username:'reader',display_name:'Reader',role,theme:'system',version:'1',initial_password_suggestion:false},session:{id:sid,issued_at:time,idle_expires_at:time,absolute_expires_at:time},csrf_token:csrf}}
function project(n=10,lifecycle='active'){return {id:id(n),owner_user_id:id(1),name:n===10?'Original':'Different',normalized_name:n===10?'original':'different',description:'Original description',lifecycle,version:'1',current_sprint_id:null,created_at:time,updated_at:time,archived_at:lifecycle==='archived'?time:null}}
function row(pid=projectID){return {audit_id:auditID,created_at:time,scope:'project',project_id:pid,actor:{kind:'human',id:id(1)},action:'secret.delete',outcome:'success',resource:{kind:'secret',id:id(30)},metadata:{version:'1'},associations:{},summary:'Secret deleted'}}
const body=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':status<400?'application/json':'application/problem+json','X-Request-ID':id(90)}})
const problem=(code:string,status=503,commit='not_started')=>body({type:'urn:agenteam:problem:test',title:'Failure',status,code,detail:'',instance:`/api/v1/projects/${projectID}/audit`,request_id:id(90),commit_state:commit,retry_hint:'lookup'},status)
function gate<T=void>(){let resolve!:(v:T)=>void,reject!:(v:unknown)=>void;const promise=new Promise<T>((r,j)=>{resolve=r;reject=j});return{promise,resolve,reject}}
const capture=(p:Promise<unknown>)=>p.then(value=>({value}),error=>({error}))
const owned:ReturnType<typeof createSessionController>[]=[], workspaces:ReturnType<typeof createProjectWorkspace>[]=[], pages:ReturnType<typeof useProjectAudit>[]=[], releases:(()=>void)[]=[]
const nativeFetch=globalThis.fetch
beforeAll(()=>{globalThis.fetch=async()=>{throw new Error('Independent state probe forbids real fetch')}})
afterAll(()=>{globalThis.fetch=nativeFetch})
afterEach(async()=>{for(const p of pages.splice(0))p.dispose();for(const w of workspaces.splice(0))w.dispose();for(const a of owned)a.leave();for(const r of releases.splice(0))r();await turn();await turn();for(const a of owned.splice(0))expect(a.state.busy).toBe(false)})
async function fixture(role:'user'|'admin'='user',bind=true){
 let view=session(role),value=project()
 let audit:Fetch=async path=>{const pid=path.split('/')[4]!;return body(path.split('?')[0]!.split('/').length===7?row(pid):{items:[row(pid)],next_cursor:null})}
 let account:Fetch=async()=>body(view)
 let owner:Fetch=async(_path,init)=>init.method==='GET'?body(value):problem('COMMIT_UNKNOWN',503,'unknown')
 let other:Fetch=async(path)=>path==='/api/v1/me'?body({user:view.user,avatar:null}):body({items:[],next_cursor:null})
 const calls:{path:string;init:RequestInit}[]=[]
 const fetch:Fetch=async(path,init)=>{calls.push({path,init});if(path==='/api/v1/session')return account(path,init);if(/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path))return audit(path,init);if(path.startsWith('/api/v1/projects/'))return owner(path,init);return other(path,init)}
 const auth=createSessionController(createAccountAPI(fetch),createSystemAccountAPI(fetch),createSystemInvitationAPI(fetch),createSystemProviderAPI(fetch),createSystemModelAPI(fetch),createSystemModelSelectionAPI(fetch),createSystemAccountSecurityAPI(fetch),createSystemSMTPSettingsAPI(fetch),createSystemSMTPDeliveryAPI(fetch),createSystemOutboundPolicyAPI(fetch),createSystemAuditAPI(fetch),createSystemRuntimeInformationAPI(fetch),createProjectOwnerAPI(fetch),createProjectAuditAPI(fetch));owned.push(auth);await auth.restore()
 const w=createProjectWorkspace(auth);workspaces.push(w)
 if(bind){w.afterNavigation('/reader/original/settings/audit');await turn();expect(w.currentReadContext.value?.projectID).toBe(projectID)}
 return {auth,w,calls,page(){const p=useProjectAudit(auth,w);pages.push(p);return p},setAudit(f:Fetch){audit=f},setOwner(f:Fetch){owner=f},setAccount(f:Fetch){account=f},setOther(f:Fetch){other=f},setProject(v:ReturnType<typeof project>){value=v},setView(v:ReturnType<typeof session>){view=v},reads(){return calls.filter(x=>/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(x.path))},writes(){return calls.filter(x=>x.init.method!=='GET')}}
}
function heldNative(media='application/json',payload:unknown={items:[row()],next_cursor:null}){
 const entered=gate(),tail=gate();let ended=false
 const stream=new ReadableStream<Uint8Array>({start(c){c.enqueue(new TextEncoder().encode(JSON.stringify(payload)))},cancel(){entered.resolve();return tail.promise}})
 const release=(reject=false)=>{if(ended)return;ended=true;if(reject)tail.reject(new Error('controlled rejected cancellation tail'));else tail.resolve()};releases.push(()=>release())
 return {stream,entered,response:()=>new Response(stream,{headers:{'Content-Type':media}}),release}
}

describe('independent domain isolation and private intent',()=>{
 it.each([[403,'FORBIDDEN'],[404,'NOT_FOUND'],[503,'COMMIT_UNKNOWN']] as const)('preserves exact Owner intent through %s/%s without read recovery writes',async(status,code)=>{
  const f=await fixture();await capture(f.auth.projects.startUpdate(projectID,{expected_version:'1',description:'Keep this exact original intent'}));expect(f.auth.projects.progress?.phase).toBe('uncertain');const original=f.writes()[0]!.init
  f.setAudit(async()=>problem(code,status,code==='COMMIT_UNKNOWN'?'unknown':'not_started'));const p=f.page();await turn();expect(['error','unavailable']).toContain(p.state.list.phase);expect(f.auth.state.phase).toBe('authenticated');expect(f.auth.system.denied).toBe(false);expect(f.auth.state.user?.role).toBe('user');expect(f.writes()).toHaveLength(1)
  f.auth.projectAudit.abandon();p.dispose();expect(f.auth.projects.progress?.phase).toBe('uncertain');await capture(f.auth.projects.retryOriginal());const replay=f.writes()[1]!.init;expect(replay.body).toBe(original.body);expect(new Headers(replay.headers).get('Idempotency-Key')).toBe(new Headers(original.headers).get('Idempotency-Key'));expect(f.calls.some(x=>x.path.includes('/lookup'))).toBe(false)
 })
 it.each(['UNAUTHENTICATED','SESSION_REVOKED'])('current 401 %s clears Audit authority synchronously at failure publication',async code=>{
  const f=await fixture();const p=f.page();await turn();p.updateFilter('resource_id',id(30));await p.openDetail(auditID);f.setAudit(async()=>problem(code,401));await p.retryDetail();expect(f.auth.state.phase).toBe('unavailable');expect(f.auth.personalContext.identity).toBeNull();expect(f.w.currentReadContext.value).toBeNull();expect(p.state.list.items).toEqual([]);expect(p.state.detail.record).toBeNull();expect(p.draft.resource_id).toBe('')
 })
 it.each(['late-success','late-401'])('retired actual fetch %s cannot publish or invalidate the same identity',async mode=>{
  const f=await fixture(),pendingResponse=gate<Response>();releases.push(()=>pendingResponse.resolve(problem('SESSION_REVOKED',401)));f.setAudit(()=>pendingResponse.promise)
  const pending=capture(f.auth.projectAudit.list(projectID,{}));await turn();f.auth.projectAudit.abandon();expect(await pending).toMatchObject({error:{kind:'cancelled'}});expect(f.auth.state.busy).toBe(true);const count=f.calls.length;await f.auth.restore();expect(f.calls).toHaveLength(count)
  pendingResponse.resolve(mode==='late-401'?problem('SESSION_REVOKED',401):body({items:[row()],next_cursor:null}));await turn();expect(f.auth.state.busy).toBe(false);expect(f.auth.state.phase).toBe('authenticated');expect(f.auth.personalContext.identity?.sessionID).toBe(id(2));expect(f.reads()).toHaveLength(1)
 })
 it('ordinary Human is independent of existing System denial',async()=>{const f=await fixture('admin');f.setOther(async()=>problem('FORBIDDEN',403));await capture(f.auth.system.listUsers({}));expect(f.auth.system.denied).toBe(true);const p=f.page();await turn();expect(p.state.list.phase).toBe('ready');expect(f.auth.system.denied).toBe(true);expect(f.writes()).toEqual([])})
})

describe('actual rejected tail and bounded initial eligibility',()=>{
 it.each(['list','detail'])('%s cancel rejection retains owner then only explicit retry in this page',async kind=>{
  const f=await fixture(),p=f.page();await turn();const h=heldNative('application/json',kind==='list'?{items:[row()],next_cursor:null}:row());f.setAudit(async()=>h.response());const pending=kind==='list'?p.refresh():p.openDetail(auditID);await turn();p.cancel();await pending;await h.entered.promise;expect(f.auth.state.busy).toBe(true);expect(p.blocked.value).toBe(true);const count=f.calls.length
  await expect(f.auth.projects.get(projectID)).rejects.toMatchObject({kind:'busy'});await expect(f.auth.personal.getProfile()).rejects.toMatchObject({kind:'busy'});await p.retry();expect(f.calls).toHaveLength(count)
  h.release(true);await turn();expect(f.auth.state.busy).toBe(false);expect(h.stream.locked).toBe(false);expect(f.calls).toHaveLength(count);expect((kind==='list'?p.state.list:p.state.detail).phase).toBe('error')
  f.setAudit(async()=>body(kind==='list'?{items:[row()],next_cursor:null}:row()));if(kind==='list')await p.retry();else await p.retryDetail();expect((kind==='list'?p.state.list:p.state.detail).phase).toBe('ready');expect(f.calls).toHaveLength(count+1)
 })
 it('new page waits through rejected old native tail and consumes exactly one default initial read',async()=>{
  const f=await fixture(),h=heldNative('text/plain');f.setAudit(async()=>h.response());const old=capture(f.auth.projectAudit.list(projectID,{}));await h.entered.promise;f.auth.projectAudit.abandon();await old;const p=f.page();expect(p.state.list.phase).toBe('waiting');expect(f.reads()).toHaveLength(1)
  f.setAudit(async()=>body({items:[],next_cursor:null}));h.release(true);await turn();await turn();expect(p.state.list.phase).toBe('empty');expect(f.reads()).toHaveLength(2);expect(f.reads()[1]!.path).toBe(`/api/v1/projects/${projectID}/audit?limit=50`);await f.auth.personal.getProfile();await turn();expect(f.reads()).toHaveLength(2)
 })
})

describe('complete Get context, privacy and recovery',()=>{
 it('Resolve and failed Get never grant authority; explicit complete Get creates it once',async()=>{
  const f=await fixture('user',false),g=gate<Response>();releases.push(()=>g.resolve(problem('DEPENDENCY_UNAVAILABLE',503)));f.setOwner(async path=>path.startsWith('/api/v1/projects/resolve?')?body(project()):g.promise);const p=f.page();f.w.afterNavigation('/reader/original/settings/audit');await turn();expect(f.w.currentReadContext.value).toBeNull();expect(f.reads()).toHaveLength(0)
  g.resolve(problem('DEPENDENCY_UNAVAILABLE',503));await turn();expect(f.w.currentReadContext.value).toBeNull();expect(p.state.list.phase).toBe('waiting');f.setOwner(async()=>body(project()));await f.w.readCurrent();await turn();expect(Object.isFrozen(f.w.currentReadContext.value)).toBe(true);expect(Object.isFrozen(f.w.currentReadContext.value?.identity)).toBe(true);expect(f.reads()).toHaveLength(1);expect(f.w.editor.ready).toBe(true);expect(f.w.draft.description).toBe('Original description')
 })
 it.each(['deleting','foreign-owner'])('%s current project cannot grant read context',async variant=>{
  const f=await fixture('user',false),v=project(10,variant==='deleting'?'deleting':'active');if(variant==='foreign-owner')v.owner_user_id=id(3);f.setOwner(async()=>body(v));const p=f.page();f.w.afterNavigation('/reader/original/settings/audit');await turn();expect(f.w.currentReadContext.value).toBeNull();expect(f.reads()).toHaveLength(0);expect(p.state.list.items).toEqual([])
 })
 it('unrelated same-identity profile publication preserves context generation and never reloads Audit',async()=>{
  const f=await fixture(),p=f.page();await turn();const context=f.w.currentReadContext.value!;p.updateFilter('action','secret.delete');f.setOther(async()=>body({user:{...session().user,version:'2',display_name:'Changed profile'},avatar:null}));await f.auth.personal.getProfile();await turn();expect(f.w.currentReadContext.value).toEqual(context);expect(p.draft.action).toBe('secret.delete');expect(f.reads()).toHaveLength(1);expect(p.state.list.phase).toBe('ready')
 })
 it('same-Session checking clears Audit immediately while a General draft survives; new page alone restarts',async()=>{
  const f=await fixture(),p=f.page();await turn();f.w.draft.description='General draft stays';p.updateFilter('resource_id',id(30));await p.openDetail(auditID);const old=p.focusGeneration(),g=gate<Response>(),entered=gate();releases.push(()=>g.resolve(body(session())));f.setAccount(()=>{entered.resolve();return g.promise});const check=f.auth.restore();await entered.promise;expect(f.auth.state.phase).toBe('checking');expect(p.state.list.items).toEqual([]);expect(p.state.detail.record).toBeNull();expect(p.state.detail.target).toBeNull();expect(p.draft.resource_id).toBe('');expect(p.isCurrent(old)).toBe(false);expect(f.w.currentReadContext.value).toBeNull();g.resolve(body(session()));await check;await turn();expect(f.w.draft.description).toBe('General draft stays');const n=f.reads().length;await p.refresh();expect(f.reads()).toHaveLength(n);const replacement=f.page();await turn();expect(replacement.state.list.phase).toBe('ready');expect(f.reads()).toHaveLength(n+1)
 })
 it('same Session with changed CSRF is a new identity epoch and requires fresh Owner Get before Audit',async()=>{
  const f=await fixture(),p=f.page();await turn();p.updateFilter('action','secret.delete');const before=f.auth.personalContext.identity!,g=gate<Response>();releases.push(()=>g.resolve(body(project())));const ownerCount=f.calls.filter(x=>x.path.startsWith('/api/v1/projects/')&&!x.path.includes('/audit')).length;f.setOwner(async path=>path.startsWith('/api/v1/projects/resolve?')?body(project()):g.promise);f.setView(session('user',id(2),'T'.repeat(43)));await f.auth.restore();await turn();expect(f.auth.personalContext.identity?.epoch).not.toBe(before.epoch);expect(p.state.list.phase).toBe('inactive');expect(f.w.currentReadContext.value).toBeNull();expect(f.calls.filter(x=>x.path.startsWith('/api/v1/projects/')&&!x.path.includes('/audit'))).toHaveLength(ownerCount+2);const n=f.reads().length;await p.refresh();expect(f.reads()).toHaveLength(n);const next=f.page();expect(next.state.list.phase).toBe('waiting');g.resolve(body(project()));await turn();expect(next.state.list.phase).toBe('ready');expect(f.reads()).toHaveLength(n+1)
 })
 it('parameter reuse clears old scope synchronously and waits for the new stable-ID Get',async()=>{
  const f=await fixture(),p=f.page();await turn();p.updateFilter('resource_id',id(30));await p.openDetail(auditID);const g=gate<Response>();releases.push(()=>g.resolve(body(project(11))));f.setOwner(async path=>path.startsWith('/api/v1/projects/resolve?')?body(project(11)):g.promise);p.leave();f.w.afterNavigation('/reader/different/settings/audit');expect(p.state.list.items).toEqual([]);expect(p.state.detail.record).toBeNull();expect(p.draft.resource_id).toBe('');const n=f.reads().length;await turn();expect(f.w.currentReadContext.value).toBeNull();expect(f.reads()).toHaveLength(n);g.resolve(body(project(11)));await turn();expect(f.w.currentReadContext.value?.projectID).toBe(id(11));expect(p.state.list.items[0]?.project_id).toBe(id(11));expect(f.reads()).toHaveLength(n+1)
 })
 it('invalid cursor retains no usable history and only explicit first-page action can resume',async()=>{
  const f=await fixture(),p=f.page();await turn();p.updateFilter('limit','1');f.setAudit(async()=>body({items:[row()],next_cursor:'page.two'}));await p.apply();expect(p.state.list.hasNext).toBe(true);f.setAudit(async()=>problem('CURSOR_INVALID',400));await p.next();const n=f.reads().length;expect(p.state.list.cursorInvalid).toBe(true);expect(p.state.list.items).toEqual([]);await p.previous();await p.retry();await p.next();expect(f.reads()).toHaveLength(n);f.setAudit(async()=>body({items:[],next_cursor:null}));await p.refresh();expect(f.reads()[n]!.path).toBe(`/api/v1/projects/${projectID}/audit?limit=1`)
 })
 it('audit route extension remains exact and raw before decoding',()=>{
  expect(projectRoute('/Reader/Owner.dot-name/settings/audit')?.path).toBe('/reader/owner.dot-name/settings/audit');expect(safeReturnTarget('/forgot-password/owner.dot-name/settings/audit')).toBe('/forgot-password/owner.dot-name/settings/audit');for(const bad of ['/reader/original/settings/audit?x=1','/reader/original/settings/audit\n','/reader/original/settings/audit/','/reader/original/settings/%61udit','/reader/original/settings/audit#x','/reader/original/settings/AUDIT'])expect(safeReturnTarget(bad)).toBe('/')
 })
})
