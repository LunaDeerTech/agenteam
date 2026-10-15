import { test, expect } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

type Topic = { kind: string; project_id: string; meeting_id: string; contribution_id?: string; generation?: string; attempt?: string; execution_id?: string };
type Content = { status?: string; session?: { summary?: { version: string } | null }; items?: { type: string; waiting?: { reason: string } }[] };
type Frame = { type: string; subscription_id: string; topic?: Topic; watermark?: { epoch: string; sequence: string }; snapshot?: { content: Content } };
type Material = { origin: string; cookie: string; project_id: string; route: string; release_path: string; prompt: string; answer: string; final: string; title: string };
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_MEETING_WEB_EVIDENCE!;
let diagnostic = () => ({ stage: "not-started", frames: 0, sent: 0, sockets: 0, closed: 0, invalid: false, runtime: [] as string[], summary: [] as string[] });

// The production Session controller owns both subscriptions. These listeners
// observe real frames; they neither substitute a socket nor inject an update.
test("Meeting receives both canonical topics through the original Owner session", async ({ page, context }) => {
  const material: Material = JSON.parse(readFileSync(join(directory, "realtime-material.json"), "utf8"));
  expect(material.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  const frames: Frame[] = [];
  const sent: Frame[] = [];
  let sockets = 0, closed = 0, invalidFrame = false;
  let stage = "open";
  diagnostic = () => ({ stage, frames: frames.length, sent: sent.length, sockets, closed, invalid: invalidFrame,
    runtime: frames.filter(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.execution").map(frame => frame.snapshot?.content.status ?? "missing"),
    summary: frames.filter(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.timeline").map(frame => frame.snapshot?.content.session?.summary?.version ?? "absent") });
  page.on("websocket", socket => {
    expect(socket.url()).toBe(material.origin.replace(/^http:/, "ws:") + "/api/v1/realtime");
    sockets++;
    socket.on("framesent", event => {
      try { sent.push(JSON.parse(event.payload.toString())); }
      catch { invalidFrame = true; }
    });
    socket.on("framereceived", event => {
      try {
        if (frames.length === 256) throw new Error("FRAME_LIMIT");
        frames.push(JSON.parse(event.payload.toString()));
      } catch { invalidFrame = true; }
    });
    socket.on("close", () => { closed++; });
  });
  let startKey = "", startBody: unknown, answerKey = "", answerBody: unknown;
  page.on("request", request => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST") return;
    if (path === `/api/v1/projects/${material.project_id}/meetings`) {
      startKey = request.headers()["idempotency-key"] ?? "";
      startBody = request.postDataJSON();
    } else if (path.endsWith("/decisions")) {
      answerKey = request.headers()["idempotency-key"] ?? "";
      answerBody = request.postDataJSON();
    }
  });
  await context.addCookies([{ name: "agenteam_local_session", value: material.cookie, url: material.origin, httpOnly: true, sameSite: "Strict" }]);
  material.cookie = "";
  await page.goto(material.route);
  await expect(page.getByRole("heading", { name: "开始一场会议", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "编排 Agent", exact: true }).click();
  const roster = page.getByRole("dialog", { name: "编排参会 Agent", exact: true });
  await roster.getByRole("checkbox", { name: /meeting-agent-1/ }).check();
  await roster.getByRole("button", { name: "完成", exact: true }).click();
  await page.getByRole("button", { name: "发言模式", exact: true }).click();
  await page.getByRole("option", { name: "串行发言", exact: true }).click();
  await page.getByRole("textbox", { name: "会议首条消息", exact: true }).fill(material.prompt);
  stage = "start";
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/meetings/[0-9a-f-]{36}$`));
  const meeting = new URL(page.url()).pathname.split("/").at(-1)!;
  stage = "initial-two-snapshots";
  await expect.poll(() => frames.some(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.timeline" && frame.topic.meeting_id === meeting)).toBe(true);
  await page.getByRole("button", { name: "查看执行与审批", exact: true }).click();
  const execution = page.getByRole("region", { name: "发言执行详情", exact: true });
  const runtime = execution.getByRole("region", { name: "执行过程", exact: true });
  await expect(runtime.getByText("执行过程已更新", { exact: true })).toBeVisible();
  await expect.poll(() => frames.some(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.execution" && frame.snapshot?.content.status === "running")).toBe(true);
  const beforeRelease = frames.length;
  expect(sockets).toBe(1);
  for (const kind of ["meeting.timeline", "meeting.execution"]) {
    const ack = frames.findIndex(frame => frame.type === "subscribed" && frame.topic?.kind === kind);
    const snapshot = frames.findIndex(frame => frame.type === "snapshot" && frame.topic?.kind === kind);
    expect(ack).toBeGreaterThanOrEqual(0);
    expect(snapshot).toBeGreaterThan(ack);
  }
  // The test-owned peer has one held response and Release has zero effect on
  // HTTP 409. This gate never mutates Meeting, Execution, or Decision state.
  stage = "release-original-peer";
  await expect.poll(async () => {
    const status = await page.evaluate(async (path) => (await fetch(path, { method: "POST", credentials: "same-origin" })).status, material.release_path);
    if (status !== 204 && status !== 409) throw new Error("OWNED_RELEASE_FAILED");
    return status;
  }, { timeout: 8_000, intervals: [100] }).toBe(204);
  stage = "committed-waiting";
  await expect(page.getByRole("textbox", { name: "你的决定", exact: true })).toBeVisible();
  await expect.poll(() => frames.slice(beforeRelease).some(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.execution" && frame.snapshot?.content.status === "waiting" && frame.snapshot.content.items?.some(item => item.type === "waiting" && item.waiting?.reason === "decision"))).toBe(true);
  await expect(runtime).toContainText("等待结果");
  await page.screenshot({ path: join(evidence, "realtime-decision-waiting.png"), fullPage: true });
  const beforeAnswer = frames.length;
  stage = "owner-answer";
  await page.getByRole("textbox", { name: "你的决定", exact: true }).fill(material.answer);
  await page.getByRole("button", { name: "提交回答", exact: true }).click();
  stage = "committed-runtime-and-summary";
  await expect(runtime).toContainText(material.final);
  await expect(runtime).toContainText("已有结果");
  await expect(page.getByRole("heading", { name: material.title, exact: true })).toBeVisible();
  await expect.poll(() => frames.slice(beforeAnswer).some(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.timeline" && frame.snapshot?.content.session?.summary?.version === "1")).toBe(true);
  await expect.poll(() => frames.slice(beforeAnswer).some(frame => frame.type === "snapshot" && frame.topic?.kind === "meeting.execution" && frame.snapshot?.content.status === "succeeded")).toBe(true);
  expect(invalidFrame).toBe(false);
  expect(frames.some(frame => frame.type === "resync")).toBe(false);
  expect(frames.filter(frame => frame.type === "invalidated" && frame.topic?.kind === "meeting.timeline").length).toBeGreaterThan(0);
  expect(frames.filter(frame => frame.type === "invalidated" && frame.topic?.kind === "meeting.execution").length).toBeGreaterThan(0);
  expect(sent.filter(frame => frame.type === "subscribe")).toHaveLength(2);
  expect(startKey).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
  expect(answerKey).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: join(evidence, "realtime-committed-runtime.png"), fullPage: true });
  // Preserve every frame from the complete business window for the original
  // strict Go checks. Later unsubscribe acknowledgements belong to teardown.
  const businessFrames = frames.slice();
  stage = "meeting-subscription-dispose";
  // Keep the page's protocol observer alive while the real Vue owners leave.
  // Context destruction does not guarantee a later Playwright socket event.
  const closeDeadline = Date.now() + 8_000;
  const closeRemaining = () => {
    const remaining = closeDeadline - Date.now();
    if (remaining <= 0) throw new Error("ORIGINAL_SOCKET_CLOSE_DEADLINE");
    return remaining;
  };
  await page.getByRole("link", { name: "任务", exact: true }).click({ timeout: closeRemaining() });
  await expect(page).toHaveURL(material.origin + material.route.replace(/\/meetings$/, "/tasks"), { timeout: closeRemaining() });
  await expect.poll(() => closed, { timeout: closeRemaining() }).toBe(sockets);
  stage = "browser-close";
  await context.close();
  stage = "completed";
  writeFileSync(join(directory, "realtime-result.json"), JSON.stringify({ completed: true, meeting, start_key: startKey, start_body: startBody, answer_key: answerKey, answer_body: answerBody, frames: businessFrames, observed_frames: frames, sockets, closed }), { mode: 0o600 });
});

test.afterEach(async ({ page }, info) => {
  console.info("MEETING_REALTIME_OBSERVATION " + JSON.stringify(diagnostic()));
  if (info.status !== info.expectedStatus)
    await page.screenshot({ path: join(evidence, "realtime-failure.png"), fullPage: true, timeout: 1_500 }).catch(() => undefined);
});
