import {
  test,
  expect,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import type { DirectoryAgent } from "../../../web/src/api/agent-directory";
import type { TaskExecution } from "../../../web/src/api/task-executions";
import type { TaskExecutionOutput } from "../../../web/src/api/task-execution-output";
import type { Task } from "../../../web/src/api/work-review";

type Material = {
  mode: "read-write-run-review";
  origin: string;
  cookie: string;
  project_id: string;
  task_id: string;
  agent_id: string;
  reviewer_id: string;
  execution_id: string;
  final_round_id: string;
  execution_started_at: string;
  final_round_started_at: string;
  execution_completed_at: string;
  route: string;
  title: string;
  task_version: string;
  expected_output: string;
  artifact_checks: { path: string; expected_content?: string }[];
};

function material(): Material {
  const path = join(
    process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
    "m1-task-workflow-material.json",
  );
  if ((statSync(path).mode & 0o077) !== 0)
    throw Error("M1_PRIVATE_MATERIAL_REQUIRED");
  const value: Material = JSON.parse(readFileSync(path, "utf8"));
  // Validate without printing the material: it contains the fixture Session cookie.
  const ids = [
    value.project_id,
    value.task_id,
    value.agent_id,
    value.reviewer_id,
    value.execution_id,
    value.final_round_id,
  ];
  const times = [
    value.execution_started_at,
    value.final_round_started_at,
    value.execution_completed_at,
  ];
  if (
    value.mode !== "read-write-run-review" ||
    value.origin !== process.env.AGENTEAM_AUTH_WEB_ORIGIN ||
    typeof value.cookie !== "string" ||
    !value.cookie ||
    ids.some((id) => typeof id !== "string" || !/^[0-9a-f-]{36}$/.test(id)) ||
    times.some(
      (time) =>
        typeof time !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(time),
    ) ||
    typeof value.route !== "string" ||
    !new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${value.task_id}$`).test(
      value.route,
    ) ||
    typeof value.title !== "string" ||
    !value.title ||
    typeof value.task_version !== "string" ||
    !/^[1-9]\d*$/.test(value.task_version) ||
    typeof value.expected_output !== "string" ||
    !value.expected_output ||
    !Array.isArray(value.artifact_checks) ||
    !value.artifact_checks.length ||
    value.artifact_checks.some(
      (item) =>
        !item ||
        typeof item.path !== "string" ||
        !/^[a-zA-Z0-9_./-]+$/.test(item.path) ||
        item.path.startsWith("/") ||
        item.path
          .split("/")
          .some((part) => !part || part === "." || part === ".."),
    )
  )
    throw Error("INVALID_M1_FIXTURE_MATERIAL");
  return value;
}

type Observation = {
  stages: string[];
  api_writes: number;
  output_reads: number;
};
const observations = new WeakMap<Page, Observation>();
test.afterEach(async ({ page }, info) => {
  const observation = observations.get(page);
  if (!observation) return;
  const directory = process.env.AGENTEAM_M1_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  // Deliberately omit cookies, headers, payloads, absolute workspace paths and errors.
  writeFileSync(
    join(directory, "m1-task-workflow-observation.json"),
    JSON.stringify(
      { schema: "m1_browser_workflow_v1", status: info.status, ...observation },
      null,
      2,
    ) + "\n",
    { mode: 0o600 },
  );
});

const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");

async function completeJSON<T>(response: Response): Promise<T> {
  expect(await response.finished()).toBeNull();
  const status = response.status();
  if (status !== 200) {
    let code = "PROBLEM_CODE_UNAVAILABLE";
    try {
      const problem: unknown = await response.json();
      if (
        problem !== null &&
        typeof problem === "object" &&
        "status" in problem &&
        problem.status === status &&
        "code" in problem &&
        typeof problem.code === "string" &&
        /^[A-Z][A-Z0-9_]{0,127}$/.test(problem.code)
      )
        code = problem.code;
    } catch {
      // Neither response bodies nor parser errors are safe diagnostics.
    }
    throw Error(`M1_HTTP_UNEXPECTED status=${status} code=${code}`);
  }
  expect(status).toBe(200);
  return (await response.json()) as T;
}

function responseFor(page: Page, data: Material, path: string, method = "GET") {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.origin === data.origin &&
      url.pathname === path &&
      response.request().method() === method
    );
  });
}

function assertTask(
  task: Task,
  data: Material,
  state: "in_progress" | "in_review",
) {
  expect(task).toMatchObject({
    id: data.task_id,
    project_id: data.project_id,
    title: data.title,
    state,
    assignee_agent_id:
      state === "in_progress" ? data.agent_id : data.reviewer_id,
    version:
      state === "in_progress"
        ? data.task_version
        : (BigInt(data.task_version) + 1n).toString(),
  });
}

async function screenshot(page: Page, name: string) {
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true);
  const directory = process.env.AGENTEAM_M1_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}

test("real multi-round Task output and explicit Owner review survive refresh [read-write-run-review]", async ({
  page,
}) => {
  const data = material();
  const taskPath = `/api/v1/projects/${data.project_id}/tasks/${data.task_id}`;
  const executionPath = `${taskPath}/executions/${data.execution_id}`;
  const transferPath = `${taskPath}/transfer`;
  const seen: Observation = { stages: [], api_writes: 0, output_reads: 0 };
  observations.set(page, seen);
  const writes: Request[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (
      url.origin === data.origin &&
      url.pathname.startsWith("/api/") &&
      !["GET", "HEAD", "OPTIONS"].includes(request.method())
    ) {
      writes.push(request);
      seen.api_writes = writes.length;
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: data.origin,
      httpOnly: true,
      sameSite: "Strict",
      secure: false,
    },
  ]);

  const initialTask = responseFor(page, data, taskPath);
  const agentsResponse = responseFor(
    page,
    data,
    `/api/v1/projects/${data.project_id}/agents`,
  );
  await page.goto(data.route);
  assertTask(await completeJSON<Task>(await initialTask), data, "in_progress");
  const agents = await completeJSON<{ items: DirectoryAgent[] }>(
    await agentsResponse,
  );
  const reviewer = agents.items.find((agent) => agent.id === data.reviewer_id);
  expect(reviewer).toBeDefined();
  const reviewerLabel =
    !reviewer!.display_name || reviewer!.display_name === reviewer!.name
      ? reviewer!.name
      : `${reviewer!.display_name} (${reviewer!.name})`;
  await expect(
    drawer(page).getByRole("heading", { name: data.title, exact: true }),
  ).toBeVisible();
  await expect(fact(page, "状态")).toHaveText("进行中");
  await expect(button(page, "提交评审")).toBeEnabled();
  seen.stages.push("initial-task-in-progress");

  async function openExecution() {
    const listResponse = responseFor(page, data, `${taskPath}/executions`);
    await expect(button(page, "查看执行记录")).toBeEnabled();
    await button(page, "查看执行记录").focus();
    await page.keyboard.press("Enter");
    const list = await completeJSON<{ items: TaskExecution[] }>(
      await listResponse,
    );
    const execution = list.items.find((item) => item.id === data.execution_id);
    expect(execution).toMatchObject({
      project_id: data.project_id,
      task_id: data.task_id,
      agent_id: data.agent_id,
      purpose: "task/work",
      status: "succeeded",
    });
    const detailResponse = responseFor(page, data, executionPath);
    // The stable identity selects the actual execution; its accessible name remains
    // the production Agent/time label rather than a test-only label.
    await page
      .getByRole("region", { name: "执行记录", exact: true })
      .locator(`button[data-execution-id="${data.execution_id}"]`)
      .click();
    expect(
      await completeJSON<{ execution: TaskExecution }>(await detailResponse),
    ).toEqual({ execution });
    await expect(
      page.getByRole("heading", { name: "执行详情", exact: true }),
    ).toBeFocused();
    await expect(
      page
        .getByRole("region", { name: "执行记录", exact: true })
        .getByText("正常完成", { exact: true }),
    ).toBeVisible();
  }

  async function output(action: string) {
    const response = responseFor(page, data, `${executionPath}/runtime-view`);
    await button(page, action).click();
    const value = await completeJSON<TaskExecutionOutput>(await response);
    expect(value.execution).toMatchObject({
      id: data.execution_id,
      project_id: data.project_id,
      task_id: data.task_id,
      agent_id: data.agent_id,
      purpose: "task/work",
      status: "succeeded",
      reason: "completed",
      started_at: data.execution_started_at,
      completed_at: data.execution_completed_at,
    });
    expect(value.availability).toBe("available");
    expect(value.snapshot).toMatchObject({
      schema: "durable_final_text_v2",
      generation: value.execution.snapshot_id,
      execution_version: value.execution.version,
      included_update_progress: "1",
    });
    expect(value.snapshot!.items).toEqual([
      {
        id: data.final_round_id,
        execution_id: data.execution_id,
        seq: "1",
        type: "text",
        status: "completed",
        revision: "1",
        started_at: data.final_round_started_at,
        updated_at: data.execution_completed_at,
        completed_at: data.execution_completed_at,
        source_reference: { kind: "execution_round", id: data.final_round_id },
        data: { content: data.expected_output },
      },
    ]);
    expect(data.final_round_started_at >= data.execution_started_at).toBe(true);
    expect(data.final_round_started_at <= data.execution_completed_at).toBe(
      true,
    );
    const text = page.getByLabel("最终输出正文", { exact: true });
    await expect(text).toBeVisible();
    await text.scrollIntoViewIfNeeded();
    expect(await text.textContent()).toBe(data.expected_output);
    await expect(text.locator("script, img, iframe, a")).toHaveCount(0);
    for (const artifact of data.artifact_checks)
      expect(await text.textContent()).toContain(basename(artifact.path));
    // The Go fixture independently checks actual file bytes and command exit.
    // This UI exposes the final result and file names, not workspace downloads.
    seen.output_reads++;
    return value;
  }

  await openExecution();
  const first = await output("加载最终输出");
  expect(await output("刷新最终输出")).toEqual(first);
  await expect(fact(page, "状态")).toHaveText("进行中");
  expect(writes.length).toBe(0);
  seen.stages.push("v2-output-and-file-names-readable-without-task-transition");
  await screenshot(page, "m1-final-output-desktop");

  await button(page, "提交评审").click();
  await button(page, "评审 Agent").click();
  await page
    .getByRole("listbox", { name: "评审 Agent", exact: true })
    .getByRole("option", { name: reviewerLabel, exact: true })
    .click();
  await expect(button(page, "评审 Agent")).toBeFocused();
  const comment = "已核对真实统计结果、文件和校验退出状态，提交 Owner 评审。";
  await page
    .getByRole("textbox", { name: "评审说明", exact: true })
    .fill(comment);
  await button(page, "确认提交评审").focus();
  expect(writes.length).toBe(0);
  const transfer = responseFor(page, data, transferPath, "POST");
  await page.keyboard.press("Enter");
  const receipt = await completeJSON<{ task: Task }>(await transfer);
  assertTask(receipt.task, data, "in_review");
  await expect(fact(page, "状态")).toHaveText("评审中");
  await expect(button(page, "接受并完成")).toBeVisible();
  expect(writes.length).toBe(1);
  expect(new URL(writes[0]!.url()).pathname).toBe(transferPath);
  expect(writes[0]!.postDataJSON()).toEqual({
    expected_version: data.task_version,
    request: {
      target_state: "in_review",
      assignee_agent_id: data.reviewer_id,
      comment,
    },
  });
  seen.stages.push("explicit-owner-transfer-confirmed-in-review");

  const reloadedTask = responseFor(page, data, taskPath);
  await page.reload();
  assertTask(await completeJSON<Task>(await reloadedTask), data, "in_review");
  await expect(fact(page, "状态")).toHaveText("评审中");
  await expect(button(page, "接受并完成")).toBeVisible();
  await openExecution();
  expect(await output("加载最终输出")).toEqual(first);
  expect(writes.length).toBe(1);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.getByLabel("最终输出正文", { exact: true }).focus();
  await expect(page.getByLabel("最终输出正文", { exact: true })).toBeFocused();
  await screenshot(page, "m1-reviewed-output-narrow");
  seen.stages.push("reload-preserves-in-review-and-identical-final-output");
});
