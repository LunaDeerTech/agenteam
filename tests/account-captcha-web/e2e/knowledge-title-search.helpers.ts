import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type TitleMaterial = {
  mode: "literal-title-and-current-read";
  cookie: string;
  project_id: string;
  parent_id: string;
  document_id: string;
  route: string;
  parent_title: string;
  title: string;
  text: string;
  query: string;
  miss_query: string;
  parent: Record<string, unknown>;
  document: Record<string, unknown>;
  input_hash: string;
};
export function titleMaterial(): TitleMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "knowledge-title-search-material.json"), "utf8"));
  expect(data.mode).toBe("literal-title-and-current-read");
  for (const value of [data.project_id, data.parent_id, data.document_id]) expect(value).toMatch(id);
  expect(data.route).toBe("/title-owner/title-search/knowledge");
  expect(data.parent_title).toBe("标题检索目录");
  expect(data.title).toBe("操作 %_! 指南 中文🙂");
  expect(data.text).toBe("原文 <%_!> 不执行 HTML。\n第二行🙂");
  expect(data.query).toBe("%_!");
  expect(data.miss_query).toBe("没有匹配的标题 019a");
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.input_hash).toBe(process.env.AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_INPUT_HASH);
  expect(data.parent).toMatchObject({ id: data.parent_id, project_id: data.project_id, parent_document_id: null, title: data.parent_title });
  expect(data.document).toMatchObject({ id: data.document_id, project_id: data.project_id, parent_document_id: data.parent_id, title: data.title });
  return data;
}

export async function observeTitleSearch(page: Page, data: TitleMaterial) {
  const project = `/api/v1/projects/${data.project_id}`, prefix = project + "/knowledge/documents";
  const searches: Request[] = [], children: Request[] = [];
  let hitVerified = false, currentVerified = false, completed = false, stage = "entry";
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(request.method()).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined();
    const global: Record<string, string> = { "/api/v1/session": "session", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects" };
    if (global[path]) return global[path];
    if (path === project && !url.search) return "project";
    if (path === prefix + "/children") {
      expect([...url.searchParams.keys()]).toEqual(["parent_document_id", "limit"]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(hitVerified ? ["null", data.parent_id, data.document_id] : ["null"]).toContain(url.searchParams.get("parent_document_id"));
      children.push(request);
      return "documents";
    }
    if (path === prefix + "/search-titles") {
      expect(searches.length).toBeLessThan(2);
      expect(children.length).toBeGreaterThan(0);
      if (searches.length === 1) expect(currentVerified).toBe(true);
      expect([...url.searchParams.keys()]).toEqual(["title_query", "limit"]);
      expect(url.searchParams.get("title_query")).toBe(searches.length === 0 ? data.query : data.miss_query);
      expect(url.searchParams.get("limit")).toBe("50");
      searches.push(request);
      return "titles";
    }
    const target = prefix + "/" + data.document_id;
    if (path === target || path === target + "/ancestors" || path === target + "/content") {
      expect(hitVerified).toBe(true);
      if (path.endsWith("/content")) {
        expect([...url.searchParams.keys()]).toEqual(["byte_offset", "max_bytes"]);
        expect(url.searchParams.get("byte_offset")).toBe("0");
        expect(["65536", "1048576"]).toContain(url.searchParams.get("max_bytes"));
        return "content";
      }
      expect(url.search).toBe("");
      return path === target ? "document" : "ancestors";
    }
    throw Error("UNOBSERVED_KNOWLEDGE_TITLE_ENDPOINT");
  };
  const evidence = process.env.AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_EVIDENCE!, inputHash = data.input_hash;
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_DIST!, evidence, inputHash,
    prefix: "knowledge-title-search", endpoint: classify, lookupCommand: null, knowledgeTitleReads: true,
  });
  return {
    ...seen,
    stage(value: string) { if (!/^[a-z0-9-]{1,64}$/.test(value)) throw Error("INVALID_TITLE_STAGE"); stage = value; },
    rootOnly() {
      expect(children.length).toBeGreaterThan(0);
      for (const request of children) expect(new URL(request.url()).searchParams.get("parent_document_id")).toBe("null");
      expect(seen.knowledgeTitleRead("documents")).toEqual({ items: [data.parent] });
      expect(searches).toHaveLength(0);
    },
    hit() {
      expect(searches).toHaveLength(1);
      expect(seen.knowledgeTitleRead("titles")).toEqual({ items: [{ document: data.document, ancestors: [data.parent] }] });
      hitVerified = true;
    },
    current() {
      expect(seen.knowledgeTitleRead("document")).toEqual({ active: data.document });
      expect(seen.knowledgeTitleRead("ancestors")).toEqual([data.parent]);
      expect(seen.knowledgeTitleRead("content")).toEqual({ document: data.document, text: { text: data.text, next_byte_offset: String(Buffer.byteLength(data.text)), truncated: false } });
      currentVerified = true;
    },
    async finish() {
      expect(searches).toHaveLength(2);
      expect(seen.requests("lookup")).toHaveLength(0);
      await seen.verify();
      expect(seen.knowledgeTitleRead("titles")).toEqual({ items: [] });
      expect(currentVerified).toBe(true);
      await seen.endDocument();
      completed = true;
    },
    save(status: unknown, errors: readonly { stack?: string }[] = []) {
      seen.save(status as string);
      writeFileSync(join(evidence, "knowledge-title-search-stage.json"), JSON.stringify({ stage, status, searches: searches.length, current_verified: currentVerified, error_locations: errors.map(error => error.stack?.match(/(?:knowledge-title-search\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0] ?? "unavailable") }), { mode: 0o600 });
      if (completed && status === "passed") {
        const proof = JSON.parse(readFileSync(join(evidence, `knowledge-title-search-${data.mode}.json`), "utf8"));
        writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "knowledge-title-search-result.json"), JSON.stringify({ completed, input_hash: inputHash, original_requests: proof.original_requests, all_observed: proof.all_observed, lookups: proof.lookups }), { mode: 0o600 });
      }
    },
  };
}
export async function titleScreenshot(page: Page, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect.poll(() => brand.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  const directory = process.env.AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `knowledge-title-search-${name}.png`), fullPage: true, animations: "disabled" });
}
