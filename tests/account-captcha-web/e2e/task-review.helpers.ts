import {
  expect,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { createHash } from "node:crypto";
import { join, resolve } from "node:path";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";

export type ReviewMaterial = {
  mode: "review-complete" | "lost-confirmation-lookup-and-session-revocation";
  cookie: string;
  project_id: string;
  task_id: string;
  sprint_id: string;
  title: string;
  version: string;
  worker_id: string;
  reviewer_id: string;
  route: string;
};
export function material(): ReviewMaterial {
  return JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-review-material.json"),
      "utf8",
    ),
  );
}

export async function taskScreenshot(page: Page, name: string) {
  const directory = process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    )
    .toBe(true);
  // Only the owned Task screen is captured. Automatic failure screenshots,
  // traces, private Session material and account/password screens remain off.
  await page.screenshot({
    path: join(directory, `task-review-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}

type Tail = "finished" | "failed" | "closed";
type SkillsHeaderDiagnostic = {
  stage:
    | "joined"
    | "header_promises"
    | "request_gate"
    | "response_id_gate"
    | "handler_pending"
    | "accepted";
  request_fulfilled: boolean;
  response_fulfilled: boolean;
  host_present: boolean;
  host_matches: boolean;
  authority_present: boolean;
  authority_matches: boolean;
  handler_host_matches: boolean;
  cookie_present: boolean;
  response_id_present: boolean;
  response_id_valid: boolean;
};
type Entry = {
  seq: number;
  request: Request;
  name: string;
  status?: number;
  requestID?: string;
  failure?: string;
  responseCount: number;
  finishedCount: number;
  failedCount: number;
  terminal?: Tail;
  settle: (state: Tail) => void;
  done: Promise<Tail>;
  checked: boolean;
  body?: unknown;
  headersReady?: Promise<void>;
  skillsHeaders?: SkillsHeaderDiagnostic;
};

// Preserve paths and the few non-secret query facts used by these reads. Never
// persist cursor values, credentials, headers, request bodies or response data.
function diagnosticURL(value: string): string {
  const url = new URL(value);
  const query = new URLSearchParams();
  for (const [key, item] of url.searchParams) {
    const safe =
      (key === "limit" && /^\d{1,3}$/.test(item)) ||
      (key === "state" &&
        /^(backlog|todo|in_progress|blocked|in_review|done|cancelled)$/.test(
          item,
        )) ||
      (["sprint_id", "milestone_id"].includes(key) &&
        /^[0-9a-f-]{36}$/.test(item));
    query.append(key, safe ? item : "<redacted>");
  }
  return `${url.origin}${url.pathname}${query.size ? `?${query}` : ""}`;
}

type BrowserDiagnostic = {
  seq: number;
  at: number;
  document_at: number;
  kind: string;
  fetch?: number;
  signal?: number;
  signal_source?: string;
  request_id?: string;
  method?: string;
  url?: string;
  status?: number;
  aborted?: boolean;
  done?: boolean;
  bytes?: number;
  error_name?: string;
  route: string;
  visibility: string;
  logout_control: boolean;
  session_check_control: boolean;
};

export async function observeBrowserIO(
  page: Page,
  accept: (event: BrowserDiagnostic) => void,
  metadataScope?: { project: string; skill: string },
) {
  await page.exposeBinding("__taskReviewDiagnostic", (_source, event) => {
    accept(event as BrowserDiagnostic);
  });
  await page.addInitScript((metadataScope?: { project: string; skill: string }) => {
    // These observers return the original fetch/read/cancel promises and the
    // original Response/reader. They do not consume, clone or replace a body.
    const send = (
      window as unknown as {
        __taskReviewDiagnostic: (event: unknown) => Promise<void>;
      }
    ).__taskReviewDiagnostic;
    let sequence = 0;
    let nextFetch = 0;
    let nextSignal = 0;
    const rows = new Map<number, any>();
    const calls: any[] = [];
    const originalCalls = new WeakMap<Promise<any>, any>();
    const identities = new WeakMap<object, string>();
    // Original public arguments and keys stay private to this document. Only
    // hashes and boolean matches can enter the persisted observation.
    const writeInputs = new WeakMap<object, any>();
    let originalCreate: any;
    let originalAgentCreate: any;
    const originalEdits: any[] = [];
    const originalAgentEdits: any[] = [];
    const originalMilestoneEdits: any[] = [];
    const originalSprintEdits: any[] = [];
    let originalSprintStart: any;
    let originalSprintCreate: any;
    let originalMilestoneCreate: any;
    const originalVariableWrites: any[] = [];
    const originalTextWrites: any[] = [];
    const originalSchedulerWrites: any[] = [];
    const tails = new Set<Promise<void>>();
    const restorers: (() => void)[] = [];
    let retired = false,
      failed = false,
      auth: any;
    const host = window as any;
    const canonical = (value: any): any =>
      Array.isArray(value)
        ? value.map(canonical)
        : value !== null && typeof value === "object"
          ? Object.fromEntries(
              Object.keys(value)
                .sort()
                .map((key) => [key, canonical(value[key])]),
            )
          : value;
    const digest = async (bytes: Uint8Array) =>
      [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))]
        .map((v) => v.toString(16).padStart(2, "0"))
        .join("");
    const typedDigest = (value: unknown) =>
      digest(new TextEncoder().encode(JSON.stringify(canonical(value))));
    const identity = () => {
      const value = auth?.personalContext.identity;
      return value
        ? JSON.stringify([value.userID, value.sessionID, value.epoch])
        : "";
    };
    const current = () =>
      auth?.state.phase === "authenticated" &&
      auth?.personalContext.phase === "current";
    function track(
      promise: Promise<any>,
      good: (value: any) => any,
      bad: (error: unknown) => any,
    ) {
      const tail = promise
        .then(good, bad)
        .catch(() => {
          failed = true;
        })
        .then(() => {
          tails.delete(tail);
        });
      tails.add(tail);
    }
    function consumer(
      object: any,
      method: string,
      kind: string,
      path: (args: any[]) => string,
      httpMethod = "GET",
    ) {
      const original = object[method],
        descriptor = Object.getOwnPropertyDescriptor(object, method);
      if (typeof original !== "function" || !descriptor)
        throw Error("TASK_REVIEW_PUBLIC_CONSUMER_MISSING");
      const wrapper = function (this: any, ...args: any[]) {
        if (calls.length >= 192) failed = true;
        const before = identity();
        const call: any = {
          id: calls.length + 1,
          kind,
          method: httpMethod,
          path: path(args),
          bound: 0,
          fulfilled: false,
          settled: false,
          rejected: false,
          current: false,
          not_busy: false,
          typed_digest: null,
          typed: null,
        };
        calls.push(call);
        if (kind === "work.startCreate") {
          if (originalCreate) failed = true;
          originalCreate = {
            call,
            project: args[0],
            draft: JSON.parse(JSON.stringify(args[1])),
          };
          writeInputs.set(call, originalCreate);
        } else if (kind === "work.startReady") {
          writeInputs.set(call, {
            project: args[0],
            task: args[1],
            input: JSON.parse(JSON.stringify(args[2])),
            source: originalCreate,
          });
        } else if (kind === "work.checkOriginal") {
          const progress = auth.workReview.progress;
          writeInputs.set(call, {
            source: originalCreate,
            current:
              progress?.kind === "create" &&
              progress.phase === "uncertain" &&
              progress.contextValid === true &&
              progress.projectID === originalCreate?.project &&
              progress.taskID === originalCreate?.request?.task_id,
          });
        } else if (kind === "edit.startEdit") {
          const original = {
            call,
            project: args[0],
            task: args[1],
            input: JSON.parse(JSON.stringify(args[2])),
          };
          originalEdits.push(original);
          writeInputs.set(call, original);
        } else if (kind === "edit.startReady") {
          writeInputs.set(call, {
            project: args[0],
            task: args[1],
            input: JSON.parse(JSON.stringify(args[2])),
          });
        } else if (kind === "edit.checkOriginal") {
          const source = originalEdits.at(-1);
          const progress = auth.workReview.progress;
          writeInputs.set(call, {
            source,
            current:
              progress?.kind === "edit" &&
              progress.phase === "uncertain" &&
              progress.contextValid === true &&
              progress.projectID === source?.project &&
              progress.taskID === source?.task,
          });
        } else if (kind === "agent.startCreate") {
          if (originalAgentCreate) failed = true;
          originalAgentCreate = {
            call,
            project: args[0],
            fields: JSON.parse(JSON.stringify(args[1])),
          };
          writeInputs.set(call, originalAgentCreate);
        } else if (kind === "agent.checkOriginal") {
          const progress = auth.agentConfiguration.progress;
          writeInputs.set(call, {
            source: originalAgentCreate,
            current:
              progress?.phase === "uncertain" &&
              progress.contextValid === true &&
              progress.projectID === originalAgentCreate?.project &&
              progress.agentID === originalAgentCreate?.request?.agent_id,
          });
        } else if (kind === "agentEdit.startUpdate") {
          const original = {
            call,
            project: args[0],
            agent: args[1],
            input: JSON.parse(JSON.stringify(args[2])),
          };
          originalAgentEdits.push(original);
          writeInputs.set(call, original);
        } else if (kind === "agentEdit.checkOriginal") {
          const source = originalAgentEdits.at(-1);
          const progress = auth.agentConfiguration.progress;
          writeInputs.set(call, {
            source,
            current:
              progress?.kind === "update" &&
              progress.phase === "uncertain" &&
              progress.contextValid === true &&
              progress.projectID === source?.project &&
              progress.agentID === source?.agent,
          });
        }
        if (kind === "milestone.update") {
          const original = { call, project: args[0], milestone: args[1], input: JSON.parse(JSON.stringify(args[2])) };
          originalMilestoneEdits.push(original);
          writeInputs.set(call, original);
        } else if (kind === "milestone.checkOriginal") {
          const source = originalMilestoneEdits.at(-1), progress = auth.workMilestoneEditing.progress;
          writeInputs.set(call, { source, current: progress?.phase === "uncertain" && progress.contextValid === true && progress.projectID === source?.project && progress.milestoneID === source?.milestone });
        }
        if (kind === "sprintEdit.update") {
          const original = { call, project: args[0], sprint: args[1], input: JSON.parse(JSON.stringify(args[2])) };
          originalSprintEdits.push(original);
          writeInputs.set(call, original);
        } else if (kind === "sprintEdit.checkOriginal") {
          const source = originalSprintEdits.at(-1), progress = auth.workSprintEditing.progress;
          writeInputs.set(call, { source, current: progress?.phase === "uncertain" && progress.contextValid === true && progress.projectID === source?.project && progress.sprintID === source?.sprint });
        }
        if (kind === "scheduler.startUpdate") {
          const original = { call, project: args[0], input: JSON.parse(JSON.stringify(args[1])) };
          originalSchedulerWrites.push(original);
          writeInputs.set(call, original);
        } else if (kind === "scheduler.checkOriginal") {
          const source = originalSchedulerWrites.at(-1), progress = auth.projectScheduler.progress;
          writeInputs.set(call, { source, current: progress?.phase === "uncertain" &&
            progress.contextValid === true && progress.projectID === source?.project });
        }
        if (kind === "knowledgeText.write") {
          const original = {call, project: args[0], intent: JSON.parse(JSON.stringify(args[1]))};
          originalTextWrites.push(original);
          writeInputs.set(call, original);
        }
        if (kind === "variable.create" || kind === "variable.update") {
          const original = {
            call,
            project: args[0],
            variable: kind === "variable.update" ? args[1] : undefined,
            expectedVersion: kind === "variable.update" ? args[2] : undefined,
            fields: JSON.parse(JSON.stringify(args[kind === "variable.create" ? 1 : 3])),
          };
          originalVariableWrites.push(original);
          writeInputs.set(call, original);
        }
        if (kind === "milestone.create") {
          if (originalMilestoneCreate) failed = true;
          originalMilestoneCreate = {
            call,
            project: args[0],
            fields: JSON.parse(JSON.stringify(args[1])),
          };
          writeInputs.set(call, originalMilestoneCreate);
        }
        if (kind === "sprint.create") {
          if (originalSprintCreate) failed = true;
          originalSprintCreate = {
            call,
            project: args[0],
            fields: JSON.parse(JSON.stringify(args[1])),
          };
          writeInputs.set(call, originalSprintCreate);
        }
        if (kind === "sprint.start") {
          if (originalSprintStart) failed = true;
          originalSprintStart = {
            call,
            project: args[0],
            sprint: args[1],
            input: JSON.parse(JSON.stringify(args[2])),
          };
          writeInputs.set(call, originalSprintStart);
        } else if (kind === "sprint.checkOriginal") {
          const progress = auth.workSprintLifecycle.progress;
          writeInputs.set(call, {
            source: originalSprintStart,
            current:
              progress?.phase === "uncertain" &&
              progress.contextValid === true &&
              progress.projectID === originalSprintStart?.project &&
              progress.sprintID === originalSprintStart?.sprint,
          });
        }
        let result: Promise<any>;
        try {
          result = Reflect.apply(original, this, args);
        } catch (error) {
          call.rejected = call.settled = true;
          throw error;
        }
        if (originalCalls.has(result)) {
          calls.pop();
          return result;
        }
        originalCalls.set(result, call);
        track(
          result,
          async (value) => {
            call.fulfilled = call.settled = true;
            call.current =
              current() && (kind === "restore" || before === identity());
            identities.set(call, identity());
            call.not_busy = auth.state.busy === false;
            if (kind === "milestone.update" || kind === "milestone.checkOriginal") {
              const original = writeInputs.get(call), source = kind === "milestone.update" ? original : original.source;
              const progress = auth.workMilestoneEditing.progress;
              const result = kind === "milestone.update" ? value : value.result;
              call.result_current = progress?.phase === "confirmed" && progress.contextValid === true && progress.projectID === source.project && progress.milestoneID === source.milestone && result != null && progress.result === result;
            }
            if (kind === "sprintEdit.update" || kind === "sprintEdit.checkOriginal") {
              const original = writeInputs.get(call), source = kind === "sprintEdit.update" ? original : original.source;
              const progress = auth.workSprintEditing.progress;
              const result = kind === "sprintEdit.update" ? value : value.result;
              call.result_current = progress?.phase === "confirmed" && progress.contextValid === true && progress.projectID === source.project && progress.sprintID === source.sprint && result != null && progress.result === result;
            }
            if (kind === "scheduler.startUpdate" || kind === "scheduler.checkOriginal") {
              const original = writeInputs.get(call), progress = auth.projectScheduler.progress;
              call.result_current = progress?.phase === "confirmed" && progress.contextValid === true &&
                progress.projectID === (original.source?.project ?? original.project) &&
                progress.receipt === (kind === "scheduler.startUpdate" ? value : value.receipt);
            }
            if (kind === "knowledgeText.write") {
              const original = writeInputs.get(call), progress = auth.knowledgeText.progress;
              call.result_current = progress?.phase === "confirmed" &&
                progress.projectID === original.project && progress.documentID === original.intent.document_id &&
                progress.command === original.intent.command && progress.receipt === value.document;
            }
            if (kind === "variable.create" || kind === "variable.update") {
              const original = writeInputs.get(call);
              const progress = auth.projectVariables.progress;
              call.result_current =
                progress?.phase === "confirmed" &&
                progress.contextValid === true &&
                progress.kind === (kind === "variable.create" ? "create" : "update") &&
                progress.projectID === original.project &&
                progress.targetID === (original.variable ?? original.request?.variable_id) &&
                progress.receipt === value;
            }
            if (kind === "milestone.create") {
              const progress = auth.workMilestonePlanning.progress;
              call.result_current =
                progress?.phase === "confirmed" &&
                progress.contextValid === true &&
                progress.projectID === originalMilestoneCreate.project &&
                progress.milestoneID ===
                  originalMilestoneCreate.request?.milestone_id &&
                progress.result === value;
            }
            if (kind === "sprint.create") {
              const progress = auth.workSprintLifecycle.createProgress;
              call.result_current =
                progress?.phase === "confirmed" &&
                progress.contextValid === true &&
                progress.projectID === originalSprintCreate.project &&
                progress.milestoneID ===
                  originalSprintCreate.fields.milestone_id &&
                progress.sprintID === originalSprintCreate.request?.sprint_id &&
                progress.result === value;
            }
            if (!value || typeof value !== "object") return;
            if (
              kind === "restore" &&
              (Object.keys(value).sort().join() !== "session,user" ||
                value.user.id !== auth.state.user?.id ||
                value.session.id !== auth.state.session?.id)
            )
              return;
            call.typed = JSON.parse(JSON.stringify(value));
            call.typed_digest = await typedDigest(value);
          },
          () => {
            call.rejected = call.settled = true;
          },
        );
        return result;
      };
      Object.defineProperty(object, method, { ...descriptor, value: wrapper });
      restorers.push(() => {
        if (object[method] !== wrapper) failed = true;
        Object.defineProperty(object, method, descriptor);
      });
    }
    host.__taskReviewProof = {
      consumers(
        value: any,
        project: string,
        intakeWrites: boolean,
        agentConfiguration: boolean,
        editTask: string | undefined,
        timelineTask: string | undefined,
        editAgent: string | undefined,
        startSprint: string | undefined,
        createMilestone: string | undefined,
        skillsTarget: string | undefined,
        milestoneCreate: boolean,
        editMilestone: string | undefined,
        editSprint: string | undefined,
        variableWrites: boolean,
        knowledgeTextWrites: boolean,
        schedulerWrites = false,
        knowledgeTitleReads = false,
      ) {
        if (auth || retired) throw Error("TASK_REVIEW_DUPLICATE_CONSUMER");
        auth = value;
        consumer(auth, "restore", "restore", () => "/api/v1/session");
        for (const method of ["get", "list", "resolve"])
          consumer(
            auth.projects,
            method,
            "project." + method,
            (a) =>
              "/api/v1/projects" +
              (method === "get"
                ? "/" + a[0]
                : method === "resolve"
                  ? "/resolve"
                  : ""),
          );
        if (skillsTarget) {
          consumer(auth.skills, "list", "skills.list", (a) => {
            if (a[0] !== project || JSON.stringify(a[1]) !== '{"limit":25}')
              throw Error("SKILL_CATALOG_CONSUMER_SCOPE");
            return "/api/v1/projects/" + project + "/skills/catalog";
          });
          consumer(auth.skills, "get", "skills.get", (a) => {
            if (a[0] !== project || a[1] !== skillsTarget)
              throw Error("SKILL_DETAIL_CONSUMER_SCOPE");
            return "/api/v1/projects/" + project + "/skills/" + skillsTarget;
          });
        } else if (schedulerWrites) {
          for (const method of ["get", "startUpdate", "checkOriginal"])
            consumer(auth.projectScheduler, method, "scheduler." + method, (a) => {
              if (method === "checkOriginal" ? a.length !== 0 || auth.projectScheduler.progress?.projectID !== project : a[0] !== project)
                throw Error("PROJECT_SCHEDULER_CONSUMER_SCOPE");
              return "/api/v1/projects/" + project + (method === "checkOriginal" ? "/commands/lookup" : "/scheduler");
            }, method === "get" ? "GET" : method === "startUpdate" ? "PATCH" : "POST");
        } else if (knowledgeTitleReads) {
          const prefix = "/api/v1/projects/" + project + "/knowledge/documents";
          for (const method of ["children", "searchTitles", "get", "ancestors", "readContent"])
            consumer(auth.knowledge, method, "knowledge." + method, (a) => {
              if (a[0] !== project) throw Error("KNOWLEDGE_TITLE_READ_SCOPE");
              return prefix + (method === "children" ? "/children" : method === "searchTitles" ? "/search-titles" : "/" + a[1] + (method === "get" ? "" : method === "ancestors" ? "/ancestors" : "/content"));
            });
        } else if (knowledgeTextWrites) {
          const prefix = "/api/v1/projects/" + project + "/knowledge/documents";
          for (const method of ["children", "get", "ancestors", "readContent"])
            consumer(auth.knowledge, method, "knowledge." + method, (a) => {
              if (a[0] !== project) throw Error("KNOWLEDGE_TEXT_READ_SCOPE");
              return prefix + (method === "children" ? "/children" : "/" + a[1] + (method === "get" ? "" : method === "ancestors" ? "/ancestors" : "/content"));
            });
          consumer(auth.knowledgeText, "start", "knowledgeText.write", (a) => {
            if (a[0] !== project || !["create", "update"].includes(a[1]?.command)) throw Error("KNOWLEDGE_TEXT_WRITE_SCOPE");
            return prefix + "/" + a[1].document_id + (a[1].command === "create" ? "/create-text" : "/replace-text");
          }, "POST");
        } else if (variableWrites) {
          for (const method of ["list", "get"])
            consumer(auth.projectVariables, method, "variable." + method, (a) => {
              if (a[0] !== project ||
                  (method === "list" && JSON.stringify(a[1]) !== '{"limit":50}'))
                throw Error("PROJECT_VARIABLE_READ_SCOPE");
              return "/api/v1/projects/" + project + "/variables" +
                (method === "get" ? "/" + a[1] : "");
            });
          consumer(auth.projectVariables, "create", "variable.create", (a) => {
            if (a[0] !== project) throw Error("PROJECT_VARIABLE_WRITE_SCOPE");
            return "/api/v1/projects/" + project + "/variables";
          }, "POST");
          consumer(auth.projectVariables, "update", "variable.update", (a) => {
            if (a[0] !== project) throw Error("PROJECT_VARIABLE_WRITE_SCOPE");
            return "/api/v1/projects/" + project + "/variables/" + a[1];
          }, "PATCH");
        } else if (!agentConfiguration && !editAgent) {
          const paths: Record<string, string> = {
            milestones: "milestones",
            milestone: "milestones",
            sprints: "sprints",
            sprint: "sprints",
            tasks: "tasks",
            task: "tasks",
            blockers: "tasks",
            agents: "agents",
            agent: "agents",
          };
          for (const method of Object.keys(paths))
            consumer(auth.workReview, method, "work." + method, (a) => {
              if (a[0] !== project) throw Error("TASK_REVIEW_CONSUMER_SCOPE");
              const single = [
                "milestone",
                "sprint",
                "task",
                "agent",
                "blockers",
              ].includes(method);
              return (
                "/api/v1/projects/" +
                project +
                "/" +
                paths[method] +
                (single ? "/" + a[1] : "") +
                (method === "blockers" ? "/blockers" : "")
              );
            });
          if (timelineTask)
            consumer(auth.workReview, "events", "work.events", (a) => {
              if (a[0] !== project || a[1] !== timelineTask)
                throw Error("TASK_TIMELINE_CONSUMER_SCOPE");
              return (
                "/api/v1/projects/" +
                project +
                "/tasks/" +
                timelineTask +
                "/events"
              );
            });
          if (intakeWrites) {
            consumer(
              auth.workReview,
              "startCreate",
              "work.startCreate",
              (a) => {
                if (a[0] !== project) throw Error("TASK_INTAKE_CONSUMER_SCOPE");
                return "/api/v1/projects/" + project + "/tasks";
              },
              "POST",
            );
            consumer(
              auth.workReview,
              "startReady",
              "work.startReady",
              (a) => {
                if (a[0] !== project) throw Error("TASK_INTAKE_CONSUMER_SCOPE");
                return (
                  "/api/v1/projects/" + project + "/tasks/" + a[1] + "/transfer"
                );
              },
              "POST",
            );
            consumer(
              auth.workReview,
              "checkOriginal",
              "work.checkOriginal",
              () => "/api/v1/projects/" + project + "/task-commands/lookup",
              "POST",
            );
          }
          if (editTask) {
            for (const [method, httpMethod, suffix] of [
              ["startEdit", "PATCH", ""],
              ["startReady", "POST", "/transfer"],
            ] as const)
              consumer(
                auth.workReview,
                method,
                "edit." + method,
                (a) => {
                  if (a[0] !== project || a[1] !== editTask)
                    throw Error("TASK_EDIT_CONSUMER_SCOPE");
                  return (
                    "/api/v1/projects/" +
                    project +
                    "/tasks/" +
                    editTask +
                    suffix
                  );
                },
                httpMethod,
              );
            consumer(
              auth.workReview,
              "checkOriginal",
              "edit.checkOriginal",
              () => "/api/v1/projects/" + project + "/task-commands/lookup",
              "POST",
            );
          }
        } else {
          // Agent Create and Edit have separate write observers. Only their
          // existing public page reads share this registration.
          const paths: Record<string, string> = {
            list: "/agents",
            getConfiguration: "/agents",
            models: "/available-chat-models",
          };
          for (const method of Object.keys(paths))
            consumer(
              auth.agentConfiguration,
              method,
              "agent." + method,
              (a) => {
                if (
                  a[0] !== project ||
                  (editAgent &&
                    method === "getConfiguration" &&
                    a[1] !== editAgent)
                )
                  throw Error("AGENT_CONFIGURATION_CONSUMER_SCOPE");
                return (
                  "/api/v1/projects/" +
                  project +
                  paths[method] +
                  (method === "getConfiguration"
                    ? "/" + a[1] + "/configuration"
                    : "")
                );
              },
            );
          if (editAgent) {
            consumer(
              auth.agentConfiguration,
              "startUpdate",
              "agentEdit.startUpdate",
              (a) => {
                if (a[0] !== project || a[1] !== editAgent)
                  throw Error("AGENT_EDIT_CONSUMER_SCOPE");
                return (
                  "/api/v1/projects/" +
                  project +
                  "/agents/" +
                  editAgent +
                  "/configuration"
                );
              },
              "PATCH",
            );
            consumer(
              auth.agentConfiguration,
              "checkOriginal",
              "agentEdit.checkOriginal",
              () => "/api/v1/projects/" + project + "/agent-commands/lookup",
              "POST",
            );
          } else {
            consumer(
              auth.agentConfiguration,
              "startCreate",
              "agent.startCreate",
              (a) => {
                if (a[0] !== project)
                  throw Error("AGENT_CONFIGURATION_CONSUMER_SCOPE");
                return "/api/v1/projects/" + project + "/agents";
              },
              "POST",
            );
            consumer(
              auth.agentConfiguration,
              "checkOriginal",
              "agent.checkOriginal",
              () => "/api/v1/projects/" + project + "/agent-commands/lookup",
              "POST",
            );
          }
        }
        if (editMilestone) {
          consumer(auth.workMilestoneEditing, "update", "milestone.update", (a) => {
            if (a[0] !== project || a[1] !== editMilestone) throw Error("MILESTONE_UPDATE_CONSUMER_SCOPE");
            return "/api/v1/projects/" + project + "/milestones/" + editMilestone;
          }, "PATCH");
          consumer(auth.workMilestoneEditing, "checkOriginal", "milestone.checkOriginal", () => "/api/v1/projects/" + project + "/structure-commands/lookup", "POST");
        }
        if (editSprint) {
          consumer(auth.workSprintEditing, "update", "sprintEdit.update", (a) => {
            if (a[0] !== project || a[1] !== editSprint) throw Error("SPRINT_UPDATE_CONSUMER_SCOPE");
            return "/api/v1/projects/" + project + "/sprints/" + editSprint;
          }, "PATCH");
          consumer(auth.workSprintEditing, "checkOriginal", "sprintEdit.checkOriginal", () => "/api/v1/projects/" + project + "/structure-commands/lookup", "POST");
        }
        if (milestoneCreate) {
          consumer(
            auth.workMilestonePlanning,
            "create",
            "milestone.create",
            (a) => {
              if (a[0] !== project)
                throw Error("MILESTONE_CREATE_CONSUMER_SCOPE");
              return "/api/v1/projects/" + project + "/milestones";
            },
            "POST",
          );
        }
        if (createMilestone) {
          consumer(
            auth.workSprintLifecycle,
            "create",
            "sprint.create",
            (a) => {
              if (a[0] !== project || a[1]?.milestone_id !== createMilestone)
                throw Error("SPRINT_CREATE_CONSUMER_SCOPE");
              return "/api/v1/projects/" + project + "/sprints";
            },
            "POST",
          );
        }
        if (startSprint) {
          consumer(
            auth.workSprintLifecycle,
            "start",
            "sprint.start",
            (a) => {
              if (a[0] !== project || a[1] !== startSprint)
                throw Error("SPRINT_START_CONSUMER_SCOPE");
              return (
                "/api/v1/projects/" +
                project +
                "/sprints/" +
                startSprint +
                "/start"
              );
            },
            "POST",
          );
          consumer(
            auth.workSprintLifecycle,
            "checkOriginal",
            "sprint.checkOriginal",
            () =>
              "/api/v1/projects/" +
              project +
              "/sprint-lifecycle-commands/lookup",
            "POST",
          );
        }
      },
      snapshot() {
        return {
          retired,
          failed,
          pending: tails.size,
          current: current(),
          not_busy: auth?.state.busy === false,
          rows: [...rows.values()],
          calls: calls.map((c) => ({
            ...c,
            current: c.current && current() && identities.get(c) === identity(),
          })),
        };
      },
      finish() {
        if (retired) return this.snapshot();
        if (tails.size)
          throw Error("TASK_REVIEW_ORIGINAL_OBSERVATIONS_PENDING");
        retired = true;
        for (const restore of restorers.reverse()) restore();
        for (const call of calls) writeInputs.delete(call);
        originalCreate = undefined;
        originalAgentCreate = undefined;
        originalEdits.length = 0;
        originalAgentEdits.length = 0;
        originalMilestoneEdits.length = 0;
        originalSprintEdits.length = 0;
        originalSprintStart = undefined;
        originalSprintCreate = undefined;
        originalMilestoneCreate = undefined;
        originalVariableWrites.length = 0;
        originalTextWrites.length = 0;
        originalSchedulerWrites.length = 0;
        return this.snapshot();
      },
    };
    const signals = new WeakMap<AbortSignal, number>();
    const streams = new WeakMap<ReadableStream, number>();
    const readers = new WeakMap<ReadableStreamDefaultReader, number>();
    const safeURL = (value: string) => {
      const url = new URL(value, location.href);
      // Browser observations need only the pathname. PW records the bounded
      // safe query projection separately for each original Request.
      return `${url.origin}${url.pathname}`;
    };
    const errorName = (error: unknown) =>
      error instanceof Error &&
      ["AbortError", "TypeError", "Error", "TimeoutError"].includes(error.name)
        ? error.name
        : "other";
    const record = (kind: string, fields: Record<string, unknown> = {}) => {
      if (sequence >= 2048) return;
      const controls = [...document.querySelectorAll("button")].map(
        (button) => {
          if (button.getAttribute("aria-hidden") === "true") return undefined;
          const label = button.getAttribute("aria-label")?.trim();
          if (label) return label;
          const visible = button.cloneNode(true) as HTMLElement;
          visible
            .querySelectorAll('[aria-hidden="true"]')
            .forEach((node) => node.remove());
          return visible.textContent?.replace(/\s+/g, " ").trim();
        },
      );
      try {
        track(
          send({
            seq: ++sequence,
            at: performance.timeOrigin + performance.now(),
            document_at: performance.timeOrigin,
            kind,
            ...fields,
            route: safeURL(location.href),
            visibility: document.visibilityState,
            logout_control: controls.includes("退出登录"),
            session_check_control: controls.includes("检查当前会话"),
          }),
          () => {},
          () => {},
        );
      } catch {
        // Closing the diagnostic channel must not alter the original call.
      }
    };
    const originalFetch = window.fetch;
    window.fetch = function (input, init) {
      const result = originalFetch.call(this, input, init);
      const rawURL = input instanceof Request ? input.url : String(input);
      try {
        if (!new URL(rawURL, location.href).pathname.startsWith("/api/"))
          return result;
      } catch {
        return result;
      }
      if (metadataScope) {
        // The package fixture reuses this existing JSON observation only for
        // its closed metadata GET set. Its binary download keeps its separate
        // original-finished/file-byte gate and never enters this JSON reader.
        const url = new URL(rawURL, location.href);
        const method = init?.method ?? (input instanceof Request ? input.method : "GET");
        const project = "/api/v1/projects/" + metadataScope.project;
        if (method !== "GET" || url.origin !== location.origin || ![
          "/api/v1/session", "/api/v1/projects", "/api/v1/projects/resolve",
          project, project + "/skills/catalog", project + "/skills/" + metadataScope.skill,
        ].includes(url.pathname)) return result;
      }
      const id = ++nextFetch;
      if (id > 192) failed = true;
      const url = new URL(rawURL, location.href);
      const method =
        init?.method ?? (input instanceof Request ? input.method : "GET");
      const candidates = calls.filter(
        (call) =>
          !call.settled &&
          call.bound === 0 &&
          call.path === url.pathname &&
          call.method === method,
      );
      const call = candidates.length === 1 ? candidates[0] : undefined;
      if (call) call.bound++;
      const row: any = {
        id,
        call_id: call?.id ?? null,
        method,
        path: url.pathname,
        request_id: null,
        status: null,
        length: null,
        bytes: 0,
        digest: null,
        typed_digest: null,
        fetch_returned: false,
        fetch_rejected: false,
        readers: 0,
        reads: 0,
        read_returns: 0,
        eof: false,
        closed: false,
        release: 0,
        reader_cancel: 0,
        stream_cancel: 0,
        aborted: false,
        error: false,
      };
      rows.set(id, row);
      if (call && writeInputs.has(call)) {
        // Observe only the string body supplied to the original fetch; never
        // read a Request stream or substitute a reconstructed command.
        const original = writeInputs.get(call);
        try {
          const raw = init?.body;
          const key = new Headers(init?.headers).get("idempotency-key");
          if (
            typeof raw !== "string" ||
            new TextEncoder().encode(raw).length > 512 << 10 ||
            !key ||
            !/^[A-Za-z0-9._:/-]{1,128}$/.test(key)
          )
            throw Error("TASK_INTAKE_ORIGINAL_REQUEST_MISSING");
          const parsed = JSON.parse(raw);
          const equal = (a: unknown, b: unknown) =>
            JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
          let matches = false;
          if (call.kind === "work.startCreate") {
            const { task_id, ...draft } = parsed.request ?? {};
            matches =
              Object.keys(parsed).join() === "request" &&
              typeof task_id === "string" &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                task_id,
              ) &&
              equal(draft, original.draft);
            if (matches) {
              original.request = parsed.request;
              original.key = key;
            }
          } else if (call.kind === "work.startReady") {
            matches =
              original.task === original.source?.request?.task_id &&
              key !== original.source?.key &&
              equal(parsed, original.input);
          } else if (call.kind === "work.checkOriginal") {
            matches =
              original.current &&
              original.source?.call.rejected === true &&
              original.source.call.settled === true &&
              key === original.source.key &&
              equal(parsed, {
                command: "work.task.create",
                request: original.source.request,
              });
          } else if (call.kind === "edit.startEdit") {
            matches =
              originalEdits.every(
                (old) => old === original || old.key !== key,
              ) && equal(parsed, original.input);
            if (matches) original.key = key;
          } else if (call.kind === "edit.startReady") {
            matches =
              originalEdits.length > 0 &&
              originalEdits.every(
                (old) =>
                  old.project === original.project &&
                  old.task === original.task &&
                  typeof old.key === "string" &&
                  old.key !== key,
              ) &&
              equal(parsed, original.input);
          } else if (call.kind === "edit.checkOriginal") {
            matches =
              original.current &&
              original.source?.call.rejected === true &&
              original.source.call.settled === true &&
              key === original.source.key &&
              equal(parsed, {
                command: "work.task.update",
                target_id: original.source.task,
                expected_version: original.source.input.expected_version,
                request: original.source.input.request,
              });
          } else if (call.kind === "agent.startCreate") {
            const { agent_id, ...fields } = parsed.request ?? {};
            matches =
              Object.keys(parsed).join() === "request" &&
              typeof agent_id === "string" &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                agent_id,
              ) &&
              equal(fields, original.fields);
            if (matches) {
              original.request = parsed.request;
              original.key = key;
            }
          } else if (call.kind === "agent.checkOriginal") {
            matches =
              original.current &&
              original.source?.call.rejected === true &&
              original.source.call.settled === true &&
              key === original.source.key &&
              equal(parsed, {
                command: "agent.create",
                request: original.source.request,
              });
          } else if (call.kind === "agentEdit.startUpdate") {
            matches =
              originalAgentEdits.every(
                (old) =>
                  old === original ||
                  (typeof old.key === "string" && old.key !== key),
              ) && equal(parsed, original.input);
            if (matches) original.key = key;
          } else if (call.kind === "agentEdit.checkOriginal") {
            matches =
              original.current &&
              original.source?.call.rejected === true &&
              original.source.call.settled === true &&
              key === original.source.key &&
              equal(parsed, {
                command: "agent.update",
                target_id: original.source.agent,
                expected_version: original.source.input.expected_version,
                request: original.source.input.request,
              });
          }
          if (call.kind === "milestone.update") {
            matches = originalMilestoneEdits.every(old => old === original || (typeof old.key === "string" && old.key !== key)) && equal(parsed, original.input);
            if (matches) original.key = key;
          } else if (call.kind === "milestone.checkOriginal") {
            matches = original.current && original.source?.call.rejected === true && original.source.call.settled === true && key === original.source.key && equal(parsed, { command: "work.milestone.update", target_id: original.source.milestone, expected_version: original.source.input.expected_version, request: original.source.input.request });
          }
          if (call.kind === "sprintEdit.update") {
            matches = originalSprintEdits.every(old => old === original || (typeof old.key === "string" && old.key !== key)) && equal(parsed, original.input);
            if (matches) original.key = key;
          } else if (call.kind === "sprintEdit.checkOriginal") {
            matches = original.current && original.source?.call.rejected === true && original.source.call.settled === true && key === original.source.key && equal(parsed, { command: "work.sprint.update", target_id: original.source.sprint, expected_version: original.source.input.expected_version, request: original.source.input.request });
          }
          if (call.kind === "scheduler.startUpdate") {
            matches = originalSchedulerWrites.every(old => old === original ||
              (typeof old.key === "string" && old.key !== key)) && equal(parsed, original.input);
            if (matches) original.key = key;
          } else if (call.kind === "scheduler.checkOriginal") {
            // Legacy Project Lookup binds only the key and command, not the
            // Scheduler intent body. Never add that body to its wire request.
            matches = original.current && original.source?.call.rejected === true &&
              original.source.call.settled === true && key === original.source.key &&
              equal(parsed, { command: "update" });
          }
          if (call.kind === "knowledgeText.write") {
            matches = originalTextWrites.every(old => old === original || (typeof old.key === "string" && old.key !== key)) && equal(parsed, original.intent.request);
            if (matches) original.key = key;
          }
          if (call.kind === "variable.create") {
            const request = parsed?.request;
            matches =
              originalVariableWrites.length === 1 &&
              Object.keys(parsed).join() === "request" &&
              request !== null && typeof request === "object" && !Array.isArray(request) &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(request.variable_id) &&
              equal(request, { ...original.fields, variable_id: request.variable_id });
            if (matches) {
              original.key = key;
              original.request = request;
            }
          } else if (call.kind === "variable.update") {
            const creation = originalVariableWrites[0];
            matches =
              creation?.call.kind === "variable.create" &&
              original.project === creation.project &&
              original.variable === creation.request?.variable_id &&
              originalVariableWrites.every((old) => old === original ||
                (typeof old.key === "string" && old.key !== key)) &&
              equal(parsed, { expected_version: original.expectedVersion, request: original.fields });
            if (matches) original.key = key;
          }
          if (call.kind === "milestone.create") {
            const request = parsed?.request;
            matches =
              Object.keys(parsed).join() === "request" &&
              request !== null &&
              typeof request === "object" &&
              !Array.isArray(request) &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                request.milestone_id,
              ) &&
              equal(request, {
                ...original.fields,
                milestone_id: request.milestone_id,
              });
            if (matches) {
              original.key = key;
              original.request = request;
            }
          }
          if (call.kind === "sprint.create") {
            const request = parsed?.request;
            matches =
              Object.keys(parsed).join() === "request" &&
              request !== null &&
              typeof request === "object" &&
              !Array.isArray(request) &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                request.sprint_id,
              ) &&
              equal(request, {
                ...original.fields,
                sprint_id: request.sprint_id,
              });
            if (matches) {
              original.key = key;
              original.request = request;
            }
          }
          if (call.kind === "sprint.start") {
            matches =
              equal(parsed, original.input) &&
              Object.keys(parsed).sort().join() ===
                "expected_version,request" &&
              parsed.request !== null &&
              typeof parsed.request === "object" &&
              !Array.isArray(parsed.request) &&
              Object.keys(parsed.request).length === 0;
            if (matches) original.key = key;
          } else if (call.kind === "sprint.checkOriginal") {
            matches =
              original.current &&
              original.source?.call.rejected === true &&
              original.source.call.settled === true &&
              key === original.source.key &&
              equal(parsed, {
                command: "work.sprint.start",
                target_id: original.source.sprint,
                expected_version: original.source.input.expected_version,
                request: original.source.input.request,
              });
          }
          call.request_bound = row.request_bound = matches;
          track(
            Promise.all([
              digest(new TextEncoder().encode(raw)),
              digest(new TextEncoder().encode(key)),
            ]),
            ([bodyHash, keyHash]) => {
              call.request_body_sha256 = row.request_body_sha256 = bodyHash;
              call.idempotency_key_sha256 = row.idempotency_key_sha256 =
                keyHash;
            },
            () => {
              row.error = true;
            },
          );
        } catch {
          row.error = true;
        }
      }
      const signal =
        init?.signal ?? (input instanceof Request ? input.signal : undefined);
      if (signal && !signals.has(signal)) signals.set(signal, ++nextSignal);
      const signalID = signal ? signals.get(signal) : undefined;
      record("fetch-call", {
        fetch: id,
        signal: signalID,
        signal_source: init?.signal
          ? "init"
          : input instanceof Request
            ? "request"
            : "none",
        url: safeURL(rawURL),
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        aborted: signal?.aborted ?? false,
      });
      const abort = () => {
        row.aborted = true;
        record("signal-abort", { fetch: id, signal: signalID });
      };
      signal?.addEventListener("abort", abort, { once: true });
      if (signal)
        restorers.push(() => signal.removeEventListener("abort", abort));
      track(
        result,
        (response) => {
          row.fetch_returned = true;
          row.status = response.status;
          row.length = response.headers.get("content-length");
          row.request_id = response.headers.get("x-request-id");
          if (response.body) streams.set(response.body, id);
          const requestID = response.headers.get("x-request-id");
          record("fetch-resolved", {
            fetch: id,
            status: response.status,
            request_id:
              requestID && /^[0-9a-f-]{36}$/.test(requestID)
                ? requestID
                : undefined,
          });
        },
        (error) => {
          row.fetch_rejected = true;
          record("fetch-rejected", { fetch: id, error_name: errorName(error) });
        },
      );
      return result;
    };
    const originalGetReader = ReadableStream.prototype.getReader;
    ReadableStream.prototype.getReader = function (...args) {
      const reader = Reflect.apply(originalGetReader, this, args);
      const id = streams.get(this);
      if (id !== undefined) {
        readers.set(reader, id);
        rows.get(id).readers++;
        record("reader-acquired", { fetch: id });
        track(
          reader.closed,
          () => {
            rows.get(id).closed = true;
            record("reader-closed", { fetch: id });
          },
          (error: unknown) => {
            rows.get(id).error = true;
            record("reader-closed-rejected", {
              fetch: id,
              error_name: errorName(error),
            });
          },
        );
      }
      return reader;
    };
    const originalRead = ReadableStreamDefaultReader.prototype.read;
    const chunks = new WeakMap<ReadableStreamDefaultReader, Uint8Array[]>();
    ReadableStreamDefaultReader.prototype.read = function (...args) {
      const result = Reflect.apply(originalRead, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        const row = rows.get(id);
        row.reads++;
        record("reader-read", { fetch: id });
        const reader = this;
        track(
          result,
          async (part: ReadableStreamReadResult<Uint8Array>) => {
            row.read_returns++;
            record("reader-read-resolved", {
              fetch: id,
              done: part.done,
              bytes: part.value?.byteLength ?? 0,
            });
            if (!part.done) {
              row.bytes += part.value.byteLength;
              if (row.bytes > 2 << 20) {
                row.error = true;
                return;
              }
              chunks.set(reader, [
                ...(chunks.get(reader) ?? []),
                part.value.slice(),
              ]);
              return;
            }
            if (row.eof) {
              row.error = true;
              return;
            }
            row.eof = true;
            const body = new Uint8Array(row.bytes);
            let at = 0;
            for (const chunk of chunks.get(reader) ?? []) {
              body.set(chunk, at);
              at += chunk.length;
              chunk.fill(0);
            }
            chunks.delete(reader);
            try {
              row.digest = await digest(body);
              let parsed = JSON.parse(
                new TextDecoder("utf-8", { fatal: true }).decode(body),
              );
              if (row.path === "/api/v1/session" && row.status === 200)
                parsed = { user: parsed.user, session: parsed.session };
              // Match the production public DTO, not the richer wire shape.
              // The untouched original bytes remain bound by row.digest.
              const kind = calls.find((call) => call.id === row.call_id)?.kind;
              if (kind === "knowledge.ancestors") parsed = parsed.items;
              if (kind === "scheduler.checkOriginal" && parsed.state === "committed")
                parsed = { state: "committed", receipt: { project_id: parsed.result.project.id, version: parsed.result.project.version } };
              if (kind === "work.sprint") {
                const { started_by, completed_by, ...sprint } = parsed;
                parsed = sprint;
              } else if (kind === "work.blockers") {
                parsed = {
                  ...parsed,
                  items: parsed.items.map((item: any) => {
                    const { metadata, created_by, resolved_by, ...blocker } =
                      item;
                    return {
                      ...blocker,
                      related_task_id:
                        item.type === "rely_on"
                          ? metadata.related_task_id
                          : null,
                    };
                  }),
                };
              }
              row.typed_digest = await typedDigest(parsed);
            } catch {
              row.error = true;
            } finally {
              body.fill(0);
            }
          },
          (error: unknown) => {
            row.error = true;
            record("reader-read-rejected", {
              fetch: id,
              error_name: errorName(error),
            });
          },
        );
      }
      return result;
    };
    const readerCancel = ReadableStreamDefaultReader.prototype.cancel;
    ReadableStreamDefaultReader.prototype.cancel = function (...args) {
      const result = Reflect.apply(readerCancel, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        rows.get(id).reader_cancel++;
        record("reader-cancel", { fetch: id });
        track(
          result,
          () => record("reader-cancel-resolved", { fetch: id }),
          (error: unknown) =>
            record("reader-cancel-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return result;
    };
    const streamCancel = ReadableStream.prototype.cancel;
    ReadableStream.prototype.cancel = function (...args) {
      const result = Reflect.apply(streamCancel, this, args);
      const id = streams.get(this);
      if (id !== undefined) {
        rows.get(id).stream_cancel++;
        record("stream-cancel", { fetch: id });
        track(
          result,
          () => record("stream-cancel-resolved", { fetch: id }),
          (error: unknown) =>
            record("stream-cancel-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return result;
    };
    const originalRelease = ReadableStreamDefaultReader.prototype.releaseLock;
    ReadableStreamDefaultReader.prototype.releaseLock = function (...args) {
      const result = Reflect.apply(originalRelease, this, args);
      const id = readers.get(this);
      if (id !== undefined) rows.get(id).release++;
      return result;
    };
    for (const [object, key, original] of [
      [window, "fetch", originalFetch],
      [ReadableStream.prototype, "getReader", originalGetReader],
      [ReadableStreamDefaultReader.prototype, "read", originalRead],
      [ReadableStreamDefaultReader.prototype, "cancel", readerCancel],
      [ReadableStream.prototype, "cancel", streamCancel],
      [ReadableStreamDefaultReader.prototype, "releaseLock", originalRelease],
    ] as const) {
      const installed = (object as any)[key];
      restorers.push(() => {
        if ((object as any)[key] !== installed) failed = true;
        (object as any)[key] = original;
      });
    }
    for (const kind of [
      "pageshow",
      "pagehide",
      "popstate",
      "visibilitychange",
    ]) {
      const listener = () => record(kind);
      window.addEventListener(kind, listener);
      restorers.push(() => window.removeEventListener(kind, listener));
    }
    record("observer-installed");
  }, metadataScope);
}

// This closed endpoint set includes every real Task/Agent/list/lookup read in
// the new page. No unmatched request falls back to an unbounded finished().
function endpoint(request: Request, data: ReviewMaterial): string | undefined {
  const { pathname } = new URL(request.url());
  if (!pathname.startsWith("/api/")) return undefined;
  const method = request.method();
  const global: Record<string, string> = {
    "GET /api/v1/session": "session",
    "GET /api/v1/me": "profile",
    "GET /api/v1/me/preferences": "preferences",
    "GET /api/v1/auth/bootstrap": "bootstrap",
    "POST /api/v1/sessions/logout": "logout",
    "GET /api/v1/projects/resolve": "resolve",
    "GET /api/v1/projects": "projects",
  };
  const direct = global[`${method} ${pathname}`];
  if (direct) return direct;
  const prefix = `/api/v1/projects/${data.project_id}`;
  if (method === "GET" && pathname === prefix) return "project";
  if (!pathname.startsWith(prefix + "/"))
    throw new Error("UNEXPECTED_API_SCOPE");
  const path = pathname.slice(prefix.length);
  const uuid =
    "[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
  const reads: [RegExp, string][] = [
    [/^\/milestones$/, "milestones"],
    [new RegExp(`^/milestones/${uuid}$`), "milestone"],
    [/^\/sprints$/, "sprints"],
    [new RegExp(`^/sprints/${uuid}$`), "sprint"],
    [/^\/tasks$/, "tasks"],
    [new RegExp(`^/tasks/${data.task_id}$`), "task"],
    [new RegExp(`^/tasks/${data.task_id}/blockers$`), "blockers"],
    [/^\/agents$/, "agents"],
    [new RegExp(`^/agents/(${data.worker_id}|${data.reviewer_id})$`), "agent"],
  ];
  if (method === "GET")
    for (const [pattern, name] of reads) if (pattern.test(path)) return name;
  if (method === "POST" && path === `/tasks/${data.task_id}/transfer`)
    return "transfer";
  if (method === "POST" && path === "/task-transition-commands/lookup")
    return "lookup";
  throw new Error("UNOBSERVED_TASK_REVIEW_ENDPOINT");
}

export type TaskObservationMaterial = {
  mode: string;
  cookie: string;
  project_id: string;
  task_id?: string;
  agent_id?: string;
  sprint_id?: string;
  milestone_id?: string;
  skill_id?: string;
};
export type TaskObservationProfile = {
  dist: string;
  evidence: string;
  inputHash: string;
  prefix:
    | "task-review"
    | "task-intake"
    | "task-edit"
    | "agent-configuration"
    | "agent-edit"
    | "agent-model-policy"
    | "task-timeline"
    | "sprint-start"
    | "sprint-create"
    | "skill-owner-read"
    | "milestone-create"
    | "milestone-edit"
    | "sprint-edit"
    | "project-variables"
    | "knowledge-text"
    | "knowledge-title-search"
    | "project-scheduler";
  endpoint: (request: Request) => string | undefined;
  cut?: "transfer" | "create" | "update" | "start";
  lookupCommand:
    | "work.task.transfer"
    | "work.task.create"
    | "work.task.update"
    | "agent.create"
    | "agent.update"
    | "work.sprint.start"
    | "work.sprint.create"
    | "work.milestone.create"
    | "work.milestone.update"
    | "work.sprint.update"
    | "update"
    | null;
  intakeWrites?: true;
  agentConfiguration?: true;
  editWrites?: true;
  timelineReads?: true;
  agentEditWrites?: true;
  agentModelPolicy?: true;
  sprintStartWrites?: true;
  sprintCreateWrites?: true;
  skillsReads?: true;
  milestoneCreateWrites?: true;
  milestoneUpdateWrites?: true;
  sprintUpdateWrites?: true;
  variableWrites?: true;
  knowledgeTextWrites?: true;
  knowledgeTitleReads?: true;
  schedulerWrites?: true;
};

export function observeReview(page: Page, data: ReviewMaterial) {
  return observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_TASK_REVIEW_WEB_DIST!,
    evidence: process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_TASK_REVIEW_WEB_INPUT_HASH!,
    prefix: "task-review",
    endpoint: (request) => endpoint(request, data),
    cut: data.mode === "review-complete" ? undefined : "transfer",
    lookupCommand: "work.task.transfer",
  });
}

// Shared observation machinery only. Each fixture supplies a closed endpoint
// classifier and at most its one declared committed-response delivery fault.
// Only explicitly opted-in Intake, Edit or Agent fixtures can additionally prove
// their closed write sets. Other POST completion rules remain identical.
export function requireCommittedLookup(value: Record<string, unknown>, structureUpdate = false) {
  expect(structureUpdate ? value.state : value.status).toBe("committed");
}

export async function observeTaskRequests(
  page: Page,
  data: TaskObservationMaterial,
  profile: TaskObservationProfile,
) {
  if (
    profile.intakeWrites &&
    (profile.prefix !== "task-intake" ||
      profile.lookupCommand !== "work.task.create")
  )
    throw Error("TASK_INTAKE_WRITE_PROFILE_MISMATCH");
  if (
    profile.agentConfiguration &&
    (profile.intakeWrites ||
      profile.prefix !== "agent-configuration" ||
      profile.lookupCommand !== "agent.create" ||
      ![
        "default-create-config-and-directory",
        "lost-create-confirmation-only-lookup",
      ].includes(data.mode))
  )
    throw Error("AGENT_CONFIGURATION_PROFILE_MISMATCH");
  if (
    profile.editWrites &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.prefix !== "task-edit" ||
      profile.lookupCommand !== "work.task.update" ||
      !data.task_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.task_id,
      ) ||
      ![
        "changed-noop-clear-and-ready",
        "lost-edit-confirmation-and-ready",
      ].includes(data.mode) ||
      profile.cut !==
        (data.mode === "lost-edit-confirmation-and-ready"
          ? "update"
          : undefined))
  )
    throw Error("TASK_EDIT_WRITE_PROFILE_MISMATCH");
  if (
    profile.timelineReads &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.prefix !== "task-timeline" ||
      profile.lookupCommand !== null ||
      profile.cut !== undefined ||
      data.mode !== "recent-older-and-refresh" ||
      !data.task_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.task_id,
      ))
  )
    throw Error("TASK_TIMELINE_READ_PROFILE_MISMATCH");
  if (
    profile.agentEditWrites &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.prefix !== "agent-edit" ||
      profile.lookupCommand !== "agent.update" ||
      !data.agent_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.agent_id,
      ) ||
      ![
        "changed-noop-and-clear",
        "lost-update-confirmation-only-lookup",
      ].includes(data.mode) ||
      profile.cut !==
        (data.mode === "lost-update-confirmation-only-lookup"
          ? "update"
          : undefined))
  )
    throw Error("AGENT_EDIT_WRITE_PROFILE_MISMATCH");
  if (
    profile.agentModelPolicy &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.agentEditWrites ||
      profile.sprintStartWrites ||
      profile.prefix !== "agent-model-policy" ||
      profile.lookupCommand !== "agent.update" ||
      profile.cut !== undefined ||
      data.mode !== "switch-model-auto-and-clear-approval" ||
      !data.agent_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.agent_id,
      ))
  )
    throw Error("AGENT_MODEL_POLICY_PROFILE_MISMATCH");
  if (
    profile.sprintStartWrites &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.agentEditWrites ||
      profile.prefix !== "sprint-start" ||
      profile.lookupCommand !== "work.sprint.start" ||
      !data.sprint_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.sprint_id,
      ) ||
      ![
        "paused-start-and-current-board",
        "lost-start-confirmation-only-lookup",
      ].includes(data.mode) ||
      profile.cut !==
        (data.mode === "lost-start-confirmation-only-lookup"
          ? "start"
          : undefined))
  )
    throw Error("SPRINT_START_WRITE_PROFILE_MISMATCH");
  if (
    profile.sprintCreateWrites &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.agentEditWrites ||
      profile.agentModelPolicy ||
      profile.sprintStartWrites ||
      profile.prefix !== "sprint-create" ||
      profile.lookupCommand !== "work.sprint.create" ||
      profile.cut !== undefined ||
      data.mode !== "plan-another-sprint-with-existing-current" ||
      !data.milestone_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.milestone_id,
      ))
  )
    throw Error("SPRINT_CREATE_PROFILE_MISMATCH");
  if (
    profile.skillsReads &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.agentEditWrites ||
      profile.agentModelPolicy ||
      profile.sprintStartWrites ||
      profile.sprintCreateWrites ||
      profile.prefix !== "skill-owner-read" ||
      profile.lookupCommand !== null ||
      profile.cut !== undefined ||
      data.mode !== "read" ||
      !data.skill_id ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        data.skill_id,
      ))
  )
    throw Error("SKILLS_READ_PROFILE_MISMATCH");
  if (
    profile.milestoneCreateWrites &&
    (profile.intakeWrites ||
      profile.agentConfiguration ||
      profile.editWrites ||
      profile.timelineReads ||
      profile.agentEditWrites ||
      profile.agentModelPolicy ||
      profile.sprintStartWrites ||
      profile.sprintCreateWrites ||
      profile.skillsReads ||
      profile.prefix !== "milestone-create" ||
      profile.lookupCommand !== "work.milestone.create" ||
      profile.cut !== undefined ||
      data.mode !== "first-milestone-and-sprint-entry")
  )
    throw Error("MILESTONE_CREATE_PROFILE_MISMATCH");
  if (
    profile.variableWrites &&
    (profile.intakeWrites || profile.agentConfiguration || profile.editWrites ||
      profile.timelineReads || profile.agentEditWrites || profile.agentModelPolicy ||
      profile.sprintStartWrites || profile.sprintCreateWrites || profile.skillsReads ||
      profile.milestoneCreateWrites || profile.prefix !== "project-variables" ||
      profile.lookupCommand !== null || profile.cut !== undefined ||
      data.mode !== "create-update-and-clear")
  )
    throw Error("PROJECT_VARIABLE_WRITE_PROFILE_MISMATCH");
  if (profile.knowledgeTextWrites &&
      (profile.prefix !== "knowledge-text" || profile.lookupCommand !== null || profile.cut !== undefined ||
       data.mode !== "create-and-replace" || profile.intakeWrites || profile.agentConfiguration || profile.editWrites ||
       profile.timelineReads || profile.agentEditWrites || profile.agentModelPolicy || profile.sprintStartWrites ||
       profile.sprintCreateWrites || profile.skillsReads || profile.milestoneCreateWrites || profile.variableWrites))
    throw Error("KNOWLEDGE_TEXT_PROFILE_MISMATCH");
  if ((profile.milestoneUpdateWrites || profile.prefix === "milestone-edit") &&
    (profile.milestoneUpdateWrites !== true || profile.prefix !== "milestone-edit" || profile.lookupCommand !== "work.milestone.update" ||
      profile.intakeWrites || profile.agentConfiguration || profile.editWrites || profile.timelineReads || profile.agentEditWrites || profile.agentModelPolicy || profile.sprintStartWrites || profile.sprintCreateWrites || profile.skillsReads || profile.milestoneCreateWrites || profile.variableWrites || profile.knowledgeTextWrites ||
      !data.milestone_id || !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(data.milestone_id) ||
      !["milestone-edit-normal", "milestone-edit-lost"].includes(data.mode) ||
      profile.cut !== (data.mode === "milestone-edit-lost" ? "update" : undefined)))
    throw Error("MILESTONE_UPDATE_PROFILE_MISMATCH");
  if ((profile.sprintUpdateWrites || profile.prefix === "sprint-edit") &&
    (profile.sprintUpdateWrites !== true || profile.prefix !== "sprint-edit" || profile.lookupCommand !== "work.sprint.update" ||
      profile.intakeWrites || profile.agentConfiguration || profile.editWrites || profile.timelineReads || profile.agentEditWrites || profile.agentModelPolicy || profile.sprintStartWrites || profile.sprintCreateWrites || profile.skillsReads || profile.milestoneCreateWrites || profile.milestoneUpdateWrites || profile.variableWrites || profile.knowledgeTextWrites || profile.schedulerWrites ||
      !data.sprint_id || !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(data.sprint_id) ||
      !data.milestone_id || !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(data.milestone_id) ||
      !["sprint-edit-normal", "sprint-edit-lost"].includes(data.mode) ||
      profile.cut !== (data.mode === "sprint-edit-lost" ? "update" : undefined)))
    throw Error("SPRINT_UPDATE_PROFILE_MISMATCH");
  if (profile.schedulerWrites &&
      (profile.prefix !== "project-scheduler" || profile.lookupCommand !== "update" ||
       !["pause-limit-and-resume", "lost-update-confirmation-only-lookup"].includes(data.mode) ||
       profile.cut !== (data.mode === "lost-update-confirmation-only-lookup" ? "update" : undefined) ||
       profile.intakeWrites || profile.agentConfiguration || profile.editWrites || profile.timelineReads ||
       profile.agentEditWrites || profile.agentModelPolicy || profile.sprintStartWrites || profile.sprintCreateWrites ||
       profile.skillsReads || profile.milestoneCreateWrites || profile.variableWrites || profile.knowledgeTextWrites))
    throw Error("PROJECT_SCHEDULER_PROFILE_MISMATCH");
  if ((profile.knowledgeTitleReads || profile.prefix === "knowledge-title-search") &&
      (profile.knowledgeTitleReads !== true || profile.prefix !== "knowledge-title-search" ||
       profile.lookupCommand !== null || profile.cut !== undefined || data.mode !== "literal-title-and-current-read" ||
       profile.intakeWrites || profile.agentConfiguration || profile.editWrites || profile.timelineReads ||
       profile.agentEditWrites || profile.agentModelPolicy || profile.sprintStartWrites || profile.sprintCreateWrites ||
       profile.skillsReads || profile.milestoneCreateWrites || profile.milestoneUpdateWrites || profile.sprintUpdateWrites ||
       profile.variableWrites || profile.knowledgeTextWrites || profile.schedulerWrites))
    throw Error("KNOWLEDGE_TITLE_PROFILE_MISMATCH");
  const entries: Entry[] = [];
  const events: Record<string, unknown>[] = [];
  const browserEvents: BrowserDiagnostic[] = [];
  let browserEventsTruncated = false;
  let observerError: Error | undefined;
  let dropped = 0;
  const proofs: any[] = [];
  const retirements: any[] = [];
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    profile.dist,
  );
  if (binding.entry === binding.asset)
    throw Error("TASK_REVIEW_ENTRY_IS_SINGLETON");
  const event = (kind: string, entry: Entry) => {
    if (events.length < 1024)
      events.push({
        kind,
        at: Date.now(),
        seq: entry.seq,
        name: entry.name,
        method: entry.request.method(),
        url: diagnosticURL(entry.request.url()),
        route: diagnosticURL(page.url()),
        status: entry.status,
        request_id: entry.requestID,
        failure: entry.failure,
        ...((profile.skillsReads || profile.schedulerWrites || profile.knowledgeTitleReads) && entry.skillsHeaders
          ? { skills_headers: { ...entry.skillsHeaders } }
          : {}),
      });
  };
  const end = (request: Request, state: Tail) => {
    const entry = entries.find((item) => item.request === request);
    if (entry) {
      if (state === "finished") entry.finishedCount++;
      if (state === "failed") entry.failedCount++;
      if (entry.terminal)
        observerError = new Error("DUPLICATE_ORIGINAL_REQUEST_TERMINAL");
    }
    if (entry && !entry.terminal) {
      entry.terminal = state;
      entry.failure = request
        .failure()
        ?.errorText.replaceAll(data.cookie, "<redacted>")
        .slice(0, 256);
      event(state, entry);
      entry.settle(state);
    }
  };
  const onRequest = (request: Request) => {
    try {
      const name = profile.endpoint(request);
      if (!name) return;
      if (entries.length >= 192)
        throw new Error("TASK_REVIEW_REQUEST_BOUND_EXCEEDED");
      let settle!: (state: Tail) => void;
      const done = new Promise<Tail>((resolve) => {
        settle = resolve;
      });
      const entry = {
        seq: entries.length + 1,
        request,
        name,
        settle,
        done,
        checked: false,
        responseCount: 0,
        finishedCount: 0,
        failedCount: 0,
      };
      entries.push(entry);
      event("request", entry);
    } catch (error) {
      observerError =
        error instanceof Error ? error : new Error("API_OBSERVER_REJECTED");
    }
  };
  const finished = (request: Request) => end(request, "finished");
  const failed = (request: Request) => end(request, "failed");
  const response = (response: Response) => {
    const entry = entries.find((item) => item.request === response.request());
    if (!entry) return;
    entry.responseCount++;
    if (entry.responseCount !== 1)
      observerError = new Error("DUPLICATE_ORIGINAL_RESPONSE");
    entry.status = response.status();
    if (profile.skillsReads || profile.schedulerWrites || profile.knowledgeTitleReads) {
      // Observe both original header promises once; retain no credential values.
      entry.headersReady = Promise.allSettled([
        entry.request.allHeaders(),
        response.allHeaders(),
      ])
        .then(([sentResult, receivedResult]) => {
          const sent =
            sentResult.status === "fulfilled" ? sentResult.value : undefined;
          const received =
            receivedResult.status === "fulfilled"
              ? receivedResult.value
              : undefined;
          const origin = new URL(entry.request.url());
          const id = received?.["x-request-id"];
          const diagnostic: SkillsHeaderDiagnostic = {
            stage: "joined",
            request_fulfilled: sentResult.status === "fulfilled",
            response_fulfilled: receivedResult.status === "fulfilled",
            host_present:
              typeof sent?.host === "string" && sent.host.length > 0,
            host_matches: sent?.host === origin.host,
            authority_present:
              typeof sent?.[":authority"] === "string" &&
              sent[":authority"].length > 0,
            authority_matches: sent?.[":authority"] === origin.host,
            handler_host_matches: false,
            cookie_present: Boolean(sent?.cookie),
            response_id_present: typeof id === "string" && id.length > 0,
            response_id_valid:
              typeof id === "string" &&
              /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                id,
              ),
          };
          entry.skillsHeaders = diagnostic;
          // Publish only closed predicates after both original promises retire.
          // Chromium may omit Host from its header projection. A visible wrong
          // Host still rejects; the original App handler proves the wire Host
          // separately, bound to this response's canonical request ID below.
          if (!diagnostic.request_fulfilled || !diagnostic.response_fulfilled) {
            diagnostic.stage = "header_promises";
            event("skills-headers", entry);
            throw Error("SKILLS_ORIGINAL_HEADERS_FAILED");
          }
          if (
            !diagnostic.cookie_present ||
            (diagnostic.host_present && !diagnostic.host_matches) ||
            (diagnostic.authority_present && !diagnostic.authority_matches)
          ) {
            diagnostic.stage = "request_gate";
            event("skills-headers", entry);
            throw Error("SKILLS_ORIGINAL_REQUEST_HEADERS");
          }
          if (!diagnostic.response_id_valid) {
            diagnostic.stage = "response_id_gate";
            event("skills-headers", entry);
            throw Error("SKILLS_ORIGINAL_RESPONSE_ID");
          }
          diagnostic.stage = "handler_pending";
          entry.requestID = id;
          event("response", entry);
        })
        .catch(() => {
          observerError = new Error("SKILLS_ORIGINAL_HEADERS_FAILED");
        });
    } else {
      const id = response.headers()["x-request-id"];
      entry.requestID = id && /^[0-9a-f-]{36}$/.test(id) ? id : undefined;
      event("response", entry);
    }
  };
  const closed = () =>
    entries
      .filter((entry) => !entry.terminal)
      .forEach((entry) => end(entry.request, "closed"));
  page.on("request", onRequest);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  page.on("response", response);
  page.on("close", closed);
  await observeBrowserIO(page, (event) => {
    if (browserEvents.length < 4096) browserEvents.push(event);
    else browserEventsTruncated = true;
  });
  const entryURL = new URL(binding.entry, process.env.AGENTEAM_AUTH_WEB_ORIGIN!)
    .href;
  let entryHandlers = 0;
  const install = async (route: import("@playwright/test").Route) => {
    entryHandlers++;
    try {
      await page.evaluate(
        async ({
          binding,
          project,
          intakeWrites,
          agentConfiguration,
          editTask,
          timelineTask,
          editAgent,
          startSprint,
          createMilestone,
          skillsTarget,
          milestoneCreate,
          editMilestone,
          editSprint,
          variableWrites,
          knowledgeTextWrites,
          schedulerWrites,
          knowledgeTitleReads,
        }) => {
          const module = await import(/* @vite-ignore */ binding.asset);
          const useSession = module[binding.exportName];
          if (typeof useSession !== "function")
            throw Error("TASK_REVIEW_SINGLETON_EXPORT");
          const auth = useSession();
          if (useSession() !== auth)
            throw Error("TASK_REVIEW_SINGLETON_IDENTITY");
          (window as any).__taskReviewProof.consumers(
            auth,
            project,
            intakeWrites,
            agentConfiguration,
            editTask,
            timelineTask,
            editAgent,
            startSprint,
            createMilestone,
            skillsTarget,
            milestoneCreate,
            editMilestone,
          editSprint,
            variableWrites,
            knowledgeTextWrites,
            schedulerWrites,
          knowledgeTitleReads,
          );
        },
        {
          binding,
          project: data.project_id,
          intakeWrites: profile.intakeWrites === true,
          agentConfiguration: profile.agentConfiguration === true,
          editTask: profile.editWrites ? data.task_id : undefined,
          timelineTask: profile.timelineReads ? data.task_id : undefined,
          editAgent:
            profile.agentEditWrites || profile.agentModelPolicy
              ? data.agent_id
              : undefined,
          startSprint: profile.sprintStartWrites ? data.sprint_id : undefined,
          createMilestone: profile.sprintCreateWrites
            ? data.milestone_id
            : undefined,
          skillsTarget: profile.skillsReads ? data.skill_id : undefined,
          milestoneCreate: profile.milestoneCreateWrites === true,
          editMilestone: profile.milestoneUpdateWrites ? data.milestone_id : undefined,
          editSprint: profile.sprintUpdateWrites ? data.sprint_id : undefined,
          variableWrites: profile.variableWrites === true,
          knowledgeTextWrites: profile.knowledgeTextWrites === true,
          schedulerWrites: profile.schedulerWrites === true,
          knowledgeTitleReads: profile.knowledgeTitleReads === true,
        },
      );
      await route.continue();
    } catch {
      observerError = new Error("TASK_REVIEW_ORIGINAL_CONSUMER_INSTALL_FAILED");
      await route.abort();
    } finally {
      entryHandlers--;
    }
  };
  await page.route(entryURL, install);

  async function checkpoint() {
    await expect
      .poll(
        async () =>
          page.evaluate(
            () => (window as any).__taskReviewProof.snapshot().pending,
          ),
        {
          timeout: 5_000,
          message:
            "original reader and public-consumer observers must actually settle",
        },
      )
      .toBe(0);
    const proof = await page.evaluate(() =>
      (window as any).__taskReviewProof.snapshot(),
    );
    expect(proof.failed).toBe(false);
    expect(proof.pending).toBe(0);
    return proof;
  }

  function intakeWrite(entry: Entry) {
    if (!profile.intakeWrites || entry.request.method() !== "POST")
      return false;
    const paths: Record<string, string> = {
      create: `/api/v1/projects/${data.project_id}/tasks`,
      lookup: `/api/v1/projects/${data.project_id}/task-commands/lookup`,
    };
    const url = new URL(entry.request.url());
    if (url.search) return false;
    if (entry.name === "ready") {
      const creates = entries.filter((e) => e.name === "create");
      if (creates.length !== 1) return false;
      const task = creates[0]!.request.postDataJSON().request.task_id;
      return (
        url.pathname ===
        `/api/v1/projects/${data.project_id}/tasks/${task}/transfer`
      );
    }
    return (
      Object.hasOwn(paths, entry.name) && url.pathname === paths[entry.name]
    );
  }

  function editWrite(entry: Entry) {
    if (!profile.editWrites) return false;
    const url = new URL(entry.request.url());
    if (url.search) return false;
    const prefix = `/api/v1/projects/${data.project_id}`;
    const method = entry.request.method();
    return (
      (entry.name === "update" &&
        method === "PATCH" &&
        url.pathname === `${prefix}/tasks/${data.task_id}`) ||
      (entry.name === "ready" &&
        method === "POST" &&
        url.pathname === `${prefix}/tasks/${data.task_id}/transfer`) ||
      (entry.name === "lookup" &&
        method === "POST" &&
        url.pathname === `${prefix}/task-commands/lookup`)
    );
  }

  function agentWrite(entry: Entry) {
    if (!profile.agentConfiguration || entry.request.method() !== "POST")
      return false;
    const url = new URL(entry.request.url());
    if (url.search) return false;
    const prefix = `/api/v1/projects/${data.project_id}`;
    return (
      (entry.name === "create" && url.pathname === prefix + "/agents") ||
      (entry.name === "lookup" &&
        url.pathname === prefix + "/agent-commands/lookup")
    );
  }
  function agentPageRead(entry: Entry) {
    return (
      (profile.agentConfiguration ||
        profile.agentEditWrites ||
        profile.agentModelPolicy) &&
      entry.request.method() === "GET" &&
      ["agent-directory", "agent-configuration", "agent-models"].includes(
        entry.name,
      )
    );
  }

  function agentEditWrite(entry: Entry) {
    if (!profile.agentEditWrites && !profile.agentModelPolicy) return false;
    const url = new URL(entry.request.url());
    if (url.search) return false;
    const prefix = `/api/v1/projects/${data.project_id}`;
    return (
      (entry.name === "update" &&
        entry.request.method() === "PATCH" &&
        url.pathname === `${prefix}/agents/${data.agent_id}/configuration`) ||
      (profile.agentEditWrites &&
        entry.name === "lookup" &&
        entry.request.method() === "POST" &&
        url.pathname === `${prefix}/agent-commands/lookup`)
    );
  }

  function timelinePageRead(entry: Entry) {
    return (
      profile.timelineReads &&
      entry.name === "events" &&
      entry.request.method() === "GET" &&
      new URL(entry.request.url()).pathname ===
        `/api/v1/projects/${data.project_id}/tasks/${data.task_id}/events`
    );
  }

  function sprintWrite(entry: Entry) {
    if (!profile.sprintStartWrites || entry.request.method() !== "POST")
      return false;
    const url = new URL(entry.request.url());
    if (url.search) return false;
    const prefix = `/api/v1/projects/${data.project_id}`;
    return (
      (entry.name === "start" &&
        url.pathname === `${prefix}/sprints/${data.sprint_id}/start`) ||
      (entry.name === "lookup" &&
        url.pathname === `${prefix}/sprint-lifecycle-commands/lookup`)
    );
  }
  function sprintCreateWrite(entry: Entry) {
    const url = new URL(entry.request.url());
    return (
      profile.sprintCreateWrites &&
      entry.name === "create" &&
      entry.request.method() === "POST" &&
      url.pathname === `/api/v1/projects/${data.project_id}/sprints` &&
      !url.search
    );
  }
  function sprintPageRead(entry: Entry) {
    return (
      entry.request.method() === "GET" &&
      ((profile.sprintUpdateWrites && ["project", "sprint", "sprints", "milestone", "milestones", "agents", "tasks"].includes(entry.name)) ||
        (profile.sprintStartWrites &&
        ["project", "sprint", "tasks"].includes(entry.name)) ||
        (profile.sprintCreateWrites &&
          [
            "project",
            "sprint",
            "tasks",
            "milestone",
            "milestones",
            "sprints",
          ].includes(entry.name)))
    );
  }
  function milestoneCreateWrite(entry: Entry) {
    const url = new URL(entry.request.url());
    return (
      profile.milestoneCreateWrites &&
      entry.name === "create" &&
      entry.request.method() === "POST" &&
      url.pathname === `/api/v1/projects/${data.project_id}/milestones` &&
      !url.search
    );
  }
  function milestoneUpdateWrite(entry: Entry) {
    if (!profile.milestoneUpdateWrites) return false;
    const url = new URL(entry.request.url()), prefix = `/api/v1/projects/${data.project_id}`;
    return !url.search && ((entry.name === "update" && entry.request.method() === "PATCH" && url.pathname === `${prefix}/milestones/${data.milestone_id}`) ||
      (entry.name === "lookup" && entry.request.method() === "POST" && url.pathname === `${prefix}/structure-commands/lookup`));
  }
  function sprintUpdateWrite(entry: Entry) {
    if (!profile.sprintUpdateWrites) return false;
    const url = new URL(entry.request.url()), prefix = `/api/v1/projects/${data.project_id}`;
    return !url.search && ((entry.name === "update" && entry.request.method() === "PATCH" && url.pathname === `${prefix}/sprints/${data.sprint_id}`) ||
      (entry.name === "lookup" && entry.request.method() === "POST" && url.pathname === `${prefix}/structure-commands/lookup`));
  }
  function milestonePageRead(entry: Entry) {
    return (
      (profile.milestoneCreateWrites || profile.milestoneUpdateWrites) &&
      entry.request.method() === "GET" &&
      ["project", "milestones", "milestone", "sprints", "agents"].includes(
        entry.name,
      )
    );
  }
  function skillsPageRead(entry: Entry) {
    return (
      profile.skillsReads &&
      entry.request.method() === "GET" &&
      ["skills-catalog", "skill"].includes(entry.name)
    );
  }
  function variableWrite(entry: Entry) {
    if (!profile.variableWrites) return false;
    const url = new URL(entry.request.url());
    const prefix = `/api/v1/projects/${data.project_id}/variables`;
    if (url.search) return false;
    return (entry.name === "create" && entry.request.method() === "POST" && url.pathname === prefix) ||
      (entry.name === "update" && entry.request.method() === "PATCH" &&
        url.pathname.startsWith(prefix + "/") &&
        /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(url.pathname.slice(prefix.length + 1)));
  }
  function variablePageRead(entry: Entry) {
    return profile.variableWrites && entry.request.method() === "GET" &&
      ["variables", "variable"].includes(entry.name);
  }
  function knowledgeTextWrite(entry: Entry) {
    if (!profile.knowledgeTextWrites || entry.request.method() !== "POST") return false;
    const url = new URL(entry.request.url()), prefix = `/api/v1/projects/${data.project_id}/knowledge/documents/`;
    return !url.search && ["create", "update"].includes(entry.name) && url.pathname.startsWith(prefix) &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\/(create-text|replace-text)$/.test(url.pathname.slice(prefix.length));
  }
  function knowledgeTextReadEntry(entry: Entry) {
    return profile.knowledgeTextWrites && entry.request.method() === "GET" && ["documents", "document", "ancestors", "content"].includes(entry.name);
  }
  function knowledgeTitleReadEntry(entry: Entry) {
    return profile.knowledgeTitleReads && entry.request.method() === "GET" &&
      ["session", "resolve", "projects", "project", "documents", "titles", "document", "ancestors", "content"].includes(entry.name);
  }
  function schedulerWrite(entry: Entry) {
    if (!profile.schedulerWrites) return false;
    const url = new URL(entry.request.url()), prefix = `/api/v1/projects/${data.project_id}`;
    return !url.search && ((entry.name === "update" && entry.request.method() === "PATCH" && url.pathname === prefix + "/scheduler") ||
      (entry.name === "lookup" && entry.request.method() === "POST" && url.pathname === prefix + "/commands/lookup"));
  }
  function schedulerReadEntry(entry: Entry) {
    return profile.schedulerWrites && entry.request.method() === "GET" &&
      ["session", "resolve", "projects", "project", "scheduler"].includes(entry.name);
  }
  function joint(entry: Entry, proof: any) {
    const edit = editWrite(entry);
    const agentEdit = agentEditWrite(entry);
    const sprint = sprintWrite(entry);
    const sprintCreate = sprintCreateWrite(entry);
    const milestoneCreate = milestoneCreateWrite(entry);
    const milestoneUpdate = milestoneUpdateWrite(entry);
    const sprintUpdate = sprintUpdateWrite(entry);
    const variable = variableWrite(entry);
    const knowledgeText = knowledgeTextWrite(entry);
    const scheduler = schedulerWrite(entry);
    const write =
      intakeWrite(entry) ||
      agentWrite(entry) ||
      edit ||
      agentEdit ||
      sprint ||
      sprintCreate ||
      milestoneCreate ||
      milestoneUpdate ||
      sprintUpdate ||
      variable ||
      knowledgeText || scheduler;
    const method =
      (edit || agentEdit || milestoneUpdate || sprintUpdate || variable || scheduler) && entry.name === "update"
        ? "PATCH"
        : write
          ? "POST"
          : "GET";
    expect(entry.request.method()).toBe(method);
    expect(entry.status).toBe(200);
    expect(entry.responseCount).toBe(1);
    expect(entry.finishedCount).toBe(entry.terminal === "finished" ? 1 : 0);
    expect(entry.failedCount).toBe(entry.terminal === "failed" ? 1 : 0);
    expect(page.isClosed()).toBe(false);
    expect(proof.current).toBe(true);
    expect(proof.not_busy).toBe(true);
    const matched = proof.rows.filter(
      (r: any) => r.request_id === entry.requestID,
    );
    expect(matched).toHaveLength(1);
    const n = matched[0];
    const consumers = proof.calls.filter((c: any) => c.id === n.call_id);
    expect(consumers).toHaveLength(1);
    const c = consumers[0];
    const expectedURL = new URL(entry.request.url());
    const record = JSON.parse(
      readFileSync(
        join(
          profile.evidence,
          `${profile.prefix}-${data.mode}-response-${entry.requestID}.json`,
        ),
        "utf8",
      ),
    );
    expect(record.input_hash).toBe(profile.inputHash);
    expect(record.request_id).toBe(entry.requestID);
    expect(record.method).toBe(method);
    expect(record.status).toBe(200);
    expect(record.url_sha256).toBe(
      createHash("sha256")
        .update(expectedURL.pathname + expectedURL.search)
        .digest("hex"),
    );
    expect(
      record.original_eof && record.original_closed && record.handler_returned,
    ).toBe(true);
    expect(n.path).toBe(expectedURL.pathname);
    expect(n.method).toBe(method);
    expect(n.status).toBe(200);
    expect(n.fetch_returned).toBe(true);
    expect(n.fetch_rejected || n.error || n.aborted).toBe(false);
    expect(n.readers).toBe(1);
    expect(n.reads).toBeGreaterThan(0);
    expect(n.read_returns).toBe(n.reads);
    expect(n.eof && n.closed).toBe(true);
    expect(n.release).toBe(1);
    expect(n.reader_cancel).toBe(0);
    expect(n.stream_cancel).toBe(0);
    expect(n.length).toMatch(/^(0|[1-9][0-9]*)$/);
    expect(Number(n.length)).toBe(n.bytes);
    expect(record.content_length).toBe(n.bytes);
    expect(n.digest).toMatch(/^[0-9a-f]{64}$/);
    expect(record.body_sha256).toBe(n.digest);
    expect(c.bound).toBe(1);
    expect(c.method).toBe(method);
    expect(c.path).toBe(n.path);
    expect(c.fulfilled && c.settled && c.current && c.not_busy).toBe(true);
    expect(c.rejected).toBe(false);
    expect(c.typed).not.toBeNull();
    expect(c.typed_digest).toMatch(/^[0-9a-f]{64}$/);
    expect(n.typed_digest).toBe(c.typed_digest);
    if (timelinePageRead(entry)) expect(c.kind).toBe("work.events");
    if (skillsPageRead(entry))
      expect(c.kind).toBe(
        entry.name === "skills-catalog" ? "skills.list" : "skills.get",
      );
    if (variablePageRead(entry))
      expect(c.kind).toBe(entry.name === "variables" ? "variable.list" : "variable.get");
    if (knowledgeTextReadEntry(entry)) expect(c.kind).toBe({documents:"knowledge.children", document:"knowledge.get", ancestors:"knowledge.ancestors", content:"knowledge.readContent"}[entry.name]);
    if (knowledgeTitleReadEntry(entry)) expect(c.kind).toBe({session:"restore", resolve:"project.resolve", projects:"project.list", project:"project.get", documents:"knowledge.children", titles:"knowledge.searchTitles", document:"knowledge.get", ancestors:"knowledge.ancestors", content:"knowledge.readContent"}[entry.name]);
    if (schedulerReadEntry(entry)) expect(c.kind).toBe({ session: "restore", resolve: "project.resolve", projects: "project.list", project: "project.get", scheduler: "scheduler.get" }[entry.name]);
    if (write) {
      if (scheduler) expect(c.result_current).toBe(true);
      if (sprintCreate || milestoneCreate || milestoneUpdate || sprintUpdate || variable || knowledgeText) expect(c.result_current).toBe(true);
      const kinds: Record<string, string> = scheduler
        ? { update: "scheduler.startUpdate", lookup: "scheduler.checkOriginal" }
        : knowledgeText
        ? { create: "knowledgeText.write", update: "knowledgeText.write" }
        : variable
        ? { create: "variable.create", update: "variable.update" }
        : sprintUpdate
        ? { update: "sprintEdit.update", lookup: "sprintEdit.checkOriginal" }
        : milestoneUpdate
        ? { update: "milestone.update", lookup: "milestone.checkOriginal" }
        : milestoneCreate
        ? { create: "milestone.create" }
        : sprintCreate
          ? { create: "sprint.create" }
          : sprint
            ? { start: "sprint.start", lookup: "sprint.checkOriginal" }
            : agentEdit
              ? {
                  update: "agentEdit.startUpdate",
                  lookup: "agentEdit.checkOriginal",
                }
              : profile.agentConfiguration
                ? {
                    create: "agent.startCreate",
                    lookup: "agent.checkOriginal",
                  }
                : edit
                  ? {
                      update: "edit.startEdit",
                      ready: "edit.startReady",
                      lookup: "edit.checkOriginal",
                    }
                  : {
                      create: "work.startCreate",
                      ready: "work.startReady",
                      lookup: "work.checkOriginal",
                    };
      const raw = entry.request.postDataBuffer();
      const key = entry.request.headers()["idempotency-key"];
      expect(raw).not.toBeNull();
      expect(raw!.length).toBeLessThanOrEqual(512 << 10);
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      const bodyHash = createHash("sha256").update(raw!).digest("hex");
      const keyHash = createHash("sha256").update(key!).digest("hex");
      expect(c.kind).toBe(kinds[entry.name]);
      expect(c.request_bound && n.request_bound).toBe(true);
      expect(record.kind).toBe(entry.name);
      expect(record.committed_verified).toBe(true);
      for (const source of [record, n, c]) {
        expect(source.request_body_sha256).toBe(bodyHash);
        expect(source.idempotency_key_sha256).toBe(keyHash);
      }
      if (entry.name === "lookup") {
        if (profile.schedulerWrites) expect(c.typed.state).toBe("committed");
        else requireCommittedLookup(c.typed, profile.milestoneUpdateWrites === true || profile.sprintUpdateWrites === true);
        originalLookup();
      }
    }
    proofs.push({
      seq: entry.seq,
      request_id: entry.requestID,
      native: n,
      consumer: { ...c, typed: undefined },
      go: record,
    });
    return c.typed;
  }

  async function verify() {
    if (observerError) throw observerError;
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length, {
        timeout: 5_000,
        message: "original API requests must actually finish or fail",
      })
      .toBe(0);
    if (observerError) throw observerError;
    await Promise.all(entries.map((entry) => entry.headersReady));
    if (observerError) throw observerError;
    const proof = await checkpoint();
    for (const entry of entries) {
      if (entry.checked) continue;
      const state = await entry.done;
      const response = await entry.request.response();
      if (profile.schedulerWrites && state === "failed" && entry.name === profile.cut) {
        // The proxy publishes this predicate only after its actual flush and
        // connection Close. Its original safe record can be mid-rewrite here.
        await expect.poll(() => {
          try {
            return JSON.parse(readFileSync(join(profile.evidence, `${profile.prefix}-${data.mode}-response-${entry.requestID}.json`), "utf8")).receipt_cut === true;
          } catch { return false; }
        }, { timeout: 5_000 }).toBe(true);
      }
      if (profile.skillsReads || profile.schedulerWrites || profile.knowledgeTitleReads) {
        // Include bootstrap reads as well as catalog/detail: a missing browser
        // projection is accepted only with this original request's App proof.
        const record = JSON.parse(
          readFileSync(
            join(
              profile.evidence,
              `${profile.prefix}-${data.mode}-response-${entry.requestID}.json`,
            ),
            "utf8",
          ),
        );
        const url = new URL(entry.request.url());
        expect(record.input_hash).toBe(profile.inputHash);
        expect(record.request_id).toBe(entry.requestID);
        expect(record.method).toBe(entry.request.method());
        expect(record.url_sha256).toBe(
          createHash("sha256")
            .update(url.pathname + url.search)
            .digest("hex"),
        );
        expect(record.request_host_matches).toBe(true);
        expect(record.handler_returned).toBe(true);
        expect(entry.skillsHeaders?.stage).toBe("handler_pending");
        entry.skillsHeaders!.handler_host_matches = true;
        entry.skillsHeaders!.stage = "accepted";
      }
      if (
        state === "failed" &&
        profile.cut !== undefined &&
        entry.name === profile.cut &&
        dropped === 0
      ) {
        // Go already validated this exact request's committed DB receipt before
        // physically truncating its original Content-Length response.
        expect(response?.status()).toBe(200);
        expect(entry.request.failure()).not.toBeNull();
        if (profile.schedulerWrites) {
          const readRecord = () => JSON.parse(readFileSync(join(profile.evidence, `${profile.prefix}-${data.mode}-response-${entry.requestID}.json`), "utf8"));
          const record = readRecord();
          expect(record.receipt_cut).toBe(true);
          expect(record.committed_verified && record.original_eof && record.original_closed && record.handler_returned).toBe(true);
          expect(record.kind).toBe("update");
          const rows = proof.rows.filter((r: any) => r.request_id === entry.requestID);
          expect(rows).toHaveLength(1);
          const n = rows[0], calls = proof.calls.filter((c: any) => c.id === n.call_id);
          expect(calls).toHaveLength(1);
          const c = calls[0];
          expect(proof.current && proof.not_busy).toBe(true);
          expect(c.kind).toBe("scheduler.startUpdate");
          expect(c.rejected && c.settled && !c.fulfilled).toBe(true);
          expect(c.bound).toBe(1);
          expect(n.path).toBe(new URL(entry.request.url()).pathname);
          expect(n.method).toBe("PATCH");
          expect(n.status).toBe(200);
          expect(n.fetch_returned).toBe(true);
          expect(n.readers).toBe(1);
          expect(n.release).toBe(1);
          expect(entry.finishedCount).toBe(0);
          expect(entry.failedCount).toBe(1);
          expect(c.request_bound && n.request_bound).toBe(true);
          const bodyHash = createHash("sha256").update(entry.request.postDataBuffer()!).digest("hex");
          const keyHash = createHash("sha256").update(entry.request.headers()["idempotency-key"]!).digest("hex");
          for (const source of [record, n, c]) {
            expect(source.request_body_sha256).toBe(bodyHash);
            expect(source.idempotency_key_sha256).toBe(keyHash);
          }
          proofs.push({ seq: entry.seq, request_id: entry.requestID, declared_cut: true, native: n, consumer: { ...c, typed: undefined }, go: record });
        }
        dropped++;
        entry.checked = true;
        continue;
      }
      if (
        state === "failed" &&
        (entry.request.method() === "GET" ||
          intakeWrite(entry) ||
          editWrite(entry) ||
          agentEditWrite(entry) ||
          sprintWrite(entry) ||
          sprintCreateWrite(entry) ||
          milestoneCreateWrite(entry) ||
          milestoneUpdateWrite(entry) ||
          sprintUpdateWrite(entry) ||
          variableWrite(entry) ||
          knowledgeTextWrite(entry) ||
          schedulerWrite(entry) ||
          agentWrite(entry)) &&
        response?.status() === 200 &&
        entry.failure === "net::ERR_ABORTED"
      ) {
        // Preserve the actual failed network terminal. Only independent
        // original-byte, native-reader and original-public-call proofs can
        // establish consumption; never create Response.finished() here.
        entry.body = joint(entry, proof);
        entry.checked = true;
        continue;
      }
      expect(state, `original ${entry.name} must not be silently aborted`).toBe(
        "finished",
      );
      expect(response).not.toBeNull();
      const r = response!;
      // Only genuine requestfinished permits waiting on Response.finished.
      // Keep an independent bounded observer deadline even for this branch.
      let timer: ReturnType<typeof setTimeout> | undefined;
      try {
        const terminal = await Promise.race([
          r.finished(),
          new Promise<never>((_, reject) => {
            timer = setTimeout(
              () => reject(new Error("FINISHED_RESPONSE_DID_NOT_SETTLE")),
              2_000,
            );
          }),
        ]);
        expect(terminal).toBeNull();
      } finally {
        if (timer) clearTimeout(timer);
      }
      const raw = await r.body();
      expect(raw.length).toBeLessThanOrEqual(2 << 20);
      if (r.status() === 204 && entry.name === "logout") {
        expect(raw.length).toBe(0);
        entry.checked = true;
        continue;
      }
      expect(r.headers()["content-length"]).toMatch(/^\d+$/);
      expect(Number(r.headers()["content-length"])).toBe(raw.length);
      const revoked = entry.name === "session" && r.status() === 401;
      expect(
        r.status() === 200 || revoked,
        `unexpected ${entry.name} status`,
      ).toBe(true);
      entry.body = JSON.parse(raw.toString("utf8"));
      expect(typeof entry.body).toBe("object");
      expect(entry.body).not.toBeNull();
      if (
        intakeWrite(entry) ||
        editWrite(entry) ||
        agentEditWrite(entry) ||
        sprintWrite(entry) ||
        sprintCreateWrite(entry) ||
        milestoneCreateWrite(entry) ||
        milestoneUpdateWrite(entry) ||
          sprintUpdateWrite(entry) ||
        milestonePageRead(entry) ||
        variableWrite(entry) ||
        variablePageRead(entry) ||
        knowledgeTextWrite(entry) ||
        knowledgeTextReadEntry(entry) || knowledgeTitleReadEntry(entry) ||
        schedulerWrite(entry) || schedulerReadEntry(entry) ||
        sprintPageRead(entry) ||
        agentWrite(entry) ||
        agentPageRead(entry) ||
        timelinePageRead(entry) ||
        skillsPageRead(entry)
      ) {
        // Even genuine requestfinished must match the original public value
        // and Go proof (including commit for writes); this does not replace its tail.
        const consumed = joint(entry, proof);
        if (sprintPageRead(entry) && entry.name === "sprint") {
          const { started_by, completed_by, ...detail } = entry.body as Record<
            string,
            unknown
          >;
          entry.body = detail;
        }
        if ((knowledgeTextReadEntry(entry) || knowledgeTitleReadEntry(entry)) && entry.name === "ancestors") entry.body = (entry.body as any).items;
        if (profile.schedulerWrites && entry.name === "lookup") {
          const wire = entry.body as any;
          expect(wire.state).toBe("committed");
          expect(wire.result.command).toBe("update");
          entry.body = { state: "committed", receipt: { project_id: wire.result.project.id, version: wire.result.project.version } };
        }
        if ((profile.schedulerWrites || profile.knowledgeTitleReads) && entry.name === "session") {
          const wire = entry.body as any;
          entry.body = { user: wire.user, session: wire.session };
        }
        expect(consumed).toEqual(entry.body);
        entry.body = consumed;
      }
      entry.checked = true;
    }
  }
  function receipt(
    name: "transfer" | "lookup" | "create" | "ready" | "update" | "start",
    index: number,
  ): Record<string, any> {
    const values = entries.filter(
      (entry) => entry.name === name && entry.checked && entry.body,
    );
    const body = values[index]?.body as Record<string, any> | undefined;
    expect(
      body,
      "original completed response must be observed before publication assertions",
    ).toBeTruthy();
    return body!;
  }
  function requests(name: string): readonly Request[] {
    return entries
      .filter((entry) => entry.name === name)
      .map((entry) => entry.request);
  }
  function originalLookup() {
    if (profile.lookupCommand === null)
      throw Error("TASK_TIMELINE_HAS_NO_WRITE_LOOKUP");
    if (profile.schedulerWrites) {
      const original = requests("update"), lookups = requests("lookup");
      expect(original).toHaveLength(1);
      expect(lookups).toHaveLength(1);
      expect(lookups[0]!.postDataJSON()).toEqual({ command: "update" });
      expect(lookups[0]!.headers()["idempotency-key"]).toBe(original[0]!.headers()["idempotency-key"]);
      expect(dropped).toBe(1);
      return;
    }
    const creation =
      profile.lookupCommand === "work.task.create" ||
      profile.lookupCommand === "agent.create";
    const originals = requests(
      creation
        ? "create"
        : profile.lookupCommand === "work.task.update" ||
            profile.lookupCommand === "agent.update" ||
            profile.lookupCommand === "work.milestone.update" ||
            profile.lookupCommand === "work.sprint.update"
          ? "update"
          : profile.lookupCommand === "work.sprint.start"
            ? "start"
            : "transfer",
    );
    const lookups = requests("lookup");
    expect(originals).toHaveLength(1);
    expect(lookups).toHaveLength(1);
    const sent = originals[0]!.postDataJSON();
    expect(lookups[0]!.postDataJSON()).toEqual(
      creation
        ? { command: profile.lookupCommand, request: sent.request }
        : {
            command: profile.lookupCommand,
            target_id:
              profile.lookupCommand === "work.milestone.update"
                ? data.milestone_id
                : profile.lookupCommand === "agent.update"
                ? data.agent_id
                : profile.lookupCommand === "work.sprint.start" || profile.lookupCommand === "work.sprint.update"
                  ? data.sprint_id
                  : data.task_id,
            expected_version: sent.expected_version,
            request: sent.request,
          },
    );
    expect(lookups[0]!.headers()["idempotency-key"]).toBe(
      originals[0]!.headers()["idempotency-key"],
    );
    expect(dropped).toBe(1);
  }
  function knowledgeTextRead(name: "documents" | "document" | "ancestors" | "content", target?: string) {
    expect(profile.knowledgeTextWrites).toBe(true);
    const rows = entries.filter(e => e.name === name && e.checked && e.body && (!target || new URL(e.request.url()).pathname.includes("/" + target)));
    expect(rows.length, "original Knowledge read must complete").toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function knowledgeTitleRead(name: "documents" | "titles" | "document" | "ancestors" | "content") {
    expect(profile.knowledgeTitleReads).toBe(true);
    const rows = entries.filter(e => e.name === name && e.checked && e.body);
    expect(rows.length, "original title search read must complete").toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function schedulerRead() {
    expect(profile.schedulerWrites).toBe(true);
    const rows = entries.filter(e => e.name === "scheduler" && e.checked && e.body);
    expect(rows.length, "original current Scheduler GET must complete").toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function variableRead(name: "variables" | "variable") {
    expect(profile.variableWrites).toBe(true);
    const rows = entries.filter((e) => e.name === name && e.checked && e.body);
    expect(rows.length, "original Variable public read must complete").toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function sprintEditRead(name: "project" | "sprint" | "sprints" | "milestone" | "milestones" | "agents" | "tasks") {
    expect(profile.sprintUpdateWrites).toBe(true);
    const rows = entries.filter(e => e.name === name && e.checked && e.body);
    expect(rows.length, "original Sprint Edit public read must complete").toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function milestoneRead(
    name: "project" | "milestones" | "milestone" | "sprints" | "agents",
  ) {
    expect(profile.milestoneCreateWrites || profile.milestoneUpdateWrites).toBe(true);
    const rows = entries.filter((e) => e.name === name && e.checked && e.body);
    expect(
      rows.length,
      "original Milestone public read must complete",
    ).toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function skillsRead(name: "skills-catalog" | "skill", index: number) {
    expect(profile.skillsReads).toBe(true);
    const values = entries.filter(
      (e) => e.name === name && e.checked && e.body,
    );
    expect(
      values[index],
      "original Skills public read must complete",
    ).toBeTruthy();
    return values[index]!.body as Record<string, any>;
  }
  function sprintCreateRead(
    name:
      "project" | "sprint" | "tasks" | "milestone" | "milestones" | "sprints",
  ) {
    expect(profile.sprintCreateWrites).toBe(true);
    const found = entries.filter((e) => e.name === name && e.checked && e.body);
    expect(
      found.length,
      "original Sprint Create page read must complete",
    ).toBeGreaterThan(0);
    return found.at(-1)!.body as Record<string, any>;
  }
  function sprintRead(name: "project" | "sprint" | "tasks") {
    expect(profile.sprintStartWrites).toBe(true);
    const rows = entries.filter(
      (entry) => entry.name === name && entry.checked && entry.body,
    );
    expect(
      rows.length,
      "original Sprint page read must complete",
    ).toBeGreaterThan(0);
    return rows.at(-1)!.body as Record<string, any>;
  }
  function agentRead(
    name: "agent-directory" | "agent-configuration" | "agent-models",
  ) {
    expect(
      profile.agentConfiguration ||
        profile.agentEditWrites ||
        profile.agentModelPolicy,
    ).toBe(true);
    const found = entries.filter(
      (entry) => entry.name === name && entry.checked && entry.body,
    );
    expect(
      found.length,
      "original Agent page read must complete",
    ).toBeGreaterThan(0);
    return found[found.length - 1]!.body as Record<string, any>;
  }
  function timelinePage(index: number): Record<string, any> {
    expect(profile.timelineReads).toBe(true);
    const pages = entries.filter(
      (entry) => timelinePageRead(entry) && entry.checked && entry.body,
    );
    expect(
      pages[index],
      "original Timeline public page must complete",
    ).toBeTruthy();
    return pages[index]!.body as Record<string, any>;
  }
  function agentOption(id: string): string {
    const pages = entries.filter(
      (entry) => entry.name === "agents" && entry.checked && entry.body,
    );
    for (const page of [...pages].reverse()) {
      const items = (
        page.body as {
          items: { id: string; name: string; display_name: string | null }[];
        }
      ).items;
      const agent = items.find((item) => item.id === id);
      if (agent) {
        const label = agent.display_name ?? agent.name;
        return label === agent.name ? agent.name : `${label} (${agent.name})`;
      }
    }
    throw new Error("CURRENT_AGENT_DIRECTORY_DID_NOT_INCLUDE_REQUIRED_AGENT");
  }
  function save(testStatus?: string) {
    const directory = profile.evidence;
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    writeFileSync(
      join(directory, `${profile.prefix}-${data.mode}.json`),
      JSON.stringify(
        {
          case: data.mode,
          input_hash: profile.inputHash,
          original_requests: entries.length,
          finished: entries.filter((e) => e.terminal === "finished").length,
          declared_receipt_cut: dropped,
          all_observed: entries.every((e) => e.checked),
          transitions: entries.filter((e) => e.name === "transfer").length,
          ...(profile.prefix === "task-intake"
            ? {
                creates: entries.filter((e) => e.name === "create").length,
                ready: entries.filter((e) => e.name === "ready").length,
              }
            : {}),
          ...(profile.prefix === "task-edit"
            ? {
                updates: entries.filter((e) => e.name === "update").length,
                ready: entries.filter((e) => e.name === "ready").length,
              }
            : {}),
          ...(profile.schedulerWrites ? { updates: entries.filter(e => e.name === "update").length, schedulerReads: entries.filter(e => e.name === "scheduler").length } : {}),
          ...(profile.agentEditWrites || profile.agentModelPolicy || profile.milestoneUpdateWrites || profile.sprintUpdateWrites
            ? { updates: entries.filter((e) => e.name === "update").length }
            : {}),
          ...(profile.sprintCreateWrites
            ? { creates: entries.filter((e) => e.name === "create").length }
            : {}),
          ...(profile.sprintStartWrites
            ? { starts: entries.filter((e) => e.name === "start").length }
            : {}),
          ...(profile.agentConfiguration
            ? {
                creates: entries.filter((e) => e.name === "create").length,
              }
            : {}),
          lookups: entries.filter((e) => e.name === "lookup").length,
          test_status: testStatus,
          route: diagnosticURL(page.url()),
          observer_error: observerError?.message,
          requests: entries.map((entry) => ({
            seq: entry.seq,
            name: entry.name,
            method: entry.request.method(),
            url: diagnosticURL(entry.request.url()),
            status: entry.status,
            request_id: entry.requestID,
            terminal: entry.terminal,
            failure: entry.failure,
            ...((profile.skillsReads || profile.schedulerWrites || profile.knowledgeTitleReads) && entry.skillsHeaders
              ? { skills_headers: { ...entry.skillsHeaders } }
              : {}),
            checked: entry.checked,
          })),
          request_events: events,
          browser_events: browserEvents,
          browser_events_truncated:
            browserEventsTruncated ||
            browserEvents.some((event) => event.seq === 2048),
          joint_proofs: proofs,
          observer_retirements: retirements,
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
  async function endDocument() {
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length, {
        timeout: 5_000,
      })
      .toBe(0);
    await Promise.all(entries.map((entry) => entry.headersReady));
    if (observerError) throw observerError;
    const snapshot = await checkpoint();
    const end = await page.evaluate(() =>
      (window as any).__taskReviewProof.finish(),
    );
    expect(end.retired).toBe(true);
    expect(end.failed).toBe(false);
    expect(end.pending).toBe(0);
    retirements.push({
      retired: end.retired,
      failed: end.failed,
      pending: end.pending,
      native_rows: snapshot.rows.length,
      calls: snapshot.calls.length,
    });
  }
  async function retire() {
    try {
      await endDocument();
    } finally {
      await page.unroute(entryURL, install);
      page.off("request", onRequest);
      page.off("requestfinished", finished);
      page.off("requestfailed", failed);
      page.off("response", response);
      page.off("close", closed);
    }
    expect(entryHandlers).toBe(0);
  }
  return {
    verify,
    receipt,
    originalLookup,
    requests,
    agentOption,
    agentRead,
    sprintRead,
    sprintCreateRead,
    skillsRead,
    milestoneRead,
    sprintEditRead,
    variableRead,
    knowledgeTextRead,
    knowledgeTitleRead,
    schedulerRead,
    timelinePage,
    save,
    endDocument,
    retire,
  };
}
