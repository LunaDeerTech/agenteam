// Diagnostic only: never consumes a body, changes a gate, or starts a request.
export type ResolvePublicationBinding = { entry: string; asset: string; export_name: string; failure_export_name: string };
export async function installResolvePublicationObservation({ binding, target, slot, expiresAt }: {
  binding: ResolvePublicationBinding;
  target: { username: string; project_name: string };
  slot: string;
  expiresAt: number;
}) {
  const host = window as any;
  const loaded = (path: string) => window.performance.getEntriesByName(new URL(path, location.origin).href).length > 0;
  if (Date.now() >= expiresAt) return 'expired';
  if (!loaded(binding.entry) || !loaded(binding.asset)) return 'assets-unobserved';
  if (host.__authorityResolvePublication) return 'observer-present';
  let module;
  try { module = await (new Function('path', 'return import(path)'))(binding.asset); }
  catch { return 'module-unavailable'; }
  if (Date.now() >= expiresAt) return 'expired';
  if (typeof module[binding.export_name] !== 'function') return 'singleton-unavailable';
  const auth = module[binding.export_name](), Failure = module[binding.failure_export_name];
  if (auth !== module[binding.export_name]() || typeof auth?.projects?.resolve !== 'function') return 'singleton-unavailable';
  if (typeof Failure !== 'function' || !(Failure.prototype instanceof Error)) return 'failure-type-unavailable';
  if (auth.state?.phase !== 'authenticated' || auth.state.busy !== false || !auth.personalContext?.identity) return 'owner-unready';
  const native = host.__projectModelsProbe;
  if (native?.resolveBegin(slot, expiresAt, target) !== true) return 'native-unavailable';
  const original = auth.projects.resolve, initialIdentity = { ...auth.personalContext.identity };
  const targetPath = '/' + target.username + '/' + target.project_name + '/settings/model-providers';
  const base = window.performance.now();
  let disposed = false, pending = 0, requestID: string | null = null, timer: ReturnType<typeof setTimeout> | undefined;
  const facts = {
    resolve_calls: 0, target_calls: 0, fulfilled: 0, rejected: 0, synchronous_throws: 0,
    typed_problem: false, problem_status: 0, problem_instance_matches: false,
    entry_authenticated: false, entry_not_busy: false, entry_identity_matches: false,
    target_url_at_call: false, target_url_current: false, left_target: false,
    identity_current: false, authenticated: false, not_busy: false,
    old_error_heading_present: false, old_loading_dom_present: false, loading_dom_observed: false, error_dom_after_rejection: false,
    error_heading_present: false, error_heading_visible: false, zero_provider_lists: false, zero_dialogs: false,
    observer_failed: false, hooks_retired: false, lifetime_expired: false,
  };
  const timing: Record<string, number | null> = { call: null, loading_dom: null, rejected: null, error_dom: null, sample: null };
  const at = (key: string) => { timing[key] ??= window.performance.now() - base; };
  const sameIdentity = () => {
    const current = auth.personalContext.identity;
    return !!current && current.userID === initialIdentity.userID && current.sessionID === initialIdentity.sessionID && current.epoch === initialIdentity.epoch;
  };
  const safe = (work: () => void) => { try { work(); } catch { facts.observer_failed = true; } };
  const headings = (selector: string, text: string) => [...document.querySelectorAll(selector)].filter(node => node.textContent?.replace(/^!/, '').trim() === text);
  const errorNodes = () => [...headings('[role="alert"] h3', '项目不可用'), ...headings('[role="alert"] h3', '项目信息读取失败')];
  facts.old_error_heading_present = errorNodes().length > 0;
  let previousReading = headings('[role="status"][aria-busy="true"] h3', '正在读取项目').length > 0;
  facts.old_loading_dom_present = previousReading;
  const sampleDOM = () => safe(() => {
    if (disposed) return;
    const onTarget = location.pathname === targetPath;
    facts.target_url_current = onTarget;
    if (facts.resolve_calls > 0 && !onTarget) facts.left_target = true;
    facts.identity_current = sameIdentity();
    facts.authenticated = auth.state.phase === 'authenticated'; facts.not_busy = auth.state.busy === false;
    facts.zero_provider_lists = document.querySelectorAll('[role="list"][aria-label="Providers 列表"],ul[aria-label="Providers 列表"]').length === 0;
    facts.zero_dialogs = document.querySelectorAll('[role="dialog"],dialog[open]').length === 0;
    const reading = headings('[role="status"][aria-busy="true"] h3', '正在读取项目').length > 0;
    const enteredLoading = reading && !previousReading;
    previousReading = reading;
    const errors = errorNodes();
    facts.error_heading_present = onTarget && errors.length === 1;
    facts.error_heading_visible = facts.error_heading_present && errors[0]!.getClientRects().length > 0 && !errors[0]!.closest('[hidden],[aria-hidden="true"]') && window.getComputedStyle(errors[0]!).visibility !== 'hidden';
    if (onTarget && facts.resolve_calls === 1 && facts.target_calls === 1 && enteredLoading && facts.rejected === 0 && facts.fulfilled === 0) {
      facts.loading_dom_observed = true; at('loading_dom');
    }
    const expectedHeading = facts.problem_status === 409 ? '项目信息读取失败' : [403, 404].includes(facts.problem_status) ? '项目不可用' : '';
    if (onTarget && !reading && facts.loading_dom_observed && facts.rejected === 1 && expectedHeading && headings('[role="alert"] h3', expectedHeading).length === 1) {
      facts.error_dom_after_rejection = true; at('error_dom');
    }
    timing.sample = window.performance.now() - base;
  });
  const observer = new MutationObserver(sampleDOM);
  const retire = () => {
    if (facts.hooks_retired) return;
    observer.disconnect();
    if (timer !== undefined) { clearTimeout(timer); timer = undefined; }
    if (auth.projects.resolve === wrapped) auth.projects.resolve = original;
    else facts.observer_failed = true;
    facts.hooks_retired = true;
  };
  const wrapped = function(this: unknown, ...args: unknown[]) {
    safe(() => {
      facts.resolve_calls++;
      const address = args[0] as Record<string, unknown> | undefined;
      const exact = args.length === 1 && !!address && Object.keys(address).length === 2 && address.username === target.username && address.project_name === target.project_name;
      if (exact) facts.target_calls++;
      if (facts.resolve_calls !== 1) return;
      facts.entry_authenticated = auth.state.phase === 'authenticated'; facts.entry_not_busy = auth.state.busy === false;
      facts.entry_identity_matches = sameIdentity(); facts.target_url_at_call = location.pathname === targetPath; at('call');
    });
    let originalPromise: Promise<unknown>;
    try { originalPromise = Reflect.apply(original, this, args); }
    catch (error) { safe(() => { facts.synchronous_throws++; }); throw error; }
    pending++;
    void originalPromise.then(() => { if (!disposed) safe(() => { facts.fulfilled++; }); }, error => {
      if (disposed) return;
      safe(() => {
        facts.rejected++; at('rejected');
        const problem = error instanceof Failure && error.kind === 'problem' ? error.problem : null;
        if (facts.resolve_calls === 1 && facts.target_calls === 1 && problem && typeof problem === 'object' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(problem.request_id) && Number.isInteger(problem.status)) {
          facts.typed_problem = true; requestID = problem.request_id;
          // Account.HTTPBoundary projects a fixed safe instance, not the
          // request endpoint. Request identity is bound separately above.
          facts.problem_status = problem.status; facts.problem_instance_matches = problem.instance === '/api/v1';
        }
      });
    }).catch(() => { if (!disposed) facts.observer_failed = true; }).then(() => { pending--; });
    return originalPromise;
  };
  const snapshot = (expectedID: string | null) => {
    sampleDOM();
    return { ...facts, pending_observations: pending, problem_request_id_matches: !!requestID && requestID === expectedID,
      clock: 'browser-monotonic-observed-relative-to-install', timing: { ...timing } };
  };
  observer.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true });
  auth.projects.resolve = wrapped;
  host.__authorityResolvePublication = {
    snapshot(expectedID: string | null) { return disposed ? null : snapshot(expectedID); },
    finish(expectedID: string | null) {
      if (disposed) return null;
      sampleDOM(); retire(); const result = snapshot(expectedID);
      disposed = true; requestID = null; delete host.__authorityResolvePublication;
      return result;
    },
  };
  // Match the existing native diagnostic slot's lifetime; never abort a request.
  timer = setTimeout(() => { facts.lifetime_expired = true; retire(); disposed = true; requestID = null; delete host.__authorityResolvePublication; }, 45_000);
  return true;
}

export function resolvePublicationProjection(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const row = value as Record<string, unknown>;
  const counts = ['resolve_calls', 'target_calls', 'fulfilled', 'rejected', 'synchronous_throws', 'problem_status', 'pending_observations'];
  const flags = ['typed_problem', 'problem_instance_matches', 'entry_authenticated', 'entry_not_busy', 'entry_identity_matches', 'target_url_at_call', 'target_url_current', 'left_target', 'identity_current', 'authenticated', 'not_busy', 'old_error_heading_present', 'old_loading_dom_present', 'loading_dom_observed', 'error_dom_after_rejection', 'error_heading_present', 'error_heading_visible', 'zero_provider_lists', 'zero_dialogs', 'observer_failed', 'hooks_retired', 'lifetime_expired', 'problem_request_id_matches'];
  const stages = ['call', 'loading_dom', 'rejected', 'error_dom', 'sample'];
  const times = row.timing as Record<string, unknown> | undefined;
  if (Object.keys(row).length !== counts.length + flags.length + 2 || !counts.every(key => Number.isSafeInteger(row[key]) && Number(row[key]) >= 0) || Number(row.problem_status) > 599 || !flags.every(key => typeof row[key] === 'boolean') || row.clock !== 'browser-monotonic-observed-relative-to-install' || !times || Object.keys(times).length !== stages.length || !stages.every(key => times[key] === null || typeof times[key] === 'number' && Number.isFinite(times[key]) && Number(times[key]) >= 0)) return null;
  return { ...Object.fromEntries([...counts, ...flags].map(key => [key, row[key]])), clock: row.clock, timing: Object.fromEntries(stages.map(key => [key, times[key]])) };
}
