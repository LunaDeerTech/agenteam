import { expect, type Page, type Request } from '@playwright/test'
import { mkdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { observeTaskRequests, type TaskObservationProfile } from './task-review.helpers'

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
export type ReorderMilestone = {
  id: string
  project_id: string
  title: string
  description: string
  manual_rank: string
  version: string
  created_at: string
  updated_at: string
}
export type MilestoneReorderMaterial = {
  mode: 'milestone-reorder-normal' | 'milestone-reorder-responsive'
  cookie: string
  project_id: string
  other_project_id: string
  milestone_id: string
  route: string
  milestone: ReorderMilestone
  rows: ReorderMilestone[]
  input_hash: string
}
export function milestoneReorderMaterial(): MilestoneReorderMaterial {
  const value = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, 'milestone-reorder-material.json'),
      'utf8',
    ),
  )
  expect(value.mode).toBe(process.env.AGENTEAM_MILESTONE_REORDER_WEB_CASE)
  expect(['milestone-reorder-normal', 'milestone-reorder-responsive']).toContain(value.mode)
  for (const key of ['project_id', 'other_project_id', 'milestone_id'])
    expect(value[key]).toMatch(uuid)
  expect(value.other_project_id).not.toBe(value.project_id)
  expect(value.input_hash).toBe(process.env.AGENTEAM_MILESTONE_REORDER_WEB_INPUT_HASH)
  expect(value.route).toMatch(/^\/[^/?#]+\/[^/?#]+\/tasks$/)
  expect(value.rows).toHaveLength(54)
  expect(new Set(value.rows.map((m: ReorderMilestone) => m.id)).size).toBe(54)
  for (const [index, m] of value.rows.entries()) {
    expect(m.id).toMatch(uuid)
    expect(m.project_id).toBe(value.project_id)
    expect(m.title).toBe(`Milestone ${String(index + 1).padStart(2, '0')}`)
    expect(m.description).toBe('Original planning content')
    expect(m.version).toBe('1')
    expect(m.manual_rank).toMatch(/^[0-9a-f]{32}$/)
    expect(m.created_at).toBe(m.updated_at)
    if (index) expect(value.rows[index - 1].manual_rank < m.manual_rank).toBe(true)
  }
  expect(value.milestone).toEqual(value.rows[0])
  expect(value.milestone_id).toBe(value.milestone.id)
  return value
}
export function milestoneReorderProfile(): Omit<TaskObservationProfile, 'endpoint'> {
  return {
    dist: process.env.AGENTEAM_MILESTONE_REORDER_WEB_DIST!,
    evidence: process.env.AGENTEAM_MILESTONE_REORDER_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_MILESTONE_REORDER_WEB_INPUT_HASH!,
    prefix: 'milestone-reorder',
    lookupCommand: null,
    milestoneReorderWrites: true,
  }
}
export async function observeMilestoneReorder(page: Page, data: MilestoneReorderMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`,
    target = `${prefix}/milestones/${data.milestone_id}`
  let writes = 0
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method()
    if (!path.startsWith('/api/')) return undefined
    if (method === 'POST' && path === target + '/reorder' && !url.search) {
      expect(data.mode).toBe('milestone-reorder-normal')
      expect(writes).toBeLessThan(3)
      expect(request.postDataJSON()).toEqual({
        expected_version: String(writes + 1),
        request: writes === 0 ? { before_id: data.rows[53]!.id } : {},
      })
      writes++
      return 'reorder'
    }
    if (method !== 'GET') throw Error('UNDECLARED_MILESTONE_REORDER_WRITE_OR_LOOKUP')
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
    if (path === target && !url.search) return 'milestone'
    if (path === prefix + '/milestones') {
      expect(url.searchParams.get('limit')).toBe('50')
      expect([...url.searchParams.keys()].sort()).toEqual(
        url.searchParams.has('cursor') ? ['cursor', 'limit'] : ['limit'],
      )
      if (url.searchParams.has('cursor')) expect(url.searchParams.get('cursor')).not.toBe('')
      return 'milestones'
    }
    if (path === prefix + '/agents') {
      expect([...url.searchParams.entries()]).toEqual([['limit', '50']])
      return 'agents'
    }
    if (path === prefix + '/sprints') {
      expect([...url.searchParams.keys()].sort()).toEqual(['limit', 'milestone_id'])
      expect(url.searchParams.get('limit')).toBe('50')
      expect(url.searchParams.get('milestone_id')).toBe(data.milestone_id)
      return 'sprints'
    }
    throw Error('UNOBSERVED_MILESTONE_REORDER_ENDPOINT')
  }
  const seen = await observeTaskRequests(page, data, {
    ...milestoneReorderProfile(),
    endpoint: classify,
  })
  return {
    ...seen,
    result(index: number, before: ReorderMilestone): ReorderMilestone {
      const value = seen.receipt('reorder', index)
      expect(Object.keys(value).sort()).toEqual([
        'changed',
        'command',
        'event_id',
        'milestone',
        'sprint',
      ])
      expect(value.command).toBe('work.milestone.reorder')
      expect(value.sprint).toBeNull()
      expect(value.changed).toBe(index < 2)
      if (index < 2) {
        expect(value.event_id).toMatch(uuid)
        expect(value.milestone.manual_rank).not.toBe(before.manual_rank)
      } else expect(value.event_id).toBeNull()
      expect(value.milestone).toEqual({
        ...before,
        version: String(BigInt(before.version) + (index < 2 ? 1n : 0n)),
        manual_rank: index < 2 ? value.milestone.manual_rank : before.manual_rank,
        updated_at: index < 2 ? value.milestone.updated_at : before.updated_at,
      })
      expect(value.milestone.updated_at >= before.updated_at).toBe(true)
      return value.milestone
    },
    current(current: ReorderMilestone, index: number) {
      expect(seen.milestoneRead('milestone')).toEqual(current)
      const remaining = data.rows.filter((m) => m.id !== data.milestone_id)
      const order =
        index < 0
          ? data.rows
          : index === 0
            ? [...remaining.slice(0, 52), current, remaining[52]!]
            : [...remaining, current]
      expect(seen.milestoneRead('milestones').items).toEqual(
        order.slice(0, 50).map(({ description: _description, ...summary }) => summary),
      )
      expect(seen.milestoneRead('milestones').next_cursor).toEqual(expect.any(String))
      expect(seen.milestoneRead('sprints')).toEqual({ items: [] })
    },
  }
}
export async function milestoneReorderScreenshot(
  page: Page,
  data: MilestoneReorderMaterial,
  name: string,
) {
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    .toBe(true)
  expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(
    true,
  )
  const evidence = process.env.AGENTEAM_MILESTONE_REORDER_WEB_EVIDENCE!
  mkdirSync(evidence, { recursive: true, mode: 0o700 })
  await page.screenshot({
    path: join(evidence, `milestone-reorder-${data.mode}-${name}.png`),
    fullPage: true,
    animations: 'disabled',
  })
}
