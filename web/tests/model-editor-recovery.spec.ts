import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { defineComponent, h, shallowRef } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
    name: "Recovery Provider",
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
    notes: "Recovery Model",
    provider_model_id: "upstream-original",
    type: "chat",
    enabled: false,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      embedding: null,
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: true,
      reasoning: false,
      reasoning_toggle_mode: null,
      input_modalities: ["text"],
      output_modalities: ["text"],
      reasoning_efforts: [],
      structured_output_modes: [],
      context_length: null,
      max_output: null,
    },
  },
});
const identity = {
  user: {
    id: id(90),
    email: "model-recovery@example.test",
    username: "model-recovery",
    display_name: "Model Recovery",
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
  csrf_token: "synthetic-model-recovery-context",
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
let wrapper: VueWrapper | undefined;
let canonical: CatalogModel;
let requests: { path: string; method: string; body: string }[];
let readModel: () => Response | Promise<Response>;
let readList: () => Response | Promise<Response>;
let leave: typeof import("../src/account/leave");
const writes = () => requests.filter((item) => item.method === "PUT");
const modelReads = () =>
  requests.filter((item) => item.path === modelPath && item.method === "GET");
const listReads = () =>
  requests.filter(
    (item) => item.path === "/system/models" && item.method === "GET",
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
const dialogStub = {
  props: ["open", "title"],
  template:
    '<section v-if="open" role="dialog" :aria-label="title"><slot /><slot name="footer" /></section>',
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
  localStorage.clear();
  canonical = model();
  requests = [];
  readModel = () => json(canonical);
  readList = () => {
    const owner = provider();
    return json({
      items: [
        {
          ...model(),
          provider: {
            id: owner.id,
            name: owner.input.name,
            protocol: owner.input.protocol,
            enabled: owner.input.enabled,
            version: owner.version,
          },
        },
      ],
      next_cursor: null,
    });
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const path = new URL(
        String(input),
        "https://fixture.test",
      ).pathname.replace("/api/v1", "");
      const method = init.method ?? "GET";
      const body = String(init.body ?? "");
      requests.push({ path, method, body });
      if (path === "/session") return json(identity);
      if (path === `/system/model-providers/${id(1)}` && method === "GET")
        return json(provider());
      if (path === "/system/model-capabilities")
        return json({
          protocols: [
            {
              protocol: "openai-compatible",
              model_types: ["chat"],
              discovery: true,
              test: true,
            },
          ],
        });
      if (path === "/system/models" && method === "GET") return readList();
      if (path === modelPath && method === "GET") return readModel();
      if (path === modelPath && method === "PUT") {
        const value = JSON.parse(body) as {
          expected_version: string;
          input: CatalogModel["input"];
        };
        canonical = {
          ...canonical,
          input: value.input,
          version: String(BigInt(value.expected_version) + 1n),
        };
        return json({
          kind: "model.update",
          resource_id: canonical.id,
          version: canonical.version,
          affected_references: "0",
        });
      }
      throw new Error(`Unexpected ${method} ${path}`);
    }),
  );
  await (await import("../src/account/session")).refreshSession();
  leave = await import("../src/account/leave");
});
afterEach(() => {
  leave?.finishAccountLeave(false);
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.body.replaceChildren();
});

describe("confirmed model editor recovery", () => {
  let editor: Pick<VueWrapper, "emitted">;
  beforeEach(async () => {
    const Editor = (
      await import("../src/components/system/ModelEditorDialog.vue")
    ).default;
    const LeaveConfirmation = (
      await import("../src/components/account/LeaveConfirmation.vue")
    ).default;
    wrapper = mount(
      {
        render: () => [
          h(Editor, { open: true, provider: provider(), model: model() }),
          h(LeaveConfirmation),
        ],
      },
      {
        attachTo: document.body,
        global: { stubs: { UiDialog: dialogStub } },
      },
    );
    editor = wrapper.getComponent(Editor);
    await flushPromises();
  });

  it("protects a confirmed save awaiting readback and preserves it when leaving is cancelled", async () => {
    const type = wrapper!.get('[role="combobox"]');
    expect(
      wrapper!.get(`[id="${type.attributes("aria-labelledby")}"]`).text(),
    ).toBe("模型类型");
    expect(type.attributes("disabled")).toBeDefined();
    await type.trigger("keydown", { key: "ArrowDown" });
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    const label = wrapper!
      .findAll("label")
      .find((item) => item.text() === "备注（可选）")!;
    await label.get("textarea").setValue("");
    readModel = () => json({ code: "INTERNAL" }, 500);
    await wrapper!.get("form").trigger("submit");
    await flushPromises();
    expect(writes()).toHaveLength(1);
    expect(JSON.parse(writes()[0]!.body).input).toMatchObject({
      type: "chat",
      notes: "",
    });
    expect(JSON.parse(writes()[0]!.body).input).not.toHaveProperty("name");
    expect(modelReads()).toHaveLength(1);
    expect(wrapper!.text()).toContain("保存已完成，最新模型尚未读回。");
    expect(button("完成").attributes("disabled")).toBeDefined();
    expect(editor.emitted("saved")).toBeUndefined();

    const leaving = leave.confirmAccountLeave();
    await flushPromises();
    expect(leave.leaveDialogOpen.value).toBe(true);
    expect(
      wrapper!.find('[role="dialog"][aria-label="放弃当前编辑？"]').exists(),
    ).toBe(true);
    await click("继续编辑");
    await expect(leaving).resolves.toBe(false);
    expect(leave.leaveDialogOpen.value).toBe(false);
    expect(wrapper!.text()).toContain("保存已完成，最新模型尚未读回。");
    expect(editor.emitted("update:open")).toBeUndefined();
    expect(wrapper!.find("form").exists()).toBe(false);

    readModel = () => json(canonical);
    await click("重新读取");
    expect(modelReads()).toHaveLength(2);
    expect(writes()).toHaveLength(1);
    expect(editor.emitted("saved")).toEqual([[canonical]]);
    expect(wrapper!.text()).toContain("模型已保存。");
    await expect(leave.confirmAccountLeave()).resolves.toBe(true);
    expect(leave.leaveDialogOpen.value).toBe(false);
    await click("完成");
    expect(editor.emitted("update:open")).toEqual([[false]]);
  });
});

describe("model editor result in the provider list", () => {
  const EditorStub = defineComponent({
    props: ["open", "provider", "model"],
    emits: ["saved", "update:open"],
    setup(props, { emit }) {
      return () =>
        props.open
          ? h("section", { role: "dialog", "aria-label": "编辑模型" }, [
              h(
                "button",
                {
                  onClick: () => {
                    emit("saved", canonical);
                    emit("update:open", false);
                  },
                },
                "保存确认结果",
              ),
            ])
          : null;
    },
  });
  const TestStub = defineComponent({
    props: ["open", "provider", "model"],
    template:
      '<section v-if="open" role="dialog" aria-label="模型测试"><p>{{ model.input.provider_model_id }} / 版本 {{ model.version }}</p></section>',
  });
  beforeEach(async () => {
    const Page = (await import("../src/views/system/ProviderDetailPage.vue"))
      .default;
    const { createMemoryHistory, createRouter } = await import("vue-router");
    const { settingsHeading } = await import("../src/layouts/settingsHeading");
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/system/models/providers/:providerId", component: Page },
      ],
    });
    await router.push(`/system/models/providers/${id(1)}`);
    await router.isReady();
    wrapper = mount(Page, {
      attachTo: document.body,
      global: {
        plugins: [router],
        provide: { [settingsHeading as symbol]: shallowRef() },
        stubs: {
          UiDialog: dialogStub,
          SettingsPageActions: { template: "<div><slot /></div>" },
          ModelEditorDialog: EditorStub,
          ModelTestDialog: TestStub,
        },
      },
    });
    await flushPromises();
  });

  it("keeps the saved model in its row when refresh fails and tests the confirmed version", async () => {
    expect(listReads()).toHaveLength(1);
    expect(
      wrapper!.get('table[aria-label="供应商模型"] tbody').text(),
    ).toContain("upstream-original");
    expect(wrapper!.get('[role="switch"]').attributes("aria-checked")).toBe(
      "false",
    );
    expect(button("测试").attributes("disabled")).toBeDefined();
    await click("编辑");
    canonical = {
      ...model(),
      version: "4",
      input: {
        ...model().input,
        enabled: true,
        provider_model_id: "upstream-saved",
      },
    };
    let finish!: (response: Response) => void;
    readList = () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      });
    await click("保存确认结果");
    expect(listReads()).toHaveLength(2);
    expect(
      wrapper!.get('table[aria-label="供应商模型"] tbody').text(),
    ).toContain("upstream-saved");
    expect(wrapper!.get('[role="switch"]').attributes("aria-checked")).toBe(
      "true",
    );
    finish(json({ code: "INTERNAL" }, 500));
    await flushPromises();
    expect(wrapper!.find('[role="alert"]').exists()).toBe(true);
    expect(
      wrapper!.get('table[aria-label="供应商模型"] tbody').text(),
    ).toContain("upstream-saved");
    expect(wrapper!.get('[role="switch"]').attributes("aria-checked")).toBe(
      "true",
    );
    await click("测试");
    const testDialog = wrapper!.getComponent(TestStub);
    expect(testDialog.props("open")).toBe(true);
    expect(testDialog.props("model")).toMatchObject(canonical);
    expect(
      wrapper!.get('[role="dialog"][aria-label="模型测试"]').text(),
    ).toContain("版本 4");
    expect(writes()).toHaveLength(0);
    expect(modelReads()).toHaveLength(0);
  });
});
