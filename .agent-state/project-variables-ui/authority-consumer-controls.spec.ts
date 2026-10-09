// Offline authority diagnostics: real Session, Workspace, Variables controller
// and HTTP decoder. Only the transport returns synthetic, explicit responses.
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises } from "@vue/test-utils";
import { createAccountAPI } from "../../web/src/api/account";
import { AccountFailure, type Fetch } from "../../web/src/api/client";
import { createProjectOwnerAPI } from "../../web/src/api/project-owner";
import { createProjectVariablesAPI } from "../../web/src/api/project-variables";
import { createSessionController } from "../../web/src/composables/useSession";
import { createProjectWorkspace } from "../../web/src/composables/useProjectWorkspace";
import { createProjectVariables } from "../../web/src/composables/useProjectVariables";

const id = (n: number) =>
  `01970000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
const projectID = id(10),
  targetID = id(20),
  requestID = id(90);
const endpoint = `/api/v1/projects/${projectID}/variables/${targetID}`;
const route = "/owner/project.name/settings/variables";
const at = "2026-10-09T12:00:00.000001Z";
const csrf = "S".repeat(43),
  draft = "prepared before archive";
const summary = {
  id: targetID,
  project_id: projectID,
  type: "variable",
  name: "CUSTOM_VALUE",
  description: "",
  version: "1",
  created_at: at,
  updated_at: at,
};
const owner = {
  id: projectID,
  owner_user_id: id(1),
  name: "project.name",
  normalized_name: "project.name",
  description: "",
  lifecycle: "active",
  version: "1",
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
};
const session = {
  user: {
    id: id(1),
    email: "owner@example.test",
    username: "owner",
    display_name: "Owner",
    role: "user",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(2),
    issued_at: at,
    idle_expires_at: at,
    absolute_expires_at: at,
  },
  csrf_token: csrf,
};
function json(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json", "X-Request-ID": requestID },
  });
}
function barrier() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const vectors = [
  {
    name: "not_started",
    code: "PROJECT_NOT_ACTIVE",
    commit: "not_started",
    xid: requestID,
    phase: "rejected",
    kind: "problem",
  },
  {
    name: "not_committed",
    code: "PROJECT_NOT_ACTIVE",
    commit: "not_committed",
    xid: requestID,
    phase: "rejected",
    kind: "problem",
  },
  {
    name: "wrong code",
    code: "INVALID_STATE",
    commit: "not_committed",
    xid: requestID,
    phase: "uncertain",
    kind: "problem",
  },
  {
    name: "unknown commit",
    code: "PROJECT_NOT_ACTIVE",
    commit: "unknown",
    xid: requestID,
    phase: "uncertain",
    kind: "problem",
  },
  {
    name: "wrong XID",
    code: "PROJECT_NOT_ACTIVE",
    commit: "not_committed",
    xid: id(91),
    phase: "uncertain",
    kind: "invalid-response",
  },
] as const;

function heldProblem(
  vector: (typeof vectors)[number],
  tail: "reader" | "outer",
) {
  const started = barrier(),
    release = barrier();
  const body = {
    type: `urn:agenteam:problem:${vector.code.toLowerCase().replaceAll("_", "-")}`,
    title: "Project not active",
    detail: "The project is not active.",
    status: 409,
    instance: endpoint,
    request_id: vector.xid,
    code: vector.code,
    commit_state: vector.commit,
  };
  const response = new Response(JSON.stringify(body), {
    status: 409,
    headers: {
      "Content-Type": "application/problem+json",
      "X-Request-ID": requestID,
    },
  });
  const stream = response.body!,
    getReader = stream.getReader.bind(stream),
    streamCancel = stream.cancel.bind(stream);
  const counts = { reads: 0, eof: 0, reader: 0, outer: 0, release: 0 };
  Object.defineProperty(stream, "getReader", {
    value: () => {
      const reader = getReader(),
        read = reader.read.bind(reader),
        cancel = reader.cancel.bind(reader),
        unlock = reader.releaseLock.bind(reader);
      Object.defineProperty(reader, "read", {
        value: async () => {
          counts.reads++;
          const result = await read();
          if (result.done) counts.eof++;
          return result;
        },
      });
      Object.defineProperty(reader, "cancel", {
        value: async (reason?: unknown) => {
          counts.reader++;
          await cancel(reason);
          if (tail === "reader") {
            started.resolve();
            await release.promise;
          }
        },
      });
      Object.defineProperty(reader, "releaseLock", {
        value: () => {
          counts.release++;
          return unlock();
        },
      });
      return reader;
    },
  });
  Object.defineProperty(stream, "cancel", {
    value: async (reason?: unknown) => {
      counts.outer++;
      await streamCancel(reason);
      if (tail === "outer") {
        started.resolve();
        await release.promise;
      }
    },
  });
  return { response, started, release, counts, body };
}

const cleanups: (() => void)[] = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup();
});
async function fixture(response: Response) {
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === "/api/v1/session") return json(session);
    if (url === endpoint && init.method === "PATCH") return response;
    if (url === endpoint && init.method === "GET")
      return json({ ...summary, value: "original" });
    if (
      url.startsWith(`/api/v1/projects/${projectID}/variables?`) &&
      init.method === "GET"
    )
      return json({ items: [summary] });
    if (url.startsWith("/api/v1/projects/") && init.method === "GET")
      return json(owner);
    throw new Error("UNEXPECTED_OFFLINE_REQUEST");
  });
  const auth = createSessionController(
    createAccountAPI(fetcher),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetcher),
    undefined,
    undefined,
    createProjectVariablesAPI(fetcher),
  );
  await auth.restore();
  const workspace = createProjectWorkspace(auth),
    page = createProjectVariables(auth, workspace);
  cleanups.push(() => {
    page.dispose();
    workspace.dispose();
    auth.leave();
  });
  workspace.afterNavigation(route, "/");
  page.afterNavigation(route, "/");
  for (let i = 0; i < 4; i++) await flushPromises();
  expect(page.visible.value).toBe(true);
  await page.select(targetID);
  page.draft.value = draft;
  expect(page.canSave.value).toBe(true);
  return { auth, workspace, page, fetcher };
}

describe("actual archived UPDATE typed consumer and Cookie owner tails", () => {
  for (const tail of ["reader", "outer"] as const) {
    for (const vector of vectors) {
      it(`${tail} cancellation held: ${vector.name}`, async () => {
        const held = heldProblem(vector, tail);
        const f = await fixture(held.response);
        const originalStart = f.auth.projectVariables.start;
        let originalPromise: ReturnType<typeof originalStart> | undefined;
        let outcome: "pending" | "fulfilled" | "rejected" = "pending",
          rejection: unknown;
        const observed = vi
          .spyOn(f.auth.projectVariables, "start")
          .mockImplementation(function (this: unknown, command) {
            originalPromise = Reflect.apply(originalStart, this, [command]);
            void originalPromise!.then(
              () => {
                outcome = "fulfilled";
              },
              (error) => {
                outcome = "rejected";
                rejection = error;
              },
            );
            return originalPromise!;
          });
        let pageSettled = false;
        const save = f.page.save().then((value) => {
          pageSettled = true;
          return value;
        });
        try {
          await held.started.promise;
          await flushPromises();
          expect(observed).toHaveBeenCalledTimes(1);
          expect(observed.mock.results[0]!.value).toBe(originalPromise);
          expect(outcome).toBe("pending");
          expect(pageSettled).toBe(false);
          expect(f.auth.state.busy).toBe(true);
          expect(f.auth.projectVariables.progress).toMatchObject({
            phase: "submitting",
            receipt: null,
            canReplayOriginal: false,
          });
          expect(f.page.canSave.value).toBe(false);
          expect(f.page.draft.value).toBe(draft);
          expect(held.counts).toEqual({
            reads: 2,
            eof: 1,
            reader: 1,
            outer: tail === "outer" ? 1 : 0,
            release: tail === "outer" ? 1 : 0,
          });
          const calls = f.fetcher.mock.calls.length;
          await expect(f.auth.projects.get(projectID)).rejects.toMatchObject({
            kind: "busy",
          });
          await expect(
            f.auth.projectVariables.get(projectID, targetID),
          ).rejects.toMatchObject({ kind: "busy" });
          await f.auth.restore();
          expect(await f.page.createNew()).toBe(false);
          expect(await f.page.save()).toBe(false);
          expect(f.fetcher).toHaveBeenCalledTimes(calls);
          expect(outcome).toBe("pending");
        } finally {
          held.release.resolve();
          await save;
        }
        expect(await save).toBe(false);
        expect(outcome).toBe("rejected");
        expect(rejection).toBeInstanceOf(AccountFailure);
        expect(rejection).toMatchObject({ kind: vector.kind });
        if (vector.kind === "problem")
          expect((rejection as AccountFailure).problem).toEqual(held.body);
        else expect((rejection as AccountFailure).problem).toBeUndefined();
        expect(f.auth.state.busy).toBe(false);
        expect(f.auth.state.phase).toBe("authenticated");
        expect(f.auth.projectVariables.progress).toMatchObject({
          phase: vector.phase,
          receipt: null,
        });
        expect(f.page.draft.value).toBe(draft);
        expect(f.page.editor.version).toBe("1");
        expect(f.page.editor.original?.value).toBe("original");
        expect(f.page.feedback.value).toBe("idle");
        expect(held.counts).toEqual({
          reads: 2,
          eof: 1,
          reader: 1,
          outer: 1,
          release: 1,
        });
        const writes = f.fetcher.mock.calls.filter(
          ([, init]) => init.method === "PATCH",
        );
        expect(writes).toHaveLength(1);
        const [url, init] = writes[0]!;
        expect(url).toBe(endpoint);
        expect(JSON.parse(String(init.body))).toEqual({
          expected_version: "1",
          request: { value: draft },
        });
        expect(new Headers(init.headers).get("X-CSRF-Token")).toBe(csrf);
        expect(new Headers(init.headers).get("Idempotency-Key")).toMatch(
          /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
        );
        expect(init.signal?.aborted).toBe(false);
        expect(f.auth.projectVariables.editRejected()).toBe(
          vector.phase === "rejected",
        );
      });
    }
  }
});
