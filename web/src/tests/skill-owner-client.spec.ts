import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createSkillOwnerAPI, parseSkillMetadata, type SkillQuery } from '../api/skill-owner'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(2),
  base = `/api/v1/projects/${project}/skills`
const metadata = (n = 2) => ({
  id: id(n),
  project_id: project,
  name: 'Add Skills',
  normalized_name: 'add-skills',
  description: '说明\nwith\ttabs',
  protected: true,
  current_revision: '9007199254740993',
  version: '9223372036854775807',
})
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
const signal = () => new AbortController().signal
const raw = (value: string) =>
  new Response(value, { headers: { 'Content-Type': 'application/json' } })

describe('Skills current Owner catalog wire', () => {
  it('uses catalog and detail bodyless GETs and preserves exact immutable metadata', async () => {
    const fetch = vi.fn<Fetch>(async (path) =>
      json(path === `${base}/catalog?limit=25` ? { items: [metadata()] } : metadata()),
    )
    const api = createSkillOwnerAPI(fetch),
      list = await api.list(project, {}, signal()),
      detail = await api.get(project, target, signal())
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/catalog?limit=25`,
      `${base}/${target}`,
    ])
    expect(detail).toEqual(metadata())
    expect([list, list.items, list.items[0], detail].every(Object.isFrozen)).toBe(true)
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.body).toBeUndefined()
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
      expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
    }
  })
  it('retains the same limit and opaque cursor, accepts terminal empty pages and rejects malformed pages whole', async () => {
    const cursor = 'opaque+/=签名',
      fetch = vi.fn<Fetch>()
    fetch
      .mockResolvedValueOnce(json({ items: [metadata()], next_cursor: cursor }))
      .mockResolvedValueOnce(json({ items: [{ ...metadata(3), protected: false }] }))
      .mockResolvedValueOnce(json({ items: [] }))
    const api = createSkillOwnerAPI(fetch),
      first = await api.list(project, { limit: 1 }, signal())
    expect(first.next_cursor).toBe(cursor)
    expect(
      (await api.list(project, { limit: 1, cursor: first.next_cursor }, signal())).items[0]!.id,
    ).toBe(id(3))
    expect(fetch.mock.calls[1]![0]).toBe(
      `${base}/catalog?${new URLSearchParams({ limit: '1', cursor })}`,
    )
    expect((await api.list(project, {}, signal())).items).toEqual([])
    for (const value of [
      { items: [metadata(), metadata()] },
      { items: [metadata(3), metadata()] },
      { items: [metadata()], next_cursor: null },
      { items: [metadata()], next_cursor: '' },
      { items: [], next_cursor: 'next' },
      { items: [metadata()], next_cursor: 'next' },
      { items: [metadata()], extra: true },
    ])
      await expect(
        createSkillOwnerAPI(async () => json(value)).list(project, {}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createSkillOwnerAPI(async () => json({ items: [metadata()], next_cursor: cursor })).list(
        project,
        { limit: 1, cursor },
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects unsafe or cross-scope metadata without rewriting server names or versions', () => {
    for (const edit of [
      { id: id(3) },
      { project_id: id(3) },
      { extra: true },
      { name: ' leading' },
      { name: 'a/b' },
      { name: '\ud800' },
      { normalized_name: 'bad\u0000' },
      { description: 'bad\rnewline' },
      { description: 'x'.repeat(8193) },
      { protected: 'true' },
      { current_revision: 1 },
      { version: '01' },
      { version: '9223372036854775808' },
    ])
      expect(() => parseSkillMetadata({ ...metadata(), ...edit }, project, target)).toThrow()
    const value = {
      ...metadata(),
      name: 'Straße',
      normalized_name: 'strasse',
      description: '\ufefftext',
    }
    expect(parseSkillMetadata(value, project, target)).toEqual(value)
  })
  it('rejects invalid query presence, IDs and extra transport options before fetch', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createSkillOwnerAPI(fetch),
      request = accountTransport(fetch)
    for (const query of [
      { limit: undefined },
      { limit: null },
      { limit: 0 },
      { limit: 101 },
      { limit: 1.5 },
      { cursor: undefined },
      { cursor: null },
      { cursor: '' },
      { cursor: '\u0000' },
      { cursor: '🙂'.repeat(2049) },
      { cursor: '\ud800' },
      { order: 'desc' },
    ])
      await expect(api.list(project, query as SkillQuery, signal())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    await expect(api.list('../other', {}, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(api.get(project, 'not-an-id', signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(
      request('listProjectSkills', () => undefined, {
        signal: signal(),
        projectID: project,
        skillQuery: { limit: 25 },
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('uses the catalog representation budget and the smaller complete detail budget', async () => {
    const body = JSON.stringify({ items: [metadata()] })
    expect(
      (
        await createSkillOwnerAPI(async () => raw(' '.repeat(5 * 1024 * 1024) + body)).list(
          project,
          {},
          signal(),
        )
      ).items,
    ).toHaveLength(1)
    await expect(
      createSkillOwnerAPI(async () => raw(' '.repeat(100 * 65536 + 8192 + 1024) + body)).list(
        project,
        {},
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createSkillOwnerAPI(async () => raw(' '.repeat(65536) + JSON.stringify(metadata()))).get(
        project,
        target,
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects duplicate members and invalid UTF8, including duplicates inside metadata', async () => {
    for (const body of [
      '{"items":[],"items":[' + JSON.stringify(metadata()) + ']}',
      '{"items":[' +
        JSON.stringify(metadata()).replace(
          '"protected":true',
          '"protected":false,"protected":true',
        ) +
        ']}',
    ])
      await expect(
        createSkillOwnerAPI(async () => raw(body)).list(project, {}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createSkillOwnerAPI(
        async () =>
          new Response(new Uint8Array([0xff]), { headers: { 'Content-Type': 'application/json' } }),
      ).list(project, {}, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('returns a current cursor Problem without silently restarting the catalog', async () => {
    const requestID = id(99),
      value = {
        type: 'urn:agenteam:problem:cursor',
        title: 'Rejected',
        detail: '',
        status: 409,
        code: 'CURSOR_INVALID',
        instance: '/api/v1',
        request_id: requestID,
        commit_state: 'not_started',
      }
    const fetch = vi.fn<Fetch>(
      async () =>
        new Response(JSON.stringify(value), {
          status: 409,
          headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': requestID },
        }),
    )
    await expect(
      createSkillOwnerAPI(fetch).list(project, { limit: 25, cursor: 'original' }, signal()),
    ).rejects.toMatchObject({ kind: 'problem', problem: value })
    expect(fetch).toHaveBeenCalledOnce()
    expect(fetch.mock.calls[0]![0]).toBe(`${base}/catalog?limit=25&cursor=original`)
  })
})
