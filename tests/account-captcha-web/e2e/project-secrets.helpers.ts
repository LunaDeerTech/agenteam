import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const fields = ["id", "project_id", "type", "name", "description", "version", "created_at", "updated_at"].sort();
export type SecretMetadata = { id: string; project_id: string; type: "secret"; name: string; description: string; version: string; created_at: string; updated_at: string };
export type SecretsMaterial = {
  mode: "metadata-pagination-and-detail"; origin: string; cookie: string; project_id: string;
  route: string; items: SecretMetadata[]; selected: SecretMetadata; input_hash: string;
};
function metadata(value: SecretMetadata, project: string) {
  expect(Object.keys(value).sort()).toEqual(fields);
  expect(value.id).toMatch(id);
  expect(value.project_id).toBe(project);
  expect(value.type).toBe("secret");
  expect(value.name).toMatch(/^SECRET_0(?:[0-4][0-9]|5[01])$/);
  expect(typeof value.description).toBe("string");
  expect(Buffer.byteLength(value.description)).toBeLessThanOrEqual(4096);
  expect(value.version).toMatch(/^[1-9][0-9]{0,18}$/);
  expect(BigInt(value.version) <= 9223372036854775807n).toBe(true);
  for (const instant of [value.created_at, value.updated_at]) {
    expect(instant).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/);
    expect(Number.isFinite(Date.parse(instant))).toBe(true);
  }
  expect(value.updated_at >= value.created_at).toBe(true);
}
export function secretsMaterial(): SecretsMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secrets-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "origin", "cookie", "project_id", "route", "items", "selected", "input_hash"].sort());
  expect(data.mode).toBe("metadata-pagination-and-detail");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.project_id).toMatch(id);
  expect(typeof data.cookie).toBe("string");
  expect(data.cookie.length).toBeGreaterThan(0);
  expect(data.route).toBe("/secrets-owner/project-secrets/settings/secrets");
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.input_hash).toBe(process.env.AGENTEAM_PROJECT_SECRETS_WEB_INPUT_HASH);
  expect(data.items).toHaveLength(51);
  for (const [index, value] of data.items.entries()) {
    metadata(value, data.project_id);
    expect(value.name).toBe(`SECRET_${String(index + 1).padStart(3, "0")}`);
  }
  expect(new Set(data.items.map((value: SecretMetadata) => value.id)).size).toBe(51);
  metadata(data.selected, data.project_id);
  expect(data.selected).toEqual(data.items[50]);
  return data;
}

export async function observeSecrets(page: Page, data: SecretsMaterial) {
  const project = `/api/v1/projects/${data.project_id}`, prefix = project + "/secret-variables";
  const lists: Request[] = [], gets: Request[] = [];
  let nextCursor: string | undefined, secondPage = false, selected = false, refreshed = false, completed = false, stage = "entry";
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(request.method()).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined();
    const global: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
    if (global[path]) return global[path];
    if (path === project && !url.search) return "project";
    if (path === prefix) {
      expect(lists.length).toBeLessThan(2);
      expect(gets).toHaveLength(0);
      expect([...url.searchParams.keys()]).toEqual(lists.length === 0 ? ["limit"] : ["limit", "cursor"]);
      expect(url.searchParams.get("limit")).toBe("50");
      if (lists.length === 1) {
        expect(nextCursor).toBeTruthy();
        expect(url.searchParams.get("cursor")).toBe(nextCursor);
      }
      lists.push(request);
      return "secrets";
    }
    if (path === prefix + "/" + data.selected.id && !url.search) {
      expect(secondPage).toBe(true);
      expect(gets.length).toBeLessThan(2);
      if (gets.length === 1) expect(selected).toBe(true);
      gets.push(request);
      return "secret";
    }
    throw Error("UNOBSERVED_PROJECT_SECRETS_ENDPOINT");
  };
  const evidence = process.env.AGENTEAM_PROJECT_SECRETS_WEB_EVIDENCE!, inputHash = data.input_hash;
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_PROJECT_SECRETS_WEB_DIST!, evidence, inputHash,
    prefix: "project-secrets", endpoint: classify, lookupCommand: null, secretReads: true,
  });
  return {
    ...seen,
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("INVALID_SECRETS_STAGE"); stage = value; },
    firstPage() {
      expect(lists).toHaveLength(1);
      const body = seen.secretRead("secrets", 0);
      expect(Object.keys(body).sort()).toEqual(["items", "next_cursor"]);
      expect(body.items).toEqual(data.items.slice(0, 50));
      expect(typeof body.next_cursor).toBe("string");
      expect(body.next_cursor.length).toBeGreaterThan(0);
      expect(body.next_cursor.length).toBeLessThanOrEqual(8192);
      nextCursor = body.next_cursor;
      expect(gets).toHaveLength(0);
    },
    secondPage() {
      expect(lists).toHaveLength(2);
      expect(seen.secretRead("secrets", 1)).toEqual({ items: [data.selected] });
      expect(gets).toHaveLength(0);
      secondPage = true;
    },
    selection() {
      expect(gets).toHaveLength(1);
      expect(seen.secretRead("secret", 0)).toEqual(data.selected);
      selected = true;
    },
    refresh() {
      expect(gets).toHaveLength(2);
      expect(seen.secretRead("secret", 1)).toEqual(data.selected);
      refreshed = true;
    },
    async finish() {
      expect(lists).toHaveLength(2);
      expect(gets).toHaveLength(2);
      expect(refreshed).toBe(true);
      expect(seen.requests("lookup")).toHaveLength(0);
      await seen.verify();
      await seen.endDocument();
      completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "project-secrets-stage.json"), JSON.stringify({ stage, status, lists: lists.length, gets: gets.length, selected, refreshed, error_locations: errors.map(error => error.stack?.match(/(?:project-secrets\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `project-secrets-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secrets-result.json"), JSON.stringify({ completed, input_hash: inputHash, original_requests: proof.original_requests, all_observed: proof.all_observed, lookups: proof.lookups, lists: lists.length, gets: gets.length }), { mode: 0o600 });
      }
    },
  };
}
export async function secretsScreenshot(page: Page, name: "1440-light-detail" | "390-dark-detail") {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_PROJECT_SECRETS_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `project-secrets-${name}.png`), fullPage: true, animations: "disabled" });
}
