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
  type ModelProviderSummary,
} from "./model-catalog";

export const platformPurposes = [
  "embedding",
  "summary",
  "reranker",
  "image",
] as const;
export type PlatformPurpose = (typeof platformPurposes)[number];
export type ModelPurpose = PlatformPurpose;
export const purposeLabels: Record<ModelPurpose, string> = {
  embedding: "嵌入模型（Embedding）",
  summary: "总结模型（Chat）",
  reranker: "重排模型（Rerank）",
  image: "图像生成模型（Image Generation）",
};
export const purposeDescriptions: Record<ModelPurpose, string> = {
  embedding: "将知识库和记忆文本转换为向量，用于语义检索；需文本嵌入模型。",
  summary: "提取整理记忆并生成会议摘要与标题；需支持 JSON Schema 的对话模型。",
  reranker: "预留知识与记忆检索结果的相关性重排；需重排模型，当前尚未接入。",
  image: "预留文本生成图像能力；需图像生成模型，当前尚未接入业务调用。",
};
export interface SelectionValues {
  embedding: string;
  summary: string;
  reranker: string | null;
  image: string | null;
}
export interface ModelSelection {
  id: string;
  version: string;
  configured: SelectionValues | null;
}
export type SelectionInput = SelectionValues & {
  id: string;
  expected_version: string;
};
export function isSelectionValues(value: unknown): value is SelectionValues {
  return (
    isRecord(value) &&
    onlyKeys(value, platformPurposes) &&
    isId(value.embedding) &&
    isId(value.summary) &&
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
export const getModelSelection = (current?: () => boolean) =>
  systemRequest("/system/model-selection", { current }, isModelSelection);
function selectionOperation(
  path: string,
  input: SelectionInput,
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
/** Mirrors the selection rules; the server rechecks current model/provider state on save. */
export function modelPurposeIssue(
  purpose: ModelPurpose,
  model: CatalogModel,
  provider: Provider | ModelProviderSummary,
  retainExisting = false,
): string | undefined {
  const providerInput = "input" in provider ? provider.input : provider;
  if (model.provider_id !== provider.id) return "供应商不匹配";
  if (!providerInput.enabled) return "供应商已停用";
  if (!model.input.enabled) return "模型已停用";
  const wanted = {
    embedding: "embedding",
    summary: "chat",
    reranker: "reranker",
    image: "image_generation",
  };
  if (model.input.type !== wanted[purpose]) return "模型类型不适用于此用途";
  const supported = providerInput.protocol === "openai-compatible";
  if (!supported && !retainExisting)
    return "当前协议仅支持查看现有用途，不能新增选择";
  if (
    purpose === "embedding" &&
    (model.input.capabilities.input_modalities.length !== 1 ||
      model.input.capabilities.input_modalities[0] !== "text" ||
      model.input.capabilities.output_modalities.length !== 1 ||
      model.input.capabilities.output_modalities[0] !== "vector")
  )
    return "嵌入模型需要 text 输入与 vector 输出能力";
  if (
    purpose === "summary" &&
    !model.input.capabilities.structured_output_modes.includes("json_schema")
  )
    return "总结模型需要 json_schema 能力";
  if (purpose === "reranker" || purpose === "image") {
    const c = model.input.capabilities;
    const expectedOutput = purpose === "image" ? ["image"] : [];
    if (
      c.input_modalities.length !== 1 ||
      c.input_modalities[0] !== "text" ||
      c.output_modalities.length !== expectedOutput.length ||
      c.output_modalities.some(
        (value, index) => value !== expectedOutput[index],
      ) ||
      c.tool_calls ||
      c.parallel_tool_calls ||
      c.streaming ||
      c.reasoning ||
      c.reasoning_efforts.length ||
      c.structured_output_modes.length ||
      c.context_length !== null ||
      c.max_output !== null
    )
      return purpose === "reranker"
        ? "重排模型需要查询与文档排序能力"
        : "图像生成模型需要文本生成图像能力";
  }
  return undefined;
}
