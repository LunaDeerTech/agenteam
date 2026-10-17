import { Blob as NodeBlob } from "node:buffer";
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const id = (n: number) =>
  `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T01:00:00.000000Z";
const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
    status,
    headers: {
      "Content-Type":
        status < 400 ? "application/json" : "application/problem+json",
    },
  });
const identity = {
  user: {
    id: id(90),
    email: "test@example.invalid",
    username: "test",
    display_name: "Test",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(91),
    issued_at: at,
    absolute_expires_at: "2026-10-18T01:00:00.000000Z",
    idle_expires_at: "2026-10-17T12:00:00.000000Z",
  },
  csrf_token: "synthetic",
};
const provider = {
  id: id(2),
  version: "2",
  created_at: at,
  updated_at: at,
  input: {
    name: "Owned",
    protocol: "openai-compatible" as const,
    base_url: "https://example.invalid",
    enabled: true,
    credential_ref: id(3),
    options: {},
  },
};
let dispose: undefined | (() => void);
beforeEach(() => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  localStorage.clear();
});
afterEach(() => {
  dispose?.();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});
it("does not create a confirmed credential again when provider association conflicts", async () => {
  let credentials = 0;
  let providers = 0;
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith("/session")) return json(identity);
      if (url.endsWith("/model-credentials")) {
        credentials++;
        return json({
          credential_id: id(3),
          purpose: "model",
          version: "1",
          deleted: false,
        });
      }
      if (url.endsWith("/model-providers") && init.method === "POST") {
        providers++;
        writes.push(JSON.parse(String(init.body)));
        if (providers === 1)
          return json(
            { code: "RESOURCE_BUSY", commit_state: "not_committed" },
            409,
          );
        return json({
          kind: "provider.create",
          resource_id: id(2),
          version: "1",
          affected_references: "0",
        });
      }
      return json({ ...provider, version: "1" });
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const Dialog = (
    await import("../src/components/system/ProviderEditorDialog.vue")
  ).default;
  const Input = (await import("../src/components/ui/UiInput.vue")).default;
  const wrapper = mount(Dialog, {
    props: { open: true },
    global: { stubs: { UiDialog: { template: "<div><slot /></div>" } } },
  });
  dispose = () => wrapper.unmount();
  async function fill(label: string, value: string) {
    await wrapper
      .findAllComponents(Input)
      .find((c) => c.props("label") === label)!
      .get("input")
      .setValue(value);
  }
  await fill("供应商名称", "Owned");
  await fill("API 端点", "https://example.invalid");
  await fill("API 凭据（可选）", "synthetic-not-a-real-secret");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(wrapper.text()).toContain("凭据已保存");
  expect(credentials).toBe(1);
  expect(providers).toBe(1);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(credentials).toBe(1);
  expect(providers).toBe(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(wrapper.text()).toContain("供应商已保存");
});

it.each(["not_started", "not_committed"])(
  "requires an explicit metadata refresh after a %s credential version conflict",
  async (commitState) => {
    let metadataReads = 0;
    const secretVersions: string[] = [];
    let providerWrites = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit) => {
        if (url.endsWith("/session")) return json(identity);
        if (
          url.endsWith(`/model-credentials/${id(3)}`) &&
          init.method === "GET"
        ) {
          metadataReads++;
          return json({
            credential_id: id(3),
            purpose: "model",
            version: metadataReads === 1 ? "2" : "3",
          });
        }
        if (url.endsWith(`/model-credentials/${id(3)}`)) {
          secretVersions.push(JSON.parse(String(init.body)).expected_version);
          if (secretVersions.length === 1)
            return json(
              { code: "VERSION_CONFLICT", commit_state: commitState },
              409,
            );
          return json({
            credential_id: id(3),
            purpose: "model",
            version: "4",
            deleted: false,
          });
        }
        if (init.method === "PUT") {
          providerWrites++;
          return json({
            kind: "provider.update",
            resource_id: id(2),
            version: "3",
            affected_references: "0",
          });
        }
        return json({ ...provider, version: "3" });
      }),
    );
    await (await import("../src/account/session")).useSession().refresh();
    const Dialog = (
      await import("../src/components/system/ProviderEditorDialog.vue")
    ).default;
    const Input = (await import("../src/components/ui/UiInput.vue")).default;
    const wrapper = mount(Dialog, {
      props: { open: true, provider },
      global: { stubs: { UiDialog: { template: "<div><slot /></div>" } } },
    });
    dispose = () => wrapper.unmount();
    const secretInput = wrapper
      .findAllComponents(Input)
      .find((c) => c.props("label") === "API 凭据（可选）")!
      .get("input");
    const nameInput = wrapper
      .findAllComponents(Input)
      .find((c) => c.props("label") === "供应商名称")!
      .get("input");
    await nameInput.setValue("Preserved non-secret draft");
    await secretInput.setValue("synthetic-not-a-real-secret");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(secretVersions).toEqual(["2"]);
    expect(providerWrites).toBe(0);
    expect(secretInput.element.value).toBe("");
    expect(secretInput.element.disabled).toBe(true);
    expect(nameInput.element.value).toBe("Preserved non-secret draft");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(secretVersions).toEqual(["2"]);
    expect(metadataReads).toBe(1);
    const refresh = wrapper
      .findAll("button")
      .find((b) => b.text() === "读取凭据最新版本");
    expect(refresh).toBeDefined();
    await refresh!.trigger("click");
    await flushPromises();
    expect(metadataReads).toBe(2);
    expect(secretInput.element.value).toBe("");
    expect(secretInput.element.disabled).toBe(false);
    await secretInput.setValue("synthetic-reentered-secret");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(secretVersions).toEqual(["2", "3"]);
    expect(providerWrites).toBe(1);
    expect(metadataReads).toBe(2);
  },
);

it("clears the secret field while an unknown write retains its original body for explicit recovery", async () => {
  const writes: { body: unknown; key: string | null }[] = [];
  let providerWrites = 0;
  const receipt = {
    credential_id: id(3),
    purpose: "model",
    version: "3",
    deleted: false,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith("/session")) return json(identity);
      if (url.endsWith("/model-credential-commands/lookup"))
        return json({ observed: true, result: receipt });
      if (url.endsWith(`/model-credentials/${id(3)}`)) {
        if (init.method === "GET")
          return json({ credential_id: id(3), purpose: "model", version: "2" });
        writes.push({
          body: init.body,
          key: new Headers(init.headers).get("Idempotency-Key"),
        });
        if (writes.length === 1)
          throw new TypeError("controlled lost response");
        return json(receipt);
      }
      if (init.method === "PUT") {
        providerWrites++;
        return json({
          kind: "provider.update",
          resource_id: id(2),
          version: "3",
          affected_references: "0",
        });
      }
      return json({ ...provider, version: "3" });
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const Dialog = (
    await import("../src/components/system/ProviderEditorDialog.vue")
  ).default;
  const Input = (await import("../src/components/ui/UiInput.vue")).default;
  const wrapper = mount(Dialog, {
    props: { open: true, provider },
    global: { stubs: { UiDialog: { template: "<div><slot /></div>" } } },
  });
  dispose = () => wrapper.unmount();
  const secretInput = wrapper
    .findAllComponents(Input)
    .find((c) => c.props("label") === "API 凭据（可选）")!
    .get("input");
  await secretInput.setValue("synthetic-original-secret");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(secretInput.element.value).toBe("");
  expect(wrapper.text()).toContain("原操作结果尚未确认");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "查询原操作")!
    .trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("已找到历史回执");
  expect(writes).toHaveLength(1);
  expect(providerWrites).toBe(0);
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "确认原请求")!
    .trigger("click");
  await flushPromises();
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(String(writes[1]!.body))).toEqual({
    expected_version: "2",
    value: "synthetic-original-secret",
  });
  expect(providerWrites).toBe(1);
  expect(wrapper.text()).toContain("供应商已保存");
});
