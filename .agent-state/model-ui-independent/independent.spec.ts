import {
  test,
  expect,
  type Page,
  type Locator,
} from "../../tests/account-captcha-web/node_modules/@playwright/test/index.js";
import { createHash, randomBytes } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  constants,
  openSync,
  closeSync,
  fstatSync,
  readSync,
  readFileSync,
  writeFileSync,
  renameSync,
  existsSync,
  readdirSync,
} from "node:fs";
import { join } from "node:path";

// Only transport/fixture observation mechanics are shared with the accepted
// harness. Scenario order, operations, expected counters and checks below are
// independently constructed; the author's mode checks are never synthesized.
type Row = Record<string, any>;
type Key = "main" | "config_recovery" | "credential_recovery";
type Fact = {
  token: string | null;
  method: string;
  path: string;
  query: string;
  status: number;
  eof: boolean;
  ended: boolean;
  cancelled: boolean;
  released: boolean;
  bytes: number;
  chunks: number[];
  has_body: boolean;
  mutation_headers: boolean;
};
const protocol = "project-owner-models.v1";
const root = process.env.MODELS_INDEPENDENT_ROOT!;
const selected = process.env.MODELS_INDEPENDENT_CASE!;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
const inputHash = process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH!;
const button = (scope: Page | Locator, name: string) =>
  scope.getByRole("button", { name, exact: true });
const credentialDialog = (page: Page) =>
  page
    .getByRole("dialog")
    .filter({ has: page.locator("#project-credential-form") });
const providerDialog = (page: Page) =>
  page
    .getByRole("dialog")
    .filter({ has: page.locator("#project-provider-form") });
function need(value: unknown, code: string): asserts value {
  if (!value) throw new Error(code);
}
function row(value: unknown): Row {
  need(
    value !== null && typeof value === "object" && !Array.isArray(value),
    "INDEPENDENT_OBJECT",
  );
  return value as Row;
}
function exact(value: unknown, keys: readonly string[]): Row {
  const v = row(value);
  need(
    Object.keys(v).length === keys.length &&
      keys.every((k) => Object.hasOwn(v, k)),
    "INDEPENDENT_MEMBERS",
  );
  return v;
}
const id = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    v,
  );
const version = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[1-9][0-9]{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const count = (v: unknown): v is number =>
  Number.isSafeInteger(v) && Number(v) >= 0;
const token = (v: unknown): v is string =>
  typeof v === "string" && /^r[0-9]{6}$/.test(v);
function publish(name: string, value: unknown, base = evidence) {
  need(/^[a-z0-9.-]+$/.test(name), "INDEPENDENT_FILE_NAME");
  const raw = JSON.stringify(value);
  need(Buffer.byteLength(raw) <= 1048576, "INDEPENDENT_FILE_LIMIT");
  const file = join(base, name);
  writeFileSync(file + ".tmp", raw, { flag: "wx", mode: 0o600 });
  renameSync(file + ".tmp", file);
}
function privateJSON(name: string, limit = 65536, base = directory): unknown {
  need(/^[a-z0-9.-]+$/.test(name), "INDEPENDENT_FILE_NAME");
  const fd = openSync(
      join(base, name),
      constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
    ),
    bytes = Buffer.alloc(limit + 1);
  try {
    const info = fstatSync(fd);
    need(
      info.isFile() &&
        (info.mode & 0o777) === 0o600 &&
        info.size > 0 &&
        info.size <= limit,
      "INDEPENDENT_PRIVATE_FILE",
    );
    let n = 0;
    while (n <= limit) {
      const read = readSync(fd, bytes, n, bytes.length - n, null);
      if (!read) break;
      n += read;
    }
    need(n > 0 && n <= limit, "INDEPENDENT_PRIVATE_LIMIT");
    const raw = new TextDecoder("utf-8", { fatal: true }).decode(
      bytes.subarray(0, n),
    );
    const parsed: unknown = JSON.parse(raw);
    const tokens = raw.match(
      /"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false|null|[{}\[\],:]/g,
    );
    need(tokens, "INDEPENDENT_JSON");
    let at = 0;
    function visit(depth: number) {
      need(depth <= 64, "INDEPENDENT_JSON_DEPTH");
      const value = tokens![at++];
      need(value !== undefined, "INDEPENDENT_JSON");
      if (value === "{") {
        const seen = new Set<string>();
        if (tokens![at] !== "}")
          for (;;) {
            const key = tokens![at++];
            need(key?.startsWith('"'), "INDEPENDENT_JSON");
            const decoded = JSON.parse(key!);
            need(
              typeof decoded === "string" &&
                decoded.isWellFormed() &&
                !seen.has(decoded),
              "INDEPENDENT_JSON_DUPLICATE",
            );
            seen.add(decoded);
            need(tokens![at++] === ":", "INDEPENDENT_JSON");
            visit(depth + 1);
            if (tokens![at] !== ",") break;
            at++;
          }
        need(tokens![at++] === "}", "INDEPENDENT_JSON");
      } else if (value === "[") {
        if (tokens![at] !== "]")
          for (;;) {
            visit(depth + 1);
            if (tokens![at] !== ",") break;
            at++;
          }
        need(tokens![at++] === "]", "INDEPENDENT_JSON");
      } else if (value.startsWith('"'))
        need(JSON.parse(value).isWellFormed(), "INDEPENDENT_JSON_UNICODE");
    }
    visit(0);
    need(at === tokens.length, "INDEPENDENT_JSON_TRAILING");
    return parsed;
  } catch {
    throw new Error("INDEPENDENT_PRIVATE_REJECTED");
  } finally {
    bytes.fill(0);
    closeSync(fd);
  }
}
function material() {
  const v = exact(privateJSON("project-models-material.json"), [
    "protocol",
    "input_hash",
    "mode",
    "actors",
    "projects",
    "expected",
    "system",
  ]);
  need(
    v.protocol === protocol &&
      v.input_hash === inputHash &&
      v.mode === (selected === "a" ? "recovery" : "authority") &&
      v.system === null,
    "INDEPENDENT_MATERIAL_IDENTITY",
  );
  const actors = exact(v.actors, ["owner", "other_owner", "other_admin"]);
  for (const actor of Object.values(actors)) {
    const a = exact(actor, ["email", "password", "user_id", "username"]);
    need(
      id(a.user_id) &&
        [a.email, a.password, a.username].every(
          (x) => typeof x === "string" && x.length > 0,
        ),
      "INDEPENDENT_ACTOR",
    );
  }
  const keys =
    selected === "a"
      ? ["main", "config_recovery", "credential_recovery"]
      : [
          "main",
          "second",
          "other",
          "admin_owned",
          "archiving",
          "archived",
          "deleting",
          "pending",
          "config_recovery",
          "credential_recovery",
          "referenced",
        ];
  exact(v.projects, keys);
  exact(v.expected, ["projects"]);
  exact(v.expected.projects, keys);
  const ids = new Set<string>();
  for (const key of keys) {
    const p = exact(v.projects[key], [
      "id",
      "username",
      "name",
      "normalized_name",
      "owner_user_id",
      "initialized",
      "lifecycle",
    ]);
    need(
      id(p.id) &&
        !ids.has(p.id) &&
        id(p.owner_user_id) &&
        [p.username, p.name, p.normalized_name].every(
          (x) => typeof x === "string" && x.length > 0,
        ) &&
        typeof p.initialized === "boolean" &&
        ["active", "archiving", "archived", "deleting"].includes(p.lifecycle),
      "INDEPENDENT_PROJECT",
    );
    ids.add(p.id);
    const seed = exact(v.expected.projects[key], [
      "providers",
      "models",
      "credentials",
    ]);
    need(Object.values(seed).every(Array.isArray), "INDEPENDENT_SEEDS");
  }
  return v;
}
let sequence = 0;
async function ipc(action: string, args: Row): Promise<Row> {
  need(
    [
      "counts",
      "snapshot",
      "arm",
      "control-state",
      "release",
      "logout",
      "archive-recovery-project",
    ].includes(action) && ++sequence <= 128,
    "INDEPENDENT_IPC_ACTION",
  );
  const envelope = { protocol, input_hash: inputHash, sequence, action, args };
  need(
    Buffer.byteLength(JSON.stringify(envelope)) <= 8192,
    "INDEPENDENT_IPC_LIMIT",
  );
  publish("project-models-ipc.json", envelope, directory);
  const name = `project-models-ack-${sequence}.json`;
  await wait("independent-b-ipc-to-be-a", () =>
    expect
      .poll(() => existsSync(join(directory, name)), { timeout: 8000 })
      .toBe(true),
  );
  const ack = exact(privateJSON(name), [
    "protocol",
    "input_hash",
    "sequence",
    "action",
    "ok",
    "result",
    "error",
  ]);
  need(
    ack.protocol === protocol &&
      ack.input_hash === inputHash &&
      ack.sequence === sequence &&
      ack.action === action &&
      ack.ok === true &&
      ack.error === null,
    "INDEPENDENT_IPC_REJECTED",
  );
  return row(ack.result);
}
async function snapshot(key: Key) {
  const v = exact(
    await wait("independent-b-snapshot-ipc-b", () =>
      ipc("snapshot", { project: key }),
    ),
    [
      "project",
      "current",
      "history",
      "reference_presence",
      "origins",
      "fixture_only",
    ],
  );
  const p = exact(v.project, [
    "project_id",
    "version",
    "initialized",
    "lifecycle",
  ]);
  need(id(p.project_id) && version(p.version), "INDEPENDENT_SNAPSHOT_PROJECT");
  exact(v.current, ["providers", "models", "credentials"]);
  exact(v.history, ["configuration", "credential"]);
  for (const [family, keys] of [
    ["configuration", ["committed_commands", "audit_records", "events"]],
    ["credential", ["committed_commands", "audit_records"]],
  ] as const)
    need(
      Object.values(exact(v.history[family], keys)).every(
        (n) => typeof n === "string" && /^(0|[1-9][0-9]*)$/.test(n),
      ),
      "INDEPENDENT_HISTORY_COUNTS",
    );
  need(
    Array.isArray(v.origins) && v.origins.length <= 4,
    "INDEPENDENT_ORIGINS",
  );
  for (const origin of v.origins) {
    exact(origin, [
      "origin_token",
      "original_request_token",
      "operation",
      "project_id",
      "target_id",
      "original_body_bytes",
      "history",
      "comparison_count",
      "comparison",
    ]);
    need(
      token(origin.origin_token) &&
        origin.origin_token === origin.original_request_token &&
        origin.project_id === p.project_id &&
        count(origin.comparison_count) &&
        count(origin.original_body_bytes) &&
        origin.original_body_bytes > 0,
      "INDEPENDENT_ORIGIN_IDENTITY",
    );
  }
  return v;
}
function delta(before: Row, after: Row, config: number, credential: number) {
  need(
    before.project.project_id === after.project.project_id,
    "INDEPENDENT_DELTA_SCOPE",
  );
  for (const family of ["configuration", "credential"])
    for (const key of Object.keys(before.history[family]))
      need(
        BigInt(after.history[family][key]) -
          BigInt(before.history[family][key]) ===
          BigInt(family === "configuration" ? config : credential),
        "INDEPENDENT_DURABLE_DELTA",
      );
}
function origin(
  value: Row,
  lost: Row,
  operation: string,
  target: string,
  family: "credential" | "configuration",
  replayed: boolean,
) {
  const matches = value.origins.filter(
    (o: Row) => o.origin_token === lost.origin_token,
  );
  need(matches.length === 1, "INDEPENDENT_ORIGIN_MISSING");
  const o = matches[0],
    h = exact(o.history, [
      "family",
      "committed_rows",
      family === "credential" ? "result" : "receipt",
    ]);
  need(
    o.operation === operation &&
      o.target_id === target &&
      h.family === family &&
      h.committed_rows === "1",
    "INDEPENDENT_COMMITTED_ORIGIN",
  );
  const receipt = exact(
    h[family === "credential" ? "result" : "receipt"],
    family === "credential"
      ? ["credential_id", "purpose", "version", "deleted"]
      : ["kind", "resource_id", "version", "affected_references"],
  );
  need(
    receipt[family === "credential" ? "credential_id" : "resource_id"] ===
      target &&
      receipt.version === "2" &&
      (family === "credential"
        ? receipt.purpose === "model" && receipt.deleted === false
        : receipt.kind === "provider.update" &&
          receipt.affected_references === "0"),
    "INDEPENDENT_HISTORY_RECEIPT",
  );
  if (!replayed)
    need(
      o.comparison_count === 0 && o.comparison === null,
      "INDEPENDENT_IMPLICIT_REPLAY",
    );
  else {
    const c = exact(o.comparison, [
      "request_token",
      "body_equal",
      "key_equal",
      "target_equal",
      "identity_equal",
      "method_equal",
      "original_body_bytes",
      "replay_body_bytes",
    ]);
    need(
      o.comparison_count === 1 &&
        token(c.request_token) &&
        c.request_token !== o.origin_token &&
        [
          "body_equal",
          "key_equal",
          "target_equal",
          "identity_equal",
          "method_equal",
        ].every((k) => c[k] === true) &&
        c.original_body_bytes === o.original_body_bytes &&
        c.replay_body_bytes === o.original_body_bytes,
      "INDEPENDENT_EXACT_ORIGINAL",
    );
  }
  return receipt;
}
async function counts() {
  const v = exact(
    await wait("independent-b-counts-ipc-c", () => ipc("counts", {})),
    [
      "operations",
      "session",
      "server",
      "controls",
      "browser_eof",
      "schema_bodies",
      "client_bodies",
    ],
  );
  const attachment = JSON.parse(
    readFileSync(
      join(
        root,
        "docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json",
      ),
      "utf8",
    ),
  );
  need(
    v.browser_eof === null &&
      v.schema_bodies === null &&
      v.client_bodies === null &&
      Array.isArray(v.operations) &&
      v.operations.length === 17,
    "INDEPENDENT_COUNT_OBSERVER",
  );
  v.operations.forEach((entry: unknown, index: number) => {
    const r = exact(entry, [
      "operation",
      "setup",
      "browser",
      "control",
      "upstream_complete",
      "handler_joined",
    ]);
    need(
      r.operation === attachment.operations[index].operation &&
        Object.entries(r).every(([k, n]) => k === "operation" || count(n)),
      "INDEPENDENT_OPERATION_COUNTS",
    );
  });
  for (const [key, fields] of [
    ["session", ["setup", "browser", "control"]],
    ["server", ["started", "finished"]],
    [
      "controls",
      ["armed", "claimed", "held", "held_joined", "cut", "disconnected"],
    ],
  ] as const)
    need(
      Object.values(exact(v[key], fields)).every(count),
      "INDEPENDENT_COUNTS",
    );
  return v;
}
function operation(v: Row, name: string) {
  return v.operations.find((r: Row) => r.operation === name).browser as number;
}
function onlyDelta(before: Row, after: Row, expected: Record<string, number>) {
  for (const r of before.operations)
    need(
      operation(after, r.operation) - r.browser ===
        (expected[r.operation] ?? 0),
      "INDEPENDENT_UNEXPECTED_REQUEST",
    );
}
async function arm(
  operation: string,
  project: Key,
  target: string,
  effect: "after_complete_cut" | "after_complete_disconnect",
) {
  const v = exact(
    await wait("independent-b-arm-ipc-d", () =>
      ipc("arm", {
        operation,
        project,
        target_id: target,
        query: null,
        effect,
      }),
    ),
    ["arm_id", "state"],
  );
  need(/^a[0-9]{4}$/.test(v.arm_id) && v.state === "armed", "INDEPENDENT_ARM");
  return v.arm_id as string;
}
async function lostControl(armID: string) {
  let v: Row = {};
  await wait("independent-b-lost-control-to-be-e", () =>
    expect
      .poll(
        async () => {
          v = exact(
            await wait("independent-b-lost-control-ipc-f", () =>
              ipc("control-state", { arm_id: armID }),
            ),
            [
              "arm_id",
              "request_token",
              "origin_token",
              "state",
              "held",
              "release_requested",
              "upstream_complete",
              "safe_admitted",
              "effect_applied",
              "joined",
            ],
          );
          return v.joined === true;
        },
        { intervals: [30, 60, 100] },
      )
      .toBe(true),
  );
  need(
    v.arm_id === armID &&
      token(v.request_token) &&
      v.origin_token === v.request_token &&
      v.state === "joined" &&
      v.upstream_complete === true &&
      v.safe_admitted === true &&
      v.effect_applied === true &&
      v.held === false,
    "INDEPENDENT_LOSS_CONTROL",
  );
  return v;
}
async function native(page: Page): Promise<Fact[]> {
  return page.evaluate(() => (window as any).__projectModelsProbe.facts());
}
async function responseBody(
  page: Page,
  start: number,
  method: string,
  path: string,
  status: number,
) {
  let matches: Fact[] = [];
  await wait("independent-b-response-body-to-be-g", () =>
    expect
      .poll(async () => {
        matches = (
          await wait("independent-b-response-body-native-h", () => native(page))
        )
          .slice(start)
          .filter((f) => f.method === method && f.path === path);
        return (
          matches.length === 1 &&
          matches[0]!.eof &&
          matches[0]!.ended &&
          matches[0]!.released
        );
      })
      .toBe(true),
  );
  const fact = matches[0]!;
  need(
    fact.status === status && token(fact.token),
    "INDEPENDENT_RESPONSE_STATUS",
  );
  const matchesMeta = readdirSync(evidence)
    .filter((name) => /^response-\d{3}\.json$/.test(name))
    .map((name) => row(privateJSON(name, 65536, evidence)))
    .filter(
      (meta) => meta.source === "browser" && meta.request_token === fact.token,
    );
  need(matchesMeta.length === 1, "INDEPENDENT_RESPONSE_SOURCE");
  const meta = matchesMeta[0]!;
  need(
    meta.protocol === protocol &&
      meta.input_hash === inputHash &&
      meta.method === method &&
      meta.endpoint === path &&
      meta.query === fact.query &&
      meta.status === status &&
      meta.body_bytes === fact.bytes &&
      /^[0-9a-f]{64}$/.test(meta.body_sha256) &&
      meta.body_file === `body-${meta.body_sha256}.json`,
    "INDEPENDENT_RESPONSE_BINDING",
  );
  const raw = readFileSync(join(evidence, meta.body_file));
  need(
    raw.length === fact.bytes &&
      raw.length <= 8388608 &&
      createHash("sha256").update(raw).digest("hex") === meta.body_sha256,
    "INDEPENDENT_RESPONSE_BYTES",
  );
  return row(privateJSON(meta.body_file, 8388608, evidence));
}
async function loss(page: Page, control: Row, maximum: 0 | 1) {
  let facts: Fact[] = [];
  await wait("independent-b-loss-to-be-i", () =>
    expect
      .poll(async () => {
        facts = (
          await wait("independent-b-loss-native-j", () => native(page))
        ).filter((f) => f.token === control.request_token);
        return facts.length === 1 && facts[0]!.ended;
      })
      .toBe(true),
  );
  const f = facts[0]!;
  need(
    f.status === 200 &&
      !f.eof &&
      f.cancelled &&
      f.released &&
      f.bytes <= maximum,
    "INDEPENDENT_NATIVE_LOSS",
  );
  publish(`independent-loss-${control.arm_id}.json`, {
    protocol,
    input_hash: inputHash,
    control,
    maximum_observed_bytes: maximum,
    native: f,
  });
}
async function receipt(scope: Page | Locator) {
  const element = scope.getByLabel("严格执行回执", { exact: true });
  await wait("independent-b-receipt-to-be-visible-k", () =>
    expect(element).toBeVisible(),
  );
  return row(
    JSON.parse(
      (await wait("independent-b-receipt-text-content-l", () =>
        element.textContent(),
      ))!,
    ),
  );
}
function sameReceipt(actual: Row, expected: Row) {
  exact(actual, Object.keys(expected));
  need(
    Object.keys(expected).every((k) => actual[k] === expected[k]),
    "INDEPENDENT_STRICT_RECEIPT",
  );
}
async function fillSecret(scope: Locator) {
  const bytes = randomBytes(47);
  let secret = bytes.toString("base64url");
  bytes.fill(0);
  try {
    await wait("independent-b-fill-secret-fill-m", () =>
      scope.getByLabel(/^新凭据材料(?:\s*\*)?$/).fill(secret),
    );
  } catch {
    throw new Error("INDEPENDENT_PRIVATE_INPUT");
  } finally {
    secret = "";
  }
}
async function secretCleared(scope: Locator) {
  const input = scope.getByLabel(/^新凭据材料(?:\s*\*)?$/);
  need(
    (await wait("independent-b-secret-cleared-input-value-n", () =>
      input.inputValue(),
    )) === "",
    "INDEPENDENT_MATERIAL_RETAINED",
  );
  await wait("independent-b-secret-cleared-to-have-attribute-o", () =>
    expect(input).toHaveAttribute("type", "password"),
  );
}
async function login(page: Page, m: Row) {
  await wait("independent-b-login-goto-p", () => page.goto("/login"));
  await wait("independent-b-login-to-be-visible-q", () =>
    expect(page.locator("#login-email")).toBeVisible(),
  );
  try {
    await wait("independent-b-login-fill-r", () =>
      page.locator("#login-email").fill(m.actors.owner.email),
    );
    await wait("independent-b-login-fill-s", () =>
      page.locator("#login-password").fill(m.actors.owner.password),
    );
  } catch {
    throw new Error("INDEPENDENT_PRIVATE_LOGIN");
  }
  await wait("independent-b-login-click-t", () => button(page, "登录").click());
  await wait("independent-b-login-to-be-enabled-u", () =>
    expect(button(page, "退出登录")).toBeEnabled(),
  );
}
async function session(page: Page) {
  const waiting = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/session" &&
      r.request().method() === "GET" &&
      r.status() === 200,
  );
  await wait("independent-b-session-evaluate-v", () =>
    page.evaluate(() => dispatchEvent(new PageTransitionEvent("pageshow"))),
  );
  const response = await wait("independent-b-session-waiting-w", () => waiting);
  need(
    (await wait("independent-b-session-finished-x", () =>
      response.finished(),
    )) === null,
    "INDEPENDENT_SESSION_EOF",
  );
  try {
    const body = row(
      await wait("independent-b-session-json-y", () => response.json()),
    );
    need(
      id(body.user.id) && id(body.session.id) && body.user.role === "user",
      "INDEPENDENT_ORDINARY_OWNER",
    );
    return { user: body.user.id as string, session: body.session.id as string };
  } catch {
    throw new Error("INDEPENDENT_SESSION_SHAPE");
  }
}
async function openProject(
  page: Page,
  m: Row,
  key: Key,
  discardPrepared = false,
) {
  const p = m.projects[key],
    target = `/${p.username}/${p.normalized_name}/settings/model-providers`;
  await wait("independent-b-open-project-evaluate-z", () =>
    page.evaluate((target) => {
      const old = history.state;
      history.pushState(
        {
          ...old,
          back: location.pathname,
          current: target,
          forward: null,
          position: (old?.position ?? 0) + 1,
          replaced: false,
        },
        "",
        target,
      );
      dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
    }, target),
  );
  const confirm = page.getByRole("dialog", {
    name: "离开项目模型设置？",
    exact: true,
  });
  if (discardPrepared) {
    await wait("independent-b-open-project-to-be-aa", () =>
      expect
        .poll(
          async () =>
            (await wait("independent-b-open-project-is-visible-ab", () =>
              confirm.isVisible(),
            )) || new URL(page.url()).pathname === target,
        )
        .toBe(true),
    );
    if (
      await wait("independent-b-open-project-is-visible-ac", () =>
        confirm.isVisible(),
      )
    )
      await wait("independent-b-open-project-click-ad", () =>
        button(confirm, "放弃并离开").click(),
      );
  }
  await wait("independent-b-open-project-to-have-url-ae", () =>
    expect(page).toHaveURL(
      new URL(target, process.env.AGENTEAM_AUTH_WEB_ORIGIN!).href,
    ),
  );
  await wait("independent-b-open-project-to-be-visible-af", () =>
    expect(
      page.getByRole("heading", { name: "Providers", level: 1, exact: true }),
    ).toBeVisible(),
  );
  const reread = button(
    page.locator("section.project-providers"),
    "重新读取项目",
  );
  if (
    await wait("independent-b-open-project-is-visible-ag", () =>
      reread.isVisible(),
    )
  ) {
    await wait("independent-b-open-project-click-ah", () => reread.click());
    await wait("independent-b-open-project-to-be-hidden-ai", () =>
      expect(reread).toBeHidden(),
    );
  }
  await wait("independent-b-open-project-to-be-enabled-aj", () =>
    expect(button(page, "创建 Provider")).toBeEnabled(),
  );
}
async function createCredential(page: Page, key: Key) {
  const before = await wait("independent-b-create-credential-snapshot-ak", () =>
      snapshot(key),
    ),
    beforeCounts = await wait("independent-b-create-credential-counts-al", () =>
      counts(),
    );
  await wait("independent-b-create-credential-click-am", () =>
    button(page, "创建凭据").click(),
  );
  const dialog = credentialDialog(page);
  await wait("independent-b-create-credential-fill-secret-an", () =>
    fillSecret(dialog),
  );
  await wait("independent-b-create-credential-click-ao", () =>
    button(dialog, "创建凭据").click(),
  );
  const r = exact(
    await wait("independent-b-create-credential-receipt-ap", () =>
      receipt(dialog),
    ),
    ["credential_id", "purpose", "version", "deleted"],
  );
  need(
    id(r.credential_id) &&
      r.purpose === "model" &&
      r.version === "1" &&
      r.deleted === false,
    "INDEPENDENT_CREDENTIAL_CREATE",
  );
  await wait("independent-b-create-credential-secret-cleared-aq", () =>
    secretCleared(dialog),
  );
  delta(
    before,
    await wait("independent-b-create-credential-snapshot-ar", () =>
      snapshot(key),
    ),
    0,
    1,
  );
  onlyDelta(
    beforeCounts,
    await wait("independent-b-create-credential-counts-as", () => counts()),
    { createProjectModelCredential: 1 },
  );
  await wait("independent-b-create-credential-click-at", () =>
    button(dialog.locator("footer"), "关闭").click(),
  );
  await wait("independent-b-create-credential-to-be-hidden-au", () =>
    expect(dialog).toBeHidden(),
  );
  await wait("independent-b-create-credential-click-av", () =>
    button(page, "管理凭据").click(),
  );
  const manage = credentialDialog(page);
  await wait("independent-b-create-credential-fill-aw", () =>
    manage
      .getByRole("textbox", { name: /^Credential ID(?:\s*\*)?$/ })
      .fill(r.credential_id),
  );
  const beforeMetadata = (
    await wait("independent-b-create-credential-native-ax", () => native(page))
  ).length;
  await wait("independent-b-create-credential-click-ay", () =>
    button(manage, "读取凭据信息").click(),
  );
  const metadata = exact(
    await wait("independent-b-create-credential-response-body-az", () =>
      responseBody(
        page,
        beforeMetadata,
        "GET",
        `/api/v1/projects/${before.project.project_id}/model-credentials/${r.credential_id}`,
        200,
      ),
    ),
    ["credential_id", "purpose", "version"],
  );
  need(
    metadata.credential_id === r.credential_id &&
      metadata.purpose === "model" &&
      metadata.version === "1",
    "INDEPENDENT_METADATA_VERSION",
  );
  await wait("independent-b-create-credential-to-have-text-ba", () =>
    expect(
      manage
        .locator("dt")
        .filter({ hasText: /^已读版本$/ })
        .locator("xpath=following-sibling::dd[1]"),
    ).toHaveText("1"),
  );
  await wait("independent-b-create-credential-to-be-disabled-bb", () =>
    expect(button(manage, "轮换凭据")).toBeDisabled(),
  );
  return { id: r.credential_id as string, dialog: manage };
}
async function rotateUnknown(
  page: Page,
  key: Key,
  id: string,
  dialog: Locator,
) {
  const before = await wait("independent-b-rotate-unknown-snapshot-bc", () =>
    snapshot(key),
  );
  await wait("independent-b-rotate-unknown-fill-secret-bd", () =>
    fillSecret(dialog),
  );
  const a = await wait("independent-b-rotate-unknown-arm-be", () =>
    arm("updateProjectModelCredential", key, id, "after_complete_cut"),
  );
  await wait("independent-b-rotate-unknown-click-bf", () =>
    button(dialog, "轮换凭据").click(),
  );
  await wait("independent-b-rotate-unknown-to-be-visible-bg", () =>
    expect(dialog.getByText(/结果尚未确认/)).toBeVisible(),
  );
  await wait("independent-b-rotate-unknown-secret-cleared-bh", () =>
    secretCleared(dialog),
  );
  const lost = await wait("independent-b-rotate-unknown-lost-control-bi", () =>
    lostControl(a),
  );
  await wait("independent-b-rotate-unknown-loss-bj", () => loss(page, lost, 1));
  const committed = await wait("independent-b-rotate-unknown-snapshot-bk", () =>
    snapshot(key),
  );
  delta(before, committed, 0, 1);
  need(
    committed.current.credentials.find((r: Row) => r.credential_id === id)
      ?.metadata?.version === "2",
    "INDEPENDENT_ROTATION_DURABLE",
  );
  origin(
    committed,
    lost,
    "updateProjectModelCredential",
    id,
    "credential",
    false,
  );
  return { lost, committed };
}
async function lookupOnly(
  page: Page,
  dialog: Locator,
  key: Key,
  family: "credential" | "configuration",
) {
  const before = await wait("independent-b-lookup-only-snapshot-bl", () =>
      snapshot(key),
    ),
    beforeCounts = await wait("independent-b-lookup-only-counts-bm", () =>
      counts(),
    );
  const nativeStart = (
    await wait("independent-b-lookup-only-native-bn", () => native(page))
  ).length;
  await wait("independent-b-lookup-only-click-bo", () =>
    button(dialog, "查证原请求").click(),
  );
  await wait("independent-b-lookup-only-to-be-visible-bp", () =>
    expect(dialog.getByLabel("历史观察", { exact: true })).toBeVisible(),
  );
  await wait("independent-b-lookup-only-to-be-visible-bq", () =>
    expect(dialog.getByText(/结果尚未确认/)).toBeVisible(),
  );
  await wait("independent-b-lookup-only-to-have-count-br", () =>
    expect(dialog.getByLabel("严格执行回执", { exact: true })).toHaveCount(0),
  );
  const body = await wait("independent-b-lookup-only-response-body-bs", () =>
    responseBody(
      page,
      nativeStart,
      "POST",
      `/api/v1/projects/${before.project.project_id}/${family === "credential" ? "model-credential-commands" : "model-commands"}/lookup`,
      200,
    ),
  );
  exact(
    body,
    family === "credential" ? ["observed", "result"] : ["found", "receipt"],
  );
  need(
    body[family === "credential" ? "observed" : "found"] === true,
    "INDEPENDENT_LOOKUP_NOT_OBSERVED",
  );
  const pendingOrigins = before.origins.filter(
    (o: Row) => o.history.family === family && o.comparison_count === 0,
  );
  need(pendingOrigins.length === 1, "INDEPENDENT_LOOKUP_ORIGIN");
  sameReceipt(
    row(body[family === "credential" ? "result" : "receipt"]),
    row(
      pendingOrigins[0].history[family === "credential" ? "result" : "receipt"],
    ),
  );
  onlyDelta(
    beforeCounts,
    await wait("independent-b-lookup-only-counts-bt", () => counts()),
    {
      [family === "credential"
        ? "lookupProjectModelCredential"
        : "lookupProjectModelConfiguration"]: 1,
    },
  );
  delta(
    before,
    await wait("independent-b-lookup-only-snapshot-bu", () => snapshot(key)),
    0,
    0,
  );
}
async function updateUnknown(page: Page, key: Key, provider: string) {
  await wait("independent-b-update-unknown-click-bv", () =>
    button(page, `读取 Provider ${provider}`).click(),
  );
  const dialog = providerDialog(page);
  await wait("independent-b-update-unknown-to-be-visible-bw", () =>
    expect(dialog).toBeVisible(),
  );
  await wait("independent-b-update-unknown-fill-bx", () =>
    dialog
      .getByRole("textbox", { name: "Provider 名称", exact: true })
      .fill("Models Independent Updated Provider"),
  );
  const before = await wait("independent-b-update-unknown-snapshot-by", () =>
      snapshot(key),
    ),
    a = await wait("independent-b-update-unknown-arm-bz", () =>
      arm(
        "updateProjectModelProvider",
        key,
        provider,
        "after_complete_disconnect",
      ),
    );
  await wait("independent-b-update-unknown-click-ca", () =>
    button(dialog, "保存 Provider").click(),
  );
  await wait("independent-b-update-unknown-to-be-visible-cb", () =>
    expect(dialog.getByText(/结果尚未确认/)).toBeVisible(),
  );
  const lost = await wait("independent-b-update-unknown-lost-control-cc", () =>
    lostControl(a),
  );
  await wait("independent-b-update-unknown-loss-cd", () => loss(page, lost, 0));
  const committed = await wait("independent-b-update-unknown-snapshot-ce", () =>
    snapshot(key),
  );
  delta(before, committed, 1, 0);
  need(
    committed.current.providers.find((r: Row) => r.id === provider)?.version ===
      "2",
    "INDEPENDENT_PROVIDER_UPDATE_DURABLE",
  );
  origin(
    committed,
    lost,
    "updateProjectModelProvider",
    provider,
    "configuration",
    false,
  );
  return { dialog, lost, committed };
}
async function archive(
  page: Page,
  key: "config_recovery" | "credential_recovery",
  committed: Row,
  ownerSession: string,
) {
  const result = exact(
    await wait("independent-b-archive-ipc-cf", () =>
      ipc("archive-recovery-project", {
        project: key,
        expected_version: committed.project.version,
      }),
    ),
    ["project_id", "initialized", "lifecycle", "fixture_only"],
  );
  need(
    result.project_id === committed.project.project_id &&
      result.initialized === true &&
      result.lifecycle === "archived" &&
      result.fixture_only === true,
    "INDEPENDENT_AUX_ARCHIVE",
  );
  need(
    (await wait("independent-b-archive-session-cg", () => session(page)))
      .session === ownerSession,
    "INDEPENDENT_ARCHIVE_IDENTITY_CHANGED",
  );
  const reread = button(
    page.locator("section.project-providers"),
    "重新读取项目",
  );
  await wait("independent-b-archive-to-be-visible-ch", () =>
    expect(reread).toBeVisible(),
  );
  await wait("independent-b-archive-click-ci", () => reread.click());
  await wait("independent-b-archive-to-be-hidden-cj", () =>
    expect(reread).toBeHidden(),
  );
  await wait("independent-b-archive-to-be-visible-ck", () =>
    expect(
      page.getByText(
        "项目当前为只读状态（archived）。可以读取信息；原请求恢复遵循其各自的当前条件。",
        { exact: true },
      ),
    ).toBeVisible(),
  );
  // Restoring the pending modal makes the page behind it inert. Inspect its
  // disabled controls explicitly without requiring a background interaction.
  const backgroundButtons = page
    .locator('section.project-providers [aria-label="项目模型操作"]')
    .getByRole("button", { includeHidden: true });
  await wait("independent-b-archive-to-be-disabled-cl", () =>
    expect(
      backgroundButtons.filter({
        has: page
          .locator('.button-label:not([aria-hidden="true"])')
          .filter({ hasText: /^创建 Provider$/ }),
      }),
    ).toBeDisabled(),
  );
  await wait("independent-b-archive-to-be-disabled-cm", () =>
    expect(
      backgroundButtons.filter({
        has: page
          .locator('.button-label:not([aria-hidden="true"])')
          .filter({ hasText: /^创建凭据$/ }),
      }),
    ).toBeDisabled(),
  );
  const after = await wait("independent-b-archive-snapshot-cn", () =>
    snapshot(key),
  );
  delta(committed, after, 0, 0);
  need(
    after.project.lifecycle === "archived" &&
      after.fixture_only.archive_recovery_applied === true,
    "INDEPENDENT_ARCHIVE_FACT",
  );
}
async function verifyBodies(page: Page) {
  const facts = await wait("independent-b-verify-bodies-native-co", () =>
    native(page),
  );
  need(
    facts.length > 0 && facts.length <= 512 && facts.every((f) => f.ended),
    "INDEPENDENT_NATIVE_TAIL",
  );
  const metas = readdirSync(evidence)
    .filter((n) => /^response-\d{3}\.json$/.test(n))
    .map((name) => ({ name, value: row(privateJSON(name, 65536, evidence)) }));
  const verified: Row[] = [];
  for (const fact of facts) {
    if (!fact.eof) continue;
    need(fact.released && token(fact.token), "INDEPENDENT_NATIVE_READER");
    const matches = metas.filter(
      (m) =>
        m.value.source === "browser" && m.value.request_token === fact.token,
    );
    need(matches.length === 1, "INDEPENDENT_BODY_CORRELATION");
    const { name, value: v } = matches[0]!;
    exact(v, [
      "protocol",
      "sequence",
      "source_run",
      "input_hash",
      "source",
      "operation",
      "method",
      "endpoint",
      "query",
      "status",
      "content_type",
      "content_length",
      "request_id",
      "project_id",
      "resource_id",
      "request_token",
      "body_file",
      "body_sha256",
      "body_bytes",
      "transfer_kind",
      "body_stage",
    ]);
    need(
      v.protocol === protocol &&
        v.input_hash === inputHash &&
        v.method === fact.method &&
        v.endpoint === fact.path &&
        v.query === fact.query &&
        v.status === fact.status &&
        v.body_bytes === fact.bytes &&
        v.body_stage === "complete_formal_upstream" &&
        ["forwarded", "hold"].includes(v.transfer_kind) &&
        /^[0-9a-f]{64}$/.test(v.body_sha256) &&
        v.body_file === `body-${v.body_sha256}.json`,
      "INDEPENDENT_BODY_BINDING",
    );
    const raw = readFileSync(join(evidence, v.body_file));
    need(
      raw.length === v.body_bytes &&
        raw.length <= 8388608 &&
        createHash("sha256").update(raw).digest("hex") === v.body_sha256,
      "INDEPENDENT_BODY_BYTES",
    );
    const result = await wait("independent-b-verify-bodies-evaluate-cp", () =>
      page.evaluate(
        (safe) => (window as any).__projectModelsProbe.verify(safe),
        {
          token: v.request_token,
          method: v.method,
          endpoint: v.endpoint,
          query: v.query,
          status: v.status,
          content_type: v.content_type,
          content_length: v.content_length,
          request_id: v.request_id,
          raw: [...raw],
        },
      ),
    );
    need(
      result.native_eof === true && result.typed_client_ok === true,
      "INDEPENDENT_NATIVE_CLIENT",
    );
    verified.push({
      sidecar: name,
      request_token: fact.token,
      browser_eof: true,
      bytes: raw.length,
      sha256: v.body_sha256,
    });
  }
  need(verified.length > 0, "INDEPENDENT_BODY_EMPTY");
  publish("same-body-input.json", verified);
  const child = spawnSync(
    "python3",
    [
      join(root, ".agent-state/model-ui-recovery/validate-same-body.py"),
      root,
      evidence,
    ],
    { encoding: "utf8", timeout: 6000, maxBuffer: 4096 },
  );
  publish("schema-validation.json", {
    status: child.status,
    signal: child.signal,
    failed_to_run: !!child.error,
    count: /^\d+\s*$/.test(child.stdout ?? "") ? Number(child.stdout) : null,
  });
  need(
    child.status === 0 && Number(child.stdout) === verified.length,
    "INDEPENDENT_SCHEMA",
  );
  publish("native-browser-observations.json", facts);
  return {
    attempts: facts.length,
    complete_eof: verified.length,
    incomplete: facts.length - verified.length,
    typed_client_ok: verified.length,
    schema_ok: verified.length,
  };
}
const required: Record<string, string[]> = {
  a: [
    "credential_rotation_unknown_original",
    "provider_update_unknown_original",
    "lookups_observe_only",
    "exact_original_and_unique_history",
    "safe_schema_client",
  ],
  b: [
    "archived_credential_rotation_lookup_only",
    "archived_provider_update_original",
    "current_revoked_session_denied",
    "exact_original_and_unique_history",
    "safe_schema_client",
  ],
};
async function finish(page: Page, checks: Record<string, boolean>) {
  const browser = await wait("independent-b-finish-verify-bodies-cq", () =>
    verifyBodies(page),
  );
  checks.safe_schema_client = true;
  exact(checks, required[selected]!);
  need(
    Object.values(checks).every((v) => v === true),
    "INDEPENDENT_CHECKS",
  );
  const server = await wait("independent-b-finish-counts-cr", () => counts());
  need(
    server.server.started === server.server.finished &&
      server.controls.held === server.controls.held_joined,
    "INDEPENDENT_HANDLER_JOIN",
  );
  need(
    server.controls.armed === 2 &&
      server.controls.claimed === 2 &&
      server.controls.cut === 1 &&
      server.controls.disconnected === 1 &&
      server.controls.held === 0,
    "INDEPENDENT_CONTROL_COUNTS",
  );
  const expected =
    selected === "a"
      ? {
          createProjectModelCredential: 1,
          updateProjectModelCredential: 2,
          updateProjectModelProvider: 2,
          lookupProjectModelCredential: 1,
          lookupProjectModelConfiguration: 1,
        }
      : {
          createProjectModelCredential: 1,
          updateProjectModelCredential: 1,
          createProjectModelProvider: 1,
          updateProjectModelProvider: 2,
          lookupProjectModelCredential: 1,
          lookupProjectModelConfiguration: 1,
        };
  for (const r of server.operations)
    if (
      r.operation.startsWith("create") ||
      r.operation.startsWith("update") ||
      r.operation.startsWith("delete") ||
      r.operation.startsWith("lookup")
    )
      need(
        r.browser === (expected as Record<string, number>)[r.operation] ||
          (r.browser === 0 && !(r.operation in expected)),
        "INDEPENDENT_FINAL_MUTATION_COUNTS",
      );
  publish(
    "independent-result.json",
    {
      protocol,
      input_hash: inputHash,
      completed: true,
      mode: `independent-${selected}`,
      checks,
      counts: { server, browser },
      schema_bodies: browser.schema_ok,
      client_bodies: browser.typed_client_ok,
      layouts: 0,
    },
    directory,
  );
  await wait("independent-b-finish-evaluate-cs", () =>
    page.evaluate(() => (window as any).__projectModelsProbe.dispose()),
  );
}
function step(value: string) {
  need(/^[a-z-]+$/.test(value), "INDEPENDENT_STEP");
  publish("independent-step.json", { case: selected, step: value });
}
// B-only diagnostic observations preserve the original Promise and action.
// A returns start() directly, without a side branch or diagnostic write.
function independentAwait(enabled: boolean, write: (name: string) => void) {
  if (!enabled)
    return <T>(_label: string, start: () => Promise<T>): Promise<T> => start();
  let sequence = 0;
  let firstRejected: string | undefined;
  const pending = new Map<number, string>();
  const record = (
    label: string,
    state: "started" | "completed" | "rejected",
  ) => {
    const labels = [...pending.values()],
      current = labels.at(-1);
    const active =
      current && labels.length > 1 ? labels[0] + "-in-" + current : current;
    const progress =
      active && state !== "started"
        ? active + "-pending-after-" + label + "-" + state
        : (active ?? label) + "-" + state;
    try {
      write(
        progress + (firstRejected ? "-first-rejected-" + firstRejected : ""),
      );
    } catch {
      /* Diagnostic I/O cannot replace the original action or error. */
    }
  };
  return <T>(label: string, start: () => Promise<T>): Promise<T> => {
    const sequenceID = ++sequence;
    pending.set(sequenceID, label);
    record(label, "started");
    const settled = (state: "completed" | "rejected") => {
      pending.delete(sequenceID);
      if (state === "rejected") firstRejected ??= label;
      record(label, state);
    };
    let original: Promise<T>;
    try {
      original = start();
    } catch (error) {
      settled("rejected");
      throw error;
    }
    void original
      .then(
        () => settled("completed"),
        () => settled("rejected"),
      )
      .catch(() => {});
    return original;
  };
}
const wait = independentAwait(selected === "b", step);
function safeFailures(errors: readonly { message?: string; stack?: string }[]) {
  // Playwright messages, stacks and call logs may contain fill values. Only
  // known codes, fixed categories and positions in this frozen source escape.
  try {
    const source = join(
      root,
      ".agent-state/model-ui-independent/independent.spec.ts",
    );
    const text = readFileSync(source, "utf8"),
      lines = text.split("\n"),
      codes = new Set(
        [...text.matchAll(/(["'])(INDEPENDENT_[A-Z_]+)\1/g)].map(
          (match) => match[2],
        ),
      );
    const escaped = source.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"),
      frame = new RegExp(
        `^\\s*at (?:[^()\\n]* \\()?(?:file:\\/\\/)?${escaped}:(\\d+):(\\d+)\\)?$`,
      );
    return errors.slice(0, 8).map((error) => {
      const message = error.message ?? "",
        candidate = /^(?:Error: )?(INDEPENDENT_[A-Z_]+)$/.exec(
          message.trim(),
        )?.[1],
        code = candidate && codes.has(candidate) ? candidate : null;
      const locations = (error.stack ?? "")
        .split("\n")
        .slice(0, 64)
        .flatMap((line) => {
          const match = frame.exec(line);
          if (!match) return [];
          const row = Number(match[1]),
            column = Number(match[2]);
          return Number.isSafeInteger(row) &&
            row > 0 &&
            row <= lines.length &&
            Number.isSafeInteger(column) &&
            column > 0 &&
            column <= lines[row - 1]!.length + 1
            ? [{ source: "independent.spec.ts", line: row, column }]
            : [];
        })
        .slice(0, 8);
      return {
        category: code
          ? "closed-harness-error"
          : message.includes("strict mode violation")
            ? "strict-locator"
            : /timeout|timed out/i.test(message)
              ? "timeout"
              : message.includes("expect(")
                ? "assertion"
                : "other",
        code,
        locations,
      };
    });
  } catch {
    return [{ category: "unavailable", code: null, locations: [] }];
  }
}
test.beforeEach(async ({ page }) => {
  const source = readFileSync(
    join(
      root,
      "output/ai/model-ui-recovery/client-probe/native-client-probe.js",
    ),
    "utf8",
  );
  await wait("independent-b-setup-add-init-script-ct", () =>
    page.addInitScript({
      content: source + "\nProjectModelsNativeProbe.install();",
    }),
  );
});
test.afterEach(async ({}, info) => {
  if (info.status !== "passed")
    publish("independent-failure.json", {
      case: selected,
      status: info.status,
      error_count: Math.min(info.errors.length, 8),
      errors: safeFailures(info.errors),
    });
});

test("[independent-a] credential rotation and existing Provider update recover their exact originals", async ({
  page,
}) => {
  const m = material(),
    checks: Record<string, boolean> = {};
  step("login");
  await login(page, m);
  await openProject(page, m, "main");
  step("credential-rotation");
  const credential = await createCredential(page, "main");
  const rotated = await rotateUnknown(
    page,
    "main",
    credential.id,
    credential.dialog,
  );
  await lookupOnly(page, credential.dialog, "main", "credential");
  await button(credential.dialog, "按原请求重放").click();
  const receiptValue = await receipt(credential.dialog),
    replayed = await snapshot("main");
  delta(rotated.committed, replayed, 0, 0);
  sameReceipt(
    receiptValue,
    origin(
      replayed,
      rotated.lost,
      "updateProjectModelCredential",
      credential.id,
      "credential",
      true,
    ),
  );
  await secretCleared(credential.dialog);
  checks.credential_rotation_unknown_original = true;
  await button(credential.dialog.locator("footer"), "关闭").click();
  await expect(credential.dialog).toBeHidden();
  step("provider-update");
  const provider = m.expected.projects.main.providers[0]?.id;
  need(id(provider), "INDEPENDENT_SEED_PROVIDER");
  const updated = await updateUnknown(page, "main", provider);
  await lookupOnly(page, updated.dialog, "main", "configuration");
  await button(updated.dialog, "按原请求重放").click();
  const confirmed = await receipt(updated.dialog),
    after = await snapshot("main");
  delta(updated.committed, after, 0, 0);
  sameReceipt(
    confirmed,
    origin(
      after,
      updated.lost,
      "updateProjectModelProvider",
      provider,
      "configuration",
      true,
    ),
  );
  need(after.origins.length === 2, "INDEPENDENT_ORIGIN_COUNT");
  checks.provider_update_unknown_original =
    checks.lookups_observe_only =
    checks.exact_original_and_unique_history =
      true;
  await button(updated.dialog, "取消").click();
  await expect(updated.dialog).toBeHidden();
  step("same-body");
  await finish(page, checks);
});

test("[independent-b] archived rotation stays observation-only while config replays under current authority", async ({
  page,
}) => {
  const m = material(),
    checks: Record<string, boolean> = {};
  step("login");
  await wait("independent-b-case-b-login-cu", () => login(page, m));
  const owner = await wait("independent-b-case-b-session-cv", () =>
    session(page),
  );
  need(owner.user === m.actors.owner.user_id, "INDEPENDENT_OWNER_ID");
  await wait("independent-b-case-b-open-project-cw", () =>
    openProject(page, m, "credential_recovery"),
  );
  step("credential-rotation-archive");
  const credential = await wait(
    "independent-b-case-b-create-credential-cx",
    () => createCredential(page, "credential_recovery"),
  );
  const rotated = await wait("independent-b-case-b-rotate-unknown-cy", () =>
    rotateUnknown(
      page,
      "credential_recovery",
      credential.id,
      credential.dialog,
    ),
  );
  await wait("independent-b-case-b-archive-cz", () =>
    archive(page, "credential_recovery", rotated.committed, owner.session),
  );
  const archivedCredential = credentialDialog(page);
  await wait("independent-b-case-b-to-be-disabled-da", () =>
    expect(button(archivedCredential, "按原请求重放")).toBeDisabled(),
  );
  await wait("independent-b-case-b-to-be-disabled-db", () =>
    expect(button(archivedCredential, "轮换凭据")).toBeDisabled(),
  );
  await wait("independent-b-case-b-lookup-only-dc", () =>
    lookupOnly(page, archivedCredential, "credential_recovery", "credential"),
  );
  const lookup = await wait("independent-b-case-b-snapshot-dd", () =>
    snapshot("credential_recovery"),
  );
  origin(
    lookup,
    rotated.lost,
    "updateProjectModelCredential",
    credential.id,
    "credential",
    false,
  );
  delta(rotated.committed, lookup, 0, 0);
  await wait("independent-b-case-b-secret-cleared-de", () =>
    secretCleared(archivedCredential),
  );
  await wait("independent-b-case-b-to-be-disabled-df", () =>
    expect(button(archivedCredential, "按原请求重放")).toBeDisabled(),
  );
  checks.archived_credential_rotation_lookup_only = true;
  await wait("independent-b-case-b-click-dg", () =>
    button(archivedCredential.locator("footer"), "关闭").click(),
  );
  await wait("independent-b-case-b-to-be-hidden-dh", () =>
    expect(archivedCredential).toBeHidden(),
  );
  const pending = page.getByLabel("原请求与历史观察", { exact: true });
  await wait("independent-b-case-b-to-be-disabled-di", () =>
    expect(button(pending, "按原请求重放")).toBeDisabled(),
  );
  await wait("independent-b-case-b-click-dj", () =>
    button(pending, "放弃本地追踪").click(),
  );
  await wait("independent-b-case-b-click-dk", () =>
    button(
      page.getByRole("dialog", { name: "放弃本地原请求追踪？", exact: true }),
      "放弃追踪",
    ).click(),
  );
  delta(
    lookup,
    await wait("independent-b-case-b-snapshot-dl", () =>
      snapshot("credential_recovery"),
    ),
    0,
    0,
  );
  step("provider-update-archive");
  await wait("independent-b-case-b-open-project-dm", () =>
    openProject(page, m, "config_recovery", true),
  );
  await wait("independent-b-case-b-click-dn", () =>
    button(page, "创建 Provider").click(),
  );
  let dialog = providerDialog(page);
  await wait("independent-b-case-b-fill-do", () =>
    dialog
      .getByRole("textbox", { name: "Provider 名称", exact: true })
      .fill("Models Independent Provider"),
  );
  await wait("independent-b-case-b-fill-dp", () =>
    dialog
      .getByRole("textbox", { name: "Base URL", exact: true })
      .fill("https://model-ui.invalid/v1"),
  );
  await wait("independent-b-case-b-click-dq", () =>
    button(dialog, "保存 Provider").click(),
  );
  const created = await wait("independent-b-case-b-receipt-dr", () =>
    receipt(dialog),
  );
  need(
    created.kind === "provider.create" &&
      id(created.resource_id) &&
      created.version === "1",
    "INDEPENDENT_PROVIDER_CREATED",
  );
  await wait("independent-b-case-b-click-ds", () =>
    button(dialog, "取消").click(),
  );
  await wait("independent-b-case-b-to-be-hidden-dt", () =>
    expect(dialog).toBeHidden(),
  );
  await wait("independent-b-case-b-click-du", () =>
    button(page, "刷新 Providers").click(),
  );
  const updated = await wait("independent-b-case-b-update-unknown-dv", () =>
    updateUnknown(page, "config_recovery", created.resource_id),
  );
  await wait("independent-b-case-b-archive-dw", () =>
    archive(page, "config_recovery", updated.committed, owner.session),
  );
  dialog = providerDialog(page);
  await wait("independent-b-case-b-to-be-disabled-dx", () =>
    expect(button(dialog, "保存 Provider")).toBeDisabled(),
  );
  await wait("independent-b-case-b-to-be-enabled-dy", () =>
    expect(button(dialog, "按原请求重放")).toBeEnabled(),
  );
  await wait("independent-b-case-b-lookup-only-dz", () =>
    lookupOnly(page, dialog, "config_recovery", "configuration"),
  );
  await wait("independent-b-case-b-click-ea", () =>
    button(dialog, "按原请求重放").click(),
  );
  const executed = await wait("independent-b-case-b-receipt-eb", () =>
      receipt(dialog),
    ),
    configAfter = await wait("independent-b-case-b-snapshot-ec", () =>
      snapshot("config_recovery"),
    );
  delta(updated.committed, configAfter, 0, 0);
  sameReceipt(
    executed,
    origin(
      configAfter,
      updated.lost,
      "updateProjectModelProvider",
      created.resource_id,
      "configuration",
      true,
    ),
  );
  need(
    configAfter.project.lifecycle === "archived",
    "INDEPENDENT_ARCHIVED_EXECUTE",
  );
  checks.archived_provider_update_original =
    checks.exact_original_and_unique_history = true;
  await wait("independent-b-case-b-click-ed", () =>
    button(dialog, "取消").click(),
  );
  await wait("independent-b-case-b-to-be-hidden-ee", () =>
    expect(dialog).toBeHidden(),
  );
  step("current-revocation");
  await wait("independent-b-case-b-open-project-ef", () =>
    openProject(page, m, "main"),
  );
  const seed = m.expected.projects.main.providers[0]?.id;
  need(id(seed), "INDEPENDENT_SEED_PROVIDER");
  await wait("independent-b-case-b-click-eg", () =>
    button(page, `读取 Provider ${seed}`).click(),
  );
  dialog = providerDialog(page);
  const before = await wait("independent-b-case-b-snapshot-eh", () =>
    snapshot("main"),
  );
  const revoked = exact(
    await wait("independent-b-case-b-ipc-ei", () =>
      ipc("logout", { session_id: owner.session }),
    ),
    ["session_id", "revoked"],
  );
  need(
    revoked.session_id === owner.session && revoked.revoked === true,
    "INDEPENDENT_REVOKED",
  );
  const previous = (
    await wait("independent-b-case-b-native-ej", () => native(page))
  ).length;
  await wait("independent-b-case-b-click-ek", () =>
    button(dialog, "重新读取 Provider").click(),
  );
  await wait("independent-b-case-b-to-be-visible-el", () =>
    expect(
      page.getByRole("heading", { name: "会话尚未确认", exact: true }),
    ).toBeVisible(),
  );
  await wait("independent-b-case-b-to-have-count-em", () =>
    expect(dialog).toHaveCount(0),
  );
  await wait("independent-b-case-b-to-have-count-en", () =>
    expect(
      page.getByRole("list", { name: "Providers 列表", exact: true }),
    ).toHaveCount(0),
  );
  await wait("independent-b-case-b-to-be-eo", () =>
    expect
      .poll(async () =>
        (await wait("independent-b-case-b-native-ep", () => native(page)))
          .slice(previous)
          .some(
            (f) =>
              f.path ===
                `/api/v1/projects/${m.projects.main.id}/model-providers/${seed}` &&
              f.method === "GET" &&
              f.status === 401 &&
              f.eof &&
              f.ended &&
              f.released,
          ),
      )
      .toBe(true),
  );
  delta(
    before,
    await wait("independent-b-case-b-snapshot-eq", () => snapshot("main")),
    0,
    0,
  );
  const deniedBody = await wait("independent-b-case-b-response-body-er", () =>
    responseBody(
      page,
      previous,
      "GET",
      `/api/v1/projects/${m.projects.main.id}/model-providers/${seed}`,
      401,
    ),
  );
  need(
    deniedBody.status === 401 &&
      ["SESSION_REVOKED", "UNAUTHENTICATED"].includes(deniedBody.code),
    "INDEPENDENT_CURRENT_AUTHORITY_PROBLEM",
  );
  checks.current_revoked_session_denied = true;
  step("same-body");
  await wait("independent-b-case-b-finish-es", () => finish(page, checks));
});
