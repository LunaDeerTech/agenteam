import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { taskStates } from '../api/work-review'
import {
  assigneeFixture,
  disposeAssigneeFixtures,
  prepareAssigneeDOM,
  id,
  selectedAgentID,
  otherAgentID,
  sprintID,
} from './task-assignee-filter.fixture'

beforeEach(prepareAssigneeDOM)
afterEach(disposeAssigneeFixtures)

describe('Task assignee filter with the original Session and Workspace', () => {
  it('loads real directory pages, explicitly applies every column and preserves the applied cursor binding', async () => {
    const f = await assigneeFixture()
    expect(f.owner.state.phase).toBe('ready')
    expect(f.owner.state.agents).toHaveLength(50)
    expect(f.owner.filterAgentOptions.value.some((a) => a.value === selectedAgentID)).toBe(false)
    f.owner.selectAssigneeFilter(selectedAgentID)
    expect(f.owner.search.assigneeDraft).toBe('')
    await f.owner.loadFilterAgents(true)
    expect(f.owner.state.agents).toHaveLength(51)
    expect(f.directoryQueries()).toEqual([
      { limit: '50' },
      { limit: '50', cursor: 'agents-page-2' },
    ])
    f.fetcher.mockClear()
    f.owner.selectAssigneeFilter(selectedAgentID)
    f.owner.search.draft = 'literal %_'
    f.owner.search.priorityDraft = 'high'
    f.owner.search.typeDraft = 'bug'
    expect(f.queries()).toEqual([])
    await f.owner.applySearch()
    expect(f.queries()).toEqual(
      taskStates.map((state) => ({
        limit: '50',
        sprint_id: sprintID,
        state,
        text: 'literal %_',
        priority: 'high',
        type: 'bug',
        assignee_agent_id: selectedAgentID,
      })),
    )
    expect(f.owner.columns.todo.items).toHaveLength(50)
    f.owner.selectAssigneeFilter('null')
    f.owner.search.priorityDraft = 'low'
    await f.owner.loadColumn('todo', true)
    expect(f.queries().at(-1)).toEqual({
      limit: '50',
      sprint_id: sprintID,
      state: 'todo',
      text: 'literal %_',
      priority: 'high',
      type: 'bug',
      assignee_agent_id: selectedAgentID,
      cursor: 'tasks-page-2',
    })
    expect(f.owner.columns.todo.items).toHaveLength(51)
    expect(f.owner.filterAgentLabel.value).toBe('第二页 Agent (worker-100)')
    expect(f.unexpected).toEqual([])
  })

  it('distinguishes an unassigned query, an empty result and clearing all conditions', async () => {
    const f = await assigneeFixture()
    f.owner.selectAssigneeFilter('null')
    await f.owner.applySearch()
    expect(f.owner.columns.backlog.items.map((item) => item.title)).toEqual(['未指派任务'])
    expect(f.owner.columns.todo.items).toEqual([])
    expect(
      f
        .queries()
        .slice(-7)
        .every((q) => q.assignee_agent_id === 'null'),
    ).toBe(true)
    f.owner.search.draft = 'no match'
    await f.owner.applySearch()
    expect(f.owner.searchEmpty.value).toBe(true)
    f.fetcher.mockClear()
    await f.owner.clearSearch()
    expect(f.queries()).toEqual(
      taskStates.map((state) => ({ limit: '50', sprint_id: sprintID, state })),
    )
    expect(f.owner.search.assigneeDraft).toBe('')
    expect(f.owner.search.assignee).toBe('')
    expect(f.owner.filtersActive.value).toBe(false)
    expect(f.owner.columns.todo.items).toHaveLength(50)
    f.owner.search.assigneeDraft = id(999)
    f.fetcher.mockClear()
    await f.owner.applySearch()
    expect(f.queries()).toEqual([])
    expect(f.owner.search.message).toContain('有效的')
  })

  it('preserves the selected Agent across first-page refresh and explicitly restarts a stale directory cursor', async () => {
    const f = await assigneeFixture()
    await f.owner.loadFilterAgents(true)
    f.owner.selectAssigneeFilter(selectedAgentID)
    await f.owner.applySearch()
    await f.owner.loadFilterAgents()
    expect(f.owner.state.agents).toHaveLength(50)
    expect(
      f.owner.filterAgentOptions.value.find((option) => option.value === selectedAgentID)?.label,
    ).toBe('第二页 Agent (worker-100)')
    f.failDirectory('CURSOR_STALE')
    const queriesBefore = f.queries().length
    await f.owner.loadFilterAgents(true)
    expect(f.owner.state.agentNext).toBeUndefined()
    expect(f.owner.state.agents).toEqual([])
    expect(f.owner.state.agentMessage).toContain('分页已失效')
    expect(f.owner.search.assignee).toBe(selectedAgentID)
    expect(f.owner.search.assigneeDraft).toBe(selectedAgentID)
    expect(f.queries()).toHaveLength(queriesBefore)
    const readsBefore = f.directoryQueries().length
    await f.owner.loadFilterAgents(true)
    expect(f.directoryQueries()).toHaveLength(readsBefore)
    await f.owner.loadFilterAgents()
    expect(f.directoryQueries().at(-1)).toEqual({ limit: '50' })
    expect(f.owner.state.agentMessage).toBe('')
    expect(f.owner.search.assignee).toBe(selectedAgentID)
  })

  it('keeps the original directory cursor on a transport failure without automatically retrying', async () => {
    const f = await assigneeFixture()
    f.fetcher.mockRejectedValueOnce(new TypeError('controlled failure'))
    await f.owner.loadFilterAgents(true)
    expect(f.owner.state.agents).toHaveLength(50)
    expect(f.owner.state.agentNext).toBe('agents-page-2')
    expect(f.owner.state.agentMessage).not.toBe('')
    expect(f.directoryQueries()).toHaveLength(2)
    await f.owner.loadFilterAgents(true)
    expect(f.owner.state.agents).toHaveLength(51)
    expect(f.directoryQueries()).toHaveLength(3)
  })

  it('discards a late directory response and both filters when the real Session identity is removed', async () => {
    const f = await assigneeFixture()
    f.owner.selectAssigneeFilter(otherAgentID)
    await f.owner.applySearch()
    const release = f.holdDirectory()
    const pending = f.owner.loadFilterAgents(true)
    await flushPromises()
    expect(f.owner.busy.value).toBe(true)
    f.auth.leave()
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.search.assignee).toBe('')
    release()
    await pending
    await flushPromises()
    expect(f.owner.state.agents).toEqual([])
    expect(f.owner.state.agentNext).toBeUndefined()
    expect(f.owner.filterAgentOptions.value).toEqual([
      { value: '', label: '全部 Agent' },
      { value: 'null', label: '未指派' },
    ])
    expect(f.owner.columns.todo.items).toEqual([])
  })

  it('clears retained Agent selections after the current Project Get explicitly denies access', async () => {
    const f = await assigneeFixture()
    await f.owner.loadFilterAgents(true)
    f.owner.selectAssigneeFilter(selectedAgentID)
    await f.owner.applySearch()
    f.denyProject()
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.search.assignee).toBe('')
    expect(f.owner.search.assigneeDraft).toBe('')
    expect(f.owner.filterAgentOptions.value).toEqual([
      { value: '', label: '全部 Agent' },
      { value: 'null', label: '未指派' },
    ])
  })
})
