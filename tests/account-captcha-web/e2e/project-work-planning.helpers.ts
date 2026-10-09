import {
  expect,
  type Page,
  type Locator,
  type Request,
  type Response,
} from "@playwright/test";
import {
  readFileSync,
  writeFileSync,
  renameSync,
  existsSync,
  readdirSync,
} from "node:fs";
import { join, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { createWorkPlanningAPI } from "../../../web/src/api/work-planning";
import { AccountFailure, uuid7 } from "../../../web/src/api/client";

export const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
export const evidence = process.env.AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE!;
const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
export type Credential = {
  email: string;
  password: string;
  user_id: string;
  username: string;
};
export type Seed = {
  project_id: string;
  milestone_id: string;
  sprint_id: string;
  task_id: string;
  related_id: string;
  blocker_id: string;
};
export type Material = {
  owner: Credential;
  admin: Credential;
  other: Credential;
  ids: Record<string, string>;
  projects: Record<string, { id: string; name: string }>;
  work: Record<string, Seed>;
};
export const material = (): Material =>
  JSON.parse(
    readFileSync(
      join(directory, "project-work-planning-material.json"),
      "utf8",
    ),
  );
export const button = (where: Page | Locator, name: string) =>
  where.getByRole("button", { name, exact: true });
export const details = (page: Page) =>
  page.getByRole("region", { name: "当前读取内容", exact: true });
export const recovery = (page: Page) =>
  page.getByRole("region", { name: "原命令恢复", exact: true });
export function path(
  data: Material,
  key = "main",
  kind?: "milestone" | "sprint" | "task",
  id?: string,
) {
  const base = `/${data.owner.username}/${data.projects[key]!.name}/tasks/explore`;
  return kind
    ? `${base}/${kind}s/${id ?? data.work[key]![`${kind}_id`]}`
    : base;
}
export async function login(page: Page, credential: Credential) {
  await expect(page.locator("#login-email")).toBeVisible();
  try {
    await page.locator("#login-email").fill(credential.email);
    await page.locator("#login-password").fill(credential.password);
  } catch {
    throw new Error("PRIVATE_WORK_LOGIN_INPUT_FAILED");
  }
  await button(page, "登录").click();
  await expect(button(page, "退出登录")).toBeEnabled();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
export async function ready(page: Page, id?: string) {
  await expect(
    page.getByRole("heading", { name: "任务规划", exact: true }),
  ).toBeVisible();
  await expect(button(page, "新建 Milestone")).toBeEnabled();
  if (id) await expect(details(page)).toContainText(id);
}
export async function enter(
  page: Page,
  data: Material,
  kind?: "milestone" | "sprint" | "task",
  key = "main",
) {
  await page.goto(path(data, key, kind));
  await login(page, data.owner);
  await ready(page, kind ? data.work[key]![`${kind}_id`] : undefined);
}
export async function choose(page: Page, label: string, option: string) {
  await button(page, label).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}
export async function confirmed(page: Page) {
  await expect(
    recovery(page).getByRole("heading", { name: "原命令已确认", exact: true }),
  ).toBeVisible();
  await expect(button(recovery(page), "查证原命令")).toBeEnabled();
}
export async function save(page: Page, creating = false) {
  const form = page.getByRole("form", {
    name: /^(规划结构编辑|Task 规划编辑)$/,
  });
  const action = form.getByRole("button", {
    name: creating ? "创建" : "保存修改",
    exact: true,
  });
  await expect(action).toBeEnabled();
  await action.click();
  await confirmed(page);
}
let sequence = 0;
export async function ipc(action: string, extra: Record<string, unknown> = {}) {
  const current = ++sequence;
  const request = join(directory, "project-work-planning-ipc.json");
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence: current, action, ...extra }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  const ack = join(directory, `project-work-planning-ack-${current}.json`);
  await expect.poll(() => existsSync(ack), { timeout: 4000 }).toBe(true);
  const result = JSON.parse(readFileSync(ack, "utf8"));
  expect(result.sequence === current).toBe(true);
  return result as Record<string, any>;
}
export function complete(checks: Record<string, unknown>) {
  writeFileSync(
    join(directory, "project-work-planning-result.json"),
    JSON.stringify({ completed: true, ...checks }),
    { mode: 0o600 },
  );
}

// Read the exact body already captured from this root response, never issue a
// replacement GET or ask Playwright to read a second transport body.
export async function originalBody(
  response: Response,
): Promise<Record<string, any>> {
  const requestID = await response.headerValue("x-request-id");
  expect(await response.finished()).toBeNull();
  const matches = readdirSync(evidence)
    .filter((name) => /^response-\d+\.json$/.test(name))
    .map((name) => JSON.parse(readFileSync(join(evidence, name), "utf8")))
    .filter((meta) => meta.request_id === requestID);
  expect(matches.length).toBe(1);
  const meta = matches[0];
  expect(
    meta.endpoint === new URL(response.url()).pathname &&
      meta.method === response.request().method() &&
      meta.status === response.status(),
  ).toBe(true);
  expect(meta.body_file === `body-${meta.body_sha256}.json`).toBe(true);
  const raw = readFileSync(join(evidence, meta.body_file));
  expect(
    createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
  ).toBe(true);
  return JSON.parse(raw.toString("utf8"));
}

// Requests are retained only in Node memory. Safe evidence contains response
// bytes and route/request-ID coordinates, never Cookie/CSRF/key/request bodies.
type Observed = {
  request: Request;
  url: URL;
  status: number;
  finished: boolean;
  failed: boolean;
};
export function observe(page: Page) {
  const facts = new Map<string, Observed>();
  const tails: Promise<void>[] = [];
  const failedRequests = new Set<Request>();
  page.on("requestfailed", (request) => {
    if (isWork(new URL(request.url()))) failedRequests.add(request);
  });
  const requests: { method: string; path: string }[] = [];
  const isWork = (url: URL) =>
    /^\/api\/v1\/projects\/[^/]+\/(?:milestones|sprints|tasks|structure-commands|task-commands)(?:\/|$)/.test(
      url.pathname,
    );
  page.on("request", (r) => {
    const url = new URL(r.url());
    if (isWork(url)) requests.push({ method: r.method(), path: url.pathname });
  });
  page.on("response", (r) => {
    const url = new URL(r.url());
    if (!isWork(url)) return;
    const fact: Observed = {
      request: r.request(),
      url,
      status: r.status(),
      finished: false,
      failed: false,
    };
    tails.push(
      (async () => {
        const id = await r.headerValue("x-request-id");
        if (!id || facts.has(id))
          throw new Error("WORK_RESPONSE_IDENTITY_MISSING_OR_DUPLICATE");
        facts.set(id, fact);
        const error = await r.finished();
        fact.finished = error === null;
        fact.failed = error !== null;
      })(),
    );
  });
  return {
    requests,
    async verify(expectedIncomplete = 0) {
      await Promise.all(tails);
      const checked = spawnSync(
        "python3",
        ["-c", schemaProgram, repository, evidence],
        { encoding: "utf8", timeout: 6000, maxBuffer: 4096 },
      );
      writeFileSync(
        join(evidence, "work-schema-validation.json"),
        JSON.stringify({
          status: checked.status,
          signal: checked.signal,
          stdout: checked.stdout,
          stderr: checked.stderr,
          failed_to_run: !!checked.error,
        }),
        { mode: 0o600 },
      );
      expect(checked.status === 0 && /^\d+\s*$/.test(checked.stdout)).toBe(
        true,
      );
      let decoded = 0,
        incomplete = 0;
      for (const name of readdirSync(evidence).filter((name) =>
        /^response-\d+\.json$/.test(name),
      )) {
        const meta = JSON.parse(readFileSync(join(evidence, name), "utf8"));
        const fact = facts.get(meta.request_id);
        if (!fact) continue;
        expect(
          meta.endpoint === fact.url.pathname &&
            meta.method === fact.request.method() &&
            meta.status === fact.status,
        ).toBe(true);
        expect(meta.body_file === `body-${meta.body_sha256}.json`).toBe(true);
        const raw = readFileSync(join(evidence, meta.body_file));
        expect(
          createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
        ).toBe(true);
        expect(fact.finished !== fact.failed).toBe(true);
        if (fact.failed) incomplete++;
        await decodeOriginal(fact, meta, raw);
        decoded++;
      }
      const failedWithoutHeaders = [...failedRequests].filter(
        (request) =>
          ![...facts.values()].some((fact) => fact.request === request),
      ).length;
      expect(
        decoded === facts.size &&
          decoded > 0 &&
          incomplete + failedWithoutHeaders === expectedIncomplete,
      ).toBe(true);
      writeFileSync(
        join(evidence, "work-body-validation.json"),
        JSON.stringify({
          original_bodies: decoded,
          browser_complete: decoded - incomplete,
          expected_incomplete: incomplete,
          failed_without_headers: failedWithoutHeaders,
          all_schema_client_validated: true,
        }),
        { mode: 0o600 },
      );
      return { original_bodies: decoded, expected_incomplete: incomplete };
    },
  };
}

async function decodeOriginal(
  fact: Observed,
  meta: Record<string, any>,
  raw: Buffer,
) {
  const api = createWorkPlanningAPI(async (url, init) => {
    const generated = new URL(String(url), fact.url.origin);
    expect(
      generated.pathname === fact.url.pathname &&
        init?.method === fact.request.method(),
    ).toBe(true);
    expect(
      [...generated.searchParams].sort().toString() ===
        [...fact.url.searchParams].sort().toString(),
    ).toBe(true);
    if (init?.body)
      expect(
        JSON.stringify(JSON.parse(String(init.body))) ===
          JSON.stringify(fact.request.postDataJSON()),
      ).toBe(true);
    return new Response(new Uint8Array(raw).buffer, {
      status: meta.status,
      headers: {
        "Content-Type": meta.content_type,
        "X-Request-ID": meta.request_id,
      },
    });
  });
  const parts = fact.url.pathname.split("/");
  const project = parts[4]!,
    entity = parts[5]!,
    target = parts[6];
  expect(uuid7.test(project)).toBe(true);
  const signal = new AbortController().signal;
  const query: Record<string, unknown> = {};
  for (const [key, value] of fact.url.searchParams)
    query[key] =
      key === "limit"
        ? Number(value)
        : key === "assignee_agent_id" && value === "null"
          ? null
          : value;
  const options = {
    signal,
    csrf: "x".repeat(43),
    key: "original-body-validation",
  };
  const method = fact.request.method();
  const body = method === "GET" ? undefined : fact.request.postDataJSON();
  let action: keyof typeof api;
  let args: unknown[];
  if (entity === "structure-commands" || entity === "task-commands") {
    action =
      entity === "structure-commands"
        ? "lookupStructureCommand"
        : "lookupTaskCommand";
    args = [project, body, options];
  } else if (
    entity === "tasks" &&
    (parts[7] === "blockers" || parts[7] === "blocker-commands")
  ) {
    action =
      parts[7] === "blocker-commands"
        ? "lookupTaskBlockerCommand"
        : method === "GET"
          ? "listTaskBlockers"
          : parts[8] === "resolve"
            ? "resolveTaskBlocker"
            : "addTaskBlocker";
    args = [
      project,
      target,
      method === "GET" ? query : body,
      method === "GET" ? signal : options,
    ];
  } else {
    const singular = (
      { milestones: "Milestone", sprints: "Sprint", tasks: "Task" } as Record<
        string,
        string
      >
    )[entity];
    if (!singular) throw new Error("UNKNOWN_WORK_RESPONSE_ROUTE");
    action = (
      method === "GET"
        ? target
          ? `get${singular}`
          : `list${singular}s`
        : method === "PATCH"
          ? `update${singular}`
          : parts[7] === "reorder"
            ? `reorder${singular}`
            : `create${singular}`
    ) as keyof typeof api;
    args =
      method === "GET"
        ? target
          ? [project, target, signal]
          : [project, query, signal]
        : target
          ? [project, target, body, options]
          : [project, body, options];
  }
  try {
    await (api[action] as (...values: unknown[]) => Promise<unknown>)(...args);
    expect(meta.status === 200).toBe(true);
  } catch (error) {
    expect(
      meta.status !== 200 &&
        error instanceof AccountFailure &&
        error.kind === "problem" &&
        error.problem?.status === meta.status,
    ).toBe(true);
  }
}
const schemaProgram = String.raw`
import hashlib,json,pathlib,re,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 base=pathlib.Path(sys.argv[1])/'api/openapi'; evidence=pathlib.Path(sys.argv[2])
 docs={n:json.loads((base/n).read_bytes()) for n in ['common.json','work-planning.json']}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  m=json.loads(path.read_bytes()); endpoint=m['endpoint']
  if not re.match(r'^/api/v1/projects/[^/]+/(milestones|sprints|tasks|structure-commands|task-commands)(/|$)',endpoint): continue
  assert m['source_run'].startswith(('TestAccountProjectWorkPlanningWeb','TestIndependentProjectWorkPlanningWeb')) and len(m['input_hash'])==64
  filename='body-'+m['body_sha256']+'.json'; assert m['body_file']==filename
  raw=(evidence/filename).read_bytes(); assert hashlib.sha256(raw).hexdigest()==m['body_sha256']
  found=[]
  for template,methods in docs['work-planning.json']['paths'].items():
   pattern=re.sub(r'\{[^{}]+\}',r'[^/]+',template)
   if re.fullmatch(pattern,endpoint) and m['method'].lower() in methods: found.append(methods[m['method'].lower()])
  assert len(found)==1
  response=found[0]['responses'].get(str(m['status']),found[0]['responses'].get('default'))
  if '$ref' in response:
   ref=response['$ref']; assert re.fullmatch(r'#/components/responses/[A-Za-z0-9_]+',ref)
   response=docs['work-planning.json']['components']['responses'][ref.rsplit('/',1)[1]]
  media=m['content_type'].split(';')[0].strip().lower(); schema=dict(docs['work-planning.json']); schema['$id']=(base/'work-planning.json').as_uri(); schema.update(response['content'][media]['schema'])
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw)); count+=1
 assert count>0
 print(count)
except Exception:
 print('SAFE_WORK_BODY_SCHEMA_FAILED',file=sys.stderr);sys.exit(1)
`;
