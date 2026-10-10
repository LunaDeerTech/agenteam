import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createProjectSecretsAPI, type SecretCommand } from '../api/project-secrets'
import { base, id, json, metadata, projectID, receipt, target } from './project-secrets-fixture'
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrf: 'S'.repeat(43), key: 'original-key' })
const create = (value = 'private secret\n中文'): SecretCommand => ({
  kind: 'create',
  projectID,
  request: { variable_id: target, name: 'TOKEN', description: 'Safe description', value },
})
describe('Secret Owner strict wire contract', () => {
  it('uses exactly six operations with identity-only Lookup and no returned material', async () => {
    const responses = [
      { items: [metadata()] },
      metadata(),
      receipt(),
      receipt('update', { ...metadata(), version: '2' }),
      {
        command: 'project.secret_variable.delete',
        changed: true,
        event_id: id(40),
        audit_id: id(41),
        deleted: {
          id: target,
          project_id: projectID,
          type: 'secret',
          version: '3',
          deleted_at: metadata().updated_at,
        },
      },
      { status: 'not_observed', receipt: null },
    ]
    const fetch = vi.fn<Fetch>(async () => json(responses.shift())),
      api = createProjectSecretsAPI(fetch)
    expect((await api.list(projectID, { limit: 50 }, signal())).items).toEqual([metadata()])
    expect(await api.get(projectID, target, signal())).toEqual(metadata())
    const result = await api.execute(create(), options())
    await api.execute(
      {
        kind: 'update',
        projectID,
        targetID: target,
        expectedVersion: '1',
        request: { value: 'another' },
      },
      options(),
    )
    await api.execute(
      { kind: 'delete', projectID, targetID: target, expectedVersion: '2' },
      options(),
    )
    expect(await api.lookup({ kind: 'create', projectID, targetID: target }, options())).toEqual({
      status: 'not_observed',
      receipt: null,
    })
    expect(fetch.mock.calls.map(([url, init]) => [url, init.method])).toEqual([
      [base + '?limit=50', 'GET'],
      [base + '/' + target, 'GET'],
      [base, 'POST'],
      [base + '/' + target, 'PATCH'],
      [base + '/' + target, 'DELETE'],
      [base + '/commands/lookup', 'POST'],
    ])
    expect(JSON.parse(fetch.mock.calls[5]![1].body as string)).toEqual({
      command: 'project.secret_variable.create',
      target_id: target,
    })
    expect(JSON.stringify(result)).not.toContain('private secret')
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(init.method !== 'GET')
    }
  })
  it.each(['', '\0', '\ud800', 'x'.repeat(65537)])(
    'rejects invalid material before Fetch (bounded sample)',
    async (value) => {
      const fetch = vi.fn<Fetch>(),
        api = createProjectSecretsAPI(fetch)
      await expect(api.execute(create(value), options())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it('accepts exact 65536 bytes including significant whitespace without trim', async () => {
    const value = ' '.repeat(65536),
      fetch = vi.fn<Fetch>(async () => json(receipt()))
    await createProjectSecretsAPI(fetch).execute(create(value), options())
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string).request.value).toBe(value)
  })
  it.each([
    { value: 'never-return' },
    { credential_ref: id(70) },
    { name: 'AGENTEAM_KEY' },
    { project_id: id(999) },
    { version: '0' },
    { description: '\ud800' },
  ])('rejects unsafe or invalid metadata %j', async (delta) => {
    await expect(
      createProjectSecretsAPI(async () => json({ ...metadata(), ...delta })).get(
        projectID,
        target,
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects duplicate members before a valid later value hides them', async () => {
    const raw = JSON.stringify(metadata()).replace('"name":"TOKEN"', '"name":"bad","name":"TOKEN"')
    await expect(
      createProjectSecretsAPI(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      ).get(projectID, target, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects material replacement no-op, wrong target/version, and fictitious Lookup states', async () => {
    for (const raw of [
      receipt('update', metadata(), false),
      receipt('update', { ...metadata(), id: id(91), version: '2' }),
      receipt('update', { ...metadata(), version: '3' }),
    ])
      await expect(
        createProjectSecretsAPI(async () => json(raw)).execute(
          {
            kind: 'update',
            projectID,
            targetID: target,
            expectedVersion: '1',
            request: { value: 'same' },
          },
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createProjectSecretsAPI(async () => json({ status: 'in_progress', receipt: null })).lookup(
        { kind: 'create', projectID, targetID: target },
        options(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects caller-supplied query/write headers on safe GETs', async () => {
    const fetch = vi.fn<Fetch>()
    await expect(
      accountTransport(fetch)('getProjectSecret', (v) => v, {
        projectID,
        target,
        signal: signal(),
        csrf: 'S'.repeat(43),
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('fails closed when original reader cancellation fails and releases its lock', async () => {
    const release = vi.fn(),
      cancel = vi.fn(async () => {
        throw new Error('private material must not escape')
      })
    let read = 0
    const response = {
      status: 200,
      redirected: false,
      type: 'basic',
      headers: new Headers({ 'Content-Type': 'application/json' }),
      body: {
        getReader: () => ({
          read: async () =>
            read++
              ? { done: true }
              : { done: false, value: new TextEncoder().encode(JSON.stringify(receipt())) },
          cancel,
          releaseLock: release,
        }),
        cancel: async () => undefined,
      },
    } as unknown as Response
    await expect(
      createProjectSecretsAPI(async () => response).execute(create(), options()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(release).toHaveBeenCalledTimes(1)
  })
})
