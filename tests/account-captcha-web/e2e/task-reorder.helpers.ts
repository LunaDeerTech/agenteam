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
import { observeBrowserIO } from "./task-review.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
import { observeSystemRunnerWire } from "./system-runner-management.helpers";
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const hash = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");
export type ReorderMaterial = {
  mode: "task-reorder-normal" | "task-reorder-recovery";
  origin: string;
  cookie: string;
  project_id: string;
  other_project_id: string;
  sprint_id: string;
  task_id: string;
  anchor_id: string;
  route: string;
  task: Record<string, any>;
  rows: Record<string, any>[];
  input_hash: string;
};
export function taskReorderMaterial(): ReorderMaterial {
  const data: ReorderMaterial = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "task-reorder-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe(process.env.AGENTEAM_TASK_REORDER_WEB_CASE);
  expect(["task-reorder-normal", "task-reorder-recovery"]).toContain(data.mode);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_TASK_REORDER_WEB_INPUT_HASH,
  );
  for (const id of [
    data.project_id,
    data.other_project_id,
    data.sprint_id,
    data.task_id,
    data.anchor_id,
  ])
    expect(id).toMatch(uuid);
  expect(data.other_project_id).not.toBe(data.project_id);
  expect(data.rows).toHaveLength(52);
  expect(new Set(data.rows.map((row) => row.id)).size).toBe(52);
  expect(data.rows[0]).toEqual(data.task);
  expect(data.task_id).toBe(data.task.id);
  expect(data.anchor_id).toBe(data.rows[51]!.id);
  for (const [index, row] of data.rows.entries()) {
    expect(row.project_id).toBe(data.project_id);
    expect(row.sprint_id).toBe(data.sprint_id);
    expect(row.state).toBe("backlog");
    expect(row.priority).toBe("medium");
    expect(row.assignee_agent_id).toBeNull();
    expect(row.version).toBe("1");
    if (index)
      expect(data.rows[index - 1]!.manual_rank < row.manual_rank).toBe(true);
  }
  expect(data.route).toMatch(
    new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${data.task_id}$`),
  );
  return data;
}

type Entry = {
  request: Request;
  name: string;
  terminal?: "finished" | "failed";
  status?: number;
  requestID?: string;
  checked?: boolean;
  body?: any;
};
export async function observeTaskReorder(page: Page, data: ReorderMaterial) {
  const evidence = process.env.AGENTEAM_TASK_REORDER_WEB_EVIDENCE!;
  const binding = await knowledgeSessionBinding(
    resolve("../.."),
    process.env.AGENTEAM_TASK_REORDER_WEB_DIST!,
  );
  const wire = await observeSystemRunnerWire(page, data.origin);
  const entries: Entry[] = [];
  const prefix = `/api/v1/projects/${data.project_id}`,
    target = `${prefix}/tasks/${data.task_id}`;
  let observerError = false,
    installed = 0,
    handlers = 0,
    retired = false,
    nativeRetired = false,
    cut = 0;
  const keys = new Set<string>(),
    diagnostics: unknown[] = [],
    proofs: unknown[] = [];
  const writes: Request[] = [],
    lookups: Request[] = [];
  function classify(request: Request): string | undefined {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    expect(url.origin).toBe(data.origin);
    if (method === "POST") {
      expect(url.search).toBe("");
      const key = request.headers()["idempotency-key"]!;
      expect(
        typeof key === "string" && /^[A-Za-z0-9._:/-]{1,128}$/.test(key),
      ).toBe(true);
      if (path === target + "/reorder") {
        const index = writes.length;
        expect(index).toBeLessThan(data.mode === "task-reorder-normal" ? 3 : 2);
        expect(request.postDataJSON()).toEqual({
          expected_version: String(index + 1),
          request:
            data.mode === "task-reorder-recovery" || index === 0
              ? { before_id: data.anchor_id }
              : {},
        });
        expect(keys.has(key)).toBe(false);
        keys.add(key);
        writes.push(request);
        return "reorder";
      }
      if (path === `${prefix}/task-commands/lookup`) {
        expect(data.mode).toBe("task-reorder-recovery");
        expect(writes).toHaveLength(2);
        expect(lookups).toHaveLength(0);
        const original = writes[1]!;
        expect(request.postDataJSON()).toEqual({
          command: "work.task.reorder",
          target_id: data.task_id,
          ...original.postDataJSON(),
        });
        expect(key === original.headers()["idempotency-key"]).toBe(true);
        lookups.push(request);
        return "lookup";
      }
      throw Error("TASK_REORDER_UNDECLARED_COMMAND");
    }
    expect(method).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined();
    if (path === "/api/v1/session" && !url.search) return "session";
    if (path === "/api/v1/projects/resolve") return "resolve";
    if (path === "/api/v1/projects") return "projects";
    if (path === prefix && !url.search) return "project";
    if (path === target && !url.search) return "task";
    if (path === `${prefix}/sprints/${data.sprint_id}` && !url.search)
      return "sprint";
    if (
      path === `${prefix}/milestones/${data.task.milestone_id}` &&
      !url.search
    )
      return "milestone";
    if (
      [
        `${prefix}/milestones`,
        `${prefix}/agents`,
        `${target}/blockers`,
      ].includes(path)
    ) {
      expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]);
      return path === target + "/blockers"
        ? "blockers"
        : path.endsWith("/agents")
          ? "agents"
          : "milestones";
    }
    if (path === `${prefix}/sprints`) {
      expect([...url.searchParams.keys()].sort()).toEqual([
        "limit",
        "milestone_id",
      ]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("milestone_id")).toBe(data.task.milestone_id);
      return "sprints";
    }
    if (path === target + "/events") {
      expect([...url.searchParams.keys()].sort()).toEqual(["limit", "order"]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("order")).toBe("desc");
      return "events";
    }
    if (path === prefix + "/tasks") {
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("sprint_id")).toBe(data.sprint_id);
      const priority = url.searchParams.get("priority");
      const expected = [
        "limit",
        "sprint_id",
        "state",
        ...(priority ? ["priority"] : []),
        ...(url.searchParams.has("cursor") ? ["cursor"] : []),
      ].sort();
      expect([...url.searchParams.keys()].sort()).toEqual(expected);
      if (priority) {
        expect(priority).toBe("medium");
        expect(url.searchParams.get("state")).toBe("backlog");
      } else
        expect([
          "backlog",
          "todo",
          "in_progress",
          "in_review",
          "blocked",
          "done",
          "cancelled",
        ]).toContain(url.searchParams.get("state"));
      if (url.searchParams.has("cursor"))
        expect(url.searchParams.get("cursor")).not.toBe("");
      return priority ? "candidates" : "tasks";
    }
    throw Error("TASK_REORDER_UNDECLARED_READ");
  }
  const requested = (request: Request) => {
    try {
      const name = classify(request);
      if (name) {
        if (entries.length >= 192) throw Error("TASK_REORDER_REQUEST_BOUND");
        entries.push({ request, name });
      }
    } catch {
      observerError = true;
    }
  };
  const response = (response: Response) => {
    const entry = entries.find((entry) => entry.request === response.request());
    if (!entry) return;
    if (entry.status !== undefined) observerError = true;
    entry.status = response.status();
    entry.requestID = response.headers()["x-request-id"];
  };
  function end(request: Request, terminal: Entry["terminal"]) {
    const entry = entries.find((entry) => entry.request === request);
    if (!entry) return;
    if (entry.terminal) observerError = true;
    entry.terminal = terminal;
  }
  const finished = (request: Request) => end(request, "finished"),
    failed = (request: Request) => end(request, "failed");
  page.on("request", requested);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  await observeBrowserIO(page, (event) => {
    if (diagnostics.length < 4096) diagnostics.push(event);
    else observerError = true;
  });
  const entryURL = new URL(binding.entry, data.origin).href;
  const install = async (route: Route) => {
    handlers++;
    try {
      expect(installed++).toBe(0);
      await page.evaluate(
        async ({ binding, project, task }) => {
          const module = await import(/* @vite-ignore */ binding.asset),
            useSession = module[binding.exportName];
          if (typeof useSession !== "function")
            throw Error("TASK_REORDER_SINGLETON");
          const auth = useSession();
          if (auth !== useSession()) throw Error("TASK_REORDER_SINGLETON");
          const proof = (window as any).__taskReviewProof;
          proof.consumers(
            auth,
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
          );
          proof.taskReorder(project, task);
        },
        { binding, project: data.project_id, task: data.task_id },
      );
      await route.continue();
    } catch {
      observerError = true;
      await route.abort();
    } finally {
      handlers--;
    }
  };
  await page.route(entryURL, install);
  const snapshot = () =>
    page.evaluate(() => (window as any).__taskReviewProof.snapshot());
  async function verify() {
    expect(observerError).toBe(false);
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length)
      .toBe(0);
    await expect.poll(async () => (await snapshot()).pending).toBe(0);
    const proof = await snapshot();
    expect(proof.failed).toBe(false);
    expect(proof.current && proof.not_busy).toBe(true);
    for (const entry of entries.filter((entry) => !entry.checked)) {
      expect(entry.requestID).toMatch(uuid);
      const requestID = entry.requestID!,
        url = new URL(entry.request.url());
      let record: any;
      await expect
        .poll(() => {
          try {
            record = JSON.parse(
              readFileSync(
                join(
                  evidence,
                  `task-reorder-${data.mode}-response-${requestID}.json`,
                ),
                "utf8",
              ),
            );
            return record.handler_returned === true;
          } catch {
            return false;
          }
        })
        .toBe(true);
      expect(record.request_id).toBe(requestID);
      expect(record.method).toBe(entry.request.method());
      expect(record.status).toBe(entry.status);
      expect(record.url_sha256).toBe(hash(url.pathname + url.search));
      expect(
        record.original_eof &&
          record.original_closed &&
          record.handler_returned &&
          record.request_host_matches,
      ).toBe(true);
      await wire.headers({
        method: entry.request.method(),
        url: entry.request.url(),
        status: entry.status!,
        requestID,
      });
      const rows = proof.rows.filter(
        (row: any) => row.request_id === requestID,
      );
      expect(rows).toHaveLength(1);
      const row = rows[0],
        calls = proof.calls.filter((call: any) => call.id === row.call_id);
      expect(calls).toHaveLength(1);
      const call = calls[0];
      expect(call.bound).toBe(1);
      expect(call.method).toBe(entry.request.method());
      expect(call.path).toBe(url.pathname);
      expect(row.fetch_returned).toBe(true);
      expect(row.fetch_rejected).toBe(false);
      expect(row.readers).toBe(1);
      expect(row.release).toBe(1);
      expect(call.settled && call.current && call.not_busy).toBe(true);
      const command = entry.name === "reorder" || entry.name === "lookup";
      if (command) {
        expect(call.request_bound && row.request_bound).toBe(true);
        for (const source of [record, row, call]) {
          expect(source.request_body_sha256).toBe(
            hash(entry.request.postDataBuffer()!),
          );
          expect(source.idempotency_key_sha256).toBe(
            hash(entry.request.headers()["idempotency-key"]!),
          );
        }
        expect(record.input_verified).toBe(true);
      }
      const isCut =
        data.mode === "task-reorder-recovery" && entry.request === writes[1];
      const conflict =
        data.mode === "task-reorder-recovery" && entry.request === writes[0];
      if (isCut) {
        expect(cut++).toBe(0);
        expect(entry.status).toBe(200);
        expect(entry.terminal).toBe("failed");
        expect(record.receipt_cut && record.committed_verified).toBe(true);
        expect(call.rejected && !call.fulfilled && call.result_current).toBe(
          true,
        );
        expect(row.read_returns + 1).toBe(row.reads);
        expect(row.error && !row.eof).toBe(true);
        expect(row.bytes).toBeLessThan(record.content_length);
      } else {
        expect(entry.status).toBe(conflict ? 409 : 200);
        expect(
          entry.terminal === "finished" ||
            entry.request.failure()?.errorText === "net::ERR_ABORTED",
        ).toBe(true);
        expect(row.read_returns).toBe(row.reads);
        expect(row.eof && row.closed && !row.error && !row.aborted).toBe(true);
        expect(row.reader_cancel).toBe(0);
        expect(row.stream_cancel).toBe(0);
        expect(row.bytes).toBe(Number(row.length));
        expect(row.bytes).toBe(record.content_length);
        expect(row.digest).toBe(record.body_sha256);
        if (conflict) {
          expect(record.rejected_verified).toBe(true);
          expect(record.committed_verified).toBe(false);
          expect(call.rejected && !call.fulfilled && call.result_current).toBe(
            true,
          );
          expect(call.rejection_code).toBe("TASK_VERSION_CONFLICT");
          expect(call.rejection_commit_state).toBe("not_committed");
        } else {
          expect(call.fulfilled && !call.rejected).toBe(true);
          expect(row.typed_digest).toBe(call.typed_digest);
          if (command)
            expect(record.committed_verified && call.result_current).toBe(true);
          entry.body = call.typed;
        }
      }
      proofs.push({
        name: entry.name,
        request_id: requestID,
        status: entry.status,
        terminal: entry.terminal,
        original_eof: row.eof,
        declared_cut: isCut,
        conflict,
        go: record,
      });
      entry.checked = true;
    }
    expect(observerError).toBe(false);
  }
  return {
    verify,
    requests: (name: string) =>
      entries
        .filter((entry) => entry.name === name)
        .map((entry) => entry.request),
    result(name: string, index = -1) {
      const rows = entries.filter(
        (entry) => entry.name === name && entry.checked && entry.body,
      );
      expect(rows.length).toBeGreaterThan(0);
      return rows.at(index)!.body;
    },
    async retire() {
      if (retired) return;
      try {
        await verify();
      } finally {
        try {
          await page.unroute(entryURL, install);
          expect(handlers).toBe(0);
          await expect.poll(async () => (await snapshot()).pending).toBe(0);
          const proof = await page.evaluate(() =>
            (window as any).__taskReviewProof.finish(),
          );
          nativeRetired =
            proof.retired === true && !proof.failed && proof.pending === 0;
          expect(nativeRetired).toBe(true);
        } finally {
          try {
            await wire.close();
            wire.complete();
          } finally {
            page.off("request", requested);
            page.off("response", response);
            page.off("requestfinished", finished);
            page.off("requestfailed", failed);
            retired = nativeRetired && wire.snapshot().actual_detach;
          }
        }
      }
    },
    save(status: string | undefined) {
      mkdirSync(evidence, { recursive: true, mode: 0o700 });
      writeFileSync(
        join(evidence, `task-reorder-${data.mode}-browser.json`),
        JSON.stringify(
          {
            status,
            retired,
            native_retired: nativeRetired,
            wire: wire.snapshot(),
            observer_error: observerError,
            requests: entries.length,
            reorders: writes.length,
            lookups: lookups.length,
            cuts: cut,
            proofs,
            diagnostics,
          },
          null,
          2,
        ),
        { mode: 0o600 },
      );
    },
  };
}
export async function taskReorderScreenshot(
  page: Page,
  data: ReorderMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  const evidence = process.env.AGENTEAM_TASK_REORDER_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(evidence, `task-reorder-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
