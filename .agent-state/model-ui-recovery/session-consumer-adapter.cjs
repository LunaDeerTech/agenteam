// Offline controls for the two explicit Session consumption points.
// Run from any cwd with locked web and account-captcha-web npm dependencies installed.
// Outputs/caches stay under output/ai; no browser, socket, or business request is opened.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),{randomUUID}=require('node:crypto'),{EventEmitter}=require('node:events');
const root=require('node:path').resolve(__dirname,'../..'),ts=require(root+'/web/node_modules/typescript'),file='.agent-state/model-ui-recovery/authority-and-identity.ts',source=fs.readFileSync(root+'/'+file,'utf8'),ast=ts.createSourceFile(file,source,ts.ScriptTarget.Latest,true);
fs.mkdirSync(root+'/output/ai/model-ui-recovery/consumer-method-controls',{recursive:true});
const declaration=name=>ast.statements.find(n=>ts.isFunctionDeclaration(n)&&n.name?.text===name).getText(ast),fixtures=[],rows=[],unhandled=[];process.on('unhandledRejection',e=>unhandled.push(e));
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject}};
const flush=async()=>{for(let i=0;i<60;i++)await Promise.resolve()};
function native() {
  return { requests: 1, readers: 1, read_calls: 2, read_settled: 2, read_rejected: 0, bytes: 483,
    reader_cancel_calls: 1, reader_cancel_settled: 1, reader_cancel_rejected: 0, stream_cancel_calls: 1, stream_cancel_settled: 1, stream_cancel_rejected: 0,
    release_calls: 1, release_successes: 1, abort_events: 0, status: 200, headers_order: 1, read_done_order: 2, read_rejected_order: 0, abort_order: 0,
    reader_cancel_order: 3, stream_cancel_order: 5, release_order: 4, content_length: 483, headers_seen: true, status_ok: true, read_done: true,
    cancel_before_eof: false, request_id_match: true, signal_aborted: false, signal_aborted_at_start: false,
    content_length_present: true, content_length_valid: true, content_encoding_identity: true, content_length_comparable: true,
    content_length_matches_eof: true, eof_before_interruption: true, failure: 'none' };
}
function fixture(options = {}) {
  let now = 1000, next = 0, predicate, observed, actionCalls = 0, finishedCalls = 0, jsonCalls = 0;
  const timers = new Map(), page = new EventEmitter(), browserContext = new EventEmitter(), cdp = new EventEmitter(), writes = [], probeCalls = [], steps = [];
  const waiting = deferred(), finish = deferred(), body = deferred(), info = { status: 'passed' };
  const uid = '11111111-1111-7111-8111-111111111111', sid = '22222222-2222-7222-8222-222222222222';
  let failure = null, ended = false;
  const request = { url: () => (options.origin ?? 'https://owned.invalid') + '/api/v1/session' + (options.query ?? ''), method: () => options.method ?? 'GET', failure: () => failure };
  const response = { url: request.url, request: () => request, status: () => options.status ?? 200,
    headerValue: () => Promise.resolve(options.missingID ? null : 'private-id-canary'),
    finished() { finishedCalls++; if (options.finishThrows) throw options.finishThrows; return finish.promise; },
    json() { jsonCalls++; return body.promise; } };
  const probe = {
    sessionBegin(...args) { probeCalls.push(['begin', args]); return true; },
    sessionSnapshot(slot, id) { probeCalls.push(['snapshot']); if(ended)return null; return { ...native(), ...(options.native ?? {}), request_id_match: !!id }; },
    sessionEnd() { probeCalls.push(['end']); ended=!options.keepNative; return options.missingEnd?null:{}; },
  };
  cdp.send = async () => {}; cdp.detach = async () => {}; browserContext.newCDPSession = async () => cdp;
  const owner = { action_calls:1,restore_calls:1,owned_restore_calls:1,session_requests:1,owned_session_requests:1,response_headers:1,pending_observations:0,restore_settled:true,restore_rejected:false,restore_threw:false,entry_authenticated:true,entry_not_busy:true,entry_user_matches:true,entry_session_matches:true,role_valid:true,published_role:'user',hooks_retired:true,authenticated:true,not_busy:true,user_matches:true,session_matches:true,observer_failed:false,request_id_match:true,completion_upper_bound:true,clock:'browser-monotonic-observed-relative-to-install',timing:{action:1,restore_enter:2,request:3,headers:4,restore_settled:5,state_sample:5},...(options.owner??{}) };
  const context = { Error, URL, Promise, randomUUID, Date, Function: function(){return async()=>({loginOwnerModule:async()=>({asset:'/a.js',entry:'/b.js',export_name:'useSession'})})}, resolve:()=>'/synthetic-root', resolveEvaluations: new WeakMap(), performance: { now: () => now }, test: { info: () => info },
    process: { env: { AGENTEAM_PROJECT_MODELS_WEB_DIST:'/synthetic-dist', AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE: '/synthetic', AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH: 'control' } }, join: (...s) => s.join('/'),
    writeFileSync(path, text) { if (options.writeThrows && path.endsWith('response-diagnostic.json')) throw Error('private-write-canary'); writes.push({ path, row: JSON.parse(text) }); },
    setTimeout(callback, delay) { const id = ++next; timers.set(id, { callback, delay }); return id; }, clearTimeout(id) { timers.delete(id); } };
  const functions = ['need', 'object', 'uuid', 'authorityAwait', 'resolveEvaluate', 'resolveSampler', 'resolveSnapshot', 'restoreOwnerProjection', 'consumedNative', 'consumedOwner', 'acceptedSessionConsumption', 'installRestoreOwnerObservation', 'beginResponseDiagnostic', 'beginSessionResponseDiagnostic', 'sessionStage', 'sessionIdentity'];
  vm.runInNewContext(ts.transpileModule(functions.map(n => declaration(n).replace(/^export /, '')).join('\n') + '\nthis.run=sessionIdentity;this.begin=beginSessionResponseDiagnostic;this.gate=acceptedSessionConsumption;', { compilerOptions: { target: ts.ScriptTarget.ES2024 } }).outputText, context);
  page.url = () => 'https://owned.invalid/'; page.context = () => browserContext;
  const publishedIdentity={userID:uid,sessionID:sid,role:'user',...(options.identity??{})};
  const host={__projectModelsProbe:probe,__authorityRestoreOwner:{snapshot:()=>options.ownerMissing?null:owner,identity:()=>publishedIdentity,finish:()=>{if(!options.keepOwner)delete host.__authorityRestoreOwner;return options.ownerMissing?null:owner}}};
  const evalHeld=deferred(),detachHeld=deferred();
  page.evaluate=(callback,args)=>options.closed?Promise.reject(Error('private-close-canary')):callback.name==='installRestoreOwnerObservation'?Promise.resolve(options.beginResult??true):options.evalHung?evalHeld.promise:Promise.resolve(vm.runInNewContext('('+callback.toString()+')(args)',{window:host,args}));
  if(options.detachHung)cdp.detach=()=>detachHeld.promise;
  if(options.preissued){const on=page.on.bind(page);let sent=false;page.on=(name,fn)=>{const r=on(name,fn);if(name==='request'&&!sent){sent=true;queueMicrotask(()=>page.emit('request',request))}return r}};

  page.waitForResponse = (filter, timeout) => { assert.equal(timeout.timeout, 5000); predicate = filter; return waiting.promise; };
  const action = () => {
    actionCalls++; if (options.actionError) return Promise.reject(options.actionError);
    page.emit('request', request); cdp.emit('Network.requestWillBeSent',{requestId:'private-cdp',request:{url:request.url(),method:request.method()}}); cdp.emit('Network.responseReceived',{requestId:'private-cdp',response:{headers:{'X-Request-ID':options.wrongCDP?'other':'private-id-canary'}}}); if(options.duplicateCDP){cdp.emit('Network.requestWillBeSent',{requestId:'private-cdp2',request:{url:request.url(),method:request.method()}});cdp.emit('Network.responseReceived',{requestId:'private-cdp2',response:{headers:{'X-Request-ID':'private-id-canary'}}});} if (options.duplicate) page.emit('request', { ...request });
    if (predicate(response)) waiting.resolve(response);
    return Promise.resolve();
  };
  if (options.stub) {
    const stub = { start() { if (options.stub === 'start') throw Error('private-start'); }, select(r) { assert.equal(r, response); if (options.stub === 'select') throw Error('private-select'); },
      finishedWait(work) { const original = work(); assert.equal(original, finish.promise); return original; },
      async finish(failed) { if (options.stub === 'finish') throw Error('private-finish'); } };
    context.beginSessionResponseDiagnostic = options.stub === 'begin' ? () => Promise.reject(Error('private-begin')) : async () => stub;
  }
  const result = { context, owner, native, uid, sid, page, browserContext, cdp, request, response, finish, body, writes, probeCalls, steps, timers, info,
    counters: () => ({ actionCalls, finishedCalls, jsonCalls }), clock: value => { now = value; },
    run() { observed = context.run(page, action, name => steps.push(name), (_label, start) => start(), options.defaultMode?undefined:{userID:uid,sessionID:sid,...(options.checking?{stage:'same-session-checking'}:{})}); observed.catch(() => {}); return observed; },
    fire(delay) { now+=delay; const found = [...timers].find(([, t]) => t.delay === delay); assert(found, 'missing timer ' + delay); timers.delete(found[0]); found[1].callback(); },
    goodBody() { body.resolve(options.body ?? { user: { id: uid, role: 'user' }, session: { id: sid } }); },
    failEvent() { failure = { errorText: 'net::ERR_ABORTED' }; page.emit('requestfailed', request); cdp.emit('Network.loadingFailed',{requestId:'private-cdp',canceled:true,errorText:'net::ERR_ABORTED'}); },
    ownerArtifact: () => writes.findLast(w=>w.path.endsWith(options.checking?'/authority-checking-session-consumption.json':'/authority-credential-session-consumption.json'))?.row,
    artifact: () => writes.findLast(w => w.path.endsWith('/authority-session-response-diagnostic.json'))?.row,
    async retire() { options.evalHung=false;evalHeld.resolve(null);detachHeld.resolve();finish.resolve(null); body.resolve({}); waiting.resolve(response); await flush(); assert.equal(timers.size, 0); assert.equal(page.eventNames().length, 0); assert.equal(browserContext.eventNames().length, 0); assert.equal(cdp.eventNames().length,0); },
  };
  fixtures.push(result); return result;
}

async function test(name,work){await work();rows.push(name)}
async function ready(f){await flush();if([...f.timers.values()].some(t=>t.delay===250))f.fire(250);await flush()}
(async()=>{
 for(const scenario of ['baseline','duplicate-during-detach','cdp-duplicate-during-detach','close-during-detach','deadline-during-detach','failed-during-detach']) await test('retirement observes final '+scenario,async()=>{
  const f=fixture(),detached=deferred();let detaching=false;
  f.cdp.detach=()=>{detaching=true;return detached.promise};
  const pending=f.run();await ready(f);assert(detaching);assert(f.page.listenerCount('request')>0);assert(f.cdp.listenerCount('Network.requestWillBeSent')>0);
  if(scenario==='duplicate-during-detach')f.page.emit('request',{...f.request});
  if(scenario==='cdp-duplicate-during-detach'){f.cdp.emit('Network.requestWillBeSent',{requestId:'second-cdp',request:{url:f.request.url(),method:'GET'}});f.cdp.emit('Network.responseReceived',{requestId:'second-cdp',response:{headers:{'X-Request-ID':'private-id-canary'}}});}
  if(scenario==='close-during-detach')f.page.emit('close');
  if(scenario==='deadline-during-detach')f.clock(6000);
  if(scenario==='failed-during-detach')f.failEvent();
  detached.resolve();const accepted=await pending.then(()=>true,()=>false);assert.equal(accepted,scenario==='baseline'||scenario==='failed-during-detach');
  const e=f.ownerArtifact();assert.equal(e.pw_target_requests,scenario==='duplicate-during-detach'?2:1);assert.equal(e.cdp_candidates,scenario==='cdp-duplicate-during-detach'?2:1);
  if(scenario==='failed-during-detach'){assert.equal(e.pw_failed_event,true);assert.equal(e.cdp.aborted,true);assert.equal(e.pw_finished_event,false);}
  await f.retire();
 });

 let accepted;
 await test('same-producer completion permits business while original PW/CDP remain failed and no PW JSON read',async()=>{const f=fixture(),p=f.run();await flush();f.failEvent();await ready(f);const r=await p;assert.equal(r.role,'user');assert.deepEqual(f.counters(),{actionCalls:1,finishedCalls:1,jsonCalls:0});const a=f.ownerArtifact();assert(a.consumer_gate.accepted&&a.pw_failed_event&&!a.pw_finished_event);assert(a.cdp.aborted&&a.cdp.canceled&&a.observers_retired&&a.browser_observers_retired);accepted={evidence:a,identity:r};assert(!JSON.stringify(f.writes).includes('canary'));await f.retire()});
 await test('held stage uses its own artifact and same actual safe role',async()=>{const f=fixture({checking:true}),p=f.run();await ready(f);await p;assert(f.ownerArtifact().consumer_gate.accepted);assert(!f.writes.some(w=>w.path.endsWith('/authority-credential-session-consumption.json')));await f.retire()});
 await test('default Session helper still requires original finished then JSON',async()=>{const f=fixture({defaultMode:true}),p=f.run();await flush();assert.deepEqual(f.counters(),{actionCalls:1,finishedCalls:1,jsonCalls:0});f.finish.resolve(null);await flush();f.goodBody();await p;assert.deepEqual(f.counters(),{actionCalls:1,finishedCalls:1,jsonCalls:1});assert.equal(f.ownerArtifact(),undefined);await f.retire()});
 for(const options of [{wrongCDP:true},{duplicateCDP:true},{duplicate:true},{preissued:true},{origin:'https://other.invalid'},{query:'?unrelated=1'},{missingEnd:true},{keepOwner:true},{keepNative:true},{identity:{role:'root'}},{identity:{userID:'01900000-0000-7000-8000-000000000099'}}])await test('final binding/retirement rejection '+JSON.stringify(options),async()=>{const f=fixture(options),p=f.run();await ready(f);await assert.rejects(p,/CONSUMPTION_INCOMPLETE/);assert.equal(f.ownerArtifact()?.consumer_gate?.accepted,false);await f.retire()});
 for(const options of [{native:{reader_cancel_settled:0}},{native:{stream_cancel_rejected:1}},{native:{release_successes:0}},{native:{eof_before_interruption:false}},{native:{signal_aborted:true}},{owner:{not_busy:false}},{owner:{restore_settled:false}},{owner:{role_valid:false,published_role:null}},{owner:{owned_restore_calls:0}}])await test('incomplete source cannot consume extra time '+JSON.stringify(options),async()=>{const f=fixture(options),p=f.run();await ready(f);f.clock(6000);f.fire(4750);await assert.rejects(p,/CONSUMPTION_TIMEOUT/);await f.retire()});
 for(const code of ['expired','assets-unobserved','observer-present','module-unavailable','singleton-unavailable','owner-unready','native-unavailable'])await test('unarmed '+code+' never defaults',async()=>{const f=fixture({beginResult:code}),p=f.run();await flush();await assert.rejects(p,/CONSUMPTION_UNAVAILABLE/);await f.retire()});
 await test('equal deadline cannot pass even with evidence becoming ready',async()=>{const f=fixture(),p=f.run();await flush();f.clock(5750);f.fire(250);await assert.rejects(p,/CONSUMPTION_INCOMPLETE/);assert.equal(f.ownerArtifact().consumer_gate.within_header_deadline,false);await f.retire()});
 await test('page close during observation rejects complete source',async()=>{const f=fixture(),p=f.run();await flush();f.page.emit('close');await ready(f);await assert.rejects(p,/CONSUMPTION_INCOMPLETE/);await f.retire()});
 await test('CDP detach that has not actually joined cannot be accepted',async()=>{const f=fixture({detachHung:true}),p=f.run();await ready(f);f.fire(250);await assert.rejects(p,/CONSUMPTION_INCOMPLETE/);await f.retire()});
 await test('unjoined original evaluate cannot overlap end or borrow snapshot',async()=>{const f=fixture({evalHung:true}),p=f.run();await flush();f.fire(5000);await flush();f.fire(250);await assert.rejects(p,/CONSUMPTION_TIMEOUT/);assert.equal(f.probeCalls.filter(x=>x[0]==='end').length,0);await f.retire()});
 const scope=fixture().context;
 const clone=()=>structuredClone(accepted), expected={userID:accepted.identity.userID,sessionID:accepted.identity.sessionID};
 assert(scope.gate(accepted,expected,100,99));
 const changes=[
 ...['requests','readers','reader_cancel_calls','reader_cancel_settled','stream_cancel_calls','stream_cancel_settled','release_calls','release_successes'].map(k=>['native '+k,x=>x.evidence.native[k]=0]),
 ...['read_rejected','reader_cancel_rejected','stream_cancel_rejected','abort_events','read_rejected_order','abort_order'].map(k=>['native '+k,x=>x.evidence.native[k]=1]),
 ...['headers_seen','status_ok','read_done','request_id_match','content_length_present','content_length_valid','content_encoding_identity','content_length_comparable','content_length_matches_eof','eof_before_interruption'].map(k=>['native '+k,x=>x.evidence.native[k]=false]),
 ['cancel-before-done',x=>{x.evidence.native.cancel_before_eof=true;x.evidence.native.reader_cancel_order=1}],
 ['unsettled read',x=>x.evidence.native.read_settled=1],['wrong bytes',x=>x.evidence.native.bytes=482],['signal at start',x=>x.evidence.native.signal_aborted_at_start=true],['signal current',x=>x.evidence.native.signal_aborted=true],['bad release order',x=>x.evidence.native.release_order=2],['observer error',x=>x.evidence.native.failure='observer-error'],
 ...['action_calls','restore_calls','owned_restore_calls','session_requests','owned_session_requests','response_headers'].map(k=>['owner '+k,x=>x.evidence.restore_owner[k]=2]),
 ...['restore_settled','entry_authenticated','entry_not_busy','entry_user_matches','entry_session_matches','authenticated','not_busy','user_matches','session_matches','role_valid','request_id_match','hooks_retired'].map(k=>['owner '+k,x=>x.evidence.restore_owner[k]=false]),
 ...['restore_rejected','restore_threw','observer_failed'].map(k=>['owner '+k,x=>x.evidence.restore_owner[k]=true]),
 ['pending observer',x=>x.evidence.restore_owner.pending_observations=1],['malformed role',x=>x.evidence.restore_owner.published_role='root'],['private role mismatch',x=>x.identity.role='admin'],
 ...['pw_selected_target_match','pw_request_match','pw_request_id_seen','snapshot_selected_bound','slot_end_observed','sample_joined','cdp_ready','cdp_selected_bound','observers_retired','browser_observers_retired'].map(k=>[k,x=>x.evidence[k]=false]),
 ['wrong source',x=>x.evidence.snapshot_source='sample'],['wrong status',x=>x.evidence.pw_status=403],['unsettled sampling',x=>x.evidence.sample_settled=0],['sampling failure',x=>x.evidence.sample_failed=1],['join unavailable',x=>x.evidence.sample_join_unavailable=true],['CDP cap',x=>x.evidence.cdp_cap_exceeded=true],['context closed',x=>x.evidence.timing.context_close_notification={order:99,elapsed_ms:1}],
 ];
 for(const [name,change] of changes)await test('explicit conjunction rejects '+name,()=>{const x=clone();change(x);assert.equal(scope.gate(x,expected,100,99),null)});
 await test('closed predicate rejects equality and lateness regardless cached complete flag',()=>{assert.equal(scope.gate(accepted,expected,100,100),null);assert.equal(scope.gate(accepted,expected,100,101),null);const x=clone();x.evidence.consumption_publication_evidence_complete=true;x.evidence.native.read_rejected=1;assert.equal(scope.gate(x,expected,100,99),null)});
 await flush();assert.equal(unhandled.length,0);assert.equal(fs.readFileSync(root+'/'+file,'utf8'),source);const result={passed:rows.length,cases:rows,actual_source:true,unhandled:0,network:false,browser:false};fs.writeFileSync(root+'/output/ai/model-ui-recovery/consumer-method-controls/adapter-result.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result));
})().catch(e=>{console.error(e);process.exitCode=1});
