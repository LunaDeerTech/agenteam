// Offline hypothesis controls only. No authority gate or production source is changed.
// Run: node .agent-state/model-ui-recovery/resolve-publication-controls.cjs
// Uses the actual controller, client, workspace and public Workspace view in jsdom.
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../..');
const { JSDOM } = require(root + '/web/node_modules/jsdom');
const output = root + '/output/ai/model-ui-recovery/resolve-publication-controls';
const boundaryInput = process.env.AGENTEAM_RESOLVE_BOUNDARY_INPUT
  ? JSON.parse(fs.readFileSync(process.env.AGENTEAM_RESOLVE_BOUNDARY_INPUT, 'utf8'))
  : null;
const id = n => n === 99 && boundaryInput ? boundaryInput['404'].body.request_id : `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`;
const instant = '2026-10-05T12:34:56.123456Z';
const session = {
  user: { id: id(1), email: 'private@example.test', username: 'owner', display_name: 'Private', role: 'user', theme: 'system', version: '1', initial_password_suggestion: false },
  session: { id: id(2), issued_at: instant, absolute_expires_at: instant, idle_expires_at: instant },
  csrf_token: 'c'.repeat(43),
};
const target = { username: 'admin', project_name: 'denied' };
const targetPath = '/admin/denied/settings/model-providers';
const rows = [];
const unhandled = [];
process.on('unhandledRejection', error => unhandled.push(error));
const drain = async () => { for (let n = 0; n < 16; n++) await Promise.resolve(); await new Promise(resolve => setImmediate(resolve)); };
async function test(name, work) { await work(); rows.push(name); }

// This is a candidate observation, not an acceptance oracle. It deliberately
// reads only the public controller and rendered DOM, never workspace internals.
function observe(w, auth, AccountFailure, expectedID, options = {}) {
  const original = auth.projects.resolve;
  const identity = auth.personalContext.identity;
  let sequence = 0, calls = 0, loading = 0, rejected = 0, published = 0;
  let typed = false, requestMatched = false, outsideTarget = false, stopped = false;
  let lastPromise, lastThis, lastArgs, finalFacts;
  const sample = () => {
    const onTarget = w.location.pathname === targetPath;
    if (calls && !onTarget) outsideTarget = true;
    if (!onTarget) return;
    const loadingNodes = [...w.document.querySelectorAll('[role="status"][aria-busy="true"] h3')];
    const errorNodes = [...w.document.querySelectorAll('[role="alert"] h3')];
    const reading = loadingNodes.some(node => node.textContent.trim() === '正在读取项目');
    const error = errorNodes.some(node => node.textContent.replace(/^!/, '').trim() === (options.status === 409 ? '项目信息读取失败' : '项目不可用'));
    if (calls === 1 && reading && !loading && !rejected) loading = ++sequence;
    if (calls === 1 && loading && rejected && error && !reading && !published) published = ++sequence;
  };
  const observer = new w.MutationObserver(sample);
  observer.observe(w.document.body, { subtree: true, childList: true, characterData: true, attributes: true });
  auth.projects.resolve = function (...args) {
    calls++;
    lastThis = this;
    lastArgs = args;
    const pending = Reflect.apply(original, this, args);
    lastPromise = pending;
    // Preserve the original caller-visible Promise, receiver and arguments.
    void pending.then(() => {}, error => {
      if (stopped) return;
      rejected = ++sequence;
      typed = error instanceof AccountFailure && error.kind === 'problem';
      requestMatched = typed && error.problem.request_id === expectedID && error.problem.instance === '/api/v1' && error.problem.status === (options.status ?? 404);
      options.onRejected?.();
    }).catch(error => unhandled.push(error));
    return pending;
  };
  return {
    sample,
    checkCall(pending, args) { assert.equal(pending, lastPromise); assert.equal(lastThis, auth.projects); assert.deepEqual(lastArgs, args); },
    finish() {
      if (finalFacts) return finalFacts;
      sample();
      observer.disconnect();
      stopped = true;
      assert.equal(auth.projects.resolve === original, false);
      auth.projects.resolve = original;
      const current = auth.personalContext.identity;
      const sameIdentity = !!identity && !!current && identity.userID === current.userID && identity.sessionID === current.sessionID && identity.epoch === current.epoch;
      const zeroModel = !w.document.querySelector('[aria-label="Providers 列表"]') && !w.document.querySelector('[role="dialog"]');
      return finalFacts = { calls, loading, rejected, published, typed, requestMatched, sameIdentity, outsideTarget, zeroModel,
        candidate: calls === 1 && loading > 0 && rejected > loading && published > rejected && typed && requestMatched && sameIdentity && !outsideTarget && w.location.pathname === targetPath && zeroModel && auth.state.phase === 'authenticated' && !auth.state.busy };
    },
  };
}

(async () => {
  const { build } = await import(root + '/web/node_modules/vite/dist/node/index.js');
  const { default: vue } = await import(root + '/web/node_modules/@vitejs/plugin-vue/dist/index.mjs');
  const entry = root + '/web/resolve-publication-virtual.js';
  const virtual = `
export { createApp, h, provide, ref, nextTick } from 'vue';
export { createRouter, createMemoryHistory } from 'vue-router';
export { createSessionController } from './src/composables/useSession.ts';
export { createProjectWorkspace, projectWorkspaceKey } from './src/composables/useProjectWorkspace.ts';
export { createAccountAPI } from './src/api/account.ts';
export { createProjectOwnerAPI } from './src/api/project-owner.ts';
export { AccountFailure } from './src/api/client.ts';
export { default as WorkspaceView } from './src/views/projects/ProjectWorkspaceView.vue';
export { sessionDiagnostics } from '../.agent-state/model-ui-recovery/native-client-probe.ts';
`;
  const built = await build({ configFile: false, root: root + '/web', logLevel: 'silent', plugins: [vue(), {
    name: 'resolve-controls-entry', resolveId: source => source === entry ? entry : undefined, load: source => source === entry ? virtual : undefined,
  }], build: { write: false, minify: false, lib: { entry, name: 'ActualResolve', formats: ['iife'] } } });
  const actual = (Array.isArray(built) ? built : [built]).flatMap(result => result.output).find(item => item.type === 'chunk').code;
  async function setup(options = {}) {
    const dom = new JSDOM('<html><body><div id="app"></div></body></html>', { url: 'https://owned.invalid/initial', runScripts: 'outside-only' });
    const w = dom.window;
    Object.assign(w, { Request, Response, Headers, ReadableStream, TextEncoder, TextDecoder, Uint8Array, Blob, AbortController, process: { env: { NODE_ENV: 'production' } } });
    Object.defineProperty(w, 'crypto', { value: crypto.webcrypto });
    w.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
    vm.runInContext(actual, dom.getInternalVMContext());
    const a = w.ActualResolve;
    const pending = [], witnesses = [];
    let native;
    let requests = 0;
    const rawFetch = (url, init) => {
      if (url === '/api/v1/session') return Promise.resolve(new Response(JSON.stringify(session), { headers: { 'content-type': 'application/json' } }));
      const parsed = new URL(url, w.location.href);
      assert.equal(parsed.pathname, '/api/v1/projects/resolve');
      assert.equal(parsed.searchParams.get('username'), target.username);
      assert.equal(parsed.searchParams.get('project_name'), target.project_name);
      requests++;
      return new Promise(resolve => pending.push(() => {
        const status = options.status ?? 404;
        const problem = boundaryInput ? structuredClone(boundaryInput[String(status)].body) : { type: 'urn:agenteam:problem:not-found', title: '', detail: '', instance: '/api/v1', status, code: status === 409 ? 'INVALID_STATE' : 'NOT_FOUND', request_id: id(99), commit_state: 'not_started' };
        if (options.badProblem) problem.request_id = id(98);
        if (options.problemInstance !== undefined) problem.instance = options.problemInstance;
        const bytes = new TextEncoder().encode(JSON.stringify(problem));
        resolve(new Response(bytes, { status, headers: { 'content-type': 'application/problem+json', 'content-length': String(bytes.byteLength), 'x-request-id': id(99) } }));
      }));
    };
    native = a.sessionDiagnostics(rawFetch, 'resolve');
    w.fetch = (url, init) => native.fetch(url, init) ?? rawFetch(url, init);
    const auth = a.createSessionController(a.createAccountAPI(w.fetch), undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, a.createProjectOwnerAPI(w.fetch));
    await auth.restore();
    assert.equal(auth.state.phase, 'authenticated');
    const workspace = a.createProjectWorkspace(auth);
    const router = a.createRouter({ history: a.createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }] });
    await router.push('/initial');
    // Like the production route shell, mount the workspace View only for a
    // Project route; injecting the public owner requires no Vue private fields.
    const showWorkspace = a.ref(false);
    const app = a.createApp({ setup() { a.provide(a.projectWorkspaceKey, workspace); return () => showWorkspace.value ? a.h(a.WorkspaceView) : null; } });
    app.use(router);
    app.mount(w.document.querySelector('#app'));
    await drain();
    return { w, a, auth, workspace, native, get requests() { return requests; },
      begin() { assert(native.begin('resolve-offline', Date.now() + 5000, target)); },
      release() { const settle = pending.shift(); assert(settle, 'exact controlled response exists'); settle(); },
      navigate(route = targetPath) { w.history.pushState(null, '', route); workspace.afterNavigation(route); showWorkspace.value = route === targetPath; },
      observe(extra = {}) { const witness = observe(w, auth, a.AccountFailure, id(99), { ...options, ...extra }); witnesses.push(witness); return witness; },
      nativeFacts() { return native.snapshot('resolve-offline', id(99)); },
      async cleanup() { while (pending.length) pending.shift()(); await drain(); for (const witness of witnesses) witness.finish(); native.end('resolve-offline', id(99)); app.unmount(); workspace.dispose(); auth.leave(); router.options.history.destroy(); dom.window.close(); },
    };
  }
  for (const status of [404, 409]) await test(`actual ${status} controller/client/workspace/View publishes after original typed rejection`, async () => {
    const x = await setup({ status });
    try {
      x.begin(); const witness = x.observe(); x.navigate(); await drain(); x.release(); await drain();
      const result = witness.finish(), native = x.nativeFacts();
      assert(result.candidate, JSON.stringify(result));
      assert(native.eof_before_interruption && native.content_length_matches_eof && native.request_id_match);
      assert.equal(native.read_calls, native.read_settled); assert.equal(native.read_rejected, 0);
      assert.equal(native.reader_cancel_calls, native.reader_cancel_settled); assert.equal(native.stream_cancel_calls, native.stream_cancel_settled);
      assert.equal(x.workspace.detail.phase, status === 404 ? 'unavailable' : 'read-error');
      assert.equal(x.requests, 1);
    } finally { await x.cleanup(); }
  });
  await test('old same-target heading plus a new valid typed rejection has no new loading/publication witness', async () => {
    const x = await setup();
    try {
      x.navigate(); await drain(); x.release(); await drain();
      assert.equal(x.workspace.detail.phase, 'unavailable');
      x.begin(); const witness = x.observe(); const args = [{ ...target }];
      const original = x.auth.projects.resolve(...args); witness.checkCall(original, args);
      const settled = original.catch(error => error); await drain(); x.release(); await settled; await drain();
      const result = witness.finish(); assert(result.typed && result.requestMatched); assert.equal(result.loading, 0); assert.equal(result.candidate, false);
    } finally { await x.cleanup(); }
  });
  for (const effect of ['navigation', 'identity', 'dispose']) await test(`typed rejection followed by real public ${effect} invalidation cannot borrow a publication`, async () => {
    const x = await setup();
    try {
      x.begin(); const witness = x.observe({ onRejected() {
        if (effect === 'navigation') x.navigate('/other-route');
        else if (effect === 'identity') x.auth.leave();
        else x.workspace.dispose();
      } });
      x.navigate(); await drain(); x.release(); await drain();
      const result = witness.finish(); assert(result.typed && result.requestMatched, JSON.stringify(result)); assert.equal(result.published, 0); assert.equal(result.candidate, false);
    } finally { await x.cleanup(); }
  });
  await test('invalid Problem with complete native EOF can publish read-error but is not validated original rejection', async () => {
    const x = await setup({ badProblem: true, status: 409 });
    try {
      x.begin(); const witness = x.observe(); x.navigate(); await drain(); x.release(); await drain();
      const result = witness.finish(), native = x.nativeFacts();
      assert(native.eof_before_interruption && native.content_length_matches_eof);
      assert(result.published > result.rejected); assert.equal(result.typed, false); assert.equal(result.candidate, false);
    } finally { await x.cleanup(); }
  });
  await drain(); assert.equal(unhandled.length, 0);
  const result = { passed: rows.length, cases: rows, actual_controller_client_workspace_view: true, original_promise_preserved: true, public_mutation_observer: true, browser: false, network: false, authority_gate_changed: false, unhandled: 0 };
  fs.mkdirSync(output, { recursive: true });
  fs.writeFileSync(output + '/result.json', JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result));
})().catch(error => { console.error(error); process.exitCode = 1; });
