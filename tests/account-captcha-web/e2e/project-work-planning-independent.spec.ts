import { test, expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { startWorkNativeDiagnostic } from "./project-work-planning.native";
import {
  material,
  button,
  details,
  recovery,
  path,
  enter,
  ready,
  login,
  choose,
  confirmed,
  ipc,
  complete,
  observe,
  projectRefreshBody,
  saveWorkFailureObservations,
  evidence,
  repository,
} from "./project-work-planning.helpers";

test.afterEach(async ({}, info) => {
  saveWorkFailureObservations(
    info.status === info.expectedStatus
      ? null
      : (info.status ?? "other-failure"),
  );
});

const editor = (page: Page) =>
  page.getByRole("form", { name: /^(规划结构编辑|Task 规划编辑)$/ });
const title = (page: Page) =>
  editor(page).getByRole("textbox", { name: "标题", exact: true });
const workDialog = (page: Page) =>
  page.getByRole("dialog", { name: "放弃任务规划修改？", exact: true });
const field = (where: ReturnType<typeof details>, label: string) =>
  where
    .locator("dt")
    .filter({ hasText: new RegExp(`^${label}$`) })
    .locator("+ dd");
const uncertain = (page: Page) =>
  recovery(page).getByRole("heading", {
    name: "原命令结果不确定",
    exact: true,
  });

// These are independent user actions and per-boundary observations. The shared
// fixture/consumer only supplies the already accepted wire and lifetime oracle.
test("[independent-recovery] old receipts survive current changes, canceled discard and archived replay", async ({
  page,
}) => {
  const data = material();
  let diagnostic: Awaited<ReturnType<typeof startWorkNativeDiagnostic>>;
  const seen = observe(page, {
    ordinaryCompletion: (request, id) => diagnostic.consumed(request, id),
  });
  diagnostic = await startWorkNativeDiagnostic(page, {
    projects: Object.values(data.work).map((seed) => seed.project_id),
    evidence,
    repository,
    classify: seen.declarationKind,
    ordinaryCompletion: true,
    projectRefreshCompletion: true,
    isOriginalReplay: seen.isOriginalReplay,
  });
  try {
    for (const [index, domain] of (
      ["blocker", "task", "structure"] as const
    ).entries()) {
      const kind = domain === "structure" ? "milestone" : "task";
      const seed = data.work[domain]!;
      if (index === 0) await enter(page, data, kind, domain);
      else {
        await diagnostic.flush();
        await page.goto(path(data, domain, kind));
        await ready(page, seed[`${kind}_id`]);
      }
      await diagnostic.installPublication();
      const expectedVersion = await field(
        details(page),
        "当前版本",
      ).innerText();
      const originalText = `独验原命令 ${domain}`;
      await ipc("arm-loss", { project: domain, domain });
      seen.declareIncomplete({
        kind:
          domain === "structure"
            ? "lost-milestone-update"
            : domain === "task"
              ? "lost-task-update"
              : "lost-blocker-add",
        projectID: seed.project_id,
        targetID: seed[`${kind}_id`],
        expectedVersion,
        text: originalText,
      });
      if (domain === "blocker") {
        await choose(page, "阻塞类型", "等待人工处理");
        await page
          .getByRole("textbox", { name: "阻塞说明", exact: true })
          .fill(originalText);
        await button(
          page.getByRole("form", { name: "添加阻塞", exact: true }),
          "添加阻塞",
        ).click();
      } else {
        await title(page).fill(originalText);
        await button(editor(page), "保存修改").click();
      }
      await expect(uncertain(page)).toBeVisible();
      const committedBeforeLookup = await ipc("observe", { project: domain });
      expect(committedBeforeLookup.dropped === index + 1).toBe(true);

      // Canceling a discard must keep the original command, and cannot cause a
      // lookup, retry or write. Compare actual browser requests and SQL facts.
      const requestsBeforeDiscard = seen.requests.length;
      await button(recovery(page), "放弃本地追踪").click();
      await expect(workDialog(page)).toBeVisible();
      await button(workDialog(page), "继续编辑").click();
      await expect(uncertain(page)).toBeVisible();
      expect(seen.requests.length === requestsBeforeDiscard).toBe(true);
      expect((await ipc("observe", { project: domain })).facts).toEqual(
        committedBeforeLookup.facts,
      );

      await ipc("update", {
        project: domain,
        resource: kind,
        text: "独验后继当前值",
      });
      await button(editor(page), "读取当前值").click();
      await expect(details(page)).toContainText("独验后继当前值");
      await expect(uncertain(page)).toBeVisible();
      const beforeLookup = (await ipc("observe", { project: domain })).facts;
      await button(recovery(page), "查证原命令").click();
      await confirmed(page);
      const historicalVersion = await field(
        recovery(page),
        "历史版本",
      ).innerText();
      expect(BigInt(historicalVersion) === BigInt(expectedVersion) + 1n).toBe(
        true,
      );
      expect(
        BigInt(await field(details(page), "当前版本").innerText()) >
          BigInt(historicalVersion),
      ).toBe(true);
      if (domain !== "blocker")
        await expect(recovery(page)).toContainText(originalText);
      await expect(details(page)).toContainText("独验后继当前值");
      expect((await ipc("observe", { project: domain })).facts).toEqual(
        beforeLookup,
      );

      if (domain === "blocker") {
        const blockerID = await field(
          recovery(page),
          "历史阻塞记录 ID",
        ).innerText();
        await ipc("resolve", {
          project: domain,
          target: blockerID,
          text: "独验后继解除",
        });
      }
      await ipc("archive", { project: domain });
      const [response] = await Promise.all([
        page.waitForResponse(
          (r) =>
            r.request().method() === "GET" &&
            new URL(r.url()).pathname === `/api/v1/projects/${seed.project_id}`,
        ),
        button(page, "刷新项目信息").click(),
      ]);
      await diagnostic.projectRefreshTerminal(response);
      const archived = await projectRefreshBody(
        response,
        seed.project_id,
        data.owner.user_id,
        "TestIndependentProjectWorkPlanningWebRecovery",
      );
      diagnostic.recordProjectRefresh(response.request(), archived.proof);
      await expect(
        page.getByText("项目已归档，当前内容只读。", { exact: true }),
      ).toBeVisible();
      await expect(button(page, "新建 Milestone")).toBeDisabled();
      await expect(button(editor(page), "保存修改")).toBeDisabled();

      const beforeReplay = (await ipc("observe", { project: domain })).facts;
      await button(recovery(page), "查证原命令").click();
      await confirmed(page);
      expect(await field(recovery(page), "历史版本").innerText()).toBe(
        historicalVersion,
      );
      await button(recovery(page), "按原请求重放").click();
      await confirmed(page);
      expect(await field(recovery(page), "历史版本").innerText()).toBe(
        historicalVersion,
      );
      await expect(details(page)).toContainText("独验后继当前值");
      expect((await ipc("observe", { project: domain })).facts).toEqual(
        beforeReplay,
      );
      await button(recovery(page), "放弃本地追踪").click();
      await button(workDialog(page), "放弃本地修改").click();
      await expect(recovery(page)).toHaveCount(0);
    }
    await diagnostic.finish();
    expect(diagnostic.projectRefreshesComplete(3)).toBe(true);
    complete({
      three_domains: true,
      canceled_discard: true,
      current_history: true,
      archive_replay: true,
      read_only_snapshots: true,
      bodies: await seen.verify(3),
    });
  } finally {
    await diagnostic.finish();
  }
});

test("[independent-authority] real sessions, held old read and installed domain guards remain isolated", async ({
  page,
  browser,
}) => {
  const data = material(),
    seen = observe(page);
  const navigation = page.getByRole("navigation", {
    name: "项目导航",
    exact: true,
  });
  const ownerDialog = page.getByRole("dialog", {
    name: "放弃项目修改？",
    exact: true,
  });
  const modelDialog = page.getByRole("dialog", {
    name: "离开项目模型设置？",
    exact: true,
  });
  let modelWrites = 0;
  page.on("request", (r) => {
    if (
      r.method() !== "GET" &&
      /\/api\/v1\/projects\/[^/]+\/(?:model-providers|models|model-credentials|available-models)(?:\/|$)/.test(
        new URL(r.url()).pathname,
      )
    )
      modelWrites++;
  });
  await enter(page, data, "task");
  await title(page).fill("独验当前 Session 草稿");
  const [checked] = await Promise.all([
    page.waitForResponse(
      (r) =>
        r.request().method() === "GET" &&
        new URL(r.url()).pathname === "/api/v1/session",
    ),
    page.evaluate(() =>
      window.dispatchEvent(new PageTransitionEvent("pageshow")),
    ),
  ]);
  expect(checked.status()).toBe(200);
  expect(await checked.finished()).toBeNull();
  const checkedID = (await checked.json()).session.id;
  await expect(title(page)).toHaveValue("独验当前 Session 草稿");
  await button(page, "退出登录").click();
  await expect(workDialog(page)).toBeVisible();
  await button(workDialog(page), "继续编辑").click();
  await expect(title(page)).toHaveValue("独验当前 Session 草稿");

  // This second client shares this browser's genuine cookie jar. The public
  // application must retire the old identity when its actual Session read fails.
  const currentSession = await page.request.get("/api/v1/session");
  expect(currentSession.status()).toBe(200);
  const identity = await currentSession.json();
  await currentSession.dispose();
  expect(identity.session.id === checkedID).toBe(true);
  const logout = await page.request.post("/api/v1/sessions/logout", {
    data: {},
    headers: {
      "Idempotency-Key": randomUUID(),
      "X-CSRF-Token": identity.csrf_token,
      Origin: new URL(page.url()).origin,
      "Sec-Fetch-Site": "same-origin",
    },
  });
  expect(logout.status()).toBe(204);
  await logout.dispose();
  await page.evaluate(() =>
    window.dispatchEvent(new PageTransitionEvent("pageshow")),
  );
  await expect(page.locator("#login-email")).toBeVisible();
  await expect(title(page)).toHaveCount(0);
  await login(page, data.owner);
  await page.goto(path(data, "main", "task"));
  await ready(page, data.work.main!.task_id);
  await expect(title(page)).toHaveValue("规划任务");
  const renewed = await page.request.get("/api/v1/session");
  expect(renewed.status()).toBe(200);
  const renewedID = (await renewed.json()).session.id;
  await renewed.dispose();
  expect(renewedID !== checkedID).toBe(true);

  await ipc("hold-read", { resource: "task" });
  const oldRead = seen.declareIncomplete({
    kind: "canceled-task-read",
    projectID: data.work.main!.project_id,
    targetID: data.work.main!.task_id,
  });
  await button(editor(page), "读取当前值").click();
  await expect
    .poll(async () => {
      const h = await ipc("hold-status");
      return h.started === true && h.finished === false;
    })
    .toBe(true);
  oldRead.authorizeCancellation();
  await page.goto(path(data, "duplicate", "task"));
  await ready(page, data.work.duplicate!.task_id);
  // No manual release can manufacture this success: the original response
  // handler must first report actual cancellation/finish (also checked in Go).
  await expect
    .poll(async () => (await ipc("hold-status")).finished === true)
    .toBe(true);
  await ipc("release-read");
  await expect(details(page)).not.toContainText(data.work.main!.task_id);
  await expect(title(page)).toHaveValue("规划任务");

  await title(page).fill("独验另一 Project 草稿");
  await navigation.getByRole("link", { name: "项目设置", exact: true }).click();
  await expect(workDialog(page)).toBeVisible();
  await expect(ownerDialog).toHaveCount(0);
  await expect(modelDialog).toHaveCount(0);
  await button(workDialog(page), "继续编辑").click();
  await expect(title(page)).toHaveValue("独验另一 Project 草稿");
  await navigation.getByRole("link", { name: "项目设置", exact: true }).click();
  await button(workDialog(page), "放弃并离开").click();
  const ownerForm = page.getByRole("form", {
    name: "项目基本信息",
    exact: true,
  });
  await expect(ownerForm).toBeVisible();
  await ownerForm
    .getByLabel("项目描述", { exact: true })
    .fill("独验 Owner 草稿");
  await navigation.getByRole("link", { name: "任务规划", exact: true }).click();
  await expect(ownerDialog).toBeVisible();
  await expect(workDialog(page)).toHaveCount(0);
  await button(ownerDialog, "继续编辑").click();
  await expect(ownerForm.getByLabel("项目描述", { exact: true })).toHaveValue(
    "独验 Owner 草稿",
  );
  await navigation.getByRole("link", { name: "任务规划", exact: true }).click();
  await button(ownerDialog, "放弃并离开").click();
  await ready(page);
  await expect(recovery(page)).toHaveCount(0);
  await navigation.getByRole("link", { name: "项目设置", exact: true }).click();
  await expect(ownerForm).toBeVisible();
  await page.getByRole("link", { name: "Providers", exact: true }).click();
  await button(
    page.locator('[aria-label="项目模型操作"]'),
    "创建 Provider",
  ).click();
  const provider = page.getByRole("dialog", {
    name: "创建 Provider",
    exact: true,
  });
  await provider
    .getByLabel("Provider 名称", { exact: true })
    .fill("独验 Model 草稿");
  await page.goBack();
  await expect(modelDialog).toBeVisible();
  await expect(workDialog(page)).toHaveCount(0);
  await expect(ownerDialog).toHaveCount(0);
  await button(modelDialog, "继续编辑").click();
  await expect(
    provider.getByLabel("Provider 名称", { exact: true }),
  ).toHaveValue("独验 Model 草稿");
  await page.goBack();
  await button(modelDialog, "放弃并离开").click();
  await expect(ownerForm).toBeVisible();
  expect(modelWrites).toBe(0);

  for (const credential of [data.admin, data.other]) {
    const isolated = await browser.newContext();
    try {
      const denied = await isolated.newPage(),
        deniedSeen = observe(denied);
      await denied.goto(path(data, "duplicate", "task"));
      await login(denied, credential);
      await expect(
        denied.getByRole("heading", { name: "项目不可用", exact: true }),
      ).toBeVisible();
      expect(deniedSeen.requests).toHaveLength(0);
    } finally {
      await isolated.close();
    }
  }
  complete({
    identity: true,
    old_tail: true,
    project_isolation: true,
    installed_guards: true,
    owner_gate: true,
    revoked_session: checkedID,
    renewed_session: renewedID,
    bodies: await seen.verify(1),
  });
});
