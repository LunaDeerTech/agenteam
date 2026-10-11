import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";
import {
  configurationKeys,
  directoryKeys,
} from "./agent-configuration.helpers";

export type AgentEditMaterial = {
  mode: "changed-noop-and-clear" | "lost-update-confirmation-only-lookup";
  cookie: string;
  project_id: string;
  agent_id: string;
  route: string;
  version: "1";
  initial: { name: string; description: string; instructions: string };
};
export function agentEditMaterial(): AgentEditMaterial {
  const value: AgentEditMaterial = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "agent-edit-material.json"),
      "utf8",
    ),
  );
  expect(value.version).toBe("1");
  expect(value.initial.name).toMatch(/^[A-Za-z][A-Za-z0-9-]{2,24}$/);
  expect(value.initial.description.length).toBeGreaterThan(0);
  expect(value.initial.instructions.length).toBeGreaterThan(0);
  return value;
}
export function agentEditInputs(data: AgentEditMaterial) {
  const name = `${data.initial.name}-edited`;
  return data.mode === "lost-update-confirmation-only-lookup"
    ? [{ expected_version: "1", request: { name, description: "" } }]
    : [
        {
          expected_version: "1",
          request: {
            name,
            description: "Browser saved Agent description.",
            instructions:
              "Browser saved Agent instructions.\nPreserve the complete text.",
          },
        },
        { expected_version: "2", request: { name } },
        {
          expected_version: "2",
          request: { description: "", instructions: "" },
        },
      ];
}

export async function observeAgentEdit(page: Page, data: AgentEditMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const inputs = agentEditInputs(data);
  const keys = new Set<string>();
  let updates = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url());
    const path = url.pathname;
    if (!path.startsWith("/api/")) return undefined;
    const method = request.method();
    const globals: Record<string, string> = {
      "GET /api/v1/session": "session",
      "GET /api/v1/me": "profile",
      "GET /api/v1/me/preferences": "preferences",
      "GET /api/v1/auth/bootstrap": "bootstrap",
      "GET /api/v1/projects/resolve": "resolve",
      "GET /api/v1/projects": "projects",
    };
    if (globals[`${method} ${path}`]) return globals[`${method} ${path}`];
    if (method === "GET" && path === prefix) return "project";
    if (!path.startsWith(prefix + "/"))
      throw Error("UNEXPECTED_AGENT_EDIT_SCOPE");
    if (
      method === "PATCH" &&
      path === `${prefix}/agents/${data.agent_id}/configuration` &&
      !url.search
    ) {
      expect(updates).toBeLessThan(inputs.length);
      expect(request.postDataJSON()).toEqual(inputs[updates]);
      const key = request.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      keys.add(key);
      updates++;
      return "update";
    }
    if (
      method === "POST" &&
      path === prefix + "/agent-commands/lookup" &&
      !url.search
    ) {
      expect(data.mode).toBe("lost-update-confirmation-only-lookup");
      expect(request.postDataJSON()).toEqual({
        command: "agent.update",
        target_id: data.agent_id,
        ...inputs[0],
      });
      return "lookup";
    }
    if (method === "GET" && path === prefix + "/agents")
      return "agent-directory";
    if (
      method === "GET" &&
      path === `${prefix}/agents/${data.agent_id}/configuration` &&
      !url.search
    )
      return "agent-configuration";
    // Creation, other Agents, Model/authorization edits and unrelated writes
    // are outside this original Update profile.
    throw Error("UNOBSERVED_AGENT_EDIT_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_AGENT_EDIT_WEB_DIST!,
    evidence: process.env.AGENTEAM_AGENT_EDIT_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_AGENT_EDIT_WEB_INPUT_HASH!,
    prefix: "agent-edit",
    endpoint: classify,
    cut:
      data.mode === "lost-update-confirmation-only-lookup"
        ? "update"
        : undefined,
    lookupCommand: "agent.update",
    agentEditWrites: true,
  });
  return {
    ...seen,
    requireReceipt(
      receipt: Record<string, any>,
      index: number,
      before: Record<string, any>,
    ) {
      expect(Object.keys(receipt).sort()).toEqual([
        "agent",
        "changed",
        "event_ids",
      ]);
      expect(Object.keys(receipt.agent).sort()).toEqual(configurationKeys);
      const changed =
        data.mode === "lost-update-confirmation-only-lookup" || index !== 1;
      expect(receipt.changed).toBe(changed);
      expect(receipt.event_ids).toHaveLength(changed ? 1 : 0);
      const input = inputs[index]!;
      const { updated_at, ...unchanged } = before;
      expect(receipt.agent).toEqual({
        ...unchanged,
        ...input.request,
        normalized_name: (input.request.name ?? before.name).toLowerCase(),
        version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
        updated_at: receipt.agent.updated_at,
      });
      if (changed) expect(receipt.agent.updated_at > updated_at).toBe(true);
      else expect(receipt.agent.updated_at).toBe(updated_at);
    },
    requireDirectory(configuration: Record<string, any>) {
      const rows = seen
        .agentRead("agent-directory")
        .items.filter((row: any) => row.id === data.agent_id);
      expect(rows).toHaveLength(1);
      expect(Object.keys(rows[0]).sort()).toEqual(directoryKeys);
      for (const key of directoryKeys)
        expect(rows[0][key]).toEqual(configuration[key]);
    },
    requireCounts() {
      expect(seen.requests("update")).toHaveLength(inputs.length);
      expect(seen.requests("lookup")).toHaveLength(
        data.mode === "lost-update-confirmation-only-lookup" ? 1 : 0,
      );
      expect(seen.requests("create")).toHaveLength(0);
    },
  };
}

export async function agentEditScreenshot(
  page: Page,
  data: AgentEditMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const directory = process.env.AGENTEAM_AGENT_EDIT_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `agent-edit-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
