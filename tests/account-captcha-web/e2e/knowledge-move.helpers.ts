import { expect, type Page, type Request } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
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

// Fixed diagnostic fields only: no input values, page text, cookies or bodies.
// Both observations are bounded; an unavailable page leaves explicit null/false.
export async function moveFailure(page: Page, data?: MoveMaterial) {
  const mode = data?.mode ?? "unknown";
  const evidence = process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE!;
  const path = join(evidence, `knowledge-move-${mode}-failure.json`);
  const observedAt = Date.now();
  const persist = (value: unknown) => {
    try {
      writeFileSync(path, JSON.stringify(value, null, 2), { mode: 0o600 });
    } catch {
      // Keep the original failure and always proceed to observer retirement.
    }
  };
  persist({
    observed_at_unix_ms: observedAt,
    before_retire: true,
    collecting: true,
  });
  let dom: unknown = null,
    screenshot = false,
    domTimedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    dom = await Promise.race([
      page.evaluate(
        ({ route, project, target }) => {
          const names = [
            "移动",
            "移动文档",
            "移到根层",
            "浏览根目录",
            "确认移动",
            "查询原移动结果",
            "按原请求重放",
            "重新读取当前文档",
            "采用当前位置",
            "关闭",
            "放弃并继续",
            "继续处理",
            "停止本次读取",
            "重读文档树",
          ];
          const visible = (e: Element) =>
            e.getClientRects().length > 0 &&
            !e.closest('[inert],[aria-hidden="true"]');
          const label = (e: Element) => {
            const copy = e.cloneNode(true) as Element;
            copy
              .querySelectorAll('[aria-hidden="true"]')
              .forEach((n) => n.remove());
            const value =
              e.getAttribute("aria-label") ?? copy.textContent?.trim();
            return names.includes(value ?? "") ? value : "other";
          };
          const active = document.activeElement;
          const native = (window as any).__taskReviewProof?.snapshot();
          const boolean = (value: unknown) =>
            typeof value === "boolean" ? value : null;
          const count = (value: unknown) =>
            typeof value === "number" &&
            Number.isSafeInteger(value) &&
            value >= 0
              ? value
              : null;
          const endpoint = (path: unknown) => {
            const prefix = `/api/v1/projects/${project}/knowledge/documents`;
            if (path === `${prefix}/children`) return "children";
            if (path === `${prefix}/${target}`) return "document";
            if (path === `${prefix}/${target}/ancestors`) return "ancestors";
            if (path === `${prefix}/${target}/content`) return "content";
            if (path === `${prefix}/${target}/move`) return "move";
            if (path === `${prefix}/commands/lookup`) return "lookup";
            return "other";
          };
          const kinds = [
            "restore",
            "project.get",
            "project.resolve",
            "project.list",
            "knowledge.children",
            "knowledge.get",
            "knowledge.ancestors",
            "knowledge.readContent",
            "knowledgeMove.startMove",
            "knowledgeMove.checkOriginal",
          ];
          return {
            captured_at_unix_ms: Date.now(),
            route: location.pathname === route ? "original-document" : "other",
            theme: ["light", "dark"].includes(
              document.documentElement.dataset.theme ?? "",
            )
              ? document.documentElement.dataset.theme
              : "other",
            horizontal_overflow:
              document.documentElement.scrollWidth > innerWidth,
            visible_dialogs: Array.from(
              document.querySelectorAll('[role="dialog"]'),
            ).filter(visible).length,
            active: {
              tag:
                active &&
                [
                  "BUTTON",
                  "INPUT",
                  "TEXTAREA",
                  "H1",
                  "H2",
                  "A",
                  "BODY",
                ].includes(active.tagName)
                  ? active.tagName
                  : "other",
              label: active ? label(active) : "none",
              connected: active?.isConnected ?? false,
              disabled:
                active instanceof HTMLButtonElement ? active.disabled : null,
            },
            controls: Array.from(
              document.querySelectorAll<HTMLButtonElement>("button"),
            )
              .filter((e) => visible(e) && label(e) !== "other")
              .slice(0, 32)
              .map((e) => ({
                label: label(e),
                disabled: e.disabled,
                focused: active === e,
              })),
            pending: count(native?.pending),
            not_busy: boolean(native?.not_busy),
            native_failed: boolean(native?.failed),
            last_calls: Array.isArray(native?.calls)
              ? native.calls.slice(-6).map((call: any) => ({
                  id: count(call.id),
                  kind: kinds.includes(call.kind) ? call.kind : "other",
                  endpoint: endpoint(call.path),
                  bound: count(call.bound),
                  settled: boolean(call.settled),
                  fulfilled: boolean(call.fulfilled),
                  rejected: boolean(call.rejected),
                }))
              : [],
            last_rows: Array.isArray(native?.rows)
              ? native.rows.slice(-6).map((row: any) => ({
                  id: count(row.id),
                  call_id: count(row.call_id),
                  endpoint: endpoint(row.path),
                  status: count(row.status),
                  fetch_returned: boolean(row.fetch_returned),
                  fetch_rejected: boolean(row.fetch_rejected),
                  eof: boolean(row.eof),
                  closed: boolean(row.closed),
                  error: boolean(row.error),
                  readers: count(row.readers),
                  reads: count(row.reads),
                  read_returns: count(row.read_returns),
                  release: count(row.release),
                }))
              : [],
          };
        },
        {
          route: data?.route ?? "",
          project: data?.project_id ?? "",
          target: data?.document_id ?? "",
        },
      ),
      new Promise<null>((resolve) => {
        timer = setTimeout(() => {
          domTimedOut = true;
          resolve(null);
        }, 750);
      }),
    ]);
  } catch {
    // No raw exception or arbitrary page data leaves this collector.
  } finally {
    clearTimeout(timer);
  }
  try {
    await page.screenshot({
      path: join(evidence, `knowledge-move-${mode}-failure.png`),
      timeout: 1_000,
      animations: "disabled",
      mask: [page.locator('input, textarea, [contenteditable="true"]')],
    });
    screenshot = true;
  } catch {
    // A timed-out/closed page does not delay the original cleanup further.
  }
  persist({
    observed_at_unix_ms: observedAt,
    completed_at_unix_ms: Date.now(),
    before_retire: true,
    collecting: false,
    dom_timed_out: domTimedOut,
    dom,
    screenshot,
  });
}
