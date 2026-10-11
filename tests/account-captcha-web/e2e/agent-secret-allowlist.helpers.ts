import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createAgentRunnerWireState } from "./agent-runner-mounts.helpers";
import { observeTaskRequests } from "./task-review.helpers";
import { configurationKeys, directoryKeys } from "./agent-configuration.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type AgentSecretMaterial = {
  mode: "secret-allowlist-normal" | "secret-allowlist-lost";
  origin: string; cookie: string; project_id: string; model_id: string; route: string;
  draft: { name: string; description: string; instructions: string };
  secrets: { id: string; project_id: string; type: "secret"; name: string; description: string; version: string; created_at: string; updated_at: string }[];
  agent_id: string | null; initial: Record<string, any> | null; input_hash: string;
};
export function agentSecretMaterial(): AgentSecretMaterial {
  const data: AgentSecretMaterial = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-secret-allowlist-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "origin", "cookie", "project_id", "model_id", "route", "draft", "secrets", "agent_id", "initial", "input_hash"].sort());
  expect(["secret-allowlist-normal", "secret-allowlist-lost"]).toContain(data.mode);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const origin = new URL(data.origin);
  expect(origin.protocol).toBe("http:"); expect(origin.hostname).toBe("127.0.0.1"); expect(origin.port).not.toBe(""); expect(origin.origin).toBe(data.origin);
  expect(data.project_id).toMatch(id); expect(data.model_id).toMatch(id);
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/); expect(data.input_hash).toBe(process.env.AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_INPUT_HASH);
  expect(data.cookie.length).toBeGreaterThan(0); expect(data.route).toMatch(/^\/[A-Za-z0-9-]+\/[A-Za-z0-9._-]+\/settings\/agents$/);
  expect(Object.keys(data.draft).sort()).toEqual(["description", "instructions", "name"]);
  expect(data.draft.name).toMatch(/^[A-Za-z][A-Za-z0-9-]{2,24}$/);
  expect(data.draft.description.length).toBeGreaterThan(0); expect(data.draft.instructions.length).toBeGreaterThan(0);
  expect(data.secrets).toHaveLength(2);
  for (const secret of data.secrets) {
    expect(Object.keys(secret).sort()).toEqual(["created_at", "description", "id", "name", "project_id", "type", "updated_at", "version"]);
    expect(secret.id).toMatch(id); expect(secret.project_id).toBe(data.project_id); expect(secret.type).toBe("secret");
    expect(secret.name).toMatch(/^AGENT_BROWSER_SECRET_[12]$/); expect(secret.version).toBe("1");
    expect(secret.created_at).toBe(secret.updated_at); expect(Number.isFinite(Date.parse(secret.created_at))).toBe(true);
  }
  expect(data.secrets[0]!.name < data.secrets[1]!.name).toBe(true);
  if (data.mode === "secret-allowlist-lost") { expect(data.agent_id).toMatch(id); expect(data.initial).toMatchObject({ id: data.agent_id, project_id: data.project_id, version: "1", ...data.draft }); }
  else { expect(data.agent_id).toBeNull(); expect(data.initial).toBeNull(); }
  return data;
}
export function agentSecretInputs(data: AgentSecretMaterial) {
  return {
    create: { ...data.draft, model_ref: data.model_id, inject_agents_md: false, approval_policy: "default", allowed_tool_ids: [], runner_mounts: [], allowed_secret_variable_ids: [data.secrets[0]!.id] },
    updates: [
      { expected_version: "1", request: { allowed_secret_variable_ids: [data.secrets[1]!.id] } },
      ...(data.mode === "secret-allowlist-normal" ? [{ expected_version: "2", request: { allowed_secret_variable_ids: [] } }] : []),
    ],
  };
}

async function observeAgentSecretWire(page: Page, origin: string) {
  const state = createAgentRunnerWireState(origin), session = await page.context().newCDPSession(page);
  const names = { request: "Network.requestWillBeSent", requestExtra: "Network.requestWillBeSentExtraInfo", response: "Network.responseReceived", responseExtra: "Network.responseReceivedExtraInfo", cached: "Network.requestServedFromCache" } as const;
  const listeners = Object.entries(names).map(([kind, name]) => ({ name, listener: (event: any) => state.feed(kind, event) }));
  let detached = false;
  async function close() {
    if (detached) return;
    try { await session.detach(); detached = true; }
    catch (error) { state.failed("AGENT_SECRET_WIRE_DETACH_FAILED"); throw error; }
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

export async function observeAgentSecret(page: Page, data: AgentSecretMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`, inputs = agentSecretInputs(data);
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
      for (const [suffix, name] of [["available-chat-models", "agent-models"], ["available-runners", "agent-runners"], ["secret-variables", "agent-secrets"]]) if (path === prefix + "/" + suffix) {
        expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]); return name;
      }
      if (agentID && !url.search) {
        if (path === `${prefix}/agents/${agentID}/configuration`) return "agent-configuration";
        if (path === `${prefix}/agents/${agentID}/runner-mounts`) return "agent-mounts";
      }
    }
    if (url.search) throw Error("AGENT_SECRET_WRITE_QUERY");
    if (method === "POST" && path === prefix + "/agents") {
      expect(data.mode).toBe("secret-allowlist-normal"); expect(creates++).toBe(0);
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
      expect(data.mode).toBe("secret-allowlist-lost"); expect(updates).toBe(1); expect(lookups++).toBe(0);
      expect(request.postDataJSON()).toEqual({ command: "agent.update", target_id: agentID, ...inputs.updates[0] });
      expect(request.headers()["idempotency-key"]).toBe(updateKey); return "lookup";
    }
    throw Error("UNOBSERVED_AGENT_SECRET_ENDPOINT");
  };
  const evidence = process.env.AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_EVIDENCE!;
  const wire = await observeAgentSecretWire(page, data.origin);
  let seen: Awaited<ReturnType<typeof observeTaskRequests>>;
  try { seen = await observeTaskRequests(page, { mode: data.mode, cookie: data.cookie, project_id: data.project_id, ...(data.agent_id ? { agent_id: data.agent_id } : {}) }, {
    dist: process.env.AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_DIST!, evidence, inputHash: data.input_hash, prefix: "agent-secret-allowlist", endpoint: classify,
    lookupCommand: "agent.update", ...(data.mode === "secret-allowlist-lost" ? { cut: "update" as const } : {}), agentSecretAllowlist: true, agentSecretWireHeaders: wire.headers,
  }); } catch (error) { try { await wire.close(); } finally { throw error; } }
  function current(version: string, allowed: readonly string[]) {
    const config = seen.agentSecretRead("agent-configuration");
    expect(Object.keys(config).sort()).toEqual(configurationKeys);
    expect(config).toMatchObject({ id: agentID, project_id: data.project_id, version, ...data.draft, model_ref: data.model_id, inject_agents_md: false, approval_policy: "default", lifecycle: "active", allowed_mount_ids: [], allowed_secret_variable_ids: allowed });
    return config;
  }
  return {
    ...seen,
    async retire() { try { await seen.retire(); wire.complete(); } finally { await wire.close(); } },
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("AGENT_SECRET_STAGE"); stage = value; },
    agentID: () => agentID!, current,
    secrets() { const value = seen.agentSecretRead("agent-secrets"); expect(value).toEqual({ items: data.secrets }); return value; },
    modelOption() {
      const rows = seen.agentSecretRead("agent-models").items.filter((row: any) => row.id === data.model_id); expect(rows).toHaveLength(1);
      expect(rows[0].scope).toEqual({ kind: "project", project_id: data.project_id }); expect(rows[0].capabilities.reasoning_efforts).toEqual([]);
      return `${rows[0].name} · ${rows[0].provider_name}（项目）`;
    },
    directory(config: Record<string, any>) {
      const rows = seen.agentSecretRead("agent-directory").items.filter((row: any) => row.id === agentID); expect(rows).toHaveLength(1);
      expect(Object.keys(rows[0]).sort()).toEqual(directoryKeys); for (const key of directoryKeys) expect(rows[0][key]).toEqual(config[key]);
    },
    requireReceipt(receipt: Record<string, any>, version: string, allowed: readonly string[], before?: Record<string, any>) {
      expect(Object.keys(receipt).sort()).toEqual(before ? ["agent", "changed", "event_ids"] : ["agent", "changed", "event_ids", "runner_mounts"]);
      if (!before) expect(receipt.runner_mounts).toEqual([]);
      expect(receipt.changed).toBe(true); expect(receipt.event_ids).toHaveLength(1); expect(receipt.event_ids[0]).toMatch(id);
      expect(receipt.agent.version).toBe(version); expect(Object.keys(receipt.agent).sort()).toEqual(configurationKeys);
      expect(receipt.agent.allowed_secret_variable_ids).toEqual(allowed);
      if (before) {
        const { version: _, updated_at: oldTime, allowed_secret_variable_ids: __, ...unchanged } = before;
        expect(receipt.agent).toEqual({ ...unchanged, version, updated_at: receipt.agent.updated_at, allowed_secret_variable_ids: allowed });
        expect(receipt.agent.updated_at > oldTime).toBe(true);
      }
    },
    async finish() {
      expect(creates).toBe(data.mode === "secret-allowlist-normal" ? 1 : 0); expect(updates).toBe(inputs.updates.length); expect(lookups).toBe(data.mode === "secret-allowlist-lost" ? 1 : 0);
      await seen.verify(); await seen.endDocument(); wire.complete(); completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "agent-secret-allowlist-wire.json"), JSON.stringify(wire.snapshot(), null, 2) + "\n", { mode: 0o600 });
      writeFileSync(join(evidence, "agent-secret-allowlist-stage.json"), JSON.stringify({ stage, status, creates, updates, lookups, error_locations: errors.map(error => error.stack?.match(/(?:agent-secret-allowlist\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `agent-secret-allowlist-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-secret-allowlist-result.json"), JSON.stringify({ completed, input_hash: data.input_hash, original_requests: proof.original_requests, all_observed: proof.all_observed, creates, updates, lookups, configurations: seen.requests("agent-configuration").length, secret_pages: seen.requests("agent-secrets").length }), { mode: 0o600 });
      }
    },
  };
}
export async function agentSecretScreenshot(page: Page, mode: string, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible"); await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_EVIDENCE!; mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `agent-secret-allowlist-${mode}-${name}.png`), fullPage: false, animations: "disabled" });
}
