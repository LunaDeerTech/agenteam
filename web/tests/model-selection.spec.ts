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
const summary = () => ({ id: id(2), version: "8", model: id(11) });
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
async function click(text: string) {
  const button = wrapper!
    .findAll("button")
    .find((item) => item.text() === text);
  expect(button, `Button ${text}`).toBeTruthy();
  await button!.trigger("click");
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
    expect(view.find('[aria-label="选择向量模型"]').exists()).toBe(false);
    expect(view.find('[aria-label="选择会议摘要模型"]').exists()).toBe(true);
  });
  it("uses separate versions and retains the other section's draft after saving", async () => {
    await page();
    await pick("向量模型", true);
    await pick("会议摘要模型");
    await click("保存平台模型用途");
    expect(writes).toHaveLength(1);
    expect(JSON.parse(writes[0]!.body)).toEqual({
      id: id(1),
      expected_version: "4",
      embedding: id(12),
      memory: id(11),
      reranker: null,
      image: null,
    });
    expect(wrapper!.get('section[aria-label="会议摘要"]').text()).toContain(
      "Model 13",
    );
    await click("保存会议摘要用途");
    expect(JSON.parse(writes[1]!.body)).toEqual({
      id: id(2),
      expected_version: "8",
      model: id(13),
    });
    expect(wrapper!.text()).toContain("会议摘要用途已保存");
    expect(
      fetcher.mock.calls.filter(
        (call) =>
          String(call[0]).includes("model-selection") &&
          call[1]?.method !== "PUT",
      ),
    ).toHaveLength(2);
  });
  it("keeps Unknown frozen after lookup and confirms only by replaying the identical original request", async () => {
    await page();
    await pick("向量模型", true);
    puts = () => {
      throw new TypeError("Synthetic interrupted response");
    };
    await click("保存平台模型用途");
    expect(wrapper!.text()).toContain("原操作结果尚未确认");
    await click("查询原操作");
    expect(wrapper!.text()).toContain("已找到历史回执");
    expect(wrapper!.text()).not.toContain("平台模型用途已保存");
    puts = () => json(receipt());
    await click("确认原请求");
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual(writes[0]);
    expect(wrapper!.text()).toContain("平台模型用途已保存");
  });
  it("preserves conflict drafts and requires explicit adoption of the new version", async () => {
    await page();
    await pick("向量模型", true);
    puts = () =>
      json({ code: "VERSION_CONFLICT", commit_state: "not_started" }, 409);
    await click("保存平台模型用途");
    gets = (path) =>
      path === "/system/model-selection" ? json(selection("7")) : undefined;
    const section = wrapper!.get('section[aria-label="平台模型用途"]');
    await section
      .findAll("button")
      .find((item) => item.text() === "重新读取")!
      .trigger("click");
    await flushPromises();
    expect(section.text()).toContain("Model 12");
    expect(writes).toHaveLength(1);
    await click("采用最新版本并保留草稿");
    puts = () => json(receipt(id(1), "8"));
    await click("保存平台模型用途");
    expect(JSON.parse(writes[1]!.body)).toMatchObject({
      expected_version: "7",
      embedding: id(12),
    });
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
    await click("保存平台模型用途");
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
    await click("保存平台模型用途");
    expect(writes).toHaveLength(1);
    expect(JSON.parse(writes[0]!.body).image).toBe(id(14));
    await wrapper!.get('[aria-label="清除图像模型"]').trigger("click");
    puts = () => json(receipt(id(1), "6"));
    await click("保存平台模型用途");
    expect(JSON.parse(writes[1]!.body).image).toBeNull();
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
