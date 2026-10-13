import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
import { observeBrowserIO } from "./task-review.helpers";
import { observeSystemRunnerWire } from "./system-runner-management.helpers";

const hash = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
type Entity = {
  id: string;
  project_id: string;
  title: string;
  version: string;
  state: string;
  assignee_agent_id: string | null;
  manual_rank: string;
  [key: string]: unknown;
};
export type Material = {
  mode: string;
  origin: string;
  cookie: string;
  project_id: string;
  task: Entity;
  description: string;
  route: string;
};
export function blockResumeMaterial(): Material {
  const data: Material = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "task-block-resume-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("assigned-todo-block-and-resume");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  for (const id of [data.project_id, data.task.id]) expect(id).toMatch(uuid);
  expect(data.task).toMatchObject({
    project_id: data.project_id,
    state: "todo",
  });
  expect(data.task.assignee_agent_id).toMatch(uuid);
  expect(data.cookie.length).toBeGreaterThan(0);
  return data;
}
type Row = {
  id: number;
  call_id: number;
  method: string;
  path: string;
  request_id: string;
  status: number;
  length: string;
  bytes: number;
  digest: string;
  typed_digest: string;
  fetch_returned: boolean;
  fetch_rejected: boolean;
  readers: number;
  reads: number;
  read_returns: number;
  eof: boolean;
  closed: boolean;
  release: number;
  reader_cancel: number;
  stream_cancel: number;
  error: boolean;
  request_bound?: boolean;
  request_body_sha256?: string;
  idempotency_key_sha256?: string;
};
type Call = {
  id: number;
  path: string;
  kind: string;
  bound: number;
  fulfilled: boolean;
  rejected: boolean;
  settled: boolean;
  current: boolean;
  not_busy: boolean;
  typed: unknown;
  typed_digest: string | null;
  result_current?: boolean;
  request_bound?: boolean;
  request_body_sha256?: string;
  idempotency_key_sha256?: string;
};
type Native = {
  failed: boolean;
  retired: boolean;
  pending: number;
  not_busy: boolean;
  current: boolean;
  rows: Row[];
  calls: Call[];
};
type Host = Window & {
  __taskReviewProof?: {
    consumers(...args: unknown[]): void;
    snapshot(): Native;
    finish(): Native;
  };
};
type Entry = {
  request: Request;
  name: string;
  uri: string;
  response?: Response;
  responses: number;
  finished: number;
  failed: number;
  failure: "none" | "net::ERR_ABORTED" | "other";
  checked: boolean;
  call?: Call;
};
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("TASK_BLOCK_RESUME_OBJECT_REQUIRED");
  return value as Record<string, unknown>;
}

export async function observeBlockResume(page: Page, data: Material) {
  const evidence = process.env.AGENTEAM_TASK_BLOCK_RESUME_WEB_EVIDENCE!,
    base = `/api/v1/projects/${data.project_id}`;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_TASK_BLOCK_RESUME_WEB_DIST!,
  );
  const entryURL = new URL(binding.entry, data.origin).href,
    wire = await observeSystemRunnerWire(page, data.origin);
  const entries: Entry[] = [],
    proofs: Record<string, unknown>[] = [];
  let failed = false,
    completed = false,
    retired = false,
    nativeRetired = false,
    routeRemoved = false,
    retirementAttempted = false,
    handlers = 0,
    stage = "entry";
  function classify(request: Request) {
    const url = new URL(request.url());
    if (url.origin !== data.origin)
      throw Error("TASK_BLOCK_RESUME_EXTERNAL_REQUEST");
    if (!url.pathname.startsWith("/api/")) return undefined;
    if (request.method() === "POST") {
      expect(url.search).toBe("");
      const body = request.postDataJSON();
      const writeIndex = entries.filter(
        (e) => e.request.method() === "POST",
      ).length;
      expect(writeIndex).toBeLessThan(3);
      expect(url.pathname).toBe(
        `${base}/tasks/${data.task.id}/${writeIndex === 0 ? "blockers" : "transfer"}`,
      );
      expect(body.expected_version).toBe(
        String(BigInt(data.task.version) + BigInt(writeIndex)),
      );
      if (writeIndex > 0)
        expect(body.request.target_state).toBe(
          writeIndex === 1 ? "blocked" : "todo",
        );
      return ["add", "block", "resume"][writeIndex];
    }
    if (request.method() !== "GET" || request.postData() !== null)
      throw Error("TASK_BLOCK_RESUME_UNOBSERVED_METHOD");
    const paths: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
      [base]: "project",
      [`${base}/milestones`]: "milestones",
      [`${base}/sprints`]: "sprints",
      [`${base}/tasks`]: "tasks",
      [`${base}/agents`]: "agents",
      [`${base}/sprints/${data.task.sprint_id}`]: "sprint",
      [`${base}/tasks/${data.task.id}`]: "task",
      [`${base}/tasks/${data.task.id}/events`]: "timeline",
      [`${base}/tasks/${data.task.id}/blockers`]: "blockers",
    };
    if (paths[url.pathname])
      return url.pathname === `${base}/tasks` &&
        !url.searchParams.has("sprint_id")
        ? "candidates"
        : paths[url.pathname];
    if (
      url.pathname.startsWith(base + "/agents/") &&
      uuid.test(url.pathname.slice((base + "/agents/").length))
    )
      return "agent";
    if (
      url.pathname.startsWith(base + "/milestones/") &&
      uuid.test(url.pathname.slice((base + "/milestones/").length))
    )
      return "milestone";
    throw Error("TASK_BLOCK_RESUME_UNOBSERVED_READ");
  }
  const request = (request: Request) => {
    try {
      const name = classify(request);
      if (!name) return;
      if (entries.length >= 128) throw Error("TASK_BLOCK_RESUME_REQUEST_BOUND");
      const u = new URL(request.url());
      entries.push({
        request,
        name,
        uri: u.pathname + u.search,
        responses: 0,
        finished: 0,
        failed: 0,
        failure: "none",
        checked: false,
      });
    } catch {
      failed = true;
    }
  };
  const response = (response: Response) => {
    const e = entries.find((e) => e.request === response.request());
    if (e) {
      e.response = response;
      if (++e.responses !== 1) failed = true;
    }
  };
  const finished = (request: Request) => {
    const e = entries.find((e) => e.request === request);
    if (e && (++e.finished !== 1 || e.failed)) failed = true;
  };
  const rejected = (request: Request) => {
    const e = entries.find((e) => e.request === request);
    if (!e) return;
    const reason = request.failure()?.errorText;
    e.failure = reason === "net::ERR_ABORTED" ? reason : "other";
    if (++e.failed !== 1 || e.finished || e.failure !== "net::ERR_ABORTED")
      failed = true;
  };
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(
        async ({ binding, project, task }) => {
          const module = await import(/* @vite-ignore */ binding.asset),
            use = module[binding.exportName];
          if (typeof use !== "function" || use() !== use())
            throw Error("TASK_BLOCK_RESUME_SESSION_SINGLETON");
          const proof = (window as Host).__taskReviewProof;
          if (!proof || typeof proof.consumers !== "function")
            throw Error("TASK_BLOCK_RESUME_NATIVE_MISSING");
          proof.consumers(
            use(),
            project,
            false,
            false,
            undefined,
            task,
            undefined,
            undefined,
            undefined,
            undefined,
            false,
            undefined,
            undefined,
            false,
            false,
            false,
            false,
            false,
            false,
            undefined,
            false,
            false,
            undefined,
            false,
            undefined,
            undefined,
            undefined,
            task,
            undefined,
            undefined,
            undefined,
            undefined,
            undefined,
            undefined, // knowledgeMoveTarget
            task,
          );
        },
        {
          binding,
          project: data.project_id,
          task: data.task.id,
        },
      );
      await route.continue();
    } catch {
      failed = true;
      await route.abort();
    } finally {
      handlers--;
    }
  };
  try {
    page.on("request", request);
    page.on("response", response);
    page.on("requestfinished", finished);
    page.on("requestfailed", rejected);
    await observeBrowserIO(page, () => {});
    await page.route(entryURL, install);
  } catch (setupError) {
    try {
      try {
        await page.unroute(entryURL, install);
      } finally {
        page.off("request", request);
        page.off("response", response);
        page.off("requestfinished", finished);
        page.off("requestfailed", rejected);
        await wire.close();
      }
    } finally {
      throw setupError;
    }
  }
  const snapshot = () =>
    page.evaluate(() => {
      const proof = (window as Host).__taskReviewProof;
      if (!proof) throw Error("TASK_BLOCK_RESUME_NATIVE_MISSING");
      return proof.snapshot();
    });
  async function verify() {
    await expect
      .poll(async () => {
        const n = await snapshot();
        return (
          !failed &&
          !n.failed &&
          !n.retired &&
          n.pending === 0 &&
          n.not_busy &&
          n.calls.every((c) => c.settled) &&
          n.rows.length === entries.length &&
          entries.every((e) => e.responses === 1 && e.finished + e.failed === 1)
        );
      })
      .toBe(true);
    const native = await snapshot();
    for (const e of entries.filter((e) => !e.checked)) {
      const response = e.response!,
        headers = await response.allHeaders(),
        rid = headers["x-request-id"]!;
      expect(rid).toMatch(uuid);
      const rows = native.rows.filter((n) => n.request_id === rid);
      expect(rows).toHaveLength(1);
      const n = rows[0]!;
      const calls = native.calls.filter((c) => c.id === n.call_id);
      expect(calls).toHaveLength(1);
      const c = calls[0]!;
      const original = JSON.parse(
        readFileSync(
          join(evidence, `task-block-resume-response-${rid}.json`),
          "utf8",
        ),
      );
      expect(original).toMatchObject({
        request_id: rid,
        method: e.request.method(),
        url_sha256: hash(e.uri),
        status: 200,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
      });
      expect(response.status()).toBe(200);
      expect(headers["cache-control"]).toBe("no-store");
      expect(Number(headers["content-length"])).toBe(original.content_length);
      expect(n).toMatchObject({
        method: e.request.method(),
        path: new URL(e.request.url()).pathname,
        status: 200,
        fetch_returned: true,
        fetch_rejected: false,
        readers: 1,
        release: 1,
      });
      expect(c).toMatchObject({
        bound: 1,
        settled: true,
        current: true,
        not_busy: true,
      });
      if (e.request.method() === "POST") {
        const body = e.request.postDataBuffer();
        if (!body) throw Error("TASK_BLOCK_RESUME_ORIGINAL_BODY_ABSENT");
        const bodyHash = hash(body),
          keyHash = hash(e.request.headers()["idempotency-key"]!);
        for (const source of [original, n, c]) {
          expect(source.request_body_sha256).toBe(bodyHash);
          expect(source.idempotency_key_sha256).toBe(keyHash);
        }
        expect(c.request_bound && n.request_bound).toBe(true);
        expect(original.committed_verified).toBe(true);
        expect(original.kind).toBe(e.name);
      }
      expect(n).toMatchObject({
        eof: true,
        closed: true,
        error: false,
        reader_cancel: 0,
        stream_cancel: 0,
      });
      expect(n.reads).toBeGreaterThan(0);
      expect(n.reads).toBe(n.read_returns);
      expect(n.bytes).toBe(Number(n.length));
      expect(n.bytes).toBe(original.content_length);
      expect(n.digest).toBe(original.body_sha256);
      expect(c).toMatchObject({ fulfilled: true, rejected: false });
      expect(n.typed_digest).toMatch(/^[a-f0-9]{64}$/);
      expect(c.typed_digest).toBe(n.typed_digest);
      if (e.request.method() === "POST") {
        expect(c.kind).toBe(
          e.name === "add" ? "humanBlocker.add" : "taskBlockResume.transfer",
        );
        expect(c.result_current).toBe(true);
      }
      if (e.finished) expect(await response.finished()).toBeNull();
      else expect(e.failure).toBe("net::ERR_ABORTED");
      const wireProof = await wire.headers({
        method: e.request.method(),
        url: e.request.url(),
        status: 200,
        requestID: rid,
      });
      proofs.push({
        name: e.name,
        request_id: rid,
        status: 200,
        url_sha256: hash(e.uri),
        body_sha256: original.body_sha256,
        typed_sha256: c.typed_digest,
        terminal: e.finished ? "finished" : "failed",
        failure_kind: e.failure,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
        wire: wireProof,
      });
      e.call = c;
      e.checked = true;
    }
  }
  function latest(name: string) {
    for (let i = entries.length - 1; i >= 0; i--)
      if (entries[i]!.name === name && entries[i]!.checked)
        return object(entries[i]!.call?.typed);
    throw Error("TASK_BLOCK_RESUME_TYPED_RESULT_MISSING");
  }
  async function retire() {
    if (retirementAttempted) return;
    retirementAttempted = true;
    try {
      const n = await page.evaluate(() => {
        const proof = (window as Host).__taskReviewProof;
        if (!proof) throw Error("TASK_BLOCK_RESUME_NATIVE_MISSING");
        return proof.finish();
      });
      expect(n.pending).toBe(0);
      expect(n.failed).toBe(false);
      expect(n.retired).toBe(true);
      nativeRetired = true;
    } finally {
      try {
        await page.unroute(entryURL, install);
        expect(handlers).toBe(0);
        routeRemoved = true;
      } finally {
        page.off("request", request);
        page.off("response", response);
        page.off("requestfinished", finished);
        page.off("requestfailed", rejected);
        await wire.close();
        retired =
          nativeRetired && routeRemoved && wire.snapshot().actual_detach;
      }
    }
  }
  return {
    verify,
    latest,
    count: (name: string) => entries.filter((e) => e.name === name).length,
    stage(value: string) {
      if (!/^[a-z-]{1,48}$/.test(value)) throw Error("TASK_BLOCK_RESUME_STAGE");
      stage = value;
    },
    async finish() {
      await verify();
      expect(
        entries.filter((e) => e.request.method() === "POST").map((e) => e.name),
      ).toEqual(["add", "block", "resume"]);
      expect(latest("task")).toMatchObject({
        id: data.task.id,
        state: "todo",
        assignee_agent_id: data.task.assignee_agent_id,
        version: String(BigInt(data.task.version) + 3n),
      });
      const blockers = latest("blockers").items as Record<string, unknown>[];
      expect(blockers).toHaveLength(1);
      expect(blockers.every((b) => typeof b.resolved_at === "string")).toBe(
        true,
      );
      wire.complete();
      await retire();
      completed = true;
      stage = "complete";
      writeFileSync(
        join(
          process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
          "task-block-resume-result.json",
        ),
        JSON.stringify({ completed: true, writes: 3 }),
        { mode: 0o600 },
      );
    },
    async save(status: string, locations: string[]) {
      let n: Native | undefined;
      try {
        n = await snapshot();
      } catch {
        failed = true;
      }
      try {
        if (!retired) await retire();
      } catch {
        failed = true;
      } finally {
        if (!retired) failed = true;
        writeFileSync(
          join(evidence, "task-block-resume-browser-proof.json"),
          JSON.stringify(
            {
              status,
              stage,
              completed,
              retired,
              native_retired: nativeRetired,
              route_removed: routeRemoved,
              failed,
              original_requests: entries.length,
              proof: proofs,
              wire: wire.snapshot(),
              native: n
                ? {
                    failed: n.failed,
                    retired: n.retired,
                    pending: n.pending,
                    current: n.current,
                    not_busy: n.not_busy,
                  }
                : null,
              error_locations: locations.filter((v) =>
                /^task-block-resume\.(spec|helpers)\.ts:\d+:\d+$/.test(v),
              ),
            },
            null,
            2,
          ),
          { mode: 0o600 },
        );
      }
    },
  };
}
export async function blockResumeScreenshot(page: Page, name: string) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_TASK_BLOCK_RESUME_WEB_EVIDENCE!,
      `task-block-resume-${name}.png`,
    ),
    animations: "disabled",
  });
}

// Save only a fixed diagnostic vocabulary. Never serialize cookies, headers,
// field values or arbitrary page text. Capture before retiring the observers.
export async function blockResumeFailure(page: Page, data?: Material) {
  const evidence = process.env.AGENTEAM_TASK_BLOCK_RESUME_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  const observedAt = Date.now();
  // Persist the assertion-failure boundary before screenshot or retirement.
  writeFileSync(
    join(evidence, "task-block-resume-failure.json"),
    JSON.stringify({ observed_at_unix_ms: observedAt, collecting: true }),
    { mode: 0o600 },
  );
  let dom: unknown = null,
    screenshot = false;
  try {
    dom = await page.evaluate(
      ({ route, project, task }) => {
        const names = [
          "添加人工阻塞",
          "确认添加人工阻塞",
          "取消人工阻塞操作",
          "阻塞说明",
          "标记受阻",
          "确认标记受阻",
          "解决阻塞并恢复待执行",
          "确认解决阻塞并恢复待执行",
          "保留当前状态",
          "恢复后的执行 Agent",
          "状态变更说明",
          "重新读取任务",
          "关闭任务详情",
          "取消",
        ];
        const visible = (element: Element) =>
          element.getClientRects().length > 0 &&
          !element.closest('[inert],[aria-hidden="true"]');
        const label = (element: Element) => {
          const text =
            element.getAttribute("aria-label") ?? element.textContent?.trim();
          return names.includes(text ?? "") ? text : "other";
        };
        const active = document.activeElement;
        const native = (window as Host).__taskReviewProof?.snapshot();
        const endpoint = (path: string) => {
          const base = `/api/v1/projects/${project}`;
          if (path === `${base}/tasks`) return "task-list";
          if (path === `${base}/tasks/${task}`) return "task-detail";
          if (path === `${base}/tasks/${task}/transfer`) return "task-transfer";
          if (path === `${base}/tasks/${task}/blockers`) return "task-blockers";
          if (path === `${base}/tasks/${task}/events`) return "task-timeline";
          return "other";
        };
        const kinds = [
          "work.tasks",
          "work.task",
          "work.blockers",
          "work.events",
          "work.sprint",
          "work.milestone",
          "work.milestones",
          "work.sprints",
          "work.agents",
          "humanBlocker.add",
          "taskBlockResume.transfer",
        ];
        return {
          captured_at_unix_ms: Date.now(),
          route:
            location.pathname === route
              ? "original-task"
              : location.pathname.endsWith("/tasks")
                ? "task-board"
                : "other",
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
            tag: active?.tagName ?? "NONE",
            label: active ? label(active) : "none",
            connected: active?.isConnected ?? false,
            disabled:
              active instanceof HTMLButtonElement ? active.disabled : null,
          },
          controls: Array.from(
            document.querySelectorAll<HTMLButtonElement>("button"),
          )
            .filter((e) => visible(e) && label(e) !== "other")
            .map((e) => ({
              label: label(e),
              disabled: e.disabled,
              focused: active === e,
            })),
          forms: Array.from(document.querySelectorAll("form[aria-label]"))
            .filter(visible)
            .map((e) => label(e)),
          pending: native?.pending ?? null,
          not_busy: native?.not_busy ?? null,
          native_failed: native?.failed ?? null,
          // pending counts observer tails, not HTTP requests. Preserve only
          // fixed endpoint/kind vocabulary and lifecycle booleans.
          last_calls:
            native?.calls.slice(-6).map((call) => ({
              id: call.id,
              kind: kinds.includes(call.kind) ? call.kind : "other",
              endpoint: endpoint(call.path),
              bound: call.bound,
              settled: call.settled,
              fulfilled: call.fulfilled,
              rejected: call.rejected,
            })) ?? [],
          last_rows:
            native?.rows.slice(-6).map((row) => ({
              id: row.id,
              call_id: row.call_id,
              endpoint: endpoint(row.path),
              status: row.status,
              fetch_returned: row.fetch_returned,
              fetch_rejected: row.fetch_rejected,
              eof: row.eof,
              closed: row.closed,
              error: row.error,
              reads: row.reads,
              read_returns: row.read_returns,
              release: row.release,
            })) ?? [],
        };
      },
      {
        route: data?.route ?? "",
        project: data?.project_id ?? "",
        task: data?.task.id ?? "",
      },
    );
  } catch {
    /* Preserve the original failure if the page has closed. */
  }
  try {
    await page.screenshot({
      path: join(evidence, "task-block-resume-failure.png"),
      animations: "disabled",
    });
    screenshot = true;
  } catch {
    /* Safe boolean below; never replace the original error. */
  }
  writeFileSync(
    join(evidence, "task-block-resume-failure.json"),
    JSON.stringify(
      {
        observed_at_unix_ms: observedAt,
        completed_at_unix_ms: Date.now(),
        collecting: false,
        dom,
        screenshot,
      },
      null,
      2,
    ),
    { mode: 0o600 },
  );
}
