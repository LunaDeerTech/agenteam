import { test, expect } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import {
  createKnowledgeReadDiagnostic,
  knowledgeOriginalCompleted,
  observeKnowledge,
  type KnowledgeReadPhase,
} from "./knowledge-owner-read.native";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_KNOWLEDGE_OWNER_WEB_EVIDENCE!;
const dist = process.env.AGENTEAM_KNOWLEDGE_OWNER_WEB_DIST!;
const inputHash = process.env.AGENTEAM_KNOWLEDGE_OWNER_WEB_INPUT_HASH!;
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
type Material = {
  email: string;
  password: string;
  user_id: string;
  username: string;
  project_id: string;
  project_name: string;
  parent_id: string;
  child_id: string;
  parent_text: string;
  child_text: string;
  input_hash: string;
};
const schemaProgram = String.raw`
import hashlib,json,pathlib,sys
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 root=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); base=root/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ('common.json','knowledge-owner.json','knowledge-content.json')}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 count=0
 for path in sorted(evidence.glob('response-*.json')):
  meta=json.loads(path.read_bytes()); raw=(evidence/meta['body_file']).read_bytes()
  assert meta['body_file']=='body-'+meta['body_sha256']+'.json' and hashlib.sha256(raw).hexdigest()==meta['body_sha256']
  assert meta['source_run']=='TestKnowledgeOwnerReadWeb' and meta['method']=='GET' and meta['status']==200
  stem='/api/v1/projects/{project_id}/knowledge/documents'; endpoint=meta['path']
  if endpoint.endswith('/content'): name='knowledge-content.json'; route=stem+'/{document_id}/content'
  elif endpoint.endswith('/children'): name='knowledge-owner.json'; route=stem+'/children'
  elif endpoint.endswith('/ancestors'): name='knowledge-owner.json'; route=stem+'/{document_id}/ancestors'
  else: name='knowledge-owner.json'; route=stem+'/{document_id}'
  ref=docs[name]['paths'][route]['get']['responses']['200']['content']['application/json']['schema']
  schema=dict(docs[name]); schema['$id']=(base/name).as_uri(); schema.update(ref)
  Draft202012Validator(schema,registry=registry).validate(json.loads(raw)); count+=1
 assert count>0; print(count)
except Exception:
 print('KNOWLEDGE_BODY_SCHEMA_FAILED',file=sys.stderr); sys.exit(1)
`;

test("[read] Knowledge Owner existing-document read", async ({ page }) => {
  const data: Material = JSON.parse(
    readFileSync(join(directory, "knowledge-owner-material.json"), "utf8"),
  );
  expect(data.input_hash).toBe(inputHash);
  const diagnostic = createKnowledgeReadDiagnostic({
    evidence,
    inputHash,
    project: data.project_id,
    documents: [data.parent_id, data.child_id],
  });
  let stage: KnowledgeReadPhase = "login";
  let observer: Awaited<ReturnType<typeof observeKnowledge>> | undefined;
  const enter = (value: KnowledgeReadPhase) => {
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
    observer = await observeKnowledge(page, {
      root,
      dist,
      project: data.project_id,
      documents: [data.parent_id, data.child_id],
      user: data.user_id,
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
    const parent = tree.locator(`[data-tree-id="${data.parent_id}"]`),
      child = tree.locator(`[data-tree-id="${data.child_id}"]`);
    await expect(parent).toHaveAttribute("aria-selected", "false");
    enter("tree-keyboard");
    await parent.focus();
    await page.keyboard.press("ArrowRight");
    await expect(parent).toHaveAttribute("aria-expanded", "true");
    await expect(child).toBeVisible();
    await page.keyboard.press("ArrowDown");
    await expect(child).toBeFocused();
    await page.keyboard.press("Enter");
    const text = area.locator("pre.canonical-text");
    enter("first-content");
    await expect.poll(() => text.textContent()).toBe("a".repeat(65535));
    await expect(area.locator(".document-facts")).toContainText(data.user_id);
    await expect(
      area.getByRole("navigation", { name: "面包屑", exact: true }),
    ).toContainText("父文档");
    await expect(
      area.getByRole("button", { name: "下一段", exact: true }),
    ).toBeEnabled();
    expect(
      await text.evaluate((node) => node.scrollWidth > node.clientWidth),
    ).toBe(true);
    await text.focus();
    await page.keyboard.press("End");
    await page.screenshot({
      path: join(evidence, "knowledge-desktop-light.png"),
    });
    enter("utf8-next");
    await area.getByRole("button", { name: "下一段", exact: true }).click();
    await expect.poll(() => text.textContent()).toBe("中🙂\n");
    await expect(area.locator(".content-range")).toContainText("65535–65543");
    await expect(
      area.getByRole("button", { name: "下一段", exact: true }),
    ).toBeDisabled();
    await expect(
      area.getByRole("button", { name: "上一段", exact: true }),
    ).toBeEnabled();
    enter("parent-read");
    await parent.focus();
    await page.keyboard.press("Enter");
    await expect.poll(() => text.textContent()).toBe(data.parent_text);
    expect(await area.locator("script").count()).toBe(0);
    enter("drawer");
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    const trigger = area.getByRole("button", { name: "文档树", exact: true });
    await trigger.click();
    const drawer = page.getByRole("dialog", { name: "文档树", exact: true });
    await expect(drawer).toBeVisible();
    const drawerParent = drawer.locator(`[data-tree-id="${data.parent_id}"]`),
      drawerChild = drawer.locator(`[data-tree-id="${data.child_id}"]`);
    await drawerParent.focus();
    await page.keyboard.press("ArrowDown");
    await expect(drawerChild).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(drawer).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await expect.poll(() => text.textContent()).toBe("a".repeat(65535));
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBe(true);
    await page.screenshot({
      path: join(evidence, "knowledge-narrow-dark-reduced.png"),
    });
    enter("completion-button");
    await expect(
      area.getByRole("button", { name: "从开头重读", exact: true }),
    ).toBeEnabled();
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
    expect(records).toHaveLength(12);
    expect(terminal.requests).toHaveLength(records.length);
    expect(terminal.native.rows).toHaveLength(records.length);
    expect(terminal.publication.rows).toHaveLength(records.length);
    const ids = new Set<string>(),
      kinds = new Set<string>();
    let nextOffset = false;
    for (const record of records) {
      expect(record.input_hash).toBe(inputHash);
      expect(record.source_run).toBe("TestKnowledgeOwnerReadWeb");
      expect(ids.has(record.request_id)).toBe(false);
      ids.add(record.request_id);
      const matches = terminal.requests.filter(
        (row: any) => row.request_id === record.request_id,
      );
      expect(matches).toHaveLength(1);
      // Counts are from this Request's live events. The failed branch must
      // never create/abandon a Response.finished() Promise.
      const original = matches[0];
      expect(["finished", "failed"]).toContain(original.terminal);
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
        knowledgeOriginalCompleted(
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
      if (
        record.path.endsWith(`/${data.child_id}/content`) &&
        record.query === "byte_offset=65535&max_bytes=65536"
      ) {
        expect(body.text).toEqual({
          text: "中🙂\n",
          next_byte_offset: "65543",
          truncated: false,
        });
        nextOffset = true;
      }
    }
    expect([...kinds].sort()).toEqual([
      "ancestors",
      "children",
      "get",
      "readContent",
    ]);
    expect(nextOffset).toBe(true);
    enter("schema");
    const schema = spawnSync(
      process.env.AGENTEAM_KNOWLEDGE_OWNER_WEB_SCHEMA_PYTHON!,
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
      join(directory, "knowledge-owner-result.json"),
      JSON.stringify({
        input_hash: inputHash,
        completed: true,
        four_get: true,
        tree_keyboard: true,
        utf8_next: true,
        parent_read: true,
        drawer_focus: true,
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
      `KNOWLEDGE_READ_STAGE_${stage.toUpperCase().replaceAll("-", "_")}`,
    );
  }
});
