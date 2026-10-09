import { expect, type Locator, type Page } from "@playwright/test";
import { randomBytes } from "node:crypto";
import {
  closeSync,
  constants,
  existsSync,
  fstatSync,
  openSync,
  readSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";

// Rebuilt from the formal project-owner-models.v1 contract. Runtime fixtures
// supply all IDs and current facts; this module contains no business stubs.
const protocol = "project-owner-models.v1";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const inputHash = process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH!;
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
function privateJSON(name: string, limit: number): unknown {
  invariant(/^[a-z0-9.-]+$/.test(name), "PROJECT_MODELS_PRIVATE_NAME_INVALID");
  const fd = openSync(
    join(directory, name),
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
    await scope.getByLabel("新凭据材料", { exact: true }).fill(value);
  } catch {
    throw new Error("PROJECT_MODELS_PRIVATE_CREDENTIAL_INPUT_FAILED");
  } finally {
    value = "";
  }
}
