import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { watch } from 'vue'
import {
  agent,
  base,
  command,
  fixture,
  json,
  problem,
  project,
  receipt,
} from './agent-skill-assignments.fixture'
const clean: (() => void)[] = []
afterEach(async () => {
  clean.splice(0).forEach((dispose) => dispose())
  await flushPromises()
})
async function setup() {
  const f = await fixture()
  clean.push(f.dispose)
  return f
}
describe('Agent Skills original Session intent', () => {
  it.each(['confirmed', 'not_observed'] as const)(
    'rejects %s publication if a synchronous observer leaves the Session',
    async (publication) => {
      const f = await setup()
      if (publication === 'not_observed') {
        f.setIntercept((path) => {
          if (path === base + '/assign') throw new TypeError('lost response')
        })
        await expect(
          f.auth.agentSkills.start(project, agent, command(), '1'),
        ).rejects.toMatchObject({ kind: 'transport' })
      }
      const stop = watch(
        () => f.auth.agentSkills.progress,
        (value) => {
          if (
            publication === 'confirmed'
              ? value?.phase === 'confirmed'
              : value?.observation === 'not_observed'
          )
            f.auth.leave()
        },
        { flush: 'sync' },
      )
      clean.push(stop)
      const result =
        publication === 'confirmed'
          ? f.auth.agentSkills.start(project, agent, command(), '1')
          : f.auth.agentSkills.checkOriginal()
      await expect(result).rejects.toMatchObject({ kind: 'cancelled' })
      expect(f.auth.agentSkills.progress).toBeNull()
      expect(f.calls('/assign')).toHaveLength(1)
    },
  )
  it('keeps original bytes/key across Unknown and not-observed; blocks config writes and retires confirmed intent', async () => {
    const f = await setup(),
      c = command()
    f.setIntercept((path) => {
      if (path === base + '/assign') throw new TypeError('lost response')
    })
    await expect(f.auth.agentSkills.start(project, agent, c, '1')).rejects.toMatchObject({
      kind: 'transport',
    })
    expect(f.auth.agentSkills.progress).toMatchObject({ phase: 'uncertain', contextValid: true })
    await expect(
      f.auth.agentConfiguration.startUpdate(project, agent, {
        expected_version: '1',
        request: { description: 'changed' },
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.agentSkills.start(project, agent, c, '1')).rejects.toMatchObject({
      kind: 'busy',
    })
    await f.auth.agentSkills.checkOriginal()
    expect(f.auth.agentSkills.progress?.observation).toBe('not_observed')
    f.state.last = receipt(c)
    await f.auth.agentSkills.checkOriginal()
    expect(f.auth.agentSkills.progress).toMatchObject({
      phase: 'confirmed',
      contextValid: false,
      observation: 'committed',
    })
    expect(f.calls('/assign')).toHaveLength(1)
    expect(f.calls('/commands/lookup')).toHaveLength(2)
    const original = f.calls('/assign')[0]![1]
    for (const [, init] of f.calls('/commands/lookup')) {
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
      expect(JSON.parse(init.body as string)).toEqual({ command: 'skill.assign', ...c.input })
    }
    expect(JSON.stringify(f.auth.agentSkills.progress)).not.toContain('expected_version')
  })
  it('holds malformed receipt sequence as Unknown and only accepts the original committed sequence', async () => {
    const f = await setup()
    f.setIntercept((path) =>
      path === base + '/assign' ? json(receipt(command(), true, '3')) : undefined,
    )
    await expect(f.auth.agentSkills.start(project, agent, command(), '1')).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(f.auth.agentSkills.progress?.phase).toBe('uncertain')
    f.state.last = receipt()
    await f.auth.agentSkills.checkOriginal()
    expect(f.auth.agentSkills.progress?.receipt?.assignment_sequence).toBe('2')
  })
  it('requires explicit edit after a known version rejection and drops scope on current denial', async () => {
    const f = await setup()
    f.setIntercept((path) => (path === base + '/assign' ? problem('VERSION_CONFLICT') : undefined))
    await expect(f.auth.agentSkills.start(project, agent, command(), '1')).rejects.toMatchObject({
      kind: 'problem',
    })
    expect(f.auth.agentSkills.progress?.phase).toBe('rejected')
    await expect(f.auth.agentSkills.start(project, agent, command(), '1')).rejects.toMatchObject({
      kind: 'busy',
    })
    f.auth.agentSkills.editRejected()
    f.setIntercept((path) => (path === base + '/assign' ? problem('FORBIDDEN', 403) : undefined))
    await expect(f.auth.agentSkills.start(project, agent, command(), '1')).rejects.toMatchObject({
      kind: 'problem',
    })
    expect(f.auth.agentSkills.progress).toBeNull()
  })
  it('never publishes a late old-Session receipt or makes it queryable under a replacement identity', async () => {
    const f = await setup()
    let release!: (value: Response) => void
    f.setIntercept((path) =>
      path === base + '/assign'
        ? new Promise<Response>((resolve) => {
            release = resolve
          })
        : undefined,
    )
    const request = f.auth.agentSkills
      .start(project, agent, command(), '1')
      .catch((e: unknown) => e)
    await flushPromises()
    f.auth.leave()
    release(json(receipt()))
    await request
    expect(f.auth.agentSkills.progress).toBeNull()
    await expect(f.auth.agentSkills.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
})
