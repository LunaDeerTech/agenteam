import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests, secretWriteEvidenceValue } from "./task-review.helpers";
import { configurationKeys } from "./agent-configuration.helpers";
import type { SecretMetadata } from "./project-secret-write.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type SecretDeleteMaterial = {
  mode: "busy-reference-then-delete"; origin: string; cookie: string; project_id: string; route: string;
  target: SecretMetadata; remaining: SecretMetadata; agent_id: string; agent_name: string; agent_route: string;
  initial: Record<string, any>; input_hash: string;
};
export function secretDeleteMaterial(): SecretDeleteMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secret-delete-material.json"), "utf8"));
  expect(Object.keys(data).sort()).toEqual(["mode", "origin", "cookie", "project_id", "route", "target", "remaining", "agent_id", "agent_name", "agent_route", "initial", "input_hash"].sort());
  expect(data.mode).toBe("busy-reference-then-delete"); expect(data.mode).toBe(process.env.AGENTEAM_PROJECT_SECRET_DELETE_WEB_CASE);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const origin = new URL(data.origin);
  expect(origin.protocol).toBe("http:"); expect(origin.hostname).toBe("127.0.0.1"); expect(origin.port).not.toBe(""); expect(origin.origin).toBe(data.origin);
  expect(data.project_id).toMatch(id); expect(data.agent_id).toMatch(id); expect(typeof data.cookie).toBe("string"); expect(data.cookie.length).toBeGreaterThan(0);
  expect(data.route).toMatch(/^\/[A-Za-z0-9-]+\/[A-Za-z0-9._-]+\/settings\/secrets$/);
  expect(data.agent_route).toBe(data.route.replace(/\/secrets$/, "/agents"));
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/); expect(data.input_hash).toBe(process.env.AGENTEAM_PROJECT_SECRET_DELETE_WEB_INPUT_HASH);
  for (const secret of [data.target, data.remaining]) {
    const safe = secretWriteEvidenceValue(secret, "detail", true);
    expect(safe.project_id).toBe(data.project_id); expect(safe.id).toMatch(id); expect(safe.version).toBe("1");
    expect(safe.created_at).toBe(safe.updated_at); expect(Number.isFinite(Date.parse(safe.created_at))).toBe(true);
  }
  expect(data.target.id).not.toBe(data.remaining.id);
  expect(Object.keys(data.initial).sort()).toEqual(configurationKeys);
  expect(data.initial).toMatchObject({ id: data.agent_id, project_id: data.project_id, name: data.agent_name, version: "1", lifecycle: "active", allowed_secret_variable_ids: [data.target.id] });
  return data;
}
export async function observeSecretDelete(page: Page, data: SecretDeleteMaterial) {
  const base = `/api/v1/projects/${data.project_id}`, prefix = base + "/secret-variables";
  let deletes = 0, updates = 0, stage = "entry", completed = false;
  const keys = new Set<string>();
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname, method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(data.origin);
    if (method === "GET") {
      expect(request.postDataBuffer()).toBeNull(); expect(request.headers()["idempotency-key"]).toBeUndefined();
      const globals: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
      if (globals[path]) return globals[path];
      if (path === base && !url.search) return "project";
      for (const [suffix, name] of [["secret-variables", "secrets"], ["agents", "agent-directory"], ["available-chat-models", "agent-models"], ["available-runners", "agent-runners"]]) {
        if (path === base + "/" + suffix) { expect(url.search).toBe("?limit=50"); return name; }
      }
      if (!url.search) {
        if (path === prefix + "/" + data.target.id) { expect(deletes).toBeLessThan(2); return "secret"; }
        if (path === prefix + "/" + data.remaining.id) { expect(deletes).toBe(2); return "secret"; }
        if (path === `${base}/agents/${data.agent_id}/configuration`) return "agent-configuration";
        if (path === `${base}/agents/${data.agent_id}/runner-mounts`) return "agent-mounts";
      }
      throw Error("SECRET_DELETE_UNDECLARED_READ");
    }
    expect(url.search).toBe("");
    const key = request.headers()["idempotency-key"]!;
    expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/); expect(keys.has(key)).toBe(false); keys.add(key);
    if (method === "DELETE" && path === prefix + "/" + data.target.id) {
      expect(deletes).toBeLessThan(2); expect(updates).toBe(deletes);
      expect(request.postDataJSON()).toEqual({ expected_version: "1" }); deletes++; return "delete";
    }
    if (method === "PATCH" && path === `${base}/agents/${data.agent_id}/configuration`) {
      expect(deletes).toBe(1); expect(updates++).toBe(0);
      expect(request.postDataJSON()).toEqual({ expected_version: "1", request: { allowed_secret_variable_ids: [] } }); return "update";
    }
    throw Error("SECRET_DELETE_UNDECLARED_WRITE");
  };
  const evidence = process.env.AGENTEAM_PROJECT_SECRET_DELETE_WEB_EVIDENCE!;
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_PROJECT_SECRET_DELETE_WEB_DIST!, evidence, inputHash: data.input_hash,
    prefix: "project-secret-delete", endpoint: classify, secretWrite: true, secretDelete: true,
    lookupCommand: "project.secret_variable.delete",
  });
  function directory(remainingOnly: boolean) {
    const value = seen.secretDeleteRead("secrets");
    expect(Object.keys(value)).toEqual(["items"]);
    expect(value.items).toEqual(remainingOnly ? [data.remaining] : [data.target, data.remaining]);
  }
  return { ...seen,
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("SECRET_DELETE_STAGE"); stage = value; },
    async initial() { await seen.verify(); expect(deletes).toBe(0); expect(updates).toBe(0); directory(false); },
    async selected() { await seen.verify(); expect(seen.secretDeleteRead("secret")).toEqual(data.target); },
    async cancelled() { await seen.verify(); expect(deletes).toBe(0); expect(updates).toBe(0); },
    async busy() {
      await seen.verify(); expect(deletes).toBe(1); expect(updates).toBe(0);
      expect(seen.receipt("delete", 0)).toEqual({ code: "RESOURCE_BUSY", commit_state: "not_committed" });
      expect(seen.secretDeleteRead("secret")).toEqual(data.target);
    },
    async originalAgent() { await seen.verify(); expect(seen.secretDeleteRead("agent-configuration")).toEqual(data.initial); },
    async secretChoices() { await seen.verify(); directory(false); },
    async cleared() {
      await seen.verify(); expect(deletes).toBe(1); expect(updates).toBe(1);
      const receipt = seen.receipt("update", 0), current = seen.secretDeleteRead("agent-configuration");
      expect(Object.keys(receipt).sort()).toEqual(["agent", "changed", "event_ids"]);
      expect(receipt.changed).toBe(true); expect(receipt.event_ids).toHaveLength(1); expect(receipt.event_ids[0]).toMatch(id);
      const { version: _, updated_at: before, allowed_secret_variable_ids: __, ...unchanged } = data.initial;
      expect(receipt.agent).toEqual({ ...unchanged, version: "2", updated_at: receipt.agent.updated_at, allowed_secret_variable_ids: [] });
      expect(receipt.agent.updated_at > before).toBe(true); expect(current).toEqual(receipt.agent);
    },
    async returned() { await seen.verify(); directory(false); expect(deletes).toBe(1); expect(updates).toBe(1); },
    async deleted() {
      await seen.verify(); expect(deletes).toBe(2); expect(updates).toBe(1);
      const receipt = seen.receipt("delete", 1);
      expect(Object.keys(receipt).sort()).toEqual(["audit_id", "changed", "command", "deleted", "event_id"]);
      expect(receipt.command).toBe("project.secret_variable.delete"); expect(receipt.changed).toBe(true);
      expect(receipt.deleted).toEqual({ id: data.target.id, project_id: data.project_id, type: "secret", version: "2", deleted_at: receipt.deleted.deleted_at });
      expect(Number.isFinite(Date.parse(receipt.deleted.deleted_at))).toBe(true);
      expect(receipt.audit_id).toMatch(id); expect(receipt.event_id).toMatch(id); directory(true);
    },
    async finish() {
      await seen.verify(); expect(seen.secretDeleteRead("secret")).toEqual(data.remaining);
      expect(deletes).toBe(2); expect(updates).toBe(1); expect(seen.requests("lookup")).toHaveLength(0);
      await seen.endDocument(); completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "project-secret-delete-stage.json"), JSON.stringify({ stage, status, deletes, updates,
        error_locations: errors.map(error => error.stack?.match(/(?:project-secret-delete\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `project-secret-delete-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-secret-delete-result.json"), JSON.stringify({ completed, input_hash: data.input_hash,
          original_requests: proof.original_requests, all_observed: proof.all_observed, deletes, updates, lookups: proof.lookups }), { mode: 0o600 });
      }
    },
  };
}
export async function secretDeleteScreenshot(page: Page, name: "1440-light-busy" | "390-dark-current") {
  expect(await page.locator("textarea.secret-material").count()).toBe(0);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible"); await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const evidence = process.env.AGENTEAM_PROJECT_SECRET_DELETE_WEB_EVIDENCE!; mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(evidence, `project-secret-delete-${name}.png`), fullPage: true, animations: "disabled" });
}
