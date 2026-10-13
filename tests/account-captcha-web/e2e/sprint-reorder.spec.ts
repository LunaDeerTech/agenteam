import { test, expect, type Page } from '@playwright/test'
import { observeTaskRequests, type TaskObservationProfile } from './task-review.helpers'
import {
  sprintReorderMaterial,
  sprintReorderProfile,
  observeSprintReorder,
  sprintReorderScreenshot,
  type SprintReorderMaterial,
} from './sprint-reorder.helpers'

async function press(page: Page, name: string) {
  const button = page.getByRole('button', { name, exact: true })
  await expect(button).toBeEnabled()
  await button.focus()
  await expect(button).toBeFocused()
  await page.keyboard.press('Enter')
}
async function choose(page: Page, label: string, value: string) {
  await press(page, label)
  const option = page
    .getByRole('listbox', { name: label, exact: true })
    .getByRole('option', { name: value, exact: true })
  await option.focus()
  await page.keyboard.press('Enter')
}
async function open(page: Page) {
  await press(page, '调整 Sprint 顺序')
  const form = page.getByRole('form', { name: '调整 Sprint 顺序', exact: true })
  await expect(form).toBeVisible()
  await expect(form.getByRole('heading', { name: '调整 Sprint 顺序', exact: true })).toBeFocused()
  return form
}
async function controls(data: SprintReorderMaterial) {
  const page = {
    on() {
      throw Error('CONTROL_REACHED_NATIVE_SETUP')
    },
  } as unknown as Page
  const profile: TaskObservationProfile = { ...sprintReorderProfile(), endpoint: () => undefined }
  for (const change of [
    { sprintReorderWrites: undefined },
    { prefix: 'sprint-edit' },
    { lookupCommand: 'work.sprint.update' },
    { cut: 'update' },
    { milestoneReorderWrites: true },
    { sprintCreateWrites: true },
    { variableWrites: true },
  ]) {
    await expect(
      observeTaskRequests(page, data, { ...profile, ...change } as TaskObservationProfile),
    ).rejects.toThrow(
      'milestoneReorderWrites' in change
        ? 'MILESTONE_REORDER_PROFILE_MISMATCH'
        : 'SPRINT_REORDER_PROFILE_MISMATCH',
    )
  }
  await expect(observeTaskRequests(page, { ...data, milestone_id: '' }, profile)).rejects.toThrow(
    'SPRINT_REORDER_PROFILE_MISMATCH',
  )
}
const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSprintReorder>>>()
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page)
  try {
    await seen?.retire()
  } finally {
    seen?.save(info.status)
  }
})
for (const mode of ['sprint-reorder-planned', 'sprint-reorder-current'] as const) {
  test(`reorders ${mode === 'sprint-reorder-planned' ? 'planned at 1440 light' : 'current at 390 dark'} with second-page anchor, tail and no-op [${mode}]`, async ({
    page,
  }) => {
    const data = sprintReorderMaterial()
    expect(data.mode).toBe(mode)
    await controls(data)
    const narrow = mode === 'sprint-reorder-current'
    await page.setViewportSize({ width: narrow ? 390 : 1440, height: narrow ? 844 : 960 })
    await page.emulateMedia({ colorScheme: narrow ? 'dark' : 'light', reducedMotion: 'reduce' })
    await page
      .context()
      .addCookies([
        {
          name: 'agenteam_local_session',
          value: data.cookie,
          url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
          httpOnly: true,
          sameSite: 'Lax',
          secure: false,
        },
      ])
    const seen = await observeSprintReorder(page, data)
    observations.set(page, seen)
    await page.goto(data.route)
    await expect(page.getByRole('button', { name: '调整 Sprint 顺序', exact: true })).toBeEnabled()
    await expect(page.locator('html')).toHaveAttribute('data-theme', narrow ? 'dark' : 'light')
    await seen.verify()
    seen.current(data.sprint, -1)
    let form = await open(page)
    await press(page, '取消调整顺序')
    await expect(form).toHaveCount(0)
    await expect(page.getByRole('button', { name: '调整 Sprint 顺序', exact: true })).toBeFocused()
    expect(seen.requests('reorder')).toHaveLength(0)
    form = await open(page)
    await press(page, '目标 Sprint')
    let list = page.getByRole('listbox', { name: '目标 Sprint', exact: true })
    await expect(list.getByRole('option')).toHaveCount(49)
    await expect(list.getByRole('option')).toHaveText(data.rows.slice(1, 50).map((s) => s.title))
    await page.keyboard.press('Escape')
    await expect(form.getByRole('button', { name: '目标 Sprint', exact: true })).toBeFocused()
    await choose(page, '目标 Sprint', data.rows[1]!.title)
    await expect(form.getByRole('button', { name: '确认调整顺序', exact: true })).toBeEnabled()
    await press(page, '下一页候选')
    await expect(form.getByText('第 2 页', { exact: true })).toBeVisible()
    await expect(form.getByRole('button', { name: '确认调整顺序', exact: true })).toBeDisabled()
    await press(page, '目标 Sprint')
    list = page.getByRole('listbox', { name: '目标 Sprint', exact: true })
    await expect(list.getByRole('option')).toHaveCount(4)
    await expect(list.getByRole('option')).toHaveText(data.rows.slice(50).map((s) => s.title))
    await page.keyboard.press('Escape')
    await press(page, '上一页候选')
    await expect(form.getByText('第 1 页', { exact: true })).toBeVisible()
    await press(page, '下一页候选')
    await expect(form.getByText('第 2 页', { exact: true })).toBeVisible()
    await choose(page, '目标 Sprint', data.rows[53]!.title)
    await seen.verify()
    expect(seen.requests('reorder')).toHaveLength(0)
    await sprintReorderScreenshot(page, data, 'before')
    let current = data.sprint
    for (let index = 0; index < 3; index++) {
      if (index) {
        form = await open(page)
        await choose(page, '排序位置', '移到组尾')
      }
      const gets = seen.requests('sprint').length,
        lists = seen.requests('sprints').length
      await press(page, '确认调整顺序')
      await expect(form).toHaveCount(0)
      const heading = page.locator('.sprint-heading h2')
      await expect(heading).toHaveText(data.sprint.title)
      await expect(heading).toBeFocused()
      await expect(
        page.getByText(
          index < 2
            ? '顺序已确认，当前 Sprint 和顺序已重新读取。'
            : '顺序已确认，原操作无需调整；当前 Sprint 和顺序已重新读取。',
          { exact: true },
        ),
      ).toBeVisible()
      await seen.verify()
      expect(seen.requests('sprint')).toHaveLength(gets + 1)
      expect(seen.requests('sprints')).toHaveLength(lists + 1)
      current = seen.result(index, current)
      seen.current(current, index)
    }
    expect(current.version).toBe(String(BigInt(data.sprint.version) + 2n))
    expect(seen.requests('reorder')).toHaveLength(3)
    expect(seen.requests('lookup')).toHaveLength(0)
    await sprintReorderScreenshot(page, data, 'confirmed-noop')
    const gets = seen.requests('sprint').length,
      lists = seen.requests('sprints').length
    await press(page, '刷新任务看板')
    await expect(page.getByRole('button', { name: '调整 Sprint 顺序', exact: true })).toBeEnabled()
    await seen.verify()
    expect(seen.requests('sprint')).toHaveLength(gets + 1)
    expect(seen.requests('sprints')).toHaveLength(lists + 1)
    seen.current(current, 2)
  })
}
