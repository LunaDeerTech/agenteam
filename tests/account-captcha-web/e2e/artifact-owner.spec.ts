import { test, expect, type Page } from "@playwright/test";
import {
  artifactMaterial,
  observeArtifacts,
  artifactScreenshot,
} from "./artifact-owner.helpers";
const owners = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeArtifacts>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = owners.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status, info.errors);
  }
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const detail = (page: Page) => page.locator(".artifact-detail");
const text = (page: Page) => page.getByLabel("产物文本内容", { exact: true });
async function press(page: Page, name: string) {
  const target = button(page, name);
  await expect(target).toBeEnabled();
  await target.focus();
  await page.keyboard.press("Enter");
}
async function cookie(page: Page, origin: string, value: string) {
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value,
        url: origin,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
}
async function kind(page: Page, value: string) {
  await press(page, "产物类型");
  const list = page.getByRole("listbox", { name: "产物类型", exact: true });
  await list.getByRole("option", { name: value, exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(button(page, "产物类型")).toBeFocused();
}

test("real Owner catalog and bounded content [catalog-and-bounded-read]", async ({
  page,
}) => {
  const data = artifactMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await cookie(page, data.origin, data.cookie);
  const seen = await observeArtifacts(page, data);
  owners.set(page, seen);
  await page.goto(data.route.replace(/\/artifacts$/, ""));
  const artifactLink = page
    .getByRole("navigation", { name: "项目导航" })
    .getByRole("link", { name: "产物", exact: true });
  await expect(artifactLink).toBeVisible();
  await artifactLink.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(data.origin + data.route);
  await expect(artifactLink).toHaveAttribute("aria-current", "page");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(
    50,
  );
  await seen.verify();
  expect(seen.last().items).toEqual(data.rows.slice(0, 50));
  await artifactScreenshot(page, "wide-light-catalog");
  seen.stage("second-page");
  await press(page, "更多产物");
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(
    52,
  );
  await expect(button(page, "更多产物")).toHaveCount(0);
  await seen.verify();
  expect(seen.last().items).toEqual(data.rows.slice(50));
  seen.stage("loading-and-first-window");
  await press(page, `查看产物 ${data.text_artifact.name}`);
  await expect(
    detail(page).getByRole("heading", { name: "产物详情", exact: true }),
  ).toBeFocused();
  await expect(
    detail(page).getByText("正在读取产物内容", { exact: true }),
  ).toBeVisible();
  await expect(button(page, "重新读取")).toBeDisabled();
  seen.releaseLoading();
  await expect(text(page)).toHaveText(data.first_text);
  await expect(button(page, "上一段")).toBeDisabled();
  await expect(button(page, "下一段")).toBeEnabled();
  await seen.verify();
  await artifactScreenshot(page, "wide-light-first-window");
  seen.stage("utf8-second-window");
  await press(page, "下一段");
  await expect(text(page)).toHaveText(data.second_text);
  await expect(button(page, "下一段")).toBeDisabled();
  await expect(button(page, "上一段")).toBeEnabled();
  expect(
    await page.evaluate(() => (window as any).artifactExecuted),
  ).toBeUndefined();
  await expect(detail(page).locator("script")).toHaveCount(0);
  await seen.verify();
  await press(page, "上一段");
  await expect(text(page)).toHaveText(data.first_text);
  await seen.verify();
  await press(page, "返回产物列表");
  await expect(detail(page)).toHaveCount(0);
  await expect(
    button(page, `查看产物 ${data.text_artifact.name}`),
  ).toBeFocused();
  seen.stage("literal-filter");
  await page
    .getByRole("textbox", { name: "产物名称", exact: true })
    .fill("%_!");
  await kind(page, "生成产物");
  await press(page, "应用筛选");
  await expect(
    page.getByRole("heading", { name: "产物", exact: true }),
  ).toBeFocused();
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(1);
  await seen.verify();
  expect(seen.last().items).toEqual([data.text_artifact]);
  await kind(page, "用户上传");
  await press(page, "应用筛选");
  await expect(page.getByText("暂无匹配的产物", { exact: true })).toBeVisible();
  await seen.verify();
  expect(seen.last().items).toEqual([]);
  await press(page, "清除筛选");
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(
    50,
  );
  await expect(
    page.getByRole("textbox", { name: "产物名称", exact: true }),
  ).toHaveValue("");
  await seen.verify();
  seen.stage("image-metadata");
  await page
    .getByRole("textbox", { name: "产物名称", exact: true })
    .fill(data.image_artifact.name);
  await press(page, "应用筛选");
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(1);
  await seen.verify();
  await press(page, `查看产物 ${data.image_artifact.name}`);
  await expect(
    detail(page).getByText("此类型暂不支持在线预览。", { exact: true }),
  ).toBeVisible();
  await expect(detail(page).locator("img,a,video,audio,iframe")).toHaveCount(0);
  await expect(text(page)).toHaveCount(0);
  await seen.verify();
  expect(seen.last().projection).toBe("image_ref");
  seen.stage("file-metadata");
  await page
    .getByRole("textbox", { name: "产物名称", exact: true })
    .fill(data.file_artifact.name);
  await press(page, "应用筛选");
  await expect(detail(page)).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(1);
  await seen.verify();
  await press(page, `查看产物 ${data.file_artifact.name}`);
  await expect(
    detail(page).getByText("此类型暂不支持在线预览。", { exact: true }),
  ).toBeVisible();
  await expect(detail(page).locator("img,a,video,audio,iframe")).toHaveCount(0);
  await seen.verify();
  expect(seen.last().projection).toBe("file_ref");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await detail(page)
    .getByRole("heading", { name: "产物详情", exact: true })
    .scrollIntoViewIfNeeded();
  await artifactScreenshot(page, "narrow-dark-file");
  seen.stage("narrow-text");
  await page
    .getByRole("textbox", { name: "产物名称", exact: true })
    .fill("%_!");
  await press(page, "应用筛选");
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(1);
  await seen.verify();
  await press(page, `查看产物 ${data.text_artifact.name}`);
  await expect(text(page)).toHaveText(data.first_text);
  await seen.verify();
  await press(page, "下一段");
  await expect(text(page)).toHaveText(data.second_text);
  await seen.verify();
  await text(page).focus();
  await page.keyboard.press("Tab");
  await expect(button(page, "上一段")).toBeFocused();
  await detail(page)
    .getByRole("heading", { name: "产物详情", exact: true })
    .scrollIntoViewIfNeeded();
  await artifactScreenshot(page, "narrow-dark-text");
  // Change the real incoming Session cookie while the existing page is open.
  // A bootstrap administrator is still not this ordinary Owner. No fake 404.
  seen.stage("nonowner-refusal");
  await cookie(page, data.origin, data.foreign_cookie);
  await press(page, "重新读取");
  await expect(
    page.getByRole("alert").getByRole("heading", {
      name: "当前产物不存在或不可访问，请重新确认访问权限。",
      exact: true,
    }),
  ).toBeVisible();
  await expect(detail(page)).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(0);
  await expect(text(page)).toHaveCount(0);
  await seen.verify();
  expect(seen.last().code).toBe("NOT_FOUND");
  seen.stage("owner-recovery");
  await cookie(page, data.origin, data.cookie);
  await press(page, "刷新产物");
  await expect(
    button(page, `查看产物 ${data.text_artifact.name}`),
  ).toBeEnabled();
  await seen.verify();
  await press(page, `查看产物 ${data.text_artifact.name}`);
  await expect(text(page)).toHaveText(data.first_text);
  await seen.verify();
  seen.stage("project-switch");
  await page
    .getByRole("navigation", { name: "系统导航" })
    .getByRole("link", { name: "项目", exact: true })
    .click();
  await expect(detail(page)).toHaveCount(0);
  await page
    .getByRole("link", { name: "artifact-browser-other", exact: true })
    .click();
  await page
    .getByRole("navigation", { name: "项目导航" })
    .getByRole("link", { name: "产物", exact: true })
    .click();
  await expect(page).toHaveURL(data.origin + data.other_route);
  await expect(page.getByRole("button", { name: /^查看产物 / })).toHaveCount(1);
  await expect(button(page, `查看产物 ${data.text_artifact.name}`)).toHaveCount(
    0,
  );
  await seen.verify();
  await press(page, `查看产物 ${data.other_artifact.name}`);
  await expect(text(page)).toHaveText(
    "The other owned project remains isolated.\n",
  );
  await seen.verify();
  seen.stage("complete");
  await seen.complete();
});
