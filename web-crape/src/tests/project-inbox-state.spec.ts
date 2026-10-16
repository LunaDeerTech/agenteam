import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectInbox } from '../composables/useProjectInbox'
import {
  inboxBase,
  inboxDecision,
  inboxDeferred,
  inboxFixture,
  inboxItem,
  inboxJSON,
  inboxPage,
  inboxPath,
  inboxProblem,
} from './project-inbox.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  cleanups.splice(0).forEach((close) => close())
  await flushPromises()
})
async function setup() {
  const f = inboxFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(inboxPath)
  workspace.afterNavigation(location.value)
  await flushPromises()
  const owner = useProjectInbox(f.auth, workspace, location)
  cleanups.push(() => {
    owner.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  expect(owner.page.phase).toBe('ready')
  return { ...f, owner, workspace, location }
}
describe('Inbox page current scope and list state', () => {
  it('loads ordered next pages and explicitly switches between pending and handled proposals', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, n) => inboxItem(200 - n))
    f.intercept((path, init) =>
      !path.startsWith(inboxBase)
        ? f.normal(path, init)
        : Promise.resolve(
            inboxJSON(
              path.includes('status=resolved')
                ? inboxPage([inboxItem(100, 'resolved')])
                : path.includes('before=')
                  ? inboxPage([inboxItem(150)])
                  : inboxPage(first, first.at(-1)!.id),
            ),
          ),
    )
    f.owner.refresh()
    await flushPromises()
    f.owner.more()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(51)
    expect(f.owner.filter('resolved')).toBe(true)
    await flushPromises()
    expect(f.owner.page.items.map((item) => item.status)).toEqual(['resolved'])
    expect(f.owner.page.nextBefore).toBeNull()
    expect(f.owner.meetingPath(f.owner.page.items[0]!)).toBe(
      '/owner/artifact-demo/meetings/' + inboxItem().source_id,
    )
    expect(f.owner.meetingPath({ ...inboxItem(), project_id: 'foreign' })).toBe('')
  })
  it('clears private rows on permission refusal and does not label refusal as an empty collection', async () => {
    const f = await setup()
    f.intercept((path, init) =>
      path.startsWith(inboxBase) ? Promise.resolve(inboxProblem(403)) : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.phase).toBe('unavailable')
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.page.message).toContain('不可访问')
    expect(f.auth.system.denied).toBe(false)
  })
  it('retires a pending list when the route changes and never republishes its late result', async () => {
    const f = await setup(),
      held = inboxDeferred<Response>()
    f.intercept((path, init) => (path.startsWith(inboxBase) ? held.promise : f.normal(path, init)))
    f.owner.refresh()
    await flushPromises()
    f.location.value = '/owner/other/inbox'
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
    held.resolve(inboxJSON(inboxPage([inboxItem(999)])))
    await flushPromises()
    expect(f.owner.page.items).toEqual([])
    expect(f.auth.state.busy).toBe(false)
    f.intercept(f.normal)
    f.location.value = inboxPath
    await flushPromises()
    expect(f.owner.page.items[0]?.id).toBe(inboxItem().id)
  })
  it('crosses from blocking Decisions to proposals and navigates through the typed parent Meeting', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, n) => inboxDecision(300 - n)),
      proposal = inboxItem()
    f.intercept((path, init) =>
      path.startsWith(inboxBase)
        ? Promise.resolve(
            inboxJSON(
              path.includes('before=') ? inboxPage([proposal]) : inboxPage(first, first.at(-1)!.id),
            ),
          )
        : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    f.owner.more()
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.page.items).toHaveLength(51)
    const decision = f.owner.page.items[0]!
    expect(decision.source_type).toBe('decision_request')
    expect(f.owner.meetingPath(decision)).toBe(
      '/owner/artifact-demo/meetings/' + inboxDecision(300).source_context.meeting_id,
    )
    expect(f.owner.meetingPath(f.owner.page.items[50]!)).toBe(
      '/owner/artifact-demo/meetings/' + proposal.source_id,
    )
  })
})
