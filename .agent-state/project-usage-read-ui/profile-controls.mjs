import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(import.meta.dirname, "../..");
const require = createRequire(pathToFileURL(resolve(root, "web/package.json")));
const ts = require("typescript");
const source = readFileSync(resolve(root, "tests/account-captcha-web/e2e/task-review.helpers.ts"), "utf8");
const file = ts.createSourceFile("task-review.helpers.ts", source, ts.ScriptTarget.Latest, true);
const names = ["requireUsageReadProfile", "requireUsageHostEvidence"];
const declarations = names.map(name => file.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name));
assert(declarations.every(Boolean));
const text = declarations.map(node => node.getText(file)).join("\n");
const js = ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
const expectPath = pathToFileURL(resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/index.mjs")).href;
const { requireUsageReadProfile: profile, requireUsageHostEvidence: host } = await import("data:text/javascript;base64," + Buffer.from(`import { expect } from ${JSON.stringify(expectPath)};\n${js}`).toString("base64"));
const candidate = { dist: "/owned/dist", evidence: "/owned/evidence", inputHash: "a".repeat(64), prefix: "project-usage", endpoint() {}, lookupCommand: null, usageReads: true, usageWireHeaders() {} };
const data = { mode: "paginated-current-read" };
profile(candidate, data);
profile({ prefix: "project-secrets", secretReads: true }, { mode: "metadata-pagination-and-detail" });
profile({ prefix: "sprint-start", sprintStartWrites: true }, { mode: "unchanged-existing" });
let rejects = 0;
for (const patch of [{ usageReads: undefined }, { usageReads: false }, { usageWireHeaders: undefined }, { usageWireHeaders: true }, { prefix: "project-secrets" }, { lookupCommand: "update" }, { cut: "update" }, { secretReads: true }, { schedulerWrites: true }, { unknownOption: true }]) {
  assert.throws(() => profile({ ...candidate, ...patch }, data)); rejects++;
}
assert.throws(() => profile(candidate, { mode: "other" })); rejects++;
const wire = { source: "cdp_network_extra_info", cdp_request_id: "9.1", request_id: "01a12c02-d66a-79c4-8cf0-7f8d8a657c4f", method: "GET", status: 200, host_present: true, host_matches: true };
const headers = { request_fulfilled: true, response_fulfilled: true, host_present: false, host_matches: false, cookie_present: true, usage_wire: wire };
const proof = { request_id: wire.request_id, method: "GET", status: 200, browser_ingress_host_matches: true, downstream_host_matches: true, handler_returned: true, handler_return_source: "app_http_request_log" };
host(proof, headers);
for (const key of ["request_fulfilled", "response_fulfilled", "cookie_present"]) { assert.throws(() => host(proof, { ...headers, [key]: false })); rejects++; }
for (const key of Object.keys(proof)) { assert.throws(() => host({ ...proof, [key]: key === "handler_return_source" ? "unspecified" : false }, headers)); rejects++; }
assert.throws(() => host({ request_host_matches: true, handler_returned: true }, headers)); rejects++;
for (const patch of [{ usage_wire: undefined }, { host_present: true, host_matches: false }, { authority_present: true, authority_matches: false }]) { assert.throws(() => host(proof, { ...headers, ...patch })); rejects++; }
for (const key of ["source", "host_present", "host_matches", "request_id", "method", "status"]) { assert.throws(() => host(proof, { ...headers, usage_wire: { ...wire, [key]: false } })); rejects++; }
assert.throws(() => profile({ prefix: "project-secrets", usageWireHeaders() {} }, data)); rejects++;
assert.throws(() => host(proof, undefined)); rejects++;
assert(source.includes("  requireUsageReadProfile(profile, data);"));
assert(source.includes("if (profile.usageReads) requireUsageHostEvidence(record, entry.skillsHeaders);"));
assert(source.includes("diagnostic.usage_wire = await profile.usageWireHeaders!"));
assert(source.includes("(diagnostic.host_present && !diagnostic.host_matches)"));
assert(source.includes('if (usageReadEntry(entry)) expect(c.kind).toBe('));
assert(source.includes("knowledgeTitleReadEntry(entry) || secretReadEntry(entry) || usageReadEntry(entry) ||"));
console.log(JSON.stringify({ actual_shared_guards: true, positive_controls: 4, rejection_controls: rejects, actual_call_sites_retained: true }));

const usageSource = readFileSync(resolve(root, "tests/account-captcha-web/e2e/project-usage.helpers.ts"), "utf8");
const usageFile = ts.createSourceFile("project-usage.helpers.ts", usageSource, ts.ScriptTarget.Latest, true);
const selected = ["createUsageWireState", "observeUsageWire"].map(name => usageFile.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name));
assert(selected.every(Boolean));
const usageJS = ts.transpileModule(selected.map(node => node.getText(usageFile)).join("\n") + "\nexport { observeUsageWire };", { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
const { createUsageWireState, observeUsageWire } = await import("data:text/javascript;base64," + Buffer.from(`import { expect } from ${JSON.stringify(expectPath)}; import { createHash } from "node:crypto";\n${usageJS}`).toString("base64"));
const origin = "https://127.0.0.1:44301", url = origin + "/api/v1/session", requestID = wire.request_id;
const events = {
  request: { requestId: "9.1", request: { url, method: "GET" } },
  requestExtra: { requestId: "9.1", headers: { Host: "127.0.0.1:44301", Cookie: "never-retain-me" } },
  response: { requestId: "9.1", hasExtraInfo: true, response: { url, status: 200 } },
  responseExtra: { requestId: "9.1", statusCode: 200, headers: { "X-Request-ID": requestID, "Set-Cookie": "never-retain-me-either" } },
};
const expected = { method: "GET", url, status: 200, requestID };
function assembled(patch = {}, omitted) {
  const state = createUsageWireState(origin);
  for (const kind of ["responseExtra", "requestExtra", "response", "request"]) if (kind !== omitted) state.feed(kind, { ...events[kind], ...patch[kind] });
  return state;
}
let wirePositive = 0, wireRejected = 0;
for (const order of [["request", "requestExtra", "response", "responseExtra"], ["responseExtra", "requestExtra", "response", "request"]]) {
  const state = createUsageWireState(origin);
  for (const kind of order) state.feed(kind, events[kind]);
  assert.equal(state.match(expected).request_id, requestID); state.complete();
  assert(!JSON.stringify(state.snapshot()).includes("never-retain-me")); state.retire(); wirePositive++;
}
function rejectState(make, expectation = expected) { assert.throws(() => { const state = make(); state.match(expectation); state.complete(); }); wireRejected++; }
for (const kind of Object.keys(events)) rejectState(() => assembled({}, kind));
for (const patch of [
  { request: { redirectResponse: { status: 302 } } },
  { request: { request: { url, method: "POST" } } },
  { requestExtra: { headers: {} } },
  { requestExtra: { headers: { Host: "other.test" } } },
  { requestExtra: { headers: { Host: "127.0.0.1:44301", host: "127.0.0.1:44301" } } },
  { requestExtra: { headers: { Host: "127.0.0.1:44301\nother" } } },
  { requestExtra: { headers: { Host: "127.0.0.1:44301", ":authority": "other.test" } } },
  { responseExtra: { headers: { "X-Request-ID": requestID, "x-request-id": requestID } } },
  { responseExtra: { headers: { "X-Request-ID": requestID + "\n" + requestID } } },
  { responseExtra: { statusCode: 201 } },
  { response: { response: { url: url + "?other=1", status: 200 } } },
  { response: { hasExtraInfo: false } },
  ...["fromDiskCache", "fromPrefetchCache", "fromServiceWorker"].map(key => ({ response: { response: { url, status: 200, [key]: true } } })),
]) rejectState(() => assembled(patch));
for (const kind of Object.keys(events)) rejectState(() => { const state = assembled(); state.feed(kind, events[kind]); return state; });
rejectState(() => { const state = assembled(); state.feed("cached", { requestId: "9.1" }); return state; });
rejectState(() => { const state = assembled(); for (const [kind, event] of Object.entries(events)) state.feed(kind, { ...event, requestId: "9.2" }); return state; });
for (const patch of [{ url: url + "?x=1" }, { status: 201 }, { method: "POST" }, { requestID: "01a12c02-d70f-7ecf-ab25-2f991a374692" }]) rejectState(() => assembled(), { ...expected, ...patch });
rejectState(() => { const state = assembled(); state.match(expected); state.complete(); state.feed("responseExtra", events.responseExtra); return state; });
rejectState(() => { const state = assembled(); state.retire(); state.feed("request", events.request); return state; });
rejectState(() => { const state = createUsageWireState(origin); for (let i = 0; i < 513; i++) state.feed("request", { requestId: String(i), request: { method: "GET", url: origin + "/assets/a" } }); return state; });
const calls = [], listeners = new Map();
const session = { on(name, listener) { listeners.set(name, listener); }, off(name) { listeners.delete(name); }, async send(name) { calls.push(name); throw Error("enable controlled failure"); }, async detach() { calls.push("detach"); } };
await assert.rejects(() => observeUsageWire({ context: () => ({ newCDPSession: async () => session }) }, origin), /enable controlled failure/);
assert.deepEqual(calls, ["Network.enable", "detach"]); assert.equal(listeners.size, 0); wirePositive++;
const stableCalls = [], stableListeners = new Map();
const stableSession = { on(name, listener) { stableListeners.set(name, listener); }, off(name) { stableListeners.delete(name); }, async send(name) { stableCalls.push(name); }, async detach() { stableCalls.push("detach"); } };
const observer = await observeUsageWire({ context: () => ({ newCDPSession: async () => stableSession }) }, origin);
const eventNames = { request: "Network.requestWillBeSent", requestExtra: "Network.requestWillBeSentExtraInfo", response: "Network.responseReceived", responseExtra: "Network.responseReceivedExtraInfo" };
for (const kind of ["responseExtra", "requestExtra", "request", "response"]) stableListeners.get(eventNames[kind])(events[kind]);
assert.equal((await observer.headers(expected)).request_id, requestID); observer.complete(); await observer.close();
assert.deepEqual(stableCalls, ["Network.enable", "detach"]); assert.equal(stableListeners.size, 0); assert.equal(observer.snapshot().actual_detach, true); wirePositive++;
let finishDetach;
const lateListeners = new Map();
const lateSession = { on(name, listener) { lateListeners.set(name, listener); }, off(name) { lateListeners.delete(name); }, async send() {}, detach() { return new Promise(resolve => { finishDetach = resolve; }); } };
const late = await observeUsageWire({ context: () => ({ newCDPSession: async () => lateSession }) }, origin);
for (const [kind, event] of Object.entries(events)) lateListeners.get(eventNames[kind])(event);
await late.headers(expected); late.complete(); const closing = late.close();
assert.equal(late.snapshot().actual_detach, false); assert.equal(late.snapshot().retired, false);
lateListeners.get(eventNames.responseExtra)(events.responseExtra); finishDetach();
await assert.rejects(closing, /USAGE_WIRE_DUPLICATE_EVENT/); assert.equal(lateListeners.size, 0); assert.equal(late.snapshot().actual_detach, true); wireRejected++;
for (const kind of ["request", "requestExtra"]) {
  let finish; const listeners = new Map();
  const session = { on(name, listener) { listeners.set(name, listener); }, off(name) { listeners.delete(name); }, async send() {}, detach() { return new Promise(resolve => { finish = resolve; }); } };
  const wire = await observeUsageWire({ context: () => ({ newCDPSession: async () => session }) }, origin);
  for (const [name, event] of Object.entries(events)) listeners.get(eventNames[name])(event);
  await wire.headers(expected); wire.complete(); const closing = wire.close();
  listeners.get(eventNames[kind])({ ...events[kind], requestId: "new-late-request" }); finish();
  await assert.rejects(closing, /USAGE_WIRE_(UNMATCHED_API|ORPHAN_EVENT)/);
  assert.equal(wire.snapshot().actual_detach, true); assert.equal(wire.snapshot().retired, true); assert.equal(listeners.size, 0); wireRejected++;
}
const failedListeners = new Map();
const failedSession = { on(name, listener) { failedListeners.set(name, listener); }, off(name) { failedListeners.delete(name); }, async send() {}, async detach() { throw Error("controlled detach failure"); } };
const failedDetach = await observeUsageWire({ context: () => ({ newCDPSession: async () => failedSession }) }, origin);
await assert.rejects(() => failedDetach.close(), /USAGE_WIRE_DETACH_FAILED/); assert.equal(failedDetach.snapshot().actual_detach, false); assert.equal(failedListeners.size, 0); wireRejected++;
assert(usageSource.includes("usageWireHeaders: wire.headers")); assert(usageSource.includes("finally { await wire.close(); }"));
console.log(JSON.stringify({ actual_usage_wire_state_and_lifecycle: true, positive_controls: wirePositive, rejection_controls: wireRejected, source: "cdp_network_extra_info", no_native: true }));
