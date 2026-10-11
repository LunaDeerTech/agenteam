import { test, expect, type Page } from "@playwright/test";
import {
  observeTaskRequests,
  requireCommittedLookup,
  type TaskObservationProfile,
} from "./task-review.helpers";
import {
  milestoneEditMaterial,
  milestoneEditProfile,
  observeMilestoneEdit,
  milestoneEditScreenshot,
  type MilestoneEditMaterial,
} from "./milestone-edit.helpers";

test("rejects mixed and incomplete Milestone Edit profiles before browser setup [milestone-edit-profile-controls]", async () => {
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
    mode: "milestone-edit-normal" as const,
    cookie: "private-control",
    project_id: "01990000-0000-7000-8000-000000000001",
    milestone_id: "01990000-0000-7000-8000-000000000002",
  };
  const profile: TaskObservationProfile = {
    ...milestoneEditProfile(data),
    endpoint: () => undefined,
  };
  const page = {
    on() {
      throw Error("CONTROL_REACHED_NATIVE_SETUP");
    },
  } as unknown as Page;
  for (const changes of [
    { milestoneUpdateWrites: undefined },
    { prefix: "milestone-create" },
    { lookupCommand: "work.milestone.create" },
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
      "variableWrites",
      "knowledgeTextWrites",
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
    { milestone_id: undefined },
    { milestone_id: "not-an-id" },
  ])
    await expect(
      observeTaskRequests(page, { ...data, ...changes }, profile),
    ).rejects.toThrow("MILESTONE_UPDATE_PROFILE_MISMATCH");
  await expect(
    observeTaskRequests(
      page,
      { ...data, mode: "milestone-edit-lost" },
      profile,
    ),
  ).rejects.toThrow("MILESTONE_UPDATE_PROFILE_MISMATCH");
  for (const mode of ["milestone-edit-normal", "milestone-edit-lost"] as const)
    await expect(
      observeTaskRequests(
        page,
        { ...data, mode },
        { ...milestoneEditProfile({ mode }), endpoint: () => undefined },
      ),
    ).rejects.toThrow("CONTROL_REACHED_NATIVE_SETUP");
  await expect(
    observeTaskRequests(
      page,
      { ...data, mode: "first-milestone-and-sprint-entry" },
      {
        ...profile,
        prefix: "milestone-create",
        lookupCommand: "work.milestone.create",
        milestoneUpdateWrites: undefined,
        milestoneCreateWrites: true,
      },
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
async function begin(page: Page, data: MilestoneEditMaterial) {
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
  const seen = await observeMilestoneEdit(page, data);
  return {
    seen,
    async enter() {
      await page.goto(data.route);
      await press(page, "Milestone");
      const option = page
        .getByRole("listbox", { name: "Milestone", exact: true })
        .getByRole("option", { name: data.title, exact: true });
      await option.focus();
      await page.keyboard.press("Enter");
      await expect(
        page.getByRole("button", { name: "编辑 Milestone", exact: true }),
      ).toBeEnabled();
      await seen.verify();
      seen.current(data.milestone);
    },
  };
}
async function open(page: Page) {
  await press(page, "编辑 Milestone");
  const form = page.getByRole("form", { name: "编辑 Milestone", exact: true });
  await expect(form).toBeVisible();
  await expect(
    form.getByRole("textbox", { name: "Milestone 标题", exact: true }),
  ).toBeFocused();
  return form;
}
async function refresh(
  page: Page,
  seen: Awaited<ReturnType<typeof observeMilestoneEdit>>,
  expected: Record<string, any>,
) {
  const gets = seen.requests("milestone").length,
    lists = seen.requests("milestones").length;
  await press(page, "刷新任务看板");
  await expect(
    page.getByRole("button", { name: "编辑 Milestone", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  expect(seen.requests("milestone")).toHaveLength(gets + 1);
  expect(seen.requests("milestones")).toHaveLength(lists + 1);
  seen.current(expected);
}

test.describe("real Milestone edit", () => {
  const observations = new WeakMap<
    Page,
    Awaited<ReturnType<typeof observeMilestoneEdit>>
  >();
  test.afterEach(async ({ page }, info) => {
    const seen = observations.get(page);
    try {
      await seen?.retire();
    } finally {
      seen?.save(info.status);
    }
  });
  test("edits, preserves a no-op, and explicitly clears the current Milestone [milestone-edit-normal]", async ({
    page,
  }) => {
    const data = milestoneEditMaterial(),
      started = await begin(page, data),
      seen = started.seen;
    observations.set(page, seen);
    await started.enter();
    let form = await open(page);
    await expect(
      form.getByRole("textbox", { name: "Milestone 标题", exact: true }),
    ).toHaveValue(data.title);
    await expect(
      form.getByRole("textbox", { name: "Milestone 描述", exact: true }),
    ).toHaveValue(data.description);
    await form
      .getByRole("textbox", { name: "Milestone 标题", exact: true })
      .fill("Discarded draft");
    await press(page, "取消编辑 Milestone");
    await expect(form).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "编辑 Milestone", exact: true }),
    ).toBeFocused();
    expect(seen.requests("update")).toHaveLength(0);
    form = await open(page);
    await form
      .getByRole("textbox", { name: "Milestone 标题", exact: true })
      .fill(data.updated_title);
    await form
      .getByRole("textbox", { name: "Milestone 描述", exact: true })
      .fill(data.updated_description);
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await milestoneEditScreenshot(page, data, "1440-light-edit");
    await press(page, "保存 Milestone");
    await expect(form).toHaveCount(0);
    const heading = page.locator(".selected-milestone h2");
    await expect(heading).toHaveText(data.updated_title);
    await expect(heading).toBeFocused();
    await seen.verify();
    let current = seen.result(0, data.milestone);
    seen.current(current);
    await refresh(page, seen, current);
    form = await open(page);
    const title = form.getByRole("textbox", {
      name: "Milestone 标题",
      exact: true,
    });
    await title.fill(data.updated_title + "x");
    await title.fill(data.updated_title);
    await press(page, "保存 Milestone");
    await expect(form).toHaveCount(0);
    await expect(heading).toBeFocused();
    await seen.verify();
    current = seen.result(1, current);
    seen.current(current);
    await refresh(page, seen, current);
    form = await open(page);
    await form
      .getByRole("textbox", { name: "Milestone 描述", exact: true })
      .fill("");
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(
      await page.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    ).toBe(true);
    await milestoneEditScreenshot(page, data, "390-dark-clear");
    await press(page, "保存 Milestone");
    await expect(form).toHaveCount(0);
    await expect(heading).toBeFocused();
    await expect(
      page.locator(".selected-milestone .milestone-description"),
    ).toHaveText("暂无描述。");
    await seen.verify();
    current = seen.result(2, current);
    seen.current(current);
    await refresh(page, seen, current);
    expect(seen.requests("update")).toHaveLength(3);
    expect(seen.requests("lookup")).toHaveLength(0);
    await milestoneEditScreenshot(page, data, "390-dark-current");
    await seen.endDocument();
  });
  test("looks up the original Milestone update after its committed response is cut [milestone-edit-lost]", async ({
    page,
  }) => {
    const data = milestoneEditMaterial(),
      started = await begin(page, data),
      seen = started.seen;
    observations.set(page, seen);
    await started.enter();
    const form = await open(page);
    await form
      .getByRole("textbox", { name: "Milestone 标题", exact: true })
      .fill(data.updated_title);
    await form
      .getByRole("textbox", { name: "Milestone 描述", exact: true })
      .fill(data.updated_description);
    await press(page, "保存 Milestone");
    const recovery = page.getByRole("region", {
      name: "Milestone 保存状态",
      exact: true,
    });
    await expect(
      recovery.getByText(
        "Milestone 保存结果尚未确认。请查证原保存，勿再次提交。",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(form).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "编辑 Milestone", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "新建 Milestone", exact: true }),
    ).toBeDisabled();
    await seen.verify();
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    await milestoneEditScreenshot(page, data, "1440-light-uncertain");
    await press(page, "查询原 Milestone 保存结果");
    const heading = page.locator(".selected-milestone h2");
    await expect(heading).toHaveText(data.updated_title);
    await expect(heading).toBeFocused();
    await expect(
      page.getByRole("button", { name: "编辑 Milestone", exact: true }),
    ).toBeEnabled();
    await seen.verify();
    const current = seen.result(0, data.milestone, true);
    seen.originalLookup();
    seen.current(current);
    await refresh(page, seen, current);
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(1);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await milestoneEditScreenshot(page, data, "390-dark-recovered");
    await seen.endDocument();
  });
});
