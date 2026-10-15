// Independent reviewer operations; no author test assertions are imported.
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const modes = ["content", "collaboration"];
if (process.argv.includes("--list")) {
  console.log(modes);
  process.exit(0);
}
const mode = process.argv[2] || "content";
if (!modes.includes(mode)) throw new Error("Unknown journey");
const require = createRequire(import.meta.url);
const { chromium } = require(
  process.env.F0_PLAYWRIGHT ||
    "/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules/@playwright/test",
);
const output =
  process.env.F0_REVIEW_OUTPUT ||
  path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../../../../output/ai/frontend-f0-review",
  );
const origin = process.env.F0_REVIEW_ORIGIN || "http://127.0.0.1:5198";
await mkdir(output, { recursive: true });
const results = [],
  errors = [],
  blocked = [];
const browser = await chromium.launch({
  executablePath: "/usr/bin/chromium",
  args: ["--no-sandbox"],
});
const context = await browser.newContext({
  viewport: { width: 390, height: 844 },
  reducedMotion: "reduce",
});
const page = await context.newPage();
page.setDefaultTimeout(8000);
page.on("pageerror", (e) => errors.push(e.message));
await context.route("**/*", (route) => {
  const r = route.request(),
    u = r.url();
  if (
    (u.startsWith(origin + "/") && ["GET", "HEAD"].includes(r.method())) ||
    /^(data:|blob:|chrome:\/\/resources\/|chrome-extension:\/\/mhjfbmdgcfjbbpaeojofohoefgiehjai\/)/.test(
      u,
    )
  )
    return route.continue();
  blocked.push({ url: u, method: r.method() });
  return route.abort();
});
const button = (name) => page.getByRole("button", { name, exact: true });
const check = (ok, message, evidence = {}) =>
  results.push({ result: ok ? "pass" : "fail", message, ...evidence });
async function open(name, scenario = "normal", extra = {}) {
  await page.goto(
    `${origin}/index.html?${new URLSearchParams({ page: name, scenario, theme: "dark", ...extra })}`,
  );
  await page.locator("#main").waitFor();
  await page.evaluate(() => document.fonts.ready);
}
async function shot(name) {
  await page.screenshot({
    path: path.join(output, `journey-${name}.png`),
    fullPage: true,
  });
}
async function caseOf(name, run) {
  try {
    await run();
  } catch (e) {
    check(false, `${name}: interrupted`, { error: e.message });
    await shot(`${name}-interrupted`);
  }
}
try {
  if (mode === "content") {
    await caseOf("knowledge-mobile", async () => {
      await open("knowledge");
      await button("打开文档目录").focus();
      await page.keyboard.press("Enter");
      check(
        await page
          .getByLabel("查找本项目文档标题")
          .evaluate((el) => el === document.activeElement),
        "390 Knowledge directory opens with focus in search",
      );
      check(
        !(await page.locator(".knowledge-reader").isVisible()),
        "390 directory is a single panel",
      );
      await button("关闭文档目录").click();
      check(
        await button("打开文档目录").evaluate(
          (el) => el === document.activeElement,
        ),
        "Knowledge directory close returns focus",
      );
      await button("编辑原文").click();
      await page
        .getByLabel("正文", { exact: true })
        .fill("Review draft belonging to Markdown only.");
      await button("关闭编辑").click();
      check(
        await page.getByRole("dialog").isVisible(),
        "Knowledge dirty editor has a close gate",
      );
      await button("保留草稿并关闭").click();
      await button("打开文档目录").click();
      await page.locator('[data-doc="txt"]').click();
      check(
        (await button("继续草稿").count()) === 0,
        "Markdown draft is not offered on TXT",
      );
      await button("打开文档目录").click();
      await page.locator('[data-doc="md"]').click();
      await button("继续草稿").click();
      check(
        (await page.getByLabel("正文", { exact: true }).inputValue()) ===
          "Review draft belonging to Markdown only.",
        "Returning to original document preserves its draft",
      );
      await button("保存原文").click();
      check(
        (await page.locator("#main").innerText()).includes("未向服务端提交"),
        "Knowledge save explicitly reports local-only result",
      );
      await shot("knowledge-draft-restored-390");
    });
    await caseOf("knowledge-read-gates", async () => {
      await open("knowledge", "long");
      check(
        await button("编辑原文").isDisabled(),
        "Segmented read blocks whole-source editing until complete",
      );
      await page.locator('[data-knowledge="read-more"]').click();
      check(
        await button("编辑原文").isEnabled(),
        "Complete read unlocks local editing",
      );
      await open("knowledge", "readonly");
      check(
        (await button("编辑原文").isDisabled()) &&
          (await button("新建文档").isDisabled()),
        "Readonly Knowledge gates edits and creation",
      );
      await open("knowledge", "unknown");
      check(
        await button("编辑原文").isDisabled(),
        "Unknown Knowledge gates repeated modification",
      );
      await button("查询原请求").click();
      check(
        await button("编辑原文").isEnabled(),
        "Knowledge original result lookup resolves local Unknown",
      );
      await open("knowledge", "normal", { doc: "docx" });
      await page
        .locator("details")
        .filter({ has: page.locator('[data-knowledge="parse-fail"]') })
        .locator("summary")
        .click();
      await page.locator('[data-knowledge="parse-fail"]').click();
      check(
        (await page.locator("#main").innerText()).includes("解析失败"),
        "DOCX parse failure stays near reader",
      );
      await shot("knowledge-docx-failed-390");
    });
    await caseOf("artifacts-bounded", async () => {
      await open("project");
      await page.getByRole("link", { name: "产物", exact: true }).click();
      await page.locator("[data-artifact]").first().click();
      const measured = await page.evaluate(() => ({
        width: innerWidth,
        scroll: document.documentElement.scrollWidth,
        text: document.querySelector("#main").innerText,
      }));
      check(
        measured.scroll <= measured.width &&
          measured.text.includes("合成样板") &&
          measured.text.includes("已完整读取"),
        "Artifacts companion navigation, raw text, fixture boundary and overflow",
        { measured },
      );
      await page.locator("[data-artifact-back]").click();
      check(
        (await page.locator("[data-artifact]").count()) > 0,
        "Artifacts returns to list",
      );
    });
  } else {
    await caseOf("meeting-upload", async () => {
      await open("meetings");
      await page
        .getByLabel("回复会议", { exact: true })
        .fill("Independent attachment draft");
      await page.getByLabel("添加会议附件").setInputFiles({
        name: "review-note.txt",
        mimeType: "text/plain",
        buffer: Buffer.from("local fixture only"),
      });
      check(
        await button("发送").isDisabled(),
        "Unresolved attachment prevents sending",
      );
      await button("创建上传").click();
      await button("停止传输").click();
      await button("查询原上传状态").click();
      check(
        await button("查询原上传结果").isVisible(),
        "Stopped attachment recovers through original upload result",
      );
      await button("查询原上传结果").click();
      await button("插入附件引用").click();
      check(
        (await button("发送").isEnabled()) &&
          (await page.getByLabel("回复会议", { exact: true }).inputValue()) ===
            "Independent attachment draft",
        "Attachment recovery preserves draft and inserts confirmed reference",
      );
      await shot("meeting-attachment-ready-390");
      await button("发送").click();
      check(
        (await page.locator("#main").innerText()).includes("样板演示：消息") &&
          (await page.getByLabel("回复会议", { exact: true }).inputValue()) ===
            "",
        "Message submission is visibly local and clears confirmed draft",
      );
    });
    await caseOf("meeting-approval", async () => {
      await open("meetings");
      await button("批准本次操作").click();
      await page.getByLabel("演示提交后结果未知").check();
      await button("确认批准").click();
      check(
        (await button("批准本次操作").count()) === 0,
        "Unknown approval prevents a second decision",
      );
      await page.locator('[data-collab="approval-lookup"]').click();
      check(
        (await page.locator(".co-approval").innerText()).includes("尚未"),
        "First approval lookup keeps pending result",
      );
      await shot("meeting-approval-unknown-390");
      await page.locator('[data-collab="approval-lookup"]').click();
      check(
        (await page.locator(".co-approval").innerText()).includes("已批准"),
        "Second original lookup confirms approval",
      );
      await open("meetings", "readonly");
      check(
        (await button("发送").isDisabled()) &&
          (await button("批准本次操作").isDisabled()),
        "Archived meeting composer and approval are readonly",
      );
    });
    await caseOf("inbox-source", async () => {
      await open("inbox");
      await page.locator('[data-collab="inbox-item:decision-01"]').click();
      await shot("inbox-source-390");
      await page
        .getByRole("link", { name: "打开来源会议", exact: true })
        .click();
      check(
        new URL(page.url()).searchParams.get("source") === "decision-01" &&
          (await page.locator(".project-identity").innerText()).includes(
            "Atlas",
          ),
        "Inbox source preserves project and decision identity",
      );
      await page
        .getByRole("link", { name: "返回项目待办", exact: true })
        .click();
      check(
        await page
          .getByRole("heading", { name: "项目待办", exact: true })
          .isVisible(),
        "Source meeting returns to project Inbox",
      );
    });
    await caseOf("mcp-bearer-repair", async () => {
      await open("mcp");
      await button("删除访问凭据").click();
      await button("确认删除凭据").click();
      check(
        await button("先完成认证").isDisabled(),
        "Deleted credential blocks connection until reconfigured",
      );
      await button("配置凭据").click();
      await page.getByLabel("凭据材料").fill("synthetic-review-material");
      await button("保存凭据").click();
      check(
        await button("连接服务").isEnabled(),
        "Reconfigured Bearer credential unlocks connection (original failed path)",
      );
      check(
        !(await page.locator("body").innerText()).includes(
          "synthetic-review-material",
        ) &&
          (await page
            .locator("input[type=password]")
            .evaluateAll((inputs) =>
              inputs.every((input) => input.value === ""),
            )),
        "Credential material is cleared after submission",
      );
      await button("连接服务").click();
      check(
        await button("刷新发现").isEnabled(),
        "Reconfigured service reaches connected state",
      );
      await button("停用连接").click();
      check(
        (await button("启用连接").isEnabled()) &&
          (await button("刷新发现").isEnabled()),
        "Disablement remains separate from live connection",
      );
    });
    await caseOf("mcp-recovery", async () => {
      await open("mcp", "unknown");
      check(
        await button("刷新发现").isDisabled(),
        "Unknown MCP prevents another refresh",
      );
      await button("查询原操作结果").click();
      check(
        await button("刷新发现").isDisabled(),
        "Stopped original refresh requires reread",
      );
      await shot("mcp-stopped-390");
      await button("重新读取连接").click();
      check(
        await button("刷新发现").isEnabled(),
        "Reread resolves stopped connection gate",
      );
      await open("mcp", "normal", { service: "system" });
      check(
        (await button("编辑配置").isDisabled()) &&
          (await button("停用连接").isEnabled()),
        "System service configuration is readonly while project connection can be managed",
      );
      await page.locator('[data-collab="tab:resources"]').click();
      check(
        (await page.locator("#main").innerText()).includes("资源发现失败"),
        "Resource failure is separate from connected state",
      );
      await shot("mcp-system-resource-failure-390");
    });
    await caseOf("oauth-direct-repair", async () => {
      await open("oauth");
      await button("确认授权返回").click();
      await page
        .getByRole("link", { name: "返回项目 MCP 服务", exact: true })
        .click();
      check(
        (await page.getByRole("heading", { level: 2 }).innerText()) ===
          "GitHub" && (await button("连接服务").isEnabled()),
        "Direct OAuth callback initializes GitHub connection as available (original failed path)",
      );
      await button("连接服务").click();
      check(
        await button("刷新发现").isEnabled(),
        "Direct callback can complete connection",
      );
      await open("oauth", "unknown");
      check(
        (await button("确认授权返回").count()) === 0,
        "Unknown callback offers lookup rather than another exchange",
      );
      await button("查询原操作结果").click();
      check(
        (await page.locator("#main").innerText()).includes("pending"),
        "First OAuth lookup retains original pending operation",
      );
      await shot("oauth-unknown-390");
      await button("查询原操作结果").click();
      check(
        await page
          .getByRole("heading", { name: "授权结果已确认", exact: true })
          .isVisible(),
        "Second OAuth lookup confirms original exchange",
      );
    });
    await caseOf("memory-delete", async () => {
      await open("memory");
      await button("来源与修订").first().click();
      check(
        (
          await page
            .getByRole("complementary", { name: "记忆来源与修订" })
            .innerText()
        ).includes("已替代"),
        "Memory shows current and superseded revisions with source",
      );
      await button("删除记忆").click();
      let trapped = true;
      for (let i = 0; i < 8; i++) {
        await page.keyboard.press(i % 2 ? "Tab" : "Shift+Tab");
        trapped &&= await page.evaluate(() =>
          document.querySelector("dialog").contains(document.activeElement),
        );
      }
      check(
        trapped,
        "Memory delete confirmation traps both keyboard directions",
      );
      await page.keyboard.press("Escape");
      check(
        await button("删除记忆").evaluate(
          (el) => el === document.activeElement,
        ),
        "Memory confirmation Escape returns focus",
      );
      await button("删除记忆").click();
      await page.getByLabel("演示提交后结果未知").check();
      await button("确认删除").click();
      check(
        (await button("删除记忆").isDisabled()) &&
          (await page
            .locator('[data-collab="memory-select:memory-01"]')
            .count()) === 1,
        "Unknown deletion retains original record and forbids duplicate delete",
      );
      await button("查询原操作结果").click();
      check(
        (await page
          .locator('[data-collab="memory-select:memory-01"]')
          .count()) === 1,
        "Pending deletion lookup retains record",
      );
      await shot("memory-delete-pending-390");
      await button("查询原操作结果").click();
      check(
        (await page
          .locator('[data-collab="memory-select:memory-01"]')
          .count()) === 0,
        "Confirmed deletion removes only original current record",
      );
      await button("关闭记忆").click();
      check(
        (await page
          .getByRole("heading", { name: "Agent 基础配置", exact: true })
          .isVisible()) &&
          (await button("记忆").evaluate(
            (el) => el === document.activeElement,
          )),
        "Closing Memory returns to same Agent with focus",
      );
    });
  }
  check(
    errors.length === 0 && blocked.length === 0,
    "No script errors or non-fixture requests",
    { errors, blocked },
  );
} finally {
  await context.close();
  await browser.close();
  await writeFile(
    path.join(output, `journeys-${mode}.json`),
    JSON.stringify(
      { boundary: "F0 synthetic only", results, errors, blocked },
      null,
      2,
    ),
  );
}
console.log(JSON.stringify(results, null, 2));
if (results.some((r) => r.result === "fail")) process.exitCode = 1;
