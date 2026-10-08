const fs = require('node:fs');
const assert = require('node:assert/strict');
const { chromium } = require('/workspace/agenteam/tests/account-captcha-web/node_modules/playwright');
const result = { scope: 'synthetic local label semantics only; not edit01 DOM', requests: [], controls: [], browserClosed: false };
let browser;
(async () => {
  try {
    browser = await chromium.launch({ executablePath: '/usr/bin/chromium', headless: true, timeout: 5000, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
    result.browserVersion = browser.version();
    const context = await browser.newContext({ serviceWorkers: 'block' });
    await context.route('**/*', async route => { result.requests.push(route.request().url()); await route.abort(); });
    const page = await context.newPage();
    page.setDefaultTimeout(1000);
    await page.setContent('<form aria-label="项目基本信息"><label for="project-name">项目名称<span aria-hidden="true"> *</span></label><input id="project-name" name="name" value="owner-main"><label for="project-description">项目描述</label><textarea id="project-description" name="description"></textarea></form>');
    async function counts(stage) {
      const row = { stage,
        exactLabel: await page.getByLabel('项目名称', { exact: true }).count(),
        exactStarLabel: await page.getByLabel('项目名称 *', { exact: true }).count(),
        exactRole: await page.getByRole('textbox', { name: '项目名称', exact: true }).count(),
        exactStarRole: await page.getByRole('textbox', { name: '项目名称 *', exact: true }).count(),
        descriptionLabel: await page.getByLabel('项目描述', { exact: true }).count(),
        labelText: await page.locator('label[for="project-name"]').textContent(),
      };
      result.controls.push(row); return row;
    }
    const original = await counts('required aria-hidden star');
    assert.deepEqual([original.exactLabel, original.exactStarLabel, original.exactRole, original.exactStarRole, original.descriptionLabel], [0, 1, 1, 0, 1]);
    try { await page.getByLabel('项目名称', { exact: true }).fill('unmatched marker', { timeout: 150 }); throw new Error('exact label unexpectedly matched'); }
    catch (error) { assert.equal(error.name, 'TimeoutError'); result.originalExactFill = { expectedTimeout: true, timeoutMs: 150 }; }
    await page.getByRole('textbox', { name: '项目名称', exact: true }).fill('semantic marker');
    assert.equal(await page.locator('#project-name').inputValue(), 'semantic marker');
    result.roleFillSucceeded = true;
    await page.locator('label[for="project-name"] span').evaluate(span => span.removeAttribute('aria-hidden'));
    const exposed = await counts('visible-to-accessibility star');
    assert.deepEqual([exposed.exactLabel, exposed.exactStarLabel, exposed.exactRole, exposed.exactStarRole], [0, 1, 0, 1]);
    await page.locator('label[for="project-name"] span').evaluate(span => span.remove());
    const noStar = await counts('no star');
    assert.deepEqual([noStar.exactLabel, noStar.exactStarLabel, noStar.exactRole, noStar.exactStarRole, noStar.descriptionLabel], [1, 0, 1, 0, 1]);
    assert.equal(page.url(), 'about:blank'); assert.equal(result.requests.length, 0);
    result.status = 'PASS';
    await context.close();
  } catch (error) { result.status = 'FAIL'; result.error = { name: error.name, message: error.message }; process.exitCode = 1; }
  finally {
    if (browser) { await browser.close(); result.browserClosed = true; }
    fs.writeFileSync(__dirname + '/result.json', JSON.stringify(result, null, 2) + '\n');
    process.stdout.write(JSON.stringify({ status: result.status, browserClosed: result.browserClosed, controls: result.controls.length, requests: result.requests.length }) + '\n');
  }
})();
