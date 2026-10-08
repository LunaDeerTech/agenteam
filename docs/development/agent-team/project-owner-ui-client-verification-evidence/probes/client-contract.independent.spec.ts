import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from './src/api/client'
import { captureProjectAddress, captureProjectUpdate, createProjectOwnerAPI } from './src/api/project-owner'
import {
  invalidDescriptions, invalidInstants, invalidVersions, maxVersion,
  operationID, otherID, ownerID, project, projectID, validDescriptions,
} from './contract-vectors'

const signal = () => new AbortController().signal
const write = { csrfToken: 'Q'.repeat(43), key: 'independent-owner-intent' }
const success = (body: unknown) => new Response(JSON.stringify(body), {
  headers: { 'Content-Type': 'application/json' },
})
const apiFor = (body: unknown) => createProjectOwnerAPI(async () => success(body))
const get = (body: unknown) => apiFor(body).get(projectID, ownerID, signal())
const update = { expected_version: '1', description: 'historical' }
const receipt = (value: unknown) => ({ state: 'committed', result: { command: 'update', project: value } })

describe('independent Owner parser and original intent boundaries', () => {
  it.each(invalidVersions)('rejects noncanonical version %j in reads and captured writes', async (version) => {
    await expect(get({ ...project(), version })).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(() => captureProjectUpdate({ expected_version: version, description: '' })).toThrow()
  })
  it.each(invalidInstants)('rejects invalid canonical/calendar instant %s', async (created_at) => {
    await expect(get({ ...project(), created_at })).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each(invalidDescriptions.map((value, index) => [index, value] as const))('rejects invalid Unicode/bytes/control description %s', async (_index, description) => {
    await expect(get({ ...project(), description })).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(() => captureProjectUpdate({ expected_version: '1', description })).toThrow()
  })
  it.each(validDescriptions.map((value, index) => [index, value] as const))('preserves valid description %s exactly', async (_index, description) => {
    expect((await get({ ...project(), description })).description).toBe(description)
    expect(captureProjectUpdate({ expected_version: '1', description }).description).toBe(description)
  })
  it.each(Object.keys(project()))('requires exact Get field %s even when nullable', async (key) => {
    const body: Record<string, unknown> = project()
    delete body[key]
    await expect(get(body)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('accepts year 0000 leap day and rejects invalid lifecycle/history ordering', async () => {
    const start = '0000-02-29T00:00:00.000000Z'
    expect((await get({ ...project(), created_at: start })).created_at).toBe(start)
    for (const change of [
      { lifecycle: 'active', archived_at: project().created_at },
      { lifecycle: 'archived', archived_at: null },
      { lifecycle: 'archived', archived_at: '2026-10-08T00:00:00.000000Z' },
      { lifecycle: 'archived', archived_at: '2026-10-08T00:00:00.000003Z' },
      { updated_at: '2026-10-08T00:00:00.000000Z' },
      { current_sprint_id: 'bad' }, { extra: true },
    ]) await expect(get({ ...project(), ...change })).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('binds both target and Owner and retains independent Resolve lifecycle and name rules', async () => {
    for (const change of [{ id: otherID }, { owner_user_id: otherID }]) {
      await expect(get({ ...project(), ...change })).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const address = { username: 'admin', project_name: 'my.project-1' }
    for (const archived_at of [null, project().created_at]) {
      const body = { ...project(), lifecycle: 'deleting', archived_at }
      expect((await apiFor(body).resolve(address, ownerID, signal())).lifecycle).toBe('deleting')
      await expect(get(body)).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(apiFor({ ...project(), owner_user_id: otherID }).resolve(address, ownerID, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(apiFor(project()).resolve({ ...address, project_name: 'other' }, ownerID, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(captureProjectAddress({ username: 'ALI--CE', project_name: 'DeMo' })).toEqual({ username: 'ali--ce', project_name: 'demo' })
  })
  it('keeps each list item projection exact and preserves service order', async () => {
    const rows = [
      { id: otherID, name: 'z', lifecycle: 'deleting', version: '1', operation_id: operationID },
      { id: projectID, name: 'a', lifecycle: 'archiving', version: maxVersion, description: '', operation_id: operationID },
    ]
    expect((await apiFor({ items: rows, next_cursor: 'opaque' }).list({ limit: 2 }, signal())).items).toEqual(rows)
    for (const rows2 of [
      [{ ...rows[0], description: '' }],
      [{ ...rows[1], lifecycle: 'active' }],
      [rows[0], rows[0]],
      [rows[0], { ...rows[1], version: '0' }],
    ]) await expect(apiFor({ items: rows2, next_cursor: null }).list({ limit: 2 }, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(apiFor({ items: rows, next_cursor: null }).list({ limit: 2, lifecycle: ['active'] }, signal())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('reads the same schema-validated >600000-byte 100-row body with a maximum cursor', async () => {
    const raw = readFileSync(new URL('./large-page.json', import.meta.url))
    expect(raw.byteLength).toBeGreaterThan(600000)
    expect(raw.byteLength).toBeLessThan(5 * 1024 * 1024)
    const api = createProjectOwnerAPI(async () => new Response(raw, {
      headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
    }))
    const value = await api.list({ limit: 100 }, signal())
    expect(value.items).toHaveLength(100)
    expect(value.next_cursor).toHaveLength(8192)
    expect(value.items.every((row) => 'description' in row && row.description === '<'.repeat(8192))).toBe(true)
  })
  it('confirms only original comparable receipt fields, target, Owner and permitted version', async () => {
    const historical = { ...project(), description: 'historical', name: 'Old', normalized_name: 'old', version: '2' }
    expect((await apiFor(receipt(historical)).lookup(projectID, ownerID, update, write)).state).toBe('committed')
    for (const change of [{ id: otherID }, { owner_user_id: otherID }, { version: '3' }, { description: 'other intent' }, { lifecycle: 'archiving' }]) {
      await expect(apiFor(receipt({ ...historical, ...change })).lookup(projectID, ownerID, update, write)).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const sameVersion = { ...project(), version: maxVersion }
    const noOp = { expected_version: maxVersion, name: sameVersion.name }
    expect((await apiFor(sameVersion).update(projectID, ownerID, noOp, write)).version).toBe(maxVersion)
    expect((await apiFor(receipt(sameVersion)).lookup(projectID, ownerID, noOp, write)).state).toBe('committed')
    for (const bad of [{ state: 'not_observed', result: null }, { state: 'in_progress', result: {} }, { ...receipt(historical), extra: 1 }]) {
      await expect(apiFor(bad).lookup(projectID, ownerID, update, write)).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('captures submitted bytes before caller mutation and never fills an omitted field', async () => {
    let release!: (value: Response) => void
    const fetcher = vi.fn<Fetch>(() => new Promise((resolve) => { release = resolve }))
    const api = createProjectOwnerAPI(fetcher)
    const input = { expected_version: '1', description: '' }
    const result = api.update(projectID, ownerID, input, write)
    input.description = 'changed after dispatch'
    input.expected_version = '99'
    expect(fetcher.mock.calls[0]![1].body).toBe('{"expected_version":"1","description":""}')
    release(success({ ...project(), description: '' }))
    expect((await result).description).toBe('')
  })
})

describe('independent transport bounds and actual cancellation tail', () => {
  it.each([false, true])('awaits actual failed-response cancellation (cancel reject=%s)', async (rejectCancel) => {
    let finish!: () => void, entered!: () => void
    const cancelled = new Promise<void>((resolve) => { entered = resolve })
    const tail = new Promise<void>((resolve, reject) => {
      finish = () => rejectCancel ? reject(new Error('controlled cancellation failure')) : resolve()
    })
    const stream = new ReadableStream<Uint8Array>({ cancel() { entered(); return tail } })
    const api = createProjectOwnerAPI(async () => new Response(stream, { headers: { 'Content-Type': 'text/plain' } }))
    let settled = false
    const result = api.get(projectID, ownerID, signal()).catch((error: unknown) => { settled = true; return error })
    await cancelled
    expect(settled).toBe(false)
    finish()
    expect(await result).toMatchObject({ kind: 'invalid-response' })
    expect(settled).toBe(true)
  })
  it('joins cancellation after abort without publishing an unfinished response', async () => {
    let finish!: () => void, entered!: () => void
    const tail = new Promise<void>((resolve) => { finish = resolve })
    const cancelled = new Promise<void>((resolve) => { entered = resolve })
    const stream = new ReadableStream<Uint8Array>({
      start(controller) { controller.enqueue(new TextEncoder().encode('{"id":')) },
      cancel() { entered(); return tail },
    })
    const ctrl = new AbortController()
    const api = createProjectOwnerAPI(async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }))
    let settled = false
    const result = api.get(projectID, ownerID, ctrl.signal).catch((error: unknown) => { settled = true; return error })
    await Promise.resolve()
    ctrl.abort()
    await cancelled
    expect(settled).toBe(false)
    finish()
    expect(await result).toMatchObject({ kind: 'cancelled' })
  })
  it('keeps previous success and Problem byte caps while adding Project-specific caps', async () => {
    const capCases = [
      ['bootstrap', { signal: signal() }, 600000],
      ['getSystemRuntimeInformation', { signal: signal() }, 16384],
      ['getOwnerProject', { signal: signal(), target: projectID }, 65536],
      ['listOwnerProjects', { signal: signal(), projects: { limit: 1 } }, 5242880],
    ] as const
    for (const [endpoint, options, cap] of capCases) {
      for (const extra of [0, 1]) {
        const request = accountTransport(async () => new Response('{}' + ' '.repeat(cap - 2 + extra), { headers: { 'Content-Type': 'application/json', 'Content-Length': '2' } }))
        const result = request(endpoint as never, (value) => value, options as never)
        if (extra) await expect(result).rejects.toMatchObject({ kind: 'invalid-response' })
        else await expect(result).resolves.toEqual({})
      }
    }
    const problem = {
      type: 'urn:agenteam:problem:dependency-unavailable', title: '', detail: '',
      instance: '/api/v1/projects', code: 'DEPENDENCY_UNAVAILABLE', status: 503,
      request_id: otherID, commit_state: 'unknown',
    }
    for (const extra of [0, 1]) {
      const base = JSON.stringify(problem)
      const api = createProjectOwnerAPI(async () => new Response(base + ' '.repeat(600000 - base.length + extra), {
        status: 503, headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': otherID },
      }))
      await expect(api.list({ limit: 100 }, signal())).rejects.toMatchObject({ kind: extra ? 'invalid-response' : 'problem' })
    }
  })
  it('rejects cross-domain options and excessive update body before any fetch', async () => {
    const fetcher = vi.fn<Fetch>()
    const request = accountTransport(fetcher)
    for (const options of [
      { signal: signal(), target: projectID, body: {} },
      { signal: signal(), target: projectID, key: write.key },
      { signal: signal(), target: projectID, csrf: write.csrfToken },
      { signal: signal(), target: projectID, projects: { limit: 1 } },
    ]) await expect(request('getOwnerProject', (value) => value, options as never)).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(request('bootstrap', (value) => value, { signal: signal(), projects: { limit: 1 } } as never)).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(request('updateOwnerProject', (value) => value, {
      signal: signal(), target: projectID, csrf: write.csrfToken, key: write.key, body: { padding: 'x'.repeat(65536) },
    })).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})
