import { expect, type Page, type Request } from "@playwright/test";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const sha = (value: string | Buffer) => createHash("sha256").update(value).digest("hex");
type Document = Record<string, unknown> & { id: string; title: string };
export type DeleteMaterial = {
  mode: "delete-normal" | "delete-lost";
  cookie: string;
  project_id: string;
  root_id: string;
  child_id: string;
  sibling_id: string;
  route: string;
  root: Document;
  child: Document;
  sibling: Document;
  root_text: string;
  child_text: string;
  sibling_text: string;
  input_hash: string;
};
export function deleteMaterial(): DeleteMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "knowledge-delete-material.json"), "utf8"));
  expect(data.mode).toBe(process.env.AGENTEAM_KNOWLEDGE_DELETE_WEB_CASE);
  expect(["delete-normal", "delete-lost"]).toContain(data.mode);
  for (const value of [data.project_id, data.root_id, data.child_id, data.sibling_id]) expect(value).toMatch(id);
  expect(new Set([data.root_id, data.child_id, data.sibling_id]).size).toBe(3);
  expect(data.route).toBe(`/knowledge-owner/knowledge-delete/knowledge/${data.root_id}`);
  expect(typeof data.cookie === "string" && data.cookie.length > 0).toBe(true);
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.input_hash).toBe(process.env.AGENTEAM_KNOWLEDGE_DELETE_WEB_INPUT_HASH);
  for (const [name, title, parent] of [["root", "待删除目录", null], ["child", "目录内正文", data.root_id], ["sibling", "保留旁支", null]] as const) {
    expect(Object.keys(data[name]).sort()).toEqual(["id", "project_id", "parent_document_id", "title", "content_version", "source_kind", "media_type", "status", "indexing_status", "created_by", "created_at", "updated_at"].sort());
    expect(data[name]).toMatchObject({ id: data[`${name}_id`], project_id: data.project_id, parent_document_id: parent, title, content_version: "1", source_kind: "text", media_type: "text/plain", status: "active" });
  }
  expect(data.root_text).toBe("删除前根正文 <script>仍是文本</script>。\n");
  expect(data.child_text).toBe("删除范围内子正文 中文🙂。\n");
  expect(data.sibling_text).toBe("删除后仍可读取的旁支正文。\n");
  return data;
}

export async function observeDelete(page: Page, data: DeleteMaterial) {
  const project = `/api/v1/projects/${data.project_id}`, prefix = project + "/knowledge/documents";
  const originals: Request[] = [], previews: Request[] = [], lookups: Request[] = [];
  let previewTokenHash: string | undefined, previewBodyHash: string | undefined;
  let ready = false, completed = false, current = false, stage = "entry";
  let rootContentBefore = 0, rootAncestorsBefore = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
    if (request.method() === "POST") {
      if (url.search) throw Error("KNOWLEDGE_DELETE_QUERY_REJECTED");
      const body = request.postDataJSON(), raw = request.postDataBuffer();
      if (path === `${prefix}/${data.root_id}/delete-preview`) {
        if (previews.length !== 0 || originals.length !== 0 || !body || Object.keys(body).length !== 0 ||
            request.headers()["idempotency-key"] !== undefined) throw Error("KNOWLEDGE_DELETE_PREVIEW_CHANGED");
        previews.push(request);
        return "preview";
      }
      const token = path === prefix + "/commands/lookup" ? body?.request?.confirmation_token : body?.confirmation_token;
      if (!ready || !raw || typeof token !== "string" || sha(token) !== previewTokenHash ||
          !request.headers()["idempotency-key"]) throw Error("KNOWLEDGE_DELETE_ORIGINAL_MATERIAL_CHANGED");
      if (path === `${prefix}/${data.root_id}/delete-subtree`) {
        if (originals.length !== 0 || Object.keys(body).join() !== "confirmation_token") throw Error("KNOWLEDGE_DELETE_REPEATED_OR_CHANGED");
        originals.push(request);
        return "delete";
      }
      if (path === prefix + "/commands/lookup") {
        const original = originals[0];
        if (data.mode !== "delete-lost" || !original || lookups.length !== 0 ||
            Object.keys(body).sort().join() !== "command,document_id,request" || body.command !== "delete-subtree" || body.document_id !== data.root_id ||
            Object.keys(body.request).join() !== "confirmation_token" ||
            request.headers()["idempotency-key"] !== original.headers()["idempotency-key"] ||
            sha(JSON.stringify(body.request)) !== sha(original.postDataBuffer()!)) throw Error("KNOWLEDGE_DELETE_LOOKUP_CHANGED");
        lookups.push(request);
        return "lookup";
      }
      throw Error("KNOWLEDGE_DELETE_UNDECLARED_WRITE");
    }
    if (request.method() !== "GET" || request.postDataBuffer() !== null || request.headers()["idempotency-key"] !== undefined)
      throw Error("KNOWLEDGE_DELETE_UNDECLARED_METHOD");
    if (path === "/api/v1/session" && !url.search) return "session";
    if (path === "/api/v1/projects/resolve") {
      expect([...url.searchParams.entries()]).toEqual([["username", "knowledge-owner"], ["project_name", "knowledge-delete"]]);
      return "resolve";
    }
    if (path === "/api/v1/projects") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      return "projects";
    }
    if (path === project && !url.search) return "project";
    if (path === prefix + "/children") {
      expect([...url.searchParams.keys()]).toEqual(["parent_document_id", "limit"]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(["null", data.root_id, data.child_id, data.sibling_id]).toContain(url.searchParams.get("parent_document_id"));
      return "documents";
    }
    for (const target of [data.root_id, data.child_id, data.sibling_id]) {
      const pathToDocument = prefix + "/" + target;
      if (path === pathToDocument && !url.search) return "document";
      if (path === pathToDocument + "/ancestors" && !url.search) return "ancestors";
      if (path === pathToDocument + "/content") {
        expect([...url.searchParams.keys()]).toEqual(["byte_offset", "max_bytes"]);
        expect(url.searchParams.get("byte_offset")).toBe("0");
        expect(["65536", "1048576"]).toContain(url.searchParams.get("max_bytes"));
        return "content";
      }
    }
    throw Error("KNOWLEDGE_DELETE_UNDECLARED_READ");
  };
  const evidence = process.env.AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE!;
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_KNOWLEDGE_DELETE_WEB_DIST!, evidence, inputHash: data.input_hash,
    prefix: "knowledge-delete", endpoint: classify, lookupCommand: "delete-subtree", knowledgeDeleteWrites: true,
    cut: data.mode === "delete-lost" ? "delete" : undefined,
  });
  const forTarget = (name: string, target: string) => seen.requests(name).filter(request => new URL(request.url()).pathname.includes("/" + target));
  return {
    ...seen,
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("KNOWLEDGE_DELETE_STAGE_INVALID"); stage = value; },
    initial() {
      expect(seen.knowledgeDeleteRead("document", data.root_id)).toEqual({ active: data.root });
      expect(seen.knowledgeDeleteRead("content", data.root_id)).toEqual({ document: data.root, text: { text: data.root_text, next_byte_offset: String(Buffer.byteLength(data.root_text)), truncated: false } });
      rootContentBefore = forTarget("content", data.root_id).length;
      rootAncestorsBefore = forTarget("ancestors", data.root_id).length;
      expect(rootContentBefore).toBeGreaterThan(0);
    },
    preview() {
      expect(previews).toHaveLength(1);
      expect(originals).toHaveLength(0);
      const value = seen.receipt("preview", 0);
      expect(Object.keys(value).sort()).toEqual(["root_id", "nodes", "scope_digest", "expires_at"].sort());
      expect(value.root_id).toBe(data.root_id);
      expect([...value.nodes].sort((a, b) => a.id.localeCompare(b.id))).toEqual([data.root, data.child].sort((a, b) => a.id.localeCompare(b.id)));
      expect(value.scope_digest).toMatch(/^sha256:[0-9a-f]{64}$/);
      const files = previews[0]!.response();
      // The actual RID comes from the one original response; no request is issued.
      return files.then(async response => {
        if (!response) throw Error("KNOWLEDGE_DELETE_PREVIEW_RESPONSE_MISSING");
        const rid = (await response.allHeaders())["x-request-id"];
        if (!rid || !id.test(rid)) throw Error("KNOWLEDGE_DELETE_PREVIEW_RID_MISSING");
        const proof = JSON.parse(readFileSync(join(evidence, `knowledge-delete-${data.mode}-response-${rid}.json`), "utf8"));
        expect(proof.kind).toBe("preview");
        expect(proof.confirmation_token_sha256).toMatch(/^[0-9a-f]{64}$/);
        previewTokenHash = proof.confirmation_token_sha256;
        previewBodyHash = proof.body_sha256;
        ready = true;
      });
    },
    uncertain() {
      expect(originals).toHaveLength(1);
      expect(lookups).toHaveLength(0);
      expect(previews).toHaveLength(1);
      expect(forTarget("document", data.root_id)).toHaveLength(1);
    },
    confirmed() {
      expect(originals).toHaveLength(1);
      expect(previews).toHaveLength(1);
      const ids = [data.root_id, data.child_id].sort();
      const receipt = { root_id: data.root_id, deleted_ids: ids, cleanup_pending: true };
      if (data.mode === "delete-lost") {
        seen.originalLookup();
        expect(seen.receipt("lookup", 0)).toEqual({ state: "committed", receipt: { ...receipt, command: "delete-subtree", changed: true } });
      } else {
        expect(lookups).toHaveLength(0);
        expect(seen.receipt("delete", 0)).toEqual(receipt);
      }
      const tombstone = seen.knowledgeDeleteRead("document", data.root_id);
      expect(Object.keys(tombstone)).toEqual(["deleted"]);
      expect(Object.keys(tombstone.deleted).sort()).toEqual(["id", "project_id", "content_version", "deleted_at"].sort());
      expect(tombstone.deleted).toMatchObject({ id: data.root_id, project_id: data.project_id, content_version: "1" });
      expect(seen.knowledgeDeleteRead("documents")).toEqual({ items: [data.sibling] });
      expect(forTarget("content", data.root_id)).toHaveLength(rootContentBefore);
      expect(forTarget("ancestors", data.root_id)).toHaveLength(rootAncestorsBefore);
      current = true;
    },
    async finish() {
      await seen.verify();
      expect(current).toBe(true);
      expect(seen.knowledgeDeleteRead("document", data.sibling_id)).toEqual({ active: data.sibling });
      expect(seen.knowledgeDeleteRead("content", data.sibling_id)).toEqual({ document: data.sibling, text: { text: data.sibling_text, next_byte_offset: String(Buffer.byteLength(data.sibling_text)), truncated: false } });
      expect(originals).toHaveLength(1);
      expect(previews).toHaveLength(1);
      expect(lookups).toHaveLength(data.mode === "delete-lost" ? 1 : 0);
      await seen.endDocument();
      completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, `knowledge-delete-${data.mode}-stage.json`), JSON.stringify({ stage, status, current_verified: current, preview_body_sha256: previewBodyHash, confirmation_token_sha256: previewTokenHash, error_locations: errors.map(error => error.stack?.match(/(?:knowledge-delete\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `knowledge-delete-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "knowledge-delete-result.json"), JSON.stringify({ completed, input_hash: data.input_hash, case: data.mode, original_requests: proof.original_requests, all_observed: proof.all_observed, lookups: proof.lookups, declared_receipt_cut: proof.declared_receipt_cut }), { mode: 0o600 });
      }
    },
  };
}

export async function deleteScreenshot(page: Page, data: DeleteMaterial, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `knowledge-delete-${data.mode}-${name}.png`), fullPage: true, animations: "disabled" });
}
