// Diagnosis only: actual Session + Project API + account transport, no gate changes.
const assert = require('node:assert/strict');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../..');
const { JSDOM } = require(root + '/web/node_modules/jsdom');
const id = n => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`;
const at = '2026-10-09T10:00:00.000000Z';
const session = {
  user: { id:id(1), email:'owner@example.test', username:'owner', display_name:'', role:'user', theme:'system', version:'1', initial_password_suggestion:false },
  session: { id:id(2), issued_at:at, absolute_expires_at:at, idle_expires_at:at }, csrf_token:'S'.repeat(43),
};
const project = { id:id(10), owner_user_id:id(1), name:'owned', normalized_name:'owned', description:'', lifecycle:'archived', version:'2', current_sprint_id:null, created_at:at, updated_at:at, archived_at:at };
const deferred = () => { let resolve; const promise=new Promise(r=>resolve=r); return {promise,resolve}; };
const drain = async () => { for(let n=0;n<16;n++) await Promise.resolve(); await new Promise(setImmediate); };
const unhandled=[];
process.on('unhandledRejection',e=>unhandled.push(e));
(async()=>{
  const {build}=await import(root+'/web/node_modules/vite/dist/node/index.js');
  const entry=root+'/output/ai/work-owner-planning-ui/virtual-project-refresh.js';
  const built=await build({configFile:false,root,logLevel:'silent',plugins:[{
    name:'actual-project-refresh',enforce:'pre',resolveId:n=>n===entry?'\0actual-refresh':undefined,
    load:n=>n==='\0actual-refresh'?`export {createSessionController} from '${root}/web/src/composables/useSession.ts';export {createAccountAPI} from '${root}/web/src/api/account.ts';export {createProjectOwnerAPI} from '${root}/web/src/api/project-owner.ts';`:undefined,
  }],build:{write:false,minify:false,lib:{entry,name:'ActualRefresh',formats:['iife']}}});
  const code=(Array.isArray(built)?built:[built]).flatMap(x=>x.output).find(x=>x.type==='chunk').code;
  let count=0;
  for(const test of [
    {name:'archived typed fulfillment after actual tails'},
    {name:'reader cancellation held',hold:'reader'},
    {name:'outer cancellation held',hold:'outer'},
    {name:'abandon rejects before reader retirement',hold:'reader',action:'abandon'},
    {name:'deadline rejects before outer retirement',hold:'outer',action:'deadline'},
    {name:'identity leave rejects before outer retirement',hold:'outer',action:'leave'},
    {name:'wrong owner strict typed rejection',bad:'owner'},
    {name:'wrong target strict typed rejection',bad:'target'},
    {name:'malformed original body rejection',bad:'json'},
  ]){
    const dom=new JSDOM('<body></body>',{url:'https://owned.invalid',runScripts:'outside-only'}), w=dom.window;
    Object.assign(w,{Request,Response,Headers,ReadableStream,TextEncoder,TextDecoder,Uint8Array,Blob,AbortController,process:{env:{NODE_ENV:'production'}}});
    Object.defineProperty(w,'crypto',{value:crypto.webcrypto});
    const timers=new Map();let sequence=0;
    w.setTimeout=(fn,ms)=>{const key=++sequence;timers.set(key,{fn,ms});return key;};w.clearTimeout=key=>timers.delete(key);
    const entered=deferred(),release=deferred();let requests=0,readerReturns=0,outerReturns=0;
    w.fetch=(url,options)=>{
      if(url==='/api/v1/session')return Promise.resolve(new Response(JSON.stringify(session),{status:200,headers:{'Content-Type':'application/json','X-Request-ID':id(99)}}));
      assert.equal(url,`/api/v1/projects/${project.id}`);assert.equal(options.method,'GET');requests++;
      const value={...project,...(test.bad==='owner'?{owner_user_id:id(3)}:{}),...(test.bad==='target'?{id:id(11)}:{})};
      const bytes=new TextEncoder().encode(test.bad==='json'?'{':JSON.stringify(value));
      const body=new ReadableStream({start(c){c.enqueue(bytes);c.close();}});
      const get=body.getReader.bind(body),cancelBody=body.cancel.bind(body);
      body.getReader=(...args)=>{const reader=get(...args),cancel=reader.cancel.bind(reader);
        reader.cancel=(...a)=>{if(test.hold==='reader')entered.resolve();return (test.hold==='reader'?release.promise:Promise.resolve()).then(()=>cancel(...a)).then(x=>{readerReturns++;return x;});};return reader;};
      body.cancel=(...args)=>{if(test.hold==='outer')entered.resolve();return (test.hold==='outer'?release.promise:Promise.resolve()).then(()=>cancelBody(...args)).then(x=>{outerReturns++;return x;});};
      return Promise.resolve(new Response(body,{status:200,headers:{'Content-Type':'application/json','Content-Length':String(bytes.length),'X-Request-ID':id(100)}}));
    };
    vm.runInContext(code,dom.getInternalVMContext());
    const fetcher=(...args)=>w.fetch(...args),deps=Array(13).fill(undefined);
    deps[0]=w.ActualRefresh.createAccountAPI(fetcher);deps[12]=w.ActualRefresh.createProjectOwnerAPI(fetcher);
    const auth=w.ActualRefresh.createSessionController(...deps);
    await auth.restore();assert.equal(auth.state.phase,'authenticated');
    let outcome='pending',failure;
    const promise=auth.projects.get(project.id).then(value=>{
      outcome='fulfilled';assert.equal(auth.state.busy,false);assert.equal(value.lifecycle,'archived');return value;
    },error=>{outcome='rejected';failure=error;});
    if(test.hold){
      await entered.promise;await drain();assert.equal(outcome,'pending');assert.equal(auth.state.busy,true);
      await assert.rejects(auth.projects.get(project.id),e=>e.kind==='busy');assert.equal(requests,1);
      if(test.action==='abandon')auth.projects.abandonRead();
      if(test.action==='deadline'){const timer=[...timers.values()].find(x=>x.ms===30000);assert.ok(timer);timer.fn();}
      if(test.action==='leave')auth.leave();
      if(test.action){await drain();assert.equal(outcome,'rejected');assert.equal(failure.kind,'cancelled');assert.equal(auth.state.busy,true);}
      release.resolve();
    }
    await promise;await drain();
    assert.equal(outcome,test.action||test.bad?'rejected':'fulfilled');assert.equal(auth.state.busy,false);
    assert.equal(requests,1);assert.equal(readerReturns,1);assert.equal(outerReturns,1);
    if(test.bad)assert.equal(failure.kind,'invalid-response');
    if(test.action)assert.equal(failure.kind,'cancelled');
    dom.window.close();count++;console.log('PASS',test.name);
  }
  await drain();assert.equal(unhandled.length,0);console.log('PASS actual Project read controls',count,'unhandled',unhandled.length);
})().catch(e=>{console.error(e);process.exitCode=1;});
