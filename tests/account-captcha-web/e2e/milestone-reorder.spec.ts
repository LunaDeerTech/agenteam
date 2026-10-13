import { test, expect, type Page } from '@playwright/test'
import { observeTaskRequests, type TaskObservationProfile } from './task-review.helpers'
import {
  milestoneReorderMaterial,
  milestoneReorderProfile,
  observeMilestoneReorder,
  milestoneReorderScreenshot,
  type MilestoneReorderMaterial,
} from './milestone-reorder.helpers'

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
async function begin(page: Page, data: MilestoneReorderMaterial) {
  await page.setViewportSize({ width: 1440, height: 960 })
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' })
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
  const seen = await observeMilestoneReorder(page, data)
  return {
    seen,
    async enter() {
      await page.goto(data.route)
      await choose(page, 'Milestone', data.milestone.title)
      await expect(
        page.getByRole('button', { name: '调整 Milestone 顺序', exact: true }),
      ).toBeEnabled()
      await seen.verify()
      seen.current(data.milestone, -1)
    },
  }
}
async function open(page: Page, confirmed = false) {
  await press(page, confirmed ? '顺序已确认' : '调整 Milestone 顺序')
  const form = page.getByRole('form', { name: '调整 Milestone 顺序', exact: true })
  await expect(form).toBeVisible()
  await expect(
    form.getByRole('heading', { name: '调整 Milestone 顺序', exact: true }),
  ).toBeFocused()
  return form
}
async function controls(data: MilestoneReorderMaterial) {
  const page = {
    on() {
      throw Error('CONTROL_REACHED_NATIVE_SETUP')
    },
  } as unknown as Page
  const profile: TaskObservationProfile = {
    ...milestoneReorderProfile(),
    endpoint: () => undefined,
  }
  for (const change of [
    { milestoneReorderWrites: undefined },
    { prefix: 'milestone-edit' },
    { lookupCommand: 'work.milestone.update' },
    { cut: 'update' },
    { milestoneUpdateWrites: true },
    { sprintCreateWrites: true },
    { variableWrites: true },
  ]) {
    await expect(
      observeTaskRequests(page, data, { ...profile, ...change } as TaskObservationProfile),
    ).rejects.toThrow('MILESTONE_REORDER_PROFILE_MISMATCH')
  }
}
const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeMilestoneReorder>>>()
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page)
  try {
    await seen?.retire()
  } finally {
    seen?.save(info.status)
  }
})

test('selects a real second-page anchor, moves to tail and preserves a no-op [milestone-reorder-normal]', async ({
  page,
}) => {
  const data = milestoneReorderMaterial()
  await controls(data)
  const started = await begin(page, data),
    seen = started.seen
  observations.set(page, seen)
  await started.enter()
  let form = await open(page)
  await press(page, '取消调整顺序')
  await expect(form).toHaveCount(0)
  await expect(page.getByRole('button', { name: '调整 Milestone 顺序', exact: true })).toBeFocused()
  expect(seen.requests('reorder')).toHaveLength(0)
  form = await open(page)
  await press(page, '目标 Milestone')
  let list = page.getByRole('listbox', { name: '目标 Milestone', exact: true })
  await expect(list.getByRole('option')).toHaveCount(49)
  await expect(list.getByRole('option')).toHaveText(data.rows.slice(1, 50).map((m) => m.title))
  await page.keyboard.press('Escape')
  await choose(page, '目标 Milestone', data.rows[1]!.title)
  await expect(form.getByRole('button', { name: '确认调整顺序', exact: true })).toBeEnabled()
  await press(page, '下一页候选')
  await expect(form.getByText('第 2 页', { exact: true })).toBeVisible()
  await expect(form.getByRole('button', { name: '确认调整顺序', exact: true })).toBeDisabled()
  await press(page, '目标 Milestone')
  list = page.getByRole('listbox', { name: '目标 Milestone', exact: true })
  await expect(list.getByRole('option')).toHaveCount(4)
  await expect(list.getByRole('option')).toHaveText(data.rows.slice(50).map((m) => m.title))
  await page.keyboard.press('Escape')
  await press(page, '上一页候选')
  await expect(form.getByText('第 1 页', { exact: true })).toBeVisible()
  await press(page, '下一页候选')
  await expect(form.getByText('第 2 页', { exact: true })).toBeVisible()
  await choose(page, '目标 Milestone', data.rows[53]!.title)
  await seen.verify()
  expect(seen.requests('reorder')).toHaveLength(0)
  await milestoneReorderScreenshot(page, data, '1440-light-before')
  let current = data.milestone
  for (let index = 0; index < 3; index++) {
    if (index) {
      form = await open(page, true)
      await choose(page, '排序位置', '移到组尾')
    }
    const gets = seen.requests('milestone').length,
      lists = seen.requests('milestones').length
    await press(page, '确认调整顺序')
    await expect(form).toHaveCount(0)
    const heading = page.locator('.selected-milestone h2')
    await expect(heading).toHaveText(data.milestone.title)
    await expect(heading).toBeFocused()
    await expect(
      page.getByText(
        index < 2
          ? '顺序已确认，当前 Milestone 和顺序已重新读取。'
          : '顺序已确认，原操作无需调整；当前 Milestone 和顺序已重新读取。',
        { exact: true },
      ),
    ).toBeVisible()
    await seen.verify()
    expect(seen.requests('milestone')).toHaveLength(gets + 1)
    expect(seen.requests('milestones')).toHaveLength(lists + 1)
    current = seen.result(index, current)
    seen.current(current, index)
  }
  expect(current.version).toBe('3')
  expect(seen.requests('reorder')).toHaveLength(3)
  expect(seen.requests('lookup')).toHaveLength(0)
  const gets = seen.requests('milestone').length,
    lists = seen.requests('milestones').length
  await press(page, '刷新任务看板')
  await expect(page.getByRole('button', { name: '顺序已确认', exact: true })).toBeEnabled()
  await seen.verify()
  expect(seen.requests('milestone')).toHaveLength(gets + 1)
  expect(seen.requests('milestones')).toHaveLength(lists + 1)
  seen.current(current, 2)
})

test('keeps real candidate controls usable at four widths in both themes [milestone-reorder-responsive]', async ({
  page,
}) => {
  const data = milestoneReorderMaterial(),
    started = await begin(page, data),
    seen = started.seen
  observations.set(page, seen)
  await started.enter()
  const form = await open(page)
  for (const width of [1440, 1024, 834, 390])
    for (const theme of ['light', 'dark'] as const) {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 960 })
      await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await expect(form.getByRole('button', { name: '目标 Milestone', exact: true })).toBeEnabled()
      await expect(form.getByRole('button', { name: '下一页候选', exact: true })).toBeEnabled()
      await expect(form.getByRole('button', { name: '确认调整顺序', exact: true })).toBeDisabled()
      await press(page, '目标 Milestone')
      await expect(
        page.getByRole('listbox', { name: '目标 Milestone', exact: true }).getByRole('option'),
      ).toHaveCount(49)
      await page.keyboard.press('Escape')
      await expect(form.getByRole('button', { name: '目标 Milestone', exact: true })).toBeFocused()
      await milestoneReorderScreenshot(page, data, `${width}-${theme}-candidates`)
    }
  await press(page, '取消调整顺序')
  await expect(form).toHaveCount(0)
  await expect(page.getByRole('button', { name: '调整 Milestone 顺序', exact: true })).toBeFocused()
  await seen.verify()
  expect(seen.requests('reorder')).toHaveLength(0)
  expect(seen.requests('lookup')).toHaveLength(0)
  seen.current(data.milestone, -1)
})
