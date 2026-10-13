import { test, expect, type Page } from "@playwright/test";
import {
  contentMaterial,
  observeContent,
  contentScreenshot,
  type ContentInput,
} from "./artifact-content.helpers";
const owners = new WeakMap<Page, Awaited<ReturnType<typeof observeContent>>>();
test.afterEach(async ({ page }, info) => {
  const owner = owners.get(page);
  try {
    await owner?.retire();
  } finally {
    owner?.save(info.status, info.errors);
  }
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const dialog = (page: Page) =>
  page.getByRole("dialog", { name: "新建文本产物", exact: true });
async function press(page: Page, name: string) {
  const target = button(page, name);
  await expect(target).toBeEnabled();
  await target.focus();
  await page.keyboard.press("Enter");
}
async function draft(page: Page, input: ContentInput) {
  await press(page, "新建文本产物");
  const form = dialog(page).getByRole("form", {
    name: "创建文本产物",
    exact: true,
  });
  await expect(
    form.getByRole("textbox", { name: "产物名称", exact: true }),
  ).toBeFocused();
  await form
    .getByRole("textbox", { name: "产物名称", exact: true })
    .fill(input.name);
  await form
    .getByRole("textbox", { name: "描述", exact: true })
    .fill(input.description ?? "");
  if (input.media_type === "text/markdown") {
    await press(page, "文本格式");
    await page
      .getByRole("listbox", { name: "文本格式", exact: true })
      .getByRole("option", { name: "Markdown", exact: true })
      .focus();
    await page.keyboard.press("Enter");
  }
  await form
    .getByRole("textbox", { name: "正文", exact: true })
    .fill(input.content);
}
async function confirmed(page: Page, input: ContentInput) {
  await expect(
    dialog(page)
      .getByRole("status")
      .filter({ hasText: "创建已确认；已读取当前产物列表与正文。" }),
  ).toBeVisible();
  await expect(button(page, "创建产物")).toHaveCount(0);
  await press(page, "返回产物");
  await expect(dialog(page)).toHaveCount(0);
  await expect(page.getByLabel("产物文本内容", { exact: true })).toHaveText(
    input.content,
  );
  await expect(page.locator(".artifact-detail script")).toHaveCount(0);
  expect(
    await page.evaluate(() => (window as any).contentExecuted),
  ).toBeUndefined();
}
async function uncertain(page: Page) {
  await expect(
    dialog(page).getByRole("status").filter({
      hasText: "创建结果尚不确定，请查询原创建结果；不会自动重发或取消。",
    }),
  ).toBeVisible();
  await expect(button(page, "创建产物")).toBeDisabled();
  await expect(button(page, "查询原创建结果")).toBeEnabled();
  await expect(button(page, "重试原创建请求")).toHaveCount(0);
  await expect(button(page, "取消原创建")).toHaveCount(0);
  await expect(
    dialog(page).getByRole("textbox", { name: "正文", exact: true }),
  ).toBeDisabled();
}
test("current Owner creates and recovers the original request [create-and-recover-original]", async ({
  page,
}) => {
  const data = contentMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: data.origin,
      httpOnly: true,
      sameSite: "Lax",
      secure: false,
    },
  ]);
  const seen = await observeContent(page, data);
  owners.set(page, seen);
  await page.goto(data.route);
  await expect(button(page, "新建文本产物")).toBeEnabled();
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(0);
  await seen.verify(1);
  seen.stage("normal-create");
  await draft(page, data.inputs[0]);
  await contentScreenshot(page, "wide-light-draft");
  await press(page, "创建产物");
  try {
    await seen.reached();
    await expect(
      dialog(page)
        .getByRole("status")
        .filter({ hasText: "正在处理原创建请求" }),
    ).toBeVisible();
    await expect(button(page, "创建产物")).toBeDisabled();
  } finally {
    seen.release();
  }
  await confirmed(page, data.inputs[0]);
  await seen.verify(4);
  await page
    .locator(".artifact-detail")
    .getByRole("heading", { name: "产物详情", exact: true })
    .scrollIntoViewIfNeeded();
  await contentScreenshot(page, "wide-light-confirmed");
  seen.stage("published-ack-unknown");
  await draft(page, data.inputs[1]);
  await press(page, "创建产物");
  await uncertain(page);
  await seen.verify(5);
  await press(page, "查询原创建结果");
  await confirmed(page, data.inputs[1]);
  await seen.verify(8);
  seen.stage("not-observed-original-retry");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await draft(page, data.inputs[2]);
  await press(page, "创建产物");
  await uncertain(page);
  await seen.verify(9);
  await press(page, "查询原创建结果");
  await expect(
    dialog(page).getByRole("status").filter({
      hasText: "尚未观察到原创建。可明确重试原请求；这不表示此前一定未提交。",
    }),
  ).toBeVisible();
  await expect(button(page, "重试原创建请求")).toBeEnabled();
  await expect(button(page, "取消原创建")).toHaveCount(0);
  await expect(button(page, "创建产物")).toBeDisabled();
  await seen.verify(10);
  await button(page, "重试原创建请求").scrollIntoViewIfNeeded();
  await contentScreenshot(page, "narrow-dark-not-observed");
  await press(page, "重试原创建请求");
  await confirmed(page, data.inputs[2]);
  await seen.verify(13);
  seen.stage("pending-original-cancel");
  await draft(page, data.inputs[3]);
  await press(page, "创建产物");
  await uncertain(page);
  await seen.verify(14);
  await press(page, "查询原创建结果");
  await expect(
    dialog(page)
      .getByRole("status")
      .filter({ hasText: "原创建仍在处理中，请稍后明确查询结果。" }),
  ).toBeVisible();
  await expect(button(page, "取消原创建")).toBeEnabled();
  await expect(button(page, "重试原创建请求")).toHaveCount(0);
  await seen.verify(15);
  await press(page, "取消原创建");
  await expect(
    dialog(page)
      .getByRole("status")
      .filter({ hasText: "原创建已取消，清理已完成。" }),
  ).toBeVisible();
  await expect(button(page, "创建产物")).toHaveCount(0);
  await expect(button(page, "取消原创建")).toHaveCount(0);
  await seen.verify(16);
  const close = dialog(page).locator(".dialog-footer").getByRole("button", {
    name: "关闭",
    exact: true,
  });
  await expect(close).toBeEnabled();
  await close.scrollIntoViewIfNeeded();
  await contentScreenshot(page, "narrow-dark-cancelled");
  await close.focus();
  await page.keyboard.press("Enter");
  await expect(dialog(page)).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(3);
  await expect(button(page, `查看产物 ${data.inputs[3].name}`)).toHaveCount(0);
  await seen.complete();
});
