// Actual controller/client/workspace/View/native probe and PW-transformed observer.
// Transport, expiry clock and layout are explicit doubles. No real page/socket.
const fs = require('node:fs'), path = require('node:path');
let program = fs.readFileSync(path.join(__dirname, 'resolve-publication-observer-controls.cjs'), 'utf8');
function change(old, next) { if (program.split(old).length !== 2) throw Error('strict control fixture location changed'); program = program.replace(old, next); }
change("const begin = source.indexOf(", String.raw`
replace("const pending = [], witnesses = [];", "const pending = [], witnesses = [], tails = []; let responseWitness, readerWitness, expiry; const originalTimer = w.setTimeout.bind(w); w.setTimeout = (fn, ms, ...args) => { const timer = originalTimer(fn, ms, ...args); if (ms === 30000) expiry = () => { w.clearTimeout(timer); fn(...args); }; return timer; }; ");
replace("if (options.badProblem) problem.request_id", "options.mutateProblem?.(problem); if (options.status === 409 && !boundaryInput && !options.mutateProblem) problem.code = 'PROJECT_NOT_ACTIVE'; if (options.badProblem) problem.request_id");
replace("resolve(new Response(bytes, { status, headers: { 'content-type': 'application/problem+json', 'content-length': String(bytes.byteLength), 'x-request-id': id(99) } }));", "const response = new Response(bytes, { status, headers: { 'content-type': options.media ?? 'application/problem+json', 'content-length': String(bytes.byteLength + (options.lengthDelta ?? 0)), 'x-request-id': id(99) } }); const cancel = response.body.cancel; response.body.cancel = function(...args) { const actual = Reflect.apply(cancel, this, args); if (options.cancelReject) return actual.then(() => { throw Error('private-cancel-canary'); }); if (options.holdCancel) return actual.then(() => new Promise(r => tails.push(r))); return actual; }; const reader = response.body.getReader; response.body.getReader = function(...args) { const r = Reflect.apply(reader, this, args); readerWitness = r; return r; }; responseWitness = { response, cancel: response.body.cancel, getReader: response.body.getReader }; resolve(response);");
replace("return { w, a, auth, workspace, native, get requests()", "return { w, a, auth, workspace, native, witness: () => responseWitness, reader: () => readerWitness, tail: () => { assert.equal(tails.length, 1); tails.shift()(); }, expire: () => { assert(expiry); expiry(); }, get requests()");
replace("async cleanup() { while (pending.length) pending.shift()(); await drain();", "async cleanup() { while (pending.length) pending.shift()(); await drain(); while (tails.length) tails.shift()(); await drain(); native.retirement('resolve-offline', id(99), true);");
const begin = source.indexOf(`);
change("expiresAt: Date.now() + (options.expired ? -1 : 500) });", "expiresAt: Date.now() + (options.expired ? -1 : 500), expected: { userID: session.user.id, sessionID: session.session.id, role: options.expectedRole ?? 'user', status: options.status ?? 404, code: options.status === 409 ? 'PROJECT_NOT_ACTIVE' : 'NOT_FOUND' }, schemas: options.schemas ?? JSON.parse(fs.readFileSync(root + '/api/openapi/common.json')).components.schemas });");
const start = program.indexOf("  for (const status of [404, 409]) await test('actual PW-transformed observer");
const end = program.indexOf("  assert.equal(fs.readFileSync(observerFile", start);
if (start < 0 || end < 0) throw Error('strict case boundary changed');
program = program.slice(0, start) + String.raw`
  const contractFile = root + '/.agent-state/model-ui-recovery/resolve-rejection-contract.ts';
  const contract = { exports: {} }; new Function('exports', ts.transpileModule(fs.readFileSync(contractFile, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2024, module: ts.ModuleKind.CommonJS } }).outputText)(contract.exports);
  const ready = contract.exports.resolveRejectionReady;
  const layoutDouble = x => { x.w.HTMLElement.prototype.getClientRects = () => [{ width: 20, height: 20 }]; };
  for (const status of [404, 409]) await test('strict actual typed rejection/publication and descriptor retirement ' + status, () => controlled({ status }, async x => {
    layoutDouble(x); const o = await arm(x, { status }); assert.equal(o.installed, true);
    x.navigate(); await drain(); x.release(); await drain();
    const before = o.snapshot(), native = x.nativeFacts(); evidence(x, before);
    assert(ready(native, before, status), JSON.stringify({ native, before }));
    assert(before.problem_schema_valid && before.problem_tuple_matches && before.rejection_not_busy && before.rejection_identity_matches && before.rejection_role_matches);
    const retired = x.native.retire('resolve-offline', id(99)); const owner = x.w.__authorityResolvePublication.finish(id(99), true);
    assert(retired.hooks_retired && owner.hooks_retired); assert.equal(x.native.snapshot('resolve-offline', id(99)), null);
    const witness = x.witness(); assert.equal(witness.response.body.cancel, witness.cancel); assert.equal(witness.response.body.getReader, witness.getReader);
    await drain(); const last = x.native.retirement('resolve-offline', id(99), true), final = x.w.__authorityResolveRetired.snapshot(id(99), true);
    assert.deepEqual(last.native, retired.native); assert(ready(last.native, final, status)); assert.equal(x.w.__authorityResolveRetired, undefined);
  }));
  for (const [name, options, expected] of [
    ['swallowed outer cancel rejection', { cancelReject: true }, row => row.typed_problem && row.rejection_not_busy],
    ['unknown formal code accepted by client grammar', { mutateProblem: p => { p.code = 'UNKNOWN_CONTROL_CODE'; } }, row => row.typed_problem && !row.problem_schema_valid],
    ['wrong closed tuple', { status: 409, mutateProblem: p => { p.code = 'INVALID_STATE'; } }, row => row.problem_schema_valid && !row.problem_tuple_matches],
    ['wrong body length', { lengthDelta: 1 }, () => true],
    ['wrong media', { media: 'application/json' }, row => !row.typed_problem],
    ['query instance', { problemInstance: '/api/v1?private=canary' }, row => !row.typed_problem],
  ]) await test('strict candidate rejects ' + name, () => controlled(options, async x => {
    layoutDouble(x); const o = await arm(x, { status: options.status }); x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(); evidence(x, row); assert(expected(row)); assert.equal(ready(x.nativeFacts(), row, options.status ?? 404), false);
    if (options.cancelReject) assert.equal(x.nativeFacts().stream_cancel_rejected, 1);
  }));
  for (const mode of ['held', 'abandon', 'expiry']) await test('native EOF cannot upgrade actual unresolved owner ' + mode, () => controlled({ holdCancel: true }, async x => {
    layoutDouble(x); const o = await arm(x); x.navigate(); await drain(); x.release(); await drain(); assert.equal(x.auth.state.busy, true);
    assert(x.nativeFacts().eof_before_interruption); assert.equal(x.nativeFacts().stream_cancel_settled, 0);
    if (mode === 'abandon') x.auth.projects.abandonRead(); if (mode === 'expiry') x.expire(); await drain();
    const before = o.snapshot(); assert.equal(ready(x.nativeFacts(), before, 404), false);
    if (mode !== 'held') { assert.equal(before.rejected, 1); assert.equal(before.rejection_not_busy, false); }
    x.tail(); await drain(); const after = o.finish();
    assert.equal(ready(x.nativeFacts(), after, 404), mode === 'held');
  }));
  for (const mode of ['role', 'identity', 'navigation']) await test('rejection instant public ' + mode + ' must remain bound', () => controlled({}, async x => {
    layoutDouble(x); const o = await arm(x, { onRejected() { if (mode === 'role') { session.user.role = 'admin'; void x.auth.restore(); } else if (mode === 'identity') x.auth.leave(); else x.navigate('/outside'); } });
    x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(); assert.equal(ready(x.nativeFacts(), row, 404), false);
    if (mode === 'role') { session.user.role = 'user'; assert.equal(row.current_role_matches, false); }
  }));
  await test('actual late native wrapper operation changes retained counters after restoration', () => controlled({}, async x => {
    layoutDouble(x); const o = await arm(x); x.navigate(); await drain(); x.release(); await drain(); const lateRead = x.reader().read;
    const before = x.native.retire('resolve-offline', id(99)); o.finish();
    await assert.rejects(lateRead()); await drain(); const after = x.native.retirement('resolve-offline', id(99), true); assert.notDeepEqual(after.native, before.native); assert.equal(after.native.read_calls, before.native.read_calls + 1);
  }));
  await test('late request is counted after slot end without a second forwarding owner', () => controlled({}, async x => {
    const o = await arm(x); x.navigate(); await drain(); x.release(); await drain(); const before = x.native.retire('resolve-offline', id(99)); o.finish();
    const promise = x.auth.projects.resolve({ ...target }).catch(() => {}); await drain(); assert.equal(x.requests, 2); x.release(); await promise; await drain();
    const after = x.native.retirement('resolve-offline', id(99), true); assert.equal(before.native.requests, 1); assert.equal(after.native.requests, 2);
  }));
  await test('late original rejection cannot be hidden by retained owner retirement', () => controlled({ holdCancel: true }, async x => {
    const o = await arm(x); x.navigate(); await drain(); x.release(); await drain();
    const before = x.w.__authorityResolvePublication.finish(id(99), true); assert.equal(before.pending_observations, 1);
    x.tail(); await drain(); const after = x.w.__authorityResolveRetired.snapshot(id(99), true); assert.equal(after.late_events, 1); assert.equal(ready(x.nativeFacts(), after, 404), false);
  }));
  await test('loading beside an unretired old error heading cannot certify fresh publication', () => controlled({}, async x => {
    layoutDouble(x); const o = await arm(x); x.navigate();
    const old = x.w.document.createElement('section'); old.setAttribute('role','alert'); old.innerHTML='<h3>项目不可用</h3>'; x.w.document.body.append(old);
    await drain(); assert.equal(o.snapshot().loading_dom_observed,false); old.remove(); x.release(); await drain(); const row=o.finish(); assert.equal(ready(x.nativeFacts(),row,404),false);
  }));
  await test('changed unsupported formal schema never counts as validated production Problem', () => controlled({}, async x => {
    const schemas = JSON.parse(fs.readFileSync(root + '/api/openapi/common.json')).components.schemas; schemas.Problem.properties.code.unsupportedConstraint = true;
    const o = await arm(x, { schemas }); x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(); assert(row.typed_problem); assert.equal(row.problem_schema_valid, false);
  }));
` + program.slice(end);
change("'/output/ai/model-ui-recovery/resolve-publication-observer-controls'", "'/output/ai/model-ui-recovery/resolve-rejection-controls'");
change('actual_playwright_transform: true, no_fetch_wrapper: true,', 'actual_playwright_transform: true, no_fetch_wrapper: true, layout_double: true, transport_tail_double: true,');
new Function('require', '__dirname', program)(require, __dirname);
