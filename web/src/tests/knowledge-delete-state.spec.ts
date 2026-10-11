import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef, watch } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useKnowledgeOwner } from '../composables/useKnowledgeOwner'
import { useKnowledgeDelete } from '../composables/useKnowledgeDelete'
import {
  base,
  fixture,
  id,
  json,
  lookup,
  preview,
  problem,
  project,
  receipt,
  token,
} from './knowledge-delete.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const stop of cleanups.splice(0).reverse()) stop()
  await flushPromises()
  vi.unstubAllGlobals()
})
async function ready() {
  const f = fixture()
  cleanups.push(() => f.auth.leave())
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  cleanups.push(() => workspace.dispose())
  workspace.afterNavigation('/owner/delete/knowledge')
  await flushPromises()
  const owner = useKnowledgeOwner(
    f.auth,
    workspace,
    shallowRef({ projectPath: '/owner/delete', documentID: id(20) }),
  )
  cleanups.push(() => owner.dispose())
  await flushPromises()
  const deletion = useKnowledgeDelete(owner, workspace, f.auth)
  cleanups.push(() => deletion.dispose())
  return { ...f, workspace, owner, deletion }
}
describe('Knowledge deletion original Session and current reads', () => {
  it.each(['confirmed', 'not_observed'])(
    'rejects late public %s after a synchronous progress watcher retires Session',
    async (phase) => {
      const f = await ready()
      if (phase === 'not_observed') {
        f.intercept(async (path, init) => {
          if (path.endsWith('/delete-subtree')) throw new TypeError('lost')
          return f.normal(path, init)
        })
        await expect(
          f.auth.knowledgeDelete.startDelete(id(10), id(20), { confirmation_token: token }, [
            id(20),
            id(21),
          ]),
        ).rejects.toMatchObject({ kind: 'transport' })
        f.intercept(async () => json({ state: 'not_observed', receipt: null }))
      }
      const stop = watch(
        () => f.auth.knowledgeDelete.progress,
        (value) => {
          if (
            phase === 'confirmed'
              ? value?.phase === 'confirmed'
              : value?.observation === 'not_observed'
          )
            f.auth.leave()
        },
        { flush: 'sync' },
      )
      cleanups.push(stop)
      const pending =
        phase === 'confirmed'
          ? f.auth.knowledgeDelete.startDelete(id(10), id(20), { confirmation_token: token }, [
              id(20),
              id(21),
            ])
          : f.auth.knowledgeDelete.checkOriginal()
      await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    },
  )
  it('requires explicit confirmation then retires old content and reads the current tombstone and sibling directory', async () => {
    const f = await ready()
    await f.deletion.open()
    expect(f.deletion.state.nodes).toHaveLength(2)
    await f.deletion.remove()
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(0)
    f.deletion.state.accepted = true
    await f.deletion.remove()
    await flushPromises()
    expect(f.owner.state.metadataPhase).toBe('deleted')
    expect(f.owner.state.document).toBeNull()
    expect(f.owner.state.content.value).toBeNull()
    expect(f.owner.state.ancestors.items).toEqual([])
    expect(f.owner.root.value.items.map((v) => v.id)).toEqual([id(22)])
    expect(f.deletion.state.message).toContain('后台清理待完成')
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(1)
  })
  it('keeps original token/key after lost receipt, permits archived Lookup and forbids archived replay', async () => {
    const f = await ready()
    await f.deletion.open()
    f.intercept(async (path, init) => {
      if (path.endsWith('/delete-subtree')) {
        f.setDeleted()
        throw new TypeError('lost')
      }
      return f.normal(path, init)
    })
    f.deletion.state.accepted = true
    await f.deletion.remove()
    await flushPromises()
    const original = f.fetch.mock.calls.find(([p]) => p.endsWith('/delete-subtree'))![1]
    expect(f.deletion.canPreview.value).toBe(false)
    f.setProject({ ...project, lifecycle: 'archived', archived_at: '2026-10-10T12:00:00.123456Z' })
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.deletion.canReplay.value).toBe(false)
    f.intercept(async (path, init) =>
      path.endsWith('/commands/lookup') ? json(lookup()) : f.normal(path, init),
    )
    await f.deletion.lookup()
    await flushPromises()
    const check = f.fetch.mock.calls.find(([p]) => p.endsWith('/commands/lookup'))![1]
    expect(new Headers(check.headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
    expect(JSON.parse(check.body as string).request).toEqual(JSON.parse(original.body as string))
    expect(f.owner.state.metadataPhase).toBe('deleted')
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(1)
  })
  it('treats a different deleted set as uncertain and preserves the original intent for Lookup', async () => {
    const f = await ready()
    f.intercept(async () => json({ ...receipt(), deleted_ids: [id(20), id(22)] }))
    await expect(
      f.auth.knowledgeDelete.startDelete(id(10), id(20), { confirmation_token: token }, [
        id(20),
        id(21),
      ]),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(f.auth.knowledgeDelete.progress?.phase).toBe('uncertain')
    f.intercept(async () => json(lookup()))
    await expect(f.auth.knowledgeDelete.checkOriginal()).resolves.toEqual(lookup())
  })
  it.each([403, 404])(
    'clears visible preview/current private data on current preview refusal %s',
    async (status) => {
      const f = await ready()
      await f.deletion.open()
      f.intercept(async () => problem(status, status === 403 ? 'FORBIDDEN' : 'NOT_FOUND'))
      await f.deletion.loadPreview()
      await flushPromises()
      expect(f.deletion.state.nodes).toEqual([])
      expect(f.deletion.state.title).toBe('')
      expect(f.deletion.canDelete.value).toBe(false)
      expect(f.owner.state.document).toBeNull()
      expect(f.owner.state.content.value).toBeNull()
    },
  )
  it('requires a new explicit preview after a known noncommitted CONFIRMATION_STALE', async () => {
    const f = await ready()
    await f.deletion.open()
    f.intercept(async (path, init) =>
      path.endsWith('/delete-subtree')
        ? json(
            {
              type: 'urn:agenteam:problem:test',
              title: '',
              detail: '',
              status: 409,
              code: 'CONFIRMATION_STALE',
              instance: base,
              request_id: id(99),
              commit_state: 'not_committed',
            },
            409,
          )
        : f.normal(path, init),
    )
    f.deletion.state.accepted = true
    await f.deletion.remove()
    expect(f.auth.knowledgeDelete.progress?.phase).toBe('rejected')
    expect(f.deletion.canPreview.value).toBe(true)
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-preview'))).toHaveLength(1)
    await f.deletion.loadPreview()
    expect(f.deletion.state.nodes).toHaveLength(2)
    expect(f.deletion.state.accepted).toBe(false)
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(1)
  })
  it('never auto-refreshes an expired preview or sends a deletion on stale confirmation', async () => {
    const f = await ready()
    f.intercept(async (path, init) =>
      path.endsWith('/delete-preview')
        ? json({ ...preview(), expires_at: '2000-01-01T00:00:00.000000Z' })
        : f.normal(path, init),
    )
    await f.deletion.open()
    f.deletion.state.accepted = true
    await f.deletion.remove()
    expect(f.deletion.state.message).toContain('过期')
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-preview'))).toHaveLength(1)
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(0)
  })
})
