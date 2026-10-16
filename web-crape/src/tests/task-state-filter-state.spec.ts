import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { taskStates } from '../api/work-review'
import {
  assigneeFixture,
  disposeAssigneeFixtures,
  prepareAssigneeDOM,
  id,
  selectedAgentID,
  sprintID,
  otherSprintID,
} from './task-assignee-filter.fixture'

beforeEach(prepareAssigneeDOM)
afterEach(disposeAssigneeFixtures)

describe('Task state filter with the original Session and Workspace', () => {
  it('queries one state with every applied condition and replaces its cursor only on explicit apply', async () => {
    const f = await assigneeFixture()
    await f.owner.loadFilterAgents(true)
    f.owner.search.stateDraft = 'todo'
    f.owner.search.draft = 'literal %_'
    f.owner.search.priorityDraft = 'high'
    f.owner.search.typeDraft = 'bug'
    f.owner.selectAssigneeFilter(selectedAgentID)
    f.fetcher.mockClear()
    await f.owner.applySearch()
    const query = {
      limit: '50',
      sprint_id: sprintID,
      state: 'todo',
      text: 'literal %_',
      priority: 'high',
      type: 'bug',
      assignee_agent_id: selectedAgentID,
    }
    expect(f.queries()).toEqual([query])
    expect(f.owner.columns.todo.items).toHaveLength(50)
    expect(f.owner.columns.todo.next).toBe('tasks-page-2')

    f.owner.search.stateDraft = 'backlog'
    expect(f.queries()).toEqual([query])
    await f.owner.loadColumn('todo', true)
    expect(f.queries()).toEqual([query, { ...query, cursor: 'tasks-page-2' }])
    expect(f.owner.columns.todo.items).toHaveLength(51)
    await f.owner.loadColumn('todo')
    expect(f.queries().at(-1)).toEqual(query)
    expect(f.owner.columns.todo.next).toBe('tasks-page-2')
    await f.owner.applySearch()
    expect(f.queries().at(-1)).toEqual({ ...query, state: 'backlog' })
    expect(f.owner.columns.todo.items).toEqual([])
    expect(f.owner.columns.todo.next).toBeUndefined()
    expect(f.owner.searchEmpty.value).toBe(true)
    const reads = f.queries().length
    await f.owner.loadColumn('todo')
    await f.owner.loadColumn('todo', true)
    expect(f.queries()).toHaveLength(reads)

    f.fetcher.mockClear()
    await f.owner.clearSearch()
    expect(f.queries()).toEqual(
      taskStates.map((state) => ({ limit: '50', sprint_id: sprintID, state })),
    )
    expect(f.owner.search.stateDraft).toBe('')
    expect(f.owner.search.state).toBe('')
    expect(f.owner.filtersActive.value).toBe(false)
    expect(f.unexpected).toEqual([])
  })

  it('keeps the selected state through same-Sprint detail navigation and clears it for another Sprint', async () => {
    const f = await assigneeFixture()
    f.owner.search.stateDraft = 'todo'
    await f.owner.applySearch()
    f.fetcher.mockClear()
    await f.owner.selectTask(id(1000))
    await flushPromises()
    expect(f.owner.state.task?.id).toBe(id(1000))
    expect(f.owner.search.state).toBe('todo')
    expect(f.queries()).toEqual([{ limit: '50', sprint_id: sprintID, state: 'todo' }])

    f.fetcher.mockClear()
    await f.owner.closeTask()
    await flushPromises()
    expect(f.owner.state.task).toBeNull()
    expect(f.owner.search.stateDraft).toBe('todo')
    expect(f.queries()).toEqual([{ limit: '50', sprint_id: sprintID, state: 'todo' }])

    f.fetcher.mockClear()
    await f.owner.selectSprint(otherSprintID)
    await flushPromises()
    expect(f.owner.state.sprint?.id).toBe(otherSprintID)
    expect(f.owner.search.state).toBe('')
    expect(f.owner.search.stateDraft).toBe('')
    expect(f.queries()).toEqual(
      taskStates.map((state) => ({ limit: '50', sprint_id: otherSprintID, state })),
    )
    expect(taskStates.every((state) => f.owner.columns[state].items.length === 0)).toBe(true)
    expect(f.unexpected).toEqual([])
  })

  it('discards a late filtered page and its cursor when the original Session leaves', async () => {
    const f = await assigneeFixture()
    f.owner.search.stateDraft = 'todo'
    await f.owner.applySearch()
    expect(f.owner.columns.todo.next).toBe('tasks-page-2')
    const release = f.holdTasks()
    const pending = f.owner.loadColumn('todo', true)
    await flushPromises()
    expect(f.owner.busy.value).toBe(true)
    f.auth.leave()
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.search.state).toBe('')
    expect(f.owner.search.stateDraft).toBe('')
    expect(f.owner.columns.todo.next).toBeUndefined()
    release()
    await pending
    await flushPromises()
    expect(f.owner.columns.todo.items).toEqual([])
    expect(f.owner.columns.todo.next).toBeUndefined()
    expect(f.owner.state.task).toBeNull()
    expect(f.unexpected).toEqual([])
  })
})
