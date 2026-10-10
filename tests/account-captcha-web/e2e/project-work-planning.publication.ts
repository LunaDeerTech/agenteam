import { createRequire } from "node:module";
import { basename, join } from "node:path";
import { readFile, readdir } from "node:fs/promises";

export type WorkSessionBinding = {
  entry: string;
  asset: string;
  export_name: string;
  workspace_marker?: "project-workspace";
};

// The same AST rule as Model's accepted singleton selector, against Work's
// unchanged private assets. No minified name or second controller is assumed.
export async function workSessionBinding(
  root: string,
  projectRefresh = false,
): Promise<WorkSessionBinding> {
  const ts = createRequire(join(root, "web/package.json"))(
    "typescript",
  ) as typeof import("../../../web/node_modules/typescript");
  const dist = join(root, "output/ai/work-owner-planning-ui/dist");
  const matches: { asset: string; export_name: string }[] = [];
  const parse = (text: string) =>
    ts.createSourceFile(
      "asset.js",
      text,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.JS,
    );
  for (const file of (await readdir(join(dist, "assets")))
    .filter((name) => name.endsWith(".js"))
    .sort()) {
    const code = await readFile(join(dist, "assets", file), "utf8");
    if (!code.includes("getSession") || !code.includes("restore:")) continue;
    const ast = parse(code);
    const functions = new Map(
      ast.statements
        .filter(ts.isFunctionDeclaration)
        .filter((n) => n.name)
        .map((n) => [n.name!.text, n]),
    );
    for (const node of functions.values()) {
      const statements = node.body?.statements;
      if (
        node.parameters.length !== 0 ||
        statements?.length !== 1 ||
        !ts.isReturnStatement(statements[0]!)
      )
        continue;
      const expression = statements[0]!.expression;
      if (
        !expression ||
        !ts.isBinaryExpression(expression) ||
        expression.operatorToken.kind !==
          ts.SyntaxKind.QuestionQuestionEqualsToken ||
        !ts.isIdentifier(expression.left) ||
        !ts.isCallExpression(expression.right) ||
        expression.right.arguments.length !== 0 ||
        !ts.isIdentifier(expression.right.expression)
      )
        continue;
      const factory = functions.get(expression.right.expression.text),
        returns = factory?.body?.statements.filter(ts.isReturnStatement);
      if (
        returns?.length !== 1 ||
        !returns[0]!.expression ||
        !ts.isObjectLiteralExpression(returns[0]!.expression)
      )
        continue;
      const keys = returns[0]!.expression.properties.map((p) =>
        p.name?.getText(ast),
      );
      if (
        ![
          "state",
          "personalContext",
          "restore",
          "login",
          "logout",
          "leave",
          "restart",
          "projects",
          "workPlanning",
        ].every((key) => keys.includes(key))
      )
        continue;
      const exports = ast.statements
        .filter(ts.isExportDeclaration)
        .flatMap((n) =>
          n.exportClause && ts.isNamedExports(n.exportClause)
            ? [...n.exportClause.elements]
            : [],
        )
        .filter((n) => (n.propertyName ?? n.name).text === node.name!.text);
      if (exports.length !== 1)
        throw new Error("WORK_DIAGNOSTIC_SINGLETON_EXPORT");
      matches.push({
        asset: "/assets/" + file,
        export_name: exports[0]!.name.text,
      });
    }
  }
  if (matches.length !== 1) throw new Error("WORK_DIAGNOSTIC_SINGLETON_UNIQUE");
  const html = await readFile(join(dist, "index.html"), "utf8");
  const scripts = [
    ...html.matchAll(/<script\b[^>]*\bsrc="(\/assets\/[^"/]+\.js)"[^>]*>/g),
  ];
  if (scripts.length !== 1) throw new Error("WORK_DIAGNOSTIC_APP_ENTRY");
  const entry = scripts[0]![1]!,
    ast = parse(await readFile(join(dist, entry.slice(1)), "utf8"));
  const imports = ast.statements
    .filter(ts.isImportDeclaration)
    .map(
      (n) =>
        (
          n.moduleSpecifier as import("../../../web/node_modules/typescript").StringLiteral
        ).text,
    );
  if (
    imports.filter((path) => path === "./" + basename(matches[0]!.asset))
      .length !== 1
  )
    throw new Error("WORK_DIAGNOSTIC_SINGLETON_LOADED");
  if (projectRefresh) {
    let markers = 0;
    const visit = (
      node: import("../../../web/node_modules/typescript").Node,
    ) => {
      if (
        ts.isCallExpression(node) &&
        ts.isIdentifier(node.expression) &&
        node.expression.text === "Symbol" &&
        node.arguments.length === 1 &&
        (ts.isStringLiteral(node.arguments[0]!) ||
          ts.isNoSubstitutionTemplateLiteral(node.arguments[0]!)) &&
        node.arguments[0]!.text === "project-workspace"
      )
        markers++;
      ts.forEachChild(node, visit);
    };
    visit(ast);
    if (markers !== 1) throw new Error("WORK_PROJECT_WORKSPACE_MARKER");
  }
  return {
    ...matches[0]!,
    entry,
    ...(projectRefresh
      ? { workspace_marker: "project-workspace" as const }
      : {}),
  };
}

// Runs only after the existing page entry is ready. This observes public
// Session results and public DOM separately; it never consumes a response.
export async function installWorkPublicationDiagnostic({
  binding,
  expiresAt,
  independentRecovery,
}: {
  binding: WorkSessionBinding;
  expiresAt: number;
  independentRecovery?: "TestIndependentProjectWorkPlanningWebRecovery";
}) {
  const host = window as any;
  if (independentRecovery !== undefined && independentRecovery !== "TestIndependentProjectWorkPlanningWebRecovery") return "unknown-scope";
  if (Date.now() >= expiresAt) return "expired";
  if (host.__workPublicationDiagnostic) return "already-installed";
  const loaded = (path: string) =>
    performance.getEntriesByName(new URL(path, location.origin).href).length >
    0;
  if (!loaded(binding.entry) || !loaded(binding.asset))
    return "assets-unobserved";
  let module: any;
  try {
    module = await new Function("path", "return import(path)")(binding.asset);
  } catch {
    return "module-unavailable";
  }
  if (Date.now() >= expiresAt) return "expired";
  if (typeof module[binding.export_name] !== "function")
    return "singleton-unavailable";
  const auth = module[binding.export_name]();
  if (
    auth !== module[binding.export_name]() ||
    !auth.workPlanning ||
    auth.state?.phase !== "authenticated" ||
    !auth.personalContext?.identity
  )
    return "owner-unready";
  const methods = [
    "getMilestone",
    "getTask",
    "getSprint",
    "checkOriginal",
    "retryOriginal",
    ...(independentRecovery ? ["listTasks", "listSprints"] : []),
  ];
  if (
    !document.body ||
    !methods.every((name) => typeof auth.workPlanning[name] === "function")
  )
    return "facade-unavailable";
  let workspace: any;
  if (binding.workspace_marker) {
    const element = document.querySelector("#app") as any;
    const instance = element?._vnode?.component;
    const keys =
      instance?.provides &&
      Object.getOwnPropertySymbols(instance.provides).filter(
        (key) => key.description === binding.workspace_marker,
      );
    if (
      !element?.__vue_app__ ||
      element.__vue_app__._container !== element ||
      instance?.parent !== null ||
      instance?.root !== instance ||
      keys?.length !== 1
    )
      return "workspace-unavailable";
    workspace = instance.provides[keys[0]];
    if (
      !workspace?.currentReadContext ||
      !workspace.detail ||
      !workspace.blocked ||
      typeof workspace.readCurrent !== "function" ||
      typeof auth.projects?.get !== "function"
    )
      return "workspace-unavailable";
  }
  let refresh: any = null;
  const initialIdentity = { ...auth.personalContext.identity };
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  let retirementReason: "active" | "explicit" | "expired" | "failed" = "active";
  let pendingAtRetirement: number | null = null;
  let retired = false,
    observerFailed = false,
    overflow = false,
    pending = 0;
  const calls: any[] = [],
    restores: (() => void)[] = [];
  const start = performance.now();
  const at = () => performance.now() - start;
  const sameIdentity = () => {
    const identity = auth.personalContext.identity;
    return (
      !!identity &&
      identity.userID === initialIdentity.userID &&
      identity.sessionID === initialIdentity.sessionID &&
      identity.epoch === initialIdentity.epoch
    );
  };
  const safe = (work: () => void) => {
    try {
      work();
    } catch {
      observerFailed = true;
    }
  };

  // Private scope, never included in snapshots. The regular observer continues
  // to own the exact facade Promise and its original transport/finally tail.
  const independentLists: any[] = [];
  const independentRows = new Map<any, any>();
  let independentHistory: any = null;
  const keyset = (value: any) => Object.keys(value ?? {}).sort().join(",");
  const independentLive = () => !!independentRecovery && !retired && Date.now() < expiresAt && sameIdentity();
  const contextMatches = (entry: any) => {
    const current = workspace?.currentReadContext?.value;
    return !!current && current.projectID === entry?.projectID &&
      current.generation === entry.generation && current.readGeneration === entry.readGeneration &&
      current.identity?.userID === initialIdentity.userID && current.identity?.sessionID === initialIdentity.sessionID && current.identity?.epoch === initialIdentity.epoch;
  };
  const childElement = (spec: any) => document.querySelector(
    `.desktop-tree [role="treeitem"][data-tree-id="${spec.policy === "independent-blocker-tree-tasks" ? "task" : "sprint"}:${spec.childID}"]`,
  );
  const independentTarget = (name: string, args: unknown[]) => {
    if (!independentRecovery || !["listTasks", "listSprints"].includes(name)) return null;
    const entry = independentLists.find(item => !item.ended && item.name === name);
    if (!entry) return null;
    if (entry.used) { entry.invalid = true; return null; }
    entry.used = true;
    const tasks = name === "listTasks", query: any = args[1];
    entry.inputMatches = args.length === 2 && args[0] === entry.spec.projectID &&
      keyset(query) === (tasks ? "assignee_agent_id,limit,sprint_id,state" : "limit,milestone_id") &&
      query.limit === 50 && (tasks ? query.sprint_id === entry.spec.parentID && query.state === "backlog" && query.assignee_agent_id === null : query.milestone_id === entry.spec.parentID);
    if (!entry.inputMatches) entry.invalid = true;
    return { method: "GET", path: `/api/v1/projects/${entry.spec.projectID}/${tasks ? "tasks" : "sprints"}`, target_id: entry.spec.childID };
  };
  const independentRow = (name: string, row: any) => {
    const entry = independentLists.find(item => !item.ended && item.name === name && item.used && !item.row);
    if (entry) { entry.row = row; independentRows.set(row, entry); }
    const history = independentHistory;
    if (history && !history.ended && name === "checkOriginal") {
      if (history.armed) history.invalid = true;
      history.latest = { row, material: null, invalid: false, receipt: null, value: null };
    }
    if (history && !history.ended && name === "retryOriginal" && history.armed) {
      if (history.row) history.invalid = true;
      else {
        history.row = row; independentRows.set(row, history);
        const progress = auth.workPlanning.progress;
        history.entryMatches = progress?.receipt === history.latest?.receipt && progress.contextValid === true &&
          progress.phase === "confirmed" && progress.observation === "committed" && progress.canReplay === true;
        if (!history.entryMatches) history.invalid = true;
      }
    }
    if (independentRows.has(row)) Object.assign(row, { independent_input: false, independent_material: false, independent_result: false, independent_published: false, independent_valid: false });
  };
  const independentReturned = (name: string, row: any, result: any) => {
    const entry = independentRows.get(row);
    if (entry && ["listTasks", "listSprints"].includes(name)) {
      const tasks = name === "listTasks", items = result?.items;
      entry.result = result;
      row.independent_input = entry.inputMatches === true;
      row.independent_result = independentLive() && contextMatches(entry.context) && entry.inputMatches && !entry.invalid &&
        Array.isArray(items) && items.length > 0 && items.length <= 50 && result.next_cursor === undefined &&
        new Set(items.map((item: any) => item.id)).size === items.length &&
        items.every((item: any) => uuid.test(item.id) && item.project_id === entry.spec.projectID &&
          (tasks ? item.sprint_id === entry.spec.parentID && item.state === "backlog" && item.assignee_agent_id === null : item.milestone_id === entry.spec.parentID)) &&
        items.some((item: any) => item.id === entry.spec.childID);
      row.result_kind = row.independent_result ? "typed-page-returned" : "other-returned";
    }
    const history = independentHistory, latest = history?.latest;
    if (latest?.row === row && name === "checkOriginal") {
      const progress = auth.workPlanning.progress;
      latest.valid = independentLive() && !history.invalid && progress?.contextValid === true &&
        progress.phase === "confirmed" && progress.observation === "committed" && progress.domain === "structure" &&
        progress.command === "work.milestone.update" && progress.projectID === history.spec.projectID && progress.targetID === history.spec.childID &&
        result?.domain === "structure" && result.value?.state === "committed" && progress.receipt?.domain === "structure" &&
        progress.receipt.value === result.value.result && result.value.result?.command === "work.milestone.update" &&
        result.value.result.changed === true && result.value.result.milestone?.id === history.spec.childID &&
        result.value.result.milestone.project_id === history.spec.projectID && result.value.result.milestone.version === String(BigInt(history.spec.expectedVersion) + 1n) &&
        result.value.result.sprint === null && uuid.test(result.value.result.event_id);
      latest.receipt = progress?.receipt;
      latest.value = latest.valid ? JSON.stringify(result.value.result) : null;
    }
    if (entry === history && name === "retryOriginal") {
      const progress = auth.workPlanning.progress;
      row.independent_input = history.entryMatches === true;
      row.independent_result = independentLive() && !history.invalid && progress?.contextValid === true &&
        progress.phase === "confirmed" && progress.observation === "committed" && progress.domain === "structure" &&
        progress.command === "work.milestone.update" && progress.projectID === history.spec.projectID && progress.targetID === history.spec.childID &&
        result === progress.receipt && result?.domain === "structure" && JSON.stringify(result.value) === history.latest.value;
    }
  };
  const independentActions = {
    arm({ spec }: any) {
      if (!independentLive() || !workspace || auth.state.busy || !spec ||
        !["independent-blocker-tree-tasks", "independent-task-tree-sprints", "independent-history-milestone"].includes(spec.policy) ||
        ![spec.projectID, spec.parentID, spec.childID].every(id => typeof id === "string" && uuid.test(id))) return false;
      const context = workspace.currentReadContext.value;
      if (context?.projectID !== spec.projectID) return false;
      if (spec.policy === "independent-history-milestone") {
        if (independentHistory || keyset(spec) !== "childID,expectedVersion,parentID,policy,projectID" || spec.childID !== spec.parentID || !/^[1-9][0-9]{0,18}$/.test(spec.expectedVersion)) return false;
        independentHistory = { spec: Object.freeze({ ...spec }), invalid: false, ended: false, armed: false, original: null, latest: null };
      } else {
        if (keyset(spec) !== "childID,parentID,policy,projectID" || independentLists.some(item => item.spec.policy === spec.policy) ||
          !location.pathname.endsWith("/tasks/explore") || childElement(spec)) return false;
        independentLists.push({ spec: Object.freeze({ ...spec }), name: spec.policy === "independent-blocker-tree-tasks" ? "listTasks" : "listSprints",
          context: { ...context }, path: location.pathname, used: false, invalid: false, ended: false, row: null, material: null, absent: true });
      }
      return true;
    },
    history({ spec, original, lookup }: any) {
      const h = independentHistory, latest = h?.latest;
      if (!independentLive() || !h || h.invalid || h.armed || h.ended || JSON.stringify(h.spec) !== JSON.stringify(spec) ||
        !latest?.valid || latest.invalid || !latest.material || !h.original || !latest.row || latest.row.fulfilled !== 1 || latest.row.active ||
        auth.workPlanning.progress?.receipt !== latest.receipt || latest.row.native_requests !== 1) return false;
      const same = (a: any, b: any) => a && b && ["url", "method", "body", "key", "csrf", "origin"].every(key => a[key] === b[key]);
      if (!same(original, h.original) || !same(lookup, latest.material) || !uuid.test(lookup.requestID) || lookup.requestID !== latest.requestID) { h.invalid = true; return false; }
      h.armed = true; return true;
    },
    bind({ spec, material }: any) {
      const entry = spec.policy === "independent-history-milestone" ? independentHistory : independentLists.find(item => item.spec.policy === spec.policy);
      if (!independentLive() || !entry || entry.invalid || entry.ended || entry.bound || !entry.row || !entry.material || JSON.stringify(entry.spec) !== JSON.stringify(spec)) return false;
      entry.bound = true;
      const same = ["url", "method", "body", "key", "csrf", "origin"].every(key => entry.material[key] === material[key]);
      entry.row.independent_material = same;
      if (!same) entry.invalid = true;
      return same;
    },
    published({ spec }: any) {
      const entry = spec.policy === "independent-history-milestone" ? independentHistory : independentLists.find(item => item.spec.policy === spec.policy);
      if (!independentLive() || !entry || entry.invalid || entry.ended || !entry.bound || !entry.row || entry.row.fulfilled !== 1 || entry.row.active || entry.row.independent_result !== true) return false;
      if (spec.policy !== "independent-history-milestone") {
        const child = childElement(spec);
        if (!contextMatches(entry.context) || location.pathname !== entry.path || !entry.absent || !child || child.getClientRects().length === 0) return false;
      }
      entry.ended = true; entry.row.independent_published = true; entry.row.independent_valid = true;
      return true;
    },
  };

  const target = (name: string, args: unknown[]) => {
    if (["listTasks", "listSprints"].includes(name)) return independentTarget(name, args);
    if (name === "getProject") {
      if (
        !refresh ||
        args.length !== 1 ||
        typeof args[0] !== "string" ||
        !uuid.test(args[0]) ||
        args[0] !== refresh.entry?.projectID ||
        refresh.row
      )
        return null;
      return {
        method: "GET",
        path: `/api/v1/projects/${args[0]}`,
        target_id: args[0],
      };
    }
    if (["getMilestone", "getTask", "getSprint"].includes(name)) {
      if (
        args.length !== 2 ||
        !args.every((value) => typeof value === "string" && uuid.test(value))
      )
        return null;
      const entity =
        name === "getMilestone"
          ? "milestones"
          : name === "getTask"
            ? "tasks"
            : "sprints";
      return {
        method: "GET",
        path: `/api/v1/projects/${args[0]}/${entity}/${args[1]}`,
        target_id: args[1],
      };
    }
    const progress = auth.workPlanning.progress;
    if (
      args.length ||
      !progress ||
      !uuid.test(progress.projectID) ||
      !uuid.test(progress.targetID)
    )
      return null;
    if (
      name === "checkOriginal" &&
      ["task", "structure", "blocker"].includes(progress.domain)
    )
      return {
        method: "POST",
        path:
          progress.domain === "blocker"
            ? `/api/v1/projects/${progress.projectID}/tasks/${progress.targetID}/blocker-commands/lookup`
            : `/api/v1/projects/${progress.projectID}/${progress.domain}-commands/lookup`,
        target_id: progress.targetID,
      };
    if (
      name === "retryOriginal" &&
      progress.domain === "structure" &&
      progress.command === "work.milestone.update"
    )
      return {
        method: "PATCH",
        path: `/api/v1/projects/${progress.projectID}/milestones/${progress.targetID}`,
        target_id: progress.targetID,
      };
    return null;
  };
  const sampleDOM = () => {
    if (retired) return;
    safe(() => {
      const detail = document.querySelector('[aria-label="当前读取内容"]');
      const recovery = document.querySelector('[aria-label="原命令恢复"]');
      const heading = recovery?.querySelector("h2,h3")?.textContent?.trim();
      for (const row of calls) {
        row.identity_current = sameIdentity();
        row.authenticated = auth.state.phase === "authenticated";
        row.not_busy = auth.state.busy === false;
        row.detail_target_present = !!detail?.textContent?.includes(
          row.target_id,
        );
        row.entry_detail_target_present ??= row.detail_target_present;
        row.recovery_confirmed = heading === "原命令已确认";
        row.entry_recovery_confirmed ??= row.recovery_confirmed;
        row.recovery_uncertain = heading === "原命令结果不确定";
        row.replay_available = auth.workPlanning.progress?.canReplay === true;
        if (row.fulfilled && row.detail_target_present)
          row.detail_observed_after_fulfilled_at ??= at();
        if (row.fulfilled && row.recovery_confirmed)
          row.confirmed_observed_after_fulfilled_at ??= at();
        const independentEntry = independentRows.get(row);
        if (independentEntry?.invalid) row.independent_valid = false;
        row.sample_at = at();
      }
    });
  };
  let observer: MutationObserver | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const retire = (reason: "explicit" | "expired" | "failed") => {
    if (retired) return;
    sampleDOM();
    retirementReason = Date.now() >= expiresAt ? "expired" : reason;
    pendingAtRetirement = pending;
    retired = true;
    observer?.disconnect();
    clearTimeout(timer);
    for (const restore of restores.splice(0)) safe(restore);
  };
  try {
    if (workspace) {
      const original = workspace.readCurrent;
      const wrapped = function (this: unknown, ...args: unknown[]) {
        const frame: any = { entry: null, row: null };
        safe(() => {
          const entry = workspace.currentReadContext.value;
          frame.entry = entry && { ...entry, identity: { ...entry.identity } };
        });
        const previous = refresh;
        if (!retired && args.length === 0 && !previous) refresh = frame;
        let promise: Promise<unknown>;
        try {
          promise = Reflect.apply(original, this, args);
        } finally {
          refresh = previous;
        }
        if (frame.row) {
          pending++;
          void promise
            .then(
              () => {
                if (retired) return;
                safe(() => {
                  const row = frame.row,
                    context = workspace.currentReadContext.value;
                  const value = workspace.detail.project;
                  row.workspace_returned = 1;
                  row.workspace_settled_at = at();
                  row.workspace_published =
                    row.fulfilled === 1 &&
                    row.rejected === 0 &&
                    sameIdentity() &&
                    context?.identity?.userID === initialIdentity.userID &&
                    context?.identity?.sessionID ===
                      initialIdentity.sessionID &&
                    context?.identity?.epoch === initialIdentity.epoch &&
                    context?.projectID === row.target_id &&
                    context.generation === frame.entry.generation &&
                    context.readGeneration === frame.entry.readGeneration + 1 &&
                    workspace.detail.phase === "current" &&
                    // Work may synchronously start its own read after accept.
                    // This Project call's fulfilled Promise already follows its
                    // original Session owner release; final observer retirement
                    // separately requires all observed operations to settle.
                    value?.id === row.target_id &&
                    value.owner_user_id === row.result_owner_id &&
                    value.lifecycle === row.result_lifecycle &&
                    value.version === row.result_version &&
                    row.result_lifecycle === "archived";
                  row.workspace_canonical = location.pathname
                    .toLowerCase()
                    .startsWith(
                      `/${auth.state.user.username.toLowerCase()}/${value?.normalized_name}/`,
                    );
                  sampleDOM();
                });
              },
              () => {
                if (!retired)
                  safe(() => {
                    frame.row.workspace_rejected = 1;
                  });
              },
            )
            .catch(() => {
              observerFailed = true;
            })
            .then(() => {
              pending--;
            });
        }
        return promise;
      };
      workspace.readCurrent = wrapped;
      restores.push(() => {
        if (workspace.readCurrent === wrapped) workspace.readCurrent = original;
        else observerFailed = true;
      });
    }
    for (const name of [...methods, ...(workspace ? ["getProject"] : [])]) {
      const facade = name === "getProject" ? auth.projects : auth.workPlanning;
      const key = name === "getProject" ? "get" : name;
      const original = facade[key];
      if (typeof original !== "function") throw Error("facade-unavailable");
      const wrapped = function (this: unknown, ...args: unknown[]) {
        let row: any;
        if (!retired)
          safe(() => {
            const selected = target(name, args);
            if (!selected) return;
            if (calls.length >= 256) {
              overflow = true;
              return;
            }
            row = {
              call_id: calls.length + 1,
              operation: name,
              ...selected,
              call_at: at(),
              entry_identity_matches: sameIdentity(),
              entry_not_busy: auth.state.busy === false,
              fulfilled: 0,
              rejected: 0,
              synchronous_throws: 0,
              native_requests: 0,
              native_sequence: null,
              result_kind: "unobserved",
              settled_at: null,
              detail_observed_after_fulfilled_at: null,
              confirmed_observed_after_fulfilled_at: null,
              active: true,
            };
            if (name === "retryOriginal") {
              const progress = auth.workPlanning.progress;
              row.replay_from_not_observed =
                progress?.phase === "uncertain" &&
                progress.observation === "not_observed" &&
                progress.contextValid === true &&
                progress.canReplay === true &&
                progress.receipt === null;
              row.replay_receipt_published = false;
            }
            if (name === "getProject") {
              refresh.row = row;
              Object.assign(row, {
                workspace_returned: 0,
                workspace_rejected: 0,
                workspace_published: false,
                workspace_canonical: false,
                workspace_settled_at: null,
              });
            }
            calls.push(row);
            if (independentRecovery) independentRow(name, row);
            sampleDOM();
          });
        let promise: Promise<unknown>;
        try {
          promise = Reflect.apply(original, this, args);
        } catch (error) {
          if (row) {
            row.synchronous_throws++;
            row.active = false;
          }
          throw error;
        }
        if (row) {
          pending++;
          void promise
            .then(
              (value) => {
                if (retired) return;
                safe(() => {
                  row.fulfilled++;
                  row.active = false;
                  row.settled_at = at();
                  const result = value as any;
                  if (name === "getProject") {
                    row.result_owner_id = uuid.test(result?.owner_user_id)
                      ? result.owner_user_id
                      : null;
                    row.result_lifecycle = [
                      "active",
                      "archiving",
                      "archived",
                    ].includes(result?.lifecycle)
                      ? result.lifecycle
                      : null;
                    row.result_version =
                      typeof result?.version === "string" &&
                      /^[1-9][0-9]{0,18}$/.test(result.version)
                        ? result.version
                        : null;
                    row.result_target_matches = result?.id === row.target_id;
                  }
                  if (name === "retryOriginal") {
                    const progress = auth.workPlanning.progress;
                    // The actual Session Promise returns the strict receipt
                    // only after live original-intent/action checks, publishWork
                    // and runAuthorized's owner finally. Require that same
                    // receipt object in the actual confirmed publication.
                    row.replay_receipt_published =
                      row.replay_from_not_observed === true &&
                      sameIdentity() &&
                      progress?.contextValid === true &&
                      progress.phase === "confirmed" &&
                      progress.observation === "committed" &&
                      progress.domain === "structure" &&
                      progress.command === "work.milestone.update" &&
                      progress.projectID === row.path.split("/")[4] &&
                      progress.targetID === row.target_id &&
                      progress.receipt === result &&
                      result?.domain === "structure" &&
                      result.value?.command === "work.milestone.update" &&
                      result.value.changed === true &&
                      result.value.milestone?.id === row.target_id &&
                      result.value.milestone?.project_id ===
                        progress.projectID &&
                      result.value.sprint === null &&
                      uuid.test(result.value.event_id);
                  }
                  row.result_kind = name.startsWith("get")
                    ? "typed-detail-returned"
                    : name === "checkOriginal" &&
                        ((row.path.endsWith("/task-commands/lookup") &&
                          result?.domain === "task") ||
                          (row.path.endsWith("/structure-commands/lookup") &&
                            result?.domain === "structure") ||
                          (row.path.endsWith("/blocker-commands/lookup") &&
                            result?.domain === "blocker")) &&
                        ["committed", "in_progress", "not_observed"].includes(
                          result.domain === "structure"
                            ? result.value?.state
                            : result.value?.status,
                        )
                      ? result.domain === "structure"
                        ? result.value.state
                        : result.value.status
                      : name === "retryOriginal" &&
                          result?.domain === "structure"
                        ? "typed-receipt-returned"
                        : "other-returned";
                  if (independentRecovery) independentReturned(name, row, result);
                  sampleDOM();
                });
              },
              () => {
                if (!retired)
                  safe(() => {
                    row.rejected++;
                    row.active = false;
                    row.settled_at = at();
                    sampleDOM();
                  });
              },
            )
            .catch(() => {
              observerFailed = true;
            })
            .then(() => {
              pending--;
            });
        }
        return promise;
      };
      facade[key] = wrapped;
      restores.push(() => {
        if (facade[key] === wrapped) facade[key] = original;
        else observerFailed = true;
      });
    }
    observer = new MutationObserver(sampleDOM);
    observer.observe(document.body, {
      subtree: true,
      childList: true,
      characterData: true,
      attributes: true,
    });
    const snapshot = () => {
      sampleDOM();
      return {
        retirement_reason: retirementReason,
        pending_at_retirement: pendingAtRetirement,
        retired,
        observer_failed: observerFailed,
        overflow,
        pending_observations: pending,
        clock: "browser-monotonic-observed-relative-to-publication-install",
        calls: calls.map((row) => ({ ...row })),
      };
    };
    timer = setTimeout(
      () => retire("expired"),
      Math.max(0, expiresAt - Date.now()),
    );
    host.__workPublicationDiagnostic = {
      snapshot,
      independentAction(action: string, value: any) {
        if (!independentRecovery || !Object.prototype.hasOwnProperty.call(independentActions, action)) return false;
        return independentActions[action as keyof typeof independentActions](value);
      },
      independentFetch(method: string, url: string, material: any, sequence: number) {
        if (!independentLive()) return;
        const parsed = new URL(url), h = independentHistory;
        const value = { url, method, ...material };
        if (h && !h.ended && parsed.pathname.startsWith(`/api/v1/projects/${h.spec.projectID}/`) && method !== "GET") {
          if (!h.original) {
            if (method !== "PATCH" || parsed.search || parsed.pathname !== `/api/v1/projects/${h.spec.projectID}/milestones/${h.spec.childID}`) h.invalid = true;
            h.original = value;
          } else if (method === "POST" && parsed.pathname.endsWith("/lookup")) {
            if (h.armed || !h.latest || h.latest.material) h.invalid = true;
            if (h.latest) { h.latest.material = value; h.latest.sequence = sequence; }
          } else if (h.armed && !h.material) {
            h.material = value;
            if (method !== "PATCH" || ["url", "method", "body", "key", "csrf", "origin"].some(key => value[key] !== h.original[key])) h.invalid = true;
          } else h.invalid = true;
        }
        for (const entry of independentLists) {
          if (entry.ended || parsed.pathname !== `/api/v1/projects/${entry.spec.projectID}/${entry.name === "listTasks" ? "tasks" : "sprints"}`) continue;
          if (entry.material || !entry.used || !entry.row) { entry.invalid = true; continue; }
          entry.material = value;
          const expected = entry.name === "listTasks" ? { sprint_id: entry.spec.parentID, state: "backlog", assignee_agent_id: "null", limit: "50" } : { milestone_id: entry.spec.parentID, limit: "50" };
          if (method !== "GET" || material.body !== null || parsed.origin !== location.origin ||
            [...parsed.searchParams].length !== Object.keys(expected).length ||
            !Object.entries(expected).every(([key, value]) => parsed.searchParams.getAll(key).length === 1 && parsed.searchParams.get(key) === value)) entry.invalid = true;
        }
      },
      independentResponse(sequence: number, requestID: string) {
        const latest = independentHistory?.latest;
        if (independentLive() && latest?.sequence === sequence) latest.requestID = requestID;
      },
      bindNative(method: string, path: string, sequence: number) {
        if (retired) return null;
        const matches = calls.filter(
          (row) =>
            row.active &&
            row.method === method &&
            row.path === path &&
            row.entry_identity_matches &&
            sameIdentity(),
        );
        if (matches.length !== 1) return null;
        const row = matches[0]!;
        row.native_requests++;
        if (row.native_requests !== 1) {
          row.native_sequence = null;
          return null;
        }
        row.native_sequence = sequence;
        return row.call_id;
      },
      finish() {
        retire("explicit");
        return snapshot();
      },
    };
    return "installed";
  } catch {
    retire("failed");
    return "observer-unavailable";
  }
}
