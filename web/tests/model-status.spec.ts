import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { CatalogModel, Provider } from "../src/system/model-catalog";

const id = (n: number) =>
  `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T04:00:00.000000Z";
const modelPath = `/system/models/${id(2)}`;
const provider = (): Provider => ({
  id: id(1),
  version: "2",
  created_at: at,
  updated_at: at,
  input: {
    name: "Status Provider",
    protocol: "openai-compatible",
    base_url: "https://provider.invalid/v1",
    enabled: true,
    credential_ref: null,
    options: {},
  },
});
const model = (): CatalogModel => ({
  id: id(2),
  provider_id: id(1),
  version: "3",
  created_at: at,
  updated_at: at,
  input: {
    name: "Status Model",
    provider_model_id: "upstream-chat",
    type: "chat",
    enabled: true,
    parameters: { temperature: 0.2, custom: { budget: 20 } },
    request_overwrite: { metadata: { purpose: "fixture" } },
    header_overwrite: { "X-Model-Region": "primary" },
    capabilities: {
      tool_calls: true,
      parallel_tool_calls: false,
      streaming: true,
      reasoning: true,
      input_modalities: ["text", "image"],
      output_modalities: ["text"],
      reasoning_efforts: ["low", "high"],
      structured_output_modes: ["json_schema"],
      context_length: "128000",
      max_output: "4096",
    },
  },
});
const identity = {
  user: {
    id: id(90),
    email: "model-status@example.test",
    username: "model-status",
    display_name: "Model Status",
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
  csrf_token: "synthetic-model-status-context",
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
const receipt = (version = "4") => ({
  kind: "model.update",
  resource_id: id(2),
  version,
  affected_references: "0",
});
interface RequestRecord {
  path: string;
  method: string;
  body: string;
  key: string | null;
}
let wrapper: VueWrapper | undefined;
let canonical: CatalogModel;
let requests: RequestRecord[];
let putModel: (body: string) => Response | Promise<Response>;
let readModel: () => Response | Promise<Response>;
function acceptModel(body: string) {
  const value = JSON.parse(body) as {
    expected_version: string;
    input: CatalogModel["input"];
  };
  canonical = {
    ...canonical,
    input: value.input,
    version: String(BigInt(value.expected_version) + 1n),
  };
  return json(receipt(canonical.version));
}
const writes = () =>
  requests.filter((item) => item.path === modelPath && item.method === "PUT");
const reads = () =>
  requests.filter((item) => item.path === modelPath && item.method === "GET");
const toggle = () => wrapper!.get('[role="switch"]');
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
  canonical = model();
  requests = [];
  putModel = acceptModel;
  readModel = () => json(canonical);
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
        return json({ found: true, receipt: receipt() });
      if (path === modelPath && method === "PUT") return putModel(body);
      if (path === modelPath && method === "GET") return readModel();
      throw new Error(`Unexpected ${method} ${path}`);
    }),
  );
  await (await import("../src/account/session")).refreshSession();
  const Status = (
    await import("../src/components/system/ModelStatusSwitch.vue")
  ).default;
  wrapper = mount(Status, {
    props: { provider: provider(), model: canonical },
    attachTo: document.body,
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

it("changes only enabled, blocks repeated clicks, and displays the confirmed model after reading it back", async () => {
  let finishWrite!: (response: Response) => void;
  let finishRead!: (response: Response) => void;
  putModel = () =>
    new Promise<Response>((resolve) => {
      finishWrite = resolve;
    });
  readModel = () =>
    new Promise<Response>((resolve) => {
      finishRead = resolve;
    });
  expect(toggle().attributes("aria-label")).toBe("启用upstream-chat");
  expect(toggle().attributes("aria-checked")).toBe("true");
  (toggle().element as HTMLButtonElement).focus();
  await toggle().trigger("click");
  await flushPromises();
  expect(writes()).toHaveLength(1);
  const original = writes()[0]!;
  expect(JSON.parse(original.body)).toEqual({
    expected_version: "3",
    input: { ...model().input, enabled: false },
  });
  expect(original.key).toBeTruthy();
  expect(toggle().attributes("aria-checked")).toBe("true");
  expect(toggle().attributes("aria-disabled")).toBe("true");
  expect(wrapper!.emitted("locked")!.at(-1)).toEqual([true]);
  await toggle().trigger("click");
  expect(writes()).toHaveLength(1);
  finishWrite(acceptModel(original.body));
  await flushPromises();
  expect(reads()).toHaveLength(1);
  expect(toggle().attributes("aria-checked")).toBe("true");
  expect(wrapper!.emitted("saved")).toBeUndefined();
  finishRead(json(canonical));
  await flushPromises();
  expect(toggle().attributes("aria-checked")).toBe("false");
  expect(toggle().attributes("disabled")).toBeUndefined();
  expect(wrapper!.emitted("saved")).toEqual([[canonical]]);
  expect(wrapper!.emitted("locked")!.at(-1)).toEqual([false]);
  expect(document.activeElement).toBe(toggle().element);

  const legacy = provider();
  legacy.input.protocol = "openai-chat-completions";
  await wrapper!.setProps({ provider: legacy });
  expect(toggle().attributes("disabled")).toBeDefined();
  await toggle().trigger("click");
  expect(writes()).toHaveLength(1);
});

it("keeps an unknown change locked after lookup and confirms only its original request", async () => {
  putModel = (body) => {
    acceptModel(body);
    throw new TypeError("Synthetic lost model update receipt");
  };
  await toggle().trigger("click");
  await flushPromises();
  expect(writes()).toHaveLength(1);
  const original = writes()[0]!;
  expect(wrapper!.text()).toContain("原操作结果尚未确认");
  expect(toggle().attributes("aria-checked")).toBe("true");
  expect(toggle().attributes("disabled")).toBeDefined();
  await click("查询原操作");
  expect(wrapper!.text()).toContain("已找到历史回执");
  expect(writes()).toHaveLength(1);
  expect(reads()).toHaveLength(0);
  expect(wrapper!.emitted("saved")).toBeUndefined();
  expect(toggle().attributes("disabled")).toBeDefined();
  const observation = requests.find(
    (item) => item.path === "/system/model-commands/lookup",
  )!;
  expect(observation.key).toBe(original.key);
  expect(JSON.parse(observation.body)).toEqual({ command: "model.update" });
  putModel = () => json(receipt(canonical.version));
  await click("确认原请求");
  expect(writes()).toHaveLength(2);
  expect(writes()[1]).toEqual(original);
  expect(toggle().attributes("aria-checked")).toBe("false");
  expect(toggle().attributes("disabled")).toBeUndefined();
  expect(wrapper!.emitted("saved")).toEqual([[canonical]]);
});

it("retains the requested status after a conflict and explicitly applies it with the latest complete input and version", async () => {
  putModel = () =>
    json({ code: "VERSION_CONFLICT", commit_state: "not_started" }, 409);
  await toggle().trigger("click");
  await flushPromises();
  expect(writes()).toHaveLength(1);
  expect(reads()).toHaveLength(0);
  expect(toggle().attributes("aria-checked")).toBe("true");
  canonical = {
    ...model(),
    version: "7",
    input: {
      ...model().input,
      name: "Refreshed Model",
      provider_model_id: "upstream-refreshed",
      parameters: { temperature: 0.8 },
      request_overwrite: { metadata: { purpose: "refreshed" } },
      header_overwrite: { "X-Model-Region": "refreshed" },
      capabilities: { ...model().input.capabilities, max_output: "8192" },
    },
  };
  await click("读取最新版本");
  expect(reads()).toHaveLength(1);
  expect(writes()).toHaveLength(1);
  expect(toggle().attributes("aria-label")).toBe("启用upstream-refreshed");
  expect(toggle().attributes("aria-checked")).toBe("true");
  const latest = canonical;
  putModel = acceptModel;
  await click("应用更改");
  expect(writes()).toHaveLength(2);
  expect(JSON.parse(writes()[1]!.body)).toEqual({
    expected_version: "7",
    input: { ...latest.input, enabled: false },
  });
  expect(writes()[1]!.key).not.toBe(writes()[0]!.key);
  expect(toggle().attributes("aria-checked")).toBe("false");
  expect(wrapper!.emitted("saved")!.at(-1)).toEqual([canonical]);
});

it("only rereads after a confirmed update whose follow-up read failed", async () => {
  readModel = () =>
    json({ code: "INTERNAL", commit_state: "not_started" }, 500);
  await toggle().trigger("click");
  await flushPromises();
  expect(writes()).toHaveLength(1);
  expect(reads()).toHaveLength(1);
  expect(wrapper!.text()).toContain("状态已保存，最新配置尚未读回。");
  expect(wrapper!.text()).not.toContain("确认原请求");
  expect(toggle().attributes("aria-checked")).toBe("true");
  expect(toggle().attributes("disabled")).toBeDefined();
  expect(wrapper!.emitted("saved")).toBeUndefined();
  readModel = () => json(canonical);
  await click("重新读取");
  expect(reads()).toHaveLength(2);
  expect(writes()).toHaveLength(1);
  expect(toggle().attributes("aria-checked")).toBe("false");
  expect(toggle().attributes("disabled")).toBeUndefined();
  expect(wrapper!.emitted("saved")).toEqual([[canonical]]);
  expect(wrapper!.emitted("locked")!.at(-1)).toEqual([false]);
});
