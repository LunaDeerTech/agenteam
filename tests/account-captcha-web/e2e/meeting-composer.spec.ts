import { test, expect, type Request } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

type Material = {
  origin: string;
  cookie: string;
  project_id: string;
  route: string;
  agent_name: string;
  agent_id: string;
  prompt: string;
  first_reply: string;
  replacement_reply: string;
  final_reply: string;
  first_title: string;
  final_title: string;
  final_summary: string;
  typed_text: string;
  reference_url: string;
  task: { id: string; label: string; milestone: string; sprint: string };
  document: { id: string; label: string };
  upload: { name: string; media_type: string; text: string };
  checkpoint_path: string;
};
type Command = { path: string; key: string; body: unknown };
type Observation = {
  method: string;
  path: string;
  status: number;
  finished: boolean;
  terminal: "pending" | "finished" | "failed";
  failure: "aborted" | "other" | null;
};
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_MEETING_WEB_EVIDENCE!;
let diagnostic = () => ({
  stage: "not-started",
  commands: 0,
  finished: 0,
  unfinished: 0,
  pageErrors: 0,
  observations: [] as Observation[],
});

test("Owner composes immutable references and recovers original upload and reply commands", async ({
  page,
  context,
}) => {
  const material: Material = JSON.parse(
    readFileSync(join(directory, "composer-material.json"), "utf8"),
  );
  expect(material.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const commands: Command[] = [],
    observations: Observation[] = [],
    tails: Promise<void>[] = [];
  const responses = new Map<
    Request,
    { observed: Observation; resolve: () => void }
  >();
  let stage = "open",
    pageErrors = 0,
    artifactID = "";
  diagnostic = () => ({
    stage,
    commands: commands.length,
    finished: observations.filter((item) => item.finished).length,
    unfinished: observations.filter((item) => !item.finished).length,
    pageErrors,
    observations,
  });
  page.on("pageerror", () => {
    pageErrors++;
  });
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      request.method() !== "POST" ||
      !path.startsWith(`/api/v1/projects/${material.project_id}/`)
    )
      return;
    if (
      !path.includes("/meetings") &&
      !path.includes("/artifacts/upload-commands/")
    )
      return;
    commands.push({
      path,
      key: request.headers()["idempotency-key"] ?? "",
      body: request.postDataJSON(),
    });
  });
  page.on("response", (response) => {
    const request = response.request(),
      path = new URL(response.url()).pathname;
    if (!path.startsWith(`/api/v1/projects/${material.project_id}/`)) return;
    if (request.method() !== "POST" && request.method() !== "PUT") return;
    if (
      !path.includes("/meetings") &&
      !path.includes("/artifacts/upload-commands/")
    )
      return;
    const observed: Observation = {
      method: request.method(),
      path,
      status: response.status(),
      finished: false,
      terminal: "pending",
      failure: null,
    };
    observations.push(observed);
    const terminal = new Promise<void>((resolve) => {
      responses.set(request, { observed, resolve });
    });
    if (
      observed.status === 200 &&
      path.endsWith("/artifacts/upload-commands/lookup")
    )
      tails.push(
        (async () => {
          await terminal;
          if (!observed.finished) return;
          const body = await response.json();
          if (body.state === "committed")
            artifactID = body.artifact.artifact_ref.artifact_id;
        })().catch(() => undefined),
      );
  });
  // Response.finished() in the locked client resolves only on requestfinished;
  // a cancelled error body emits requestfailed instead. Observe both original
  // Request terminal events without closing the page to manufacture a result.
  function settle(request: Request, failed: boolean) {
    const row = responses.get(request);
    if (!row || row.observed.terminal !== "pending") return;
    row.observed.finished = !failed;
    row.observed.terminal = failed ? "failed" : "finished";
    row.observed.failure = failed
      ? request.failure()?.errorText === "net::ERR_ABORTED"
        ? "aborted"
        : "other"
      : null;
    row.resolve();
  }
  page.on("requestfinished", (request) => settle(request, false));
  page.on("requestfailed", (request) => settle(request, true));
  await context.addCookies([
    {
      name: "agenteam_local_session",
      value: material.cookie,
      url: material.origin,
      httpOnly: true,
      sameSite: "Strict",
    },
  ]);
  material.cookie = "";
  await page.goto(material.route);
  await expect(
    page.getByRole("heading", { name: "开始一场会议", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "编排 Agent", exact: true }).click();
  const roster = page.getByRole("dialog", {
    name: "编排参会 Agent",
    exact: true,
  });
  await roster
    .getByRole("checkbox", { name: new RegExp(material.agent_name) })
    .check();
  await roster.getByRole("button", { name: "完成", exact: true }).click();
  await page
    .getByRole("textbox", { name: "会议首条消息", exact: true })
    .fill(material.prompt);
  stage = "start-original-turn";
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/meetings/[0-9a-f-]{36}$`));
  const meeting = new URL(page.url()).pathname.split("/").at(-1)!;
  const timeline = page.getByLabel("会议消息", { exact: true });
  await expect(timeline).toContainText(material.first_reply);
  await expect(
    page.getByRole("heading", { name: material.first_title, exact: true }),
  ).toBeVisible();
  stage = "original-history-reply";
  await page.getByRole("button", { name: "重新生成回复", exact: true }).click();
  await page
    .getByRole("dialog", { name: "重新生成回复？", exact: true })
    .getByRole("button", { name: "确认重新生成", exact: true })
    .click();
  await expect(timeline).toContainText(material.replacement_reply);
  await timeline.locator("summary").filter({ hasText: "历史版本（" }).click();
  await timeline
    .getByRole("button", { name: "回复第 1 版消息", exact: true })
    .click();
  const editor = page.getByRole("textbox", { name: "发送到会议", exact: true });
  await expect(editor).toBeFocused();
  await expect(page.locator(".reply-preview")).toContainText(
    material.first_reply,
  );
  await editor.pressSequentially(material.typed_text + "@");
  await page
    .getByRole("button", {
      name: `提及 ${material.agent_name}（Agent）`,
      exact: true,
    })
    .click();
  const remove = editor.getByRole("button", {
    name: `移除 @${material.agent_name}`,
    exact: true,
  });
  await remove.focus();
  await remove.press("Enter");
  await expect(remove).toHaveCount(0);
  await expect(editor).toHaveText(material.typed_text);
  await expect(editor).toBeFocused();
  // This is Chromium's native button default activation, not a test-dispatched click.
  await editor.pressSequentially("@");
  await page
    .getByRole("button", {
      name: `提及 ${material.agent_name}（Agent）`,
      exact: true,
    })
    .click();
  await editor.pressSequentially(" check #");
  let picker = page.getByRole("dialog", { name: "插入项目资源", exact: true });
  await picker.getByRole("button", { name: "Task", exact: true }).click();
  await picker
    .getByRole("button", {
      name: "打开 " + material.task.milestone,
      exact: true,
    })
    .click();
  await picker
    .getByRole("button", { name: "打开 " + material.task.sprint, exact: true })
    .click();
  await picker
    .getByRole("button", { name: "筛选 Task 状态", exact: true })
    .click();
  await page.getByRole("option", { name: "待规划", exact: true }).click();
  await picker
    .getByRole("button", { name: "引用 " + material.task.label, exact: true })
    .click();
  await expect(editor).toBeFocused();
  await editor.pressSequentially(" and #");
  picker = page.getByRole("dialog", { name: "插入项目资源", exact: true });
  await picker.getByRole("button", { name: "Knowledge", exact: true }).click();
  await picker
    .getByRole("button", {
      name: "引用 " + material.document.label,
      exact: true,
    })
    .click();
  await editor.pressSequentially(" with ");
  await page.getByRole("button", { name: "添加到消息", exact: true }).click();
  await page.getByRole("menuitem", { name: "添加链接", exact: true }).click();
  const link = page.getByRole("dialog", { name: "添加链接", exact: true });
  await link
    .getByRole("textbox", { name: "链接地址", exact: true })
    .fill(material.reference_url);
  await link
    .getByRole("textbox", { name: "显示文字（可选）", exact: true })
    .fill("Review link");
  await link.getByRole("button", { name: "插入链接", exact: true }).click();
  await editor.pressSequentially(" and ");
  const beforeUpload = await editor.innerText();
  stage = "original-file-upload-unknown";
  // Feed the browser File drag input. No canonical Meeting node or response is injected.
  const transfer = await page.evaluateHandle((upload) => {
    const data = new DataTransfer();
    data.items.add(
      new File([upload.text], upload.name, { type: upload.media_type }),
    );
    return data;
  }, material.upload);
  await editor.dispatchEvent("drop", { dataTransfer: transfer });
  await transfer.dispose();
  const upload = page.getByRole("dialog", { name: "上传附件", exact: true });
  await expect(upload).toContainText(material.upload.name);
  expect(await editor.innerText()).toBe(beforeUpload);
  await upload.getByRole("button", { name: "上传并插入", exact: true }).click();
  await expect(
    upload.getByRole("button", { name: "查询原上传", exact: true }),
  ).toBeEnabled();
  expect(await editor.innerText()).toBe(beforeUpload);
  await upload.getByRole("button", { name: "查询原上传", exact: true }).click();
  await expect(upload).toHaveCount(0);
  await expect(
    editor.getByRole("button", {
      name: "移除 " + material.upload.name,
      exact: true,
    }),
  ).toBeVisible();
  await expect(editor).toBeFocused();
  await expect.poll(() => artifactID).toMatch(/^[0-9a-f-]{36}$/);
  const draftBeforeSend = await editor.innerText();
  stage = "original-send-unknown";
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "查询原操作", exact: true }),
  ).toBeEnabled();
  expect(await editor.innerText()).toBe(draftBeforeSend);
  await expect(page.locator(".reply-preview")).toContainText(
    material.first_reply,
  );
  await page.screenshot({
    path: join(evidence, "composer-original-send-unknown.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "查询原操作", exact: true }).click();
  await expect(editor).toHaveText("");
  await expect(page.locator(".reply-preview")).toHaveCount(0);
  await expect(timeline).toContainText(material.final_reply);
  await expect(
    page.getByRole("heading", { name: material.final_title, exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(material.final_summary, { exact: true }),
  ).toBeVisible();
  const fileMenu = timeline.getByRole("button", {
    name: material.upload.name,
    exact: true,
  });
  stage = "independent-pin-and-file-read";
  await fileMenu.click();
  await page
    .getByRole("menuitem", { name: "置顶到会议引用", exact: true })
    .click();
  const desktopPinned = page
    .locator("aside.meeting-details")
    .getByRole("button", {
      name: "打开文件 " + material.upload.name,
      exact: true,
    });
  await expect(desktopPinned).toBeVisible();
  await desktopPinned.click();
  let preview = page.getByRole("dialog", {
    name: material.upload.name,
    exact: true,
  });
  await expect(preview.getByLabel("文件正文", { exact: true })).toHaveText(
    material.upload.text,
  );
  await expect(preview.locator("script, img, iframe, a[download]")).toHaveCount(
    0,
  );
  await preview.getByRole("button", { name: "返回会议", exact: true }).click();
  await expect(desktopPinned).toBeFocused();
  await page.screenshot({
    path: join(evidence, "composer-published-references-light.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const information = page.getByRole("dialog", {
    name: "会议详情",
    exact: true,
  });
  const narrowPinned = information.getByRole("button", {
    name: "打开文件 " + material.upload.name,
    exact: true,
  });
  await narrowPinned.click();
  preview = page.getByRole("dialog", {
    name: material.upload.name,
    exact: true,
  });
  await expect(preview.getByLabel("文件正文", { exact: true })).toHaveText(
    material.upload.text,
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: join(evidence, "composer-file-preview-narrow-dark.png"),
    fullPage: true,
  });
  await preview.getByRole("button", { name: "返回会议", exact: true }).click();
  await expect(narrowPinned).toBeFocused();
  await information.getByRole("button", { name: "关闭", exact: true }).click();
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.emulateMedia({ colorScheme: "light" });
  stage = "archive-and-restore";
  await page.getByRole("button", { name: "更多操作", exact: true }).click();
  await page.getByRole("menuitem", { name: "归档会议", exact: true }).click();
  await expect(
    timeline.getByText("会议已归档。", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "发送", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "更多操作", exact: true }).click();
  await page.getByRole("menuitem", { name: "恢复会议", exact: true }).click();
  await expect(timeline.getByText("会议已归档。", { exact: true })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeEnabled();
  const checkpoint = await page.evaluate(async (path) => {
    const result = await fetch(path, {
      method: "POST",
      credentials: "same-origin",
    });
    await result.arrayBuffer();
    return result.status;
  }, material.checkpoint_path);
  expect(checkpoint).toBe(204);
  stage = "delete-original-meeting";
  await page.getByRole("button", { name: "更多操作", exact: true }).click();
  await page.getByRole("menuitem", { name: "删除会议…", exact: true }).click();
  await page
    .getByRole("dialog", { name: "删除这场会议？", exact: true })
    .getByRole("button", { name: "确认删除会议", exact: true })
    .click();
  await expect
    .poll(
      async () => {
        if (
          await page
            .getByRole("heading", { name: "会议已删除", exact: true })
            .first()
            .isVisible()
        )
          return true;
        const lookup = page.getByRole("button", {
          name: "查询原操作",
          exact: true,
        });
        if ((await lookup.isVisible()) && (await lookup.isEnabled()))
          await lookup.click();
        return false;
      },
      { timeout: 8_000, intervals: [100, 250, 500] },
    )
    .toBe(true);
  await page.getByRole("button", { name: "返回会议列表", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "开始一场会议", exact: true }),
  ).toBeVisible();
  stage = "observe-command-tails";
  await expect
    .poll(() => observations.every((item) => item.terminal !== "pending"))
    .toBe(true);
  await Promise.all(tails);
  expect(pageErrors).toBe(0);
  const plannedDrops = [
    `/api/v1/projects/${material.project_id}/artifacts/upload-commands/complete`,
    `/api/v1/projects/${material.project_id}/meetings/${meeting}/turns`,
  ];
  expect(
    observations
      .filter((item) => item.status === 502)
      .map((item) => item.path)
      .sort(),
  ).toEqual([...plannedDrops].sort());
  expect(
    observations.filter(
      (item) =>
        !(item.status === 200 && item.finished) &&
        !(
          item.method === "POST" &&
          item.status === 502 &&
          plannedDrops.includes(item.path)
        ),
    ),
  ).toEqual([]);
  expect(
    commands.filter((command) =>
      command.path.endsWith(`/meetings/${meeting}/turns`),
    ),
  ).toHaveLength(1);
  expect(
    commands.filter((command) =>
      command.path.endsWith("/artifacts/upload-commands/complete"),
    ),
  ).toHaveLength(1);
  for (const command of commands)
    expect(command.key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
  stage = "browser-close";
  await context.close();
  stage = "completed";
  writeFileSync(
    join(directory, "composer-result.json"),
    JSON.stringify({
      completed: true,
      meeting,
      artifact_id: artifactID,
      commands,
      observations,
    }),
    { mode: 0o600 },
  );
});

test.afterEach(async ({ page }, info) => {
  console.info("MEETING_COMPOSER_OBSERVATION " + JSON.stringify(diagnostic()));
  if (info.status !== info.expectedStatus)
    await page
      .screenshot({
        path: join(evidence, "composer-failure.png"),
        fullPage: true,
        timeout: 1_500,
      })
      .catch(() => undefined);
});
