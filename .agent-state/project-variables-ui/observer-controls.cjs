#!/usr/bin/env node
// Actual helper event handling, with explicit Playwright event substitutes.
// No browser, socket, HTTP or database. Native consumption has separate controls.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { EventEmitter } = require('node:events')
const root = path.resolve(__dirname, '../..')
const ts = require(path.join(root, 'web/node_modules/typescript'))
const { expect } = require(path.join(root, 'tests/account-captcha-web/node_modules/@playwright/test'))
const source = ts.createSourceFile('helpers.ts', fs.readFileSync(path.join(root, 'tests/account-captcha-web/e2e/project-variables.helpers.ts'), 'utf8'), ts.ScriptTarget.ES2022, true)
const names = new Set(['emptyNetworkDiagnostic', 'networkObservations', 'networkRetire', 'authorityObservations', 'observe', 'recordFailure'])
const code = ts.transpileModule(source.statements.filter((n) => (ts.isFunctionDeclaration(n) && names.has(n.name?.text)) || (ts.isVariableStatement(n) && n.declarationList.declarations.some((d) => names.has(d.name.getText(source))))).map((n) => n.getText(source)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
const client = {}
new Function('exports', ts.transpileModule(fs.readFileSync(path.join(root, 'web/src/api/client.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText)(client)
const exportsObject = {}
let validationCalls = 0, written
const emptyNativeDiagnostic = () => ({ binding: 'unobserved', facts: null })
const dependencies = { exports: exportsObject, expect, uuid7: client.uuid7, emptyNativeDiagnostic,
  nativeConsumption: () => ({ request() {}, response() {}, stop: async () => {}, snapshot: emptyNativeDiagnostic }),
  validateOriginalBodies: async () => { validationCalls++ }, directory: 'private', join: path.join, step: 6,
  writeFileSync: (_path, body) => { written = JSON.parse(body) }, process: { env: { AGENTEAM_PROJECT_VARIABLE_WEB_CASE: 'crud' } },
}
new Function(...Object.keys(dependencies), code)(...Object.values(dependencies))
const project = '01900000-0000-7000-8000-000000000001', target = '01900000-0000-7000-8000-000000000002'
const base = `/api/v1/projects/${project}/variables`
const canary = 'PRIVATE_CANARY_HEADERS_BODY_STACK'
async function run(name, expectedReason, stimulus, method = 'GET') {
  const page = new EventEmitter()
  page.evaluate = async () => ({ observed: true, variables: true, editor: true, close: true, history: false, confirmed: false, uncertain: false, dialog: false })
  const observation = exportsObject.observe(page)
  const request = { method: () => method, url: () => 'http://offline.invalid' + (method === 'POST' ? base : base + '/' + target), postData: () => method === 'POST' ? JSON.stringify({ request: { variable_id: target, value: canary } }) : null }
  const response = { request: () => request, finished: async () => null, status: () => 200 }
  const before = validationCalls
  await stimulus({ page, observation, request, response })
  if (expectedReason === 'none') {
    await observation.finish()
    assert.equal(validationCalls, before + 1)
  } else {
    await assert.rejects(observation.finish())
    assert.equal(validationCalls, before)
  }
  written = undefined
  await exportsObject.recordFailure(page, { status: 'failed', expectedStatus: 'passed', errors: [{ stack: canary + '\n at project-variables.helpers.ts:372:1' }] })
  assert.equal(written.network.reason, expectedReason, name)
  assert.equal(written.network.total, 1)
  assert(!JSON.stringify(written).includes(canary))
  assert(!JSON.stringify(written).includes(project))
  assert(!JSON.stringify(written).includes(target))
  console.log(name + ': PASS')
}
const start = ({ page, request, response }) => { page.emit('request', request); page.emit('response', response) }
const cut = (x) => { x.observation.cut('GET', project, target); start(x) }
async function main() {
  await run('ordinary complete still requires finished tail', 'none', (x) => { start(x); x.page.emit('requestfinished', x.request) })
  await run('declared cut failure remains separate', 'none', (x) => { cut(x); x.page.emit('requestfailed', x.request) })
  await run('declared original cancellation remains separate', 'none', (x) => { start(x); x.observation.cancel(project, target); x.page.emit('requestfailed', x.request) })
  await run('ordinary failure retains failure gate', 'unexpected-failed', (x) => { start(x); x.page.emit('requestfailed', x.request) })
  await run('duplicate response', 'duplicate-response', (x) => { start(x); x.page.emit('response', x.response); x.page.emit('requestfinished', x.request) })
  await run('duplicate failure', 'duplicate-failed', (x) => { cut(x); x.page.emit('requestfailed', x.request); x.page.emit('requestfailed', x.request) })
  await run('failure after finish', 'failed-after-finished', (x) => { start(x); x.page.emit('requestfinished', x.request); x.page.emit('requestfailed', x.request) })
  await run('unexpected finish of declared cut', 'unexpected-finished', (x) => { cut(x); x.page.emit('requestfinished', x.request) })
  await run('duplicate finish', 'duplicate-finished', (x) => { start(x); x.page.emit('requestfinished', x.request); x.page.emit('requestfinished', x.request) })
  await run('finish after declared failure', 'finished-after-failed', (x) => { cut(x); x.page.emit('requestfailed', x.request); x.page.emit('requestfinished', x.request) })
  await run('missing original response', 'missing-response', (x) => { x.page.emit('request', x.request); x.page.emit('requestfinished', x.request) })
  await run('non-null finished tail', 'finished-tail', (x) => { x.response.finished = async () => new Error(canary); start(x); x.page.emit('requestfinished', x.request) })
  await run('rejected finished tail', 'finished-tail', (x) => { x.response.finished = async () => { throw new Error(canary) }; start(x); x.page.emit('requestfinished', x.request) })
  await run('cut target mismatch', 'cut-binding', (x) => { x.observation.cut('POST', project, project); start(x); x.page.emit('requestfailed', x.request) }, 'POST')
  console.log('all diagnostic projections exclude original IDs/material/error text: PASS')
}
main().catch((error) => { console.error(error); process.exitCode = 1 })
