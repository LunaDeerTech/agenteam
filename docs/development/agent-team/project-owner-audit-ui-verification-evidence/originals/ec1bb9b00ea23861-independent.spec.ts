import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, isNavigationFailure, type Router } from 'vue-router'
import type { SessionController } from '/workspace/agenteam/web/src/composables/useSession'
const selected=vi.hoisted(()=>({auth:null as SessionController|null}))
vi.mock('/workspace/agenteam/web/src/composables/useSession.ts',async original=>({...await original<typeof import('/workspace/agenteam/web/src/composables/useSession')>(),useSession:()=>selected.auth!}))
import { createSessionController } from '/workspace/agenteam/web/src/composables/useSession'
import { createAccountAPI } from '/workspace/agenteam/web/src/api/account'
import { createProjectOwnerAPI } from '/workspace/agenteam/web/src/api/project-owner'
import { createProjectAuditAPI } from '/workspace/agenteam/web/src/api/project-audit'
import { installAuthentication, projectRoute } from '/workspace/agenteam/web/src/router/auth'
import App from '/workspace/agenteam/web/src/App.vue'
import ProjectAuditView from '/workspace/agenteam/web/src/views/projects/ProjectAuditView.vue'

const id=(n:number)=>`01970000-0000-7000-8000-${n.toString(16).padStart(12,'0')}`
const time='2026-10-08T12:34:56.123456Z',auditID=id(80),pid=id(10)
const session=()=>({user:{id:id(1),email:'owner@example.test',username:'owner',display_name:'Owner',role:'user',theme:'system',version:'1',initial_password_suggestion:false},session:{id:id(2),issued_at:time,idle_expires_at:time,absolute_expires_at:time},csrf_token:'S'.repeat(43)})
const project=(n=10,name='Demo')=>({id:id(n),owner_user_id:id(1),name,normalized_name:name.toLowerCase(),description:'Original safe project',lifecycle:'active',version:'1',current_sprint_id:null,created_at:time,updated_at:time,archived_at:null})
const row=(projectID=pid)=>({audit_id:auditID,created_at:time,scope:'project',project_id:projectID,actor:{kind:'human',id:id(1)},action:'secret.delete',outcome:'success',resource:{kind:'secret',id:id(30)},metadata:{version:'9007199254740993'},associations:{},summary:'Secret deleted'})
const body=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':status<400?'application/json':'application/problem+json','X-Request-ID':id(90)}})
const unavailable=()=>body({type:'urn:agenteam:problem:test',title:'Failure',status:503,code:'DEPENDENCY_UNAVAILABLE',detail:'PRIVATE_DIAGNOSTIC_SHOULD_NOT_RENDER',instance:'/api/v1/session',request_id:id(90),commit_state:'not_started'},503)
function gate<T>(){let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>resolve=r);return{promise,resolve}}
const wrappers:VueWrapper[]=[],owners:SessionController[]=[],routers:Router[]=[],releases:(()=>void)[]=[]
const realFetch=globalThis.fetch
beforeAll(()=>{globalThis.fetch=async()=>{throw new Error('Real network prohibited in controlled App probe')}})
afterAll(()=>{globalThis.fetch=realFetch})
beforeEach(()=>{vi.stubGlobal('matchMedia',(media:string)=>({matches:false,media,addEventListener(){},removeEventListener(){}}))})
afterEach(async()=>{for(const release of releases.splice(0))release();for(const wrapper of wrappers.splice(0))wrapper.unmount();for(const owner of owners)owner.leave();await flushPromises();for(const owner of owners.splice(0))expect(owner.state.busy).toBe(false);for(const router of routers.splice(0))router.options.history.destroy();selected.auth=null;document.body.replaceChildren();vi.unstubAllGlobals();vi.restoreAllMocks()})
async function app(target='/owner/demo/settings/audit',initialProject=project()){
 let current=initialProject
 let sessionFetch=async()=>body(session())
 let ownerFetch=async(_path:string,_init:RequestInit)=>body(current)
 let auditFetch=async(path:string)=>{const projectID=path.split('/')[4]!;return body(path.split('?')[0]!.split('/').length===7?row(projectID):{items:[row(projectID)],next_cursor:null})}
 const calls:{path:string;init:RequestInit}[]=[]
 const fetch=async(path:string,init:RequestInit)=>{calls.push({path,init});if(path==='/api/v1/session')return sessionFetch();if(/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path))return auditFetch(path);if(path.startsWith('/api/v1/projects/'))return ownerFetch(path,init);throw new Error('Unexpected controlled endpoint '+path)}
 const auth=createSessionController(createAccountAPI(fetch),undefined,undefined,undefined,undefined,undefined,undefined,undefined,undefined,undefined,undefined,undefined,createProjectOwnerAPI(fetch),createProjectAuditAPI(fetch));selected.auth=auth;owners.push(auth);await auth.restore()
 const {router:production}=await import('/workspace/agenteam/web/src/router/index');production.options.history.destroy()
 const router=createRouter({history:createMemoryHistory(),routes:production.options.routes});routers.push(router);installAuthentication(router,auth);await router.push(target);await router.isReady()
 const host=document.createElement('div');host.id='app';document.body.append(host);const wrapper=mount(App,{attachTo:host,global:{plugins:[router]}});wrappers.push(wrapper);await flushPromises()
 return{auth,router,wrapper,calls,projectCalls:()=>calls.filter(x=>x.path.startsWith('/api/v1/projects')),auditCalls:()=>calls.filter(x=>/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(x.path)),setAudit(f:typeof auditFetch){auditFetch=f},setOwner(f:typeof ownerFetch){ownerFetch=f},setSession(f:typeof sessionFetch){sessionFetch=f},setProject(p:ReturnType<typeof project>){current=p}}
}
function button(name:string){const found=[...document.querySelectorAll<HTMLButtonElement>('button')].find(node=>{const copy=node.cloneNode(true) as HTMLElement;copy.querySelectorAll('[aria-hidden="true"]').forEach(n=>n.remove());return(node.getAttribute('aria-label')??copy.textContent?.trim())===name});if(!found)throw new Error('Missing button '+name);return found}
async function click(name:string){button(name).click();await flushPromises()}
function input(label:string){const node=[...document.querySelectorAll<HTMLLabelElement>('label')].find(n=>n.textContent?.trim()===label);if(!node?.htmlFor)throw new Error('Missing label '+label);return document.getElementById(node.htmlFor) as HTMLInputElement|HTMLSelectElement}
async function setInput(label:string,value:string){const el=input(label);el.value=value;el.dispatchEvent(new Event(el instanceof HTMLSelectElement?'change':'input',{bubbles:true}));await flushPromises()}

describe('independent production App raw rejection',()=>{
 it.each(['/owner/demo/settings/audit?x=1','/owner/demo/settings/audit?','/owner/demo/settings/general#fragment','/owner/demo/settings#','/owner/demo/%73ettings/audit','/owner/demo/settings/%61udit'])('cold %s sends zero Project requests after guard and onMounted',async target=>{
  const f=await app(target);expect(f.router.currentRoute.value.name).toBe('not-found');expect(projectRoute(f.router.currentRoute.value.fullPath)).toBeNull();expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(false);expect(f.projectCalls()).toEqual([]);window.dispatchEvent(new Event('pageshow'));await flushPromises();expect(f.projectCalls()).toEqual([]);expect(f.calls).toHaveLength(1)
 })
 it('warm invalid query retires the real page without reauthorizing its sanitized address',async()=>{
  const f=await app('/OWNER/OWNER.Dot-Name/settings/audit',project(10,'Owner.Dot-Name'));expect(f.router.currentRoute.value.path).toBe('/owner/owner.dot-name/settings/audit');expect(f.wrapper.get('nav[aria-label="项目导航"] a[href="/owner/owner.dot-name/settings/general"]').attributes('aria-current')).toBe('page');const old=f.wrapper.get('h1').element;const count=f.projectCalls().length;await f.router.push('/owner/owner.dot-name/settings/audit?cursor=private');await flushPromises();expect(f.router.currentRoute.value.name).toBe('not-found');expect(projectRoute(f.router.currentRoute.value.fullPath)).toBeNull();expect(old.isConnected).toBe(false);expect(f.projectCalls()).toHaveLength(count)
 })
})

describe('independent App lifetime and explicit recovery',()=>{
 it('a late detail completion after a later guard cancels navigation cannot republish or steal focus',async()=>{
  const f=await app(),g=gate<Response>();releases.push(()=>g.resolve(body(row())));f.setAudit(()=>g.promise);await click('查看详情 '+auditID);expect(f.auth.state.busy).toBe(true);const oldHeading=document.getElementById('project-audit-detail-heading')!;const remove=f.router.beforeResolve(()=>false);const outcome=await f.router.push('/owner/second/settings/audit');await flushPromises();expect(isNavigationFailure(outcome)).toBe(true);expect(f.wrapper.text()).toContain('本页读取已停止');expect(oldHeading.isConnected).toBe(false);const sentinel=document.createElement('button');sentinel.textContent='Focus observer';document.body.append(sentinel);sentinel.focus();const count=f.auditCalls().length;g.resolve(body(row()));await flushPromises();expect(f.auth.state.busy).toBe(false);expect(f.auditCalls()).toHaveLength(count);expect(document.activeElement).toBe(sentinel);expect(document.getElementById('project-audit-detail-heading')).toBeNull();remove();f.setAudit(async()=>body({items:[row()],next_cursor:null}));await click('重试读取');expect(f.wrapper.find('[aria-label="项目审计列表"]').exists()).toBe(true);expect(f.auditCalls()).toHaveLength(count+1)
 })
 it('idle actual App pageshow503 removes private DOM and a manual Session check creates one fresh default read',async()=>{
  const f=await app();await setInput('动作','secret.delete');await click('应用筛选');await click('查看详情 '+auditID);const oldHeading=document.getElementById('project-audit-detail-heading')!;expect(f.auth.state.busy).toBe(false);const count=f.auditCalls().length,sessionCount=f.calls.filter(x=>x.path==='/api/v1/session').length;f.setSession(async()=>unavailable());window.dispatchEvent(new Event('pageshow'));await flushPromises();expect(f.calls.filter(x=>x.path==='/api/v1/session')).toHaveLength(sessionCount+1);expect(oldHeading.isConnected).toBe(false);expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(false);expect(f.wrapper.text()).toContain('会话尚未确认');expect(f.auditCalls()).toHaveLength(count);f.setSession(async()=>body(session()));await click('检查当前会话');expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(true);expect(input('动作').value).toBe('');expect(input('每页数量').value).toBe('50');expect(f.auditCalls()).toHaveLength(count+1);expect(f.calls.every(x=>x.init.method==='GET')).toBe(true)
 })
 it('parameter reuse removes the old detail and filter before its new Get completes',async()=>{
  const f=await app();await setInput('Tool ID',id(40));await click('查看详情 '+auditID);const oldHeading=document.getElementById('project-audit-detail-heading')!,g=gate<Response>();releases.push(()=>g.resolve(body(project(11,'Second'))));f.setOwner(async path=>path.startsWith('/api/v1/projects/resolve?')?body(project(11,'Second')):g.promise);const count=f.auditCalls().length;await f.router.push('/owner/second/settings/audit');await flushPromises();expect(oldHeading.isConnected).toBe(false);expect(f.wrapper.find('[aria-label="项目审计列表"]').exists()).toBe(false);expect(f.auditCalls()).toHaveLength(count);g.resolve(body(project(11,'Second')));await flushPromises();expect(input('Tool ID').value).toBe('');expect(f.auditCalls()).toHaveLength(count+1);expect(f.auditCalls().at(-1)!.path).toBe(`/api/v1/projects/${id(11)}/audit?limit=50`);await click('查看详情 '+auditID);expect(f.wrapper.get('[aria-labelledby="project-audit-detail-heading"]').text()).toContain(id(11))
 })
})
