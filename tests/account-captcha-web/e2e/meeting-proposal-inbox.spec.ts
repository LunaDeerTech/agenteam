import { test, expect, type Page, type Request, type Response } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

type Receipt = { meeting_id: string; version: string; turn_id?: string; replayed?: boolean };
type Proposal = {
  session: { id: string; project_id: string; version: string; status: string; proposal_content: string };
  source_execution_id: string;
  created_by_participant_id: string;
  origin?: { kind: "meeting" | "task"; id: string };
};
type ReviewCase = { reason: string; approve: boolean; proposal: Proposal };
type Material = { origin: string; cookie: string; project_id: string; route: string; proposals: ReviewCase[] };
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_MEETING_WEB_EVIDENCE!;

function observeResponses(page: Page) {
  type Observation = { endpoint: string; terminal: "pending" | "finished" | "failed"; status?: number; failure?: string };
  const requests = new Map<Request, Observation>();
  function entry(request: Request) {
    const path = new URL(request.url()).pathname;
    const endpoint = path.endsWith("/inbox") ? "inbox" : path.endsWith("/proposal/review") ? "proposal-review" : path.endsWith("/proposal") ? "proposal" : null;
    if (!endpoint) return;
    let value = requests.get(request);
    if (!value) { value = { endpoint, terminal: "pending" }; requests.set(request, value); }
    return value;
  }
  const started = (request: Request) => { entry(request); };
  const responded = (response: Response) => { const value = entry(response.request()); if (value) value.status = response.status(); };
  const finished = (request: Request) => { const value = entry(request); if (value) value.terminal = "finished"; };
  const failed = (request: Request) => {
    const value = entry(request);
    if (value) {
      value.terminal = "failed";
      const error = request.failure()?.errorText ?? "";
      value.failure = /^net::ERR_[A-Z_]+$/.test(error) ? error : "transport-failed";
    }
  };
  page.on("request", started);
  page.on("response", responded);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  return {
    async complete(response: Response) {
      expect(response.status()).toBe(200);
      const observed = entry(response.request());
      expect(observed).toBeDefined();
      // A failed browser Request has no successful completion to await. Keep
      // the original terminal evidence instead of leaving finished() pending.
      await expect.poll(() => observed!.terminal, { timeout: 5_000 }).not.toBe("pending");
      expect(observed, JSON.stringify(observed)).toMatchObject({ terminal: "finished" });
      expect(await response.finished()).toBeNull();
      expect(response.headers()["cache-control"]).toBe("no-store");
      return await response.json();
    },
    close() {
      console.info("PROPOSAL_BROWSER_RESPONSES " + JSON.stringify([...requests.values()]));
      page.off("request", started);
      page.off("response", responded);
      page.off("requestfinished", finished);
      page.off("requestfailed", failed);
    },
  };
}
function inboxResponse(page: Page, base: string, status: "open" | "resolved") {
  return page.waitForResponse(response => {
    const url = new URL(response.url());
    return response.request().method() === "GET" && url.pathname === base && url.searchParams.get("status") === status;
  });
}
async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

// Proposals, Agent identity, source Execution and receipts all come from the
// original App. The test observes browser actions and complete HTTP responses.
test("Owner reviews original Agent proposals through the project Inbox", async ({ page, context }) => {
  const responses = observeResponses(page);
  const complete = responses.complete;
  try {
  const material: Material = JSON.parse(readFileSync(join(directory, "proposal-material.json"), "utf8"));
  expect(material.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(material.proposals).toHaveLength(2);
  expect(material.proposals.map(value => value.approve)).toEqual([true, false]);
  const base = `/api/v1/projects/${material.project_id}/inbox`;
  const home = material.route.slice(0, -"/inbox".length);
  await context.addCookies([{ name: "agenteam_local_session", value: material.cookie, url: material.origin, httpOnly: true, sameSite: "Lax" }]);
  material.cookie = "";
  const initialRead = inboxResponse(page, base, "open");
  await page.goto(material.route);
  const initial = await complete(await initialRead);
  expect(initial.items.map((item: { source_id: string }) => item.source_id).sort()).toEqual(material.proposals.map(value => value.proposal.session.id).sort());
  await expect(page.getByRole("heading", { name: "收件箱", exact: true })).toBeVisible();
  await expect(page.getByRole("list", { name: "会议提议" }).getByRole("listitem")).toHaveCount(2);
  await noOverflow(page);
  await page.screenshot({ path: join(evidence, "inbox-light-desktop.png"), fullPage: true });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.setViewportSize({ width: 390, height: 844 });
  await noOverflow(page);
  await page.screenshot({ path: join(evidence, "inbox-dark-narrow.png"), fullPage: true });
  const reviews: Record<string, { key: string; approve: boolean; receipt: Receipt }> = {};
  for (const [index, entry] of material.proposals.entries()) {
    const id = entry.proposal.session.id;
    const proposalPath = `/api/v1/projects/${material.project_id}/meetings/${id}/proposal`;
    const sourceRead = page.waitForResponse(response => response.request().method() === "GET" && new URL(response.url()).pathname === proposalPath);
    const row = page.getByRole("list", { name: "会议提议" }).getByRole("listitem").filter({ hasText: entry.reason });
    const open = row.getByRole("link", { name: /^打开会议提议：/ });
    await open.focus();
    await expect(open).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(material.origin + home + "/meetings/" + id);
    const source: Proposal = await complete(await sourceRead);
    expect(source.source_execution_id).toBe(entry.proposal.source_execution_id);
    expect(source.created_by_participant_id).toBe(entry.proposal.created_by_participant_id);
    expect(source.origin).toEqual(entry.proposal.origin);
    expect(source.session.proposal_content).toBe(entry.reason);
    expect(source.session.status).toBe("proposed");
    const proposal = page.getByRole("region", { name: "会议提案", exact: true });
    await expect(proposal).toContainText(entry.reason);
    await noOverflow(page);
    if (index === 0) await page.screenshot({ path: join(evidence, "proposal-dark-narrow.png"), fullPage: true });
    await proposal.getByRole("button", { name: entry.approve ? "接受会议" : "拒绝并归档", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: entry.approve ? "接受这份会议提案？" : "拒绝并归档这份提案？", exact: true });
    await expect(dialog).toBeVisible();
    if (index === 0) await page.screenshot({ path: join(evidence, "proposal-review-dark-narrow.png"), fullPage: true });
    const committed = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === proposalPath + "/review");
    await dialog.getByRole("button", { name: entry.approve ? "确认接受" : "确认拒绝并归档", exact: true }).click();
    const response = await committed;
    const receipt: Receipt = await complete(response);
    const request = response.request();
    expect(request.postDataJSON()).toEqual({ expected_version: "1", approve: entry.approve });
    const key = request.headers()["idempotency-key"];
    expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
    expect(receipt.meeting_id).toBe(id);
    expect(receipt.version).toBe("2");
    expect(receipt.turn_id).toBeUndefined();
    reviews[id] = { key, approve: entry.approve, receipt };
    await expect(dialog).not.toBeVisible();
    await expect(proposal.getByRole("button", { name: entry.approve ? "接受会议" : "拒绝并归档", exact: true })).toHaveCount(0);
    const returned = inboxResponse(page, base, "open");
    await page.getByRole("link", { name: "返回收件箱", exact: true }).click();
    await expect(page).toHaveURL(material.origin + material.route);
    let current = await complete(await returned);
    // Canonical Outbox reconciliation is asynchronous. Explicit user refreshes
    // observe its convergence; they never repeat a review command.
    await expect.poll(async () => {
      if (current.items.length === 1 - index) return current.items.length;
      const refreshed = inboxResponse(page, base, "open");
      await page.getByRole("button", { name: "刷新收件箱", exact: true }).click();
      current = await complete(await refreshed);
      return current.items.length;
    }, { timeout: 5_000, intervals: [250] }).toBe(1 - index);
    expect(current.items.some((item: { source_id: string }) => item.source_id === id)).toBe(false);
  }
  await expect(page.getByText("暂无待处理的会议提议", { exact: true })).toBeVisible();
  const handledRead = inboxResponse(page, base, "resolved");
  await page.getByRole("radio", { name: "已处理", exact: true }).check();
  const handled = await complete(await handledRead);
  expect(handled.items).toHaveLength(2);
  expect(handled.items.map((item: { source_id: string }) => item.source_id).sort()).toEqual(Object.keys(reviews).sort());
  expect(handled.items.every((item: { status: string }) => item.status === "resolved")).toBe(true);
  await expect(page.getByRole("heading", { name: "收件箱", exact: true })).toBeFocused();
  await expect(page.getByRole("list", { name: "会议提议" }).getByRole("listitem")).toHaveCount(2);
  await noOverflow(page);
  await page.screenshot({ path: join(evidence, "inbox-handled-dark-narrow.png"), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await noOverflow(page);
  await page.screenshot({ path: join(evidence, "inbox-handled-light-desktop.png"), fullPage: true });
  writeFileSync(join(directory, "proposal-result.json"), JSON.stringify({ completed: true, reviews }), { mode: 0o600 });
  } finally {
    responses.close();
  }
});

test.afterEach(async ({ page }, info) => {
  if (info.status !== info.expectedStatus)
    await page.screenshot({ path: join(evidence, "proposal-failure.png"), fullPage: true, timeout: 1_500 }).catch(() => undefined);
});
