import { expect, type Page, type Request, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";
import { createAgentRunnerWireState } from "./agent-runner-mounts.helpers";

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type SystemRunnerMaterial = {
  mode: "system-runner-normal" | "system-runner-lost";
  origin: string; cookie: string; route: "/system/runners"; input_hash: string;
  draft: { name: string; description: string; tags: string[]; root_path: string };
  patch: { expected_version: "1"; name: string; description: string; tags: string[] };
};
export function systemRunnerMaterial(): SystemRunnerMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "system-runner-management-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["cookie", "draft", "input_hash", "mode", "origin", "patch", "route"]);
  expect(["system-runner-normal", "system-runner-lost"]).toContain(data.mode);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const origin = new URL(data.origin);
  expect(origin.protocol).toBe("http:"); expect(origin.hostname).toBe("127.0.0.1"); expect(origin.port).not.toBe(""); expect(origin.origin).toBe(data.origin);
  expect(data.route).toBe("/system/runners"); expect(typeof data.cookie).toBe("string"); expect(data.cookie.length > 0).toBe(true);
  expect(data.input_hash).toMatch(/^[a-f0-9]{64}$/); expect(data.input_hash).toBe(process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_INPUT_HASH);
  expect(data.draft).toEqual({ name: "Browser managed Runner", description: "Created by the current system administrator.", tags: ["browser"], root_path: "/owned-system-runner-fixture/workspace" });
  expect(data.patch).toEqual({ expected_version: "1", name: "Renamed browser Runner", description: "Metadata edited without connecting a device.", tags: ["browser", "managed"] });
  return data;
}

// Same actual CDP state as the accepted HTTP Agent Runner profile. The new
// wrapper owns its own session and waits for actual detach before final check.
export async function observeSystemRunnerWire(page: Page, origin: string) {
  const state = createAgentRunnerWireState(origin), session = await page.context().newCDPSession(page);
  const names = { request: "Network.requestWillBeSent", requestExtra: "Network.requestWillBeSentExtraInfo", response: "Network.responseReceived", responseExtra: "Network.responseReceivedExtraInfo", cached: "Network.requestServedFromCache" };
  const listeners = Object.entries(names).map(([kind, name]) => ({ name, listener: (event: any) => state.feed(kind, event) }));
  let detached = false;
  async function close() {
    if (detached) return;
    try { await session.detach(); detached = true; }
    catch (error) { state.failed("SYSTEM_RUNNER_WIRE_DETACH_FAILED"); throw error; }
    finally { for (const { name, listener } of listeners) session.off(name, listener); state.retire(); }
  }
  try { for (const { name, listener } of listeners) session.on(name, listener); await session.send("Network.enable"); }
  catch (error) { try { await close(); } finally { throw error; } }
  return {
    async headers(expected: { method: string; url: string; status: number; requestID: string }) {
      let proof: ReturnType<typeof state.match>;
      await expect.poll(() => { proof = state.match(expected); return proof !== undefined; }, { timeout: 5_000 }).toBe(true);
      return proof!;
    }, complete: () => state.complete(), close,
    snapshot: () => ({ ...state.snapshot(), actual_detach: detached }),
  };
}

export async function observeSystemRunner(page: Page, data: SystemRunnerMaterial) {
  const prefix = "/api/v1/system/runners";
  let target: string | undefined, originalKey: string | undefined, originalRequest: any;
  let creates = 0, updates = 0, enrollments = 0, revokes = 0, lookups = 0, completed = false, stage = "entry";
  const keys = new Set<string>();
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), method = request.method(), path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(data.origin);
    if (method === "GET") {
      expect(request.postDataBuffer()).toBeNull(); expect(request.headers()["idempotency-key"]).toBeUndefined();
      if (path === "/api/v1/session" && !url.search) return "session";
      if (path === "/api/v1/projects") return "projects";
      if (path === prefix) { expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]); return "runner-list"; }
      if (target && path === prefix + "/" + target && !url.search) return "runner-detail";
    }
    expect(url.search).toBe("");
    const body = request.postDataJSON(), key = request.headers()["idempotency-key"]!;
    expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
    if (method === "POST" && path === prefix) {
      expect(creates++).toBe(0); expect(body.runner_id).toMatch(uuid);
      expect(body).toEqual({ runner_id: body.runner_id, ...data.draft });
      target = body.runner_id; originalRequest = body; originalKey = key;
      expect(keys.has(key)).toBe(false); keys.add(key); return "create";
    }
    if (target && method === "POST" && path === prefix + "/" + target + "/commands/lookup") {
      expect(data.mode).toBe("system-runner-lost"); expect(lookups++).toBe(0); expect(creates).toBe(1);
      expect(body).toEqual({ command: "runner.create", request: originalRequest }); expect(key).toBe(originalKey); return "lookup";
    }
    expect(keys.has(key)).toBe(false); keys.add(key);
    if (target && method === "PATCH" && path === prefix + "/" + target) {
      expect(data.mode).toBe("system-runner-normal"); expect(updates++).toBe(0); expect(body).toEqual(data.patch); return "update";
    }
    if (target && method === "POST" && path === prefix + "/" + target + "/enrollment") {
      expect(enrollments++).toBe(0); expect(body).toEqual({ expected_version: data.mode === "system-runner-normal" ? "2" : "1" });
      if (data.mode === "system-runner-lost") expect(lookups).toBe(1);
      return "enrollment";
    }
    if (target && method === "POST" && path === prefix + "/" + target + "/revoke") {
      expect(data.mode).toBe("system-runner-normal"); expect(revokes++).toBe(0); expect(enrollments).toBe(1); expect(body).toEqual({ expected_version: "3" }); return "revoke";
    }
    throw Error("UNOBSERVED_SYSTEM_RUNNER_ENDPOINT");
  };
  const wire = await observeSystemRunnerWire(page, data.origin);
  let seen: Awaited<ReturnType<typeof observeTaskRequests>>;
  try {
    seen = await observeTaskRequests(page, { mode: data.mode, cookie: data.cookie, project_id: "" }, {
      dist: process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_DIST!, evidence: process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE!, inputHash: data.input_hash,
      prefix: "system-runner-management", endpoint: classify, lookupCommand: "runner.create", systemRunnerManagement: true, systemRunnerWireHeaders: wire.headers,
      ...(data.mode === "system-runner-lost" ? { cut: "create" as const } : {}),
    });
  } catch (error) { try { await wire.close(); } finally { throw error; } }
  return {
    ...seen,
    stage(value: string) { if (!/^[a-z-]{1,48}$/.test(value)) throw Error("SYSTEM_RUNNER_STAGE"); stage = value; },
    async materialMatches(field: Locator) {
      // Only a boolean leaves the document. No assertion receives the token.
      const matched = await field.evaluate(element => (window as any).__taskReviewProof.runnerMaterialMatches((element as HTMLInputElement).value));
      expect(matched, "one-time field must equal the original public result").toBe(true);
    },
    current(version: string, generation: string) {
      const value = seen.systemRunnerRead("runner-detail");
      expect(Object.keys(value).sort()).toEqual(["id", "name", "description", "tags", "root_path", "version", "credential_generation", "status", "public_key_fingerprint", "enrolled_at", "last_seen_at", "last_hello", "created_at", "updated_at"].sort());
      expect(value).toMatchObject({ id: target, root_path: data.draft.root_path, version, credential_generation: generation, status: "offline", public_key_fingerprint: null, enrolled_at: null, last_seen_at: null, last_hello: null });
      const metadata = data.mode === "system-runner-normal" && version !== "1" ? data.patch : data.draft;
      expect(value.name).toBe(metadata.name); expect(value.description).toBe(metadata.description); expect(value.tags).toEqual(metadata.tags);
      return value;
    },
    async finish() {
      await seen.verify(); wire.complete();
      expect(creates).toBe(1); expect(enrollments).toBe(1); expect(updates).toBe(data.mode === "system-runner-normal" ? 1 : 0);
      expect(revokes).toBe(data.mode === "system-runner-normal" ? 1 : 0); expect(lookups).toBe(data.mode === "system-runner-lost" ? 1 : 0);
      completed = true;
    },
    async retire() { try { await seen.retire(); wire.complete(); } finally { await wire.close(); } },
    save(status?: string) {
      seen.save(status);
      writeFileSync(join(process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE!, `system-runner-management-${data.mode}-wire.json`), JSON.stringify({ stage, ...wire.snapshot() }, null, 2), { mode: 0o600 });
      const safe = JSON.parse(readFileSync(join(process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE!, `system-runner-management-${data.mode}.json`), "utf8"));
      writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "system-runner-management-result.json"), JSON.stringify({ completed, input_hash: data.input_hash, original_requests: safe.original_requests, all_observed: safe.all_observed, creates, updates, enrollments, revokes, lookups, lists: seen.requests("runner-list").length, details: seen.requests("runner-detail").length }), { mode: 0o600 });
    },
  };
}

export async function systemRunnerScreenshot(page: Page, mode: string, name: string) {
  // Screenshots are allowed only after the one-time panel has been dismissed.
  const visibleMaterial = await page.evaluate(() => /[A-Za-z0-9_-]{43}/.test(document.body.innerText) || [...document.querySelectorAll("input,textarea")].some(element => /[A-Za-z0-9_-]{43}/.test((element as HTMLInputElement).value)));
  expect(visibleMaterial, "screenshots must exclude all one-time material").toBe(false);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: join(process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE!, `${mode}-${name}.png`), fullPage: false });
}
