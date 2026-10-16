import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import {
  assigneeFixture,
  disposeAssigneeFixtures,
  prepareAssigneeDOM,
  sprintID,
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
async function chooseState(option: string) {
  await click('筛选状态')
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

describe('Task state filter controls', () => {
  it('explicitly applies and clears a state while keeping seven accurately labelled columns', async () => {
    const f = await assigneeFixture(true)
    f.fetcher.mockClear()
    await chooseState('待执行')
    expect(document.activeElement).toBe(button('筛选状态').element)
    expect(f.queries()).toEqual([])
    expect(body().findAll('.task-column')).toHaveLength(7)
    expect(body().findAll('.task-column [aria-label="已读取任务数"]')).toHaveLength(7)
    await submit()
    expect(f.queries()).toEqual([{ limit: '50', sprint_id: sprintID, state: 'todo' }])
    expect(body().get('.task-search-status').text()).toContain('状态：待执行')
    const columns = body().findAll('.task-column')
    expect(columns).toHaveLength(7)
    expect(body().get('.task-column[aria-label="待执行"] [aria-label="已读取任务数"]').text()).toBe(
      '50',
    )
    for (const column of columns.filter((item) => item.attributes('aria-label') !== '待执行')) {
      expect(column.text()).toContain('当前筛选未包含此状态')
      expect(column.text()).not.toContain('暂无任务')
      expect(column.findAll('[aria-label="已读取任务数"], button')).toHaveLength(0)
    }

    await chooseState('已完成')
    expect(f.queries()).toHaveLength(1)
    expect(body().get('.task-search-status').text()).toContain('状态：待执行')
    expect(body().get('.task-column[aria-label="待执行"]').findAll('.task-card')).toHaveLength(50)
    await click('清除筛选')
    expect(f.queries()).toHaveLength(8)
    expect(button('筛选状态').text()).toContain('全部状态')
    expect(button('清除筛选').element.disabled).toBe(true)
    expect(body().text()).not.toContain('当前筛选未包含此状态')
    expect(body().findAll('.task-column [aria-label="已读取任务数"]')).toHaveLength(7)
    expect(f.unexpected).toEqual([])
  })

  it('distinguishes an empty selected state from a failed read and retries only the selected column', async () => {
    const f = await assigneeFixture(true)
    f.fetcher.mockClear()
    await chooseState('已完成')
    await submit()
    expect(f.queries()).toEqual([{ limit: '50', sprint_id: sprintID, state: 'done' }])
    expect(body().get('.task-search-status').text()).toContain('没有匹配的任务，可清除筛选。')
    expect(body().get('.task-column[aria-label="已完成"]').text()).toContain('暂无任务')

    await chooseState('待执行')
    await submit()
    expect(button('更多待执行任务').element.disabled).toBe(false)
    await chooseState('已完成')
    await submit()
    await chooseState('待执行')
    f.fetcher.mockRejectedValueOnce(new TypeError('controlled task list failure'))
    await submit()
    expect(f.queries()).toHaveLength(4)
    expect(body().get('.task-column[aria-label="待执行"]').text()).toContain('任务读取失败')
    expect(body().get('.task-search-status').text()).not.toContain('没有匹配的任务')
    expect(body().text()).not.toContain('更多待执行任务')
    expect(body().get('.task-column[aria-label="待执行"]').findAll('.task-card')).toHaveLength(0)
    const reads = f.queries().length
    await f.owner.loadColumn('todo', true)
    expect(f.queries()).toHaveLength(reads)
    await click('重读此列')
    expect(f.queries()).toHaveLength(5)
    expect(f.queries().at(-1)).toEqual({ limit: '50', sprint_id: sprintID, state: 'todo' })
    expect(body().get('.task-column[aria-label="待执行"]').findAll('.task-card')).toHaveLength(50)
    expect(body().text()).not.toContain('任务读取失败')
    expect(f.unexpected).toEqual([])
  })
})
