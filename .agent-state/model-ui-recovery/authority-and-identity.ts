import { expect, test, type Locator, type Page, type Request, type Response, type TestInfo } from '../../tests/account-captcha-web/node_modules/@playwright/test/index.js';
import { createHash, randomUUID } from 'node:crypto';
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { performance } from 'node:perf_hooks';
import type { ConfigCredentialSnapshot as Snapshot } from './configuration-and-credential';

type ProjectKey = 'main' | 'second' | 'other' | 'admin_owned' | 'archiving' | 'archived' | 'deleting' | 'pending' | 'config_recovery' | 'credential_recovery' | 'referenced';
type Project = Readonly<{ id: string; username: string; normalized_name: string; owner_user_id: string; initialized: boolean; lifecycle: string }>;
type Actor = Readonly<{ email: string; password: string; user_id: string }>;
type NativeFact = Readonly<{ token: string | null; method: string; path: string; query: string; status: number; eof: boolean; ended: boolean; released: boolean; bytes: number }>;
type IPC =
  | { action: 'arm'; args: { operation: string; project: ProjectKey | null; target_id: string | null; query: string | null; effect: string } }
  | { action: 'release'; args: { arm_id: string; request_token: string } }
  | { action: 'logout'; args: { session_id: string } }
  | { action: 'rename-reuse'; args: { project: 'main' } }
  | { action: 'archive-recovery-project'; args: { project: 'config_recovery' | 'credential_recovery'; expected_version: string } }
  | { action: 'reference-fact'; args: { project: 'referenced'; state: 'present' | 'absent' } };
export type AuthorityHarness = Readonly<{
  material: unknown;
  loginOwner(page: Page): Promise<void>;
  navigate(page: Page, target: string): Promise<void>;
  fillCredential(scope: Locator): Promise<void>;
  ipc(request: IPC): Promise<Record<string, unknown>>;
  snapshot(project: ProjectKey): Promise<Snapshot>;
  strictReceipt(scope: Page | Locator): Promise<Record<string, unknown>>;
  durableDelta(before: Snapshot, after: Snapshot, configuration: number, credential: number): void;
  originalReplay(value: Snapshot, token: string): void;
  arm(operation: string, project: ProjectKey, target: string | null, effect: string, query?: string | null): Promise<string>;
  control(id: string, ready: (value: Record<string, unknown>) => boolean): Promise<Record<string, unknown>>;
  actualLoss(page: Page, control: Record<string, unknown>, maximumObservedBytes: 0 | 1): Promise<void>;
  counts(): Promise<Record<string, unknown>>;
  nativeFacts(page: Page): Promise<readonly NativeFact[]>;
  finish(page: Page, checks: Record<string, boolean>): Promise<void>;
  step(name: string): void;
}>;
const button = (scope: Page | Locator, name: string) => scope.getByRole('button', { name, exact: true });
function need(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function object(value: unknown): Record<string, unknown> {
  need(value !== null && typeof value === 'object' && !Array.isArray(value), 'PROJECT_MODELS_AUTHORITY_OBJECT_INVALID');
  return value as Record<string, unknown>;
}
function uuid(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value); }
function locator(value: unknown): Project {
  const row = object(value);
  need(uuid(row.id) && uuid(row.owner_user_id) && typeof row.username === 'string' && typeof row.normalized_name === 'string' && typeof row.initialized === 'boolean' && typeof row.lifecycle === 'string', 'PROJECT_MODELS_AUTHORITY_PROJECT_INVALID');
  return row as unknown as Project;
}
const route = (project: Project) => '/' + project.username + '/' + project.normalized_name + '/settings/model-providers';
function operationCount(value: Record<string, unknown>, operation: string) {
  need(Array.isArray(value.operations), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  const rows = value.operations.map(object).filter((row) => row.operation === operation);
  need(rows.length === 1 && Number.isSafeInteger(rows[0]!.browser), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  return Number(rows[0]!.browser);
}
function sameOperations(before: Record<string, unknown>, after: Record<string, unknown>) {
  need(Array.isArray(before.operations), 'PROJECT_MODELS_AUTHORITY_COUNTS_INVALID');
  for (const row of before.operations.map(object)) need(operationCount(before, String(row.operation)) === operationCount(after, String(row.operation)), 'PROJECT_MODELS_AUTHORITY_UNEXPECTED_OPERATION');
}
function originalBody(fact: NativeFact, operation: string, projectID: string) {
  const directory = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!;
  const rows = readdirSync(directory).filter((name) => /^response-\d+\.json$/.test(name)).map((name) => object(JSON.parse(readFileSync(join(directory, name), 'utf8')))).filter((row) => row.source === 'browser' && row.request_token === fact.token);
  need(rows.length === 1, 'PROJECT_MODELS_AUTHORITY_BODY_MISSING');
  const row = rows[0]!;
  need(row.protocol === 'project-owner-models.v1' && row.input_hash === process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH && row.operation === operation && row.project_id === projectID && row.method === fact.method && row.endpoint === fact.path && row.query === fact.query && row.status === fact.status && row.body_stage === 'complete_formal_upstream' && /^[0-9a-f]{64}$/.test(String(row.body_sha256)) && row.body_file === 'body-' + row.body_sha256 + '.json', 'PROJECT_MODELS_AUTHORITY_BODY_BINDING_INVALID');
  const bytes = readFileSync(join(directory, String(row.body_file)));
  need(bytes.length === row.body_bytes && bytes.length === fact.bytes && bytes.length <= 8388608 && createHash('sha256').update(bytes).digest('hex') === row.body_sha256, 'PROJECT_MODELS_AUTHORITY_BODY_BYTES_INVALID');
  return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)));
}
type AuthorityAwait = <T>(label: string, start: () => Promise<T>) => Promise<T>;
function authorityAwait(step: (name: string) => void): AuthorityAwait {
  let sequence = 0;
  let firstRejected: string | undefined;
  const pending = new Map<number, string>();
  const publish = (label: string, state: 'started' | 'completed' | 'rejected') => {
    // Labels are source literals (or the fixed lifecycle key union), never
    // Project data. A late completion must retain the newest pending wait.
    const labels = [...pending.values()], current = labels.at(-1);
    const active = current && labels.length > 1 ? labels[0] + '-in-' + current : current;
    const progress = active && state !== 'started' ? active + '-pending-after-' + label + '-' + state : (active ?? label) + '-' + state;
    try { step(progress + (firstRejected ? '-first-rejected-' + firstRejected : '')); }
    catch { /* Diagnostic I/O never changes the original action or error. */ }
  };
  return <T>(label: string, start: () => Promise<T>) => {
    const sequenceID = ++sequence;
    pending.set(sequenceID, label); publish(label, 'started');
    const settled = (state: 'completed' | 'rejected') => { pending.delete(sequenceID); if (state === 'rejected') firstRejected ??= label; publish(label, state); };
    let original: Promise<T>;
    try { original = start(); }
    catch (error) { settled('rejected'); throw error; }
    // Observe a handled side branch and return the exact original Promise.
    void original.then(() => settled('completed'), () => settled('rejected')).catch(() => {});
    return original;
  };
}
function observer(page: Page, harness: AuthorityHarness, wait: AuthorityAwait) {
  const tokens = new Set<string>();
  return async (operation: string, projectID: string, method: string, leaf: string, status: number, action: () => Promise<void>, query = '') => {
    const before = await wait('authority-observer-native-facts-001', () => harness.nativeFacts(page)), path = '/api/v1/projects/' + projectID + '/' + leaf;
    await wait('authority-observer-action-002', () => action());
    let matches: readonly NativeFact[] = [];
    await wait('authority-observer-to-be-003', () => expect.poll(async () => {
      const facts = await wait('authority-observer-native-facts-004', () => harness.nativeFacts(page));
      need(facts.length >= before.length, 'PROJECT_MODELS_AUTHORITY_OBSERVATIONS_RESET');
      matches = facts.slice(before.length).filter((fact) => fact.method === method && fact.path === path);
      return matches.length === 1 && matches[0]!.status === status && matches[0]!.eof && matches[0]!.ended && matches[0]!.released;
    }).toBe(true));
    const fact = matches[0]!;
    need(fact.token !== null && /^r[0-9]{6}$/.test(fact.token) && !tokens.has(fact.token) && fact.query === query, 'PROJECT_MODELS_AUTHORITY_NATIVE_BINDING_INVALID');
    tokens.add(fact.token);
    return originalBody(fact, operation, projectID);
  };
}
// Diagnostic failures must never replace the existing Session result. No
// response body, request headers, IDs or raw error strings enter this artifact.
export async function beginSessionDiagnostic(page: Page, mode: 'authority' | 'navigation') {
  const slot = randomUUID(), expiresAt = Date.now() + 250;
  let selected: Request | undefined, requestID: string | null = null;
  const requests: Request[] = [];
  const bounded = async <T>(work: () => Promise<T>): Promise<T | null> => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try { return await Promise.race([work(), new Promise<null>((resolve) => { timer = setTimeout(() => resolve(null), 250); })]); }
    catch { return null; }
    finally { if (timer !== undefined) clearTimeout(timer); }
  };
  const requested = (request: Request) => {
    try {
      const url = new URL(request.url());
      if (url.origin === new URL(page.url()).origin && url.pathname === '/api/v1/session' && !url.search && request.method() === 'GET') requests.push(request);
    } catch { /* Diagnostic metadata cannot interrupt a real request. */ }
  };
  page.on('request', requested);
  await bounded(() => page.evaluate(({ slot, expiresAt }) => (window as any).__projectModelsProbe.sessionBegin(slot, expiresAt) as boolean, { slot, expiresAt }));
  return {
    select(response: Response) {
      try {
        selected = response.request();
        void response.headerValue('x-request-id').then((value) => { requestID = value; }, () => {}).catch(() => {});
      } catch { /* Only the diagnostic binding becomes unavailable. */ }
    },
    async finish(failed: boolean) {
      try {
        const value: unknown = await bounded(() => page.evaluate(({ slot, requestID }) => (window as any).__projectModelsProbe.sessionEnd(slot, requestID), { slot, requestID }));
        if (!failed) return;
        const counts = ['requests', 'readers', 'read_calls', 'read_settled', 'read_rejected', 'bytes', 'reader_cancel_calls', 'reader_cancel_settled', 'reader_cancel_rejected', 'stream_cancel_calls', 'stream_cancel_settled', 'stream_cancel_rejected', 'release_calls', 'release_successes', 'abort_events'];
        const flags = ['headers_seen', 'status_ok', 'read_done', 'cancel_before_eof', 'request_id_match', 'signal_aborted'];
        const codes = ['none', 'fetch-rejected', 'get-reader-threw', 'read-rejected', 'reader-cancel-rejected', 'stream-cancel-rejected', 'release-threw', 'observer-error'];
        let native: Record<string, boolean | number | string> | null = null;
        if (value && typeof value === 'object' && !Array.isArray(value)) {
          const row = value as Record<string, unknown>;
          if (Object.keys(row).length === counts.length + flags.length + 1 && counts.every((key) => Number.isSafeInteger(row[key]) && Number(row[key]) >= 0) && flags.every((key) => typeof row[key] === 'boolean') && typeof row.failure === 'string' && codes.includes(row.failure)) {
            native = Object.fromEntries([...counts, ...flags, 'failure'].map((key) => [key, row[key] as boolean | number | string]));
          }
        }
        const error = selected?.failure()?.errorText;
        const pwFailure = error === undefined ? 'none' : error === 'net::ERR_ABORTED' ? 'aborted' : error === 'net::ERR_CONNECTION_RESET' ? 'reset' : error === 'net::ERR_CONNECTION_CLOSED' ? 'closed' : 'other';
        writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, `${mode}-session-native-diagnostic.json`), JSON.stringify({
          protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH,
          diagnostic: native === null ? 'unavailable' : 'captured', pw_requests: requests.length,
          pw_request_match: requests.length === 1 && requests[0] === selected, pw_request_id_seen: !!requestID,
          pw_failure: pwFailure, native,
        }), { mode: 0o600 });
      } catch { /* A missing diagnostic cannot alter the original gate. */ }
      finally { page.off('request', requested); }
    },
  };
}

const resolveEvaluations = new WeakMap<Page, Promise<unknown>>();
function resolveEvaluate(page: Page, work: () => Promise<unknown>): Promise<unknown> {
  const prior = resolveEvaluations.get(page);
  // A timed-out begin/end still owns its real evaluate. Join that original
  // work without treating its result as the newly requested observation.
  if (prior) return prior.then(() => null, () => null);
  const original = work();
  resolveEvaluations.set(page, original);
  const clear = () => { if (resolveEvaluations.get(page) === original) resolveEvaluations.delete(page); };
  void original.then(clear, clear);
  return original;
}
function resolveSampler(page: Page, slot: string, requestID: () => string | null, publish: (value: unknown, expectedID: string | null) => void, initial?: Promise<unknown>) {
  let stopped = false, started = false, timer: ReturnType<typeof setTimeout> | undefined;
  let pending: Promise<void> | undefined, samples = 0, settled = 0, failed = 0;
  const schedule = () => { if (!stopped) timer = setTimeout(sample, 250); };
  function sample() {
    timer = undefined;
    if (stopped || pending) return;
    const expectedID = requestID();
    samples++;
    let work: Promise<unknown>;
    try { work = resolveEvaluate(page, () => page.evaluate(({ slot, expectedID }) => (window as any).__projectModelsProbe.resolveSnapshot(slot, expectedID), { slot, expectedID })); }
    catch { settled++; failed++; schedule(); return; }
    const observed = work.then((value) => { settled++; if (!stopped) publish(value, expectedID); }, () => { settled++; failed++; }).catch(() => { failed++; });
    pending = observed;
    void observed.then(() => { if (pending === observed) pending = undefined; schedule(); }).catch(() => {});
  }
  if (initial) {
    const joined = initial.then(() => {}, () => {});
    pending = joined;
    void joined.then(() => { if (pending === joined) pending = undefined; if (started && !stopped) sample(); }).catch(() => {});
  }
  return {
    start() { if (!started && !stopped) { started = true; sample(); } },
    async stop() {
      stopped = true;
      if (timer !== undefined) { clearTimeout(timer); timer = undefined; }
      const tail = pending;
      let joined = !tail, joinTimer: ReturnType<typeof setTimeout> | undefined;
      try { if (tail) joined = await Promise.race([tail.then(() => true), new Promise<false>((resolve) => { joinTimer = setTimeout(() => resolve(false), 250); })]); }
      finally { if (joinTimer !== undefined) clearTimeout(joinTimer); }
      return { samples, sample_settled: settled, sample_failed: failed, sample_joined: joined, sample_join_unavailable: !joined };
    },
  };
}

function resolveSnapshot(value: unknown): Record<string, boolean | number | string> | null {
  const counts = ['requests', 'readers', 'read_calls', 'read_settled', 'read_rejected', 'bytes', 'reader_cancel_calls', 'reader_cancel_settled', 'reader_cancel_rejected', 'stream_cancel_calls', 'stream_cancel_settled', 'stream_cancel_rejected', 'release_calls', 'release_successes', 'abort_events', 'status', 'headers_order', 'read_done_order', 'read_rejected_order', 'abort_order', 'reader_cancel_order', 'stream_cancel_order', 'release_order'];
  const flags = ['headers_seen', 'status_ok', 'read_done', 'cancel_before_eof', 'request_id_match', 'signal_aborted', 'signal_aborted_at_start'];
  const codes = ['none', 'fetch-rejected', 'get-reader-threw', 'read-rejected', 'reader-cancel-rejected', 'stream-cancel-rejected', 'release-threw', 'observer-error'];
  let native: Record<string, boolean | number | string> | null = null;
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const row = value as Record<string, unknown>;
    if (Object.keys(row).length === counts.length + flags.length + 1 && counts.every((key) => Number.isSafeInteger(row[key]) && Number(row[key]) >= 0) && Number(row.status) <= 599 && flags.every((key) => typeof row[key] === 'boolean') && typeof row.failure === 'string' && codes.includes(row.failure))
      native = Object.fromEntries([...counts, ...flags, 'failure'].map((key) => [key, row[key] as boolean | number | string]));
  }
  return native;
}

// Resolve remains outside the Model operation whitelist. This observes the
// original selected response without selecting a replacement or changing a gate.
async function beginResolveDiagnostic(page: Page, project: Project) {
  // All times below are Node observations relative to this invocation, not
  // browser EOF times or the beginning of Playwright's overall test budget.
  const timeOrigin = performance.now();
  let timeSequence = 0, info: TestInfo | undefined;
  try { info = test.info(); } catch { /* Public test status can be unavailable. */ }
  type Mark = Readonly<{ order: number; elapsed_ms: number }>;
  const mark = (): Mark => ({ order: ++timeSequence, elapsed_ms: performance.now() - timeOrigin });
  const testStatus = () => {
    try { const status = info?.status; if (status && ['passed', 'failed', 'timedOut', 'skipped', 'interrupted'].includes(status)) return status; } catch {}
    return 'unknown';
  };
  let actionStarted: Mark | null = null, firstEOFSample: Mark | null = null, waitRejected: Mark | null = null;
  let pageClose: Mark | null = null, contextClose: Mark | null = null;
  const finishedTimes = new Map<Request, Mark>(), failedTimes = new Map<Request, { at: Mark; test_status: string }>();
  const pageClosed = () => { try { pageClose ??= mark(); } catch {} };
  const contextClosed = () => { try { contextClose ??= mark(); } catch {} };
  const context = page.context();
  const slot = randomUUID(), expiresAt = Date.now() + 250;
  const target = { username: project.username, project_name: project.normalized_name };
  let active = false, selected: Response | undefined, requestID: string | null = null;
  let beforeAction = 0, afterAction = 0;
  let latest: Record<string, boolean | number | string> | null = null, latestID: string | null = null;
  let snapshotSource: 'unavailable' | 'sample' | 'end' = 'unavailable';
  const targets: Request[] = [], finished = new Set<Request>(), failed = new Set<Request>();
  const bounded = async <T>(work: () => Promise<T>): Promise<T | null> => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try { return await Promise.race([work(), new Promise<null>((resolve) => { timer = setTimeout(() => resolve(null), 250); })]); }
    catch { return null; }
    finally { if (timer !== undefined) clearTimeout(timer); }
  };
  const candidate = (request: Request) => {
    const url = new URL(request.url());
    return url.origin === new URL(page.url()).origin && url.pathname === '/api/v1/projects/resolve';
  };
  const targetMatch = (request: Request) => {
    const query = new URL(request.url()).searchParams;
    return candidate(request) && request.method() === 'GET' && query.size === 2 &&
      query.getAll('username').length === 1 && query.getAll('project_name').length === 1 &&
      query.get('username') === target.username && query.get('project_name') === target.project_name;
  };
  const requested = (request: Request) => { try {
    if (!candidate(request)) return;
    if (active) { afterAction++; if (targetMatch(request)) targets.push(request); }
    else beforeAction++;
  } catch { /* Observation cannot interrupt the original request. */ } };
  const completed = (request: Request) => { try { if (candidate(request)) { finished.add(request); if (!finishedTimes.has(request)) finishedTimes.set(request, mark()); } } catch {} };
  const rejected = (request: Request) => { try { if (candidate(request)) { failed.add(request); if (!failedTimes.has(request)) failedTimes.set(request, { at: mark(), test_status: testStatus() }); } } catch {} };
  page.on('request', requested); page.on('requestfinished', completed); page.on('requestfailed', rejected);
  page.on('close', pageClosed); context.on('close', contextClosed);
  let beginning: Promise<unknown>;
  try { beginning = resolveEvaluate(page, () => page.evaluate(({ slot, expiresAt, target }) => (window as any).__projectModelsProbe.resolveBegin(slot, expiresAt, target), { slot, expiresAt, target })); }
  catch { beginning = Promise.resolve(null); }
  const sampler = resolveSampler(page, slot, () => requestID, (value, expectedID) => {
    const safe = resolveSnapshot(value);
    if (safe !== null) {
      latest = safe; latestID = expectedID; snapshotSource = 'sample';
      if (safe.read_done === true && safe.request_id_match === true && expectedID && expectedID === requestID && targets.length === 1 && targets[0] === selected?.request() && targetMatch(targets[0]!)) firstEOFSample ??= mark();
    }
  }, beginning);
  await bounded(() => beginning);
  return {
    start() { active = true; try { actionStarted ??= mark(); } catch {} },
    select(response: Response) {
      selected = response;
      sampler.start();
      try { void response.headerValue('x-request-id').then((value) => { requestID = value; }, () => {}).catch(() => {}); } catch {}
    },
    finishedWait<T>(work: () => Promise<T>): Promise<T> {
      const rejected = () => { try { waitRejected ??= mark(); } catch {} };
      let original: Promise<T>;
      try { original = work(); } catch (error) { rejected(); throw error; }
      void original.then(() => {}, rejected).catch(() => {});
      return original;
    },
    async finish(failure: boolean) {
      try {
        const sampling = await sampler.stop();
        // A bounded join timeout does not retire the underlying evaluate.
        // Never overlap it with a second evaluate, including diagnostic end.
        const value: unknown = sampling.sample_joined ? await bounded(() => resolveEvaluate(page, () => page.evaluate(({ slot, requestID }) => (window as any).__projectModelsProbe.resolveEnd(slot, requestID), { slot, requestID }))) : null;
        const final = resolveSnapshot(value);
        if (final !== null) { latest = final; latestID = requestID; snapshotSource = 'end'; }
        if (!failure) return;
        const native = latest;
        const request = selected?.request(), error = request?.failure()?.errorText;
        const pwFailure = error === undefined ? 'none' : error === 'net::ERR_ABORTED' ? 'aborted' : error === 'net::ERR_CONNECTION_RESET' ? 'reset' : error === 'net::ERR_CONNECTION_CLOSED' ? 'closed' : 'other';
        writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'authority-resolve-diagnostic.json'), JSON.stringify({
          protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH,
          diagnostic: native === null ? 'unavailable' : 'captured', pw_candidates_before_action: beforeAction,
          pw_candidates_after_action: afterAction, pw_target_requests: targets.length,
          pw_selected_target_match: !!request && targetMatch(request), pw_request_match: targets.length === 1 && targets[0] === request,
          pw_request_id_seen: !!requestID, pw_status: selected?.status() ?? null,
          snapshot_source: snapshotSource, snapshot_selected_bound: !!request && targets.length === 1 && targets[0] === request && !!latestID && latestID === requestID && native?.request_id_match === true,
          snapshot_native_terminal: native !== null && (native.read_done === true || Number(native.read_rejected) > 0 || native.failure === 'fetch-rejected'),
          slot_end_observed: final !== null, ...sampling,
          pw_finished_event: !!request && finished.has(request), pw_failed_event: !!request && failed.has(request), pw_failure: pwFailure, native,
          // Null means not observed before this projection. A close notification
          // is not evidence of when a close operation was initiated.
          timing: { clock: 'node-performance', origin: 'resolve-diagnostic-begin', action_started: actionStarted,
            first_bound_eof_sample: firstEOFSample, pw_finished: request ? finishedTimes.get(request) ?? null : null,
            pw_failed: request ? failedTimes.get(request)?.at ?? null : null,
            pw_failed_test_status: request ? failedTimes.get(request)?.test_status ?? 'unknown' : 'unknown',
            original_finished_wait_rejected: waitRejected, page_close_notification: pageClose,
            context_close_notification: contextClose, projection_recorded: mark() },
        }), { mode: 0o600 });
      } catch { /* Missing diagnostic evidence must preserve the original error. */ }
      finally { page.off('request', requested); page.off('requestfinished', completed); page.off('requestfailed', rejected); page.off('close', pageClosed); context.off('close', contextClosed); }
    },
  };
}

// Session bodies remain private in this call. Only the formal safe identity is
// returned, never CSRF, cookies, login inputs, headers or their digests.
type SessionStageCode = `PROJECT_MODELS_AUTHORITY_${'SESSION_ACTION' | 'SESSION_HEADERS' | 'SESSION_FINISH' | 'SESSION_JSON' | 'CHECKING_COUNTS' | 'CHECKING_FACTS' | 'CHECKING_ARM' | 'CHECKING_HOLD' | 'CHECKING_RELEASE' | 'CHECKING_JOIN'}_TIMEOUT`;
async function sessionStage<T>(work: Promise<T>, code: SessionStageCode) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([work, new Promise<never>((_resolve, reject) => {
      timer = setTimeout(() => reject(new Error(code)), 5_000);
    })]);
  } finally { if (timer !== undefined) clearTimeout(timer); }
}
async function sessionIdentity(page: Page, action: () => Promise<void>, step: (name: string) => void, wait: AuthorityAwait) {
  const diagnostic = await wait('authority-session-identity-begin-session-diagnostic-005', () => beginSessionDiagnostic(page, 'authority'));
  let diagnosticFailed = false;
  let selected: Request | undefined;
  const observation = { headers_seen: false, finished_event: false, failed_event: false };
  const publish = () => writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'authority-session-events.json'), JSON.stringify({ protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH, observation }), { mode: 0o600 });
  const requested = (request: Request) => { if (new URL(request.url()).pathname === '/api/v1/session' && request.method() === 'GET') step('authority-session-request-observed'); };
  const finished = (request: Request) => { if (request === selected) { observation.finished_event = true; publish(); } };
  const failed = (request: Request) => { if (request === selected) { observation.failed_event = true; publish(); } };
  page.on('request', requested); page.on('requestfinished', finished); page.on('requestfailed', failed);
  try {
    publish(); step('authority-session-action-started');
    const waiting = page.waitForResponse((response) => {
      if (new URL(response.url()).pathname !== '/api/v1/session' || response.request().method() !== 'GET' || response.status() !== 200) return false;
      selected = response.request(); diagnostic.select(response); observation.headers_seen = true; publish(); return true;
    }, { timeout: 5_000 });
    const [response] = await wait('authority-session-identity-all-006', () => Promise.all([
      sessionStage(waiting, 'PROJECT_MODELS_AUTHORITY_SESSION_HEADERS_TIMEOUT'),
      (async () => { await wait('authority-session-identity-session-stage-007', () => sessionStage(action(), 'PROJECT_MODELS_AUTHORITY_SESSION_ACTION_TIMEOUT')); step('authority-session-action-returned'); })(),
    ]));
    step('authority-session-headers-observed');
    try { need(await wait('authority-session-identity-session-stage-008', () => sessionStage(response.finished(), 'PROJECT_MODELS_AUTHORITY_SESSION_FINISH_TIMEOUT')) === null, 'PROJECT_MODELS_AUTHORITY_SESSION_INCOMPLETE'); }
    catch (error) {
      if (error instanceof Error && error.message === 'PROJECT_MODELS_AUTHORITY_SESSION_FINISH_TIMEOUT') throw error;
      throw new Error('PROJECT_MODELS_AUTHORITY_SESSION_INCOMPLETE');
    }
    step('authority-session-finished');
    try {
      const body = object(await wait('authority-session-identity-session-stage-009', () => sessionStage(response.json(), 'PROJECT_MODELS_AUTHORITY_SESSION_JSON_TIMEOUT'))), user = object(body.user), session = object(body.session);
      need(uuid(user.id) && uuid(session.id) && (user.role === 'user' || user.role === 'admin'), 'PROJECT_MODELS_AUTHORITY_SESSION_INVALID');
      step('authority-session-json-validated');
      return { userID: user.id, sessionID: session.id, role: user.role };
    } catch (error) {
      if (error instanceof Error && error.message === 'PROJECT_MODELS_AUTHORITY_SESSION_JSON_TIMEOUT') throw error;
      throw new Error('PROJECT_MODELS_AUTHORITY_SESSION_INVALID');
    }
  } catch (error) { diagnosticFailed = true; throw error; } finally {
    page.off('request', requested); page.off('requestfinished', finished); page.off('requestfailed', failed); publish();
    await wait('authority-session-identity-finish-010', () => diagnostic.finish(diagnosticFailed));
  }
}
async function pageshow(page: Page, wait: AuthorityAwait) { await wait('authority-pageshow-evaluate-011', () => page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow')))); }
async function privateLogin(page: Page, actor: Actor, wait: AuthorityAwait) {
  await wait('authority-private-login-to-be-visible-012', () => expect(page.locator('#login-email')).toBeVisible());
  try { await wait('authority-private-login-fill-013', () => page.locator('#login-email').fill(actor.email)); await wait('authority-private-login-fill-014', () => page.locator('#login-password').fill(actor.password)); }
  catch { throw new Error('PROJECT_MODELS_PRIVATE_LOGIN_INPUT_FAILED'); }
  await wait('authority-private-login-click-015', () => button(page, '登录').click()); await wait('authority-private-login-to-be-enabled-016', () => expect(button(page, '退出登录')).toBeEnabled());
}
async function newProvider(page: Page, name: string, wait: AuthorityAwait) {
  await wait('authority-new-provider-click-017', () => button(page, '创建 Provider').click());
  const dialog = page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  await wait('authority-new-provider-fill-018', () => dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill(name));
  await wait('authority-new-provider-fill-019', () => dialog.getByRole('textbox', { name: 'Base URL', exact: true }).fill('https://model-ui.invalid/v1'));
  return dialog;
}
async function discardClick(page: Page, target: Locator, stage: 'cancel' | 'confirm', wait: AuthorityAwait) {
  try { await wait('authority-discard-click-click-020', () => target.click({ timeout: 5_000 })); }
  catch {
    // Observe only this real failure. No DOM text, titles, field values or
    // computed-style strings leave the browser, and no hit target is modified.
    let observation: unknown = null;
    try {
      observation = await wait('authority-discard-click-evaluate-021', () => page.evaluate((stage) => {
        const overlays = [...document.querySelectorAll<HTMLElement>('.ui-overlay')].filter((node) => node.getClientRects().length > 0);
        const panels = overlays.map((node) => node.querySelector<HTMLElement>('[role="dialog"]'));
        const index = panels.findIndex((panel) => panel && (stage === 'cancel' ? !!panel.querySelector('#project-provider-form') : panel.querySelector('h2')?.textContent?.trim() === '放弃当前表单修改？'));
        const panel = panels[index];
        const wanted = panel ? [...panel.querySelectorAll<HTMLButtonElement>('button')].find((node) => node.querySelector('.button-label:not([aria-hidden="true"])')?.textContent?.trim() === (stage === 'cancel' ? '取消' : '放弃修改')) : undefined;
        const rect = wanted?.getBoundingClientRect();
        const hit = rect && rect.width > 0 && rect.height > 0 ? document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2) : null;
        return {
          overlay_count: overlays.length,
          overlays: overlays.slice(0, 8).map((node, at) => { const z = Number.parseInt(getComputedStyle(node).zIndex, 10); return { index: at, z_index: Number.isFinite(z) ? z : null, panel_inert: panels[at]?.inert === true, panel_aria_hidden: panels[at]?.getAttribute('aria-hidden') === 'true' }; }),
          target_overlay_index: index, target_found: !!wanted, target_disabled: wanted?.disabled === true,
          target_visible: !!rect && rect.width > 0 && rect.height > 0, target_inert: !!wanted?.closest('[inert]'),
          center_hits_target: !!hit && !!wanted && (hit === wanted || wanted.contains(hit)),
          center_hits_overlay: !!hit?.closest('.ui-overlay'), hit_overlay_index: overlays.findIndex((node) => !!hit && (hit === node || node.contains(hit))),
        };
      }, stage));
    } catch { /* A missing observation remains null, never a synthetic fact. */ }
    writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'authority-discard-hit.json'), JSON.stringify({ protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH, stage, observation }), { mode: 0o600 });
    throw new Error('PROJECT_MODELS_AUTHORITY_DISCARD_CLICK_FAILED');
  }
}
async function discardProvider(page: Page, dialog: Locator, step: (name: string) => void, wait: AuthorityAwait) {
  await wait('authority-discard-provider-discard-click-022', () => discardClick(page, button(dialog, '取消'), 'cancel', wait));
  const confirmation = page.getByRole('dialog', { name: '放弃当前表单修改？', exact: true });
  await wait('authority-discard-provider-to-be-visible-023', () => expect(confirmation).toBeVisible()); step('authority-discard-confirm-visible');
  await wait('authority-discard-provider-discard-click-024', () => discardClick(page, button(confirmation, '放弃修改'), 'confirm', wait)); await wait('authority-discard-provider-to-be-hidden-025', () => expect(dialog).toBeHidden());
}

// A failed navigation can still have the pushState URL while guards or Owner
// publication are pending. Inspect public DOM only; never export its text/URLs.
async function navigationFailure(page: Page, project: Project, projects: readonly Project[]) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const observation = await Promise.race([
      page.evaluate(({ target, settings, knownSettings }) => {
        const visible = (node: Element | null | undefined): boolean => !!node && node.isConnected && node.getClientRects().length > 0 && !['hidden', 'collapse'].includes(getComputedStyle(node).visibility);
        const text = (node: Element) => node.textContent?.trim();
        const buttons = (scope: ParentNode | null, label: string) => [...(scope?.querySelectorAll<HTMLButtonElement>('button') ?? [])].filter((node) => text(node.querySelector('.button-label:not([aria-hidden="true"])') ?? node) === label);
        const buttonState = (nodes: HTMLButtonElement[]) => ({ count: nodes.length, visible: nodes.some(visible), enabled: nodes.some((node) => visible(node) && !node.disabled), inert: nodes.some((node) => !!node.closest('[inert]')), aria_hidden: nodes.some((node) => !!node.closest('[aria-hidden="true"]')) });
        const state = (scope: string, title: string) => [...document.querySelectorAll(scope + ' .ui-state')].some((node) => visible(node) && [...(node.querySelector('h3')?.childNodes ?? [])].filter((child) => child.nodeType === Node.TEXT_NODE).map((child) => child.textContent).join('').trim() === title);
        const dialogs = [...document.querySelectorAll('[role="dialog"]')];
        const confirmation = (title: string) => dialogs.some((node) => visible(node) && node.querySelector('h2')?.textContent?.trim() === title);
        const navs = [...document.querySelectorAll('nav[aria-label="项目导航"]')];
        const links = navs.flatMap((node) => [...node.querySelectorAll('a')].filter((link) => text(link) === '项目设置'));
        const workspace = document.querySelector('.project-workspace');
        const leaf = workspace?.querySelector('section.project-providers');
        return {
          url_is_target: location.pathname === target,
          nav_count: navs.length, settings_count: links.length,
          settings_target: links.some((node) => node.getAttribute('href') === settings),
          settings_other_known: links.some((node) => node.getAttribute('href') !== settings && knownSettings.includes(node.getAttribute('href') ?? '')),
          settings_current: links.some((node) => node.getAttribute('aria-current') === 'page'),
          nav_visible: navs.some(visible), nav_inert: navs.some((node) => !!node.closest('[inert]')),
          model_leave_confirmation: confirmation('离开项目模型设置？'), owner_leave_confirmation: confirmation('放弃项目修改？'),
          session_checking: state('.session-check', '正在确认会话'), session_unconfirmed: state('.session-check', '会话尚未确认'),
          owner_checking: state('.project-workspace', '正在确认项目访问身份'), owner_loading: state('.project-workspace', '正在读取项目'),
          owner_read_error: state('.project-workspace', '项目信息读取失败'), owner_unavailable: state('.project-workspace', '项目不可用'),
          owner_reread: buttonState(buttons(workspace?.querySelector('.workspace-actions') ?? null, '重新读取项目')),
          model_leaf_visible: visible(leaf), model_reread: buttonState(buttons(leaf ?? null, '重新读取项目')),
          logout: buttonState(buttons(document.querySelector('.account-actions'), '退出登录')),
        };
      }, { target: route(project), settings: '/' + project.username + '/' + project.normalized_name + '/settings/general', knownSettings: projects.map((value) => '/' + value.username + '/' + value.normalized_name + '/settings/general') }),
      new Promise<null>((resolve) => { timer = setTimeout(() => resolve(null), 250); }),
    ]);
    writeFileSync(join(process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE!, 'authority-navigation-dom.json'), JSON.stringify({ protocol: 'project-owner-models.v1', input_hash: process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH, observation }), { mode: 0o600 });
  } catch { /* A missing diagnostic never replaces the original failure. */ }
  finally { if (timer !== undefined) clearTimeout(timer); }
}

export async function runAuthorityAndIdentity(page: Page, harness: AuthorityHarness) {
  const material = object(harness.material), rawProjects = object(material.projects), rawActors = object(material.actors);
  const keys: ProjectKey[] = ['main', 'second', 'other', 'admin_owned', 'archiving', 'archived', 'deleting', 'pending', 'config_recovery', 'credential_recovery', 'referenced'];
  need(material.mode === 'authority' && material.system === null && Object.keys(rawProjects).length === keys.length, 'PROJECT_MODELS_AUTHORITY_MATERIAL_INVALID');
  const projects = Object.fromEntries(keys.map((key) => [key, locator(rawProjects[key])])) as Record<ProjectKey, Project>;
  const actors = Object.fromEntries(['owner', 'other_owner', 'other_admin'].map((key) => {
    const row = object(rawActors[key]);
    need(uuid(row.user_id) && typeof row.email === 'string' && typeof row.password === 'string', 'PROJECT_MODELS_AUTHORITY_ACTOR_INVALID');
    return [key, row];
  })) as Record<'owner' | 'other_owner' | 'other_admin', Actor>;
  need(projects.main.owner_user_id === actors.owner.user_id && projects.other.owner_user_id === actors.other_owner.user_id && projects.admin_owned.owner_user_id === actors.other_admin.user_id, 'PROJECT_MODELS_AUTHORITY_OWNERS_INVALID');
  const wait = authorityAwait(harness.step);
  const checks: Record<string, boolean> = {}, observe = observer(page, harness, wait);
  const providerDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-provider-form') });
  const credentialDialog = () => page.getByRole('dialog').filter({ has: page.locator('#project-credential-form') });
  async function leafReady(project: Project, before: readonly NativeFact[]) {
    const leaf = page.locator('section.project-providers');
    await wait('authority-leaf-ready-to-be-visible-026', () => expect(leaf).toBeVisible());
    const reread = button(leaf, '重新读取项目');
    const freshRead = async () => {
      const facts = await wait('authority-leaf-ready-native-facts-027', () => harness.nativeFacts(page));
      need(facts.length >= before.length, 'PROJECT_MODELS_AUTHORITY_OBSERVATIONS_RESET');
      return facts.slice(before.length).some((fact) => fact.method === 'GET' && fact.path === '/api/v1/projects/' + project.id + '/model-providers' && fact.status === 200 && fact.eof && fact.ended && fact.released);
    };
    // Owner revalidation can publish this gate after the leaf first appears.
    // Observe until the fresh read or the real gate exists, then click at most once.
    await wait('authority-leaf-ready-to-be-028', () => expect.poll(async () => await wait('authority-leaf-ready-fresh-read-029', () => freshRead()) || await wait('authority-leaf-ready-is-visible-030', () => reread.isVisible())).toBe(true));
    if (!(await wait('authority-leaf-ready-fresh-read-031', () => freshRead())) && await wait('authority-leaf-ready-is-visible-032', () => reread.isVisible())) { await wait('authority-leaf-ready-to-be-enabled-033', () => expect(reread).toBeEnabled()); await wait('authority-leaf-ready-click-034', () => reread.click()); await wait('authority-leaf-ready-to-be-hidden-035', () => expect(reread).toBeHidden()); }
    await wait('authority-leaf-ready-to-be-enabled-036', () => expect(button(leaf, '刷新 Providers')).toBeEnabled());
  }
  async function open(project: Project, discard = false) {
    const before = await wait('authority-open-native-facts-037', () => harness.nativeFacts(page));
    const diagnostic = await wait('authority-open-begin-session-diagnostic-038', () => beginSessionDiagnostic(page, 'authority'));
    let diagnosticFailed = false;
    const sessionResponse = (response: Response) => {
      try {
        const url = new URL(response.url());
        if (url.origin === new URL(page.url()).origin && url.pathname === '/api/v1/session' && !url.search && response.request().method() === 'GET' && response.status() === 200) diagnostic.select(response);
      } catch { /* Diagnostic metadata cannot alter navigation. */ }
    };
    page.on('response', sessionResponse);
    try { return await wait('authority-open-observe-039', () => observe('listProjectModelProviders', project.id, 'GET', 'model-providers', 200, async () => {
      await wait('authority-open-navigate-040', () => harness.navigate(page, route(project)));
      if (discard) await wait('authority-open-click-041', () => button(page.getByRole('dialog', { name: '离开项目模型设置？', exact: true }), '放弃并离开').click());
      await wait('authority-open-to-be-042', () => expect.poll(() => new URL(page.url()).pathname).toBe(route(project)));
      // pushState changes the URL before the router and Owner read publish the
      // destination. This public link is rendered from that Project's detail.
      const settings = page.getByRole('navigation', { name: '项目导航', exact: true }).getByRole('link', { name: '项目设置', exact: true });
      await wait('authority-open-to-have-attribute-043', () => expect(settings).toHaveAttribute('href', '/' + project.username + '/' + project.normalized_name + '/settings/general'));
      await wait('authority-open-to-have-attribute-044', () => expect(settings).toHaveAttribute('aria-current', 'page'));
      await wait('authority-open-leaf-ready-045', () => leafReady(project, before));
    }, 'limit=25')); }
    catch (error) { diagnosticFailed = true; await wait('authority-open-navigation-failure-046', () => navigationFailure(page, project, Object.values(projects))); throw error; }
    finally { page.off('response', sessionResponse); await wait('authority-open-finish-047', () => diagnostic.finish(diagnosticFailed)); }
  }
  async function reread(project: Project) {
    return observe('listProjectModelProviders', project.id, 'GET', 'model-providers', 200, async () => {
      const leaf = page.locator('section.project-providers');
      await wait('authority-reread-to-be-visible-048', () => expect(button(leaf, '重新读取项目')).toBeVisible());
      await wait('authority-reread-click-049', () => button(leaf, '重新读取项目').click());
      await wait('authority-reread-to-be-hidden-050', () => expect(button(leaf, '重新读取项目')).toBeHidden());
      // A preserved modal can make underlying controls inert; native EOF below
      // is the readiness witness, so do not require an underlying click.
    }, 'limit=25');
  }
  async function denied(project: Project, foreign: boolean) {
    const before = await wait('authority-denied-native-facts-051', () => harness.nativeFacts(page)), beforeCounts = await wait('authority-denied-counts-052', () => harness.counts());
    const diagnostic = await beginResolveDiagnostic(page, project);
    let diagnosticFailed = false;
    try {
    const responsePromise = wait('authority-denied-resolve-headers', () => page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/projects/resolve'));
    diagnostic.start();
    const [response] = await wait('authority-denied-all-053', () => Promise.all([responsePromise, wait('authority-denied-navigation-action', () => harness.navigate(page, route(project)))]));
    diagnostic.select(response);
    need(await wait('authority-denied-finished-054', () => diagnostic.finishedWait(() => response.finished())) === null && (foreign ? [403, 404].includes(response.status()) : response.status() === 409), 'PROJECT_MODELS_AUTHORITY_OWNER_GATE_INVALID');
    await wait('authority-denied-to-be-visible-055', () => expect(page.getByRole('heading', { name: foreign ? '项目不可用' : '项目信息读取失败', exact: true })).toBeVisible());
    await wait('authority-denied-to-have-count-056', () => expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0));
    await wait('authority-denied-to-have-count-057', () => expect(page.getByRole('dialog')).toHaveCount(0));
    need((await wait('authority-denied-native-facts-058', () => harness.nativeFacts(page))).length === before.length, 'PROJECT_MODELS_AUTHORITY_DENIED_MODEL_REQUEST');
    sameOperations(beforeCounts, await wait('authority-denied-counts-059', () => harness.counts()));
    } catch (error) { diagnosticFailed = true; throw error; }
    finally { await diagnostic.finish(diagnosticFailed); }
  }

  harness.step('authority-login');
  await wait('authority-flow-login-owner-060', () => harness.loginOwner(page));
  const ownerSession = await wait('authority-flow-session-identity-061', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(ownerSession.userID === actors.owner.user_id && ownerSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_ORDINARY_OWNER_REQUIRED');
  const first = await wait('authority-flow-open-062', () => open(projects.main)), initial = await wait('authority-flow-snapshot-063', () => harness.snapshot('main'));
  const seed = initial.current.providers.find((row) => row.present);
  need(seed && Array.isArray(first.items) && first.items.length === 1 && object(first.items[0]).id === seed.id, 'PROJECT_MODELS_AUTHORITY_SEED_MISSING');
  await wait('authority-flow-to-be-enabled-064', () => expect(button(page, '创建 Provider')).toBeEnabled());
  checks.ordinary_owner = true;

  harness.step('authority-same-session-checking');
  let dialog = await wait('authority-flow-new-provider-065', () => newProvider(page, 'Models Same Session Draft', wait));
  harness.step('authority-checking-draft-ready');
  const checkingCounts = await wait('authority-flow-session-stage-066', () => sessionStage(harness.counts(), 'PROJECT_MODELS_AUTHORITY_CHECKING_COUNTS_TIMEOUT'));
  harness.step('authority-checking-counts-ready');
  const checkingFacts = await wait('authority-flow-session-stage-067', () => sessionStage(harness.nativeFacts(page), 'PROJECT_MODELS_AUTHORITY_CHECKING_FACTS_TIMEOUT'));
  harness.step('authority-checking-arm-started');
  const sessionArmResult = await wait('authority-flow-session-stage-068', () => sessionStage(harness.ipc({ action: 'arm', args: { operation: 'getCurrentSession', project: null, target_id: null, query: null, effect: 'before_dispatch_hold' } }), 'PROJECT_MODELS_AUTHORITY_CHECKING_ARM_TIMEOUT'));
  need(typeof sessionArmResult.arm_id === 'string', 'PROJECT_MODELS_AUTHORITY_ARM_INVALID');
  const sessionArm = sessionArmResult.arm_id;
  // Handle the concurrent observer immediately, then actually settle it on
  // every path, including a failed hold, UI assertion, or release.
  const restoredSession = sessionIdentity(page, () => pageshow(page, wait), harness.step, wait).then(
    (value) => ({ ok: true as const, value }), (error: unknown) => ({ ok: false as const, error }),
  );
  try {
    harness.step('authority-checking-hold-started');
    const sessionHeld = await wait('authority-flow-session-stage-069', () => sessionStage(harness.control(sessionArm, (state) => state.held === true), 'PROJECT_MODELS_AUTHORITY_CHECKING_HOLD_TIMEOUT'));
    harness.step('authority-checking-held');
    try {
      await wait('authority-flow-to-be-visible-070', () => expect(page.getByRole('heading', { name: '正在确认会话', exact: true })).toBeVisible());
      harness.step('authority-checking-heading-visible');
      await wait('authority-flow-to-be-hidden-071', () => expect(dialog).toBeHidden());
      await wait('authority-flow-to-have-count-072', () => expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0));
      harness.step('authority-checking-private-view-hidden');
      need((await wait('authority-flow-session-stage-073', () => sessionStage(harness.nativeFacts(page), 'PROJECT_MODELS_AUTHORITY_CHECKING_FACTS_TIMEOUT'))).length === checkingFacts.length, 'PROJECT_MODELS_AUTHORITY_CHECKING_MODEL_REQUEST');
      sameOperations(checkingCounts, await wait('authority-flow-session-stage-074', () => sessionStage(harness.counts(), 'PROJECT_MODELS_AUTHORITY_CHECKING_COUNTS_TIMEOUT')));
    } finally {
      harness.step('authority-checking-release-started');
      await wait('authority-flow-session-stage-075', () => sessionStage(harness.ipc({ action: 'release', args: { arm_id: sessionArm, request_token: String(sessionHeld.request_token) } }), 'PROJECT_MODELS_AUTHORITY_CHECKING_RELEASE_TIMEOUT'));
      harness.step('authority-checking-release-returned');
    }
    const restored = await wait('authority-flow-restored-session-076', () => restoredSession);
    if (!restored.ok) throw restored.error;
    const sameSession = restored.value;
    need(sameSession.sessionID === ownerSession.sessionID && sameSession.userID === ownerSession.userID && sameSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_SAME_SESSION_CHANGED');
    harness.step('authority-checking-join-started');
    await wait('authority-flow-session-stage-077', () => sessionStage(harness.control(sessionArm, (state) => state.joined === true), 'PROJECT_MODELS_AUTHORITY_CHECKING_JOIN_TIMEOUT'));
    harness.step('authority-checking-joined');
  } finally { await wait('authority-flow-restored-session-078', () => restoredSession); }
  harness.step('authority-checking-restored');
  await wait('authority-flow-reread-079', () => reread(projects.main));
  harness.step('authority-owner-reread-complete');
  dialog = providerDialog();
  await wait('authority-flow-to-have-value-080', () => expect(dialog.getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue('Models Same Session Draft'));
  await wait('authority-flow-to-have-value-081', () => expect(dialog.getByRole('textbox', { name: 'Base URL', exact: true })).toHaveValue('https://model-ui.invalid/v1'));
  harness.durableDelta(initial, await wait('authority-flow-snapshot-082', () => harness.snapshot('main')), 0, 0);
  harness.step('authority-draft-verified');
  checks.same_session_checking = true;
  await wait('authority-flow-discard-provider-083', () => discardProvider(page, dialog, harness.step, wait));

  harness.step('authority-read-owner-tail');
  const heldArm = await wait('authority-flow-arm-084', () => harness.arm('getProjectModelProvider', 'main', seed.id, 'after_complete_hold'));
  const beforeHeld = await wait('authority-flow-counts-085', () => harness.counts()), originalPath = route(projects.main);
  const heldBody = await wait('authority-flow-observe-086', () => observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, async () => {
    await wait('authority-flow-click-087', () => button(page, '读取 Provider ' + seed.id).click());
    const held = await wait('authority-flow-control-088', () => harness.control(heldArm, (state) => state.held === true && state.upstream_complete === true && state.safe_admitted === true));
    try {
      need((await wait('authority-flow-native-facts-089', () => harness.nativeFacts(page))).some((fact) => fact.method === 'GET' && fact.path.endsWith('/model-providers/' + seed.id) && !fact.ended), 'PROJECT_MODELS_AUTHORITY_NATIVE_TAIL_MISSING');
      await wait('authority-flow-to-be-disabled-090', () => expect(button(providerDialog(), '取消')).toBeDisabled());
      await wait('authority-flow-to-be-disabled-091', () => expect(button(page, '退出登录')).toBeDisabled());
      await wait('authority-flow-navigate-092', () => harness.navigate(page, route(projects.second)));
      await wait('authority-flow-to-be-093', () => expect.poll(() => new URL(page.url()).pathname).toBe(originalPath));
      await wait('authority-flow-pageshow-094', () => pageshow(page, wait));
      const afterHeld = await wait('authority-flow-counts-095', () => harness.counts());
      need(object(afterHeld.session).browser === object(beforeHeld.session).browser && operationCount(afterHeld, 'getProjectModelProvider') === operationCount(beforeHeld, 'getProjectModelProvider') + 1, 'PROJECT_MODELS_AUTHORITY_HELD_OWNER_BYPASSED');
      for (const entry of (beforeHeld.operations as unknown[]).map(object)) if (entry.operation !== 'getProjectModelProvider') need(operationCount(afterHeld, String(entry.operation)) === Number(entry.browser), 'PROJECT_MODELS_AUTHORITY_HELD_OWNER_BYPASSED');
    } finally {
      await wait('authority-flow-ipc-096', () => harness.ipc({ action: 'release', args: { arm_id: heldArm, request_token: String(held.request_token) } }));
    }
  }));
  need(heldBody.id === seed.id, 'PROJECT_MODELS_AUTHORITY_HELD_TARGET_INVALID');
  const joinedRead = await wait('authority-flow-control-097', () => harness.control(heldArm, (state) => state.joined === true));
  need((await wait('authority-flow-native-facts-098', () => harness.nativeFacts(page))).some((fact) => fact.token === joinedRead.request_token && fact.eof && fact.ended && fact.released), 'PROJECT_MODELS_AUTHORITY_HELD_RELEASE_MISSING');
  await wait('authority-flow-click-099', () => button(providerDialog(), '取消').click()); await wait('authority-flow-to-be-hidden-100', () => expect(providerDialog()).toBeHidden());
  await wait('authority-flow-open-101', () => open(projects.second));
  await wait('authority-flow-to-have-count-102', () => expect(providerDialog()).toHaveCount(0));
  await wait('authority-flow-to-have-count-103', () => expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0));
  // The UI explicitly blocks navigation while this owner is held. This is a
  // real transport-tail exclusion representative, not a fabricated late 401.
  checks.late_tail_isolation = true;

  harness.step('authority-project-name-reuse');
  await wait('authority-flow-open-104', () => open(projects.main));
  dialog = await wait('authority-flow-new-provider-105', () => newProvider(page, 'Models Retired Project Draft', wait));
  await wait('authority-flow-open-106', () => open(projects.second, true));
  const renamed = await wait('authority-flow-ipc-107', () => harness.ipc({ action: 'rename-reuse', args: { project: 'main' } }));
  const oldNameReplacement = locator(renamed.replacement), originalRenamed = locator(renamed.renamed);
  need(originalRenamed.id === projects.main.id && oldNameReplacement.id !== originalRenamed.id && oldNameReplacement.normalized_name === projects.main.normalized_name, 'PROJECT_MODELS_AUTHORITY_RENAME_BINDING_INVALID');
  const replacementPage = await wait('authority-flow-open-108', () => open(oldNameReplacement));
  need(Array.isArray(replacementPage.items) && replacementPage.items.length === 0, 'PROJECT_MODELS_AUTHORITY_REUSED_NAME_DATA_LEAK');
  await wait('authority-flow-to-have-count-109', () => expect(providerDialog()).toHaveCount(0));
  await wait('authority-flow-click-110', () => button(page, '创建 Provider').click());
  await wait('authority-flow-to-have-value-111', () => expect(providerDialog().getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue(''));
  await wait('authority-flow-click-112', () => button(providerDialog(), '取消').click());
  const renamedPage = await wait('authority-flow-open-113', () => open(originalRenamed));
  need(Array.isArray(renamedPage.items) && renamedPage.items.length === 1 && object(renamedPage.items[0]).id === seed.id, 'PROJECT_MODELS_AUTHORITY_STABLE_ID_LOST');
  projects.main = originalRenamed;
  checks.cross_project_and_name_reuse = true;

  harness.step('authority-lifecycle-gates');
  for (const key of ['archiving', 'archived'] as const) {
    const body = await wait('authority-lifecycle-' + key + '-open', () => open(projects[key]));
    need(Array.isArray(body.items) && body.items.length === 0, 'PROJECT_MODELS_AUTHORITY_READONLY_LIST_INVALID');
    await wait('authority-lifecycle-' + key + '-readonly-visible', () => expect(page.getByText('项目当前为只读状态（' + key + '）。可以读取信息；原请求恢复遵循其各自的当前条件。', { exact: true })).toBeVisible());
    await wait('authority-lifecycle-' + key + '-provider-disabled', () => expect(button(page, '创建 Provider')).toBeDisabled()); await wait('authority-lifecycle-' + key + '-credential-disabled', () => expect(button(page, '创建凭据')).toBeDisabled());
    await wait('authority-lifecycle-' + key + '-manage-enabled', () => expect(button(page, '管理凭据')).toBeEnabled());
  }
  for (const key of ['deleting', 'pending'] as const) await wait('authority-lifecycle-' + key + '-denied', () => denied(projects[key], false));
  await wait('authority-lifecycle-other-owner-denied', () => denied(projects.other, true)); await wait('authority-lifecycle-admin-owned-denied', () => denied(projects.admin_owned, true));
  checks.aux_lifecycle_gates = true;

  harness.step('authority-archived-configuration');
  await wait('authority-flow-open-122', () => open(projects.config_recovery));
  const configBefore = await wait('authority-flow-snapshot-123', () => harness.snapshot('config_recovery'));
  dialog = await wait('authority-flow-new-provider-124', () => newProvider(page, 'Models Archived Original Provider', wait));
  const configArm = await wait('authority-flow-arm-125', () => harness.arm('createProjectModelProvider', 'config_recovery', null, 'after_complete_cut'));
  await wait('authority-flow-click-126', () => button(dialog, '保存 Provider').click()); await wait('authority-flow-to-be-visible-127', () => expect(dialog.getByText(/结果尚未确认/)).toBeVisible());
  const configLost = await wait('authority-flow-control-128', () => harness.control(configArm, (state) => state.joined === true));
  await wait('authority-flow-actual-loss-129', () => harness.actualLoss(page, configLost, 1));
  const configCommitted = await wait('authority-flow-snapshot-130', () => harness.snapshot('config_recovery')); harness.durableDelta(configBefore, configCommitted, 1, 0);
  const createdProvider = configCommitted.current.providers.find((row) => row.present);
  need(createdProvider, 'PROJECT_MODELS_AUTHORITY_COMMITTED_PROVIDER_MISSING');
  const configArchived = await wait('authority-flow-ipc-131', () => harness.ipc({ action: 'archive-recovery-project', args: { project: 'config_recovery', expected_version: configCommitted.project.version } }));
  need(configArchived.project_id === projects.config_recovery.id && configArchived.lifecycle === 'archived' && configArchived.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_INVALID');
  const archiveSession = await wait('authority-flow-session-identity-132', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(archiveSession.sessionID === ownerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_SESSION_CHANGED');
  await wait('authority-flow-reread-133', () => reread(projects.config_recovery)); dialog = providerDialog();
  await wait('authority-flow-to-be-disabled-134', () => expect(button(dialog, '保存 Provider')).toBeDisabled()); await wait('authority-flow-to-be-enabled-135', () => expect(button(dialog, '按原请求重放')).toBeEnabled());
  const configReplay = await wait('authority-flow-observe-136', () => observe('createProjectModelProvider', projects.config_recovery.id, 'POST', 'model-providers', 200, () => button(dialog, '按原请求重放').click()));
  need(configReplay.kind === 'provider.create' && configReplay.resource_id === createdProvider.id && configReplay.version === createdProvider.version && configReplay.affected_references === '0', 'PROJECT_MODELS_AUTHORITY_CONFIG_REPLAY_INVALID');
  const receipt = await wait('authority-flow-strict-receipt-137', () => harness.strictReceipt(dialog));
  need(Object.keys(receipt).length === Object.keys(configReplay).length && Object.keys(configReplay).every((key) => receipt[key] === configReplay[key]), 'PROJECT_MODELS_AUTHORITY_CONFIG_RECEIPT_INVALID');
  const configAfter = await wait('authority-flow-snapshot-138', () => harness.snapshot('config_recovery')); harness.durableDelta(configCommitted, configAfter, 0, 0); harness.originalReplay(configAfter, String(configLost.origin_token));
  need(configAfter.project.lifecycle === 'archived' && configAfter.fixture_only.archive_recovery_applied, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_FACT_MISSING');
  await wait('authority-flow-click-139', () => button(dialog, '取消').click()); await wait('authority-flow-to-be-hidden-140', () => expect(dialog).toBeHidden());
  checks.archived_config_original_replay = true;

  harness.step('authority-archived-credential');
  await wait('authority-flow-open-141', () => open(projects.credential_recovery));
  const credentialBefore = await wait('authority-flow-snapshot-142', () => harness.snapshot('credential_recovery'));
  await wait('authority-flow-click-143', () => button(page, '创建凭据').click());
  let credential = credentialDialog(); await wait('authority-flow-fill-credential-144', () => harness.fillCredential(credential));
  const credentialArm = await wait('authority-flow-arm-145', () => harness.arm('createProjectModelCredential', 'credential_recovery', null, 'after_complete_disconnect'));
  await wait('authority-flow-click-146', () => button(credential, '创建凭据').click()); await wait('authority-flow-to-be-visible-147', () => expect(credential.getByText(/结果尚未确认/)).toBeVisible());
  need(await wait('authority-flow-input-value-148', () => credential.getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue()) === '', 'PROJECT_MODELS_AUTHORITY_PRIVATE_INPUT_NOT_CLEARED');
  const credentialLost = await wait('authority-flow-control-149', () => harness.control(credentialArm, (state) => state.joined === true)); await wait('authority-flow-actual-loss-150', () => harness.actualLoss(page, credentialLost, 0));
  const credentialCommitted = await wait('authority-flow-snapshot-151', () => harness.snapshot('credential_recovery')); harness.durableDelta(credentialBefore, credentialCommitted, 0, 1);
  const credentialArchived = await wait('authority-flow-ipc-152', () => harness.ipc({ action: 'archive-recovery-project', args: { project: 'credential_recovery', expected_version: credentialCommitted.project.version } }));
  need(credentialArchived.project_id === projects.credential_recovery.id && credentialArchived.lifecycle === 'archived' && credentialArchived.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_INVALID');
  const credentialSession = await wait('authority-flow-session-identity-153', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(credentialSession.sessionID === ownerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ARCHIVE_SESSION_CHANGED');
  await wait('authority-flow-reread-154', () => reread(projects.credential_recovery)); credential = credentialDialog();
  await wait('authority-flow-to-be-disabled-155', () => expect(button(credential, '按原请求重放')).toBeDisabled()); await wait('authority-flow-to-be-disabled-156', () => expect(button(credential, '创建凭据')).toBeDisabled());
  const lookupCounts = await wait('authority-flow-counts-157', () => harness.counts());
  const lookup = await wait('authority-flow-observe-158', () => observe('lookupProjectModelCredential', projects.credential_recovery.id, 'POST', 'model-credential-commands/lookup', 200, () => button(credential, '查证原请求').click()));
  need(lookup.observed === true && object(lookup.result).credential_id === credentialCommitted.current.credentials[0]?.credential_id, 'PROJECT_MODELS_AUTHORITY_CREDENTIAL_LOOKUP_INVALID');
  await wait('authority-flow-to-be-visible-159', () => expect(credential.getByLabel('历史观察', { exact: true })).toBeVisible());
  await wait('authority-flow-to-be-visible-160', () => expect(credential.getByText(/结果尚未确认/)).toBeVisible()); await wait('authority-flow-to-have-count-161', () => expect(credential.getByLabel('严格执行回执', { exact: true })).toHaveCount(0));
  await wait('authority-flow-to-be-disabled-162', () => expect(button(credential, '按原请求重放')).toBeDisabled());
  const credentialAfter = await wait('authority-flow-snapshot-163', () => harness.snapshot('credential_recovery')); harness.durableDelta(credentialCommitted, credentialAfter, 0, 0);
  need(credentialAfter.project.lifecycle === 'archived' && credentialAfter.fixture_only.archive_recovery_applied && credentialAfter.origins.length === 1 && credentialAfter.origins[0]!.comparison_count === 0, 'PROJECT_MODELS_AUTHORITY_CREDENTIAL_REPLAY_OCCURRED');
  const afterLookup = await wait('authority-flow-counts-164', () => harness.counts());
  for (const row of (lookupCounts.operations as unknown[]).map(object)) need(operationCount(afterLookup, String(row.operation)) === Number(row.browser) + (row.operation === 'lookupProjectModelCredential' ? 1 : 0), 'PROJECT_MODELS_AUTHORITY_LOOKUP_SIDE_EFFECT');
  await wait('authority-flow-click-165', () => button(credential.locator('footer'), '关闭').click()); await wait('authority-flow-to-be-hidden-166', () => expect(credential).toBeHidden());
  const pending = page.getByLabel('原请求与历史观察', { exact: true });
  await wait('authority-flow-to-be-disabled-167', () => expect(button(pending, '按原请求重放')).toBeDisabled());
  await wait('authority-flow-click-168', () => button(pending, '放弃本地追踪').click());
  await wait('authority-flow-click-169', () => button(page.getByRole('dialog', { name: '放弃本地原请求追踪？', exact: true }), '放弃追踪').click());
  harness.durableDelta(credentialAfter, await wait('authority-flow-snapshot-170', () => harness.snapshot('credential_recovery')), 0, 0);
  checks.archived_credential_lookup_only = true;

  harness.step('authority-reference-unbound');
  await wait('authority-flow-open-171', () => open(projects.referenced));
  const referencedBefore = await wait('authority-flow-snapshot-172', () => harness.snapshot('referenced')), referencedModel = referencedBefore.current.models.find((row) => row.present);
  need(referencedModel, 'PROJECT_MODELS_AUTHORITY_REFERENCE_TARGET_MISSING');
  await wait('authority-flow-observe-173', () => observe('listProjectModels', projects.referenced.id, 'GET', 'models', 200, () => button(page, '项目 Models（全部 Providers）').click(), 'limit=25'));
  await wait('authority-flow-observe-174', () => observe('getProjectModel', projects.referenced.id, 'GET', 'models/' + referencedModel.id, 200, () => button(page, '读取 Model ' + referencedModel.id).click()));
  const modelDialog = page.getByRole('dialog').filter({ has: page.locator('#project-model-form') });
  await wait('authority-flow-observe-175', () => observe('listProjectAvailableChatModels', projects.referenced.id, 'GET', 'available-chat-models', 200, () => button(modelDialog, '删除 Model').click(), 'limit=25'));
  const deletion = page.getByRole('dialog', { name: '删除 Model', exact: true });
  await wait('authority-flow-click-176', () => button(deletion, '删除替代').click()); await wait('authority-flow-click-177', () => page.getByRole('option', { name: '无替代', exact: true }).click());
  const refResult = await wait('authority-flow-ipc-178', () => harness.ipc({ action: 'reference-fact', args: { project: 'referenced', state: 'present' } }));
  need(refResult.project_id === projects.referenced.id && refResult.model_id === referencedModel.id && refResult.reference_present === true && refResult.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_FACT_INVALID');
  const referenced = await wait('authority-flow-snapshot-179', () => harness.snapshot('referenced'));
  need(referenced.reference_presence.models.find((row) => row.id === referencedModel.id)?.present === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_FACT_MISSING');
  const rejection = await wait('authority-flow-observe-180', () => observe('deleteProjectModel', projects.referenced.id, 'DELETE', 'models/' + referencedModel.id, 503, () => button(deletion, '确认删除').click()));
  need(rejection.code === 'DEPENDENCY_UNBOUND' && rejection.status === 503 && ['not_started', 'not_committed'].includes(String(rejection.commit_state)), 'PROJECT_MODELS_AUTHORITY_REFERENCE_REJECTION_INVALID');
  await wait('authority-flow-to-be-visible-181', () => expect(deletion.getByText('仍被使用的模型暂不能在此删除。没有执行引用迁移。', { exact: true })).toBeVisible());
  await wait('authority-flow-to-have-count-182', () => expect(deletion.getByLabel('严格执行回执', { exact: true })).toHaveCount(0));
  const rejected = await wait('authority-flow-snapshot-183', () => harness.snapshot('referenced')); harness.durableDelta(referenced, rejected, 0, 0);
  need(rejected.current.models.find((row) => row.id === referencedModel.id)?.version === referencedModel.version && rejected.current.models.find((row) => row.id === referencedModel.id)?.present === true && rejected.reference_presence.models.find((row) => row.id === referencedModel.id)?.present === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_REWRITTEN');
  await wait('authority-flow-click-184', () => button(deletion, '取消').click()); await wait('authority-flow-to-be-hidden-185', () => expect(deletion).toBeHidden());
  await wait('authority-flow-click-186', () => button(modelDialog, '取消').click()); await wait('authority-flow-to-be-hidden-187', () => expect(modelDialog).toBeHidden());
  const removed = await wait('authority-flow-ipc-188', () => harness.ipc({ action: 'reference-fact', args: { project: 'referenced', state: 'absent' } }));
  need(removed.reference_present === false && removed.fixture_only === true, 'PROJECT_MODELS_AUTHORITY_REFERENCE_CLEANUP_INVALID');
  checks.reference_unbound = true;

  harness.step('authority-current-revocation');
  await wait('authority-flow-open-189', () => open(projects.main));
  await wait('authority-flow-observe-190', () => observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, () => button(page, '读取 Provider ' + seed.id).click()));
  dialog = providerDialog(); await wait('authority-flow-fill-191', () => dialog.getByRole('textbox', { name: 'Provider 名称', exact: true }).fill('Models Revoked Session Draft'));
  const beforeRevocation = await wait('authority-flow-snapshot-192', () => harness.snapshot('main'));
  const revoked = await wait('authority-flow-ipc-193', () => harness.ipc({ action: 'logout', args: { session_id: ownerSession.sessionID } }));
  need(revoked.session_id === ownerSession.sessionID && revoked.revoked === true, 'PROJECT_MODELS_AUTHORITY_REVOCATION_INVALID');
  const deniedCurrent = await wait('authority-flow-observe-194', () => observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 401, () => button(dialog, '重新读取 Provider').click()));
  need(['SESSION_REVOKED', 'UNAUTHENTICATED'].includes(String(deniedCurrent.code)) && deniedCurrent.status === 401, 'PROJECT_MODELS_AUTHORITY_REVOCATION_RESPONSE_INVALID');
  await wait('authority-flow-to-be-visible-195', () => expect(page.getByRole('heading', { name: '会话尚未确认', exact: true })).toBeVisible());
  await wait('authority-flow-to-have-count-196', () => expect(providerDialog()).toHaveCount(0));
  await wait('authority-flow-to-have-count-197', () => expect(page.getByRole('list', { name: 'Providers 列表', exact: true })).toHaveCount(0));
  harness.durableDelta(beforeRevocation, await wait('authority-flow-snapshot-198', () => harness.snapshot('main')), 0, 0);
  checks.current_revocation = true;

  harness.step('authority-true-identity-change');
  await wait('authority-flow-click-199', () => button(page, '检查当前会话').click());
  await wait('authority-flow-to-be-visible-200', () => expect(page.locator('#login-email')).toBeVisible());
  // Same-document form interactions preserve every earlier native observation.
  // Re-login as the same human still creates a genuinely different Session.
  // The live old-session draft here is Provider input. Credential tracking was
  // explicitly abandoned earlier; the later empty input is a fresh-form check.
  await wait('authority-flow-private-login-201', () => privateLogin(page, actors.owner, wait));
  const newOwnerSession = await wait('authority-flow-session-identity-202', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(newOwnerSession.userID === ownerSession.userID && newOwnerSession.sessionID !== ownerSession.sessionID && newOwnerSession.role === 'user', 'PROJECT_MODELS_AUTHORITY_NEW_SESSION_MISSING');
  await wait('authority-flow-open-203', () => open(projects.main));
  await wait('authority-flow-to-have-count-204', () => expect(providerDialog()).toHaveCount(0));
  await wait('authority-flow-to-have-count-205', () => expect(page.getByLabel('原请求与历史观察', { exact: true })).toHaveCount(0));
  await wait('authority-flow-to-have-count-206', () => expect(page.getByLabel('已创建凭据', { exact: true })).toHaveCount(0));
  await wait('authority-flow-observe-207', () => observe('getProjectModelProvider', projects.main.id, 'GET', 'model-providers/' + seed.id, 200, () => button(page, '读取 Provider ' + seed.id).click()));
  await wait('authority-flow-to-have-value-208', () => expect(providerDialog().getByRole('textbox', { name: 'Provider 名称', exact: true })).toHaveValue('Models Seed Provider'));
  await wait('authority-flow-click-209', () => button(providerDialog(), '取消').click());
  await wait('authority-flow-click-210', () => button(page, '创建凭据').click());
  need(await wait('authority-flow-input-value-211', () => credentialDialog().getByLabel(/^新凭据材料(?:\s*\*)?$/).inputValue()) === '', 'PROJECT_MODELS_AUTHORITY_NEW_SESSION_MATERIAL_LEAK');
  await wait('authority-flow-click-212', () => button(credentialDialog().locator('footer'), '关闭').click());
  checks.true_identity_change = true;

  harness.step('authority-other-owner-and-admin');
  await wait('authority-flow-click-213', () => button(page, '退出登录').click()); await wait('authority-flow-to-be-visible-214', () => expect(page.locator('#login-email')).toBeVisible());
  await wait('authority-flow-private-login-215', () => privateLogin(page, actors.other_owner, wait));
  const otherSession = await wait('authority-flow-session-identity-216', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(otherSession.userID === actors.other_owner.user_id && otherSession.role === 'user' && otherSession.sessionID !== newOwnerSession.sessionID, 'PROJECT_MODELS_AUTHORITY_OTHER_OWNER_INVALID');
  await wait('authority-flow-denied-217', () => denied(projects.main, true));
  await wait('authority-flow-open-218', () => open(projects.other)); await wait('authority-flow-to-be-enabled-219', () => expect(button(page, '创建 Provider')).toBeEnabled());
  await wait('authority-flow-click-220', () => button(page, '退出登录').click()); await wait('authority-flow-to-be-visible-221', () => expect(page.locator('#login-email')).toBeVisible());
  await wait('authority-flow-private-login-222', () => privateLogin(page, actors.other_admin, wait));
  const adminSession = await wait('authority-flow-session-identity-223', () => sessionIdentity(page, () => pageshow(page, wait), harness.step, wait));
  need(adminSession.userID === actors.other_admin.user_id && adminSession.role === 'admin' && adminSession.sessionID !== otherSession.sessionID, 'PROJECT_MODELS_AUTHORITY_ADMIN_INVALID');
  await wait('authority-flow-denied-224', () => denied(projects.main, true));
  await wait('authority-flow-open-225', () => open(projects.admin_owned)); await wait('authority-flow-to-be-enabled-226', () => expect(button(page, '创建 Provider')).toBeEnabled());
  await wait('authority-flow-to-have-count-227', () => expect(page.getByLabel('原请求与历史观察', { exact: true })).toHaveCount(0));
  await wait('authority-flow-to-have-count-228', () => expect(page.getByRole('dialog')).toHaveCount(0));
  checks.admin_owner_only = checks.other_owner_and_admin_rejected = true;
  const finalCounts = await wait('authority-flow-counts-229', () => harness.counts()), controls = object(finalCounts.controls);
  need(controls.armed === 4 && controls.claimed === 4 && controls.held === 2 && controls.held_joined === 2 && controls.cut === 1 && controls.disconnected === 1, 'PROJECT_MODELS_AUTHORITY_CONTROL_COUNTS_INVALID');
  for (const [operation, expected] of [['createProjectModelProvider', 2], ['createProjectModelCredential', 1], ['lookupProjectModelCredential', 1], ['deleteProjectModel', 1]] as const) need(operationCount(finalCounts, operation) === expected, 'PROJECT_MODELS_AUTHORITY_OPERATION_COUNTS_INVALID');
  for (const operation of ['updateProjectModelProvider', 'deleteProjectModelProvider', 'createProjectModel', 'updateProjectModel', 'updateProjectModelCredential', 'deleteProjectModelCredential', 'lookupProjectModelConfiguration']) need(operationCount(finalCounts, operation) === 0, 'PROJECT_MODELS_AUTHORITY_IMPLICIT_WRITE');
  harness.step('authority-same-body-finish');
  await wait('authority-flow-finish-230', () => harness.finish(page, checks));
}

