import { expect, type Locator, type Page, type Request } from '../../tests/account-captcha-web/node_modules/@playwright/test/index.js';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { readPaginationExpectation } from './read-pagination-contract';

type NativeRead = Readonly<{ token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; bytes: number }>;
type ReadWitness = Readonly<{ fact: NativeRead; body: Record<string, unknown> }>;
export type ReadCaseHarness = Readonly<{
  material: unknown;
  loginOwner(page: Page): Promise<void>;
  openProject(page: Page, project: 'main' | 'second', leaf: 'model-providers' | 'available-models', discardPrepared?: boolean): Promise<void>;
  fillCredential(scope: Locator): Promise<void>;
  nativeFacts(page: Page): Promise<readonly NativeRead[]>;
  counts(): Promise<Record<string, unknown>>;
  finish(page: Page, checks: Record<string, boolean>): Promise<void>;
  step(name: string): void;
}>;
function need(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function object(value: unknown): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value), 'PROJECT_MODELS_READ_OBJECT_REQUIRED');
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const row = object(value);
  need(Object.keys(row).length === keys.length && keys.every((key) => Object.hasOwn(row, key)), 'PROJECT_MODELS_READ_MEMBERS_INVALID');
  return row;
}
const button = (scope: Page | Locator, name: string) => scope.getByRole('button', { name, exact: true });
const id = (value: unknown): value is string => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value);
function sameIDs(actual: readonly string[], expected: readonly string[]) {
  need(actual.length === expected.length && new Set(actual).size === actual.length && actual.every((value) => expected.includes(value)), 'PROJECT_MODELS_READ_ID_COVERAGE_INVALID');
}
function operationCount(value: Record<string, unknown>, name: string) {
  need(Array.isArray(value.operations), 'PROJECT_MODELS_READ_COUNTS_INVALID');
  const row = value.operations.map(object).find((row) => row.operation === name);
  need(row && Number.isSafeInteger(row.browser) && Number(row.browser) >= 0, 'PROJECT_MODELS_READ_COUNTS_INVALID');
  return Number(row.browser);
}
async function pageIDs(list: Locator, attribute: string, length: number) {
  const items = list.locator(`[${attribute}]`);
  await expect(items).toHaveCount(length);
  const values = await items.evaluateAll((nodes, attribute) => nodes.map((node) => node.getAttribute(attribute)), attribute);
  need(values.every(id), 'PROJECT_MODELS_READ_DOM_ID_INVALID');
  return values;
}
function pageQuery(cursor: unknown = null) {
  const query = new URLSearchParams();
  if (cursor !== null) { need(typeof cursor === 'string' && cursor.length > 0, 'PROJECT_MODELS_READ_CURSOR_MISSING'); query.set('cursor', cursor); }
  query.set('limit', '25'); return query.toString();
}
async function orderedPage(list: Locator, attribute: string, witness: ReadWitness) {
  const body = exact(witness.body, ['items', 'next_cursor']);
  need(Array.isArray(body.items), 'PROJECT_MODELS_READ_PAGE_ITEMS_INVALID');
  const rows = body.items.map(object), expected = rows.map((row) => row.id);
  need(expected.every(id), 'PROJECT_MODELS_READ_BODY_ID_INVALID');
  const actual = await pageIDs(list, attribute, expected.length);
  need(actual.every((value, index) => value === expected[index]), 'PROJECT_MODELS_READ_DOM_BODY_ORDER_INVALID');
  return { ids: actual, rows, cursor: body.next_cursor };
}
async function twoPages(page: Page, listName: string, paginationName: string, attribute: string, nextName: string, previousName: string, firstWitness: ReadWitness, read: (query: string, action: () => Promise<void>) => Promise<ReadWitness>, showRows?: (rows: readonly Record<string, unknown>[]) => Promise<void>) {
  const list = page.getByRole('list', { name: listName, exact: true });
  const pagination = page.getByLabel(paginationName, { exact: true });
  const firstPage = await orderedPage(list, attribute, firstWitness), first = firstPage.ids;
  if (showRows) await showRows(firstPage.rows);
  need(first.length === 25 && firstPage.cursor !== null, 'PROJECT_MODELS_READ_FIRST_PAGE_BOUNDARY_INVALID');
  await expect(pagination.getByText('第 1 页', { exact: true })).toBeVisible();
  await expect(button(pagination, previousName)).toBeDisabled();
  const secondWitness = await read(pageQuery(firstPage.cursor), () => button(pagination, nextName).click());
  const secondPage = await orderedPage(list, attribute, secondWitness), second = secondPage.ids;
  if (showRows) await showRows(secondPage.rows);
  need(second.length === 1 && secondPage.cursor === null, 'PROJECT_MODELS_READ_SECOND_PAGE_BOUNDARY_INVALID');
  await expect(pagination.getByText('第 2 页', { exact: true })).toBeVisible();
  await expect(button(pagination, nextName)).toBeDisabled();
  const previousWitness = await read(firstWitness.fact.query, () => button(pagination, previousName).click());
  const previous = await orderedPage(list, attribute, previousWitness), returned = previous.ids;
  if (showRows) await showRows(previous.rows);
  need(previous.cursor !== null, 'PROJECT_MODELS_READ_PREVIOUS_PAGE_CURSOR_MISSING');
  need(returned.length === first.length && returned.every((value, index) => value === first[index]), 'PROJECT_MODELS_READ_PREVIOUS_PAGE_CHANGED');
  return { first, second, all: [...first, ...second], rows: [...firstPage.rows, ...secondPage.rows], previous };
}

// These are original admitted upstream bytes. The parent finish helper later
// validates every native EOF against these bytes through both formal schema
// and the real browser client; this reader does not synthesize a DTO sample.
function safeBodies(operation: string, project: string) {
  const evidence = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
  const inputHash = process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH!;
  const rows: { metadata: Record<string, unknown>; body: Record<string, unknown> }[] = [];
  for (const name of readdirSync(evidence).filter((name) => /^response-\d+\.json$/.test(name))) {
    const metadata = object(JSON.parse(readFileSync(join(evidence, name), 'utf8')));
    if (metadata.source !== 'browser' || metadata.operation !== operation || metadata.project_id !== project || metadata.status !== 200) continue;
    need(metadata.protocol === 'project-owner-models.v1' && metadata.input_hash === inputHash && metadata.method === 'GET' && metadata.body_stage === 'complete_formal_upstream' && /^[0-9a-f]{64}$/.test(String(metadata.body_sha256)) && metadata.body_file === `body-${metadata.body_sha256}.json`, 'PROJECT_MODELS_READ_BODY_BINDING_INVALID');
    const bytes = readFileSync(join(evidence, String(metadata.body_file)));
    need(bytes.length === metadata.body_bytes && bytes.length <= 8388608 && createHash('sha256').update(bytes).digest('hex') === metadata.body_sha256, 'PROJECT_MODELS_READ_BODY_BYTES_INVALID');
    rows.push({ metadata, body: object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes))) });
  }
  need(rows.length > 0, 'PROJECT_MODELS_READ_BODY_MISSING');
  return rows;
}
async function observedRead(page: Page, harness: ReadCaseHarness, proved: Set<string>, operation: string, project: string, path: string, query: string, action: () => Promise<void>): Promise<ReadWitness> {
  const before = await harness.nativeFacts(page);
  await action();
  let matches: readonly NativeRead[] = [];
  await expect.poll(async () => {
    const after = await harness.nativeFacts(page);
    need(after.length >= before.length, 'PROJECT_MODELS_READ_OBSERVATIONS_RESET');
    matches = after.slice(before.length).filter((fact) => fact.method === 'GET' && fact.path === path);
    return matches.length === 1 && matches[0]!.status === 200 && matches[0]!.eof && matches[0]!.ended;
  }).toBe(true);
  const fact = matches[0]!;
  need(fact.token !== null && /^r[0-9]{6}$/.test(fact.token) && !proved.has(fact.token) && fact.query === query, 'PROJECT_MODELS_READ_NEW_REQUEST_BINDING_INVALID');
  const candidates = safeBodies(operation, project).filter(({ metadata }) => metadata.request_token === fact.token);
  need(candidates.length === 1 && candidates[0]!.metadata.endpoint === path && candidates[0]!.metadata.query === query && candidates[0]!.metadata.body_bytes === fact.bytes, 'PROJECT_MODELS_READ_NATIVE_BODY_BINDING_INVALID');
  proved.add(fact.token);
  return { fact, body: candidates[0]!.body };
}
async function definition(scope: Locator, label: string, value: string) {
  await expect(scope.locator('dt').filter({ hasText: new RegExp(`^${label}$`) }).locator('xpath=following-sibling::dd[1]')).toHaveText(value);
}
async function directoryRows(page: Page, rows: readonly Record<string, unknown>[], expected: ReturnType<typeof readPaginationExpectation>['available']) {
  for (const row of rows) {
    exact(row, ['id', 'provider_id', 'scope', 'name', 'provider_name', 'version', 'capabilities']);
    const reference = expected.find((item) => item.id === row.id), scope = object(row.scope);
    need(reference && row.provider_id === reference.provider_id && row.name === reference.name && row.provider_name === reference.provider_name && row.version === reference.version && scope.kind === reference.scope.kind && (reference.scope.kind === 'system' ? Object.keys(scope).length === 1 : Object.keys(scope).length === 2 && scope.project_id === reference.scope.project_id), 'PROJECT_MODELS_READ_DIRECTORY_REFERENCE_INVALID');
    const item = page.getByRole('list', { name: '可用模型目录', exact: true }).locator(`[data-model-id="${row.id}"]`);
    await expect(item.getByRole('heading', { name: reference.name, exact: true })).toBeVisible();
    await definition(item, '来源', reference.scope.kind === 'system' ? 'System（只读）' : 'Project（本项目）');
    await definition(item, 'Model ID', reference.id); await definition(item, 'Provider', reference.provider_name);
    await definition(item, 'Provider ID', reference.provider_id); await definition(item, '版本', reference.version);
  }
}

// Register this function under the formal [read] case in the shared spec.
// Every dependency is the existing real harness/UI path; no alternate server,
// fetch substitution, state mutation, request fulfillment or fixture is used.
export async function runReadAndPagination(page: Page, harness: ReadCaseHarness) {
  const material = object(harness.material), projects = object(material.projects), main = object(projects.main), second = object(projects.second);
  need(material.mode === 'read' && id(main.id) && id(second.id) && main.id !== second.id, 'PROJECT_MODELS_READ_CASE_INVALID');
  const expected = object(material.expected), seeds = object(object(expected.projects).main);
  const pagination = readPaginationExpectation(expected.pagination, main.id, seeds);
  const checks: Record<string, boolean> = {};
  const proved = new Set<string>();
  const mainID = main.id;
  const read = (operation: string, project: string, path: string, query: string, action: () => Promise<void>) => observedRead(page, harness, proved, operation, project, path, query, action);
  const providersRead = (query: string, action: () => Promise<void>) => read('listProjectModelProviders', mainID, `/api/v1/projects/${mainID}/model-providers`, query, action);
  const modelsRead = (query: string, action: () => Promise<void>) => read('listProjectModels', mainID, `/api/v1/projects/${mainID}/models`, query, action);
  const availableRead = (query: string, action: () => Promise<void>) => read('listProjectAvailableChatModels', mainID, `/api/v1/projects/${mainID}/available-chat-models`, query, action);
  let systemBypass = false;
  const observe = (request: Request) => {
    const path = new URL(request.url()).pathname;
    if (/^\/api\/v1\/system\/(model-providers|models)(?:\/|$)/.test(path)) systemBypass = true;
  };
  page.on('request', observe);
  try {
    harness.step('read-login');
    await harness.loginOwner(page);
    const initialProviders = await providersRead(pageQuery(), () => harness.openProject(page, 'main', 'model-providers'));
    harness.step('read-provider-pages');
    const providers = await twoPages(page, 'Providers 列表', 'Providers 分页', 'data-provider-id', '下一页 Providers', '上一页 Providers', initialProviders, providersRead);
    sameIDs(providers.all, pagination.provider_ids);
    checks.providers_26 = true;
    const providerID = providers.first[0]!;
    const providerDetail = await read('getProjectModelProvider', mainID, `/api/v1/projects/${mainID}/model-providers/${providerID}`, '', () => button(page, `读取 Provider ${providerID}`).click());
    need(providerDetail.body.id === providerID && object(providerDetail.body.scope).kind === 'project' && object(providerDetail.body.scope).project_id === mainID, 'PROJECT_MODELS_READ_PROVIDER_DETAIL_TARGET_INVALID');
    let dialog = page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
    await expect(dialog.getByRole('textbox', { name: '协议', exact: true })).toHaveAttribute('readonly', '');
    await expect(dialog.getByRole('textbox', { name: 'Base URL', exact: true })).toHaveValue('https://model-ui.invalid/v1');
    await expect(dialog.getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue(String(object(providerDetail.body.input).name));
    await definition(dialog, 'Provider ID', providerID); await definition(dialog, '当前编辑版本', String(providerDetail.body.version));
    await button(dialog, '取消').click(); await expect(dialog).toBeHidden();

    harness.step('read-model-pages');
    const initialModels = await modelsRead(pageQuery(), () => button(page, '项目 Models（全部 Providers）').click());
    const models = await twoPages(page, '项目 Models 列表', 'Models 分页', 'data-model-id', '下一页 Models', '上一页 Models', initialModels, modelsRead);
    sameIDs(models.all, pagination.model_ids);
    const modelRows = (seeds.models as unknown[]).map(object);
    need(new Set(modelRows.filter((row) => models.all.includes(String(row.id))).map((row) => row.provider_id)).size === 2, 'PROJECT_MODELS_READ_PROVIDER_COVERAGE_INVALID');
    checks.models_26_cross_two_providers = true;
    const disabledID = models.all.find((model) => !pagination.available.some((item) => item.id === model));
    need(disabledID && models.first.includes(disabledID), 'PROJECT_MODELS_READ_DISABLED_MODEL_MISSING');
    await expect(page.locator(`[data-model-id="${disabledID}"]`).getByText('禁用', { exact: true })).toBeVisible();
    const modelDetail = await read('getProjectModel', mainID, `/api/v1/projects/${mainID}/models/${disabledID}`, '', () => button(page, `读取 Model ${disabledID}`).click());
    need(modelDetail.body.id === disabledID && object(modelDetail.body.scope).kind === 'project' && object(modelDetail.body.scope).project_id === mainID && modelDetail.body.provider_id === modelRows.find((row) => row.id === disabledID)!.provider_id && object(modelDetail.body.input).enabled === false, 'PROJECT_MODELS_READ_MODEL_DETAIL_TARGET_INVALID');
    dialog = page.getByRole('dialog').filter({ has: page.locator('#project-model-form') });
    await expect(dialog.getByText('chat（只读）', { exact: true })).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Model 名称', exact: true })).toHaveValue(String(modelRows.find((row) => row.id === disabledID)!.name));
    await definition(dialog, 'Model ID', disabledID); await definition(dialog, 'Provider ID', String(modelDetail.body.provider_id)); await definition(dialog, '当前编辑版本', String(modelDetail.body.version));
    await button(dialog, '取消').click(); await expect(dialog).toBeHidden();

    harness.step('read-available-pages');
    const initialAvailable = await availableRead(pageQuery(), () => harness.openProject(page, 'main', 'available-models'));
    const available = await twoPages(page, '可用模型目录', '可用模型分页', 'data-model-id', '下一页可用模型', '上一页可用模型', initialAvailable, availableRead, (rows) => directoryRows(page, rows, pagination.available));
    sameIDs(available.all, pagination.available.map((row) => row.id));
    need(!available.all.includes(disabledID), 'PROJECT_MODELS_READ_DISABLED_CATALOG_ENTRY');
    const actualAvailable = available.rows.map((row) => exact(row, ['id', 'provider_id', 'scope', 'name', 'provider_name', 'version', 'capabilities']));
    need(actualAvailable.some((row) => object(row.scope).kind === 'system') && actualAvailable.some((row) => object(row.scope).kind === 'project' && object(row.scope).project_id === main.id), 'PROJECT_MODELS_READ_MIXED_SCOPE_MISSING');
    checks.available_26_mixed_scopes = checks.safe_seven_fields = true;

    // Explicit first-page recovery after a successful next-page observation.
    // Bad/oversized pages remain separate controlled tests, not fabricated PG.
    const nextAvailable = await availableRead(pageQuery(available.previous.cursor), () => button(page, '下一页可用模型').click());
    const nextAvailablePage = await orderedPage(page.getByRole('list', { name: '可用模型目录', exact: true }), 'data-model-id', nextAvailable);
    need(nextAvailablePage.ids.length === 1, 'PROJECT_MODELS_READ_REFRESH_START_INVALID');
    await directoryRows(page, nextAvailablePage.rows, pagination.available);
    const beforeRefresh = await harness.counts();
    const refreshWitness = await availableRead(pageQuery(), () => button(page, '刷新可用模型').click());
    const refreshedPage = await orderedPage(page.getByRole('list', { name: '可用模型目录', exact: true }), 'data-model-id', refreshWitness), refreshed = refreshedPage.ids;
    await directoryRows(page, refreshedPage.rows, pagination.available);
    need(refreshed.every((value, index) => value === available.first[index]), 'PROJECT_MODELS_READ_EXPLICIT_REFRESH_CHANGED');
    await expect(button(page, '上一页可用模型')).toBeDisabled();
    need(operationCount(await harness.counts(), 'listProjectAvailableChatModels') === operationCount(beforeRefresh, 'listProjectAvailableChatModels') + 1, 'PROJECT_MODELS_READ_REFRESH_REQUEST_COUNT_INVALID');
    checks.explicit_page_recovery = true;

    harness.step('read-safe-metadata');
    const secondProviders = await read('listProjectModelProviders', second.id, `/api/v1/projects/${second.id}/model-providers`, pageQuery(), () => harness.openProject(page, 'second', 'model-providers'));
    need(Array.isArray(secondProviders.body.items) && secondProviders.body.items.length === 0 && secondProviders.body.next_cursor === null, 'PROJECT_MODELS_READ_SECOND_PROJECT_BODY_INVALID');
    await expect(page.getByText('本页没有 Provider。可以在当前可编辑项目中创建 Provider。', { exact: true })).toBeVisible();
    const returnProviders = await providersRead(pageQuery(), () => harness.openProject(page, 'main', 'model-providers'));
    await orderedPage(page.getByRole('list', { name: 'Providers 列表', exact: true }), 'data-provider-id', returnProviders);
    await button(page, '创建凭据').click();
    dialog = page.getByRole('dialog').filter({ has: page.locator('#project-credential-form') });
    await harness.fillCredential(dialog);
    await button(dialog, '创建凭据').click();
    const receipt = dialog.getByLabel('严格执行回执', { exact: true }); await expect(receipt).toBeVisible();
    const created = exact(JSON.parse((await receipt.textContent())!), ['credential_id', 'purpose', 'version', 'deleted']);
    need(id(created.credential_id) && created.purpose === 'model' && created.deleted === false, 'PROJECT_MODELS_READ_CREDENTIAL_RECEIPT_INVALID');
    need(await dialog.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === '', 'PROJECT_MODELS_READ_MATERIAL_NOT_CLEARED');
    const metadataWitness = await read('getProjectModelCredentialMetadata', mainID, `/api/v1/projects/${mainID}/model-credentials/${created.credential_id}`, '', () => button(dialog, '读取凭据信息').click());
    await expect(button(dialog, '删除凭据')).toBeEnabled();
    const metadata = exact(metadataWitness.body, ['credential_id', 'purpose', 'version']);
    need(metadata.credential_id === created.credential_id && metadata.purpose === 'model' && metadata.version === created.version, 'PROJECT_MODELS_READ_METADATA_MISMATCH');
    await button(dialog, '关闭').click(); await expect(dialog).toBeHidden();
    checks.metadata_no_material = true;

    const facts = await harness.nativeFacts(page);
    const listPaths = ['/model-providers', '/models', '/available-chat-models'];
    const pages = facts.filter((fact) => fact.method === 'GET' && listPaths.some((suffix) => fact.path === `/api/v1/projects/${main.id}${suffix}`));
    need(pages.length >= 10 && pages.every((fact) => {
      const query = new URLSearchParams(fact.query), keys = [...query.keys()];
      return fact.token !== null && proved.has(fact.token) && fact.status === 200 && fact.eof && fact.ended && keys.length === new Set(keys).size && keys.every((key) => key === 'cursor' || key === 'limit') && query.get('limit') === '25' && (!query.has('cursor') || !!query.get('cursor'));
    }), 'PROJECT_MODELS_READ_QUERY_OR_EOF_INVALID');
    need(pages.some((fact) => new URLSearchParams(fact.query).has('cursor')) && !facts.some((fact) => new URLSearchParams(fact.query).has('provider_id')), 'PROJECT_MODELS_READ_CURSOR_OR_PROVIDER_FILTER_INVALID');
    need(!systemBypass, 'PROJECT_MODELS_READ_SYSTEM_BYPASS');
    const finalCounts = await harness.counts();
    for (const operation of ['createProjectModelProvider', 'updateProjectModelProvider', 'deleteProjectModelProvider', 'createProjectModel', 'updateProjectModel', 'deleteProjectModel', 'updateProjectModelCredential', 'deleteProjectModelCredential']) need(operationCount(finalCounts, operation) === 0, 'PROJECT_MODELS_READ_IMPLICIT_WRITE');
    need(operationCount(finalCounts, 'createProjectModelCredential') === 1 && operationCount(finalCounts, 'getProjectModelCredentialMetadata') === 1, 'PROJECT_MODELS_READ_METADATA_OPERATION_COUNT_INVALID');
    checks.cursor_limit_only = checks.no_provider_id = checks.no_system_detail_bypass = true;
    harness.step('read-same-body-finish');
    await harness.finish(page, checks);
  } finally { page.off('request', observe); }
}
