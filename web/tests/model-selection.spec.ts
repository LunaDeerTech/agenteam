import { Blob as NodeBlob } from "node:buffer";
import {
  flushPromises,
  mount,
  type DOMWrapper,
  type VueWrapper,
} from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  CatalogModel,
  ModelType,
  Provider,
} from "../src/system/model-catalog";
type PickerProps = InstanceType<
  typeof import("../src/components/system/ModelSelect.vue").default
>["$props"];
// @ts-expect-error Every picker caller must explicitly provide a model type.
const missingKind: Pick<PickerProps, "label" | "issue" | "requiredKind"> = {
  label: "模型",
  issue: () => undefined,
};
void missingKind;
const id = (n: number) =>
  `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const choice = (n: number) => ({
  model_id: id(n),
  reasoning: { mode: "default" as const },
});
const at = "2026-10-16T12:00:00.000000Z";
const identity = () => ({
  user: {
    id: id(90),
    email: "selection@example.test",
    username: "selection-user",
    display_name: "Selection User",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(91),
    issued_at: at,
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "synthetic-selection-context",
});
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
const provider = (embedding = false): Provider => ({
  id: id(embedding ? 20 : 21),
  version: "2",
  created_at: at,
  updated_at: at,
  input: {
    name: embedding ? "Vector Provider" : "Chat Provider",
    protocol: "openai-compatible",
    base_url: "https://owned.invalid/v1",
    enabled: true,
    credential_ref: id(70),
    options: {},
  },
});
const model = (n: number, embedding = false): CatalogModel => ({
  id: id(n),
  provider_id: provider(embedding).id,
  version: "3",
  created_at: at,
  updated_at: at,
  input: {
    notes: `Model ${n}`,
    provider_model_id: `upstream-${n}`,
    type: embedding ? "embedding" : "chat",
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      embedding: embedding
        ? { dimensions: "1536", dimensions_mode: "fixed" }
        : null,
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      reasoning_toggle_mode: null,
      input_modalities: ["text"],
      output_modalities: [embedding ? "vector" : "text"],
      reasoning_efforts: [],
      structured_output_modes: embedding ? [] : ["json_schema"],
      context_length: null,
      max_output: null,
    },
  },
});
const modelOption = (n: number, embedding = false) => {
  const source = provider(embedding);
  return {
    ...model(n, embedding),
    provider: {
      id: source.id,
      name: source.input.name,
      protocol: source.input.protocol,
      enabled: source.input.enabled,
      version: source.version,
    },
  };
};
const selection = (version = "4") => ({
  id: id(1),
  version,
  configured: {
    embedding: choice(10),
    summary: choice(11),
    reranker: null,
    image: null,
  },
});
const selectionPath = "/system/model-selection";
const selectionInput = (version = "4") => ({
  id: id(1),
  expected_version: version,
  embedding: choice(12),
  summary: choice(11),
  reranker: null,
  image: null,
});
const receipt = (resource = id(1), version = "5") => ({
  kind: "model.selection.update",
  resource_id: resource,
  version,
  affected_references: "0",
});
let wrapper: VueWrapper | undefined;
let stopTheme: (() => void) | undefined;
let writes: Array<{ path: string; body: string; key: string | null }>;
let puts: (path: string, body: string) => Response | Promise<Response>;
let gets: (path: string, url: URL) => Response | Promise<Response> | undefined;
let lookup: () => Response;
let fetcher: ReturnType<typeof vi.fn>;
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
  stopTheme = (await import("../src/composables/useTheme")).initTheme();
  writes = [];
  puts = () => json(receipt());
  gets = () => undefined;
  lookup = () => json({ found: true, receipt: receipt() });
  fetcher = vi.fn(
    async (input: RequestInfo | URL, options: RequestInit = {}) => {
      const url = new URL(String(input), "https://fixture.test");
      const path = url.pathname.replace("/api/v1", "");
      if (path === "/session") return json(identity());
      if (path === "/system/model-commands/lookup") return lookup();
      if (options.method === "PUT") {
        const body = String(options.body);
        writes.push({
          path,
          body,
          key: new Headers(options.headers).get("Idempotency-Key"),
        });
        return puts(path, body);
      }
      const response = gets(path, url);
      if (response) return response;
      if (path === "/system/model-selection") return json(selection());
      if (path === "/system/model-providers")
        return json({
          items: [provider(true), provider()],
          next_cursor: "provider-next",
        });
      if (path === "/system/models") {
        return json({
          items: [modelOption(12, true), modelOption(13)],
          next_cursor: null,
        });
      }
      if (path.startsWith("/system/model-providers/"))
        return json(provider(path.endsWith(id(20))));
      if (path.startsWith("/system/models/")) {
        const n = Number(path.slice(-12));
        return json(model(n, n === 10 || n === 12));
      }
      throw new Error(`Unexpected request: ${path}`);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  await import("../src/views/system/ModelSelectionPage.vue");
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  stopTheme?.();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  document.body.replaceChildren();
});
async function page() {
  const Page = (await import("../src/views/system/ModelSelectionPage.vue"))
    .default;
  wrapper = mount(Page, {
    attachTo: document.body,
    global: {
      stubs: {
        SettingsPageActions: { template: "<div><slot /></div>" },
        Teleport: true,
      },
    },
  });
  await flushPromises();
  return wrapper;
}
function button(text: string) {
  const buttons = wrapper!
    .findAll("button")
    .filter((item) => item.text() === text);
  expect(buttons, `Exactly one button ${text}`).toHaveLength(1);
  return buttons[0]!;
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
async function pick(purpose: string, embedding = false) {
  await wrapper!.get(`[aria-label^="选择${purpose}"]`).trigger("click");
  await flushPromises();
  const label = embedding
    ? "Vector Provider/upstream-12"
    : "Chat Provider/upstream-13";
  const option = modelButton(label);
  expect(option.attributes("aria-disabled")).toBeUndefined();
  await option.trigger("click");
  await flushPromises();
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.get(`[aria-label^="选择${purpose}"]`).text()).toBe(label);
}
function modelButton(label: string) {
  const rows = wrapper!
    .get('[role="grid"]')
    .findAll('[role="row"]')
    .filter((item) => item.text().includes(label));
  expect(rows, `Exactly one model row ${label}`).toHaveLength(1);
  return rows[0]!.get('[role="gridcell"] button');
}
async function detailsFor(label: string) {
  const trigger = wrapper!.get('[aria-label^="选择总结模型"]');
  if (trigger.attributes("aria-expanded") !== "true") {
    await trigger.trigger("click");
    await flushPromises();
  }
  await wrapper!.get(`button[aria-label="${label}，更多"]`).trigger("click");
  await flushPromises();
  const panel = wrapper!.get(`[role="dialog"][aria-label="${label}，更多"]`);
  expect(panel.get("legend").text()).toBe("思考策略");
  return panel;
}
function reasoningRadio(
  panel: Pick<DOMWrapper<Element>, "findAll">,
  text: string,
) {
  const labels = panel.findAll("label").filter((item) => item.text() === text);
  expect(labels, `Exactly one reasoning option ${text}`).toHaveLength(1);
  return labels[0]!.get('input[type="radio"]');
}
async function closePicker() {
  const trigger = wrapper!.get('[aria-label^="选择总结模型"]');
  if (trigger.attributes("aria-expanded") === "true")
    await trigger.trigger("click");
  await flushPromises();
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
}
function selectionReads() {
  return fetcher.mock.calls.filter(
    ([input, options]) =>
      String(input).includes("/model-selection") && options?.method !== "PUT",
  );
}
function candidateReads() {
  return fetcher.mock.calls
    .map(([input]) => new URL(String(input), "https://fixture.test"))
    .filter((url) => url.pathname === "/api/v1/system/models");
}
describe("platform model selections", () => {
  it("reads one configuration with four purposes and provider/model identifiers", async () => {
    const view = await page();
    expect(
      view
        .findAll('button[aria-label^="选择"]')
        .map((item) => item.attributes("aria-label")?.split("，")[0]),
    ).toEqual([
      "选择嵌入模型（Embedding）",
      "选择总结模型（Chat）",
      "选择重排模型（ReRank）",
      "选择图像生成模型（Image Generation）",
    ]);
    expect(view.text()).toMatch(/Vector Provider\s*\/\s*upstream-10/);
    expect(view.text()).toMatch(/Chat Provider\s*\/\s*upstream-11/);
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(selectionReads()).toHaveLength(1);
    expect(String(selectionReads()[0]![0])).toBe(`/api/v1${selectionPath}`);
    expect(writes).toHaveLength(0);
  });
  it("requires each purpose's type and hides mismatches while retaining same-type eligibility reasons", async () => {
    const embedding = modelOption(12, true);
    embedding.input.capabilities.context_length = "8000";
    const chat = modelOption(13);
    chat.input.capabilities.context_length = "128000";
    chat.input.capabilities.input_modalities = ["text", "image", "file"];
    const reranker = modelOption(14);
    reranker.input.type = "reranker";
    reranker.input.capabilities.output_modalities = [];
    reranker.input.capabilities.structured_output_modes = [];
    const image = modelOption(15);
    image.input.type = "image_generation";
    image.input.capabilities.output_modalities = ["image"];
    image.input.capabilities.structured_output_modes = [];
    const disabled = modelOption(16);
    disabled.input.capabilities.structured_output_modes = [];
    // Deliberately return mixed types to check the component's defensive filter.
    gets = (path) =>
      path === "/system/models"
        ? json({
            items: [embedding, chat, reranker, image, disabled],
            next_cursor: null,
          })
        : undefined;
    const view = await page();
    for (const [purpose, type, labels, tags] of [
      [
        "嵌入模型",
        "embedding",
        ["Vector Provider/upstream-12"],
        ["嵌入（Embedding）", "1536 维", "8K"],
      ],
      [
        "总结模型",
        "chat",
        ["Chat Provider/upstream-13", "Chat Provider/upstream-16"],
        ["对话（Chat）", "128K", "图片", "文件"],
      ],
      [
        "重排模型",
        "reranker",
        ["Chat Provider/upstream-14"],
        ["重排（ReRank）"],
      ],
      [
        "图像生成模型",
        "image_generation",
        ["Chat Provider/upstream-15"],
        ["图像生成（Image Generation）", "文生图"],
      ],
    ] as const) {
      const trigger = view.get(`[aria-label^="选择${purpose}"]`);
      await trigger.trigger("click");
      await flushPromises();
      const choices = view.get('[role="grid"]').findAll('[role="row"]');
      expect(
        choices.map((row) => row.get(".ui-search-select-name").text()),
      ).toEqual(labels);
      expect(
        choices[0]!
          .findAll(".ui-search-select-tags > span")
          .map((tag) => tag.text()),
      ).toEqual(tags);
      expect(candidateReads().at(-1)!.searchParams.get("type")).toBe(type);
      expect(candidateReads().at(-1)!.searchParams.has("provider_id")).toBe(
        false,
      );
      if (type === "chat") {
        expect(choices[1]!.text()).toContain("总结模型需要 json_schema 能力");
        expect(choices[1]!.get("button").attributes("aria-disabled")).toBe(
          "true",
        );
        await choices[1]!.get("button").trigger("click");
        expect(trigger.text()).toBe("Chat Provider/upstream-11");
      }
      await trigger.trigger("click");
    }
    expect(writes).toHaveLength(0);
  });
  it.each([undefined, "unknown"])(
    "does not issue an unfiltered request for invalid requiredKind %s",
    async (kind) => {
      const Picker = (await import("../src/components/system/ModelSelect.vue"))
        .default;
      wrapper = mount(Picker, {
        attachTo: document.body,
        props: {
          label: "选择模型",
          requiredKind: kind as ModelType,
          issue: () => undefined,
        },
        global: { stubs: { Teleport: true } },
      });
      const trigger = wrapper.get('[aria-label="选择模型"]');
      expect(trigger.attributes("disabled")).toBeDefined();
      await trigger.trigger("click");
      await flushPromises();
      expect(candidateReads()).toHaveLength(0);
    },
  );
  it("retires old requests, pages and details when the required type changes", async () => {
    let finish!: (value: Response) => void;
    gets = (path, url) => {
      if (path !== "/system/models") return undefined;
      if (url.searchParams.has("cursor"))
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      return json({ items: [modelOption(13)], next_cursor: "chat-next" });
    };
    const Picker = (await import("../src/components/system/ModelSelect.vue"))
      .default;
    wrapper = mount(Picker, {
      attachTo: document.body,
      props: {
        label: "选择模型",
        requiredKind: "chat",
        issue: () => undefined,
      },
      global: { stubs: { Teleport: true } },
    });
    await wrapper.get('[aria-label="选择模型"]').trigger("click");
    await flushPromises();
    await wrapper
      .get('button[aria-label="Chat Provider/upstream-13，更多"]')
      .trigger("click");
    await flushPromises();
    expect(wrapper.findAll('[role="dialog"]')).toHaveLength(2);
    await wrapper.setProps({ requiredKind: "embedding" });
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    await wrapper.setProps({ requiredKind: "chat" });
    await wrapper.get('[aria-label="选择模型"]').trigger("click");
    await flushPromises();
    await click("下一页");
    expect(candidateReads().at(-1)!.searchParams.get("cursor")).toBe(
      "chat-next",
    );
    await wrapper.setProps({ requiredKind: "embedding" });
    gets = (path) =>
      path === "/system/models"
        ? json({ items: [], next_cursor: null })
        : undefined;
    await wrapper.get('[aria-label="选择模型"]').trigger("click");
    await flushPromises();
    finish(json({ items: [modelOption(17, true)], next_cursor: "stale-next" }));
    await flushPromises();
    expect(wrapper.text()).toContain("没有匹配的嵌入（Embedding）模型。");
    expect(wrapper.text()).not.toContain("upstream-17");
    expect(button("下一页").attributes("disabled")).toBeDefined();
    expect(candidateReads().at(-1)!.searchParams.get("type")).toBe("embedding");
    expect(candidateReads().at(-1)!.searchParams.has("cursor")).toBe(false);
  });
  it("passes the refreshed original model type to the delete replacement picker", async () => {
    gets = (path) =>
      path.endsWith("/deletion-impact")
        ? json({
            model_id: id(11),
            version: "3",
            reference_count: "1",
            reference_groups: [
              { owner_kind: "platform_selector", role: "summary", count: "1" },
            ],
            replacement_requirement: "required",
            delete_blocker: null,
          })
        : undefined;
    const Dialog = (
      await import("../src/components/system/CatalogDeleteDialog.vue")
    ).default;
    wrapper = mount(Dialog, {
      attachTo: document.body,
      props: { open: false, provider: provider(), model: model(11, true) },
      global: {
        stubs: {
          Teleport: true,
          UiDialog: { template: "<section><slot /></section>" },
        },
      },
    });
    await flushPromises();
    await wrapper.setProps({ open: true });
    await flushPromises();
    await wrapper.get('[aria-label="选择替代模型"]').trigger("click");
    await flushPromises();
    expect(candidateReads().at(-1)!.searchParams.get("type")).toBe("chat");
    expect(wrapper.get('[role="grid"]').text()).toContain(
      "Chat Provider/upstream-13",
    );
    expect(wrapper.get('[role="grid"]').text()).not.toContain(
      "Vector Provider",
    );
  });
  describe("model candidate search", () => {
    let view: VueWrapper;
    beforeEach(async () => {
      gets = (path, url) => {
        if (path !== "/system/models") return undefined;
        return json({
          items: [
            modelOption(
              url.searchParams.has("q")
                ? 16
                : url.searchParams.has("cursor")
                  ? 15
                  : 13,
            ),
          ],
          next_cursor:
            url.searchParams.has("q") || url.searchParams.has("cursor")
              ? null
              : "model-next",
        });
      };
      view = await page();
    });
    it("uses server search and cursor pages while retaining the selected label and resetting pagination for a new query", async () => {
      await view.get('[aria-label^="选择总结模型"]').trigger("click");
      await flushPromises();
      await click("下一页");
      expect(candidateReads()).toHaveLength(2);
      expect(candidateReads()[1]!.searchParams.get("cursor")).toBe(
        "model-next",
      );
      expect(view.get('[role="grid"] [role="row"]').text()).toContain(
        "Chat Provider/upstream-15",
      );
      expect(view.get('[role="dialog"]').text()).toContain("第 2 页");
      expect(view.get('[aria-label^="选择总结模型"]').text()).toBe(
        "Chat Provider/upstream-11",
      );
      vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
      await view.get('input[role="combobox"]').setValue("  Needle  ");
      expect(view.get('[role="dialog"]').text()).toContain("正在读取模型");
      expect(view.get('[role="dialog"]').text()).not.toContain(
        "没有匹配的模型",
      );
      await vi.advanceTimersByTimeAsync(250);
      await flushPromises();
      expect(candidateReads()).toHaveLength(3);
      const request = candidateReads()[2]!;
      expect(request.searchParams.get("q")).toBe("needle");
      expect(
        candidateReads().every(
          (read) => read.searchParams.get("type") === "chat",
        ),
      ).toBe(true);
      expect(request.searchParams.has("cursor")).toBe(false);
      expect(request.searchParams.has("provider_id")).toBe(false);
      expect(view.get('[role="dialog"]').text()).toContain("第 1 页");
      expect(view.get('[role="grid"] [role="row"]').text()).toContain(
        "Chat Provider/upstream-16",
      );
      expect(view.get('[aria-label^="选择总结模型"]').text()).toBe(
        "Chat Provider/upstream-11",
      );
      expect(button("上一页").attributes("disabled")).toBeDefined();
      expect(button("下一页").attributes("disabled")).toBeDefined();
      expect(writes).toHaveLength(0);
    });
  });
  it("rejects the removed memory field without making any purpose editable", async () => {
    gets = (path) =>
      path === selectionPath
        ? json({
            ...selection(),
            configured: {
              embedding: choice(10),
              memory: choice(11),
              reranker: null,
              image: null,
            },
          })
        : undefined;
    const view = await page();
    expect(view.text()).toContain("读取失败");
    for (const purpose of ["嵌入模型", "总结模型", "重排模型", "图像生成模型"])
      expect(
        view.get(`[aria-label^="选择${purpose}"]`).attributes("disabled"),
      ).toBeDefined();
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(selectionReads()).toHaveLength(1);
  });
  it("requires embedding and summary when configuring the resource for the first time", async () => {
    gets = (path) =>
      path === selectionPath
        ? json({ ...selection(), configured: null })
        : undefined;
    await page();
    expect(button("保存").attributes("disabled")).toBeDefined();
    await pick("嵌入模型", true);
    expect(button("保存").attributes("disabled")).toBeDefined();
    await pick("总结模型");
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(writes[0]!.path).toBe(selectionPath);
    expect(JSON.parse(writes[0]!.body)).toEqual({
      ...selectionInput(),
      summary: choice(13),
    });
    expect(wrapper!.text()).toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
  });
  it("saves all changed purposes in one pending request and never repeats it after receipt or read", async () => {
    await page();
    await pick("嵌入模型", true);
    await pick("总结模型");
    let finish!: (value: Response) => void;
    puts = () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      });
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(writes[0]!.path).toBe(selectionPath);
    expect(JSON.parse(writes[0]!.body)).toEqual({
      ...selectionInput(),
      summary: choice(13),
    });
    expect(writes[0]!.key).toBeTruthy();
    for (const purpose of ["嵌入模型", "总结模型", "重排模型", "图像生成模型"])
      expect(
        wrapper!.get(`[aria-label^="选择${purpose}"]`).attributes("disabled"),
      ).toBeDefined();
    for (const action of ["保存", "重新获取", "放弃修改"])
      expect(button(action).attributes("disabled")).toBeDefined();
    finish(json(receipt()));
    await flushPromises();
    expect(wrapper!.text()).toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(writes).toHaveLength(1);
    expect(selectionReads()).toHaveLength(1);
    gets = (path) =>
      path === selectionPath
        ? json({
            ...selection("5"),
            configured: {
              embedding: choice(12),
              summary: choice(13),
              reranker: null,
              image: null,
            },
          })
        : undefined;
    await click("重新获取");
    expect(selectionReads()).toHaveLength(2);
    expect(writes).toHaveLength(1);
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(wrapper!.text()).toMatch(/Vector Provider\s*\/\s*upstream-12/);
    expect(wrapper!.text()).toMatch(/Chat Provider\s*\/\s*upstream-13/);
  });
  it.each([
    { code: "INVALID_ARGUMENT", status: 400 },
    { code: "VERSION_CONFLICT", status: 409 },
  ])(
    "preserves the whole draft after $code and adopts a fresh version only explicitly",
    async ({ code, status }) => {
      await page();
      await pick("嵌入模型", true);
      await pick("总结模型");
      puts = () => json({ code, commit_state: "not_started" }, status);
      await click("保存");
      expect(writes).toHaveLength(1);
      expect(wrapper!.text()).toMatch(/Vector Provider\s*\/\s*upstream-12/);
      expect(wrapper!.text()).toMatch(/Chat Provider\s*\/\s*upstream-13/);
      expect(wrapper!.find('[role="alert"]').exists()).toBe(true);
      expect(wrapper!.text()).not.toContain("已保存。");
      if (status === 409) {
        gets = (path) =>
          path === selectionPath ? json(selection("7")) : undefined;
        expect(button("保存").attributes("disabled")).toBeDefined();
        await click("重新获取");
        expect(writes).toHaveLength(1);
        expect(wrapper!.text()).toMatch(/Vector Provider\s*\/\s*upstream-12/);
        expect(wrapper!.text()).toMatch(/Chat Provider\s*\/\s*upstream-13/);
        expect(button("保存").attributes("disabled")).toBeDefined();
        await click("采用最新版本并保留草稿");
      }
      const version = status === 409 ? "7" : "4";
      puts = () => json(receipt(id(1), String(Number(version) + 1)));
      await click("保存");
      expect(writes).toHaveLength(2);
      expect(writes[1]!.path).toBe(selectionPath);
      expect(JSON.parse(writes[1]!.body)).toEqual({
        ...selectionInput(version),
        summary: choice(13),
      });
      expect(writes[1]!.key).not.toBe(writes[0]!.key);
      expect(wrapper!.text()).toContain("已保存。");
      expect(button("保存").attributes("disabled")).toBeDefined();
    },
  );
  it("keeps an unknown request frozen after lookup and accepts only its original body and key", async () => {
    const effortModel = modelOption(13);
    effortModel.input.capabilities.reasoning = true;
    effortModel.input.capabilities.reasoning_efforts = [
      { name: "快速", value: "budget.low" },
      { name: "深入", value: "vendor.high:2" },
    ];
    gets = (path) =>
      path === "/system/models"
        ? json({
            items: [modelOption(12, true), effortModel],
            next_cursor: null,
          })
        : undefined;
    await page();
    await pick("嵌入模型", true);
    const details = await detailsFor("Chat Provider/upstream-13");
    expect(details.findAll("label").map((item) => item.text())).toEqual([
      "供应商默认",
      "快速",
      "深入",
    ]);
    await reasoningRadio(details, "深入").setValue(true);
    expect(writes).toHaveLength(0);
    await closePicker();
    puts = () => {
      throw new TypeError("Synthetic interrupted response");
    };
    await click("保存");
    expect(writes).toHaveLength(1);
    const original = writes[0]!;
    expect(original.path).toBe(selectionPath);
    expect(original.key).toBeTruthy();
    expect(JSON.parse(original.body).summary).toEqual({
      model_id: id(13),
      reasoning: { mode: "effort", value: "vendor.high:2" },
    });
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    expect(wrapper!.text()).toMatch(/Chat Provider\s*\/\s*upstream-13/);
    for (const purpose of ["嵌入模型", "总结模型", "重排模型", "图像生成模型"])
      expect(
        wrapper!.get(`[aria-label^="选择${purpose}"]`).attributes("disabled"),
      ).toBeDefined();
    for (const action of ["保存", "重新获取", "放弃修改"])
      expect(button(action).attributes("disabled")).toBeDefined();
    await click("查询原操作");
    expect(writes).toHaveLength(1);
    expect(wrapper!.text()).toContain("已找到历史回执");
    expect(wrapper!.text()).not.toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
    const observations = fetcher.mock.calls.filter(([input]) =>
      String(input).includes("/model-commands/lookup"),
    );
    expect(observations).toHaveLength(1);
    expect(
      new Headers(observations[0]![1]!.headers).get("Idempotency-Key"),
    ).toBe(original.key);
    expect(JSON.parse(String(observations[0]![1]!.body))).toEqual({
      command: "model.selection.update",
    });
    puts = () => json(receipt());
    await click("确认原请求");
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual(original);
    expect(wrapper!.text()).toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(selectionReads()).toHaveLength(1);
  });
  describe("reasoning choices", () => {
    beforeEach(async () => {
      const toggle = modelOption(11);
      toggle.input.capabilities.reasoning = true;
      toggle.input.capabilities.reasoning_toggle_mode = "enable_thinking";
      const unmapped = modelOption(14);
      unmapped.input.notes = "供应商未声明可用的开关格式。";
      unmapped.input.capabilities.reasoning = true;
      gets = (path) => {
        if (path === "/system/models")
          return json({ items: [toggle, unmapped], next_cursor: null });
        if (path === `/system/models/${id(11)}`)
          return json({ ...model(11), input: toggle.input });
        return undefined;
      };
      await page();
    });

    it("saves a changed reasoning policy on the same model and resets it only when selecting another model", async () => {
      expect(button("保存").attributes("disabled")).toBeDefined();
      const details = await detailsFor("Chat Provider/upstream-11");
      expect(details.findAll("label").map((item) => item.text())).toEqual([
        "供应商默认",
        "开启思考",
        "关闭思考",
      ]);
      expect(
        (reasoningRadio(details, "供应商默认").element as HTMLInputElement)
          .checked,
      ).toBe(true);
      await reasoningRadio(details, "关闭思考").setValue(true);
      expect(writes).toHaveLength(0);
      expect(button("保存").attributes("disabled")).toBeUndefined();
      await details.trigger("keydown", { key: "Escape" });
      await flushPromises();
      await modelButton("Chat Provider/upstream-11").trigger("click");
      await flushPromises();
      const reopened = await detailsFor("Chat Provider/upstream-11");
      expect(
        (reasoningRadio(reopened, "关闭思考").element as HTMLInputElement)
          .checked,
      ).toBe(true);
      await closePicker();
      await click("保存");
      expect(writes).toHaveLength(1);
      expect(JSON.parse(writes[0]!.body)).toEqual({
        ...selectionInput(),
        embedding: choice(10),
        summary: { model_id: id(11), reasoning: { mode: "off" } },
      });
      expect(button("保存").attributes("disabled")).toBeDefined();

      await wrapper!.get('[aria-label^="选择总结模型"]').trigger("click");
      await flushPromises();
      await modelButton("Chat Provider/upstream-14").trigger("click");
      await flushPromises();
      const changed = await detailsFor("Chat Provider/upstream-14");
      expect(
        (reasoningRadio(changed, "供应商默认").element as HTMLInputElement)
          .checked,
      ).toBe(true);
      expect(writes).toHaveLength(1);
      await closePicker();
      puts = () => json(receipt(id(1), "6"));
      await click("保存");
      expect(writes).toHaveLength(2);
      expect(JSON.parse(writes[1]!.body)).toEqual({
        ...selectionInput("5"),
        embedding: choice(10),
        summary: choice(14),
      });
    });

    it("offers only provider default without a declared toggle mapping and saves that exact choice", async () => {
      const details = await detailsFor("Chat Provider/upstream-14");
      expect(details.text()).toContain("供应商未声明可用的开关格式。");
      expect(details.text()).toContain("此模型尚未配置思考开关格式");
      expect(details.findAll("label").map((item) => item.text())).toEqual([
        "供应商默认",
      ]);
      expect(details.find('[role="switch"]').exists()).toBe(false);
      await details.trigger("keydown", { key: "Escape" });
      await flushPromises();
      await modelButton("Chat Provider/upstream-14").trigger("click");
      await flushPromises();
      expect(writes).toHaveLength(0);
      await closePicker();
      await click("保存");
      expect(writes).toHaveLength(1);
      expect(JSON.parse(writes[0]!.body).summary).toEqual(choice(14));
    });
  });
  it("shows missing references and clears drafts on permission loss without logging out", async () => {
    gets = (path) =>
      path === `/system/models/${id(10)}`
        ? json({ code: "NOT_FOUND", commit_state: "not_started" }, 404)
        : undefined;
    await page();
    expect(wrapper!.text()).toContain("当前引用的模型或供应商已不存在");
    await pick("嵌入模型", true);
    puts = () => json({ code: "FORBIDDEN", commit_state: "not_started" }, 403);
    await click("保存");
    expect(wrapper!.text()).toContain("页面数据已清除");
    expect(wrapper!.text()).not.toContain("upstream-12");
    expect(wrapper!.text()).not.toContain("确认原请求");
    expect(
      (await import("../src/account/session")).useSession().user.value,
    ).not.toBeNull();
  });
  it("binds the selection receipt to its ID, successor version, kind and zero references", async () => {
    const { saveModelSelection } =
      await import("../src/system/model-selection");
    for (const wrong of [
      receipt(id(2)),
      receipt(id(1), "4"),
      { ...receipt(), kind: "model.update" },
      { ...receipt(), affected_references: "1" },
    ]) {
      puts = () => json(wrong);
      const operation = saveModelSelection(selectionInput());
      await expect(operation.command.run()).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      expect(operation.command.confirmed).toBe(false);
      operation.command.release();
    }
  });
  it("requires chat with json_schema for summary and reports disabled references", async () => {
    const { modelPurposeIssue } = await import("../src/system/model-selection");
    const chat = model(11);
    expect(modelPurposeIssue("summary", chat, provider())).toBeUndefined();
    chat.input.capabilities.structured_output_modes = [];
    expect(modelPurposeIssue("summary", chat, provider())).toContain(
      "json_schema",
    );
    chat.input.capabilities.structured_output_modes = ["json_schema"];
    chat.input.enabled = false;
    expect(modelPurposeIssue("summary", chat, provider())).toContain("停用");
    expect(
      modelPurposeIssue("summary", model(10, true), provider(true)),
    ).toContain("类型");
  });
  it("keeps legacy image references unchanged while rejecting them as new choices", async () => {
    const imageProvider = provider();
    imageProvider.id = id(22);
    imageProvider.input.protocol = "openai-images-generations";
    const imageModel = model(14);
    imageModel.provider_id = imageProvider.id;
    imageModel.input.type = "image_generation";
    imageModel.input.capabilities.output_modalities = ["image"];
    imageModel.input.capabilities.structured_output_modes = [];
    const { modelPurposeIssue } = await import("../src/system/model-selection");
    expect(modelPurposeIssue("image", imageModel, imageProvider)).toContain(
      "不能新增选择",
    );
    gets = (path) =>
      path === selectionPath
        ? json({
            ...selection(),
            configured: { ...selection().configured, image: choice(14) },
          })
        : path === `/system/models/${id(14)}`
          ? json(imageModel)
          : path === `/system/model-providers/${id(22)}`
            ? json(imageProvider)
            : undefined;
    await page();
    expect(wrapper!.text()).toContain("当前协议仅支持查看现有用途");
    await pick("嵌入模型", true);
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(JSON.parse(writes[0]!.body).image).toEqual(choice(14));
    await wrapper!
      .get('[aria-label="清除图像生成模型（Image Generation）"]')
      .trigger("click");
    puts = () => json(receipt(id(1), "6"));
    await click("保存");
    expect(JSON.parse(writes[1]!.body)).toMatchObject({
      expected_version: "5",
      image: null,
    });
  });
  it("requires text to vector capabilities for embedding choices", async () => {
    const { modelPurposeIssue } = await import("../src/system/model-selection");
    const embedding = model(10, true);
    embedding.input.capabilities.output_modalities = ["text"];
    expect(modelPurposeIssue("embedding", embedding, provider(true))).toContain(
      "vector",
    );
  });
  it.each([
    {
      type: "reranker",
      purpose: "reranker",
      output: [],
      legacy: "jina-rerank",
      issue: "重排模型需要查询与文档排序能力",
    },
    {
      type: "image_generation",
      purpose: "image",
      output: ["image"],
      legacy: "openai-images-generations",
      issue: "图像生成模型需要文本生成图像能力",
    },
  ] as const)(
    "accepts compatible $type choices but rejects unsuitable capabilities and legacy protocols",
    async ({ type, purpose, output, legacy, issue }) => {
      const { modelPurposeIssue } =
        await import("../src/system/model-selection");
      const candidate = model(14);
      candidate.input.type = type;
      candidate.input.capabilities.output_modalities = [...output];
      candidate.input.capabilities.structured_output_modes = [];
      const source = provider();
      expect(modelPurposeIssue(purpose, candidate, source)).toBeUndefined();

      candidate.input.capabilities.output_modalities = ["vector"];
      expect(modelPurposeIssue(purpose, candidate, source)).toBe(issue);
      candidate.input.capabilities.output_modalities = [...output];
      candidate.input.capabilities.reasoning = true;
      candidate.input.capabilities.reasoning_efforts = [
        { name: "深入", value: "high" },
      ];
      candidate.input.capabilities.context_length = "128000";
      expect(modelPurposeIssue(purpose, candidate, source)).toBe(issue);

      candidate.input.capabilities.reasoning = false;
      candidate.input.capabilities.reasoning_efforts = [];
      candidate.input.capabilities.context_length = null;
      source.input.protocol = legacy;
      expect(modelPurposeIssue(purpose, candidate, source)).toContain(
        "不能新增选择",
      );
    },
  );
});
