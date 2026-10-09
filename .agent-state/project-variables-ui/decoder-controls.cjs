#!/usr/bin/env node
// Run the actual browser helper and API decoders with private, synthetic files.
// No server, browser, socket or outbound request is started.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { createHash } = require('node:crypto')
const { spawnSync } = require('node:child_process')
const root = path.resolve(__dirname, '../..')
const ts = require(path.join(root, 'web/node_modules/typescript'))
const { expect } = require(path.join(root, 'tests/account-captcha-web/node_modules/@playwright/test'))
const cache = new Map()
function moduleSource(name) {
  assert(['client', 'account', 'system-account', 'project-variables'].includes(name))
  if (cache.has(name)) return cache.get(name)
  const output = {}
  cache.set(name, output)
  const code = ts.transpileModule(fs.readFileSync(path.join(root, 'web/src/api', name + '.ts'), 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText
  new Function('exports', 'require', code)(output, (file) => {
    assert(file.startsWith('./'))
    return moduleSource(file.slice(2))
  })
  return output
}
const client = moduleSource('client')
const variables = moduleSource('project-variables')
const helperPath = path.join(root, 'tests/account-captcha-web/e2e/project-variables.helpers.ts')
const source = ts.createSourceFile(helperPath, fs.readFileSync(helperPath, 'utf8'), ts.ScriptTarget.ES2022, true)
const wanted = new Set(['command', 'schemaProgram', 'decoderCSRF', 'originalResponse', 'validateOriginalBodies'])
const chunks = source.statements.filter((node) =>
  (ts.isFunctionDeclaration(node) && wanted.has(node.name?.text)) ||
  (ts.isVariableStatement(node) && node.declarationList.declarations.some((d) => wanted.has(d.name.getText(source)))),
)
assert(chunks.some((node) => ts.isFunctionDeclaration(node) && node.name.text === 'validateOriginalBodies'))
const compiled = ts.transpileModule(chunks.map((node) => node.getText(source)).join('\n'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText
const directory = fs.mkdtempSync(path.join(root, 'output/ai/project-variables-ui/implementation/decoder-control-'))
let decoderFetches = 0
const dependencies = {
  expect, directory, repository: root, join: path.join,
  readFileSync: fs.readFileSync, readdirSync: fs.readdirSync, writeFileSync: fs.writeFileSync,
  createHash, spawnSync: (...args) => {
    const result = spawnSync(...args)
    // The only files in this offline fixture contain synthetic DTOs.
    if (result.status !== 0) console.error(result.stderr || result.error?.message)
    return result
  }, AccountFailure: client.AccountFailure, uuid7: client.uuid7,
  captureProjectVariableCommand: variables.captureProjectVariableCommand,
  createProjectVariablesAPI: (fetcher) => variables.createProjectVariablesAPI(async (...args) => {
    decoderFetches++
    return fetcher(...args)
  }),
}
const { validate, schemaProgram } = new Function(...Object.keys(dependencies), compiled + '\nreturn { validate: validateOriginalBodies, schemaProgram };')(...Object.values(dependencies))
const id = (n) => '01900000-0000-7000-8000-' + String(n).padStart(12, '0')
const project = id(1), target = id(2), event = id(3), audit = id(4)
const at = '2026-10-09T09:00:00.000001Z'
const full = { id: target, project_id: project, type: 'variable', name: 'API_URL', description: '', value: 'synthetic', version: '1', created_at: at, updated_at: at }
const create = { variable_id: target, name: full.name, description: '', value: full.value }
const created = { command: 'project.variable.create', changed: true, variable: full, event_id: event, audit_id: audit }
const updated = { ...created, command: 'project.variable.update', variable: { ...full, value: '', version: '2' } }
const deleted = { command: 'project.variable.delete', changed: true, deleted: { id: target, project_id: project, type: 'variable', version: '3', deleted_at: at }, event_id: event, audit_id: audit }
const base = `/api/v1/projects/${project}/variables`
const inputs = [
  ['POST', base, { request: create }, created],
  ['PATCH', base + '/' + target, { expected_version: '1', request: { value: '' } }, updated],
  ['DELETE', base + '/' + target, { expected_version: '2' }, deleted],
  ['POST', base + '/commands/lookup', { command: created.command, request: create }, { status: 'committed', receipt: created }],
  ['POST', base + '/commands/lookup', { command: updated.command, target_id: target, expected_version: '1', request: { value: '' } }, { status: 'committed', receipt: updated }],
  ['POST', base + '/commands/lookup', { command: deleted.command, target_id: target, expected_version: '2' }, { status: 'committed', receipt: deleted }],
]
const headers = { 'idempotency-key': 'original-synthetic-key', 'x-csrf-token': 'R'.repeat(43) }
const records = []
const entries = inputs.map(([method, pathname, body, value], index) => {
  const raw = Buffer.from(JSON.stringify(value)), requestID = id(100 + index)
  const record = { method, path: pathname, query: '', status: 200, content_type: 'application/json', request_id: requestID,
    request_b64: Buffer.from(JSON.stringify(body)).toString('base64'), body_b64: raw.toString('base64'),
    key: headers['idempotency-key'], csrf_sha256: createHash('sha256').update(headers['x-csrf-token']).digest('hex') }
  records.push(record)
  fs.writeFileSync(path.join(directory, `project-variables-response-${index}.json`), JSON.stringify(record), { mode: 0o600 })
  return { method, url: new URL(pathname, 'http://offline.invalid'), body: JSON.stringify(body),
    request: { allHeaders: async () => headers },
    response: { headerValue: async (name) => name === 'x-request-id' ? requestID : 'application/json', status: () => 200, body: async () => raw } }
})
async function main() {
  await validate(entries, false)
  assert.equal(decoderFetches, 6)
  console.log('actual create/update/delete + three Lookup decoders and formal schemas: PASS')
  for (const [field, wrong] of [['method', 'PATCH'], ['path', base + '/wrong'], ['query', 'wrong=1'], ['request_b64', Buffer.from('{}').toString('base64')], ['key', 'wrong-key'], ['csrf_sha256', '0'.repeat(64)]]) {
    const filename = path.join(directory, 'project-variables-response-0.json')
    fs.writeFileSync(filename, JSON.stringify({ ...records[0], [field]: wrong }), { mode: 0o600 })
    const before = decoderFetches
    await assert.rejects(validate([entries[0]], false))
    assert.equal(decoderFetches, before)
    fs.writeFileSync(filename, JSON.stringify(records[0]), { mode: 0o600 })
  }
  console.log('original method/path/query/body/key/CSRF binding rejects before decoder: PASS')
  const requestID = id(200), pathname = base + '/' + target
  const problem = { type: 'urn:agenteam:problem:not-found', title: 'Not found', status: 404, detail: '', instance: pathname, code: 'NOT_FOUND', request_id: requestID, commit_state: 'not_started' }
  const raw = Buffer.from(JSON.stringify(problem))
  fs.writeFileSync(path.join(directory, 'project-variables-response-6.json'), JSON.stringify({
    method: 'GET', path: pathname, query: '', status: 404, content_type: 'application/problem+json', request_id: requestID,
    request_b64: '', body_b64: raw.toString('base64'), key: '', csrf_sha256: createHash('sha256').update('').digest('hex'),
  }), { mode: 0o600 })
  await validate([{ method: 'GET', url: new URL(pathname, 'http://offline.invalid'), body: null,
    request: { allHeaders: async () => ({}) },
    response: { headerValue: async (name) => name === 'x-request-id' ? requestID : 'application/problem+json', status: () => 404, body: async () => raw },
  }], true)
  assert.equal(decoderFetches, 7)
  // The narrowly qualified GET still uses the exact private response and the
  // actual client/schema; only PW's unavailable body() is replaced by the
  // separately proven original native/public consumption chain.
  const detailID = id(201), detailRaw = Buffer.from(JSON.stringify(full)), detailFile = path.join(directory, 'project-variables-response-7.json')
  const detailRecord = { method: 'GET', path: pathname, query: '', status: 200, content_type: 'application/json', request_id: detailID,
    request_b64: '', body_b64: detailRaw.toString('base64'), key: '', csrf_sha256: createHash('sha256').update('').digest('hex') }
  fs.writeFileSync(detailFile, JSON.stringify(detailRecord), { mode: 0o600 })
  let bodyReads = 0
  const detail = { method: 'GET', url: new URL(pathname, 'http://offline.invalid'), body: null, request: { allHeaders: async () => ({}) },
    response: { headerValue: async name => name === 'x-request-id' ? detailID : 'application/json', status: () => 200, body: async () => { bodyReads++; throw Error('unavailable original PW body') } } }
  await validate([detail], false, entry => entry === detail)
  assert.equal(bodyReads, 0)
  await assert.rejects(validate([detail], false))
  assert.equal(bodyReads, 1)
  for (const changed of [JSON.stringify({ ...full, id: id(999) }), JSON.stringify(full).replace('"value":', '"value":"other","value":'), JSON.stringify({ ...full, unknown: true })]) {
    fs.writeFileSync(detailFile, JSON.stringify({ ...detailRecord, body_b64: Buffer.from(changed).toString('base64') }), { mode: 0o600 })
    await assert.rejects(validate([detail], false, entry => entry === detail))
  }
  fs.writeFileSync(detailFile, JSON.stringify(detailRecord), { mode: 0o600 })
  assert.equal(bodyReads, 1)
  console.log('qualified GET keeps actual raw strict decoder/schema; ordinary body gate and malformed-response rejection unchanged: PASS')
  for (const [schema, body] of [['#/components/schemas/VariableMutation', { ...created, undeclared: true }], ['./common.json#/components/schemas/Problem', { ...problem, status: '404' }]]) {
    const file = path.join(directory, 'invalid-schema.json')
    fs.writeFileSync(file, JSON.stringify([{ schema, raw: Buffer.from(JSON.stringify(body)).toString('base64') }]), { mode: 0o600 })
    const result = spawnSync('/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3', ['-c', schemaProgram, root, file], { encoding: 'utf8', timeout: 6000, maxBuffer: 4096 })
    assert.equal(result.status, 1)
    assert.match(result.stderr, /ValidationError/)
  }
  console.log('relative common Problem schema resolves; both formal schemas reject invalid DTOs: PASS')
}
main().catch((error) => {
  console.error(`decoder controls FAIL: ${error.name}; decoder_fetches=${decoderFetches}`)
  process.exitCode = 1
}).finally(() => fs.rmSync(directory, { recursive: true }))
