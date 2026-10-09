import { expect, type Locator, type Page } from '../../tests/account-captcha-web/node_modules/@playwright/test/index.js';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

type NativeFact = Readonly<{ token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; released: boolean; bytes: number }>;
export type ConfigCredentialSnapshot = {
  project: { project_id: string; version: string; initialized: boolean; lifecycle: string };
  current: { providers: { id: string; present: boolean; version: string | null; credential_ref: string | null }[]; models: { id: string; present: boolean; version: string | null; provider_id: string | null }[]; credentials: { credential_id: string; metadata: null | { credential_id: string; purpose: string; version: string } }[] };
  history: { configuration: Record<string, string>; credential: Record<string, string> };
  reference_presence: { models: { id: string; present: boolean }[]; credentials: { credential_id: string; present: boolean }[] };
  origins: { origin_token: string; original_request_token: string; operation: string; project_id: string; target_id: string | null; original_body_bytes: number; history: { family: 'configuration' | 'credential'; committed_rows: string; receipt?: Record<string, unknown> | null; result?: Record<string, unknown> | null }; comparison_count: number; comparison: null | Record<string, unknown> }[];
  fixture_only: { archive_recovery_applied: boolean; reference_fact_state: null | 'present' | 'absent'; rename_reuse_applied: boolean };
};
export type ConfigCredentialHarness = Readonly<{
  material: unknown;
  loginOwner(page: Page): Promise<void>;
  openProject(page: Page, project: 'main', leaf: 'model-providers' | 'available-models', discardPrepared?: boolean): Promise<void>;
  fillCredential(scope: Locator): Promise<void>;
  snapshot(project: 'main'): Promise<ConfigCredentialSnapshot>;
  strictReceipt(scope: Page | Locator): Promise<Record<string, unknown>>;
  durableDelta(before: ConfigCredentialSnapshot, after: ConfigCredentialSnapshot, configuration: number, credential: number): void;
  originalReplay(value: ConfigCredentialSnapshot, token: string): void;
  arm(operation: string, project: 'main', target_id: string | null, effect: string, query?: string | null): Promise<string>;
  control(id: string, ready: (value: Record<string, unknown>) => boolean): Promise<Record<string, unknown>>;
  actualLoss(page: Page, control: Record<string, unknown>, maximumObservedBytes: 0 | 1): Promise<void>;
  counts(): Promise<Record<string, unknown>>;
  nativeFacts(page: Page): Promise<readonly NativeFact[]>;
  finish(page: Page, mode: 'configuration' | 'credential', checks: Record<string, boolean>): Promise<void>;
  step(name: string): void;
}>;

const mutationOperations = ['createProjectModelProvider', 'updateProjectModelProvider', 'deleteProjectModelProvider', 'createProjectModel', 'updateProjectModel', 'deleteProjectModel', 'createProjectModelCredential', 'updateProjectModelCredential', 'deleteProjectModelCredential'] as const;
const button = (scope: Page | Locator, name: string) => scope.getByRole('button', { name, exact: true });
function need(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function object(value: unknown): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value), 'PROJECT_MODELS_LIFECYCLE_OBJECT_INVALID');
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const row = object(value);
  need(Object.keys(row).length === keys.length && keys.every((key) => Object.hasOwn(row, key)), 'PROJECT_MODELS_LIFECYCLE_MEMBERS_INVALID');
  return row;
}
function uuid(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value); }
function version(value: unknown): value is string { return typeof value === 'string' && /^[1-9][0-9]*$/.test(value) && BigInt(value) <= 9223372036854775807n; }
function caseMaterial(value: unknown, mode: 'configuration' | 'credential') {
  const material = exact(value, ['protocol', 'input_hash', 'mode', 'actors', 'projects', 'expected', 'system']);
  need(material.protocol === 'project-owner-models.v1' && material.mode === mode && material.system === null && /^[0-9a-f]{64}$/.test(String(material.input_hash)), 'PROJECT_MODELS_LIFECYCLE_MATERIAL_INVALID');
  const actors = exact(material.actors, ['owner', 'other_owner', 'other_admin']);
  for (const key of Object.keys(actors)) {
    const actor = exact(actors[key], ['email', 'password', 'user_id', 'username']);
    need(uuid(actor.user_id) && ['email', 'password', 'username'].every((field) => typeof actor[field] === 'string' && String(actor[field]).length > 0), 'PROJECT_MODELS_LIFECYCLE_ACTOR_INVALID');
  }
  const main = exact(exact(material.projects, ['main']).main, ['id', 'username', 'name', 'normalized_name', 'owner_user_id', 'initialized', 'lifecycle']);
  need(uuid(main.id) && main.owner_user_id === object(actors.owner).user_id && main.initialized === true && main.lifecycle === 'active' && ['username', 'name', 'normalized_name'].every((key) => typeof main[key] === 'string' && String(main[key]).length > 0), 'PROJECT_MODELS_LIFECYCLE_PROJECT_INVALID');
  const seeds = exact(exact(exact(material.expected, ['projects']).projects, ['main']).main, ['providers', 'models', 'credentials']);
  need(Array.isArray(seeds.providers) && seeds.providers.length === 1 && Array.isArray(seeds.models) && seeds.models.length === 0 && Array.isArray(seeds.credentials) && seeds.credentials.length === 0, 'PROJECT_MODELS_LIFECYCLE_SEEDS_INVALID');
  const seed = exact(seeds.providers[0], ['id', 'project_id', 'name', 'protocol', 'version', 'credential_ref']);
  need(uuid(seed.id) && seed.project_id === main.id && seed.name === 'Models Seed Provider' && seed.protocol === 'openai-chat-completions' && version(seed.version) && seed.credential_ref === null, 'PROJECT_MODELS_LIFECYCLE_PROVIDER_SEED_INVALID');
  return { projectID: main.id, seedID: seed.id, seedVersion: seed.version };
}
function operationCount(value: Record<string, unknown>, operation: string) {
  need(Array.isArray(value.operations), 'PROJECT_MODELS_LIFECYCLE_COUNTS_INVALID');
  const rows = value.operations.map(object).filter((row) => row.operation === operation);
  need(rows.length === 1 && Number.isSafeInteger(rows[0]!.browser) && Number(rows[0]!.browser) >= 0, 'PROJECT_MODELS_LIFECYCLE_COUNTS_INVALID');
  return Number(rows[0]!.browser);
}
function mutationDelta(before: Record<string, unknown>, after: Record<string, unknown>, expected: Partial<Record<(typeof mutationOperations)[number], number>>) {
  for (const operation of mutationOperations) need(operationCount(after, operation) - operationCount(before, operation) === (expected[operation] ?? 0), 'PROJECT_MODELS_LIFECYCLE_IMPLICIT_MUTATION');
}
function readOriginalBody(fact: NativeFact, operation: string, projectID: string) {
  const directory = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
  const matches = readdirSync(directory).filter((name) => /^response-\d+\.json$/.test(name)).map((name) => object(JSON.parse(readFileSync(join(directory, name), 'utf8')))).filter((row) => row.request_token === fact.token && row.source === 'browser');
  need(matches.length === 1, 'PROJECT_MODELS_LIFECYCLE_RESPONSE_MISSING');
  const row = matches[0]!;
  need(row.protocol === 'project-owner-models.v1' && row.input_hash === process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH && row.operation === operation && row.project_id === projectID && row.method === fact.method && row.endpoint === fact.path && row.query === fact.query && row.status === fact.status && row.body_stage === 'complete_formal_upstream' && /^[0-9a-f]{64}$/.test(String(row.body_sha256)) && row.body_file === `body-${row.body_sha256}.json`, 'PROJECT_MODELS_LIFECYCLE_RESPONSE_BINDING_INVALID');
  const bytes = readFileSync(join(directory, String(row.body_file)));
  need(bytes.length === row.body_bytes && bytes.length === fact.bytes && bytes.length <= 8388608 && createHash('sha256').update(bytes).digest('hex') === row.body_sha256, 'PROJECT_MODELS_LIFECYCLE_RESPONSE_BYTES_INVALID');
  return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)));
}
function observer(page: Page, harness: ConfigCredentialHarness, projectID: string) {
  const proved = new Set<string>();
  return async (operation: string, method: string, leaf: string, status: number, action: () => Promise<void>, query = '') => {
    const before = await harness.nativeFacts(page), path = `/api/v1/projects/${projectID}/${leaf}`;
    await action();
    let matches: readonly NativeFact[] = [];
    await expect.poll(async () => {
      const facts = await harness.nativeFacts(page);
      need(facts.length >= before.length, 'PROJECT_MODELS_LIFECYCLE_OBSERVATIONS_RESET');
      matches = facts.slice(before.length).filter((fact) => fact.method === method && fact.path === path);
      return matches.length === 1 && matches[0]!.status === status && matches[0]!.eof && matches[0]!.ended && matches[0]!.released;
    }).toBe(true);
    const fact = matches[0]!;
    need(fact.token !== null && /^r[0-9]{6}$/.test(fact.token) && !proved.has(fact.token) && fact.query === query, 'PROJECT_MODELS_LIFECYCLE_REQUEST_BINDING_INVALID');
    proved.add(fact.token);
    return readOriginalBody(fact, operation, projectID);
  };
}
async function definition(scope: Locator, label: string, value: string) {
  await expect(scope.locator('dt').filter({ hasText: new RegExp(`^${label}$`) }).locator('xpath=following-sibling::dd[1]')).toHaveText(value);
}
async function receipt(harness: ConfigCredentialHarness, scope: Page | Locator, expected: Record<string, unknown>) {
  await expect.poll(async () => {
    const value = await harness.strictReceipt(scope);
    return Object.keys(value).length === Object.keys(expected).length && Object.keys(expected).every((key) => value[key] === expected[key]);
  }).toBe(true);
}
function configurationReceipt(body: Record<string, unknown>, kind: string, resource?: string, expectedVersion?: string) {
  exact(body, ['kind', 'resource_id', 'version', 'affected_references']);
  need(body.kind === kind && uuid(body.resource_id) && version(body.version) && body.affected_references === '0' && (resource === undefined || body.resource_id === resource) && (expectedVersion === undefined || body.version === expectedVersion), 'PROJECT_MODELS_LIFECYCLE_CONFIGURATION_RECEIPT_INVALID');
  return { id: body.resource_id, version: body.version };
}
function credentialReceipt(body: Record<string, unknown>, deleted: boolean, resource?: string, expectedVersion?: string) {
  exact(body, ['credential_id', 'purpose', 'version', 'deleted']);
  need(uuid(body.credential_id) && body.purpose === 'model' && version(body.version) && body.deleted === deleted && (resource === undefined || body.credential_id === resource) && (expectedVersion === undefined || body.version === expectedVersion), 'PROJECT_MODELS_LIFECYCLE_CREDENTIAL_RECEIPT_INVALID');
  return { id: body.credential_id, version: body.version };
}
async function closeEditor(dialog: Locator, name = '取消') { await button(name === '关闭' ? dialog.locator('footer') : dialog, name).click(); await expect(dialog).toBeHidden(); }
async function providerDraft(page: Page, name: string, protocol = 'openai-chat-completions') {
  await button(page, '创建 Provider').click();
  const dialog = page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  await dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill(name);
  await dialog.getByRole('textbox', { name: 'Base URL', exact: true }).fill('https://model-ui.invalid/v1');
  if (protocol !== 'openai-chat-completions') {
    await button(dialog, '协议').click();
    await page.getByRole('option', { name: protocol, exact: true }).click();
  }
  return dialog;
}
function currentProvider(snapshot: ConfigCredentialSnapshot, id: string, expectedVersion: string, reference: string | null) {
  const rows = snapshot.current.providers.filter((row) => row.id === id);
  need(rows.length === 1 && rows[0]!.present && rows[0]!.version === expectedVersion && rows[0]!.credential_ref === reference, 'PROJECT_MODELS_LIFECYCLE_PROVIDER_FACT_INVALID');
}
function currentCredential(snapshot: ConfigCredentialSnapshot, id: string, expectedVersion: string | null, referenced: boolean) {
  const rows = snapshot.current.credentials.filter((row) => row.credential_id === id), refs = snapshot.reference_presence.credentials.filter((row) => row.credential_id === id);
  need(rows.length === 1 && (expectedVersion === null ? rows[0]!.metadata === null : rows[0]!.metadata?.credential_id === id && rows[0]!.metadata?.purpose === 'model' && rows[0]!.metadata?.version === expectedVersion) && refs.length === 1 && refs[0]!.present === referenced, 'PROJECT_MODELS_LIFECYCLE_CREDENTIAL_FACT_INVALID');
}

// These functions are registered by the shared spec. They never import it,
// replace fetch, mutate Vue state, or construct an alternative server/fixture.
export async function runConfigurationLifecycle(page: Page, harness: ConfigCredentialHarness) {
  const material = caseMaterial(harness.material, 'configuration'), checks: Record<string, boolean> = {};
  const observe = observer(page, harness, material.projectID);
  harness.step('configuration-login'); await harness.loginOwner(page);
  await observe('listProjectModelProviders', 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
  const initial = await harness.snapshot('main'), initialCounts = await harness.counts();
  currentProvider(initial, material.seedID, material.seedVersion, null);
  let latest = initial, writes = 0;
  const resources: { providerID: string; providerVersion: string; modelID: string; modelVersion: string; protocol: string; name: string }[] = [];
  async function committed(body: Record<string, unknown>, scope: Page | Locator, kind: string, id?: string, expectedVersion?: string) {
    const result = configurationReceipt(body, kind, id, expectedVersion);
    await receipt(harness, scope, body);
    const after = await harness.snapshot('main'); harness.durableDelta(latest, after, 1, 0); latest = after; writes++;
    return result;
  }
  async function providersView() {
    if (await page.getByRole('heading', { name: 'Providers', level: 1, exact: true }).count()) return observe('listProjectModelProviders', 'GET', 'model-providers', 200, () => button(page, '刷新 Providers').click(), 'limit=25');
    return observe('listProjectModelProviders', 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
  }
  async function modelsView() {
    return observe('listProjectModels', 'GET', 'models', 200, () => button(page, (awaitModelsOpen ? '刷新 Models' : '项目 Models（全部 Providers）')).click(), 'limit=25');
  }
  let awaitModelsOpen = false;
  async function refreshModels() {
    awaitModelsOpen = (await button(page, '项目 Models（全部 Providers）').getAttribute('aria-expanded')) === 'true';
    return modelsView();
  }
  async function providerEditor(id: string, protocol: string) {
    const body = await observe('getProjectModelProvider', 'GET', `model-providers/${id}`, 200, () => button(page, `读取 Provider ${id}`).click());
    const dialog = page.getByRole('dialog', { name: 'Provider 详情与编辑', exact: true });
    need(body.id === id && object(body.input).protocol === protocol, 'PROJECT_MODELS_LIFECYCLE_PROVIDER_READ_INVALID');
    await expect(dialog.getByRole('textbox', { name: '协议', exact: true })).toHaveAttribute('readonly', '');
    await expect(dialog.getByRole('textbox', { name: '协议', exact: true })).toHaveValue(protocol);
    await expect(button(dialog, '保存 Provider')).toBeDisabled();
    return dialog;
  }
  async function modelEditor(resource: (typeof resources)[number]) {
    const body = await observe('getProjectModel', 'GET', `models/${resource.modelID}`, 200, () => button(page, `读取 Model ${resource.modelID}`).click());
    const dialog = page.getByRole('dialog', { name: 'Model 详情与编辑', exact: true });
    need(body.id === resource.modelID && body.provider_id === resource.providerID && object(body.input).type === 'chat', 'PROJECT_MODELS_LIFECYCLE_MODEL_READ_INVALID');
    await definition(dialog, 'Provider ID', resource.providerID); await definition(dialog, '类型', 'chat（只读）'); await definition(dialog, '协议', resource.protocol);
    await expect(dialog.getByRole('textbox', { name: /^(Provider|Provider ID|类型)$/ })).toHaveCount(0);
    await expect(dialog.getByRole('button', { name: /^(Provider|Provider ID|类型)$/ })).toHaveCount(0);
    await expect(button(dialog, '保存 Model')).toBeDisabled();
    return dialog;
  }
  async function available(expected: readonly string[]) {
    const body = await observe('listProjectAvailableChatModels', 'GET', 'available-chat-models', 200, () => harness.openProject(page, 'main', 'available-models'), 'limit=25');
    exact(body, ['items', 'next_cursor']);
    need(Array.isArray(body.items) && body.next_cursor === null && body.items.length === expected.length, 'PROJECT_MODELS_LIFECYCLE_CATALOG_SIZE_INVALID');
    const ids = body.items.map((item) => {
      const row = exact(item, ['id', 'provider_id', 'scope', 'name', 'provider_name', 'version', 'capabilities']);
      const resource = resources.find((resource) => resource.modelID === row.id), scope = exact(row.scope, ['kind', 'project_id']);
      need(resource && row.provider_id === resource.providerID && row.version === resource.modelVersion && scope.kind === 'project' && scope.project_id === material.projectID, 'PROJECT_MODELS_LIFECYCLE_CATALOG_TARGET_INVALID');
      return row.id;
    });
    need(new Set(ids).size === ids.length && expected.every((id) => ids.includes(id)), 'PROJECT_MODELS_LIFECYCLE_CATALOG_CONTENT_INVALID');
    for (const resource of resources) await expect(page.getByRole('list', { name: '可用模型目录', exact: true }).locator(`[data-model-id="${resource.modelID}"]`)).toHaveCount(expected.includes(resource.modelID) ? 1 : 0);
  }

  for (const [index, protocol] of ['openai-chat-completions', 'anthropic-messages'].entries()) {
    harness.step(`configuration-protocol-${index + 1}`);
    let dialog = await providerDraft(page, `Models Lifecycle Provider ${index + 1}`, protocol);
    const provider = await committed(await observe('createProjectModelProvider', 'POST', 'model-providers', 200, () => button(dialog, '保存 Provider').click()), dialog, 'provider.create', undefined, '1');
    currentProvider(latest, provider.id, provider.version, null); await closeEditor(dialog); await providersView();
    await observe('getProjectModelProvider', 'GET', `model-providers/${provider.id}`, 200, () => button(page, `为 Provider ${provider.id} 创建 Model`).click());
    dialog = page.getByRole('dialog').filter({ has: page.locator('#project-model-form') });
    await definition(dialog, 'Provider ID', provider.id); await definition(dialog, '类型', 'chat（只读）'); await definition(dialog, '协议', protocol);
    await expect(dialog.getByRole('group', { name: '结构化输出', exact: true }).getByRole('checkbox', { name: 'json_schema', exact: true })).toHaveCount(protocol === 'openai-chat-completions' ? 1 : 0);
    const name = `Models Lifecycle Chat ${index + 1}`;
    await dialog.getByRole('textbox', { name: 'Model 名称', exact: true }).fill(name);
    await dialog.getByRole('textbox', { name: '原生型号', exact: true }).fill(`models-fixture-lifecycle-${index + 1}`);
    const model = await committed(await observe('createProjectModel', 'POST', 'models', 200, () => button(dialog, '保存 Model').click()), dialog, 'model.create', undefined, '1');
    need(latest.current.models.some((row) => row.id === model.id && row.present && row.provider_id === provider.id && row.version === model.version), 'PROJECT_MODELS_LIFECYCLE_MODEL_FACT_INVALID');
    resources.push({ providerID: provider.id, providerVersion: provider.version, modelID: model.id, modelVersion: model.version, protocol, name });
    await closeEditor(dialog);
  }
  checks.two_chat_protocols = true;
  await available(resources.map((resource) => resource.modelID)); await providersView(); await refreshModels();
  for (const resource of resources) {
    const dialog = await modelEditor(resource);
    await dialog.getByRole('textbox', { name: 'Model 名称', exact: true }).fill(resource.name + ' Updated');
    await dialog.getByRole('switch', { name: '启用状态', exact: true }).uncheck();
    resource.modelVersion = (await committed(await observe('updateProjectModel', 'PUT', `models/${resource.modelID}`, 200, () => button(dialog, '保存 Model').click()), dialog, 'model.update', resource.modelID, '2')).version;
    await closeEditor(dialog);
  }
  await refreshModels();
  for (const resource of resources) await expect(page.getByRole('list', { name: '项目 Models 列表', exact: true }).locator(`[data-model-id="${resource.modelID}"]`).getByText('禁用', { exact: true })).toBeVisible();
  await available([]); await providersView(); await refreshModels();
  for (const resource of resources) {
    const dialog = await modelEditor(resource);
    await expect(dialog.getByRole('textbox', { name: 'Model 名称', exact: true })).toHaveValue(resource.name + ' Updated');
    await dialog.getByRole('switch', { name: '启用状态', exact: true }).check();
    resource.modelVersion = (await committed(await observe('updateProjectModel', 'PUT', `models/${resource.modelID}`, 200, () => button(dialog, '保存 Model').click()), dialog, 'model.update', resource.modelID, '3')).version;
    await closeEditor(dialog);
  }
  await available(resources.map((resource) => resource.modelID)); await providersView();
  for (const resource of resources) {
    const dialog = await providerEditor(resource.providerID, resource.protocol);
    await dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill(`Models Lifecycle Provider ${resource.protocol} Updated`);
    await dialog.getByRole('switch', { name: '启用状态', exact: true }).uncheck();
    resource.providerVersion = (await committed(await observe('updateProjectModelProvider', 'PUT', `model-providers/${resource.providerID}`, 200, () => button(dialog, '保存 Provider').click()), dialog, 'provider.update', resource.providerID, '2')).version;
    await closeEditor(dialog);
  }
  await providersView();
  for (const resource of resources) await expect(page.getByRole('list', { name: 'Providers 列表', exact: true }).locator(`[data-provider-id="${resource.providerID}"]`).getByText('禁用', { exact: true })).toBeVisible();
  await refreshModels();
  for (const resource of resources) await expect(page.getByRole('list', { name: '项目 Models 列表', exact: true }).locator(`[data-model-id="${resource.modelID}"]`).getByText('启用', { exact: true })).toBeVisible();
  await available([]); await providersView();
  for (const resource of resources) {
    const dialog = await providerEditor(resource.providerID, resource.protocol);
    await dialog.getByRole('switch', { name: '启用状态', exact: true }).check();
    resource.providerVersion = (await committed(await observe('updateProjectModelProvider', 'PUT', `model-providers/${resource.providerID}`, 200, () => button(dialog, '保存 Provider').click()), dialog, 'provider.update', resource.providerID, '3')).version;
    await closeEditor(dialog);
  }
  await available(resources.map((resource) => resource.modelID));
  checks.immutable_provider_protocol_and_model_provider_type = checks.enabled_and_catalog_separate = true;

  harness.step('configuration-delete-boundaries'); await providersView();
  let dialog = await providerEditor(material.seedID, 'openai-chat-completions');
  await button(dialog, '删除 Provider').click();
  let deletion = page.getByRole('dialog', { name: '删除 Provider', exact: true });
  await definition(deletion, '稳定 ID', material.seedID); await definition(deletion, '捕获版本', material.seedVersion);
  const deletedBody = await observe('deleteProjectModelProvider', 'DELETE', `model-providers/${material.seedID}`, 200, () => button(deletion, '确认删除').click());
  await expect(deletion).toBeHidden();
  await committed(deletedBody, page, 'provider.delete', material.seedID, (BigInt(material.seedVersion) + 1n).toString());
  need(latest.current.providers.find((row) => row.id === material.seedID)?.present === false, 'PROJECT_MODELS_LIFECYCLE_EMPTY_PROVIDER_PRESENT');
  checks.empty_provider_delete = true;
  await providersView();
  const occupied = resources[0]!;
  dialog = await providerEditor(occupied.providerID, occupied.protocol); await button(dialog, '删除 Provider').click();
  deletion = page.getByRole('dialog', { name: '删除 Provider', exact: true });
  const rejected = await observe('deleteProjectModelProvider', 'DELETE', `model-providers/${occupied.providerID}`, 409, () => button(deletion, '确认删除').click());
  need(rejected.code === 'INVALID_STATE', 'PROJECT_MODELS_LIFECYCLE_PROVIDER_DELETE_WRONG_PROBLEM');
  await expect(deletion.getByText(/本次请求被明确拒绝/)).toBeVisible(); await expect(deletion.getByLabel('严格执行回执', { exact: true })).toHaveCount(0);
  const afterRejected = await harness.snapshot('main'); harness.durableDelta(latest, afterRejected, 0, 0);
  for (const resource of resources) {
    currentProvider(afterRejected, resource.providerID, resource.providerVersion, null);
    need(afterRejected.current.models.some((row) => row.id === resource.modelID && row.present && row.provider_id === resource.providerID && row.version === resource.modelVersion) && afterRejected.reference_presence.models.find((row) => row.id === resource.modelID)?.present === false, 'PROJECT_MODELS_LIFECYCLE_MODEL_FINAL_INVALID');
  }
  await closeEditor(deletion); await closeEditor(dialog);
  checks.provider_with_models_rejected = true;
  harness.durableDelta(initial, afterRejected, writes, 0);
  need(writes === 13 && afterRejected.origins.length === 0 && afterRejected.current.providers.length === 3 && afterRejected.current.models.length === 2 && afterRejected.current.credentials.length === 0, 'PROJECT_MODELS_LIFECYCLE_UNIQUE_FACTS_INVALID');
  mutationDelta(initialCounts, await harness.counts(), { createProjectModelProvider: 2, updateProjectModelProvider: 4, deleteProjectModelProvider: 2, createProjectModel: 2, updateProjectModel: 4 });
  checks.owner_formal_commands = checks.unique_durable_facts = true;
  harness.step('configuration-same-body-finish'); await harness.finish(page, 'configuration', checks);
}

export async function runCredentialLifecycle(page: Page, harness: ConfigCredentialHarness) {
  const material = caseMaterial(harness.material, 'credential'), checks: Record<string, boolean> = {};
  const observe = observer(page, harness, material.projectID);
  harness.step('credential-login'); await harness.loginOwner(page);
  await observe('listProjectModelProviders', 'GET', 'model-providers', 200, () => harness.openProject(page, 'main', 'model-providers'), 'limit=25');
  const initial = await harness.snapshot('main'), initialCounts = await harness.counts();
  currentProvider(initial, material.seedID, material.seedVersion, null);
  const credentialDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-credential-form') });
  const materialInput = (dialog: Locator) => dialog.getByLabel(/^新凭据材料(?:\s*\*)?$/);
  async function materialCleared(dialog: Locator) {
    need(await materialInput(dialog).inputValue() === '', 'PROJECT_MODELS_LIFECYCLE_PRIVATE_INPUT_NOT_CLEARED');
    await expect(materialInput(dialog)).toHaveAttribute('type', 'password');
  }
  async function createCredential() {
    const before = await harness.snapshot('main'), beforeCounts = await harness.counts();
    await button(page, '创建凭据').click(); const dialog = credentialDialog();
    await harness.fillCredential(dialog);
    const body = await observe('createProjectModelCredential', 'POST', 'model-credentials', 200, () => button(dialog, '创建凭据').click());
    const created = credentialReceipt(body, false, undefined, '1'); await receipt(harness, dialog, body); await materialCleared(dialog);
    const prepared = dialog.getByLabel('已创建凭据安全引用', { exact: true });
    await definition(prepared, 'Credential ID', created.id); await definition(prepared, '本地绑定状态', '尚未绑定');
    const after = await harness.snapshot('main'); harness.durableDelta(before, after, 0, 1); currentCredential(after, created.id, '1', false);
    mutationDelta(beforeCounts, await harness.counts(), { createProjectModelCredential: 1 });
    await closeEditor(dialog, '关闭'); return { ...created, snapshot: after };
  }
  async function readProvider() {
    const body = await observe('getProjectModelProvider', 'GET', `model-providers/${material.seedID}`, 200, () => button(page, `读取 Provider ${material.seedID}`).click());
    need(body.id === material.seedID && object(body.input).protocol === 'openai-chat-completions', 'PROJECT_MODELS_LIFECYCLE_PROVIDER_READ_INVALID');
    return page.getByRole('dialog', { name: 'Provider 详情与编辑', exact: true });
  }
  async function metadata(id: string, expectedVersion: string, action: () => Promise<void>) {
    const body = await observe('getProjectModelCredentialMetadata', 'GET', `model-credentials/${id}`, 200, action);
    exact(body, ['credential_id', 'purpose', 'version']);
    need(body.credential_id === id && body.purpose === 'model' && body.version === expectedVersion, 'PROJECT_MODELS_LIFECYCLE_METADATA_INVALID');
    const dialog = credentialDialog(); await definition(dialog, '用途', 'model'); await definition(dialog, '已读版本', expectedVersion); await materialCleared(dialog);
    await expect(button(dialog, '轮换凭据')).toBeDisabled(); return dialog;
  }

  harness.step('credential-create-and-explicit-bind');
  const created = await createCredential(); checks.create_safe_ref_only = true;
  let provider = await readProvider();
  await expect(provider.getByRole('textbox', { name: '凭据 ID', exact: true })).toHaveValue('');
  const beforeUse = await harness.counts(); await button(provider, '使用已创建凭据').click();
  await expect(provider.getByRole('textbox', { name: '凭据 ID', exact: true })).toHaveValue(created.id);
  mutationDelta(beforeUse, await harness.counts(), {}); harness.durableDelta(created.snapshot, await harness.snapshot('main'), 0, 0);
  const boundBody = await observe('updateProjectModelProvider', 'PUT', `model-providers/${material.seedID}`, 200, () => button(provider, '保存 Provider').click());
  const boundProvider = configurationReceipt(boundBody, 'provider.update', material.seedID, (BigInt(material.seedVersion) + 1n).toString()); await receipt(harness, provider, boundBody);
  const bound = await harness.snapshot('main'); harness.durableDelta(created.snapshot, bound, 1, 0); currentProvider(bound, material.seedID, boundProvider.version, created.id); currentCredential(bound, created.id, '1', true);
  await closeEditor(provider); checks.explicit_provider_bind = true;
  provider = await readProvider();
  let credential = await metadata(created.id, '1', () => button(provider, '管理凭据').click()); checks.metadata = true;
  harness.step('credential-rotate-and-reference-rejection');
  await harness.fillCredential(credential);
  const rotatedBody = await observe('updateProjectModelCredential', 'PUT', `model-credentials/${created.id}`, 200, () => button(credential, '轮换凭据').click());
  credentialReceipt(rotatedBody, false, created.id, '2'); await receipt(harness, credential, rotatedBody); await materialCleared(credential);
  const rotated = await harness.snapshot('main'); harness.durableDelta(bound, rotated, 0, 1); currentCredential(rotated, created.id, '2', true); currentProvider(rotated, material.seedID, boundProvider.version, created.id); checks.rotation = true;
  credential = await metadata(created.id, '2', () => button(credential, '读取凭据信息').click());
  await button(credential, '删除凭据').click();
  let deletion = page.getByRole('dialog', { name: '删除凭据', exact: true });
  await definition(deletion, '稳定 ID', created.id); await definition(deletion, '捕获版本', '2');
  const busy = await observe('deleteProjectModelCredential', 'DELETE', `model-credentials/${created.id}`, 409, () => button(deletion, '确认删除').click());
  need(busy.code === 'RESOURCE_BUSY', 'PROJECT_MODELS_LIFECYCLE_CREDENTIAL_DELETE_WRONG_PROBLEM');
  await expect(deletion.getByText(/本次请求被明确拒绝/)).toBeVisible(); await expect(deletion.getByLabel('严格执行回执', { exact: true })).toHaveCount(0);
  const rejected = await harness.snapshot('main'); harness.durableDelta(rotated, rejected, 0, 0); currentCredential(rejected, created.id, '2', true); currentProvider(rejected, material.seedID, boundProvider.version, created.id);
  await closeEditor(deletion); await closeEditor(credential, '关闭'); await closeEditor(provider); checks.referenced_delete_rejected = true;

  harness.step('credential-explicit-unbind-and-delete'); provider = await readProvider();
  await provider.getByRole('textbox', { name: '凭据 ID', exact: true }).fill('');
  const unboundBody = await observe('updateProjectModelProvider', 'PUT', `model-providers/${material.seedID}`, 200, () => button(provider, '保存 Provider').click());
  const unboundProvider = configurationReceipt(unboundBody, 'provider.update', material.seedID, (BigInt(boundProvider.version) + 1n).toString()); await receipt(harness, provider, unboundBody);
  const unbound = await harness.snapshot('main'); harness.durableDelta(rejected, unbound, 1, 0); currentProvider(unbound, material.seedID, unboundProvider.version, null); currentCredential(unbound, created.id, '2', false);
  await closeEditor(provider); await button(page, '管理凭据').click(); credential = credentialDialog();
  await credential.getByRole('textbox', { name: 'Credential ID', exact: true }).fill(created.id);
  credential = await metadata(created.id, '2', () => button(credential, '读取凭据信息').click());
  await button(credential, '删除凭据').click(); deletion = page.getByRole('dialog', { name: '删除凭据', exact: true });
  const deletedBody = await observe('deleteProjectModelCredential', 'DELETE', `model-credentials/${created.id}`, 200, () => button(deletion, '确认删除').click());
  credentialReceipt(deletedBody, true, created.id, '3'); await expect(deletion).toBeHidden(); await receipt(harness, credential, deletedBody);
  const deleted = await harness.snapshot('main'); harness.durableDelta(unbound, deleted, 0, 1); currentCredential(deleted, created.id, null, false); currentProvider(deleted, material.seedID, unboundProvider.version, null);
  await closeEditor(credential, '关闭'); checks.explicit_unbind_delete = true;

  // The only selected origin in this case is the final collection create.
  // No later independent create can become a false comparison candidate.
  harness.step('credential-partial-provider-loss');
  const retained = await createCredential(); need(retained.id !== created.id, 'PROJECT_MODELS_LIFECYCLE_CREDENTIAL_ID_REUSED');
  provider = await providerDraft(page, 'Models Credential Recovery Provider');
  const beforeUseRetained = await harness.counts(); await button(provider, '使用已创建凭据').click();
  await expect(provider.getByRole('textbox', { name: '凭据 ID', exact: true })).toHaveValue(retained.id);
  mutationDelta(beforeUseRetained, await harness.counts(), {});
  const armID = await harness.arm('createProjectModelProvider', 'main', null, 'after_complete_disconnect');
  await button(provider, '保存 Provider').click(); await expect(provider.getByText(/结果尚未确认/)).toBeVisible();
  const loss = await harness.control(armID, (value) => value.joined === true); await harness.actualLoss(page, loss, 0);
  await expect(provider.getByLabel('严格执行回执', { exact: true })).toHaveCount(0);
  await expect(provider.getByLabel('可选已创建凭据', { exact: true })).toContainText(retained.id);
  await expect(provider.getByLabel('可选已创建凭据', { exact: true })).toContainText('尚未绑定');
  await expect(provider.getByRole('textbox', { name: '凭据 ID', exact: true })).toHaveValue(retained.id);
  const lost = await harness.snapshot('main'); harness.durableDelta(retained.snapshot, lost, 1, 0); currentCredential(lost, retained.id, '1', true);
  const origin = lost.origins.find((row) => row.origin_token === loss.origin_token);
  need(lost.origins.length === 1 && origin && origin.operation === 'createProjectModelProvider' && origin.target_id === null && origin.history.family === 'configuration' && origin.history.committed_rows === '1' && origin.history.receipt && origin.comparison === null && origin.comparison_count === 0, 'PROJECT_MODELS_LIFECYCLE_PARTIAL_ORIGIN_INVALID');
  const original = configurationReceipt(origin.history.receipt, 'provider.create', undefined, '1'); currentProvider(lost, original.id, '1', retained.id);
  checks.partial_success_retained = true;
  const beforeReplayCounts = await harness.counts();
  harness.step('credential-original-provider-only-replay');
  const replayBody = await observe('createProjectModelProvider', 'POST', 'model-providers', 200, () => button(provider, '按原请求重放').click());
  configurationReceipt(replayBody, 'provider.create', original.id, '1'); await receipt(harness, provider, replayBody);
  const replayed = await harness.snapshot('main'); harness.durableDelta(lost, replayed, 0, 0); harness.originalReplay(replayed, String(loss.origin_token));
  currentCredential(replayed, retained.id, '1', true); currentCredential(replayed, created.id, null, false); currentProvider(replayed, original.id, '1', retained.id);
  mutationDelta(beforeReplayCounts, await harness.counts(), { createProjectModelProvider: 1 });
  await expect(provider.getByLabel('可选已创建凭据', { exact: true })).toContainText('已用于本地确认的 Provider 保存');
  await closeEditor(provider); checks.original_provider_only_recovery = true;
  harness.durableDelta(initial, replayed, 3, 4);
  need(replayed.origins.length === 1 && replayed.current.providers.length === 2 && replayed.current.models.length === 0 && replayed.current.credentials.length === 2, 'PROJECT_MODELS_LIFECYCLE_UNIQUE_FACTS_INVALID');
  const finalCounts = await harness.counts();
  mutationDelta(initialCounts, finalCounts, { createProjectModelProvider: 2, updateProjectModelProvider: 2, createProjectModelCredential: 2, updateProjectModelCredential: 1, deleteProjectModelCredential: 2 });
  const controls = exact(finalCounts.controls, ['armed', 'claimed', 'held', 'held_joined', 'cut', 'disconnected']);
  need(controls.armed === 1 && controls.claimed === 1 && controls.held === 0 && controls.held_joined === 0 && controls.cut === 0 && controls.disconnected === 1, 'PROJECT_MODELS_LIFECYCLE_CONTROLS_INVALID');
  harness.step('credential-same-body-finish'); await harness.finish(page, 'credential', checks);
}
