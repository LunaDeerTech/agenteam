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
}: {
  binding: WorkSessionBinding;
  expiresAt: number;
}) {
  const host = window as any;
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
  const history = new Map<string, { row: any; published: any; value: any }>();
  const replayAnchors = new WeakMap<
    object,
    { row: any; published: any; value: any }
  >();
  let armedReplay: {
    policy: "not-observed-milestone" | "historical-task";
    anchor?: { row: any; published: any; value: any };
  } | null = null;
  const methods = [
    "getMilestone",
    "getTask",
    "getSprint",
    "checkOriginal",
    "retryOriginal",
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
  const target = (name: string, args: unknown[]) => {
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
      ((progress.domain === "structure" &&
        progress.command === "work.milestone.update") ||
        (progress.domain === "task" && progress.command === "work.task.update"))
    )
      return {
        method: "PATCH",
        path: `/api/v1/projects/${progress.projectID}/${progress.domain === "task" ? "tasks" : "milestones"}/${progress.targetID}`,
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
                armedReplay?.policy === "not-observed-milestone" &&
                progress?.phase === "uncertain" &&
                progress.observation === "not_observed" &&
                progress.contextValid === true &&
                progress.canReplay === true &&
                progress.receipt === null;
              const anchor =
                armedReplay?.policy === "historical-task"
                  ? armedReplay.anchor
                  : undefined;
              row.replay_from_history =
                !!anchor &&
                sameIdentity() &&
                progress?.contextValid === true &&
                progress.phase === "confirmed" &&
                progress.observation === "committed" &&
                progress.canReplay === true &&
                progress.domain === "task" &&
                progress.command === "work.task.update" &&
                progress.receipt === anchor.published;
              row.history_lookup_call_id = row.replay_from_history
                ? anchor!.row.call_id
                : null;
              if (row.replay_from_history) replayAnchors.set(row, anchor!);
              armedReplay = null;
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
                    const common =
                      sameIdentity() &&
                      progress?.contextValid === true &&
                      progress.phase === "confirmed" &&
                      progress.observation === "committed" &&
                      progress.projectID === row.path.split("/")[4] &&
                      progress.targetID === row.target_id &&
                      progress.receipt === result;
                    const anchor = replayAnchors.get(row);
                    row.replay_receipt_published =
                      common &&
                      ((row.replay_from_not_observed === true &&
                        progress.domain === "structure" &&
                        progress.command === "work.milestone.update" &&
                        result?.domain === "structure" &&
                        result.value?.command === "work.milestone.update" &&
                        result.value.changed === true &&
                        result.value.milestone?.id === row.target_id &&
                        result.value.milestone?.project_id ===
                          progress.projectID &&
                        result.value.sprint === null &&
                        uuid.test(result.value.event_id)) ||
                        (row.replay_from_history === true &&
                          !!anchor &&
                          progress.domain === "task" &&
                          progress.command === "work.task.update" &&
                          result?.domain === "task" &&
                          result.value?.task?.id === row.target_id &&
                          result.value.task.project_id === progress.projectID &&
                          JSON.stringify(result.value) ===
                            JSON.stringify(anchor.value)));
                  }
                  if (
                    name === "checkOriginal" &&
                    row.path.endsWith("/task-commands/lookup")
                  ) {
                    const progress = auth.workPlanning.progress;
                    row.history_receipt_published =
                      sameIdentity() &&
                      progress?.contextValid === true &&
                      progress.phase === "confirmed" &&
                      progress.observation === "committed" &&
                      progress.domain === "task" &&
                      progress.command === "work.task.update" &&
                      progress.projectID === row.path.split("/")[4] &&
                      progress.targetID === row.target_id &&
                      result?.domain === "task" &&
                      result.value?.status === "committed" &&
                      progress.receipt?.domain === "task" &&
                      progress.receipt.value === result.value.receipt;
                    if (row.history_receipt_published)
                      history.set(
                        row.path.split("/")[4] + "/" + row.target_id,
                        {
                          row,
                          published: progress.receipt,
                          value: result.value.receipt,
                        },
                      );
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
                          ["structure", "task"].includes(result?.domain)
                        ? "typed-receipt-returned"
                        : "other-returned";
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
      armReplay(policy: "not-observed-milestone" | "historical-task") {
        if (retired || armedReplay || !sameIdentity() || auth.state.busy)
          return false;
        const progress = auth.workPlanning.progress;
        if (policy === "not-observed-milestone") {
          if (
            progress?.domain !== "structure" ||
            progress.command !== "work.milestone.update" ||
            progress.phase !== "uncertain" ||
            progress.observation !== "not_observed" ||
            !progress.canReplay
          )
            return false;
          armedReplay = { policy };
          return true;
        }
        if (
          policy !== "historical-task" ||
          progress?.domain !== "task" ||
          progress.command !== "work.task.update" ||
          progress.phase !== "confirmed" ||
          progress.observation !== "committed" ||
          !progress.canReplay
        )
          return false;
        const anchor = history.get(
          progress.projectID + "/" + progress.targetID,
        );
        if (
          !anchor ||
          anchor.published !== progress.receipt ||
          anchor.row.active ||
          anchor.row.fulfilled !== 1
        )
          return false;
        armedReplay = { policy, anchor };
        return true;
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
