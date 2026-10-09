import type { VariableAuthorityBinding } from "./project-variables.authority";
import type { ProjectVariable } from "../../../web/src/api/project-variables";

const detailCounts = [
  "calls",
  "target_calls",
  "fulfilled",
  "rejected",
  "synchronous_throws",
  "pending",
  "native_before",
  "native_after",
] as const;
const detailFlags = [
  "entry_idle",
  "entry_authenticated",
  "entry_empty",
  "identity_current",
  "authenticated",
  "owner_idle",
  "document_matches",
  "target_route",
  "native_request_matches",
  "body_matches",
  "fresh_editor",
  "published",
  "observer_failed",
  "hooks_retired",
] as const;
export function variableDetailProjection(value: unknown) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const row = value as Record<string, unknown>;
  if (
    Object.keys(row).length !== detailCounts.length + detailFlags.length ||
    !detailCounts.every(
      (k) =>
        Number.isSafeInteger(row[k]) &&
        Number(row[k]) >= 0 &&
        Number(row[k]) <= 4096,
    ) ||
    !detailFlags.every((k) => typeof row[k] === "boolean")
  )
    return null;
  return Object.fromEntries(
    [...detailCounts, ...detailFlags].map((k) => [k, row[k]]),
  );
}

// One authority-stage GET only. This hook observes the actual loaded Session's
// public Promise, preserving the original receiver, arguments, return and throw.
// The result stays in this document until compared to the bound private response.
export async function installVariableDetail({
  binding,
  target,
}: {
  binding: VariableAuthorityBinding;
  target: { project: string; variable: string; route: string };
}) {
  const host = window as any;
  if (host.__variableDetail) return false;
  const loaded = (p: string) =>
    performance.getEntriesByName(new URL(p, location.origin).href).length > 0;
  if (!loaded(binding.entry) || !loaded(binding.asset)) return false;
  let module: any;
  try {
    module = await new Function("path", "return import(path)")(binding.asset);
  } catch {
    return false;
  }
  if (typeof module[binding.singleton] !== "function") return false;
  const auth = module[binding.singleton]();
  if (
    auth !== module[binding.singleton]() ||
    typeof auth?.projectVariables?.get !== "function" ||
    auth.state.phase !== "authenticated" ||
    auth.state.busy ||
    !auth.personalContext.identity ||
    document.querySelectorAll(".variable-editor").length !== 0 ||
    location.pathname !== target.route
  )
    return false;
  const native = host.__variableNativeDiagnostic,
    initial = native?.snapshot(false);
  if (!initial || initial.retired || typeof initial.document !== "string")
    return false;
  const original = auth.projectVariables.get,
    identity = { ...auth.personalContext.identity },
    documentID = initial.document;
  const endpoint = `/api/v1/projects/${target.project}/variables/${target.variable}`;
  const selected = (row: any) =>
    row.method === "GET" && row.path === endpoint && row.query === "";
  const fields = [
    "id",
    "project_id",
    "type",
    "name",
    "description",
    "version",
    "created_at",
    "updated_at",
    "value",
  ];
  let retired = false,
    result: any = null,
    pending = 0;
  const facts = {
    calls: 0,
    target_calls: 0,
    fulfilled: 0,
    rejected: 0,
    synchronous_throws: 0,
    native_before: 0,
    native_after: 0,
    entry_idle: false,
    entry_authenticated: false,
    entry_empty: false,
    identity_current: false,
    authenticated: false,
    owner_idle: false,
    document_matches: false,
    target_route: false,
    native_request_matches: false,
    body_matches: false,
    fresh_editor: false,
    published: false,
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
  const authenticated = () =>
    auth.state.phase === "authenticated" &&
    auth.personalContext.phase === "current" &&
    auth.state.user?.id === identity.userID &&
    auth.state.session?.id === identity.sessionID;
  function wrapped(this: unknown, ...args: unknown[]) {
    if (retired) return Reflect.apply(original, this, args);
    safe(() => {
      facts.calls++;
      if (
        args.length === 2 &&
        args[0] === target.project &&
        args[1] === target.variable
      )
        facts.target_calls++;
      if (facts.calls === 1) {
        facts.entry_idle = auth.state.busy === false;
        facts.entry_authenticated = authenticated() && same();
        // The controller opens an empty shell immediately before calling get.
        const forms = document.querySelectorAll(
          ".variable-editor .variable-form",
        );
        facts.entry_empty = forms.length === 0;
        const before = native.snapshot(false);
        if (before.document !== documentID || before.retired) {
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
          (value) => {
            if (!retired)
              safe(() => {
                facts.fulfilled++;
                result = value;
              });
          },
          () => {
            if (!retired)
              safe(() => {
                facts.rejected++;
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
  const snapshot = (input: {
    id: string | null;
    value: ProjectVariable | null;
  }) => {
    safe(() => {
      const current = native.snapshot(false);
      facts.document_matches =
        current.document === documentID && !current.retired;
      facts.native_after = current.records.filter(selected).length;
      const matches = current.records.filter(
        (r: any) => !!input.id && r.request_id === input.id,
      );
      facts.native_request_matches =
        facts.document_matches &&
        matches.length === 1 &&
        selected(matches[0]) &&
        matches[0].status === 200 &&
        facts.native_before === 0 &&
        facts.native_after === 1;
      facts.identity_current = same();
      facts.authenticated = authenticated();
      facts.owner_idle = auth.state.busy === false;
      facts.target_route = location.pathname === target.route;
      const expected = input.value as any;
      facts.body_matches =
        !!expected &&
        !!result &&
        typeof result === "object" &&
        !Array.isArray(result) &&
        Object.keys(result).length === fields.length &&
        fields.every(
          (k) => typeof result[k] === "string" && result[k] === expected[k],
        ) &&
        result.id === target.variable &&
        result.project_id === target.project &&
        result.type === "variable";
      const editors = document.querySelectorAll(".variable-editor");
      facts.fresh_editor = facts.entry_empty && editors.length === 1;
      if (!facts.body_matches || !facts.fresh_editor) return;
      const editor = editors[0]!,
        meta = editor.querySelectorAll(".meta");
      const field = (label: string) => {
        const labels = [...editor.querySelectorAll("label")].filter((n) => {
          const copy = n.cloneNode(true) as HTMLElement;
          for (const hidden of copy.querySelectorAll('[aria-hidden="true"]'))
            hidden.remove();
          return copy.textContent?.trim() === label;
        });
        return labels.length === 1
          ? document.getElementById(labels[0]!.getAttribute("for") ?? "")
          : null;
      };
      const name = field("名称"),
        description = field("描述"),
        value = field("值");
      facts.published =
        meta.length === 1 &&
        meta[0]!.textContent?.replace(/\s+/g, " ").trim() ===
          `变量 ID：${result.id} · 已读版本 ${result.version}` &&
        name instanceof HTMLInputElement &&
        name.value === result.name &&
        !name.disabled &&
        description instanceof HTMLTextAreaElement &&
        description.value === result.description &&
        !description.disabled &&
        value instanceof HTMLTextAreaElement &&
        value.value === result.value &&
        !value.disabled &&
        editor.querySelectorAll(".variable-form").length === 1 &&
        editor.querySelectorAll('[aria-label="当前信息审阅"]').length === 0 &&
        auth.projectVariables.progress === null;
    });
    return { ...facts, pending };
  };
  const retire = () => {
    if (retired) return;
    retired = true;
    try {
      if (auth.projectVariables.get === wrapped) {
        auth.projectVariables.get = original;
        facts.hooks_retired = auth.projectVariables.get === original;
      } else facts.observer_failed = true;
    } catch {
      facts.observer_failed = true;
    }
    result = null;
  };
  try {
    auth.projectVariables.get = wrapped;
    if (auth.projectVariables.get !== wrapped) return false;
  } catch {
    return false;
  }
  const finish = (input: {
    id: string | null;
    value: ProjectVariable | null;
  }) => {
    if (!retired) snapshot(input);
    retire();
    return { ...facts, pending };
  };
  host.__variableDetail = { finish };
  return true;
}

// Deliberately scoped by the caller to the one predeclared authority GET.
// Other methods, duplicate events and every missing proof retain ordinary gates.
export function variableDetailComplete(
  consumer: ReturnType<typeof variableDetailProjection>,
  native: any,
) {
  if (
    !consumer ||
    !native ||
    native.binding !== "bound" ||
    !native.sample_joined ||
    !Number.isSafeInteger(native.sample_count) ||
    native.sample_count < 1 ||
    native.sample_count > 4096 ||
    native.sample_count !== native.sample_settled ||
    native.sample_failed !== 0 ||
    !native.hooks_retired ||
    !native.eof_before_interruption ||
    !native.length_comparable ||
    !native.length_matches
  )
    return false;
  if (
    ![
      "entry_idle",
      "entry_authenticated",
      "entry_empty",
      "identity_current",
      "authenticated",
      "owner_idle",
      "document_matches",
      "target_route",
      "native_request_matches",
      "body_matches",
      "fresh_editor",
      "published",
      "hooks_retired",
    ].every((k) => consumer[k] === true) ||
    consumer.observer_failed !== false ||
    consumer.calls !== 1 ||
    consumer.target_calls !== 1 ||
    consumer.fulfilled !== 1 ||
    consumer.rejected !== 0 ||
    consumer.synchronous_throws !== 0 ||
    consumer.pending !== 0 ||
    consumer.native_before !== 0 ||
    consumer.native_after !== 1
  )
    return false;
  const f = native.facts;
  return (
    !!f &&
    f.failure === "none" &&
    f.readers === 1 &&
    f.read_calls >= 1 &&
    f.read_calls === f.read_settled &&
    f.read_rejected === 0 &&
    f.reader_cancel_calls === 1 &&
    f.reader_cancel_settled === 1 &&
    f.reader_cancel_rejected === 0 &&
    f.stream_cancel_calls === 1 &&
    f.stream_cancel_settled === 1 &&
    f.stream_cancel_rejected === 0 &&
    f.release_calls === 1 &&
    f.release_successes === 1 &&
    f.abort_events === 0
  );
}
