import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import vm from "node:vm";

const root = resolve(import.meta.dirname, "../..");
const require = createRequire(pathToFileURL(resolve(root, "web/package.json")));
const ts = require("typescript");
const source = readFileSync(resolve(root, "tests/account-captcha-web/e2e/task-review.helpers.ts"), "utf8");
const file = ts.createSourceFile("helper.ts", source, ts.ScriptTarget.Latest, true);
const names = ["systemRunnerEvidenceValue", "requireSystemRunnerProfile", "requireAgentRunnerProfile", "requireUsageReadProfile", "requireAgentRunnerHostEvidence"];
const selected = names.map(name => file.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name));
assert(selected.every(Boolean));
const code = ts.transpileModule(selected.map(node => node.getText(file)).join("\n"), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
const expectPath = pathToFileURL(resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/index.mjs")).href;
const guards = await import("data:text/javascript;base64," + Buffer.from(`import { expect } from ${JSON.stringify(expectPath)};\n${code}`).toString("base64"));
const { systemRunnerEvidenceValue: project, requireSystemRunnerProfile: profile } = guards;
const normal = { dist: "/owned/dist", evidence: "/owned/evidence", inputHash: "a".repeat(64), prefix: "system-runner-management", endpoint() {}, lookupCommand: "runner.create", systemRunnerManagement: true, systemRunnerWireHeaders() {} };
profile(normal, { mode: "system-runner-normal" }); profile({ ...normal, cut: "create" }, { mode: "system-runner-lost" });
let rejected = 0;
for (const patch of [{ prefix: "agent-runner-mounts" }, { systemRunnerManagement: undefined }, { systemRunnerManagement: false }, { systemRunnerWireHeaders: undefined }, { systemRunnerWireHeaders: true }, { lookupCommand: "agent.create" }, { cut: "update" }, { secretReads: true }, { agentRunnerMounts: true }, { unexpected: true }]) {
  assert.throws(() => profile({ ...normal, ...patch }, { mode: "system-runner-normal" })); rejected++;
}
assert.throws(() => profile(normal, { mode: "other" })); rejected++;
assert.throws(() => profile(normal, { mode: "system-runner-lost" })); rejected++;
const oldAgent = { prefix: "agent-runner-mounts", lookupCommand: "agent.update", agentRunnerMounts: true, agentRunnerWireHeaders() {}, endpoint() {}, dist: "", evidence: "", inputHash: "" };
const oldUsage = { prefix: "project-usage", lookupCommand: null, usageReads: true, usageWireHeaders() {}, endpoint() {}, dist: "", evidence: "", inputHash: "" };
profile(oldAgent, { mode: "runner-mounts-normal" }); guards.requireAgentRunnerProfile(oldAgent, { mode: "runner-mounts-normal" });
profile(oldUsage, { mode: "paginated-current-read" }); guards.requireUsageReadProfile(oldUsage, { mode: "paginated-current-read" });

// The actual projector is used before both consumer publication and Node
// assertions. Inputs stay unchanged; only safe objects can cross the binding.
const canary = "s".repeat(43);
const original = { receipt: { command: "runner.create", runner: { id: "safe-id" } }, token_available: true, enrollment_token: canary, expires_at: "2026-10-11T00:10:00.000000Z" };
const projected = project(original);
assert.equal(JSON.stringify(projected).includes(canary), false);
assert.equal(Object.hasOwn(projected, "enrollment_token"), false);
assert.equal(original.enrollment_token === canary, true);
assert.equal(projected.token_available, true);
assert.equal(projected.receipt, original.receipt);
const lookup = { state: "committed", receipt: original.receipt, token_available: false };
assert.equal(project(lookup), lookup);
for (const value of [{ ...original, enrollment_token: "bad" }, { ...original, token_available: false }, { ...original, token_available: undefined }, { ...original, state: "committed" }, { enrollment_token: canary }, { token_available: false, expires_at: original.expires_at }]) {
  let message = "";
  try { project(value); } catch (error) { message = error.message; }
  assert.match(message, /^SYSTEM_RUNNER_(MATERIAL_SHAPE|UNEXPECTED_MATERIAL)$/);
  assert.equal(message.includes(canary), false); rejected++;
}
assert(source.includes('if (kind?.startsWith("systemRunner.")) {'));
assert(source.includes('if (c.typed.token_available === true) expect(c.material_matches).toBe(true);'));
assert(source.includes('const published = kind.startsWith("systemRunner.") ? host.__systemRunnerEvidenceValue(value) : value;'));
assert(source.includes('if (profile.systemRunnerManagement) entry.body = systemRunnerEvidenceValue(entry.body);'));
assert(source.includes('observer_error: profile.systemRunnerManagement && observerError ? "SYSTEM_RUNNER_OBSERVER_FAILED"'));
assert(source.includes('consumer(auth.system.runners, "start"'));
assert(source.includes('profile.systemRunnerManagement ? "POST" : "PATCH"'));

let binder;
function findBinder(node) {
  if (ts.isFunctionDeclaration(node) && node.name?.text === "bindSystemRunnerMaterial") binder = node;
  ts.forEachChild(node, findBinder);
}
findBinder(file); assert(binder);
const binderJS = ts.transpileModule(binder.getText(file), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const bind = vm.runInNewContext(`const systemRunnerMaterials = new WeakMap(); ${binderJS}; bindSystemRunnerMaterial`);
for (const order of [["wire", "published"], ["published", "wire"]]) {
  const call = {}; bind(call, canary, order[0]); assert.equal(Object.hasOwn(call, "material_matches"), false);
  bind(call, canary, order[1]); assert.equal(call.material_matches, true);
  assert.equal(JSON.stringify(call).includes(canary), false);
}
const mismatch = {}; bind(mismatch, canary, "wire"); bind(mismatch, "x".repeat(43), "published"); assert.equal(mismatch.material_matches, false);
const duplicate = {}; bind(duplicate, canary, "wire"); assert.throws(() => bind(duplicate, canary, "wire"), /SYSTEM_RUNNER_DUPLICATE_MATERIAL/);

// Execute the locked Playwright artifact recorder's real failure methods with
// inert recording endpoints. This is an offline failure control, not a browser.
const pwSource = readFileSync(resolve(root, "tests/account-captcha-web/node_modules/playwright/lib/index.js"), "utf8");
const pwFile = ts.createSourceFile("playwright.js", pwSource, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
const recorder = pwFile.statements.find(node => ts.isClassDeclaration(node) && node.name?.text === "ArtifactsRecorder");
assert(recorder);
const methods = ["_takePageSnapshot", "didFinishTest"].map(name => recorder.members.find(node => node.name?.getText(pwFile) === name));
assert(methods.every(Boolean));
let snapshots = 0, writes = 0, attachments = 0;
const context = { pages: () => [{ _wrapApiCall: work => work(), _snapshotForAI: async () => { snapshots++; return canary; } }], tracing: {} };
const sandbox = { process: { env: { PLAYWRIGHT_NO_COPY_PROMPT: "1" } }, Promise, import_fs: { default: { promises: { writeFile: async () => { writes++; } } } } };
const actual = vm.runInNewContext(`({${methods.map(node => node.getText(pwFile)).join(",")}})`, sandbox);
Object.assign(actual, { _testInfo: { errors: [{ message: "controlled failure" }], attachments: [], outputPath: () => "/inert/error-context.md", _attach: () => { attachments++; } }, _playwright: { _allContexts: () => [context], request: { _contexts: new Set() } }, _screenshotRecorder: { persistTemporary: async () => {} }, _stopTracing: async () => {}, didFinishTestFunction: async () => {} });
await actual.didFinishTest();
assert.equal(snapshots, 0); assert.equal(writes, 0); assert.equal(attachments, 0);
// Positive control shows this exact locked source would capture without the flag.
delete sandbox.process.env.PLAYWRIGHT_NO_COPY_PROMPT;
await actual.didFinishTest();
assert.equal(snapshots, 1); assert.equal(writes, 1); assert.equal(attachments, 1);
const config = readFileSync(resolve(root, "tests/account-captcha-web/system-runner-management.config.js"), "utf8");
const go = readFileSync(resolve(root, "tests/projectvariable/system_runner_management_web_test.go"), "utf8");
assert(config.includes('process.env.PLAYWRIGHT_NO_COPY_PROMPT !== "1"'));
assert(config.includes('screenshot: "off"') && config.includes('video: "off"') && config.includes('trace: "off"'));
assert(go.includes('"PLAYWRIGHT_NO_COPY_PROMPT=1"'));
assert(go.includes('regexp.MustCompile(`[A-Za-z0-9_-]{43}`).ReplaceAllString(safe, "[redacted-material]")'));
console.log(JSON.stringify({ actual_projector_and_profile_controls: true, negative_controls: rejected, old_agent_and_usage_profiles: "unchanged accepted", actual_locked_playwright_failure_context: { flag_blocks_snapshot_write_attachment: true, no_flag_positive_control: true }, browser_or_service_started: false }));
