import { test, expect, type Page } from "@playwright/test";
import {
  observeTaskRequests,
  requireCommittedLookup,
  type TaskObservationProfile,
} from "./task-review.helpers";
import {
  sprintEditMaterial,
  sprintEditProfile,
  observeSprintEdit,
  sprintEditScreenshot,
  type SprintEditMaterial,
} from "./sprint-edit.helpers";

test("rejects mixed and incomplete Sprint Edit profiles before browser setup [sprint-edit-profile-controls]", async () => {
  expect(() =>
    requireCommittedLookup({ state: "committed", result: {} }, true),
  ).not.toThrow();
  expect(() =>
    requireCommittedLookup({ status: "committed", receipt: {} }, true),
  ).toThrow();
  for (const state of ["in_progress", "not_observed", undefined])
    expect(() =>
      requireCommittedLookup(
        { state, status: "committed", result: null },
        true,
      ),
    ).toThrow();
  expect(() =>
    requireCommittedLookup({ status: "committed", receipt: {} }),
  ).not.toThrow();
  expect(() =>
    requireCommittedLookup({ state: "committed", result: {} }),
  ).toThrow();
  expect(() =>
    requireCommittedLookup({ status: "in_progress", state: "committed" }),
  ).toThrow();
  const data = {
    mode: "sprint-edit-normal" as const,
    cookie: "private-control",
    project_id: "01990000-0000-7000-8000-000000000001",
    sprint_id: "01990000-0000-7000-8000-000000000002",
    milestone_id: "01990000-0000-7000-8000-000000000003",
  };
  const profile: TaskObservationProfile = {
    ...sprintEditProfile(data),
    endpoint: () => undefined,
  };
  const page = {
    on() {
      throw Error("CONTROL_REACHED_NATIVE_SETUP");
    },
  } as unknown as Page;
  for (const changes of [
    { sprintUpdateWrites: undefined },
    { prefix: "sprint-create" },
    { lookupCommand: "work.sprint.create" },
    { cut: "update" },
    ...[
      "intakeWrites",
      "agentConfiguration",
      "editWrites",
      "timelineReads",
      "agentEditWrites",
      "agentModelPolicy",
      "sprintStartWrites",
      "sprintCreateWrites",
      "skillsReads",
      "milestoneCreateWrites",
      "milestoneUpdateWrites",
      "variableWrites",
      "knowledgeTextWrites",
      "schedulerWrites",
    ].map((key) => ({ [key]: true })),
  ])
    await expect(
      observeTaskRequests(page, data, {
        ...profile,
        ...changes,
      } as TaskObservationProfile),
    ).rejects.toThrow(/PROFILE_MISMATCH/);
  for (const changes of [
    { mode: "other" },
    { sprint_id: undefined },
    { sprint_id: "not-an-id" },
    { milestone_id: undefined },
    { milestone_id: "not-an-id" },
  ])
    await expect(
      observeTaskRequests(page, { ...data, ...changes }, profile),
    ).rejects.toThrow("SPRINT_UPDATE_PROFILE_MISMATCH");
  await expect(
    observeTaskRequests(page, { ...data, mode: "sprint-edit-lost" }, profile),
  ).rejects.toThrow("SPRINT_UPDATE_PROFILE_MISMATCH");
  const defaults: Array<{ mode: string; profile: TaskObservationProfile }> = [
    {
      mode: "milestone-edit-normal",
      profile: {
        ...profile,
        prefix: "milestone-edit",
        sprintUpdateWrites: undefined,
        milestoneUpdateWrites: true,
        lookupCommand: "work.milestone.update",
      },
    },
    {
      mode: "pause-limit-and-resume",
      profile: {
        ...profile,
        prefix: "project-scheduler",
        sprintUpdateWrites: undefined,
        schedulerWrites: true,
        lookupCommand: "update",
      },
    },
    {
      mode: "paused-start-and-current-board",
      profile: {
        ...profile,
        prefix: "sprint-start",
        sprintUpdateWrites: undefined,
        sprintStartWrites: true,
        lookupCommand: "work.sprint.start",
      },
    },
  ];
  for (const legacy of defaults) {
    await expect(
      observeTaskRequests(page, { ...data, mode: legacy.mode }, legacy.profile),
    ).rejects.toThrow("CONTROL_REACHED_NATIVE_SETUP");
    await expect(
      observeTaskRequests(
        page,
        { ...data, mode: legacy.mode },
        { ...legacy.profile, sprintUpdateWrites: true },
      ),
    ).rejects.toThrow(/PROFILE_MISMATCH/);
  }
  for (const mode of ["sprint-edit-normal", "sprint-edit-lost"] as const)
    await expect(
      observeTaskRequests(
        page,
        { ...data, mode },
        { ...sprintEditProfile({ mode }), endpoint: () => undefined },
      ),
    ).rejects.toThrow("CONTROL_REACHED_NATIVE_SETUP");
});

async function press(page: Page, name: string) {
  const button = page.getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}
async function begin(page: Page, data: SprintEditMaterial) {
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      sameSite: "Lax",
      secure: false,
    },
  ]);
  const seen = await observeSprintEdit(page, data);
  return {
    seen,
    async enter() {
      await page.goto(data.route);
      await expect(
        page.getByRole("button", { name: "编辑 Sprint", exact: true }),
      ).toBeEnabled();
      await seen.verify();
      seen.current(data.sprint);
    },
  };
}
async function open(page: Page) {
  await press(page, "编辑 Sprint");
  const form = page.getByRole("form", { name: "编辑 Sprint", exact: true });
  await expect(form).toBeVisible();
  await expect(
    form.getByRole("textbox", { name: "Sprint 标题", exact: true }),
  ).toBeFocused();
  return form;
}
async function refresh(
  page: Page,
  seen: Awaited<ReturnType<typeof observeSprintEdit>>,
  expected: Record<string, any>,
) {
  const gets = seen.requests("sprint").length,
    lists = seen.requests("sprints").length;
  await press(page, "刷新任务看板");
  await expect(
    page.getByRole("button", { name: "编辑 Sprint", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  expect(seen.requests("sprint")).toHaveLength(gets + 1);
  expect(seen.requests("sprints")).toHaveLength(lists + 1);
  seen.current(expected);
}

test.describe("real Sprint edit", () => {
  const observations = new WeakMap<
    Page,
    Awaited<ReturnType<typeof observeSprintEdit>>
  >();
  test.afterEach(async ({ page }, info) => {
    const seen = observations.get(page);
    try {
      await seen?.retire();
    } finally {
      seen?.save(info.status);
    }
  });
  test("edits, preserves a no-op, and explicitly clears the current Sprint [sprint-edit-normal]", async ({
    page,
  }) => {
    const data = sprintEditMaterial(),
      started = await begin(page, data),
      seen = started.seen;
    observations.set(page, seen);
    await started.enter();
    let form = await open(page);
    await expect(
      form.getByRole("textbox", { name: "Sprint 标题", exact: true }),
    ).toHaveValue(data.title);
    await expect(
      form.getByRole("textbox", { name: "Sprint 描述", exact: true }),
    ).toHaveValue(data.description);
    await form
      .getByRole("textbox", { name: "Sprint 标题", exact: true })
      .fill("Discarded draft");
    await press(page, "取消编辑 Sprint");
    await expect(form).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "编辑 Sprint", exact: true }),
    ).toBeFocused();
    expect(seen.requests("update")).toHaveLength(0);
    form = await open(page);
    await form
      .getByRole("textbox", { name: "Sprint 标题", exact: true })
      .fill(data.updated_title);
    await form
      .getByRole("textbox", { name: "Sprint 描述", exact: true })
      .fill(data.updated_description);
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await sprintEditScreenshot(page, data, "1440-light-edit");
    await press(page, "保存 Sprint");
    await expect(form).toHaveCount(0);
    const heading = page.locator(".sprint-heading h2");
    await expect(heading).toHaveText(data.updated_title);
    await expect(heading).toBeFocused();
    await seen.verify();
    let current = seen.result(0, data.sprint);
    seen.current(current);
    await refresh(page, seen, current);
    form = await open(page);
    const title = form.getByRole("textbox", {
      name: "Sprint 标题",
      exact: true,
    });
    await title.fill(data.updated_title + "x");
    await title.fill(data.updated_title);
    await press(page, "保存 Sprint");
    await expect(form).toHaveCount(0);
    await expect(heading).toBeFocused();
    await seen.verify();
    current = seen.result(1, current);
    seen.current(current);
    await refresh(page, seen, current);
    form = await open(page);
    await form
      .getByRole("textbox", { name: "Sprint 描述", exact: true })
      .fill("");
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(
      await page.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    ).toBe(true);
    await sprintEditScreenshot(page, data, "390-dark-clear");
    await press(page, "保存 Sprint");
    await expect(form).toHaveCount(0);
    await expect(heading).toBeFocused();
    await expect(page.locator(".sprint-description")).toHaveText("暂无描述。");
    await seen.verify();
    current = seen.result(2, current);
    seen.current(current);
    await refresh(page, seen, current);
    expect(seen.requests("update")).toHaveLength(3);
    expect(seen.requests("lookup")).toHaveLength(0);
    await sprintEditScreenshot(page, data, "390-dark-current");
    await seen.endDocument();
  });
  test("looks up the original Sprint update after its committed response is cut [sprint-edit-lost]", async ({
    page,
  }) => {
    const data = sprintEditMaterial(),
      started = await begin(page, data),
      seen = started.seen;
    observations.set(page, seen);
    await started.enter();
    const form = await open(page);
    await form
      .getByRole("textbox", { name: "Sprint 标题", exact: true })
      .fill(data.updated_title);
    await form
      .getByRole("textbox", { name: "Sprint 描述", exact: true })
      .fill(data.updated_description);
    await press(page, "保存 Sprint");
    const recovery = page.getByRole("region", {
      name: "Sprint 保存状态",
      exact: true,
    });
    await expect(
      recovery.getByText(
        "Sprint 保存结果尚未确认。请查证原保存，勿再次提交。",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(form).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "编辑 Sprint", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "新建 Milestone", exact: true }),
    ).toBeDisabled();
    await seen.verify();
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    await sprintEditScreenshot(page, data, "1440-light-uncertain");
    await press(page, "查询原 Sprint 保存结果");
    const heading = page.locator(".sprint-heading h2");
    await expect(heading).toHaveText(data.updated_title);
    await expect(heading).toBeFocused();
    await expect(
      page.getByRole("button", { name: "编辑 Sprint", exact: true }),
    ).toBeEnabled();
    await seen.verify();
    const current = seen.result(0, data.sprint, true);
    seen.originalLookup();
    seen.current(current);
    await refresh(page, seen, current);
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(1);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await sprintEditScreenshot(page, data, "390-dark-recovered");
    await seen.endDocument();
  });
});
