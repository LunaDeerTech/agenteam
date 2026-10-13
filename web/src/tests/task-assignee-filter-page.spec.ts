import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import {
  assigneeFixture,
  disposeAssigneeFixtures,
  prepareAssigneeDOM,
  selectedAgentID,
} from './task-assignee-filter.fixture'

beforeEach(prepareAssigneeDOM)
afterEach(disposeAssigneeFixtures)
const body = () => new DOMWrapper(document.body)
function button(label: string) {
  const result = body()
    .findAll<HTMLButtonElement>('button')
    .find((item) => {
      const clone = item.element.cloneNode(true) as HTMLButtonElement
      clone.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
      return (clone.getAttribute('aria-label') ?? clone.textContent?.trim()) === label
    })
  expect(result, `missing button: ${label}`).toBeDefined()
  return result!
}
async function click(label: string) {
  button(label).element.focus()
  await button(label).trigger('click')
  await flushPromises()
}
async function choose(label: string, option: string) {
  await click(label)
  const item = body()
    .findAll('[role="option"]')
    .find((item) => item.text() === option)
  expect(item).toBeDefined()
  await item!.trigger('click')
  await flushPromises()
}
async function submit() {
  await body().get('form[aria-label="Sprint 任务搜索"]').trigger('submit')
  await flushPromises()
}

describe('Task Agent filter controls', () => {
  it('loads another directory page, restores selector focus and only applies after explicit search', async () => {
    const f = await assigneeFixture(true)
    await click('加载更多筛选 Agent')
    expect(document.activeElement).toBe(button('筛选 Agent').element)
    expect(body().text()).not.toContain('加载更多筛选 Agent')
    f.fetcher.mockClear()
    await choose('筛选 Agent', '第二页 Agent (worker-100)')
    expect(document.activeElement).toBe(button('筛选 Agent').element)
    expect(f.queries()).toEqual([])
    await submit()
    expect(f.queries()).toHaveLength(7)
    expect(f.queries().every((q) => q.assignee_agent_id === selectedAgentID)).toBe(true)
    expect(body().get('.task-search-status').text()).toContain('Agent：第二页 Agent (worker-100)')
    await choose('筛选 Agent', '未指派')
    expect(f.queries()).toHaveLength(7)
    await submit()
    expect(
      f
        .queries()
        .slice(-7)
        .every((q) => q.assignee_agent_id === 'null'),
    ).toBe(true)
    expect(body().text()).toContain('未指派任务')
    await click('清除筛选')
    expect(
      f
        .queries()
        .slice(-7)
        .every((q) => !Object.hasOwn(q, 'assignee_agent_id')),
    ).toBe(true)
    expect(button('筛选 Agent').text()).toContain('全部 Agent')
    expect(button('清除筛选').element.disabled).toBe(true)
    expect(f.unexpected).toEqual([])
  })

  it('shows a stale directory error and a user-triggered recovery without losing the chosen filter', async () => {
    const f = await assigneeFixture(true)
    await choose('筛选 Agent', 'worker-150')
    await submit()
    f.failDirectory('CURSOR_INVALID')
    await click('加载更多筛选 Agent')
    expect(body().text()).toContain('Agent 目录分页已失效')
    expect(body().text()).not.toContain('加载更多筛选 Agent')
    expect(button('筛选 Agent').text()).toContain('worker-150')
    expect(document.activeElement).toBe(button('筛选 Agent').element)
    const taskReads = f.queries().length
    await click('重新读取筛选 Agent')
    expect(f.directoryQueries().at(-1)).toEqual({ limit: '50' })
    expect(f.queries()).toHaveLength(taskReads)
    expect(button('筛选 Agent').text()).toContain('worker-150')
    expect(document.activeElement).toBe(button('筛选 Agent').element)
    expect(body().text()).not.toContain('Agent 目录分页已失效')
  })
})
