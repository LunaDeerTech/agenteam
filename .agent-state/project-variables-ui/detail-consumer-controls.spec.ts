// Production GET transport, Session owner, Workspace and Variables publisher.
// Only HTTP transport is synthetic; no browser, socket or upstream service.
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createAccountAPI } from "../../web/src/api/account";
import { AccountFailure, type Fetch } from "../../web/src/api/client";
import { createProjectOwnerAPI } from "../../web/src/api/project-owner";
import { createProjectVariablesAPI } from "../../web/src/api/project-variables";
import { createSessionController } from "../../web/src/composables/useSession";
import { createProjectWorkspace } from "../../web/src/composables/useProjectWorkspace";
import {
  createProjectVariables,
  projectVariablesKey,
} from "../../web/src/composables/useProjectVariables";
import ProjectVariableEditor from "../../web/src/views/projects/ProjectVariableEditor.vue";
import { installVariableDetail } from "../../tests/account-captcha-web/e2e/project-variables.detail";

const id = (n: number) =>
  `01970000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const project = id(10),
  target = id(20),
  xid = id(90),
  at = "2026-10-09T12:00:00.000001Z";
const endpoint = `/api/v1/projects/${project}/variables/${target}`;
const route = "/owner/project.name/settings/variables";
const summary = {
  id: target,
  project_id: project,
  type: "variable",
  name: "VALUE",
  description: "original description",
  version: "1",
  created_at: at,
  updated_at: at,
};
const value = { ...summary, value: "fresh complete response" };
const owner = {
  id: project,
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
  csrf_token: "S".repeat(43),
};
const json = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json", "X-Request-ID": xid },
  });
function barrier() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function heldResponse(raw: string, tail: "reader" | "outer") {
  const started = barrier(),
    release = barrier();
  const response = new Response(raw, {
    headers: { "Content-Type": "application/json", "X-Request-ID": xid },
  });
  const stream = response.body!,
    getReader = stream.getReader.bind(stream),
    cancelStream = stream.cancel.bind(stream);
  const counts = { read: 0, eof: 0, reader: 0, outer: 0, unlock: 0 };
  Object.defineProperty(stream, "getReader", {
    value: () => {
      const reader = getReader(),
        read = reader.read.bind(reader),
        cancel = reader.cancel.bind(reader),
        unlock = reader.releaseLock.bind(reader);
      Object.defineProperty(reader, "read", {
        value: async () => {
          counts.read++;
          const out = await read();
          if (out.done) counts.eof++;
          return out;
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
          counts.unlock++;
          return unlock();
        },
      });
      return reader;
    },
  });
  Object.defineProperty(stream, "cancel", {
    value: async (reason?: unknown) => {
      counts.outer++;
      await cancelStream(reason);
      if (tail === "outer") {
        started.resolve();
        await release.promise;
      }
    },
  });
  return { response, started, release, counts };
}
const cleanups: (() => void)[] = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup();
  delete (window as any).__variableDetail;
  delete (window as any).__variableNativeDiagnostic;
});
async function fixture(response: Response) {
  const native = {
    document: "same-document",
    retired: false,
    records: [] as unknown[],
  };
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === "/api/v1/session") return json(session);
    if (url === endpoint && init.method === "GET") {
      native.records.push({
        method: "GET",
        path: endpoint,
        query: "",
        status: 200,
        request_id: xid,
      });
      return response;
    }
    if (
      url.startsWith(`/api/v1/projects/${project}/variables?`) &&
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
  expect(page.editor.open).toBe(false);
  expect(page.editor.original).toBeNull();
  const editor = mount(ProjectVariableEditor, {
    attachTo: document.body,
    global: { provide: { [projectVariablesKey as symbol]: page } },
  });
  cleanups.push(() => editor.unmount());
  const original = auth.projectVariables.get;
  let promise: ReturnType<typeof original> | undefined;
  let state: "pending" | "fulfilled" | "rejected" = "pending";
  let result: unknown, rejection: unknown;
  const observed = vi
    .spyOn(auth.projectVariables, "get")
    .mockImplementation(function (this: unknown, ...args) {
      promise = Reflect.apply(original, this, args);
      void promise!.then(
        (v) => {
          state = "fulfilled";
          result = v;
        },
        (e) => {
          state = "rejected";
          rejection = e;
        },
      );
      return promise!;
    });
  return {
    auth,
    workspace,
    page,
    fetcher,
    observed,
    native,
    editor,
    observation: () => ({ promise, state, result, rejection }),
  };
}

describe("original GET public Promise and current Variables publication", () => {
  for (const tail of ["reader", "outer"] as const) {
    for (const mode of [
      "valid",
      "wrong-target",
      "duplicate-member",
      "navigation",
      "identity",
    ] as const) {
      it(`${tail} held: ${mode}`, async () => {
        const raw =
          mode === "wrong-target"
            ? JSON.stringify({ ...value, id: id(21) })
            : mode === "duplicate-member"
              ? JSON.stringify(value).replace(
                  '"value":',
                  '"value":"first","value":',
                )
              : JSON.stringify(value);
        const held = heldResponse(raw, tail),
          f = await fixture(held.response);
        if (mode === "valid") {
          window.history.replaceState(null, "", route);
          (window as any).__variableNativeDiagnostic = {
            snapshot: () => ({ ...f.native }),
          };
          // Only module discovery is substituted. The observer then wraps this
          // actual Session and reads the actual production editor's DOM.
          const oldFunction = globalThis.Function,
            oldPerformance = globalThis.performance;
          vi.stubGlobal("Function", function () {
            return async () => ({ singleton: () => f.auth });
          });
          vi.stubGlobal("performance", { getEntriesByName: () => [{}] });
          try {
            expect(
              await installVariableDetail({
                binding: {
                  entry: "/entry.js",
                  asset: "/asset.js",
                  singleton: "singleton",
                  failure: "unused",
                },
                target: { project, variable: target, route },
              }),
            ).toBe(true);
          } finally {
            vi.stubGlobal("Function", oldFunction);
            vi.stubGlobal("performance", oldPerformance);
          }
        }
        let controllerSettled = false;
        const selection = f.page.select(target).then((result) => {
          controllerSettled = true;
          return result;
        });
        try {
          await held.started.promise;
          await flushPromises();
          expect(f.observed).toHaveBeenCalledTimes(1);
          expect(f.observed.mock.results[0]!.value).toBe(
            f.observation().promise,
          );
          expect(f.observation().state).toBe("pending");
          expect(controllerSettled).toBe(false);
          expect(f.auth.state.busy).toBe(true);
          expect(f.page.editor.original).toBeNull();
          expect(f.page.editor.version).toBe("");
          expect(f.page.draft.value).toBe("");
          const calls = f.fetcher.mock.calls.length;
          await expect(f.auth.projects.get(project)).rejects.toMatchObject({
            kind: "busy",
          });
          await expect(
            f.auth.projectVariables.start({
              kind: "update",
              projectID: project,
              targetID: target,
              expectedVersion: "1",
              request: { value: "must not send" },
            }),
          ).rejects.toMatchObject({ kind: "busy" });
          await f.auth.restore();
          expect(f.fetcher).toHaveBeenCalledTimes(calls);
          if (mode === "navigation") f.page.afterNavigation("/projects", route);
          if (mode === "identity") f.auth.leave();
        } finally {
          held.release.resolve();
          await selection;
        }
        await flushPromises();
        expect(f.auth.state.busy).toBe(false);
        expect(held.counts).toEqual({
          read: 2,
          eof: 1,
          reader: 1,
          outer: 1,
          unlock: 1,
        });
        if (mode === "valid") {
          expect(await selection).toBe(true);
          expect(f.observation().state).toBe("fulfilled");
          expect(f.observation().result).toEqual(value);
          expect(f.page.editor.original).toBe(f.observation().result);
          expect(f.page.editor).toMatchObject({
            open: true,
            phase: "ready",
            targetID: target,
            version: "1",
            review: null,
            conflict: false,
            requiresRead: false,
          });
          expect(f.page.draft).toEqual({
            name: value.name,
            description: value.description,
            value: value.value,
          });
          expect(f.page.visible.value).toBe(true);
          expect(f.auth.state.phase).toBe("authenticated");
          expect(f.auth.projectVariables.progress).toBeNull();
          const observed = (window as any).__variableDetail.finish({
            id: xid,
            value,
          });
          expect(observed).toMatchObject({
            calls: 1,
            target_calls: 1,
            fulfilled: 1,
            rejected: 0,
            pending: 0,
            body_matches: true,
            published: true,
            owner_idle: true,
            identity_current: true,
            native_request_matches: true,
            hooks_retired: true,
            observer_failed: false,
          });
        } else {
          expect(await selection).toBe(false);
          expect(f.page.editor.original).toBeNull();
          expect(f.page.draft.value).toBe("");
          if (mode === "wrong-target" || mode === "duplicate-member") {
            expect(f.observation().state).toBe("rejected");
            expect(f.observation().rejection).toBeInstanceOf(AccountFailure);
            expect(f.observation().rejection).toMatchObject({
              kind: "invalid-response",
            });
          }
          if (mode === "navigation" || mode === "identity")
            expect(f.page.visible.value).toBe(false);
        }
        const gets = f.fetcher.mock.calls.filter(([url]) => url === endpoint);
        expect(gets).toHaveLength(1);
        expect(gets[0]![1].method).toBe("GET");
        expect(gets[0]![1].body).toBeUndefined();
        expect(new Headers(gets[0]![1].headers).has("Idempotency-Key")).toBe(
          false,
        );
        expect(new Headers(gets[0]![1].headers).has("X-CSRF-Token")).toBe(
          false,
        );
      });
    }
  }
});
