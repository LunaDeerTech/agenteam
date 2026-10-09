// Node-side diagnostic wiring controls. Production client/DOM controls live in
// resolve-publication-observer-controls.cjs; this file uses explicit PW doubles.
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const path = require('node:path'), cp = require('node:child_process');
const { EventEmitter } = require('node:events');
const root = path.resolve(__dirname, '../..'), ts = require(root + '/web/node_modules/typescript');
const file = '.agent-state/model-ui-recovery/authority-and-identity.ts';
const source = fs.readFileSync(root + '/' + file, 'utf8');
const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
const declaration = (tree, name) => tree.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name).getText(tree).replace(/^export /, '');
const observerTree = ts.createSourceFile('observer.ts', fs.readFileSync(path.join(__dirname, 'resolve-publication-observer.ts'), 'utf8'), ts.ScriptTarget.Latest, true);
const fixtureTree = ts.createSourceFile('fixture.cjs', fs.readFileSync(path.join(__dirname, 'session-consumer-adapter.cjs'), 'utf8'), ts.ScriptTarget.Latest, true);
const native = new Function(declaration(fixtureTree, 'native') + ';return native;')();
const flush = async () => { for (let i = 0; i < 70; i++) await Promise.resolve(); };
const rows = [], unhandled = [];
process.on('unhandledRejection', error => unhandled.push(error));
const id = '01900000-0000-7000-8000-000000000099';
function fixture(options = {}) {
  const page = new EventEmitter(), browser = new EventEmitter(), writes = [], timers = new Map();
  let next = 0, now = 0, ended = false, closed = false, finishedCalls = 0, settleFinished;
  const originalFinished = new Promise(resolve => { settleFinished = resolve; });
  const request = { url: () => 'https://owned.invalid/api/v1/projects/resolve?username=admin&project_name=denied', method: () => 'GET', failure: () => ({ errorText: 'net::ERR_ABORTED' }) };
  const response = { request: () => request, status: () => 404, headerValue: async () => id, finished: () => { finishedCalls++; return originalFinished; } };
  const publication = {
    resolve_calls: 1, target_calls: 1, fulfilled: 0, rejected: 1, synchronous_throws: 0, problem_status: 404, pending_observations: 0,
    typed_problem: true, problem_instance_matches: true, entry_authenticated: true, entry_not_busy: true, entry_identity_matches: true,
    target_url_at_call: true, target_url_current: true, left_target: false, identity_current: true, authenticated: true, not_busy: true,
    old_error_heading_present: true, old_loading_dom_present: false, loading_dom_observed: true, error_dom_after_rejection: true,
    error_heading_present: true, error_heading_visible: true, zero_provider_lists: true, zero_dialogs: true, observer_failed: false,
    hooks_retired: false, lifetime_expired: false, problem_request_id_matches: true,
    clock: 'browser-monotonic-observed-relative-to-install', timing: { call: 1, loading_dom: 2, rejected: 3, error_dom: 4, sample: 5 },
    ...(options.publication ?? {}),
  };
  const probe = {
    resolveSnapshot(_slot, expectedID) { return ended ? null : { ...native(), status: 404, status_ok: false, request_id_match: expectedID === id, ...(options.native ?? {}) }; },
    resolveEnd(slot, expectedID) { const value = this.resolveSnapshot(slot, expectedID); ended = true; return value; },
  };
  const host = { __projectModelsProbe: probe, __authorityResolvePublication: {
    snapshot(expectedID) { return { ...publication, problem_request_id_matches: expectedID === id && publication.problem_request_id_matches }; },
    finish(expectedID) { const value = { ...this.snapshot(expectedID), hooks_retired: true }; delete host.__authorityResolvePublication; return value; },
  } };
  const context = { URL, Promise, Error, Date, randomUUID: () => 'owned-slot', resolve: path.resolve, join: path.join,
    Function: function() { return async () => ({ resolveOwnerModule: async () => ({ entry: '/entry.js', asset: '/owner.js', export_name: 'singleton', failure_export_name: 'Failure' }) }); },
    installResolvePublicationObservation: async function installResolvePublicationObservation() {},
    resolveEvaluations: new WeakMap(), performance: { now: () => ++now }, test: { info: () => ({ status: 'timedOut' }) },
    process: { env: { AGENTEAM_PROJECT_MODELS_WEB_DIST: '/owned/dist', AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE: '/owned/evidence', AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH: 'controlled' } },
    writeFileSync(name, text) { writes.push({ name, value: JSON.parse(text) }); },
    setTimeout(callback, delay) { const token = ++next; timers.set(token, { callback, delay }); return token; }, clearTimeout(token) { timers.delete(token); },
  };
  const names = ['need', 'resolveEvaluate', 'resolveSampler', 'resolveSnapshot', 'restoreOwnerProjection', 'beginResponseDiagnostic', 'beginResolveDiagnostic'];
  vm.runInNewContext(ts.transpileModule(names.map(name => declaration(ast, name)).join('\n') + '\n' + declaration(observerTree, 'resolvePublicationProjection') + '\nthis.begin=beginResolveDiagnostic;', { compilerOptions: { target: ts.ScriptTarget.ES2024 } }).outputText, context);
  page.url = () => 'https://owned.invalid/admin/denied/settings/model-providers'; page.context = () => browser;
  page.evaluate = async (callback, args) => {
    if (closed) throw Error('closed-private-error');
    if (callback.name === 'installResolvePublicationObservation') return options.installResult ?? true;
    return vm.runInNewContext('(' + callback.toString() + ')(args)', { window: host, args });
  };
  return { page, browser, request, response, publication, writes,
    async begin() { return context.begin(page, { username: 'admin', normalized_name: 'denied' }); },
    async sample(diagnostic) {
      diagnostic.start(); page.emit('request', request); diagnostic.select(response); await flush();
      const scheduled = [...timers].find(([, timer]) => timer.delay === 250); assert(scheduled); timers.delete(scheduled[0]); scheduled[1].callback(); await flush();
    },
    close() { page.emit('close'); closed = true; },
    original(diagnostic) { const value = diagnostic.finishedWait(() => response.finished()); assert.equal(value, originalFinished); return value; },
    artifact() { return writes.findLast(row => row.name.endsWith('authority-resolve-diagnostic.json'))?.value; },
    async retired() { settleFinished(null); await flush(); assert.equal(finishedCalls, 1); assert.equal(timers.size, 0); assert.equal(page.eventNames().length, 0); assert.equal(browser.eventNames().length, 0); },
  };
}
async function test(name, run) { await run(); rows.push(name); }
(async () => {
  await test('sampled original typed/public DOM survives page close without claiming end or original finished', async () => {
    const f = fixture(), d = await f.begin(); await f.sample(d); f.original(d); f.close(); await d.finish(true);
    const result = f.artifact(); assert.equal(result.resolve_publication_source, 'sample'); assert.equal(result.resolve_publication_selected_bound, true); assert.equal(result.resolve_publication_observers_retired, false);
    assert.equal(result.slot_end_observed, false); assert.equal(result.pw_finished_event, false); assert(result.resolve_publication.typed_problem && result.resolve_publication.error_dom_after_rejection); assert(!JSON.stringify(result).includes(id));
    await f.retired();
  });
  await test('actual end is separately recorded while original finished remains unfulfilled', async () => {
    const f = fixture(), d = await f.begin(); await f.sample(d); const original = f.original(d); let settled = false; void original.then(() => { settled = true; }); await d.finish(true); await flush();
    const result = f.artifact(); assert.equal(result.resolve_publication_source, 'end'); assert.equal(result.resolve_publication_selected_bound, true); assert.equal(result.resolve_publication_observers_retired, true); assert.equal(settled, false); await f.retired();
  });
  for (const [name, options, late] of [
    ['wrong Problem ID', { publication: { problem_request_id_matches: false } }],
    ['second owner call', { publication: { resolve_calls: 2 } }],
    ['wrong Problem status', { publication: { problem_status: 409 } }],
    ['wrong Problem instance', { publication: { problem_instance_matches: false } }],
    ['second native request', { native: { requests: 2 } }],
    ['late second PW request', {}, true],
  ]) await test(name + ' cannot claim same original response binding', async () => {
    const f = fixture(options), d = await f.begin(); await f.sample(d); f.original(d); if (late) f.page.emit('request', { ...f.request }); await d.finish(true); assert.equal(f.artifact().resolve_publication_selected_bound, false); await f.retired();
  });
  await test('original denied flow including finished054 and postconditions remains byte-identical', () => {
    const old = cp.execFileSync('git', ['show', 'ca3c73db:' + file], { cwd: root, encoding: 'utf8' });
    const oldTree = ts.createSourceFile('old.ts', old, ts.ScriptTarget.Latest, true);
    assert.equal(declaration(ast, 'runAuthorityAndIdentity'), declaration(oldTree, 'runAuthorityAndIdentity'));
    assert.equal(declaration(ast, 'sessionIdentity'), declaration(oldTree, 'sessionIdentity'));
  });
  await flush(); assert.equal(unhandled.length, 0); assert.equal(fs.readFileSync(root + '/' + file, 'utf8'), source);
  const output = root + '/output/ai/model-ui-recovery/resolve-publication-observer-controls'; fs.mkdirSync(output, { recursive: true });
  const result = { passed: rows.length, cases: rows, actual_adapter_source: true, playwright_doubles: true, browser: false, network: false, original_finished_gate_unchanged: true, unhandled: 0 };
  fs.writeFileSync(output + '/adapter-result.json', JSON.stringify(result, null, 2) + '\n'); console.log(JSON.stringify(result));
})().catch(error => { console.error(error); process.exitCode = 1; });
