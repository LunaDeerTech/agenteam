import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { h, shallowRef } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
    name: "Fixture Provider",
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
    email: "provider-settings@example.test",
    username: "provider-settings",
    display_name: "Provider Settings",
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
  csrf_token: "synthetic-provider-settings-context",
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
const receipt = (version = "3") => ({
  kind: "provider.update",
  resource_id: id(1),
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
let restoreDialog: (() => void) | undefined;
let canonical: Provider;
let requests: RequestRecord[];
let putProvider: (body: string) => Response | Promise<Response>;
let readProvider: () => Response | Promise<Response>;
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
  return json(receipt(canonical.version));
}
const providerWrites = () =>
  requests.filter(
    (item) => item.path === providerPath && item.method === "PUT",
  );
const providerReads = () =>
  requests.filter(
    (item) => item.path === providerPath && item.method === "GET",
  );
const secretWrites = () =>
  requests.filter(
    (item) =>
      item.path.startsWith("/system/model-credentials") &&
      ["POST", "PUT", "DELETE"].includes(item.method),
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
function field(label: string) {
  const matches = wrapper!
    .findAll("label")
    .filter((item) => item.text() === label);
  expect(matches, `Exactly one field ${label}`).toHaveLength(1);
  return wrapper!.get(`input[id="${matches[0]!.attributes("for")}"]`);
}
const toggle = () => wrapper!.get('[role="switch"]');
async function submit() {
  await wrapper!.get("form").trigger("submit");
  await flushPromises();
}
const stubs = {
  SettingsPageActions: { template: "<div><slot /></div>" },
  RouterLink: {
    props: ["to"],
    template: '<a :href="to"><slot /></a>',
  },
};

beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  localStorage.clear();
  canonical = provider();
  requests = [];
  putProvider = acceptProvider;
  readProvider = () => json(canonical);
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
      if (path === "/system/model-providers" && method === "GET")
        return json({ items: [canonical], next_cursor: null });
      if (path === providerPath)
        return method === "PUT" ? putProvider(body) : readProvider();
      if (path === credentialPath)
        return json({
          credential_id: id(2),
          purpose: "model",
          version: method === "GET" ? "4" : "5",
          ...(method === "GET" ? {} : { deleted: false }),
        });
      throw new Error(`Unexpected ${method} ${path}`);
    }),
  );
  await (await import("../src/account/session")).refreshSession();
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  restoreDialog?.();
  restoreDialog = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.body.replaceChildren();
});

describe("provider status in the catalog", () => {
  beforeEach(async () => {
    const originalShowModal = Object.getOwnPropertyDescriptor(
      HTMLDialogElement.prototype,
      "showModal",
    );
    const originalClose = Object.getOwnPropertyDescriptor(
      HTMLDialogElement.prototype,
      "close",
    );
    // Model the native open property for jsdom; modal behavior is browser-tested.
    Object.defineProperties(HTMLDialogElement.prototype, {
      showModal: {
        configurable: true,
        value(this: HTMLDialogElement) {
          this.open = true;
        },
      },
      close: {
        configurable: true,
        value(this: HTMLDialogElement) {
          this.open = false;
        },
      },
    });
    restoreDialog = () => {
      for (const [name, descriptor] of [
        ["showModal", originalShowModal],
        ["close", originalClose],
      ] as const) {
        if (descriptor)
          Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
        else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
      }
    };
    const Page = (await import("../src/views/system/ModelCatalogPage.vue"))
      .default;
    const { createMemoryHistory, createRouter } = await import("vue-router");
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/system/models", component: Page }],
    });
    await router.push("/system/models");
    await router.isReady();
    wrapper = mount(Page, {
      attachTo: document.body,
      global: { stubs, plugins: [router] },
    });
    await flushPromises();
  });

  it("changes only enabled and keeps the confirmed switch value while the save is pending", async () => {
    let finish!: (response: Response) => void;
    putProvider = () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      });
    expect(toggle().attributes("aria-label")).toBe("启用Fixture Provider");
    expect(toggle().attributes("aria-checked")).toBe("true");
    (toggle().element as HTMLButtonElement).focus();
    await toggle().trigger("click");
    await flushPromises();
    expect(providerWrites()).toHaveLength(1);
    const original = providerWrites()[0]!;
    expect(JSON.parse(original.body)).toEqual({
      expected_version: "2",
      input: { ...provider().input, enabled: false },
    });
    expect(original.key).toBeTruthy();
    expect(toggle().attributes("aria-checked")).toBe("true");
    expect(toggle().attributes("aria-disabled")).toBe("true");
    await toggle().trigger("click");
    expect(providerWrites()).toHaveLength(1);
    finish(acceptProvider(original.body));
    await flushPromises();
    expect(toggle().attributes("aria-checked")).toBe("false");
    expect(toggle().attributes("disabled")).toBeUndefined();
    expect(document.activeElement).toBe(toggle().element);
    expect(providerWrites()).toHaveLength(1);
    expect(secretWrites()).toHaveLength(0);
    await toggle().trigger("click");
    await flushPromises();
    expect(providerWrites()).toHaveLength(2);
    const outside = document.createElement("button");
    outside.textContent = "其他操作";
    document.body.append(outside);
    outside.focus();
    finish(acceptProvider(providerWrites()[1]!.body));
    await flushPromises();
    expect(toggle().attributes("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(outside);
    expect(providerWrites()).toHaveLength(2);
  });

  it("keeps an unknown toggle locked after lookup and confirms only the original request", async () => {
    putProvider = () => {
      throw new TypeError("Synthetic interrupted response");
    };
    await toggle().trigger("click");
    await flushPromises();
    const original = providerWrites()[0]!;
    expect(providerWrites()).toHaveLength(1);
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    expect(toggle().attributes("aria-checked")).toBe("true");
    expect(toggle().attributes("disabled")).toBeDefined();
    await click("查询原操作");
    expect(wrapper!.text()).toContain("已找到历史回执");
    expect(providerWrites()).toHaveLength(1);
    expect(providerReads()).toHaveLength(0);
    expect(toggle().attributes("aria-checked")).toBe("true");
    expect(toggle().attributes("disabled")).toBeDefined();
    const observation = requests.find(
      (item) => item.path === "/system/model-commands/lookup",
    )!;
    expect(observation.key).toBe(original.key);
    expect(JSON.parse(observation.body)).toEqual({
      command: "provider.update",
    });
    putProvider = acceptProvider;
    await click("确认原请求");
    expect(providerWrites()).toHaveLength(2);
    expect(providerWrites()[1]).toEqual(original);
    expect(toggle().attributes("aria-checked")).toBe("false");
    expect(toggle().attributes("disabled")).toBeUndefined();
  });

  it("retains the requested status after a conflict and applies it explicitly to the latest provider", async () => {
    putProvider = () =>
      json({ code: "VERSION_CONFLICT", commit_state: "not_started" }, 409);
    await toggle().trigger("click");
    await flushPromises();
    expect(providerWrites()).toHaveLength(1);
    expect(toggle().attributes("aria-checked")).toBe("true");
    expect(providerReads()).toHaveLength(0);
    canonical = {
      ...provider(),
      version: "5",
      input: {
        ...provider().input,
        name: "Updated Provider",
        base_url: "https://updated.invalid/v1",
        options: { region: "updated" },
      },
    };
    await click("读取最新版本");
    expect(providerReads()).toHaveLength(1);
    expect(providerWrites()).toHaveLength(1);
    expect(button("应用更改").attributes("disabled")).toBeUndefined();
    const latestInput = { ...canonical.input };
    putProvider = acceptProvider;
    await click("应用更改");
    expect(providerWrites()).toHaveLength(2);
    expect(JSON.parse(providerWrites()[1]!.body)).toEqual({
      expected_version: "5",
      input: { ...latestInput, enabled: false },
    });
    expect(providerWrites()[1]!.key).not.toBe(providerWrites()[0]!.key);
    expect(toggle().attributes("aria-checked")).toBe("false");
  });

  it("only rereads after a confirmed status save whose follow-up read failed", async () => {
    readProvider = () =>
      json({ code: "INTERNAL", commit_state: "not_started" }, 500);
    await toggle().trigger("click");
    await flushPromises();
    expect(providerWrites()).toHaveLength(1);
    expect(providerReads()).toHaveLength(1);
    expect(wrapper!.find('[role="alert"]').exists()).toBe(true);
    expect(wrapper!.text()).not.toContain("确认原请求");
    readProvider = () => json(canonical);
    await click("重新读取");
    expect(providerReads()).toHaveLength(2);
    expect(providerWrites()).toHaveLength(1);
    expect(toggle().attributes("aria-checked")).toBe("false");
    expect(toggle().attributes("disabled")).toBeUndefined();
  });
});

describe("inline provider settings", () => {
  let Editor: (typeof import("../src/components/system/ProviderEditor.vue"))["default"];
  beforeEach(async () => {
    Editor = (await import("../src/components/system/ProviderEditor.vue"))
      .default;
    wrapper = mount(Editor, {
      props: { inline: true, open: true, provider: canonical },
      attachTo: document.body,
    });
    await flushPromises();
  });

  describe("saved provider feedback", () => {
    let feedbackEditor: VueWrapper;
    beforeEach(async () => {
      wrapper!.unmount();
      wrapper = mount(
        {
          setup() {
            const currentProvider = shallowRef(canonical);
            return () =>
              h(Editor, {
                open: true,
                inline: true,
                provider: currentProvider.value,
                onSaved: (value: Provider) => {
                  currentProvider.value = value;
                },
              });
          },
        },
        { attachTo: document.body },
      );
      feedbackEditor = wrapper.getComponent(Editor);
      await flushPromises();
    });

    it("edits the endpoint inline without replacing a credential left blank", async () => {
      expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
      expect((field("API 端点").element as HTMLInputElement).value).toBe(
        provider().input.base_url,
      );
      expect(
        (wrapper!.get('input[type="password"]').element as HTMLInputElement)
          .value,
      ).toBe("");
      await field("API 端点").setValue("https://changed.invalid/v1");
      await submit();
      expect(providerWrites()).toHaveLength(1);
      expect(JSON.parse(providerWrites()[0]!.body)).toEqual({
        expected_version: "2",
        input: { ...provider().input, base_url: "https://changed.invalid/v1" },
      });
      expect(secretWrites()).toHaveLength(0);
      expect(requests.some((item) => item.path === credentialPath)).toBe(false);
      expect(feedbackEditor.emitted("saved")).toEqual([[canonical]]);
      const savedProvider = feedbackEditor.emitted("saved")![0]![0] as Provider;
      await flushPromises();
      expect(wrapper!.text()).toContain("供应商已保存。");
      expect((field("API 端点").element as HTMLInputElement).value).toBe(
        "https://changed.invalid/v1",
      );
      await field("供应商名称").setValue("Next Edit Provider");
      await submit();
      expect(providerWrites()).toHaveLength(2);
      expect(JSON.parse(providerWrites()[1]!.body)).toEqual({
        expected_version: "3",
        input: { ...savedProvider.input, name: "Next Edit Provider" },
      });
      expect(secretWrites()).toHaveLength(0);
      await field("供应商名称").setValue("Discarded Draft");
      (button("取消").element as HTMLButtonElement).focus();
      await click("取消");
      (button("放弃编辑").element as HTMLButtonElement).focus();
      await click("放弃编辑");
      expect(document.activeElement).toBe(field("供应商名称").element);
      expect((field("供应商名称").element as HTMLInputElement).value).toBe(
        "Next Edit Provider",
      );
    });
  });

  it("adopts an external provider refresh while clean and saves against its version and credential reference", async () => {
    const refreshed: Provider = {
      ...provider(),
      version: "5",
      input: {
        ...provider().input,
        name: "Refreshed Provider",
        base_url: "https://refreshed.invalid/v1",
        enabled: false,
        credential_ref: id(3),
        options: { region: "refreshed" },
      },
    };
    canonical = refreshed;
    await wrapper!.setProps({ provider: refreshed });
    expect((field("供应商名称").element as HTMLInputElement).value).toBe(
      "Refreshed Provider",
    );
    expect((field("API 端点").element as HTMLInputElement).value).toBe(
      "https://refreshed.invalid/v1",
    );
    expect(
      (wrapper!.get('input[type="password"]').element as HTMLInputElement)
        .value,
    ).toBe("");
    await field("供应商名称").setValue("Edited Refreshed Provider");
    await submit();
    expect(providerWrites()).toHaveLength(1);
    expect(JSON.parse(providerWrites()[0]!.body)).toEqual({
      expected_version: "5",
      input: { ...refreshed.input, name: "Edited Refreshed Provider" },
    });
    expect(secretWrites()).toHaveLength(0);
    expect(
      requests.some((item) =>
        item.path.startsWith("/system/model-credentials"),
      ),
    ).toBe(false);
    expect(wrapper!.emitted("saved")).toEqual([[canonical]]);
  });

  it("preserves a dirty draft and an unknown request across external provider refreshes", async () => {
    await field("供应商名称").setValue("My Draft Provider");
    await field("API 端点").setValue("https://draft.invalid/v1");
    const refreshed: Provider = {
      ...provider(),
      version: "5",
      input: {
        ...provider().input,
        name: "External Provider",
        base_url: "https://external.invalid/v1",
        credential_ref: id(3),
      },
    };
    canonical = refreshed;
    await wrapper!.setProps({ provider: refreshed });
    expect((field("供应商名称").element as HTMLInputElement).value).toBe(
      "My Draft Provider",
    );
    expect((field("API 端点").element as HTMLInputElement).value).toBe(
      "https://draft.invalid/v1",
    );
    putProvider = () => {
      throw new TypeError("Synthetic interrupted response");
    };
    await submit();
    expect(providerWrites()).toHaveLength(1);
    const original = providerWrites()[0]!;
    expect(original.key).toBeTruthy();
    expect(JSON.parse(original.body)).toEqual({
      expected_version: "2",
      input: {
        ...provider().input,
        name: "My Draft Provider",
        base_url: "https://draft.invalid/v1",
      },
    });
    await wrapper!.setProps({
      provider: { ...refreshed, version: "6" },
    });
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    expect((field("供应商名称").element as HTMLInputElement).value).toBe(
      "My Draft Provider",
    );
    expect((field("API 端点").element as HTMLInputElement).value).toBe(
      "https://draft.invalid/v1",
    );
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(providerWrites()).toHaveLength(1);
    putProvider = () =>
      json({ code: "VERSION_CONFLICT", commit_state: "not_started" }, 409);
    await click("确认原请求");
    expect(providerWrites()).toHaveLength(2);
    expect(providerWrites()[1]).toEqual(original);
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    expect((field("供应商名称").element as HTMLInputElement).value).toBe(
      "My Draft Provider",
    );
    expect(secretWrites()).toHaveLength(0);
    expect(wrapper!.emitted("saved")).toBeUndefined();
  });

  it("keeps a confirmed credential update across provider failure and retries only unfinished work", async () => {
    await field("供应商名称").setValue("Changed Provider");
    await wrapper!
      .get('input[type="password"]')
      .setValue("synthetic-new-secret");
    putProvider = () =>
      json({ code: "INVALID_ARGUMENT", commit_state: "not_started" }, 400);
    await submit();
    expect(secretWrites()).toHaveLength(1);
    expect(secretWrites()[0]!.method).toBe("PUT");
    expect(JSON.parse(secretWrites()[0]!.body)).toEqual({
      expected_version: "4",
      value: "synthetic-new-secret",
    });
    expect(providerWrites()).toHaveLength(1);
    expect(wrapper!.text()).toContain("凭据已保存；供应商配置尚未完成");
    expect(
      (wrapper!.get('input[type="password"]').element as HTMLInputElement)
        .value,
    ).toBe("");
    expect(wrapper!.emitted("saved")).toBeUndefined();
    putProvider = acceptProvider;
    readProvider = () =>
      json({ code: "INTERNAL", commit_state: "not_started" }, 500);
    await submit();
    expect(secretWrites()).toHaveLength(1);
    expect(providerWrites()).toHaveLength(2);
    expect(JSON.parse(providerWrites()[1]!.body)).toEqual({
      expected_version: "2",
      input: { ...provider().input, name: "Changed Provider" },
    });
    expect(wrapper!.text()).toContain("保存已完成，最新配置尚未读回");
    expect(wrapper!.emitted("saved")).toBeUndefined();
    readProvider = () => json(canonical);
    await click("重新读取");
    expect(providerReads()).toHaveLength(2);
    expect(providerWrites()).toHaveLength(2);
    expect(secretWrites()).toHaveLength(1);
    expect(wrapper!.emitted("saved")).toEqual([[canonical]]);
  });
});
