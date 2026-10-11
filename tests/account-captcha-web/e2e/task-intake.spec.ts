import { test, expect, type Page } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  intakeMaterial,
  observeIntake,
  intakeScreenshot,
  type IntakeMaterial,
} from "./task-intake.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeIntake>>
>();
const navigations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeNavigation>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  const navigation = navigations.get(page);
  navigation?.mark("afterEach:begin", { status: info.status });
  try {
    await seen?.retire();
  } finally {
    try {
      await navigation?.retire();
    } finally {
      seen?.save(info.status);
      navigation?.save(info.status);
    }
  }
});

// One passive session belongs to this original Page. Host epoch timestamps are
// shared by actions and CDP events; browser timestamps always include timeOrigin.
// No browser input, history entry, scroll offset or product callback is replaced.
async function observeNavigation(page: Page, data: IntakeMaterial) {
  const cdp = await page.context().newCDPSession(page);
  const rows: Record<string, unknown>[] = [];
  const faults = new Set<string>();
  let mainFrame = "";
  let loader = "";
  let detached = false;
  let setup: Promise<void> | undefined;
  let setupReturned = false;
  function route(value: string) {
    try {
      const url = new URL(value);
      if (url.href === "about:blank") return "about:blank";
      return url.origin === process.env.AGENTEAM_AUTH_WEB_ORIGIN
        ? `${url.origin}${url.pathname}`
        : "<other-origin>";
    } catch {
      return "<unavailable-route>";
    }
  }
  function mark(kind: string, fields: Record<string, unknown> = {}) {
    if (rows.length >= 128) {
      faults.add("event-cap");
      return;
    }
    rows.push({
      at_host: Date.now(),
      kind,
      frame_id: mainFrame,
      loader_id: loader,
      route: route(page.url()),
      ...fields,
    });
  }
  const navigated = ({ frame }: any) => {
    if (frame.parentId) return;
    mainFrame = frame.id;
    loader = frame.loaderId;
    mark("frame-navigated", { destination: route(frame.url) });
  };
  const requested = (event: any) => {
    if (event.frameId === mainFrame)
      mark("navigation-requested", {
        reason: event.reason,
        disposition: event.disposition,
        destination: route(event.url),
      });
  };
  const scheduled = (event: any) => {
    if (event.frameId === mainFrame)
      mark("navigation-scheduled", {
        reason: event.reason,
        delay: event.delay,
        destination: route(event.url),
      });
  };
  cdp.on("Page.frameNavigated", navigated);
  cdp.on("Page.frameRequestedNavigation", requested);
  cdp.on("Page.frameScheduledNavigation", scheduled);
  function start() {
    setup = (async () => {
      try {
        await cdp.send("Page.enable");
        const value = await cdp.send("Page.getFrameTree");
        mainFrame = value.frameTree.frame.id;
        loader = value.frameTree.frame.loaderId;
        mark("before-original-goto");
      } finally {
        setupReturned = true;
      }
    })();
    return setup;
  }
  async function action<T>(stage: string, work: () => Promise<T>) {
    mark(`${stage}:begin`);
    let returned = false;
    try {
      const value = await work();
      returned = true;
      return value;
    } finally {
      mark(`${stage}:end`, { returned });
    }
  }
  async function retire() {
    mark("retire:begin");
    cdp.off("Page.frameNavigated", navigated);
    cdp.off("Page.frameRequestedNavigation", requested);
    cdp.off("Page.frameScheduledNavigation", scheduled);
    try {
      await cdp.detach();
      detached = true;
    } finally {
      // If the original test ended during setup, detach retires that session's
      // pending calls; still wait for the original setup Promise to return.
      if (setup) await Promise.allSettled([setup]);
      mark("retire:end", {
        detached,
        setup_returned: setupReturned,
        listeners_removed: true,
      });
    }
    expect(
      faults.size,
      "bounded navigation diagnostics must actually retire",
    ).toBe(0);
    expect(detached && setupReturned).toBe(true);
  }
  function save(status?: string) {
    const directory = process.env.AGENTEAM_TASK_INTAKE_WEB_EVIDENCE!;
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    writeFileSync(
      join(directory, `task-intake-${data.mode}-navigation.json`),
      JSON.stringify(
        {
          input_hash: process.env.AGENTEAM_TASK_INTAKE_WEB_INPUT_HASH!,
          status,
          page: "original-page",
          rows,
          faults: [...faults],
          detached,
          setup_returned: setupReturned,
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
  return { start, action, mark, retire, save };
}
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");

async function layout(
  page: Page,
  width: number,
  colorScheme: "light" | "dark",
) {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
}

async function boardEnds(page: Page, data: IntakeMaterial, label: string) {
  const navigation = navigations.get(page)!;
  const board = page.locator('.task-board[aria-label="任务看板"]');
  await expect(board.locator(".task-column")).toHaveCount(7);
  await expect(board.locator(".task-column h3")).toHaveText([
    "待规划",
    "待执行",
    "进行中",
    "评审中",
    "受阻",
    "已完成",
    "已取消",
  ]);
  const samples: Record<string, unknown>[] = [];
  async function sample(stage: string) {
    const before = Date.now();
    const value = await board.evaluate((node) => {
      const last = node.lastElementChild!.getBoundingClientRect(),
        own = node.getBoundingClientRect();
      const rect = (r: DOMRect) => ({
        left: r.left,
        right: r.right,
        top: r.top,
        bottom: r.bottom,
        width: r.width,
        height: r.height,
      });
      return {
        at_browser: performance.timeOrigin + performance.now(),
        document_at: performance.timeOrigin,
        overscroll_x: getComputedStyle(node).overscrollBehaviorX,
        scrollLeft: node.scrollLeft,
        scrollWidth: node.scrollWidth,
        clientWidth: node.clientWidth,
        boardRect: rect(own),
        lastRect: rect(last),
        hoverWithinBoard: node.contains(
          document.elementFromPoint(own.left + 20, own.top + 20),
        ),
        lastVisible: last.left >= own.left - 1 && last.right <= own.right + 1,
      };
    });
    if (samples.length < 48)
      samples.push({
        stage,
        at_host_before: before,
        at_host_after: Date.now(),
        ...value,
      });
    return value;
  }
  async function hover() {
    // Screenshotting may have changed the viewport's scroll position. Re-hit
    // the visible board before every real wheel; never set its scroll offset.
    await navigation.action(`${label}:hover`, () =>
      board.hover({ position: { x: 20, y: 20 } }),
    );
    const value = await sample("hover");
    expect(value.hoverWithinBoard).toBe(true);
    return value;
  }
  try {
    await navigation.action(`${label}:focus`, () => board.focus());
    await expect(board).toBeFocused();
    const initial = await hover();
    if (initial.scrollLeft > 0) {
      await navigation.action(`${label}:initial-reset-wheel`, () =>
        page.mouse.wheel(-3000, 0),
      );
    } else {
      navigation.mark(`${label}:initial-reset-not-needed`, {
        scroll_left: initial.scrollLeft,
      });
    }
    await expect.poll(() => sample("start")).toMatchObject({ scrollLeft: 0 });
    await navigation.action(`${label}:screenshot-start`, () =>
      intakeScreenshot(page, data, `${label}-board-start`),
    );
    await hover();
    await navigation.action(`${label}:right-wheel`, () =>
      page.mouse.wheel(3000, 0),
    );
    await expect.poll(() => sample("end")).toMatchObject({ lastVisible: true });
    await navigation.action(`${label}:screenshot-end`, () =>
      intakeScreenshot(page, data, `${label}-board-end`),
    );
    await hover();
    await navigation.action(`${label}:left-wheel`, () =>
      page.mouse.wheel(-3000, 0),
    );
    await expect.poll(() => sample("reset")).toMatchObject({ scrollLeft: 0 });
  } finally {
    navigation.mark(`${label}:finally`);
    // Keep the original samples; failure cleanup must not wait for a missing
    // locator or start another browser evaluation.
    const directory = process.env.AGENTEAM_TASK_INTAKE_WEB_EVIDENCE!;
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    writeFileSync(
      join(directory, `task-intake-${data.mode}-${label}-scroll.json`),
      JSON.stringify(
        {
          input_hash: process.env.AGENTEAM_TASK_INTAKE_WEB_INPUT_HASH!,
          samples,
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
}

async function selectWithKeyboard(page: Page, label: string, option: string) {
  const trigger = button(page, label);
  await trigger.focus();
  await page.keyboard.press("Enter");
  const item = page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true });
  await item.focus();
  await page.keyboard.press("Enter");
  await expect(trigger).toBeFocused();
}

async function runIntake(page: Page, lost: boolean) {
  const data = intakeMaterial();
  expect(data.mode).toBe(
    lost ? "lost-create-confirmation-and-ready" : "create-and-ready",
  );
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
  const seen = await observeIntake(page, data);
  observations.set(page, seen);
  const navigation = await observeNavigation(page, data);
  navigations.set(page, navigation);
  await navigation.start();
  await navigation.action("original-goto", () => page.goto(data.route));
  await expect(button(page, "新建任务")).toBeEnabled();
  await seen.verify();
  await layout(page, 1440, "light");
  await boardEnds(page, data, "1440-light");

  // Empty cancellation proves focus restoration without another write intent.
  await button(page, "新建任务").focus();
  await page.keyboard.press("Enter");
  const form = page.getByRole("form", { name: "新建任务", exact: true });
  await expect(
    form.getByRole("textbox", { name: "任务标题", exact: true }),
  ).toBeFocused();
  await expect(button(page, "确认创建任务")).toBeDisabled();
  await button(page, "取消创建").focus();
  await page.keyboard.press("Enter");
  await expect(form).toHaveCount(0);
  await expect(button(page, "新建任务")).toBeFocused();
  await page.keyboard.press("Enter");
  await form
    .getByRole("textbox", { name: "任务标题", exact: true })
    .fill(data.draft.title);
  await selectWithKeyboard(page, "任务类型", "任务");
  await selectWithKeyboard(page, "优先级", "中");
  await form
    .getByRole("textbox", { name: "任务描述", exact: true })
    .fill(data.draft.description);
  await form
    .getByRole("textbox", { name: "执行计划", exact: true })
    .fill(data.draft.plan);
  await layout(page, 1024, "dark");
  await intakeScreenshot(page, data, "1024-dark-create-form");
  await button(page, "确认创建任务").focus();
  await page.keyboard.press("Enter");

  if (lost) {
    const recovery = page.getByRole("region", {
      name: "创建结果恢复",
      exact: true,
    });
    await expect(
      recovery.getByText("创建结果待确认。请查询原创建结果，不要重复创建。", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(drawer(page)).toHaveCount(0);
    await expect(button(page, "新建任务")).toHaveCount(0);
    await seen.verify();
    expect(seen.requests("create")).toHaveLength(1);
    expect(seen.requests("ready")).toHaveLength(0);
    expect(seen.requests("lookup")).toHaveLength(0);
    await layout(page, 390, "dark");
    await intakeScreenshot(page, data, "390-dark-create-uncertain");
    await button(page, "查询原创建结果").focus();
    await page.keyboard.press("Enter");
  }
  await expect(fact(page, "状态")).toHaveText("待规划");
  await expect(fact(page, "版本")).toHaveText("1");
  await expect(fact(page, "负责人")).toHaveText("未指派");
  await expect(button(page, "指派并加入待执行")).toBeEnabled();
  await seen.verify();
  if (lost) seen.originalLookup();
  const created = lost
    ? seen.receipt("lookup", 0).receipt
    : seen.receipt("create", 0);
  expect(created.task.id).toBe(seen.taskID());
  expect(created.task.state).toBe("backlog");
  expect(created.task.assignee_agent_id).toBeNull();
  expect(seen.requests("ready")).toHaveLength(0);
  await layout(page, 834, "light");
  await intakeScreenshot(page, data, "834-light-unassigned-backlog");

  await button(page, "指派并加入待执行").focus();
  await page.keyboard.press("Enter");
  await expect(button(page, "执行 Agent")).toBeEnabled();
  await seen.verify();
  await selectWithKeyboard(
    page,
    "执行 Agent",
    seen.agentOption(data.worker_id),
  );
  await button(page, "确认指派").focus();
  await expect(button(page, "确认指派")).toBeEnabled();
  await page.keyboard.press("Enter");
  await expect(fact(page, "状态")).toHaveText("待执行");
  await expect(fact(page, "版本")).toHaveText("2");
  await seen.verify();
  const ready = seen.receipt("ready", 0);
  expect(ready.task.id).toBe(seen.taskID());
  expect(ready.task.assignee_agent_id).toBe(data.worker_id);
  seen.requireSeparateReady();
  await layout(page, 390, "dark");
  await intakeScreenshot(page, data, "390-dark-assigned-todo");
  await button(page, "关闭任务详情").click();
  await expect(drawer(page)).toHaveCount(0);
  await button(page, "刷新任务看板").click();
  await expect(button(page, data.draft.title)).toBeEnabled();
  await seen.verify();
  await boardEnds(page, data, "390-dark");
  await button(page, data.draft.title).click();
  await expect(fact(page, "任务 ID")).toHaveText(seen.taskID());
  await expect(fact(page, "状态")).toHaveText("待执行");
  await expect(fact(page, "版本")).toHaveText("2");
  await seen.verify();
  seen.requireSeparateReady();
}

test("current Sprint creates backlog then separately assigns [create-and-ready]", async ({
  page,
}) => runIntake(page, false));
test("lost create confirmation only looks up before assignment [lost-create-confirmation-and-ready]", async ({
  page,
}) => runIntake(page, true));
