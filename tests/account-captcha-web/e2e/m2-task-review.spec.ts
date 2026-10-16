import {
  test,
  expect,
  type Page,
  type Request,
  type Response,
  type TestInfo,
} from "@playwright/test";
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import type {
  ArtifactContent,
  ArtifactMetadata,
  ArtifactRef,
} from "../../../web-crape/src/api/project-artifacts";
import type { DirectoryAgent } from "../../../web-crape/src/api/agent-directory";
import type { TaskExecution } from "../../../web-crape/src/api/task-executions";
import type { Task } from "../../../web-crape/src/api/work-review";
import {
  m2RemainingBudget,
  m2ReviewOptions,
  observeM2Bodies,
  type M2ReviewOptions,
} from "./m2-task-review.helpers";

type Material = M2ReviewOptions & {
  mode: "accept" | "return-and-accept";
  origin: string;
  cookie: string;
  route: string;
  title: string;
  project_id: string;
  task_id: string;
  task_version: string;
  worker_id: string;
  reviewer_id: string;
  work_execution_id: string;
  source_operation_id: string;
  artifact_ref: ArtifactRef;
  name: string;
  sha256: string;
  expected_content: string;
};
function material(next = false): Material {
  const path = join(
    process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
    next ? "m2-task-review-next-material.json" : "m2-task-review-material.json",
  );
  if ((statSync(path).mode & 0o077) !== 0)
    throw Error("M2_PRIVATE_MATERIAL_REQUIRED");
  const value: Material = JSON.parse(readFileSync(path, "utf8"));
  m2ReviewOptions(value);
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  if (
    !["accept", "return-and-accept"].includes(value.mode) ||
    value.mode !== process.env.AGENTEAM_M2_WEB_CASE ||
    value.origin !== process.env.AGENTEAM_AUTH_WEB_ORIGIN ||
    typeof value.cookie !== "string" ||
    !value.cookie ||
    !value.artifact_ref ||
    [
      value.project_id,
      value.task_id,
      value.worker_id,
      value.reviewer_id,
      value.work_execution_id,
      value.source_operation_id,
      value.artifact_ref.project_id,
      value.artifact_ref.artifact_id,
      value.artifact_ref.file_id,
    ].some((id) => typeof id !== "string" || !uuid.test(id)) ||
    value.artifact_ref.project_id !== value.project_id ||
    typeof value.route !== "string" ||
    !new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${value.task_id}$`).test(
      value.route,
    ) ||
    typeof value.title !== "string" ||
    !value.title ||
    typeof value.name !== "string" ||
    !value.name ||
    typeof value.task_version !== "string" ||
    !/^[1-9]\d*$/.test(value.task_version) ||
    typeof value.expected_content !== "string" ||
    !value.expected_content ||
    Buffer.byteLength(value.expected_content) > 8192 ||
    value.sha256 !==
      `sha256:${createHash("sha256").update(value.expected_content).digest("hex")}`
  )
    throw Error("INVALID_M2_REVIEW_MATERIAL");
  return value;
}
type Observation = {
  mode: Material["mode"];
  stages: string[];
  api_writes: number;
  artifact_reads: number;
};
const observations = new WeakMap<Page, Observation>();
const testPages = new WeakMap<TestInfo, Page>();
const bodyObservers = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeM2Bodies>>
>();
test.afterEach(async ({}, info) => {
  const page = testPages.get(info);
  if (!page) return;
  const bodies = bodyObservers.get(page);
  if (bodies) {
    try {
      await bodies.close();
    } finally {
      bodies.save();
    }
  }
  const observation = observations.get(page);
  if (!observation) return;
  const directory = process.env.AGENTEAM_M2_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  writeFileSync(
    join(directory, "m2-task-review-observation.json"),
    JSON.stringify(
      { schema: "m2_browser_review_v1", status: info.status, ...observation },
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
async function completeJSON<T>(response: Response): Promise<T> {
  const status = response.status();
  const bodies = bodyObservers.get(response.request().frame().page());
  let body: Buffer;
  try {
    body = await response.body();
  } catch {
    const failure = response.request().failure()?.errorText;
    if (failure === "net::ERR_ABORTED" && bodies)
      return (await bodies.complete(response)) as T;
    const code =
      failure && /^net::ERR_[A-Z0-9_]{1,96}$/.test(failure)
        ? failure
        : "NETWORK_ERROR_UNAVAILABLE";
    throw Error(`M2_HTTP_BODY_INCOMPLETE status=${status} code=${code}`);
  }
  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body));
  } catch {
    throw Error(`M2_HTTP_INVALID_JSON status=${status}`);
  }
  if (status !== 200) {
    const raw = value as { status?: unknown; code?: unknown } | null;
    const code =
      raw?.status === status &&
      typeof raw.code === "string" &&
      /^[A-Z][A-Z0-9_]{0,127}$/.test(raw.code)
        ? raw.code
        : "PROBLEM_CODE_UNAVAILABLE";
    throw Error(`M2_HTTP_UNEXPECTED status=${status} code=${code}`);
  }
  expect(bodies).toBeDefined();
  expect(await bodies!.complete(response)).toEqual(value);
  return value as T;
}
function assertTask(task: Task, data: Material, state: "in_review" | "done") {
  expect(task).toMatchObject({
    id: data.task_id,
    project_id: data.project_id,
    title: data.title,
    state,
    assignee_agent_id: data.reviewer_id,
    version:
      state === "in_review"
        ? data.task_version
        : (BigInt(data.task_version) + 1n).toString(),
  });
}
function assertArtifact(artifact: ArtifactMetadata, data: Material) {
  expect(artifact).toMatchObject({
    artifact_ref: data.artifact_ref,
    kind: "generated",
    name: data.name,
    source_execution_id: data.work_execution_id,
    source_operation_id: data.source_operation_id,
    created_by_agent_id: data.worker_id,
    object: {
      sha256: data.sha256,
      byte_size: String(Buffer.byteLength(data.expected_content)),
    },
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
  const directory = process.env.AGENTEAM_M2_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, name + ".png"),
    fullPage: true,
    animations: "disabled",
  });
}

for (const mode of ["accept", "return-and-accept"] as const)
  test(`Owner reads real worker Artifacts and explicitly reviews the same Task [${mode}]`, async ({
    page,
  }, info) => {
    testPages.set(info, page);
    let data = material();
    const runtime = m2ReviewOptions(data);
    if (runtime.deadline_unix_ms !== undefined)
      test.setTimeout(m2RemainingBudget(runtime, 45_000));
    expect(data.mode).toBe(mode);
    const firstExecution = data.work_execution_id;
    const taskPath = `/api/v1/projects/${data.project_id}/tasks/${data.task_id}`;
    const artifactPath = `/api/v1/projects/${data.project_id}/artifacts`;
    const seen: Observation = {
      mode,
      stages: [],
      api_writes: 0,
      artifact_reads: 0,
    };
    observations.set(page, seen);
    bodyObservers.set(
      page,
      await observeM2Bodies(page, {
        ...data,
        execution_id: data.work_execution_id,
      }),
    );
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
    const initial = responseFor(page, data, taskPath);
    const directory = responseFor(
      page,
      data,
      `/api/v1/projects/${data.project_id}/agents`,
    );
    await page.goto(data.route);
    assertTask(await completeJSON<Task>(await initial), data, "in_review");
    const agents = await completeJSON<{ items: DirectoryAgent[] }>(
      await directory,
    );
    await expect(fact(page, "状态")).toHaveText("评审中");
    await expect(button(page, "接受并完成")).toBeEnabled();
    seen.stages.push("initial-real-task-in-review");

    async function readArtifact() {
      const executionPath = `${taskPath}/executions/${data.work_execution_id}`;
      const contentPath = `${artifactPath}/${data.artifact_ref.artifact_id}/files/${data.artifact_ref.file_id}/content`;
      const listResponse = responseFor(page, data, `${taskPath}/executions`);
      await button(page, "查看执行记录").click();
      const executions = await completeJSON<{ items: TaskExecution[] }>(
        await listResponse,
      );
      if (data.work_execution_id !== firstExecution)
        expect(
          executions.items.find((item) => item.id === firstExecution),
        ).toMatchObject({
          project_id: data.project_id,
          task_id: data.task_id,
          agent_id: data.worker_id,
          purpose: "task/work",
          status: "succeeded",
        });
      const original = executions.items.find(
        (item) => item.id === data.work_execution_id,
      );
      expect(original).toMatchObject({
        project_id: data.project_id,
        task_id: data.task_id,
        agent_id: data.worker_id,
        purpose: "task/work",
        status: "succeeded",
      });
      const detail = responseFor(page, data, executionPath);
      await page
        .getByRole("region", { name: "执行记录", exact: true })
        .locator(`button[data-execution-id="${data.work_execution_id}"]`)
        .click();
      expect(
        await completeJSON<{ execution: TaskExecution }>(await detail),
      ).toEqual({ execution: original });
      const listing = responseFor(page, data, artifactPath);
      await button(page, "查看执行成果").click();
      const list = await completeJSON<{ items: ArtifactMetadata[] }>(
        await listing,
      );
      expect(
        new URL((await listing).url()).searchParams.get("execution_id"),
      ).toBe(data.work_execution_id);
      const artifact = list.items.find(
        (item) =>
          item.artifact_ref.artifact_id === data.artifact_ref.artifact_id,
      );
      expect(artifact).toBeDefined();
      assertArtifact(artifact!, data);
      const reading = responseFor(page, data, contentPath);
      await page
        .getByRole("region", { name: "执行成果", exact: true })
        .getByRole("button", { name: data.name, exact: true })
        .click();
      const content = await completeJSON<ArtifactContent>(await reading);
      assertArtifact(content.artifact, data);
      expect(content).toMatchObject({
        projection: "text",
        offset: "0",
        next_offset: String(Buffer.byteLength(data.expected_content)),
        truncated: false,
        text: data.expected_content,
      });
      const text = page
        .getByRole("region", { name: "成果内容", exact: true })
        .locator("pre");
      await expect(text).toBeVisible();
      expect(await text.textContent()).toBe(data.expected_content);
      await expect(text.locator("script, img, iframe, a")).toHaveCount(0);
      seen.artifact_reads++;
    }
    await readArtifact();
    expect(writes).toHaveLength(0);
    await expect(fact(page, "状态")).toHaveText("评审中");
    seen.stages.push("original-work-artifact-provenance-and-content-readable");
    await screenshot(page, "m2-worker-artifact-desktop");

    if (mode === "return-and-accept") {
      const worker = agents.items.find((agent) => agent.id === data.worker_id);
      expect(worker).toBeDefined();
      const label =
        !worker!.display_name || worker!.display_name === worker!.name
          ? worker!.name
          : `${worker!.display_name} (${worker!.name})`;
      await button(page, "退回修改").click();
      await button(page, "执行 Agent").click();
      await page
        .getByRole("listbox", { name: "执行 Agent", exact: true })
        .getByRole("option", { name: label, exact: true })
        .click();
      const comment =
        runtime.return_comment ??
        "请根据本次审核意见重做并发布新的执行成果，完成后再次提交评审。";
      await page
        .getByRole("textbox", { name: "评审说明", exact: true })
        .fill(comment);
      expect(writes).toHaveLength(0);
      const returning = responseFor(page, data, `${taskPath}/transfer`, "POST");
      await button(page, "确认退回修改").click();
      const returned = await completeJSON<{ task: Task }>(await returning);
      expect(returned.task).toMatchObject({
        id: data.task_id,
        project_id: data.project_id,
        state: "todo",
        assignee_agent_id: data.worker_id,
        version: (BigInt(data.task_version) + 1n).toString(),
      });
      expect(writes).toHaveLength(1);
      expect(new URL(writes[0]!.url()).pathname).toBe(`${taskPath}/transfer`);
      expect(writes[0]!.postDataJSON()).toEqual({
        expected_version: data.task_version,
        request: {
          target_state: "todo",
          assignee_agent_id: data.worker_id,
          comment,
        },
      });
      seen.stages.push("explicit-owner-return-to-same-task-and-worker");
      await bodyObservers.get(page)!.endDocument();
      const nextPath = join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "m2-task-review-next-material.json",
      );
      await expect
        .poll(() => existsSync(nextPath), {
          timeout: m2RemainingBudget(runtime, 25_000),
          intervals: [100, 250, 500],
        })
        .toBe(true);
      const next = material(true);
      // Private material includes a cookie: never compare or print its full object.
      if (
        [
          "mode",
          "origin",
          "cookie",
          "route",
          "title",
          "project_id",
          "task_id",
          "worker_id",
          "reviewer_id",
          "return_comment",
          "deadline_unix_ms",
        ].some(
          (key) => next[key as keyof Material] !== data[key as keyof Material],
        ) ||
        next.work_execution_id === data.work_execution_id ||
        next.source_operation_id === data.source_operation_id ||
        next.artifact_ref.artifact_id === data.artifact_ref.artifact_id ||
        BigInt(next.task_version) <= BigInt(returned.task.version)
      )
        throw Error("M2_REWORK_IDENTITY_MISMATCH");
      await bodyObservers
        .get(page)!
        .setNextExecution(next.work_execution_id, next.artifact_ref);
      data = next;
      const reworked = responseFor(page, data, taskPath);
      await page.reload();
      assertTask(await completeJSON<Task>(await reworked), data, "in_review");
      await expect(fact(page, "状态")).toHaveText("评审中");
      await readArtifact();
      expect(writes).toHaveLength(1);
      seen.stages.push(
        "same-task-new-execution-and-artifact-after-real-rework",
      );
      await screenshot(page, "m2-reworked-artifact-desktop");
    }
    const previousWrites = mode === "accept" ? 0 : 1;
    await button(page, "接受并完成").click();
    const comment = "已核对原执行发布的成果及其来源，接受并完成任务。";
    await page
      .getByRole("textbox", { name: "评审说明", exact: true })
      .fill(comment);
    expect(writes).toHaveLength(previousWrites);
    const transfer = responseFor(page, data, `${taskPath}/transfer`, "POST");
    await button(page, "确认接受并完成").focus();
    await page.keyboard.press("Enter");
    const receipt = await completeJSON<{ task: Task }>(await transfer);
    assertTask(receipt.task, data, "done");
    await expect(fact(page, "状态")).toHaveText("已完成");
    expect(writes).toHaveLength(previousWrites + 1);
    expect(new URL(writes[previousWrites]!.url()).pathname).toBe(
      `${taskPath}/transfer`,
    );
    expect(writes[previousWrites]!.postDataJSON()).toEqual({
      expected_version: data.task_version,
      request: { target_state: "done", comment },
    });
    seen.stages.push("explicit-owner-accept-confirmed-done");

    await bodyObservers.get(page)!.endDocument();
    const reloaded = responseFor(page, data, taskPath);
    await page.reload();
    assertTask(await completeJSON<Task>(await reloaded), data, "done");
    await expect(fact(page, "状态")).toHaveText("已完成");
    await readArtifact();
    expect(writes).toHaveLength(previousWrites + 1);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await screenshot(page, "m2-accepted-artifact-narrow");
    seen.stages.push("reload-preserves-done-and-original-worker-artifact");
    await bodyObservers.get(page)!.endDocument();
  });

// These three tests take no page/browser fixture and make no HTTP request.
// Run explicitly with CASE=material-check; normal case grep excludes them.
test("M2 live material retains controlled defaults and preserves explicit review text", () => {
  const defaults = m2ReviewOptions({});
  expect(defaults).toEqual({});
  expect(m2RemainingBudget(defaults, 45_000)).toBe(45_000);
  expect(m2RemainingBudget(defaults, 25_000)).toBe(25_000);
  const comment = "  补充 verified=true 的校验结果并重新发布 Artifact。\n";
  expect(m2ReviewOptions({ return_comment: comment }).return_comment).toBe(
    comment,
  );
  expect(
    m2ReviewOptions({ return_comment: "a".repeat(2000) }).return_comment
      ?.length,
  ).toBe(2000);
  for (const value of [
    null,
    "",
    " \n\t",
    "a".repeat(2001),
    "中".repeat(667),
    "bad\0comment",
    "\ud800",
  ])
    expect(() => m2ReviewOptions({ return_comment: value })).toThrow(
      "M2_INVALID_RETURN_COMMENT",
    );
});
test("M2 live material rejects expired and unsafe deadlines and spends one absolute budget", () => {
  const now = 1_000_000;
  for (const deadline of [
    null,
    "1000001",
    NaN,
    Infinity,
    now,
    now - 1,
    now + 0.5,
    now + 300_001,
    Number.MAX_SAFE_INTEGER + 1,
  ])
    expect(() => m2ReviewOptions({ deadline_unix_ms: deadline }, now)).toThrow(
      "M2_INVALID_REVIEW_DEADLINE",
    );
  const options = m2ReviewOptions({ deadline_unix_ms: now + 300_000 }, now);
  expect(m2RemainingBudget(options, 45_000, now)).toBe(300_000);
  expect(m2RemainingBudget(options, 25_000, now + 299_000)).toBe(1000);
  expect(() => m2RemainingBudget(options, 25_000, now + 300_000)).toThrow(
    "M2_REVIEW_DEADLINE_EXPIRED",
  );
});
test("M2 live material rejects present-but-undefined options and preserves immutable deadline", () => {
  expect(() => m2ReviewOptions({ return_comment: undefined })).toThrow(
    "M2_INVALID_RETURN_COMMENT",
  );
  expect(() => m2ReviewOptions({ deadline_unix_ms: undefined })).toThrow(
    "M2_INVALID_REVIEW_DEADLINE",
  );
  const options = m2ReviewOptions({ deadline_unix_ms: 2_000_000 }, 1_900_000);
  expect(Object.isFrozen(options)).toBe(true);
  expect(m2ReviewOptions(options, 1_950_000).deadline_unix_ms).toBe(2_000_000);
});
