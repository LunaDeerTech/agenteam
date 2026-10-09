import { createRequire } from "node:module";
import { basename, join } from "node:path";
import { readFile, readdir } from "node:fs/promises";

export type WorkSessionBinding = {
  entry: string;
  asset: string;
  export_name: string;
};

// The same AST rule as Model's accepted singleton selector, against Work's
// unchanged private assets. No minified name or second controller is assumed.
export async function workSessionBinding(
  root: string,
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
  return { ...matches[0]!, entry };
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
  const initialIdentity = { ...auth.personalContext.identity };
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
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
    if (name === "checkOriginal" && progress.domain === "task")
      return {
        method: "POST",
        path: `/api/v1/projects/${progress.projectID}/task-commands/lookup`,
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
        row.recovery_confirmed = heading === "原命令已确认";
        row.recovery_uncertain = heading === "原命令结果不确定";
        row.replay_available = auth.workPlanning.progress?.canReplay === true;
        if (row.fulfilled && row.detail_target_present)
          row.first_detail_after_fulfilled ??= at();
        if (row.fulfilled && row.recovery_confirmed)
          row.first_confirmed_after_fulfilled ??= at();
        row.sample_at = at();
      }
    });
  };
  for (const name of methods) {
    const original = auth.workPlanning[name];
    if (typeof original !== "function") return "facade-unavailable";
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
            first_detail_after_fulfilled: null,
            first_confirmed_after_fulfilled: null,
            active: true,
          };
          calls.push(row);
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
                row.result_kind = name.startsWith("get")
                  ? "typed-detail-returned"
                  : name === "checkOriginal" &&
                      result?.domain === "task" &&
                      ["committed", "in_progress", "not_observed"].includes(
                        result.value?.status,
                      )
                    ? result.value.status
                    : name === "retryOriginal" && result?.domain === "structure"
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
    auth.workPlanning[name] = wrapped;
    restores.push(() => {
      if (auth.workPlanning[name] === wrapped)
        auth.workPlanning[name] = original;
      else observerFailed = true;
    });
  }
  const observer = new MutationObserver(sampleDOM);
  observer.observe(document.body, {
    subtree: true,
    childList: true,
    characterData: true,
    attributes: true,
  });
  const snapshot = () => {
    sampleDOM();
    return {
      retired,
      observer_failed: observerFailed,
      overflow,
      pending_observations: pending,
      clock: "browser-monotonic-observed-relative-to-publication-install",
      calls: calls.map((row) => ({ ...row })),
    };
  };
  const retire = () => {
    if (retired) return;
    sampleDOM();
    retired = true;
    observer.disconnect();
    clearTimeout(timer);
    for (const restore of restores.splice(0)) restore();
  };
  const timer = setTimeout(retire, Math.max(0, expiresAt - Date.now()));
  host.__workPublicationDiagnostic = {
    snapshot,
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
      retire();
      return snapshot();
    },
  };
  return "installed";
}
