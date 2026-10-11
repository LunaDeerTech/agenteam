import { test, expect, type Page } from "@playwright/test";
import { material, observeReview, taskScreenshot } from "./task-review.helpers";

const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const comment = (page: Page) =>
  page.getByRole("textbox", { name: "评审说明", exact: true });
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");
const uncertain = (page: Page) =>
  page.getByText(/^提交结果待确认。请查询原操作结果，不要重复提交。$/);

async function chooseAgent(page: Page, label: string, option: string) {
  // UiSelect is the existing accessible popover/listbox, not a native select.
  const trigger = button(page, label);
  await trigger.click();
  await page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true })
    .click();
  await expect(trigger).toBeFocused();
}

async function published(page: Page, state: string, version: bigint) {
  await expect(fact(page, "状态")).toHaveText(state);
  await expect(fact(page, "版本")).toHaveText(version.toString());
  await expect(button(page, "重新读取任务")).toBeEnabled();
}

async function openTask(page: Page) {
  const data = material();
  await page.setViewportSize(
    data.mode === "review-complete"
      ? { width: 1440, height: 960 }
      : { width: 390, height: 844 },
  );
  await page.emulateMedia({ reducedMotion: "reduce" });
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
  const seen = observeReview(page, data);
  await page.goto(data.route);
  await expect(
    page.getByRole("heading", { name: "任务详情", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(data.title, { exact: true }).first(),
  ).toBeVisible();
  await published(
    page,
    data.mode === "review-complete" ? "进行中" : "评审中",
    BigInt(data.version),
  );
  await seen.verify();
  return { data, seen };
}

test("current Owner completes real work review [review-complete]", async ({
  page,
}) => {
  const { data, seen } = await openTask(page);
  await button(page, "提交评审").click();
  await chooseAgent(page, "评审 Agent", seen.agentOption(data.reviewer_id));
  await comment(page).fill("Browser review of the completed work.");
  await button(page, "确认提交评审").focus();
  await expect(button(page, "确认提交评审")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(button(page, "接受并完成")).toBeVisible();
  await published(page, "评审中", BigInt(data.version) + 1n);
  await seen.verify();
  const review = seen.receipt("transfer", 0);
  expect(review.task.id).toBe(data.task_id);
  expect(review.task.state).toBe("in_review");
  expect(review.task.assignee_agent_id).toBe(data.reviewer_id);
  expect(BigInt(review.task.version)).toBe(BigInt(data.version) + 1n);
  await taskScreenshot(page, "review-desktop");

  await button(page, "接受并完成").click();
  await expect(button(page, "评审 Agent")).toHaveCount(0);
  await expect(button(page, "执行 Agent")).toHaveCount(0);
  await comment(page).fill("Browser accepts the reviewed work.");
  await button(page, "确认接受并完成").click();
  await published(page, "已完成", BigInt(data.version) + 2n);
  await expect(button(page, "接受并完成")).toHaveCount(0);
  await expect(button(page, "退回修改")).toHaveCount(0);
  await seen.verify();
  const done = seen.receipt("transfer", 1);
  expect(done.task.state).toBe("done");
  expect(done.task.assignee_agent_id).toBe(data.reviewer_id);
  expect(BigInt(done.task.version)).toBe(BigInt(data.version) + 2n);
  await taskScreenshot(page, "done-desktop");
  seen.save();
});

test("original rework confirmation and Session revocation [lost-confirmation-lookup-and-session-revocation]", async ({
  page,
}) => {
  const { data, seen } = await openTask(page);
  await button(page, "退回修改").click();
  await chooseAgent(page, "执行 Agent", seen.agentOption(data.worker_id));
  await comment(page).fill("Browser requests a revision before acceptance.");
  await button(page, "确认退回修改").click();
  await expect(uncertain(page)).toBeVisible();
  await expect(button(page, "查询原操作结果")).toBeEnabled();
  await seen.verify();
  await taskScreenshot(page, "uncertain-narrow");
  await button(page, "查询原操作结果").focus();
  await expect(button(page, "查询原操作结果")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(uncertain(page)).toHaveCount(0);
  await published(page, "待执行", BigInt(data.version) + 1n);
  await seen.verify();
  seen.originalLookup();
  const original = seen.receipt("lookup", 0);
  expect(original.status).toBe("committed");
  expect(original.receipt.task.id).toBe(data.task_id);
  expect(original.receipt.task.state).toBe("todo");
  expect(original.receipt.task.assignee_agent_id).toBe(data.worker_id);
  expect(BigInt(original.receipt.task.version)).toBe(BigInt(data.version) + 1n);
  await taskScreenshot(page, "todo-narrow");

  // Keep this SPA/Session alive while the real Account service revokes it.
  // Close the modal through its real action before using the App's logout.
  await button(page, "关闭任务详情").click();
  await expect(drawer(page)).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: data.title, exact: true }),
  ).toBeVisible();
  await expect(button(page, "退出登录")).toBeVisible();
  await seen.verify();
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(page.getByText(data.title, { exact: true })).toHaveCount(0);
  await seen.verify();
  await page.goto(data.route);
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(
    page.getByRole("heading", { name: "任务详情", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText(data.title, { exact: true })).toHaveCount(0);
  await expect(button(page, "查询原操作结果")).toHaveCount(0);
  await seen.verify();
  seen.originalLookup();
  seen.save();
});
