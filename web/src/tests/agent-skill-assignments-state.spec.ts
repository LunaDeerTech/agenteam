import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  agent,
  assignment,
  base,
  id,
  json,
  pageFixture,
  problem,
  project,
  skill,
} from './agent-skill-assignments.fixture'
const clean: (() => void)[] = []
afterEach(async () => {
  clean.splice(0).forEach((dispose) => dispose())
  await flushPromises()
})
async function setup() {
  const f = await pageFixture()
  clean.push(f.dispose)
  return f
}
describe('Agent Skills coherent directory and command selection', () => {
  it('retires a known rejected Skills intent after confirmed navigation so an Agent PATCH can start', async () => {
    const f = await setup()
    f.setIntercept((path, init) => {
      if (path === base + '/assign') return problem('VERSION_CONFLICT')
      if (path.endsWith('/configuration') && init.method === 'PATCH')
        return json({
          changed: true,
          event_ids: [id(80)],
          agent: { ...f.state.config, version: '2', description: 'After leaving Skills' },
        })
    })
    await f.owner.skills.open()
    f.owner.skills.draft.selected = skill
    f.owner.skills.choose('assign')
    await f.owner.skills.submit()
    expect(f.auth.agentSkills.progress?.phase).toBe('rejected')
    const leaving = f.owner.confirmLeave('/owner/demo/tasks')
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.finishConfirmation(true)
    expect(await leaving).toBe(true)
    f.owner.afterNavigation('/owner/demo/tasks')
    expect(f.auth.agentSkills.progress).toBeNull()
    await expect(
      f.auth.agentConfiguration.startUpdate(project, agent, {
        expected_version: '1',
        request: { description: 'After leaving Skills' },
      }),
    ).resolves.toMatchObject({ changed: true })
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
  })
  it('keeps an uncertain Skills intent on confirmed navigation and queries its original key instead of freeing a write', async () => {
    const f = await setup()
    f.setIntercept((path) => {
      if (path === base + '/assign') throw new TypeError('lost response')
    })
    await f.owner.skills.open()
    f.owner.skills.draft.selected = skill
    f.owner.skills.choose('assign')
    await f.owner.skills.submit()
    const leaving = f.owner.confirmLeave('/owner/demo/tasks')
    f.owner.finishConfirmation(true)
    expect(await leaving).toBe(true)
    f.owner.afterNavigation('/owner/demo/tasks')
    expect(f.auth.agentSkills.progress?.phase).toBe('uncertain')
    await expect(
      f.auth.agentConfiguration.startUpdate(project, agent, {
        expected_version: '1',
        request: { description: 'Must remain blocked' },
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.agentSkills.checkOriginal()
    expect(f.calls('/assign')).toHaveLength(1)
    expect(new Headers(f.calls('/commands/lookup')[0]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(f.calls('/assign')[0]![1].headers).get('Idempotency-Key'),
    )
  })
  it('does not merge a later page from a changed Agent version and clears its cursor', async () => {
    const f = await setup(),
      rows = Array.from({ length: 25 }, (_, n) => assignment(n + 40))
    f.setIntercept((path) => {
      if (!path.startsWith(base + '?')) return
      const more = new URL(path, 'https://example.test').searchParams.has('cursor')
      return json({
        project_id: project,
        agent_id: agent,
        agent_version: more ? '2' : '1',
        assignment_sequence: '1',
        items: more ? [assignment(65)] : rows,
        ...(more ? {} : { next_cursor: 'next-page' }),
      })
    })
    await f.owner.skills.open()
    expect(f.owner.skills.state.items).toHaveLength(25)
    await f.owner.skills.loadAssignments(true)
    expect(f.owner.skills.state.items).toHaveLength(25)
    expect(f.owner.skills.state.next).toBeUndefined()
    expect(f.owner.skills.canManage.value).toBe(false)
    expect(f.owner.skills.state.message).toContain('版本已变化')
    expect(f.fetch.mock.calls.some(([p]) => p === base + '?limit=25&cursor=next-page')).toBe(true)
  })
  it('pins a selected command across a fresh read and requires cancellation before using the new version', async () => {
    const f = await setup()
    await f.owner.skills.open()
    f.owner.skills.draft.selected = skill
    f.owner.skills.choose('assign')
    expect(f.owner.canEdit.value).toBe(false)
    f.state.config = { ...f.state.config, version: '2' }
    f.state.sequence = '2'
    await f.owner.skills.refresh()
    expect(f.owner.skills.draft.command?.input.expected_version).toBe('1')
    expect(f.owner.skills.stale.value).toBe(true)
    await f.owner.skills.submit()
    expect(f.calls('/assign')).toHaveLength(0)
    f.owner.skills.cancel()
    f.owner.skills.draft.selected = skill
    f.owner.skills.choose('assign')
    expect(f.owner.skills.draft.command?.input.expected_version).toBe('2')
    expect(f.owner.skills.canConfirm.value).toBe(true)
  })
  it('isolates a late directory after navigation and clears local choices without resending', async () => {
    const f = await setup()
    let release!: (response: Response) => void
    f.setIntercept((path) =>
      path.startsWith(base + '?')
        ? new Promise<Response>((resolve) => {
            release = resolve
          })
        : undefined,
    )
    const read = f.owner.skills.open()
    await flushPromises()
    f.owner.afterNavigation('/owner/demo/tasks')
    release(
      json({
        project_id: project,
        agent_id: agent,
        agent_version: '1',
        assignment_sequence: '1',
        items: [assignment()],
      }),
    )
    await read
    expect(f.owner.skills.state.open).toBe(false)
    expect(f.owner.skills.state.items).toEqual([])
    expect(f.owner.skills.state.next).toBeUndefined()
    expect(f.calls('/assign')).toHaveLength(0)
  })
  it('uses the exact currently selected grant for removal and keeps archived projects read-only', async () => {
    const f = await setup()
    f.state.rows = [assignment(30, false)]
    await f.owner.skills.open()
    f.owner.skills.choose('remove', id(1030))
    expect(f.owner.skills.draft.command).toEqual({
      kind: 'remove',
      input: { expected_version: '1', request: { assignment_id: id(1030) } },
    })
    f.owner.skills.cancel()
    f.detail.project = { id: project, lifecycle: 'archived' }
    f.owner.skills.choose('enable', id(1030))
    expect(f.owner.skills.draft.command).toBeNull()
    expect(f.owner.skills.state.items[0]?.enabled).toBe(false)
    expect(f.owner.skills.canManage.value).toBe(false)
  })
})
