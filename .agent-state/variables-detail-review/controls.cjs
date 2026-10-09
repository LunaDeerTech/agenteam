#!/usr/bin/env node
// Independent delta probes. Reuse only the author's disclosed VM/jsdom fixture
// setup; execute the actual locked-PW installer/helper, not its full test suite.
const fs = require('node:fs')
const author = '/workspace/agenteam-project-variables-ui'
const own = '/workspace/agenteam-skills'
let setup = fs.readFileSync(author + '/.agent-state/project-variables-ui/detail-observer-controls.cjs', 'utf8')
setup = setup.replace(/^#![^\n]*\n/, '')
const marker = ';(async () => {'
if (setup.split(marker).length !== 2) throw Error('fixture setup boundary changed')
setup = setup.slice(0, setup.indexOf(marker))
setup = setup.replace("const root = path.resolve(__dirname, '../..')", 'const root = ' + JSON.stringify(author))
setup = setup.replace("path.join(root, 'output/ai/project-variables-ui/implementation/pw-detail-offline-cache')", JSON.stringify(own + '/output/ai/skills/variables-detail-review/pw-cache'))
// Add only one actual event ordering stimulus to the original helper adapter.
const event = "page.emit(options.ordinary ? 'requestfinished' : 'requestfailed', request)"
if (setup.split(event).length !== 2) throw Error('adapter event boundary changed')
setup = setup.replace(event, event + "\n  if (options.finishAfterFailure) page.emit('requestfinished', request)")
const suite = String.raw`
;(async () => {
  let count = 0;
  // A first retirement with the wrong original request ID cannot be repaired by
  // a second call after receiving otherwise valid input.
  {
    const f = await fixture(); assert.equal(f.installed, true);
    const p = f.auth.projectVariables.get(target.project, target.variable);
    assert.equal(p, f.held.promise); f.request(); f.held.resolve(value);
    await flush(); f.publish();
    const first = f.finish(value, id(999));
    assert.equal(first.body_matches, true); assert.equal(first.native_request_matches, false);
    assert(!source.variableDetailComplete(first, nativeProof()));
    const late = f.finish(value, xid);
    assert.deepEqual(late, first); assert(!source.variableDetailComplete(late, nativeProof()));
    count++;
  }
  // Actual Promise fulfillment does not stand in for the publisher's DOM tail.
  // A late render after the first finish must not promote its frozen evidence.
  {
    const f = await fixture(); f.auth.projectVariables.get(target.project, target.variable);
    f.request(); f.held.resolve(value); await flush();
    const first = f.finish();
    assert.equal(first.fulfilled, 1); assert.equal(first.pending, 0);
    assert.equal(first.body_matches, true); assert.equal(first.published, false);
    f.publish(); const late = f.finish();
    assert.deepEqual(late, first); assert(!source.variableDetailComplete(late, nativeProof()));
    count++;
  }
  // Losing the ability to restore an owned hook is a real failure even when the
  // original GET and DOM completed. A cached retired hook must still delegate.
  {
    const f = await fixture(); const cached = f.auth.projectVariables.get;
    f.auth.projectVariables.get(target.project, target.variable);
    f.request(); f.held.resolve(value); await flush(); f.publish();
    Object.defineProperty(f.auth.projectVariables, 'get', {value: cached, writable: false, configurable: false});
    const first = f.finish();
    assert.equal(first.published, true); assert.equal(first.hooks_retired, false);
    // The PW-serialized function may be non-strict: failed assignment can be
    // silent, but hooks_retired=false must still prevent acceptance.
    assert(!source.variableDetailComplete(first, nativeProof()));
    const receiver = { original: true }, args = ['after-retirement', 'original-argument'];
    assert.equal(Reflect.apply(cached, receiver, args), f.held.promise); await flush();
    assert.equal(f.calls.at(-1).receiver, receiver); assert.deepEqual(f.calls.at(-1).args, args);
    assert.deepEqual(f.finish(), first); count++;
  }
  // Multiple ordinary event families cannot be excused by the one detail proof.
  {
    const p = await positive();
    await adapter(p.facts, {finishAfterFailure: true, fail: true}); count++;
  }
  // The exact two-argument facade call is required; an extra argument is passed
  // unchanged to the original while eligibility is rejected.
  {
    const f = await fixture(); const p = f.auth.projectVariables.get(target.project, target.variable, undefined);
    assert.equal(p, f.held.promise); assert.equal(f.calls[0].args.length, 3);
    f.request(); f.held.resolve(value); await flush(); f.publish(); const facts = f.finish();
    assert.equal(facts.calls, 1); assert.equal(facts.target_calls, 0);
    assert(!source.variableDetailComplete(facts, nativeProof())); count++;
  }
  console.log('independent Variables detail delta: ' + count + ' controls PASS');
})().catch(error => { console.error(error); process.exitCode = 1; });
`
new Function('require', setup + suite)(require)
