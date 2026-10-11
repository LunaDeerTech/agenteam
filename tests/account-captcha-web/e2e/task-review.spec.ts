import { test, expect, type Page } from "@playwright/test";
import { material, observeReview } from "./task-review.helpers";

const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
const comment = (page: Page) => page.getByRole("textbox", { name: "评审说明", exact: true });

async function openTask(page: Page) {
  const data = material();
  await page.context().addCookies([{
    name: "agenteam_local_session", value: data.cookie,
    url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!, httpOnly: true, sameSite: "Lax", secure: false,
  }]);
  const seen = observeReview(page, data);
  await page.goto(data.route);
  await expect(page.getByRole("heading", { name: "任务详情", exact: true })).toBeVisible();
  await expect(page.getByText(data.title, { exact: true }).first()).toBeVisible();
  await seen.verify();
  return { data, seen };
}

test("current Owner completes real work review [review-complete]", async ({ page }) => {
  const { data, seen } = await openTask(page);
  await button(page, "提交评审").click();
  await page.getByLabel("评审 Agent", { exact: true }).selectOption(data.reviewer_id);
  await comment(page).fill("Browser review of the completed work.");
  await button(page, "确认提交评审").click();
  await expect(button(page, "接受并完成")).toBeVisible();
  await seen.verify();
  const review = seen.receipt("transfer", 0);
  expect(review.task.id).toBe(data.task_id);
  expect(review.task.state).toBe("in_review");
  expect(review.task.assignee_agent_id).toBe(data.reviewer_id);
  expect(BigInt(review.task.version)).toBe(BigInt(data.version) + 1n);

  await button(page, "接受并完成").click();
  await expect(page.getByLabel("评审 Agent", { exact: true })).toHaveCount(0);
  await comment(page).fill("Browser accepts the reviewed work.");
  await button(page, "确认接受并完成").click();
  await expect(button(page, "接受并完成")).toHaveCount(0);
  await expect(button(page, "退回修改")).toHaveCount(0);
  await seen.verify();
  const done = seen.receipt("transfer", 1);
  expect(done.task.state).toBe("done");
  expect(done.task.assignee_agent_id).toBe(data.reviewer_id);
  expect(BigInt(done.task.version)).toBe(BigInt(data.version) + 2n);
  seen.save();
});

test("original rework confirmation and Session revocation [lost-confirmation-lookup-and-session-revocation]", async ({ page }) => {
  const { data, seen } = await openTask(page);
  await button(page, "退回修改").click();
  await page.getByLabel("执行 Agent", { exact: true }).selectOption(data.worker_id);
  await comment(page).fill("Browser requests a revision before acceptance.");
  await button(page, "确认退回修改").click();
  await expect(page.getByText("提交结果待确认", { exact: true })).toBeVisible();
  await expect(button(page, "查询原操作结果")).toBeEnabled();
  await seen.verify();
  await button(page, "查询原操作结果").click();
  await expect(page.getByText("提交结果待确认", { exact: true })).toHaveCount(0);
  await seen.verify();
  seen.originalLookup();
  const original = seen.receipt("lookup", 0);
  expect(original.status).toBe("committed");
  expect(original.receipt.task.id).toBe(data.task_id);
  expect(original.receipt.task.state).toBe("todo");
  expect(original.receipt.task.assignee_agent_id).toBe(data.worker_id);
  expect(BigInt(original.receipt.task.version)).toBe(BigInt(data.version) + 1n);

  // The existing settings logout invokes the real Account service. Returning
  // to the protected Task route must not retain the old Owner's Task/actions.
  await page.goto("/settings/profile");
  await expect(button(page, "退出登录")).toBeVisible();
  await seen.verify();
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await seen.verify();
  await page.goto(data.route);
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(page.getByRole("heading", { name: "任务详情", exact: true })).toHaveCount(0);
  await expect(page.getByText(data.title, { exact: true })).toHaveCount(0);
  await expect(button(page, "查询原操作结果")).toHaveCount(0);
  await seen.verify();
  seen.originalLookup();
  seen.save();
});
