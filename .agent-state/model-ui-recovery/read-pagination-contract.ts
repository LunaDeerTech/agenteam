// Admission for the read-mode fixture's safe expectation subset. Actual page
// ordering, DTO/schema/client validation and browser reads remain separate.
export type DirectoryRef = Readonly<{
  id: string; provider_id: string;
  scope: Readonly<{ kind: 'system' } | { kind: 'project'; project_id: string }>;
  name: string; provider_name: string; version: string;
}>;
export type PaginationExpectation = Readonly<{
  provider_ids: readonly string[];
  model_ids: readonly string[];
  available: readonly DirectoryRef[];
}>;
const reject = () => { throw new Error('PROJECT_MODELS_PAGINATION_EXPECTATION_REJECTED'); };
function need(value: unknown): asserts value { if (!value) reject(); }
function shape(value: unknown, keys: readonly string[]): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value));
  const row = value as Record<string, unknown>;
  need(Object.keys(row).length === keys.length && keys.every((key) => Object.hasOwn(row, key)));
  return row;
}
const id = (value: unknown): value is string => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value);
const version = (value: unknown): value is string => typeof value === 'string' && /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n;
const name = (value: unknown): value is string => typeof value === 'string' && value.length > 0;

export function readPaginationExpectation(value: unknown, projectID: string, seedValue: unknown): PaginationExpectation {
  need(id(projectID));
  const seed = shape(seedValue, ['providers', 'models', 'credentials']);
  need(Array.isArray(seed.providers) && seed.providers.length === 26 && Array.isArray(seed.models) && seed.models.length === 26 && Array.isArray(seed.credentials));
  const providers = new Map<string, Record<string, unknown>>(), models = new Map<string, Record<string, unknown>>();
  const knownIDs = new Set<string>(), credentialIDs = new Set<string>(), modelProviders = new Set<string>();
  for (const entry of seed.credentials) {
    const row = shape(entry, ['project_id', 'credential_id', 'purpose', 'version']);
    need(row.project_id === projectID && id(row.credential_id) && !knownIDs.has(row.credential_id) && row.purpose === 'model' && version(row.version));
    knownIDs.add(row.credential_id); credentialIDs.add(row.credential_id);
  }
  for (const entry of seed.providers) {
    const row = shape(entry, ['id', 'project_id', 'name', 'protocol', 'version', 'credential_ref']);
    need(id(row.id) && row.project_id === projectID && !knownIDs.has(row.id) && name(row.name) && version(row.version) && ['openai-chat-completions', 'anthropic-messages'].includes(String(row.protocol)) && (row.credential_ref === null || credentialIDs.has(String(row.credential_ref))));
    knownIDs.add(row.id); providers.set(row.id, row);
  }
  for (const entry of seed.models) {
    const row = shape(entry, ['id', 'project_id', 'provider_id', 'name', 'version']);
    need(id(row.id) && row.project_id === projectID && !knownIDs.has(row.id) && name(row.name) && version(row.version) && id(row.provider_id) && providers.has(row.provider_id));
    knownIDs.add(row.id); models.set(row.id, row); modelProviders.add(row.provider_id);
  }
  need(modelProviders.size === 2);
  const pagination = shape(value, ['provider_ids', 'model_ids', 'available']);
  function ids(value: unknown, known: ReadonlyMap<string, unknown>) {
    need(Array.isArray(value) && value.length === 26);
    const seen = new Set<string>();
    const result = value.map((entry) => { need(id(entry) && known.has(entry) && !seen.has(entry)); seen.add(entry); return entry; });
    return Object.freeze(result);
  }
  const provider_ids = ids(pagination.provider_ids, providers), model_ids = ids(pagination.model_ids, models);
  need(Array.isArray(pagination.available) && pagination.available.length === 26);
  const directoryIDs = new Set<string>(), scopes = new Set<string>();
  const available = pagination.available.map((entry): DirectoryRef => {
    const row = shape(entry, ['id', 'provider_id', 'scope', 'name', 'provider_name', 'version']);
    need(id(row.id) && id(row.provider_id) && !directoryIDs.has(row.id) && name(row.name) && name(row.provider_name) && version(row.version));
    const discriminator = (row.scope as { kind?: unknown } | null)?.kind;
    const scope = shape(row.scope, discriminator === 'system' ? ['kind'] : ['kind', 'project_id']);
    let capturedScope: DirectoryRef['scope'];
    if (scope.kind === 'project') {
      need(scope.project_id === projectID);
      const model = models.get(row.id), provider = providers.get(row.provider_id);
      need(model && provider && model.provider_id === row.provider_id && model.name === row.name && model.version === row.version && provider.name === row.provider_name);
      capturedScope = Object.freeze({ kind: 'project', project_id: projectID });
    } else {
      need(scope.kind === 'system' && !knownIDs.has(row.id) && !knownIDs.has(row.provider_id) && row.id !== row.provider_id);
      capturedScope = Object.freeze({ kind: 'system' });
    }
    directoryIDs.add(row.id); scopes.add(capturedScope.kind);
    return Object.freeze({ id: row.id, provider_id: row.provider_id, scope: capturedScope, name: row.name, provider_name: row.provider_name, version: row.version });
  });
  need(scopes.has('system') && scopes.has('project'));
  return Object.freeze({ provider_ids, model_ids, available: Object.freeze(available) });
}
