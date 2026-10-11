import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { spawn } from "node:child_process";

// Task-local controls execute the real selected helper AST. Synthetic request
// and proof inputs exercise predicates only; they are never native evidence.
const root = resolve(import.meta.dirname, "../..");
const output = process.env.KNOWLEDGE_DELETE_CONTROL_OUTPUT;
assert(output && output.startsWith(resolve(root, "output/ai/knowledge-delete-ui") + "/"));
mkdirSync(output, { recursive: true, mode: 0o700 });
const require = createRequire(pathToFileURL(resolve(root, "web/package.json")));
const ts = require("typescript");
const sha = value => createHash("sha256").update(value).digest("hex");
const paths = [
  ".agent-state/knowledge-subtree-delete-ui/method-controls.mjs",
  "internal/central/app/knowledge_delete_web_test.go",
  "tests/account-captcha-web/knowledge-delete.config.js",
  "tests/account-captcha-web/e2e/knowledge-delete.helpers.ts",
  "tests/account-captcha-web/e2e/knowledge-delete.spec.ts",
  "tests/account-captcha-web/e2e/task-review.helpers.ts",
  "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",
  ".agent-state/work-owner-http/root_chain_driver.py",
  ".agent-state/task-planning-recovery/pg_only_supervisor.py",
  "web/package-lock.json", "tests/account-captcha-web/package-lock.json",
];
const inputs = Object.fromEntries(paths.map(path => [path, sha(readFileSync(resolve(root, path)))]));
inputs.node = sha(readFileSync(process.execPath));
inputs.typescript = sha(readFileSync(require.resolve("typescript")));
const inputHash = sha(JSON.stringify(inputs));
writeFileSync(resolve(output, "inputs.json"), JSON.stringify({ input_hash: inputHash, inputs }, null, 2));
const source = readFileSync(resolve(root, "tests/account-captcha-web/e2e/task-review.helpers.ts"), "utf8");
const file = ts.createSourceFile("task-review.helpers.ts", source, ts.ScriptTarget.Latest, true);
const all = [];
function visit(node) { all.push(node); ts.forEachChild(node, visit); }
visit(file);
const top = name => {
  const node = file.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name);
  assert(node, name); return node;
};
const observer = top("observeTaskRequests");
const nested = name => {
  const node = observer.body.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name);
  assert(node, name); return node;
};
const expectURL = pathToFileURL(resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/index.mjs")).href;
async function moduleOf(text) {
  const result = ts.transpileModule(text, { reportDiagnostics: true, compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  assert.equal(result.diagnostics?.length ?? 0, 0);
  return import("data:text/javascript;base64," + Buffer.from(`import { expect } from ${JSON.stringify(expectURL)}; import { createHash } from "node:crypto"; import { join, resolve } from "node:path";\n${result.outputText}`).toString("base64"));
}
const rows = [];
async function control(name, body) {
  try { await body(); rows.push({ name, pass: true }); }
  catch (error) { rows.push({ name, pass: false, error_name: error?.name ?? "Error" }); }
  console.log(JSON.stringify(rows.at(-1)));
}
const topNames = ["knowledgeDeleteEvidenceValue", "systemRunnerEvidenceValue", "requireUsageReadProfile", "requireAgentRunnerProfile", "requireAgentSecretProfile", "requireSystemRunnerProfile", "requireCommittedLookup", "observeTaskRequests"];
const guards = await moduleOf(`let bindings = 0; const knowledgeSessionBinding = async () => { bindings++; throw Error("CONTROL_BINDING_REACHED"); }; export function bindingCount() { return bindings; }\n` + topNames.map(name => top(name).getText(file)).join("\n"));
const id = "01a12c02-d66a-79c4-8cf0-7f8d8a657c4f";
const project = "01a12c02-d66a-79c4-8cf0-7f8d8a657c50";
const data = { mode: "delete-normal", project_id: project, root_id: id };
const base = { dist: "/owned/dist", evidence: "/owned/evidence", inputHash, endpoint() {} };
const profile = { ...base, prefix: "knowledge-delete", lookupCommand: "delete-subtree", knowledgeDeleteWrites: true };
const defaults = { ...base, prefix: "task-review", lookupCommand: "work.task.transfer" };
const system = { ...base, prefix: "system-runner-management", lookupCommand: "runner.create", systemRunnerManagement: true, systemRunnerWireHeaders() {} };
const secret = { ...base, prefix: "agent-secret-allowlist", lookupCommand: "agent.update", agentSecretAllowlist: true, agentSecretWireHeaders() {} };
async function reaches(candidate, material, expected) {
  const before = guards.bindingCount();
  await assert.rejects(() => guards.observeTaskRequests({}, material, candidate), error => expected ? error.message === "CONTROL_BINDING_REACHED" : error.message !== "CONTROL_BINDING_REACHED");
  assert.equal(guards.bindingCount() - before, expected ? 1 : 0);
}
await control("source-typescript-parse", () => {
  for (const path of paths.filter(p => /\.(ts|js|mjs)$/.test(p))) {
    const parsed = ts.createSourceFile(path, readFileSync(resolve(root, path), "utf8"), ts.ScriptTarget.Latest, true);
    assert.equal(parsed.parseDiagnostics.length, 0, path);
  }
});
for (const [name, candidate, material] of [
  ["delete-normal", profile, data], ["delete-lost", { ...profile, cut: "delete" }, { ...data, mode: "delete-lost" }],
  ["default", defaults, { mode: "review-complete" }],
  ["system-independent", system, { mode: "system-runner-normal" }],
  ["secret-independent", secret, { mode: "secret-allowlist-normal" }],
]) await control(`profile-positive-${name}`, () => reaches(candidate, material, true));
for (const [name, patch] of Object.entries({ missing_optin: { knowledgeDeleteWrites: undefined }, false_optin: { knowledgeDeleteWrites: false }, foreign_prefix: { prefix: "task-review" }, foreign_command: { lookupCommand: "update" }, normal_cut: { cut: "delete" }, other_write: { schedulerWrites: true }, other_read: { secretReads: true }, foreign_wire: { usageWireHeaders() {} }, unknown_option: { unknownOption: true } }))
  await control(`profile-reject-${name}`, () => reaches({ ...profile, ...patch }, data, false));
for (const [name, patch] of Object.entries({ missing_root: { root_id: undefined }, malformed_root: { root_id: "wrong" }, foreign_mode: { mode: "other" }, missing_lost_cut: { mode: "delete-lost" } }))
  await control(`profile-reject-${name}`, () => reaches(profile, { ...data, ...patch }, false));
for (const [name, candidate, material] of [
  ["delete-system", { ...profile, systemRunnerManagement: true, systemRunnerWireHeaders() {} }, data],
  ["delete-secret", { ...profile, agentSecretAllowlist: true, agentSecretWireHeaders() {} }, data],
  ["system-delete", { ...system, knowledgeDeleteWrites: true }, { mode: "system-runner-normal" }],
  ["secret-delete", { ...secret, knowledgeDeleteWrites: true }, { mode: "secret-allowlist-normal" }],
  ["default-delete", { ...defaults, knowledgeDeleteWrites: true }, data],
]) await control(`profile-reject-mixed-${name}`, () => reaches(candidate, material, false));

const token = `test-header.test-payload.${"A".repeat(43)}`;
const key = "control-original-delete-key";
const preview = { confirmation_token: token, expires_at: "2030-01-01T00:00:00Z", nodes: [{ id }], root_id: id, scope_digest: "b".repeat(64) };
const safePreview = guards.knowledgeDeleteEvidenceValue(preview);
await control("preview-projection-removes-token-without-mutating-original", () => {
  assert.equal(preview.confirmation_token, token);
  assert.deepEqual(Object.keys(safePreview).sort(), ["expires_at", "nodes", "root_id", "scope_digest"]);
  assert(!JSON.stringify(safePreview).includes(token));
});
for (const [name, value] of Object.entries({ null: null, array: [], no_token: safePreview, wrong_type: { ...preview, confirmation_token: 1 }, wrong_shape: { ...preview, confirmation_token: "wrong" }, oversized: { ...preview, confirmation_token: "A".repeat(8193) }, extra_field: { ...preview, enrollment_token: "extra" } }))
  await control(`preview-projection-reject-${name}`, () => assert.throws(() => guards.knowledgeDeleteEvidenceValue(value), /^Error: KNOWLEDGE_DELETE_PREVIEW_EVIDENCE_SHAPE$/));

// Execute the merged public projection expression and original native EOF
// projection statements. Host hooks use the actual exported projections.
const published = all.find(n => ts.isVariableDeclaration(n) && n.name.getText(file) === "published");
const nativeDelete = all.find(n => ts.isIfStatement(n) && n.expression.getText(file) === 'kind === "knowledgeDelete.preview"' && n.thenStatement.getText(file).includes('"wire"'));
const nativeSystem = all.find(n => ts.isIfStatement(n) && n.expression.getText(file) === 'kind?.startsWith("systemRunner.")');
assert(published && nativeDelete && nativeSystem);
const projectionModule = await moduleOf([top("knowledgeDeleteEvidenceValue"), top("systemRunnerEvidenceValue")].map(n => n.getText(file)).join("\n") + `
const host = { __knowledgeDeleteEvidenceValue: knowledgeDeleteEvidenceValue, __systemRunnerEvidenceValue: systemRunnerEvidenceValue };
export function publicValue(kind, value) { return ${published.initializer.getText(file)}; }
export async function nativeValue(kind, parsed) {
  const calls = [{ id: 1 }], row = { call_id: 1 }, materials = [];
  const bindDeleteMaterial = (_call, value, source) => materials.push({ kind: "delete", source, hash: createHash("sha256").update(value).digest("hex") });
  const bindSystemRunnerMaterial = (_call, value, source) => materials.push({ kind: "system", source, hash: createHash("sha256").update(value).digest("hex") });
  const digest = async value => createHash("sha256").update(value).digest("hex");
  ${nativeDelete.getText(file)}
  ${nativeSystem.getText(file)}
  return { value: parsed, row, materials };
}`);
for (const [name, kind, value, secretValue, expectedMaterial] of [
  ["delete", "knowledgeDelete.preview", preview, token, "delete"],
  ["system", "systemRunner.start", { token_available: true, enrollment_token: "B".repeat(43), expires_at: "2030-01-01T00:00:00Z", receipt: { id } }, "B".repeat(43), "system"],
  ["default", "knowledge.get", { document: { id } }, token, undefined],
]) await control(`merged-public-native-projection-${name}`, async () => {
  const publicValue = projectionModule.publicValue(kind, value);
  const native = await projectionModule.nativeValue(kind, value);
  assert.deepEqual(native.value, publicValue);
  assert(!JSON.stringify({ publicValue, native }).includes(secretValue));
  assert.deepEqual(native.materials.map(row => row.kind), expectedMaterial ? [expectedMaterial] : []);
});

const predicateNodes = observer.body.statements.filter(n => ts.isFunctionDeclaration(n) && n.pos >= nested("intakeWrite").pos && n.pos <= nested("joint").pos);
const joins = await moduleOf(`export function makeJoint(profile, data, entries, record) {
  const page = { isClosed: () => false }, proofs = [], dropped = 1;
  const readFileSync = () => JSON.stringify(record);
  ${top("requireCommittedLookup").getText(file).replace("export ", "")}
  ${predicateNodes.map(n => n.getText(file)).join("\n")}
  ${nested("requests").getText(file)}
  ${nested("originalLookup").getText(file)}
  return { joint, originalLookup, proofs, isWrite: knowledgeDeleteWrite, isPreview: knowledgeDeletePreview };
}`);
function request(name, body, overrides = {}) {
  const path = `/api/v1/projects/${project}/knowledge/documents` + (name === "lookup" ? "/commands/lookup" : `/${id}/${name === "preview" ? "delete-preview" : "delete-subtree"}`);
  return { method: () => "POST", url: () => `http://127.0.0.1:44301${path}`, headers: () => name === "preview" ? {} : { "idempotency-key": key }, postDataBuffer: () => Buffer.from(JSON.stringify(body)), postDataJSON: () => body, ...overrides };
}
function sample(name) {
  const body = name === "preview" ? {} : name === "delete" ? { confirmation_token: token } : { command: "delete-subtree", document_id: id, request: { confirmation_token: token } };
  const req = request(name, body), path = new URL(req.url()).pathname;
  const typed = name === "preview" ? safePreview : name === "lookup" ? { state: "committed", receipt: { cleanup_pending: true } } : { cleanup_pending: true };
  const wire = Buffer.from(JSON.stringify(name === "preview" ? preview : typed));
  const material = { request_bound: true, request_body_sha256: sha(req.postDataBuffer()), confirmation_token_sha256: sha(token), ...(name === "preview" ? {} : { idempotency_key_sha256: sha(key) }) };
  const row = { ...material, request_id: id, call_id: 1, path, method: "POST", status: 200, fetch_returned: true, fetch_rejected: false, error: false, aborted: false, readers: 1, reads: 1, read_returns: 1, eof: true, closed: true, release: 1, reader_cancel: 0, stream_cancel: 0, length: String(wire.length), bytes: wire.length, digest: sha(wire), typed_digest: sha(JSON.stringify(typed)) };
  const call = { ...material, id: 1, kind: name === "preview" ? "knowledgeDelete.preview" : name === "delete" ? "knowledgeDelete.startDelete" : "knowledgeDelete.checkOriginal", bound: 1, method: "POST", path, fulfilled: true, settled: true, current: true, not_busy: true, rejected: false, typed, typed_digest: row.typed_digest, material_matches: true, result_current: true };
  const record = { ...material, input_hash: inputHash, request_id: id, method: "POST", status: 200, url_sha256: sha(path), original_eof: true, original_closed: true, handler_returned: true, content_length: wire.length, body_sha256: row.digest, kind: name, ...(name === "preview" ? {} : { committed_verified: true, cleanup_pending: true }) };
  const entry = { seq: 1, name, request: req, requestID: id, status: 200, responseCount: 1, finishedCount: 1, failedCount: 0, terminal: "finished" };
  const entries = name === "lookup" ? [{ name: "delete", request: request("delete", { confirmation_token: token }) }, entry] : [entry];
  return { entry, record, row, call, entries, proof: { current: true, not_busy: true, rows: [row], calls: [call] } };
}
for (const name of ["preview", "delete", "lookup"]) {
  await control(`joint-positive-${name}`, () => {
    const s = sample(name), harness = joins.makeJoint(profile, data, s.entries, s.record);
    assert.deepEqual(harness.joint(s.entry, s.proof), s.call.typed);
    const publicEvidence = JSON.stringify(harness.proofs);
    assert(!publicEvidence.includes(token) && !publicEvidence.includes(key));
  });
  for (const [mutation, edit] of [
    ["consumer-kind", s => { s.call.kind = "systemRunner.start"; }],
    ["go-kind", s => { s.record.kind = "create"; }],
    ["token-hash", s => { s.row.confirmation_token_sha256 = "c".repeat(64); }],
    ["unbound", s => { s.call.request_bound = false; }],
    ["wrong-rid", s => { s.row.request_id = project; }],
  ]) await control(`joint-reject-${name}-${mutation}`, () => { const s = sample(name); edit(s); assert.throws(() => joins.makeJoint(profile, data, s.entries, s.record).joint(s.entry, s.proof)); });
}
for (const [name, edit] of [
  ["preview-key", s => { s.entry.request.headers = () => ({ "idempotency-key": key }); }],
  ["preview-commit", s => { s.record.committed_verified = true; }],
  ["preview-material-mismatch", s => { s.call.material_matches = false; }],
]) await control(`joint-reject-${name}`, () => { const s = sample("preview"); edit(s); assert.throws(() => joins.makeJoint(profile, data, s.entries, s.record).joint(s.entry, s.proof)); });
for (const [name, edit] of [
  ["refresh-token", body => { body.request.confirmation_token = `${token}changed`; }],
  ["different-root", body => { body.document_id = project; }],
  ["missing-intent", body => { delete body.request; }],
  ["extra-intent", body => { body.request.extra = true; }],
]) await control(`lookup-reject-${name}`, () => { const s = sample("lookup"); edit(s.entry.request.postDataJSON()); assert.throws(() => joins.makeJoint(profile, data, s.entries, s.record).originalLookup()); });
await control("lookup-reject-new-key", () => { const s = sample("lookup"); s.entry.request.headers = () => ({ "idempotency-key": "changed-key" }); assert.throws(() => joins.makeJoint(profile, data, s.entries, s.record).originalLookup()); });

// AST wiring supplements the behavioral controls; it does not stand in for
// public browser consumption or real Go receipt-cut evidence.
await control("merged-consumer-argument-order", () => {
  const method = all.find(n => ts.isMethodDeclaration(n) && n.name.getText(file) === "consumers");
  const call = all.find(n => ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression) && n.expression.name.text === "consumers");
  assert(method && call);
  const params = method.parameters.map(n => n.name.getText(file));
  assert.deepEqual(params.slice(-3), ["runnerMounts", "systemRunnerManagement", "knowledgeDelete"]);
  assert.deepEqual(call.arguments.slice(-3).map(n => n.getText(file)), params.slice(-3));
});

for (const mode of process.argv.includes("--controls-only") ? [] : ["delete-normal", "delete-lost"]) {
  const directory = resolve(output, mode); mkdirSync(directory, { mode: 0o700 });
  const command = [resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/cli.js"), "test", "--config", resolve(root, "tests/account-captcha-web/knowledge-delete.config.js"), "--list"];
  const env = { ...process.env, AGENTEAM_AUTH_WEB_ORIGIN: "http://127.0.0.1:44301", AGENTEAM_AUTH_WEB_PRIVATE: directory, AGENTEAM_AUTH_WEB_CHROMIUM: "/usr/bin/chromium", AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE: directory, AGENTEAM_KNOWLEDGE_DELETE_WEB_CASE: mode, AGENTEAM_KNOWLEDGE_DELETE_WEB_INPUT_HASH: inputHash, AGENTEAM_KNOWLEDGE_DELETE_WEB_DIST: resolve(output, "unused-dist") };
  const child = spawn(process.execPath, command, { cwd: resolve(root, "tests/account-captcha-web"), env, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = ""; child.stdout.on("data", value => stdout += value); child.stderr.on("data", value => stderr += value);
  console.log(JSON.stringify({ list_command: command, pid: child.pid, mode }));
  const actual = await new Promise((resolve, reject) => { child.once("error", reject); child.once("close", (code, signal) => resolve({ code, signal })); });
  writeFileSync(resolve(directory, "stdout.log"), stdout); writeFileSync(resolve(directory, "stderr.log"), stderr);
  writeFileSync(resolve(directory, "actual-wait.json"), JSON.stringify({ pid: child.pid, mode, command, input_hash: inputHash, actual_wait: true, ...actual }, null, 2));
  await control(`config-list-${mode}`, () => { assert.equal(actual.code, 0); assert.equal(actual.signal, null); assert(stdout.includes("Total: 1 test in 1 file")); assert(stdout.includes(`[${mode}]`)); assert(!stdout.includes(`[${mode === "delete-normal" ? "delete-lost" : "delete-normal"}]`)); });
}
await control("inputs-unchanged", () => { for (const path of paths) assert.equal(sha(readFileSync(resolve(root, path))), inputs[path], path); });
const result = { input_hash: inputHash, pid: process.pid, no_native: true, no_network: true, real_selected_helper_ast: true, synthetic_controls_only: true, passed: rows.filter(row => row.pass).length, failed: rows.filter(row => !row.pass).length, controls: rows };
writeFileSync(resolve(output, "result.json"), JSON.stringify(result, null, 2));
console.log(JSON.stringify({ completed: true, passed: result.passed, failed: result.failed, input_hash: inputHash }));
process.exitCode = result.failed ? 1 : 0;
