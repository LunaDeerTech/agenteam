import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeBrowserIO } from "./task-review.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const hash = (v: Buffer | string) =>
  createHash("sha256").update(v).digest("hex");
export type AgentSkillsMaterial = {
  mode: "assign-disable-enable-remove";
  cookie: string;
  owner_id: string;
  project_id: string;
  agent_id: string;
  skill_id: string;
  builtin: Record<string, any>;
  route: string;
  input_hash: string;
};
export function agentSkillsMaterial(): AgentSkillsMaterial {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "agent-skills-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("assign-disable-enable-remove");
  for (const id of [
    data.owner_id,
    data.project_id,
    data.agent_id,
    data.skill_id,
  ])
    expect(id).toMatch(uuid);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_AGENT_SKILLS_WEB_INPUT_HASH,
  );
  return data;
}
type Entry = {
  request: Request;
  kind: string;
  response?: Response;
  terminal?: "finished" | "failed";
  responses: number;
  headers?: Promise<void>;
  requestID?: string;
  checked: boolean;
  value?: Record<string, any>;
};
export async function observeAgentSkills(
  page: Page,
  data: AgentSkillsMaterial,
) {
  const evidence = process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!;
  const prefix = `/api/v1/projects/${data.project_id}`,
    base = `${prefix}/agents/${data.agent_id}`;
  const entries: Entry[] = [],
    keys = new Set<string>();
  let failure = "",
    installing = 0,
    retired = false,
    lastNative: any,
    lastPublic: any;
  const fail = (stage: string) => {
    failure ||= stage;
  };
  const classify = (r: Request): string | undefined => {
    const u = new URL(r.url()),
      p = u.pathname,
      m = r.method();
    if (!p.startsWith("/api/")) return;
    expect(u.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
    expect(entries.length).toBeLessThan(64);
    if (m === "POST") {
      expect(u.search).toBe("");
      expect([`${base}/skills/assign`, `${base}/skills/remove`]).toContain(p);
      const n = entries.filter((e) => e.request.method() === "POST").length;
      expect(n).toBeLessThan(4);
      const input = r.postDataJSON();
      expect(Object.keys(input).sort()).toEqual([
        "expected_version",
        "request",
      ]);
      expect(input.expected_version).toBe(String(n + 1));
      if (n < 3) {
        expect(p).toBe(`${base}/skills/assign`);
        expect(input.request).toEqual({
          skill_id: data.skill_id,
          enabled: n !== 1,
        });
      } else {
        expect(p).toBe(`${base}/skills/remove`);
        const prior = entries.filter((e) => e.kind === "assign")[2]!.value!;
        expect(input.request).toEqual({ assignment_id: prior.assignment.id });
      }
      const key = r.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      keys.add(key);
      expect(r.headers()["x-csrf-token"]).toMatch(/^[A-Za-z0-9_-]{43}$/);
      return n < 3 ? "assign" : "remove";
    }
    expect(m).toBe("GET");
    expect(r.postDataBuffer()).toBeNull();
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (globals[p]) return globals[p];
    if (p === prefix) return "project";
    if (p === base + "/runner-mounts") {
      expect(u.search).toBe("");
      return "mounts";
    }
    if (p === base + "/configuration") {
      expect(u.search).toBe("");
      return "configuration";
    }
    if (p === prefix + "/agents") {
      expect([...u.searchParams]).toEqual([["limit", "50"]]);
      return "directory";
    }
    if (p === base + "/skills" || p === prefix + "/skills/catalog") {
      expect([...u.searchParams]).toEqual([["limit", "25"]]);
      return p.endsWith("/catalog") ? "catalog" : "assignments";
    }
    throw Error("UNEXPECTED_AGENT_SKILLS_ENDPOINT");
  };
  const request = (r: Request) => {
    try {
      const kind = classify(r);
      if (kind)
        entries.push({ request: r, kind, responses: 0, checked: false });
    } catch {
      fail("REQUEST_SCOPE");
    }
  };
  const response = (r: Response) => {
    const e = entries.find((e) => e.request === r.request());
    if (!e) return;
    e.response = r;
    if (++e.responses !== 1) fail("RESPONSE_COUNT");
    e.headers = Promise.all([e.request.allHeaders(), r.allHeaders()])
      .then(([sent, received]) => {
        expect(sent.cookie).toBeTruthy();
        if (Object.hasOwn(sent, "host"))
          expect(sent.host).toBe(new URL(r.url()).host);
        if (Object.hasOwn(sent, ":authority"))
          expect(sent[":authority"]).toBe(new URL(r.url()).host);
        expect(received["x-request-id"]).toMatch(uuid);
        expect(received["cache-control"]).toBe("no-store");
        e.requestID = received["x-request-id"];
      })
      .catch(() => fail("HEADERS"));
  };
  const terminal = (r: Request, kind: "finished" | "failed") => {
    const e = entries.find((e) => e.request === r);
    if (!e) return;
    if (e.terminal) fail("TERMINAL_COUNT");
    e.terminal = kind;
  };
  const finished = (r: Request) => terminal(r, "finished"),
    failed = (r: Request) => terminal(r, "failed");
  page.on("request", request);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  await observeBrowserIO(page, () => {});
  const binding = await knowledgeSessionBinding(
    join(import.meta.dirname, "../../.."),
    process.env.AGENTEAM_AGENT_SKILLS_WEB_DIST!,
  );
  const entryURL = new URL(binding.entry, process.env.AGENTEAM_AUTH_WEB_ORIGIN!)
    .href;
  const install = async (route: Route) => {
    installing++;
    try {
      await page.evaluate(
        async ({ binding, project, agent, owner }) => {
          const module = await import(/* @vite-ignore */ binding.asset),
            useSession = module[binding.exportName];
          const auth = useSession();
          if (auth !== useSession()) throw Error("SKILLS_SINGLETON");
          (window as any).__taskReviewProof.consumers(
            auth,
            project,
            false,
            false,
          );
          const rows: any[] = [],
            restores: (() => void)[] = [];
          let pending = 0,
            failed = false,
            retired = false;
          const current = () =>
            auth.state.phase === "authenticated" &&
            auth.state.user?.id === owner &&
            auth.personalContext.phase === "current";
          const wrap = (object: any, method: string, kind: string) => {
            const original = object[method];
            const tracked = function (this: unknown, ...args: any[]) {
              const row: any = {
                kind,
                returned: false,
                rejected: false,
                current: false,
                receipt_identity: false,
                value: null,
              };
              if (
                retired ||
                rows.length >= 32 ||
                args[0] !== project ||
                (kind !== "catalog" &&
                  kind !== "directory" &&
                  args[1] !== agent)
              )
                failed = true;
              if (kind === "start") {
                row.command = JSON.parse(JSON.stringify(args[2]));
                row.sequence = args[3];
              }
              rows.push(row);
              pending++;
              let result: Promise<any>;
              try {
                result = Reflect.apply(original, this, args);
              } catch (error) {
                row.returned = row.rejected = true;
                pending--;
                throw error;
              }
              // Preserve the identical public Promise, original input and return value.
              void Promise.resolve(result).then(
                (value) => {
                  try {
                    row.returned = true;
                    row.current = current();
                    if (kind === "start") {
                      const progress = auth.agentSkills.progress;
                      row.receipt_identity = progress?.receipt === value;
                      row.confirmed_retired =
                        progress?.phase === "confirmed" &&
                        progress?.contextValid === false &&
                        progress.projectID === project &&
                        progress.agentID === agent;
                    }
                    row.value = JSON.parse(JSON.stringify(value));
                  } catch {
                    failed = true;
                  } finally {
                    pending--;
                  }
                },
                () => {
                  row.returned = row.rejected = true;
                  pending--;
                },
              );
              return result;
            };
            object[method] = tracked;
            restores.push(() => {
              if (object[method] !== tracked) failed = true;
              object[method] = original;
            });
          };
          wrap(auth.agentSkills, "start", "start");
          wrap(auth.agentSkills, "list", "assignments");
          wrap(auth.agentSkills, "catalog", "catalog");
          wrap(auth.agentConfiguration, "list", "directory");
          wrap(auth.agentConfiguration, "getConfiguration", "configuration");
          wrap(auth.agentConfiguration, "runnerMounts", "mounts");
          (window as any).__agentSkillsOriginal = {
            snapshot: () => ({ rows, pending, failed, retired }),
            finish() {
              if (pending) throw Error("SKILLS_PENDING");
              for (const restore of restores.reverse()) restore();
              retired = true;
              return { pending, failed, retired };
            },
          };
        },
        {
          binding,
          project: data.project_id,
          agent: data.agent_id,
          owner: data.owner_id,
        },
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
  const requests = (kind: string) => entries.filter((e) => e.kind === kind);
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
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as any).__agentSkillsOriginal.snapshot().pending,
        ),
      )
      .toBe(0);
    lastNative = await page.evaluate(() =>
      (window as any).__taskReviewProof.snapshot(),
    );
    lastPublic = await page.evaluate(() =>
      (window as any).__agentSkillsOriginal.snapshot(),
    );
    expect(failure).toBe("");
    expect(lastNative.failed || lastPublic.failed).toBe(false);
    expect(lastNative.current && lastNative.not_busy).toBe(true);
    const indices = new Map<string, number>();
    for (const e of entries) {
      const index = indices.get(e.kind) ?? 0;
      indices.set(e.kind, index + 1);
      if (e.checked) continue;
      expect(e.responses).toBe(1);
      expect(e.response!.status()).toBe(200);
      const path = join(evidence, `agent-skills-response-${e.requestID}.json`);
      await expect.poll(() => existsSync(path)).toBe(true);
      const record = JSON.parse(readFileSync(path, "utf8")),
        url = new URL(e.request.url());
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
      const native = lastNative.rows.filter(
        (r: any) => r.request_id === e.requestID,
      );
      expect(native).toHaveLength(1);
      const row = native[0];
      expect(row.fetch_returned).toBe(true);
      expect(row.fetch_rejected || row.error || row.aborted).toBe(false);
      expect(row.readers).toBe(1);
      expect(row.reads).toBeGreaterThan(0);
      expect(row.read_returns).toBe(row.reads);
      expect(row.eof && row.closed).toBe(true);
      expect(row.release).toBe(1);
      expect(row.reader_cancel + row.stream_cancel).toBe(0);
      expect(record.content_length).toBe(row.bytes);
      expect(record.body_sha256).toBe(row.digest);
      let value = record.value;
      if (e.terminal === "failed")
        expect(e.request.failure()?.errorText).toBe("net::ERR_ABORTED");
      else {
        expect(e.terminal).toBe("finished");
        expect(await e.response!.finished()).toBeNull();
        const bytes = await e.response!.body();
        expect(bytes.length).toBe(row.bytes);
        expect(hash(bytes)).toBe(row.digest);
        const decoded = JSON.parse(bytes.toString("utf8"));
        if (value !== undefined) expect(decoded).toEqual(value);
        value = decoded;
      }
      if (
        [
          "assign",
          "remove",
          "assignments",
          "configuration",
          "mounts",
          "directory",
          "catalog",
        ].includes(e.kind)
      ) {
        const isWrite = e.kind === "assign" || e.kind === "remove";
        const publicIndex = e.kind === "remove" ? 3 : index;
        const consumer = lastPublic.rows.filter(
          (r: any) => r.kind === (isWrite ? "start" : e.kind),
        )[publicIndex];
        expect(consumer).toMatchObject({
          returned: true,
          rejected: false,
          current: true,
          value,
        });
        if (isWrite) {
          expect(consumer).toMatchObject({
            receipt_identity: true,
            confirmed_retired: true,
            sequence: e.request.postDataJSON().expected_version,
          });
          expect(consumer.command.input).toEqual(e.request.postDataJSON());
          expect(Object.keys(value).sort()).toEqual([
            "agent_id",
            "agent_version",
            "assignment",
            "assignment_sequence",
            "change_id",
            "changed",
            "project_id",
            "runtime",
          ]);
          expect(value).toMatchObject({
            project_id: data.project_id,
            agent_id: data.agent_id,
            agent_version: String(publicIndex + 2),
            assignment_sequence: String(publicIndex + 2),
            changed: true,
          });
          expect(value.change_id).toMatch(uuid);
          if (publicIndex === 0 || publicIndex === 2)
            expect(value.runtime).toEqual({
              change_id: value.change_id,
              queued: false,
              execution_id: null,
            });
          else expect(value.runtime).toBeNull();
          if (publicIndex === 3) expect(value.assignment).toBeNull();
          else {
            expect(value.assignment).toMatchObject({
              skill_id: data.skill_id,
              enabled: publicIndex !== 1,
              assignment_sequence: String(publicIndex + 2),
            });
            expect(value.assignment.id).toMatch(uuid);
          }
        } else if (e.kind === "mounts") {
          expect(value).toEqual({
            project_id: data.project_id,
            agent_id: data.agent_id,
            agent_version: String(index + 1),
            items: [],
          });
        } else if (e.kind === "assignments") {
          expect(Object.keys(value).sort()).toEqual([
            "agent_id",
            "agent_version",
            "assignment_sequence",
            "items",
            "project_id",
          ]);
          expect(value).toMatchObject({
            agent_id: data.agent_id,
            project_id: data.project_id,
            agent_version: String(index + 1),
            assignment_sequence: String(index + 1),
          });
          expect(
            value.items.filter(
              (a: any) => a.skill_id === data.builtin.skill_id,
            ),
          ).toEqual([data.builtin]);
          const ordinary = value.items.filter(
            (a: any) => a.skill_id === data.skill_id,
          );
          expect(ordinary).toHaveLength(index === 0 || index === 4 ? 0 : 1);
          if (ordinary.length)
            expect(ordinary[0]).toEqual(
              requests("assign")[index - 1]!.value!.assignment,
            );
        }
        e.value = value;
      }
      e.checked = true;
    }
  }
  return {
    verify,
    requests,
    async retire() {
      if (retired) return;
      try {
        await expect.poll(() => installing).toBe(0);
        if (!page.isClosed()) {
          await expect
            .poll(() =>
              page.evaluate(
                () =>
                  (window as any).__agentSkillsOriginal?.snapshot().pending ??
                  0,
              ),
            )
            .toBe(0);
          const publicEnd = await page.evaluate(() =>
            (window as any).__agentSkillsOriginal?.finish(),
          );
          expect(publicEnd).toEqual({
            pending: 0,
            failed: false,
            retired: true,
          });
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
          expect(end.pending).toBe(0);
          expect(end.failed).toBe(false);
          expect(end.retired).toBe(true);
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
      expect(entries.every((e) => e.checked)).toBe(true);
      for (const kind of [
        "assignments",
        "configuration",
        "directory",
        "mounts",
      ])
        expect(requests(kind)).toHaveLength(5);
      expect(requests("catalog")).toHaveLength(1);
      expect(requests("assign")).toHaveLength(3);
      expect(requests("remove")).toHaveLength(1);
      const ids = requests("assign").map((e) => e.value!.assignment.id);
      expect(new Set(ids).size).toBe(3);
      writeFileSync(
        join(
          process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
          "agent-skills-result.json",
        ),
        JSON.stringify({
          completed: true,
          original_requests: entries.length,
          writes: 4,
          input_hash: data.input_hash,
          observers_joined: true,
        }),
        { mode: 0o600, flag: "wx" },
      );
    },
    save(status: string) {
      writeFileSync(
        join(evidence, "agent-skills-originals.json"),
        JSON.stringify({
          status,
          failure,
          retired,
          original_requests: entries.length,
          checked: entries.filter((e) => e.checked).length,
          writes: keys.size,
          public: lastPublic,
          readers: lastNative?.rows.map((r: any) => ({
            request_id: r.request_id,
            eof: r.eof,
            closed: r.closed,
            release: r.release,
            pending_reads: r.reads - r.read_returns,
            bytes: r.bytes,
            digest: r.digest,
          })),
        }),
        { mode: 0o600 },
      );
    },
  };
}
