import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
type Credential = { email: string; password: string; user_id: string };
type User = {
  id: string;
  email: string;
  username: string;
  display_name: string;
  role: string;
  theme: string;
  version: string;
  initial_password_suggestion: boolean;
  created_at: string;
};
type DirectoryPage = { items: User[]; next_cursor?: string };
type Observation = {
  sequence: number;
  status: number;
  limit: string | null;
  cursor: string | null;
  queryKeys: string[];
  value?: DirectoryPage;
  code?: string;
  codePresent?: boolean;
  stage: "fetch" | "body" | "parsed";
  parseFailure?: "abort" | "syntax" | "type" | "limit" | "other";
  requestAborted: boolean;
  problemSource?: "playwright-response" | "native-reader";
  responseBodyUnavailable?: boolean;
  settled: boolean;
};
type ObservedWindow = Window & {
  __directoryObservation?: { sequence: number; last?: Observation };
};
type Material = { admin: Credential; member: Credential; expected: User[] };
const material = (): Material =>
  JSON.parse(readFileSync(join(directory, "directory-material.json"), "utf8"));
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "directory-result.json"),
    JSON.stringify({ ...value, completed: true }),
    { mode: 0o600 },
  );
}
async function privateFill(input: Locator, value: string) {
  try {
    await input.fill(value);
  } catch {
    throw new Error("private directory credential entry failed");
  }
}
function installDirectoryObservation() {
  const scope = window as ObservedWindow;
  const observation = {
    sequence: 0,
    last: undefined as Observation | undefined,
  };
  scope.__directoryObservation = observation;
  const original = window.fetch;
  window.fetch = function (this: unknown, ...args) {
    const pending = Reflect.apply(original, this, args) as Promise<Response>;
    const [input, init] = args;
    const url = new URL(
      input instanceof Request ? input.url : String(input),
      location.href,
    );
    const method =
      init?.method ?? (input instanceof Request ? input.method : "GET");
    if (
      url.origin === location.origin &&
      url.pathname === "/api/v1/system/users" &&
      method === "GET"
    ) {
      const value: Observation = {
        sequence: ++observation.sequence,
        status: 0,
        limit: url.searchParams.get("limit"),
        cursor: url.searchParams.get("cursor"),
        queryKeys: [...url.searchParams.keys()],
        stage: "fetch",
        requestAborted: false,
        settled: false,
      };
      observation.last = value;
      let captured: Uint8Array | undefined;
      let bytes = 0;
      const finish = () => {
        captured?.fill(0);
        captured = undefined;
        value.requestAborted =
          (init?.signal ?? (input instanceof Request ? input.signal : null))
            ?.aborted === true;
        value.settled = true;
      };
      const fail = (error: unknown) => {
        if (value.settled) return;
        value.parseFailure =
          error instanceof DOMException && error.name === "AbortError"
            ? "abort"
            : error instanceof SyntaxError
              ? "syntax"
              : error instanceof TypeError
                ? "type"
                : "other";
        finish();
      };
      // Observe only the production reader's actual chunks/done. Fetch,
      // Response, reader and each native read Promise are returned unchanged;
      // there is no clone/tee, extra read, delayed abort or awaited observer.
      // Done here is NOT evidence that the owner's body/cancel tail completed.
      void pending
        .then((response) => {
          value.status = response.status;
          value.stage = "body";
          const stream = response.body;
          if (!stream) throw new TypeError();
          const getReader = stream.getReader;
          stream.getReader = function (
            this: ReadableStream<Uint8Array>,
            ...readerArgs: unknown[]
          ) {
            const reader = Reflect.apply(
              getReader,
              this,
              readerArgs,
            ) as ReadableStreamDefaultReader<Uint8Array>;
            stream.getReader = getReader;
            const read = reader.read;
            reader.read = function () {
              const reading = Reflect.apply(read, this, []) as ReturnType<
                typeof read
              >;
              void reading.then((chunk) => {
                if (value.settled) return;
                try {
                  if (!chunk.done) {
                    if (bytes + chunk.value.byteLength > 600_000) {
                      value.parseFailure = "limit";
                      finish();
                      return;
                    }
                    captured ??= new Uint8Array(600_000);
                    captured.set(chunk.value, bytes);
                    bytes += chunk.value.byteLength;
                    return;
                  }
                  const body = JSON.parse(
                    new TextDecoder("utf-8", { fatal: true }).decode(
                      captured?.subarray(0, bytes),
                    ),
                  );
                  value.stage = "parsed";
                  if (response.status === 200) value.value = body;
                  else {
                    value.codePresent = Object.hasOwn(body, "code");
                    if (
                      typeof body.code === "string" &&
                      /^[A-Z][A-Z0-9_]{0,127}$/.test(body.code)
                    )
                      value.code = body.code;
                  }
                  finish();
                } catch (error: unknown) {
                  fail(error);
                }
              }, fail);
              return reading;
            };
            return reader;
          } as typeof stream.getReader;
        })
        .catch(fail);
    }
    return pending;
  };
}
async function takeRead(page: Page, previous: number) {
  await expect
    .poll(() =>
      page.evaluate((before) => {
        const last = (window as ObservedWindow).__directoryObservation?.last;
        return !!last && last.sequence > before && last.settled;
      }, previous),
    )
    .toBe(true);
  return page.evaluate(() => {
    const observation = (window as ObservedWindow).__directoryObservation!;
    const value = observation.last!;
    delete observation.last;
    return value;
  });
}
async function readAfter(page: Page, action: () => Promise<unknown>) {
  const before = await page.evaluate(
    () => (window as ObservedWindow).__directoryObservation!.sequence,
  );
  await action();
  return takeRead(page, before);
}
async function problemAfter(page: Page, action: () => Promise<unknown>) {
  const network = page
    .waitForResponse(
      (response) =>
        new URL(response.url()).origin === new URL(page.url()).origin &&
        new URL(response.url()).pathname === "/api/v1/system/users" &&
        response.request().method() === "GET",
    )
    .then(async (response) => {
      try {
        const body = await response.json();
        const code =
          typeof body?.code === "string" &&
          /^[A-Z][A-Z0-9_]{0,127}$/.test(body.code)
            ? body.code
            : undefined;
        return { status: response.status(), parsed: true, code };
      } catch {
        // Chromium may discard the network body after the product has already
        // read a 401/403 and aborted its finished request while retiring a page.
        return { status: response.status(), parsed: false, code: undefined };
      }
    });
  const [observed, actual] = await Promise.all([
    readAfter(page, action),
    network,
  ]);
  expect(actual.status).toBe(observed.status);
  if (actual.parsed) {
    expect(actual.code).toBe(observed.code);
    observed.code = actual.code;
  }
  observed.problemSource = actual.parsed
    ? "playwright-response"
    : "native-reader";
  observed.responseBodyUnavailable = !actual.parsed;
  return observed;
}
async function login(
  page: Page,
  credential: Credential,
  path = "/login",
  heading = "首页",
) {
  await page.goto(path);
  await expect(page.locator("#login-email")).toBeEditable();
  await page.locator("#login-email").fill(credential.email);
  await privateFill(page.locator("#login-password"), credential.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: heading, exact: true }),
  ).toBeVisible();
}
async function identity(page: Page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (response.status !== 200)
      return { status: response.status, user_id: "", session_id: "" };
    const value = await response.json();
    return {
      status: response.status,
      user_id: value.user.id as string,
      session_id: value.session.id as string,
    };
  });
}
const keys = [
  "id",
  "email",
  "username",
  "display_name",
  "role",
  "theme",
  "version",
  "initial_password_suggestion",
  "created_at",
];
async function matchesDatabase(
  page: Page,
  observed: Observation,
  expected: User[],
) {
  expect(observed.status).toBe(200);
  expect(observed.limit).toBe("25");
  expect(
    observed.queryKeys.every((key) => key === "limit" || key === "cursor"),
  ).toBe(true);
  const value = observed.value!;
  expect(
    Array.isArray(value.items) && value.items.length === expected.length,
  ).toBe(true);
  expect(
    value.items.every(
      (row, index) =>
        Object.keys(row).length === 9 &&
        keys.every(
          (key) =>
            row[key as keyof User] === expected[index]![key as keyof User],
        ),
    ),
  ).toBe(true);
  await expect(page.locator("tbody tr")).toHaveCount(expected.length);
  const visible = await page.locator("tbody tr").evaluateAll((rows) =>
    rows.map((row) => ({
      cells: [...row.querySelectorAll("td")].map((cell) => cell.textContent),
      at: row.querySelector("time")?.getAttribute("datetime"),
      accessible: row.querySelector("time")?.getAttribute("aria-label"),
      links: row.querySelectorAll("a").length,
    })),
  );
  const differences: object[] = [];
  for (const [index, row] of visible.entries()) {
    const wanted = expected[index]!;
    const fields = {
      email: [row.cells[0], wanted.email],
      username: [row.cells[1], wanted.username || "—"],
      display_name: [row.cells[2], wanted.display_name || wanted.email],
      role: [row.cells[3], wanted.role === "admin" ? "管理员" : "普通用户"],
      visible_time: [
        row.cells[4],
        wanted.created_at.slice(0, 10) + " " + wanted.created_at.slice(11, 19),
      ],
      datetime: [row.at, wanted.created_at],
      accessible_time: [row.accessible, wanted.created_at],
    };
    if (row.cells.length !== 5 || row.links !== 0)
      differences.push({ index, cells: row.cells.length, links: row.links });
    for (const [field, [actual, expected]] of Object.entries(fields)) {
      if (actual === expected) continue;
      let firstDifference = 0;
      while (
        firstDifference <
          Math.min(actual?.length ?? 0, expected?.length ?? 0) &&
        actual![firstDifference] === expected![firstDifference]
      )
        ++firstDifference;
      differences.push({
        index,
        field,
        actualLength: actual?.length ?? null,
        expectedLength: expected?.length ?? null,
        firstDifference,
      });
    }
  }
  // Diagnostics contain only field names, lengths and indices, never directory
  // text, a cursor, credentials or a raw response body.
  expect(differences).toEqual([]);
  expect(
    new URL(page.url()).pathname === "/system/users" &&
      new URL(page.url()).search === "",
  ).toBe(true);
}
let sequence = 0;
async function changeAuthority(
  action: "revoke-session" | "demote",
  current: { user_id: string; session_id: string },
) {
  const next = ++sequence;
  const request = join(directory, "directory-ipc.json");
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence: next, action, ...current }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  const ack = join(directory, `directory-ack-${next}.json`);
  await expect.poll(() => existsSync(ack)).toBe(true);
  const value = JSON.parse(readFileSync(ack, "utf8"));
  expect(value.ok === true && value.sequence === next).toBe(true);
}
async function noOverflow(page: Page) {
  expect(
    await page.evaluate(() => {
      const content = document.querySelector(".app-content")!;
      return (
        document.documentElement.scrollWidth <= innerWidth + 1 &&
        content.scrollWidth <= content.clientWidth + 1
      );
    }),
  ).toBe(true);
}
async function directoryFieldsFit(page: Page, narrow: boolean) {
  const layout = await page
    .locator(".directory-scroll")
    .evaluate((region, stacked) => {
      const bounds = region.getBoundingClientRect();
      const rows = [...region.querySelectorAll("tbody tr")];
      const labels = ["邮箱", "用户名", "显示名", "角色", "注册时间（UTC）"];
      const failures: object[] = [];
      if (region.scrollWidth > region.clientWidth + 1)
        failures.push({
          field: "region",
          excess: region.scrollWidth - region.clientWidth,
        });
      for (const [index, row] of rows.entries()) {
        const cells = [...row.querySelectorAll("td")];
        if (cells.length !== 5) failures.push({ index, cells: cells.length });
        let previousBottom = 0;
        for (const [column, cell] of cells.entries()) {
          const rect = cell.getBoundingClientRect();
          if (
            rect.width <= 0 ||
            rect.height <= 0 ||
            rect.left < bounds.left - 1 ||
            rect.right > bounds.right + 1 ||
            cell.scrollWidth > cell.clientWidth + 1
          )
            failures.push({
              index,
              column,
              field: labels[column],
              width: rect.width,
              excess: cell.scrollWidth - cell.clientWidth,
              insideRegion:
                rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1,
            });
          if (
            stacked &&
            (cell.dataset.label !== labels[column] ||
              rect.top < previousBottom - 1)
          )
            failures.push({
              index,
              column,
              field: "stacked-field-label-or-order",
            });
          previousBottom = rect.bottom;
        }
      }
      return { rows: rows.length, failures };
    }, narrow);
  expect(layout).toEqual({ rows: 25, failures: [] });
}
function safeProblemObservation(observed: Observation) {
  return JSON.stringify({
    status: observed.status,
    codePresent: observed.codePresent ?? false,
    code: observed.code ?? null,
    stage: observed.stage,
    parseFailure: observed.parseFailure ?? null,
    requestAborted: observed.requestAborted,
    problemSource: observed.problemSource ?? null,
    responseBodyUnavailable: observed.responseBodyUnavailable ?? false,
  });
}
test.beforeEach(async ({ context }) => {
  await context.addInitScript(installDirectoryObservation);
});

test("[read] real canonical fields and fresh cursor pagination", async ({
  page,
}) => {
  const data = material(),
    logs: string[] = [];
  page.on("console", (message) => {
    logs.push(message.text());
  });
  await login(page, data.admin);
  const first = await readAfter(page, () =>
    page
      .getByRole("navigation", { name: "系统导航", exact: true })
      .getByRole("link", { name: "系统设置", exact: true })
      .click(),
  );
  await matchesDatabase(page, first, data.expected.slice(0, 25));
  expect(
    typeof first.value?.next_cursor === "string" &&
      first.value.next_cursor.length > 0,
  ).toBe(true);
  const next = await readAfter(page, () =>
    page.getByRole("button", { name: "下一页", exact: true }).click(),
  );
  expect(next.cursor === first.value!.next_cursor).toBe(true);
  await matchesDatabase(page, next, data.expected.slice(25));
  await expect(
    page.getByRole("button", { name: "下一页", exact: true }),
  ).toBeDisabled();
  const previous = await readAfter(page, () =>
    page.getByRole("button", { name: "上一页", exact: true }).click(),
  );
  expect(previous.cursor).toBeNull();
  await matchesDatabase(page, previous, data.expected.slice(0, 25));
  const refresh = await readAfter(page, () =>
    page.getByRole("button", { name: "刷新", exact: true }).click(),
  );
  expect(refresh.cursor).toBeNull();
  await matchesDatabase(page, refresh, data.expected.slice(0, 25));
  const unique = new Set(
    [...first.value!.items, ...next.value!.items].map((item) => item.id),
  );
  expect(unique.size === data.expected.length).toBe(true);
  expect(
    logs.some(
      (line) =>
        data.expected.some((user) => line.includes(user.email)) ||
        line.includes(first.value!.next_cursor!),
    ),
  ).toBe(false);
  expect(
    await page.evaluate(
      () => localStorage.length === 0 && sessionStorage.length === 0,
    ),
  ).toBe(true);
  result({ canonical: true, pages: 4, rows: unique.size });
});

test("[authority] real revocation, role loss and explicit identity switch", async ({
  page,
}) => {
  const data = material();
  await login(page, data.member);
  await expect(
    page.getByRole("link", { name: "系统设置", exact: true }),
  ).toHaveCount(0);
  await page.goto("/system/users");
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => (window as ObservedWindow).__directoryObservation!.sequence,
    ),
  ).toBe(0);
  await expect(page.locator("table")).toHaveCount(0);
  await page.goto("/login?switch=1");
  await page
    .getByRole("button", { name: "退出当前账号后登录", exact: true })
    .click();
  await expect(page.locator("#login-email")).toBeEditable();
  await login(page, data.admin);
  await readAfter(page, () =>
    page.getByRole("link", { name: "系统设置", exact: true }).click(),
  );
  await expect(page.locator("tbody tr")).toHaveCount(25);
  const before = await identity(page);
  expect(before.status === 200 && before.user_id === data.admin.user_id).toBe(
    true,
  );
  await changeAuthority("revoke-session", before);
  const revoked = await problemAfter(page, () =>
    page.getByRole("button", { name: "刷新", exact: true }).click(),
  );
  const revokedDiagnostic = safeProblemObservation(revoked);
  expect(revoked.status, revokedDiagnostic).toBe(401);
  expect(revoked.code, revokedDiagnostic).toBe("SESSION_REVOKED");
  await expect(page.locator("table")).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "系统设置", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "检查当前会话", exact: true }),
  ).toBeVisible();
  await login(page, data.admin);
  await readAfter(page, () =>
    page.getByRole("link", { name: "系统设置", exact: true }).click(),
  );
  const current = await identity(page);
  expect(
    current.status === 200 && current.session_id !== before.session_id,
  ).toBe(true);
  await changeAuthority("demote", current);
  const denied = await problemAfter(page, () =>
    page.getByRole("button", { name: "刷新", exact: true }).click(),
  );
  const deniedDiagnostic = safeProblemObservation(denied);
  expect(denied.status, deniedDiagnostic).toBe(403);
  expect(denied.code, deniedDiagnostic).toBe("FORBIDDEN");
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  await expect(page.locator("table")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "下一页", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "重新检查权限", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  await page.goto("/login?switch=1");
  await page
    .getByRole("button", { name: "退出当前账号后登录", exact: true })
    .click();
  await expect(page.locator("#login-email")).toBeEditable();
  await login(page, data.member);
  const member = await identity(page);
  expect(member.user_id === data.member.user_id).toBe(true);
  await page.goto("/system/users");
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  await expect(page.locator("table")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => (window as ObservedWindow).__directoryObservation!.sequence,
    ),
  ).toBe(0);
  result({ denied: 2, revoked: true, demoted: true, switched: true });
});

test("[navigation] production navigation, draft confirmation and responsive accessibility", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin, "/system/users", "用户");
  const first = await takeRead(page, 0);
  await matchesDatabase(page, first, data.expected.slice(0, 25));
  await expect(
    page.getByRole("heading", { name: "用户", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeFocused();
  await readAfter(page, () => page.keyboard.press("Enter"));
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeFocused();
  await page.locator(".account-name").click();
  await expect(page.locator("#profile-display-name")).toBeEditable();
  await page.locator("#profile-display-name").fill("未保存 directory draft");
  let sessionReads = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/session") ++sessionReads;
  });
  await page.getByRole("link", { name: "系统设置", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "放弃未保存修改？", exact: true }),
  ).toBeVisible();
  expect(sessionReads).toBe(0);
  await page.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(page.locator("#profile-display-name")).toHaveValue(
    "未保存 directory draft",
  );
  await page.getByRole("link", { name: "系统设置", exact: true }).click();
  await readAfter(page, () =>
    page.getByRole("button", { name: "放弃修改", exact: true }).click(),
  );
  await expect(
    page.getByRole("navigation", { name: "个人设置", exact: true }),
  ).toHaveCount(0);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      await noOverflow(page);
      await directoryFieldsFit(page, width <= 760);
      if (width === 390) {
        const trigger = page.getByRole("button", {
          name: "系统设置栏目",
          exact: true,
        });
        await trigger.click();
        const dialog = page.getByRole("dialog", {
          name: "系统设置栏目",
          exact: true,
        });
        await expect(dialog).toBeVisible();
        for (let tab = 0; tab < 6; tab++) {
          await page.keyboard.press("Tab");
          expect(
            await dialog.evaluate((element) =>
              element.contains(document.activeElement),
            ),
          ).toBe(true);
        }
        expect(
          await dialog.evaluate((element) =>
            [element, ...element.querySelectorAll("*")].every((node) => {
              const style = getComputedStyle(node);
              return [
                ...style.animationDuration.split(","),
                ...style.transitionDuration.split(","),
              ].every((duration) => parseFloat(duration) <= 0.00001);
            }),
          ),
        ).toBe(true);
        await page.keyboard.press("Escape");
        await expect(dialog).toHaveCount(0);
        await expect(trigger).toBeFocused();
        await trigger.click();
        await page.locator(".ui-overlay").click({ position: { x: 1, y: 1 } });
        await expect(dialog).toHaveCount(0);
        await expect(trigger).toBeFocused();
        await trigger.click();
        await dialog.getByRole("link", { name: "用户", exact: true }).click();
        await expect(dialog).toHaveCount(0);
        await noOverflow(page);
      }
      const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
      if (images)
        await page.screenshot({
          path: join(images, `system-directory-${theme}-${width}.png`),
        });
      ++layouts;
    }
  }
  const missingAPI = await page.request.get("/api/v1/does-not-exist");
  const missingAsset = await page.request.get("/assets/does-not-exist.js");
  expect(missingAPI.status() === 404 && missingAsset.status() === 404).toBe(
    true,
  );
  expect(
    (await missingAPI.text()).includes('id="app"') ||
      (await missingAsset.text()).includes('id="app"'),
  ).toBe(false);
  await page
    .locator(".account-actions")
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  await expect(page.locator("#login-email")).toBeEditable();
  await page.goto("/debug");
  await expect(
    page.getByRole("heading", { name: "未找到页面", exact: true }),
  ).toBeVisible();
  result({ navigation: true, layouts });
});
