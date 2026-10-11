import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";
import { configurationKeys, directoryKeys } from "./agent-configuration.helpers";

export type ModelPolicyMaterial = {
  mode: "switch-model-auto-and-clear-approval";
  cookie: string; project_id: string; agent_id: string; route: string; version: "1";
  old_model_id: string; new_model_id: string;
  initial: { name: string; description: string; instructions: string };
};
export function modelPolicyMaterial(): ModelPolicyMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-model-policy-material.json"), "utf8"));
  expect(data.mode).toBe("switch-model-auto-and-clear-approval");
  expect(data.version).toBe("1");
  expect(data.old_model_id).not.toBe(data.new_model_id);
  expect(data.initial.name).toBe("model-policy-agent");
  return data;
}
export function modelPolicyInputs(data: ModelPolicyMaterial) {
  return [
    { expected_version: "1", request: { model_ref: data.new_model_id, reasoning_effort: null, inject_agents_md: false, approval_policy: "auto", approval_model_ref: data.old_model_id } },
    { expected_version: "2", request: { approval_policy: "default", approval_model_ref: null } },
  ];
}
export async function observeModelPolicy(page: Page, data: ModelPolicyMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const inputs = modelPolicyInputs(data), keys = new Set<string>();
  let updates = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname, method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    const globals: Record<string, string> = {
      "GET /api/v1/session": "session", "GET /api/v1/me": "profile",
      "GET /api/v1/me/preferences": "preferences", "GET /api/v1/auth/bootstrap": "bootstrap",
      "GET /api/v1/projects/resolve": "resolve", "GET /api/v1/projects": "projects",
    };
    if (globals[`${method} ${path}`]) return globals[`${method} ${path}`];
    if (method === "GET" && path === prefix) return "project";
    if (!path.startsWith(prefix + "/")) throw Error("AGENT_MODEL_POLICY_SCOPE");
    if (method === "PATCH" && path === `${prefix}/agents/${data.agent_id}/configuration` && !url.search) {
      expect(updates).toBeLessThan(2);
      expect(request.postDataJSON()).toEqual(inputs[updates]);
      const key = request.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false); keys.add(key); updates++;
      return "update";
    }
    if (method === "GET" && path === prefix + "/agents") return "agent-directory";
    if (method === "GET" && path === `${prefix}/agents/${data.agent_id}/configuration` && !url.search) return "agent-configuration";
    if (method === "GET" && path === prefix + "/available-chat-models") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]);
      return "agent-models";
    }
    // Only two original PATCH commands; no Create, Lookup or unrelated write.
    throw Error("UNOBSERVED_AGENT_MODEL_POLICY_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_AGENT_MODEL_POLICY_WEB_DIST!,
    evidence: process.env.AGENTEAM_AGENT_MODEL_POLICY_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_AGENT_MODEL_POLICY_WEB_INPUT_HASH!,
    prefix: "agent-model-policy", endpoint: classify, lookupCommand: "agent.update", agentModelPolicy: true,
  });
  return {
    ...seen,
    requireReceipt(receipt: Record<string, any>, index: number, before: Record<string, any>) {
      expect(Object.keys(receipt).sort()).toEqual(["agent", "changed", "event_ids"]);
      expect(Object.keys(receipt.agent).sort()).toEqual(configurationKeys);
      expect(receipt.changed).toBe(true); expect(receipt.event_ids).toHaveLength(1);
      const { updated_at, ...unchanged } = before;
      expect(receipt.agent).toEqual({ ...unchanged, ...inputs[index]!.request, version: String(index + 2), updated_at: receipt.agent.updated_at });
      expect(receipt.agent.updated_at > updated_at).toBe(true);
    },
    requireDirectory(config: Record<string, any>) {
      const rows = seen.agentRead("agent-directory").items.filter((row: any) => row.id === data.agent_id);
      expect(rows).toHaveLength(1); expect(Object.keys(rows[0]).sort()).toEqual(directoryKeys);
      for (const key of directoryKeys) expect(rows[0][key]).toEqual(config[key]);
    },
    modelOption(id: string) {
      const page = seen.agentRead("agent-models");
      expect(page.next_cursor).toBeNull(); expect(page.items).toHaveLength(2);
      expect(page.items.map((m: any) => m.id).sort()).toEqual([data.old_model_id, data.new_model_id].sort());
      for (const model of page.items) {
        expect(model.scope).toEqual({ kind: "project", project_id: data.project_id });
        expect(model.capabilities).toMatchObject({ input_modalities: ["text"], output_modalities: ["text"], reasoning: false, reasoning_efforts: [], tool_calls: false, parallel_tool_calls: false });
      }
      const model = page.items.find((m: any) => m.id === id)!;
      return `${model.name} · ${model.provider_name}（项目）`;
    },
    requireCounts() {
      expect(seen.requests("update")).toHaveLength(2);
      expect(seen.requests("lookup")).toHaveLength(0);
      expect(seen.requests("create")).toHaveLength(0);
      expect(seen.requests("agent-models")).toHaveLength(2);
    },
  };
}
export async function modelPolicyScreenshot(page: Page, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const directory = process.env.AGENTEAM_AGENT_MODEL_POLICY_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `agent-model-policy-${name}.png`), fullPage: true, animations: "disabled" });
}
