const fs = require('node:fs'), path = require('node:path');
const root = process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if (require(path.join(root, 'package.json')).version !== '1.56.1') throw new Error('LOCKED_PLAYWRIGHT_REQUIRED');
const { chromium } = require(root);
const { expect } = require(path.join(root, 'test.js'));
if (typeof chromium.launch !== 'function' || typeof expect !== 'function') throw new Error('LOCKED_PLAYWRIGHT_API_REQUIRED');
expect({ lockedAssertions: true }).toEqual({ lockedAssertions: true });
const turnStates = new Set(['queued', 'running', 'finalizing', 'completed', 'cancelled']);
const turnLabels = { '排队中': 'queued', '正在发言': 'running', '进行中': 'running', '正在整理摘要': 'finalizing', '已完成': 'completed', '已取消': 'cancelled' };
async function waitForCompletedTurn(page, n, observation, budget = 8000) {
  const deadline = observation.started + budget;
  let matcher = 'count';
  try {
    await expect(page.locator('.turn')).toHaveCount(n + 1, { timeout: Math.max(1, deadline - performance.now()) });
    matcher = 'status';
    await expect(page.locator('.turn').nth(n).locator('.turn-heading > span').nth(1)).toHaveText('已完成', { timeout: Math.max(1, deadline - performance.now()) });
  } catch (error) {
    const result = error && error.matcherResult;
    const actual = matcher === 'count'
      ? Number.isSafeInteger(result && result.actual) && result.actual >= 0 ? result.actual : null
      : typeof (result && result.actual) === 'string' ? turnLabels[result.actual.trim()] || 'unobserved' : 'unobserved';
    const labels = await page.locator('.turn').nth(n).locator('.turn-heading > span').allTextContents().catch(() => []);
    observation.failure = {
      turn_index: n, elapsed_ms: Math.round(performance.now() - observation.started), matcher,
      matcher_actual: actual, matcher_failed: result && result.pass === false,
      state: labels.map(x => turnLabels[x.trim()]).find(Boolean) || 'unobserved',
      reads: observation.reads,
    };
    throw new Error('TURN_NOT_COMPLETE');
  }
}
module.exports = { waitForCompletedTurn };
if (require.main === module) {
if (process.argv.includes('--list')) {
  console.log('TestMeetingM3MetadataOwnerWeb: remove / original lookup / Link and typed Owner references / rejoin and reorder / immutable historical turns');
  process.exit(0);
}
const input = JSON.parse(fs.readFileSync('input.json', 'utf8'));
let stage = 'launch', step = 'launch', status = 0, turnObservation = null;
(async () => {
  let browser, page;
  let observedMeeting = '', observedTurn = null;
  try {
    browser = await chromium.launch({ executablePath: process.env.AGENTEAM_AUTH_WEB_CHROMIUM, args: ['--no-sandbox'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light' });
    await context.addCookies([{ name: 'agenteam_local_session', value: input.cookie, url: input.origin, httpOnly: true, sameSite: 'Strict' }]);
    page = await context.newPage(); page.setDefaultTimeout(8000);
    // Observe only the page's existing exact Meeting GETs. No extra read,
    // mutation, body/header log or polling owner is introduced by diagnostics.
    page.on('response', response => {
      const observation = observedTurn;
      if (!observation || !observedMeeting || response.request().method() !== 'GET' || response.url() !== input.origin + '/api/v1/projects/' + input.project_id + '/meetings/' + observedMeeting || observation.reads.length >= 16) return;
      const entry = { response_ms: Math.round(performance.now() - observation.started), status: response.status(), identity: 'unchecked', state: 'unobserved' };
      observation.reads.push(entry);
      void (async () => {
        try {
          const bytes = await response.body();
          if (bytes.length > 1048576) { entry.identity = 'oversized'; return; }
          const value = JSON.parse(bytes.toString('utf8'));
          if (value?.session?.id !== observedMeeting || value.session.project_id !== input.project_id || !Array.isArray(value.turns)) { entry.identity = 'mismatch'; return; }
          entry.identity = 'matched';
          const turn = value.turns[observation.index];
          entry.state = turnStates.has(turn?.status) ? turn.status : 'unobserved';
          entry.summary_version = typeof value.session.summary?.version === 'string' && /^[1-9][0-9]{0,17}$/.test(value.session.summary.version) ? value.session.summary.version : null;
        } catch { entry.identity = 'unavailable'; }
        finally { entry.observed_ms = Math.round(performance.now() - observation.started); }
      })();
    });
    stage = 'open'; await page.goto(input.origin + input.route);
    await page.getByRole('heading', { name: '开始一场会议' }).waitFor();
    stage = 'choose'; await page.getByRole('button', { name: '编排 Agent', exact: true }).click();
    const initialRoster = page.getByRole('dialog', { name: '编排参会 Agent' });
    await initialRoster.getByRole('checkbox', { name: /meeting-agent-1/ }).check();
    await initialRoster.getByRole('checkbox', { name: /meeting-agent-2/ }).check();
    await initialRoster.getByRole('button', { name: '完成', exact: true }).click();
    await page.getByRole('button', { name: '发言模式', exact: true }).click();
    await page.getByRole('option', { name: '串行发言', exact: true }).click();
    await page.getByRole('textbox', { name: '会议首条消息', exact: true }).fill('Discuss the original plan with both participants.');
    stage = 'start';
    const created = page.waitForResponse(r => r.request().method() === 'POST' && r.url() === input.origin + '/api/v1/projects/' + input.project_id + '/meetings');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    status = (await created).status(); if (status !== 200) throw new Error('START_REJECTED');
    const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    await page.waitForURL(url => url.origin === input.origin && url.pathname.startsWith(input.route + '/') && uuid.test(url.pathname.slice(input.route.length + 1)));
    const meeting = new URL(page.url()).pathname.slice(input.route.length + 1);
    observedMeeting = meeting;
    const turn = n => page.locator('.turn').nth(n);
    const done = async n => {
      const observation = { started: performance.now(), index: n, reads: [] };
      observedTurn = observation;
      try {
        await waitForCompletedTurn(page, n, observation);
      } catch (error) { turnObservation = observation.failure; throw error; }
      finally { observedTurn = null; }
    };
    step = 'first-turn'; await done(0);
    await page.getByRole('heading', { name: 'Metadata discussion', exact: true }).waitFor();
    await expect(turn(0).locator('article.contribution.agent')).toHaveCount(2);
    await expect(turn(0)).toContainText('B joined the original discussion.');
    stage = 'send'; step = 'remove-b';
    await page.getByRole('button', { name: '编辑成员', exact: true }).click();
    const roster = page.getByRole('dialog', { name: '编辑参会成员', exact: true });
    await expect(roster).toContainText('移除成员会保留其历史发言');
    await roster.getByRole('checkbox', { name: /meeting-agent-2/ }).uncheck();
    await roster.getByRole('button', { name: '保存修改', exact: true }).click();
    await roster.getByRole('button', { name: '核对原操作', exact: true }).waitFor();
    stage = 'lookup';
    const looked = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/meetings/' + meeting + '/participants/lookup'));
    await roster.getByRole('button', { name: '核对原操作', exact: true }).click();
    status = (await looked).status(); if (status !== 200) throw new Error('ROSTER_LOOKUP_REJECTED');
    await expect(roster).toHaveCount(0);
    await expect(turn(0)).toContainText('B joined the original discussion.');
    const references = page.getByRole('dialog', { name: '管理会议引用', exact: true });
    const saveReferences = async () => {
      const saved = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/meetings/' + meeting + '/references'));
      await references.getByRole('button', { name: '保存修改', exact: true }).click();
      status = (await saved).status(); if (status !== 200) throw new Error('REFERENCE_SAVE_REJECTED');
      await expect(references).toHaveCount(0);
    };
    stage = 'send'; step = 'pin-links';
    await page.getByRole('button', { name: '管理引用', exact: true }).click();
    await expect(references).toContainText('固定引用不会自动读取链接内容');
    for (const [url, label] of [['HTTPS://Example.COM:443/design', 'Design link'], ['https://example.com/notes', 'Notes link']]) {
      await references.getByRole('textbox', { name: '链接', exact: true }).fill(url);
      await references.getByRole('textbox', { name: '显示名称（可选）', exact: true }).fill(label);
      await references.getByRole('button', { name: '加入引用列表', exact: true }).click();
    }
    step = 'pick-task';
    const picker = references.getByRole('region', { name: '添加项目资源引用', exact: true });
    await picker.getByRole('button', { name: 'Task', exact: true }).click();
    await picker.getByRole('button', { name: '打开 Meeting reference milestone', exact: true }).click();
    await picker.getByRole('button', { name: '打开 Meeting reference sprint', exact: true }).click();
    await picker.getByRole('button', { name: '筛选 Task 状态', exact: true }).click();
    await page.getByRole('option', { name: '待规划', exact: true }).click();
    await picker.getByRole('button', { name: '引用 Meeting referenced Task', exact: true }).click();
    await expect(picker.getByRole('button', { name: '已选择 Meeting referenced Task', exact: true })).toBeDisabled();
    step = 'pick-document';
    await picker.getByRole('button', { name: 'Knowledge', exact: true }).click();
    await picker.getByRole('button', { name: '引用 Meeting referenced Document', exact: true }).click();
    step = 'pick-artifact';
    await picker.getByRole('button', { name: '文件', exact: true }).click();
    await picker.getByRole('button', { name: '引用 meeting-referenced-file.txt', exact: true }).click();
    await expect(references).not.toContainText('MEETING_REFERENCE_PRIVATE_BODY');
    await page.screenshot({ path: path.join(input.evidence, 'metadata-resource-picker-desktop.png'), fullPage: true });
    await saveReferences();
    step = 'single-agent-turn';
    await page.getByRole('textbox', { name: '发送到会议', exact: true }).fill('Continue with the current participant and pinned reference identities.');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await done(1);
    await expect(turn(1).locator('article.contribution.agent')).toHaveCount(1);
    await expect(turn(1)).toContainText('A continued with the pinned references.');
    await expect(turn(1)).not.toContainText('meeting-agent-2');
    step = 'rejoin-and-reorder'; await page.getByRole('button', { name: '编辑成员', exact: true }).click();
    await roster.getByRole('checkbox', { name: /meeting-agent-2/ }).check();
    await roster.getByRole('button', { name: '上移 meeting-agent-2', exact: true }).click();
    const savedRoster = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/meetings/' + meeting + '/participants'));
    await roster.getByRole('button', { name: '保存修改', exact: true }).click();
    status = (await savedRoster).status(); if (status !== 200) throw new Error('REJOIN_REJECTED');
    await expect(roster).toHaveCount(0);
    step = 'unpin-one'; await page.getByRole('button', { name: '管理引用', exact: true }).click();
    await references.getByRole('button', { name: '取消固定 Design link', exact: true }).click();
    await saveReferences();
    stage = 'reload'; await page.reload(); await page.getByRole('button', { name: '编辑成员', exact: true }).click();
    await expect(roster.locator('.ordered-items li > span')).toHaveText(['meeting-agent-2', 'meeting-agent-1']);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: '编辑成员', exact: true })).toBeFocused();
    step = 'reload-identities';
    await page.getByRole('button', { name: '管理引用', exact: true }).click();
    for (const label of ['Notes link', 'Meeting referenced Task', 'Meeting referenced Document', 'meeting-referenced-file.txt']) {
      await expect(references.getByRole('button', { name: '取消固定 ' + label, exact: true })).toBeVisible();
    }
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: '管理引用', exact: true })).toBeFocused();
    stage = 'send'; step = 'reordered-turn';
    await page.getByRole('textbox', { name: '发送到会议', exact: true }).fill('Continue in the revised participant order with the remaining link identity.');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await done(2);
    await expect(turn(2).locator('article.contribution.agent')).toHaveCount(2);
    await expect(turn(2).locator('article.contribution.agent').nth(0)).toContainText('B rejoined before A.');
    await expect(turn(2).locator('article.contribution.agent').nth(1)).toContainText('A followed the revised order.');
    await expect(turn(0).locator('article.contribution.agent').nth(0)).toContainText('A joined the original discussion.');
    await expect(turn(0).locator('article.contribution.agent').nth(1)).toContainText('B joined the original discussion.');
    await page.screenshot({ path: path.join(input.evidence, 'metadata-history-desktop.png'), fullPage: true });
    step = 'clear-references'; await page.getByRole('button', { name: '管理引用', exact: true }).click();
    for (const label of ['Notes link', 'Meeting referenced Task', 'Meeting referenced Document', 'meeting-referenced-file.txt']) {
      await references.getByRole('button', { name: '取消固定 ' + label, exact: true }).click();
    }
    await saveReferences();
    stage = 'layout'; await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' }); await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: '管理引用', exact: true }).click();
    await expect(references).toContainText('暂无固定引用');
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('PAGE_OVERFLOW');
    await page.screenshot({ path: path.join(input.evidence, 'metadata-empty-mobile.png'), fullPage: true });
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: '管理引用', exact: true })).toBeFocused();
    stage = 'close'; await context.close(); await browser.close(); browser = null;
    fs.writeFileSync('result.json', JSON.stringify({ completed: true, meeting }), { mode: 0o600 });
    console.log('MEETING_BROWSER_COMPLETE');
  } catch (error) {
    const message = String(error && error.message || '');
    const kind = error && error.name === 'TimeoutError' ? 'TIMEOUT' : /^[A-Z_]{1,64}$/.test(message) ? message : 'UNCLASSIFIED';
    fs.writeFileSync('failure.json', JSON.stringify({ stage, step, status, error: kind }), { mode: 0o600 });
    if (turnObservation) fs.writeFileSync(path.join(input.evidence, 'metadata-turn-observation.json'), JSON.stringify(turnObservation), { mode: 0o600 });
    if (page) await page.screenshot({ path: path.join(input.evidence, 'metadata-failure.png'), fullPage: true }).catch(() => {});
    if (browser) await browser.close().catch(() => {});
    process.exitCode = 1;
  }
})();
}
