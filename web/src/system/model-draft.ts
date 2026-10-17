import { isRecord, isVersion } from "../account/types";
import {
  type ModelInput,
  type ModelType,
  type Protocol,
} from "./model-catalog";
export interface ModelDraft {
  name: string;
  providerModelId: string;
  type: ModelType;
  enabled: boolean;
  toolCalls: boolean;
  parallelToolCalls: boolean;
  streaming: boolean;
  reasoning: boolean;
  inputModalities: string;
  outputModalities: string;
  reasoningEfforts: string;
  structuredOutputModes: string;
  contextLength: string;
  maxOutput: string;
  parameters: string;
  requestOverwrite: string;
  headerOverwrite: string;
}
export function modelDraft(protocol: Protocol, input?: ModelInput): ModelDraft {
  const c = input?.capabilities;
  return {
    name: input?.name ?? "",
    providerModelId: input?.provider_model_id ?? "",
    type: input?.type ?? ("" as ModelType),
    enabled: input?.enabled ?? true,
    toolCalls: c?.tool_calls ?? false,
    parallelToolCalls: c?.parallel_tool_calls ?? false,
    streaming: c?.streaming ?? false,
    reasoning: c?.reasoning ?? false,
    inputModalities: c?.input_modalities.join(", ") ?? "",
    outputModalities: c?.output_modalities.join(", ") ?? "",
    reasoningEfforts: c?.reasoning_efforts.join(", ") ?? "",
    structuredOutputModes: c?.structured_output_modes.join(", ") ?? "",
    contextLength: c?.context_length ?? "",
    maxOutput: c?.max_output ?? "",
    parameters: JSON.stringify(input?.parameters ?? {}, null, 2),
    requestOverwrite: JSON.stringify(input?.request_overwrite ?? {}, null, 2),
    headerOverwrite: JSON.stringify(input?.header_overwrite ?? {}, null, 2),
  };
}
const split = (value: string) =>
  value.trim() ? value.split(",").map((v) => v.trim()) : [];
export function modelDraftInput(d: ModelDraft, protocol: Protocol): ModelInput {
  if (!d.name.trim() || [...d.name.trim()].length > 128)
    throw new Error("模型名称须为 1–128 个字符。");
  if (!d.providerModelId.trim() || [...d.providerModelId.trim()].length > 256)
    throw new Error("模型 ID 须为 1–256 个字符。");
  if (
    (protocol === "openai-compatible" &&
      !["chat", "embedding"].includes(d.type)) ||
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
  const input = values(d.inputModalities, ["text", "image", "file", "vector"]);
  const output = values(d.outputModalities, [
    "text",
    "image",
    "file",
    "vector",
  ]);
  if (!input.length || !output.length)
    throw new Error("请选择模型的输入和输出模态。");
  if (d.parallelToolCalls && !d.toolCalls)
    throw new Error("并行工具调用需要同时启用工具调用。");
  const efforts = values(d.reasoningEfforts);
  if (!d.reasoning && efforts.length)
    throw new Error("推理档位需要同时启用推理能力。");
  for (const value of [d.contextLength, d.maxOutput])
    if (value && !isVersion(value))
      throw new Error("容量须为正整数，未知时留空。");
  if (
    d.contextLength &&
    d.maxOutput &&
    BigInt(d.maxOutput) > BigInt(d.contextLength)
  )
    throw new Error("最大输出不能超过上下文长度。");
  function object(value: string, label: string) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      throw new Error(`${label}须为 JSON 对象。`);
    }
    if (!isRecord(parsed)) throw new Error(`${label}须为 JSON 对象。`);
    return parsed;
  }
  const header = object(d.headerOverwrite, "请求头覆盖");
  if (Object.values(header).some((v) => typeof v !== "string"))
    throw new Error("请求头覆盖的值须为字符串。");
  return {
    name: d.name.trim(),
    provider_model_id: d.providerModelId.trim(),
    type: d.type,
    enabled: d.enabled,
    parameters: object(d.parameters, "参数"),
    request_overwrite: object(d.requestOverwrite, "请求体覆盖"),
    header_overwrite: header as Record<string, string>,
    capabilities: {
      tool_calls: d.toolCalls,
      parallel_tool_calls: d.parallelToolCalls,
      streaming: d.streaming,
      reasoning: d.reasoning,
      input_modalities: input,
      output_modalities: output,
      reasoning_efforts: efforts,
      structured_output_modes: values(d.structuredOutputModes, [
        "text",
        "json_schema",
      ]),
      context_length: d.contextLength || null,
      max_output: d.maxOutput || null,
    },
  };
}
