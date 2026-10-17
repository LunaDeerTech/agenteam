import { isRecord, isVersion } from "../account/types";
import { AccountError, type AccountRequest } from "../account/transport";
import { isId, isInstant } from "./directory";
import { systemCommand, systemRequest } from "./requests";
import type { CursorPage } from "./useCursorList";
import type { AccountCommand } from "../account/session";

export function modelDisplayName(supplier: string, modelId: string): string {
  return `${supplier}/${modelId}`;
}

export const protocols = [
  "openai-compatible",
  "openai-chat-completions",
  "anthropic-messages",
  "openai-embeddings",
  "jina-rerank",
  "openai-images-generations",
] as const;
export type Protocol = (typeof protocols)[number];
export const modelTypes = [
  "chat",
  "embedding",
  "reranker",
  "image_generation",
] as const;
export type ModelType = (typeof modelTypes)[number];
export const editableProtocols: readonly Protocol[] = ["openai-compatible"];
export const protocolChoices = [
  { value: "openai-compatible", label: "OpenAI 兼容" },
];
export const modelTypeChoices = [
  { value: "chat", label: "对话" },
  { value: "embedding", label: "嵌入" },
  { value: "reranker", label: "重排" },
  { value: "image_generation", label: "图像生成" },
];
export interface ProviderInput {
  name: string;
  protocol: Protocol;
  base_url: string;
  enabled: boolean;
  credential_ref: string | null;
  options: Record<string, unknown>;
}
export interface Provider {
  id: string;
  input: ProviderInput;
  version: string;
  created_at: string;
  updated_at: string;
}
export interface ReasoningEffort {
  name: string;
  value: string;
}
export interface Capabilities {
  tool_calls: boolean;
  parallel_tool_calls: boolean;
  streaming: boolean;
  reasoning: boolean;
  input_modalities: string[];
  output_modalities: string[];
  reasoning_efforts: ReasoningEffort[];
  structured_output_modes: string[];
  context_length: string | null;
  max_output: string | null;
}
export interface ModelInput {
  name: string;
  provider_model_id: string;
  type: ModelType;
  enabled: boolean;
  parameters: Record<string, unknown>;
  request_overwrite: Record<string, unknown>;
  header_overwrite: Record<string, string>;
  capabilities: Capabilities;
}
export interface CatalogModel {
  id: string;
  provider_id: string;
  input: ModelInput;
  version: string;
  created_at: string;
  updated_at: string;
}
export const commandKinds = [
  "provider.create",
  "provider.update",
  "provider.delete",
  "model.create",
  "model.update",
  "model.delete",
  "model.selection.update",
] as const;
export type CommandKind = (typeof commandKinds)[number];
export interface CatalogReceipt {
  kind: CommandKind;
  resource_id: string;
  version: string;
  affected_references: string;
}
export interface CredentialMetadata {
  credential_id: string;
  purpose: "model";
  version: string;
}
export interface CredentialReceipt extends CredentialMetadata {
  deleted: boolean;
}
export interface CatalogLookup {
  found: boolean;
  receipt: CatalogReceipt | null;
}
export interface CredentialLookup {
  observed: boolean;
  result: CredentialReceipt | null;
}
export type CredentialIntent =
  | { kind: "create" }
  | {
      kind: "update" | "delete";
      credential_id: string;
      expected_version: string;
    };
export interface CatalogOperation<T> {
  command: AccountCommand<T>;
  observe: () => Promise<T | null>;
}
export const onlyKeys = (v: Record<string, unknown>, keys: readonly string[]) =>
  Object.keys(v).length === keys.length &&
  keys.every((key) => Object.hasOwn(v, key));
const text = (v: unknown, max: number) =>
  typeof v === "string" && v.length > 0 && [...v].length <= max;
export const isCount = (v: unknown): v is string =>
  typeof v === "string" &&
  /^(0|[1-9]\d{0,18})$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const strings = (v: unknown): v is string[] =>
  Array.isArray(v) &&
  v.every((x) => typeof x === "string") &&
  new Set(v).size === v.length;
export function isProviderInput(v: unknown): v is ProviderInput {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "name",
      "protocol",
      "base_url",
      "enabled",
      "credential_ref",
      "options",
    ]) &&
    text(v.name, 128) &&
    protocols.includes(v.protocol as Protocol) &&
    typeof v.base_url === "string" &&
    v.base_url.length <= 8192 &&
    typeof v.enabled === "boolean" &&
    (v.credential_ref === null || isId(v.credential_ref)) &&
    isRecord(v.options)
  );
}
export function isProvider(v: unknown): v is Provider {
  return (
    isRecord(v) &&
    onlyKeys(v, ["id", "input", "version", "created_at", "updated_at"]) &&
    isId(v.id) &&
    isProviderInput(v.input) &&
    isVersion(v.version) &&
    isInstant(v.created_at) &&
    isInstant(v.updated_at)
  );
}
export function isReasoningEfforts(v: unknown): v is ReasoningEffort[] {
  return (
    Array.isArray(v) &&
    v.length <= 32 &&
    v.every(
      (item) =>
        isRecord(item) &&
        onlyKeys(item, ["name", "value"]) &&
        typeof item.name === "string" &&
        [...item.name].length > 0 &&
        [...item.name].length <= 64 &&
        !/^\p{White_Space}|\p{White_Space}$/u.test(item.name) &&
        !/[\u0000-\u001f\u007f-\u009f]/.test(item.name) &&
        typeof item.value === "string" &&
        /^[A-Za-z0-9_.:-]{1,32}$/.test(item.value),
    ) &&
    new Set(v.map((item) => item.name)).size === v.length &&
    new Set(v.map((item) => item.value)).size === v.length
  );
}
export function isCapabilities(v: unknown): v is Capabilities {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "tool_calls",
      "parallel_tool_calls",
      "streaming",
      "reasoning",
      "input_modalities",
      "output_modalities",
      "reasoning_efforts",
      "structured_output_modes",
      "context_length",
      "max_output",
    ]) &&
    [v.tool_calls, v.parallel_tool_calls, v.streaming, v.reasoning].every(
      (x) => typeof x === "boolean",
    ) &&
    [v.input_modalities, v.output_modalities, v.structured_output_modes].every(
      strings,
    ) &&
    (v.input_modalities as string[]).every((value) =>
      ["text", "image", "file", "vector"].includes(value),
    ) &&
    (v.output_modalities as string[]).every((value) =>
      ["text", "image", "file", "vector"].includes(value),
    ) &&
    (v.structured_output_modes as string[]).every((value) =>
      ["text", "json_schema"].includes(value),
    ) &&
    isReasoningEfforts(v.reasoning_efforts) &&
    (!v.parallel_tool_calls || !!v.tool_calls) &&
    (!!v.reasoning || v.reasoning_efforts.length === 0) &&
    (v.context_length === null || isVersion(v.context_length)) &&
    (v.max_output === null || isVersion(v.max_output)) &&
    (v.context_length === null ||
      v.max_output === null ||
      BigInt(v.max_output as string) <= BigInt(v.context_length as string))
  );
}
export function isModelInput(v: unknown): v is ModelInput {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "name",
      "provider_model_id",
      "type",
      "enabled",
      "parameters",
      "request_overwrite",
      "header_overwrite",
      "capabilities",
    ]) &&
    text(v.name, 128) &&
    text(v.provider_model_id, 256) &&
    modelTypes.includes(v.type as ModelType) &&
    typeof v.enabled === "boolean" &&
    isRecord(v.parameters) &&
    isRecord(v.request_overwrite) &&
    isRecord(v.header_overwrite) &&
    Object.values(v.header_overwrite).every((x) => typeof x === "string") &&
    isCapabilities(v.capabilities)
  );
}
export function isCatalogModel(v: unknown): v is CatalogModel {
  return (
    isRecord(v) &&
    onlyKeys(v, [
      "id",
      "provider_id",
      "input",
      "version",
      "created_at",
      "updated_at",
    ]) &&
    isId(v.id) &&
    isId(v.provider_id) &&
    isModelInput(v.input) &&
    isVersion(v.version) &&
    isInstant(v.created_at) &&
    isInstant(v.updated_at)
  );
}
export function isCatalogReceipt(v: unknown): v is CatalogReceipt {
  return (
    isRecord(v) &&
    onlyKeys(v, ["kind", "resource_id", "version", "affected_references"]) &&
    commandKinds.includes(v.kind as CommandKind) &&
    isId(v.resource_id) &&
    isVersion(v.version) &&
    isCount(v.affected_references)
  );
}
export function isCredentialMetadata(v: unknown): v is CredentialMetadata {
  return (
    isRecord(v) &&
    onlyKeys(v, ["credential_id", "purpose", "version"]) &&
    isId(v.credential_id) &&
    v.purpose === "model" &&
    isVersion(v.version)
  );
}
export function isCredentialReceipt(v: unknown): v is CredentialReceipt {
  return (
    isRecord(v) &&
    onlyKeys(v, ["credential_id", "purpose", "version", "deleted"]) &&
    isId(v.credential_id) &&
    v.purpose === "model" &&
    isVersion(v.version) &&
    typeof v.deleted === "boolean"
  );
}
export function isCatalogLookup(v: unknown): v is CatalogLookup {
  return (
    isRecord(v) &&
    onlyKeys(v, ["found", "receipt"]) &&
    ((v.found === false && v.receipt === null) ||
      (v.found === true && isCatalogReceipt(v.receipt)))
  );
}
export function isCredentialLookup(v: unknown): v is CredentialLookup {
  return (
    isRecord(v) &&
    onlyKeys(v, ["observed", "result"]) &&
    ((v.observed === false && v.result === null) ||
      (v.observed === true && isCredentialReceipt(v.result)))
  );
}
function pageOf<T>(item: (v: unknown) => v is T, limit: number) {
  return (v: unknown): v is { items: T[]; next_cursor: string | null } =>
    isRecord(v) &&
    onlyKeys(v, ["items", "next_cursor"]) &&
    Array.isArray(v.items) &&
    v.items.length <= limit &&
    v.items.every(item) &&
    (v.next_cursor === null ||
      (typeof v.next_cursor === "string" &&
        v.next_cursor.length > 0 &&
        new TextEncoder().encode(v.next_cursor).length <= 8192));
}
async function list<T>(
  path: string,
  item: (v: unknown) => v is T,
  cursor: string | undefined,
  current: (() => boolean) | undefined,
  limit: number,
  provider_id?: string,
): Promise<CursorPage<T>> {
  const value = await systemRequest(
    path,
    {
      query: { cursor, limit, ...(provider_id ? { provider_id } : {}) },
      current,
    },
    pageOf(item, limit),
  );
  return {
    items: value.items,
    ...(value.next_cursor ? { next_cursor: value.next_cursor } : {}),
  };
}
export const listProviders = (
  cursor?: string,
  current?: () => boolean,
  limit = 10,
) => list("/system/model-providers", isProvider, cursor, current, limit);
export interface ModelProviderSummary {
  id: string;
  name: string;
  protocol: Protocol;
  enabled: boolean;
  version: string;
}
export interface CatalogModelOption extends CatalogModel {
  provider: ModelProviderSummary;
}
export function isCatalogModelOption(v: unknown): v is CatalogModelOption {
  if (!isRecord(v)) return false;
  const { provider, ...model } = v;
  return (
    isCatalogModel(model) &&
    isRecord(provider) &&
    onlyKeys(provider, ["id", "name", "protocol", "enabled", "version"]) &&
    provider.id === model.provider_id &&
    text(provider.name, 128) &&
    protocols.includes(provider.protocol as Protocol) &&
    typeof provider.enabled === "boolean" &&
    isVersion(provider.version)
  );
}
export const listModels = (
  provider: string | undefined,
  cursor?: string,
  current?: () => boolean,
  limit = 10,
  query?: string,
) => {
  const q = query?.trim().toLowerCase();
  return systemRequest(
    "/system/models",
    {
      current,
      query: {
        cursor,
        limit,
        ...(provider ? { provider_id: provider } : {}),
        ...(q ? { q } : {}),
      },
    },
    pageOf(isCatalogModelOption, limit),
  ).then((value) => ({
    items: value.items,
    ...(value.next_cursor ? { next_cursor: value.next_cursor } : {}),
  }));
};
export function getProvider(id: string, current?: () => boolean) {
  if (!isId(id)) throw new AccountError("NOT_FOUND", 404);
  return systemRequest(
    `/system/model-providers/${id}`,
    { current },
    (v): v is Provider => isProvider(v) && v.id === id,
  );
}
export function getModel(id: string, current?: () => boolean) {
  if (!isId(id)) throw new AccountError("NOT_FOUND", 404);
  return systemRequest(
    `/system/models/${id}`,
    { current },
    (v): v is CatalogModel => isCatalogModel(v) && v.id === id,
  );
}
export function getCredential(id: string, current?: () => boolean) {
  if (!isId(id)) throw new AccountError("NOT_FOUND", 404);
  return systemRequest(
    `/system/model-credentials/${id}`,
    { current },
    (v): v is CredentialMetadata =>
      isCredentialMetadata(v) && v.credential_id === id,
  );
}
export function catalogOperation(
  path: string,
  kind: CommandKind,
  body: unknown,
  method: AccountRequest["method"] = "POST",
  resourceId?: string,
): CatalogOperation<CatalogReceipt> {
  const expected =
    isRecord(body) && isVersion(body.expected_version)
      ? String(BigInt(body.expected_version) + 1n)
      : "1";
  const validate = (v: unknown): v is CatalogReceipt =>
    isCatalogReceipt(v) &&
    v.kind === kind &&
    v.version === expected &&
    (kind === "model.delete" || v.affected_references === "0") &&
    (!resourceId || v.resource_id === resourceId);
  const command = systemCommand(
    path,
    { method, body: JSON.stringify(body) },
    validate,
  );
  return {
    command,
    async observe() {
      const value = await systemRequest(
        "/system/model-commands/lookup",
        {
          method: "POST",
          key: command.key,
          body: JSON.stringify({ command: kind }),
          current: () => command.active,
        },
        isCatalogLookup,
      );
      if (value.receipt && !validate(value.receipt))
        throw new AccountError("INVALID_RESPONSE");
      return value.receipt;
    },
  };
}
export function credentialOperation(
  value: string | undefined,
  existing?: CredentialMetadata,
  remove = false,
): CatalogOperation<CredentialReceipt> {
  const intent: CredentialIntent = existing
    ? {
        kind: remove ? "delete" : "update",
        credential_id: existing.credential_id,
        expected_version: existing.version,
      }
    : { kind: "create" };
  const path =
    "/system/model-credentials" +
    (existing ? `/${existing.credential_id}` : "");
  const body = {
    ...(existing ? { expected_version: existing.version } : {}),
    ...(!remove ? { value } : {}),
  };
  const validate = (v: unknown): v is CredentialReceipt =>
    isCredentialReceipt(v) &&
    v.deleted === remove &&
    v.version === (existing ? String(BigInt(existing.version) + 1n) : "1") &&
    (!existing || v.credential_id === existing.credential_id);
  const command = systemCommand(
    path,
    {
      method: existing ? (remove ? "DELETE" : "PUT") : "POST",
      body: JSON.stringify(body),
    },
    validate,
  );
  return {
    command,
    async observe() {
      const found = await systemRequest(
        "/system/model-credential-commands/lookup",
        {
          method: "POST",
          key: command.key,
          body: JSON.stringify(intent),
          current: () => command.active,
        },
        isCredentialLookup,
      );
      if (found.result && !validate(found.result))
        throw new AccountError("INVALID_RESPONSE");
      return found.result;
    },
  };
}
export function providerOperation(input: ProviderInput, existing?: Provider) {
  return catalogOperation(
    "/system/model-providers" + (existing ? `/${existing.id}` : ""),
    existing ? "provider.update" : "provider.create",
    existing ? { expected_version: existing.version, input } : { input },
    existing ? "PUT" : "POST",
    existing?.id,
  );
}
export function modelOperation(
  providerId: string,
  input: ModelInput,
  existing?: CatalogModel,
) {
  return catalogOperation(
    "/system/models" + (existing ? `/${existing.id}` : ""),
    existing ? "model.update" : "model.create",
    existing
      ? { expected_version: existing.version, input }
      : { provider_id: providerId, input },
    existing ? "PUT" : "POST",
    existing?.id,
  );
}
export function catalogError(error: unknown) {
  if (!(error instanceof AccountError)) return "暂时无法完成操作，请重试。";
  return (
    (
      {
        VERSION_CONFLICT: "配置已变化，已保留草稿。请读取最新配置后再保存。",
        NOT_FOUND: "此对象已不存在，请返回模型接入。",
        RESOURCE_BUSY: "此对象仍被使用，请先处理关联项。",
        INVALID_ARGUMENT: "请检查填写内容。",
        FORBIDDEN: "当前没有系统管理权限。",
        INVALID_STATE: "当前配置不支持此操作，请重新读取。",
      } as Record<string, string>
    )[error.code] ?? "暂时无法完成操作，请重试。"
  );
}
