import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
import { observeBrowserIO } from "./task-review.helpers";
import { observeSystemRunnerWire } from "./system-runner-management.helpers";

export type M2ReviewOptions = Readonly<{
  return_comment?: string;
  deadline_unix_ms?: number;
}>;
export function m2ReviewOptions(
  value: unknown,
  now = Date.now(),
): M2ReviewOptions {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("M2_INVALID_LIVE_REVIEW_OPTIONS");
  const raw = value as Record<string, unknown>;
  const options: { return_comment?: string; deadline_unix_ms?: number } = {};
  if (Object.hasOwn(raw, "return_comment")) {
    const comment = raw.return_comment;
    if (
      typeof comment !== "string" ||
      !comment.trim() ||
      Buffer.byteLength(comment, "utf8") > 2000 ||
      /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/u.test(comment) ||
      /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(
        comment,
      )
    )
      throw Error("M2_INVALID_RETURN_COMMENT");
    options.return_comment = comment;
  }
  if (Object.hasOwn(raw, "deadline_unix_ms")) {
    const deadline = raw.deadline_unix_ms;
    if (
      typeof deadline !== "number" ||
      !Number.isSafeInteger(deadline) ||
      deadline <= now ||
      deadline > now + 300_000
    )
      throw Error("M2_INVALID_REVIEW_DEADLINE");
    options.deadline_unix_ms = deadline;
  }
  return Object.freeze(options);
}
export function m2RemainingBudget(
  options: M2ReviewOptions,
  controlled: number,
  now = Date.now(),
) {
  if (options.deadline_unix_ms === undefined) return controlled;
  const remaining = options.deadline_unix_ms - now;
  if (!Number.isSafeInteger(remaining) || remaining <= 0)
    throw Error("M2_REVIEW_DEADLINE_EXPIRED");
  return remaining;
}

const digest = (value: string) =>
  createHash("sha256").update(value).digest("hex");
const canonical = (value: any): any =>
  Array.isArray(value)
    ? value.map(canonical)
    : value && typeof value === "object"
      ? Object.fromEntries(
          Object.keys(value)
            .sort()
            .map((key) => [key, canonical(value[key])]),
        )
      : value;

// Reuse D22's observers of the original Promise, Response and stream reader.
// No HTTP response is mocked and no observer fetch, clone or body read is added.
export async function observeM2Bodies(
  page: Page,
  data: {
    origin: string;
    project_id: string;
    task_id: string;
    mode: "accept" | "return-and-accept";
    execution_id: string;
    artifact_ref: { project_id: string; artifact_id: string; file_id: string };
  },
) {
  let transferTarget = data.mode === "return-and-accept" ? "todo" : "done";
  let replaced = false;
  const evidence = process.env.AGENTEAM_M2_WEB_EVIDENCE!;
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_M2_WEB_DIST!,
  );
  const entryURL = new URL(binding.entry, data.origin).href;
  const wire = await observeSystemRunnerWire(page, data.origin);
  const responses: Response[] = [],
    proofs: Record<string, unknown>[] = [];
  const terminals = new WeakMap<Request, "finished" | "failed">();
  const onFinished = (request: Request) => terminals.set(request, "finished");
  const onFailed = (request: Request) => terminals.set(request, "failed");
  let installationFailed = false,
    closed = false,
    handlers = 0;
  const onResponse = (response: Response) => {
    const url = new URL(response.url());
    if (url.origin === data.origin && url.pathname.startsWith("/api/"))
      responses.push(response);
  };
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(
        async ({ binding, project, task, execution, artifact, target }) => {
          const host = window as any;
          const module = await import(/* @vite-ignore */ binding.asset);
          const use = module[binding.exportName];
          if (typeof use !== "function" || use() !== use())
            throw Error("M2_SESSION_SINGLETON");
          const auth = use(),
            proof = host.__taskReviewProof;
          if (!proof) throw Error("M2_NATIVE_OBSERVER_MISSING");
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
            task,
            task,
          );
          const calls: any[] = [],
            restorers: (() => void)[] = [];
          const identity = () => JSON.stringify(auth.personalContext.identity);
          function observe(
            method: string,
            kind: string,
            path: (args: any[]) => string,
            verb = "GET",
          ) {
            const object = auth.workReview;
            const descriptor = Object.getOwnPropertyDescriptor(object, method)!;
            const original = object[method];
            if (typeof original !== "function" || !descriptor)
              throw Error("M2_PUBLIC_CONSUMER_MISSING");
            const wrapper = function (this: unknown, ...args: any[]) {
              if (calls.length >= 16) throw Error("M2_CONSUMER_LIMIT");
              const before = proof.snapshot(),
                own = identity();
              const call: any = {
                kind,
                method: verb,
                path: path(args),
                settled: false,
                fulfilled: false,
                rejected: false,
                current: false,
                not_busy: false,
                native_row_id: null,
              };
              const previous = new Set(before.rows.map((row: any) => row.id));
              calls.push(call);
              const pending = Reflect.apply(original, this, args);
              void pending.then(
                (value: unknown) => {
                  const rows = proof
                    .snapshot()
                    .rows.filter(
                      (row: any) =>
                        !previous.has(row.id) &&
                        row.method === verb &&
                        row.path === call.path,
                    );
                  call.native_row_id = rows.length === 1 ? rows[0].id : null;
                  call.typed = value;
                  call.fulfilled = true;
                  call.current =
                    auth.state.phase === "authenticated" &&
                    auth.personalContext.phase === "current" &&
                    identity() === own;
                  call.not_busy = auth.state.busy === false;
                  call.settled = true;
                },
                () => {
                  call.rejected = call.settled = true;
                },
              );
              return pending;
            };
            Object.defineProperty(object, method, {
              ...descriptor,
              value: wrapper,
            });
            restorers.push(() => {
              if (object[method] !== wrapper)
                throw Error("M2_CONSUMER_REPLACED");
              Object.defineProperty(object, method, descriptor);
            });
          }
          observe(
            "startTransfer",
            "owner.transfer",
            (args) => {
              if (
                args.length !== 3 ||
                args[0] !== project ||
                args[1] !== task ||
                args[2]?.request?.target_state !== target
              )
                throw Error("M2_OWNER_TRANSFER_SCOPE");
              return `/api/v1/projects/${project}/tasks/${task}/transfer`;
            },
            "POST",
          );
          observe("executionArtifacts", "work.artifacts", (args) => {
            if (
              args.length !== 2 ||
              args[0] !== project ||
              args[1]?.execution_id !== execution ||
              args[1]?.limit !== 50 ||
              Object.keys(args[1]).sort().join() !== "execution_id,limit"
            )
              throw Error("M2_ARTIFACT_LIST_SCOPE");
            return `/api/v1/projects/${project}/artifacts`;
          });
          observe(
            "executionArtifactContent",
            "work.artifactContent",
            (args) => {
              if (
                args.length !== 2 ||
                !args[0] ||
                Object.keys(args[0]).sort().join() !==
                  "artifact_id,file_id,project_id" ||
                args[0].project_id !== artifact.project_id ||
                args[0].artifact_id !== artifact.artifact_id ||
                args[0].file_id !== artifact.file_id ||
                args[1]?.offset !== "0" ||
                args[1]?.limit !== 8192 ||
                Object.keys(args[1]).sort().join() !== "limit,offset"
              )
                throw Error("M2_ARTIFACT_READ_SCOPE");
              return `/api/v1/projects/${project}/artifacts/${artifact.artifact_id}/files/${artifact.file_id}/content`;
            },
          );
          host.__m2OwnerConsumer = {
            calls,
            finish() {
              if (calls.some((call) => !call.settled))
                throw Error("M2_PUBLIC_OBSERVATION_PENDING");
              for (const restore of restorers.reverse()) restore();
            },
          };
        },
        {
          binding,
          project: data.project_id,
          task: data.task_id,
          execution: data.execution_id,
          artifact: data.artifact_ref,
          target: transferTarget,
        },
      );
      await route.continue();
    } catch {
      installationFailed = true;
      await route.abort();
    } finally {
      handlers--;
    }
  };
  page.on("response", onResponse);
  page.on("requestfinished", onFinished);
  page.on("requestfailed", onFailed);
  await observeBrowserIO(page, () => {});
  await page.route(entryURL, install);
  const snapshot = () =>
    page.evaluate(() => {
      const host = window as any;
      if (!host.__taskReviewProof || !host.__m2OwnerConsumer)
        throw Error("M2_NATIVE_PROOF_MISSING");
      return {
        ...host.__taskReviewProof.snapshot(),
        ownerCalls: host.__m2OwnerConsumer.calls,
      };
    });
  async function quiet() {
    await expect
      .poll(async () => {
        const native = await snapshot();
        return (
          !installationFailed &&
          !native.failed &&
          !native.retired &&
          native.pending === 0 &&
          native.current &&
          native.not_busy &&
          native.calls.every((call: any) => call.settled) &&
          native.ownerCalls.every((call: any) => call.settled)
        );
      })
      .toBe(true);
  }
  return {
    async setNextExecution(
      execution: string,
      artifact: typeof data.artifact_ref,
    ) {
      expect(data.mode === "return-and-accept" && !replaced).toBe(true);
      const native = await snapshot();
      expect(native.retired && !native.failed && native.pending === 0).toBe(
        true,
      );
      expect(
        execution !== data.execution_id &&
          artifact.project_id === data.project_id &&
          artifact.artifact_id !== data.artifact_ref.artifact_id,
      ).toBe(true);
      data = { ...data, execution_id: execution, artifact_ref: artifact };
      transferTarget = "done";
      replaced = true;
    },
    async complete(response: Response): Promise<unknown> {
      const request = response.request(),
        url = new URL(response.url());
      const task = `/api/v1/projects/${data.project_id}/tasks/${data.task_id}`;
      const execution = `${task}/executions/${data.execution_id}`;
      const allowed =
        request.method() === "POST"
          ? url.pathname === `${task}/transfer`
          : request.method() === "GET" &&
            [
              task,
              `${task}/executions`,
              execution,
              `${execution}/runtime-view`,
              `/api/v1/projects/${data.project_id}/agents`,
              `/api/v1/projects/${data.project_id}/artifacts`,
              `/api/v1/projects/${data.project_id}/artifacts/${data.artifact_ref.artifact_id}/files/${data.artifact_ref.file_id}/content`,
            ].includes(url.pathname);
      expect(allowed && url.origin === data.origin).toBe(true);
      expect(response.status()).toBe(200);
      await expect.poll(() => terminals.has(request)).toBe(true);
      const failure = request.failure()?.errorText;
      expect(
        terminals.get(request) === "finished" ||
          (terminals.get(request) === "failed" &&
            failure === "net::ERR_ABORTED"),
      ).toBe(true);
      const rid = await response.headerValue("x-request-id");
      expect(rid).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      );
      await quiet();
      const native = await snapshot();
      const rows = native.rows.filter((row: any) => row.request_id === rid);
      expect(rows).toHaveLength(1);
      const row = rows[0];
      expect(row).toMatchObject({
        method: request.method(),
        path: url.pathname,
        status: 200,
        fetch_returned: true,
        fetch_rejected: false,
        readers: 1,
        eof: true,
        closed: true,
        release: 1,
        reader_cancel: 0,
        stream_cancel: 0,
        aborted: false,
        error: false,
      });
      expect(row.reads).toBeGreaterThan(0);
      expect(row.reads).toBe(row.read_returns);
      expect(row.length).toMatch(/^(0|[1-9][0-9]*)$/);
      expect(Number(row.length)).toBe(row.bytes);
      expect(row.digest).toMatch(/^[a-f0-9]{64}$/);
      expect(Number(await response.headerValue("content-length"))).toBe(
        row.bytes,
      );
      expect(await response.headerValue("cache-control")).toBe("no-store");
      const record = JSON.parse(
        readFileSync(join(evidence, `m2-response-${rid}.json`), "utf8"),
      );
      const { value, ...go } = record;
      expect(go).toEqual({
        request_id: rid,
        method: request.method(),
        url_sha256: digest(url.pathname + url.search),
        status: 200,
        content_length: row.bytes,
        body_sha256: row.digest,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
        request_host_matches: true,
      });
      const calls =
        row.call_id === null
          ? native.ownerCalls.filter(
              (call: any) => call.native_row_id === row.id,
            )
          : native.calls.filter((call: any) => call.id === row.call_id);
      expect(calls).toHaveLength(1);
      const call = calls[0];
      expect(call).toMatchObject({
        settled: true,
        fulfilled: true,
        rejected: false,
        current: true,
        not_busy: true,
      });
      const published =
        call.kind === "work.execution"
          ? value.execution
          : call.kind === "work.artifacts"
            ? { ...value, next_cursor: value.next_cursor ?? null }
            : call.kind === "work.artifactContent"
              ? {
                  ...value,
                  text: value.text ?? "",
                  invalid_utf8: value.invalid_utf8 === true,
                }
              : value;
      expect(call.typed).toEqual(published);
      // The native reader has not installed a typed DTO transform for Artifact.
      // Bind its untouched wire JSON and then the actual public parsed value.
      expect(row.typed_digest).toBe(
        digest(
          JSON.stringify(canonical(row.call_id === null ? value : published)),
        ),
      );
      if (row.call_id !== null) {
        expect(call.bound).toBe(1);
        expect(call.typed_digest).toBe(row.typed_digest);
      } else expect(row.call_id).toBeNull();
      const wireProof = await wire.headers({
        method: request.method(),
        url: response.url(),
        status: 200,
        requestID: rid!,
      });
      proofs.push({
        request_id: rid,
        terminal:
          failure === "net::ERR_ABORTED"
            ? "requestfailed/net::ERR_ABORTED"
            : "requestfinished",
        body_sha256: row.digest,
        typed_sha256: digest(JSON.stringify(canonical(published))),
        bytes: row.bytes,
        native_eof: true,
        native_cancel: false,
        original_consumer_fulfilled: true,
        go,
        wire: wireProof,
      });
      return value;
    },
    async endDocument() {
      await quiet();
      await page.evaluate(() => {
        const host = window as any;
        host.__m2OwnerConsumer.finish();
        const proof = host.__taskReviewProof.finish();
        if (!proof.retired || proof.pending || proof.failed)
          throw Error("M2_NATIVE_RETIRE_FAILED");
      });
    },
    async close() {
      if (closed) return;
      try {
        for (const response of responses) {
          const rid = await response.headerValue("x-request-id");
          if (!rid) throw Error("M2_WIRE_REQUEST_ID_MISSING");
          await wire.headers({
            method: response.request().method(),
            url: response.url(),
            status: response.status(),
            requestID: rid,
          });
        }
        wire.complete();
      } finally {
        await page.unroute(entryURL, install);
        page.off("response", onResponse);
        page.off("requestfinished", onFinished);
        page.off("requestfailed", onFailed);
        await wire.close();
        closed = true;
        expect(handlers).toBe(0);
      }
    },
    save() {
      writeFileSync(
        join(evidence, "m2-complete-response-proofs.json"),
        JSON.stringify(
          {
            closed,
            installation_failed: installationFailed,
            proofs,
            wire: wire.snapshot(),
          },
          null,
          2,
        ) + "\n",
        { mode: 0o600 },
      );
    },
  };
}
