import { test, expect } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  createRenameDiagnostic,
  renameOriginalCompleted,
  observeRename,
  type RenamePhase,
} from "./knowledge-owner-rename.native";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_KNOWLEDGE_OWNER_RENAME_WEB_EVIDENCE!;
const dist = process.env.AGENTEAM_KNOWLEDGE_OWNER_RENAME_WEB_DIST!;
const inputHash = process.env.AGENTEAM_KNOWLEDGE_OWNER_RENAME_WEB_INPUT_HASH!;
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
type Material = {
  email: string;
  password: string;
  user_id: string;
  username: string;
  project_id: string;
  project_name: string;
  document_id: string;
  text: string;
  title: string;
  input_hash: string;
};
const schemaProgram = String.raw`
import hashlib,json,pathlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 root=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); base=root/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ('common.json','knowledge-owner.json','knowledge-content.json','knowledge-tree-commands.json')}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  meta=json.loads(path.read_bytes()); raw=(evidence/meta['body_file']).read_bytes()
  assert meta['body_file']=='body-'+meta['body_sha256']+'.json' and hashlib.sha256(raw).hexdigest()==meta['body_sha256']
  assert meta['source_run']=='TestKnowledgeOwnerRenameWeb' and meta['method'] in ('GET','POST') and meta['status']==200
  stem='/api/v1/projects/{project_id}/knowledge/documents'; endpoint=meta['path']
  if endpoint.endswith('/rename'): name='knowledge-tree-commands.json'; route=stem+'/{document_id}/rename'
  elif endpoint.endswith('/content'): name='knowledge-content.json'; route=stem+'/{document_id}/content'
  elif endpoint.endswith('/children'): name='knowledge-owner.json'; route=stem+'/children'
  elif endpoint.endswith('/ancestors'): name='knowledge-owner.json'; route=stem+'/{document_id}/ancestors'
  else: name='knowledge-owner.json'; route=stem+'/{document_id}'
  ref=docs[name]['paths'][route][meta['method'].lower()]['responses']['200']['content']['application/json']['schema']
  schema=dict(docs[name]); schema['$id']=(base/name).as_uri(); schema.update(ref)
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw)); count+=1
 assert count>0; print(count)
except Exception:
 print('KNOWLEDGE_BODY_SCHEMA_FAILED',file=sys.stderr); sys.exit(1)
`;

test("[rename] Knowledge Owner existing-document rename", async ({ page }) => {
  const data: Material = JSON.parse(
    readFileSync(
      join(directory, "knowledge-owner-rename-material.json"),
      "utf8",
    ),
  );
  expect(data.input_hash).toBe(inputHash);
  const diagnostic = createRenameDiagnostic({
    evidence,
    inputHash,
    project: data.project_id,
    documents: [data.document_id],
  });
  let stage: RenamePhase = "login";
  let observer: Awaited<ReturnType<typeof observeRename>> | undefined;
  const enter = (value: RenamePhase) => {
    diagnostic.complete(stage);
    stage = value;
    diagnostic.enter(value);
  };
  diagnostic.enter(stage);
  let terminal: any;
  try {
    await page.emulateMedia({
      colorScheme: "light",
      reducedMotion: "no-preference",
    });
    await page.goto("/projects");
    await expect(page.locator("#login-email")).toBeVisible();
    try {
      await page.locator("#login-email").fill(data.email);
      await page.locator("#login-password").fill(data.password);
    } catch {
      throw Error("PRIVATE_KNOWLEDGE_LOGIN_INPUT_FAILED");
    }
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "退出登录", exact: true }),
    ).toBeEnabled();
    await expect(
      page.getByRole("list", { name: "项目列表", exact: true }),
    ).toBeVisible();
    enter("project-entry");
    await page
      .getByRole("list", { name: "项目列表", exact: true })
      .getByRole("link", { name: data.project_name, exact: true })
      .click();
    await expect(
      page.getByRole("navigation", { name: "项目导航", exact: true }),
    ).toBeVisible();
    observer = await observeRename(page, {
      root,
      dist,
      project: data.project_id,
      documents: [data.document_id],
      user: data.user_id,
      title: data.title,
      evidence,
      inputHash,
      diagnostic,
    });
    await page
      .getByRole("navigation", { name: "项目导航", exact: true })
      .getByRole("link", { name: "知识库", exact: true })
      .click();
    const area = page.getByRole("region", { name: "项目知识库", exact: true });
    await expect(
      area.getByRole("heading", { name: "选择一篇文档", exact: true }),
    ).toBeVisible();
    const tree = area.getByRole("tree", { name: "知识库文档", exact: true });
    const document = tree.locator(`[data-tree-id="${data.document_id}"]`);
    await expect(document).toHaveAttribute("aria-selected", "false");
    enter("first-content");
    await document.focus();
    await page.keyboard.press("Enter");
    const text = area.locator("pre.canonical-text");
    await expect.poll(() => text.textContent()).toBe(data.text);
    await expect(area.locator(".document-facts")).toContainText(data.user_id);
    await expect(
      area.getByRole("button", { name: "改名", exact: true }),
    ).toBeEnabled();
    enter("rename");
    await area.getByRole("button", { name: "改名", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "文档改名", exact: true });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("textbox").fill(data.title);
    await dialog.getByRole("button", { name: "保存标题", exact: true }).click();
    enter("current-read");
    await expect(dialog).toContainText("改名已确认；已读取当前文档。");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(area.locator("h2")).toHaveText(data.title);
    await expect(document).toContainText(data.title);
    await expect(area.locator(".document-facts")).toContainText("2");
    await expect.poll(() => text.textContent()).toBe(data.text);
    expect(await area.locator("script").count()).toBe(0);
    await expect(
      area.getByRole("button", { name: "从开头重读", exact: true }),
    ).toBeEnabled();
    await page.screenshot({
      path: join(evidence, "knowledge-rename-current.png"),
    });
    enter("observer-idle");
    await expect.poll(() => observer!.idle()).toBe(true);
    enter("observer-finish");
    terminal = await observer.finish();
    enter("joint-proof");
    expect(terminal.pw_failed).toBe(false);
    expect(terminal.pw_pending).toBe(0);
    const records = readdirSync(evidence)
      .filter((name) => /^response-\d+\.json$/.test(name))
      .sort()
      .map((name) => JSON.parse(readFileSync(join(evidence, name), "utf8")));
    expect(records).toHaveLength(9);
    expect(terminal.requests).toHaveLength(records.length);
    expect(terminal.native.rows).toHaveLength(records.length);
    expect(terminal.publication.rows).toHaveLength(records.length);
    const ids = new Set<string>(),
      kinds = new Set<string>();
    let writes = 0,
      currentBodies = 0;
    for (const record of records) {
      expect(record.input_hash).toBe(inputHash);
      expect(record.source_run).toBe("TestKnowledgeOwnerRenameWeb");
      expect(ids.has(record.request_id)).toBe(false);
      ids.add(record.request_id);
      const matches = terminal.requests.filter(
        (row: any) => row.request_id === record.request_id,
      );
      expect(matches).toHaveLength(1);
      // Counts are from this Request's live events. The failed branch must
      // never create/abandon a Response.finished() Promise.
      const original = matches[0];
      expect(
        record.method === "POST" ? ["finished"] : ["finished", "failed"],
      ).toContain(original.terminal);
      expect(original.finished_calls).toBe(
        original.terminal === "finished" ? 1 : 0,
      );
      expect(original.finished_count).toBe(
        original.terminal === "finished" ? 1 : 0,
      );
      expect(original.failed_count).toBe(
        original.terminal === "failed" ? 1 : 0,
      );
      expect(
        renameOriginalCompleted(
          terminal.native,
          terminal.publication,
          matches[0],
          record,
        ),
      ).toBe(true);
      const raw = readFileSync(join(evidence, record.body_file));
      expect(record.body_file).toBe(`body-${record.body_sha256}.json`);
      expect(createHash("sha256").update(raw).digest("hex")).toBe(
        record.body_sha256,
      );
      const body = JSON.parse(raw.toString("utf8"));
      const native = terminal.native.rows.find(
        (row: any) => row.request_id === record.request_id,
      );
      const published = terminal.publication.rows.find(
        (row: any) => row.id === native.call_id,
      );
      expect(published.typed).toEqual(
        published.method === "ancestors" ? body.items : body,
      );
      kinds.add(published.method);
      if (record.method === "POST") {
        writes++;
        expect(published.method).toBe("startRename");
        expect(published.same_receipt).toBe(true);
        expect(published.confirmed).toBe(true);
        expect(body.document).toMatchObject({
          id: data.document_id,
          project_id: data.project_id,
          title: data.title,
          content_version: "2",
        });
      }
      if (
        record.path.endsWith("/content") &&
        body.document.content_version === "2"
      ) {
        expect(body.text).toEqual({
          text: data.text,
          next_byte_offset: String(Buffer.byteLength(data.text)),
          truncated: false,
        });
        currentBodies++;
      }
    }
    expect([...kinds].sort()).toEqual([
      "ancestors",
      "children",
      "get",
      "readContent",
      "startRename",
    ]);
    expect(writes).toBe(1);
    expect(currentBodies).toBe(1);

    enter("schema");
    const schema = spawnSync(
      process.env.AGENTEAM_KNOWLEDGE_OWNER_RENAME_WEB_SCHEMA_PYTHON!,
      ["-c", schemaProgram, root, evidence],
      { encoding: "utf8", timeout: 6000, maxBuffer: 4096 },
    );
    writeFileSync(
      join(evidence, "schema-validation.json"),
      JSON.stringify({
        status: schema.status,
        signal: schema.signal,
        output: schema.stdout,
        diagnostic: schema.stderr,
        failed_to_run: !!schema.error,
      }),
      { mode: 0o600 },
    );
    expect(schema.status).toBe(0);
    expect(schema.stdout.trim()).toBe(String(records.length));
    diagnostic.complete("schema");
    expect(diagnostic.healthy()).toBe(true);
    writeFileSync(
      join(directory, "knowledge-owner-rename-result.json"),
      JSON.stringify({
        input_hash: inputHash,
        completed: true,
        rename_confirmed: true,
        current_read: true,
        original_consumed: true,
        typed_published: true,
        observers_joined: true,
        responses: records.length,
      }),
      { mode: 0o600 },
    );
  } catch {
    // Preserve the actual first phase and known state before any tail await.
    // finish/close may append later observations but cannot rewrite this copy.
    diagnostic.firstFailure(page.isClosed(), !!terminal);
    if (observer && !terminal) {
      try {
        terminal = await observer.finish();
        diagnostic.event("cleanup-return");
      } catch {
        diagnostic.event("cleanup-reject");
        /* Original stage remains failed. */
      }
    }
    throw Error(
      `KNOWLEDGE_RENAME_STAGE_${stage.toUpperCase().replaceAll("-", "_")}`,
    );
  }
});
