import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { watch } from 'vue'
import { createKnowledgeDeleteAPI } from '../api/knowledge-delete'
import { createArtifactCreationAPI } from '../api/artifact-creation'
import { createAccountAPI, type SessionView } from '../api/account'
import { createKnowledgeMoveAPI } from '../api/knowledge-move'
import { createSessionController, type SessionController } from '../composables/useSession'
import { type Fetch } from '../api/client'
import {
  titleID as id,
  titleDocument as document,
  titleDeferred as deferred,
  titleJSON as json,
} from './knowledge-title-search.fixture'
const time = '2026-10-11T12:00:00.123456Z'
const input = { expected_parent_id: id(10), target_parent_id: null }
const deletion = { confirmation_token: 'e30.e30.' + 'A'.repeat(43) }
const artifact = { name: 'result.txt', media_type: 'text/plain' as const, content: 'Saved content' }
const session = (): SessionView => ({
  user: {
    id: id(2),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(3), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const owners: SessionController[] = []
afterEach(async () => {
  for (const auth of owners.splice(0)) auth.leave()
  await flushPromises()
})
async function fixture() {
  let current = session(),
    intercept: Fetch | undefined
  const fetch = vi.fn<Fetch>((path, init) =>
    path === '/api/v1/session' ? Promise.resolve(json(current)) : intercept!(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = {
    knowledgeMove: createKnowledgeMoveAPI(fetch),
    knowledgeDelete: createKnowledgeDeleteAPI(fetch),
    artifactCreation: createArtifactCreationAPI(fetch),
  }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    intercept(value: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      current = value
    },
  }
}
describe('Knowledge Move original Session owner', () => {
  it('keeps Move Unknown exclusive against deletion preview, deletion and Artifact creation until explicit abandon', async () => {
    const f = await fixture()
    f.intercept(async () => {
      throw new TypeError('controlled loss')
    })
    await expect(f.auth.knowledgeMove.startMove(id(1), id(11), input)).rejects.toMatchObject({
      kind: 'transport',
    })
    await expect(f.auth.knowledgeDelete.preview(id(1), id(11))).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.knowledgeDelete.startDelete(id(1), id(11), deletion, [id(11)]),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.artifactCreation.start(id(1), artifact)).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.fetch.mock.calls.filter(([path]) => path !== '/api/v1/session')).toHaveLength(1)
    expect(f.auth.knowledgeMove.progress?.phase).toBe('uncertain')
    f.auth.knowledgeMove.abandon()
    await expect(f.auth.artifactCreation.start(id(1), artifact)).rejects.toMatchObject({
      kind: 'transport',
    })
    expect(f.auth.artifactCreation.progress?.phase).toBe('uncertain')
  })
  it.each(['deletion', 'artifact'] as const)(
    'preserves an original %s Unknown when a new Move is refused',
    async (original) => {
      const f = await fixture()
      f.intercept(async () => {
        throw new TypeError('controlled loss')
      })
      const pending =
        original === 'deletion'
          ? f.auth.knowledgeDelete.startDelete(id(1), id(11), deletion, [id(11)])
          : f.auth.artifactCreation.start(id(1), artifact)
      await expect(pending).rejects.toMatchObject({ kind: 'transport' })
      await expect(f.auth.knowledgeMove.startMove(id(1), id(11), input)).rejects.toMatchObject({
        kind: 'busy',
      })
      expect(f.auth.knowledgeMove.progress).toBeNull()
      expect(
        (original === 'deletion'
          ? f.auth.knowledgeDelete.progress
          : f.auth.artifactCreation.progress
        )?.phase,
      ).toBe('uncertain')
      expect(f.fetch.mock.calls.filter(([path]) => path !== '/api/v1/session')).toHaveLength(1)
    },
  )
  it.each(['confirmed', 'not_observed'])(
    'rejects the late public %s result if its progress watcher retires the Session',
    async (phase) => {
      const f = await fixture()
      if (phase === 'not_observed') {
        f.intercept(async () => {
          throw new TypeError('controlled loss')
        })
        await f.auth.knowledgeMove.startMove(id(1), id(11), input).catch(() => undefined)
        f.intercept(async () => json({ state: 'not_observed', receipt: null }))
      } else
        f.intercept(async () => json({ document: document(11, 'Current', null), changed: true }))
      const stop = watch(
        () => f.auth.knowledgeMove.progress,
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
      try {
        await expect(
          phase === 'confirmed'
            ? f.auth.knowledgeMove.startMove(id(1), id(11), input)
            : f.auth.knowledgeMove.checkOriginal(),
        ).rejects.toMatchObject({ kind: 'cancelled' })
        expect(f.auth.knowledgeMove.progress).toBeNull()
      } finally {
        stop()
      }
    },
  )

  it('retains the exact original body/key through Unknown, key-bound Lookup and explicit replay', async () => {
    const f = await fixture()
    let writes = 0
    f.intercept(async (path) => {
      if (path.endsWith('/commands/lookup')) return json({ state: 'not_observed', receipt: null })
      if (++writes === 1) throw new TypeError('controlled lost receipt')
      return json({ document: document(11, '并发标题', null), changed: true })
    })
    await expect(f.auth.knowledgeMove.startMove(id(1), id(11), input)).rejects.toMatchObject({
      kind: 'transport',
    })
    expect(f.auth.knowledgeMove.progress).toMatchObject({
      phase: 'uncertain',
      canRetryOriginal: false,
    })
    await expect(
      f.auth.knowledgeMove.startMove(id(1), id(11), { ...input, target_parent_id: id(12) }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.knowledgeCommands.startRename(id(1), id(11), { expected_version: '1', title: 'x' }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.knowledgeMove.checkOriginal()
    expect(writes).toBe(1)
    expect(f.auth.knowledgeMove.progress?.canRetryOriginal).toBe(true)
    await f.auth.knowledgeMove.retryOriginal()
    expect(f.auth.knowledgeMove.progress?.phase).toBe('confirmed')
    const calls = f.fetch.mock.calls.filter(([p]) => p !== '/api/v1/session')
    expect(calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      input,
      { command: 'move', document_id: id(11), request: input },
      input,
    ])
    const keys = calls.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))
    expect(keys[0]).toBeTruthy()
    expect(new Set(keys).size).toBe(1)
  })
  it('keeps original intent after same-session restore and clears it on a different identity', async () => {
    const f = await fixture()
    f.intercept(async () => {
      throw new TypeError('controlled loss')
    })
    await f.auth.knowledgeMove.startMove(id(1), id(11), input).catch(() => undefined)
    await f.auth.restore()
    expect(f.auth.knowledgeMove.progress).toMatchObject({ phase: 'uncertain', contextValid: true })
    f.session({ ...session(), session: { ...session().session, id: id(88) } })
    await f.auth.restore()
    expect(f.auth.knowledgeMove.progress).toBeNull()
    await expect(f.auth.knowledgeMove.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it('abandon aborts but keeps the owner busy until actual original transport retirement', async () => {
    const f = await fixture(),
      held = deferred<void>()
    let originalSignal: AbortSignal | undefined
    f.intercept(async (_path, init) => {
      originalSignal = init.signal!
      await held.promise
      return json({ document: document(11, '当前标题', null), changed: true })
    })
    const pending = f.auth.knowledgeMove.startMove(id(1), id(11), input).catch((e) => e)
    await flushPromises()
    expect(originalSignal).toBeDefined()
    f.auth.knowledgeMove.abandon()
    expect(originalSignal!.aborted).toBe(true)
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.knowledgeMove.progress).toBeNull()
    held.resolve()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    await vi.waitFor(() => expect(f.auth.state.busy).toBe(false))
    expect(f.auth.knowledgeMove.progress).toBeNull()
  })
})
