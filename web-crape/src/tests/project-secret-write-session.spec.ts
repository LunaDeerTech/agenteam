import { afterEach, describe, expect, it } from 'vitest'
import { watch } from 'vue'
import { flushPromises } from '@vue/test-utils'
import {
  secretWriteFixture,
  secretProject,
  secretID,
  secretBase,
  secretDeferred,
  secretJSON,
} from './project-secret-write.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
async function setup() {
  const f = secretWriteFixture()
  await f.auth.restore()
  cleanups.push(() => f.auth.leave())
  return f
}
describe('Secret command Session ownership without retained material', () => {
  it('retires a synchronous submitting publication before any material is dispatched', async () => {
    const f = await setup()
    const stop = watch(
      () => f.auth.projectSecretCommands.progress?.phase,
      (phase) => {
        if (phase === 'submitting') f.auth.projectSecretCommands.abandon()
      },
      { flush: 'sync' },
    )
    cleanups.push(stop)
    const result = await f.auth.projectSecretCommands
      .create(secretProject, { name: 'SYNTHETIC', description: '', value: 'test' })
      .catch((error) => error)
    expect(f.writes).toHaveLength(0)
    expect(result).toMatchObject({ kind: 'cancelled' })
  })
  it('stores only safe original identity after a lost response and never repeats the write on Lookup', async () => {
    const f = await setup(),
      material = 'synthetic-session-material\r\n'
    f.cutNext()
    await expect(
      f.auth.projectSecretCommands.update(secretProject, f.current.id, '1', { value: material }),
    ).rejects.toMatchObject({ kind: 'transport' })
    const progress = f.auth.projectSecretCommands.progress!
    expect(progress.phase).toBe('uncertain')
    expect(JSON.stringify(progress).includes(material)).toBe(false)
    expect(Object.keys(progress.identity).sort()).toEqual([
      'expectedVersion',
      'kind',
      'projectID',
      'targetID',
    ])
    expect(Object.hasOwn(progress, 'request')).toBe(false)
    const result = await f.auth.projectSecretCommands.lookup()
    expect('status' in result && result.status).toBe('committed')
    expect(f.writes).toHaveLength(1)
    expect(f.lookups).toEqual([
      {
        key: f.writes[0]!.key,
        body: {
          command: 'project.secret_variable.update',
          target_id: f.current.id,
          expected_version: '1',
        },
      },
    ])
    expect(
      f.fetch.mock.calls.some(
        ([path, init]) => path === secretBase + '/' + f.current.id && init.method === 'GET',
      ),
    ).toBe(false)
  })
  it('does not fulfill even a safe receipt after synchronous confirmed publication retires the identity', async () => {
    const f = await setup()
    const stop = watch(
      () => f.auth.projectSecretCommands.progress?.phase,
      (phase) => {
        if (phase === 'confirmed') f.auth.leave()
      },
      { flush: 'sync' },
    )
    cleanups.push(stop)
    await expect(
      f.auth.projectSecretCommands.create(secretProject, {
        name: 'SYNTHETIC',
        description: '',
        value: 'test',
      }),
    ).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.auth.projectSecretCommands.progress).toBeNull()
    expect(f.writes).toHaveLength(1)
  })
  it('retains the actual body-cancel owner after visible cancellation and only then permits another read', async () => {
    const f = await setup(),
      tail = secretDeferred<void>()
    cleanups.push(() => tail.resolve())
    let cancelled = 0
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') {
        const response = await f.normal(path, init),
          raw = await response.text()
        return new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(new TextEncoder().encode(raw))
            },
            cancel() {
              ++cancelled
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        )
      }
      return f.normal(path, init)
    })
    const pending = f.auth.projectSecretCommands
      .update(secretProject, f.current.id, '1', { value: 'synthetic-held' })
      .catch((error) => error)
    await flushPromises()
    f.auth.projectSecretCommands.abandon()
    await pending
    expect(cancelled).toBe(1)
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.projectSecrets.get(secretProject, secretID(10))).rejects.toMatchObject({
      kind: 'busy',
    })
    tail.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    await f.auth.projectSecretCommands.lookup()
    expect(f.writes).toHaveLength(1)
  })
  it('keeps not_observed uncertain and prevents creation of another key until identity is resolved', async () => {
    const f = await setup()
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') throw new TypeError('Controlled unavailable')
      return f.normal(path, init)
    })
    await expect(
      f.auth.projectSecretCommands.update(secretProject, f.current.id, '1', { value: 'synthetic' }),
    ).rejects.toMatchObject({ kind: 'transport' })
    const key = f.auth.projectSecretCommands.progress!.key
    await f.auth.projectSecretCommands.lookup()
    expect(f.auth.projectSecretCommands.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
      key,
    })
    await expect(
      f.auth.projectSecretCommands.create(secretProject, {
        name: 'OTHER',
        description: '',
        value: 'test',
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    expect(f.lookups).toHaveLength(1)
  })
})
