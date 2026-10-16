import { AccountError, type AccountImage } from "./transport";
import type { ProfileResponse } from "./types";

export class CandidateError extends Error {}
export const maximumBytes = 5 * 1024 * 1024;

// Read bounded containers before decoding so extreme dimensions and animated
// PNG/WebP candidates do not enter the preview decoder.
export function inspectImage(bytes: ArrayBuffer, mediaType: string) {
  const view = new DataView(bytes);
  const data = new Uint8Array(bytes);
  const textAt = (start: number, length: number) =>
    String.fromCharCode(...data.subarray(start, start + length));
  const invalid = () => {
    throw new CandidateError("请选择完整的静态 JPEG、PNG 或 WebP 图片。");
  };
  let width = 0;
  let height = 0;
  if (mediaType === "image/png") {
    if (
      data.length < 33 ||
      ![137, 80, 78, 71, 13, 10, 26, 10].every(
        (byte, index) => data[index] === byte,
      ) ||
      textAt(12, 4) !== "IHDR"
    )
      invalid();
    width = view.getUint32(16);
    height = view.getUint32(20);
    let offset = 8;
    while (offset + 12 <= data.length) {
      const size = view.getUint32(offset);
      if (offset + size + 12 > data.length || textAt(offset + 4, 4) === "acTL")
        invalid();
      offset += size + 12;
    }
    if (offset !== data.length) invalid();
  } else if (mediaType === "image/webp") {
    if (
      data.length < 20 ||
      textAt(0, 4) !== "RIFF" ||
      textAt(8, 4) !== "WEBP" ||
      view.getUint32(4, true) + 8 !== data.length
    )
      invalid();
    const uint24 = (at: number) =>
      data[at]! + (data[at + 1]! << 8) + (data[at + 2]! << 16);
    let offset = 12;
    while (offset + 8 <= data.length) {
      const kind = textAt(offset, 4);
      const size = view.getUint32(offset + 4, true);
      const start = offset + 8;
      if (start + size > data.length || kind === "ANIM" || kind === "ANMF")
        invalid();
      if (kind === "VP8X" && size >= 10) {
        if (data[start]! & 2) invalid();
        width = uint24(start + 4) + 1;
        height = uint24(start + 7) + 1;
      } else if (!width && kind === "VP8 " && size >= 10) {
        if (
          data[start + 3] !== 0x9d ||
          data[start + 4] !== 1 ||
          data[start + 5] !== 0x2a
        )
          invalid();
        width = view.getUint16(start + 6, true) & 0x3fff;
        height = view.getUint16(start + 8, true) & 0x3fff;
      } else if (!width && kind === "VP8L" && size >= 5) {
        if (data[start] !== 0x2f) invalid();
        const bits = view.getUint32(start + 1, true);
        width = (bits & 0x3fff) + 1;
        height = ((bits >>> 14) & 0x3fff) + 1;
      }
      offset = start + size + (size % 2);
    }
    if (offset !== data.length) invalid();
  } else if (mediaType === "image/jpeg") {
    if (data.length < 4 || data[0] !== 0xff || data[1] !== 0xd8) invalid();
    let offset = 2;
    while (offset + 4 <= data.length) {
      if (data[offset] !== 0xff) invalid();
      while (data[offset] === 0xff) offset++;
      const marker = data[offset++]!;
      if (marker === 0xd9 || marker === 0xda) break;
      if (marker === 1 || (marker >= 0xd0 && marker <= 0xd7)) continue;
      if (offset + 2 > data.length) invalid();
      const size = view.getUint16(offset);
      if (size < 2 || offset + size > data.length) invalid();
      if ([0xc0, 0xc1, 0xc2].includes(marker)) {
        if (size < 8) invalid();
        height = view.getUint16(offset + 3);
        width = view.getUint16(offset + 5);
      }
      offset += size;
    }
  } else invalid();
  if (!width || !height) invalid();
  if (width > 4096 || height > 4096 || width * height > 16_777_216)
    throw new CandidateError(
      "图片每边不能超过 4096 像素，总像素不能超过 16,777,216。",
    );
  return { width, height };
}

export async function boundedBlobURL(
  image: AccountImage,
  metadata: NonNullable<ProfileResponse["avatar"]>,
) {
  const expected = BigInt(metadata.byte_size);
  if (
    expected <= 0n ||
    expected > BigInt(maximumBytes) ||
    image.contentLength !== metadata.byte_size ||
    BigInt(image.blob.size) !== expected ||
    image.blob.type !== metadata.media_type ||
    image.etag !== `"${metadata.sha256}"`
  )
    throw new AccountError("INVALID_RESPONSE");
  const bytes = await image.blob.arrayBuffer();
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  const hash = `sha256:${Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
  if (hash !== metadata.sha256) throw new AccountError("INVALID_RESPONSE");
  inspectImage(bytes, image.blob.type);
  return URL.createObjectURL(image.blob);
}
