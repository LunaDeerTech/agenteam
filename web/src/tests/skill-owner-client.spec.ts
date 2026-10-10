import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createSkillOwnerAPI, parseSkillMetadata } from '../api/skill-owner'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(2),
  base = `/api/v1/projects/${project}/skills`
const metadata = () => ({
  id: target,
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

describe('Skills current Owner wire contract', () => {
  it('uses the two bodyless same-origin GETs and preserves exact immutable values', async () => {
    const fetch = vi.fn<Fetch>(async (path) =>
      json(path === base ? { items: [metadata()] } : metadata()),
    )
    const api = createSkillOwnerAPI(fetch)
    const list = await api.list(project, signal()),
      detail = await api.get(project, target, signal())
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([base, `${base}/${target}`])
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
  it.each([
    { id: id(3) },
    { project_id: id(3) },
    { extra: true },
    { name: ' leading' },
    { name: 'trailing\u0085' },
    { name: '..' },
    { name: 'a/b' },
    { name: 'a\\b' },
    { name: '🙂'.repeat(33) },
    { name: '\ud800' },
    { normalized_name: 'bad\u0000' },
    { normalized_name: '🙂'.repeat(97) },
    { description: '\u0085\u2003' },
    { description: 'bad\rnewline' },
    { description: 'x'.repeat(8193) },
    { protected: 'true' },
    { current_revision: 1 },
    { current_revision: '0' },
    { version: '01' },
    { version: '9223372036854775808' },
    { version: '1\n' },
  ])('rejects invalid metadata without a partial result: %j', (edit) => {
    expect(() => parseSkillMetadata({ ...metadata(), ...edit }, project, target)).toThrow()
  })
  it('does not silently trim text or replace the server full-fold spelling', () => {
    const value = {
      ...metadata(),
      name: 'Straße',
      normalized_name: 'strasse',
      description: '\ufefftext',
    }
    expect(parseSkillMetadata(value, project, target)).toEqual(value)
  })
  it.each([
    { items: [] },
    { items: [metadata(), metadata()] },
    { items: [metadata()], next_cursor: 'x' },
  ])('rejects a nonformal directory: %j', async (value) => {
    await expect(
      createSkillOwnerAPI(async () => json(value)).list(project, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects invalid IDs and extra read options before fetch', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createSkillOwnerAPI(fetch),
      request = accountTransport(fetch)
    await expect(api.list('../other', signal())).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(api.get(project, 'not-an-id', signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(
      request('listProjectSkills', () => undefined, {
        signal: signal(),
        projectID: project,
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    () =>
      new Response('{"items":[],"items":[' + JSON.stringify(metadata()) + ']}', {
        headers: { 'Content-Type': 'application/json' },
      }),
    () =>
      new Response(' '.repeat(65536) + JSON.stringify({ items: [metadata()] }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    () => new Response(new Uint8Array([0xff]), { headers: { 'Content-Type': 'application/json' } }),
  ])('rejects duplicate members, oversized or non-UTF8 responses', async (response) => {
    await expect(
      createSkillOwnerAPI(async () => response()).list(project, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
