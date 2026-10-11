import { afterEach, describe, expect, it, vi } from 'vitest'
import { webcrypto } from 'node:crypto'
import { accountTransport, type Fetch } from '../api/client'
import { createKnowledgeDeleteAPI } from '../api/knowledge-delete'
import { base, id, json, lookup, preview, receipt, token } from './knowledge-delete.fixture'
const write = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-key',
  signal: new AbortController().signal,
})
afterEach(() => vi.unstubAllGlobals())
describe('Knowledge deletion HTTP contract', () => {
  it('validates the complete connected preview and binds exact commands without a preview key', async () => {
    vi.stubGlobal('crypto', webcrypto)
    const values = [preview(), receipt(), lookup()]
    const fetch = vi.fn<Fetch>(async () => json(values.shift()))
    const api = createKnowledgeDeleteAPI(fetch),
      w = write(),
      input = { confirmation_token: token }
    const result = await api.preview(id(10), id(20), { csrfToken: w.csrfToken, signal: w.signal })
    expect(Object.isFrozen(result.nodes)).toBe(true)
    await api.remove(id(10), id(20), input, w)
    await api.lookup(id(10), id(20), input, w)
    expect(fetch.mock.calls.map(([p]) => p)).toEqual([
      `${base}/${id(20)}/delete-preview`,
      `${base}/${id(20)}/delete-subtree`,
      `${base}/commands/lookup`,
    ])
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      {},
      input,
      { command: 'delete-subtree', document_id: id(20), request: input },
    ])
    expect(new Headers(fetch.mock.calls[0]![1].headers).has('Idempotency-Key')).toBe(false)
    for (const [, init] of fetch.mock.calls.slice(1))
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe('original-key')
  })
  it.each(['duplicate', 'disconnected', 'cycle', 'wrong-project', 'bad-digest', 'private-field'])(
    'rejects a complete malformed %s preview without partial publication',
    async (fault) => {
      vi.stubGlobal('crypto', webcrypto)
      const value = preview()
      if (fault === 'duplicate') value.nodes.push(value.nodes[0]!)
      if (fault === 'disconnected') value.nodes[1]!.parent_document_id = id(99)
      if (fault === 'cycle') value.nodes[0]!.parent_document_id = id(21)
      if (fault === 'wrong-project') value.nodes[1]!.project_id = id(99)
      if (fault === 'bad-digest') value.scope_digest = 'sha256:' + '0'.repeat(64)
      if (fault === 'private-field') Object.assign(value.nodes[1]!, { object_id: id(99) })
      const w = write()
      await expect(
        createKnowledgeDeleteAPI(async () => json(value)).preview(id(10), id(20), {
          csrfToken: w.csrfToken,
          signal: w.signal,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    },
  )
  it('rejects preview key injection and unexpected command fields before dispatch', async () => {
    const fetch = vi.fn<Fetch>(),
      w = write()
    await expect(
      accountTransport(fetch)('knowledgeDeletePreview', (v) => v, {
        csrf: w.csrfToken,
        signal: w.signal,
        projectID: id(10),
        target: id(20),
        body: {},
        key: w.key,
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      createKnowledgeDeleteAPI(fetch).remove(
        id(10),
        id(20),
        { confirmation_token: token, expected_version: '1' } as never,
        w,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    { ...receipt(), deleted_ids: [id(21)] },
    { ...receipt(), deleted_ids: [id(21), id(20)] },
    { ...receipt(), deleted_ids: [id(20), id(20)] },
    { ...receipt(), cleanup_pending: 'true' },
  ])('rejects unsafe receipt %j', async (value) => {
    await expect(
      createKnowledgeDeleteAPI(async () => json(value)).remove(
        id(10),
        id(20),
        { confirmation_token: token },
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('does not apply a browser expiry check to original Lookup', async () => {
    const api = createKnowledgeDeleteAPI(async () => json(lookup()))
    await expect(
      api.lookup(id(10), id(20), { confirmation_token: token }, write()),
    ).resolves.toEqual(lookup())
  })
})
