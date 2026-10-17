import type { CatalogModel } from "./model-catalog";

export interface ModelTestAttachment {
  name: string;
  media_type: string;
  data_base64: string;
}
export interface ModelTestInput {
  text: string;
  attachments: ModelTestAttachment[];
}
const mib = 1024 * 1024;
const encoder = new TextEncoder();
export const modelTestMediaTypes = [
  "image/png",
  "image/jpeg",
  "image/webp",
  "image/gif",
  "application/pdf",
  "text/plain",
  "text/markdown",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
] as const;
const extensions: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  gif: "image/gif",
  pdf: "application/pdf",
  txt: "text/plain",
  md: "text/markdown",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
};
export function modelTestFileType(file: File) {
  return (
    file.type ||
    extensions[file.name.split(".").pop()?.toLowerCase() ?? ""] ||
    ""
  );
}
export function attachmentBytes(value: ModelTestAttachment) {
  return (
    (value.data_base64.length * 3) / 4 -
    (value.data_base64.endsWith("==")
      ? 2
      : value.data_base64.endsWith("=")
        ? 1
        : 0)
  );
}
function fileError(
  name: string,
  mediaType: string,
  size: number,
  model: CatalogModel,
) {
  if (
    !name ||
    name.trim() !== name ||
    name === "." ||
    name === ".." ||
    /[\u0000-\u001f\u007f-\u009f/\\]/u.test(name) ||
    encoder.encode(name).length > 255
  )
    return "文件名须为不含路径或控制字符的名称，且不超过 255 字节。";
  if (!(modelTestMediaTypes as readonly string[]).includes(mediaType))
    return "不支持此文件格式。";
  const image = mediaType.startsWith("image/");
  if (
    !model.input.capabilities.input_modalities.includes(
      image ? "image" : "file",
    )
  )
    return image ? "此模型未声明图像输入能力。" : "此模型未声明文件输入能力。";
  const limit = image || mediaType === "application/pdf" ? 4 * mib : mib;
  if (size <= 0 || size > limit)
    return `文件不能为空，且单个文件不得超过 ${limit / mib} MiB。`;
}
export function validateModelTestInput(
  input: ModelTestInput,
  model: CatalogModel,
): string | undefined {
  if (
    model.input.type !== "chat" ||
    !model.input.capabilities.input_modalities.includes("text")
  )
    return "此模型不支持对话测试输入。";
  if (encoder.encode(input.text).length > 16384)
    return "测试文本不能超过 16 KiB。";
  if (!input.text.trim() && !input.attachments.length)
    return "请输入测试文本或添加附件。";
  if (input.attachments.length > 4) return "最多添加 4 个附件。";
  let total = 0;
  for (const item of input.attachments) {
    if (
      item.data_base64.length % 4 !== 0 ||
      !/^[A-Za-z0-9+/]*={0,2}$/.test(item.data_base64)
    )
      return "附件内容无效，请重新选择文件。";
    const size = attachmentBytes(item);
    const error = fileError(item.name, item.media_type, size, model);
    if (error) return error;
    total += size;
  }
  if (total > 8 * mib) return "附件总大小不能超过 8 MiB。";
}
// Count actual image blocks rather than searching compressed bytes for markers.
function singleFrameGif(bytes: Uint8Array) {
  if (
    bytes.length < 13 ||
    !["GIF87a", "GIF89a"].includes(String.fromCharCode(...bytes.slice(0, 6)))
  )
    return false;
  let at = 13 + (bytes[10]! & 128 ? 3 * 2 ** ((bytes[10]! & 7) + 1) : 0);
  let frames = 0;
  const skipBlocks = () => {
    while (at < bytes.length) {
      const size = bytes[at++]!;
      if (!size) return true;
      at += size;
    }
    return false;
  };
  while (at < bytes.length) {
    const block = bytes[at++];
    if (block === 0x3b) return frames === 1;
    if (block === 0x21) {
      at++;
      if (!skipBlocks()) return false;
    } else if (block === 0x2c) {
      if (++frames > 1 || at + 9 > bytes.length) return false;
      const packed = bytes[at + 8]!;
      at += 9 + (packed & 128 ? 3 * 2 ** ((packed & 7) + 1) : 0) + 1;
      if (!skipBlocks()) return false;
    } else return false;
  }
  return false;
}
export async function readModelTestFiles(
  files: readonly File[],
  existing: readonly ModelTestAttachment[],
  model: CatalogModel,
  current: () => boolean,
): Promise<ModelTestAttachment[]> {
  if (files.length + existing.length > 4)
    throw new Error("最多添加 4 个附件。");
  let total = existing.reduce((sum, item) => sum + attachmentBytes(item), 0);
  for (const file of files) {
    const error = fileError(
      file.name,
      modelTestFileType(file),
      file.size,
      model,
    );
    if (error) throw new Error(error);
    total += file.size;
  }
  if (total > 8 * mib) throw new Error("附件总大小不能超过 8 MiB。");
  const result = [...existing];
  for (const file of files) {
    if (!current()) throw new Error("文件读取已取消。");
    const bytes = new Uint8Array(await file.arrayBuffer());
    if (!current()) throw new Error("文件读取已取消。");
    const mediaType = modelTestFileType(file);
    if (mediaType === "image/gif" && !singleFrameGif(bytes))
      throw new Error("仅支持非动画 GIF，请选择静态图像。");
    let binary = "";
    for (let at = 0; at < bytes.length; at += 8192)
      binary += String.fromCharCode(...bytes.subarray(at, at + 8192));
    result.push({
      name: file.name,
      media_type: mediaType,
      data_base64: btoa(binary),
    });
  }
  return result;
}
