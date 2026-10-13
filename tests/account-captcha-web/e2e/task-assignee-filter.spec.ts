import { test, expect, type Request, type Response } from '@playwright/test'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { observeTaskRequests } from './task-review.helpers'

const states = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'blocked',
  'done',
  'cancelled',
] as const
type State = (typeof states)[number]
type Task = {
  id: string
  project_id: string
  sprint_id: string
  title: string
  description: string
  plan: string
  state: State
  priority: string
  type: string
  manual_rank: string
  assignee_agent_id?: string | null
}
type Agent = { id: string; name: string; display_name?: string | null }
type Material = {
  mode: string
  cookie: string
  project_id: string
  sprint_id: string
  route: string
  foreign_id: string
  agent_id: string
  other_agent_id: string
  target: Task
  rows: Task[]
  agents: Agent[]
  term: string
  input_hash: string
}
type Filter = { assignee?: string; text?: string; priority?: string; type?: string }
const all: Filter = {}

// Only the accepted read-only Work shell, its current Project and real seeded
// identities are admitted. The observer neither authorizes nor issues reads.
function classify(request: Request, data: Material): string | undefined {
  const { pathname } = new URL(request.url())
  if (!pathname.startsWith('/api/')) return undefined
  if (request.method() !== 'GET') throw Error('ASSIGNEE_FILTER_WRITE_FORBIDDEN')
  const global: Record<string, string> = {
    '/api/v1/session': 'session',
    '/api/v1/me': 'profile',
    '/api/v1/me/preferences': 'preferences',
    '/api/v1/auth/bootstrap': 'bootstrap',
    '/api/v1/projects/resolve': 'resolve',
    '/api/v1/projects': 'projects',
  }
  if (global[pathname]) return global[pathname]
  const prefix = `/api/v1/projects/${data.project_id}`
  if (pathname === prefix) return 'project'
  if (!pathname.startsWith(prefix + '/')) throw Error('ASSIGNEE_FILTER_PROJECT_SCOPE')
  const path = pathname.slice(prefix.length)
  const reads: Record<string, string> = {
    '/milestones': 'milestones',
    '/sprints': 'sprints',
    '/tasks': 'tasks',
    '/agents': 'agents',
    [`/sprints/${data.sprint_id}`]: 'sprint',
    [`/tasks/${data.target.id}`]: 'task',
    [`/tasks/${data.target.id}/blockers`]: 'blockers',
    [`/tasks/${data.target.id}/events`]: 'events',
  }
  if (reads[path]) return reads[path]
  if (data.agents.some((agent) => path === `/agents/${agent.id}`)) return 'agent'
  if (
    /^\/milestones\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(path)
  )
    return 'milestone'
  throw Error('UNOBSERVED_ASSIGNEE_FILTER_ENDPOINT')
}

function expected(data: Material, state: State, filter: Filter) {
  const order: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 }
  return data.rows
    .filter(
      (row) =>
        row.state === state &&
        (!filter.priority || row.priority === filter.priority) &&
        (!filter.type || row.type === filter.type) &&
        (!filter.assignee ||
          (filter.assignee === 'null'
            ? !row.assignee_agent_id
            : row.assignee_agent_id === filter.assignee)) &&
        (!filter.text ||
          [row.title, row.description, row.plan].some((value) => value.includes(filter.text!))),
    )
    .sort(
      (a, b) =>
        order[a.priority]! - order[b.priority]! ||
        (a.manual_rank < b.manual_rank
          ? -1
          : a.manual_rank > b.manual_rank
            ? 1
            : a.id < b.id
              ? -1
              : a.id > b.id
                ? 1
                : 0),
    )
    .map(({ description: _description, plan: _plan, ...summary }) => summary)
}

for (const mode of ['assignee-pagination', 'assignee-combination-detail'] as const) {
  test(`current Sprint Agent filtering [${mode}]`, async ({ page }, testInfo) => {
    const data: Material = JSON.parse(
      readFileSync(
        join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, 'task-assignee-filter-material.json'),
        'utf8',
      ),
    )
    expect(data.mode).toBe(mode)
    expect(data.input_hash).toBe(process.env.AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_INPUT_HASH)
    expect(data.agents).toHaveLength(51)
    expect(data.agents[50]!.id).toBe(data.agent_id)
    const evidence = process.env.AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_EVIDENCE!
    const observations: { state: string; filter: Filter; cursor: boolean; ids: string[] }[] = []
    const faults: string[] = []
    const onError = () => faults.push('page-error')
    page.on('pageerror', onError)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' })
    await page.context().addCookies([
      {
        name: 'agenteam_local_session',
        value: data.cookie,
        url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
        httpOnly: true,
        sameSite: 'Lax',
        secure: false,
      },
    ])
    const seen = await observeTaskRequests(
      page,
      { ...data, task_id: data.target.id },
      {
        dist: process.env.AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_DIST!,
        evidence,
        inputHash: process.env.AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_INPUT_HASH!,
        prefix: 'task-assignee-filter',
        endpoint: (request) => classify(request, data),
        lookupCommand: null,
      },
    )
    const form = page.getByRole('search', { name: 'Sprint 任务搜索', exact: true })
    const input = form.getByRole('searchbox', { name: '搜索任务', exact: true })
    const submit = form.getByRole('button', { name: '搜索', exact: true })
    const clear = form.getByRole('button', { name: '清除筛选', exact: true })
    const target = page.getByRole('button', { name: data.target.title, exact: true })
    const taskPath = `/api/v1/projects/${data.project_id}/tasks`
    const agentPath = `/api/v1/projects/${data.project_id}/agents`
    const label = (id: string) => {
      const agent = data.agents.find((item) => item.id === id)!
      return agent.display_name && agent.display_name !== agent.name
        ? `${agent.display_name} (${agent.name})`
        : agent.name
    }
    const ownFilter: Filter = { assignee: data.agent_id, priority: 'high', type: 'bug' }
    const combination: Filter = { ...ownFilter, text: data.term }

    function queryMatches(response: Response, filter: Filter, state: State) {
      const u = new URL(response.url())
      return (
        response.request().method() === 'GET' &&
        u.pathname === taskPath &&
        u.searchParams.get('state') === state &&
        !u.searchParams.has('cursor') &&
        u.searchParams.get('assignee_agent_id') === (filter.assignee || null) &&
        u.searchParams.get('text') === (filter.text || null) &&
        u.searchParams.get('priority') === (filter.priority || null) &&
        u.searchParams.get('type') === (filter.type || null)
      )
    }
    function assertQuery(response: Response, state: State, filter: Filter, cursor?: string) {
      expect(Object.fromEntries(new URL(response.url()).searchParams)).toEqual({
        sprint_id: data.sprint_id,
        state,
        limit: '50',
        ...(filter.assignee ? { assignee_agent_id: filter.assignee } : {}),
        ...(filter.text ? { text: filter.text } : {}),
        ...(filter.priority ? { priority: filter.priority } : {}),
        ...(filter.type ? { type: filter.type } : {}),
        ...(cursor ? { cursor } : {}),
      })
    }
    async function columns(filter: Filter, action: () => Promise<unknown>) {
      const waits = states.map((state) =>
        page.waitForResponse((response) => queryMatches(response, filter, state)),
      )
      const responses = await Promise.all([...waits, action()])
      // Each original request must finish, its real native reader/public Work
      // facade must consume the body, and the Go EOF/Close/return proof must join.
      await seen.verify()
      const cursors: Partial<Record<State, string>> = {}
      for (let n = 0; n < states.length; n++) {
        const response = responses[n] as Response
        const state = states[n]!
        assertQuery(response, state, filter)
        const body = seen.taskSearchRead(response.request())
        const want = expected(data, state, filter)
        expect(body.items).toEqual(want.slice(0, 50))
        expect(Boolean(body.next_cursor)).toBe(want.length > 50)
        for (const row of body.items) {
          expect(row.project_id).toBe(data.project_id)
          expect(row.sprint_id).toBe(data.sprint_id)
          expect(row.id).not.toBe(data.foreign_id)
        }
        if (body.next_cursor) cursors[state] = body.next_cursor
        observations.push({
          state,
          filter,
          cursor: false,
          ids: body.items.map((row: Task) => row.id),
        })
      }
      await expect(page.getByText('正在读取任务', { exact: true })).toHaveCount(0)
      return cursors
    }
    async function select(name: string, option: string) {
      const trigger = form.getByRole('button', { name, exact: true })
      await trigger.focus()
      await page.keyboard.press('Enter')
      const choice = page
        .getByRole('listbox', { name, exact: true })
        .getByRole('option', { name: option, exact: true })
      await choice.focus()
      await page.keyboard.press('Enter')
      await expect(trigger).toBeFocused()
      await expect(trigger).toContainText(option)
    }
    async function draft(assignee: string, priority: string, type: string) {
      const before = seen.requests('tasks').length
      await select('筛选 Agent', assignee)
      await select('筛选优先级', priority)
      await select('筛选类型', type)
      await seen.verify()
      expect(seen.requests('tasks')).toHaveLength(before)
    }
    async function directory() {
      const first = seen.requests('agents')
      expect(first).toHaveLength(1)
      const initial = seen.taskSearchRead(first[0]!)
      expect(initial.items).toEqual(data.agents.slice(0, 50))
      expect(initial.next_cursor).toEqual(expect.any(String))
      const trigger = form.getByRole('button', { name: '筛选 Agent', exact: true })
      await trigger.focus()
      await page.keyboard.press('Enter')
      const options = page.getByRole('listbox', { name: '筛选 Agent', exact: true })
      await expect(options.getByRole('option')).toHaveCount(52)
      await expect(
        options.getByRole('option', { name: label(data.agent_id), exact: true }),
      ).toHaveCount(0)
      await page.keyboard.press('Escape')
      await expect(trigger).toBeFocused()
      const before = seen.requests('tasks').length
      const next = page.waitForResponse((response) => {
        const u = new URL(response.url())
        return (
          response.request().method() === 'GET' &&
          u.pathname === agentPath &&
          u.searchParams.get('cursor') === initial.next_cursor
        )
      })
      const [response] = await Promise.all([
        next,
        form.getByRole('button', { name: '加载更多筛选 Agent', exact: true }).click(),
      ])
      await seen.verify()
      expect(Object.fromEntries(new URL(response.url()).searchParams)).toEqual({
        limit: '50',
        cursor: initial.next_cursor,
      })
      expect(seen.taskSearchRead(response.request())).toEqual({ items: data.agents.slice(50) })
      expect(seen.requests('tasks')).toHaveLength(before)
      await expect(
        form.getByRole('button', { name: '加载更多筛选 Agent', exact: true }),
      ).toHaveCount(0)
      await trigger.focus()
      await page.keyboard.press('Enter')
      await expect(options.getByRole('option')).toHaveCount(53)
      await expect(
        options.getByRole('option', { name: label(data.agent_id), exact: true }),
      ).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(trigger).toBeFocused()
    }
    async function screenshot(name: string, dark: boolean) {
      await expect(page.locator('html')).toHaveAttribute('data-theme', dark ? 'dark' : 'light')
      await expect
        .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
        .toBe(true)
      expect(
        await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches),
      ).toBe(true)
      expect(await page.evaluate(() => matchMedia('(prefers-color-scheme: dark)').matches)).toBe(
        dark,
      )
      await expect(form).toBeVisible()
      mkdirSync(evidence, { recursive: true, mode: 0o700 })
      await page.screenshot({
        path: join(evidence, `task-assignee-filter-${mode}-${name}.png`),
        fullPage: true,
        animations: 'disabled',
      })
    }
    async function cleared() {
      await columns(all, () => clear.click())
      await expect(input).toHaveValue('')
      for (const [name, option] of [
        ['筛选 Agent', '全部 Agent'],
        ['筛选优先级', '全部优先级'],
        ['筛选类型', '全部类型'],
      ]) {
        await expect(form.getByRole('button', { name, exact: true })).toContainText(option!)
      }
      await expect(clear).toBeDisabled()
    }
    try {
      await columns(all, () => page.goto(data.route))
      await directory()
      if (mode === 'assignee-pagination') {
        await draft(label(data.agent_id), '高', '缺陷')
        const cursors = await columns(ownFilter, () => submit.click())
        const cursor = cursors.todo
        expect(cursor).toEqual(expect.any(String))
        await expect(target).toHaveCount(0)
        await draft(label(data.other_agent_id), '低', '功能')
        const next = page.waitForResponse((response) => {
          const u = new URL(response.url())
          return (
            response.request().method() === 'GET' &&
            u.pathname === taskPath &&
            u.searchParams.get('cursor') === cursor
          )
        })
        const more = page
          .getByRole('region', { name: '待执行', exact: true })
          .getByRole('button', { name: '更多待执行任务', exact: true })
        const [response] = await Promise.all([next, more.click()])
        await seen.verify()
        assertQuery(response, 'todo', ownFilter, cursor)
        const body = seen.taskSearchRead(response.request())
        expect(body).toEqual({ items: expected(data, 'todo', ownFilter).slice(50) })
        expect(body.items.map((row: Task) => row.id)).toEqual([data.target.id])
        observations.push({ state: 'todo', filter: ownFilter, cursor: true, ids: [data.target.id] })
        await expect(target).toBeVisible()
        await expect(more).toHaveCount(0)
        await screenshot('original-applied-cursor-desktop-light', false)
        await columns({ assignee: data.other_agent_id, priority: 'low', type: 'feature' }, () =>
          submit.click(),
        )
        await expect(target).toHaveCount(0)
        await expect(page.getByText('没有匹配的任务，可清除筛选。', { exact: false })).toBeVisible()
        await draft('未指派', '全部优先级', '全部类型')
        await columns({ assignee: 'null' }, () => submit.click())
        await draft(label(data.other_agent_id), '全部优先级', '全部类型')
        await columns({ assignee: data.other_agent_id }, () => submit.click())
        await cleared()
      } else {
        await draft(label(data.agent_id), '高', '缺陷')
        await input.fill(data.term)
        await columns(combination, () => input.press('Enter'))
        await expect(target).toBeVisible()
        await screenshot('combined-desktop-light', false)
        await draft(label(data.other_agent_id), '高', '缺陷')
        await columns({ ...combination, assignee: data.other_agent_id }, () => submit.click())
        await expect(target).toHaveCount(0)
        await draft('未指派', '高', '缺陷')
        await columns({ ...combination, assignee: 'null' }, () => input.press('Enter'))
        await expect(target).toHaveCount(0)
        await page.setViewportSize({ width: 390, height: 844 })
        await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
        await draft(label(data.agent_id), '高', '缺陷')
        await columns(combination, () => input.press('Enter'))
        const detail = page.waitForResponse(
          (response) =>
            response.request().method() === 'GET' &&
            new URL(response.url()).pathname === `${taskPath}/${data.target.id}`,
        )
        const [response] = await Promise.all([detail, columns(combination, () => target.click())])
        expect(seen.taskSearchRead(response.request())).toEqual(data.target)
        const drawer = page.getByRole('dialog', { name: '任务详情', exact: true })
        await expect(
          drawer.getByRole('heading', { name: data.target.title, exact: true }),
        ).toBeVisible()
        await expect(drawer.getByText(data.target.description, { exact: true })).toBeVisible()
        await expect(drawer.getByText(data.target.plan, { exact: true })).toBeVisible()
        await columns(combination, () =>
          drawer.getByRole('button', { name: '关闭任务详情', exact: true }).click(),
        )
        await expect(drawer).toHaveCount(0)
        await expect(input).toHaveValue(data.term)
        await expect(form.getByRole('button', { name: '筛选 Agent', exact: true })).toContainText(
          label(data.agent_id),
        )
        await expect(target).toBeVisible()
        await screenshot('retained-combination-narrow-dark', true)
        await cleared()
        await screenshot('cleared-narrow-dark', true)
      }
      expect(faults).toEqual([])
    } finally {
      try {
        await seen.retire()
      } finally {
        seen.save(testInfo.status)
        page.off('pageerror', onError)
        mkdirSync(evidence, { recursive: true, mode: 0o700 })
        writeFileSync(
          join(evidence, `task-assignee-filter-${mode}-observations.json`),
          JSON.stringify(
            {
              input_hash: process.env.AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_INPUT_HASH,
              observations,
              faults,
            },
            null,
            2,
          ) + '\n',
          { mode: 0o600 },
        )
      }
    }
  })
}
