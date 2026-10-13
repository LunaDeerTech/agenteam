import { expect, type Page, type Request } from '@playwright/test'
import { mkdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { observeTaskRequests, type TaskObservationProfile } from './task-review.helpers'

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
export type ReorderSprint = {
  id: string
  project_id: string
  milestone_id: string
  title: string
  description: string
  manual_rank: string
  version: string
  created_at: string
  updated_at: string
  state: 'planned' | 'current'
  started_at: string | null
  started_by: Record<string, string> | null
  completed_at: null
  completed_by: null
}
export type SprintReorderMaterial = {
  mode: 'sprint-reorder-planned' | 'sprint-reorder-current'
  cookie: string
  project_id: string
  other_project_id: string
  milestone_id: string
  sprint_id: string
  route: string
  sprint: ReorderSprint
  milestone: Record<string, unknown>
  project: { id: string; version: string; current_sprint_id: string }
  rows: ReorderSprint[]
  input_hash: string
}
export function sprintReorderMaterial(): SprintReorderMaterial {
  const value = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, 'sprint-reorder-material.json'),
      'utf8',
    ),
  )
  expect(value.mode).toBe(process.env.AGENTEAM_SPRINT_REORDER_WEB_CASE)
  expect(['sprint-reorder-planned', 'sprint-reorder-current']).toContain(value.mode)
  for (const key of ['project_id', 'other_project_id', 'milestone_id', 'sprint_id'])
    expect(value[key]).toMatch(uuid)
  expect(value.other_project_id).not.toBe(value.project_id)
  expect(value.input_hash).toBe(process.env.AGENTEAM_SPRINT_REORDER_WEB_INPUT_HASH)
  expect(value.route).toMatch(new RegExp(`^/[^/?#]+/[^/?#]+/tasks/sprints/${value.sprint_id}$`))
  expect(value.rows).toHaveLength(54)
  expect(new Set(value.rows.map((s: ReorderSprint) => s.id)).size).toBe(54)
  const currentIndex = value.mode === 'sprint-reorder-current' ? 0 : 1
  for (const [index, sprint] of value.rows.entries()) {
    expect(sprint.id).toMatch(uuid)
    expect(sprint.project_id).toBe(value.project_id)
    expect(sprint.milestone_id).toBe(value.milestone_id)
    expect(sprint.title).toBe(`Sprint ${String(index + 1).padStart(2, '0')}`)
    expect(sprint.description).toBe('Original planning content')
    expect(sprint.version).toBe(index === currentIndex ? '2' : '1')
    expect(sprint.state).toBe(index === currentIndex ? 'current' : 'planned')
    expect(sprint.manual_rank).toMatch(/^[0-9a-f]{32}$/)
    expect(sprint.completed_at).toBeNull()
    expect(sprint.completed_by).toBeNull()
    if (index === currentIndex) {
      expect(sprint.started_at).toBe(sprint.updated_at)
      expect(sprint.started_by.kind).toBe('human')
      expect(sprint.started_by.user_id).toMatch(uuid)
    } else {
      expect(sprint.started_at).toBeNull()
      expect(sprint.started_by).toBeNull()
      expect(sprint.created_at).toBe(sprint.updated_at)
    }
    if (index) expect(value.rows[index - 1].manual_rank < sprint.manual_rank).toBe(true)
  }
  expect(value.sprint).toEqual(value.rows[0])
  expect(value.sprint_id).toBe(value.sprint.id)
  expect(value.project.current_sprint_id).toBe(value.rows[currentIndex].id)
  expect(value.milestone.id).toBe(value.milestone_id)
  return value
}
export function sprintReorderProfile(): Omit<TaskObservationProfile, 'endpoint'> {
  return {
    dist: process.env.AGENTEAM_SPRINT_REORDER_WEB_DIST!,
    evidence: process.env.AGENTEAM_SPRINT_REORDER_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_SPRINT_REORDER_WEB_INPUT_HASH!,
    prefix: 'sprint-reorder',
    lookupCommand: null,
    sprintReorderWrites: true,
  }
}
function summary(sprint: ReorderSprint) {
  const {
    description: _d,
    started_at: _sa,
    started_by: _sb,
    completed_at: _ca,
    completed_by: _cb,
    ...item
  } = sprint
  return item
}
export async function observeSprintReorder(page: Page, data: SprintReorderMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`,
    target = `${prefix}/sprints/${data.sprint_id}`
  let writes = 0
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method()
    if (!path.startsWith('/api/')) return undefined
    if (method === 'POST' && path === target + '/reorder' && !url.search) {
      expect(writes).toBeLessThan(3)
      expect(request.postDataJSON()).toEqual({
        expected_version: String(BigInt(data.sprint.version) + BigInt(writes)),
        request: {
          milestone_id: data.milestone_id,
          ...(writes === 0 ? { before_id: data.rows[53]!.id } : {}),
        },
      })
      writes++
      return 'reorder'
    }
    if (method !== 'GET') throw Error('UNDECLARED_SPRINT_REORDER_WRITE_OR_LOOKUP')
    const globals: Record<string, string> = {
      '/api/v1/session': 'session',
      '/api/v1/me': 'profile',
      '/api/v1/me/preferences': 'preferences',
      '/api/v1/auth/bootstrap': 'bootstrap',
      '/api/v1/projects/resolve': 'resolve',
      '/api/v1/projects': 'projects',
    }
    if (globals[path]) return globals[path]
    if (path === prefix && !url.search) return 'project'
    if (path === target && !url.search) return 'sprint'
    if (path === `${prefix}/milestones/${data.milestone_id}` && !url.search) return 'milestone'
    for (const name of ['milestones', 'agents']) {
      if (path === `${prefix}/${name}`) {
        expect([...url.searchParams.entries()]).toEqual([['limit', '50']])
        return name
      }
    }
    if (path === prefix + '/sprints') {
      expect(url.searchParams.get('limit')).toBe('50')
      expect(url.searchParams.get('milestone_id')).toBe(data.milestone_id)
      expect([...url.searchParams.keys()].sort()).toEqual(
        url.searchParams.has('cursor')
          ? ['cursor', 'limit', 'milestone_id']
          : ['limit', 'milestone_id'],
      )
      if (url.searchParams.has('cursor')) expect(url.searchParams.get('cursor')).not.toBe('')
      return 'sprints'
    }
    if (path === prefix + '/tasks') {
      expect([...url.searchParams.keys()].sort()).toEqual(['limit', 'sprint_id', 'state'])
      expect(url.searchParams.get('limit')).toBe('50')
      expect(url.searchParams.get('sprint_id')).toBe(data.sprint_id)
      expect([
        'backlog',
        'todo',
        'in_progress',
        'in_review',
        'blocked',
        'done',
        'cancelled',
      ]).toContain(url.searchParams.get('state'))
      return 'tasks'
    }
    throw Error('UNOBSERVED_SPRINT_REORDER_ENDPOINT')
  }
  const seen = await observeTaskRequests(page, data, {
    ...sprintReorderProfile(),
    endpoint: classify,
  })
  return {
    ...seen,
    result(index: number, before: ReorderSprint): ReorderSprint {
      const value = seen.receipt('reorder', index)
      expect(Object.keys(value).sort()).toEqual([
        'changed',
        'command',
        'event_id',
        'milestone',
        'sprint',
      ])
      expect(value.command).toBe('work.sprint.reorder')
      expect(value.milestone).toBeNull()
      expect(value.changed).toBe(index < 2)
      if (index < 2) {
        expect(value.event_id).toMatch(uuid)
        expect(value.sprint.manual_rank).not.toBe(before.manual_rank)
      } else expect(value.event_id).toBeNull()
      expect(value.sprint).toEqual({
        ...before,
        version: String(BigInt(before.version) + (index < 2 ? 1n : 0n)),
        manual_rank: index < 2 ? value.sprint.manual_rank : before.manual_rank,
        updated_at: index < 2 ? value.sprint.updated_at : before.updated_at,
      })
      expect(value.sprint.updated_at >= before.updated_at).toBe(true)
      return value.sprint
    },
    current(current: ReorderSprint, index: number) {
      const { started_by: _sb, completed_by: _cb, ...read } = current
      expect(seen.sprintEditRead('sprint')).toEqual(read)
      const others = data.rows.filter((s) => s.id !== data.sprint_id)
      const order =
        index < 0
          ? data.rows
          : index === 0
            ? [...others.slice(0, 52), current, others[52]!]
            : [...others, current]
      expect(seen.sprintEditRead('sprints').items).toEqual(order.slice(0, 50).map(summary))
      expect(seen.sprintEditRead('sprints').next_cursor).toEqual(expect.any(String))
      expect(seen.sprintEditRead('milestone')).toEqual(data.milestone)
      expect(seen.sprintEditRead('tasks')).toEqual({ items: [] })
      expect(seen.sprintEditRead('project')).toMatchObject({
        id: data.project_id,
        version: data.project.version,
        current_sprint_id: data.project.current_sprint_id,
        lifecycle: 'active',
      })
    },
  }
}
export async function sprintReorderScreenshot(
  page: Page,
  data: SprintReorderMaterial,
  name: string,
) {
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    .toBe(true)
  expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(
    true,
  )
  const evidence = process.env.AGENTEAM_SPRINT_REORDER_WEB_EVIDENCE!
  mkdirSync(evidence, { recursive: true, mode: 0o700 })
  await page.screenshot({
    path: join(evidence, `sprint-reorder-${data.mode}-${name}.png`),
    fullPage: true,
    animations: 'disabled',
  })
}
