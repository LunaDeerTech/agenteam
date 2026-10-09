// Proposal evidence only: run the actual Session project-read rejection lane.
// The controlled transport holds its outer cancel Promise; the expiry clock is
// an explicit double. No production callback or owner closure is replaced.
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../..');
let source = fs.readFileSync(path.join(__dirname, 'resolve-publication-controls.cjs'), 'utf8');
const replace = (old, next) => {
  if (source.split(old).length !== 2) throw Error('controlled owner fixture location changed');
  source = source.replace(old, next);
};
replace('export { createApp, h, provide, ref, nextTick }', 'export { createApp, h, provide, ref, nextTick, watch }');
replace('const pending = [], witnesses = [];', `const pending = [], witnesses = [], cancelTails = [], expiry = new Map();
    const nativeSetTimeout = w.setTimeout.bind(w), nativeClearTimeout = w.clearTimeout.bind(w);
    w.setTimeout = (callback, delay, ...args) => {
      const timer = nativeSetTimeout(callback, delay, ...args);
      if (delay === 30_000) expiry.set(timer, () => callback(...args));
      return timer;
    };
    w.clearTimeout = timer => { expiry.delete(timer); nativeClearTimeout(timer); };`);
replace("resolve(new Response(bytes, { status, headers: { 'content-type': 'application/problem+json', 'content-length': String(bytes.byteLength), 'x-request-id': id(99) } }));", `const response = new Response(bytes, { status, headers: { 'content-type': 'application/problem+json', 'content-length': String(bytes.byteLength), 'x-request-id': id(99) } });
        const originalCancel = response.body.cancel;
        response.body.cancel = function(...args) {
          const actual = Reflect.apply(originalCancel, this, args);
          return actual.then(() => new Promise(settle => cancelTails.push(settle)));
        };
        resolve(response);`);
replace('return { w, a, auth, workspace, native, get requests()', `return { w, a, auth, workspace, native,
      cancelTailCount() { return cancelTails.length; },
      releaseCancel() { assert.equal(cancelTails.length, 1); cancelTails.shift()(); },
      expire() { assert.equal(expiry.size, 1); const [timer, callback] = [...expiry][0]; nativeClearTimeout(timer); expiry.delete(timer); callback(); },
      get requests()`);
replace('async cleanup() { while (pending.length) pending.shift()(); await drain();', 'async cleanup() { while (pending.length) pending.shift()(); await drain(); while (cancelTails.length) cancelTails.shift()(); await drain();');
const begin = source.indexOf('  for (const status of [404, 409]) await test(');
const end = source.indexOf('  await drain(); assert.equal(unhandled.length, 0);', begin);
if (begin < 0 || end < 0) throw Error('controlled owner test boundary changed');
source = source.slice(0, begin) + String.raw`
  const timelines = [];
  for (const mode of ['typed404', 'typed409', 'abandon', 'expiry']) await test('actual project-read owner: ' + mode, async () => {
    const status = mode === 'typed409' ? 409 : 404, x = await setup({ status });
    const initial = { ...x.auth.personalContext.identity }, events = [];
    const stop = x.a.watch(() => x.auth.state.busy, value => events.push(value ? 'busy' : 'released'), { flush: 'sync' });
    let seen, settled = false;
    try {
      x.begin();
      const original = x.auth.projects.resolve({ ...target });
      const observed = original.then(() => { throw Error('denied resolve unexpectedly fulfilled'); }, error => {
        seen = { error, busy: x.auth.state.busy, identity: { ...x.auth.personalContext.identity } };
        events.push('rejected'); settled = true;
      });
      await drain(); x.release(); await drain();
      const middle = x.nativeFacts();
      assert.equal(x.cancelTailCount(), 1); assert.equal(settled, false); assert.equal(x.auth.state.busy, true);
      assert(middle.eof_before_interruption && middle.content_length_matches_eof && middle.request_id_match);
      assert.equal(middle.reader_cancel_calls, middle.reader_cancel_settled);
      assert.equal(middle.stream_cancel_calls, 1); assert.equal(middle.stream_cancel_settled, 0);
      if (mode === 'abandon' || mode === 'expiry') {
        if (mode === 'abandon') x.auth.projects.abandonRead(); else x.expire();
        await drain();
        assert(settled); assert(seen.error instanceof x.a.AccountFailure);
        assert.equal(seen.error.kind, 'cancelled'); assert.equal(seen.error.problem, undefined);
        assert.equal(seen.busy, true); assert.equal(x.auth.state.busy, true);
        assert.deepEqual(events, ['busy', 'rejected']);
      }
      events.push('cancel-tail-release'); x.releaseCancel(); await observed; await drain();
      assert.equal(x.auth.state.busy, false); assert.equal(x.auth.state.phase, 'authenticated');
      assert.deepEqual({ ...x.auth.personalContext.identity }, initial);
      const final = x.nativeFacts();
      assert.equal(final.stream_cancel_calls, final.stream_cancel_settled);
      if (mode.startsWith('typed')) {
        assert(seen.error instanceof x.a.AccountFailure); assert.equal(seen.error.kind, 'problem');
        assert.equal(seen.error.problem.status, status); assert.equal(seen.error.problem.request_id, id(99));
        assert.equal(seen.busy, false); assert.deepEqual(seen.identity, initial);
        assert.deepEqual(events, ['busy', 'cancel-tail-release', 'released', 'rejected']);
        assert.equal(final.signal_aborted, false);
      } else {
        assert.deepEqual(events, ['busy', 'rejected', 'cancel-tail-release', 'released']);
        assert.equal(seen.error.kind, 'cancelled'); assert.equal(final.signal_aborted, true);
      }
      assert.equal(x.requests, 1); timelines.push({ mode, events });
    } finally { stop(); await x.cleanup(); }
  });
` + source.slice(end);
replace("const output = root + '/output/ai/model-ui-recovery/resolve-publication-controls';", "const output = root + '/output/ai/model-ui-recovery/resolve-rejection-owner-controls';");
replace('public_mutation_observer: true,', 'public_mutation_observer: false, actual_session_project_read: true, controlled_cancel_tail: true, controlled_expiry_clock: true, timelines,');
new Function('require', '__dirname', source)(require, __dirname);
