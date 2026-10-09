import { expect, type Locator, type Page } from '../../tests/account-captcha-web/node_modules/@playwright/test/index.js';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import type { ConfigCredentialSnapshot as Snapshot } from './configuration-and-credential';

type ProjectKey = 'main' | 'second' | 'other' | 'admin_owned' | 'archiving' | 'archived' | 'deleting' | 'pending' | 'config_recovery' | 'credential_recovery' | 'referenced';
type Project = Readonly<{ id: string; username: string; normalized_name: string; owner_user_id: string; initialized: boolean; lifecycle: string }>;
type Actor = Readonly<{ email: string; password: string; user_id: string }>;
type NativeFact = Readonly<{ token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; released: boolean; bytes: number }>;
type IPC =
  | { action: 'arm'; args: { operation: string; project: ProjectKey | null; target_id: string | null; query: string | null; effect: string } }
  | { action: 'release'; args: { arm_id: string; request_token: string } }
  | { action: 'logout'; args: { session_id: string } }
  | { action: 'rename-reuse'; args: { project: 'main' } }
  | { action: 'archive-recovery-project'; args: { project: 'config_recovery' | 'credential_recovery'; expected_version: string } }
  | { action: 'reference-fact'; args: { project: 'referenced'; state: 'present' | 'absent' } };
export type AuthorityHarness = Readonly<{
  material: unknown;
  loginOwner(page: Page): Promise<void>;
  navigate(page: Page, target: string): Promise<void>;
  fillCredential(scope: Locator): Promise<void>;
  ipc(request: IPC): Promise<Record<string, unknown>>;
  snapshot(project: ProjectKey): Promise<Snapshot>;
  strictReceipt(scope: Page | Locator): Promise<Record<string, unknown>>;
  durableDelta(before: Snapshot, after: Snapshot, configuration: number, credential: number): void;
  originalReplay(value: Snapshot, token: string): void;
  arm(operation: string, project: ProjectKey, target: string | null, effect: string, query?: string | null): Promise<string>;
  control(id: string, ready: (value: Record<string, unknown>) => boolean): Promise<Record<string, unknown>>;
  actualLoss(page: Page, control: Record<string, unknown>, maximumObservedBytes: 0 | 1): Promise<void>;
  counts(): Promise<Record<string, unknown>>;
  nativeFacts(page: Page): Promise<readonly NativeFact[]>;
  finish(page: Page, checks: Record<string, boolean>): Promise<void>;
  step(name: string): void;
}>;
const button = (scope: Page | Locator, name: string) => scope.getByRole('button', { name, exact: true });
function need(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function object(value: unknown): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value), 'PROJECT_MODELS_AUTHORITY_OBJECT_INVALID');
  return value as Record<string, unknown>;
}
function uuid(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value); }
function locator(value: unknown): Project {
  const row = object(value);
  need(uuid(row.id) && uuid(row.owner_user_id) && typeof row.username === 'string' && typeof row.normalized_name === 'string' && typeof row.initialized === 'boolean' && typeof row.lifecycle === 'string', 'PROJECT_MODELS_AUTHORITY_PROJECT_INVALID');
  return row as unknown as Project;
}
const route = (project: Project) => '/' + project.username + '/' + project.normalized_name + '/settings/model-providers';
function operationCount(value: Record<string, unknown>, operation: string) {
  need(Array.isArray(value.operations), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  const rows = value.operations.map(object).filter((row) => row.operation === operation);
  need(rows.length === 1 && Number.isSafeInteger(rows[0]!.browser), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  return Number(rows[0]!.browser);
}
function sameOperations(before: Record<string, unknown>, after: Record<string, unknown>) {
  need(Array.isArray(before.operations), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  for (const row of before.operations.map(object)) need(operationCount(before, String(row.operation)) === operationCount(after, String(row.operation)), 'PROJECT_MODELS_AUTHORITY_UNEXPECTED_OPERATION');
}
function originalBody(fact: NativeFact, operation: string, projectID: string) {
  const directory = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
  const rows = readdirSync(directory).filter((name) => /^response-\d+\.json$/.test(name)).map((name) => object(JSON.parse(readFileSync(join(directory, name), 'utf8')))).filter((row) => row.source === 'browser' && row.request_token === fact.token);
  need(rows.length === 1, 'PROJECT_MODELS_AUTHORITY_BODY_MISSING');
  const row = rows[0]!;
  need(row.protocol === 'project-owner-models.v1' && row.input_hash === process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH && row.operation === operation && row.project_id === projectID && row.method === fact.method && row.endpoint === fact.path && row.query === fact.query && row.status === fact.status && row.body_stage === 'complete_formal_upstream' && /^[0-9a-f]{64}$/.test(String(row.body_sha256)) && row.body_file === 'body-' + row.body_sha256 + '.json', 'PROJECT_MODELS_AUTHORITY_BODY_BINDING_INVALID');
  const bytes = readFileSync(join(directory, String(row.body_file)));
  need(bytes.length === row.body_bytes && bytes.length === fact.bytes && bytes.length <= 8388608 && createHash('sha256').update(bytes).digest('hex') === row.body_sha256, 'PROJECT_MODELS_AUTHORITY_BODY_BYTES_INVALID');
  return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)));
}
function observer(page: Page, harness: AuthorityHarness) {
  const tokens = new Set<string>();
  return async (operation: string, projectID: string, method: string, leaf: string, status: number, action: () => Promise<void>, query = '') => {
    const before = await harness.nativeFacts(page), path = '/api/v1/projects/' + projectID + '/' + leaf;
    await action();
    let matches: readonly NativeFact[] = [];
    await expect.poll(async () => {
      const facts = await harness.nativeFacts(page);
      need(facts.length >= before.length, 'PROJECT_MODELS_AUTHORITY_OBSERVATIONS_RESET');
      matches = facts.slice(before.length).filter((fact) => fact.method === method && fact.path === path);
      return matches.length === 1 && matches[0]!.status === status && matches[0]!.eof && matches[0]!.ended && matches[0]!.released;
    }).toBe(true);
    const fact = matches[0]!;
    need(fact.token !== null && /^r[0-9]{6}$/.test(fact.token) && !tokens.has(fact.token) && fact.query === query, 'PROJECT_MODELS_AUTHORITY_NATIVE_BINDING_INVALID');
    tokens.add(fact.token);
    return originalBody(fact, operation, projectID);
  };
}
// Session bodies remain private in this call. Only the formal safe identity is
// returned, never CSRF, cookies, login inputs, headers or their digests.
async function sessionIdentity(page: Page, action: () => Promise<void>) {
  const waiting = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/session' && response.request().method() === 'GET' && response.status() === 200);
  const [response] = await Promise.all([waiting, action()]);
  need(await response.finished() === null, 'PROJECT_MODELS_AUTHORITY_SESSION_INCOMPLETE');
  try {
    const body = object(await response.json()), user = object(body.user), session = object(body.session);
    need(uuid(user.id) && uuid(session.id) && (user.role === 'user' || user.role === 'admin'), 'PROJECT_MODELS_AUTHORITY_SESSION_INVALID');
    return { userID: user.id, sessionID: session.id, role: user.role };
  } catch { throw new Error('PROJECT_MODELS_AUTHORITY_SESSION_INVALID'); }
}
async function pageshow(page: Page) { await page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow'))); }
async function privateLogin(page: Page, actor: Actor) {
  await expect(page.locator('#login-email')).toBeVisible();
  try { await page.locator('#login-email').fill(actor.email); await page.locator('#login-password').fill(actor.password); }
  catch { throw new Error('PROJECT_MODELS_PRIVATE_LOGIN_INPUT_FAILED'); }
  await button(page, '登录').click(); await expect(button(page, '退出登录')).toBeEnabled();
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

export async function runAuthorityAndIdentity(page: Page, harness: AuthorityHarness) {
  const material = object(harness.material), rawProjects = object(material.projects), rawActors = object(material.actors);
  const keys: ProjectKey[] = ['main', 'second', 'other', 'admin_owned', 'archiving', 'archived', 'deleting', 'pending', 'config_recovery', 'credential_recovery', 'referenced'];
  need(material.mode === 'authority' && material.system === null && Object.keys(rawProjects).length === keys.length, 'PROJECT_MODELS_AUTHORITY_MATERIAL_INVALID');
  const projects = Object.fromEntries(keys.map((key) => [key, locator(rawProjects[key])])) as Record<ProjectKey, Project>;
  const actors = Object.fromEntries(['owner', 'other_owner', 'other_admin'].map((key) => {
    const row = object(rawActors[key]);
    need(uuid(row.user_id) && typeof row.email === 'string' && typeof row.password === 'string', 'PROJECT_MODELS_AUTHORITY_ACTOR_INVALID');
    return [key, row];
  })) as Record<'owner' | 'other_owner' | 'other_admin', Actor>;
  need(projects.main.owner_user_id === actors.owner.user_id && projects.other.owner_user_id === actors.other_owner.user_id && projects.admin_owned.owner_user_id === actors.other_admin.user_id, 'PROJECT_MODELS_AUTHORITY_OWNERS_INVALID');
  const checks: Record<string, boolean> = {}, observe = observer(page, harness);
  const providerDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  const credentialDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-credential-form') });
  async function leafReady() {
    const leaf = page.locator('section.project-providers');
    await expect(leaf).toBeVisible();
    const reread = button(leaf, '重新读取项目');
    if (await reread.isVisible()) { await expect(reread).toBeEnabled(); await reread.click(); await expect(reread).toBeHidden(); }
    await expect(button(leaf, '刷新 Providers')).toBeEnabled();
  }
  async function open(project: Project, discard = false) {
    return observe('listProjectModelProviders', project.id, 'GET', 'model-providers', 200, async () => {
      await harness.navigate(page, route(project));
      if (discard) await button(page.getByRole('dialog', { name: '离开项目模型设置？', exact: true }), '放弃并离开').click();
      await expect.poll(() => new URL(page.url()).pathname).toBe(route(project));
      await leafReady();
    }, 'limit=25');
  }
  async function reread(project: Project) {
    return observe('listProjectModelProviders', project.id, 'GET', 'model-providers', 200, async () => {
      const leaf = page.locator('section.project-providers');
      await expect(button(leaf, '重新读取项目')).toBeVisible();
      await button(leaf, '重新读取项目').click();
      await expect(button(leaf, '重新读取项目')).toBeHidden();
      // A preserved modal can make underlying controls inert; native EOF below
      // is the readiness witness, so do not require an underlying click.
    }, 'limit=25');
  }
  async function denied(project: Project, foreign: boolean) {
    const before = await harness.nativeFacts(page), beforeCounts = await harness.counts();
    const responsePromise = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/projects/resolve');
    const [response] = await Promise.all([responsePromise, harness.navigate(page, route(project))]);
    need(await response.finished() === null && (foreign ? [403, 404].includes(response.status()) : response.status() === 409), 'PROJECT_MODELS_AUTHORITY_OWNER_GATE_INVALID');
    await expect(page.getByRole('heading', { name: foreign ? '项目不可用' : '项目信息读取失败', exact: true })).toBeVisible();
    await expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0);
    await expect(page.getByRole('dialog')).toHaveCount(0);
    need((await harness.nativeFacts(page)).length === before.length, 'PROJECT_MODELS_AUTHORITY_DENIED_MODEL_REQUEST');
    sameOperations(beforeCounts, await harness.counts());
  }

  harness.step('authority-login');
  await harness.loginOwner(page);
  const ownerSession = await sessionIdentity(page, () => pageshow(page));
  need(ownerSession.userID === actors.owner.user_id && ownerSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_ORDINARY_OWNER_REQUIRED');
  const first = await open(projects.main), initial = await harness.snapshot('main');
  const seed = initial.current.providers.find((row) => row.present);
  need(seed && Array.isArray(first.items) && first.items.length === 1 && object(first.items[0]).id === seed.id, 'PROJECT_MODELS_AUTHORITY_SEED_MISSING');
  await expect(button(page, '创建 Provider')).toBeEnabled();
  checks.ordinary_owner = true;

  harness.step('authority-same-session-checking');
  let dialog = await newProvider(page, 'Models Same Session Draft');
  const checkingCounts = await harness.counts(), checkingFacts = await harness.nativeFacts(page);
  const sessionArmResult = await harness.ipc({ action: 'arm', args: { operation: 'getCurrentSession', project: null, target_id: null, query: null, effect: 'before_dispatch_hold' } });
  need(typeof sessionArmResult.arm_id === 'string', 'PROJECT_MODELS_AUTHORITY_ARM_INVALID');
  const sessionArm = sessionArmResult.arm_id;
  const restoredSession = sessionIdentity(page, () => pageshow(page));
  const sessionHeld = await harness.control(sessionArm, (state) => state.held === true);
  try {
    await expect(page.getByRole('heading', { name: '正在确认会话', exact: true })).toBeVisible();
    await expect(dialog).toBeHidden();
    await expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0);
    need((await harness.nativeFacts(page)).length === checkingFacts.length, 'PROJECT_MODELS_AUTHORITY_CHECKING_MODEL_REQUEST');
    sameOperations(checkingCounts, await harness.counts());
  } finally {
    await harness.ipc({ action: 'release', args: { arm_id: sessionArm, request_token: String(sessionHeld.request_token) } });
  }
  const sameSession = await restoredSession;
  need(sameSession.sessionID === ownerSession.sessionID && sameSession.userID === ownerSession.userID && sameSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_SAME_SESSION_CHANGED');
  await harness.control(sessionArm, (state) => state.joined === true);
  await reread(projects.main);
  dialog = providerDialog();
  await expect(dialog.getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue('Models Same Session Draft');
  await expect(dialog.getByRole('textbox', { name: 'Base URL', exact: true })).toHaveValue('https://model-ui.invalid/v1');
  harness.durableDelta(initial, await harness.snapshot('main'), 0, 0);
  checks.same_session_checking = true;
  await discardProvider(page, dialog);

  harness.step('authority-read-owner-tail');
  const heldArm = await harness.arm('getProjectModelProvider', 'main', seed.id, 'after_complete_hold');
  const beforeHeld = await harness.counts(), originalPath = route(projects.main);
  const heldBody = await observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, async () => {
    await button(page, '读取 Provider ' + seed.id).click();
    const held = await harness.control(heldArm, (state) => state.held === true && state.upstream_complete === true && state.safe_admitted === true);
    try {
      need((await harness.nativeFacts(page)).some((fact) => fact.method === 'GET' && fact.path.endsWith('/model-providers/' + seed.id) && !fact.ended), 'PROJECT_MODELS_AUTHORITY_NATIVE_TAIL_MISSING');
      await expect(button(providerDialog(), '取消')).toBeDisabled();
      await expect(button(page, '退出登录')).toBeDisabled();
      await harness.navigate(page, route(projects.second));
      await expect.poll(() => new URL(page.url()).pathname).toBe(originalPath);
      await pageshow(page);
      const afterHeld = await harness.counts();
      need(object(afterHeld.session).browser === object(beforeHeld.session).browser && operationCount(afterHeld, 'getProjectModelProvider') === operationCount(beforeHeld, 'getProjectModelProvider') + 1, 'PROJECT_MODELS_AUTHORITY_HELD_OWNER_BYPASSED');
      for (const entry of (beforeHeld.operations as unknown[]).map(object)) if (entry.operation !== 'getProjectModelProvider') need(operationCount(afterHeld, String(entry.operation)) === Number(entry.browser), 'PROJECT_MODELS_AUTHORITY_HELD_OWNER_BYPASSED');
    } finally {
      await harness.ipc({ action: 'release', args: { arm_id: heldArm, request_token: String(held.request_token) } });
    }
  });
  need(heldBody.id === seed.id, 'PROJECT_MODELS_AUTHORITY_HELD_TARGET_INVALID');
  const joinedRead = await harness.control(heldArm, (state) => state.joined === true);
  need((await harness.nativeFacts(page)).some((fact) => fact.token === joinedRead.request_token && fact.eof && fact.ended && fact.released), 'PROJECT_MODELS_AUTHORITY_HELD_RELEASE_MISSING');
  await button(providerDialog(), '取消').click(); await expect(providerDialog()).toBeHidden();
  await open(projects.second);
  await expect(providerDialog()).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0);
  // The UI explicitly blocks navigation while this owner is held. This is a
  // real transport-tail exclusion representative, not a fabricated late 401.
  checks.late_tail_isolation = true;

  harness.step('authority-project-name-reuse');
  await open(projects.main);
  dialog = await newProvider(page, 'Models Retired Project Draft');
  await open(projects.second, true);
  const renamed = await harness.ipc({ action: 'rename-reuse', args: { project: 'main' } });
  const oldNameReplacement = locator(renamed.replacement), originalRenamed = locator(renamed.renamed);
  need(originalRenamed.id === projects.main.id && oldNameReplacement.id !== originalRenamed.id && oldNameReplacement.normalized_name === projects.main.normalized_name, 'PROJECT_MODELS_AUTHORITY_RENAME_BINDING_INVALID');
  const replacementPage = await open(oldNameReplacement);
  need(Array.isArray(replacementPage.items) && replacementPage.items.length === 0, 'PROJECT_MODELS_AUTHORITY_REUSED_NAME_DATA_LEAK');
  await expect(providerDialog()).toHaveCount(0);
  await button(page, '创建 Provider').click();
  await expect(providerDialog().getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue('');
  await button(providerDialog(), '取消').click();
  const renamedPage = await open(originalRenamed);
  need(Array.isArray(renamedPage.items) && renamedPage.items.length === 1 && object(renamedPage.items[0]).id === seed.id, 'PROJECT_MODELS_AUTHORITY_STABLE_ID_LOST');
  projects.main = originalRenamed;
  checks.cross_project_and_name_reuse = true;

  harness.step('authority-lifecycle-gates');
  for (const key of ['archiving', 'archived'] as const) {
    const body = await open(projects[key]);
    need(Array.isArray(body.items) && body.items.length === 0, 'PROJECT_MODELS_AUTHORITY_READONLY_LIST_INVALID');
    await expect(page.getByText('项目当前为只读状态（' + key + '）。可以读取信息；原请求恢复遵循其各自的当前条件。', { exact: true })).toBeVisible();
    await expect(button(page, '创建 Provider')).toBeDisabled(); await expect(button(page, '创建凭据')).toBeDisabled();
    await expect(button(page, '管理凭据')).toBeEnabled();
  }
  for (const key of ['deleting', 'pending'] as const) await denied(projects[key], false);
  await denied(projects.other, true); await denied(projects.admin_owned, true);
  checks.aux_lifecycle_gates = true;

  harness.step('authority-archived-configuration');
  await open(projects.config_recovery);
  const configBefore = await harness.snapshot('config_recovery');
  dialog = await newProvider(page, 'Models Archived Original Provider');
  const configArm = await harness.arm('createProjectModelProvider', 'config_recovery', null, 'after_complete_cut');
  await button(dialog, '保存 Provider').click(); await expect(dialog.getByText(/结果尚未确认/)).toBeVisible();
  const configLost = await harness.control(configArm, (state) => state.joined === true);
  await harness.actualLoss(page, configLost, 1);
  const configCommitted = await harness.snapshot('config_recovery'); harness.durableDelta(configBefore, configCommitted, 1, 0);
  const createdProvider = configCommitted.current.providers.find((row) => row.present);
  need(createdProvider, 'PROJECT_MODELS_AUTHORITY_COMMITTED_PROVIDER_MISSING');
  const configArchived = await harness.ipc({ action: 'archive-recovery-project', args: { project: 'config_recovery', expected_version: configCommitted.project.version } });
  need(configArchived.project_id === projects.config_recovery.id && configArchived.lifecycle === 'archived' && configArchived.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_INVALID');
  const archiveSession = await sessionIdentity(page, () => pageshow(page));
  need(archiveSession.sessionID === ownerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_SESSION_CHANGED');
  await reread(projects.config_recovery); dialog = providerDialog();
  await expect(button(dialog, '保存 Provider')).toBeDisabled(); await expect(button(dialog, '按原请求重放')).toBeEnabled();
  const configReplay = await observe('createProjectModelProvider', projects.config_recovery.id, 'POST', 'model-providers', 200, () => button(dialog, '按原请求重放').click());
  need(configReplay.kind === 'provider.create' && configReplay.resource_id === createdProvider.id && configReplay.version === createdProvider.version && configReplay.affected_references === '0', 'PROJECT_MODELS_AUTHORITY_CONFIG_REPLAY_INVALID');
  const receipt = await harness.strictReceipt(dialog);
  need(Object.keys(receipt).length === Object.keys(configReplay).length && Object.keys(configReplay).every((key) => receipt[key] === configReplay[key]), 'PROJECT_MODELS_AUTHORITY_CONFIG_RECEIPT_INVALID');
  const configAfter = await harness.snapshot('config_recovery'); harness.durableDelta(configCommitted, configAfter, 0, 0); harness.originalReplay(configAfter, String(configLost.origin_token));
  need(configAfter.project.lifecycle === 'archived' && configAfter.fixture_only.archive_recovery_applied, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_FACT_MISSING');
  await button(dialog, '取消').click(); await expect(dialog).toBeHidden();
  checks.archived_config_original_replay = true;

  harness.step('authority-archived-credential');
  await open(projects.credential_recovery);
  const credentialBefore = await harness.snapshot('credential_recovery');
  await button(page, '创建凭据').click();
  let credential = credentialDialog(); await harness.fillCredential(credential);
  const credentialArm = await harness.arm('createProjectModelCredential', 'credential_recovery', null, 'after_complete_disconnect');
  await button(credential, '创建凭据').click(); await expect(credential.getByText(/结果尚未确认/)).toBeVisible();
  need(await credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === '', 'PROJECT_MODELS_AUTHORITY_PRIVATE_INPUT_NOT_CLEARED');
  const credentialLost = await harness.control(credentialArm, (state) => state.joined === true); await harness.actualLoss(page, credentialLost, 0);
  const credentialCommitted = await harness.snapshot('credential_recovery'); harness.durableDelta(credentialBefore, credentialCommitted, 0, 1);
  const credentialArchived = await harness.ipc({ action: 'archive-recovery-project', args: { project: 'credential_recovery', expected_version: credentialCommitted.project.version } });
  need(credentialArchived.project_id === projects.credential_recovery.id && credentialArchived.lifecycle === 'archived' && credentialArchived.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_INVALID');
  const credentialSession = await sessionIdentity(page, () => pageshow(page));
  need(credentialSession.sessionID === ownerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_SESSION_CHANGED');
  await reread(projects.credential_recovery); credential = credentialDialog();
  await expect(button(credential, '按原请求重放')).toBeDisabled(); await expect(button(credential, '创建凭据')).toBeDisabled();
  const lookupCounts = await harness.counts();
  const lookup = await observe('lookupProjectModelCredential', projects.credential_recovery.id, 'POST', 'model-credential-commands/lookup', 200, () => button(credential, '查证原请求').click());
  need(lookup.observed === true && object(lookup.result).credential_id === credentialCommitted.current.credentials[0]?.credential_id, 'PROJECT_MODELS_AUTHORITY_CREDENTIAL_LOOKUP_INVALID');
  await expect(credential.getByLabel('历史观察', { exact: true })).toBeVisible();
  await expect(credential.getByText(/结果尚未确认/)).toBeVisible(); await expect(credential.getByLabel('严格执行回执', { exact: true })).toHaveCount(0);
  await expect(button(credential, '按原请求重放')).toBeDisabled();
  const credentialAfter = await harness.snapshot('credential_recovery'); harness.durableDelta(credentialCommitted, credentialAfter, 0, 0);
  need(credentialAfter.project.lifecycle === 'archived' && credentialAfter.fixture_only.archive_recovery_applied && credentialAfter.origins.length === 1 && credentialAfter.origins[0]!.comparison_count === 0, 'PROJECT_MODELS_AUTHORITY_CREDENTIAL_REPLAY_OCCURRED');
  const afterLookup = await harness.counts();
  for (const row of (lookupCounts.operations as unknown[]).map(object)) need(operationCount(afterLookup, String(row.operation)) === Number(row.browser) + (row.operation === 'lookupProjectModelCredential' ? 1 : 0), 'PROJECT_MODELS_AUTHORITY_LOOKUP_SIDE_EFFECT');
  await button(credential.locator('footer'), '关闭').click(); await expect(credential).toBeHidden();
  const pending = page.getByLabel('原请求与历史观察', { exact: true });
  await expect(button(pending, '按原请求重放')).toBeDisabled();
  await button(pending, '放弃本地追踪').click();
  await button(page.getByRole('dialog', { name: '放弃本地原请求追踪？', exact: true }), '放弃追踪').click();
  harness.durableDelta(credentialAfter, await harness.snapshot('credential_recovery'), 0, 0);
  checks.archived_credential_lookup_only = true;

  harness.step('authority-reference-unbound');
  await open(projects.referenced);
  const referencedBefore = await harness.snapshot('referenced'), referencedModel = referencedBefore.current.models.find((row) => row.present);
  need(referencedModel, 'PROJECT_MODELS_AUTHORITY_REFERENCE_TARGET_MISSING');
  await observe('listProjectModels', projects.referenced.id, 'GET', 'models', 200, () => button(page, '项目 Models（全部 Providers）').click(), 'limit=25');
  await observe('getProjectModel', projects.referenced.id, 'GET', 'models/' + referencedModel.id, 200, () => button(page, '读取 Model ' + referencedModel.id).click());
  const modelDialog = page.getByRole('dialog').filter({ has: page.locator('#project-model-form') });
  await observe('listProjectAvailableChatModels', projects.referenced.id, 'GET', 'available-chat-models', 200, () => button(modelDialog, '删除 Model').click(), 'limit=25');
  const deletion = page.getByRole('dialog', { name: '删除 Model', exact: true });
  await button(deletion, '删除替代').click(); await page.getByRole('option', { name: '无替代', exact: true }).click();
  const refResult = await harness.ipc({ action: 'reference-fact', args: { project: 'referenced', state: 'present' } });
  need(refResult.project_id === projects.referenced.id && refResult.model_id === referencedModel.id && refResult.reference_present === true && refResult.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_FACT_INVALID');
  const referenced = await harness.snapshot('referenced');
  need(referenced.reference_presence.models.find((row) => row.id === referencedModel.id)?.present === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_FACT_MISSING');
  const rejection = await observe('deleteProjectModel', projects.referenced.id, 'DELETE', 'models/' + referencedModel.id, 503, () => button(deletion, '确认删除').click());
  need(rejection.code === 'DEPENDENCY_UNBOUND' && rejection.status === 503 && ['not_started', 'not_committed'].includes(String(rejection.commit_state)), 'PROJECT_MODELS_AUTHORITY_REFERENCE_REJECTION_INVALID');
  await expect(deletion.getByText('仍被使用的模型暂不能在此删除。没有执行引用迁移。', { exact: true })).toBeVisible();
  await expect(deletion.getByLabel('严格执行回执', { exact: true })).toHaveCount(0);
  const rejected = await harness.snapshot('referenced'); harness.durableDelta(referenced, rejected, 0, 0);
  need(rejected.current.models.find((row) => row.id === referencedModel.id)?.version === referencedModel.version && rejected.current.models.find((row) => row.id === referencedModel.id)?.present === true && rejected.reference_presence.models.find((row) => row.id === referencedModel.id)?.present === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_REWRITTEN');
  await button(deletion, '取消').click(); await expect(deletion).toBeHidden();
  await button(modelDialog, '取消').click(); await expect(modelDialog).toBeHidden();
  const removed = await harness.ipc({ action: 'reference-fact', args: { project: 'referenced', state: 'absent' } });
  need(removed.reference_present === false && removed.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_CLEANUP_INVALID');
  checks.reference_unbound = true;

  harness.step('authority-current-revocation');
  await open(projects.main);
  await observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, () => button(page, '读取 Provider ' + seed.id).click());
  dialog = providerDialog(); await dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill('Models Revoked Session Draft');
  const beforeRevocation = await harness.snapshot('main');
  const revoked = await harness.ipc({ action: 'logout', args: { session_id: ownerSession.sessionID } });
  need(revoked.session_id === ownerSession.sessionID && revoked.revoked === true, 'PROJECT_MODELS_AUTHORITY_REVOCATION_INVALID');
  const deniedCurrent = await observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 401, () => button(dialog, '重新读取 Provider').click());
  need(['SESSION_REVOKED', 'UNAUTHENTICATED'].includes(String(deniedCurrent.code)) && deniedCurrent.status === 401, 'PROJECT_MODELS_AUTHORITY_REVOCATION_RESPONSE_INVALID');
  await expect(page.getByRole('heading', { name: '会话尚未确认', exact: true })).toBeVisible();
  await expect(providerDialog()).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0);
  harness.durableDelta(beforeRevocation, await harness.snapshot('main'), 0, 0);
  checks.current_revocation = true;

  harness.step('authority-true-identity-change');
  await button(page, '检查当前会话').click();
  await expect(page.locator('#login-email')).toBeVisible();
  // Same-document form interactions preserve every earlier native observation.
  // Re-login as the same human still creates a genuinely different Session.
  // The live old-session draft here is Provider input. Credential tracking was
  // explicitly abandoned earlier; the later empty input is a fresh-form check.
  await privateLogin(page, actors.owner);
  const newOwnerSession = await sessionIdentity(page, () => pageshow(page));
  need(newOwnerSession.userID === ownerSession.userID && newOwnerSession.sessionID !== ownerSession.sessionID && newOwnerSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_NEW_SESSION_MISSING');
  await open(projects.main);
  await expect(providerDialog()).toHaveCount(0);
  await expect(page.getByLabel('原请求与历史观察', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('已创建凭据', { exact: true })).toHaveCount(0);
  await observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, () => button(page, '读取 Provider ' + seed.id).click());
  await expect(providerDialog().getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue('Models Seed Provider');
  await button(providerDialog(), '取消').click();
  await button(page, '创建凭据').click();
  need(await credentialDialog().getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === '', 'PROJECT_MODELS_AUTHORITY_NEW_SESSION_MATERIAL_LEAK');
  await button(credentialDialog().locator('footer'), '关闭').click();
  checks.true_identity_change = true;

  harness.step('authority-other-owner-and-admin');
  await button(page, '退出登录').click(); await expect(page.locator('#login-email')).toBeVisible();
  await privateLogin(page, actors.other_owner);
  const otherSession = await sessionIdentity(page, () => pageshow(page));
  need(otherSession.userID === actors.other_owner.user_id && otherSession.role === 'user' && otherSession.sessionID !== newOwnerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_OTHER_OWNER_INVALID');
  await denied(projects.main, true);
  await open(projects.other); await expect(button(page, '创建 Provider')).toBeEnabled();
  await button(page, '退出登录').click(); await expect(page.locator('#login-email')).toBeVisible();
  await privateLogin(page, actors.other_admin);
  const adminSession = await sessionIdentity(page, () => pageshow(page));
  need(adminSession.userID === actors.other_admin.user_id && adminSession.role === 'admin' && adminSession.sessionID !== otherSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ADMIN_INVALID');
  await denied(projects.main, true);
  await open(projects.admin_owned); await expect(button(page, '创建 Provider')).toBeEnabled();
  await expect(page.getByLabel('原请求与历史观察', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  checks.admin_owner_only = checks.other_owner_and_admin_rejected = true;
  const finalCounts = await harness.counts(), controls = object(finalCounts.controls);
  need(controls.armed === 4 && controls.claimed === 4 && controls.held === 2 && controls.held_joined === 2 && controls.cut === 1 && controls.disconnected === 1, 'PROJECT_MODELS_AUTHORITY_CONTROL_COUNTS_INVALID');
  for (const [operation, expected] of [['createProjectModelProvider', 2], ['createProjectModelCredential', 1], ['lookupProjectModelCredential', 1], ['deleteProjectModel', 1]] as const) need(operationCount(finalCounts, operation) === expected, 'PROJECT_MODELS_AUTHORITY_OPERATION_COUNTS_INVALID');
  for (const operation of ['updateProjectModelProvider', 'deleteProjectModelProvider', 'createProjectModel', 'updateProjectModel', 'updateProjectModelCredential', 'deleteProjectModelCredential', 'lookupProjectModelConfiguration']) need(operationCount(finalCounts, operation) === 0, 'PROJECT_MODELS_AUTHORITY_IMPLICIT_WRITE');
  harness.step('authority-same-body-finish');
  await harness.finish(page, checks);
}

