import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { setImmediate as turn } from 'node:timers/promises'
import { accountTransport } from './api/client'
import {
  auditFilterActions, auditFilterResourceKinds, captureAuditQuery,
  createProjectAuditAPI, parseProjectAuditRecord,
} from './api/project-audit'
import { projectAuditActions, projectAuditMetadataFields } from './api/project-audit-metadata'
import { auditMetadataJSONBytes } from './api/system-audit-metadata'

// Independently transcribed from accepted HTTP card §§3–4 / typed Go constructors.
// These are controlled wire examples, not real producer observations.
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1), user = id(2), audit = id(3), max = '9223372036854775807'
const human = { kind: 'human', id: user }
const service = (name: string, cause = id(9)) => ({ kind: 'service', service: name, cause_ref: cause, project_id: project })
const resource = (kind: string, target = id(4)) => ({ kind, id: target })
const objectBase = { object_id: id(4), initiator_kind: 'human', initiator_id: user, media_type: '&'.repeat(256), byte_size: max, sent_bytes: '1' }
const artifactBase = { artifact_id: id(4), object_id: id(6), media_type: 'application/json; charset=utf-8', byte_size: max, sent_bytes: '1' }
const projectBase = { project_id: project, initiator_id: user, project_version: max }
const operationBase = { ...projectBase, operation_id: id(6), operation_version: max }
const creation = { ...projectBase, project_version: '1', creation_id: id(5), creation_version: max }
type Example = { action: string; metadata: Record<string, unknown>; resource: Record<string, unknown>; actor?: Record<string, unknown>; outcome?: string }
const examples: Example[] = [
  { action: 'secret.create', metadata: { version: max, changed_fields: ['purpose', 'value'] }, resource: resource('secret') },
  { action: 'secret.update', metadata: { version: max, changed_fields: ['value'] }, resource: resource('secret') },
  { action: 'secret.delete', metadata: { version: max }, resource: resource('secret') },
  { action: 'secret.resolve', metadata: { lease_id: id(10), consumer: 'mcp', reason: 'integrity_mismatch' }, resource: resource('secret'), outcome: 'unknown' },
  { action: 'outbound.access.deny', metadata: { version: max, consumer: 'runner', reason: 'private_not_allowed' }, resource: { kind: 'outbound_policy' }, outcome: 'denied' },
  { action: 'object.upload.complete', metadata: { ...objectBase, phase: 'published' }, resource: resource('stored_object'), actor: service('object') },
  { action: 'object.upload.failed', metadata: { ...objectBase, phase: 'failed', reason: 'storage_unavailable' }, resource: resource('stored_object'), actor: service('object-maintenance'), outcome: 'unknown' },
  { action: 'object.delete', metadata: { ...objectBase, phase: 'deleted' }, resource: resource('stored_object'), actor: service('object') },
  { action: 'object.transfer.issue', metadata: { ...objectBase, transfer_id: id(11), phase: 'issued' }, resource: resource('object_transfer', id(11)), actor: service('object') },
  { action: 'object.transfer.complete', metadata: { ...objectBase, transfer_id: id(11), phase: 'sent', sent_bytes: max }, resource: resource('object_transfer', id(11)), actor: service('object') },
  { action: 'object.transfer.revoke', metadata: { ...objectBase, transfer_id: id(11), phase: 'revoked', reason: 'cancelled' }, resource: resource('object_transfer', id(11)), actor: service('object-maintenance') },
  { action: 'artifact.create', metadata: { ...artifactBase, phase: 'published', source_kind: 'inline', sent_bytes: '0' }, resource: resource('artifact') },
  { action: 'artifact.read', metadata: { ...artifactBase, phase: 'read' }, resource: resource('artifact') },
  { action: 'artifact.download', metadata: { ...artifactBase, phase: 'sent', source_kind: 'knowledge_file', source_id: id(14), source_revision: max }, resource: resource('artifact') },
  { action: 'artifact.list', metadata: { phase: 'listed', count: '0' }, resource: resource('artifact_collection', project) },
  { action: 'outbox.delivery.requeue', metadata: { delivery_id: id(12), event_id: id(13), handler_id: 'z' + 'a'.repeat(127), from_state: 'dead_letter', redrive_cycle: max, reason_code: 'schema_available' }, resource: resource('outbox_delivery', id(12)) },
  { action: 'project.create.accepted', metadata: creation, resource: resource('project_creation', id(5)) },
  { action: 'project.create.completed', metadata: creation, resource: resource('project_creation', id(5)), actor: service('project-initialization', id(5)) },
  { action: 'project.update', metadata: { ...projectBase, changed_fields: ['description', 'name'] }, resource: resource('project', project) },
  { action: 'project.archive.accepted', metadata: { ...operationBase, from: 'active', to: 'archiving', action: 'archive' }, resource: resource('project_operation', id(6)) },
  { action: 'project.archive.completed', metadata: { ...operationBase, from: 'archiving', to: 'archived', action: 'archive' }, resource: resource('project_operation', id(6)), actor: service('project-lifecycle', id(6)) },
  { action: 'project.restore', metadata: { ...projectBase, from: 'archived', to: 'active', action: 'restore' }, resource: resource('project', project) },
  { action: 'project.delete.accepted', metadata: { ...operationBase, from: 'archived', to: 'deleting', action: 'delete' }, resource: resource('project_operation', id(6)) },
  { action: 'project.lifecycle.retry', metadata: { ...operationBase, action: 'delete' }, resource: resource('project_operation', id(6)) },
  { action: 'provider.create', metadata: { provider_id: id(7), version: max, changed_fields: ['created'] }, resource: resource('model_provider', id(7)) },
  { action: 'provider.update', metadata: { provider_id: id(7), version: max, changed_fields: ['base_url', 'enabled', 'name'] }, resource: resource('model_provider', id(7)) },
  { action: 'provider.delete', metadata: { provider_id: id(7), version: max, changed_fields: ['deleted'] }, resource: resource('model_provider', id(7)) },
  { action: 'model.create', metadata: { provider_id: id(7), model_id: id(8), version: max, changed_fields: ['created'] }, resource: resource('model_config', id(8)) },
  { action: 'model.update', metadata: { provider_id: id(7), model_id: id(8), version: max, changed_fields: ['capabilities', 'header_overwrite', 'parameters'] }, resource: resource('model_config', id(8)) },
  { action: 'model.delete', metadata: { provider_id: id(7), model_id: id(8), version: max, changed_fields: ['deleted', 'replacement'], replacement_id: id(15), affected_count: '0' }, resource: resource('model_config', id(8)) },
  { action: 'knowledge.delete_subtree', metadata: { project_id: project, root_id: id(4), initiator_id: user, scope_digest: 'sha256:' + 'f'.repeat(64), deleted_count: max }, resource: resource('knowledge_document') },
]
const summaries: Record<string, string> = { 'secret.create': 'Secret created', 'secret.update': 'Secret updated', 'secret.delete': 'Secret deleted', 'secret.resolve': 'Secret use recorded', 'outbound.access.deny': 'Outbound access denied' }
function row(action = 'secret.delete') {
  const e = examples.find(x => x.action === action)!
  return structuredClone({ audit_id: audit, created_at: '0000-02-29T00:00:00.123456Z', scope: 'project', project_id: project, actor: e.actor ?? human, action, outcome: e.outcome ?? 'success', resource: e.resource, metadata: e.metadata, associations: {} as Record<string, string>, summary: summaries[action] ?? 'Audit event' })
}
const headers = { 'Content-Type': 'application/json', 'X-Request-ID': id(88) }
const signal = () => new AbortController().signal
const deferred = () => { let resolve!: () => void, reject!: (e: Error) => void; const promise = new Promise<void>((a,b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
const originalFetch = globalThis.fetch
beforeAll(() => { globalThis.fetch = async () => { throw new Error('independent probe forbids real fetch') } })
afterAll(() => { globalThis.fetch = originalFetch })

describe('independent formal output witnesses via the public API', () => {
  it('matches 31 separate formal schema branches and keeps the wider query alphabets', () => {
    const schema = JSON.parse(readFileSync('/workspace/agenteam/api/openapi/project-audit.json', 'utf8'))
    const actions = [...new Set(Object.values(schema.components.schemas).flatMap((x: any) => x.properties?.action?.const ? [x.properties.action.const] : []))].sort()
    expect(actions).toEqual(examples.map(x => x.action).sort())
    expect([...projectAuditActions].sort()).toEqual(actions)
    expect(auditFilterActions).toHaveLength(53); expect(auditFilterResourceKinds).toHaveLength(25)
  })
  for (const e of examples) it(e.action, async () => {
    const input = row(e.action); let count = 0
    const api = createProjectAuditAPI(async (url, init) => {
      ++count; expect(url).toBe(`/api/v1/projects/${project}/audit/${audit}`)
      expect(init?.method).toBe('GET'); expect(init?.body).toBeUndefined()
      expect(init?.credentials).toBe('same-origin'); expect(init?.redirect).toBe('error')
      return new Response(JSON.stringify(input), { headers })
    })
    const value = await api.get(project, audit, signal())
    expect(value).toEqual(input); expect(count).toBe(1)
    expect([value, value.actor, value.resource, value.metadata, value.associations].every(Object.isFrozen)).toBe(true)
    expect(projectAuditMetadataFields(value).map(x => x.key).sort()).toEqual(Object.keys(input.metadata).sort())
    // A structurally valid record from another Project must not leak through any action branch.
    await expect(createProjectAuditAPI(async () => new Response(JSON.stringify({ ...input, project_id: id(99) }), { headers })).get(project, audit, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})

describe('conditional contract counterexamples', () => {
  const bad: [string, (r: any) => void][] = [
    ['secret.delete', r => { r.metadata.version = '9223372036854775808' }],
    ['secret.create', r => { r.metadata.changed_fields.reverse() }],
    ['secret.resolve', r => { r.metadata.reason = null }],
    ['outbound.access.deny', r => { r.resource.id = id(4) }],
    ['object.upload.complete', r => { r.metadata.initiator_execution_id = id(9) }],
    ['object.upload.failed', r => { delete r.metadata.reason }],
    ['object.delete', r => { r.actor.service = 'outbound' }],
    ['object.transfer.issue', r => { r.resource.id = id(90) }],
    ['object.transfer.complete', r => { r.metadata.sent_bytes = '1' }],
    ['object.transfer.revoke', r => { r.metadata.initiator_kind = 'agent_run' }],
    ['artifact.create', r => { r.metadata.source_revision = '1' }],
    ['artifact.read', r => { r.metadata.source_id = id(14) }],
    ['artifact.download', r => { r.metadata.reason = 'timeout' }],
    ['artifact.list', r => { r.resource.id = id(90) }],
    ['outbox.delivery.requeue', r => { r.metadata.handler_id += 'x' }],
    ['project.create.completed', r => { r.actor.cause_ref = id(90) }],
    ['project.archive.completed', r => { r.associations.operation_id = id(90) }],
    ['project.restore', r => { r.metadata.operation_id = id(6) }],
    ['project.update', r => { r.actor.id = id(90) }],
    ['project.lifecycle.retry', r => { r.metadata.from = 'archived' }],
    ['provider.update', r => { r.associations.request_id = id(90) }],
    ['model.delete', r => { r.metadata.changed_fields = ['deleted'] }],
    ['knowledge.delete_subtree', r => { r.associations.correlation_id = id(90) }],
  ]
  for (const [action, mutate] of bad) it(`rejects inconsistent ${action}`, async () => {
    const r = row(action); mutate(r)
    await expect(createProjectAuditAPI(async () => new Response(JSON.stringify(r), { headers })).get(project, audit, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('keeps valid AgentRun association and action-limited optional branches', () => {
    const r = row('artifact.read')
    r.actor = { kind: 'agent_run', id: id(16), execution_id: id(17), project_id: project }
    r.associations = { execution_id: id(17), tool_call_id: id(18) }
    expect(parseProjectAuditRecord(r, project).actor).toEqual(r.actor)
    expect(() => parseProjectAuditRecord({ ...r, associations: { execution_id: id(18) } }, project)).toThrow('invalid-response')
    const download = row('artifact.download'); delete download.metadata.source_kind; delete download.metadata.source_id; delete download.metadata.source_revision
    expect(parseProjectAuditRecord(download, project).metadata.sent_bytes).toBe('1')
  })
  it('uses the accepted Go escape byte accounting for scalar text and a legal MIME maximum', () => {
    expect(auditMetadataJSONBytes({ value: '<>&\u2028\u2029' })).toBe(42)
    expect(auditMetadataJSONBytes({ value: '&'.repeat(700) })).toBe(4212)
    expect(auditMetadataJSONBytes({ value: '汉😀' })).toBe(19)
    const input = row('object.transfer.revoke')
    const json = JSON.stringify(input.metadata)
    expect(auditMetadataJSONBytes(input.metadata)).toBe(Buffer.byteLength(json) + 256 * 5)
    expect(parseProjectAuditRecord(input, project).metadata.media_type).toBe('&'.repeat(256))
  })
})

describe('query capture, ordered full pages and target binding', () => {
  it('accepts every legal filter value including System-only empty results without writes', async () => {
    let seen = 0
    const api = createProjectAuditAPI(async (path, init) => {
      ++seen; expect(init?.method).toBe('GET'); expect(init?.body).toBeUndefined()
      expect(path).not.toContain('??')
      return new Response('{"items":[],"next_cursor":null}', { headers })
    })
    for (const action of auditFilterActions) expect((await api.list(project, { action }, signal())).items).toHaveLength(0)
    for (const resource_kind of auditFilterResourceKinds) await api.list(project, { resource_kind }, signal())
    expect(seen).toBe(78)
  })
  it('captures query getters once and prevents later caller mutation from changing filter checks', async () => {
    const gate = deferred(); let reads = 0, capturedURL = ''
    const query = { get action() { ++reads; return 'secret.delete' } }
    const api = createProjectAuditAPI(async url => { capturedURL = String(url); await gate.promise; return new Response(JSON.stringify({ items: [row()], next_cursor: null }), { headers }) })
    const pending = api.list(project, query, signal())
    Object.defineProperty(query, 'action', { value: 'account.login' })
    gate.resolve(); expect((await pending).items).toHaveLength(1)
    expect(reads).toBe(1); expect(capturedURL).toContain('action=secret.delete')
  })
  it('normalizes exact calendar microseconds and rejects forbidden direct input before fetch', async () => {
    expect(captureAuditQuery({ from: '0000-03-01T01:00:00.000001+01:00', to: '9999-12-31T23:59:59.999999Z' }).from).toBe('0000-03-01T00:00:00.000001Z')
    let calls = 0; const api = createProjectAuditAPI(async () => { ++calls; throw new Error('must not fetch') })
    for (const query of [{ action: 'model.provider.create' }, { limit: '01' }, { actor_kind: 'service', actor_id: user }, { from: '0000-01-01T00:00:00+01:00' }, { cursor: 'x'.repeat(8193) }, { private: 'value' }, { agent_id: user.toUpperCase() + 'A' }]) {
      await expect(api.list(project, query as never, signal())).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(calls).toBe(0)
  })
  it('rejects malformed last row, wrong detail target and nonmatching agent OR projection', async () => {
    const rows = Array.from({ length: 200 }, (_, i) => ({ ...row(), audit_id: id(300-i) }))
    const api = (value: unknown) => createProjectAuditAPI(async () => new Response(JSON.stringify(value), { headers }))
    expect((await api({ items: rows, next_cursor: 'c'.repeat(8192) }).list(project, { limit: '200' }, signal())).items).toHaveLength(200)
    await expect(api({ items: [...rows.slice(0,-1), { ...rows[199], summary: 'private' }], next_cursor: null }).list(project, { limit: '200' }, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(api({ ...row(), audit_id: id(90) }).get(project, audit, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(api({ items: [row()], next_cursor: null }).list(project, { agent_id: id(90) }, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})

describe('public API streaming boundary and actual cancellation tail', () => {
  for (const kind of ['list','get'] as const) it(`${kind}: legal complete DTO at 1MiB waits for real EOF`, async () => {
    const dto = kind === 'list' ? { items: [], next_cursor: null } : row()
    const json = JSON.stringify(dto), bytes = Buffer.from(json + ' '.repeat(1048576-Buffer.byteLength(json)))
    let control!: ReadableStreamDefaultController<Uint8Array>
    const stream = new ReadableStream<Uint8Array>({ start(c) { control=c; c.enqueue(bytes.subarray(0,65537)); c.enqueue(bytes.subarray(65537)) } })
    const api = createProjectAuditAPI(async () => new Response(stream, { headers: { ...headers, 'Content-Length': '0' } }))
    let settled = false
    const promise = (kind === 'list' ? api.list(project, {}, signal()) : api.get(project, audit, signal())).finally(() => { settled=true })
    try { await turn(); expect(settled).toBe(false) } finally { control.close() }
    expect(await promise).toEqual(dto); expect(stream.locked).toBe(false)
  })
  for (const rejection of [false,true]) it(`cap+1 retains transport until cancel ${rejection?'rejects':'resolves'}`, async () => {
    const entered = deferred(), tail = deferred()
    const stream = new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new Uint8Array(1048577)) }, cancel() { entered.resolve(); return tail.promise } })
    const api = createProjectAuditAPI(async () => new Response(stream, { headers }))
    let settled=false
    const result = api.list(project, {}, signal()).then(()=>'unexpected success', (e: {kind:string})=>e.kind).finally(()=>{settled=true})
    await entered.promise
    try { await turn(); expect(settled).toBe(false) } finally { if(rejection)tail.reject(new Error('controlled cancel failure'));else tail.resolve() }
    expect(await result).toBe('invalid-response'); expect(stream.locked).toBe(false)
  })
  it('joins explicit abort tail without claiming a Session owner test', async () => {
    const entered=deferred(), tail=deferred(), abort=new AbortController()
    const stream=new ReadableStream<Uint8Array>({ start(c){c.enqueue(new TextEncoder().encode('{'))},cancel(){entered.resolve();return tail.promise} })
    const api=createProjectAuditAPI(async()=>new Response(stream,{headers}))
    let settled=false;const pending=api.list(project,{},abort.signal).then(()=>'unexpected',(e:{kind:string})=>e.kind).finally(()=>{settled=true})
    await turn();abort.abort();await entered.promise
    try{await turn();expect(settled).toBe(false)}finally{tail.resolve()}
    expect(await pending).toBe('cancelled');expect(stream.locked).toBe(false)
  })
  it.each(['truncated','utf8','valid-prefix-extra','bad-last-row'])('rejects complete %s without publishing',async(kind)=>{
    const bytes=kind==='utf8'?new Uint8Array([123,34,120,34,58,34,0xf0,0x9f]):Buffer.from(kind==='truncated'?'{"items":[':kind==='valid-prefix-extra'?'{"items":[],"next_cursor":null}x':JSON.stringify({items:[row(),{...row(),audit_id:id(4),metadata:{version:null}}],next_cursor:null}))
    const api=createProjectAuditAPI(async()=>new Response(bytes,{headers}))
    await expect(api.list(project,{},signal())).rejects.toMatchObject({kind:'invalid-response'})
  })
  it('keeps old Owner64KiB versus new Audit1MiB classification distinct',async()=>{
    const json='{}'+' '.repeat(65535)
    await expect(accountTransport(async()=>new Response(json,{headers}))('getOwnerProject',x=>x,{signal:signal(),target:project})).rejects.toMatchObject({kind:'invalid-response'})
    expect(await accountTransport(async()=>new Response(json,{headers}))('getProjectAudit',x=>x,{signal:signal(),projectID:project,target:audit})).toEqual({})
  })
})
