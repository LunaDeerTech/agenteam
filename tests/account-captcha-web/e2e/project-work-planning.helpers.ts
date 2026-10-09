import {
  expect,
  type Page,
  type Locator,
  type Request,
  type Response,
} from "@playwright/test";
import {
  readFileSync,
  writeFileSync,
  renameSync,
  existsSync,
  readdirSync,
} from "node:fs";
import { join, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { createProjectOwnerAPI } from "../../../web/src/api/project-owner";
import { createWorkPlanningAPI } from "../../../web/src/api/work-planning";
import { AccountFailure, uuid7 } from "../../../web/src/api/client";

export const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
export const evidence = process.env.AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE!;
export const repository = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../..",
);
export type Credential = {
  email: string;
  password: string;
  user_id: string;
  username: string;
};
export type Seed = {
  project_id: string;
  milestone_id: string;
  sprint_id: string;
  task_id: string;
  related_id: string;
  blocker_id: string;
};
export type Material = {
  owner: Credential;
  admin: Credential;
  other: Credential;
  ids: Record<string, string>;
  projects: Record<string, { id: string; name: string }>;
  work: Record<string, Seed>;
};
export const material = (): Material =>
  JSON.parse(
    readFileSync(
      join(directory, "project-work-planning-material.json"),
      "utf8",
    ),
  );
export const button = (where: Page | Locator, name: string) =>
  where.getByRole("button", { name, exact: true });
export const details = (page: Page) =>
  page.getByRole("region", { name: "当前读取内容", exact: true });
export const recovery = (page: Page) =>
  page.getByRole("region", { name: "原命令恢复", exact: true });
export function path(
  data: Material,
  key = "main",
  kind?: "milestone" | "sprint" | "task",
  id?: string,
) {
  const base = `/${data.owner.username}/${data.projects[key]!.name}/tasks/explore`;
  return kind
    ? `${base}/${kind}s/${id ?? data.work[key]![`${kind}_id`]}`
    : base;
}
export async function login(page: Page, credential: Credential) {
  await expect(page.locator("#login-email")).toBeVisible();
  try {
    await page.locator("#login-email").fill(credential.email);
    await page.locator("#login-password").fill(credential.password);
  } catch {
    throw new Error("PRIVATE_WORK_LOGIN_INPUT_FAILED");
  }
  await button(page, "登录").click();
  await expect(button(page, "退出登录")).toBeEnabled();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
export async function ready(page: Page, id?: string) {
  await expect(
    page.getByRole("heading", { name: "任务规划", exact: true }),
  ).toBeVisible();
  await expect(button(page, "新建 Milestone")).toBeEnabled();
  if (id) await expect(details(page)).toContainText(id);
}
export async function enter(
  page: Page,
  data: Material,
  kind?: "milestone" | "sprint" | "task",
  key = "main",
) {
  await page.goto(path(data, key, kind));
  await login(page, data.owner);
  await ready(page, kind ? data.work[key]![`${kind}_id`] : undefined);
}
export async function choose(page: Page, label: string, option: string) {
  await button(page, label).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}
export async function confirmed(page: Page) {
  await expect(
    recovery(page).getByRole("heading", { name: "原命令已确认", exact: true }),
  ).toBeVisible();
  await expect(button(recovery(page), "查证原命令")).toBeEnabled();
}
export async function save(
  page: Page,
  creating: false | "structure" | "task" = false,
) {
  const form = page.getByRole("form", {
    name:
      creating === "task"
        ? "Task 规划编辑"
        : creating === "structure"
          ? "规划结构编辑"
          : /^(规划结构编辑|Task 规划编辑)$/,
    exact: true,
  });
  const action = form.getByRole("button", {
    name:
      creating === "task"
        ? "创建 Task"
        : creating === "structure"
          ? "创建"
          : "保存修改",
    exact: true,
  });
  await expect(action).toBeEnabled();
  await action.click();
  await confirmed(page);
}
let sequence = 0;
export async function ipc(action: string, extra: Record<string, unknown> = {}) {
  const current = ++sequence;
  const request = join(directory, "project-work-planning-ipc.json");
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence: current, action, ...extra }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  const ack = join(directory, `project-work-planning-ack-${current}.json`);
  await expect.poll(() => existsSync(ack), { timeout: 4000 }).toBe(true);
  const result = JSON.parse(readFileSync(ack, "utf8"));
  expect(result.sequence === current).toBe(true);
  return result as Record<string, any>;
}
export function complete(checks: Record<string, unknown>) {
  writeFileSync(
    join(directory, "project-work-planning-result.json"),
    JSON.stringify({ completed: true, ...checks }),
    { mode: 0o600 },
  );
}

type WorkAwaitStage = "header" | "finished" | "sidecar" | "complete";
const workBodyAwaits = new WeakMap<
  Response,
  { stage: WorkAwaitStage; at: number }
>();
const failureSnapshots: (() => unknown)[] = [];
function markWorkBodyAwait(response: Response, stage: WorkAwaitStage) {
  workBodyAwaits.set(response, { stage, at: performance.now() });
}
// Only fixed endpoint kinds and canonical public IDs leave the observer. Never
// persist a raw URL/query, request headers/body, or arbitrary failure text.
function workDiagnosticTarget(request: Request) {
  const method = ["GET", "POST", "PATCH"].includes(request.method())
    ? request.method()
    : "other";
  const p = new URL(request.url()).pathname.split("/").slice(1);
  const base = { method, endpoint: "unclassified-work" };
  if (
    p[0] !== "api" ||
    p[1] !== "v1" ||
    p[2] !== "projects" ||
    !uuid7.test(p[3] ?? "")
  )
    return base;
  if (
    (p[4] === "structure-commands" || p[4] === "task-commands") &&
    p.length === 6 &&
    p[5] === "lookup"
  )
    return { method, endpoint: p[4] + "/lookup", project_id: p[3] };
  if (!["milestones", "sprints", "tasks"].includes(p[4] ?? "")) return base;
  if (p.length === 5) return { method, endpoint: p[4], project_id: p[3] };
  if (!uuid7.test(p[5] ?? "")) return base;
  const target = { method, project_id: p[3], target_id: p[5] };
  if (p.length === 6) return { ...target, endpoint: p[4] + "/id" };
  if (p.length === 7 && p[6] === "reorder")
    return { ...target, endpoint: p[4] + "/id/reorder" };
  if (
    p[4] === "tasks" &&
    p[6] === "blockers" &&
    (p.length === 7 || (p.length === 8 && p[7] === "resolve"))
  )
    return {
      ...target,
      endpoint:
        p.length === 7 ? "tasks/id/blockers" : "tasks/id/blockers/resolve",
    };
  if (
    p[4] === "tasks" &&
    p[6] === "blocker-commands" &&
    p.length === 8 &&
    p[7] === "lookup"
  )
    return { ...target, endpoint: "tasks/id/blocker-commands/lookup" };
  return base;
}
function safeWorkFailure(request: Request) {
  const reason = request.failure()?.errorText;
  return reason === "net::ERR_ABORTED"
    ? "aborted"
    : reason === "net::ERR_CONNECTION_CLOSED"
      ? "connection-closed"
      : reason === "net::ERR_CONTENT_LENGTH_MISMATCH"
        ? "content-length-mismatch"
        : reason
          ? "other-network-failure"
          : "unspecified";
}
export function saveWorkFailureObservations(status: string | null) {
  try {
    if (status !== null)
      writeFileSync(
        join(evidence, "work-failure-observation.json"),
        JSON.stringify({
          status: ["failed", "timedOut", "interrupted"].includes(status)
            ? status
            : "other-failure",
          boundary:
            "Node observation snapshot at afterEach; not a final post-close state",
          browser_native_eof_observed: false,
          observers: failureSnapshots.map((snapshot) => snapshot()),
        }),
        { mode: 0o600 },
      );
  } catch {
    // Diagnostic I/O cannot replace the original case failure or its wait gate.
    console.error("WORK_DIAGNOSTIC_WRITE_FAILED");
  } finally {
    failureSnapshots.length = 0;
  }
}

// Read the exact body already captured from this root response, never issue a
// replacement GET or ask Playwright to read a second transport body.
export async function originalBody(
  response: Response,
): Promise<Record<string, any>> {
  markWorkBodyAwait(response, "header");
  const requestID = await response.headerValue("x-request-id");
  markWorkBodyAwait(response, "finished");
  expect(await response.finished()).toBeNull();
  markWorkBodyAwait(response, "sidecar");
  const matches = readdirSync(evidence)
    .filter((name) => /^response-\d+\.json$/.test(name))
    .map((name) => JSON.parse(readFileSync(join(evidence, name), "utf8")))
    .filter((meta) => meta.request_id === requestID);
  expect(matches.length).toBe(1);
  const meta = matches[0];
  expect(
    meta.endpoint === new URL(response.url()).pathname &&
      meta.method === response.request().method() &&
      meta.status === response.status(),
  ).toBe(true);
  expect(meta.body_file === `body-${meta.body_sha256}.json`).toBe(true);
  const raw = readFileSync(join(evidence, meta.body_file));
  expect(
    createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
  ).toBe(true);
  const body = JSON.parse(raw.toString("utf8"));
  markWorkBodyAwait(response, "complete");
  return body;
}

// Recovery Project refresh only. Decode the one original sidecar through both
// the formal schema and actual typed client; native/public completion is checked
// separately after all observers have actually retired.
export async function projectRefreshBody(
  response: Response,
  projectID: string,
  ownerID: string,
) {
  const url = new URL(response.url()),
    requestID = await response.headerValue("x-request-id");
  expect(
    !!requestID &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        requestID,
      ),
  ).toBe(true);
  expect(
    response.request().method() === "GET" &&
      !url.search &&
      url.pathname === `/api/v1/projects/${projectID}` &&
      response.status() === 200,
  ).toBe(true);
  const matches = readdirSync(evidence)
    .filter((name) => /^response-\d+\.json$/.test(name))
    .map((name) => JSON.parse(readFileSync(join(evidence, name), "utf8")))
    .filter((meta) => meta.request_id === requestID);
  expect(matches.length).toBe(1);
  const meta = matches[0];
  expect(
    meta.endpoint === url.pathname &&
      meta.method === "GET" &&
      meta.status === 200 &&
      meta.source_run === "TestAccountProjectWorkPlanningWebOriginalRecovery" &&
      /^[0-9a-f]{64}$/.test(meta.input_hash) &&
      /^[0-9a-f]{64}$/.test(meta.body_sha256) &&
      meta.body_file === `body-${meta.body_sha256}.json` &&
      meta.content_type.split(";")[0].trim().toLowerCase() ===
        "application/json",
  ).toBe(true);
  const raw = readFileSync(join(evidence, meta.body_file));
  expect(
    createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
  ).toBe(true);
  const schema = spawnSync(
    "python3",
    [
      "-c",
      String.raw`
import json,pathlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 base=pathlib.Path(sys.argv[1])/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ['common.json','project-owner.json']}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 doc=docs['project-owner.json']; response=doc['paths']['/api/v1/projects/{id}']['get']['responses']['200']
 if '$ref' in response: response=doc['components']['responses'][response['$ref'].rsplit('/',1)[1]]
 schema=dict(doc); schema['$id']=(base/'project-owner.json').as_uri(); schema.update(response['content']['application/json']['schema'])
 Draft202012Validator(schema,registry=registry).validate(json.loads(sys.stdin.buffer.read()))
except Exception: sys.exit(1)
`,
      repository,
    ],
    { input: raw, timeout: 5000, maxBuffer: 1024 },
  );
  expect(schema.status === 0 && !schema.error).toBe(true);
  let calls = 0;
  const api = createProjectOwnerAPI(async (input, init) => {
    calls++;
    expect(
      input === url.pathname &&
        init?.method === "GET" &&
        init.body === undefined,
    ).toBe(true);
    return new globalThis.Response(raw, {
      status: 200,
      headers: {
        "Content-Type": meta.content_type,
        "Content-Length": String(raw.length),
        "X-Request-ID": requestID!,
      },
    });
  });
  const project = await api.get(
    projectID,
    ownerID,
    new AbortController().signal,
  );
  expect(calls).toBe(1);
  expect(project.lifecycle).toBe("archived");
  return {
    project,
    proof: {
      requestID: requestID!,
      bytes: raw.length,
      ownerID,
      version: project.version,
    },
  };
}

// Requests are retained only in Node memory. Safe evidence contains response
// bytes and route/request-ID coordinates, never Cookie/CSRF/key/request bodies.
type Observed = {
  request: Request;
  url: URL;
  status: number;
  finished: boolean;
  failed: boolean;
};
// Only these declared faults may produce an expected incomplete Request;
// a case still admits at most four original Requests.
type WorkIncompleteDeclaration = {
  kind:
    | "unforwarded-milestone-update"
    | "lost-milestone-update"
    | "lost-task-update"
    | "lost-blocker-add"
    | "canceled-task-read";
  projectID: string;
  targetID: string;
  expectedVersion?: string;
  text?: string;
};
function workIncompleteLedger() {
  type Slot = {
    spec: WorkIncompleteDeclaration;
    request?: Request;
    key?: string;
    body?: string;
    allowed: boolean;
    failed: boolean;
    succeeded: boolean;
    ownedResponse: boolean;
    failure: Promise<void>;
    settleFailure: () => void;
  };
  const slots: Slot[] = [];
  let closed = false;
  let errors = 0;
  const keys = (value: object) => Object.keys(value).sort().join(",");
  function matches(slot: Slot, request: Request) {
    const spec = slot.spec,
      url = new URL(request.url());
    const entity = [
      "lost-milestone-update",
      "unforwarded-milestone-update",
    ].includes(spec.kind)
      ? "milestones"
      : "tasks";
    const suffix = spec.kind === "lost-blocker-add" ? "/blockers" : "";
    if (
      url.search ||
      url.pathname !==
        `/api/v1/projects/${spec.projectID}/${entity}/${spec.targetID}${suffix}`
    )
      return false;
    if (spec.kind === "canceled-task-read")
      return request.method() === "GET" && request.postData() === null;
    if (
      request.method() !== (spec.kind === "lost-blocker-add" ? "POST" : "PATCH")
    )
      return false;
    const key = request.headers()["idempotency-key"];
    // Intent keys use Foundation's scalar contract; UUIDv7 identifies resources.
    if (!key || !/^[A-Za-z0-9._:\/-]{1,128}$/.test(key)) return false;
    try {
      const body = request.postDataJSON();
      if (
        !body ||
        keys(body) !== "expected_version,request" ||
        body.expected_version !== spec.expectedVersion
      )
        return false;
      const payload = body.request;
      if (!payload || typeof payload !== "object" || Array.isArray(payload))
        return false;
      if (spec.kind === "lost-blocker-add") {
        if (
          keys(payload) !== "blocker_id,description,metadata,type" ||
          !uuid7.test(payload.blocker_id) ||
          payload.description !== spec.text ||
          payload.type !== "waiting_for_human" ||
          !payload.metadata ||
          Array.isArray(payload.metadata) ||
          typeof payload.metadata !== "object" ||
          keys(payload.metadata) !== ""
        )
          return false;
      } else if (keys(payload) !== "title" || payload.title !== spec.text)
        return false;
      return true;
    } catch {
      return false;
    }
  }
  function expectedFailure(request: Request) {
    return slots.some(
      (slot) => slot.request === request && slot.allowed && slot.failed,
    );
  }
  return {
    declare(spec: WorkIncompleteDeclaration) {
      const cancel = spec.kind === "canceled-task-read";
      if (
        closed ||
        slots.length >= 4 ||
        slots.some((slot) => !slot.failed) ||
        ![
          "unforwarded-milestone-update",
          "lost-milestone-update",
          "lost-task-update",
          "lost-blocker-add",
          "canceled-task-read",
        ].includes(spec.kind) ||
        !uuid7.test(spec.projectID) ||
        !uuid7.test(spec.targetID) ||
        (cancel
          ? spec.expectedVersion !== undefined || spec.text !== undefined
          : !/^[1-9][0-9]*$/.test(spec.expectedVersion ?? "") || !spec.text)
      ) {
        throw new Error("WORK_INCOMPLETE_DECLARATION_REJECTED");
      }
      let settleFailure!: () => void;
      const failure = new Promise<void>((resolve) => {
        settleFailure = resolve;
      });
      const slot: Slot = {
        spec: { ...spec },
        allowed: !cancel,
        failed: false,
        succeeded: false,
        ownedResponse: false,
        failure,
        settleFailure,
      };
      slots.push(slot);
      return {
        authorizeCancellation() {
          if (!cancel || !slot.request || slot.failed || slot.allowed || closed)
            throw new Error("WORK_INCOMPLETE_CANCEL_NOT_HELD");
          // Caller first requires the real fixture hold.started, then invokes
          // this immediately before the actual navigation cancellation action.
          slot.allowed = true;
        },
      };
    },
    request(request: Request) {
      for (const slot of slots) {
        if (slot.failed || !matches(slot, request)) continue;
        if (slot.request) {
          errors++;
          return;
        }
        slot.request = request;
        slot.key = request.headers()["idempotency-key"];
        slot.body = request.postData() ?? undefined;
        return;
      }
    },
    failed(request: Request) {
      const slot = slots.find((value) => value.request === request);
      if (!slot) {
        errors++;
        return;
      }
      if (closed || !slot.allowed || slot.failed || slot.succeeded) errors++;
      slot.failed = true;
      slot.settleFailure();
    },
    finished(request: Request) {
      const slot = slots.find((value) => value.request === request);
      if (!slot || slot.spec.kind === "canceled-task-read") return;
      errors++;
      slot.succeeded = true;
      slot.settleFailure();
    },
    close() {
      closed = true;
      // End our event wait without creating a transport-finished operation.
      for (const slot of slots)
        if (slot.spec.kind !== "canceled-task-read") slot.settleFailure();
    },
    truncation(request: Request) {
      return slots.some(
        (slot) =>
          slot.request === request && slot.spec.kind !== "canceled-task-read",
      );
    },
    async truncationTerminal(request: Request) {
      const slot = slots.find((value) => value.request === request);
      if (!slot || slot.spec.kind === "canceled-task-read")
        throw new Error("WORK_TRUNCATION_NOT_DECLARED");
      await slot.failure;
      if (closed || !slot.allowed || !slot.failed || slot.succeeded)
        throw new Error("WORK_TRUNCATION_NOT_ACTUAL_FAILED");
      return new Error("WORK_DECLARED_INCOMPLETE");
    },
    terminal(request: Request, originalFinished: Promise<Error | null>) {
      const slot = slots.find((value) => value.request === request);
      if (!slot) return originalFinished;
      // The caller still starts exactly one original finished() operation and
      // records its own real return; failure winning never labels it finished.
      return Promise.race([
        originalFinished,
        slot.failure.then(() => new Error("WORK_DECLARED_INCOMPLETE")),
      ]);
    },
    expectedFailure,
    unforwarded(request: Request) {
      return slots.some(
        (slot) =>
          slot.request === request &&
          slot.spec.kind === "unforwarded-milestone-update",
      );
    },
    ownedUnforwardedResponse(
      request: Request,
      status: number,
      headers: Record<string, string>,
    ) {
      const slot = slots.find(
        (value) =>
          value.request === request &&
          value.spec.kind === "unforwarded-milestone-update",
      );
      if (
        !slot ||
        closed ||
        !slot.allowed ||
        slot.ownedResponse ||
        status !== 503 ||
        headers["content-type"] !== "application/problem+json" ||
        headers["content-length"] !== "4096" ||
        headers.connection !== "close" ||
        "x-request-id" in headers ||
        "transfer-encoding" in headers
      )
        return false;
      slot.ownedResponse = true;
      return true;
    },
    declared(request: Request) {
      return slots.some((slot) => slot.request === request);
    },
    declarationKind(request: Request) {
      return slots.find((slot) => slot.request === request)?.spec.kind ?? null;
    },
    verify(expected: number, failedRequests: Set<Request>) {
      return (
        (slots.length === 0 || !closed) &&
        errors === 0 &&
        expected === slots.length &&
        slots.every(
          (slot) =>
            slot.request &&
            slot.allowed &&
            slot.failed &&
            !slot.succeeded &&
            (slot.spec.kind !== "unforwarded-milestone-update" ||
              slot.ownedResponse) &&
            failedRequests.has(slot.request),
        ) &&
        [...failedRequests].every(expectedFailure)
      );
    },
  };
}

// Installed at the original request event, before its response or failure.
// Only the recovery case opts into this closed endpoint set.
export function workOrdinaryCompletionEvents() {
  type Terminal = "finished" | "failed" | "closed";
  const rows = new Map<
    Request,
    {
      terminal: Terminal | null;
      finished: number;
      failed: number;
      release?: (terminal: Terminal) => void;
    }
  >();
  let sealed = false;
  const signal = (request: Request, terminal: Terminal) => {
    const row = rows.get(request);
    if (!row) return;
    if (terminal === "finished") row.finished++;
    if (terminal === "failed") row.failed++;
    if (row.terminal === null) {
      row.terminal = terminal;
      row.release?.(terminal);
      row.release = undefined;
    }
  };
  const seal = () => {
    sealed = true;
    for (const request of rows.keys()) signal(request, "closed");
  };
  return {
    request(request: Request) {
      if (sealed || rows.has(request)) return;
      const url = new URL(request.url());
      const parts = url.pathname.split("/");
      if (
        url.search ||
        parts.slice(0, 4).join("/") !== "/api/v1/projects" ||
        !uuid7.test(parts[4]!) ||
        !(
          (parts.length === 7 &&
            request.method() === "GET" &&
            ["milestones", "sprints", "tasks"].includes(parts[5]!) &&
            uuid7.test(parts[6]!)) ||
          (parts.length === 7 &&
            request.method() === "POST" &&
            ["structure-commands", "task-commands"].includes(parts[5]!) &&
            parts[6] === "lookup") ||
          (parts.length === 9 &&
            request.method() === "POST" &&
            parts[5] === "tasks" &&
            uuid7.test(parts[6]!) &&
            parts[7] === "blocker-commands" &&
            parts[8] === "lookup")
        )
      )
        return;
      rows.set(request, { terminal: null, finished: 0, failed: 0 });
    },
    selected: (request: Request) => rows.has(request),
    failed: (request: Request) => signal(request, "failed"),
    finished: (request: Request) => signal(request, "finished"),
    terminal(request: Request): Promise<Terminal> {
      const row = rows.get(request);
      if (!row || row.release) return Promise.resolve("closed");
      if (row.terminal !== null) return Promise.resolve(row.terminal);
      return new Promise((resolve) => {
        row.release = resolve;
      });
    },
    failedOnly(request: Request) {
      const row = rows.get(request);
      return (
        row?.terminal === "failed" && row.failed === 1 && row.finished === 0
      );
    },
    seal,
    pending: () => [...rows.values()].filter((row) => row.release).length,
  };
}

export function observe(
  page: Page,
  options: {
    ordinaryCompletion?: (request: Request, requestID: string) => boolean;
  } = {},
) {
  const startedAt = performance.now();
  const incompleteRequests = workIncompleteLedger();
  const ordinaryEvents = workOrdinaryCompletionEvents();
  const nativeComplete = new Set<Request>();
  const pendingNative = new Set<Request>();
  let ordinaryClosed = false;
  const closeOrdinary = () => {
    ordinaryClosed = true;
    ordinaryEvents.seal();
  };
  page.context().on("close", closeOrdinary);
  type Timing = {
    request: Request;
    response?: Response;
    request_id: string | null;
    request_at: number | null;
    response_at: number | null;
    request_failed_at: number | null;
    request_finished_at: number | null;
    response_finished_at: number | null;
    observer_rejected_at: number | null;
    failure_reason: string | null;
  };
  const timings = new Map<Request, Timing>();
  let truncated = false;
  let closedAt: number | null = null;
  const now = () => Number((performance.now() - startedAt).toFixed(3));
  const timing = (request: Request) => {
    let value = timings.get(request);
    if (!value) {
      value = {
        request,
        request_id: null,
        request_at: null,
        response_at: null,
        request_failed_at: null,
        request_finished_at: null,
        response_finished_at: null,
        observer_rejected_at: null,
        failure_reason: null,
      };
      if (timings.size < 256) timings.set(request, value);
      else truncated = true;
    }
    return value;
  };
  failureSnapshots.push(() => ({
    snapshot_at: now(),
    page_closed_at: closedAt,
    truncated,
    requests: [...timings.values()].map((value) => {
      const { request, response, ...safe } = value;
      const stage = response && workBodyAwaits.get(response);
      return {
        ...workDiagnosticTarget(request),
        ...safe,
        response_same_request_object: response
          ? response.request() === request
          : null,
        original_body_await: stage
          ? {
              stage: stage.stage,
              at: Number((stage.at - startedAt).toFixed(3)),
            }
          : null,
      };
    }),
  }));
  page.on("close", () => {
    closedAt = now();
    incompleteRequests.close();
    closeOrdinary();
  });
  const facts = new Map<string, Observed>();
  const ownedTruncations = new Set<Request>();
  const tails: Promise<void>[] = [];
  let observerErrors = 0;
  const failedRequests = new Set<Request>();
  page.on("requestfailed", (request) => {
    if (isWork(new URL(request.url()))) {
      failedRequests.add(request);
      if (
        !ordinaryEvents.selected(request) ||
        incompleteRequests.declared(request)
      )
        incompleteRequests.failed(request);
      ordinaryEvents.failed(request);
      const value = timing(request);
      value.request_failed_at = now();
      value.failure_reason = safeWorkFailure(request);
    }
  });
  const requests: { method: string; path: string }[] = [];
  const isWork = (url: URL) =>
    /^\/api\/v1\/projects\/[^/]+\/(?:milestones|sprints|tasks|structure-commands|task-commands)(?:\/|$)/.test(
      url.pathname,
    );
  page.on("request", (r) => {
    const url = new URL(r.url());
    if (isWork(url)) {
      requests.push({ method: r.method(), path: url.pathname });
      timing(r).request_at = now();
      incompleteRequests.request(r);
      if (options.ordinaryCompletion) ordinaryEvents.request(r);
    }
  });
  page.on("requestfinished", (request) => {
    if (isWork(new URL(request.url()))) {
      timing(request).request_finished_at = now();
      incompleteRequests.finished(request);
      ordinaryEvents.finished(request);
    }
  });
  page.on("response", (r) => {
    const url = new URL(r.url());
    if (!isWork(url)) return;
    const observed = timing(r.request());
    observed.response = r;
    observed.response_at = now();
    const fact: Observed = {
      request: r.request(),
      url,
      status: r.status(),
      finished: false,
      failed: false,
    };
    tails.push(
      (async () => {
        const id = await r.headerValue("x-request-id");
        observed.request_id = id && uuid7.test(id) ? id : null;
        const owned = incompleteRequests.unforwarded(r.request());
        if (owned) {
          if (
            !incompleteRequests.ownedUnforwardedResponse(
              r.request(),
              r.status(),
              await r.allHeaders(),
            )
          )
            throw new Error("WORK_OWNED_TRUNCATION_HEADERS_REJECTED");
        } else {
          if (!id || facts.has(id))
            throw new Error("WORK_RESPONSE_IDENTITY_MISSING_OR_DUPLICATE");
          facts.set(id, fact);
        }
        let error: Error | null;
        if (incompleteRequests.truncation(r.request())) {
          // PW1.56.1 requestfailed does not settle Response.finished(). Only
          // these four predeclared cuts use the original Request event tail.
          error = await incompleteRequests.truncationTerminal(r.request());
        } else if (incompleteRequests.declared(r.request())) {
          const originalFinished = r.finished();
          error = await incompleteRequests.terminal(
            r.request(),
            originalFinished.then(
              (result) => {
                observed.response_finished_at = now();
                return result;
              },
              (failure: unknown) => {
                observerErrors++;
                observed.observer_rejected_at = now();
                throw failure;
              },
            ),
          );
        } else if (ordinaryEvents.selected(r.request()) && r.status() === 200) {
          const terminal = await ordinaryEvents.terminal(r.request());
          if (terminal === "closed")
            throw new Error("WORK_ORDINARY_EVENT_MISSING");
          if (terminal === "failed") {
            // No Response.finished() is created for known failed requests.
            // This remains a failure unless verify proves the whole method.
            pendingNative.add(r.request());
            error = new Error("WORK_ORDINARY_CONSUMPTION_UNPROVEN");
          } else {
            error = await r.finished();
            observed.response_finished_at = now();
          }
        } else {
          error = await r.finished();
          observed.response_finished_at = now();
        }
        fact.finished = error === null;
        fact.failed = error !== null;
        if (owned) {
          if (!fact.failed || !incompleteRequests.expectedFailure(r.request()))
            throw new Error("WORK_OWNED_TRUNCATION_NOT_FAILED");
          ownedTruncations.add(r.request());
        }
      })().catch(() => {
        // Attach the rejection sink immediately, including when the case ends
        // before verify. This records observation failure, never transport EOF.
        observerErrors++;
        observed.observer_rejected_at = now();
        fact.failed = true;
      }),
    );
  });
  return {
    requests,
    declareIncomplete: incompleteRequests.declare,
    declarationKind: incompleteRequests.declarationKind,
    async verify(expectedIncomplete = 0) {
      // The caller must already have taken actual diagnostic end snapshots.
      // Missing original events retire as failure, never as an abandoned wait.
      ordinaryEvents.seal();
      try {
        await Promise.all(tails);
        expect(observerErrors).toBe(0);
        expect(ordinaryEvents.pending()).toBe(0);
        for (const request of pendingNative) {
          const observed = timings.get(request);
          expect(
            closedAt === null &&
              !ordinaryClosed &&
              !truncated &&
              !incompleteRequests.declared(request) &&
              ordinaryEvents.failedOnly(request) &&
              observed?.failure_reason === "aborted" &&
              observed.request_failed_at !== null &&
              observed.request_finished_at === null &&
              observed.response_finished_at === null &&
              observed.observer_rejected_at === null &&
              observed.response?.request() === request &&
              options.ordinaryCompletion?.(request, observed.request_id!) ===
                true,
          ).toBe(true);
          const fact = observed?.request_id && facts.get(observed.request_id);
          expect(
            !!fact && fact.request === request && fact.failed && !fact.finished,
          ).toBe(true);
          if (fact) {
            fact.finished = true;
            fact.failed = false;
          }
          nativeComplete.add(request);
        }
        expect(
          incompleteRequests.verify(
            expectedIncomplete,
            new Set(
              [...failedRequests].filter(
                (request) => !nativeComplete.has(request),
              ),
            ),
          ),
        ).toBe(true);
        const checked = spawnSync(
          "python3",
          ["-c", schemaProgram, repository, evidence],
          { encoding: "utf8", timeout: 6000, maxBuffer: 4096 },
        );
        writeFileSync(
          join(evidence, "work-schema-validation.json"),
          JSON.stringify({
            status: checked.status,
            signal: checked.signal,
            stdout: checked.stdout,
            stderr: checked.stderr,
            failed_to_run: !!checked.error,
          }),
          { mode: 0o600 },
        );
        expect(checked.status === 0 && /^\d+\s*$/.test(checked.stdout)).toBe(
          true,
        );
        let decoded = 0,
          incomplete = 0;
        for (const name of readdirSync(evidence).filter((name) =>
          /^response-\d+\.json$/.test(name),
        )) {
          const meta = JSON.parse(readFileSync(join(evidence, name), "utf8"));
          const fact = facts.get(meta.request_id);
          if (!fact) continue;
          expect(
            meta.endpoint === fact.url.pathname &&
              meta.method === fact.request.method() &&
              meta.status === fact.status,
          ).toBe(true);
          expect(meta.body_file === `body-${meta.body_sha256}.json`).toBe(true);
          const raw = readFileSync(join(evidence, meta.body_file));
          expect(
            createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
          ).toBe(true);
          expect(fact.finished !== fact.failed).toBe(true);
          if (fact.failed) incomplete++;
          await decodeOriginal(fact, meta, raw);
          decoded++;
        }
        const failedWithoutHeaders = [...failedRequests].filter(
          (request) =>
            !ownedTruncations.has(request) &&
            ![...facts.values()].some((fact) => fact.request === request),
        ).length;
        expect(
          decoded === facts.size &&
            decoded > 0 &&
            incomplete + failedWithoutHeaders + ownedTruncations.size ===
              expectedIncomplete,
        ).toBe(true);
        expect(
          [...nativeComplete].every(
            (request) =>
              !ordinaryClosed &&
              ordinaryEvents.failedOnly(request) &&
              options.ordinaryCompletion?.(
                request,
                timings.get(request)!.request_id!,
              ) === true,
          ),
        ).toBe(true);
        writeFileSync(
          join(evidence, "work-body-validation.json"),
          JSON.stringify({
            original_bodies: decoded,
            browser_complete: decoded - incomplete,
            original_native_complete_with_pw_failed: nativeComplete.size,
            expected_incomplete: incomplete,
            owned_unforwarded_truncations: ownedTruncations.size,
            failed_without_headers: failedWithoutHeaders,
            all_schema_client_validated: true,
          }),
          { mode: 0o600 },
        );
        return { original_bodies: decoded, expected_incomplete: incomplete };
      } finally {
        ordinaryEvents.seal();
        page.context().off("close", closeOrdinary);
      }
    },
  };
}

async function decodeOriginal(
  fact: Observed,
  meta: Record<string, any>,
  raw: Buffer,
) {
  const api = createWorkPlanningAPI(async (url, init) => {
    const generated = new URL(String(url), fact.url.origin);
    expect(
      generated.pathname === fact.url.pathname &&
        init?.method === fact.request.method(),
    ).toBe(true);
    expect(
      [...generated.searchParams].sort().toString() ===
        [...fact.url.searchParams].sort().toString(),
    ).toBe(true);
    if (init?.body)
      expect(
        JSON.stringify(JSON.parse(String(init.body))) ===
          JSON.stringify(fact.request.postDataJSON()),
      ).toBe(true);
    return new Response(new Uint8Array(raw).buffer, {
      status: meta.status,
      headers: {
        "Content-Type": meta.content_type,
        "X-Request-ID": meta.request_id,
      },
    });
  });
  const parts = fact.url.pathname.split("/");
  const project = parts[4]!,
    entity = parts[5]!,
    target = parts[6];
  expect(uuid7.test(project)).toBe(true);
  const signal = new AbortController().signal;
  const query: Record<string, unknown> = {};
  for (const [key, value] of fact.url.searchParams)
    query[key] =
      key === "limit"
        ? Number(value)
        : key === "assignee_agent_id" && value === "null"
          ? null
          : value;
  const options = {
    signal,
    csrf: "x".repeat(43),
    key: "original-body-validation",
  };
  const method = fact.request.method();
  const body = method === "GET" ? undefined : fact.request.postDataJSON();
  let action: keyof typeof api;
  let args: unknown[];
  if (entity === "structure-commands" || entity === "task-commands") {
    action =
      entity === "structure-commands"
        ? "lookupStructureCommand"
        : "lookupTaskCommand";
    args = [project, body, options];
  } else if (
    entity === "tasks" &&
    (parts[7] === "blockers" || parts[7] === "blocker-commands")
  ) {
    action =
      parts[7] === "blocker-commands"
        ? "lookupTaskBlockerCommand"
        : method === "GET"
          ? "listTaskBlockers"
          : parts[8] === "resolve"
            ? "resolveTaskBlocker"
            : "addTaskBlocker";
    args = [
      project,
      target,
      method === "GET" ? query : body,
      method === "GET" ? signal : options,
    ];
  } else {
    const singular = (
      { milestones: "Milestone", sprints: "Sprint", tasks: "Task" } as Record<
        string,
        string
      >
    )[entity];
    if (!singular) throw new Error("UNKNOWN_WORK_RESPONSE_ROUTE");
    action = (
      method === "GET"
        ? target
          ? `get${singular}`
          : `list${singular}s`
        : method === "PATCH"
          ? `update${singular}`
          : parts[7] === "reorder"
            ? `reorder${singular}`
            : `create${singular}`
    ) as keyof typeof api;
    args =
      method === "GET"
        ? target
          ? [project, target, signal]
          : [project, query, signal]
        : target
          ? [project, target, body, options]
          : [project, body, options];
  }
  try {
    await (api[action] as (...values: unknown[]) => Promise<unknown>)(...args);
    expect(meta.status === 200).toBe(true);
  } catch (error) {
    expect(
      meta.status !== 200 &&
        error instanceof AccountFailure &&
        error.kind === "problem" &&
        error.problem?.status === meta.status,
    ).toBe(true);
  }
}
const schemaProgram = String.raw`
import hashlib,json,pathlib,re,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 base=pathlib.Path(sys.argv[1])/'api/openapi'; evidence=pathlib.Path(sys.argv[2])
 docs={n:json.loads((base/n).read_bytes()) for n in ['common.json','work-planning.json']}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  m=json.loads(path.read_bytes()); endpoint=m['endpoint']
  if not re.match(r'^/api/v1/projects/[^/]+/(milestones|sprints|tasks|structure-commands|task-commands)(/|$)',endpoint): continue
  assert m['source_run'].startswith(('TestAccountProjectWorkPlanningWeb','TestIndependentProjectWorkPlanningWeb')) and len(m['input_hash'])==64
  filename='body-'+m['body_sha256']+'.json'; assert m['body_file']==filename
  raw=(evidence/filename).read_bytes(); assert hashlib.sha256(raw).hexdigest()==m['body_sha256']
  found=[]
  for template,methods in docs['work-planning.json']['paths'].items():
   pattern=re.sub(r'\{[^{}]+\}',r'[^/]+',template)
   if re.fullmatch(pattern,endpoint) and m['method'].lower() in methods: found.append(methods[m['method'].lower()])
  assert len(found)==1
  response=found[0]['responses'].get(str(m['status']),found[0]['responses'].get('default'))
  if '$ref' in response:
   ref=response['$ref']; assert re.fullmatch(r'#/components/responses/[A-Za-z0-9_]+',ref)
   response=docs['work-planning.json']['components']['responses'][ref.rsplit('/',1)[1]]
  media=m['content_type'].split(';')[0].strip().lower(); schema=dict(docs['work-planning.json']); schema['$id']=(base/'work-planning.json').as_uri(); schema.update(response['content'][media]['schema'])
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw)); count+=1
 assert count>0
 print(count)
except Exception:
 print('SAFE_WORK_BODY_SCHEMA_FAILED',file=sys.stderr);sys.exit(1)
`;
