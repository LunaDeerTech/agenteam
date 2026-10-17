import { isVersion } from "../account/types";
import {
  isReasoningEfforts,
  isEmbeddingCapabilities,
  modelTypes,
  type ModelInput,
  type ModelType,
  type Protocol,
  type ReasoningEffort,
  type ReasoningToggleMode,
} from "./model-catalog";
export interface ModelDraft {
  notes: string;
  providerModelId: string;
  type: ModelType;
  enabled: boolean;
  toolCalls: boolean;
  parallelToolCalls: boolean;
  streaming: boolean;
  reasoning: boolean;
  inputModalities: string;
  reasoningEfforts: ReasoningEffort[];
  reasoningToggleMode: ReasoningToggleMode | "";
  structuredOutput: boolean;
  contextLength: string;
  maxOutput: string;
  embeddingDimensions: string;
  embeddingDimensionsMode: "fixed" | "request";
}
export function modelDraft(protocol: Protocol, input?: ModelInput): ModelDraft {
  const c = input?.capabilities;
  return {
    notes: input?.notes ?? "",
    providerModelId: input?.provider_model_id ?? "",
    type: input?.type ?? ("" as ModelType),
    enabled: input?.enabled ?? true,
    toolCalls: c?.tool_calls ?? false,
    parallelToolCalls: c?.parallel_tool_calls ?? false,
    streaming: c?.streaming ?? false,
    reasoning: c?.reasoning ?? false,
    inputModalities: c?.input_modalities.join(", ") ?? "text",
    reasoningEfforts:
      c?.reasoning_efforts.map((effort) => ({ ...effort })) ?? [],
    reasoningToggleMode: c?.reasoning_toggle_mode ?? "",
    structuredOutput:
      c?.structured_output_modes.includes("json_schema") ?? false,
    contextLength: c?.context_length ?? "",
    maxOutput: c?.max_output ?? "",
    embeddingDimensions: c?.embedding?.dimensions ?? "",
    embeddingDimensionsMode: c?.embedding?.dimensions_mode ?? "fixed",
  };
}
const split = (value: string) =>
  value.trim() ? value.split(",").map((v) => v.trim()) : [];
export function modelDraftInput(d: ModelDraft, protocol: Protocol): ModelInput {
  if (
    [...d.notes].length > 2000 ||
    new TextEncoder().encode(d.notes).length > 8000 ||
    d.notes.includes("\0")
  )
    throw new Error("备注最多 2000 个字符、8000 字节，不能包含空字符。");
  if (!d.providerModelId.trim() || [...d.providerModelId.trim()].length > 256)
    throw new Error("模型 ID 须为 1–256 个字符。");
  if (
    (protocol === "openai-compatible" && !modelTypes.includes(d.type)) ||
    (protocol === "openai-chat-completions" && d.type !== "chat") ||
    (protocol === "openai-embeddings" && d.type !== "embedding")
  )
    throw new Error("模型类型与供应商协议不匹配。");
  function values(raw: string, allowed?: string[]) {
    const values = split(raw);
    if (
      new Set(values).size !== values.length ||
      values.some(
        (v) =>
          !v ||
          v.length > 32 ||
          /\s/.test(v) ||
          (allowed && !allowed.includes(v)),
      )
    )
      throw new Error("能力列表须用英文逗号分隔，且不可重复或包含未知值。");
    return values;
  }
  const chat = d.type === "chat";
  const embedding = d.type === "embedding";
  const capacity = chat || embedding;
  const embeddingConfiguration = embedding
    ? {
        dimensions: d.embeddingDimensions,
        dimensions_mode: d.embeddingDimensionsMode,
      }
    : null;
  if (embedding && !isEmbeddingCapabilities(embeddingConfiguration))
    throw new Error("向量维度须为 1–4096 的整数，并选择有效的维度模式。");
  const declaredInput = chat
    ? values(d.inputModalities, ["text", "image", "file"])
    : [];
  const input = ["text", ...declaredInput.filter((value) => value !== "text")];
  const output = chat
    ? ["text"]
    : embedding
      ? ["vector"]
      : d.type === "image_generation"
        ? ["image"]
        : [];
  if (chat && d.parallelToolCalls && !d.toolCalls)
    throw new Error("并行工具调用需要同时启用工具调用。");
  const efforts =
    chat && d.reasoning
      ? d.reasoningEfforts.map((effort) => ({
          name: effort.name.replace(
            /^\p{White_Space}+|\p{White_Space}+$/gu,
            "",
          ),
          value: effort.value,
        }))
      : [];
  if (!isReasoningEfforts(efforts))
    throw new Error(
      "思考等级最多32项，名称须为1–64个字符，值须为1–32位字母、数字或 - _ . :；名称和值均不可重复。",
    );
  for (const value of [
    capacity ? d.contextLength : "",
    chat ? d.maxOutput : "",
  ])
    if (value && !isVersion(value))
      throw new Error("容量须为正整数，未知时留空。");
  if (
    chat &&
    d.contextLength &&
    d.maxOutput &&
    BigInt(d.maxOutput) > BigInt(d.contextLength)
  )
    throw new Error("最大输出不能超过上下文长度。");
  return {
    notes: d.notes,
    provider_model_id: d.providerModelId.trim(),
    type: d.type,
    enabled: d.enabled,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: chat && d.toolCalls,
      parallel_tool_calls: chat && d.parallelToolCalls,
      streaming: chat && d.streaming,
      reasoning: chat && d.reasoning,
      input_modalities: input,
      output_modalities: output,
      reasoning_efforts: efforts,
      reasoning_toggle_mode:
        chat && d.reasoning && !efforts.length
          ? d.reasoningToggleMode || null
          : null,
      structured_output_modes: !chat
        ? []
        : d.structuredOutput
          ? ["text", "json_schema"]
          : ["text"],
      context_length: capacity ? d.contextLength || null : null,
      max_output: chat ? d.maxOutput || null : null,
      embedding: embeddingConfiguration,
    },
  };
}
