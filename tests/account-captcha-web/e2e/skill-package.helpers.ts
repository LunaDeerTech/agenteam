import {
  expect,
  type Page,
  type Request,
  type Response,
  type Download,
} from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { observeBrowserIO } from "./task-review.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";

const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const digest = (body: Buffer | string) =>
  createHash("sha256").update(body).digest("hex");
export type PackageMaterial = {
  mode: "download";
  cookie: string;
  project_id: string;
  route: string;
  input_hash: string;
  revision: string;
  byte_size: number;
  sha256: string;
  skill: {
    id: string;
    project_id: string;
    name: string;
    normalized_name: string;
    description: string;
    protected: boolean;
    current_revision: string;
    version: string;
  };
};
export function packageMaterial(): PackageMaterial {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "skill-package-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("download");
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_SKILL_PACKAGE_WEB_INPUT_HASH,
  );
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.project_id).toMatch(uuid);
  expect(data.skill.id).toMatch(uuid);
  expect(data.skill.project_id).toBe(data.project_id);
  expect(data.revision).toMatch(/^[1-9][0-9]{0,18}$/);
  expect(BigInt(data.revision)).toBeLessThanOrEqual(9223372036854775807n);
  expect(data.skill.current_revision).toBe(data.revision);
  expect(data.sha256).toMatch(/^[0-9a-f]{64}$/);
  expect(Number.isSafeInteger(data.byte_size)).toBe(true);
  expect(data.byte_size).toBeGreaterThan(0);
  expect(data.byte_size).toBeLessThanOrEqual(33832982);
  expect(data.route).toMatch(/^\/[a-z0-9-]+\/[a-z0-9-]+\/settings\/skills$/);
  expect(typeof data.cookie).toBe("string");
  expect(data.cookie.length).toBeGreaterThan(0);
  return data;
}

type Entry = {
  request: Request;
  name: string;
  response?: Response;
  terminal?: "finished" | "failed" | "closed";
  responses: number;
  finished: number;
  failed: number;
  checked: boolean;
  requestID?: string;
  headersJoined?: Promise<void>;
  headersAccepted?: boolean;
  length?: number;
  sha256?: string;
  failure?: string;
  native?: Record<string, unknown>;
};
// Reuse the original JSON-reader/public-consumer observers for metadata only.
// They preserve each original call and Promise; binary bytes and terminals are
// never replaced. A failed original GET remains failed in the evidence.
export async function observePackage(page: Page, data: PackageMaterial) {
  const entries: Entry[] = [];
  const evidence = process.env.AGENTEAM_SKILL_PACKAGE_WEB_EVIDENCE!;
  const prefix = `/api/v1/projects/${data.project_id}`;
  const packagePath = `${prefix}/skills/${data.skill.id}/revisions/${data.revision}/package`;
  let failure = "",
    stage = "installed",
    stopped = false;
  let browserFile: { length: number; sha256: string } | undefined;
  let nativeRetired = false,
    nativePending: number | undefined;
  const metadataKinds: Record<string, string> = {
    session: "restore",
    resolve: "project.resolve",
    projects: "project.list",
    project: "project.get",
    catalog: "skills.list",
    detail: "skills.get",
  };
  const binding = await knowledgeSessionBinding(
    join(import.meta.dirname, "../../.."),
    process.env.AGENTEAM_SKILL_PACKAGE_WEB_DIST!,
  );
  await observeBrowserIO(page, () => {}, {
    project: data.project_id,
    skill: data.skill.id,
  });
  const entryURL = new URL(binding.entry, process.env.AGENTEAM_AUTH_WEB_ORIGIN!)
    .href;
  let installing = 0;
  const install = async (route: import("@playwright/test").Route) => {
    installing++;
    try {
      await page.evaluate(
        async ({ binding, project, skill }) => {
          const module = await import(/* @vite-ignore */ binding.asset);
          const useSession = module[binding.exportName];
          if (typeof useSession !== "function")
            throw Error("SKILL_NATIVE_SINGLETON");
          const auth = useSession();
          if (useSession() !== auth)
            throw Error("SKILL_NATIVE_SINGLETON_IDENTITY");
          (window as any).__taskReviewProof.consumers(
            auth,
            project,
            false,
            false,
            undefined,
            undefined,
            undefined,
            undefined,
            undefined,
            skill,
            false,
            false,
            false,
          );
        },
        { binding, project: data.project_id, skill: data.skill.id },
      );
      await route.continue();
    } catch {
      fail("NATIVE_INSTALL");
      await route.abort();
    } finally {
      installing--;
    }
  };
  await page.route(entryURL, install);
  const fail = (code: string) => {
    failure ||= code;
  };
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) return;
    expect(url.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
    expect(request.method()).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (globals[url.pathname]) {
      if (globals[url.pathname] === "resolve") {
        const [, username, project] = data.route.split("/");
        expect([...url.searchParams.entries()]).toEqual([
          ["username", username],
          ["project_name", project],
        ]);
      } else if (globals[url.pathname] === "projects") {
        expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      } else expect(url.search).toBe("");
      return globals[url.pathname];
    }
    if (url.pathname === prefix && !url.search) return "project";
    if (url.pathname === `${prefix}/skills/catalog`) {
      expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      return "catalog";
    }
    if (url.pathname === `${prefix}/skills/${data.skill.id}` && !url.search)
      return "detail";
    if (url.pathname === packagePath && !url.search) return "package";
    throw Error("UNOBSERVED_SKILL_PACKAGE_ENDPOINT");
  };
  const request = (request: Request) => {
    try {
      const name = classify(request);
      if (name)
        entries.push({
          request,
          name,
          responses: 0,
          finished: 0,
          failed: 0,
          checked: false,
        });
    } catch {
      fail("REQUEST_SCOPE");
    }
  };
  const response = (response: Response) => {
    const entry = entries.find((e) => e.request === response.request());
    if (!entry) return;
    entry.response = response;
    if (++entry.responses !== 1) fail("DUPLICATE_RESPONSE");
    entry.headersJoined = Promise.allSettled([
      entry.request.allHeaders(),
      response.allHeaders(),
    ])
      .then(([sent, received]) => {
        if (sent.status !== "fulfilled" || received.status !== "fulfilled")
          throw Error();
        const origin = new URL(entry.request.url());
        expect(Boolean(sent.value.cookie)).toBe(true);
        if (Object.hasOwn(sent.value, "host"))
          expect(sent.value.host).toBe(origin.host);
        if (Object.hasOwn(sent.value, ":authority"))
          expect(sent.value[":authority"]).toBe(origin.host);
        expect(received.value["x-request-id"]).toMatch(uuid);
        entry.requestID = received.value["x-request-id"];
        if (entry.name === "package") {
          for (const field of [
            "range",
            "if-range",
            "if-none-match",
            "if-modified-since",
            "content-encoding",
          ])
            expect(sent.value[field]).toBeUndefined();
          expect(received.value["content-type"]).toBe("application/zip");
          expect(received.value["content-disposition"]).toBe(
            'attachment; filename="skill-package.zip"',
          );
          expect(received.value["cache-control"]).toBe("no-store");
          expect(received.value["x-content-type-options"]).toBe("nosniff");
          expect(received.value["content-encoding"]).toBeUndefined();
          expect(received.value["content-range"]).toBeUndefined();
          expect(received.value["content-length"]).toBe(String(data.byte_size));
        }
        entry.headersAccepted = true;
      })
      .catch(() => {
        fail("ORIGINAL_HEADERS");
      });
  };
  const end = (request: Request, terminal: "finished" | "failed") => {
    const entry = entries.find((e) => e.request === request);
    if (!entry) return;
    if (entry.terminal) fail("DUPLICATE_TERMINAL");
    entry.terminal = terminal;
    entry[terminal]++;
    if (terminal === "failed") entry.failure = request.failure()?.errorText;
  };
  const finished = (r: Request) => end(r, "finished"),
    failed = (r: Request) => end(r, "failed");
  const closed = () => {
    for (const e of entries) if (!e.terminal) e.terminal = "closed";
  };
  page.on("request", request);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  page.on("close", closed);
  async function checkpoint() {
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as any).__taskReviewProof.snapshot().pending,
        ),
      )
      .toBe(0);
    const proof = await page.evaluate(() =>
      (window as any).__taskReviewProof.snapshot(),
    );
    expect(proof.failed).toBe(false);
    expect(proof.pending).toBe(0);
    expect(proof.current).toBe(true);
    expect(proof.not_busy).toBe(true);
    return proof;
  }
  function originalMetadata(e: Entry, proof: any, record: any) {
    expect(Object.hasOwn(metadataKinds, e.name)).toBe(true);
    const matches = proof.rows.filter((n: any) => n.request_id === e.requestID);
    expect(matches).toHaveLength(1);
    const n = matches[0],
      calls = proof.calls.filter((c: any) => c.id === n.call_id);
    expect(calls).toHaveLength(1);
    const c = calls[0];
    expect(n.path).toBe(new URL(e.request.url()).pathname);
    expect(n.method).toBe("GET");
    expect(n.status).toBe(200);
    expect(n.fetch_returned).toBe(true);
    expect(n.fetch_rejected || n.error || n.aborted).toBe(false);
    expect(n.readers).toBe(1);
    expect(n.reads).toBeGreaterThan(0);
    expect(n.read_returns).toBe(n.reads);
    expect(n.eof && n.closed).toBe(true);
    expect(n.release).toBe(1);
    expect(n.reader_cancel).toBe(0);
    expect(n.stream_cancel).toBe(0);
    expect(n.length).toMatch(/^(0|[1-9][0-9]*)$/);
    expect(Number(n.length)).toBe(n.bytes);
    expect(n.bytes).toBeLessThanOrEqual(2 << 20);
    expect(record.content_length).toBe(n.bytes);
    expect(n.digest).toMatch(/^[0-9a-f]{64}$/);
    expect(record.body_sha256).toBe(n.digest);
    expect(c.bound).toBe(1);
    expect(c.method).toBe("GET");
    expect(c.path).toBe(n.path);
    expect(c.kind).toBe(metadataKinds[e.name]);
    expect(c.fulfilled && c.settled && c.current && c.not_busy).toBe(true);
    expect(c.rejected).toBe(false);
    expect(c.typed).not.toBeNull();
    expect(c.typed_digest).toMatch(/^[0-9a-f]{64}$/);
    expect(n.typed_digest).toBe(c.typed_digest);
    // The original safe predicates/digests remain reviewable; public DTO values
    // (especially Session fields) stay private and never enter the evidence.
    e.native = {
      reader: { ...n },
      consumer: { ...c, typed: undefined },
      pending: proof.pending,
      current: proof.current,
      not_busy: proof.not_busy,
    };
    return {
      length: n.bytes as number,
      sha256: n.digest as string,
      typed: c.typed,
    };
  }
  async function verify() {
    stage = "original_terminals";
    await expect.poll(() => entries.filter((e) => !e.terminal).length).toBe(0);
    await Promise.all(entries.map((e) => e.headersJoined));
    expect(failure).toBe("");
    const proof = await checkpoint();
    for (const e of entries.filter((e) => !e.checked)) {
      expect(e.responses).toBe(1);
      expect(e.headersAccepted).toBe(true);
      const r = e.response!;
      expect(r.status()).toBe(200);
      const record = JSON.parse(
        readFileSync(
          join(evidence, `skill-package-download-response-${e.requestID}.json`),
          "utf8",
        ),
      );
      const url = new URL(e.request.url());
      expect(record).toMatchObject({
        input_hash: data.input_hash,
        request_id: e.requestID,
        method: "GET",
        url_sha256: digest(url.pathname + url.search),
        status: 200,
        request_host_matches: true,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
      });
      if (e.terminal === "failed") {
        stage = "original_failed_metadata";
        expect(Object.hasOwn(metadataKinds, e.name)).toBe(true);
        expect(e.failure).toBe("net::ERR_ABORTED");
        expect(e.finished).toBe(0);
        expect(e.failed).toBe(1);
        const actual = originalMetadata(e, proof, record);
        e.length = actual.length;
        e.sha256 = actual.sha256;
        checkMetadata(e, actual.typed);
        e.checked = true;
        continue;
      }
      expect(e.terminal).toBe("finished");
      expect(e.finished).toBe(1);
      expect(e.failed).toBe(0);
      let timer: ReturnType<typeof setTimeout> | undefined;
      try {
        expect(
          await Promise.race([
            r.finished(),
            new Promise((_, reject) => {
              timer = setTimeout(
                () => reject(Error("ORIGINAL_FINISHED_TIMEOUT")),
                2000,
              );
            }),
          ]),
        ).toBeNull();
      } finally {
        if (timer) clearTimeout(timer);
      }
      stage = "original_body";
      const bytes = await r.body();
      expect(bytes.length).toBeLessThanOrEqual(
        e.name === "package" ? 33832982 : 2 << 20,
      );
      e.length = bytes.length;
      e.sha256 = digest(bytes);
      expect(r.headers()["content-length"]).toBe(String(bytes.length));
      stage = "handler_binding";
      expect(record).toMatchObject({
        input_hash: data.input_hash,
        request_id: e.requestID,
        method: "GET",
        url_sha256: digest(url.pathname + url.search),
        status: 200,
        content_length: bytes.length,
        body_sha256: e.sha256,
        request_host_matches: true,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
      });
      if (Object.hasOwn(metadataKinds, e.name)) {
        const actual = originalMetadata(e, proof, record);
        expect(actual.length).toBe(bytes.length);
        expect(actual.sha256).toBe(e.sha256);
      }
      if (e.name === "package") {
        expect(bytes.length).toBe(data.byte_size);
        expect(e.sha256).toBe(data.sha256);
        expect(record).toMatchObject({
          content_type: "application/zip",
          content_disposition: 'attachment; filename="skill-package.zip"',
          cache_control: "no-store",
          nosniff: true,
          package_verified: true,
        });
      } else checkMetadata(e, JSON.parse(bytes.toString("utf8")));
      e.checked = true;
    }
    stage = "verified";
  }
  function checkMetadata(e: Entry, value: any) {
    if (e.name === "catalog") {
      expect(value.items).toHaveLength(2);
      expect(value.items).toContainEqual(data.skill);
      expect(value.next_cursor).toBeUndefined();
    } else if (e.name === "detail") expect(value).toEqual(data.skill);
  }
  return {
    verify,
    async downloaded(download: Download) {
      stage = "browser_file";
      try {
        expect(download.suggestedFilename()).toBe("skill-package.zip");
        expect(download.url()).toMatch(/^blob:/);
        expect(await download.failure()).toBeNull();
        const path = await download.path();
        expect(path).not.toBeNull();
        const bytes = readFileSync(path!);
        expect(bytes.length).toBe(data.byte_size);
        expect(digest(bytes)).toBe(data.sha256);
        browserFile = { length: bytes.length, sha256: digest(bytes) };
      } finally {
        await download.delete();
      }
    },
    async retire() {
      if (stopped) return;
      try {
        if (!page.isClosed()) {
          await expect.poll(() => installing).toBe(0);
          await expect
            .poll(() =>
              page.evaluate(
                () => (window as any).__taskReviewProof.snapshot().pending,
              ),
            )
            .toBe(0);
          const retired = await page.evaluate(() =>
            (window as any).__taskReviewProof.finish(),
          );
          nativePending = retired.pending;
          nativeRetired = retired.retired;
          expect(retired.pending).toBe(0);
          expect(retired.failed).toBe(false);
          expect(retired.retired).toBe(true);
          await page.unroute(entryURL, install);
          await page.goto("about:blank");
        }
        await expect
          .poll(() => entries.filter((e) => !e.terminal).length)
          .toBe(0);
        await Promise.all(entries.map((e) => e.headersJoined));
      } finally {
        page.off("request", request);
        page.off("response", response);
        page.off("requestfinished", finished);
        page.off("requestfailed", failed);
        page.off("close", closed);
        stopped = true;
      }
    },
    complete() {
      expect(failure).toBe("");
      expect(stopped).toBe(true);
      expect(nativeRetired).toBe(true);
      expect(nativePending).toBe(0);
      expect(entries.every((e) => e.checked)).toBe(true);
      for (const name of ["catalog", "detail", "package"])
        expect(entries.filter((e) => e.name === name)).toHaveLength(1);
      const pkg = entries.find((e) => e.name === "package")!;
      expect(browserFile).toEqual({ length: pkg.length, sha256: pkg.sha256 });
      writeFileSync(
        join(
          process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
          "skill-package-result.json",
        ),
        JSON.stringify({
          input_hash: data.input_hash,
          completed: true,
          one_catalog: true,
          one_get: true,
          one_package: true,
          keyboard_focus: true,
          themes_layout: true,
          original_consumed: true,
          typed_published: true,
          observers_joined: true,
          responses: entries.length,
          download_length: pkg.length,
          download_sha256: pkg.sha256,
          download_request_id: pkg.requestID,
        }),
        { mode: 0o600 },
      );
    },
    save(status: string | undefined) {
      writeFileSync(
        join(evidence, "skill-package-download.json"),
        JSON.stringify(
          {
            input_hash: data.input_hash,
            status,
            stage,
            failure: failure || undefined,
            observers_removed: stopped,
            native_retired: nativeRetired,
            native_pending: nativePending,
            browser_file_verified: browserFile !== undefined,
            all_observed: entries.length > 0 && entries.every((e) => e.checked),
            requests: entries.map((e) => ({
              name: e.name,
              request_id: e.requestID,
              status: e.response?.status(),
              terminal: e.terminal,
              responses: e.responses,
              finished: e.finished,
              failed: e.failed,
              failure: e.failure,
              native: e.native,
              headers_joined: e.headersAccepted === true,
              checked: e.checked,
              length: e.length,
              sha256: e.sha256,
            })),
          },
          null,
          2,
        ),
        { mode: 0o600 },
      );
    },
  };
}

export async function packageScreenshot(page: Page, name: string) {
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect
    .poll(() =>
      brand.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_SKILL_PACKAGE_WEB_EVIDENCE!,
      `skill-package-${name}.png`,
    ),
    fullPage: true,
    animations: "disabled",
  });
}
