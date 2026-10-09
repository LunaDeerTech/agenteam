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
const target = { project: id(1), variable: id(2), version: '1', value: 'PRIVATE_DIAGNOSTIC_CANARY', route: '/owner/project/settings/variables' }
const endpoint = `/api/v1/projects/${target.project}/variables/${target.variable}`
const xid = id(90), identity = { userID: id(3), sessionID: id(4), epoch: 1 }
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
let checks = 0
let consumerSample

(async()=>{
 const page=new EventEmitter(), frame={};page.mainFrame=()=>frame;
 let clock=1000;const originalNow=Date.now;Date.now=()=>clock;
 page.evaluate=async(_fn,end)=>{ if(end)clock=1251;return {document:'same',retired:!!end,records:[]}; };
 const observation=nativeSource.nativeConsumption(page);
 try {await flush();const result=await observation.endDocument();console.log(JSON.stringify({elapsed:clock-1000,deadline:250,retired:result}));assert.equal(result,false,'expired end snapshot must not report bounded retirement');}
 finally{Date.now=originalNow;await observation.stop();}
})().catch(e=>{console.error(e);process.exitCode=1})
