const fs = require('node:fs'), path = require('node:path');
const root = process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if (require(path.join(root, 'package.json')).version !== '1.56.1') throw new Error('LOCKED_PLAYWRIGHT_REQUIRED');
const { chromium } = require(root);
const { expect } = require(path.join(root, 'test.js'));
if (typeof chromium.launch !== 'function' || typeof expect !== 'function') throw new Error('LOCKED_PLAYWRIGHT_API_REQUIRED');
expect({ lockedAssertions: true }).toEqual({ lockedAssertions: true });
if (process.argv.includes('--list')) {
  console.log('TestMeetingM3ReplacementOwnerWeb: failed A / original retry lookup / running retry / regenerate / historical replies / deferred Summary');
  process.exit(0);
}
const input = JSON.parse(fs.readFileSync('input.json', 'utf8'));
let stage = 'launch', step = 'launch', status = 0;
(async () => {
  let browser, page;
  try {
    browser = await chromium.launch({ executablePath: process.env.AGENTEAM_AUTH_WEB_CHROMIUM, args: ['--no-sandbox'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light' });
    await context.addCookies([{ name: 'agenteam_local_session', value: input.cookie, url: input.origin, httpOnly: true, sameSite: 'Strict' }]);
    page = await context.newPage(); page.setDefaultTimeout(8000);
    stage = 'open'; await page.goto(input.origin + input.route); await page.getByRole('heading', { name: '开始一场会议' }).waitFor();
    stage = 'choose'; await page.getByRole('button', { name: '编排 Agent', exact: true }).click();
    const roster = page.getByRole('dialog', { name: '编排参会 Agent' });
    await roster.getByRole('checkbox', { name: /meeting-agent-1/ }).check();
    await roster.getByRole('checkbox', { name: /meeting-agent-2/ }).check();
    await roster.getByRole('button', { name: '完成', exact: true }).click();
    await page.getByRole('button', { name: '发言模式', exact: true }).click();
    await page.getByRole('option', { name: '串行发言', exact: true }).click();
    await page.getByRole('textbox', { name: '会议首条消息', exact: true }).fill('Discuss the original plan; I may retry or revise one participant.');
    stage = 'start';
    const created = page.waitForResponse(r => r.request().method() === 'POST' && r.url() === input.origin + '/api/v1/projects/' + input.project_id + '/meetings');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    status = (await created).status(); if (status !== 200) throw new Error('START_REJECTED');
    const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    await page.waitForURL(url => url.origin === input.origin && url.pathname.startsWith(input.route + '/') && uuid.test(url.pathname.slice(input.route.length + 1)));
    const meeting = new URL(page.url()).pathname.slice(input.route.length + 1);
    const first = () => page.locator('.turn').first();
    const agent = () => first().locator('article.contribution.agent').filter({ has: page.getByText('meeting-agent-1', { exact: true }) });
    const currentReply = () => agent().locator('.message-body > .message-text');
    step = 'initial-partial-failure'; await agent().getByText('发言失败', { exact: true }).waitFor();
    await first().getByText('B kept the original contribution.', { exact: true }).waitFor();
    await page.getByRole('heading', { name: 'Replacement discussion', exact: true }).waitFor();
    await expect(first().locator('.turn-heading > span').nth(1)).toHaveText('已完成');
    await page.screenshot({ path: path.join(input.evidence, 'replacement-failed-desktop.png'), fullPage: true });
    stage = 'send'; step = 'retry-confirm';
    await agent().getByRole('button', { name: '重试此 Agent', exact: true }).click();
    const retry = page.getByRole('dialog', { name: '重试此 Agent？', exact: true });
    await expect(retry).toContainText('原输入重试');
    await retry.getByRole('button', { name: '确认重试', exact: true }).click();
    await page.getByRole('button', { name: '查询原操作', exact: true }).waitFor();
    await page.screenshot({ path: path.join(input.evidence, 'replacement-original-lookup-desktop.png'), fullPage: true });
    stage = 'lookup';
    const lookup = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/meetings/' + meeting + '/controls/lookup'));
    await page.getByRole('button', { name: '查询原操作', exact: true }).click();
    status = (await lookup).status(); if (status !== 200) throw new Error('RETRY_LOOKUP_REJECTED');
    step = 'retry-running'; await agent().getByText('正在发言', { exact: true }).waitFor();
    await expect(agent().getByRole('button', { name: '重试此 Agent', exact: true })).toHaveCount(0);
    await page.screenshot({ path: path.join(input.evidence, 'replacement-running-desktop.png'), fullPage: true });
    // This releases only the owned Provider's fourth response after the visible
    // running state. It is a test fixture action, never a product mutation.
    const releaseDeadline = performance.now() + 8000;
    let released = false;
    while (performance.now() < releaseDeadline) {
      const response = await page.request.post(input.origin + input.release_path, { timeout: Math.max(1, releaseDeadline - performance.now()) });
      if (response.status() === 204) { released = true; break; }
      // This owned fixture's 409 is returned before Release and has no effect.
      // A product response or an ambiguous release failure is never retried.
      if (response.status() !== 409) throw new Error('OWNED_PEER_RELEASE_REJECTED');
      await new Promise(resolve => setTimeout(resolve, Math.min(50, Math.max(0, releaseDeadline - performance.now()))));
    }
    if (!released) throw new Error('OWNED_RETRY_NOT_AT_PROVIDER');
    step = 'retry-published'; await expect(currentReply()).toHaveText('A retried the original contribution.');
    stage = 'send'; step = 'regenerate-confirm';
    await agent().getByRole('button', { name: '重新生成回复', exact: true }).click();
    const regenerate = page.getByRole('dialog', { name: '重新生成回复？', exact: true });
    await expect(regenerate).toContainText('旧版本仍可查看');
    await expect(regenerate).toContainText('下一轮正常讨论结束后更新');
    await regenerate.getByRole('button', { name: '确认重新生成', exact: true }).click();
    step = 'regenerate-published'; await expect(currentReply()).toHaveText('A regenerated with the current discussion.');
    await agent().locator('details.history > summary').click();
    await expect(agent().locator('details.history')).toContainText('第 1 版 · 第 1 次尝试 · 失败');
    await expect(agent().locator('details.history')).toContainText('第 1 版 · 第 2 次尝试 · 已完成');
    await expect(agent().locator('details.history')).toContainText('A retried the original contribution.');
    await expect(first().getByText('B kept the original contribution.', { exact: true })).toHaveCount(1);
    step = 'summary-pending';
    const info = page.locator('aside.meeting-details').getByRole('region', { name: '会议摘要', exact: true });
    await expect(info).toBeVisible();
    await expect(info).toContainText('摘要待更新，以下内容尚未覆盖最新变化。');
    await expect(info).toContainText('Original partial discussion');
    stage = 'reload'; await page.reload(); await expect(currentReply()).toHaveText('A regenerated with the current discussion.');
    await agent().locator('details.history > summary').click();
    await expect(agent().locator('details.history')).toContainText('A retried the original contribution.');
    stage = 'send'; step = 'normal-next-turn';
    await page.getByRole('textbox', { name: '发送到会议', exact: true }).fill('Now discuss the revised plan in the next normal turn.');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await expect(page.locator('.turn')).toHaveCount(2);
    await page.getByText('B completed the next normal turn.', { exact: true }).waitFor();
    await expect(page.locator('.turn').last().locator('.turn-heading > span').nth(1)).toHaveText('已完成');
    await expect(info).toBeVisible();
    await expect(info).toContainText('Summary includes the replacement and next turn');
    await expect(info).not.toContainText('摘要待更新');
    stage = 'layout'; await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' }); await page.setViewportSize({ width: 390, height: 844 });
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('PAGE_OVERFLOW');
    await page.screenshot({ path: path.join(input.evidence, 'replacement-history-mobile.png'), fullPage: true });
    stage = 'close'; await context.close(); await browser.close(); browser = null;
    fs.writeFileSync('result.json', JSON.stringify({ completed: true, meeting }), { mode: 0o600 });
    console.log('MEETING_BROWSER_COMPLETE');
  } catch (error) {
    const message = String(error && error.message || '');
    const kind = error && error.name === 'TimeoutError' ? 'TIMEOUT' : /^[A-Z_]{1,64}$/.test(message) ? message : 'UNCLASSIFIED';
    fs.writeFileSync('failure.json', JSON.stringify({ stage, step, status, error: kind }), { mode: 0o600 });
    if (page) await page.screenshot({ path: path.join(input.evidence, 'replacement-failure.png'), fullPage: true }).catch(() => {});
    if (browser) await browser.close().catch(() => {});
    process.exitCode = 1;
  }
})();
