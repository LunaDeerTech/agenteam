import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  artifactBase,
  artifactDeferred,
  artifactID,
  artifactJSON,
  artifactProblem,
  artifactProject,
  creationInput,
  createdRecord,
  creationSessionFixture,
} from './artifact-creation.fixture'
const cleanup: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanup.splice(0)) close()
  await flushPromises()
  vi.unstubAllGlobals()
})
async function fixture() {
  const f = creationSessionFixture()
  await f.auth.restore()
  cleanup.push(() => f.auth.leave())
  return f
}
const writes = (f: Awaited<ReturnType<typeof fixture>>) =>
  f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
describe('Artifact Session original intent and actual request ownership', () => {
  it('retains a sent result even after NotCommitted and permits only explicit same-intent lookup and observed replay', async () => {
    const f = await fixture(),
      input = { ...creationInput }
    f.intercept(async () => {
      const response = artifactProblem(503),
        value = await response.json()
      return new Response(JSON.stringify({ ...value, commit_state: 'not_committed' }), {
        status: 503,
        headers: response.headers,
      })
    })
    await expect(f.auth.artifactCreation.start(artifactProject, input)).rejects.toBeDefined()
    input.content = 'edited after send'
    expect(f.auth.artifactCreation.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'failed',
      canRetryOriginal: false,
      canCancel: false,
    })
    await expect(f.auth.artifactCreation.retryOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(f.auth.artifactCreation.cancelOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(writes(f)).toHaveLength(1)
    f.intercept(async () => artifactJSON({ state: 'not_observed', artifact: null }))
    await f.auth.artifactCreation.checkOriginal()
    expect(f.auth.artifactCreation.progress?.canRetryOriginal).toBe(true)
    await expect(f.auth.artifactCreation.cancelOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    f.intercept(async () => artifactJSON({ artifact: createdRecord() }))
    await f.auth.artifactCreation.retryOriginal()
    expect(f.auth.artifactCreation.progress?.phase).toBe('confirmed')
    const sent = writes(f)
    expect(sent.map(([path]) => path)).toEqual([
      artifactBase + '/create-content',
      artifactBase + '/creation-commands/lookup',
      artifactBase + '/create-content',
    ])
    expect(
      new Set(sent.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
    expect(sent.every(([, init]) => init.body === JSON.stringify(creationInput))).toBe(true)
    expect(JSON.stringify(f.auth.artifactCreation.progress)).not.toContain(creationInput.content)
  })
  it('offers cancel only for an observed pending intent, and waits for explicit cleanup lookup', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await expect(
      f.auth.artifactCreation.start(artifactProject, creationInput),
    ).rejects.toBeDefined()
    f.intercept(async () => artifactJSON({ state: 'pending', artifact: null }))
    await f.auth.artifactCreation.checkOriginal()
    expect(f.auth.artifactCreation.progress).toMatchObject({
      canRetryOriginal: false,
      canCancel: true,
    })
    f.intercept(async () => artifactJSON({ state: 'cancel_requested', cleanup: 'pending' }))
    await f.auth.artifactCreation.cancelOriginal()
    expect(f.auth.artifactCreation.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'cancel_requested',
    })
    expect(writes(f)).toHaveLength(3)
    f.intercept(async () => artifactJSON({ state: 'cancelled', artifact: null }))
    await f.auth.artifactCreation.checkOriginal()
    expect(f.auth.artifactCreation.progress?.phase).toBe('cancelled')
    expect(new Set(writes(f).map(([, init]) => init.body)).size).toBe(1)
    expect(
      new Set(writes(f).map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
  })
  it('keeps uncertain intent over same Session checking and rejects it after a true Session change', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await expect(
      f.auth.artifactCreation.start(artifactProject, creationInput),
    ).rejects.toBeDefined()
    const original = writes(f)[0]![1],
      held = artifactDeferred<Response>()
    cleanup.push(() => held.resolve(artifactJSON(f.sessionValue())))
    f.intercept((path, init) => (path === '/api/v1/session' ? held.promise : f.normal(path, init)))
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.auth.artifactCreation.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    held.resolve(artifactJSON(f.sessionValue()))
    await restore
    f.intercept(async () => artifactJSON({ state: 'pending', artifact: null }))
    await f.auth.artifactCreation.checkOriginal()
    expect(writes(f)[1]![1].body).toBe(original.body)
    expect(new Headers(writes(f)[1]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: artifactID(7) } })
    f.intercept(f.normal)
    await f.auth.restore()
    expect(f.auth.artifactCreation.progress).toBeNull()
    await expect(f.auth.artifactCreation.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it('abandoning local recovery keeps the original request busy until its actual body cancellation exits', async () => {
    const f = await fixture(),
      tail = artifactDeferred<void>(),
      reading = artifactDeferred<void>()
    cleanup.push(() => tail.resolve())
    const body = new ReadableStream<Uint8Array>(
      {
        pull() {
          reading.resolve()
        },
        cancel() {
          return tail.promise
        },
      },
      { highWaterMark: 0 },
    )
    f.intercept(async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }))
    const started = f.auth.artifactCreation
      .start(artifactProject, creationInput)
      .catch((error) => error)
    await reading.promise
    f.auth.artifactCreation.abandonLocal()
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.artifactCreation.progress).toBeNull()
    await expect(
      f.auth.artifactCreation.start(artifactProject, creationInput),
    ).rejects.toMatchObject({ kind: 'busy' })
    expect(writes(f)).toHaveLength(1)
    tail.resolve()
    await started
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.artifactCreation.progress).toBeNull()
  })
  it('blocks Knowledge commands while Artifact intent is unresolved, and blocks Artifact during Knowledge recovery', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await expect(
      f.auth.artifactCreation.start(artifactProject, creationInput),
    ).rejects.toBeDefined()
    await expect(
      f.auth.knowledgeCommands.startRename(artifactProject, artifactID(8), {
        expected_version: '1',
        title: 'renamed',
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.knowledgeText.startCreate(artifactProject, {
        parent_document_id: null,
        title: 'document',
        media_type: 'text/plain',
        text: '',
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    f.auth.artifactCreation.abandonLocal()
    await expect(
      f.auth.knowledgeCommands.startRename(artifactProject, artifactID(8), {
        expected_version: '1',
        title: 'renamed',
      }),
    ).rejects.toBeDefined()
    expect(f.auth.knowledgeCommands.progress?.phase).toBe('uncertain')
    await expect(
      f.auth.artifactCreation.start(artifactProject, creationInput),
    ).rejects.toMatchObject({ kind: 'busy' })
    expect(writes(f)).toHaveLength(2)
  })
})
