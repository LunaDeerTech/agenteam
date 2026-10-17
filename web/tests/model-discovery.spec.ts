import { Blob as NodeBlob } from "node:buffer";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ModelInput } from "../src/system/model-catalog";
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
let dispose: undefined | (() => void);
let wrapper: VueWrapper;
let Fields: (typeof import("../src/components/system/ModelFormFields.vue"))["default"];
let creates: string[];
let importedInputs: ModelInput[];
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
  creates = [];
  importedInputs = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith("/session"))
        return json({
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
        });
      if (url.endsWith("/discovery"))
        return json({
          operation_id: id(1),
          provider_id: id(2),
          provider_version: "1",
          outcome: "succeeded",
          reason: null,
          items: ["__proto__", "rejected", "last"].map((provider_model_id) => ({
            provider_model_id,
          })),
          duration_ms: 1,
          created_at: at,
        });
      const body = JSON.parse(String(init.body));
      creates.push(body.input.provider_model_id);
      importedInputs.push(body.input);
      if (body.input.provider_model_id === "rejected")
        return json(
          { code: "INVALID_ARGUMENT", commit_state: "not_committed" },
          422,
        );
      return json({
        kind: "model.create",
        resource_id: id(creates.length + 10),
        version: "1",
        affected_references: "0",
      });
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const Dialog = (
    await import("../src/components/system/ModelDiscoveryDialog.vue")
  ).default;
  Fields = (await import("../src/components/system/ModelFormFields.vue"))
    .default;
  wrapper = mount(Dialog, {
    props: {
      open: true,
      provider: {
        id: id(2),
        version: "1",
        created_at: at,
        updated_at: at,
        input: {
          name: "Owned",
          protocol: "openai-compatible",
          base_url: "https://example.invalid",
          enabled: true,
          credential_ref: null,
          options: {},
        },
      },
    },
    attachTo: document.body,
    global: { stubs: { UiDialog: { template: "<div><slot /></div>" } } },
  });
  dispose = () => wrapper.unmount();
});
afterEach(() => {
  dispose?.();
  dispose = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.body.replaceChildren();
});
it("keeps successful imports, explicitly skips a failed item, and sends only the remaining model", async () => {
  const click = async (text: string) => {
    const button = wrapper.findAll("button").find((b) => b.text() === text);
    expect(button, text).toBeDefined();
    await button!.trigger("click");
    await flushPromises();
  };
  await click("开始发现");
  for (const input of wrapper.findAll(".discovery-items input"))
    await input.setValue(true);
  await click("配置所选模型");
  for (const field of wrapper.findAllComponents(Fields)) {
    const type = field.get('[role="combobox"]');
    expect(type.attributes("disabled")).toBeUndefined();
    await type.trigger("keydown", { key: "ArrowDown" });
    await flushPromises();
    await type.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(type.text()).toBe("对话（Chat）");
    const input = field
      .findAll("fieldset")
      .find((group) => group.find("legend").text() === "输入模态")!;
    const text = input
      .findAll("label")
      .find((label) => label.text() === "文本")!
      .get('input[type="checkbox"]');
    expect((text.element as HTMLInputElement).checked).toBe(true);
    expect((text.element as HTMLInputElement).disabled).toBe(true);
    await input
      .findAll("label")
      .find((label) => label.text() === "图像")!
      .get('input[type="checkbox"]')
      .setValue(true);
  }
  await click("导入所选模型");
  expect(creates).toEqual(["__proto__", "rejected"]);
  expect(wrapper.text()).toContain("已导入 1 / 3");
  expect(wrapper.text()).toContain("失败");
  expect(wrapper.text()).toContain("未开始");
  await click("跳过此项并继续");
  expect(creates).toEqual(["__proto__", "rejected", "last"]);
  expect(importedInputs.map((input) => input.type)).toEqual([
    "chat",
    "chat",
    "chat",
  ]);
  expect(importedInputs.map((input) => input.notes)).toEqual(["", "", ""]);
  for (const input of importedInputs)
    expect(input.capabilities).toMatchObject({
      input_modalities: ["text", "image"],
      output_modalities: ["text"],
      structured_output_modes: ["text"],
      reasoning_efforts: [],
      reasoning_toggle_mode: null,
    });
  expect(wrapper.text()).toContain("已导入 2 / 3");
  expect(wrapper.text()).toContain("已跳过");
  expect(wrapper.text()).not.toContain("全部成功");
});
