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
const selection = (version = "4") => ({
  id: id(1),
  version,
  configured: {
    embedding: id(10),
    memory: id(11),
    reranker: null,
    image: null,
  },
});
const summary = (version = "8") => ({ id: id(2), version, model: id(11) });
const platformPath = "/system/model-selection";
const summaryPath = `${platformPath}/meeting-summary`;
const platformInput = (version = "4") => ({
  id: id(1),
  expected_version: version,
  embedding: id(12),
  memory: id(11),
  reranker: null,
  image: null,
});
const summaryInput = (version = "8") => ({
  id: id(2),
  expected_version: version,
  model: id(13),
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
let gets: (path: string) => Response | Promise<Response> | undefined;
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
  puts = (path) =>
    json(path.endsWith("meeting-summary") ? receipt(id(2), "9") : receipt());
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
      const response = gets(path);
      if (response) return response;
      if (path === "/system/model-selection") return json(selection());
      if (path === "/system/model-selection/meeting-summary")
        return json(summary());
      if (path === "/system/model-providers")
        return json({
          items: [provider(true), provider()],
          next_cursor: "provider-next",
        });
      if (path === "/system/models") {
        const embedding =
          url.searchParams.get("provider_id") === provider(true).id;
        return json({
          items: [model(embedding ? 12 : 13, embedding)],
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
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});
async function page() {
  const Page = (await import("../src/views/system/ModelSelectionPage.vue"))
    .default;
  wrapper = mount(Page, {
    global: {
      stubs: {
        UiDialog: {
          props: ["open", "title"],
          template:
            '<section v-if="open" role="dialog" :aria-label="title"><slot /></section>',
        },
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
  await wrapper!.get(`[aria-label="选择${purpose}"]`).trigger("click");
  await flushPromises();
  const row = wrapper!
    .get('[role="dialog"]')
    .findAll("li")
    .find((item) =>
      item.text().includes(embedding ? "Vector Provider" : "Chat Provider"),
    );
  expect(row).toBeTruthy();
  await row!.get("button").trigger("click");
  await flushPromises();
  await click("使用此模型");
}
describe("platform model selections", () => {
  it("rejects malformed reads without replacing a failed group with defaults", async () => {
    gets = (path) =>
      path === "/system/model-selection"
        ? json({ id: id(1), version: "4", configured: {} })
        : undefined;
    const view = await page();
    expect(view.text()).toContain("读取失败");
    expect(
      view.get('[aria-label="选择向量模型"]').attributes("disabled"),
    ).toBeDefined();
    expect(
      view.get('[aria-label="选择会议摘要模型"]').attributes("disabled"),
    ).toBeUndefined();
  });
  it.each(["platform", "summary"] as const)(
    "saves only the changed %s resource even when the other resource is unconfigured",
    async (resource) => {
      gets = (path) => {
        if (resource === "summary" && path === platformPath)
          return json({ ...selection(), configured: null });
        if (resource === "platform" && path === summaryPath)
          return json({ ...summary(), model: null });
      };
      await page();
      expect(button("保存").attributes("disabled")).toBeDefined();
      await pick(
        resource === "summary" ? "会议摘要模型" : "向量模型",
        resource === "platform",
      );
      await click("保存");
      expect(writes).toHaveLength(1);
      expect(writes[0]!.path).toBe(
        resource === "summary" ? summaryPath : platformPath,
      );
      expect(JSON.parse(writes[0]!.body)).toEqual(
        resource === "summary" ? summaryInput() : platformInput(),
      );
      expect(wrapper!.text()).toContain("已保存。");
      expect(button("保存").attributes("disabled")).toBeDefined();
    },
  );
  it("saves both changed resources from one action in order with independent versions and keys", async () => {
    await page();
    await pick("向量模型", true);
    await pick("会议摘要模型");
    let finishPlatform!: (value: Response) => void;
    puts = (path) =>
      path === platformPath
        ? new Promise<Response>((resolve) => {
            finishPlatform = resolve;
          })
        : json(receipt(id(2), "9"));
    await click("保存");
    expect(writes).toHaveLength(1);
    expect(writes[0]!.path).toBe(platformPath);
    expect(button("保存").attributes("disabled")).toBeDefined();
    finishPlatform(json(receipt()));
    await flushPromises();
    expect(writes.map((write) => write.path)).toEqual([
      platformPath,
      summaryPath,
    ]);
    expect(writes.map((write) => JSON.parse(write.body))).toEqual([
      platformInput(),
      summaryInput(),
    ]);
    expect(writes[0]!.key).toBeTruthy();
    expect(writes[1]!.key).toBeTruthy();
    expect(writes[1]!.key).not.toBe(writes[0]!.key);
    expect(wrapper!.text()).toContain("已保存。");
    expect(button("保存").attributes("disabled")).toBeDefined();
    expect(
      fetcher.mock.calls.filter(
        (call) =>
          String(call[0]).includes("model-selection") &&
          call[1]?.method !== "PUT",
      ),
    ).toHaveLength(2);
  });
  it.each([
    { resource: "platform", code: "INVALID_ARGUMENT", status: 400 },
    { resource: "platform", code: "VERSION_CONFLICT", status: 409 },
    { resource: "summary", code: "INVALID_ARGUMENT", status: 400 },
    { resource: "summary", code: "VERSION_CONFLICT", status: 409 },
  ])(
    "preserves unfinished drafts after $resource returns $code and never repeats a successful write",
    async ({ resource, code, status }) => {
      await page();
      await pick("向量模型", true);
      await pick("会议摘要模型");
      const failedPath = resource === "platform" ? platformPath : summaryPath;
      puts = (path) =>
        path === failedPath
          ? json({ code, commit_state: "not_started" }, status)
          : json(receipt());
      await click("保存");
      const initialCount = resource === "platform" ? 1 : 2;
      expect(writes.map((write) => write.path)).toEqual(
        resource === "platform" ? [platformPath] : [platformPath, summaryPath],
      );
      expect(wrapper!.text()).toContain("Model 12");
      expect(wrapper!.text()).toContain("Model 13");
      expect(wrapper!.find('[role="alert"]').exists()).toBe(true);
      expect(wrapper!.text()).not.toContain("已保存。");
      if (resource === "summary")
        expect(wrapper!.text()).toContain("部分用途已保存，剩余修改尚未保存。");
      if (status === 409) {
        gets = (path) => {
          if (path === platformPath)
            return json(
              resource === "platform"
                ? selection("7")
                : {
                    ...selection("5"),
                    configured: {
                      ...selection().configured,
                      embedding: id(12),
                    },
                  },
            );
          if (path === summaryPath)
            return json(summary(resource === "summary" ? "11" : "8"));
        };
        expect(button("保存").attributes("disabled")).toBeDefined();
        await click("重新读取");
        expect(writes).toHaveLength(initialCount);
        expect(wrapper!.text()).toContain("Model 12");
        expect(wrapper!.text()).toContain("Model 13");
        expect(button("保存").attributes("disabled")).toBeDefined();
        await click("采用最新版本并保留草稿");
      }
      const platformVersion =
        status === 409 && resource === "platform" ? "7" : "4";
      const summaryVersion =
        status === 409 && resource === "summary" ? "11" : "8";
      puts = (path) =>
        json(
          path === platformPath
            ? receipt(id(1), String(Number(platformVersion) + 1))
            : receipt(id(2), String(Number(summaryVersion) + 1)),
        );
      await click("保存");
      const resumed = writes.slice(initialCount);
      expect(resumed.map((write) => write.path)).toEqual(
        resource === "platform" ? [platformPath, summaryPath] : [summaryPath],
      );
      expect(resumed.map((write) => JSON.parse(write.body))).toEqual(
        resource === "platform"
          ? [platformInput(platformVersion), summaryInput(summaryVersion)]
          : [summaryInput(summaryVersion)],
      );
      expect(resumed[0]!.key).not.toBe(writes[initialCount - 1]!.key);
      expect(wrapper!.text()).toContain("已保存。");
      expect(button("保存").attributes("disabled")).toBeDefined();
    },
  );
  it.each(["platform", "summary"] as const)(
    "keeps an unknown %s request frozen through lookup and resumes only unfinished drafts after exact replay",
    async (resource) => {
      await page();
      await pick("向量模型", true);
      await pick("会议摘要模型");
      const failedPath = resource === "platform" ? platformPath : summaryPath;
      puts = (path) => {
        if (path === failedPath)
          throw new TypeError("Synthetic interrupted response");
        return json(receipt());
      };
      await click("保存");
      const originalCount = resource === "platform" ? 1 : 2;
      expect(writes).toHaveLength(originalCount);
      const original = writes[originalCount - 1]!;
      expect(original.path).toBe(failedPath);
      expect(original.key).toBeTruthy();
      expect(wrapper!.text()).toContain("原操作结果尚未确认");
      expect(wrapper!.text()).toContain("Model 13");
      expect(button("保存").attributes("disabled")).toBeDefined();
      const confirmed =
        resource === "platform" ? receipt() : receipt(id(2), "9");
      lookup = () => json({ found: true, receipt: confirmed });
      await click("查询原操作");
      expect(writes).toHaveLength(originalCount);
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
      puts = (path) =>
        json(path === platformPath ? receipt() : receipt(id(2), "9"));
      await click("确认原请求");
      expect(writes).toHaveLength(originalCount + 1);
      expect(writes[originalCount]).toEqual(original);
      if (resource === "platform") {
        expect(wrapper!.text()).not.toContain("已保存。");
        expect(wrapper!.text()).toContain("Model 13");
        await click("保存");
      }
      expect(writes).toHaveLength(3);
      expect(writes.map((write) => write.path)).toEqual(
        resource === "platform"
          ? [platformPath, platformPath, summaryPath]
          : [platformPath, summaryPath, summaryPath],
      );
      expect(JSON.parse(writes[2]!.body)).toEqual(summaryInput());
      expect(wrapper!.text()).toContain("已保存。");
      expect(button("保存").attributes("disabled")).toBeDefined();
    },
  );
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
    expect(wrapper!.text()).not.toContain("Model 12");
    expect(wrapper!.text()).not.toContain("确认原请求");
    expect(
      (await import("../src/account/session")).useSession().user.value,
    ).not.toBeNull();
  });
  it("binds Summary receipts to its own ID, successor version, kind and zero references", async () => {
    const { saveMeetingSummarySelection } =
      await import("../src/system/model-selection");
    for (const wrong of [
      receipt(id(1), "9"),
      receipt(id(2), "8"),
      { ...receipt(id(2), "9"), kind: "model.update" },
      { ...receipt(id(2), "9"), affected_references: "1" },
    ]) {
      puts = () => json(wrong);
      const operation = saveMeetingSummarySelection({
        id: id(2),
        expected_version: "8",
        model: id(11),
      });
      await expect(operation.command.run()).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      expect(operation.command.confirmed).toBe(false);
      operation.command.release();
    }
  });
  it("requires declared memory capabilities and reports disabled references", async () => {
    const { modelPurposeIssue } = await import("../src/system/model-selection");
    const chat = model(11);
    chat.input.capabilities.structured_output_modes = [];
    expect(modelPurposeIssue("memory", chat, provider())).toContain(
      "json_schema",
    );
    expect(
      modelPurposeIssue("meeting_summary", chat, provider()),
    ).toBeUndefined();
    chat.input.enabled = false;
    expect(modelPurposeIssue("meeting_summary", chat, provider())).toContain(
      "停用",
    );
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
      path === "/system/model-selection"
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
    await wrapper!.get('[aria-label="清除图像模型"]').trigger("click");
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
