import { test, expect, type Request, type Response } from "@playwright/test";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const uuid7 =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
type Material = {
  mode: "create-and-enter";
  cookie: string;
  owner_id: string;
  username: string;
  draft: { name: string; description: string };
};

// The original verified-TLS bridge writes this proof after reading and closing
// the response and returning from its handler. No route or response is mocked.
async function originalCreateJSON(response: Response) {
  const status = 201;
  expect(response.request().method()).toBe("POST");
  expect(response.status()).toBe(status);
  expect(await response.finished()).toBeNull();
  const bytes = await response.body();
  const requestID = response.headers()["x-request-id"];
  expect(requestID).toMatch(uuid7);
  const path = join(
    process.env.AGENTEAM_PROJECT_CREATION_WEB_EVIDENCE!,
    `project-creation-create-and-enter-response-${requestID}.json`,
  );
  await expect.poll(() => existsSync(path)).toBe(true);
  const url = new URL(response.url());
  expect(JSON.parse(readFileSync(path, "utf8"))).toEqual({
    input_hash: process.env.AGENTEAM_PROJECT_CREATION_WEB_INPUT_HASH,
    request_id: requestID,
    method: response.request().method(),
    url_sha256: createHash("sha256")
      .update(url.pathname + url.search)
      .digest("hex"),
    status,
    content_length: bytes.length,
    body_sha256: createHash("sha256").update(bytes).digest("hex"),
    original_eof: true,
    original_closed: true,
    handler_returned: true,
  });
  return JSON.parse(bytes.toString("utf8"));
}

test("creates and enters a project through the default App [create-and-enter]", async ({
  page,
}, testInfo) => {
  const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
  const evidence = process.env.AGENTEAM_PROJECT_CREATION_WEB_EVIDENCE!;
  const data: Material = JSON.parse(
    readFileSync(join(directory, "project-creation-material.json"), "utf8"),
  );
  expect(data.mode).toBe("create-and-enter");
  expect(data.owner_id).toMatch(uuid7);
  expect(data.username).toMatch(/^[a-z0-9][a-z0-9_.-]*$/);
  expect(data.draft).toEqual({
    name: "Browser_Project",
    description: "Created through the default App.",
  });
  const writes: Request[] = [];
  const faults: string[] = [];
  const observeRequest = (request: Request) => {
    const path = new URL(request.url()).pathname;
    if (
      /^\/api\/v1\/project(?:s|creations)(?:\/|$)/.test(path) &&
      !["GET", "HEAD"].includes(request.method())
    )
      writes.push(request);
  };
  const observeError = () => faults.push("page-error");
  page.on("request", observeRequest);
  page.on("pageerror", observeError);
  const observation = { mode: data.mode, cookie: data.cookie, project_id: "" };
  const seen = await observeTaskRequests(page, observation, {
    dist: process.env.AGENTEAM_PROJECT_CREATION_WEB_DIST!,
    evidence,
    inputHash: process.env.AGENTEAM_PROJECT_CREATION_WEB_INPUT_HASH!,
    prefix: "project-creation",
    lookupCommand: null,
    endpoint(request) {
      const path = new URL(request.url()).pathname;
      if (request.method() !== "GET") return undefined;
      if (path === "/api/v1/projects") return "projects";
      if (path === "/api/v1/projects/resolve") return "resolve";
      if (
        observation.project_id &&
        path === `/api/v1/projects/${observation.project_id}`
      )
        return "project";
      if (path.startsWith("/api/v1/projects/"))
        throw Error("PROJECT_CREATION_READ_SCOPE");
      return undefined;
    },
  });
  async function originalRead(response: Response) {
    expect(response.status()).toBe(200);
    await seen.verify();
    return seen.projectCreationRead(response.request());
  }
  const isList = (response: Response) =>
    response.request().method() === "GET" &&
    new URL(response.url()).pathname === "/api/v1/projects";
  const press = async (name: string) => {
    const button = page.getByRole("button", { name, exact: true });
    await expect(button).toBeEnabled();
    await button.focus();
    await expect(button).toBeFocused();
    await page.keyboard.press("Enter");
  };
  const screenshot = async (name: string) => {
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      )
      .toBe(true);
    mkdirSync(evidence, { recursive: true, mode: 0o700 });
    await page.screenshot({
      path: join(evidence, `project-creation-${name}.png`),
      fullPage: true,
      animations: "disabled",
    });
  };
  try {
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
    const firstRead = page.waitForResponse(isList);
    await page.goto("/projects");
    const before = await originalRead(await firstRead);
    expect(
      before.items.some(
        (item: { name: string }) => item.name === data.draft.name,
      ),
    ).toBe(false);
    await expect(
      page.getByRole("heading", { name: "项目", exact: true, level: 1 }),
    ).toBeVisible();

    await press("新建项目");
    const form = page.getByRole("region", { name: "新建项目", exact: true });
    await expect(form).toBeVisible();
    await expect(
      form.getByRole("heading", { name: "新建项目", exact: true }),
    ).toBeFocused();
    await form
      .getByRole("textbox", { name: "项目名称", exact: true })
      .fill(data.draft.name);
    await press("关闭创建面板");
    await expect(form).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "新建项目", exact: true }),
    ).toBeFocused();
    expect(writes).toHaveLength(0);
    await press("新建项目");
    await expect(
      form.getByRole("textbox", { name: "项目名称", exact: true }),
    ).toHaveValue("");
    await form
      .getByRole("textbox", { name: "项目名称", exact: true })
      .fill(data.draft.name);
    await form
      .getByRole("textbox", { name: "项目描述", exact: true })
      .fill(data.draft.description);
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await screenshot("1440-light-form");

    const createdResponse = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/api/v1/projects",
    );
    const refreshedRead = page.waitForResponse(isList);
    await press("确认创建项目");
    const response = await createdResponse;
    // This normal integration path requires real initialization to complete.
    // Accepted/failed/Unknown recovery is not manufactured by this browser case.
    const result = await originalCreateJSON(response);
    expect(Object.keys(result).sort()).toEqual(["project", "state"]);
    expect(result.state).toBe("ready");
    const project = result.project;
    expect(project.id).toMatch(uuid7);
    observation.project_id = project.id;
    expect(project).toMatchObject({
      owner_user_id: data.owner_id,
      name: data.draft.name,
      normalized_name: "browser_project",
      description: data.draft.description,
      lifecycle: "active",
      current_sprint_id: null,
    });
    expect(writes).toHaveLength(1);
    expect(writes[0]!.method()).toBe("POST");
    expect(writes[0]!.postDataJSON()).toEqual({
      project_id: project.id,
      ...data.draft,
    });
    expect(writes[0]!.headers()["idempotency-key"]).toMatch(
      /^[A-Za-z0-9._:/-]{1,128}$/,
    );
    expect(writes[0]!.headers()["x-csrf-token"]).toMatch(/^[A-Za-z0-9_-]{43}$/);
    const listed = await originalRead(await refreshedRead);
    expect(listed.items).toContainEqual({
      id: project.id,
      name: project.name,
      description: project.description,
      lifecycle: project.lifecycle,
      version: project.version,
    });
    await expect(form.getByRole("status")).toHaveText(
      "项目已完成初始化，可以进入使用。",
    );

    const address = `/${data.username}/${project.normalized_name}`;
    const resolvedResponse = page.waitForResponse(
      (r) =>
        r.request().method() === "GET" &&
        new URL(r.url()).pathname === "/api/v1/projects/resolve",
    );
    const detailResponse = page.waitForResponse(
      (r) =>
        r.request().method() === "GET" &&
        new URL(r.url()).pathname === `/api/v1/projects/${project.id}`,
    );
    await form.getByRole("link", { name: "进入项目", exact: true }).click();
    expect(await originalRead(await resolvedResponse)).toEqual(project);
    expect(await originalRead(await detailResponse)).toEqual(project);
    await expect(page).toHaveURL(
      new URL(address, process.env.AGENTEAM_AUTH_WEB_ORIGIN!).href,
    );
    const home = page.getByRole("region", { name: "项目首页", exact: true });
    await expect(
      home.getByRole("heading", { name: data.draft.name, exact: true }),
    ).toBeVisible();
    await expect(
      home.getByText(data.draft.description, { exact: true }),
    ).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(
      await page.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    ).toBe(true);
    await screenshot("390-dark-project");

    // A new document reads the durable listing rather than reusing creation state.
    await seen.endDocument();
    const reloadedRead = page.waitForResponse(isList);
    await page.goto("/projects");
    const reloaded = await originalRead(await reloadedRead);
    expect(reloaded.items).toContainEqual({
      id: project.id,
      name: project.name,
      description: project.description,
      lifecycle: project.lifecycle,
      version: project.version,
    });
    const row = page
      .getByRole("list", { name: "项目列表", exact: true })
      .getByRole("link", { name: data.draft.name, exact: true });
    await expect(row).toHaveAttribute("href", address);
    await expect(row).toBeVisible();
    await screenshot("390-dark-list");
    expect(writes).toHaveLength(1);
    expect(faults).toEqual([]);
    writeFileSync(
      join(directory, "created.json"),
      JSON.stringify({ status: 201, result }) + "\n",
      { flag: "wx", mode: 0o600 },
    );
  } finally {
    page.off("request", observeRequest);
    page.off("pageerror", observeError);
    try {
      await seen.retire();
    } finally {
      seen.save(testInfo.status);
    }
  }
});
