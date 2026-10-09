#!/usr/bin/env node
// Locked Playwright transform + actual init-script serialization. Explicit
// reader/PW substitutes exercise diagnostics only, not product acceptance.
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const { EventEmitter } = require('node:events')
const { webcrypto } = require('node:crypto')
const { spawnSync } = require('node:child_process')
const root = path.resolve(__dirname, '../..')
process.env.PWTEST_CACHE_DIR = path.join(root, 'output/ai/project-variables-ui/implementation/pw-offline-cache')
const { transformHook } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright/lib/transform/transform.js'))
const { evaluationScript } = require(path.join(root, 'tests/account-captcha-web/node_modules/playwright-core/lib/client/clientHelper.js'))
const filename = path.join(root, 'tests/account-captcha-web/e2e/project-variables.native.ts')
const code = transformHook(fs.readFileSync(filename, 'utf8'), filename).code
const id = (n) => '01900000-0000-7000-8000-' + String(n).padStart(12, '0')
const pathname = `/api/v1/projects/${id(1)}/variables/${id(2)}`, requestID = id(3)
const flush = async () => { for (let n = 0; n < 8; n++) await Promise.resolve() }
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
function scheduler() {
  let now = 0, sequence = 0
  const timers = new Map()
  return { Date: { now: () => now }, timers,
    setTimeout(fn, ms) { const key = ++sequence; timers.set(key, { at: now + ms, fn }); return key },
    clearTimeout(key) { timers.delete(key) },
    async advance(ms) { now += ms; for (const [key, value] of [...timers]) if (value.at <= now && timers.delete(key)) value.fn(); await flush() },
  }
}
function moduleWith(clock) {
  const output = {}
  new Function('exports', 'setTimeout', 'clearTimeout', 'Date', code)(output, clock.setTimeout, clock.clearTimeout, clock.Date)
  return output
}
async function browser(options = {}) {
  const clock = scheduler(), module = moduleWith(clock), pending = deferred(), signal = new AbortController()
  const counts = { fetch: 0, read: 0, readerCancel: 0, streamCancel: 0, release: 0 }
  const calls = []
  const readValues = options.readValues ?? [{ done: false, value: new Uint8Array([1, 2, 3]) }, { done: true }]
  const reads = readValues.map((value) => Promise.resolve(value))
  const readerCancel = deferred(), streamCancel = deferred()
  const reader = {
    read(...args) { calls.push(['read', this, args]); const at = counts.read++; if (options.readThrow) throw options.readThrow; return reads[at] },
    cancel(...args) { calls.push(['cancel', this, args]); counts.readerCancel++; if (options.cancelThrow) throw options.cancelThrow; return readerCancel.promise },
    releaseLock(...args) { calls.push(['release', this, args]); counts.release++; if (options.releaseThrow) throw options.releaseThrow },
  }
  const stream = { getReader(...args) { calls.push(['getReader', this, args]); if (options.readerThrow) throw options.readerThrow; return reader }, cancel(...args) { calls.push(['streamCancel', this, args]); counts.streamCancel++; if (options.streamCancelThrow) throw options.streamCancelThrow; return streamCancel.promise } }
  const original = { getReader: stream.getReader, read: reader.read, cancel: reader.cancel, streamCancel: stream.cancel, release: reader.releaseLock }
  const response = { status: 200, headers: new Headers({ 'X-Request-ID': requestID, 'Content-Length': options.length ?? '3', ...(options.encoding ? { 'Content-Encoding': options.encoding } : {}) }), body: stream }
  const window = { fetch(...args) { calls.push(['fetch', this, args]); counts.fetch++; if (options.fetchThrow) throw options.fetchThrow; return pending.promise } }
  const originalFetch = window.fetch
  const context = vm.createContext({ window, location: { origin: 'http://offline.invalid' }, crypto: webcrypto, URL, Request, setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout })
  vm.runInContext(await evaluationScript(null, module.installVariableNativeDiagnostic), context)
  const fetchPromise = window.fetch(options.url ?? pathname, { method: 'GET', signal: signal.signal, headers: { private: 'NATIVE_PRIVATE_CANARY' } })
  assert.equal(fetchPromise, pending.promise)
  pending.resolve(response); await pending.promise; await flush()
  const snapshot = (end = false) => JSON.parse(JSON.stringify(window.__variableNativeDiagnostic.snapshot(end)))
  return { clock, module, context, window, originalFetch, original, counts, calls, reader, stream, reads, signal, readerCancel, streamCancel, snapshot }
}
async function completed(options = {}) {
  const b = await browser(options)
  assert.equal(b.stream.getReader(), b.reader)
  for (const expected of b.reads) { const original = b.reader.read(); assert.equal(original, expected); await original }
  const rc = b.reader.cancel(); assert.equal(rc, b.readerCancel.promise); b.readerCancel.resolve(); await rc
  b.reader.releaseLock()
  const sc = b.stream.cancel(); assert.equal(sc, b.streamCancel.promise); b.streamCancel.resolve(); await sc
  await flush()
  return b
}
function fakePage(clock, value) {
  const page = new EventEmitter(), frame = {}
  page.mainFrame = () => frame
  page.evaluations = 0; page.active = 0; page.maximum = 0
  page.evaluate = (_fn, end) => {
    page.evaluations++; page.active++; page.maximum = Math.max(page.maximum, page.active)
    const result = typeof value === 'function' ? value(end) : { ...value, retired: !!end }
    return Promise.resolve(result).finally(() => { page.active-- })
  }
  return page
}
async function bind(snapshot, mutate = () => {}, expected = 'bound') {
  const clock = scheduler(), module = moduleWith(clock), page = fakePage(clock, snapshot), adapter = module.nativeConsumption(page)
  const request = { url: () => 'http://offline.invalid' + pathname, method: () => 'GET' }
  const response = { request: () => request, headers: () => ({ 'x-request-id': requestID }), status: () => 200 }
  adapter.request(request); adapter.response(response)
  mutate({ adapter, page, request, response, snapshot })
  await flush(); await adapter.stop()
  const result = adapter.snapshot(request)
  assert.equal(result.binding, expected)
  assert.equal(page.maximum, 1)
  assert.equal(clock.timers.size, 0)
  assert(!JSON.stringify(result).includes('NATIVE_PRIVATE_CANARY'))
  assert(!JSON.stringify(result).includes(requestID))
  return result
}
async function lateEventsControl(snapshot) {
  for (const responseBeforeStop of [false, true]) {
    const clock = scheduler(), page = fakePage(clock, snapshot), adapter = moduleWith(clock).nativeConsumption(page)
    const request = { url: () => 'http://offline.invalid' + pathname, method: () => 'GET' }
    const response = { request: () => request, headers: () => ({ 'x-request-id': requestID }), status: () => 200 }
    adapter.request(request)
    if (responseBeforeStop) adapter.response(response)
    await flush(); await adapter.stop()
    const before = JSON.stringify(adapter.snapshot(request))
    adapter.response(response)
    assert.equal(JSON.stringify(adapter.snapshot(request)), before)
    const late = { ...request }; adapter.request(late); adapter.response({ ...response, request: () => late })
    assert.equal(JSON.stringify(adapter.snapshot(request)), before)
    assert.equal(adapter.snapshot(late).binding, 'unobserved')
  }
  console.log('late PW request/response cannot bind, upgrade or invalidate a retired snapshot: PASS')
}
async function installerFailureControl() {
  const frozen = await browser(); Object.freeze(frozen.reader)
  assert.equal(frozen.stream.getReader(), frozen.reader)
  assert.equal(frozen.snapshot().records[0].facts.failure, 'observer-error')
  frozen.snapshot(true)
  const partial = await browser()
  Object.defineProperty(partial.reader, 'cancel', { writable: false, configurable: false })
  assert.equal(partial.stream.getReader(), partial.reader)
  for (const expected of partial.reads) { const pending = partial.reader.read(); assert.equal(pending, expected); await pending }
  await flush()
  const bound = await bind(partial.snapshot())
  assert.equal(bound.facts.read_done, true)
  assert.equal(bound.facts.failure, 'observer-error')
  assert.equal(bound.eof_before_interruption, false)
  assert.equal(bound.length_comparable, false)
  Object.defineProperty(partial.reader, 'read', { writable: false, configurable: false })
  assert.equal(partial.snapshot(true).retired, false)
  const getters = await browser(), originalError = new Error('NATIVE_PRIVATE_CANARY')
  Object.defineProperty(getters.reader, 'read', { get() { throw originalError }, configurable: true })
  assert.equal(getters.stream.getReader(), getters.reader)
  assert.equal(getters.snapshot().records[0].facts.failure, 'observer-error')
  getters.snapshot(true)
  console.log('frozen/partial/getter installation preserves original return/Promise; incomplete observation never proves EOF or hook retirement: PASS')
}
async function cachedRetirementControl() {
  const b = await browser(), cachedFetch = b.window.fetch, cachedGetReader = b.stream.getReader, cachedStreamCancel = b.stream.cancel
  const reader = b.stream.getReader(), cachedRead = reader.read, cachedCancel = reader.cancel, cachedRelease = reader.releaseLock
  const before = JSON.stringify(b.snapshot(true))
  const from = b.calls.length
  assert.equal(Reflect.apply(cachedGetReader, b.stream, []), reader)
  assert.equal(reader.read, b.original.read)
  assert.equal(reader.cancel, b.original.cancel)
  assert.equal(reader.releaseLock, b.original.release)
  const read = Reflect.apply(cachedRead, reader, []); assert.equal(read, b.reads[0]); await read
  const cancel = Reflect.apply(cachedCancel, reader, ['NATIVE_PRIVATE_CANARY']); assert.equal(cancel, b.readerCancel.promise); b.readerCancel.resolve(); await cancel
  assert.equal(Reflect.apply(cachedRelease, reader, []), undefined)
  const stream = Reflect.apply(cachedStreamCancel, b.stream, ['NATIVE_PRIVATE_CANARY']); assert.equal(stream, b.streamCancel.promise); b.streamCancel.resolve(); await stream
  let inspected = 0
  const input = { get url() { inspected++; return pathname } }
  const fetch = Reflect.apply(cachedFetch, b.window, [input]); await fetch
  assert.equal(inspected, 0)
  await flush()
  assert.equal(JSON.stringify(b.snapshot()), before)
  assert.deepEqual(b.counts, { fetch: 2, read: 1, readerCancel: 1, streamCancel: 1, release: 1 })
  const expectedCalls = [['getReader', b.stream, []], ['read', reader, []], ['cancel', reader, ['NATIVE_PRIVATE_CANARY']], ['release', reader, []], ['streamCancel', b.stream, ['NATIVE_PRIVATE_CANARY']], ['fetch', b.window, [input]]]
  assert.equal(b.calls.length - from, expectedCalls.length)
  b.calls.slice(from).forEach((call, index) => { assert.equal(call[0], expectedCalls[index][0]); assert.equal(call[1], expectedCalls[index][1]); assert.deepEqual(call[2], expectedCalls[index][2]) })
  for (const [field, name] of [['readThrow', 'read'], ['cancelThrow', 'cancel'], ['releaseThrow', 'releaseLock'], ['streamCancelThrow', 'cancel'], ['readerThrow', 'getReader']]) {
    const error = new Error('NATIVE_PRIVATE_CANARY'), x = await browser({ [field]: error })
    const receiver = field === 'streamCancelThrow' || field === 'readerThrow' ? x.stream : x.stream.getReader()
    const callback = receiver[name], snapshot = JSON.stringify(x.snapshot(true))
    assert.throws(() => Reflect.apply(callback, receiver, []), (caught) => caught === error)
    assert.equal(JSON.stringify(x.snapshot()), snapshot)
  }
  console.log('cached retired fetch/getReader/read/cancel/release delegate exactly once and never observe or reinstall: PASS')
}
function goProjectionControl(native) {
  const fixture = fs.readFileSync(path.join(root, 'tests/account/project_variables_web_fixture_test.go'), 'utf8')
  const types = fixture.slice(fixture.indexOf('type variableWebNetworkFailure struct'), fixture.indexOf('// Independent SQL postconditions'))
  assert(types.includes('func (n variableWebNativeDiagnostic) valid() bool'))
  const accepted = { reason: 'unexpected-failed', index: 1, method: 'GET', route: 'detail', received: true, status: 200, finished: false, failed: true, expected: 'none', total: 1, completed: 0, incomplete: 1, native }
  const program = `package main
import("bytes";"encoding/json";"fmt";"io")
${types}
func check(raw []byte) bool { var n variableWebNetworkFailure; d:=json.NewDecoder(bytes.NewReader(raw)); d.DisallowUnknownFields(); if d.Decode(&n)!=nil{return false}; var tail any; if d.Decode(&tail)!=io.EOF{return false};return n.valid()&&n.Native!=nil&&n.Native.valid() }
func main(){
 original:=[]byte(${JSON.stringify(JSON.stringify(accepted))})
 if !check(original){panic("positive projection")}
 var incomplete map[string]any;json.Unmarshal(original,&incomplete);ni:=incomplete["native"].(map[string]any);ni["facts"].(map[string]any)["failure"]="observer-error";ni["eof_before_interruption"]=false;ni["length_comparable"]=false;ni["length_matches"]=false;iraw,_:=json.Marshal(incomplete);if !check(iraw){panic("incomplete observation projection")}
 changes:=[]func(map[string]any){
  func(m map[string]any){m["body"]="CANARY"},
  func(m map[string]any){m["reason"]="CANARY"},
  func(m map[string]any){m["method"]="CANARY"},
  func(m map[string]any){m["route"]="CANARY"},
  func(m map[string]any){m["total"]=4097},
  func(m map[string]any){m["index"]=2},
  func(m map[string]any){m["native"].(map[string]any)["binding"]="CANARY"},
  func(m map[string]any){m["native"].(map[string]any)["sample_count"]=-1},
  func(m map[string]any){m["native"].(map[string]any)["facts"]=nil},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["failure"]="CANARY"},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["failure"]="observer-error"},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["body"]="CANARY"},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["bytes"]=-1},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["read_done_order"]=1048577},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["cancel_before_eof"]=true},
  func(m map[string]any){m["native"].(map[string]any)["facts"].(map[string]any)["content_encoding_identity"]=false},
 }
 for i,change:=range changes{var m map[string]any;json.Unmarshal(original,&m);change(m);raw,_:=json.Marshal(m);if check(raw){panic(fmt.Sprint("negative ",i))}}
 if check(append(append([]byte{},original...),[]byte(" {}")...))||check(original[:len(original)-1]){panic("EOF")}
 fmt.Println("actual Go typed decoder: closed fields/enums/ranges/EOF and derived EOF/length flags PASS")
}`
  const directory = fs.mkdtempSync(path.join(root, 'output/ai/project-variables-ui/implementation/native-go-control-'))
  try {
    const file = path.join(directory, 'main.go'); fs.writeFileSync(file, program)
    const env = { ...process.env, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOSUMDB: 'off', GOTELEMETRY: 'off', GOMAXPROCS: '2', GOMODCACHE: '/workspace/agenteam/output/ai/model-ui-recovery/go-mod', GOCACHE: path.join(root, 'output/ai/project-variables-ui/implementation/gocache'), GOTMPDIR: path.join(root, 'output/ai/project-variables-ui/implementation/tmp') }
    const result = spawnSync('/workspace/toolchains/go1.27.1/bin/go', ['run', '-p=1', file], { cwd: root, env, encoding: 'utf8', timeout: 45000, maxBuffer: 8192 })
    assert.equal(result.status, 0, result.stderr)
    process.stdout.write(result.stdout)
  } finally { fs.rmSync(directory, { recursive: true }) }
}
async function main() {
  const unhandled = []
  process.on('unhandledRejection', (error) => unhandled.push(error))
  const b = await completed(), snap = b.snapshot()
  if (process.argv.includes('--late-only')) { await lateEventsControl(snap); b.snapshot(true); return }
  if (process.argv.includes('--installer-only')) { b.snapshot(true); await installerFailureControl(); return }
  if (process.argv.includes('--cached-only')) { b.snapshot(true); await cachedRetirementControl(); return }
  assert.deepEqual(b.counts, { fetch: 1, read: 2, readerCancel: 1, streamCancel: 1, release: 1 })
  let result = await bind(snap)
  assert(result.eof_before_interruption && result.length_comparable && result.length_matches && result.hooks_retired)
  if (!process.argv.includes('--js-only')) goProjectionControl(result)
  if (process.argv.includes('--go-only')) { b.snapshot(true); return }
  b.snapshot(true)
  assert.equal(b.window.fetch, b.originalFetch); assert.equal(b.reader.read, b.original.read); assert.equal(b.stream.getReader, b.original.getReader)
  console.log('locked PW init serialization; original fetch/read/cancel Promise identity, EOF and restoration: PASS')
  for (const [options, comparable, matches] of [[{ encoding: 'gzip' }, false, false], [{ length: 'x' }, false, false], [{ length: '4' }, true, false]]) {
    const x = await completed(options); result = await bind(x.snapshot()); assert.equal(result.length_comparable, comparable); assert.equal(result.length_matches, matches); x.snapshot(true)
  }
  const canceled = await browser({ readValues: [{ done: true }] })
  canceled.stream.getReader(); const rc = canceled.reader.cancel(); canceled.readerCancel.resolve(); await rc; await canceled.reader.read(); await flush()
  result = await bind(canceled.snapshot()); assert(!result.eof_before_interruption); canceled.snapshot(true)
  const aborted = await browser(); aborted.stream.getReader(); aborted.signal.abort(); for (const unused of aborted.reads) await aborted.reader.read(); await flush()
  result = await bind(aborted.snapshot()); assert(!result.eof_before_interruption); aborted.snapshot(true)
  console.log('cancel-induced done, abort-before-EOF, invalid/compressed/mismatched lengths: PASS')
  for (const field of ['readerThrow', 'readThrow', 'releaseThrow']) {
    const failure = new Error('NATIVE_PRIVATE_CANARY'), x = await browser({ [field]: failure })
    assert.throws(() => field === 'readerThrow' ? x.stream.getReader() : field === 'readThrow' ? x.stream.getReader().read() : x.stream.getReader().releaseLock(), (e) => e === failure)
    assert.notEqual(x.snapshot().records[0].facts.failure, 'none'); x.snapshot(true)
  }
  const rejected = await browser(); rejected.stream.getReader(); const cancel = rejected.reader.cancel(); rejected.readerCancel.reject(new Error('NATIVE_PRIVATE_CANARY')); await assert.rejects(cancel); await flush(); assert.equal(rejected.snapshot().records[0].facts.reader_cancel_rejected, 1); rejected.snapshot(true)
  const rejectedStream = await browser(); const streamCancel = rejectedStream.stream.cancel(); rejectedStream.streamCancel.reject(new Error('NATIVE_PRIVATE_CANARY')); await assert.rejects(streamCancel); await flush(); assert.equal(rejectedStream.snapshot().records[0].facts.stream_cancel_rejected, 1); rejectedStream.snapshot(true)
  for (const key of ['cancelThrow', 'streamCancelThrow']) {
    const failure = new Error('NATIVE_PRIVATE_CANARY'), x = await browser({ [key]: failure })
    assert.throws(() => key === 'cancelThrow' ? x.stream.getReader().cancel() : x.stream.cancel(), (error) => error === failure)
    assert.notEqual(x.snapshot().records[0].facts.failure, 'none'); x.snapshot(true)
  }
  const fetchThrow = new Error('NATIVE_PRIVATE_CANARY'); await assert.rejects(browser({ fetchThrow }), (error) => error === fetchThrow)
  const ownership = await browser(); const foreign = () => Promise.resolve(); ownership.stream.cancel = foreign; ownership.snapshot(true); assert.equal(ownership.stream.cancel, foreign)
  const frozen = await browser(); Object.defineProperty(frozen.stream, 'cancel', { configurable: false, writable: false }); assert.equal(frozen.snapshot(true).retired, false)
  const lateRead = deferred(), late = await browser({ readValues: [lateRead.promise] }); late.stream.getReader(); const originalRead = late.reader.read(); late.snapshot(true); lateRead.resolve({ done: true }); await originalRead; await flush(); assert.equal(late.snapshot().records[0].facts.read_done, false)
  const expired = await browser(); await expired.clock.advance(45_000); assert.equal(expired.window.fetch, expired.originalFetch); assert.equal(expired.counts.readerCancel + expired.counts.streamCancel, 0)
  const excluded = await browser({ url: '/api/v1/session' }); assert.equal(excluded.snapshot().records.length, 0); assert.equal(excluded.counts.fetch, 1); excluded.snapshot(true)
  console.log('original throws/rejected cancellation, owned-only restoration, expiry and unrelated request: PASS')
  const clone = () => structuredClone(snap)
  let altered = clone(); altered.records[0].request_id = ''; await bind(altered, () => {}, 'ambiguous')
  altered = clone(); altered.records.push(structuredClone(altered.records[0])); await bind(altered, () => {}, 'ambiguous')
  await bind(clone(), ({ response }) => { response.headers = () => ({}) }, 'missing-id')
  await bind(clone(), ({ adapter, request, response }) => { adapter.response(response) }, 'ambiguous')
  await bind(clone(), ({ adapter, request, response }) => { const another = { ...request }; adapter.request(another); adapter.response({ ...response, request: () => another }) }, 'ambiguous')
  for (const [key, wrong] of [['method', 'PATCH'], ['path', pathname + '/wrong'], ['query', '?wrong=1'], ['status', 201]]) {
    altered = clone(); altered.records[0][key] = wrong; await bind(altered, () => {}, 'mismatch')
  }
  console.log('unique original PW/native binding rejects missing/duplicate ID and method/path/query/status mismatch: PASS')
  const clock = scheduler(), module = moduleWith(clock), held = deferred(), page = fakePage(clock, () => held.promise), adapter = module.nativeConsumption(page)
  await clock.advance(1000); assert.equal(page.evaluations, 1)
  const stop = adapter.stop(); await clock.advance(250); await stop
  assert(!adapter.snapshot().sample_joined); held.resolve(clone()); await flush(); assert.equal(page.evaluations, 1); assert.equal(clock.timers.size, 0)
  const navClock = scheduler(), navPage = fakePage(navClock, clone()), nav = moduleWith(navClock).nativeConsumption(navPage)
  const req = { url: () => 'http://offline.invalid' + pathname, method: () => 'GET' }
  nav.request(req); nav.response({ request: () => req, headers: () => ({ 'x-request-id': requestID }), status: () => 200 })
  navPage.emit('framenavigated', navPage.mainFrame()); await flush(); await nav.stop(); assert.equal(nav.snapshot(req).binding, 'document-unavailable')
  console.log('hung evaluate single-flight, bounded unavailable join, late settlement retirement and navigation isolation: PASS')
  const throwClock = scheduler(), throwPage = fakePage(throwClock, null)
  throwPage.evaluate = () => { throw new Error('NATIVE_PRIVATE_CANARY') }
  const unavailable = moduleWith(throwClock).nativeConsumption(throwPage)
  await unavailable.stop()
  assert.equal(unavailable.snapshot().sample_failed, 2)
  assert.equal(unavailable.snapshot().hooks_retired, false)
  assert.equal(throwClock.timers.size, 0)
  console.log('synchronous evaluate throw is unavailable, never an observer or retirement success: PASS')
  const continueClock = scheduler(), continuePage = fakePage(continueClock, clone()), continued = moduleWith(continueClock).nativeConsumption(continuePage)
  const finished = deferred(); let finishSettled = false; void finished.promise.then(() => { finishSettled = true })
  await flush(); await continueClock.advance(250); await continueClock.advance(250)
  assert(continuePage.evaluations >= 3); assert.equal(continuePage.maximum, 1); assert.equal(finishSettled, false)
  finished.resolve(null); await finished.promise; await continued.stop(); assert.equal(continueClock.timers.size, 0)
  await new Promise(setImmediate); assert.equal(unhandled.length, 0)
  console.log('samples continue while an independent finished promise is pending; no unhandled observer rejections: PASS')
  await lateEventsControl(snap)
  await installerFailureControl()
  await cachedRetirementControl()
}
main().catch((error) => { console.error(error); process.exitCode = 1 })
