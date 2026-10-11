import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type AgentMaterial = {
  mode: "default-create-config-and-directory" | "lost-create-confirmation-only-lookup";
  cookie: string;
  project_id: string;
  model_id: string;
  route: string;
  draft: { name: string; description: string; instructions: string };
};
export function agentMaterial(): AgentMaterial {
  return JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
    "agent-configuration-material.json"), "utf8"));
}
const uuid7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export const directoryKeys = [
  "id", "project_id", "name", "display_name", "tag_color", "description",
  "version", "created_at", "updated_at",
].sort();
export const configurationKeys = [
  ...directoryKeys, "normalized_name", "instructions", "inject_agents_md", "model_ref",
  "reasoning_effort", "approval_policy", "approval_model_ref", "allowed_tool_ids",
  "allowed_mount_ids", "allowed_secret_variable_ids", "lifecycle",
].sort();

export async function observeAgentConfiguration(page: Page, data: AgentMaterial) {
  let agentID: string | undefined;
  const prefix = `/api/v1/projects/${data.project_id}`;
  const classify = (request: Request): string | undefined => {
    const { pathname } = new URL(request.url());
    if (!pathname.startsWith("/api/")) return undefined;
    const method = request.method();
    const globals: Record<string, string> = {
      "GET /api/v1/session": "session",
      "GET /api/v1/me": "profile",
      "GET /api/v1/me/preferences": "preferences",
      "GET /api/v1/auth/bootstrap": "bootstrap",
      "GET /api/v1/projects/resolve": "resolve",
      "GET /api/v1/projects": "projects",
    };
    const global = globals[`${method} ${pathname}`];
    if (global) return global;
    if (method === "GET" && pathname === prefix) return "project";
    if (!pathname.startsWith(prefix + "/")) throw Error("UNEXPECTED_AGENT_CONFIGURATION_SCOPE");
    const path = pathname.slice(prefix.length);
    if (method === "POST" && path === "/agents") {
      const body = request.postDataJSON();
      expect(Object.keys(body)).toEqual(["request"]);
      expect(body.request.agent_id).toMatch(uuid7);
      expect(body.request).toEqual({
        agent_id: body.request.agent_id,
        ...data.draft,
        model_ref: data.model_id,
        inject_agents_md: false,
        approval_policy: "default",
        allowed_tool_ids: [],
        allowed_mount_ids: [],
        allowed_secret_variable_ids: [],
      });
      if (agentID) throw Error("AGENT_CREATE_WAS_RETRANSMITTED");
      agentID = body.request.agent_id;
      return "create";
    }
    if (method === "POST" && path === "/agent-commands/lookup") return "lookup";
    if (method === "GET") {
      if (path === "/agents") return "agent-directory";
      if (path === "/available-chat-models") return "agent-models";
      if (agentID && path === `/agents/${agentID}/configuration`) return "agent-configuration";
    }
    // In particular, the compiled Update controls never grant this fixture
    // permission to call PATCH or to observe an agent.update lookup.
    throw Error("UNOBSERVED_AGENT_CONFIGURATION_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_AGENT_CONFIGURATION_WEB_DIST!,
    evidence: process.env.AGENTEAM_AGENT_CONFIGURATION_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_AGENT_CONFIGURATION_WEB_INPUT_HASH!,
    prefix: "agent-configuration",
    endpoint: classify,
    cut: data.mode === "lost-create-confirmation-only-lookup" ? "create" : undefined,
    lookupCommand: "agent.create",
    agentConfiguration: true,
  });
  return {
    ...seen,
    agentID() {
      expect(agentID).toMatch(uuid7);
      return agentID!;
    },
    modelOption() {
      const rows = seen.agentRead("agent-models").items.filter((v: any) => v.id === data.model_id);
      expect(rows).toHaveLength(1);
      const model = rows[0];
      expect(model.scope).toEqual({ kind: "project", project_id: data.project_id });
      expect(model.capabilities.reasoning_efforts).toEqual([]);
      return `${model.name} · ${model.provider_name}（项目）`;
    },
    requireConfiguration(agent: Record<string, any>) {
      expect(Object.keys(agent).sort()).toEqual(configurationKeys);
      expect(agent).toMatchObject({
        id: agentID, project_id: data.project_id, ...data.draft,
        normalized_name: data.draft.name.toLowerCase(),
        display_name: null, tag_color: null, inject_agents_md: false,
        model_ref: data.model_id, reasoning_effort: null, approval_policy: "default",
        approval_model_ref: null, allowed_mount_ids: [], allowed_secret_variable_ids: [],
        lifecycle: "active", version: "1",
      });
      expect(agent.created_at).toBe(agent.updated_at);
      expect(agent.allowed_tool_ids).toHaveLength(1);
      expect(agent.allowed_tool_ids[0]).toMatch(uuid7);
    },
  };
}

export async function agentScreenshot(page: Page, data: AgentMaterial, name: string) {
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const directory = process.env.AGENTEAM_AGENT_CONFIGURATION_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `agent-configuration-${data.mode}-${name}.png`),
    fullPage: true, animations: "disabled",
  });
}
