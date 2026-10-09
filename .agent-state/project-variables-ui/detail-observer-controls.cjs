#!/usr/bin/env node
// Execute the original PW-serialized observer. DOM/Session/native are disclosed
// doubles here; detail-consumer-controls separately exercises production code.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const root = path.resolve(__dirname, '../..')
process.env.PWTEST_CACHE_DIR = path.join(root, 'output/ai/project-variables-ui/implementation/pw-detail-offline-cache')
const { transformHook } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright/lib/transform/transform.js'))
const { evaluationScript } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright-core/lib/client/clientHelper.js'))
const { JSDOM } = require(path.join(root, 'web/node_modules/jsdom'))
const { EventEmitter } = require('node:events'), { createHash } = require('node:crypto')
const ts = require(path.join(root, 'web/node_modules/typescript'))
const { expect } = require(path.join(root, 'tests/account-captcha-web/node_modules/@playwright/test'))
function load(name) {
  const filename = path.join(root, 'tests/account-captcha-web/e2e', name + '.ts'), exports = {}
  new Function('require', 'exports', transformHook(fs.readFileSync(filename, 'utf8'), filename).code)(require, exports)
  return exports
}
const source = load('project-variables.detail'), authority = load('project-variables.authority')
const binding = authority.variableAuthorityBinding(root, path.join(root, 'output/ai/project-variables-ui/dist-03'))
const id = n => '01970000-0000-7000-8000-' + String(n).padStart(12, '0')
const target = { project: id(10), variable: id(20), route: '/owner/project/settings/variables' }, xid = id(90)
const endpoint = `/api/v1/projects/${target.project}/variables/${target.variable}`
const value = { id: target.variable, project_id: target.project, type: 'variable', name: 'VALUE', description: 'PRIVATE_DESCRIPTION', version: '1', created_at: '2026-10-09T12:00:00.000001Z', updated_at: '2026-10-09T12:00:00.000001Z', value: 'PRIVATE_VALUE' }
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
const nativeProof = () => ({ binding: 'bound', sample_count: 1, sample_settled: 1, sample_joined: true, sample_failed: 0, hooks_retired: true, eof_before_interruption: true, length_comparable: true, length_matches: true,
  facts: { failure: 'none', readers: 1, read_calls: 2, read_settled: 2, read_rejected: 0, reader_cancel_calls: 1, reader_cancel_settled: 1, reader_cancel_rejected: 0, stream_cancel_calls: 1, stream_cancel_settled: 1, stream_cancel_rejected: 0, release_calls: 1, release_successes: 1, abort_events: 0 } })
async function fixture(options = {}) {
  const held = deferred(), calls = [], native = { document: 'original-document', retired: false, records: [] }
  const auth = { state: { phase: 'authenticated', busy: false, user: { id: id(1) }, session: { id: id(2) } }, personalContext: { phase: 'current', identity: { userID: id(1), sessionID: id(2), epoch: 1 } }, projectVariables: { progress: null, get(...args) { calls.push({ receiver: this, args }); if (options.throw) throw options.throw; return held.promise } } }
  const original = auth.projectVariables.get
  if (options.frozen) Object.freeze(auth.projectVariables)
  const dom = new JSDOM(options.stale ? '<section class="variable-editor"></section>' : '', { url: 'http://offline.invalid' + target.route })
  const host = { __variableNativeDiagnostic: { snapshot: () => structuredClone(native) } }
  const context = vm.createContext({ window: host, document: dom.window.document, HTMLInputElement: dom.window.HTMLInputElement, HTMLTextAreaElement: dom.window.HTMLTextAreaElement, Error, URL,
    location: dom.window.location, performance: { getEntriesByName: () => options.unloaded ? [] : [{}] }, Function: function () { return async () => ({ [binding.singleton]: () => auth }) } })
  const installed = await vm.runInContext(await evaluationScript(null, source.installVariableDetail, { binding, target }), context)
  const request = () => native.records.push({ request_id: xid, method: 'GET', path: endpoint, query: '', status: 200 })
  const publish = (v = value) => {
    dom.window.document.body.innerHTML = '<section class="variable-editor"><p class="meta"></p><form class="variable-form"><label for="n">名称</label><input id="n"><label for="d">描述</label><textarea id="d"></textarea><label for="v">值</label><textarea id="v"></textarea></form></section>'
    dom.window.document.querySelector('.meta').textContent = `变量 ID：${v.id} · 已读版本 ${v.version}`
    dom.window.document.getElementById('n').value = v.name; dom.window.document.getElementById('d').value = v.description; dom.window.document.getElementById('v').value = v.value
  }
  return { installed, auth, original, native, host, dom, held, calls, request, publish, finish: (v = value, requestID = xid) => source.variableDetailProjection(host.__variableDetail.finish({ id: requestID, value: v })) }
}
let checks = 0
const modules = new Map()
function apiModule(name) {
  assert(['client', 'account', 'system-account', 'project-variables'].includes(name))
  if (modules.has(name)) return modules.get(name)
  const exports = {}; modules.set(name, exports)
  const filename = path.join(root, 'web/src/api', name + '.ts')
  new Function('exports', 'require', ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText)(exports, dependency => apiModule(dependency.slice(2)))
  return exports
}
async function adapter(consumer, options = {}) {
  const ast = ts.createSourceFile('helpers.ts', fs.readFileSync(path.join(root, 'tests/account-captcha-web/e2e/project-variables.helpers.ts'), 'utf8'), ts.ScriptTarget.Latest, true)
  const names = new Set(['emptyNetworkDiagnostic', 'networkObservations', 'networkRetire', 'authorityObservations', 'detailObservations', 'observe', 'recordFailure', 'originalResponse'])
  const code = ts.transpileModule(ast.statements.filter(n => ts.isFunctionDeclaration(n) && names.has(n.name?.text) || ts.isVariableStatement(n) && n.declarationList.declarations.some(d => names.has(d.name.getText(ast)))).map(n => n.getText(ast)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
  const raw = Buffer.from(JSON.stringify(value)), records = [{ method: 'GET', path: endpoint, query: '', status: 200, content_type: 'application/json', request_id: xid, request_b64: '', body_b64: raw.toString('base64'), key: '', csrf_sha256: createHash('sha256').update('').digest('hex') }]
  if (options.record) Object.assign(records[0], options.record)
  let now = 1000, written, validations = 0, stops = 0, extraBodyReads = 0
  const native = nativeProof(); native.facts.bytes = raw.length
  if (options.native) Object.assign(native, options.native)
  if (options.bytes) native.facts.bytes++
  const dependencies = { exports: {}, expect, uuid7: apiModule('client').uuid7, createHash, Buffer, join: path.join, repository: root, directory: 'private', step: 1, Date: { now: () => now },
    emptyNativeDiagnostic: () => ({ binding: 'unobserved', facts: null }),
    nativeConsumption: () => ({ request() {}, response() {}, stop: async () => { stops++ }, endDocument: async () => options.retired !== false, snapshot: () => native }),
    readFileSync: () => JSON.stringify(records[0]), readdirSync: () => ['project-variables-response-1.json'], writeFileSync: (_p, data) => { written = JSON.parse(data) },
    createProjectVariablesAPI: apiModule('project-variables').createProjectVariablesAPI,
    validateOriginalBodies: async (entries, _allow, qualifies) => { validations++; assert.equal(qualifies(entries[0]), !options.ordinary); },
    variableAuthorityBinding: () => binding, installVariableAuthority: authority.installVariableAuthority, variableAuthorityProjection: authority.variableAuthorityProjection,
    installVariableDetail: source.installVariableDetail, variableDetailProjection: source.variableDetailProjection, variableDetailComplete: source.variableDetailComplete,
    process: { env: { AGENTEAM_PROJECT_VARIABLE_WEB_CASE: options.mode ?? 'authority' } }, path: () => target.route,
  }
  new Function(...Object.keys(dependencies), code)(...Object.values(dependencies))
  const page = new EventEmitter()
  page.evaluate = async (fn, input) => {
    if (fn === source.installVariableDetail || fn === authority.installVariableAuthority) return options.install !== false
    if (input === undefined) return { observed: true, variables: true, editor: true, close: true, history: false, confirmed: false, uncertain: false, dialog: false }
    if (input && typeof input === 'object' && 'value' in input) {
      if (options.late) now += options.elapsed ?? 251
      return { ...consumer, body_matches: !!input.value && JSON.stringify(input.value) === JSON.stringify(apiModule('project-variables').parseProjectVariable(value, target.project, target.variable)), native_request_matches: input.id === xid }
    }
    return null
  }
  const observed = dependencies.exports.observe(page), data = { ids: { main: target.project }, targets: { main: target.variable }, variables: { main: value }, diagnostic_dist: 'actual-bound-dist' }
  if (options.mode && options.mode !== 'authority') { await assert.rejects(observed.detail(data)); return }
  const detail = await observed.detail(data)
  const request = { method: () => options.method ?? 'GET', url: () => 'http://offline.invalid' + endpoint + (options.query ?? ''), postData: () => options.body ?? null, allHeaders: async () => options.headers ?? {} }
  const response = { request: () => options.foreignRequest ? {} : request, status: () => 200, headers: () => ({ 'x-request-id': xid }), headerValue: async name => name === 'x-request-id' ? xid : 'application/json', finished: async () => null, body: async () => { extraBodyReads++; throw Error('extra body consumption') } }
  page.emit('request', request); page.emit('response', response)
  page.emit(options.ordinary ? 'requestfinished' : 'requestfailed', request)
  if (options.duplicate) page.emit('requestfailed', request)
  if (options.duplicateResponse) page.emit('response', response)
  if (options.extra) {
    const extra = { ...request, method: () => options.extra === 'PATCH' ? 'PATCH' : 'GET', url: () => 'http://offline.invalid' + endpoint + (options.extra === 'other' ? '/other' : '') }
    page.emit('request', extra); page.emit('response', { ...response, request: () => extra }); page.emit('requestfailed', extra)
  }
  await detail.finish()
  const a = await observed.authority(data); await a.finish()
  if (options.fail) { await assert.rejects(observed.finish()); assert.equal(validations, 0) }
  else { const report = await observed.finish(); assert.equal(report.equivalent_completed, options.ordinary ? 0 : 1); assert.equal(report.completed, options.ordinary ? 1 : 0); assert.equal(validations, 1) }
  await dependencies.exports.recordFailure(page, { status: 'failed', expectedStatus: 'passed', errors: [{ stack: 'private at project-variables.helpers.ts:700:1' }] })
  assert(stops > 0); assert.equal(extraBodyReads, 0)
  assert(!JSON.stringify(written).includes(value.value)); assert(!JSON.stringify(written).includes(target.project)); assert(!JSON.stringify(written).includes(xid))
  return written
}
async function positive(mutator) {
  const f = await fixture(); assert.equal(f.installed, true)
  const wrapper = f.auth.projectVariables.get, receiver = { marker: true }, args = [target.project, target.variable]
  const promise = Reflect.apply(wrapper, receiver, args); assert.equal(promise, f.held.promise); assert.equal(f.calls[0].receiver, receiver); assert.deepEqual(f.calls[0].args, args)
  f.request(); f.held.resolve(value); await flush(); f.publish()
  if (mutator) await mutator(f)
  const beforeRetirement = f.auth.projectVariables.get
  const facts = f.finish(); assert.equal(f.auth.projectVariables.get, beforeRetirement === wrapper ? f.original : beforeRetirement)
  assert(!JSON.stringify(facts).includes(value.value)); assert(!JSON.stringify(facts).includes(value.description)); assert(!JSON.stringify(facts).includes(xid)); assert(!JSON.stringify(facts).includes(target.variable))
  return { f, facts, wrapper }
}
;(async () => {
  const p = await positive(); assert(source.variableDetailComplete(p.facts, nativeProof())); checks++
  const frozen = JSON.stringify(p.facts); assert.equal(Reflect.apply(p.wrapper, p.f.auth.projectVariables, ['cached', 'argument']), p.f.held.promise); await flush(); assert.equal(JSON.stringify(p.f.finish()), frozen); checks++
  for (const options of [{ frozen: true }, { stale: true }, { unloaded: true }]) { assert.equal((await fixture(options)).installed, false); checks++ }
  const thrown = Error('private original throw'), t = await fixture({ throw: thrown }); assert.throws(() => t.auth.projectVariables.get(target.project, target.variable), e => e === thrown); assert.equal(t.finish().synchronous_throws, 1); checks++
  const pending = await fixture(); pending.auth.projectVariables.get(target.project, target.variable); pending.request(); pending.publish(); const before = pending.finish(); assert.equal(before.pending, 1); assert.equal(before.fulfilled, 0); assert(!source.variableDetailComplete(before, nativeProof())); pending.held.resolve(value); await flush(); assert.equal(pending.finish().fulfilled, 0); checks++
  const rejected = await fixture(); const rejectedPromise = rejected.auth.projectVariables.get(target.project, target.variable); rejected.request(); rejected.publish(); rejected.held.reject(Error('private rejection')); await rejectedPromise.catch(() => {}); await flush(); const refused = rejected.finish(); assert.equal(refused.rejected, 1); assert(!source.variableDetailComplete(refused, nativeProof())); checks++
  for (const mutate of [
    f => { f.auth.personalContext.phase = 'checking' }, f => { f.auth.state.user.id = id(3) }, f => { f.auth.state.session.id = id(4) },
    f => { f.auth.state.busy = true }, f => { f.auth.state.phase = 'checking' }, f => { f.auth.personalContext.identity.epoch++ },
    f => { f.native.document = 'new-document' }, f => { f.native.retired = true }, f => { f.native.records[0].request_id = id(91) },
    f => { f.native.records.push({ ...f.native.records[0] }) }, f => { f.native.records[0].method = 'PATCH' },
    f => { f.native.records[0].path += '/other' }, f => { f.native.records[0].query = '?cursor=x' }, f => { f.native.records[0].status = 201 },
    f => { f.dom.window.history.replaceState(null, '', '/other') }, f => { f.publish({ ...value, value: 'old draft' }) },
    f => { f.publish({ ...value, version: '2' }) }, f => { f.dom.window.document.getElementById('v').disabled = true },
    f => { f.auth.projectVariables.progress = { phase: 'confirmed' } }, f => { f.auth.projectVariables.get = () => Promise.resolve(value) },
    f => { f.dom.window.document.querySelector('.variable-form').remove() },
  ]) { const out = await positive(mutate); assert(!source.variableDetailComplete(out.facts, nativeProof())); checks++ }
  for (const mutate of [
    f => { f.request(); f.auth.projectVariables.get(target.project, target.variable) },
    f => { f.auth.projectVariables.get('wrong-project', target.variable) },
  ]) { const f = await fixture(); mutate(f); f.request(); f.held.resolve(value); await flush(); f.publish(); assert(!source.variableDetailComplete(f.finish(), nativeProof())); checks++ }
  for (const [key, v] of Object.entries(p.facts)) {
    const changed = { ...p.facts, [key]: typeof v === 'boolean' ? !v : v + 1 }
    assert(!source.variableDetailComplete(changed, nativeProof()), key); checks++
  }
  for (const key of Object.keys(nativeProof())) {
    const n = nativeProof(); if (key === 'facts') n.facts = null; else if (typeof n[key] === 'boolean') n[key] = false; else if (typeof n[key] === 'number') n[key]++; else n[key] = 'ambiguous'
    assert(!source.variableDetailComplete(p.facts, n), key); checks++
  }
  for (const key of Object.keys(nativeProof().facts)) { const n = nativeProof(); n.facts[key] = typeof n.facts[key] === 'number' ? n.facts[key] + 1 : 'observer-error'; assert(!source.variableDetailComplete(p.facts, n), key); checks++ }
  for (const bad of [{ ...p.facts, value: 'private' }, { ...p.facts, calls: 0.5 }, { ...p.facts, calls: -1 }, { ...p.facts, calls: 4097 }, { ...p.facts, owner_idle: 1 }]) { assert.equal(source.variableDetailProjection(bad), null); checks++ }
  await adapter(p.facts); checks++
  await adapter(p.facts, { late: true, elapsed: 249 }); checks++
  await adapter(p.facts, { ordinary: true }); checks++
  for (const options of [
    { install: false }, { retired: false }, { late: true }, { late: true, elapsed: 250 }, { bytes: true }, { duplicate: true }, { duplicateResponse: true }, { foreignRequest: true },
    { extra: 'same' }, { extra: 'other' }, { extra: 'PATCH' }, { method: 'PATCH' }, { query: '?different=1' }, { body: '{}' },
    { headers: { 'idempotency-key': 'wrong' } }, { headers: { 'x-csrf-token': 'wrong' } },
    { record: { method: 'PATCH' } }, { record: { path: endpoint + '/other' } }, { record: { query: 'different=1' } }, { record: { status: 201 } },
    { record: { content_type: 'text/plain' } }, { record: { key: 'wrong' } }, { record: { csrf_sha256: 'wrong' } }, { record: { request_b64: Buffer.from('{}').toString('base64') } },
    { record: { body_b64: Buffer.from(JSON.stringify({ ...value, id: id(21) })).toString('base64') } },
    { native: { sample_count: undefined, sample_settled: undefined } }, { native: { sample_count: 0, sample_settled: 0 } }, { native: { sample_count: 4097, sample_settled: 4097 } },
    { native: { sample_joined: false } }, { native: { hooks_retired: false } },
  ]) { await adapter(p.facts, { ...options, fail: true }); checks++ }
  await adapter(p.facts, { mode: 'crud' }); checks++
  console.log('detail serialized observer controls', checks, 'PASS')
})().catch(error => { console.error(error); process.exitCode = 1 })
