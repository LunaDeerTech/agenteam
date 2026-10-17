import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogModel, Provider } from "../src/system/model-catalog";
const id = (n: number) =>
  `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
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
    name: `Model ${n}`,
    provider_model_id: `upstream-${n}`,
    type: embedding ? "embedding" : "chat",
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
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
    embedding: id(10),
    summary: id(11),
    reranker: null,
    image: null,
  },
});
const selectionPath = "/system/model-selection";
const selectionInput = (version = "4") => ({
  id: id(1),
  expected_version: version,
  embedding: id(12),
  summary: id(11),
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
  const option = wrapper!
    .get('[role="listbox"]')
    .findAll('[role="option"]')
    .find((item) => item.text().includes(label));
  expect(option).toBeTruthy();
  expect(option!.attributes("aria-disabled")).toBeUndefined();
  await option!.trigger("click");
  await flushPromises();
  expect(wrapper!.find('[role="dialog"]').exists()).toBe(false);
  expect(wrapper!.get(`[aria-label^="选择${purpose}"]`).text()).toBe(label);
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
      "选择向量模型（Embedding）",
      "选择总结模型（Chat）",
      "选择重排模型（Rerank）",
      "选择图像生成模型（Image Generation）",
    ]);
    expect(view.text()).toMatch(/Vector Provider\s*\/\s*upstream-10/);
    expect(view.text()).toMatch(/Chat Provider\s*\/\s*upstream-11/);
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(selectionReads()).toHaveLength(1);
    expect(String(selectionReads()[0]![0])).toBe(`/api/v1${selectionPath}`);
    expect(writes).toHaveLength(0);
  });
  it("offers cross-provider models in one list and explains choices that do not match the purpose", async () => {
    const view = await page();
    expect(candidateReads()).toHaveLength(0);
    await view.get('[aria-label^="选择向量模型"]').trigger("click");
    await flushPromises();
    const choices = view.findAll('[role="option"]');
    expect(choices).toHaveLength(2);
    expect(choices[0]!.text()).toContain("Vector Provider/upstream-12");
    expect(choices[1]!.text()).toContain("Chat Provider/upstream-13");
    expect(choices[1]!.attributes("aria-disabled")).toBe("true");
    expect(choices[1]!.text()).toContain("模型类型不适用于此用途");
    await choices[1]!.trigger("click");
    expect(view.get('[aria-label^="选择向量模型"]').text()).toBe(
      "Vector Provider/upstream-10",
    );
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(candidateReads()).toHaveLength(1);
    expect(candidateReads()[0]!.searchParams.has("provider_id")).toBe(false);
    expect(candidateReads()[0]!.searchParams.get("limit")).toBe("10");
    expect(
      fetcher.mock.calls.some(
        ([input]) =>
          new URL(String(input), "https://fixture.test").pathname ===
          "/api/v1/system/model-providers",
      ),
    ).toBe(false);
    await choices[0]!.trigger("click");
    await flushPromises();
    expect(view.find('[role="dialog"]').exists()).toBe(false);
    expect(view.get('[aria-label^="选择向量模型"]').text()).toBe(
      "Vector Provider/upstream-12",
    );
    expect(button("保存").attributes("disabled")).toBeUndefined();
    expect(writes).toHaveLength(0);
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
      expect(view.get('[role="option"]').text()).toContain(
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
      expect(request.searchParams.has("cursor")).toBe(false);
      expect(request.searchParams.has("provider_id")).toBe(false);
      expect(view.get('[role="dialog"]').text()).toContain("第 1 页");
      expect(view.get('[role="option"]').text()).toContain(
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
              embedding: id(10),
              memory: id(11),
              reranker: null,
              image: null,
            },
          })
        : undefined;
    const view = await page();
    expect(view.text()).toContain("读取失败");
    for (const purpose of ["向量模型", "总结模型", "重排模型", "图像生成模型"])
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
    await pick("向量模型", true);
    expect(button("保存").attributes("disabled")).toBeDefined();
    await pick("总结模型");
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(writes[0]!.path).toBe(selectionPath);
    expect(JSON.parse(writes[0]!.body)).toEqual({
      ...selectionInput(),
      summary: id(13),
    });
    expect(wrapper!.text()).toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
  });
  it("saves all changed purposes in one pending request and never repeats it after receipt or read", async () => {
    await page();
    await pick("向量模型", true);
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
      summary: id(13),
    });
    expect(writes[0]!.key).toBeTruthy();
    for (const purpose of ["向量模型", "总结模型", "重排模型", "图像生成模型"])
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
              embedding: id(12),
              summary: id(13),
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
      await pick("向量模型", true);
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
        summary: id(13),
      });
      expect(writes[1]!.key).not.toBe(writes[0]!.key);
      expect(wrapper!.text()).toContain("已保存。");
      expect(button("保存").attributes("disabled")).toBeDefined();
    },
  );
  it("keeps an unknown request frozen after lookup and accepts only its original body and key", async () => {
    await page();
    await pick("向量模型", true);
    await pick("总结模型");
    puts = () => {
      throw new TypeError("Synthetic interrupted response");
    };
    await click("保存");
    expect(writes).toHaveLength(1);
    const original = writes[0]!;
    expect(original.path).toBe(selectionPath);
    expect(original.key).toBeTruthy();
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    expect(wrapper!.text()).toMatch(/Chat Provider\s*\/\s*upstream-13/);
    for (const purpose of ["向量模型", "总结模型", "重排模型", "图像生成模型"])
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
  it("shows missing references and clears drafts on permission loss without logging out", async () => {
    gets = (path) =>
      path === `/system/models/${id(10)}`
        ? json({ code: "NOT_FOUND", commit_state: "not_started" }, 404)
        : undefined;
    await page();
    expect(wrapper!.text()).toContain("当前引用的模型或供应商已不存在");
    await pick("向量模型", true);
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
            configured: { ...selection().configured, image: id(14) },
          })
        : path === `/system/models/${id(14)}`
          ? json(imageModel)
          : path === `/system/model-providers/${id(22)}`
            ? json(imageProvider)
            : undefined;
    await page();
    expect(wrapper!.text()).toContain("当前协议仅支持查看现有用途");
    await pick("向量模型", true);
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(JSON.parse(writes[0]!.body).image).toBe(id(14));
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
});
