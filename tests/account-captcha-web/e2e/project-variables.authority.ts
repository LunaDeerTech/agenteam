import { readFileSync, readdirSync } from "node:fs";
import { basename, join, relative, resolve } from "node:path";
import { createRequire } from "node:module";

export type VariableAuthorityBinding = {
  entry: string;
  asset: string;
  singleton: string;
  failure: string;
};
export function variableAuthorityProjection(value: unknown) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const row = value as Record<string, unknown>;
  const counts = [
    "calls",
    "target_calls",
    "fulfilled",
    "rejected",
    "synchronous_throws",
    "status",
    "native_before",
    "native_after",
    "pending",
  ];
  const flags = [
    "typed_problem",
    "instance_matches",
    "entry_authenticated",
    "entry_idle",
    "entry_identity",
    "identity_current",
    "authenticated",
    "owner_idle",
    "progress_rejected",
    "receipt_absent",
    "draft_matches",
    "document_matches",
    "target_route",
    "native_request_matches",
    "problem_request_matches",
    "observer_failed",
    "hooks_retired",
  ];
  if (
    Object.keys(row).length !== counts.length + flags.length + 2 ||
    !counts.every(
      (k) =>
        Number.isSafeInteger(row[k]) &&
        Number(row[k]) >= 0 &&
        Number(row[k]) <= 4096,
    ) ||
    Number(row.status) > 599 ||
    !flags.every((k) => typeof row[k] === "boolean") ||
    typeof row.code !== "string" ||
    !["none", "PROJECT_NOT_ACTIVE", "other"].includes(row.code) ||
    typeof row.commit_state !== "string" ||
    !["none", "not_started", "not_committed", "unknown", "other"].includes(
      row.commit_state,
    )
  )
    return null;
  return Object.fromEntries(
    [...counts, ...flags, "code", "commit_state"].map((k) => [k, row[k]]),
  );
}
// Adapted from Model's reviewed session-controller-binding AST method. Inspect
// this exact Variables dist, never guess minified names or bundle a new Session.
export function variableAuthorityBinding(
  repository: string,
  dist: string,
): VariableAuthorityBinding {
  const inside = relative(
    join(repository, "output/ai/project-variables-ui"),
    resolve(dist),
  );
  if (!inside || inside.startsWith("..") || !dist.startsWith("/"))
    throw new Error("VARIABLE_AUTHORITY_DIST");
  const ts = createRequire(join(repository, "web/package.json"))("typescript");
  const parse = (code: string) =>
    ts.createSourceFile(
      "asset.js",
      code,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.JS,
    );
  const matches: { asset: string; singleton: string; ast: any }[] = [];
  const exportsFor = (ast: any, name: string) =>
    ast.statements
      .filter(ts.isExportDeclaration)
      .flatMap((n: any) =>
        n.exportClause && ts.isNamedExports(n.exportClause)
          ? [...n.exportClause.elements]
          : [],
      )
      .filter((n: any) => (n.propertyName ?? n.name).text === name);
  for (const file of readdirSync(join(dist, "assets"))
    .filter((n) => n.endsWith(".js"))
    .sort()) {
    const ast = parse(readFileSync(join(dist, "assets", file), "utf8"));
    const functions = new Map<string, any>(
      ast.statements
        .filter(ts.isFunctionDeclaration)
        .filter((n: any) => n.name)
        .map((n: any) => [n.name.text, n]),
    );
    for (const node of functions.values()) {
      const statements = node.body?.statements;
      if (
        node.parameters.length !== 0 ||
        statements?.length !== 1 ||
        !ts.isReturnStatement(statements[0])
      )
        continue;
      const expression = statements[0].expression;
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
        !returns[0].expression ||
        !ts.isObjectLiteralExpression(returns[0].expression)
      )
        continue;
      const keys = returns[0].expression.properties.map((p: any) =>
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
          "projectVariables",
        ].every((k) => keys.includes(k))
      )
        continue;
      const exports = exportsFor(ast, node.name.text);
      if (exports.length !== 1) throw new Error("VARIABLE_AUTHORITY_EXPORT");
      matches.push({
        asset: "/assets/" + file,
        singleton: exports[0].name.text,
        ast,
      });
    }
  }
  if (matches.length !== 1) throw new Error("VARIABLE_AUTHORITY_SINGLETON");
  const match = matches[0]!,
    classes: { name: string; node: any }[] = [];
  for (const statement of match.ast.statements) {
    if (ts.isClassDeclaration(statement) && statement.name)
      classes.push({ name: statement.name.text, node: statement });
    if (ts.isVariableStatement(statement))
      for (const d of statement.declarationList.declarations)
        if (
          ts.isIdentifier(d.name) &&
          d.initializer &&
          ts.isClassExpression(d.initializer)
        )
          classes.push({ name: d.name.text, node: d.initializer });
  }
  const failures = classes.filter(({ node }) => {
    if (
      !node.heritageClauses?.some(
        (c: any) =>
          c.token === ts.SyntaxKind.ExtendsKeyword &&
          c.types.length === 1 &&
          c.types[0].expression.getText(match.ast) === "Error",
      )
    )
      return false;
    const ctor = node.members.find(ts.isConstructorDeclaration);
    if (
      !ctor ||
      ctor.parameters.length !== 2 ||
      !ctor.parameters.every((p: any) => ts.isIdentifier(p.name))
    )
      return false;
    const assigned = new Map<string, any>();
    const visit = (n: any) => {
      if (
        ts.isBinaryExpression(n) &&
        n.operatorToken.kind === ts.SyntaxKind.EqualsToken &&
        ts.isPropertyAccessExpression(n.left) &&
        n.left.expression.kind === ts.SyntaxKind.ThisKeyword
      )
        assigned.set(n.left.name.text, n.right);
      ts.forEachChild(n, visit);
    };
    visit(ctor);
    return (
      assigned.get("kind")?.getText(match.ast) ===
        ctor.parameters[0].name.text &&
      assigned.get("problem")?.getText(match.ast) ===
        ctor.parameters[1].name.text &&
      assigned.has("name") &&
      ts.isStringLiteralLike(assigned.get("name")) &&
      assigned.get("name").text === "AccountFailure"
    );
  });
  if (failures.length !== 1) throw new Error("VARIABLE_AUTHORITY_FAILURE");
  const exports = exportsFor(match.ast, failures[0]!.name);
  const scripts = [
    ...readFileSync(join(dist, "index.html"), "utf8").matchAll(
      /<script\b[^>]*\bsrc="(\/assets\/[^"/]+\.js)"[^>]*>/g,
    ),
  ];
  if (exports.length !== 1 || scripts.length !== 1)
    throw new Error("VARIABLE_AUTHORITY_ENTRY");
  const entry = scripts[0]![1]!,
    ast = parse(readFileSync(join(dist, entry.slice(1)), "utf8"));
  const imports = ast.statements
    .filter(ts.isImportDeclaration)
    .map((n: any) => n.moduleSpecifier.text);
  if (
    imports.filter((p: string) => p === "./" + basename(match.asset)).length !==
    1
  )
    throw new Error("VARIABLE_AUTHORITY_LOADED");
  return {
    entry,
    asset: match.asset,
    singleton: match.singleton,
    failure: exports[0].name.text,
  };
}

// Serialized by locked Playwright into the original document. Diagnostic only:
// return exactly the original start Promise, never issue or consume a request.
export async function installVariableAuthority({
  binding,
  target,
}: {
  binding: VariableAuthorityBinding;
  target: {
    project: string;
    variable: string;
    version: string;
    value: string;
    route: string;
  };
}) {
  const host = window as any;
  if (host.__variableAuthority) return false;
  const loaded = (path: string) =>
    performance.getEntriesByName(new URL(path, location.origin).href).length >
    0;
  if (!loaded(binding.entry) || !loaded(binding.asset)) return false;
  let module: any;
  try {
    module = await new Function("path", "return import(path)")(binding.asset);
  } catch {
    return false;
  }
  if (
    typeof module[binding.singleton] !== "function" ||
    typeof module[binding.failure] !== "function"
  )
    return false;
  const auth = module[binding.singleton](),
    Failure = module[binding.failure];
  if (
    auth !== module[binding.singleton]() ||
    typeof auth?.projectVariables?.start !== "function" ||
    !(Failure.prototype instanceof Error)
  )
    return false;
  if (
    auth.state.phase !== "authenticated" ||
    auth.state.busy ||
    !auth.personalContext.identity
  )
    return false;
  const native = host.__variableNativeDiagnostic;
  const initial = native?.snapshot(false);
  if (!initial || initial.retired || typeof initial.document !== "string")
    return false;
  const documentID = initial.document,
    identity = { ...auth.personalContext.identity };
  const endpoint = `/api/v1/projects/${target.project}/variables/${target.variable}`;
  const original = auth.projectVariables.start;
  let retired = false,
    requestID = "",
    pending = 0;
  const facts = {
    calls: 0,
    target_calls: 0,
    fulfilled: 0,
    rejected: 0,
    synchronous_throws: 0,
    typed_problem: false,
    status: 0,
    code: "none",
    commit_state: "none",
    instance_matches: false,
    entry_authenticated: false,
    entry_idle: false,
    entry_identity: false,
    identity_current: false,
    authenticated: false,
    owner_idle: false,
    progress_rejected: false,
    receipt_absent: false,
    draft_matches: false,
    document_matches: false,
    target_route: false,
    native_before: 0,
    native_after: 0,
    native_request_matches: false,
    problem_request_matches: false,
    observer_failed: false,
    hooks_retired: false,
  };
  const safe = (work: () => void) => {
    try {
      work();
    } catch {
      facts.observer_failed = true;
    }
  };
  const same = () => {
    const current = auth.personalContext.identity;
    return (
      !!current &&
      current.userID === identity.userID &&
      current.sessionID === identity.sessionID &&
      current.epoch === identity.epoch
    );
  };
  const selected = (row: any) =>
    row.method === "PATCH" && row.path === endpoint && row.query === "";
  function wrapped(this: unknown, ...args: unknown[]) {
    if (retired) return Reflect.apply(original, this, args);
    safe(() => {
      facts.calls++;
      const command = args[0] as any;
      if (
        args.length === 1 &&
        command &&
        Object.keys(command).length === 5 &&
        command.kind === "update" &&
        command.projectID === target.project &&
        command.targetID === target.variable &&
        command.expectedVersion === target.version &&
        command.request &&
        Object.keys(command.request).length === 1 &&
        command.request.value === target.value
      )
        facts.target_calls++;
      if (facts.calls === 1) {
        facts.entry_authenticated = auth.state.phase === "authenticated";
        facts.entry_idle = auth.state.busy === false;
        facts.entry_identity = same();
        const before = native.snapshot(false);
        if (before.document !== documentID) {
          facts.observer_failed = true;
          return;
        }
        facts.native_before = before.records.filter(selected).length;
      }
    });
    let promise: Promise<unknown>;
    try {
      promise = Reflect.apply(original, this, args);
    } catch (error) {
      safe(() => {
        facts.synchronous_throws++;
      });
      throw error;
    }
    pending++;
    try {
      void promise
        .then(
          () => {
            if (!retired)
              safe(() => {
                facts.fulfilled++;
              });
          },
          (error) => {
            if (retired) return;
            safe(() => {
              facts.rejected++;
              const problem =
                error instanceof Failure && error.kind === "problem"
                  ? error.problem
                  : null;
              if (!problem || typeof problem !== "object") return;
              facts.typed_problem = true;
              facts.status =
                Number.isInteger(problem.status) &&
                problem.status >= 100 &&
                problem.status <= 599
                  ? problem.status
                  : 0;
              facts.code =
                problem.code === "PROJECT_NOT_ACTIVE"
                  ? "PROJECT_NOT_ACTIVE"
                  : "other";
              facts.commit_state = [
                "not_started",
                "not_committed",
                "unknown",
              ].includes(problem.commit_state)
                ? problem.commit_state
                : "other";
              facts.instance_matches = problem.instance === endpoint;
              if (
                /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                  problem.request_id,
                )
              )
                requestID = problem.request_id;
            });
          },
        )
        .catch(() => {
          if (!retired) facts.observer_failed = true;
        })
        .then(() => {
          pending--;
        });
    } catch {
      pending--;
      facts.observer_failed = true;
    }
    return promise;
  }
  const snapshot = (expectedID: string | null) => {
    safe(() => {
      const current = native.snapshot(false),
        progress = auth.projectVariables.progress;
      facts.document_matches = current.document === documentID;
      facts.native_after = current.records.filter(selected).length;
      const matches = current.records.filter(
        (r: any) => !!expectedID && r.request_id === expectedID,
      );
      facts.native_request_matches =
        facts.document_matches &&
        matches.length === 1 &&
        selected(matches[0]) &&
        matches[0].status === facts.status &&
        facts.native_before === 0 &&
        facts.native_after === 1;
      facts.problem_request_matches = !!requestID && requestID === expectedID;
      facts.identity_current = same();
      facts.authenticated = auth.state.phase === "authenticated";
      facts.owner_idle = auth.state.busy === false;
      facts.progress_rejected =
        progress?.projectID === target.project &&
        progress?.targetID === target.variable &&
        progress?.kind === "update" &&
        progress?.phase === "rejected";
      facts.receipt_absent = !!progress && progress.receipt === null;
      facts.target_route = location.pathname === target.route;
      const labels = [
        ...document.querySelectorAll(".variable-editor label"),
      ].filter((n) => n.textContent?.trim() === "值");
      const control =
        labels.length === 1
          ? document.getElementById(labels[0]!.getAttribute("for") ?? "")
          : null;
      facts.draft_matches =
        control instanceof HTMLTextAreaElement &&
        control.value === target.value;
    });
    return { ...facts, pending };
  };
  const retire = () => {
    if (retired) return;
    retired = true;
    try {
      if (auth.projectVariables.start === wrapped) {
        auth.projectVariables.start = original;
        facts.hooks_retired = auth.projectVariables.start === original;
      } else facts.observer_failed = true;
    } catch {
      facts.observer_failed = true;
    }
  };
  try {
    auth.projectVariables.start = wrapped;
    if (auth.projectVariables.start !== wrapped) return false;
  } catch {
    return false;
  }
  host.__variableAuthority = {
    snapshot(expectedID: string | null) {
      return retired ? null : snapshot(expectedID);
    },
    finish(expectedID: string | null) {
      if (retired) return null;
      const result = snapshot(expectedID);
      retire();
      result.hooks_retired = facts.hooks_retired;
      result.observer_failed = facts.observer_failed;
      delete host.__variableAuthority;
      return result;
    },
  };
  return true;
}
