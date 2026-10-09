import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const directory = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const ts = require(resolve(directory, '../../tests/account-captcha-web/node_modules/typescript'));
const result = ts.transpileModule(readFileSync(resolve(directory, 'read-pagination-contract.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2024, module: ts.ModuleKind.ESNext } });
const { readPaginationExpectation } = await import('data:text/javascript;base64,' + Buffer.from(result.outputText).toString('base64'));
const id = (n) => `00000000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`;
const project = id(1000);
const seed = {
  providers: Array.from({ length: 26 }, (_, i) => ({ id: id(i + 1), project_id: project, name: `Models Provider ${i}`, protocol: 'openai-chat-completions', version: '1', credential_ref: null })),
  models: Array.from({ length: 26 }, (_, i) => ({ id: id(i + 101), project_id: project, provider_id: id(i % 2 + 1), name: `Models Chat ${i}`, version: '1' })),
  credentials: [],
};
const pagination = {
  provider_ids: seed.providers.map((row) => row.id), model_ids: seed.models.map((row) => row.id),
  available: [...seed.models.slice(0, 25).map((row) => ({ id: row.id, provider_id: row.provider_id, scope: { kind: 'project', project_id: project }, name: row.name, provider_name: seed.providers.find((p) => p.id === row.provider_id).name, version: row.version })), { id: id(201), provider_id: id(202), scope: { kind: 'system' }, name: 'Models System Chat', provider_name: 'Models System Provider', version: '1' }],
};
const accepted = readPaginationExpectation(pagination, project, seed);
if (!Object.isFrozen(accepted) || !Object.isFrozen(accepted.available) || !accepted.available.every((row) => Object.isFrozen(row) && Object.isFrozen(row.scope))) throw new Error('PURE_PAGINATION_CAPTURE_NOT_FROZEN');
const cases = [
  (p) => { p.value = 'private-canary'; },
  (p) => { p.provider_ids[0] = p.provider_ids[1]; },
  (p) => { p.model_ids[0] = id(999); },
  (p) => { p.provider_ids.push(id(999)); },
  (p, s) => { s.models.forEach((row) => { row.provider_id = id(1); }); },
  (p, s) => { s.models[0].project_id = id(2000); },
  (p, s) => { s.providers[0].credential_ref = id(900); },
  (p) => { p.available[0].capabilities = {}; },
  (p) => { p.available[25].scope.project_id = project; },
  (p) => { p.available[0].scope.project_id = id(2000); },
  (p) => { p.available[25].id = seed.models[25].id; },
  (p) => { p.available[0].provider_id = id(2); },
  (p) => { p.available[0].version = '2'; },
  (p) => { p.available[0].name = 'private-canary'; },
  (p) => { p.available[1] = structuredClone(p.available[0]); },
  (p) => { p.available[25].scope.kind = 'other'; },
];
for (const mutate of cases) {
  const p = structuredClone(pagination), s = structuredClone(seed); mutate(p, s);
  let rejected = false;
  try { readPaginationExpectation(p, project, s); }
  catch (error) { rejected = error.message === 'PROJECT_MODELS_PAGINATION_EXPECTATION_REJECTED'; }
  if (!rejected) throw new Error('PURE_PAGINATION_INVALID_INPUT_ADMITTED');
}
process.stdout.write(JSON.stringify({ pure_only: true, valid: 1, invalid_rejected: cases.length, immutable_capture: true }) + '\n');
