// Reuse the frozen actual-product/jsdom fixture, but execute the new observer
// after the exact locked Playwright transform. No browser/socket/network.
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../..');
const fixtureFile = path.join(__dirname, 'resolve-publication-controls.cjs');
let source = fs.readFileSync(fixtureFile, 'utf8');
const replace = (old, next) => {
  if (source.split(old).length !== 2) throw Error('controlled fixture location changed');
  source = source.replace(old, next);
};
replace("const { JSDOM }", `
const ts = require(root + '/web/node_modules/typescript');
process.env.PWTEST_CACHE_DIR = root + '/output/ai/model-ui-recovery/resolve-publication-observer-controls/pw-cache';
const pw = require(root + '/tests/account-captcha-web/node_modules/playwright/lib/transform/transform.js');
const observerFile = root + '/.agent-state/model-ui-recovery/resolve-publication-observer.ts';
const observerSource = fs.readFileSync(observerFile, 'utf8');
const transformed = pw.transformHook(observerSource, observerFile).code;
const transformedTree = ts.createSourceFile('observer.js', transformed, ts.ScriptTarget.Latest, true);
const transformedFunctions = ['installResolvePublicationObservation', 'resolvePublicationProjection'].map(name => {
 const fn = transformedTree.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name);
 assert(fn); return fn.getText(transformedTree);
}).join('\\n');
const { JSDOM }`);
replace("const a = w.ActualResolve;", `const a = w.ActualResolve;
    vm.runInContext(transformedFunctions, dom.getInternalVMContext());`);
const begin = source.indexOf('  for (const status of [404, 409]) await test(');
const end = source.indexOf('  await drain(); assert.equal(unhandled.length, 0);', begin);
if (begin < 0 || end < 0) throw Error('controlled test boundary changed');
source = source.slice(0, begin) + String.raw`
  const actualBinding = await (await import(root + '/.agent-state/model-ui-recovery/session-controller-binding.mjs')).resolveOwnerModule(root);
  assert(actualBinding.failure_export_name && actualBinding.asset && actualBinding.entry);
  async function arm(x, options = {}) {
    x.w.__projectModelsProbe = { resolveBegin: x.native.begin, resolveSnapshot: x.native.snapshot, resolveEnd: x.native.end };
    x.w.performance.getEntriesByName = () => options.notLoaded ? [] : [{}];
    const module = { singleton: () => x.auth, Failure: options.fakeFailure ? Error : x.a.AccountFailure };
    x.w.Function = function() { return async () => module; };
    const beforeFetch = x.w.fetch, original = x.auth.projects.resolve;
    let lastPromise, lastReceiver, lastArgs;
    const spy = function(...args) {
      lastReceiver = this; lastArgs = args;
      if (options.syncThrow) throw options.syncThrow;
      lastPromise = Reflect.apply(original, this, args);
      if (options.onRejected) void lastPromise.then(() => {}, options.onRejected).catch(error => unhandled.push(error));
      return lastPromise;
    };
    x.auth.projects.resolve = spy;
    const installed = await x.w.installResolvePublicationObservation({ binding: { entry: '/assets/entry.js', asset: '/assets/owner.js', export_name: 'singleton', failure_export_name: 'Failure' }, target, slot: 'resolve-offline', expiresAt: Date.now() + (options.expired ? -1 : 500) });
    assert.equal(x.w.fetch, beforeFetch);
    return { installed,
      snapshot(expected = id(99)) { return x.w.__authorityResolvePublication?.snapshot(expected) ?? null; },
      finish(expected = id(99)) { const result = x.w.__authorityResolvePublication?.finish(expected) ?? null; assert.equal(x.auth.projects.resolve, spy); assert.equal(x.w.fetch, beforeFetch); return result; },
      check(promise, args) { assert.equal(promise, lastPromise); assert.equal(lastReceiver, x.auth.projects); assert.deepEqual(lastArgs, args); },
    };
  }
  async function controlled(options, work) {
    const x = await setup(options);
    try { await work(x); }
    finally { x.w.__authorityResolvePublication?.finish(null); await x.cleanup(); }
  }
  const evidence = (x, row) => {
    assert(x.w.resolvePublicationProjection(row));
    assert.equal(JSON.stringify(row).includes(session.user.id), false);
    assert.equal(JSON.stringify(row).includes('private'), false);
  };
  for (const status of [404, 409]) await test('actual PW-transformed observer captures original ' + status + ' consumption and new public DOM', () => controlled({ status }, async x => {
    const o = await arm(x); assert.equal(o.installed, true); x.navigate(); await drain(); x.release(); await drain();
    const row = o.finish(); evidence(x, row);
    assert(row.typed_problem && row.problem_request_id_matches && row.problem_instance_matches && row.loading_dom_observed && row.error_dom_after_rejection);
    assert(row.entry_identity_matches && row.identity_current && row.authenticated && row.not_busy && row.hooks_retired);
    assert.equal(row.resolve_calls, 1); assert.equal(row.target_calls, 1); assert.equal(row.rejected, 1); assert.equal(row.pending_observations, 0);
    assert(row.timing.call <= row.timing.loading_dom && row.timing.loading_dom <= row.timing.rejected && row.timing.rejected <= row.timing.error_dom);
    assert.equal(row.error_heading_visible, false); // jsdom has no browser layout.
    const native = x.nativeFacts(); assert(native.eof_before_interruption && native.content_length_matches_eof && native.request_id_match);
  }));
  await test('same-target old heading plus valid original Promise cannot manufacture loading/publication', () => controlled({}, async x => {
    x.navigate(); await drain(); x.release(); await drain();
    const o = await arm(x); const args = [{ ...target }], pending = x.auth.projects.resolve(...args); o.check(pending, args);
    const settled = pending.catch(() => {}); await drain(); x.release(); await settled; await drain(); const row = o.finish(); evidence(x, row);
    assert(row.old_error_heading_present && row.typed_problem && row.problem_request_id_matches); assert.equal(row.loading_dom_observed, false); assert.equal(row.error_dom_after_rejection, false);
  }));
  await test('pre-existing actual inactive/loading DOM cannot be relabeled as a new loading transition', () => controlled({}, async x => {
    x.navigate(); await drain(); x.release(); await drain(); x.workspace.dispose(); await drain();
    const o = await arm(x), pending = x.auth.projects.resolve({ ...target }).catch(() => {}); await drain();
    const middle = o.snapshot(); assert(middle.old_loading_dom_present); assert.equal(middle.loading_dom_observed, false);
    x.release(); await pending; await drain(); const row = o.finish(); evidence(x, row); assert(row.typed_problem); assert.equal(row.loading_dom_observed, false); assert.equal(row.error_dom_after_rejection, false);
  }));
  for (const name of ['navigation', 'identity', 'dispose']) await test('original rejection after public ' + name + ' invalidation does not report published DOM', () => controlled({}, async x => {
    const o = await arm(x, { onRejected() { if (name === 'navigation') x.navigate('/elsewhere'); else if (name === 'identity') x.auth.leave(); else x.workspace.dispose(); } });
    x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(); evidence(x, row); assert(row.typed_problem); assert.equal(row.error_dom_after_rejection, false);
  }));
  await test('complete EOF and read-error from malformed Problem remain distinct from typed rejection', () => controlled({ badProblem: true, status: 409 }, async x => {
    const o = await arm(x); x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(); evidence(x, row); assert.equal(row.typed_problem, false); assert.equal(row.error_dom_after_rejection, false); assert(row.error_heading_present);
  }));
  await test('selected wrong ID does not bind a real typed error', () => controlled({}, async x => {
    const o = await arm(x); x.navigate(); await drain(); x.release(); await drain(); const row = o.finish(id(88)); evidence(x, row); assert(row.typed_problem); assert.equal(row.problem_request_id_matches, false);
  }));
  await test('second exact target resolve is counted and cannot borrow one typed Problem binding', () => controlled({}, async x => {
    const o = await arm(x); x.navigate(); await drain(); const other = x.auth.projects.resolve({ ...target }).catch(() => {}); await other; x.release(); await drain(); const row = o.finish(); evidence(x, row); assert.equal(row.resolve_calls, 2); assert.equal(row.target_calls, 2); assert.equal(row.typed_problem, false); assert.equal(row.error_dom_after_rejection, false);
  }));
  await test('finish with outstanding original Promise restores method and preserves incomplete final facts', () => controlled({}, async x => {
    const o = await arm(x); x.navigate(); await drain(); const row = o.finish(); evidence(x, row); assert.equal(row.pending_observations, 1); assert.equal(row.rejected, 0); x.release(); await drain(); assert.equal(row.rejected, 0); assert.equal(row.pending_observations, 1); assert.equal(x.w.__authorityResolvePublication, undefined);
  }));
  await test('synchronous original throw preserves exact error identity and restores method', () => controlled({}, async x => {
    const error = Error('secret-error-canary'), o = await arm(x, { syncThrow: error });
    assert.throws(() => x.auth.projects.resolve({ ...target }), value => value === error); const row = o.finish(); evidence(x, row); assert.equal(row.synchronous_throws, 1); assert.equal(row.pending_observations, 0); assert(!JSON.stringify(row).includes('canary'));
  }));
  for (const [options, expected] of [[{ expired: true }, 'expired'], [{ notLoaded: true }, 'assets-unobserved'], [{ fakeFailure: true }, 'failure-type-unavailable']]) await test('installation fails closed: ' + expected, () => controlled({}, async x => {
    const o = await arm(x, options); assert.equal(o.installed, expected); assert.equal(x.w.__authorityResolvePublication, undefined); assert.equal(x.requests, 0); o.finish();
  }));
  await test('safe projection rejects raw extra properties and invalid timing', () => controlled({}, async x => {
    const o = await arm(x); const row = o.finish(); evidence(x, row); assert.equal(x.w.resolvePublicationProjection({ ...row, raw: 'secret' }), null); assert.equal(x.w.resolvePublicationProjection({ ...row, timing: { ...row.timing, call: -1 } }), null);
  }));
  assert.equal(fs.readFileSync(observerFile, 'utf8'), observerSource);
` + source.slice(end);
replace("const output = root + '/output/ai/model-ui-recovery/resolve-publication-controls';", "const output = root + '/output/ai/model-ui-recovery/resolve-publication-observer-controls';");
replace('public_mutation_observer: true,', 'public_mutation_observer: true, actual_playwright_transform: true, no_fetch_wrapper: true,');
new Function('require', '__dirname', source)(require, __dirname);
