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
export async function observeM1Bodies(
  page: Page,
  data: {
    origin: string;
    project_id: string;
    task_id: string;
    execution_id: string;
  },
) {
  const evidence = process.env.AGENTEAM_M1_WEB_EVIDENCE!;
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_M1_WEB_DIST!,
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
        async ({ binding, project, task }) => {
          const host = window as any;
          const module = await import(/* @vite-ignore */ binding.asset);
          const use = module[binding.exportName];
          if (typeof use !== "function" || use() !== use())
            throw Error("M1_SESSION_SINGLETON");
          const auth = use(),
            proof = host.__taskReviewProof;
          if (!proof) throw Error("M1_NATIVE_OBSERVER_MISSING");
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
          // The shared reader observer already covers transfer bytes. Observe the
          // one existing public Owner method without changing its returned Promise.
          const descriptor = Object.getOwnPropertyDescriptor(
            auth.workReview,
            "startTransfer",
          )!;
          const original = auth.workReview.startTransfer;
          const calls: any[] = [];
          const wrapper = function (this: unknown, ...args: any[]) {
            if (
              calls.length ||
              args.length !== 3 ||
              args[0] !== project ||
              args[1] !== task ||
              args[2]?.request?.target_state !== "in_review"
            )
              throw Error("M1_OWNER_TRANSFER_SCOPE");
            const call: any = {
              settled: false,
              fulfilled: false,
              rejected: false,
              current: false,
              not_busy: false,
            };
            calls.push(call);
            const pending = Reflect.apply(original, this, args);
            void pending.then(
              (value: unknown) => {
                call.typed = value;
                call.fulfilled = true;
                call.current =
                  auth.state.phase === "authenticated" &&
                  auth.personalContext.phase === "current";
                call.not_busy = auth.state.busy === false;
                call.settled = true;
              },
              () => {
                call.rejected = call.settled = true;
              },
            );
            return pending;
          };
          Object.defineProperty(auth.workReview, "startTransfer", {
            ...descriptor,
            value: wrapper,
          });
          host.__m1OwnerConsumer = {
            calls,
            finish() {
              if (
                calls.some((call) => !call.settled) ||
                auth.workReview.startTransfer !== wrapper
              )
                throw Error("M1_OWNER_OBSERVATION_PENDING");
              Object.defineProperty(
                auth.workReview,
                "startTransfer",
                descriptor,
              );
            },
          };
        },
        { binding, project: data.project_id, task: data.task_id },
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
      if (!host.__taskReviewProof || !host.__m1OwnerConsumer)
        throw Error("M1_NATIVE_PROOF_MISSING");
      return {
        ...host.__taskReviewProof.snapshot(),
        ownerCalls: host.__m1OwnerConsumer.calls,
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
        readFileSync(join(evidence, `m1-response-${rid}.json`), "utf8"),
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
        request.method() === "POST"
          ? native.ownerCalls
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
        call.kind === "work.execution" ? value.execution : value;
      expect(call.typed).toEqual(published);
      expect(row.typed_digest).toBe(
        digest(JSON.stringify(canonical(published))),
      );
      if (request.method() === "GET") {
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
        typed_sha256: row.typed_digest,
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
        host.__m1OwnerConsumer.finish();
        const proof = host.__taskReviewProof.finish();
        if (!proof.retired || proof.pending || proof.failed)
          throw Error("M1_NATIVE_RETIRE_FAILED");
      });
    },
    async close() {
      if (closed) return;
      try {
        for (const response of responses) {
          const rid = await response.headerValue("x-request-id");
          if (!rid) throw Error("M1_WIRE_REQUEST_ID_MISSING");
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
        join(evidence, "m1-complete-response-proofs.json"),
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
