import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests, secretWriteEvidenceValue } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type SecretMetadata = { id: string; project_id: string; type: "secret"; name: string; description: string; version: string; created_at: string; updated_at: string };
export type SecretWriteMaterial = { mode: "secret-write-normal" | "secret-write-lost"; origin: string; cookie: string; project_id: string; route: string; seeded: SecretMetadata | null; input_hash: string };
export function secretWriteMaterial(): SecretWriteMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secret-write-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "cookie", "project_id", "route", "seeded", "input_hash"].sort());
  expect(data.mode).toBe(process.env.AGENTEAM_PROJECT_SECRET_WRITE_WEB_CASE);
  expect(["secret-write-normal", "secret-write-lost"].includes(data.mode)).toBe(true);
  expect(data.project_id).toMatch(id); expect(typeof data.cookie).toBe("string");
  expect(data.cookie.length > 0).toBe(true);
  expect(data.route).toBe("/secrets-owner/project-secret-write/settings/secrets");
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.input_hash).toBe(process.env.AGENTEAM_PROJECT_SECRET_WRITE_WEB_INPUT_HASH);
  if (data.mode === "secret-write-normal") expect(data.seeded).toBeNull();
  else {
    const value = secretWriteEvidenceValue(data.seeded, "detail");
    expect(value.project_id).toBe(data.project_id); expect(value.id).toMatch(id);
    expect(value.name).toBe("BROWSER_SECRET"); expect(value.version).toBe("1");
  }
  return { ...data, origin: process.env.AGENTEAM_AUTH_WEB_ORIGIN! };
}
export type SecretWriteScope = { target?: string; creates: number; updates: number; lookups: number; lists: number; gets: number };
// Keep this exact classifier callable by small method controls. It rejects before
// retaining any request payload; material is never part of its state or errors.
export function classifySecretWriteRequest(request: Pick<Request, "url" | "method" | "postDataBuffer" | "headers">, data: SecretWriteMaterial, state: SecretWriteScope): string | undefined {
  const fail = () => { throw Error("SECRET_WRITE_REQUEST_SCOPE"); };
  const url = new URL(request.url()), path = url.pathname, method = request.method();
  if (!path.startsWith("/api/")) return undefined;
  if (url.origin !== data.origin) return fail();
  const project = `/api/v1/projects/${data.project_id}`, prefix = project + "/secret-variables", lost = data.mode === "secret-write-lost";
  if (method === "GET") {
    if (request.postDataBuffer() !== null || request.headers()["idempotency-key"] !== undefined) return fail();
    const global: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
    if (global[path]) return global[path];
    if (path === project && !url.search) return "project";
    if (path === prefix && url.search === "?limit=50" && state.lists < (lost ? 2 : 4)) { state.lists++; return "secrets"; }
    if (state.target && path === prefix + "/" + state.target && !url.search && state.gets < (lost ? 2 : 3)) { state.gets++; return "secret"; }
    return fail();
  }
  if (url.search || !/^[A-Za-z0-9._:/-]{1,128}$/.test(request.headers()["idempotency-key"] ?? "")) return fail();
  let body: any;
  try { body = JSON.parse(request.postDataBuffer()!.toString("utf8")); } catch { return fail(); }
  const keys = (value: any, expected: string[]) => value && typeof value === "object" && !Array.isArray(value) && Object.keys(value).sort().join() === expected.slice().sort().join();
  if (method === "POST" && path === prefix && !lost && state.creates === 0 && state.updates === 0 && state.lists === 1 &&
      keys(body, ["request"]) && keys(body.request, ["variable_id", "name", "description", "value"]) && id.test(body.request.variable_id) &&
      body.request.name === "BROWSER_SECRET" && body.request.description === "创建说明" && typeof body.request.value === "string") {
    state.target = body.request.variable_id; state.creates++; return "create";
  }
  if (method === "PATCH" && state.target && path === prefix + "/" + state.target && state.updates < (lost ? 1 : 2) &&
      keys(body, ["expected_version", "request"]) && body.expected_version === String(state.updates + 1) &&
      (lost ? state.gets === 1 : state.creates === 1 && state.gets === state.updates + 1)) {
    const metadata = !lost && state.updates === 0;
    if (metadata ? keys(body.request, ["name", "description"]) && body.request.name === "RENAMED_BROWSER_SECRET" && body.request.description === "更新后的安全说明" :
        keys(body.request, ["value"]) && typeof body.request.value === "string") { state.updates++; return "update"; }
  }
  if (method === "POST" && path === prefix + "/commands/lookup" && lost && state.updates === 1 && state.lookups === 0 &&
      keys(body, ["command", "target_id", "expected_version"]) && body.command === "project.secret_variable.update" && body.target_id === state.target && body.expected_version === "1") {
    state.lookups++; return "lookup";
  }
  return fail();
}
export async function observeSecretWrite(page: Page, data: SecretWriteMaterial) {
  const state: SecretWriteScope = { target: data.seeded?.id, creates: 0, updates: 0, lookups: 0, lists: 0, gets: 0 };
  const evidence = process.env.AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE!, inputHash = data.input_hash;
  const seen = await observeTaskRequests(page, data, { dist: process.env.AGENTEAM_PROJECT_SECRET_WRITE_WEB_DIST!, evidence, inputHash,
    prefix: "project-secret-write", endpoint: request => classifySecretWriteRequest(request, data, state), secretWrite: true,
    lookupCommand: "project.secret_variable.update", cut: data.mode === "secret-write-lost" ? "update" : undefined });
  let stage = "entry", completed = false, verified = 0;
  return { ...seen,
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("SECRET_WRITE_STAGE"); stage = value; },
    async initial() { await seen.verify(); expect(state.lists).toBe(1); expect(state.gets).toBe(0); expect(seen.secretRead("secrets", 0)).toEqual({ items: data.seeded ? [data.seeded] : [] }); },
    async seedSelected() { await seen.verify(); expect(state.gets).toBe(1); expect(seen.secretRead("secret", 0)).toEqual(data.seeded); },
    async current(version: string, name: string, description: string) {
      await seen.verify();
      const lost = data.mode === "secret-write-lost", count = lost ? 2 : Number(version);
      expect(state.gets).toBe(count); expect(state.lists).toBe(lost ? 2 : Number(version) + 1);
      const value = seen.secretRead("secret", state.gets - 1);
      expect(value.id).toBe(state.target); expect(value.project_id).toBe(data.project_id);
      expect(value.version).toBe(version); expect(value.name).toBe(name); expect(value.description).toBe(description);
      expect(seen.secretRead("secrets", state.lists - 1)).toEqual({ items: [value] });
      const receipt = lost ? seen.receipt("lookup", 0).receipt : seen.receipt(version === "1" ? "create" : "update", version === "1" ? 0 : Number(version) - 2);
      expect(receipt.variable).toEqual(value); verified++; return value;
    },
    async uncertain() { await seen.verify(); expect(state.updates).toBe(1); expect(state.lookups).toBe(0); expect(state.gets).toBe(1); expect(state.lists).toBe(1); },
    async finish() {
      await seen.verify(); const lost = data.mode === "secret-write-lost";
      expect(state.creates).toBe(lost ? 0 : 1); expect(state.updates).toBe(lost ? 1 : 2); expect(state.lookups).toBe(lost ? 1 : 0);
      expect(verified).toBe(lost ? 1 : 3); await seen.endDocument(); completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "project-secret-write-stage.json"), JSON.stringify({ stage, status, ...state, current_verified: verified,
        error_locations: errors.map(error => error.stack?.match(/(?:project-secret-write\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `project-secret-write-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secret-write-result.json"), JSON.stringify({ case: data.mode, completed, input_hash: inputHash,
          original_requests: proof.original_requests, all_observed: proof.all_observed, lookups: proof.lookups, declared_receipt_cut: proof.declared_receipt_cut }), { mode: 0o600 });
      }
    },
  };
}
// No generated material crosses the browser evaluation boundary. The real paste
// handler receives original CR/LF/TAB bytes; retained DOM nodes are checked later.
export async function pasteSecretMaterial(page: Page) {
  expect(await page.evaluate(() => {
    const field = document.querySelector<HTMLTextAreaElement>("textarea.secret-material");
    if (!field || field.disabled) return false;
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    let value = "  " + Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("") + "\r\n\tsecond\rthird\n  "; bytes.fill(0);
    (window as any).__taskReviewProof.expectSecretMaterial(value);
    const clipboard = new DataTransfer(); clipboard.setData("text/plain", value);
    field.dispatchEvent(new ClipboardEvent("paste", { clipboardData: clipboard, bubbles: true, cancelable: true }));
    const matches = field.value === value.replace(/\r\n?/g, "\n");
    ((window as any).__secretWriteOldInputs ??= []).push(field);
    clipboard.clearData(); value = ""; return matches;
  })).toBe(true);
}
export async function secretInputsCleared(page: Page, clear = false) {
  expect(await page.evaluate(clear => {
    const nodes: HTMLTextAreaElement[] = [...((window as any).__secretWriteOldInputs ?? []), ...document.querySelectorAll<HTMLTextAreaElement>("textarea.secret-material")];
    if (clear) for (const field of nodes) field.value = "";
    return nodes.every(field => field.value === "");
  }, clear)).toBe(true);
}
export async function secretWriteScreenshot(page: Page, name: "1440-light-current" | "390-dark-current") {
  await secretInputsCleared(page);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible"); await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE!; mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `project-secret-write-${name}.png`), fullPage: true, animations: "disabled" });
}
