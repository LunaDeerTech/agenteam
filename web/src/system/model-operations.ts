import { isRecord, isVersion } from "../account/types";
import { AccountError } from "../account/transport";
import { isId, isInstant } from "./directory";
import {
  onlyKeys,
  protocols,
  modelTypes,
  type Protocol,
  type ModelType,
  type Provider,
  type CatalogModel,
} from "./model-catalog";
import { systemCommand, systemRequest } from "./requests";
import type { AccountCommand } from "../account/session";
import {
  validateModelTestInput,
  type ModelTestInput,
} from "./model-test-input";
export interface NetworkCommand<T> extends AccountCommand<T> {
  cancel: () => void;
}
function networkCommand<T>(
  path: string,
  body: unknown,
  validate: (v: unknown) => v is T,
  timeoutMs?: 130000,
): NetworkCommand<T> {
  let controller = new AbortController();
  const command = systemCommand(
    path,
    {
      method: "POST",
      body: JSON.stringify(body),
      modelOperationSignal: () => controller.signal,
      modelOperationTimeoutMs: timeoutMs,
    },
    validate,
  );
  return {
    get key() {
      return command.key;
    },
    get active() {
      return command.active;
    },
    get confirmed() {
      return command.confirmed;
    },
    run(callback) {
      controller = new AbortController();
      return command.run(callback);
    },
    cancel() {
      controller.abort();
    },
    release() {
      controller.abort();
      command.release();
    },
  };
}
export const operationReasons = [
  "unsupported_protocol",
  "configuration_changed",
  "credential_unavailable",
  "policy_rejected",
  "network_failed",
  "tls_failed",
  "timeout",
  "cancelled",
  "upstream_rejected",
  "invalid_response",
  "response_limit",
  "interrupted",
] as const;
export type OperationReason = (typeof operationReasons)[number];
export interface ProtocolCapability {
  protocol: Protocol;
  model_types: ModelType[];
  discovery: boolean;
  test: boolean;
}
export interface OperationResult {
  operation_id: string;
  provider_id: string;
  provider_version: string;
  outcome: "succeeded" | "failed" | "unknown";
  reason: OperationReason | null;
  duration_ms: number;
  created_at: string;
}
export interface DiscoveryResult extends OperationResult {
  items: { provider_model_id: string }[];
}
export interface TestResult extends OperationResult {
  model_id: string;
  model_version: string;
  answer_text: string | null;
  answer_truncated: boolean;
}
function common(v: Record<string, unknown>, maximumDuration: number) {
  return (
    isId(v.operation_id) &&
    isId(v.provider_id) &&
    isVersion(v.provider_version) &&
    ["succeeded", "failed", "unknown"].includes(String(v.outcome)) &&
    (v.outcome === "succeeded"
      ? v.reason === null
      : operationReasons.includes(v.reason as OperationReason)) &&
    Number.isInteger(v.duration_ms) &&
    Number(v.duration_ms) >= 0 &&
    Number(v.duration_ms) <= maximumDuration &&
    isInstant(v.created_at)
  );
}
export function isDiscoveryResult(v: unknown): v is DiscoveryResult {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "operation_id",
      "provider_id",
      "provider_version",
      "outcome",
      "reason",
      "items",
      "duration_ms",
      "created_at",
    ]) &&
    common(v, 20000) &&
    Array.isArray(v.items) &&
    v.items.length <= 1000 &&
    v.items.every(
      (i) =>
        isRecord(i) &&
        onlyKeys(i, ["provider_model_id"]) &&
        typeof i.provider_model_id === "string" &&
        i.provider_model_id.length > 0 &&
        [...i.provider_model_id].length <= 256,
    ) &&
    new Set(v.items.map((i) => i.provider_model_id)).size === v.items.length &&
    (v.outcome === "succeeded" || v.items.length === 0)
  );
}
export function isTestResult(v: unknown): v is TestResult {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "operation_id",
      "provider_id",
      "provider_version",
      "model_id",
      "model_version",
      "outcome",
      "reason",
      "duration_ms",
      "created_at",
      "answer_text",
      "answer_truncated",
    ]) &&
    common(v, 120000) &&
    isId(v.model_id) &&
    isVersion(v.model_version) &&
    typeof v.answer_truncated === "boolean" &&
    (v.answer_text === null
      ? v.answer_truncated === false
      : typeof v.answer_text === "string" &&
        new TextEncoder().encode(v.answer_text).length <= 8192) &&
    (v.outcome === "succeeded" ||
      (v.answer_text === null && v.answer_truncated === false))
  );
}
export function getModelCapabilities(current?: () => boolean) {
  return systemRequest(
    "/system/model-capabilities",
    { current },
    (v): v is { protocols: ProtocolCapability[] } =>
      isRecord(v) &&
      onlyKeys(v, ["protocols"]) &&
      Array.isArray(v.protocols) &&
      v.protocols.length <= protocols.length &&
      v.protocols.every(
        (p) =>
          isRecord(p) &&
          onlyKeys(p, ["protocol", "model_types", "discovery", "test"]) &&
          protocols.includes(p.protocol as Protocol) &&
          Array.isArray(p.model_types) &&
          p.model_types.length > 0 &&
          p.model_types.every((t) => modelTypes.includes(t as ModelType)) &&
          new Set(p.model_types).size === p.model_types.length &&
          typeof p.discovery === "boolean" &&
          typeof p.test === "boolean",
      ) &&
      new Set(v.protocols.map((p) => p.protocol)).size === v.protocols.length,
  );
}
export function discoveryCommand(provider: Provider) {
  return networkCommand(
    `/system/model-providers/${provider.id}/discovery`,
    { expected_version: provider.version },
    (v): v is DiscoveryResult =>
      isDiscoveryResult(v) &&
      v.provider_id === provider.id &&
      v.provider_version === provider.version,
  );
}
export function modelTestCommand(
  provider: Provider,
  model: CatalogModel,
  input?: ModelTestInput,
) {
  if (input && validateModelTestInput(input, model))
    throw new AccountError("INVALID_ARGUMENT");
  const extended =
    model.input.type === "image_generation" ||
    (model.input.type === "chat" && !!input?.attachments.length);
  const body = {
    expected_version: model.version,
    provider_version: provider.version,
    ...(input ? { input } : {}),
  };
  if (new TextEncoder().encode(JSON.stringify(body)).length > 16 * 1024 * 1024)
    throw new AccountError("INVALID_ARGUMENT");
  return networkCommand(
    `/system/models/${model.id}/test`,
    body,
    (v): v is TestResult =>
      isTestResult(v) &&
      v.provider_id === provider.id &&
      v.provider_version === provider.version &&
      v.model_id === model.id &&
      v.model_version === model.version &&
      v.duration_ms <= (extended ? 120000 : 20000) &&
      (model.input.type === "chat" && v.outcome === "succeeded"
        ? typeof v.answer_text === "string"
        : v.answer_text === null && v.answer_truncated === false),
    extended ? 130000 : undefined,
  );
}
export const reasonLabels: Record<OperationReason, string> = {
  unsupported_protocol: "当前协议不支持此操作。",
  configuration_changed: "配置已变化，请读取最新版本。",
  credential_unavailable: "凭据不可用，请检查供应商配置。",
  policy_rejected: "出站规则拒绝了此请求。",
  network_failed: "连接供应商失败。",
  tls_failed: "供应商 TLS 验证失败。",
  timeout: "供应商请求超时。",
  cancelled: "操作已取消；无法据此判断上游是否已执行。",
  upstream_rejected: "供应商拒绝了请求。",
  invalid_response: "供应商响应格式无效。",
  response_limit: "供应商响应超过限制。",
  interrupted: "操作中断，结果无法确认。",
};
export interface DeletionImpact {
  model_id: string;
  version: string;
  reference_count: string;
  reference_groups: { owner_kind: string; role: string; count: string }[];
  replacement_requirement: "none" | "optional" | "required";
  delete_blocker: null | "reference_adapter_unbound";
}
export async function getDeletionImpact(
  model: CatalogModel,
  current?: () => boolean,
) {
  const result = await systemRequest(
    `/system/models/${model.id}/deletion-impact`,
    { current },
    (v): v is DeletionImpact =>
      isRecord(v) &&
      onlyKeys(v, [
        "model_id",
        "version",
        "reference_count",
        "reference_groups",
        "replacement_requirement",
        "delete_blocker",
      ]) &&
      v.model_id === model.id &&
      isVersion(v.version) &&
      typeof v.reference_count === "string" &&
      /^(0|[1-9]\d{0,4})$/.test(v.reference_count) &&
      Number(v.reference_count) <= 10000 &&
      Array.isArray(v.reference_groups) &&
      v.reference_groups.length <= 8 &&
      v.reference_groups.every(
        (g) =>
          isRecord(g) &&
          onlyKeys(g, ["owner_kind", "role", "count"]) &&
          ["agent", "platform_selector", "project_summary"].includes(
            String(g.owner_kind),
          ) &&
          [
            "agent_model",
            "approval_model",
            "embedding",
            "image",
            "summary",
            "reranker",
            "meeting_summary",
          ].includes(String(g.role)) &&
          typeof g.count === "string" &&
          /^[1-9]\d{0,4}$/.test(g.count),
      ) &&
      ["none", "optional", "required"].includes(
        String(v.replacement_requirement),
      ) &&
      (v.delete_blocker === null ||
        v.delete_blocker === "reference_adapter_unbound"),
  );
  if (result.version !== model.version)
    throw new AccountError("VERSION_CONFLICT", 409);
  const keys = result.reference_groups.map((g) => `${g.owner_kind}:${g.role}`);
  const validRoles: Record<string, string[]> = {
    agent: ["agent_model", "approval_model"],
    platform_selector: ["embedding", "image", "summary", "reranker"],
    project_summary: ["meeting_summary"],
  };
  const blocked = result.reference_groups.some(
    (g) => g.owner_kind !== "platform_selector",
  );
  const required =
    blocked ||
    result.reference_groups.some((g) =>
      ["embedding", "summary", "meeting_summary"].includes(g.role),
    );
  const requirement =
    result.reference_count === "0"
      ? "none"
      : required
        ? "required"
        : "optional";
  if (
    new Set(keys).size !== keys.length ||
    keys.some((key, index) => index > 0 && key <= keys[index - 1]!) ||
    result.reference_groups.some(
      (g) => !validRoles[g.owner_kind]?.includes(g.role),
    ) ||
    result.reference_groups.reduce((sum, g) => sum + Number(g.count), 0) !==
      Number(result.reference_count) ||
    result.replacement_requirement !== requirement ||
    result.delete_blocker !== (blocked ? "reference_adapter_unbound" : null)
  )
    throw new AccountError("INVALID_RESPONSE");
  return result;
}
