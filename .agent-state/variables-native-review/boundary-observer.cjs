#!/usr/bin/env node
// Actual locked Playwright transform/serialization; explicit browser and
// Session facade substitutes test diagnostics, not production acceptance.
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const { EventEmitter } = require('node:events')
const { createHash } = require('node:crypto')
const root = '/workspace/agenteam-project-variables-ui'
process.env.PWTEST_CACHE_DIR = '/workspace/agenteam-runner-control/output/ai/runner-control/tmp/variables-native-review/pw-authority-cache'
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
const actualProblem = JSON.parse(fs.readFileSync('/workspace/agenteam-runner-control/output/ai/runner-control/tmp/variables-native-review/authority-boundary.json', 'utf8'))
const xid = actualProblem.request_id, identity = { userID: id(3), sessionID: id(4), epoch: 1 }
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
let checks = 0
let consumerSample
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
  const problem = actualProblem
  const complete = async (changes = {}) => {
    native.records.push({ method: 'PATCH', path: endpoint, query: '', status: 409, request_id: xid })
    auth.projectVariables.progress = { projectID: target.project, targetID: target.variable, kind: 'update', phase: 'rejected', receipt: null }
    pending.reject(new Failure('problem', { ...problem, ...changes })); await flush()
  }
  return { installed, auth, original, calls, pending, native, host, command, problem, Failure, complete, dom }
}

(async()=>{
 const x=await fixture();try{
  assert.equal(x.installed,true);const promise=x.auth.projectVariables.start(x.command);assert.equal(promise,x.pending.promise);
  await x.complete();const sample=x.host.__variableAuthority.finish(xid);
  assert.equal(sample.instance_matches,true,'actual producer instance binding');
  assert.equal(sample.problem_request_matches,true);assert.equal(sample.native_request_matches,true);assert.equal(sample.hooks_retired,true);
  console.log('actual producer -> serialized original observer: PASS');
 }finally{x.dom.window.close();}
})().catch(e=>{console.error(e);process.exitCode=1})
