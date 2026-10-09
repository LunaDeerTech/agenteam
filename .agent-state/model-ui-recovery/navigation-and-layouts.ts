import { expect, type Locator, type Page, type Request } from '../../tests/account-captcha-web/node_modules/@playwright/test/index.js';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { isAbsolute, join } from 'node:path';
import { beginSessionDiagnostic, type AuthorityHarness } from './authority-and-identity';

type NativeFact = Readonly<{ token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; released: boolean; bytes: number }>;
type Project = Readonly<{ id: string; username: string; normalized_name: string; owner_user_id: string; initialized: boolean; lifecycle: string }>;
export type NavigationHarness = Readonly<Pick<AuthorityHarness, 'material' | 'loginOwner' | 'navigate' | 'fillCredential' | 'snapshot' | 'strictReceipt' | 'durableDelta' | 'originalReplay' | 'arm' | 'control' | 'actualLoss' | 'counts' | 'nativeFacts' | 'step'> & {
  openProject(page: Page, project: 'main' | 'second', leaf: 'model-providers' | 'available-models', discardPrepared?: boolean): Promise<void>;
  finish(page: Page, checks: Record<string, boolean>, layouts: number): Promise<void>;
}>;
const button = (scope: Page | Locator, name: string) => scope.getByRole('button', { name, exact: true });
function need(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function object(value: unknown): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value), 'PROJECT_MODELS_NAVIGATION_OBJECT_INVALID');
  return value as Record<string, unknown>;
}
function uuid(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value); }
function locator(value: unknown): Project {
  const row = object(value);
  need(uuid(row.id) && uuid(row.owner_user_id) && typeof row.username === 'string' && typeof row.normalized_name === 'string' && typeof row.initialized === 'boolean' && typeof row.lifecycle === 'string', 'PROJECT_MODELS_NAVIGATION_PROJECT_INVALID');
  return row as unknown as Project;
}
const route = (project: Project) => '/' + project.username + '/' + project.normalized_name + '/settings/model-providers';
function operationCount(value: Record<string, unknown>, operation: string) {
  need(Array.isArray(value.operations), 'PROJECT_MODELS_NAVIGATION_COUNTS_INVALID');
  const rows = value.operations.map(object).filter((row) => row.operation === operation);
  need(rows.length === 1 && Number.isSafeInteger(rows[0]!.browser), 'PROJECT_MODELS_NAVIGATION_COUNTS_INVALID');
  return Number(rows[0]!.browser);
}
function sameOperations(before: Record<string, unknown>, after: Record<string, unknown>) {
  need(Array.isArray(before.operations), 'PROJECT_MODELS_NAVIGATION_COUNTS_INVALID');
  for (const row of before.operations.map(object)) need(operationCount(before, String(row.operation)) === operationCount(after, String(row.operation)), 'PROJECT_MODELS_NAVIGATION_UNEXPECTED_OPERATION');
}
function originalBody(fact: NativeFact, operation: string, projectID: string) {
  const directory = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
  const rows = readdirSync(directory).filter((name) => /^response-\d+\.json$/.test(name)).map((name) => object(JSON.parse(readFileSync(join(directory, name), 'utf8')))).filter((row) => row.source === 'browser' && row.request_token === fact.token);
  need(rows.length === 1, 'PROJECT_MODELS_NAVIGATION_BODY_MISSING');
  const row = rows[0]!;
  need(row.protocol === 'project-owner-models.v1' && row.input_hash === process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH && row.operation === operation && row.project_id === projectID && row.method === fact.method && row.endpoint === fact.path && row.query === fact.query && row.status === fact.status && row.body_stage === 'complete_formal_upstream' && /^[0-9a-f]{64}$/.test(String(row.body_sha256)) && row.body_file === 'body-' + row.body_sha256 + '.json', 'PROJECT_MODELS_NAVIGATION_BODY_BINDING_INVALID');
  const bytes = readFileSync(join(directory, String(row.body_file)));
  need(bytes.length === row.body_bytes && bytes.length === fact.bytes && bytes.length <= 8388608 && createHash('sha256').update(bytes).digest('hex') === row.body_sha256, 'PROJECT_MODELS_NAVIGATION_BODY_BYTES_INVALID');
  return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)));
}
function observer(page: Page, harness: NavigationHarness) {
  const tokens = new Set<string>();
  return async (operation: string, projectID: string, method: string, leaf: string, status: number, action: () => Promise<void>, query = '') => {
    const before = await harness.nativeFacts(page), path = '/api/v1/projects/' + projectID + '/' + leaf;
    await action();
    let matches: readonly NativeFact[] = [];
    await expect.poll(async () => {
      const facts = await harness.nativeFacts(page);
      need(facts.length >= before.length, 'PROJECT_MODELS_NAVIGATION_OBSERVATIONS_RESET');
      matches = facts.slice(before.length).filter((fact) => fact.method === method && fact.path === path);
      return matches.length === 1 && matches[0]!.status === status && matches[0]!.eof && matches[0]!.ended && matches[0]!.released;
    }).toBe(true);
    const fact = matches[0]!;
    need(fact.token !== null && /^r[0-9]{6}$/.test(fact.token) && !tokens.has(fact.token) && fact.query === query, 'PROJECT_MODELS_NAVIGATION_NATIVE_BINDING_INVALID');
    tokens.add(fact.token);
    return originalBody(fact, operation, projectID);
  };
}
// Session bodies remain private in this call. Only the formal safe identity is
// returned, never CSRF, cookies, login inputs, headers or their digests.
async function sessionStage<T>(work: Promise<T>, code: 'PROJECT_MODELS_NAVIGATION_SESSION_FINISH_TIMEOUT' | 'PROJECT_MODELS_NAVIGATION_SESSION_JSON_TIMEOUT') {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([work, new Promise<never>((_resolve, reject) => {
      timer = setTimeout(() => reject(new Error(code)), 5_000);
    })]);
  } finally { if (timer !== undefined) clearTimeout(timer); }
}
async function sessionIdentity(page: Page, action: () => Promise<void>, step: (name: string) => void) {
  const diagnostic = await beginSessionDiagnostic(page, 'navigation');
  let diagnosticFailed = false;
  let selected: Request | undefined;
  const observation = { headers_seen: false, finished_event: false, failed_event: false };
  const publish = () => writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'navigation-session-events.json'), JSON.stringify({ protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH, observation }), { mode: 0o600 });
  const finished = (request: Request) => { if (request === selected) { observation.finished_event = true; publish(); } };
  const failed = (request: Request) => { if (request === selected) { observation.failed_event = true; publish(); } };
  page.on('requestfinished', finished); page.on('requestfailed', failed);
  try {
    publish();
    const waiting = page.waitForResponse((response) => {
      if (new URL(response.url()).pathname !== '/api/v1/session' || response.request().method() !== 'GET' || response.status() !== 200) return false;
      selected = response.request(); diagnostic.select(response); observation.headers_seen = true; publish(); return true;
    }, { timeout: 5_000 });
    const [response] = await Promise.all([waiting, (async () => { await action(); step('navigation-session-action-returned'); })()]);
    step('navigation-session-headers-observed');
    try { need(await sessionStage(response.finished(), 'PROJECT_MODELS_NAVIGATION_SESSION_FINISH_TIMEOUT') === null, 'PROJECT_MODELS_NAVIGATION_SESSION_INCOMPLETE'); }
    catch (error) {
      if (error instanceof Error && error.message === 'PROJECT_MODELS_NAVIGATION_SESSION_FINISH_TIMEOUT') throw error;
      throw new Error('PROJECT_MODELS_NAVIGATION_SESSION_INCOMPLETE');
    }
    step('navigation-session-finished');
    try {
      const body = object(await sessionStage(response.json(), 'PROJECT_MODELS_NAVIGATION_SESSION_JSON_TIMEOUT')), user = object(body.user), session = object(body.session);
      need(uuid(user.id) && uuid(session.id) && (user.role === 'user' || user.role === 'admin'), 'PROJECT_MODELS_NAVIGATION_SESSION_INVALID');
      step('navigation-session-json-validated');
      return { userID: user.id, sessionID: session.id, role: user.role };
    } catch (error) {
      if (error instanceof Error && error.message === 'PROJECT_MODELS_NAVIGATION_SESSION_JSON_TIMEOUT') throw error;
      throw new Error('PROJECT_MODELS_NAVIGATION_SESSION_INVALID');
    }
  } catch (error) { diagnosticFailed = true; throw error; } finally {
    page.off('requestfinished', finished); page.off('requestfailed', failed); publish();
    await diagnostic.finish(diagnosticFailed);
  }
}
async function pageshow(page: Page) {
  const observation = await page.evaluate(async () => {
    const safeState = () => {
      const logout = [...document.querySelectorAll<HTMLButtonElement>('.account-actions button')].find((node) => node.querySelector('.button-label:not([aria-hidden="true"])')?.textContent?.trim() === '退出登录');
      return { path_home: location.pathname === '/', path_login: location.pathname === '/login', document_visible: document.visibilityState === 'visible', logout_present: !!logout, logout_disabled: logout?.disabled === true };
    };
    const before = safeState();
    dispatchEvent(new PageTransitionEvent('pageshow'));
    await Promise.resolve();
    return { before, after: safeState() };
  });
  writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'navigation-session-state.json'), JSON.stringify({ protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH, stage: 'initial-pageshow', observation }), { mode: 0o600 });
}
async function newProvider(page: Page, name: string) {
  await button(page, '创建 Provider').click();
  const dialog = page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  await dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill(name);
  await dialog.getByRole('textbox', { name: 'Base URL', exact: true }).fill('https://model-ui.invalid/v1');
  return dialog;
}
async function discardProvider(page: Page, dialog: Locator) {
  await button(dialog, '取消').click();
  const confirmation = page.getByRole('dialog', { name: '放弃当前表单修改？', exact: true });
  await button(confirmation, '放弃修改').click(); await expect(dialog).toBeHidden();
}


export async function runNavigationAndLayouts(page: Page, harness: NavigationHarness) {
  const material = object(harness.material), projects = object(material.projects), main = locator(projects.main), second = locator(projects.second), owner = object(object(material.actors).owner), system = object(material.system);
  need(material.mode === 'navigation' && Object.keys(projects).length === 2 && main.id !== second.id && main.owner_user_id === owner.user_id && second.owner_user_id === owner.user_id, 'PROJECT_MODELS_NAVIGATION_MATERIAL_INVALID');
  const selection = object(system.selection), summary = object(system.summary), selectionDraft = object(object(selection.draft).model), summaryDraft = object(object(summary.draft).model);
  need(uuid(selectionDraft.id) && typeof selectionDraft.name === 'string' && uuid(summaryDraft.id) && typeof summaryDraft.name === 'string' && object(selection.draft).purpose === 'memory', 'PROJECT_MODELS_NAVIGATION_SYSTEM_DRAFT_INVALID');
  const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
  need(typeof images === 'string' && isAbsolute(images), 'PROJECT_MODELS_NAVIGATION_IMAGES_REQUIRED');
  const selectedImage = process.env.MODELS_NAVIGATION_IMAGE;
  need(selectedImage === undefined || selectedImage === 'light-narrow-normal', 'PROJECT_MODELS_NAVIGATION_IMAGE_SELECTION_INVALID');
  const checks: Record<string, boolean> = {}, observe = observer(page, harness);
  const providerDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  const credentialDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-credential-form') });
  const confirmLeave = () => page.getByRole('dialog', { name: '离开项目模型设置？', exact: true });
  const path = (leaf: string) => '/' + main.username + '/' + main.normalized_name + '/settings/' + leaf;
  let systemWrites = 0;
  const record = (request: Request) => {
    const target = new URL(request.url()).pathname;
    if (request.method() === 'PUT' && ['/api/v1/system/model-selection', '/api/v1/system/model-selection/meeting-summary'].includes(target)) systemWrites++;
  };
  page.on('request', record);
  try {
    harness.step('navigation-login');
    await harness.loginOwner(page);
    harness.step('navigation-login-complete');
    const identity = await sessionIdentity(page, () => pageshow(page), harness.step);
    harness.step('navigation-session-observed');
    need(identity.userID === owner.user_id && identity.role === 'admin', 'PROJECT_MODELS_NAVIGATION_ADMIN_OWNER_REQUIRED');
    harness.step('navigation-open-begin');
    const initialPage = await observe('listProjectModelProviders', main.id, 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
    harness.step('navigation-open-completed');
    const initial = await harness.snapshot('main'), seed = initial.current.providers.find((row) => row.present);
    need(seed && Array.isArray(initialPage.items) && initialPage.items.length === 1 && object(initialPage.items[0]).id === seed.id, 'PROJECT_MODELS_NAVIGATION_SEED_MISSING');
    await expect(page.getByRole('navigation', { name: '项目导航', exact: true }).getByRole('link', { name: '项目设置', exact: true })).toHaveAttribute('aria-current', 'page');

    harness.step('navigation-model-panel');
    await observe('getProjectModelProvider', main.id, 'GET', 'model-providers/' + seed.id, 200, () => button(page, '为 Provider ' + seed.id + ' 创建 Model').click());
    const model = page.getByRole('dialog').filter({ has: page.locator('#project-model-form') });
    await model.getByRole('textbox', { name: 'Model 名称', exact: true }).fill('Models Navigation Chat');
    await model.getByRole('textbox', { name: '原生型号', exact: true }).fill('models-fixture-navigation');
    const modelReceipt = await observe('createProjectModel', main.id, 'POST', 'models', 200, () => button(model, '保存 Model').click());
    need(modelReceipt.kind === 'model.create' && uuid(modelReceipt.resource_id) && modelReceipt.version === '1' && modelReceipt.affected_references === '0', 'PROJECT_MODELS_NAVIGATION_MODEL_RECEIPT_INVALID');
    const confirmedModel = await harness.strictReceipt(model);
    need(Object.keys(modelReceipt).length === Object.keys(confirmedModel).length && Object.keys(modelReceipt).every((key) => modelReceipt[key] === confirmedModel[key]), 'PROJECT_MODELS_NAVIGATION_MODEL_UI_RECEIPT_INVALID');
    const afterModel = await harness.snapshot('main'); harness.durableDelta(initial, afterModel, 1, 0);
    need(afterModel.current.models.find((row) => row.id === modelReceipt.resource_id)?.provider_id === seed.id, 'PROJECT_MODELS_NAVIGATION_MODEL_FACT_INVALID');
    await button(model, '取消').click(); await expect(model).toBeHidden();
    const models = await observe('listProjectModels', main.id, 'GET', 'models', 200, () => button(page, '项目 Models（全部 Providers）').click(), 'limit=25');
    need(Array.isArray(models.items) && models.items.length === 1 && object(models.items[0]).id === modelReceipt.resource_id, 'PROJECT_MODELS_NAVIGATION_MODEL_LIST_INVALID');
    await expect(page.getByRole('list', { name: '项目 Models 列表', exact: true }).locator('[data-model-id="' + modelReceipt.resource_id + '"]')).toBeVisible();
    const available = await observe('listProjectAvailableChatModels', main.id, 'GET', 'available-chat-models', 200, () => harness.openProject(page, 'main', 'available-models'), 'limit=25');
    need(Array.isArray(available.items) && available.items.some((entry) => object(entry).id === modelReceipt.resource_id), 'PROJECT_MODELS_NAVIGATION_AVAILABLE_MISSING');
    await expect(page.getByRole('heading', { name: '可用模型', level: 1, exact: true })).toBeVisible();
    await expect(page.getByRole('navigation', { name: '项目导航', exact: true }).getByRole('link', { name: '项目设置', exact: true })).toHaveAttribute('aria-current', 'page');
    checks.two_suffixes = true;

    harness.step('navigation-general-and-audit');
    await harness.navigate(page, '/' + main.username + '/' + main.normalized_name + '/settings');
    await expect.poll(() => new URL(page.url()).pathname).toBe(path('general'));
    await expect(page.getByRole('heading', { name: '基本信息', level: 2, exact: true })).toBeVisible();
    await page.getByRole('link', { name: '项目审计', exact: true }).click();
    await expect.poll(() => new URL(page.url()).pathname).toBe(path('audit'));
    await expect(page.getByRole('heading', { name: '项目审计', level: 1, exact: true })).toBeVisible();
    await expect(button(page.getByLabel('项目审计分页', { exact: true }), '重新读取')).toBeEnabled();
    await expect(page.getByRole('navigation', { name: '项目导航', exact: true }).getByRole('link', { name: '项目设置', exact: true })).toHaveAttribute('aria-current', 'page');
    checks.default_general_and_audit = true;

    harness.step('navigation-raw-return');
    for (const raw of [path('model-providers') + '/', path('available-models') + '/child', path('model-providers') + '?x=1', path('available-models') + '#fragment', path('model-providers').replace('/settings/', '/%73ettings/'), path('available-models').replace('/settings/', '\\settings\\')]) {
      const before = await harness.nativeFacts(page);
      await harness.navigate(page, '/login?return=' + encodeURIComponent(raw));
      await expect.poll(() => new URL(page.url()).pathname + new URL(page.url()).search + new URL(page.url()).hash).toBe('/');
      need((await harness.nativeFacts(page)).length === before.length, 'PROJECT_MODELS_NAVIGATION_INVALID_RETURN_REQUEST');
    }
    for (const leaf of ['model-providers', 'available-models'] as const) {
      const operation = leaf === 'model-providers' ? 'listProjectModelProviders' : 'listProjectAvailableChatModels', endpoint = leaf === 'model-providers' ? leaf : 'available-chat-models';
      await observe(operation, main.id, 'GET', endpoint, 200, async () => {
        await harness.navigate(page, '/login?return=' + encodeURIComponent(path(leaf)));
        await expect.poll(() => new URL(page.url()).pathname).toBe(path(leaf));
        const section = page.locator(leaf === 'model-providers' ? 'section.project-providers' : 'section.available-models');
        await expect(section).toBeVisible();
        const reread = button(section, '重新读取项目');
        if (await reread.isVisible()) { await reread.click(); await expect(reread).toBeHidden(); }
      }, 'limit=25');
    }
    checks.raw_return_rejection = true;

    harness.step('navigation-draft-and-focus');
    await observe('listProjectModelProviders', main.id, 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
    const createTrigger = button(page, '创建 Provider');
    await createTrigger.focus(); await page.keyboard.press('Enter');
    let editor = providerDialog();
    const nameInput = editor.getByRole('textbox', { name: 'Provider 名称', exact: true });
    await nameInput.fill('Models Navigation Unsaved');
    await editor.getByRole('textbox', { name: 'Base URL', exact: true }).fill('https://model-ui.invalid/v1');
    await nameInput.focus();
    await page.evaluate(() => history.back());
    await expect(confirmLeave()).toBeVisible(); await button(confirmLeave(), '继续编辑').click();
    await expect.poll(() => new URL(page.url()).pathname).toBe(path('model-providers'));
    await expect(nameInput).toHaveValue('Models Navigation Unsaved'); await expect(nameInput).toBeFocused();
    await page.keyboard.press('Tab');
    need(await editor.evaluate((node) => node.contains(document.activeElement)), 'PROJECT_MODELS_NAVIGATION_MODAL_FOCUS_ESCAPED');
    await page.keyboard.press('Shift+Tab');
    need(await editor.evaluate((node) => node.contains(document.activeElement)), 'PROJECT_MODELS_NAVIGATION_MODAL_FOCUS_ESCAPED');
    await observe('listProjectAvailableChatModels', main.id, 'GET', 'available-chat-models', 200, async () => {
      await harness.navigate(page, path('available-models'));
      await button(confirmLeave(), '放弃并离开').click();
      await expect.poll(() => new URL(page.url()).pathname).toBe(path('available-models'));
      const reread = button(page.locator('section.available-models'), '重新读取项目');
      await expect(page.locator('section.available-models')).toBeVisible();
      if (await reread.isVisible()) { await reread.click(); await expect(reread).toBeHidden(); }
    }, 'limit=25');
    await expect(providerDialog()).toHaveCount(0);
    harness.durableDelta(afterModel, await harness.snapshot('main'), 0, 0);

    harness.step('navigation-private-input-close');
    await observe('listProjectModelProviders', main.id, 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
    const credentialBefore = await harness.snapshot('main'), credentialCounts = await harness.counts();
    await button(page, '创建凭据').click(); let credential = credentialDialog();
    await harness.fillCredential(credential);
    await button(credential.locator('footer'), '关闭').click();
    const privateConfirmation = page.getByRole('dialog', { name: '放弃尚未提交的凭据材料？', exact: true });
    await expect(privateConfirmation).toBeVisible(); await button(privateConfirmation, '继续编辑').click();
    need((await credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue()).length > 0, 'PROJECT_MODELS_NAVIGATION_PRIVATE_CANCEL_LOST_INPUT');
    await button(credential.locator('footer'), '关闭').click(); await button(privateConfirmation, '放弃材料').click(); await expect(credential).toBeHidden();
    sameOperations(credentialCounts, await harness.counts());
    await button(page, '创建凭据').click(); credential = credentialDialog();
    need(await credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === '', 'PROJECT_MODELS_NAVIGATION_PRIVATE_CLOSE_NOT_CLEARED');

    harness.step('navigation-pending-guard');
    await harness.fillCredential(credential);
    const arm = await harness.arm('createProjectModelCredential', 'main', null, 'after_complete_disconnect');
    await button(credential, '创建凭据').click(); await expect(credential.getByText(/结果尚未确认/)).toBeVisible();
    need(await credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === '', 'PROJECT_MODELS_NAVIGATION_PRIVATE_SUBMIT_NOT_CLEARED');
    const lost = await harness.control(arm, (state) => state.joined === true); await harness.actualLoss(page, lost, 0);
    const committed = await harness.snapshot('main'); harness.durableDelta(credentialBefore, committed, 0, 1);
    const credentialID = committed.current.credentials.find((row) => row.metadata !== null)?.credential_id;
    need(uuid(credentialID), 'PROJECT_MODELS_NAVIGATION_CREDENTIAL_FACT_MISSING');
    await button(credential.locator('footer'), '关闭').click(); await expect(credential).toBeHidden();
    const pending = page.getByLabel('原请求与历史观察', { exact: true });
    await expect(pending.getByText(/结果尚未确认/)).toBeVisible();
    await harness.navigate(page, route(second)); await expect(confirmLeave()).toBeVisible();
    await button(confirmLeave(), '继续编辑').click();
    await expect.poll(() => new URL(page.url()).pathname).toBe(path('model-providers'));
    await expect(pending.getByText(/结果尚未确认/)).toBeVisible();
    need(operationCount(await harness.counts(), 'createProjectModelCredential') === operationCount(credentialCounts, 'createProjectModelCredential') + 1, 'PROJECT_MODELS_NAVIGATION_PENDING_IMPLICIT_REPLAY');
    harness.durableDelta(committed, await harness.snapshot('main'), 0, 0);
    const replay = await observe('createProjectModelCredential', main.id, 'POST', 'model-credentials', 200, () => button(pending, '按原请求重放').click());
    need(replay.credential_id === credentialID && replay.purpose === 'model' && replay.version === '1' && replay.deleted === false, 'PROJECT_MODELS_NAVIGATION_CREDENTIAL_REPLAY_INVALID');
    const confirmed = await harness.strictReceipt(pending);
    need(Object.keys(confirmed).length === Object.keys(replay).length && Object.keys(replay).every((key) => replay[key] === confirmed[key]), 'PROJECT_MODELS_NAVIGATION_CREDENTIAL_UI_RECEIPT_INVALID');
    const replayed = await harness.snapshot('main'); harness.durableDelta(committed, replayed, 0, 0); harness.originalReplay(replayed, String(lost.origin_token));

    harness.step('navigation-partial-success-guard');
    const prepared = page.getByLabel('已创建凭据', { exact: true });
    await expect(prepared).toBeVisible(); await expect(prepared.getByText(credentialID, { exact: true })).toBeVisible();
    need(replayed.current.providers.every((row) => row.credential_ref === null) && replayed.reference_presence.credentials.find((row) => row.credential_id === credentialID)?.present === false, 'PROJECT_MODELS_NAVIGATION_CREDENTIAL_IMPLICIT_BIND');
    await harness.navigate(page, route(second)); await expect(confirmLeave()).toBeVisible();
    await button(confirmLeave(), '继续编辑').click();
    await expect(prepared).toBeVisible(); await expect(prepared.getByText(credentialID, { exact: true })).toBeVisible();
    await observe('listProjectModelProviders', second.id, 'GET', 'model-providers', 200, () => harness.openProject(page, 'second', 'model-providers', true), 'limit=25');
    await expect(page.getByLabel('已创建凭据', { exact: true })).toHaveCount(0);
    await expect(page.getByLabel('原请求与历史观察', { exact: true })).toHaveCount(0);
    const afterLeave = await harness.snapshot('main'); harness.durableDelta(replayed, afterLeave, 0, 0);
    need(afterLeave.current.credentials.find((row) => row.credential_id === credentialID)?.metadata?.version === '1' && afterLeave.reference_presence.credentials.find((row) => row.credential_id === credentialID)?.present === false, 'PROJECT_MODELS_NAVIGATION_PARTIAL_FACT_REVOKED');
    checks.draft_pending_partial_guard = checks.private_input_cleared = true;

    harness.step('navigation-system-intent-isolation');
    await harness.navigate(page, '/system/model-selection');
    await expect(page.getByRole('heading', { name: '平台模型用途', level: 1, exact: true })).toBeVisible();
    const summarySection = page.locator('.meeting-summary-settings');
    await expect(button(summarySection, '编辑会议 Summary')).toBeEnabled();
    await button(summarySection, '编辑会议 Summary').click();
    const summaryEditor = page.getByRole('form', { name: '会议 Summary 编辑', exact: true });
    await button(summaryEditor, '选择会议 Summary Model').click();
    const summaryProvider = summaryEditor.locator('.choices > li').filter({ has: page.locator('strong', { hasText: /^Models draft memory Provider$/ }) });
    await button(summaryProvider, '查看此 Provider 的 Models').click();
    const summaryCandidate = summaryEditor.locator('.choices > li').filter({ has: page.locator('strong', { hasText: new RegExp('^' + String(summaryDraft.name).replace(/[.*+?^$()|[\]\\]/g, '\\$&') + '$') }) });
    await expect(button(summaryCandidate, '选择此会议 Summary Model')).toBeEnabled(); await button(summaryCandidate, '选择此会议 Summary Model').click();
    await expect(summaryEditor.getByText('草稿 Model：' + summaryDraft.name, { exact: true })).toBeVisible();
    await button(page, '编辑用途').click();
    const selectionEditor = page.getByRole('dialog', { name: '配置平台模型用途', exact: true });
    await button(selectionEditor, '选择 Memory').click(); await button(selectionEditor, '浏览 Models draft memory Provider').click();
    await expect(button(selectionEditor, '选择 ' + selectionDraft.name)).toBeEnabled(); await button(selectionEditor, '选择 ' + selectionDraft.name).click();
    const purposes = selectionEditor.getByRole('list', { name: '四项用途草稿', exact: true });
    await expect(purposes.getByText(String(selectionDraft.id), { exact: true })).toBeVisible();
    await harness.navigate(page, route(main));
    const aggregate = page.getByRole('dialog', { name: '放弃未保存修改？', exact: true });
    await expect(aggregate.getByText(/四项平台用途、会议 Summary/)).toBeVisible();
    await button(aggregate, '继续编辑').click();
    await expect.poll(() => new URL(page.url()).pathname).toBe('/system/model-selection');
    await expect(purposes.getByText(String(selectionDraft.id), { exact: true })).toBeVisible();
    need(await summaryEditor.getByText('草稿 Model：' + summaryDraft.name, { exact: true }).count() === 1, 'PROJECT_MODELS_NAVIGATION_SUMMARY_DRAFT_LOST');
    await button(selectionEditor, '取消').click();
    await expect(aggregate.getByText('仅放弃四项平台用途草稿和请求跟踪；此前保存仍可能生效，会议 Summary 保持。', { exact: true })).toBeVisible();
    await button(aggregate, '放弃修改').click(); await expect(selectionEditor).toBeHidden();
    await expect(summaryEditor.getByText('草稿 Model：' + summaryDraft.name, { exact: true })).toBeVisible();
    await button(summaryEditor, '取消会议 Summary 编辑').click();
    const summaryConfirmation = page.getByRole('dialog', { name: '放弃会议 Summary 修改？', exact: true });
    await button(summaryConfirmation, '继续编辑').click();
    await expect(summaryEditor.getByText('草稿 Model：' + summaryDraft.name, { exact: true })).toBeVisible();
    await button(summaryEditor, '取消会议 Summary 编辑').click(); await button(summaryConfirmation, '放弃修改').click();
    await expect(summaryEditor).toBeHidden();
    await button(page, '编辑用途').click();
    need((await purposes.getByText(String(selectionDraft.id), { exact: true }).count()) === 0, 'PROJECT_MODELS_NAVIGATION_SELECTION_DISCARD_FAILED');
    await button(selectionEditor, '取消').click(); await expect(selectionEditor).toBeHidden();
    need(systemWrites === 0, 'PROJECT_MODELS_NAVIGATION_SYSTEM_WRITE_OCCURRED');
    checks.selection_summary_separate_intents = true;

    harness.step('navigation-layouts');
    await observe('listProjectModelProviders', main.id, 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
    await observe('listProjectModels', main.id, 'GET', 'models', 200, () => button(page, '项目 Models（全部 Providers）').click(), 'limit=25');
    let layouts = 0, captures = 0, drawerChecked = false, reducedChecked = 0;
    for (const colorScheme of ['light', 'dark'] as const) {
      for (const viewport of [{ width: 1440, height: 900, name: 'desktop' }, { width: 390, height: 844, name: 'narrow' }] as const) {
        for (const reducedMotion of ['no-preference', 'reduce'] as const) {
          await page.emulateMedia({ colorScheme, reducedMotion });
          await page.setViewportSize({ width: viewport.width, height: viewport.height });
          await expect(page.locator('html')).toHaveAttribute('data-theme', colorScheme);
          await expect(page.getByRole('heading', { name: 'Providers', level: 1, exact: true })).toBeVisible();
          await expect(page.getByRole('list', { name: '项目 Models 列表', exact: true }).locator('[data-model-id="' + modelReceipt.resource_id + '"]')).toBeVisible();
          if (viewport.name === 'narrow' && !drawerChecked) {
            const trigger = button(page, '项目设置栏目');
            await trigger.focus(); await page.keyboard.press('Enter');
            const drawer = page.getByRole('dialog', { name: '项目设置栏目', exact: true });
            await expect(drawer).toBeVisible();
            await page.keyboard.press('Tab');
            need(await drawer.evaluate((node) => node.contains(document.activeElement)), 'PROJECT_MODELS_NAVIGATION_DRAWER_FOCUS_ESCAPED');
            await page.keyboard.press('Shift+Tab');
            need(await drawer.evaluate((node) => node.contains(document.activeElement)), 'PROJECT_MODELS_NAVIGATION_DRAWER_FOCUS_ESCAPED');
            const group = button(drawer, '模型与 Provider');
            await expect(group).toHaveAttribute('aria-expanded', 'true');
            const controls = await group.getAttribute('aria-controls');
            need(controls !== null && await drawer.locator('[id="' + controls + '"]').count() === 1, 'PROJECT_MODELS_NAVIGATION_DRAWER_CONTROLS_INVALID');
            await group.click(); await expect(group).toHaveAttribute('aria-expanded', 'false');
            await expect(drawer.getByRole('link', { name: 'Providers', exact: true })).toBeHidden();
            await group.click(); await expect(group).toHaveAttribute('aria-expanded', 'true');
            await expect(drawer.getByRole('link', { name: 'Providers', exact: true })).toHaveAttribute('aria-current', 'page');
            await page.keyboard.press('Escape'); await expect(drawer).toBeHidden(); await expect(trigger).toBeFocused();
            // beforeLeave hides the role immediately; the public overlay node
            // remains until Vue finishes its real CSS leave transition.
            await expect(page.locator('.ui-overlay.drawer-overlay')).toHaveCount(0);
            drawerChecked = true;
          }
          await expect(page.getByRole('dialog')).toHaveCount(0);
          need(await page.locator('input[type="password"]').count() === 0, 'PROJECT_MODELS_NAVIGATION_PRIVATE_LAYOUT_INPUT_PRESENT');
          const layout = await page.evaluate(() => {
            const root = document.documentElement, content = document.querySelector<HTMLElement>('section.project-providers');
            const visible = [...document.querySelectorAll<HTMLElement>('section.project-providers *, .project-nav *, .settings-menu-button')].filter((node) => node.getClientRects().length > 0);
            const allMotionZero = visible.every((node) => {
              const style = getComputedStyle(node);
              return [style.animationDuration, style.transitionDuration].every((value) => value.split(',').every((part) => Number.parseFloat(part) === 0));
            });
            return { overflow: root.scrollWidth > root.clientWidth + 1 || !content || content.scrollWidth > content.clientWidth + 1, reduced: matchMedia('(prefers-reduced-motion: reduce)').matches, dark: matchMedia('(prefers-color-scheme: dark)').matches, allMotionZero };
          });
          need(!layout.overflow && layout.reduced === (reducedMotion === 'reduce') && layout.dark === (colorScheme === 'dark'), 'PROJECT_MODELS_NAVIGATION_LAYOUT_INVALID');
          if (reducedMotion === 'reduce') { need(layout.allMotionZero, 'PROJECT_MODELS_NAVIGATION_REDUCED_MOTION_INVALID'); reducedChecked++; }
          const name = 'navigation-' + colorScheme + '-' + viewport.name + '-' + (reducedMotion === 'reduce' ? 'reduced' : 'normal') + '.png';
          if (selectedImage === undefined || name === 'navigation-' + selectedImage + '.png') {
            try { await page.screenshot({ path: join(images, name), fullPage: true, animations: 'allow' }); }
            catch { throw new Error('PROJECT_MODELS_NAVIGATION_IMAGE_FAILED'); }
            captures++;
          }
          layouts++;
        }
      }
    }
    need(layouts === 8 && drawerChecked && reducedChecked === 4 && captures === (selectedImage === undefined ? 8 : 1), 'PROJECT_MODELS_NAVIGATION_LAYOUT_COVERAGE_INVALID');
    // All eight browser conditions still run for a selected-image recheck.
    // Its one new capture combines with the separately accepted seven images;
    // default runs capture all eight. Actual image review remains required.
    checks.keyboard_focus_drawer = checks.eight_layouts = checks.reduced_motion = checks.no_overflow = true;
    harness.step('navigation-no-debug');
    await harness.navigate(page, '/debug');
    await expect(page.getByRole('heading', { name: '未找到页面', exact: true })).toBeVisible();
    checks.no_debug = true;
    need(systemWrites === 0, 'PROJECT_MODELS_NAVIGATION_SYSTEM_WRITE_OCCURRED');
    const finalSnapshot = await harness.snapshot('main'); harness.durableDelta(initial, finalSnapshot, 1, 1);
    need(finalSnapshot.current.providers.length === 1 && finalSnapshot.current.providers[0]!.credential_ref === null && finalSnapshot.current.models.find((row) => row.id === modelReceipt.resource_id)?.present === true && finalSnapshot.current.credentials.find((row) => row.credential_id === credentialID)?.metadata?.version === '1', 'PROJECT_MODELS_NAVIGATION_FINAL_FACTS_INVALID');
    const finalCounts = await harness.counts(), controls = object(finalCounts.controls);
    need(controls.armed === 1 && controls.claimed === 1 && controls.disconnected === 1 && controls.held === 0 && controls.cut === 0, 'PROJECT_MODELS_NAVIGATION_CONTROL_COUNTS_INVALID');
    need(operationCount(finalCounts, 'createProjectModel') === 1 && operationCount(finalCounts, 'createProjectModelCredential') === 2, 'PROJECT_MODELS_NAVIGATION_OPERATION_COUNTS_INVALID');
    for (const operation of ['createProjectModelProvider', 'updateProjectModelProvider', 'deleteProjectModelProvider', 'updateProjectModel', 'deleteProjectModel', 'updateProjectModelCredential', 'deleteProjectModelCredential', 'lookupProjectModelConfiguration', 'lookupProjectModelCredential']) need(operationCount(finalCounts, operation) === 0, 'PROJECT_MODELS_NAVIGATION_IMPLICIT_MUTATION');
    harness.step('navigation-same-body-finish');
    await harness.finish(page, checks, layouts);
  } finally { page.off('request', record); }
}

