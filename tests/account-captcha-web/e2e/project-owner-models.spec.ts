import { test, expect, type Locator, type Page } from "@playwright/test";
import { randomBytes, createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  closeSync,
  constants,
  existsSync,
  fstatSync,
  openSync,
  readFileSync,
  readdirSync,
  readSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

// Recovery helpers live outside this harness's type=module package. Use the
// Node CommonJS boundary while keeping the real exported function's TS type.
const { runReadAndPagination } = createRequire(import.meta.url)("../../../.agent-state/model-ui-recovery/read-and-pagination") as typeof import("../../../.agent-state/model-ui-recovery/read-and-pagination");

// Rebuilt from the formal project-owner-models.v1 contract. Runtime fixtures
// supply all IDs and current facts; this module contains no business stubs.
const protocol = "project-owner-models.v1";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const inputHash = process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH!;
const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const evidence = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
const modes = [
  "configuration",
  "credential",
  "recovery",
  "read",
  "authority",
  "navigation",
] as const;
type Mode = (typeof modes)[number];
type ProjectKey =
  | "main"
  | "second"
  | "other"
  | "admin_owned"
  | "archiving"
  | "archived"
  | "deleting"
  | "pending"
  | "config_recovery"
  | "credential_recovery"
  | "referenced";
type ProjectLocator = {
  id: string;
  username: string;
  name: string;
  normalized_name: string;
  owner_user_id: string;
  initialized: boolean;
  lifecycle: "active" | "archiving" | "archived" | "deleting";
};
type Actor = {
  email: string;
  password: string;
  user_id: string;
  username: string;
};
type Material = {
  protocol: typeof protocol;
  input_hash: string;
  mode: Mode;
  actors: Record<"owner" | "other_owner" | "other_admin", Actor>;
  projects: Partial<Record<ProjectKey, ProjectLocator>>;
  expected: Record<string, unknown>;
  system: Record<string, unknown> | null;
};

const uuid = (value: unknown): value is string =>
  typeof value === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    value,
  );
const version = (value: unknown): value is string =>
  typeof value === "string" &&
  /^[1-9][0-9]{0,18}$/.test(value) &&
  BigInt(value) <= 9223372036854775807n;
const count = (value: unknown): value is number =>
  Number.isSafeInteger(value) && Number(value) >= 0;
const requestToken = (value: unknown): value is string =>
  typeof value === "string" && /^r[0-9]{6}$/.test(value);
const armToken = (value: unknown): value is string =>
  typeof value === "string" && /^a[0-9]{4}$/.test(value);

// Deliberately report only fixed diagnostic labels. Never feed private object
// values or fill() parameters into assertion diffs, console, or attachments.
function invariant(value: unknown, code: string): asserts value {
  if (!value) throw new Error(code);
}
function object(value: unknown): Record<string, unknown> {
  invariant(
    value !== null && typeof value === "object" && !Array.isArray(value),
    "PROJECT_MODELS_CLOSED_OBJECT_REQUIRED",
  );
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const result = object(value);
  invariant(
    Object.keys(result).length === keys.length &&
      keys.every((key) => Object.hasOwn(result, key)),
    "PROJECT_MODELS_CLOSED_MEMBERS_REQUIRED",
  );
  return result;
}
function privateJSON(name: string, limit: number, base = directory): unknown {
  invariant(/^[a-z0-9.-]+$/.test(name), "PROJECT_MODELS_PRIVATE_NAME_INVALID");
  const fd = openSync(
    join(base, name),
    constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
  );
  const buffer = Buffer.alloc(limit + 1);
  try {
    const stat = fstatSync(fd);
    invariant(
      stat.isFile() &&
        (stat.mode & 0o777) === 0o600 &&
        stat.size > 0 &&
        stat.size <= limit,
      "PROJECT_MODELS_PRIVATE_FILE_INVALID",
    );
    let offset = 0;
    while (offset <= limit) {
      const size = readSync(fd, buffer, offset, buffer.length - offset, null);
      if (size === 0) break;
      offset += size;
    }
    invariant(offset <= limit, "PROJECT_MODELS_PRIVATE_FILE_TOO_LARGE");
    const raw = new TextDecoder("utf-8", { fatal: true }).decode(
      buffer.subarray(0, offset),
    );
    // Reject duplicate object members before JSON.parse can discard them.
    // The tokenizer only recognizes JSON grammar and never emits raw strings.
    const tokens = raw.match(
      /"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false|null|[{}\[\],:]/g,
    );
    invariant(tokens !== null, "PROJECT_MODELS_PRIVATE_JSON_INVALID");
    let position = 0;
    function visit(depth: number) {
      invariant(depth <= 64, "PROJECT_MODELS_PRIVATE_JSON_DEPTH");
      const token = tokens![position++];
      invariant(token !== undefined, "PROJECT_MODELS_PRIVATE_JSON_INVALID");
      if (token === "{") {
        const seen = new Set<string>();
        if (tokens![position] !== "}") {
          for (;;) {
            const key = tokens![position++];
            invariant(key?.startsWith('"'), "PROJECT_MODELS_PRIVATE_JSON_KEY");
            const decoded: unknown = JSON.parse(key!);
            invariant(
              typeof decoded === "string" && !seen.has(decoded),
              "PROJECT_MODELS_PRIVATE_JSON_DUPLICATE",
            );
            seen.add(decoded);
            invariant(
              tokens![position++] === ":",
              "PROJECT_MODELS_PRIVATE_JSON_INVALID",
            );
            visit(depth + 1);
            if (tokens![position] !== ",") break;
            position++;
          }
        }
        invariant(
          tokens![position++] === "}",
          "PROJECT_MODELS_PRIVATE_JSON_INVALID",
        );
      } else if (token === "[") {
        if (tokens![position] !== "]") {
          for (;;) {
            visit(depth + 1);
            if (tokens![position] !== ",") break;
            position++;
          }
        }
        invariant(
          tokens![position++] === "]",
          "PROJECT_MODELS_PRIVATE_JSON_INVALID",
        );
      }
    }
    visit(0);
    invariant(
      position === tokens.length,
      "PROJECT_MODELS_PRIVATE_JSON_TRAILING",
    );
    const parsed: unknown = JSON.parse(raw);
    function unicode(value: unknown) {
      if (typeof value === "string")
        invariant(value.isWellFormed(), "PROJECT_MODELS_PRIVATE_JSON_UNICODE");
      else if (value !== null && typeof value === "object")
        for (const [key, child] of Object.entries(value)) {
          invariant(key.isWellFormed(), "PROJECT_MODELS_PRIVATE_JSON_UNICODE");
          unicode(child);
        }
    }
    unicode(parsed);
    return parsed;
  } catch {
    throw new Error("PROJECT_MODELS_PRIVATE_INPUT_REJECTED");
  } finally {
    buffer.fill(0);
    closeSync(fd);
  }
}

export function readProjectModelsMaterial(): Material {
  const value = exact(privateJSON("project-models-material.json", 65536), [
    "protocol",
    "input_hash",
    "mode",
    "actors",
    "projects",
    "expected",
    "system",
  ]);
  invariant(
    value.protocol === protocol &&
      value.input_hash === inputHash &&
      modes.includes(value.mode as Mode),
    "PROJECT_MODELS_MATERIAL_IDENTITY_INVALID",
  );
  const actors = exact(value.actors, ["owner", "other_owner", "other_admin"]);
  for (const actor of Object.values(actors)) {
    const entry = exact(actor, ["email", "password", "user_id", "username"]);
    invariant(
      uuid(entry.user_id) &&
        [entry.email, entry.password, entry.username].every(
          (part) => typeof part === "string" && part.length > 0,
        ),
      "PROJECT_MODELS_ACTOR_INVALID",
    );
  }
  const keys: ProjectKey[] =
    value.mode === "authority"
      ? [
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
        ]
      : value.mode === "recovery"
        ? ["main", "config_recovery", "credential_recovery"]
        : value.mode === "read" || value.mode === "navigation"
          ? ["main", "second"]
          : ["main"];
  const projects = exact(value.projects, keys);
  const expected = exact(
    value.expected,
    value.mode === "read" ? ["projects", "pagination"] : ["projects"],
  );
  const seeds = exact(expected.projects, keys);
  const allIDs = new Set<string>();
  for (const key of keys) {
    const project = exact(projects[key], [
      "id",
      "username",
      "name",
      "normalized_name",
      "owner_user_id",
      "initialized",
      "lifecycle",
    ]);
    invariant(
      uuid(project.id) &&
        uuid(project.owner_user_id) &&
        typeof project.initialized === "boolean" &&
        ["active", "archiving", "archived", "deleting"].includes(
          String(project.lifecycle),
        ) &&
        [project.username, project.name, project.normalized_name].every(
          (part) => typeof part === "string" && part.length > 0,
        ),
      "PROJECT_MODELS_LOCATOR_INVALID",
    );
    const seed = exact(seeds[key], ["providers", "models", "credentials"]);
    const providerIDs = new Set<string>();
    const credentialIDs = new Set<string>();
    for (const family of ["credentials", "providers", "models"] as const) {
      invariant(Array.isArray(seed[family]), "PROJECT_MODELS_SEEDS_INVALID");
      let previous = "";
      for (const entry of seed[family] as unknown[]) {
        const row = exact(
          entry,
          family === "credentials"
            ? ["project_id", "credential_id", "purpose", "version"]
            : family === "providers"
              ? [
                  "id",
                  "project_id",
                  "name",
                  "protocol",
                  "version",
                  "credential_ref",
                ]
              : ["id", "project_id", "provider_id", "name", "version"],
        );
        const id = family === "credentials" ? row.credential_id : row.id;
        invariant(
          uuid(id) &&
            !allIDs.has(id) &&
            id > previous &&
            row.project_id === project.id &&
            version(row.version),
          "PROJECT_MODELS_SEED_IDENTITY_INVALID",
        );
        previous = id;
        allIDs.add(id);
        if (family === "credentials") {
          invariant(
            row.purpose === "model",
            "PROJECT_MODELS_CREDENTIAL_PURPOSE_INVALID",
          );
          credentialIDs.add(id);
        } else {
          invariant(
            typeof row.name === "string" && row.name.length > 0,
            "PROJECT_MODELS_SEED_NAME_INVALID",
          );
          if (family === "providers") {
            invariant(
              ["openai-chat-completions", "anthropic-messages"].includes(
                String(row.protocol),
              ) &&
                (row.credential_ref === null ||
                  credentialIDs.has(String(row.credential_ref))),
              "PROJECT_MODELS_PROVIDER_SEED_INVALID",
            );
            providerIDs.add(id);
          } else
            invariant(
              providerIDs.has(String(row.provider_id)),
              "PROJECT_MODELS_PROVIDER_BINDING_INVALID",
            );
        }
      }
    }
  }
  if (value.mode !== "navigation")
    invariant(value.system === null, "PROJECT_MODELS_SYSTEM_SCOPE_INVALID");
  return value as Material;
}

type IPC =
  | { action: "counts"; args: Record<string, never> }
  | { action: "snapshot" | "rename-reuse"; args: { project: ProjectKey } }
  | {
      action: "arm";
      args: {
        operation: string;
        project: ProjectKey | null;
        target_id: string | null;
        query: string | null;
        effect: string;
      };
    }
  | { action: "control-state"; args: { arm_id: string } }
  | { action: "release"; args: { arm_id: string; request_token: string } }
  | { action: "logout"; args: { session_id: string } }
  | {
      action: "archive-recovery-project";
      args: {
        project: "config_recovery" | "credential_recovery";
        expected_version: string;
      };
    }
  | {
      action: "reference-fact";
      args: { project: "referenced"; state: "present" | "absent" };
    };
let sequence = 0;
export async function projectModelsIPC(request: IPC) {
  invariant(++sequence <= 128, "PROJECT_MODELS_IPC_SEQUENCE_EXHAUSTED");
  const envelope = { protocol, input_hash: inputHash, sequence, ...request };
  const raw = JSON.stringify(envelope);
  invariant(
    Buffer.byteLength(raw) <= 8192,
    "PROJECT_MODELS_IPC_REQUEST_TOO_LARGE",
  );
  const file = join(directory, "project-models-ipc.json");
  writeFileSync(file + ".tmp", raw, { mode: 0o600, flag: "wx" });
  renameSync(file + ".tmp", file);
  const name = `project-models-ack-${sequence}.json`;
  await expect
    .poll(() => existsSync(join(directory, name)), { timeout: 8_000 })
    .toBe(true);
  const ack = exact(privateJSON(name, 65536), [
    "protocol",
    "input_hash",
    "sequence",
    "action",
    "ok",
    "result",
    "error",
  ]);
  invariant(
    ack.protocol === protocol &&
      ack.input_hash === inputHash &&
      ack.sequence === sequence &&
      ack.action === request.action,
    "PROJECT_MODELS_IPC_ACK_IDENTITY_INVALID",
  );
  if (ack.ok !== true) {
    invariant(
      ack.ok === false &&
        ack.result === null &&
        [
          "invalid_envelope",
          "invalid_sequence",
          "invalid_action",
          "invalid_arguments",
          "unknown_target",
          "arm_busy",
          "token_mismatch",
          "not_ready",
          "budget_exhausted",
          "fixture_failed",
        ].includes(String(ack.error)),
      "PROJECT_MODELS_IPC_FAILURE_INVALID",
    );
    throw new Error("PROJECT_MODELS_IPC_REJECTED");
  }
  invariant(ack.error === null, "PROJECT_MODELS_IPC_SUCCESS_INVALID");
  const result = object(ack.result);
  if (request.action === "arm") {
    exact(result, ["arm_id", "state"]);
    invariant(
      armToken(result.arm_id) && result.state === "armed",
      "PROJECT_MODELS_ARM_INVALID",
    );
  } else if (request.action === "control-state") {
    exact(result, [
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
    ]);
    invariant(
      result.arm_id === request.args.arm_id &&
        (result.request_token === null || requestToken(result.request_token)) &&
        (result.origin_token === null || requestToken(result.origin_token)) &&
        [
          "armed",
          "claimed",
          "upstream_complete",
          "released",
          "joined",
        ].includes(String(result.state)) &&
        [
          "held",
          "release_requested",
          "upstream_complete",
          "safe_admitted",
          "effect_applied",
          "joined",
        ].every((key) => typeof result[key] === "boolean"),
      "PROJECT_MODELS_CONTROL_STATE_INVALID",
    );
    invariant(
      result.state ===
        (result.joined
          ? "joined"
          : result.release_requested
            ? "released"
            : result.upstream_complete
              ? "upstream_complete"
              : result.request_token
                ? "claimed"
                : "armed"),
      "PROJECT_MODELS_CONTROL_STATE_ORDER_INVALID",
    );
  } else if (request.action === "release") {
    exact(result, ["arm_id", "request_token", "release_requested"]);
    invariant(
      result.arm_id === request.args.arm_id &&
        result.request_token === request.args.request_token &&
        result.release_requested === true,
      "PROJECT_MODELS_RELEASE_INVALID",
    );
  }
  return result;
}

const button = (scope: Page | Locator, name: string) =>
  scope.getByRole("button", { name, exact: true });
export async function loginProjectModels(page: Page, actor: Actor) {
  await page.goto("/login");
  await expect(page.locator("#login-email")).toBeVisible();
  try {
    await page.locator("#login-email").fill(actor.email);
    await page.locator("#login-password").fill(actor.password);
  } catch {
    throw new Error("PROJECT_MODELS_PRIVATE_LOGIN_INPUT_FAILED");
  }
  await button(page, "登录").click();
  await expect(button(page, "退出登录")).toBeEnabled();
}

export async function fillProjectModelsCredential(scope: Page | Locator) {
  const bytes = randomBytes(48);
  let value = bytes.toString("base64url");
  bytes.fill(0);
  try {
    await scope.getByLabel(/^新凭据材料(?:\s*\*)?$/).fill(value);
  } catch {
    throw new Error("PROJECT_MODELS_PRIVATE_CREDENTIAL_INPUT_FAILED");
  } finally {
    value = "";
  }
}

type OriginFact = {
  origin_token: string; original_request_token: string; operation: string;
  project_id: string; target_id: string | null; original_body_bytes: number;
  history: { family: "configuration" | "credential"; committed_rows: string; receipt?: Record<string, unknown> | null; result?: Record<string, unknown> | null };
  comparison_count: number; comparison: null | Record<string, unknown>;
};
type Snapshot = {
  project: { project_id: string; version: string; initialized: boolean; lifecycle: string };
  current: { providers: { id: string; present: boolean; version: string | null; credential_ref: string | null }[]; models: { id: string; present: boolean; version: string | null; provider_id: string | null }[]; credentials: { credential_id: string; metadata: null | { credential_id: string; purpose: string; version: string } }[] };
  history: { configuration: Record<string, string>; credential: Record<string, string> };
  reference_presence: { models: { id: string; present: boolean }[]; credentials: { credential_id: string; present: boolean }[] };
  origins: OriginFact[];
  fixture_only: { archive_recovery_applied: boolean; reference_fact_state: null | "present" | "absent"; rename_reuse_applied: boolean };
};
function validateSnapshot(value: unknown): Snapshot {
  const v = exact(value, ["project", "current", "history", "reference_presence", "origins", "fixture_only"]);
  const project = exact(v.project, ["project_id", "version", "initialized", "lifecycle"]);
  invariant(uuid(project.project_id) && version(project.version) && typeof project.initialized === "boolean" && ["active", "archiving", "archived", "deleting"].includes(String(project.lifecycle)), "PROJECT_MODELS_SNAPSHOT_PROJECT_INVALID");
  const current = exact(v.current, ["providers", "models", "credentials"]);
  for (const family of ["providers", "models", "credentials"] as const) {
    invariant(Array.isArray(current[family]), "PROJECT_MODELS_SNAPSHOT_ARRAY_INVALID");
    let previous = "";
    for (const entry of current[family] as unknown[]) {
      const row = exact(entry, family === "providers" ? ["id", "present", "version", "credential_ref"] : family === "models" ? ["id", "present", "version", "provider_id"] : ["credential_id", "metadata"]);
      const id = family === "credentials" ? row.credential_id : row.id;
      invariant(uuid(id) && id > previous, "PROJECT_MODELS_SNAPSHOT_IDS_INVALID"); previous = id;
      if (family === "credentials") {
        if (row.metadata !== null) {
          const metadata = exact(row.metadata, ["credential_id", "purpose", "version"]);
          invariant(metadata.credential_id === id && metadata.purpose === "model" && version(metadata.version), "PROJECT_MODELS_SNAPSHOT_METADATA_INVALID");
        }
      } else {
        const reference = family === "providers" ? row.credential_ref : row.provider_id;
        invariant(typeof row.present === "boolean" && (row.present ? version(row.version) && (family === "models" ? uuid(reference) : reference === null || uuid(reference)) : row.version === null && reference === null), "PROJECT_MODELS_SNAPSHOT_CURRENT_INVALID");
      }
    }
  }
  const history = exact(v.history, ["configuration", "credential"]);
  for (const family of ["configuration", "credential"] as const) {
    const row = exact(history[family], family === "configuration" ? ["committed_commands", "audit_records", "events"] : ["committed_commands", "audit_records"]);
    invariant(Object.values(row).every((n) => typeof n === "string" && /^(0|[1-9][0-9]*)$/.test(n)), "PROJECT_MODELS_SNAPSHOT_COUNTS_INVALID");
  }
  const references = exact(v.reference_presence, ["models", "credentials"]);
  for (const family of ["models", "credentials"] as const) {
    invariant(Array.isArray(references[family]), "PROJECT_MODELS_SNAPSHOT_REFERENCES_INVALID");
    let previous = "";
    for (const entry of references[family] as unknown[]) {
      const key = family === "models" ? "id" : "credential_id", row = exact(entry, [key, "present"]);
      invariant(uuid(row[key]) && String(row[key]) > previous && typeof row.present === "boolean", "PROJECT_MODELS_SNAPSHOT_REFERENCE_INVALID"); previous = String(row[key]);
    }
  }
  invariant(Array.isArray(v.origins) && v.origins.length <= 4, "PROJECT_MODELS_SNAPSHOT_ORIGINS_INVALID");
  const origins = new Set<string>();
  for (const entry of v.origins) {
    const row = exact(entry, ["origin_token", "original_request_token", "operation", "project_id", "target_id", "original_body_bytes", "history", "comparison_count", "comparison"]);
    invariant(requestToken(row.origin_token) && row.origin_token === row.original_request_token && !origins.has(row.origin_token) && row.project_id === project.project_id && (row.target_id === null || uuid(row.target_id)) && count(row.original_body_bytes) && Number(row.original_body_bytes) > 0 && count(row.comparison_count), "PROJECT_MODELS_SNAPSHOT_ORIGIN_INVALID"); origins.add(row.origin_token);
    const h = object(row.history), credential = h.family === "credential";
    exact(h, ["family", "committed_rows", credential ? "result" : "receipt"]);
    invariant((credential || h.family === "configuration") && ["0", "1"].includes(String(h.committed_rows)), "PROJECT_MODELS_SNAPSHOT_HISTORY_INVALID");
    const receipt = h[credential ? "result" : "receipt"];
    if (h.committed_rows === "0") invariant(receipt === null, "PROJECT_MODELS_SNAPSHOT_NO_RECEIPT_INVALID");
    else {
      const r = exact(receipt, credential ? ["credential_id", "purpose", "version", "deleted"] : ["kind", "resource_id", "version", "affected_references"]);
      invariant(version(r.version) && (credential ? uuid(r.credential_id) && r.purpose === "model" && typeof r.deleted === "boolean" : uuid(r.resource_id) && /^(provider|model)\.(create|update|delete)$/.test(String(r.kind)) && r.affected_references === "0"), "PROJECT_MODELS_SNAPSHOT_RECEIPT_INVALID");
    }
    if (row.comparison === null) invariant(row.comparison_count === 0, "PROJECT_MODELS_SNAPSHOT_COMPARISON_MISSING");
    else {
      const c = exact(row.comparison, ["request_token", "body_equal", "key_equal", "target_equal", "identity_equal", "method_equal", "original_body_bytes", "replay_body_bytes"]);
      invariant(Number(row.comparison_count) > 0 && requestToken(c.request_token) && ["body_equal", "key_equal", "target_equal", "identity_equal", "method_equal"].every((key) => typeof c[key] === "boolean") && count(c.original_body_bytes) && count(c.replay_body_bytes), "PROJECT_MODELS_SNAPSHOT_COMPARISON_INVALID");
    }
  }
  const auxiliary = exact(v.fixture_only, ["archive_recovery_applied", "reference_fact_state", "rename_reuse_applied"]);
  invariant(typeof auxiliary.archive_recovery_applied === "boolean" && typeof auxiliary.rename_reuse_applied === "boolean" && [null, "present", "absent"].includes(auxiliary.reference_fact_state as null), "PROJECT_MODELS_SNAPSHOT_AUXILIARY_INVALID");
  return value as Snapshot;
}
async function snapshot(project: ProjectKey) { return validateSnapshot(await projectModelsIPC({ action: "snapshot", args: { project } })); }
async function counts() {
  const value = await projectModelsIPC({ action: "counts", args: {} });
  exact(value, ["operations", "session", "server", "controls", "browser_eof", "schema_bodies", "client_bodies"]);
  invariant(value.browser_eof === null && value.schema_bodies === null && value.client_bodies === null, "PROJECT_MODELS_COUNTS_OBSERVER_INVALID");
  const attachment = JSON.parse(readFileSync(join(repository, "docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json"), "utf8"));
  invariant(Array.isArray(value.operations) && value.operations.length === 17, "PROJECT_MODELS_COUNTS_OPERATIONS_INVALID");
  value.operations.forEach((entry, index) => { const row = exact(entry, ["operation", "setup", "browser", "control", "upstream_complete", "handler_joined"]); invariant(row.operation === attachment.operations[index].operation && Object.entries(row).every(([key, n]) => key === "operation" || count(n)), "PROJECT_MODELS_COUNTS_ROW_INVALID"); });
  for (const [key, members] of [["session", ["setup", "browser", "control"]], ["server", ["started", "finished"]], ["controls", ["armed", "claimed", "held", "held_joined", "cut", "disconnected"]]] as const) invariant(Object.values(exact(value[key], members)).every(count), "PROJECT_MODELS_COUNTS_VALUES_INVALID");
  return value;
}
function operationCount(value: Record<string, unknown>, operation: string) {
  return Number((value.operations as Record<string, unknown>[]).find((row) => row.operation === operation)!.browser);
}
async function arm(operation: string, project: ProjectKey, target_id: string | null, effect: string, query: string | null = null) {
  const result = await projectModelsIPC({ action: "arm", args: { operation, project, target_id, effect, query } }); return String(result.arm_id);
}
async function control(id: string, ready: (value: Record<string, unknown>) => boolean) {
  let value: Record<string, unknown> = {};
  await expect.poll(async () => { value = await projectModelsIPC({ action: "control-state", args: { arm_id: id } }); return ready(value); }, { intervals: [30, 60, 100], timeout: 5_000 }).toBe(true);
  return value;
}
async function navigate(page: Page, target: string) {
  await page.evaluate((target) => {
    const old = history.state as { position?: number } | null;
    history.pushState({ ...old, back: location.pathname, current: target, forward: null, position: (old?.position ?? 0) + 1, replaced: false }, "", target);
    dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
  }, target);
}
async function openProject(page: Page, material: Material, key: ProjectKey, suffix = "model-providers", discardPrepared = false) {
  const project = material.projects[key]!;
  await navigate(page, `/${project.username}/${project.normalized_name}/settings/${suffix}`);
  if (discardPrepared) {
    const confirmation = page.getByRole("dialog", { name: "离开项目模型设置？", exact: true });
    await expect(confirmation).toBeVisible();
    await button(confirmation, "放弃并离开").click();
  }
  await expect(page).toHaveURL(new URL(`/${project.username}/${project.normalized_name}/settings/${suffix}`, process.env.AGENTEAM_AUTH_WEB_ORIGIN!).href);
  await expect(page.getByRole("heading", { name: suffix === "model-providers" ? "Providers" : "可用模型", level: 1, exact: true })).toBeVisible();
  // A route change may retire the cached Owner observation. Obtain the
  // fresh read through the actual leaf action before expecting its list.
  const leaf = page.locator(suffix === "model-providers" ? "section.project-providers" : "section.available-models");
  const reread = button(leaf, "重新读取项目");
  if (await reread.isVisible()) {
    await expect(reread).toBeEnabled();
    await reread.click();
    await expect(reread).toBeHidden();
  }
  if (suffix === "model-providers") await expect(button(page, "创建 Provider")).toBeEnabled();
  const endpoint = `/api/v1/projects/${project.id}/${suffix === "model-providers" ? "model-providers" : "available-chat-models"}`;
  await expect.poll(async () => (await nativeFacts(page)).some((fact) => fact.path === endpoint && fact.method === "GET" && fact.eof && fact.ended)).toBe(true);
}
async function newProvider(page: Page, name: string) {
  await button(page, "创建 Provider").click();
  const dialog = page.getByRole("dialog").filter({ has: page.locator("#project-provider-form") });
  await dialog.getByRole("textbox", { name: "Provider 名称", exact: true }).fill(name);
  await dialog.getByRole("textbox", { name: "Base URL", exact: true }).fill("https://model-ui.invalid/v1");
  return dialog;
}
async function strictReceipt(scope: Page | Locator): Promise<Record<string, unknown>> {
  const receipt = scope.getByLabel("严格执行回执", { exact: true }); await expect(receipt).toBeVisible();
  return object(JSON.parse((await receipt.textContent())!));
}
function durableDelta(before: Snapshot, after: Snapshot, configuration: number, credential: number) {
  for (const family of ["configuration", "credential"] as const) for (const key of Object.keys(before.history[family])) {
    invariant(BigInt(after.history[family][key]!) - BigInt(before.history[family][key]!) === BigInt(family === "configuration" ? configuration : credential), "PROJECT_MODELS_DURABLE_DELTA_INVALID");
  }
}
function originalReplay(value: Snapshot, token: string) {
  const origin = value.origins.find((o) => o.origin_token === token); invariant(origin && origin.history.committed_rows === "1" && origin.comparison_count === 1 && origin.comparison, "PROJECT_MODELS_ORIGINAL_HISTORY_INVALID");
  invariant(["body_equal", "key_equal", "target_equal", "identity_equal", "method_equal"].every((key) => origin.comparison![key] === true) && origin.comparison.original_body_bytes === origin.comparison.replay_body_bytes && origin.comparison.original_body_bytes === origin.original_body_bytes, "PROJECT_MODELS_ORIGINAL_COMPARISON_INVALID");
}
type NativeFact = { token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; cancelled: boolean; released: boolean; bytes: number; chunks: number[]; has_body: boolean; mutation_headers: boolean };
async function nativeFacts(page: Page): Promise<NativeFact[]> { return page.evaluate(() => (window as any).__projectModelsProbe.facts()); }
async function actualLoss(page: Page, control: Record<string, unknown>, maximumObservedBytes: 0 | 1) {
  invariant(control.joined === true && control.upstream_complete === true && control.safe_admitted === true && control.effect_applied === true && requestToken(control.request_token), "PROJECT_MODELS_CONTROLLED_LOSS_NOT_APPLIED");
  const facts = await nativeFacts(page);
  // A failed assertion must preserve its safe observations. The stream can
  // fail before exposing response headers, so retain every bounded mutation
  // attempt, including a null token; never retain input/header values.
  const attempts = facts.filter((fact) => fact.method !== "GET").map((fact) => {
    const row = exact(fact, ["token", "method", "path", "query", "status", "eof", "ended", "cancelled", "released", "bytes", "chunks", "has_body", "mutation_headers"]);
    invariant((row.token === null || requestToken(row.token)) && ["POST", "PUT", "DELETE"].includes(String(row.method)) && /^\/api\/v1\/projects\/[0-9a-f-]{36}\/(model-providers|models|model-credentials|model-commands\/lookup|model-credential-commands\/lookup)(?:\/[0-9a-f-]{36})?$/.test(String(row.path)) && row.query === "" && count(row.status) && Number(row.status) <= 599 && count(row.bytes) && Number(row.bytes) <= 8388608 && Array.isArray(row.chunks) && row.chunks.length <= 8388608 && row.chunks.every(count) && row.chunks.reduce((total, n) => total + n, 0) === row.bytes && ["eof", "ended", "cancelled", "released", "has_body", "mutation_headers"].every((key) => typeof row[key] === "boolean"), "PROJECT_MODELS_CONTROLLED_NATIVE_DIAGNOSTIC_REJECTED");
    return row;
  });
  invariant(attempts.length <= 512 && armToken(control.arm_id), "PROJECT_MODELS_CONTROLLED_NATIVE_DIAGNOSTIC_REJECTED");
  const diagnostic = JSON.stringify({ protocol, input_hash: inputHash, control, maximum_observed_bytes: maximumObservedBytes, mutation_attempts: attempts });
  invariant(Buffer.byteLength(diagnostic) <= 1048576, "PROJECT_MODELS_CONTROLLED_NATIVE_DIAGNOSTIC_REJECTED");
  writeFileSync(join(evidence, `controlled-loss-${control.arm_id}.json`), diagnostic, { mode: 0o600, flag: "wx" });
  const matches = facts.filter((fact) => fact.token === control.request_token);
  // A native errored stream may discard its queued cut byte before the actual
  // reader receives it. Keep the control's bound and require native failure
  // plus the real cancel/release tail, without equating writes with delivery.
  invariant(matches.length === 1 && matches[0]!.ended && !matches[0]!.eof && matches[0]!.cancelled && matches[0]!.released && matches[0]!.status === 200 && matches[0]!.bytes <= maximumObservedBytes, "PROJECT_MODELS_CONTROLLED_NATIVE_LOSS_NOT_OBSERVED");
}
async function verifyBodies(page: Page) {
  const facts = await nativeFacts(page); invariant(facts.length > 0 && facts.every((fact) => fact.ended), "PROJECT_MODELS_NATIVE_TAIL_INCOMPLETE");
  const sidecars = readdirSync(evidence).filter((name) => /^response-\d+\.json$/.test(name)).map((name) => ({ name, value: object(privateJSON(name, 65536, evidence)) }));
  const selected: Record<string, unknown>[] = [];
  for (const fact of facts) {
    if (!fact.eof) continue;
    invariant(fact.released, "PROJECT_MODELS_NATIVE_READER_NOT_RELEASED");
    const matches = sidecars.filter(({ value }) => value.request_token === fact.token && value.source === "browser"); invariant(matches.length === 1, "PROJECT_MODELS_BODY_CORRELATION_INVALID");
    const { name, value } = matches[0]!;
    exact(value, ["protocol", "sequence", "source_run", "input_hash", "source", "operation", "method", "endpoint", "query", "status", "content_type", "content_length", "request_id", "project_id", "resource_id", "request_token", "body_file", "body_sha256", "body_bytes", "transfer_kind", "body_stage"]);
    invariant(value.protocol === protocol && value.input_hash === inputHash && value.body_stage === "complete_formal_upstream" && ["forwarded", "hold"].includes(String(value.transfer_kind)) && value.method === fact.method && value.endpoint === fact.path && value.query === fact.query && value.status === fact.status && value.body_bytes === fact.bytes && /^[0-9a-f]{64}$/.test(String(value.body_sha256)) && value.body_file === `body-${value.body_sha256}.json`, "PROJECT_MODELS_SAME_BODY_METADATA_INVALID");
    const raw = readFileSync(join(evidence, String(value.body_file)));
    invariant(raw.length === value.body_bytes && createHash("sha256").update(raw).digest("hex") === value.body_sha256, "PROJECT_MODELS_SAME_BODY_BYTES_INVALID");
    const result = await page.evaluate((safe) => (window as any).__projectModelsProbe.verify(safe), { token: value.request_token, method: value.method, endpoint: value.endpoint, query: value.query, status: value.status, content_type: value.content_type, content_length: value.content_length, request_id: value.request_id, raw: [...raw] });
    invariant(result.native_eof === true && result.typed_client_ok === true, "PROJECT_MODELS_NATIVE_CLIENT_FAILED");
    selected.push({ sidecar: name, request_token: fact.token, browser_eof: true, bytes: raw.length, sha256: value.body_sha256 });
  }
  invariant(selected.length > 0, "PROJECT_MODELS_SAME_BODY_EMPTY");
  writeFileSync(join(evidence, "same-body-input.json"), JSON.stringify(selected), { mode: 0o600 });
  const child = spawnSync("python3", [join(repository, ".agent-state/model-ui-recovery/validate-same-body.py"), repository, evidence], { encoding: "utf8", timeout: 6_000, maxBuffer: 4096 });
  writeFileSync(join(evidence, "schema-validation.json"), JSON.stringify({ status: child.status, signal: child.signal, failed_to_run: !!child.error, count: /^\d+\s*$/.test(child.stdout ?? "") ? Number(child.stdout) : null }), { mode: 0o600 });
  invariant(child.status === 0 && Number(child.stdout) === selected.length, "PROJECT_MODELS_SAME_BODY_SCHEMA_FAILED");
  writeFileSync(join(evidence, "native-browser-observations.json"), JSON.stringify(facts), { mode: 0o600 });
  return { attempts: facts.length, complete_eof: facts.filter((fact) => fact.eof).length, incomplete: facts.filter((fact) => !fact.eof).length, typed_client_ok: selected.length, schema_ok: selected.length };
}
async function finish(page: Page, mode: Mode, checks: Record<string, boolean>, layouts = 0) {
  const browser = await verifyBodies(page); checks.safe_schema_client = true;
  const card = readFileSync(join(repository, "docs/development/work-items/d27-project-owner-model-settings-ui.md"), "utf8");
  const contract = JSON.parse(card.split('```json\n{\n  "files":')[1]!.split("\n```")[0]!.replace(/^/, '{\n  "files":'));
  exact(checks, contract.checks_by_mode[mode]); invariant(Object.values(checks).every((value) => value === true), "PROJECT_MODELS_REQUIRED_CHECK_MISSING");
  const server = await counts(); invariant(object(server.server).started === object(server.server).finished && object(server.controls).held === object(server.controls).held_joined, "PROJECT_MODELS_SERVER_TAIL_INCOMPLETE");
  const result = { protocol, input_hash: inputHash, completed: true, mode, checks, counts: { server, browser }, schema_bodies: browser.schema_ok, client_bodies: browser.typed_client_ok, layouts };
  const raw = JSON.stringify(result); invariant(Buffer.byteLength(raw) <= 65536, "PROJECT_MODELS_RESULT_TOO_LARGE");
  const file = join(directory, "project-models-result.json"); writeFileSync(file + ".tmp", raw, { mode: 0o600, flag: "wx" }); renameSync(file + ".tmp", file);
  await page.evaluate(() => (window as any).__projectModelsProbe.dispose());
}
test.beforeEach(async ({ page }) => {
  step("native-probe-installing");
  const source = readFileSync(join(repository, "output/ai/model-ui-recovery/client-probe/native-client-probe.js"), "utf8");
  await page.addInitScript({ content: source + "\nProjectModelsNativeProbe.install();" });
  step("native-probe-installed");
});
test.afterEach(async ({}, info) => {
  if (info.status === "passed") return;
  // Never persist Playwright's message/stack/call log: fill diagnostics can
  // contain private values. Retain only closed categories and our own numeric
  // source locations so a failed actual run remains diagnosable.
  const diagnosticSources = [fileURLToPath(import.meta.url), join(repository, ".agent-state/model-ui-recovery/read-and-pagination.ts"), join(repository, ".agent-state/model-ui-recovery/read-pagination-contract.ts")];
  const publicCodes = new Set(diagnosticSources.flatMap((path) => [...readFileSync(path, "utf8").matchAll(/(["'])(PROJECT_MODELS_[A-Z_]+)\1/g)].map((match) => match[2])));
  const errors = info.errors.slice(0, 8).map((error) => {
    const message = error.message ?? "", stack = error.stack ?? "";
    const candidate = /\b(PROJECT_MODELS_[A-Z_]+)\b/.exec(message)?.[1];
    const code = candidate && publicCodes.has(candidate) ? candidate : null;
    const call = /\b(locator\.(?:fill|click|inputValue)|page\.goto|expect\.poll)\b/.exec(message)?.[1] ?? null;
    const locations = [...stack.matchAll(/project-owner-models\.spec\.ts:(\d+):(\d+)/g)].slice(0, 8).map((match) => ({ line: Number(match[1]), column: Number(match[2]) }));
    return { category: code ? "closed-harness-error" : /timeout|timed out/i.test(message) ? "timeout" : message.includes("expect(") ? "assertion" : "other", code, call, locations };
  });
  writeFileSync(join(evidence, "browser-failure.json"), JSON.stringify({ protocol, input_hash: inputHash, mode: process.env.AGENTEAM_PROJECT_MODELS_WEB_CASE, status: info.status, errors }), { mode: 0o600 });
});

function step(name: string) {
  invariant(/^[a-z0-9-]+$/.test(name), "PROJECT_MODELS_STEP_INVALID");
  writeFileSync(join(evidence, "browser-step.json"), JSON.stringify({ mode: process.env.AGENTEAM_PROJECT_MODELS_WEB_CASE, step: name }), { mode: 0o600 });
}

test("[recovery] actual original configuration and credential requests", async ({ page }) => {
  step("material-reading");
  const material = readProjectModelsMaterial(), checks: Record<string, boolean> = {};
  invariant(material.mode === "recovery", "PROJECT_MODELS_CASE_MISMATCH");
  step("login");
  await loginProjectModels(page, material.actors.owner);
  step("configuration-loss");
  await openProject(page, material, "config_recovery");
  step("configuration-opened");
  const before = await snapshot("config_recovery");
  step("configuration-snapshot-read");
  let dialog = await newProvider(page, "Models Recovery Provider");
  step("configuration-form-filled");
  const createArm = await arm("createProjectModelProvider", "config_recovery", null, "after_complete_cut");
  step("configuration-loss-armed");
  await button(dialog, "保存 Provider").click();
  await expect(dialog.getByText(/结果尚未确认/)).toBeVisible();
  const lostCreate = await control(createArm, (value) => value.joined === true);
  await actualLoss(page, lostCreate, 1);
  const committed = await snapshot("config_recovery"); durableDelta(before, committed, 1, 0);
  const providerID = committed.current.providers.find((row) => row.present)!.id;
  const noImplicit = await counts();
  step("configuration-lookup");
  await button(dialog, "查证原请求").click();
  await expect.poll(async () => operationCount(await counts(), "lookupProjectModelConfiguration")).toBe(operationCount(noImplicit, "lookupProjectModelConfiguration") + 1);
  await expect(dialog.getByLabel("历史观察", { exact: true })).toBeVisible();
  await expect(dialog.getByText(/结果尚未确认/)).toBeVisible();
  await expect(dialog.getByLabel("严格执行回执", { exact: true })).toHaveCount(0);
  invariant(operationCount(await counts(), "createProjectModelProvider") === operationCount(noImplicit, "createProjectModelProvider"), "PROJECT_MODELS_LOOKUP_WROTE");
  durableDelta(committed, await snapshot("config_recovery"), 0, 0);
  step("configuration-replay");
  await button(dialog, "按原请求重放").click();
  invariant((await strictReceipt(dialog)).resource_id === providerID, "PROJECT_MODELS_CREATE_RECEIPT_MISMATCH");
  const replayed = await snapshot("config_recovery"); durableDelta(committed, replayed, 0, 0); originalReplay(replayed, String(lostCreate.origin_token));
  checks.configuration_response_loss = checks.lookup_observation_only = true;
  await button(dialog, "取消").click();
  await expect(dialog).toBeHidden();
  await button(page, "刷新 Providers").click();
  await button(page, `读取 Provider ${providerID}`).click();
  dialog = page.getByRole("dialog", { name: "Provider 详情与编辑", exact: true });
  step("delete-loss");
  await button(dialog, "删除 Provider").click();
  const deletion = page.getByRole("dialog", { name: "删除 Provider", exact: true });
  const deleteArm = await arm("deleteProjectModelProvider", "config_recovery", providerID, "after_complete_disconnect");
  await button(deletion, "确认删除").click(); await expect(deletion.getByText(/结果尚未确认/)).toBeVisible();
  const lostDelete = await control(deleteArm, (value) => value.joined === true);
  await actualLoss(page, lostDelete, 0);
  const deleted = await snapshot("config_recovery"); durableDelta(replayed, deleted, 1, 0);
  invariant(deleted.current.providers.find((row) => row.id === providerID)?.present === false, "PROJECT_MODELS_DELETED_CURRENT_STILL_PRESENT");
  await button(deletion, "查证原请求").click(); await expect(deletion.getByLabel("历史观察", { exact: true })).toBeVisible();
  await expect(deletion.getByText(/结果尚未确认/)).toBeVisible();
  step("delete-replay");
  await button(deletion, "按原请求重放").click(); await expect(deletion).toBeHidden();
  invariant((await strictReceipt(page)).resource_id === providerID, "PROJECT_MODELS_DELETE_RECEIPT_MISMATCH");
  const deleteReplay = await snapshot("config_recovery"); durableDelta(deleted, deleteReplay, 0, 0); originalReplay(deleteReplay, String(lostDelete.origin_token));
  checks.deleted_target_original_delete = true;

  step("credential-loss");
  await openProject(page, material, "credential_recovery");
  const credentialBefore = await snapshot("credential_recovery");
  await button(page, "创建凭据").click();
  const credential = page.getByRole("dialog").filter({ has: page.locator("#project-credential-form") });
  await fillProjectModelsCredential(credential);
  const credentialArm = await arm("createProjectModelCredential", "credential_recovery", null, "after_complete_disconnect");
  await button(credential, "创建凭据").click(); await expect(credential.getByText(/结果尚未确认/)).toBeVisible();
  invariant(await credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue() === "", "PROJECT_MODELS_MATERIAL_NOT_CLEARED");
  const lostCredential = await control(credentialArm, (value) => value.joined === true);
  await actualLoss(page, lostCredential, 0);
  const credentialCommitted = await snapshot("credential_recovery"); durableDelta(credentialBefore, credentialCommitted, 0, 1);
  const credentialCounts = await counts();
  await button(credential, "查证原请求").click(); await expect(credential.getByLabel("历史观察", { exact: true })).toBeVisible();
  await expect(credential.getByText(/结果尚未确认/)).toBeVisible();
  await expect(credential.getByLabel("严格执行回执", { exact: true })).toHaveCount(0);
  invariant(operationCount(await counts(), "createProjectModelCredential") === operationCount(credentialCounts, "createProjectModelCredential"), "PROJECT_MODELS_CREDENTIAL_LOOKUP_WROTE");
  step("credential-replay");
  await button(credential, "按原请求重放").click();
  const credentialReceipt = await strictReceipt(credential); invariant(credentialReceipt.credential_id === credentialCommitted.current.credentials[0]?.credential_id, "PROJECT_MODELS_CREDENTIAL_RECEIPT_MISMATCH");
  const credentialReplayed = await snapshot("credential_recovery"); durableDelta(credentialCommitted, credentialReplayed, 0, 0); originalReplay(credentialReplayed, String(lostCredential.origin_token));
  checks.credential_response_loss = checks.same_original_bytes_and_key = checks.unique_committed_facts = checks.no_implicit_writes_or_rekey = true;
  await button(credential.locator("footer"), "关闭").click(); await expect(credential).toBeHidden();

  step("owner-tail");
  await openProject(page, material, "main", "model-providers", true);
  const mainBefore = await snapshot("main"), seedID = mainBefore.current.providers[0]!.id;
  await button(page, `读取 Provider ${seedID}`).click();
  dialog = page.getByRole("dialog", { name: "Provider 详情与编辑", exact: true });
  await dialog.getByRole("textbox", { name: "Provider 名称", exact: true }).fill("Models Held Update");
  const heldArm = await arm("updateProjectModelProvider", "main", seedID, "after_complete_hold");
  await button(dialog, "保存 Provider").click();
  const held = await control(heldArm, (value) => value.held === true && value.upstream_complete === true);
  await expect(button(dialog, "取消")).toBeDisabled(); await expect(button(dialog, "按原请求重放")).toBeDisabled();
  invariant((await nativeFacts(page)).some((fact) => fact.path.endsWith(seedID) && fact.method === "PUT" && !fact.ended), "PROJECT_MODELS_NATIVE_OWNER_TAIL_NOT_OBSERVED");
  durableDelta(mainBefore, await snapshot("main"), 1, 0);
  await projectModelsIPC({ action: "release", args: { arm_id: heldArm, request_token: String(held.request_token) } });
  invariant((await strictReceipt(dialog)).resource_id === seedID, "PROJECT_MODELS_HELD_RECEIPT_MISMATCH");
  await control(heldArm, (value) => value.joined === true);
  invariant((await nativeFacts(page)).some((fact) => fact.token === held.request_token && fact.eof && fact.released && fact.ended), "PROJECT_MODELS_NATIVE_FINAL_RELEASE_MISSING");
  checks.actual_owner_tail = true;
  await button(dialog, "取消").click(); await expect(dialog).toBeHidden();
  const finalCounts = await counts();
  const finalControls = object(finalCounts.controls);
  invariant(finalControls.armed === 4 && finalControls.claimed === 4 && finalControls.held === 1 && finalControls.held_joined === 1 && finalControls.cut === 1 && finalControls.disconnected === 2, "PROJECT_MODELS_CONTROLLED_RECOVERY_COUNTS_INVALID");
  for (const [operation, expected] of [["createProjectModelProvider", 2], ["deleteProjectModelProvider", 2], ["createProjectModelCredential", 2], ["updateProjectModelProvider", 1], ["lookupProjectModelConfiguration", 2], ["lookupProjectModelCredential", 1]] as const) invariant(operationCount(finalCounts, operation) === expected, "PROJECT_MODELS_RECOVERY_OPERATION_COUNT_INVALID");
  step("same-body-finish");
  await finish(page, "recovery", checks);
});

test("[read] actual project model reads and cursor pagination", async ({ page }) => {
  step("material-reading");
  const material = readProjectModelsMaterial();
  invariant(material.mode === "read", "PROJECT_MODELS_CASE_MISMATCH");
  await runReadAndPagination(page, {
    material,
    loginOwner: (page) => loginProjectModels(page, material.actors.owner),
    openProject: (page, project, leaf, discardPrepared) => openProject(page, material, project, leaf, discardPrepared),
    fillCredential: fillProjectModelsCredential,
    nativeFacts,
    counts,
    finish: (page, checks) => finish(page, "read", checks),
    step,
  });
});
