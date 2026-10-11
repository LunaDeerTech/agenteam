import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const instant = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/;
const fields = ["id", "call_id", "attempt_index", "project_id", "consumer_kind", "purpose", "agent_id", "execution_id", "meeting_id", "provider_id", "model_id", "provider_name", "model_name", "provider_model_id", "protocol", "model_type", "live_provider_id", "live_model_id", "dispatch", "started_at", "dispatched_at", "final", "usage", "provider_request_id"].sort();
export const tokens = ["input_tokens", "output_tokens", "total_tokens", "cached_input_tokens", "cache_write_tokens", "reasoning_tokens"] as const;
export type UsageRow = { id: string; started_at: string; [key: string]: any };
export type UsageMaterial = { mode: "paginated-current-read"; origin: string; cookie: string; project_id: string; route: string; items: UsageRow[]; input_hash: string };
function record(value: UsageRow, project: string) {
  expect(Object.keys(value).sort()).toEqual(fields);
  for (const key of ["id", "call_id", "project_id", "provider_id", "model_id"]) expect(value[key]).toMatch(id);
  for (const key of ["agent_id", "execution_id", "meeting_id", "live_provider_id", "live_model_id"]) if (value[key] !== null) expect(value[key]).toMatch(id);
  expect(value.project_id).toBe(project);
  expect(value.attempt_index).toMatch(/^[1-9][0-9]{0,18}$/);
  expect(BigInt(value.attempt_index) <= 9223372036854775807n).toBe(true);
  expect(value.started_at).toMatch(instant);
  expect(value.dispatch).toBe("sent");
  expect(value.dispatched_at).toMatch(instant);
  expect(Object.keys(value.usage).sort()).toEqual([...tokens, "source"].sort());
  for (const field of tokens) if (value.usage[field] !== null) {
    expect(value.usage[field]).toMatch(/^(0|[1-9][0-9]{0,18})$/);
    expect(BigInt(value.usage[field]) <= 9223372036854775807n).toBe(true);
  }
  const known = tokens.some(field => value.usage[field] !== null);
  expect(value.usage.source).toBe(known ? "provider" : "unknown");
  expect(Object.keys(value.final).sort()).toEqual(["error_category", "finished_at", "status"]);
  expect(value.final.status).toBe("succeeded");
  expect(value.final.error_category).toBeNull();
  expect(value.final.finished_at).toMatch(instant);
  expect(value.final.finished_at >= value.dispatched_at && value.dispatched_at >= value.started_at).toBe(true);
  expect(value.consumer_kind).toBe("agent"); expect(value.purpose).toBe("agent_generation");
  expect(value.protocol).toBe("openai-chat-completions"); expect(value.model_type).toBe("chat");
  for (const key of ["provider_name", "model_name", "provider_model_id"]) { expect(typeof value[key]).toBe("string"); expect(value[key].length).toBeGreaterThan(0); }
}
export function usageMaterial(): UsageMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-usage-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "origin", "cookie", "project_id", "route", "items", "input_hash"].sort());
  expect(data.mode).toBe("paginated-current-read"); expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(new URL(data.origin).protocol).toBe("https:"); expect(data.project_id).toMatch(id);
  expect(typeof data.cookie).toBe("string"); expect(data.cookie.length).toBeGreaterThan(0);
  expect(data.route).toMatch(/^\/[A-Za-z0-9-]+\/[A-Za-z0-9._-]+\/settings\/model-usage$/);
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/); expect(data.input_hash).toBe(process.env.AGENTEAM_PROJECT_USAGE_WEB_INPUT_HASH);
  expect(data.items).toHaveLength(51);
  for (const [index, value] of data.items.entries()) {
    record(value, data.project_id);
    if (index) { const previous = data.items[index - 1]; expect(previous.started_at > value.started_at || (previous.started_at === value.started_at && previous.id > value.id)).toBe(true); }
  }
  expect(new Set(data.items.map((v: UsageRow) => v.id)).size).toBe(51);
  for (const value of [null, "0", "9007199254740993", "9223372036854775807"]) expect(data.items.slice(0, 50).some((row: UsageRow) => row.usage.input_tokens === value)).toBe(true);
  return data;
}

// Pure state shared by the real read-only CDP observer and its focused controls.
// Only predicates, canonical IDs and hashes survive an event callback.
export function createUsageWireState(origin: string) {
  const parsedOrigin = new URL(origin);
  if (parsedOrigin.origin !== origin || parsedOrigin.protocol !== "https:" || parsedOrigin.hostname !== "127.0.0.1" || !parsedOrigin.port) throw Error("USAGE_WIRE_ORIGIN");
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
    if (row.cached || row.request.redirect || !row.request.sameOrigin || row.request.method !== "GET") reject("USAGE_WIRE_REQUEST");
    if (!row.requestExtra || !row.response || !row.responseExtra) return undefined;
    if (!row.requestExtra.host_present || !row.requestExtra.host_matches || row.requestExtra.authority_conflict) reject("USAGE_WIRE_HOST");
    if (row.response.cache || !row.response.extra || row.response.url_sha256 !== row.request.url_sha256 || row.response.status !== row.responseExtra.status || !row.responseExtra.request_id) reject("USAGE_WIRE_RESPONSE");
    return { source: "cdp_network_extra_info" as const, cdp_request_id: row.cdp_request_id, request_id: row.responseExtra.request_id as string, method: row.request.method as string, url_sha256: row.request.url_sha256 as string, status: row.response.status as number, host_present: true as const, host_matches: true as const };
  }
  function feed(kind: string, event: any) {
    try {
      check();
      if (closed) reject("USAGE_WIRE_EVENT_AFTER_RETIRE");
      if (++eventCount > 2048 || typeof event.requestId !== "string" || !event.requestId || event.requestId.length > 256) reject("USAGE_WIRE_EVENT_BOUND");
      let row = rows.get(event.requestId);
      if (!row) {
        if (rows.size >= 512) reject("USAGE_WIRE_ROW_BOUND");
        row = { cdp_request_id: event.requestId }; rows.set(event.requestId, row);
      }
      if (row[kind] !== undefined) reject("USAGE_WIRE_DUPLICATE_EVENT");
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
      else reject("USAGE_WIRE_EVENT_KIND");
      assembled(row);
      const ids = [...rows.values()].filter(item => item.request?.api && item.responseExtra?.request_id).map(item => item.responseExtra.request_id);
      if (new Set(ids).size !== ids.length) reject("USAGE_WIRE_DUPLICATE_RID");
    } catch (error) {
      failure ??= error instanceof Error && /^USAGE_WIRE_/.test(error.message) ? error.message : "USAGE_WIRE_MALFORMED_EVENT";
    }
  }
  function complete() {
    check();
    for (const row of rows.values()) if (row.request?.api && (!assembled(row) || !row.matched)) reject("USAGE_WIRE_UNMATCHED_API");
    // ExtraInfo without its request event cannot be classified safely.
    if ([...rows.values()].some(row => !row.request)) reject("USAGE_WIRE_ORPHAN_EVENT");
  }
  return {
    feed,
    match(expected: { method: string; url: string; status: number; requestID: string }) {
      check();
      if (closed) reject("USAGE_WIRE_READ_AFTER_RETIRE");
      const matches = [...rows.values()].filter(row => row.request?.api && row.responseExtra?.request_id === expected.requestID);
      if (matches.length > 1) reject("USAGE_WIRE_AMBIGUOUS_RID");
      if (!matches.length) return undefined;
      const row = matches[0], proof = assembled(row);
      if (!proof) return undefined;
      if (proof.method !== expected.method || proof.url_sha256 !== hash(expected.url) || proof.status !== expected.status) reject("USAGE_WIRE_PW_CONFLICT");
      row.matched = true; return proof;
    },
    complete,
    retire() { closed = true; complete(); },
    failed(code: string) { failure ??= code; },
    snapshot() { return { source: "cdp_network_extra_info", event_count: eventCount, retired: closed, failure, requests: [...rows.values()].filter(row => row.request?.api).map(row => ({ ...row })) }; },
  };
}

async function observeUsageWire(page: Page, origin: string) {
  const state = createUsageWireState(origin), session = await page.context().newCDPSession(page);
  const names = { request: "Network.requestWillBeSent", requestExtra: "Network.requestWillBeSentExtraInfo", response: "Network.responseReceived", responseExtra: "Network.responseReceivedExtraInfo", cached: "Network.requestServedFromCache" } as const;
  const listeners = Object.entries(names).map(([kind, name]) => ({ name, listener: (event: any) => state.feed(kind, event) }));
  let detached = false;
  async function close() {
    if (detached) return;
    try { await session.detach(); detached = true; }
    catch (error) { state.failed("USAGE_WIRE_DETACH_FAILED"); throw error; }
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

export async function observeUsage(page: Page, data: UsageMaterial) {
  const project = `/api/v1/projects/${data.project_id}`, prefix = project + "/model-usage", lists: Request[] = [];
  let nextCursor: string | null = null, window: { from: string; to: string } | null = null, completed = false, verified = 0, stage = "entry";
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(data.origin); expect(request.method()).toBe("GET"); expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined(); expect(request.headers()["x-csrf-token"]).toBeUndefined();
    const global: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
    if (global[path]) return global[path];
    if (path === project && !url.search) return "project";
    if (path !== prefix) throw Error("UNOBSERVED_PROJECT_USAGE_ENDPOINT");
    expect(lists.length).toBeLessThan(5); expect(verified).toBe(lists.length);
    const index = lists.length, next = index === 1 || index === 3;
    expect([...url.searchParams.keys()]).toEqual(next ? ["from", "to", "limit", "cursor"] : ["from", "to", "limit"]);
    expect(url.searchParams.get("limit")).toBe("50");
    const from = url.searchParams.get("from")!, to = url.searchParams.get("to")!;
    expect(from).toMatch(instant); expect(to).toMatch(instant); expect(Date.parse(to) - Date.parse(from)).toBe(30 * 24 * 60 * 60 * 1000);
    if (index === 0) window = { from, to };
    else if (index === 4) { expect(from > window!.from && to > window!.to).toBe(true); }
    else expect({ from, to }).toEqual(window);
    if (next) { expect(nextCursor).not.toBeNull(); expect(url.searchParams.get("cursor")).toBe(nextCursor); }
    lists.push(request); return "usage";
  };
  const evidence = process.env.AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE!;
  const wire = await observeUsageWire(page, data.origin);
  let seen: Awaited<ReturnType<typeof observeTaskRequests>>;
  try { seen = await observeTaskRequests(page, data, { dist: process.env.AGENTEAM_PROJECT_USAGE_WEB_DIST!, evidence, inputHash: data.input_hash, prefix: "project-usage", endpoint: classify, lookupCommand: null, usageReads: true, usageWireHeaders: wire.headers }); }
  catch (error) { try { await wire.close(); } finally { throw error; } }
  return {
    ...seen,
    async retire() { try { await seen.retire(); wire.complete(); } finally { await wire.close(); } },
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("INVALID_USAGE_STAGE"); stage = value; },
    page(index: number) {
      expect(verified).toBe(index); expect(lists).toHaveLength(index + 1);
      const body = seen.usageRead(index), next = index === 1 || index === 3;
      expect(Object.keys(body).sort()).toEqual(["items", "next_cursor"]);
      expect(body.items).toEqual(next ? data.items.slice(50) : data.items.slice(0, 50));
      if (next) expect(body.next_cursor).toBeNull();
      else { expect(typeof body.next_cursor).toBe("string"); expect(Buffer.byteLength(body.next_cursor)).toBeGreaterThan(0); expect(Buffer.byteLength(body.next_cursor)).toBeLessThanOrEqual(8192); }
      nextCursor = body.next_cursor; verified++;
    },
    async finish() { expect(verified).toBe(5); expect(lists).toHaveLength(5); expect(seen.requests("lookup")).toHaveLength(0); await seen.verify(); await seen.endDocument(); wire.complete(); completed = true; },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "project-usage-wire.json"), JSON.stringify(wire.snapshot(), null, 2) + "\n", { mode: 0o600 });
      writeFileSync(join(evidence, "project-usage-stage.json"), JSON.stringify({ stage, status, lists: lists.length, verified, error_locations: errors.map(error => error.stack?.match(/(?:project-usage\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") { const proof = JSON.parse(readFileSync(join(evidence, `project-usage-${data.mode}.json`), "utf8")); writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-usage-result.json"), JSON.stringify({ completed, input_hash: data.input_hash, original_requests: proof.original_requests, all_observed: proof.all_observed, lookups: proof.lookups, lists: lists.length }), { mode: 0o600 }); }
    },
  };
}
export async function usageScreenshot(page: Page, name: "1440-light-detail" | "390-dark-detail" | "390-dark-returned") {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible"); await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE!; mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `project-usage-${name}.png`), fullPage: false, animations: "disabled" });
}
