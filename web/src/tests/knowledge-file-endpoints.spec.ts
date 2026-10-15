import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const options = () => ({
  projectID: id(1),
  signal: new AbortController().signal,
  csrf: 'S'.repeat(43),
  key: 'original-file-key',
})
const parse = (value: unknown) => value
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })

describe('Knowledge file closed transport', () => {
  it('uses the three original command paths and a separate revision source read', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ ok: true }))
    const api = accountTransport(fetch),
      write = options(),
      body = { bytes_base64: 'eA==' }
    await api('knowledgeFileCreate', parse, { ...write, target: id(2), body })
    await api('knowledgeFileReplace', parse, { ...write, target: id(2), body })
    await api('knowledgeFileLookup', parse, {
      ...write,
      body: { command: 'create', document_id: id(2), request: body },
    })
    await api('knowledgeSource', parse, {
      projectID: id(1),
      target: id(2),
      revision: '9007199254740993',
      signal: write.signal,
    })
    const base = `/api/v1/projects/${id(1)}/knowledge/documents`
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/${id(2)}/create-file`,
      `${base}/${id(2)}/replace-file`,
      `${base}/file-commands/lookup`,
      `${base}/${id(2)}/revisions/9007199254740993/source`,
    ])
    for (const [, init] of fetch.mock.calls)
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
    for (const [, init] of fetch.mock.calls.slice(0, 3)) {
      expect(init.method).toBe('POST')
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(write.key)
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(write.csrf)
    }
    const read = fetch.mock.calls[3]![1]
    expect(read.method).toBe('GET')
    expect(read.body).toBeUndefined()
    expect(new Headers(read.headers).has('Idempotency-Key')).toBe(false)
  })

  it('bounds encoded command and original lookup bytes before dispatch', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ ok: true })),
      api = accountTransport(fetch),
      write = options()
    for (const [endpoint, maximum] of [
      ['knowledgeFileCreate', 1414488],
      ['knowledgeFileLookup', 1414572],
    ] as const) {
      const body = { value: 'x'.repeat(maximum - JSON.stringify({ value: '' }).length) }
      const target = endpoint === 'knowledgeFileCreate' ? { target: id(2) } : {}
      // Each branch has its own closed target shape; the cast is only for this table's union.
      if (endpoint === 'knowledgeFileCreate')
        await api(endpoint, parse, { ...write, body, target: id(2) })
      else await api(endpoint, parse, { ...write, body })
      const request = api as (
        endpoint: string,
        parse: (value: unknown) => unknown,
        input: unknown,
      ) => Promise<unknown>
      await expect(
        request(endpoint, parse, { ...write, ...target, body: { value: body.value + 'x' } }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('rejects source option injection and noncanonical revision without a request', async () => {
    const fetch = vi.fn<Fetch>(),
      api = accountTransport(fetch),
      write = options()
    const request = api as (
      endpoint: string,
      parse: (value: unknown) => unknown,
      input: unknown,
    ) => Promise<unknown>
    for (const extra of [
      { revision: '01' },
      { revision: '9223372036854775808' },
      { revision: '1/2' },
      { body: {} },
      { key: 'write-key' },
      { csrf: write.csrf },
    ]) {
      await expect(
        request('knowledgeSource', parse, {
          projectID: id(1),
          target: id(2),
          revision: '1',
          signal: write.signal,
          ...extra,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetch).not.toHaveBeenCalled()
  })

  it('uses bounded JSON source and command response budgets', async () => {
    for (const [endpoint, maximum] of [
      ['knowledgeSource', 2097152],
      ['knowledgeFileCreate', 16384],
    ] as const) {
      const fetch = vi.fn<Fetch>(async () => json({ value: 'x'.repeat(maximum) })),
        api = accountTransport(fetch),
        write = options()
      const result =
        endpoint === 'knowledgeSource'
          ? api(endpoint, parse, {
              projectID: id(1),
              target: id(2),
              revision: '1',
              signal: write.signal,
            })
          : api(endpoint, parse, { ...write, target: id(2), body: {} })
      await expect(result).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(fetch).toHaveBeenCalledOnce()
    }
  })
})
