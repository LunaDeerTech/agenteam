import {
  expect,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeBrowserIO } from "./task-review.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";

export const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export const hash = (value: Buffer | string) =>
  createHash("sha256").update(value).digest("hex");
export type ImportMaterial = {
  mode: "existing-file-import-and-fixed-revision-download";
  cookie: string;
  owner_id: string;
  project_id: string;
  route: string;
  input_hash: string;
  filename: "SKILL.md";
  file_size: number;
  file_sha256: string;
};
type Entry = {
  request: Request;
  response?: Response;
  terminal?: "finished" | "failed";
  responses: number;
  headers?: Promise<void>;
  requestID?: string;
  checked: boolean;
};

// Reuse the original fetch/read/cancel instrumentation. The import phase has
// only JSON responses; binary download uses the existing fixed-revision helper
// in a fresh page. Neither observer clones or replaces the original stream.
export async function observeImport(page: Page, data: ImportMaterial) {
  const evidence = process.env.AGENTEAM_SKILL_IMPORT_WEB_EVIDENCE!;
  const prefix = `/api/v1/projects/${data.project_id}`;
  const entries: Entry[] = [];
  let failure = "",
    retired = false,
    installing = 0;
  let original:
    | {
        requestID: string;
        receipt: { skill_id: string; revision: string; version: string };
      }
    | undefined;
  let lastPublic: Record<string, any> | null = null;
  let lastNative: Record<string, any>[] = [];
  const fail = (code: string) => {
    failure ||= code;
  };
  const classify = (request: Request) => {
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) return false;
    expect(url.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
    const globals = [
      "/api/v1/session",
      "/api/v1/me",
      "/api/v1/me/preferences",
      "/api/v1/auth/bootstrap",
      "/api/v1/projects",
      "/api/v1/projects/resolve",
    ];
    if (request.method() === "POST") {
      expect(url.pathname).toBe(prefix + "/skills");
      expect(url.search).toBe("");
    } else {
      expect(request.method()).toBe("GET");
      expect(request.postDataBuffer()).toBeNull();
      expect(
        globals.includes(url.pathname) ||
          url.pathname === prefix ||
          url.pathname === prefix + "/skills/catalog",
      ).toBe(true);
    }
    return true;
  };
  const request = (r: Request) => {
    try {
      if (classify(r))
        entries.push({ request: r, responses: 0, checked: false });
    } catch {
      fail("REQUEST_SCOPE");
    }
  };
  const response = (r: Response) => {
    const e = entries.find((e) => e.request === r.request());
    if (!e) return;
    e.response = r;
    if (++e.responses !== 1) fail("DUPLICATE_RESPONSE");
    e.headers = Promise.all([e.request.allHeaders(), r.allHeaders()])
      .then(([sent, received]) => {
        expect(sent.cookie).toBeTruthy();
        const host = new URL(r.url()).host;
        if (Object.hasOwn(sent, "host")) expect(sent.host).toBe(host);
        if (Object.hasOwn(sent, ":authority"))
          expect(sent[":authority"]).toBe(host);
        expect(received["x-request-id"]).toMatch(uuid);
        expect(received["cache-control"]).toBe("no-store");
        e.requestID = received["x-request-id"];
      })
      .catch(() => fail("HEADERS"));
  };
  const end = (r: Request, terminal: "finished" | "failed") => {
    const e = entries.find((e) => e.request === r);
    if (!e) return;
    if (e.terminal) fail("DUPLICATE_TERMINAL");
    e.terminal = terminal;
  };
  const finished = (r: Request) => end(r, "finished"),
    failed = (r: Request) => end(r, "failed");
  page.on("request", request);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  await observeBrowserIO(page, () => {});
  const binding = await knowledgeSessionBinding(
    join(import.meta.dirname, "../../.."),
    process.env.AGENTEAM_SKILL_IMPORT_WEB_DIST!,
  );
  const entryURL = new URL(binding.entry, process.env.AGENTEAM_AUTH_WEB_ORIGIN!)
    .href;
  const install = async (route: import("@playwright/test").Route) => {
    installing++;
    try {
      await page.evaluate(
        async ({ binding, project, owner }) => {
          const module = await import(/* @vite-ignore */ binding.asset);
          const useSession = module[binding.exportName];
          if (typeof useSession !== "function")
            throw Error("IMPORT_SESSION_MISSING");
          const auth = useSession();
          if (useSession() !== auth)
            throw Error("IMPORT_SESSION_NOT_SINGLETON");
          (window as any).__taskReviewProof.consumers(
            auth,
            project,
            false,
            false,
          );
          // Observe the actual public install promise without replacing its
          // result, issuing another request, or reading another response body.
          const install = auth.skillImport.start;
          const state = {
            calls: 0,
            pending: 0,
            returned: false,
            rejected: false,
            failed: false,
            current: false,
            retired: false,
            project_id: "",
            receipt: null as null | Record<string, string>,
          };
          const tracked = function (
            this: unknown,
            projectID: string,
            text: string,
          ) {
            state.calls++;
            state.pending++;
            state.project_id = projectID;
            if (
              state.retired ||
              state.calls !== 1 ||
              projectID !== project ||
              typeof text !== "string"
            )
              state.failed = true;
            let result: Promise<unknown>;
            try {
              result = Reflect.apply(install, this, [projectID, text]);
            } catch (error) {
              state.pending--;
              state.returned = true;
              state.rejected = true;
              throw error;
            }
            void Promise.resolve(result).then(
              (value: any) => {
                try {
                  state.returned = true;
                  if (
                    !value ||
                    Object.keys(value).sort().join() !==
                      "revision,skill_id,version" ||
                    typeof value.skill_id !== "string" ||
                    value.revision !== "1" ||
                    value.version !== "1"
                  ) {
                    state.failed = true;
                  } else {
                    state.receipt = {
                      skill_id: value.skill_id,
                      revision: value.revision,
                      version: value.version,
                    };
                  }
                  const progress = auth.skillImport.progress;
                  state.current =
                    auth.state.phase === "authenticated" &&
                    auth.state.user?.id === owner &&
                    auth.personalContext.phase === "current" &&
                    progress?.contextValid === true &&
                    progress.projectID === project &&
                    progress.phase === "confirmed" &&
                    progress.skillID === value?.skill_id;
                } catch {
                  state.failed = true;
                } finally {
                  state.pending--;
                }
              },
              () => {
                state.returned = true;
                state.rejected = true;
                state.pending--;
              },
            );
            return result;
          };
          auth.skillImport.start = tracked;
          (window as any).__skillImportOriginal = {
            snapshot: () => ({
              ...state,
              receipt: state.receipt ? { ...state.receipt } : null,
            }),
            finish() {
              if (state.pending !== 0 || auth.skillImport.start !== tracked)
                state.failed = true;
              auth.skillImport.start = install;
              state.retired = true;
              return {
                pending: state.pending,
                failed: state.failed,
                retired: state.retired,
              };
            },
          };
        },
        { binding, project: data.project_id, owner: data.owner_id },
      );
      await route.continue();
    } catch {
      fail("SESSION_OBSERVATION");
      await route.abort();
    } finally {
      installing--;
    }
  };
  await page.route(entryURL, install);
  async function verify() {
    await expect.poll(() => entries.filter((e) => !e.terminal).length).toBe(0);
    await Promise.all(entries.map((e) => e.headers));
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as any).__taskReviewProof.snapshot().pending,
        ),
      )
      .toBe(0);
    const native = await page.evaluate(() =>
      (window as any).__taskReviewProof.snapshot(),
    );
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as any).__skillImportOriginal.snapshot().pending,
        ),
      )
      .toBe(0);
    lastPublic = await page.evaluate(() =>
      (window as any).__skillImportOriginal.snapshot(),
    );
    lastNative = native.rows.map((row: any) => ({
      request_id: row.request_id,
      fetch_returned: row.fetch_returned,
      fetch_rejected: row.fetch_rejected,
      error: row.error,
      aborted: row.aborted,
      readers: row.readers,
      reads: row.reads,
      read_returns: row.read_returns,
      eof: row.eof,
      closed: row.closed,
      release: row.release,
      reader_cancel: row.reader_cancel,
      stream_cancel: row.stream_cancel,
      bytes: row.bytes,
      digest: row.digest,
    }));
    expect(lastPublic?.failed).toBe(false);
    expect(native.failed).toBe(false);
    expect(native.current).toBe(true);
    expect(native.not_busy).toBe(true);
    expect(failure).toBe("");
    for (const e of entries.filter((e) => !e.checked)) {
      expect(e.responses).toBe(1);
      expect(e.response!.status()).toBe(200);
      const path = join(evidence, `skill-import-response-${e.requestID}.json`);
      await expect.poll(() => existsSync(path)).toBe(true);
      const record = JSON.parse(readFileSync(path, "utf8"));
      const url = new URL(e.request.url());
      expect(record).toMatchObject({
        input_hash: data.input_hash,
        request_id: e.requestID,
        method: e.request.method(),
        url_sha256: hash(url.pathname + url.search),
        status: 200,
        request_host_matches: true,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
      });
      const rows = native.rows.filter((n: any) => n.request_id === e.requestID);
      expect(rows).toHaveLength(1);
      const row = rows[0];
      expect(row.fetch_returned).toBe(true);
      expect(row.fetch_rejected || row.error || row.aborted).toBe(false);
      expect(row.readers).toBe(1);
      expect(row.reads).toBeGreaterThan(0);
      expect(row.read_returns).toBe(row.reads);
      expect(row.eof && row.closed).toBe(true);
      expect(row.release).toBe(1);
      expect(row.reader_cancel).toBe(0);
      expect(row.stream_cancel).toBe(0);
      expect(record.content_length).toBe(row.bytes);
      expect(record.body_sha256).toBe(row.digest);
      const isInstall = e.request.method() === "POST";
      let receipt:
        { skill_id: string; revision: string; version: string } | undefined;
      if (isInstall) {
        // This sole write profile is not a generic exception for failed POSTs.
        expect(url.pathname).toBe(prefix + "/skills");
        expect(url.search).toBe("");
        expect(
          entries.filter((entry) => entry.request.method() === "POST"),
        ).toHaveLength(1);
        expect(lastPublic).toMatchObject({
          calls: 1,
          pending: 0,
          returned: true,
          rejected: false,
          failed: false,
          current: true,
          project_id: data.project_id,
        });
        receipt = lastPublic!.receipt;
        expect(Object.keys(receipt!).sort()).toEqual([
          "revision",
          "skill_id",
          "version",
        ]);
        expect(receipt!.skill_id).toMatch(uuid);
        expect(receipt!.revision).toBe("1");
        expect(receipt!.version).toBe("1");
        const disk = readFileSync(
          join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "SKILL.md"),
        );
        expect(disk.length).toBe(data.file_size);
        expect(hash(disk)).toBe(data.file_sha256);
        const text = new TextDecoder("utf-8", { fatal: true }).decode(disk);
        expect(e.request.postDataJSON()).toEqual({
          request: {
            skill_id: receipt!.skill_id,
            mode: "create",
            source: {
              kind: "text_files",
              files: [{ path: "SKILL.md", utf8_text: text }],
            },
          },
        });
        expect(e.request.headers()["idempotency-key"]).toMatch(
          /^[A-Za-z0-9._:/-]{1,128}$/,
        );
        expect(e.request.headers()["x-csrf-token"]).toMatch(
          /^[A-Za-z0-9_-]{43}$/,
        );
        expect(original).toBeUndefined();
      }
      if (e.terminal === "failed") {
        if (!isInstall) expect(e.request.method()).toBe("GET");
        expect(e.request.failure()?.errorText).toBe("net::ERR_ABORTED");
      } else {
        expect(e.terminal).toBe("finished");
        expect(await e.response!.finished()).toBeNull();
        const bytes = await e.response!.body();
        expect(bytes.length).toBe(row.bytes);
        expect(hash(bytes)).toBe(row.digest);
        if (isInstall)
          expect(JSON.parse(bytes.toString("utf8"))).toEqual(receipt);
      }
      if (isInstall) original = { requestID: e.requestID!, receipt: receipt! };
      e.checked = true;
    }
    return original?.receipt;
  }
  return {
    verify,
    posts: () => entries.filter((e) => e.request.method() === "POST").length,
    async retire() {
      if (retired) return;
      try {
        await expect.poll(() => installing).toBe(0);
        if (!page.isClosed()) {
          await expect
            .poll(() =>
              page.evaluate(
                () => (window as any).__taskReviewProof.snapshot().pending,
              ),
            )
            .toBe(0);
          const end = await page.evaluate(() =>
            (window as any).__taskReviewProof.finish(),
          );
          expect(end.failed).toBe(false);
          expect(end.retired).toBe(true);
          expect(end.pending).toBe(0);
          await expect
            .poll(() =>
              page.evaluate(
                () => (window as any).__skillImportOriginal.snapshot().pending,
              ),
            )
            .toBe(0);
          const publicEnd = await page.evaluate(() =>
            (window as any).__skillImportOriginal.finish(),
          );
          expect(publicEnd).toEqual({
            pending: 0,
            failed: false,
            retired: true,
          });
          await page.unroute(entryURL, install);
          await page.goto("about:blank");
        }
        await expect
          .poll(() => entries.filter((e) => !e.terminal).length)
          .toBe(0);
        await Promise.all(entries.map((e) => e.headers));
      } finally {
        page.off("request", request);
        page.off("response", response);
        page.off("requestfinished", finished);
        page.off("requestfailed", failed);
        retired = true;
      }
    },
    complete() {
      expect(retired).toBe(true);
      expect(failure).toBe("");
      expect(original).toBeDefined();
      expect(entries.every((e) => e.checked)).toBe(true);
      expect(entries.filter((e) => e.request.method() === "POST")).toHaveLength(
        1,
      );
      writeFileSync(
        join(
          process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
          "skill-import-result.json",
        ),
        JSON.stringify({
          completed: true,
          posts: 1,
          request_id: original!.requestID,
          observers_joined: true,
        }),
        { mode: 0o600, flag: "wx" },
      );
    },
    save(status: string | undefined) {
      writeFileSync(
        join(evidence, "skill-import-originals.json"),
        JSON.stringify({
          status,
          input_hash: data.input_hash,
          failure,
          observers_removed: retired,
          all_observed: entries.every((e) => e.checked),
          public_install: lastPublic,
          native_rows: lastNative,
          requests: entries.map((e) => ({
            method: e.request.method(),
            request_id: e.requestID,
            status: e.response?.status(),
            terminal: e.terminal,
            error_text:
              e.terminal === "failed"
                ? /^net::ERR_[A-Z_]+$/.test(
                    e.request.failure()?.errorText ?? "",
                  )
                  ? e.request.failure()!.errorText
                  : "OTHER"
                : null,
            responses: e.responses,
            checked: e.checked,
          })),
        }),
        { mode: 0o600 },
      );
    },
  };
}
