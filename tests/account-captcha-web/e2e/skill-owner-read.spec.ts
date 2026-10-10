import { test, expect } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  observeSkills,
  skillOriginalCompleted,
} from "./skill-owner-read.native";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_SKILL_OWNER_WEB_EVIDENCE!;
const dist = process.env.AGENTEAM_SKILL_OWNER_WEB_DIST!;
const inputHash = process.env.AGENTEAM_SKILL_OWNER_WEB_INPUT_HASH!;
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
type Metadata = {
  id: string;
  project_id: string;
  name: string;
  normalized_name: string;
  description: string;
  protected: boolean;
  current_revision: string;
  version: string;
};
type Material = {
  email: string;
  password: string;
  user_id: string;
  username: string;
  project_id: string;
  project_name: string;
  skill: Metadata;
  other_email: string;
  other_password: string;
  other_user_id: string;
  input_hash: string;
};
const schemaProgram = String.raw`
import hashlib,json,pathlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 root=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); base=root/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ('common.json','skill-owner.json')}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  meta=json.loads(path.read_bytes()); raw=(evidence/meta['body_file']).read_bytes()
  assert meta['body_file']=='body-'+meta['body_sha256']+'.json' and hashlib.sha256(raw).hexdigest()==meta['body_sha256']
  assert meta['source_run']=='TestSkillOwnerReadWeb' and meta['method']=='GET' and meta['status']==200 and meta['query']==''
  route='/api/v1/projects/{project_id}/skills'+('' if meta['path'].endswith('/skills') else '/{skill_id}')
  ref=docs['skill-owner.json']['paths'][route]['get']['responses']['200']['content']['application/json']['schema']
  schema=dict(docs['skill-owner.json']); schema['$id']=(base/'skill-owner.json').as_uri(); schema.update(ref)
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw)); count+=1
 assert count==4; print(count)
except Exception:
 print('SKILL_BODY_SCHEMA_FAILED',file=sys.stderr); sys.exit(1)
`;

test("[read] Skill Owner existing-skill read", async ({ page }) => {
  const expiresAt = Date.now() + 45_000;
  const data: Material = JSON.parse(
    readFileSync(join(directory, "skill-owner-material.json"), "utf8"),
  );
  expect(data.input_hash).toBe(inputHash);
  let stage = "login",
    observer: Awaited<ReturnType<typeof observeSkills>> | undefined;
  let retired = false,
    firstFailure: unknown = null;
  const save = (name: string, value: unknown) =>
    writeFileSync(join(evidence, name), JSON.stringify(value), {
      mode: 0o600,
      flag: "wx",
    });
  const login = async (email: string, password: string) => {
    await expect(page.locator("#login-email")).toBeVisible();
    try {
      await page.locator("#login-email").fill(email);
      await page.locator("#login-password").fill(password);
    } catch {
      throw Error("PRIVATE_SKILL_LOGIN_INPUT_FAILED");
    }
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "退出登录", exact: true }),
    ).toBeEnabled();
    await expect(
      page.getByRole("list", { name: "项目列表", exact: true }),
    ).toBeVisible();
  };
  try {
    await page.emulateMedia({
      colorScheme: "light",
      reducedMotion: "no-preference",
    });
    await page.goto("/projects");
    await login(data.email, data.password);
    stage = "project-entry";
    await page
      .getByRole("list", { name: "项目列表", exact: true })
      .getByRole("link", { name: data.project_name, exact: true })
      .click();
    const nav = page.getByRole("navigation", { name: "项目导航", exact: true });
    await expect(nav).toBeVisible();
    await nav.getByRole("link", { name: "项目设置", exact: true }).click();
    const settings = page.getByRole("navigation", {
      name: "项目设置",
      exact: true,
    });
    await expect(settings).toBeVisible();
    // No Skill method has started. Install in this existing Session/document;
    // every following Skills navigation stays on the normal SPA router.
    observer = await observeSkills(page, {
      root,
      dist,
      project: data.project_id,
      skill: data.skill.id,
      user: data.user_id,
      expiresAt,
    });
    stage = "directory";
    await settings
      .getByRole("link", { name: "项目技能库", exact: true })
      .click();
    const area = page.getByRole("region", { name: "项目技能库", exact: true });
    const directoryHeading = area.getByRole("heading", {
      name: "项目技能库",
      exact: true,
    });
    const list = area.getByRole("list", { name: "技能目录", exact: true });
    await expect(list.getByRole("listitem")).toHaveCount(1);
    await expect(directoryHeading).toBeFocused();
    await expect(area.locator(".skill-description")).toHaveText(
      data.skill.description,
    );
    await expect(area.getByText("受保护", { exact: true })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBe(true);
    await page.screenshot({ path: join(evidence, "skill-desktop-light.png") });
    stage = "detail-keyboard";
    const skill = list.getByRole("link", {
      name: data.skill.name,
      exact: true,
    });
    await skill.focus();
    await page.keyboard.press("Enter");
    const detailHeading = area.getByRole("heading", {
      name: "技能详情",
      exact: true,
    });
    await expect(detailHeading).toBeFocused();
    await expect(
      area.getByRole("heading", { name: data.skill.name, exact: true }),
    ).toBeVisible();
    await expect(area.locator(".skill-description")).toHaveText(
      data.skill.description,
    );
    const facts = area.locator(".skill-facts");
    await expect(facts.locator("dd")).toHaveText([
      data.skill.current_revision,
      data.skill.version,
      data.skill.normalized_name,
      data.skill.id,
    ]);
    expect(await area.locator("script").count()).toBe(0);
    stage = "reread";
    const reread = area.getByRole("button", { name: "重新读取", exact: true });
    await expect(reread).toBeEnabled();
    await reread.focus();
    await page.keyboard.press("Enter");
    await expect(detailHeading).toBeFocused();
    await expect(reread).toBeEnabled();
    await expect(facts.locator("dd")).toHaveText([
      data.skill.current_revision,
      data.skill.version,
      data.skill.normalized_name,
      data.skill.id,
    ]);
    stage = "narrow-dark";
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(
      await page.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBe(true);
    await page.screenshot({
      path: join(evidence, "skill-narrow-dark-reduced.png"),
    });
    stage = "back-keyboard";
    const back = area.getByRole("link", { name: "返回技能库", exact: true });
    await back.focus();
    await page.keyboard.press("Enter");
    await expect(directoryHeading).toBeFocused();
    await expect(list.getByRole("listitem")).toHaveCount(1);
    stage = "original-completion";
    await expect.poll(() => observer!.idle()).toBe(true);
    const terminal = await observer.finish();
    retired = true;
    expect(terminal.pw_failed).toBe(false);
    expect(terminal.pw_pending).toBe(0);
    const records = readdirSync(evidence)
      .filter((n) => /^response-\d+\.json$/.test(n))
      .sort()
      .map((n) => JSON.parse(readFileSync(join(evidence, n), "utf8")));
    expect(records).toHaveLength(4);
    expect(terminal.requests).toHaveLength(4);
    expect(terminal.native.rows).toHaveLength(4);
    expect(terminal.publication.rows).toHaveLength(4);
    const ids = new Set<string>(),
      methods: string[] = [];
    for (const record of records) {
      expect(record.input_hash).toBe(inputHash);
      expect(record.source_run).toBe("TestSkillOwnerReadWeb");
      expect(ids.has(record.request_id)).toBe(false);
      ids.add(record.request_id);
      const matches = terminal.requests.filter(
        (r: any) => r.request_id === record.request_id,
      );
      expect(matches).toHaveLength(1);
      expect(
        skillOriginalCompleted(
          terminal.native,
          terminal.publication,
          matches[0],
          record,
        ),
      ).toBe(true);
      const raw = readFileSync(join(evidence, record.body_file));
      expect(record.body_file).toBe(`body-${record.body_sha256}.json`);
      expect(createHash("sha256").update(raw).digest("hex")).toBe(
        record.body_sha256,
      );
      const body = JSON.parse(raw.toString("utf8"));
      const n = terminal.native.rows.find(
        (r: any) => r.request_id === record.request_id,
      );
      const published = terminal.publication.rows.find(
        (r: any) => r.id === n.call_id,
      );
      expect(published.typed).toEqual(body);
      expect(body).toEqual(
        published.method === "list" ? { items: [data.skill] } : data.skill,
      );
      methods.push(published.method);
    }
    expect(methods).toEqual(["list", "get", "get", "list"]);
    const remaining = Math.min(5_000, expiresAt - Date.now());
    expect(remaining).toBeGreaterThan(0);
    const schema = spawnSync(
      process.env.AGENTEAM_SKILL_OWNER_WEB_SCHEMA_PYTHON!,
      ["-c", schemaProgram, root, evidence],
      { encoding: "utf8", timeout: remaining, maxBuffer: 16 << 10 },
    );
    expect(schema.status).toBe(0);
    expect(schema.stdout.trim()).toBe("4");
    save("skill-native-consumption.json", {
      input_hash: inputHash,
      ...terminal,
    });
    stage = "other-human";
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await expect(area).toHaveCount(0);
    await login(data.other_email, data.other_password);
    let unauthorizedSkillRequests = 0;
    const seen = (r: import("@playwright/test").Request) => {
      if (
        new URL(r.url()).pathname.startsWith(
          `/api/v1/projects/${data.project_id}/skills`,
        )
      )
        unauthorizedSkillRequests++;
    };
    page.on("request", seen);
    try {
      await page.goto(`/${data.username}/${data.project_name}/settings/skills`);
      await expect(
        page.getByRole("heading", { name: "项目不可用", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("region", { name: "项目技能库", exact: true }),
      ).toHaveCount(0);
      expect(unauthorizedSkillRequests).toBe(0);
      await expect(
        page.getByText(data.skill.description, { exact: true }),
      ).toHaveCount(0);
    } finally {
      page.off("request", seen);
    }
    writeFileSync(
      join(directory, "skill-result.json"),
      JSON.stringify({
        input_hash: inputHash,
        completed: true,
        responses: records.length,
        two_get: true,
        keyboard_focus: true,
        themes_layout: true,
        other_human_hidden: true,
        original_consumed: true,
        typed_published: true,
        observers_joined: true,
      }),
      { mode: 0o600, flag: "wx" },
    );
  } catch (error) {
    // First snapshot is immutable. Cleanup outcomes cannot rewrite the failure.
    let original: unknown = null;
    try {
      original = (await observer?.snapshot()) ?? null;
    } catch {
      /* unavailable */
    }
    firstFailure = { input_hash: inputHash, stage, original };
    save("skill-failure.json", firstFailure);
    throw error;
  } finally {
    if (observer && !retired) {
      const tail = await observer.abort();
      if (firstFailure)
        save("skill-failure-tail.json", { input_hash: inputHash, ...tail });
    }
  }
});
