import { test, expect } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  observeSecretOwner,
  secretOriginalCompleted,
} from "./project-secret-owner.native";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
  evidence = process.env.AGENTEAM_SECRET_OWNER_WEB_EVIDENCE!,
  dist = process.env.AGENTEAM_SECRET_OWNER_WEB_DIST!,
  inputHash = process.env.AGENTEAM_SECRET_OWNER_WEB_INPUT_HASH!;
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
type Material = {
  email: string;
  password: string;
  user_id: string;
  username: string;
  project_id: string;
  project_name: string;
  values: string[];
  input_hash: string;
};
const forms = (value: string) => [
  value,
  JSON.stringify(value).slice(1, -1),
  Buffer.from(value).toString("base64"),
  Buffer.from(value).toString("base64").replace(/=+$/, ""),
  createHash("sha256").update(value).digest("hex"),
];
const schemaProgram = String.raw`
import hashlib,json,pathlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 root=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); base=root/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ('common.json','secret-variables.json')}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  meta=json.loads(path.read_bytes()); raw=(evidence/meta['body_file']).read_bytes()
  assert meta['body_file']=='body-'+meta['body_sha256']+'.json' and hashlib.sha256(raw).hexdigest()==meta['body_sha256']
  assert meta['source_run']=='TestProjectSecretOwnerWeb' and meta['status']==200 and meta['original_read_eof'] and meta['original_close']
  assert meta['input_hash']==sys.argv[3] and meta['content_length']==len(raw)
  stem='/api/v1/projects/{project_id}/secret-variables'; operation=meta['operation']
  route=stem if operation in ('list','create') else stem+'/commands/lookup' if operation=='lookup' else stem+'/{variable_id}'
  ref=docs['secret-variables.json']['paths'][route][meta['method'].lower()]['responses']['200']['content']['application/json']['schema']
  schema=dict(docs['secret-variables.json']); schema['$id']=(base/'secret-variables.json').as_uri(); schema.update(ref)
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw));count+=1
 assert count>=8;print(count)
except Exception:
 print('SECRET_SAFE_BODY_SCHEMA_FAILED',file=sys.stderr);sys.exit(1)
`;

test("[owner] Project Secret lifecycle", async ({ page }) => {
  const data: Material = JSON.parse(
    readFileSync(join(directory, "secret-owner-material.json"), "utf8"),
  );
  expect(data.input_hash).toBe(inputHash);
  expect(data.values.length).toBe(2);
  const forbidden = data.values.flatMap(forms),
    privateForms = [...forbidden, ...forms(data.password)];
  let stage = "login",
    observer: Awaited<ReturnType<typeof observeSecretOwner>> | undefined,
    terminal: any,
    logSafe = true;
  const consoleEvent = (message: any) => {
    const text = message.text();
    if (privateForms.some((v) => text.includes(v))) logSafe = false;
  };
  const pageError = () => {
    logSafe = false;
  };
  page.on("console", consoleEvent);
  page.on("pageerror", pageError);
  const noMaterial = async () => {
    const safe = await page.evaluate((values) => {
      const fields = Array.from(
        document.querySelectorAll("input,textarea"),
      ).map((n) => (n as HTMLInputElement).value);
      return !values.some(
        (v) =>
          document.documentElement.outerHTML.includes(v) ||
          fields.some((text) => text.includes(v)),
      );
    }, privateForms);
    expect(safe).toBe(true);
    expect(logSafe).toBe(true);
  };
  try {
    await page.goto("/projects");
    await expect(page.locator("#login-email")).toBeVisible();
    try {
      await page.locator("#login-email").fill(data.email);
      await page.locator("#login-password").fill(data.password);
    } catch {
      throw Error("PRIVATE_SECRET_LOGIN_INPUT");
    }
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "退出登录", exact: true }),
    ).toBeEnabled();
    await page
      .getByRole("list", { name: "项目列表", exact: true })
      .getByRole("link", { name: data.project_name, exact: true })
      .click();
    await page
      .getByRole("navigation", { name: "项目导航", exact: true })
      .getByRole("link", { name: "项目设置", exact: true })
      .click();
    await expect(
      page.getByRole("navigation", { name: "项目设置", exact: true }),
    ).toBeVisible();
    observer = await observeSecretOwner(page, {
      root,
      dist,
      project: data.project_id,
      user: data.user_id,
      forbidden: privateForms,
      expiresAt: Date.now() + 40_000,
    });
    stage = "initial-list";
    await page
      .getByRole("navigation", { name: "项目设置", exact: true })
      .getByRole("link", { name: "Secrets", exact: true })
      .click();
    const area = page.getByRole("region", { name: "Secrets", exact: true }),
      list = page.getByRole("region", { name: "Secret 列表", exact: true }),
      detail = page.getByRole("region", { name: "Secret 详情", exact: true });
    await expect(area).toBeVisible();
    await expect(list).toContainText("此项目尚无 Secret");
    stage = "create";
    await area
      .getByRole("button", { name: "创建 Secret", exact: true })
      .click();
    let dialog = page.getByRole("dialog", { name: "创建 Secret", exact: true });
    await dialog.getByLabel("名称", { exact: true }).fill("UI_SECRET");
    await dialog
      .getByLabel("描述", { exact: true })
      .fill("Initial safe metadata");
    try {
      await dialog
        .getByLabel("新的 Secret 值", { exact: true })
        .fill(data.values[0]!);
    } catch {
      throw Error("PRIVATE_SECRET_CREATE_INPUT");
    }
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(
      detail.getByRole("heading", { name: "UI_SECRET", exact: true }),
    ).toBeVisible();
    await expect(list).toContainText("UI_SECRET");
    await expect(
      area.getByRole("button", { name: "编辑 Secret", exact: true }),
    ).toBeEnabled();
    await noMaterial();
    let snap = await observer.snapshot();
    const create = snap.rows.find((r: any) => r.operation === "create");
    expect(create?.typed?.variable?.version).toBe("1");
    const target = create.typed.variable.id;
    expect(create.typed.variable.project_id).toBe(data.project_id);
    await expect(detail).toContainText(target);
    stage = "committed-response-loss";
    await area
      .getByRole("button", { name: "编辑 Secret", exact: true })
      .click();
    dialog = page.getByRole("dialog", { name: "编辑 Secret", exact: true });
    await dialog.getByLabel("名称", { exact: true }).fill("UI_SECRET_UPDATED");
    await dialog
      .getByLabel("描述", { exact: true })
      .fill("Updated safe metadata");
    await dialog.getByLabel("替换 Secret 值", { exact: true }).check();
    try {
      await dialog
        .getByLabel("新的 Secret 值", { exact: true })
        .fill(data.values[1]!);
    } catch {
      throw Error("PRIVATE_SECRET_UPDATE_INPUT");
    }
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(
      dialog.getByLabel("新的 Secret 值", { exact: true }),
    ).toHaveValue("");
    await expect(dialog).toContainText("结果尚未确认");
    await expect(
      dialog.getByRole("button", { name: "保存", exact: true }),
    ).toBeDisabled();
    await expect(
      dialog.getByRole("button", { name: "查证原命令", exact: true }),
    ).toBeEnabled();
    await noMaterial();
    snap = await observer.snapshot();
    const update = snap.rows.filter((r: any) => r.operation === "update");
    expect(update).toHaveLength(1);
    expect(update[0].rejected).toBe(true);
    expect(update[0].progress.phase).toBe("uncertain");
    stage = "identity-only-lookup";
    await dialog
      .getByRole("button", { name: "查证原命令", exact: true })
      .click();
    await expect(dialog).toHaveCount(0);
    await expect(
      detail.getByRole("heading", { name: "UI_SECRET_UPDATED", exact: true }),
    ).toBeVisible();
    await expect(detail).toContainText("Updated safe metadata");
    await expect(list).toContainText("UI_SECRET_UPDATED");
    await expect(
      area.getByRole("button", { name: "删除 Secret", exact: true }),
    ).toBeEnabled();
    snap = await observer.snapshot();
    const lookup = snap.rows.find((r: any) => r.operation === "lookup"),
      latest = snap.rows.filter((r: any) => r.operation === "get").at(-1);
    expect(lookup?.typed?.status).toBe("committed");
    expect(lookup?.typed?.receipt?.variable?.id).toBe(target);
    expect(lookup?.typed?.receipt?.variable?.version).toBe("2");
    expect(latest?.typed?.version).toBe("2");
    expect(latest?.typed?.name).toBe("UI_SECRET_UPDATED");
    await expect(
      detail.locator("dt", { hasText: /^版本$/ }).locator("+ dd"),
    ).toHaveText("2");
    await noMaterial();
    await page.screenshot({
      path: join(evidence, "secret-safe-metadata.png"),
      fullPage: true,
    });
    stage = "delete";
    await area
      .getByRole("button", { name: "删除 Secret", exact: true })
      .click();
    dialog = page.getByRole("dialog", { name: "删除 Secret", exact: true });
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(list).toContainText("此项目尚无 Secret");
    await expect(detail).toContainText("选择一项查看安全元数据");
    await noMaterial();
    stage = "original-tails";
    await expect.poll(() => observer!.ready()).toBe(true);
    terminal = await observer.finish();
    expect(secretOriginalCompleted(terminal)).toBe(true);
    const deleted = terminal.browser.rows.find(
      (r: any) => r.operation === "delete",
    );
    expect(deleted.typed.deleted.id).toBe(target);
    expect(deleted.typed.deleted.version).toBe("3");
    expect(
      terminal.browser.rows.filter((r: any) => r.operation === "list").at(-1)
        .typed.items,
    ).toEqual([]);
    stage = "same-original-evidence";
    const records = readdirSync(evidence)
      .filter((p) => /^response-\d+\.json$/.test(p))
      .map((p) => JSON.parse(readFileSync(join(evidence, p), "utf8")));
    expect(records).toHaveLength(terminal.browser.rows.length);
    for (const row of terminal.browser.rows) {
      const matched = records.filter((r: any) => r.request_id === row.xid);
      expect(matched).toHaveLength(1);
      const record = matched[0];
      expect(record).toMatchObject({
        input_hash: inputHash,
        source_run: "TestProjectSecretOwnerWeb",
        operation: row.operation,
        method: row.method,
        wire_status: row.status,
        original_read_eof: true,
        original_close: true,
      });
      const raw = readFileSync(join(evidence, record.body_file));
      expect(createHash("sha256").update(raw).digest("hex")).toBe(
        record.body_sha256,
      );
      expect(raw.byteLength).toBe(record.content_length);
      expect(forbidden.some((v) => raw.includes(Buffer.from(v)))).toBe(false);
      if (row.operation !== "update") {
        expect(record.body_sha256).toBe(row.digest);
        expect(JSON.parse(raw.toString("utf8"))).toEqual(row.typed);
      } else {
        const actual = JSON.parse(raw.toString("utf8"));
        expect(actual.command).toBe("project.secret_variable.update");
        expect(actual.variable.version).toBe("2");
        expect(actual.variable.id).toBe(target);
      }
    }
    stage = "standard-schema";
    const schema = spawnSync(
      process.env.AGENTEAM_SECRET_OWNER_WEB_SCHEMA_PYTHON!,
      ["-c", schemaProgram, root, evidence, inputHash],
      { encoding: "utf8", timeout: 10_000, maxBuffer: 16 << 10 },
    );
    expect(schema.status).toBe(0);
    expect(schema.signal).toBeNull();
    expect(Number(schema.stdout.trim())).toBe(records.length);
    const result = {
      input_hash: inputHash,
      completed: true,
      created: true,
      unknown: true,
      lookup_confirmed: true,
      current_version: true,
      deleted: true,
      input_cleared: true,
      no_material: logSafe,
      original_consumed: true,
      typed_published: true,
      observers_joined: true,
      responses: records.length,
    };
    writeFileSync(
      join(evidence, "secret-observer.json"),
      JSON.stringify({ input_hash: inputHash, ...terminal }),
      { mode: 0o600 },
    );
    writeFileSync(
      join(directory, "secret-owner-result.json"),
      JSON.stringify(result),
      { mode: 0o600 },
    );
  } catch {
    // The first safe failure stage is durable before any potentially held tail.
    writeFileSync(
      join(evidence, "secret-first-failure.json"),
      JSON.stringify({
        input_hash: inputHash,
        stage,
        page_closed: page.isClosed(),
        observer_present: !!observer,
      }),
      { mode: 0o600 },
    );
    try {
      terminal ??= await observer?.finish();
    } catch {
      /* Original failure remains. */
    }
    // Partial response data could itself be the rejected leak. Persist counts only.
    writeFileSync(
      join(evidence, "secret-failure-tail.json"),
      JSON.stringify({
        input_hash: inputHash,
        stage,
        rows: terminal?.rows?.length ?? null,
        pw_pending: terminal?.pw_pending ?? null,
        browser_pending: terminal?.browser?.pending ?? null,
        failed: true,
      }),
      { mode: 0o600 },
    );
    throw Error("SECRET_OWNER_REAL_CHAIN_FAILED_" + stage);
  } finally {
    await observer?.abort();
    page.off("console", consoleEvent);
    page.off("pageerror", pageError);
    data.values.fill("");
    data.password = "";
    await page.close();
    await page.context().close();
  }
});
