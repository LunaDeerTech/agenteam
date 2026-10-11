import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { observeTaskRequests } from "./task-review.helpers";
import { configurationKeys, directoryKeys } from "./agent-configuration.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type AgentRunnerMaterial = {
  mode: "runner-mounts-normal" | "runner-mounts-lost";
  origin: string; cookie: string; project_id: string; model_id: string; route: string;
  draft: { name: string; description: string; instructions: string };
  runners: { id: string; name: string; description: string; version: string; status: "offline" }[];
  agent_id: string | null; initial: Record<string, any> | null; input_hash: string;
};
export function agentRunnerMaterial(): AgentRunnerMaterial {
  const data: AgentRunnerMaterial = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-runner-mounts-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "origin", "cookie", "project_id", "model_id", "route", "draft", "runners", "agent_id", "initial", "input_hash"].sort());
  expect(["runner-mounts-normal", "runner-mounts-lost"]).toContain(data.mode);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const origin = new URL(data.origin);
  expect(origin.protocol).toBe("http:"); expect(origin.hostname).toBe("127.0.0.1"); expect(origin.port).not.toBe(""); expect(origin.origin).toBe(data.origin);
  expect(data.project_id).toMatch(id); expect(data.model_id).toMatch(id);
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/); expect(data.input_hash).toBe(process.env.AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_INPUT_HASH);
  expect(data.cookie.length).toBeGreaterThan(0); expect(data.route).toMatch(/^\/[A-Za-z0-9-]+\/[A-Za-z0-9._-]+\/settings\/agents$/);
  expect(Object.keys(data.draft).sort()).toEqual(["description", "instructions", "name"]);
  expect(data.draft.name).toMatch(/^[A-Za-z][A-Za-z0-9-]{2,24}$/);
  expect(data.draft.description.length).toBeGreaterThan(0); expect(data.draft.instructions.length).toBeGreaterThan(0);
  expect(data.runners).toHaveLength(2);
  for (const runner of data.runners) {
    expect(Object.keys(runner).sort()).toEqual(["description", "id", "name", "status", "version"]);
    expect(runner.id).toMatch(id); expect(runner.status).toBe("offline"); expect(runner.name.length).toBeGreaterThan(0); expect(runner.version).toMatch(/^[1-9][0-9]*$/);
  }
  expect(data.runners[0]!.id < data.runners[1]!.id).toBe(true);
  if (data.mode === "runner-mounts-lost") { expect(data.agent_id).toMatch(id); expect(data.initial).toMatchObject({ id: data.agent_id, project_id: data.project_id, version: "1", ...data.draft }); }
  else { expect(data.agent_id).toBeNull(); expect(data.initial).toBeNull(); }
  return data;
}
export function agentRunnerInputs(data: AgentRunnerMaterial) {
  return {
    create: { ...data.draft, model_ref: data.model_id, inject_agents_md: false, approval_policy: "default", allowed_tool_ids: [], runner_mounts: [{ runner_id: data.runners[0]!.id, workspace: "workspace-a" }], allowed_secret_variable_ids: [] },
    updates: [
      { expected_version: "1", request: { runner_mounts: [{ runner_id: data.runners[1]!.id, workspace: "workspace-b" }] } },
      ...(data.mode === "runner-mounts-normal" ? [{ expected_version: "2", request: { runner_mounts: [] } }] : []),
    ],
  };
}

// Pure state shared by the real read-only CDP observer and its focused controls.
// Only predicates, canonical IDs and hashes survive an event callback.
export function createAgentRunnerWireState(origin: string) {
  const parsedOrigin = new URL(origin);
  if (parsedOrigin.origin !== origin || parsedOrigin.protocol !== "http:" || parsedOrigin.hostname !== "127.0.0.1" || !parsedOrigin.port) throw Error("AGENT_RUNNER_WIRE_ORIGIN");
  const rows = new Map<string, any>();
  let failure: string | undefined, eventCount = 0, closed = false;
  const hash = (value: string) => createHash("sha256").update(value).digest("hex");
  const header = (headers: Record<string, unknown>, name: string) => {
    const keys = Object.keys(headers ?? {}).filter(key => key.toLowerCase() === name);
    const value = keys.length === 1 ? headers[keys[0]!] : undefined;
    return typeof value === "string" && value.length > 0 && !/[\r\n,]/.test(value) ? value : undefined;
  };
  const reject = (code: string) => { failure ??= code; throw Error(failure); };
  const check = () => { if (failure) throw Error(failure); };
  function assembled(row: any) {
    if (!row.request?.api) return undefined;
    if (row.cached || row.request.redirect || !row.request.sameOrigin || !["GET", "POST", "PATCH"].includes(row.request.method)) reject("AGENT_RUNNER_WIRE_REQUEST");
    if (!row.requestExtra || !row.response || !row.responseExtra) return undefined;
    if (!row.requestExtra.host_present || !row.requestExtra.host_matches || row.requestExtra.authority_conflict) reject("AGENT_RUNNER_WIRE_HOST");
    if (row.response.cache || !row.response.extra || row.response.url_sha256 !== row.request.url_sha256 || row.response.status !== row.responseExtra.status || !row.responseExtra.request_id) reject("AGENT_RUNNER_WIRE_RESPONSE");
    return { source: "cdp_network_extra_info" as const, cdp_request_id: row.cdp_request_id, request_id: row.responseExtra.request_id as string, method: row.request.method as string, url_sha256: row.request.url_sha256 as string, status: row.response.status as number, host_present: true as const, host_matches: true as const };
  }
  function feed(kind: string, event: any) {
    try {
      check();
      if (closed) reject("AGENT_RUNNER_WIRE_EVENT_AFTER_RETIRE");
      if (++eventCount > 2048 || typeof event.requestId !== "string" || !event.requestId || event.requestId.length > 256) reject("AGENT_RUNNER_WIRE_EVENT_BOUND");
      let row = rows.get(event.requestId);
      if (!row) {
        if (rows.size >= 512) reject("AGENT_RUNNER_WIRE_ROW_BOUND");
        row = { cdp_request_id: event.requestId }; rows.set(event.requestId, row);
      }
      if (row[kind] !== undefined) reject("AGENT_RUNNER_WIRE_DUPLICATE_EVENT");
      if (kind === "request") {
        const url = new URL(event.request.url);
        row.request = { api: url.pathname.startsWith("/api/"), sameOrigin: url.origin === origin, method: event.request.method, url_sha256: hash(event.request.url), redirect: Boolean(event.redirectResponse) };
      } else if (kind === "requestExtra") {
        const host = header(event.headers, "host"), authority = header(event.headers, ":authority");
        row.requestExtra = { host_present: host !== undefined, host_matches: host === parsedOrigin.host, authority_conflict: Object.keys(event.headers ?? {}).some(key => key.toLowerCase() === ":authority") && authority !== parsedOrigin.host };
      } else if (kind === "response") {
        row.response = { url_sha256: hash(event.response.url), status: event.response.status, extra: event.hasExtraInfo === true, cache: Boolean(event.response.fromDiskCache || event.response.fromPrefetchCache || event.response.fromServiceWorker) };
      } else if (kind === "responseExtra") {
        const value = header(event.headers, "x-request-id");
        row.responseExtra = { request_id: value && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value) ? value : undefined, status: event.statusCode };
      } else if (kind === "cached") row.cached = true;
      else reject("AGENT_RUNNER_WIRE_EVENT_KIND");
      assembled(row);
      const ids = [...rows.values()].filter(item => item.request?.api && item.responseExtra?.request_id).map(item => item.responseExtra.request_id);
      if (new Set(ids).size !== ids.length) reject("AGENT_RUNNER_WIRE_DUPLICATE_RID");
    } catch (error) {
      failure ??= error instanceof Error && /^AGENT_RUNNER_WIRE_/.test(error.message) ? error.message : "AGENT_RUNNER_WIRE_MALFORMED_EVENT";
    }
  }
  function complete() {
    check();
    for (const row of rows.values()) if (row.request?.api && (!assembled(row) || !row.matched)) reject("AGENT_RUNNER_WIRE_UNMATCHED_API");
    // ExtraInfo without its request event cannot be classified safely.
    if ([...rows.values()].some(row => !row.request)) reject("AGENT_RUNNER_WIRE_ORPHAN_EVENT");
  }
  return {
    feed,
    match(expected: { method: string; url: string; status: number; requestID: string }) {
      check();
      if (closed) reject("AGENT_RUNNER_WIRE_READ_AFTER_RETIRE");
      const matches = [...rows.values()].filter(row => row.request?.api && row.responseExtra?.request_id === expected.requestID);
      if (matches.length > 1) reject("AGENT_RUNNER_WIRE_AMBIGUOUS_RID");
      if (!matches.length) return undefined;
      const row = matches[0], proof = assembled(row);
      if (!proof) return undefined;
      if (proof.method !== expected.method || proof.url_sha256 !== hash(expected.url) || proof.status !== expected.status) reject("AGENT_RUNNER_WIRE_PW_CONFLICT");
      row.matched = true; return proof;
    },
    complete,
    retire() { closed = true; complete(); },
    failed(code: string) { failure ??= code; },
    snapshot() { return { source: "cdp_network_extra_info", event_count: eventCount, retired: closed, failure, requests: [...rows.values()].filter(row => row.request?.api).map(row => ({ ...row })) }; },
  };
}

async function observeAgentRunnerWire(page: Page, origin: string) {
  const state = createAgentRunnerWireState(origin), session = await page.context().newCDPSession(page);
  const names = { request: "Network.requestWillBeSent", requestExtra: "Network.requestWillBeSentExtraInfo", response: "Network.responseReceived", responseExtra: "Network.responseReceivedExtraInfo", cached: "Network.requestServedFromCache" } as const;
  const listeners = Object.entries(names).map(([kind, name]) => ({ name, listener: (event: any) => state.feed(kind, event) }));
  let detached = false;
  async function close() {
    if (detached) return;
    try { await session.detach(); detached = true; }
    catch (error) { state.failed("AGENT_RUNNER_WIRE_DETACH_FAILED"); throw error; }
    finally { for (const { name, listener } of listeners) session.off(name, listener); state.retire(); }
  }
  try {
    for (const { name, listener } of listeners) session.on(name, listener);
    await session.send("Network.enable");
  } catch (error) { try { await close(); } finally { throw error; } }
  return {
    async headers(expected: { method: string; url: string; status: number; requestID: string }) {
      let proof: ReturnType<typeof state.match>;
      await expect.poll(() => { proof = state.match(expected); return proof !== undefined; }, { timeout: 5_000 }).toBe(true);
      return proof!;
    },
    complete: () => state.complete(),
    close,
    snapshot: () => ({ ...state.snapshot(), actual_detach: detached }),
  };
}

export async function observeAgentRunner(page: Page, data: AgentRunnerMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`, inputs = agentRunnerInputs(data);
  let agentID = data.agent_id, creates = 0, updates = 0, lookups = 0, completed = false, stage = "entry";
  const keys = new Set<string>();
  let updateKey: string | undefined;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname, method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(data.origin);
    if (method === "GET") {
      expect(request.postDataBuffer()).toBeNull(); expect(request.headers()["idempotency-key"]).toBeUndefined();
      const globals: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
      if (globals[path]) return globals[path];
      if (path === prefix && !url.search) return "project";
      if (path === prefix + "/agents") { expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]); return "agent-directory"; }
      for (const [suffix, name] of [["available-chat-models", "agent-models"], ["available-runners", "agent-runners"]]) if (path === prefix + "/" + suffix) {
        expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]); return name;
      }
      if (agentID && !url.search) {
        if (path === `${prefix}/agents/${agentID}/configuration`) return "agent-configuration";
        if (path === `${prefix}/agents/${agentID}/runner-mounts`) return "agent-mounts";
      }
    }
    if (url.search) throw Error("AGENT_RUNNER_WRITE_QUERY");
    if (method === "POST" && path === prefix + "/agents") {
      expect(data.mode).toBe("runner-mounts-normal"); expect(creates++).toBe(0);
      const body = request.postDataJSON(); expect(Object.keys(body)).toEqual(["request"]); expect(body.request.agent_id).toMatch(id);
      expect(body.request).toEqual({ agent_id: body.request.agent_id, ...inputs.create }); agentID = body.request.agent_id;
      const key = request.headers()["idempotency-key"]!; expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/); expect(keys.has(key)).toBe(false); keys.add(key);
      return "create";
    }
    if (method === "PATCH" && agentID && path === `${prefix}/agents/${agentID}/configuration`) {
      expect(updates).toBeLessThan(inputs.updates.length); expect(request.postDataJSON()).toEqual(inputs.updates[updates++]);
      const key = request.headers()["idempotency-key"]!; expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/); expect(keys.has(key)).toBe(false); keys.add(key); updateKey = key;
      return "update";
    }
    if (method === "POST" && path === prefix + "/agent-commands/lookup") {
      expect(data.mode).toBe("runner-mounts-lost"); expect(updates).toBe(1); expect(lookups++).toBe(0);
      expect(request.postDataJSON()).toEqual({ command: "agent.update", target_id: agentID, ...inputs.updates[0] });
      expect(request.headers()["idempotency-key"]).toBe(updateKey); return "lookup";
    }
    throw Error("UNOBSERVED_AGENT_RUNNER_ENDPOINT");
  };
  const evidence = process.env.AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_EVIDENCE!;
  const wire = await observeAgentRunnerWire(page, data.origin);
  let seen: Awaited<ReturnType<typeof observeTaskRequests>>;
  try { seen = await observeTaskRequests(page, { mode: data.mode, cookie: data.cookie, project_id: data.project_id, ...(data.agent_id ? { agent_id: data.agent_id } : {}) }, {
    dist: process.env.AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_DIST!, evidence, inputHash: data.input_hash, prefix: "agent-runner-mounts", endpoint: classify,
    lookupCommand: "agent.update", ...(data.mode === "runner-mounts-lost" ? { cut: "update" as const } : {}), agentRunnerMounts: true, agentRunnerWireHeaders: wire.headers,
  }); } catch (error) { try { await wire.close(); } finally { throw error; } }
  function current(version: string, declarations: readonly { runner_id: string; workspace: string }[]) {
    const config = seen.runnerMountRead("agent-configuration"), mounts = seen.runnerMountRead("agent-mounts");
    expect(Object.keys(config).sort()).toEqual(configurationKeys);
    expect(config).toMatchObject({ id: agentID, project_id: data.project_id, version, ...data.draft, model_ref: data.model_id, inject_agents_md: false, approval_policy: "default", lifecycle: "active" });
    expect(Object.keys(mounts).sort()).toEqual(["agent_id", "agent_version", "items", "project_id"]);
    expect(mounts).toMatchObject({ agent_id: agentID, project_id: data.project_id, agent_version: version });
    expect(mounts.items).toHaveLength(declarations.length);
    for (const row of mounts.items) { expect(Object.keys(row).sort()).toEqual(["mount_id", "name", "runner_id", "workspace"]); expect(row.mount_id).toMatch(id); expect(row.name).toBe(row.mount_id); }
    expect(mounts.items.map((v: any) => v.mount_id)).toEqual(config.allowed_mount_ids);
    expect(mounts.items.map((v: any) => ({ runner_id: v.runner_id, workspace: v.workspace })).sort((a: any, b: any) => a.runner_id.localeCompare(b.runner_id))).toEqual([...declarations].sort((a, b) => a.runner_id.localeCompare(b.runner_id)));
    return config;
  }
  return {
    ...seen,
    async retire() { try { await seen.retire(); wire.complete(); } finally { await wire.close(); } },
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("AGENT_RUNNER_STAGE"); stage = value; },
    agentID: () => agentID!, current,
    runners() { const value = seen.runnerMountRead("agent-runners"); expect(value).toEqual({ items: data.runners }); return value; },
    modelOption() {
      const rows = seen.runnerMountRead("agent-models").items.filter((row: any) => row.id === data.model_id); expect(rows).toHaveLength(1);
      expect(rows[0].scope).toEqual({ kind: "project", project_id: data.project_id }); expect(rows[0].capabilities.reasoning_efforts).toEqual([]);
      return `${rows[0].name} · ${rows[0].provider_name}（项目）`;
    },
    directory(config: Record<string, any>) {
      const rows = seen.runnerMountRead("agent-directory").items.filter((row: any) => row.id === agentID); expect(rows).toHaveLength(1);
      expect(Object.keys(rows[0]).sort()).toEqual(directoryKeys); for (const key of directoryKeys) expect(rows[0][key]).toEqual(config[key]);
    },
    requireReceipt(receipt: Record<string, any>, version: string, declarations: readonly { runner_id: string; workspace: string }[], before?: Record<string, any>) {
      expect(Object.keys(receipt).sort()).toEqual(["agent", "changed", "event_ids", "runner_mounts"]);
      expect(receipt.changed).toBe(true); expect(receipt.event_ids).toHaveLength(1); expect(receipt.event_ids[0]).toMatch(id);
      expect(receipt.agent.version).toBe(version); expect(Object.keys(receipt.agent).sort()).toEqual(configurationKeys);
      expect(receipt.runner_mounts).toHaveLength(declarations.length);
      for (const [n, row] of receipt.runner_mounts.entries()) { expect(Object.keys(row).sort()).toEqual(["mount_id", "runner_id", "workspace", "workspace_id"]); expect(row.mount_id).toMatch(id); expect(row.workspace_id).toMatch(id); expect({ runner_id: row.runner_id, workspace: row.workspace }).toEqual(declarations[n]); }
      expect(receipt.runner_mounts.map((v: any) => v.mount_id).sort()).toEqual([...receipt.agent.allowed_mount_ids].sort());
      if (before) { const { version: _, updated_at: oldTime, allowed_mount_ids: __, ...unchanged } = before;
        expect(receipt.agent).toEqual({ ...unchanged, version, updated_at: receipt.agent.updated_at, allowed_mount_ids: receipt.agent.allowed_mount_ids }); expect(receipt.agent.updated_at > oldTime).toBe(true);
      }
    },
    async finish() {
      expect(creates).toBe(data.mode === "runner-mounts-normal" ? 1 : 0); expect(updates).toBe(inputs.updates.length); expect(lookups).toBe(data.mode === "runner-mounts-lost" ? 1 : 0);
      await seen.verify(); await seen.endDocument(); wire.complete(); completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "agent-runner-mounts-wire.json"), JSON.stringify(wire.snapshot(), null, 2) + "\n", { mode: 0o600 });
      writeFileSync(join(evidence, "agent-runner-mounts-stage.json"), JSON.stringify({ stage, status, creates, updates, lookups, error_locations: errors.map(error => error.stack?.match(/(?:agent-runner-mounts\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `agent-runner-mounts-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-runner-mounts-result.json"), JSON.stringify({ completed, input_hash: data.input_hash, original_requests: proof.original_requests, all_observed: proof.all_observed, creates, updates, lookups, configurations: seen.requests("agent-configuration").length, mounts: seen.requests("agent-mounts").length }), { mode: 0o600 });
      }
    },
  };
}
export async function agentRunnerScreenshot(page: Page, mode: string, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible"); await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_EVIDENCE!; mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `agent-runner-mounts-${mode}-${name}.png`), fullPage: false, animations: "disabled" });
}
