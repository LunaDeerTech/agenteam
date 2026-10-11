import { test, expect, type Page } from "@playwright/test";
import {
  milestoneCreateMaterial,
  observeMilestoneCreate,
  milestoneCreateScreenshot,
} from "./milestone-create.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeMilestoneCreate>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
async function press(page: Page, name: string) {
  const button = page.getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}
test("creates the first Milestone and only enters Sprint planning [first-milestone-and-sprint-entry]", async ({
  page,
}) => {
  const data = milestoneCreateMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value: data.cookie,
        url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
  const seen = await observeMilestoneCreate(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(
    page.getByRole("button", { name: "新建 Milestone", exact: true }),
  ).toBeEnabled();
  await expect(
    page.getByText("暂无 Milestone，暂时无法创建 Sprint。", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "新建 Sprint", exact: true }),
  ).toBeDisabled();
  await seen.verify();
  const originalProject = seen.milestoneRead("project");
  expect(originalProject).toMatchObject({
    id: data.project_id,
    current_sprint_id: null,
    version: data.project_version,
  });
  expect(seen.milestoneRead("milestones")).toEqual({ items: [] });
  await press(page, "新建 Milestone");
  const form = page.getByRole("form", { name: "新建 Milestone", exact: true });
  await expect(form).toBeVisible();
  const title = form.getByRole("textbox", {
    name: "Milestone 标题",
    exact: true,
  });
  await expect(title).toBeFocused();
  await title.fill(data.title);
  await form
    .getByRole("textbox", { name: "Milestone 描述", exact: true })
    .fill(data.description);
  expect(seen.requests("create")).toHaveLength(0);
  await press(page, "确认创建 Milestone");
  await expect(form).toHaveCount(0);
  const heading = page.locator(".selected-milestone h2");
  await expect(heading).toHaveText(data.title);
  await expect(heading).toBeFocused();
  await expect(
    page.getByRole("button", { name: "新建 Sprint", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  seen.requireResult(originalProject);
  await expect(page).toHaveURL(
    new URL(data.route, process.env.AGENTEAM_AUTH_WEB_ORIGIN!).href,
  );
  await expect(
    page
      .getByRole("navigation", { name: "Sprint 列表", exact: true })
      .getByRole("button"),
  ).toHaveCount(0);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await milestoneCreateScreenshot(page, data, "1440-light-first-milestone");
  await press(page, "新建 Sprint");
  const sprint = page.getByRole("form", { name: "新建 Sprint", exact: true });
  await expect(sprint).toBeVisible();
  const parent = sprint.getByRole("button", {
    name: "所属 Milestone",
    exact: true,
  });
  await expect(parent).toContainText(data.title);
  await parent.focus();
  await expect(parent).toBeFocused();
  await page.keyboard.press("Enter");
  const option = page
    .getByRole("listbox", { name: "所属 Milestone", exact: true })
    .getByRole("option", { name: data.title, exact: true });
  await expect(option).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Escape");
  await expect(page.locator(".ui-popover")).toHaveCount(0);
  await expect(parent).toBeFocused();
  await expect(
    sprint.getByRole("textbox", { name: "Sprint 标题", exact: true }),
  ).toHaveValue("");
  await expect(
    sprint.getByRole("button", { name: "确认创建 Sprint", exact: true }),
  ).toBeDisabled();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  await milestoneCreateScreenshot(page, data, "390-dark-sprint-entry");
  await press(page, "取消创建 Sprint");
  await expect(sprint).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "新建 Sprint", exact: true }),
  ).toBeFocused();
  await seen.verify();
  seen.requireResult(originalProject);
  await seen.endDocument();
});
