import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type SkillMaterial = {
  mode: "read";
  cookie: string;
  project_id: string;
  route: string;
  input_hash: string;
  skill: {
    id: string;
    project_id: string;
    name: string;
    normalized_name: string;
    description: string;
    protected: boolean;
    current_revision: string;
    version: string;
  };
};
export function skillMaterial(): SkillMaterial {
  const data = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "skill-owner-material.json"),
      "utf8",
    ),
  );
  expect(data.mode).toBe("read");
  expect(data.input_hash).toBe(process.env.AGENTEAM_SKILL_OWNER_WEB_INPUT_HASH);
  expect(Object.keys(data.skill).sort()).toEqual([
    "current_revision",
    "description",
    "id",
    "name",
    "normalized_name",
    "project_id",
    "protected",
    "version",
  ]);
  expect(data.skill).toMatchObject({
    project_id: data.project_id,
    name: "Add Skills",
    protected: true,
    current_revision: "1",
    version: "1",
  });
  expect(data.skill.id).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  expect(data.route).toMatch(/^\/[a-z0-9-]+\/skill-read\/settings\/skills$/);
  expect(typeof data.cookie).toBe("string");
  expect(data.cookie.length).toBeGreaterThan(0);
  return data;
}
export async function observeSkills(page: Page, data: SkillMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const names = [
    "session",
    "profile",
    "preferences",
    "bootstrap",
    "resolve",
    "projects",
    "project",
    "skills-catalog",
    "skill",
  ];
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    if (request.method() !== "GET") throw Error("SKILLS_READ_ONLY");
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (globals[path]) return globals[path];
    if (path === prefix && !url.search) return "project";
    if (path === prefix + "/skills/catalog") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      return "skills-catalog";
    }
    if (path === prefix + "/skills/" + data.skill.id && !url.search)
      return "skill";
    throw Error("UNOBSERVED_SKILLS_ENDPOINT");
  };
  const seen = await observeTaskRequests(
    page,
    { ...data, skill_id: data.skill.id },
    {
      dist: process.env.AGENTEAM_SKILL_OWNER_WEB_DIST!,
      evidence: process.env.AGENTEAM_SKILL_OWNER_WEB_EVIDENCE!,
      inputHash: data.input_hash,
      prefix: "skill-owner-read",
      endpoint: classify,
      lookupCommand: null,
      skillsReads: true,
    },
  );
  return {
    ...seen,
    requireCatalog(index: number) {
      expect(seen.skillsRead("skills-catalog", index)).toEqual({
        items: [data.skill],
      });
    },
    requireDetail(index: number) {
      expect(seen.skillsRead("skill", index)).toEqual(data.skill);
    },
    complete() {
      expect(seen.requests("skills-catalog")).toHaveLength(2);
      expect(seen.requests("skill")).toHaveLength(2);
      writeFileSync(
        join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "skill-result.json"),
        JSON.stringify({
          input_hash: data.input_hash,
          completed: true,
          two_catalog: true,
          two_get: true,
          keyboard_focus: true,
          themes_layout: true,
          original_consumed: true,
          typed_published: true,
          observers_joined: true,
          responses: names.reduce(
            (n, name) => n + seen.requests(name).length,
            0,
          ),
        }),
        { mode: 0o600 },
      );
    },
  };
}
export async function skillScreenshot(page: Page, name: string) {
  const evidence = process.env.AGENTEAM_SKILL_OWNER_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect
    .poll(() =>
      brand.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: join(evidence, `skill-owner-read-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
