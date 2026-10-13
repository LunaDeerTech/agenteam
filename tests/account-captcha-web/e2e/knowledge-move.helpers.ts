import { expect, type Page, type Request } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";
const id =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type MoveMaterial = {
  mode: "move-normal" | "move-lost";
  origin: string;
  cookie: string;
  route: string;
  project_id: string;
  parent_id: string;
  document_id: string;
  parent: Record<string, any>;
  document: Record<string, any>;
  text: string;
  input_hash: string;
};
export function moveMaterial(): MoveMaterial {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "knowledge-move-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe(process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_CASE);
  expect(["move-normal", "move-lost"]).toContain(data.mode);
  for (const field of ["project_id", "document_id", "parent_id"])
    expect(data[field]).toMatch(id);
  expect(data.parent_id).not.toBe(data.document_id);
  expect(data.route).toBe(
    `/move-owner/knowledge-move/knowledge/${data.document_id}`,
  );
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_INPUT_HASH,
  );
  expect(data.cookie).toEqual(expect.any(String));
  expect(data.cookie.length).toBeGreaterThan(0);
  expect(data.parent).toMatchObject({
    id: data.parent_id,
    project_id: data.project_id,
    parent_document_id: null,
    title: "目标父文档",
    content_version: "1",
    media_type: "text/plain",
    source_kind: "text",
    status: "active",
  });
  expect(data.document).toMatchObject({
    id: data.document_id,
    project_id: data.project_id,
    parent_document_id: null,
    title: "待移动文档 中文🙂",
    content_version: "1",
    media_type: "text/plain",
    source_kind: "text",
    status: "active",
  });
  expect(data.text).toBe("移动保留正文 <%_!>。\n第二行🙂");
  return data;
}
export async function observeMove(page: Page, data: MoveMaterial) {
  const project = `/api/v1/projects/${data.project_id}`,
    prefix = project + "/knowledge/documents";
  let writes = 0,
    lookups = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    if (!path.startsWith("/api/")) return;
    expect(url.origin).toBe(data.origin);
    if (method === "POST") {
      expect(url.search).toBe("");
      if (path === `${prefix}/${data.document_id}/move`) {
        expect(writes).toBeLessThan(data.mode === "move-normal" ? 2 : 1);
        expect(request.postDataJSON()).toEqual(
          writes === 0
            ? { expected_parent_id: null, target_parent_id: data.parent_id }
            : { expected_parent_id: data.parent_id, target_parent_id: null },
        );
        writes++;
        return "move";
      }
      expect(path).toBe(prefix + "/commands/lookup");
      expect(data.mode).toBe("move-lost");
      expect(writes).toBe(1);
      expect(lookups++).toBe(0);
      expect(request.postDataJSON()).toEqual({
        command: "move",
        document_id: data.document_id,
        request: { expected_parent_id: null, target_parent_id: data.parent_id },
      });
      return "lookup";
    }
    expect(method).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    if (path === "/api/v1/projects/resolve") {
      expect([...url.searchParams.entries()]).toEqual([
        ["username", "move-owner"],
        ["project_name", "knowledge-move"],
      ]);
      return "resolve";
    }
    if (path === "/api/v1/projects") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      return "projects";
    }
    if (path === prefix + "/children") {
      expect([...url.searchParams.keys()]).toEqual([
        "parent_document_id",
        "limit",
      ]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(["null", data.parent_id, data.document_id]).toContain(
        url.searchParams.get("parent_document_id"),
      );
      return "documents";
    }
    for (const target of [data.document_id, data.parent_id]) {
      if (path === `${prefix}/${target}/content`) {
        expect([...url.searchParams.entries()]).toEqual([
          ["byte_offset", "0"],
          ["max_bytes", "65536"],
        ]);
        return "content";
      }
      if (path === `${prefix}/${target}`) {
        expect(url.search).toBe("");
        return "document";
      }
      if (path === `${prefix}/${target}/ancestors`) {
        expect(url.search).toBe("");
        return "ancestors";
      }
    }
    expect(url.search).toBe("");
    if (path === "/api/v1/session") return "session";
    if (path === project) return "project";
    throw Error("UNOBSERVED_KNOWLEDGE_MOVE_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_DIST!,
    evidence: process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE!,
    inputHash: data.input_hash,
    prefix: "knowledge-move",
    endpoint: classify,
    lookupCommand: "move",
    knowledgeMoveWrites: true,
    cut: data.mode === "move-lost" ? "move" : undefined,
  });
  return {
    ...seen,
    sourceReads() {
      return seen
        .requests("document")
        .filter(
          (request) =>
            new URL(request.url()).pathname === `${prefix}/${data.document_id}`,
        ).length;
    },
    current(stage: 0 | 1 | 2) {
      const parent = stage === 1 ? data.parent_id : null;
      const head = seen.knowledgeMoveRead("document", data.document_id) as any;
      const { updated_at: _updated, ...stable } = data.document;
      expect(head.active).toMatchObject({
        ...stable,
        parent_document_id: parent,
      });
      expect(head.active.updated_at >= data.document.updated_at).toBe(true);
      expect(seen.knowledgeMoveRead("ancestors", data.document_id)).toEqual(
        parent ? [data.parent] : [],
      );
      expect(seen.knowledgeMoveRead("content", data.document_id)).toEqual({
        document: head.active,
        text: {
          text: data.text,
          next_byte_offset: String(Buffer.byteLength(data.text)),
          truncated: false,
        },
      });
      if (stage > 0) {
        const receipt =
          data.mode === "move-lost"
            ? seen.receipt("lookup", 0).receipt
            : seen.receipt("move", stage - 1);
        expect(receipt).toEqual(
          data.mode === "move-lost"
            ? { command: "move", document: head.active, changed: true }
            : { document: head.active, changed: true },
        );
      }
      if (data.mode === "move-normal") {
        expect(seen.requests("move")).toHaveLength(stage);
        expect(seen.requests("lookup")).toHaveLength(0);
        expect(
          new Set(
            seen
              .requests("move")
              .map((request) => request.headers()["idempotency-key"]),
          ).size,
        ).toBe(stage);
      } else if (stage === 1) {
        seen.originalLookup();
        expect(seen.requests("move")).toHaveLength(1);
        expect(seen.requests("lookup")).toHaveLength(1);
      }
    },
  };
}
export async function moveScreenshot(
  page: Page,
  data: MoveMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect
    .poll(() =>
      brand.evaluate(
        (node: HTMLImageElement) => node.complete && node.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE!,
      `knowledge-move-${data.mode}-${name}.png`,
    ),
    fullPage: true,
    animations: "disabled",
  });
}
