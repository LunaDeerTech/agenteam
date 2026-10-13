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
  started_by: unknown;
  completed_by: unknown;
  [key: string]: unknown;
};
type Material = {
  mode: string;
  origin: string;
  cookie: string;
  project_id: string;
  tasks: [Entity, Entity];
  blocker: Record<string, unknown>;
  sprint: Entity;
  comment: string;
  project: Record<string, unknown>;
  route: string;
  sprint_route: string;
};
export function taskCancelMaterial(): Material {
  const data: Material = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-cancel-material.json"),
      "utf8",
    ),
  );
  expect(data.mode).toBe(
    "explicit-cancel-preserves-relations-and-completes-sprint",
  );
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  for (const id of [
    data.project_id,
    data.tasks[0].id,
    data.tasks[1].id,
    data.sprint.id,
  ])
    expect(id).toMatch(uuid);
  expect(data.tasks[0]).toMatchObject({
    project_id: data.project_id,
    state: "todo",
  });
  expect(data.tasks[0].assignee_agent_id).toMatch(uuid);
  expect(data.tasks[1]).toMatchObject({
    project_id: data.project_id,
    state: "backlog",
    assignee_agent_id: null,
  });
  expect(data.blocker).toMatchObject({
    task_id: data.tasks[1].id,
    type: "waiting_for_human",
    resolved_at: null,
  });
  expect(data.sprint).toMatchObject({
    project_id: data.project_id,
    state: "current",
  });
  expect(data.project.current_sprint_id).toBe(data.sprint.id);
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
  kind: string;
  bound: number;
  fulfilled: boolean;
  rejected: boolean;
  settled: boolean;
  current: boolean;
  not_busy: boolean;
  typed: unknown;
  typed_digest: string | null;
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
    throw Error("TASK_CANCEL_OBJECT_REQUIRED");
  return value as Record<string, unknown>;
}

export async function observeTaskCancel(page: Page, data: Material) {
  const evidence = process.env.AGENTEAM_TASK_CANCEL_WEB_EVIDENCE!,
    base = `/api/v1/projects/${data.project_id}`;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_TASK_CANCEL_WEB_DIST!,
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
    if (url.origin !== data.origin) throw Error("TASK_CANCEL_EXTERNAL_REQUEST");
    if (!url.pathname.startsWith("/api/")) return undefined;
    if (request.method() === "POST") {
      expect(url.search).toBe("");
      const paths: Record<string, string> = {
        [`${base}/tasks/${data.tasks[0].id}/transfer`]: "cancel-0",
        [`${base}/tasks/${data.tasks[1].id}/transfer`]: "cancel-1",
        [`${base}/sprints/${data.sprint.id}/complete`]: "complete",
      };
      const name = paths[url.pathname];
      if (!name) throw Error("TASK_CANCEL_FOREIGN_WRITE");
      return name;
    }
    if (request.method() !== "GET" || request.postData() !== null)
      throw Error("TASK_CANCEL_UNOBSERVED_METHOD");
    const paths: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
      [base]: "project",
      [`${base}/milestones`]: "milestones",
      [`${base}/sprints`]: "sprints",
      [`${base}/tasks`]: "tasks",
      [`${base}/agents`]: "agents",
      [`${base}/sprints/${data.sprint.id}`]: "sprint",
      [`${base}/tasks/${data.tasks[0].id}`]: "task-0",
      [`${base}/tasks/${data.tasks[1].id}`]: "task-1",
      [`${base}/tasks/${data.tasks[0].id}/events`]: "timeline-0",
      [`${base}/tasks/${data.tasks[1].id}/events`]: "timeline-1",
      [`${base}/tasks/${data.tasks[0].id}/blockers`]: "blockers-0",
      [`${base}/tasks/${data.tasks[1].id}/blockers`]: "blockers-1",
    };
    if (paths[url.pathname])
      return url.pathname === `${base}/tasks` &&
        url.searchParams.get("state") === "cancelled"
        ? "cancelled"
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
    throw Error("TASK_CANCEL_UNOBSERVED_READ");
  }
  const request = (request: Request) => {
    try {
      const name = classify(request);
      if (!name) return;
      if (entries.length >= 128) throw Error("TASK_CANCEL_REQUEST_BOUND");
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
        async ({ binding, project, tasks, sprint }) => {
          const module = await import(/* @vite-ignore */ binding.asset),
            use = module[binding.exportName];
          if (typeof use !== "function" || use() !== use())
            throw Error("TASK_CANCEL_SESSION_SINGLETON");
          const proof = (window as Host).__taskReviewProof;
          if (!proof || typeof proof.consumers !== "function")
            throw Error("TASK_CANCEL_NATIVE_MISSING");
          proof.consumers(
            use(),
            project,
            false,
            false,
            undefined,
            undefined,
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
            undefined,
            undefined,
            undefined,
            { first: sprint, next: sprint },
            tasks,
          );
        },
        {
          binding,
          project: data.project_id,
          tasks: data.tasks.map((task) => task.id),
          sprint: data.sprint.id,
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
      if (!proof) throw Error("TASK_CANCEL_NATIVE_MISSING");
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
          join(evidence, `task-cancel-response-${rid}.json`),
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
        if (!body) throw Error("TASK_CANCEL_ORIGINAL_BODY_ABSENT");
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
      if (e.request.method() === "POST")
        expect(c.kind).toBe(
          e.name === "complete"
            ? "sprintCompletion.complete"
            : "taskCancel.transfer",
        );
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
    throw Error("TASK_CANCEL_TYPED_RESULT_MISSING");
  }
  async function retire() {
    if (retirementAttempted) return;
    retirementAttempted = true;
    try {
      const n = await page.evaluate(() => {
        const proof = (window as Host).__taskReviewProof;
        if (!proof) throw Error("TASK_CANCEL_NATIVE_MISSING");
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
      if (!/^[a-z-]{1,48}$/.test(value)) throw Error("TASK_CANCEL_STAGE");
      stage = value;
    },
    async finish() {
      await verify();
      expect(
        entries.filter((e) => e.request.method() === "POST").map((e) => e.name),
      ).toEqual(["cancel-0", "cancel-1", "complete"]);
      expect(latest("project").current_sprint_id).toBeNull();
      expect(latest("sprint").state).toBe("completed");
      wire.complete();
      await retire();
      completed = true;
      stage = "complete";
      writeFileSync(
        join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-cancel-result.json"),
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
          join(evidence, "task-cancel-browser-proof.json"),
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
                /^task-cancel\.(spec|helpers)\.ts:\d+:\d+$/.test(v),
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
export async function taskCancelNavigationDiagnostic(
  page: Page,
  data: Material,
  action: () => Promise<void>,
) {
  const observation = await page.evaluateHandle(
    ({ title, first, second, sprint }) => {
      const targets = () =>
        Array.from(
          document.querySelectorAll<HTMLButtonElement>("button.task-card"),
        ).filter((element) => element.getAttribute("aria-label") === title);
      const role = (element: EventTarget | null) => {
        if (element instanceof HTMLButtonElement && targets().includes(element))
          return "second-task";
        if (element === document.body) return "body";
        if (element instanceof HTMLButtonElement) return "other-button";
        return element ? "other" : "none";
      };
      const snapshot = () => {
        const elements = targets(),
          target = elements.length === 1 ? elements[0] : undefined;
        const dialogs = Array.from(
          document.querySelectorAll<HTMLElement>('[role="dialog"]'),
        ).filter(
          (element) =>
            element.getClientRects().length > 0 &&
            !element.closest('[inert], [aria-hidden="true"]'),
        );
        const dialogNamed = (name: string) =>
          dialogs.some(
            (element) =>
              element
                .getAttribute("aria-labelledby")
                ?.split(/\s+/)
                .map((id) => document.getElementById(id)?.textContent ?? "")
                .join(" ") === name,
          );
        const native = (window as Host).__taskReviewProof?.snapshot();
        return {
          route:
            location.pathname === first
              ? "first-task"
              : location.pathname === second
                ? "second-task"
                : location.pathname === sprint
                  ? "sprint"
                  : "other",
          target_count: elements.length,
          target_connected: target?.isConnected ?? false,
          target_disabled: target?.disabled ?? null,
          target_inert: target ? !!target.closest("[inert]") : null,
          active: role(document.activeElement),
          visible_dialog_count: dialogs.length,
          project_guard: dialogNamed("放弃项目修改？"),
          task_guard: dialogNamed("离开任务"),
          native_pending: native?.pending ?? null,
          native_not_busy: native?.not_busy ?? null,
        };
      };
      const initial = snapshot();
      const events: Array<
        ReturnType<typeof snapshot> & {
          type: "keydown" | "click";
          event_target: string;
          default_prevented: boolean;
        }
      > = [];
      let eventCount = 0;
      const capture = (event: Event) => {
        const type = event.type === "keydown" ? "keydown" : "click";
        if (
          type === "keydown" &&
          (!(event instanceof KeyboardEvent) || event.key !== "Enter")
        )
          return;
        if (type === "click" && role(event.target) !== "second-task") return;
        eventCount++;
        if (events.length < 4)
          events.push({
            ...snapshot(),
            type,
            event_target: role(event.target),
            default_prevented: event.defaultPrevented,
          });
      };
      document.addEventListener("keydown", capture, true);
      document.addEventListener("click", capture, true);
      return {
        finish() {
          document.removeEventListener("keydown", capture, true);
          document.removeEventListener("click", capture, true);
          return {
            initial,
            events,
            event_count: eventCount,
            final: snapshot(),
            listeners_removed: true,
          };
        },
      };
    },
    {
      title: data.tasks[1].title,
      first: new URL(data.route, data.origin).pathname,
      second: new URL(data.route, data.origin).pathname.replace(
        data.tasks[0].id,
        data.tasks[1].id,
      ),
      sprint: new URL(data.sprint_route, data.origin).pathname,
    },
  );
  let actionFailed = false;
  try {
    await action();
  } catch (error) {
    actionFailed = true;
    throw error;
  } finally {
    let diagnostic: Record<string, unknown> = {},
      collected = false,
      disposed = false,
      screenshot = false;
    try {
      diagnostic = await observation.evaluate((value) => value.finish());
      collected = true;
    } catch {
      /* Preserve the original action failure, without recording raw errors. */
    } finally {
      try {
        await observation.dispose();
        disposed = true;
      } catch {
        /* Safe failure flag below. */
      }
    }
    if (actionFailed) {
      try {
        await page.screenshot({
          path: join(
            process.env.AGENTEAM_TASK_CANCEL_WEB_EVIDENCE!,
            "task-cancel-second-task-failure.png",
          ),
          animations: "disabled",
        });
        screenshot = true;
      } catch {
        /* The original locator error remains authoritative. */
      }
    }
    try {
      writeFileSync(
        join(
          process.env.AGENTEAM_TASK_CANCEL_WEB_EVIDENCE!,
          "task-cancel-navigation-diagnostic.json",
        ),
        JSON.stringify(
          {
            ...diagnostic,
            action_failed: actionFailed,
            collected,
            handle_disposed: disposed,
            failure_screenshot: screenshot,
          },
          null,
          2,
        ),
        { mode: 0o600 },
      );
      if (!actionFailed && (!collected || !disposed))
        throw Error("TASK_CANCEL_NAVIGATION_DIAGNOSTIC_CLEANUP");
    } catch (error) {
      if (!actionFailed) throw error;
    }
  }
}

export async function taskCancelScreenshot(page: Page, name: string) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_TASK_CANCEL_WEB_EVIDENCE!,
      `task-cancel-${name}.png`,
    ),
    animations: "disabled",
  });
}
