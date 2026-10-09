// Offline actual-source controls: no server, socket, browser or real account.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../..');
const { JSDOM } = require(root + '/web/node_modules/jsdom');
const pwRoot = root + '/tests/account-captcha-web/node_modules/playwright';
assert.equal(require(pwRoot + '/package.json').version, '1.56.1');
const { transformHook } = require(pwRoot + '/lib/transform/transform.js');
const id = n => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`;
const project = id(10), target = id(11), at = '2026-10-09T10:00:00.000000Z';
const session = { user: { id:id(1),email:'owner@example.test',username:'owner',display_name:'',role:'user',theme:'system',version:'1',initial_password_suggestion:false }, session:{id:id(2),issued_at:at,absolute_expires_at:at,idle_expires_at:at},csrf_token:'S'.repeat(43) };
const milestone = {id:target,project_id:project,title:'private-material-canary',description:'',manual_rank:'8'.repeat(32),version:'1',created_at:at,updated_at:at};
const task = {...milestone,milestone_id:id(12),sprint_id:id(13),type:'task',priority:'medium',state:'backlog',assignee_agent_id:null,plan:'',version:'2'};
const sprint = {...milestone,milestone_id:id(12),state:'planned',started_at:null,started_by:null,completed_at:null,completed_by:null};
const drain = async () => { for(let i=0;i<16;i++)await Promise.resolve();await new Promise(r=>setImmediate(r)); };
const deferred = () => { let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve}; };
const failures=[];process.on('unhandledRejection', e=>failures.push(e));
function installer(file, name) {
 const source=fs.readFileSync(root+'/'+file,'utf8');
 const compiled=transformHook(source,root+'/'+file).code;
 const ctx=vm.createContext({exports:{},require:id=>id.startsWith('./')?{}:require(id)});
 vm.runInContext(compiled,ctx);
 return '('+ctx.exports[name].toString()+')';
}
(async()=>{
 const {build}=await import(root+'/web/node_modules/vite/dist/node/index.js');
 const entry=root+'/output/ai/work-owner-planning-ui/virtual-ordinary-owner.js';
 const built=await build({configFile:false,root,logLevel:'silent',plugins:[{name:'actual-work-consumer',enforce:'pre',resolveId:id=>id===entry?'\0actual-work-consumer':undefined,load:id=>id==='\0actual-work-consumer'?`export {createSessionController} from '${root}/web/src/composables/useSession.ts';export {createAccountAPI} from '${root}/web/src/api/account.ts';export {createWorkPlanningAPI} from '${root}/web/src/api/work-planning.ts';`:undefined}],build:{write:false,minify:false,lib:{entry,name:'ActualWork',formats:['iife']}}});
 const code=(Array.isArray(built)?built:[built]).flatMap(x=>x.output).find(x=>x.type==='chunk').code;
 const nativeCode=installer('tests/account-captcha-web/e2e/project-work-planning.native.ts','installWorkNativeDiagnostic');
 const publicCode=installer('tests/account-captcha-web/e2e/project-work-planning.publication.ts','installWorkPublicationDiagnostic');
 const passed=[];
 async function check(name,fn){await fn();passed.push(name);}
 async function setup(kind='milestone',options={}){
  const dom=new JSDOM('<body></body>',{url:'https://owned.invalid',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
  Object.assign(w,{Request,Response,Headers,ReadableStream,TextEncoder,TextDecoder,Uint8Array,Blob,AbortController,process:{env:{NODE_ENV:'production'}}});
  Object.defineProperty(w,'crypto',{value:crypto.webcrypto});w.performance.getEntriesByName=()=>[{}];
  let timerSequence=0;const timers=new Map();
  w.setTimeout=(fn,ms)=>{const id=++timerSequence;timers.set(id,{fn,ms});return id};w.clearTimeout=id=>timers.delete(id);
  let requestCount=0,preparing=false;const readerHold=deferred(),streamHold=deferred(),readerEntered=deferred(),streamEntered=deferred();
  const endpoint=kind==='structure'||kind==='lookup-task'?`/api/v1/projects/${project}/${kind==='structure'?'structure':'task'}-commands/lookup`:`/api/v1/projects/${project}/${kind==='milestone'?'milestones':kind==='sprint'?'sprints':'tasks'}/${target}`;
  w.fetch=(url,init)=>{
   if(url==='/api/v1/session')return Promise.resolve(new Response(JSON.stringify(session),{status:200,headers:{'Content-Type':'application/json','X-Request-ID':id(99)}}));
   if(preparing)return Promise.reject(Error('owned preparation transport failure'));
   assert.equal(url,endpoint);assert.equal(init.method,kind==='structure'||kind==='lookup-task'?'POST':'GET');requestCount++;
   let value=kind==='milestone'?milestone:kind==='sprint'?sprint:kind==='task'?task:kind==='structure'?{state:'in_progress',result:null}:{status:'in_progress',receipt:null};
   const bytes=new TextEncoder().encode(options.badJSON?'{':JSON.stringify(value));
   const body=new ReadableStream({start(c){c.enqueue(bytes);c.close()}});
   const get=body.getReader.bind(body),cancelStream=body.cancel.bind(body);
   body.getReader=(...args)=>{const reader=get(...args),cancel=reader.cancel.bind(reader);reader.cancel=()=>{readerEntered.resolve();return(options.readerHeld?readerHold.promise:Promise.resolve()).then(()=>cancel())};return reader;};
   body.cancel=()=>{streamEntered.resolve();return(options.streamHeld?streamHold.promise:Promise.resolve()).then(()=>cancelStream())};
   return Promise.resolve(new Response(body,{status:200,headers:{'Content-Type':'application/json','Content-Length':String(bytes.length),'X-Request-ID':id(100)}}));
  };
  vm.runInContext(code,ctx);
  const fetcher=(...args)=>w.fetch(...args);
  const dependencies=Array(16).fill(undefined);dependencies[0]=w.ActualWork.createAccountAPI(fetcher);dependencies[15]=w.ActualWork.createWorkPlanningAPI(fetcher);
  const auth=w.ActualWork.createSessionController(...dependencies);await auth.restore();assert.equal(auth.state.phase,'authenticated');assert.equal(auth.state.busy,false);
  if(kind==='structure'||kind==='lookup-task'){
   preparing=true;
   await auth.workPlanning.start(kind==='structure'?{domain:'structure',projectID:project,command:'work.milestone.update',targetID:target,expected_version:'1',request:{title:'original'}}:{domain:'task',projectID:project,command:'work.task.update',targetID:target,expected_version:'1',request:{plan:'original'}}).catch(()=>{});
   preparing=false;assert.equal(auth.workPlanning.progress.canLookup,true);
  }
  vm.runInContext(`this.installNative=${nativeCode};this.installPublic=${publicCode}`,ctx);
  w.installNative({projects:[project],expiresAt:Date.now()+45000});
  w.Function=function(){return async()=>({singleton:()=>auth})};
  assert.equal(await w.installPublic({binding:{entry:'/assets/main.js',asset:'/assets/session.js',export_name:'singleton'},expiresAt:Date.now()+45000}),'installed');
  let settled=false,outcome='pending';
  const call=()=>{
   const result=kind==='structure'||kind==='lookup-task'?auth.workPlanning.checkOriginal():auth.workPlanning[kind==='milestone'?'getMilestone':kind==='sprint'?'getSprint':'getTask'](project,target);
   void result.then(()=>{settled=true;outcome='fulfilled'},()=>{settled=true;outcome='rejected'});return result;
  };
  const finish=()=>({public:w.__workPublicationDiagnostic.finish(),native:w.__workNativeDiagnostic.finish()});
  return {auth,w,endpoint,call,readerEntered,streamEntered,releaseReader:()=>readerHold.resolve(),releaseStream:()=>streamHold.resolve(),settled:()=>settled,outcome:()=>outcome,count:()=>requestCount,timer(){const found=[...timers.values()].filter(t=>t.ms===30000);assert.equal(found.length,1);found[0].fn()},finish,async cleanup(){readerHold.resolve();streamHold.resolve();auth.leave();await drain();finish();dom.window.close()}};
 }
 for(const kind of ['milestone','sprint','task','structure','lookup-task'])await check('actual '+kind+' typed fulfillment follows real native and owner tails',async()=>{
  const x=await setup(kind);try{await x.call();await drain();assert.equal(x.auth.state.busy,false);const e=x.finish(),n=e.native.requests[0],p=e.public.calls[0];assert.equal(x.count(),1);assert.equal(p.fulfilled,1);assert.equal(p.native_requests,1);assert.equal(p.native_sequence,n.sequence);assert.equal(n.call_id,p.call_id);assert(n.eof_before_interruption&&n.length_matches_before_binding);assert.equal(n.read_settled,n.read_calls);assert.equal(n.reader_cancel_settled,1);assert.equal(n.stream_cancel_settled,1);assert.equal(n.release_successes,1);assert.equal(e.native.pending_at_retirement,0);assert.equal(e.public.pending_at_retirement,0);assert.equal(e.native.retirement_reason,'explicit');assert.equal(e.public.retirement_reason,'explicit');assert.equal(p.identity_current,true);assert.equal(p.not_busy,true);assert(!JSON.stringify(e).includes('private-material-canary'));}finally{await x.cleanup()}
 });
 for(const kind of ['milestone','structure'])for(const held of ['reader','stream'])await check('actual '+kind+' '+held+' cancellation must join before normal visible fulfillment',async()=>{
  const x=await setup(kind,{[held+'Held']:true});try{const promise=x.call();await x[held+'Entered'].promise;await drain();assert.equal(x.settled(),false);assert.equal(x.auth.state.busy,true);assert.equal(x.w.__workPublicationDiagnostic.snapshot().calls[0].fulfilled,0);x[held==='reader'?'releaseReader':'releaseStream']();await promise;await drain();assert.equal(x.outcome(),'fulfilled');assert.equal(x.auth.state.busy,false);const e=x.finish();assert.equal(e.public.calls[0].fulfilled,1);assert.equal(e.native.pending_at_retirement,0);assert.equal(e.public.pending_at_retirement,0);assert.equal(x.count(),1);}finally{await x.cleanup()}
 });
 for(const ending of ['abandon','timer','identity'])await check('actual early '+ending+' rejects visibly without releasing held original owner or late success',async()=>{
  const x=await setup('milestone',{streamHeld:true});try{const promise=x.call();await x.streamEntered.promise;await drain();if(ending==='abandon')x.auth.workPlanning.abandonRead();else if(ending==='timer')x.timer();else x.auth.leave();await promise.catch(()=>{});await drain();assert.equal(x.outcome(),'rejected');assert.equal(x.auth.state.busy,true);assert.equal(x.w.__workPublicationDiagnostic.snapshot().calls[0].fulfilled,0);x.releaseStream();await drain();assert.equal(x.auth.state.busy,false);const e=x.finish();assert.equal(e.public.calls[0].fulfilled,0);assert.equal(e.public.calls[0].rejected,1);assert.equal(e.native.requests[0].signal_aborted,true);if(ending==='identity')assert.equal(e.public.calls[0].identity_current,false);assert.equal(x.count(),1);}finally{await x.cleanup()}
 });
 await check('actual complete native EOF with invalid JSON cannot become typed completion',async()=>{
  const x=await setup('milestone',{badJSON:true});try{await x.call().catch(()=>{});await drain();const e=x.finish();assert(e.native.requests[0].length_matches_before_binding);assert.equal(e.public.calls[0].fulfilled,0);assert.equal(e.public.calls[0].rejected,1);assert.equal(x.auth.state.busy,false);}finally{await x.cleanup()}
 });
 await drain();assert.equal(failures.length,0);console.log(JSON.stringify({passed:passed.length,controls:passed,unhandled:0,actualSession:true,actualWorkAPI:true,actualTransport:true,browser:false,network:false}));
})().catch(error=>{console.error(error);process.exitCode=1});
