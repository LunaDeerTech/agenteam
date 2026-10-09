#!/usr/bin/env node
// Actual locked Playwright transform/serialization; explicit browser and
// Session facade substitutes test diagnostics, not production acceptance.
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const { EventEmitter } = require('node:events')
const { createHash } = require('node:crypto')
const { execFileSync } = require('node:child_process')
const root = path.resolve(__dirname, '../..')
process.env.PWTEST_CACHE_DIR = path.join(root, 'output/ai/project-variables-ui/implementation/pw-authority-offline-cache')
const { transformHook } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright/lib/transform/transform.js'))
const { evaluationScript } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright-core/lib/client/clientHelper.js'))
const { JSDOM } = require(path.join(root, 'web/node_modules/jsdom'))
function load(name) {
  const filename = path.join(root, 'tests/account-captcha-web/e2e', name + '.ts'), exports = {}
  new Function('require', 'exports', transformHook(fs.readFileSync(filename, 'utf8'), filename).code)(require, exports)
  return exports
}
const moduleSource = load('project-variables.authority'), nativeSource = load('project-variables.native')
const ts = require(path.join(root, 'web/node_modules/typescript'))
const { expect } = require(path.join(root, 'tests/account-captcha-web/node_modules/@playwright/test'))
const binding = moduleSource.variableAuthorityBinding(root, path.join(root, 'output/ai/project-variables-ui/dist-03'))
assert(binding.asset && binding.singleton && binding.failure && binding.entry)
assert.throws(() => moduleSource.variableAuthorityBinding(root, '/tmp/unowned-dist'))
const id = n => '01970000-0000-7000-8000-' + String(n).padStart(12, '0')
const target = { project: id(10), variable: id(20), version: '1', value: 'PRIVATE_DIAGNOSTIC_CANARY', route: '/owner/project/settings/variables' }
const endpoint = `/api/v1/projects/${target.project}/variables/${target.variable}`
// This is the actual Account boundary's original JSON, with no server/socket.
const producerProblem = JSON.parse(execFileSync('python3', [path.join(__dirname, 'authority-refusal-controls.py'), '--producer-only'], { encoding: 'utf8', timeout: 50000 }))
assert.equal(producerProblem.instance, '/api/v1')
const xid = producerProblem.request_id, identity = { userID: id(3), sessionID: id(4), epoch: 1 }
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
let checks = 0
let consumerSample
async function adapter(options = {}) {
  const source = ts.createSourceFile('helper.ts', fs.readFileSync(path.join(root, 'tests/account-captcha-web/e2e/project-variables.helpers.ts'), 'utf8'), ts.ScriptTarget.Latest, true)
  const names = new Set(['emptyNetworkDiagnostic', 'networkObservations', 'networkRetire', 'authorityObservations', 'detailObservations', 'observe', 'recordFailure', 'originalResponse'])
  const code = ts.transpileModule(source.statements.filter(n => ts.isFunctionDeclaration(n) && names.has(n.name?.text) || ts.isVariableStatement(n) && n.declarationList.declarations.some(d => names.has(d.name.getText(source)))).map(n => n.getText(source)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
  const body = JSON.stringify({ expected_version: '1', request: { value: 'prepared before archive' } }), csrf = 'S'.repeat(43), key = 'original-key'
  const record = { method: 'PATCH', path: endpoint, query: '', status: 409, content_type: 'application/problem+json', request_id: xid, request_b64: Buffer.from(body).toString('base64'), key, csrf_sha256: createHash('sha256').update(csrf).digest('hex') }
  if (options.record) Object.assign(record, options.record)
  const exports = {}, held = deferred(); let written, bodyReads = 0, stopCalls = 0
  const emptyNativeDiagnostic = () => ({ binding: 'unobserved', facts: null })
  const dependencies = { exports, expect, uuid7: /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/, emptyNativeDiagnostic,
    nativeConsumption: () => ({ request() {}, response() {}, stop: async () => { stopCalls++ }, endDocument: async () => options.retired !== false, snapshot: emptyNativeDiagnostic }),
    validateOriginalBodies: async () => { throw Error('ordinary validation must remain blocked') },
    directory: 'synthetic-private', repository: root, join: path.join, createHash, Buffer,
    readFileSync: () => JSON.stringify(record), readdirSync: () => ['project-variables-response-1.json'],
    writeFileSync: (_file, raw) => { written = JSON.parse(raw) }, step: 2,
    process: { env: { AGENTEAM_PROJECT_VARIABLE_WEB_CASE: 'authority' } },
    variableAuthorityBinding: () => binding, installVariableAuthority: moduleSource.installVariableAuthority,
    variableAuthorityProjection: moduleSource.variableAuthorityProjection,
    path: () => target.route,
  }
  new Function(...Object.keys(dependencies), code)(...Object.values(dependencies))
  const page = new EventEmitter()
  page.evaluate = (fn, arg) => {
    if (fn === moduleSource.installVariableAuthority) return Promise.resolve(true)
    if (arg === undefined) return Promise.resolve({ observed: true, variables: true, editor: true, close: true, history: true, confirmed: false, uncertain: false, dialog: false })
    if (options.closed) return Promise.reject(Error('private closed error'))
    if (options.hung) return held.promise
    return Promise.resolve({ ...consumerSample, problem_request_matches: arg === xid })
  }
  const observation = exports.observe(page)
  const diagnostic = await observation.authority({ ids: { main: target.project }, targets: { main: target.variable }, variables: { main: { version: '1' } }, diagnostic_dist: 'controlled-dist' })
  const request = { method: () => 'PATCH', url: () => 'http://offline.invalid' + endpoint, postData: () => body, allHeaders: async () => ({ 'idempotency-key': key, 'x-csrf-token': csrf }) }
  const response = { request: () => request, headers: () => ({ 'x-request-id': xid }), status: () => 409, headerValue: async name => name === 'x-request-id' ? xid : 'application/problem+json', body: async () => { bodyReads++; throw Error('extra body consumption') } }
  page.emit('request', request); page.emit('response', response); page.emit('requestfailed', request)
  for (let i = 0; i < (options.more ?? 0); i++) {
    const extra = { method: () => 'GET', url: () => 'http://offline.invalid' + endpoint, postData: () => null }
    page.emit('request', extra); page.emit('requestfailed', extra)
  }
  if (options.duplicate) { const extra = { ...request }; page.emit('request', extra); page.emit('response', { ...response, request: () => extra }); page.emit('requestfailed', extra) }
  await diagnostic.finish()
  await assert.rejects(observation.finish())
  await exports.recordFailure(page, { status: 'failed', expectedStatus: 'passed', errors: [{ stack: 'PRIVATE_DIAGNOSTIC_CANARY at project-variables.helpers.ts:463:1' }] })
  assert.equal(bodyReads, 0); assert(stopCalls >= 1)
  assert(!JSON.stringify(written).includes(target.value)); assert(!JSON.stringify(written).includes(xid)); assert(!JSON.stringify(written).includes(key))
  return { written, held, diagnostic, page, observation, read: () => written }
}
async function fixture(options = {}) {
  const pending = deferred(), calls = [], native = { document: 'original-document', retired: false, records: [] }
  class Failure extends Error { constructor(kind, problem) { super(kind); this.kind = kind; this.problem = problem } }
  const auth = { state: { phase: 'authenticated', busy: false }, personalContext: { identity: { ...identity } }, projectVariables: {
    start(...args) { calls.push([this, args]); if (options.throw) throw options.throw; return pending.promise }, progress: null,
  } }
  const original = auth.projectVariables.start
  if (options.frozen) Object.freeze(auth.projectVariables)
  const dom = new JSDOM('<div class="variable-editor"><label for="value">值</label><textarea id="value"></textarea></div>', { url: 'http://offline.invalid' + target.route })
  dom.window.document.querySelector('textarea').value = target.value
  const host = { __variableNativeDiagnostic: { snapshot: () => structuredClone(native) } }
  const exports = { [binding.singleton]: () => auth, [binding.failure]: Failure }
  if (options.nonSingleton) exports[binding.singleton] = () => ({ ...auth })
  const context = vm.createContext({ window: host, document: dom.window.document, HTMLTextAreaElement: dom.window.HTMLTextAreaElement,
    Error, URL, location: dom.window.location, performance: { getEntriesByName: () => options.unloaded ? [] : [{}] },
    Function: function () { return async () => exports },
  })
  const installed = await vm.runInContext(await evaluationScript(null, moduleSource.installVariableAuthority, { binding, target }), context)
  const command = { kind: 'update', projectID: target.project, targetID: target.variable, expectedVersion: '1', request: { value: target.value } }
  const problem = { ...producerProblem }
  const complete = async (changes = {}) => {
    native.records.push({ method: 'PATCH', path: endpoint, query: '', status: 409, request_id: xid })
    auth.projectVariables.progress = { projectID: target.project, targetID: target.variable, kind: 'update', phase: 'rejected', receipt: null }
    pending.reject(new Failure('problem', { ...problem, ...changes })); await flush()
  }
  return { installed, auth, original, calls, pending, native, host, command, problem, Failure, complete, dom }
}
async function run(name, work) { await work(); checks++; console.log(name + ': PASS') }
async function main() {
  for (const [elapsed, expected] of [[249, true], [250, false], [251, false]]) await run('original retirement deadline: ' + elapsed, async () => {
    const page = new EventEmitter(), frame = {}; page.mainFrame = () => frame
    let clock = 1000; const originalNow = Date.now; Date.now = () => clock
    page.evaluate = async (_fn, end) => { if (end) clock = 1000 + elapsed; return { document: 'same', retired: !!end, records: [] } }
    const observation = nativeSource.nativeConsumption(page)
    try { await flush(); assert.equal(await observation.endDocument(), expected) }
    finally { Date.now = originalNow; await observation.stop() }
  })
  await run('stop during original retirement cannot report success', async () => {
    const page = new EventEmitter(), frame = {}, held = deferred(); page.mainFrame = () => frame
    page.evaluate = async (_fn, end) => end ? held.promise : { document: 'same', retired: false, records: [] }
    const observation = nativeSource.nativeConsumption(page); await flush()
    const ending = observation.endDocument(); await flush(); const stopping = observation.stop()
    held.resolve({ document: 'same', retired: true, records: [] })
    assert.equal(await ending, false); await stopping
  })
  await run('actual dist AST singleton and exported failure binding', async () => {})
  await run('original Promise, receiver, args, typed refusal and actual retirement', async () => {
    const x = await fixture(); assert.equal(x.installed, true)
    const cached = x.auth.projectVariables.start, receiver = {}
    const promise = Reflect.apply(cached, receiver, [x.command]); assert.equal(promise, x.pending.promise)
    await x.complete(); assert.equal(x.calls.length, 1); assert.equal(x.calls[0][0], receiver); assert.equal(x.calls[0][1][0], x.command)
    const before = x.host.__variableAuthority.snapshot(xid)
    assert.equal(before.pending, 0); assert.equal(before.calls, 1); assert.equal(before.target_calls, 1); assert.equal(before.rejected, 1); assert.equal(before.fulfilled, 0)
    for (const k of ['typed_problem', 'instance_matches', 'entry_authenticated', 'entry_idle', 'entry_identity', 'identity_current', 'authenticated', 'owner_idle', 'progress_rejected', 'receipt_absent', 'draft_matches', 'document_matches', 'target_route', 'native_request_matches', 'problem_request_matches']) assert.equal(before[k], true, k)
    const final = x.host.__variableAuthority.finish(xid), copy = JSON.stringify(final)
    consumerSample = JSON.parse(copy)
    assert.equal(final.hooks_retired, true); assert.equal(x.auth.projectVariables.start, x.original)
    assert.equal(Reflect.apply(cached, receiver, [x.command]), promise); assert.equal(x.calls.length, 2)
    await flush(); assert.equal(JSON.stringify(final), copy)
    const projected = moduleSource.variableAuthorityProjection(final); assert(projected); assert(!JSON.stringify(projected).includes(target.value)); assert(!JSON.stringify(projected).includes(xid))
    for (const bad of [{ ...final, private: target.value }, { ...final, code: target.value }, { ...final, commit_state: target.value }, { ...final, calls: 4097 }, { ...final, pending: -1 }, { ...final, owner_idle: 1 },
      { ...final, code: { toString: () => 'none', private: target.value } }, { ...final, commit_state: { toString: () => 'none', private: target.value } },
      { ...final, code: new String('none') }, { ...final, commit_state: new String('none') },
    ]) assert.equal(moduleSource.variableAuthorityProjection(bad), null)
    x.dom.window.close()
  })
  for (const [name, change, field, expected] of [
    ['wrong code', { code: 'INVALID_STATE' }, 'code', 'other'], ['unknown commit', { commit_state: 'unknown' }, 'commit_state', 'unknown'],
    ['wrong instance', { instance: '/unrelated' }, 'instance_matches', false], ['unprojected endpoint instance', { instance: endpoint }, 'instance_matches', false], ['wrong XID', { request_id: id(91) }, 'problem_request_matches', false],
  ]) await run(name, async () => { const x = await fixture(); x.auth.projectVariables.start(x.command); await x.complete(change); assert.equal(x.host.__variableAuthority.finish(xid)[field], expected); x.dom.window.close() })
  for (const [name, mutate, field] of [
    ['duplicate native XID', x => x.native.records.push({ ...x.native.records[0] }), 'native_request_matches'],
    ['wrong native method', x => x.native.records[0].method = 'POST', 'native_request_matches'],
    ['wrong native query', x => x.native.records[0].query = '?changed', 'native_request_matches'],
    ['wrong document', x => x.native.document = 'new-document', 'document_matches'],
    ['changed identity', x => x.auth.personalContext.identity.sessionID = id(9), 'identity_current'],
    ['owner still held', x => x.auth.state.busy = true, 'owner_idle'],
    ['wrong draft', x => x.dom.window.document.querySelector('textarea').value = 'changed', 'draft_matches'],
  ]) await run(name, async () => { const x = await fixture(); x.auth.projectVariables.start(x.command); await x.complete(); mutate(x); assert.equal(x.host.__variableAuthority.finish(xid)[field], false); x.dom.window.close() })
  await run('original synchronous throw identity', async () => { const error = new Error('original'); const x = await fixture({ throw: error }); assert.throws(() => x.auth.projectVariables.start(x.command), e => e === error); const facts = x.host.__variableAuthority.finish(xid); assert.equal(facts.synchronous_throws, 1); assert.equal(facts.pending, 0); x.dom.window.close() })
  await run('late rejection after retirement cannot publish', async () => { const x = await fixture(); const original = x.auth.projectVariables.start(x.command); assert.equal(original, x.pending.promise); const final = x.host.__variableAuthority.finish(xid), before = JSON.stringify(final); await x.complete(); assert.equal(JSON.stringify(final), before); assert.equal(final.pending, 1); assert.equal(final.rejected, 0); x.dom.window.close() })
  await run('foreign wrapper not overwritten or reported retired', async () => { const x = await fixture(); const foreign = () => {}; x.auth.projectVariables.start = foreign; const final = x.host.__variableAuthority.finish(xid); assert.equal(x.auth.projectVariables.start, foreign); assert.equal(final.hooks_retired, false); assert.equal(final.observer_failed, true); x.dom.window.close() })
  for (const option of ['frozen', 'nonSingleton', 'unloaded']) await run('installation unavailable: ' + option, async () => { const x = await fixture({ [option]: true }); assert.equal(x.installed, false); assert.equal(x.auth.projectVariables.start, x.original); assert.equal(x.calls.length, 0); x.dom.window.close() })
  await run('original document retired before navigation; next document independent', async () => {
    const page = new EventEmitter(), frame = {}, snapshots = []
    page.mainFrame = () => frame
    let document = 'first', active = 0, maximum = 0
    page.evaluate = async (_fn, end) => { active++; maximum = Math.max(maximum, active); const value = { document, retired: !!end, records: [] }; snapshots.push(value); await flush(); active--; return value }
    const observation = nativeSource.nativeConsumption(page)
    await flush(); assert.equal(await observation.endDocument(), true)
    assert.equal(snapshots.at(-1).document, 'first'); assert.equal(snapshots.at(-1).retired, true)
    document = 'second'; page.emit('framenavigated', frame)
    await observation.stop(); assert.equal(snapshots.at(-1).document, 'second'); assert.equal(snapshots.at(-1).retired, true); assert.equal(maximum, 1)
  })
  await run('navigation during end cannot claim old-document retirement', async () => {
    const page = new EventEmitter(), frame = {}, held = deferred(); page.mainFrame = () => frame
    let calls = 0
    page.evaluate = async (_fn, end) => { calls++; return end ? held.promise : { document: 'old', retired: false, records: [] } }
    const observation = nativeSource.nativeConsumption(page); await flush()
    const ended = observation.endDocument(); await flush(); page.emit('framenavigated', frame); held.resolve({ document: 'old', retired: true, records: [] })
    assert.equal(await ended, false); await observation.stop(); assert(calls >= 2)
  })
  await run('actual helper binds private original; all three incomplete; ordinary failed gate still rejects', async () => {
    const x = await adapter({ more: 2 }), a = x.written.authority
    assert.equal(a.installed, true); assert.equal(a.joined, true); assert.equal(a.request_bound, true); assert.equal(a.private_bound, true); assert.equal(a.native_retired, true)
    assert.equal(a.consumer.problem_request_matches, true)
    assert.equal(x.written.network.incomplete_entries.length, 3); assert.equal(x.written.network.incomplete_truncated, false)
  })
  for (const record of [{ method: 'POST' }, { path: '/different' }, { query: 'other=1' }, { status: 200 }, { content_type: 'application/json' }, { request_b64: Buffer.from('{}').toString('base64') }, { key: 'different' }, { csrf_sha256: 'different' }, { request_id: id(91) }]) await run('actual helper private mismatch: ' + Object.keys(record)[0], async () => {
    const x = await adapter({ record }); assert.equal(x.written.authority.private_bound, false)
  })
  await run('duplicate original request cannot bind consumer', async () => { const x = await adapter({ duplicate: true }); assert.equal(x.written.authority.request_bound, false); assert.equal(x.written.authority.private_bound, false); assert.equal(x.written.authority.consumer.problem_request_matches, false) })
  await run('closed page cannot publish consumer', async () => { const x = await adapter({ closed: true }); assert.equal(x.written.authority.consumer, null) })
  await run('failed native retirement remains explicit', async () => { const x = await adapter({ retired: false }); assert.equal(x.written.authority.native_retired, false) })
  await run('stage timeout and late evaluation cannot upgrade frozen diagnostic', async () => { const x = await adapter({ hung: true }); const before = JSON.stringify(x.written.authority); assert.equal(x.written.authority.joined, false); x.held.resolve(consumerSample); await flush(); await x.diagnostic.finish(); assert.equal(JSON.stringify(x.written.authority), before) })
  await run('incomplete projection cap is explicit without changing gate', async () => { const x = await adapter({ more: 193 }); assert.equal(x.written.network.incomplete_entries.length, 192); assert.equal(x.written.network.incomplete_truncated, true) })
  console.log(`authority observer controls: ${checks} PASS; no browser/server/database`)
}
main().catch(e => { console.error(e); process.exitCode = 1 })
