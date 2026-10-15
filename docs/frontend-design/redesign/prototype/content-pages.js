import {
  esc,
  icon,
  status,
  banner,
  action,
  stateGate,
  readonlyNotice,
  showDialog,
  closeDialog,
  dialogHeader,
  dialogFooter,
  inlineResult,
} from "./core.js";

// F0 fixtures only. Field names follow KnowledgeDocument and ProjectListItem.
// Commands are demonstrated locally; this module never calls a product API.
const utf8 = new TextEncoder();
const projectID = "0198a000-1000-7000-8000-000000000001";
const userID = "0198a000-1000-7000-8000-000000000002";
const markdown = `# 项目交付说明\n\n这份文档记录 Atlas 项目的工作边界、资料约定和交付检查。先对齐上下文，再开始一项具体任务。\n\n## 工作方式\n\n每项任务都应有明确的输入、负责人和可观察的完成结果。讨论中的决定回到任务正文，供后续协作者查阅。\n\n- 在任务中说明目标与约束。\n- 将执行结果附在对应任务下。\n- 交付前核对实际行为和文档。\n\n## 资料目录\n\n本目录包含交付清单、接口参考 PDF 和协作约定 DOCX。文件的解析状态与原文件可用性分别展示。\n\n### 内容引用\n\n引用时保留文档与版本；针对某页的讨论补充 PDF 页码。不要把片段读取结果误认为全文。\n\n\`\`\`text\nproject: atlas\ndocument: delivery-notes\ncontent_version: 4\n\`\`\`\n\n## 交付检查\n\n1. 打开目标页面，确认内容与请求一致。\n2. 验证失败后的恢复入口。\n3. 保留检查范围及尚未解决的问题。\n\n## 下一步\n\n打开「交付清单」，根据当前任务补充具体检查内容。\n\n---\n\n本文以 Markdown 原文显示，链接与 HTML 均不会在此执行。`;
const plainText = `交付清单\n\n开始之前\n确认任务目标、所属项目和当前资料版本。\n把需要回答的问题写清楚，避免在执行中丢失上下文。\n\n实施与检查\n□ 输入与边界已说明\n□ 正常流程已检查\n□ 失败信息包含可执行的下一步\n□ 文档中的命令与实际行为一致\n\n交付记录\n完成内容：记录用户能够观察到的变化。\n检查结果：记录真实执行的检查及输入。\n尚未完成：标明问题、影响与下一位负责人。\n\n注意\n这是一份本地合成的 TXT 文件。可以在样板中编辑正文，或下载所选原版本。`;
function makePDF() {
  const stream = `q\n0.95 0.95 0.94 rg\n0 718 595 124 re f\nQ\nBT\n/F1 11 Tf\n0.35 0.36 0.38 rg\n48 790 Td\n(ATLAS / REFERENCE DOCUMENT) Tj\n0 -38 Td\n/F2 25 Tf\n0.1 0.12 0.15 rg\n(Interface delivery guide) Tj\n0 -24 Td\n/F1 11 Tf\n(Revision 2   /   October 2026   /   Local prototype fixture) Tj\n0 -72 Td\n/F2 15 Tf\n(01  Establish a shared context) Tj\n0 -28 Td\n/F1 11 Tf\n(Every task starts with an explicit goal, a known project and a) Tj\n0 -18 Td\n(documented source version. Keep the reading surface calm.) Tj\n0 -48 Td\n/F2 15 Tf\n(02  Keep the original file available) Tj\n0 -28 Td\n/F1 11 Tf\n(Parsing and indexing are independent from source-file access.) Tj\n0 -18 Td\n(This page is rendered from a valid PDF byte stream.) Tj\n0 -48 Td\n/F2 15 Tf\n(03  Verify observable outcomes) Tj\n0 -28 Td\n/F1 11 Tf\n(Record the input, the result and any remaining limitation.) Tj\n0 -18 Td\n(Do not report an accepted request as a completed operation.) Tj\n0 -190 Td\n0.42 0.43 0.45 rg\n/F1 9 Tf\n(ATLAS KNOWLEDGE                           SAMPLE DOCUMENT                         1 / 1) Tj\nET`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((obj, i) => {
    offsets.push(pdf.length);
    pdf += `${i + 1} 0 obj\n${obj}\nendobj\n`;
  });
  const start = pdf.length;
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets
    .slice(1)
    .map((n) => `${String(n).padStart(10, "0")} 00000 n \n`)
    .join("")}trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n${start}\n%%EOF`;
  return utf8.encode(pdf);
}
function makeDOCX() {
  // Minimal valid OOXML archive; uncompressed ZIP avoids a new runtime dependency.
  const files = {
    "[Content_Types].xml":
      '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
    "_rels/.rels":
      '<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
    "word/document.xml":
      '<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Atlas 协作约定</w:t></w:r></w:p><w:p><w:r><w:t>这是 F0 样板原文件。解析状态仅为演示，下载内容为有效的 DOCX 文件。</w:t></w:r></w:p><w:sectPr/></w:body></w:document>',
  };
  const chunks = [],
    central = [];
  let offset = 0;
  const pack = (size, writes) => {
    const bytes = new Uint8Array(size);
    const view = new DataView(bytes.buffer);
    writes(view);
    return bytes;
  };
  const crc32 = (bytes) => {
    let crc = 0xffffffff;
    for (const byte of bytes) {
      crc ^= byte;
      for (let i = 0; i < 8; i++)
        crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
    }
    return (crc ^ 0xffffffff) >>> 0;
  };
  for (const [name, source] of Object.entries(files)) {
    const n = utf8.encode(name),
      bytes = utf8.encode(source),
      crc = crc32(bytes);
    const header = pack(30, (v) => {
      v.setUint32(0, 0x04034b50, true);
      v.setUint16(4, 20, true);
      v.setUint32(14, crc, true);
      v.setUint32(18, bytes.length, true);
      v.setUint32(22, bytes.length, true);
      v.setUint16(26, n.length, true);
    });
    const record = pack(46, (v) => {
      v.setUint32(0, 0x02014b50, true);
      v.setUint16(4, 20, true);
      v.setUint16(6, 20, true);
      v.setUint32(16, crc, true);
      v.setUint32(20, bytes.length, true);
      v.setUint32(24, bytes.length, true);
      v.setUint16(28, n.length, true);
      v.setUint32(42, offset, true);
    });
    chunks.push(header, n, bytes);
    central.push(record, n);
    offset += header.length + n.length + bytes.length;
  }
  const size = central.reduce((n, a) => n + a.length, 0);
  const end = pack(22, (v) => {
    v.setUint32(0, 0x06054b50, true);
    v.setUint16(8, 3, true);
    v.setUint16(10, 3, true);
    v.setUint32(12, size, true);
    v.setUint32(16, offset, true);
  });
  const output = new Uint8Array(offset + size + 22);
  let cursor = 0;
  for (const part of [...chunks, ...central, end]) {
    output.set(part, cursor);
    cursor += part.length;
  }
  return output;
}
const formats = {
  md: ["Markdown", "text/markdown"],
  txt: ["TXT", "text/plain"],
  pdf: ["PDF", "application/pdf"],
  docx: [
    "DOCX",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  ],
};
function documentFixture(key, title, parent, content, indexing = "ready") {
  const media = formats[key][1];
  return {
    id: `0198a000-2000-7000-8000-00000000000${Object.keys(formats).indexOf(key) + 1}`,
    project_id: projectID,
    parent_document_id:
      parent === "md" ? "0198a000-2000-7000-8000-000000000001" : parent,
    title,
    content_version: key === "md" ? "4" : "2",
    source_kind: key === "md" || key === "txt" ? "text" : "file",
    media_type: media,
    status: "active",
    indexing_status: indexing,
    created_by: { kind: "human", user_id: userID },
    created_at: "2026-10-12T08:30:00Z",
    updated_at: "2026-10-15T09:20:00Z",
    key,
    content,
    revisions: [
      {
        version: key === "md" ? "4" : "2",
        bytes: typeof content === "string" ? utf8.encode(content) : content,
      },
      {
        version: "1",
        bytes:
          typeof content === "string"
            ? utf8.encode(content.replace("交付", "初稿"))
            : content,
      },
    ],
  };
}
const documents = [
  documentFixture("md", "项目交付说明", null, markdown),
  documentFixture("txt", "交付清单.txt", "md", plainText),
  documentFixture("pdf", "接口交付参考.pdf", "md", makePDF()),
  documentFixture(
    "docx",
    "跨团队协作约定.docx",
    "md",
    makeDOCX(),
    "processing",
  ),
];
documents.forEach((doc) =>
  doc.revisions.forEach((revision) => {
    revision.media_type = doc.media_type;
    revision.format = doc.key;
  }),
);
const formatLabel = (doc) =>
  formats[doc.format || doc.key]?.[0] || (doc.format || doc.key).toUpperCase();
const knowledge = {
  selected: "md",
  tree: false,
  group: true,
  search: "",
  info: false,
  editor: null,
  draft: null,
  message: "",
  unknown: false,
  unknownResolved: false,
  pdfURL: null,
  previewKey: "",
  revision: "",
  readMore: false,
};
const longTitle =
  "Atlas 跨区域协作与知识内容交付规范——包含长名称、原文件版本和失败恢复的完整说明";
const query = () => new URLSearchParams(location.search);
const href = (ctx, page, extra = {}) => {
  const p = new URLSearchParams(location.search);
  p.set("page", page);
  Object.entries(extra).forEach(([key, value]) => p.set(key, value));
  return `?${p}`;
};
const date = (value) =>
  new Intl.DateTimeFormat("zh-CN", {
    month: "long",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
  }).format(new Date(value));
const selected = () =>
  documents.find((d) => d.key === knowledge.selected) || documents[0];
const displayTitle = (ctx, doc) =>
  ctx.long && doc.key === "md" ? longTitle : doc.title;
const locked = (ctx) =>
  ctx.scenario === "readonly" ||
  knowledge.unknown ||
  ctx.scenario === "unknown";
function revokePreview() {
  if (knowledge.pdfURL) URL.revokeObjectURL(knowledge.pdfURL);
  knowledge.pdfURL = null;
  knowledge.previewKey = "";
}
function pdfURL(doc) {
  const key = `${doc.key}:${doc.content_version}`;
  if (knowledge.previewKey !== key) {
    revokePreview();
    knowledge.pdfURL = URL.createObjectURL(
      new Blob([doc.content], { type: doc.media_type }),
    );
    knowledge.previewKey = key;
  }
  return knowledge.pdfURL;
}
window.addEventListener("pagehide", revokePreview);
window.addEventListener("popstate", () => {
  if (query().get("page") !== "knowledge") revokePreview();
});
document.addEventListener("click", (event) => {
  const link = event.target.closest("a[href]");
  if (
    link &&
    new URL(link.href, location.href).searchParams.get("page") !== "knowledge"
  )
    revokePreview();
});
window.addEventListener("beforeunload", (event) => {
  if (knowledge.editor && knowledge.editor.text !== knowledge.editor.original) {
    event.preventDefault();
    event.returnValue = "";
  }
});

function treeHTML(ctx) {
  const matches = documents.filter((d) =>
    d.title.toLowerCase().includes(knowledge.search.toLowerCase()),
  );
  const row = (doc) =>
    `<a class="knowledge-tree-link ${doc.key === knowledge.selected ? "is-selected" : ""} ${doc.parent_document_id && !knowledge.search ? "is-child" : ""}" href="${esc(href(ctx, "knowledge", { doc: doc.key }))}" data-doc="${esc(doc.key)}" ${doc.key === knowledge.selected ? 'aria-current="page"' : ""}>${icon("file")}<span>${esc(displayTitle(ctx, doc))}</span><small>${esc(formatLabel(doc))}</small></a>`;
  return `<aside class="knowledge-tree ${knowledge.tree ? "is-open" : ""}" aria-label="知识文档目录"><div class="knowledge-tree-heading"><strong>文档</strong><button class="icon-button knowledge-mobile" type="button" aria-label="关闭文档目录" data-knowledge="close-tree">${icon("close")}</button></div><label class="knowledge-search">${icon("search")}<input name="knowledge-title" aria-label="查找本项目文档标题" type="search" placeholder="查找文档标题…" value="${esc(knowledge.search)}"></label><div class="knowledge-tree-list">${
    knowledge.search
      ? `<p class="muted small">本项目 · 标题匹配</p>${matches.length ? matches.map(row).join("") : '<div class="knowledge-no-results"><p>没有匹配的标题</p><button type="button" class="button quiet" data-knowledge="clear-search">清除查找</button></div>'}`
      : `<div class="knowledge-tree-group"><button class="icon-button" type="button" aria-label="${knowledge.group ? "收起" : "展开"}项目交付说明的子文档" aria-expanded="${knowledge.group}" data-knowledge="toggle-group">${icon(knowledge.group ? "chevron" : "arrow", 14)}</button>${row(documents[0])}</div>${documents
          .slice(1)
          .filter((d) => !d.parent_document_id || knowledge.group)
          .map(row)
          .join("")}`
  }</div><p class="knowledge-tree-foot">标题查找限当前项目<br>示例文档仅存于此标签页</p></aside>`;
}
function infoHTML(doc) {
  return `<aside class="knowledge-info" aria-label="文档详细信息"><div class="knowledge-info-heading"><h2>详细信息</h2><button class="icon-button" type="button" aria-label="关闭详细信息" data-knowledge="info">${icon("close")}</button></div><dl><dt>格式</dt><dd>${esc(formatLabel(doc))}</dd><dt>内容版本</dt><dd>v${esc(doc.content_version)}</dd><dt>索引状态</dt><dd>${esc({ ready: "已就绪", processing: "解析中", failed: "解析失败", pending: "等待处理" }[doc.indexing_status])}</dd><dt>创建来源</dt><dd>用户创建</dd><dt>更新于</dt><dd>${date(doc.updated_at)} UTC</dd><dt>读取范围</dt><dd>${doc.source_kind === "text" ? "当前完整原文" : "所选版本原文件"}</dd></dl><details><summary>文档标识</summary><p class="knowledge-id">${esc(doc.id)}</p><p class="muted small">项目内的稳定文档标识</p></details><div class="knowledge-source-info"><h3>原文件</h3><p class="muted small">下载绑定所选版本，之后的修改不会替换此次下载内容。</p><label class="field">原版本<select name="knowledge-revision" aria-label="原版本">${doc.revisions.map((r) => `<option value="${esc(r.version)}" ${knowledge.revision === r.version ? "selected" : ""}>v${esc(r.version)}${r.version === doc.content_version ? " · 当前" : ""}</option>`).join("")}</select></label><button class="button" type="button" data-knowledge="download">${icon("file")} 下载原版本</button></div></aside>`;
}
function readerBody(ctx, doc) {
  const format = doc.format || doc.key;
  if (format === "pdf")
    return `<div class="knowledge-file-context"><span>PDF · 原文件预览</span><span>v${esc(doc.content_version)}</span></div><object class="knowledge-pdf" data="${esc(pdfURL(doc))}#toolbar=1&navpanes=0&view=FitH" type="application/pdf" aria-label="接口交付参考 PDF 原文件预览"><div class="knowledge-file-empty"><h2>此浏览器无法内嵌 PDF</h2><p>下载原版本后，使用 PDF 阅读器打开。</p><button class="button" data-knowledge="download">下载原版本</button></div></object><p class="knowledge-preview-note">有效 PDF 文件 · 可在浏览器预览；若当前浏览器不支持，使用“下载原版本”。</p>`;
  if (format === "docx")
    return `<div class="knowledge-file-empty"><span class="knowledge-format-icon">${icon("file", 32)}</span><h2>${doc.indexing_status === "failed" ? "正文解析失败" : "正在解析文档正文"}</h2><p>${doc.indexing_status === "failed" ? "尚未获得可读取的正文。原文件仍可下载，稍后可重新读取解析状态。" : "原文件已经就绪。正文解析完成后可以在这里阅读；当前可下载 DOCX 原版本。"}</p><div class="knowledge-file-actions"><button class="button primary" data-knowledge="download">下载原版本</button><button class="button" data-knowledge="check-parse">重新读取状态</button></div><p class="muted small">DOCX 不提供版式预览。此样板保留真实格式边界。</p><details class="knowledge-demo-details"><summary>演示解析失败</summary><button class="button quiet" data-knowledge="parse-fail">切换为解析失败</button></details></div>`;
  return `<div class="knowledge-reading-label"><span>${format === "md" ? "Markdown 原文" : "纯文本"}</span><span>${ctx.long && !knowledge.readMore ? "部分内容 · 尚未完整读取" : "已完整读取"}</span></div><pre class="knowledge-prose ${format === "md" ? "is-markdown" : ""}">${esc(doc.content)}${ctx.long && !knowledge.readMore ? "\n\n—— 以下为长内容演示，继续读取后展示 ——" : ""}${ctx.long && knowledge.readMore ? "\n\n附录：交付时保持来源与内容版本一致。\n\n".repeat(20) : ""}</pre>${ctx.long && !knowledge.readMore ? `<div class="knowledge-read-more"><p>本次显示前一段内容。继续读取保留当前文档与版本。</p><button class="button" data-knowledge="read-more">继续读取</button></div>` : ""}`;
}
function editorHTML(ctx, doc) {
  const edit = knowledge.editor;
  return `<div class="knowledge-editor"><div class="knowledge-editor-caption"><strong>编辑原文</strong><span class="muted small">${esc(formatLabel(doc))} · 基于 v${esc(edit.version)}</span></div><label class="knowledge-editor-label" for="knowledge-source">正文</label><textarea id="knowledge-source" name="knowledge-source" spellcheck="false" ${locked(ctx) ? "readonly" : ""}>${esc(edit.text)}</textarea><div class="knowledge-editor-footer"><span class="muted small" id="knowledge-draft-status">草稿保留在当前标签页</span><div><button type="button" class="button" data-knowledge="close-editor">关闭编辑</button><button type="button" class="button primary" data-knowledge="save" ${locked(ctx) ? 'disabled title="先查询原请求结果"' : ""}>保存原文</button></div></div></div>`;
}
function renderKnowledge(ctx) {
  const key = query().get("doc");
  if (key && documents.some((d) => d.key === key)) knowledge.selected = key;
  const doc = selected();
  const gate = stateGate(ctx, "知识库");
  const isUnknown = locked(ctx) && ctx.scenario !== "readonly";
  const head = `<header class="knowledge-viewbar"><div class="knowledge-view-title"><button type="button" class="icon-button knowledge-mobile" aria-label="打开文档目录" data-knowledge="open-tree">${icon("menu")}</button><h1>知识库</h1></div><div class="knowledge-top-actions"><button class="button quiet" type="button" data-knowledge="upload" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>上传文件</button><button class="button primary" type="button" data-knowledge="create" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>${icon("plus")} 新建文档</button></div></header>`;
  if (gate) return `<section class="knowledge-page">${head}${gate}</section>`;
  if (ctx.scenario === "empty")
    return `<section class="knowledge-page">${head}<div class="empty-state knowledge-empty">${icon("file", 32)}<h2>还没有项目资料</h2><p>新建文本，或上传 TXT、Markdown、PDF、DOCX 文件。<br>文档可以包含子文档，形成项目资料目录。</p><button class="button" data-knowledge="load-fixtures">载入示例资料</button></div></section>`;
  return `<section class="knowledge-page">${head}${readonlyNotice(ctx)}${isUnknown ? banner("原请求结果未知", "已保留原版本与本次意图。请先查询原请求；不要再次创建或覆盖。此处为本地 Unknown 状态演示。", "warning", action("查询原请求", "knowledge-lookup")) : ""}${knowledge.message ? `<div class="knowledge-result" role="status">${esc(knowledge.message)}</div>` : ""}<div class="knowledge-workspace ${knowledge.tree ? "is-tree-open" : ""} ${knowledge.info ? "has-info" : ""}">${treeHTML(ctx)}<div class="knowledge-reader"><header class="knowledge-document-header"><div class="knowledge-breadcrumb">${doc.parent_document_id ? "<span>" + esc(documents.find((d) => d.id === doc.parent_document_id)?.title || "父级文档") + '</span><span aria-hidden="true">/</span>' : "<span>项目根目录</span>"}<span>${esc(formatLabel(doc))}</span></div><div class="knowledge-document-title"><h2>${esc(displayTitle(ctx, doc))}</h2><div class="knowledge-document-actions">${doc.source_kind === "text" && !knowledge.editor ? `<button class="button" data-knowledge="edit" ${locked(ctx) || (ctx.long && !knowledge.readMore) ? 'disabled title="当前只读、请求结果未知或尚未完整读取；请先继续读取"' : ""}>编辑原文</button>` : ""}<button type="button" class="icon-button" aria-label="${knowledge.info ? "关闭" : "打开"}详细信息" aria-expanded="${knowledge.info}" data-knowledge="info">${icon("settings")}</button><details class="knowledge-more"><summary class="icon-button" aria-label="更多文档操作">${icon("more")}</summary><div class="knowledge-more-menu"><button data-knowledge="download">下载当前原版本</button><button data-knowledge="replace" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>替换原文件</button><button data-knowledge="rename" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>重命名</button><button data-knowledge="move" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>移动文档</button><button class="danger" data-knowledge="delete" ${locked(ctx) ? 'disabled title="当前只读或请求结果未知"' : ""}>删除文档…</button></div></details></div></div><p class="knowledge-document-meta">更新于 ${date(doc.updated_at)} <span>·</span> v${esc(doc.content_version)} ${doc.indexing_status !== "ready" ? `<span>·</span> ${status(doc.indexing_status === "failed" ? "解析失败" : "解析中", doc.indexing_status === "failed" ? "danger" : "warning")}` : ""}</p></header>${knowledge.draft && !knowledge.editor && knowledge.draft.key === doc.key ? banner("此文档有未保存的草稿", "草稿保留在当前标签页，可以继续编辑。", "info", '<button class="button" data-knowledge="resume">继续草稿</button>') : ""}${knowledge.editor && knowledge.editor.key === doc.key ? editorHTML(ctx, doc) : `<div class="knowledge-reader-scroll">${readerBody(ctx, doc)}</div>`}</div>${knowledge.info ? infoHTML(doc) : ""}</div></section>`;
}
function refresh(ctx, focus) {
  ctx.render();
  if (focus) document.querySelector(focus)?.focus();
}
function guardEditor(ctx, proceed) {
  if (
    !knowledge.editor ||
    knowledge.editor.text === knowledge.editor.original
  ) {
    knowledge.editor = null;
    proceed();
    return;
  }
  showDialog(
    `${dialogHeader("保留未保存的草稿？")}<div class="dialog-body"><p>「${esc(selected().title)}」的原文尚未保存。保留草稿后可在当前标签页继续编辑。</p></div>${dialogFooter('<button class="button danger" data-close-edit="discard">放弃修改</button><button class="button" data-dialog-close>继续编辑</button><button class="button primary" data-close-edit="keep">保留草稿并关闭</button>')}`,
    (d) => {
      d.querySelectorAll("[data-close-edit]").forEach((button) => {
        button.onclick = () => {
          const mode = button.dataset.closeEdit;
          knowledge.draft = mode === "keep" ? { ...knowledge.editor } : null;
          knowledge.editor = null;
          closeDialog();
          proceed();
        };
      });
    },
  );
}
function download(doc) {
  const revision =
    doc.revisions.find((r) => r.version === knowledge.revision) ||
    doc.revisions[0];
  const url = URL.createObjectURL(
    new Blob([revision.bytes], { type: revision.media_type || doc.media_type }),
  );
  const a = document.createElement("a");
  a.href = url;
  const ext = revision.format || doc.format || doc.key;
  a.download = `${doc.title.replace(/\.(txt|md|pdf|docx)$/i, "")}-v${revision.version}.${ext}`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1500);
  knowledge.message = `已生成 v${revision.version} 原版本下载（本地样板文件）。`;
}
function createText(ctx) {
  showDialog(
    `${dialogHeader("新建文档")}<form id="knowledge-create-form"><div class="dialog-body"><label class="field">文档标题<input name="title" required maxlength="512" placeholder="例如：项目实施说明…"></label><label class="field">格式<select name="format"><option value="md">Markdown 原文</option><option value="txt">纯文本 TXT</option></select></label><label class="field">放置位置<select name="parent"><option value="">项目根目录</option><option value="${esc(selected()?.id || "")}">${esc(selected()?.title || "项目根目录")} 下</option></select></label><p class="muted small">新建后在主区编写原文。本操作只创建本地样板文档。</p></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button type="submit" class="button primary">创建并编辑</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        const form = new FormData(e.target);
        const format = form.get("format");
        const key = `local-${Date.now()}`;
        const doc = documentFixture(
          format,
          form.get("title").trim(),
          form.get("parent") || null,
          "",
        );
        doc.id =
          "0198a000-2000-7000-8000-" +
          crypto.randomUUID().replaceAll("-", "").slice(0, 12);
        doc.key = key;
        doc.format = format;
        doc.content_version = "1";
        doc.revisions = [
          {
            version: "1",
            bytes: utf8.encode(""),
            media_type: doc.media_type,
            format,
          },
        ];
        documents.push(doc);
        knowledge.selected = key;
        knowledge.editor = { key, version: "1", original: "", text: "" };
        closeDialog();
        ctx.navigate("knowledge", { doc: key, scenario: "normal" });
      };
    },
  );
}
function fileDialog(ctx, replace) {
  const target = selected();
  showDialog(
    `${dialogHeader(replace ? "替换原文件" : "上传文件")}<form id="knowledge-file-form"><div class="dialog-body">${replace ? `<p>替换「${esc(target.title)}」的原文件，基于 v${esc(target.content_version)}。原版本仍可下载。</p>` : '<label class="field">文档标题<input name="title" required maxlength="512" placeholder="例如：接口说明…"></label><label class="field">放置位置<select name="parent"><option value="">项目根目录</option><option value="' + esc(target?.id || "") + '">' + esc(target?.title || "项目根目录") + " 下</option></select></label>"}<label class="field">选择原文件<input name="file" type="file" accept=".txt,.md,.markdown,.pdf,.docx" required aria-describedby="knowledge-file-help"></label><p id="knowledge-file-help" class="muted small">支持 TXT、Markdown、PDF、DOCX，最大 1 MiB。文件只读入此标签页，不会上传到服务端。</p><p class="knowledge-field-error" role="alert"></p></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button type="submit" class="button primary">' + (replace ? "替换样板文件" : "载入样板文件") + "</button>")}</form>`,
    (d) => {
      const form = d.querySelector("form");
      form.onsubmit = async (e) => {
        e.preventDefault();
        const file = form.elements.file.files[0];
        const error = d.querySelector(".knowledge-field-error");
        const ext = file?.name.split(".").pop().toLowerCase();
        const format = ext === "markdown" ? "md" : ext;
        if (!file || !formats[format] || file.size > 1048576) {
          error.textContent = "请选择支持的文件格式，且文件大小不超过 1 MiB。";
          form.elements.file.focus();
          return;
        }
        const submit = form.querySelector("button[type=submit]");
        submit.disabled = true;
        submit.textContent = "正在读取…";
        const bytes = new Uint8Array(await file.arrayBuffer());
        if (!d.open || d.querySelector("form") !== form) return;
        let content = bytes;
        if (format === "md" || format === "txt") {
          try {
            content = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
          } catch {
            error.textContent =
              "文本文件需要 UTF-8 编码，请另存为 UTF-8 后选择。";
            submit.disabled = false;
            submit.textContent = replace ? "替换样板文件" : "载入样板文件";
            return;
          }
        }
        const doc = replace
          ? target
          : documentFixture(
              format,
              form.elements.title.value.trim(),
              form.elements.parent.value || null,
              content,
              format === "docx" ? "processing" : "ready",
            );
        if (!replace) {
          doc.id =
            "0198a000-2000-7000-8000-" +
            crypto.randomUUID().replaceAll("-", "").slice(0, 12);
          doc.key = `local-${Date.now()}`;
          doc.content_version = "1";
          doc.revisions = [];
          documents.push(doc);
        } else doc.content_version = String(Number(doc.content_version) + 1);
        doc.format = format;
        doc.content = content;
        doc.media_type = formats[format][1];
        doc.source_kind = ["md", "txt"].includes(format) ? "text" : "file";
        doc.indexing_status = ["docx", "pdf"].includes(format)
          ? "processing"
          : "ready";
        doc.revisions.unshift({
          version: doc.content_version,
          bytes,
          media_type: doc.media_type,
          format,
        });
        knowledge.selected = doc.key;
        knowledge.message = `${replace ? "已替换" : "已载入"}本地样板文件；没有发送到后端。`;
        closeDialog();
        ctx.navigate("knowledge", { doc: doc.key, scenario: "normal" });
      };
    },
    {
      beforeClose: () => {
        const d = document.querySelector("#dialog");
        if (
          d.querySelector("input[type=file]")?.files.length ||
          d.querySelector("input[name=title]")?.value
        ) {
          errorForDraft(d);
          return false;
        }
        return true;
      },
    },
  );
}
function errorForDraft(d) {
  const error = d.querySelector(".knowledge-field-error");
  if (!error) return;
  error.innerHTML =
    '尚未提交的文件选择将丢失。<button class="button danger" type="button" data-discard-file>放弃并关闭</button>';
  error.querySelector("button").onclick = closeDialog;
}
function renameDialog(ctx) {
  const doc = selected();
  showDialog(
    `${dialogHeader("重命名文档")}<form><div class="dialog-body"><label class="field">文档标题<input name="title" value="${esc(doc.title)}" required maxlength="512"></label><p class="muted small">本地样板演示；文档内容保持在原版本。</p></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button class="button primary" type="submit">保存名称</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        doc.title = e.target.elements.title.value.trim();
        knowledge.message = "已更新本地样板名称。";
        closeDialog();
        refresh(ctx);
      };
    },
  );
}
function bindKnowledge(ctx) {
  const main = document.querySelector("#main");
  const search = main.querySelector("input[name=knowledge-title]");
  if (search)
    search.oninput = (e) => {
      const start = e.target.selectionStart;
      knowledge.search = e.target.value;
      refresh(ctx);
      const input = main.querySelector("input[name=knowledge-title]");
      input.focus();
      try {
        input.setSelectionRange(start, start);
      } catch {}
    };
  const source = main.querySelector("#knowledge-source");
  if (source)
    source.oninput = (e) => {
      knowledge.editor.text = e.target.value;
      main.querySelector("#knowledge-draft-status").textContent =
        "有未保存的修改 · 草稿保留在当前标签页";
    };
  const revision = main.querySelector("[name=knowledge-revision]");
  if (revision)
    revision.onchange = (e) => {
      knowledge.revision = e.target.value;
    };
  main.onclick = (e) => {
    const link = e.target.closest("[data-doc]");
    if (link) {
      if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      guardEditor(ctx, () => {
        knowledge.selected = link.dataset.doc;
        knowledge.tree = false;
        knowledge.revision = "";
        knowledge.readMore = false;
        ctx.navigate("knowledge", { doc: link.dataset.doc });
      });
      return;
    }
    const control = e.target.closest(
      '[data-knowledge], [data-action="knowledge-lookup"]',
    );
    if (!control) return;
    const command = control.dataset.knowledge || "lookup";
    const doc = selected();
    if (command === "open-tree") knowledge.tree = true;
    else if (command === "close-tree") knowledge.tree = false;
    else if (command === "toggle-group") knowledge.group = !knowledge.group;
    else if (command === "clear-search") knowledge.search = "";
    else if (command === "info") knowledge.info = !knowledge.info;
    else if (command === "load-fixtures") {
      ctx.setScenario("normal");
      return;
    } else if (command === "edit") {
      knowledge.editor = {
        key: doc.key,
        version: doc.content_version,
        original: doc.content,
        text: doc.content,
      };
      knowledge.tree = false;
    } else if (command === "resume") {
      knowledge.editor = { ...knowledge.draft };
      knowledge.tree = false;
    } else if (command === "close-editor") {
      guardEditor(ctx, () => refresh(ctx, "[data-knowledge=edit]"));
      return;
    } else if (command === "save") {
      if (locked(ctx)) return;
      const edit = knowledge.editor;
      if (utf8.encode(edit.text).length > 1048576) {
        knowledge.message = "原文超过 1 MiB，未保存。请缩短内容后再次保存。";
        refresh(ctx);
        return;
      }
      doc.content = edit.text;
      doc.content_version = String(Number(doc.content_version) + 1);
      doc.revisions.unshift({
        version: doc.content_version,
        bytes: utf8.encode(edit.text),
      });
      knowledge.editor = null;
      knowledge.draft = null;
      knowledge.message = `已保存到本地样板 v${doc.content_version}，未向服务端提交。`;
    } else if (command === "download") download(doc);
    else if (command === "lookup") {
      knowledge.unknown = false;
      knowledge.unknownResolved = true;
      knowledge.message =
        "样板查询结果：原请求已完成；保留原请求身份，没有重复提交。当前可继续阅读或编辑。";
      if (ctx.scenario === "unknown") {
        ctx.setScenario("normal");
        return;
      }
    } else if (command === "check-parse") {
      inlineResult(
        control,
        doc.indexing_status === "failed"
          ? "样板重新读取：解析仍失败，原文件可继续下载。"
          : "样板重新读取：正文仍在解析中，原文件可继续下载。",
      );
      return;
    } else if (command === "parse-fail") doc.indexing_status = "failed";
    else if (command === "read-more") knowledge.readMore = true;
    else if (command === "create") {
      guardEditor(ctx, () => createText(ctx));
      return;
    } else if (command === "upload" || command === "replace") {
      guardEditor(ctx, () => fileDialog(ctx, command === "replace"));
      return;
    } else if (command === "rename") {
      renameDialog(ctx);
      return;
    } else if (command === "move") {
      showDialog(
        `${dialogHeader("移动文档")}<div class="dialog-body"><p>将「${esc(doc.title)}」移动到项目根目录。</p><p class="muted small">移动保留正文和文档身份。本地样板演示。</p></div>${dialogFooter('<button class="button" data-dialog-close>取消</button><button class="button primary" data-move-confirm>移动到根目录</button>')}`,
        (d) => {
          d.querySelector("[data-move-confirm]").onclick = () => {
            doc.parent_document_id = null;
            knowledge.message = "已在本地样板中移动到根目录。";
            closeDialog();
            refresh(ctx);
          };
        },
      );
      return;
    } else if (command === "delete") {
      const descendants = new Set([doc.id]);
      let found = true;
      while (found) {
        found = false;
        for (const item of documents)
          if (
            descendants.has(item.parent_document_id) &&
            !descendants.has(item.id)
          ) {
            descendants.add(item.id);
            found = true;
          }
      }
      const children = documents.filter(
        (d) => d.id !== doc.id && descendants.has(d.id),
      );
      showDialog(
        `${dialogHeader("删除文档及子文档？")}<div class="dialog-body"><p>将删除「${esc(doc.title)}」${children.length ? `及其 ${children.length} 个子文档` : ""}。</p><p>此操作只影响当前样板标签页。刷新页面可恢复初始样例。</p></div>${dialogFooter('<button class="button" data-dialog-close>取消</button><button class="button danger" data-delete-confirm>删除样板文档</button>')}`,
        (d) => {
          d.querySelector("[data-delete-confirm]").onclick = () => {
            const keys = new Set([doc.key, ...children.map((c) => c.key)]);
            for (let i = documents.length - 1; i >= 0; i--)
              if (keys.has(documents[i].key)) documents.splice(i, 1);
            knowledge.editor = null;
            knowledge.draft = null;
            closeDialog();
            if (!documents.length) ctx.setScenario("empty");
            else {
              knowledge.selected = documents[0].key;
              ctx.navigate("knowledge", { doc: knowledge.selected });
            }
          };
        },
      );
      return;
    } else return;
    refresh(
      ctx,
      command === "edit" || command === "resume"
        ? "#knowledge-source"
        : command === "info"
          ? "[data-knowledge=info]"
          : command === "close-tree"
            ? "[data-knowledge=open-tree]"
            : command === "open-tree"
              ? "input[name=knowledge-title]"
              : undefined,
    );
  };
}

const projects = [
  {
    id: "atlas",
    name: "atlas",
    description:
      "构建让 Agent 团队共同工作的项目空间。任务、项目资料与讨论在同一处保持上下文。",
    lifecycle: "active",
    version: "12",
  },
  {
    id: "research-notes",
    name: "research-notes",
    description: "研究资料、接口约定与阶段结论。",
    lifecycle: "active",
    version: "5",
  },
  {
    id: "agent-runtime",
    name: "agent-runtime",
    description: "运行时行为、工具调用与执行结果的验证工作。",
    lifecycle: "active",
    version: "9",
  },
  {
    id: "archive-2025",
    name: "archive-2025",
    description: "已归档的探索记录，可继续查阅。",
    lifecycle: "archived",
    version: "3",
  },
];
function projectRow(ctx, p) {
  return `<div class="dashboard-project-row"><div class="dashboard-project-mark" aria-hidden="true">${icon(p.lifecycle === "archived" ? "file" : "grid", 20)}</div><div class="dashboard-project-description"><a href="${esc(href(ctx, "project", { project: p.id, project_name: p.name, project_lifecycle: p.lifecycle, project_description: p.description }))}" data-dashboard-page="project" data-project="${p.id}">${esc(ctx.long && p.id === "atlas" ? "atlas-cross-region-agent-collaboration-platform-2026" : p.name)}</a><p>${esc(p.description)}</p></div>${status(p.lifecycle === "archived" ? "已归档" : "活跃", p.lifecycle === "archived" ? "neutral" : "success")}<a class="icon-button" href="${esc(href(ctx, "project", { project: p.id, project_name: p.name, project_lifecycle: p.lifecycle, project_description: p.description }))}" data-dashboard-page="project" data-project="${p.id}" aria-label="进入 ${esc(p.name)}">${icon("arrow")}</a></div>`;
}
function renderDashboard(ctx) {
  const gate = stateGate(ctx, "项目目录");
  return `<section class="dashboard-page"><header class="dashboard-heading"><h1>首页</h1><p>从一个项目继续工作。</p></header><section class="dashboard-projects" aria-labelledby="dashboard-project-title"><div class="dashboard-section-heading"><h2 id="dashboard-project-title">你的项目</h2><a class="dashboard-text-link" href="${esc(href(ctx, "projects"))}" data-dashboard-page="projects">查看全部项目 ${icon("arrow")}</a></div>${gate || (ctx.scenario === "empty" ? `<div class="dashboard-empty"><h3>还没有项目</h3><p>项目将任务、讨论和资料组织在一起。</p><a class="button primary" href="${esc(href(ctx, "projects"))}" data-dashboard-page="projects">前往项目列表</a></div>` : `<div class="dashboard-project-list">${projects.map((p) => projectRow(ctx, p)).join("")}</div>`)}</section><p class="dashboard-data-note">按项目目录顺序展示 · 合成样板数据</p></section>`;
}
function renderProject(ctx) {
  const base =
    projects.find((p) => p.id === query().get("project")) || projects[0];
  const current = {
    ...base,
    name: ctx.params?.get("project_name") || base.name,
    lifecycle: ctx.params?.get("project_lifecycle") || base.lifecycle,
    description: ctx.params?.get("project_description") || base.description,
  };
  const archived =
    ctx.scenario === "readonly" || current.lifecycle === "archived";
  const archiving = current.lifecycle === "archiving";
  const entries = [
    ["tasks", "任务工作台", "组织任务，查看当前工作与执行结果。", "grid"],
    ["meetings", "会议", "围绕项目讨论，形成可跟进的决定。", "message"],
    ["inbox", "项目待办", "审阅来自本项目会议的提案与决策。", "check"],
    ["knowledge", "知识库", "查阅项目资料，维护文档与原文件。", "file"],
    ["artifacts", "产物", "阅读执行成果，核对内容与来源。", "file"],
  ];
  return `<section class="dashboard-page dashboard-project-page"><div class="dashboard-overview-heading"><div><h1>项目概览</h1><p class="dashboard-project-name">${esc(ctx.long ? "atlas-cross-region-agent-collaboration-platform-2026" : current.name)}</p></div>${status(archiving ? "正在归档" : archived ? "已归档" : "活跃", archiving ? "warning" : archived ? "neutral" : "success")}</div>${stateGate(ctx, "项目信息") || `<p class="dashboard-overview-description">${ctx.scenario === "empty" ? "项目尚未填写描述。" : esc(current.description)}</p>${archiving ? banner("项目正在归档", "归档尚未完成。现有内容可继续查阅，修改暂不可用。", "warning") : ""}${archived ? banner("项目已归档", "现有任务和资料仍可查阅。修改操作暂不可用。", "warning") : ""}${ctx.scenario === "unknown" ? banner("项目信息更新结果未知", "保留原请求。先查询结果，再继续修改项目信息。", "warning", '<button class="button" data-dashboard-check>查询原请求</button>') : ""}<section class="dashboard-continue"><h2>继续工作</h2><div class="dashboard-entry-list">${entries.map(([page, title, detail, symbol]) => `<a href="${esc(href(ctx, page, { scenario: archived || archiving ? "readonly" : "normal" }))}" data-dashboard-page="${page}" data-entry-scenario="${archived || archiving ? "readonly" : "normal"}" class="dashboard-work-entry"><span class="dashboard-entry-icon">${icon(symbol, 21)}</span><span><strong>${title}</strong><span>${detail}</span></span>${icon("arrow")}</a>`).join("")}</div></section><section class="dashboard-project-information"><div class="dashboard-section-heading"><h2>项目信息</h2><button class="button quiet" data-project-info>查看基本信息</button></div><dl><dt>状态</dt><dd>${archiving ? "正在归档 · 只读" : archived ? "已归档 · 只读" : "活跃"}</dd><dt>名称</dt><dd>${esc(current.name)}</dd><dt>说明</dt><dd>${esc(current.description)}</dd></dl></section>`}<p class="dashboard-data-note">合成样板 · 项目状态与入口按现有 Project 字段和路由组织</p></section>`;
}
function bindDashboard(ctx) {
  document.querySelector("#main").onclick = (e) => {
    const link = e.target.closest("[data-dashboard-page]");
    if (link) {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      ctx.navigate(link.dataset.dashboardPage, {
        ...(link.dataset.project
          ? {
              project: link.dataset.project,
              project_name:
                projects.find((p) => p.id === link.dataset.project)?.name || "",
              project_lifecycle:
                projects.find((p) => p.id === link.dataset.project)
                  ?.lifecycle || "active",
              project_description:
                projects.find((p) => p.id === link.dataset.project)
                  ?.description || "",
            }
          : {}),
        ...(link.dataset.entryScenario
          ? { scenario: link.dataset.entryScenario }
          : {}),
      });
    }
    if (e.target.closest("[data-dashboard-check]")) {
      ctx.setScenario("normal");
    }
    if (e.target.closest("[data-project-info]")) {
      ctx.navigate("settings");
    }
  };
}

const artifactFixtures = [
  {
    artifact_ref: {
      project_id: projectID,
      artifact_id: "0198a000-3000-7000-8000-000000000001",
      file_id: "0198a000-3000-7000-8000-000000000101",
    },
    kind: "generated",
    name: "交付检查记录.md",
    description: "本次工作结果与验证范围",
    object: {
      media_type: "text/markdown",
      byte_size: "546",
      sha256: "a".repeat(64),
    },
    created_by_agent_id: "0198a000-4000-7000-8000-000000000001",
    source_execution_id: "0198a000-4000-7000-8000-000000000002",
    created_at: "2026-10-15T09:40:00Z",
    version: "1",
    text: "# 交付检查记录\n\n## 已完成\n\n项目资料已整理到知识库，原文件与内容版本可以对应查阅。\n\n## 验证范围\n\n- 检查阅读与返回路径。\n- 检查错误状态的恢复操作。\n- 核对结果与任务原始目标。\n\n## 尚未完成\n\n此文本为 F0 合成样板，不代表真实任务执行或后端操作已完成。",
  },
  {
    artifact_ref: {
      project_id: projectID,
      artifact_id: "0198a000-3000-7000-8000-000000000002",
      file_id: "0198a000-3000-7000-8000-000000000102",
    },
    kind: "user_upload",
    name: "接口边界说明.txt",
    description: "随项目讨论提交的原始资料",
    object: {
      media_type: "text/plain",
      byte_size: "208",
      sha256: "b".repeat(64),
    },
    created_by_user_id: userID,
    created_at: "2026-10-14T15:20:00Z",
    version: "1",
    text: "接口边界说明\n\n产物身份由项目、产物和文件共同组成。读取时保持原始引用，不把目录中另一个同名文件当作当前结果。\n\n本页只演示现有列表和文本读取能力。",
  },
  {
    artifact_ref: {
      project_id: projectID,
      artifact_id: "0198a000-3000-7000-8000-000000000003",
      file_id: "0198a000-3000-7000-8000-000000000103",
    },
    kind: "generated",
    name: "执行结果打包.zip",
    description: "二进制文件，仅展示元信息",
    object: {
      media_type: "application/zip",
      byte_size: "18432",
      sha256: "c".repeat(64),
    },
    created_by_agent_id: "0198a000-4000-7000-8000-000000000001",
    source_execution_id: "0198a000-4000-7000-8000-000000000002",
    created_at: "2026-10-13T08:00:00Z",
    version: "1",
    text: null,
  },
];
artifactFixtures.forEach((a) => {
  if (a.text !== null) a.object.byte_size = String(utf8.encode(a.text).length);
});
let artifactFilter = "all";
function renderArtifacts(ctx) {
  const active = artifactFixtures.find(
    (a) => a.artifact_ref.artifact_id === query().get("artifact"),
  );
  const rows = artifactFixtures.filter(
    (a) => artifactFilter === "all" || a.kind === artifactFilter,
  );
  const size = (bytes) =>
    Number(bytes) < 1024
      ? `${bytes} B`
      : `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(Number(bytes) / 1024)} KiB`;
  const gate = stateGate(ctx, "项目产物");
  return `<section class="dashboard-page dashboard-artifacts"><header class="dashboard-section-heading"><div><h1>产物</h1><p class="muted small">项目中的执行成果与文件</p></div>${active ? `<a class="button" href="${esc(href(ctx, "artifacts", { artifact: "" }))}" data-artifact-back>${icon("back")} 返回列表</a>` : ""}</header>${gate || (ctx.scenario === "empty" ? '<div class="dashboard-empty"><h2>还没有产物</h2><p>任务执行生成的产物与已有附件将出现在这里。</p></div>' : active ? `<article class="dashboard-artifact-reader"><header><h2>${esc(active.name)}</h2><p>${esc(active.description)}</p></header><div class="dashboard-artifact-meta"><span>${active.kind === "generated" ? "执行生成" : "用户上传"}</span><span>${esc(active.object.media_type)}</span><span>${size(active.object.byte_size)}</span><span>${date(active.created_at)}</span></div>${active.text !== null ? `<pre class="knowledge-prose is-markdown">${esc(active.text)}</pre><p class="dashboard-data-note">已完整读取 · 文本按原文显示</p>` : banner("此文件不支持在线预览", "当前产物读取投影为文件引用。本样板只展示真实元信息，不伪造二进制预览。")}<details class="dashboard-artifact-details"><summary>来源与文件标识</summary><dl><dt>产物</dt><dd>${esc(active.artifact_ref.artifact_id)}</dd><dt>文件</dt><dd>${esc(active.artifact_ref.file_id)}</dd><dt>来源</dt><dd>${active.source_execution_id ? "执行 " + esc(active.source_execution_id) : "用户上传"}</dd><dt>大小</dt><dd>${active.object.byte_size} 字节</dd></dl></details></article>` : `<div class="dashboard-artifact-toolbar"><label>来源 <select name="artifact-kind"><option value="all" ${artifactFilter === "all" ? "selected" : ""}>全部来源</option><option value="generated" ${artifactFilter === "generated" ? "selected" : ""}>执行生成</option><option value="user_upload" ${artifactFilter === "user_upload" ? "selected" : ""}>用户上传</option></select></label><span class="muted small">当前项目</span></div><div class="dashboard-artifact-list">${rows.map((a) => `<a class="dashboard-artifact-row" href="${esc(href(ctx, "artifacts", { artifact: a.artifact_ref.artifact_id }))}" data-artifact="${esc(a.artifact_ref.artifact_id)}"><span class="dashboard-project-mark">${icon("file", 21)}</span><span class="dashboard-artifact-name"><strong>${esc(ctx.long ? "Atlas 跨团队协作与交付检查——" + a.name : a.name)}</strong><span>${esc(a.description)}</span></span><span class="dashboard-artifact-size">${size(a.object.byte_size)}</span><span class="dashboard-artifact-date">${date(a.created_at)}</span>${icon("arrow")}</a>`).join("")}</div>`)}<p class="dashboard-data-note">合成样板 · 列表与读取对应 ProjectArtifactsAPI.list / read</p></section>`;
}
function bindArtifacts(ctx) {
  const main = document.querySelector("#main");
  const filter = main.querySelector("[name=artifact-kind]");
  if (filter)
    filter.onchange = (e) => {
      artifactFilter = e.target.value;
      ctx.render();
      document.querySelector("[name=artifact-kind]")?.focus();
    };
  main.onclick = (e) => {
    const link = e.target.closest("[data-artifact], [data-artifact-back]");
    if (!link || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    ctx.navigate("artifacts", { artifact: link.dataset.artifact || "" });
  };
}

export const contentPages = {
  dashboard: {
    title: "首页",
    project: false,
    render: renderDashboard,
    bind: bindDashboard,
  },
  project: {
    title: "项目概览",
    project: true,
    render: renderProject,
    bind: bindDashboard,
  },
  artifacts: {
    title: "产物",
    project: true,
    render: renderArtifacts,
    bind: bindArtifacts,
  },
  knowledge: {
    title: "知识库",
    project: true,
    render: renderKnowledge,
    bind: bindKnowledge,
  },
};
