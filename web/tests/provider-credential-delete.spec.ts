import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Provider } from "../src/system/model-catalog";

const id = (n: number) =>
  `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T04:00:00.000000Z";
const providerPath = `/system/model-providers/${id(1)}`;
const credentialPath = `/system/model-credentials/${id(2)}`;
const provider = (): Provider => ({
  id: id(1),
  version: "2",
  created_at: at,
  updated_at: at,
  input: {
    name: "Credential Owner",
    protocol: "openai-compatible",
    base_url: "https://provider.invalid/v1",
    enabled: true,
    credential_ref: id(2),
    options: { region: "primary", retries: 2 },
  },
});
const identity = {
  user: {
    id: id(90),
    email: "credential-delete@example.test",
    username: "credential-delete",
    display_name: "Credential Delete",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(91),
    issued_at: at,
    absolute_expires_at: "2026-10-18T12:00:00.000000Z",
    idle_expires_at: "2026-10-17T18:00:00.000000Z",
  },
  csrf_token: "synthetic-credential-delete-context",
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
const providerReceipt = (version = "3") => ({
  kind: "provider.update",
  resource_id: id(1),
  version,
  affected_references: "0",
});
const deleteReceipt = () => ({
  credential_id: id(2),
  purpose: "model",
  version: "5",
  deleted: true,
});
interface RequestRecord {
  path: string;
  method: string;
  body: string;
  key: string | null;
}
let wrapper: VueWrapper | undefined;
let canonical: Provider;
let requests: RequestRecord[];
let putProvider: (body: string) => Response | Promise<Response>;
let readProvider: () => Response | Promise<Response>;
let deleteCredential: () => Response | Promise<Response>;
function acceptProvider(body: string) {
  const value = JSON.parse(body) as {
    expected_version: string;
    input: Provider["input"];
  };
  canonical = {
    ...canonical,
    input: value.input,
    version: String(BigInt(value.expected_version) + 1n),
  };
  return json(providerReceipt(canonical.version));
}
const writes = () =>
  requests.filter((item) => ["PUT", "DELETE"].includes(item.method));
const providerWrites = () =>
  writes().filter((item) => item.path === providerPath);
const credentialWrites = () =>
  writes().filter((item) => item.method === "DELETE");
const providerReads = () =>
  requests.filter(
    (item) => item.path === providerPath && item.method === "GET",
  );
function button(text: string) {
  const matches = wrapper!
    .findAll("button")
    .filter((item) => item.text() === text);
  expect(matches, `Exactly one button ${text}`).toHaveLength(1);
  return matches[0]!;
}
async function click(text: string) {
  const target = button(text);
  expect(
    target.attributes("disabled"),
    `Enabled button ${text}`,
  ).toBeUndefined();
  await target.trigger("click");
  await flushPromises();
}

beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  localStorage.clear();
  canonical = provider();
  requests = [];
  putProvider = acceptProvider;
  readProvider = () => json(canonical);
  deleteCredential = () => json(deleteReceipt());
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const path = new URL(
        String(input),
        "https://fixture.test",
      ).pathname.replace("/api/v1", "");
      const method = init.method ?? "GET";
      const body = String(init.body ?? "");
      requests.push({
        path,
        method,
        body,
        key: new Headers(init.headers).get("Idempotency-Key"),
      });
      if (path === "/session") return json(identity);
      if (path === "/system/model-commands/lookup")
        return json({ found: true, receipt: providerReceipt() });
      if (path === "/system/model-credential-commands/lookup")
        return json({ observed: true, result: deleteReceipt() });
      if (path === providerPath && method === "GET") return readProvider();
      if (path === providerPath && method === "PUT") return putProvider(body);
      if (path === credentialPath && method === "GET")
        return json({ credential_id: id(2), purpose: "model", version: "4" });
      if (path === credentialPath && method === "DELETE")
        return deleteCredential();
      throw new Error(`Unexpected ${method} ${path}`);
    }),
  );
  await (await import("../src/account/session")).refreshSession();
  const Delete = (
    await import("../src/components/system/ProviderCredentialDelete.vue")
  ).default;
  wrapper = mount(Delete, {
    props: { provider: provider() },
    attachTo: document.body,
    global: {
      stubs: {
        // Native modal positioning and focus are exercised by the browser harness.
        UiDialog: {
          props: ["open", "title"],
          template:
            '<section v-if="open" role="dialog" :aria-label="title"><slot /></section>',
        },
      },
    },
  });
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.body.replaceChildren();
});

it("cancels without writes, unlinks the latest provider before deleting, and only rereads after a confirmed delete", async () => {
  await click("删除凭据");
  expect(wrapper!.get('[role="dialog"]').attributes("aria-label")).toBe(
    "删除凭据",
  );
  await click("取消");
  expect(writes()).toHaveLength(0);
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.emitted("closed")).toEqual([[false]]);
  expect(wrapper!.emitted("active")!.at(-1)).toEqual([false]);

  canonical = {
    ...canonical,
    version: "6",
    input: {
      ...canonical.input,
      name: "Latest Provider",
      base_url: "https://latest.invalid/v1",
      options: { region: "latest" },
    },
  };
  const latest = canonical;
  await click("删除凭据");
  let finish!: () => void;
  putProvider = (body) =>
    new Promise<Response>((resolve) => {
      finish = () => resolve(acceptProvider(body));
    });
  await click("确认删除");
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(0);
  expect(JSON.parse(providerWrites()[0]!.body)).toEqual({
    expected_version: latest.version,
    input: { ...latest.input, credential_ref: null },
  });
  expect(button("取消").attributes("disabled")).toBeDefined();
  readProvider = () => json({ code: "INTERNAL" }, 500);
  finish();
  await flushPromises();
  expect(writes().map(({ method, path }) => [method, path])).toEqual([
    ["PUT", providerPath],
    ["DELETE", credentialPath],
  ]);
  expect(JSON.parse(credentialWrites()[0]!.body)).toEqual({
    expected_version: "4",
  });
  expect(credentialWrites()[0]!.key).toBeTruthy();
  expect(credentialWrites()[0]!.key).not.toBe(providerWrites()[0]!.key);
  expect(wrapper!.text()).toContain("删除已完成，最新供应商配置尚未读回");
  expect(button("完成").attributes("disabled")).toBeDefined();
  const readsBeforeRetry = providerReads().length;
  readProvider = () => json(canonical);
  await click("重新读取");
  expect(providerReads()).toHaveLength(readsBeforeRetry + 1);
  expect(writes()).toHaveLength(2);
  expect(wrapper!.emitted("updated")!.at(-1)).toEqual([canonical]);
  expect(canonical.input.credential_ref).toBeNull();
  await click("完成");
  expect(wrapper!.emitted("closed")!.at(-1)).toEqual([true]);
  expect(wrapper!.emitted("active")!.at(-1)).toEqual([false]);
});

it("keeps an unknown unlink locked until its original request is confirmed before deleting", async () => {
  await click("删除凭据");
  putProvider = (body) => {
    acceptProvider(body);
    throw new TypeError("Synthetic lost unlink receipt");
  };
  await click("确认删除");
  const original = providerWrites()[0]!;
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(0);
  expect(wrapper!.text()).toContain("原操作结果尚未确认");
  expect(button("确认删除").attributes("disabled")).toBeDefined();
  expect(button("取消").attributes("disabled")).toBeDefined();
  const readsBeforeLookup = providerReads().length;
  await click("查询原操作");
  expect(wrapper!.text()).toContain("已找到历史回执");
  expect(writes()).toHaveLength(1);
  expect(providerReads()).toHaveLength(readsBeforeLookup);
  const observation = requests.find(
    (item) => item.path === "/system/model-commands/lookup",
  )!;
  expect(observation.key).toBe(original.key);
  expect(JSON.parse(observation.body)).toEqual({ command: "provider.update" });
  putProvider = () => json(providerReceipt(canonical.version));
  await click("确认原请求");
  expect(providerWrites()).toHaveLength(2);
  expect(providerWrites()[1]).toEqual(original);
  expect(credentialWrites()).toHaveLength(1);
  expect(wrapper!.text()).toContain("凭据已删除。");
  expect(wrapper!.emitted("updated")!.at(-1)).toEqual([canonical]);
});

it("resumes a partially completed delete against the fixed credential and replays an unknown delete without unlinking again", async () => {
  await click("删除凭据");
  deleteCredential = () =>
    json({ code: "RESOURCE_BUSY", commit_state: "not_started" }, 409);
  await click("确认删除");
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(1);
  expect(wrapper!.text()).toContain("当前供应商已不使用此凭据，凭据尚未删除");
  expect(wrapper!.text()).toContain("此对象仍被使用");
  expect(wrapper!.emitted("updated")!.at(-1)).toEqual([canonical]);
  expect(canonical.input.credential_ref).toBeNull();
  await click("关闭");
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.emitted("closed")!.at(-1)).toEqual([false]);
  expect(wrapper!.emitted("active")!.at(-1)).toEqual([true]);

  canonical = {
    ...canonical,
    version: "4",
    input: { ...canonical.input, credential_ref: id(3) },
  };
  await wrapper!.setProps({ provider: canonical });
  await click("删除凭据");
  deleteCredential = () => {
    throw new TypeError("Synthetic lost delete receipt");
  };
  await click("继续删除");
  const original = credentialWrites()[1]!;
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(2);
  expect(original.path).toBe(credentialPath);
  expect(JSON.parse(original.body)).toEqual({ expected_version: "4" });
  expect(wrapper!.text()).toContain("原操作结果尚未确认");
  expect(button("关闭").attributes("disabled")).toBeDefined();
  await click("查询原操作");
  expect(wrapper!.text()).toContain("已找到历史回执");
  expect(credentialWrites()).toHaveLength(2);
  const observation = requests.find(
    (item) => item.path === "/system/model-credential-commands/lookup",
  )!;
  expect(observation.key).toBe(original.key);
  expect(JSON.parse(observation.body)).toEqual({
    kind: "delete",
    credential_id: id(2),
    expected_version: "4",
  });
  deleteCredential = () => json(deleteReceipt());
  await click("确认原请求");
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(3);
  expect(credentialWrites()[2]).toEqual(original);
  expect(
    requests.some((item) => item.path === `/system/model-credentials/${id(3)}`),
  ).toBe(false);
  expect(wrapper!.text()).toContain("凭据已删除。");
  expect(wrapper!.emitted("updated")!.at(-1)).toEqual([canonical]);
  expect(canonical.input.credential_ref).toBe(id(3));
});

it("blocks deletion when conflict recovery finds the provider now uses a different credential", async () => {
  await click("删除凭据");
  putProvider = () =>
    json({ code: "VERSION_CONFLICT", commit_state: "not_started" }, 409);
  await click("确认删除");
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(0);
  expect(button("确认删除").attributes("disabled")).toBeDefined();
  canonical = {
    ...canonical,
    version: "3",
    input: { ...canonical.input, credential_ref: id(3) },
  };
  const readsBeforeReload = providerReads().length;
  await click("读取最新版本");
  expect(providerReads()).toHaveLength(readsBeforeReload + 1);
  expect(wrapper!.text()).toContain(
    "供应商关联的凭据已更改。请取消后重新发起删除。",
  );
  expect(button("确认删除").attributes("disabled")).toBeDefined();
  expect(writes()).toHaveLength(1);
  expect(
    requests.some((item) => item.path === `/system/model-credentials/${id(3)}`),
  ).toBe(false);
  await click("取消");
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.emitted("closed")!.at(-1)).toEqual([false]);
});

it("explicitly ends a known partial delete without restoring the association or deleting the credential", async () => {
  await click("删除凭据");
  deleteCredential = () =>
    json({ code: "RESOURCE_BUSY", commit_state: "not_started" }, 409);
  await click("确认删除");
  await wrapper!.setProps({ provider: canonical });
  expect(canonical.input.credential_ref).toBeNull();
  await click("结束删除…");
  expect(wrapper!.text()).toContain("结束本次删除？");
  expect(writes()).toHaveLength(2);
  await click("继续处理");
  expect(button("继续删除").attributes("disabled")).toBeUndefined();
  expect(writes()).toHaveLength(2);
  await click("结束删除…");
  await click("结束删除");
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.emitted("closed")!.at(-1)).toEqual([false]);
  expect(wrapper!.emitted("active")!.at(-1)).toEqual([false]);
  expect(button("删除凭据").attributes("disabled")).toBeDefined();
  expect(providerWrites()).toHaveLength(1);
  expect(credentialWrites()).toHaveLength(1);
  expect(canonical.input.credential_ref).toBeNull();
});
