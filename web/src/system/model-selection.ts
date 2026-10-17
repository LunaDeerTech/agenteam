import { isRecord, isVersion } from "../account/types";
import { AccountError } from "../account/transport";
import { isId } from "./directory";
import { systemCommand, systemRequest } from "./requests";
import {
  isCatalogLookup,
  isCatalogReceipt,
  onlyKeys,
  type CatalogModel,
  type CatalogOperation,
  type CatalogReceipt,
  type Provider,
} from "./model-catalog";

export const platformPurposes = [
  "embedding",
  "memory",
  "reranker",
  "image",
] as const;
export type PlatformPurpose = (typeof platformPurposes)[number];
export type ModelPurpose = PlatformPurpose | "meeting_summary";
export const purposeLabels: Record<ModelPurpose, string> = {
  embedding: "向量模型",
  memory: "记忆模型",
  reranker: "重排模型",
  image: "图像模型",
  meeting_summary: "会议摘要模型",
};
export interface SelectionValues {
  embedding: string;
  memory: string;
  reranker: string | null;
  image: string | null;
}
export interface ModelSelection {
  id: string;
  version: string;
  configured: SelectionValues | null;
}
export interface MeetingSummarySelection {
  id: string;
  version: string;
  model: string | null;
}
export type SelectionInput = SelectionValues & {
  id: string;
  expected_version: string;
};
export interface MeetingSummaryInput {
  id: string;
  expected_version: string;
  model: string;
}
export function isSelectionValues(value: unknown): value is SelectionValues {
  return (
    isRecord(value) &&
    onlyKeys(value, platformPurposes) &&
    isId(value.embedding) &&
    isId(value.memory) &&
    (value.reranker === null || isId(value.reranker)) &&
    (value.image === null || isId(value.image))
  );
}
export function isModelSelection(value: unknown): value is ModelSelection {
  return (
    isRecord(value) &&
    onlyKeys(value, ["id", "version", "configured"]) &&
    isId(value.id) &&
    isVersion(value.version) &&
    (value.configured === null || isSelectionValues(value.configured))
  );
}
export function isMeetingSummarySelection(
  value: unknown,
): value is MeetingSummarySelection {
  return (
    isRecord(value) &&
    onlyKeys(value, ["id", "version", "model"]) &&
    isId(value.id) &&
    isVersion(value.version) &&
    (value.model === null || isId(value.model))
  );
}
export const getModelSelection = (current?: () => boolean) =>
  systemRequest("/system/model-selection", { current }, isModelSelection);
export const getMeetingSummarySelection = (current?: () => boolean) =>
  systemRequest(
    "/system/model-selection/meeting-summary",
    { current },
    isMeetingSummarySelection,
  );

function selectionOperation(
  path: string,
  input: SelectionInput | MeetingSummaryInput,
): CatalogOperation<CatalogReceipt> {
  if (
    !isId(input.id) ||
    !isVersion(input.expected_version) ||
    BigInt(input.expected_version) === 9223372036854775807n
  )
    throw new AccountError("INVALID_ARGUMENT", 400);
  const nextVersion = String(BigInt(input.expected_version) + 1n);
  const validate = (value: unknown): value is CatalogReceipt =>
    isCatalogReceipt(value) &&
    value.kind === "model.selection.update" &&
    value.resource_id === input.id &&
    value.version === nextVersion &&
    value.affected_references === "0";
  const command = systemCommand(
    path,
    { method: "PUT", body: JSON.stringify(input) },
    validate,
  );
  return {
    command,
    async observe() {
      const result = await systemRequest(
        "/system/model-commands/lookup",
        {
          method: "POST",
          key: command.key,
          body: JSON.stringify({ command: "model.selection.update" }),
          current: () => command.active,
        },
        isCatalogLookup,
      );
      if (result.receipt && !validate(result.receipt))
        throw new AccountError("INVALID_RESPONSE");
      return result.receipt;
    },
  };
}
export const saveModelSelection = (input: SelectionInput) =>
  selectionOperation("/system/model-selection", input);
export const saveMeetingSummarySelection = (input: MeetingSummaryInput) =>
  selectionOperation("/system/model-selection/meeting-summary", input);

/** Mirrors the selection rules; the server rechecks current model/provider state on save. */
export function modelPurposeIssue(
  purpose: ModelPurpose,
  model: CatalogModel,
  provider: Provider,
  retainExisting = false,
): string | undefined {
  if (model.provider_id !== provider.id) return "供应商不匹配";
  if (!provider.input.enabled) return "供应商已停用";
  if (!model.input.enabled) return "模型已停用";
  const wanted = {
    embedding: "embedding",
    memory: "chat",
    reranker: "reranker",
    image: "image_generation",
    meeting_summary: "chat",
  };
  if (model.input.type !== wanted[purpose]) return "模型类型不适用于此用途";
  const supported =
    purpose === "embedding"
      ? provider.input.protocol === "openai-compatible"
      : (purpose === "memory" || purpose === "meeting_summary") &&
        provider.input.protocol === "openai-compatible";
  if (!supported && !retainExisting)
    return "当前协议仅支持查看现有用途，不能新增选择";
  if (
    purpose === "embedding" &&
    (model.input.capabilities.input_modalities.length !== 1 ||
      model.input.capabilities.input_modalities[0] !== "text" ||
      model.input.capabilities.output_modalities.length !== 1 ||
      model.input.capabilities.output_modalities[0] !== "vector")
  )
    return "向量模型需要 text 输入与 vector 输出能力";
  if (
    purpose === "memory" &&
    !model.input.capabilities.structured_output_modes.includes("json_schema")
  )
    return "记忆模型需要 json_schema 能力";
  return undefined;
}
