// Actual Node adapter/contract with explicit page/native/publication doubles.
// The final lifecycle test invokes the locked PW public finished implementation.
const fs = require('node:fs'), path = require('node:path');
let source = fs.readFileSync(path.join(__dirname, 'resolve-publication-adapter-controls.cjs'), 'utf8');
const replace = (old, next) => { if (source.split(old).length !== 2) throw Error('strict adapter fixture location changed'); source = source.replace(old, next); };
replace("const id = '01900000", `const contract = { exports: {} };
new Function('exports', ts.transpileModule(fs.readFileSync(path.join(__dirname, 'resolve-rejection-contract.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2024, module: ts.ModuleKind.CommonJS } }).outputText)(contract.exports);
const id = '01900000`);
replace('  const probe = {', '  let retained, ownerRetained, lateEndResolve;\n  const probe = {');
replace('    resolveEnd(slot, expectedID)', `    resolveRetire(slot, expectedID) { retained = this.resolveSnapshot(slot, expectedID); ended = true; return { native: retained, hooks_retired: !options.badNativeHooks }; },
    resolveRetirement(slot, expectedID, forget) { if (!retained) return null; const native = { ...retained, ...(options.finalNative ?? {}) }; if (forget) retained = null; return { native, hooks_retired: !options.badNativeHooks }; },
    resolveEnd(slot, expectedID)`);
replace("finish(expectedID) { const value = { ...this.snapshot(expectedID), hooks_retired: true }; delete host.__authorityResolvePublication; return value; },", `finish(expectedID, retain) { const value = { ...this.snapshot(expectedID), hooks_retired: !options.badOwnerHooks }; delete host.__authorityResolvePublication;
      if (retain) host.__authorityResolveRetired = { snapshot(_id, forget) { const result = { ...value, ...(options.finalOwner ?? {}) }; if (forget) delete host.__authorityResolveRetired; return result; } };
      return value;
    },`);
replace('const context = { URL, Promise, Error, Date,', "const context = { ...contract.exports, readFileSync: () => fs.readFileSync(root + '/api/openapi/common.json', 'utf8'), URL, Promise, Error, Date,");
replace("const names = ['need',", "const names = ['need', 'object',");
replace("    return vm.runInNewContext('(' + callback.toString() + ')(args)', { window: host, args });", `    const value = vm.runInNewContext('(' + callback.toString() + ')(args)', { window: host, args });
    if (ended && host.__authorityResolveRetired && options.lateEnd) return new Promise(resolve => { lateEndResolve = () => resolve(value); });
    if (ended && host.__authorityResolveRetired && options.duplicateAtEnd) page.emit('request', { ...request });
    return value;`);
replace("async begin() { return context.begin(page, { username: 'admin', normalized_name: 'denied' }); },", "async begin() { return context.begin(page, { username: 'admin', normalized_name: 'denied' }, { userID: 'owned-user', sessionID: 'owned-session', role: 'user', status: 404, code: 'NOT_FOUND' }); },");
replace("page.emit('request', request); diagnostic.select(response);", "page.emit('request', request); if (!options.missingHeaders) page.emit('response', response); diagnostic.select(response);");
replace('return { page, browser, request, response, publication, writes,', `return { page, browser, request, response, publication, writes,
    advance(ms) { now += ms; }, lateEnd() { lateEndResolve?.(); },
    async timers(delay) { const matches = [...timers].filter(([, row]) => row.delay <= delay); for (const [id, timer] of matches) { timers.delete(id); timer.callback(); } await flush(); },
    consumption() { return writes.findLast(row => row.name.includes('authority-resolve-consumption-'))?.value; },`);
const begin = source.indexOf("  await test('sampled original typed/public DOM");
const end = source.indexOf('  await flush(); assert.equal(unhandled.length, 0);', begin);
if (begin < 0 || end < 0) throw Error('strict adapter cases moved');
source = source.slice(0, begin) + String.raw`
  await test('same original request has complete end/retirement within original headers budget while PW remains pending', async () => {
    const f = fixture(), d = await f.begin(); await f.sample(d); const original = f.original(d); let settled = false; void original.then(() => { settled = true; });
    assert.equal(await d.resolveReady(), 'consumed'); await d.acceptResolve(); const e = f.consumption(); assert.equal(e.consumer_gate.accepted, true); assert.equal(settled, false); assert.equal(e.pw_finished_event, false); assert.equal(e.pw_failure, 'aborted'); assert(e.resolve_final_counters_stable && e.resolve_listeners_retired); await f.retired();
  });
  for (const [name, options] of [
    ['native hook mismatch', { badNativeHooks: true }], ['public hook mismatch', { badOwnerHooks: true }],
    ['late native read', { finalNative: { read_calls: 3, read_settled: 3 } }],
    ['late other-target request after end', { duplicateAtEnd: true }],
    ['late public rejection', { finalOwner: { late_events: 1 } }],
    ['changed final identity', { finalOwner: { identity_current: false } }],
    ['changed final role', { finalOwner: { current_role_matches: false } }],
    ['changed final URL', { finalOwner: { target_url_current: false } }],
    ['final owner still busy', { finalOwner: { not_busy: false } }],
  ]) await test('final retirement rejects ' + name, async () => {
    const f = fixture(options), d = await f.begin(); await f.sample(d); f.original(d); assert.equal(await d.resolveReady(), 'consumed');
    await assert.rejects(d.acceptResolve(), /PROJECT_MODELS_RESOLVE_CONSUMPTION_INCOMPLETE/); assert.equal(f.consumption().consumer_gate.accepted, false); await f.retired();
  });
  await test('late header deadline cannot be upgraded by complete cached observation', async () => {
    const f = fixture(), d = await f.begin(); await f.sample(d); f.original(d); f.advance(5000); await assert.rejects(d.resolveReady(), /TIMEOUT/); await assert.rejects(d.acceptResolve(), /INCOMPLETE/); await f.retired();
  });
  await test('missing actual header event cannot acquire a fresh five-second allowance at select', async () => {
    const f = fixture({ missingHeaders: true }), d = await f.begin(); await f.sample(d); f.original(d); await assert.rejects(d.resolveReady(), /UNAVAILABLE/); await d.finish(true); await f.retired();
  });
  await test('original end evaluate after bounded wait cannot impersonate joined end or run a concurrent final sample', async () => {
    const f = fixture({ lateEnd: true }), d = await f.begin(); await f.sample(d); f.original(d);
    const accepting = d.acceptResolve(); const observed = accepting.catch(() => {}); await flush(); await f.timers(250); await f.timers(250); await observed;
    assert.equal(f.consumption().consumer_gate.accepted, false); assert.equal(f.consumption().slot_end_observed, false); f.lateEnd(); await flush(); assert.equal(f.consumption().slot_end_observed, false); await f.retired();
  });
  await test('strict safe contract rejects every incomplete fact and deadline equality', async () => {
    const f = fixture(), d = await f.begin(); await f.sample(d); f.original(d); await d.acceptResolve(); const e = f.consumption(); const gate = row => contract.exports.acceptedResolveRejection(row, 404, 5000, 1000); assert(gate(e));
    for (const key of ['pw_selected_target_match','pw_request_match','pw_request_id_seen','snapshot_selected_bound','slot_end_observed','sample_joined','resolve_publication_selected_bound','resolve_publication_observers_retired','resolve_listeners_retired','resolve_final_counters_stable']) assert.equal(gate({ ...e, [key]:false }), false, key);
    for (const key of ['typed_problem','problem_schema_valid','problem_tuple_matches','problem_instance_matches','problem_request_id_matches','entry_authenticated','entry_not_busy','entry_identity_matches','rejection_authenticated','rejection_not_busy','rejection_identity_matches','rejection_role_matches','current_role_matches','target_url_at_call','target_url_current','identity_current','authenticated','not_busy','loading_dom_observed','error_dom_after_rejection','error_heading_visible','zero_provider_lists','zero_dialogs','hooks_retired']) assert.equal(gate({ ...e, resolve_publication:{ ...e.resolve_publication,[key]:false } }), false, key);
    for (const key of ['reader_cancel_rejected','stream_cancel_rejected','read_rejected','abort_events']) assert.equal(gate({ ...e, native:{ ...e.native,[key]:1 } }), false, key);
    for (const key of ['left_target','observer_failed','lifetime_expired','old_loading_dom_present']) assert.equal(gate({ ...e, resolve_publication:{ ...e.resolve_publication,[key]:true } }), false, key);
    assert.equal(contract.exports.acceptedResolveRejection(e,404,1000,1000),false); assert.equal(contract.exports.acceptedResolveRejection(e,404,1000,1001),false); await f.retired();
  });
  await test('locked PW original finished joins only after controlled target close; underlying network promise remains pending', async () => {
    const { Response } = require(root + '/tests/account-captcha-web/node_modules/playwright-core/lib/client/network.js');
    const { ManualPromise, LongStandingScope } = require(root + '/tests/account-captcha-web/node_modules/playwright-core/lib/utils/isomorphic/manualPromise.js');
    const scope = new LongStandingScope(), network = new ManualPromise(); let closed = false, closeCalls = 0;
    const page = { isClosed: () => closed, async close() { closeCalls++; closed = true; scope.close(Error('controlled-close')); } };
    const original = Response.prototype.finished.call({ request: () => ({ _targetClosedScope: () => scope }), _finishedPromise: network });
    const lifetime = contract.exports.resolveFinishedLifetime(page); assert.equal(lifetime.register(original),original); assert.throws(() => lifetime.register(original), /REGISTRATION/);
    await flush(); assert.equal(lifetime.facts().all_original_promises_joined,false); assert.equal(lifetime.facts().outcomes[0].result,'pending');
    await lifetime.closeAndJoin(); await lifetime.closeAndJoin(); assert.equal(closeCalls,1); assert.equal(network.isDone(),false); assert.deepEqual(lifetime.facts(),{registered:1,page_closed:true,all_original_promises_joined:true,outcomes:[{settled:true,result:'rejected',after_close:true}]});
  });
  await test('failed close and unjoined original Promise cannot report completed retirement', async () => {
    const never = new Promise(() => {}); let closed = false, release; const original = new Promise(r => { release = r; });
    const failed = contract.exports.resolveFinishedLifetime({ isClosed:()=>false, close: async()=>{} }); failed.register(never); await assert.rejects(failed.closeAndJoin(), /PAGE_CLOSE/); assert.equal(failed.facts().all_original_promises_joined,false);
    const delayed = contract.exports.resolveFinishedLifetime({ isClosed:()=>closed, close:async()=>{closed=true;} }); delayed.register(original); let done=false; const joined=delayed.closeAndJoin().then(()=>{done=true;}); await flush(); assert.equal(done,false); assert.equal(delayed.facts().all_original_promises_joined,false); release(null); await joined; assert.equal(done,true);
  });
` + source.slice(end);
replace("'/output/ai/model-ui-recovery/resolve-publication-observer-controls'", "'/output/ai/model-ui-recovery/resolve-rejection-adapter-controls'");
replace('original_session_gate_and_resolve_postconditions_unchanged: true,', 'original_session_gate_and_resolve_postconditions_unchanged: true, controlled_target_close: true, actual_locked_pw_finished: true,');
new Function('require', '__dirname', source)(require, __dirname);
