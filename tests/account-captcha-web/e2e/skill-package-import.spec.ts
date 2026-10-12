import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  hash,
  observeImport,
  uuid,
  type ImportMaterial,
} from "./skill-package-import.helpers";
import {
  observePackage,
  packageScreenshot,
  type PackageMaterial,
} from "./skill-package.helpers";

test("imports an existing file and downloads its immutable revision", async ({
  page,
  context,
}) => {
  const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
  const data: ImportMaterial = JSON.parse(
    readFileSync(join(directory, "skill-import-material.json"), "utf8"),
  );
  expect(data.mode).toBe("existing-file-import-and-fixed-revision-download");
  expect(data.project_id).toMatch(uuid);
  expect(data.owner_id).toMatch(uuid);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_SKILL_IMPORT_WEB_INPUT_HASH,
  );
  expect(data.filename).toBe("SKILL.md");
  const disk = readFileSync(join(directory, data.filename));
  expect(disk.length).toBe(data.file_size);
  expect(hash(disk)).toBe(data.file_sha256);
  expect(disk.length).toBeLessThanOrEqual(256 << 10);
  await context.addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      secure: false,
      sameSite: "Lax",
    },
  ]);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  const seen = await observeImport(page, data);
  let receipt:
    { skill_id: string; revision: string; version: string } | undefined;
  let importCompleted = false;
  try {
    await page.goto(data.route);
    const region = page.getByRole("region", {
      name: "项目技能库",
      exact: true,
    });
    await expect(
      region.getByRole("heading", {
        name: "项目技能库",
        level: 1,
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      region
        .getByRole("list", { name: "技能目录", exact: true })
        .getByRole("listitem"),
    ).toHaveCount(1);
    await seen.verify();
    const open = region.getByRole("button", {
      name: "导入技能包",
      exact: true,
    });
    await open.focus();
    await expect(open).toBeFocused();
    await page.keyboard.press("Enter");
    // getByLabel matches the complete label text, including UiField's hidden
    // required marker; the accessible name correctly excludes that marker.
    const file = region.getByLabel("SKILL.md 文件 *", { exact: true });
    await expect(file).toHaveAttribute("type", "file");
    await expect(file).toHaveAccessibleName("SKILL.md 文件");
    await expect(file).toBeVisible();
    await expect(region.locator("textarea,input:not([type=file])")).toHaveCount(
      0,
    );
    // The browser reads the pre-existing disk file. No in-memory generated
    // File/Buffer payload or editable metadata supplies the import request.
    await file.setInputFiles(join(directory, "SKILL.md"));
    const confirm = region.getByRole("button", {
      name: "确认导入",
      exact: true,
    });
    await expect(confirm).toBeEnabled();
    const cancel = region.getByRole("button", { name: "取消", exact: true });
    await cancel.focus();
    await page.keyboard.press("Enter");
    await expect(open).toBeFocused();
    expect(seen.posts()).toBe(0);
    await open.click();
    await file.setInputFiles(join(directory, "SKILL.md"));
    await expect(confirm).toBeEnabled();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await packageScreenshot(page, "import-1440-light-file");
    const response = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname ===
          `/api/v1/projects/${data.project_id}/skills`,
    );
    await confirm.focus();
    await page.keyboard.press("Enter");
    expect((await response).status()).toBe(200);
    await expect(
      region.getByRole("link", { name: "查看导入的技能", exact: true }),
    ).toBeVisible();
    await expect(
      region
        .getByRole("list", { name: "技能目录", exact: true })
        .getByRole("listitem"),
    ).toHaveCount(2);
    receipt = await seen.verify();
    expect(receipt).toBeDefined();
    await expect(
      region.getByRole("link", { name: "查看导入的技能", exact: true }),
    ).toHaveAttribute("href", data.route + "/" + receipt!.skill_id);
    await seen.retire();
    seen.complete();
    importCompleted = true;
  } finally {
    try {
      await seen.retire();
    } finally {
      seen.save(importCompleted ? "passed" : "failed");
      await page.close();
    }
  }

  // Fresh-document discovery proves the published catalogue independently of
  // the completed form's in-memory receipt. Reuse the established download
  // observer, including public consumer, original stream and saved Blob bytes.
  const installed: PackageMaterial = JSON.parse(
    readFileSync(join(directory, "skill-package-material.json"), "utf8"),
  );
  expect(installed.skill.id).toBe(receipt!.skill_id);
  expect(installed.project_id).toBe(data.project_id);
  expect(installed.revision).toBe("1");
  expect(installed.skill.protected).toBe(false);
  const downloadPage = await context.newPage();
  const download = await observePackage(downloadPage, installed, true);
  let downloadCompleted = false;
  try {
    await downloadPage.setViewportSize({ width: 1440, height: 960 });
    await downloadPage.emulateMedia({
      colorScheme: "light",
      reducedMotion: "reduce",
    });
    await downloadPage.goto(data.route);
    const region = downloadPage.getByRole("region", {
      name: "项目技能库",
      exact: true,
    });
    const list = region.getByRole("list", { name: "技能目录", exact: true });
    await expect(list.getByRole("listitem")).toHaveCount(2);
    await download.verify();
    const link = list.getByRole("link", {
      name: installed.skill.name,
      exact: true,
    });
    await link.focus();
    await expect(link).toBeFocused();
    await downloadPage.keyboard.press("Enter");
    await expect(
      region.getByRole("heading", { name: "技能详情", level: 1, exact: true }),
    ).toBeFocused();
    await expect(
      region.getByRole("heading", {
        name: installed.skill.name,
        level: 2,
        exact: true,
      }),
    ).toBeVisible();
    await download.verify();
    const save = region.getByRole("button", {
      name: "下载当前修订",
      exact: true,
    });
    await expect(save).toBeEnabled();
    await save.focus();
    await expect(save).toBeFocused();
    const event = downloadPage.waitForEvent("download");
    await downloadPage.keyboard.press("Enter");
    await download.downloaded(await event);
    await expect(region.getByRole("status")).toHaveText(
      "修订 1 已交给浏览器保存。",
    );
    await expect(save).toBeEnabled();
    await download.verify();
    await downloadPage.setViewportSize({ width: 390, height: 844 });
    await downloadPage.emulateMedia({
      colorScheme: "dark",
      reducedMotion: "reduce",
    });
    await expect(downloadPage.locator("html")).toHaveAttribute(
      "data-theme",
      "dark",
    );
    expect(
      await downloadPage.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    ).toBe(true);
    await packageScreenshot(downloadPage, "import-390-dark-downloaded");
    await download.retire();
    download.complete();
    downloadCompleted = true;
  } finally {
    try {
      await download.retire();
    } finally {
      download.save(downloadCompleted ? "passed" : "failed");
      await downloadPage.close();
    }
  }
});
