import { strict as assert } from 'node:assert';
import { readFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const output = resolve(root, 'output/ai/resource-identity-recovery/model-native-context');
mkdirSync(output, { recursive: true });
const { build } = await import(pathToFileURL(resolve(root, 'web/node_modules/vite/dist/node/index.js')).href);
const built = await build({
  configFile: false,
  root,
  cacheDir: resolve(output, 'vite-cache'),
  logLevel: 'silent',
  build: {
    write: false, minify: false, sourcemap: false,
    outDir: resolve(output, 'build'),
    lib: { entry: resolve(root, 'web/src/api/project-models.ts'), formats: ['es'], fileName: () => 'client.mjs' },
  },
});
const chunks = (Array.isArray(built) ? built : [built]).flatMap((result) => result.output).filter((entry) => entry.type === 'chunk');
assert.equal(chunks.length, 1, 'PROBE_EXPECTS_ONE_REAL_CLIENT_BUNDLE');
assert.equal(chunks[0].imports.length, 0, 'PROBE_EXPECTS_NO_EXTERNAL_RUNTIME_IMPORT');
const { createProjectModelSettingsAPI, captureProjectConfigurationCommand } = await import('data:text/javascript;base64,' + Buffer.from(chunks[0].code).toString('base64'));

const id = (n) => `0191ac00-4751-7234-899a-${n.toString(16).padStart(12, '0')}`;
const project = id(1), provider = id(2), model = id(3);
const input = {
  name: 'Independent context control', provider_model_id: 'synthetic-context-model', type: 'chat', enabled: true,
  parameters: {}, request_overwrite: {}, header_overwrite: {},
  capabilities: {
    tool_calls: false, parallel_tool_calls: false, streaming: true, reasoning: false,
    input_modalities: ['text'], output_modalities: ['text'], reasoning_efforts: [],
    structured_output_modes: ['text'], context_length: null, max_output: null,
  },
};
const originalFetch = globalThis.fetch;
globalThis.fetch = async () => { throw new Error('PROBE_NETWORK_FORBIDDEN'); };
let rejected = 0, accepted = 0, calls = 0;
try {
  for (const protocol of ['openai-chat-completions', 'anthropic-messages']) {
    for (const action of ['create', 'update']) {
      const captured = captureProjectConfigurationCommand({
        kind: `model.${action}`, provider_id: provider, protocol,
        ...(action === 'update' ? { id: model, expected_version: '7' } : {}), input,
      });
      const original = JSON.stringify(captured);
      const options = { key: 'independent-context-command', csrfToken: 'a'.repeat(43), signal: new AbortController().signal };
      let attempted = 0;
      const receipt = { kind: `model.${action}`, resource_id: model, version: action === 'create' ? '1' : '8', affected_references: '0' };
      const api = createProjectModelSettingsAPI(async (url, init) => {
        attempted++; calls++;
        assert.equal(attempted, 1, 'PROBE_EXPECTS_ONE_FETCH');
        assert.equal(url, `/api/v1/projects/${project}/models${action === 'update' ? `/${model}` : ''}`, 'PROBE_TARGET_MISMATCH');
        assert.equal(init.method, action === 'create' ? 'POST' : 'PUT', 'PROBE_METHOD_MISMATCH');
        assert.deepEqual(JSON.parse(init.body), action === 'create' ? { provider_id: provider, input: captured.input } : { expected_version: '7', input: captured.input }, 'PROBE_BODY_MISMATCH');
        const headers = new Headers(init.headers);
        assert.equal(headers.get('Idempotency-Key'), options.key, 'PROBE_KEY_MISMATCH');
        assert.equal(headers.get('X-CSRF-Token'), options.csrfToken, 'PROBE_CSRF_MISMATCH');
        return new Response(JSON.stringify(receipt), { headers: { 'Content-Type': 'application/json' } });
      });
      const invoke = (context) => action === 'create'
        ? api.createModel(project, context, captured.input, options)
        : api.updateModel(project, context, captured.expected_version, captured.input, options);
      await assert.rejects(() => invoke(captured), (error) => error?.kind === 'invalid-input', 'PROBE_FULL_COMMAND_MUST_BE_REJECTED');
      assert.equal(attempted, 0, 'PROBE_INVALID_CONTEXT_REACHED_FETCH');
      rejected++;
      const context = action === 'create'
        ? { provider_id: captured.provider_id, protocol: captured.protocol }
        : { id: captured.id, provider_id: captured.provider_id, protocol: captured.protocol };
      assert.deepEqual(await invoke(context), receipt, 'PROBE_REAL_CLIENT_RECEIPT_MISMATCH');
      assert.equal(attempted, 1, 'PROBE_EXACT_CONTEXT_DID_NOT_FETCH_ONCE');
      assert.equal(JSON.stringify(captured), original, 'PROBE_CHANGED_ORIGINAL_COMMAND');
      accepted++;
    }
  }
} finally {
  globalThis.fetch = originalFetch;
}

const source = readFileSync(resolve(root, '.agent-state/model-ui-recovery/native-client-probe.ts'), 'utf8');
assert(source.includes("case 'model.create': result = await api.createModel(project, { provider_id: captured.provider_id, protocol: captured.protocol }, captured.input, options); break"), 'PROBE_NATIVE_CREATE_CONTEXT_NOT_WIRED');
assert(source.includes("case 'model.update': result = await api.updateModel(project, { id: captured.id, provider_id: captured.provider_id, protocol: captured.protocol }, captured.expected_version, captured.input, options); break"), 'PROBE_NATIVE_UPDATE_CONTEXT_NOT_WIRED');
assert.equal(rejected, 4);
assert.equal(accepted, 4);
assert.equal(calls, 4);
process.stdout.write(JSON.stringify({ pure_only: true, actual_client: true, full_command_invalid_input_fetch_zero: rejected, exact_context_valid_request: accepted, total_fetch_calls: calls, source_binding_checked: true }) + '\n');
