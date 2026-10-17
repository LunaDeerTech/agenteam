import { Buffer, File as NodeFile } from "node:buffer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogModel } from "../src/system/model-catalog";
import {
  readModelTestFiles,
  validateModelTestInput,
  type ModelTestAttachment,
} from "../src/system/model-test-input";

const mib = 1024 * 1024;
const model = (modalities = ["text", "image", "file"]): CatalogModel => ({
  id: "019a4444-4444-7444-8444-444444444444",
  provider_id: "019a2222-2222-7222-8222-222222222222",
  version: "3",
  created_at: "2026-10-17T01:00:00.000000Z",
  updated_at: "2026-10-17T01:00:00.000000Z",
  input: {
    notes: "",
    provider_model_id: "owned-multimodal",
    type: "chat",
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      input_modalities: modalities,
      output_modalities: ["text"],
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      reasoning_toggle_mode: null,
      reasoning_efforts: [],
      structured_output_modes: ["text"],
      context_length: null,
      max_output: null,
    },
  },
});
const attachment = (
  size: number,
  media_type = "application/pdf",
  name = "sample.pdf",
): ModelTestAttachment => ({
  name,
  media_type,
  data_base64: Buffer.alloc(size, 0x61).toString("base64"),
});
const validate = (attachments: ModelTestAttachment[]) =>
  validateModelTestInput({ text: "", attachments }, model());

beforeEach(() => {
  vi.stubGlobal("File", NodeFile);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("model test input", () => {
  it("counts text in UTF-8 bytes and requires content for an explicit input", () => {
    const text = "🧠".repeat(4096);
    expect(
      validateModelTestInput({ text, attachments: [] }, model()),
    ).toBeUndefined();
    expect(
      validateModelTestInput({ text: text + "a", attachments: [] }, model()),
    ).toBeDefined();
    expect(
      validateModelTestInput({ text: " \n\t", attachments: [] }, model()),
    ).toBeDefined();
    expect(
      validateModelTestInput(
        { text: " \n", attachments: [attachment(1)] },
        model(),
      ),
    ).toBeUndefined();
  });

  it.each([
    { media: "image/png", name: "image.png", limit: 4 * mib },
    { media: "application/pdf", name: "paper.pdf", limit: 4 * mib },
    { media: "text/markdown", name: "notes.md", limit: mib },
    {
      media:
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      name: "notes.docx",
      limit: mib,
    },
  ])(
    "enforces decoded bytes at the $media file boundary",
    ({ media, name, limit }) => {
      expect(validate([attachment(limit, media, name)])).toBeUndefined();
      expect(validate([attachment(limit + 1, media, name)])).toBeDefined();
    },
  );

  it("bounds the combined decoded size and file count without counting base64 expansion as raw bytes", () => {
    const full = [
      attachment(4 * mib),
      attachment(4 * mib, "application/pdf", "second.pdf"),
    ];
    expect(full[0]!.data_base64.length).toBeGreaterThan(4 * mib);
    expect(validate(full)).toBeUndefined();
    expect(
      validate([...full, attachment(1, "text/plain", "extra.txt")]),
    ).toBeDefined();
    const four = Array.from({ length: 4 }, (_, index) =>
      attachment(1, "text/plain", `note-${index}.txt`),
    );
    expect(validate(four)).toBeUndefined();
    expect(validate([...four, attachment(1)])).toBeDefined();
  });

  it("rejects unsafe basenames and malformed base64 before reading or sending attachments", () => {
    const source = attachment(1);
    expect(validate([{ ...source, name: "文".repeat(85) }])).toBeUndefined();
    for (const name of [
      "",
      ".",
      "..",
      "a/b.pdf",
      "a\\b.pdf",
      "bad\0.pdf",
      "bad\u0085.pdf",
      " note.pdf",
      "note.pdf ",
      "文".repeat(86),
    ])
      expect(validate([{ ...source, name }]), name).toBeDefined();
    for (const data_base64 of [
      "",
      "a",
      "YQ?=",
      "data:application/pdf;base64,YQ==",
    ])
      expect(validate([{ ...source, data_base64 }]), data_base64).toBeDefined();
  });

  it("requires the declared image or file modality and rejects explicit input for non-chat models", () => {
    const image = {
      text: "inspect",
      attachments: [attachment(1, "image/png", "image.png")],
    };
    const document = {
      text: "read",
      attachments: [attachment(1, "text/plain", "note.txt")],
    };
    expect(
      validateModelTestInput(image, model(["text", "image"])),
    ).toBeUndefined();
    expect(
      validateModelTestInput(image, model(["text", "file"])),
    ).toBeDefined();
    expect(
      validateModelTestInput(document, model(["text", "file"])),
    ).toBeUndefined();
    expect(
      validateModelTestInput(document, model(["text", "image"])),
    ).toBeDefined();
    expect(
      validateModelTestInput(
        { text: "hello", attachments: [] },
        model(["image"]),
      ),
    ).toBeDefined();
    const embedding = model();
    embedding.input.type = "embedding";
    expect(validateModelTestInput(document, embedding)).toBeDefined();
    expect(
      validate([attachment(1, "application/octet-stream", "unknown.bin")]),
    ).toBeDefined();
  });

  it("reads exact file bytes into a new attachment list and recognizes a missing MIME type by extension", async () => {
    const existing = [attachment(1)];
    const text = "附件内容 🧠\n";
    const markdown = new File([text], "notes.MD");
    const bytes = new Uint8Array([0, 1, 127, 128, 255]);
    const image = new File([bytes], "image.png", { type: "image/png" });
    const read = await readModelTestFiles(
      [markdown, image],
      existing,
      model(),
      () => true,
    );
    expect(existing).toEqual([attachment(1)]);
    expect(read).not.toBe(existing);
    expect(read).toEqual([
      existing[0],
      {
        name: "notes.MD",
        media_type: "text/markdown",
        data_base64: Buffer.from(text).toString("base64"),
      },
      { name: "image.png", media_type: "image/png", data_base64: "AAF/gP8=" },
    ]);
  });

  it("accepts a static GIF even with a marker in a comment and rejects a second image frame", async () => {
    const header = Buffer.from("47494638396101000100800000000000ffffff", "hex");
    const frame = Buffer.from("2c0000000001000100000202440100", "hex");
    const comment = new Uint8Array([0x21, 0xfe, 1, 0x2c, 0]);
    const staticBytes = Uint8Array.from(
      Buffer.concat([header, comment, frame, new Uint8Array([0x3b])]),
    );
    const animatedBytes = Uint8Array.from(
      Buffer.concat([header, frame, frame, new Uint8Array([0x3b])]),
    );
    const single = new File([staticBytes], "static.gif", { type: "image/gif" });
    await expect(
      readModelTestFiles([single], [], model(), () => true),
    ).resolves.toEqual([
      {
        name: "static.gif",
        media_type: "image/gif",
        data_base64: Buffer.from(staticBytes).toString("base64"),
      },
    ]);
    const animated = new File([animatedBytes], "animated.gif", {
      type: "image/gif",
    });
    await expect(
      readModelTestFiles([animated], [], model(), () => true),
    ).rejects.toThrow("非动画");
  });

  it("discards an in-flight file read when its owner becomes stale and leaves the original list untouched", async () => {
    let began!: () => void;
    let finish!: (bytes: ArrayBuffer) => void;
    const started = new Promise<void>((resolve) => {
      began = resolve;
    });
    const reading = new Promise<ArrayBuffer>((resolve) => {
      finish = resolve;
    });
    const first = new File(["pending"], "first.txt", { type: "text/plain" });
    vi.spyOn(first, "arrayBuffer").mockImplementation(() => {
      began();
      return reading;
    });
    const second = new File(["next"], "second.txt", { type: "text/plain" });
    const secondRead = vi.spyOn(second, "arrayBuffer");
    const existing = [attachment(1)];
    let current = true;
    const result = readModelTestFiles(
      [first, second],
      existing,
      model(),
      () => current,
    );
    await started;
    current = false;
    finish(new ArrayBuffer(7));
    await expect(result).rejects.toThrow("取消");
    expect(secondRead).not.toHaveBeenCalled();
    expect(existing).toEqual([attachment(1)]);
  });
});
