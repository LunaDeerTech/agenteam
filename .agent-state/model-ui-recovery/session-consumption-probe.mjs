#!/usr/bin/env node
// Independent investigation only. --prepare is offline. The Python subreaper
// owns local runtime entry/retirement; --owned-fixture requires a private Go
// supervisor descriptor. Direct --run is deliberately unavailable.
import { createHash, randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lstat, mkdir, readFile, writeFile } from 'node:fs/promises';
import { build } from '../../web/node_modules/vite/dist/node/index.js';
import { rolldown } from '../../web/node_modules/rolldown/dist/index.mjs';

const source = fileURLToPath(import.meta.url), root = resolve(dirname(source), '../..');
const output = join(root, 'output/ai/model-ui-session-probe');
const require = createRequire(join(root, 'tests/account-captcha-web/package.json'));
const { chromium } = require('playwright');
const { expect } = require('@playwright/test');
const packageVersion = require('@playwright/test/package.json').version;
const need = (value, code = 'SESSION_PROBE_INVARIANT') => { if (!value) throw new Error(code); };
const json = async (path, value) => writeFile(path, JSON.stringify(value, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
const nativeSource = join(root, '.agent-state/model-ui-recovery/native-client-probe.ts');
const nativeBundlePath = join(root, 'output/ai/model-ui-recovery/client-probe/native-client-probe.js');
function casesFor(mode) {
  need(['default', 'promise-boundary', 'owned-fixture', 'owned-app', 'owned-login'].includes(mode), 'SESSION_PROBE_MODE');
  if (mode === 'owned-login') return ['early', 'stable'].map(action => ({ frame: 'browser-login', consumer: 'account', action }));
  if (mode === 'owned-app') return ['direct-account', 'pageshow'].map(action => ({ frame: 'app', consumer: 'account', action }));
  if (mode === 'owned-fixture') return [{ frame: 'owned-fixture', consumer: 'account' }];
  return mode === 'default'
    ? ['length', 'chunked', 'truncated-json', 'disconnect'].flatMap(frame => ['native', 'account'].map(consumer => ({ frame, consumer })))
    : ['original', 'derived'].map(promise_mode => ({ frame: 'length', consumer: 'account', promise_mode }));
}
function argumentsFor(values) {
  const args = [...values], index = args.indexOf('--mode');
  let mode = 'default';
  if (index !== -1) { need(index === args.length - 2, 'SESSION_PROBE_ARGUMENTS'); mode = args[index + 1]; args.splice(index); }
  casesFor(mode);
  return { args, mode };
}
async function processIdentity(pid) {
  const raw = await readFile(`/proc/${pid}/stat`, 'utf8'), fields = raw.slice(raw.lastIndexOf(')') + 2).trim().split(/\s+/);
  return { pid, group: Number(fields[2]), start: fields[19] };
}
const bounded = async (work, ms, code) => {
  let timer;
  try { return await Promise.race([work, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(code)), ms); })]); }
  finally { clearTimeout(timer); }
};

// Serialized into the private browser bundle. Values and decoded Session stay
// in memory; only fixed enums, booleans, lengths and call order cross back out.
function installBrowser(createAccountAPI) {
  window.runSessionConsumption = async (consumer, expected, disconnect) => {
    const facts = { fetches: 0, readers: 0, bytes: 0, read_done: false, read_failed: false,
      reader_cancel_calls: 0, reader_cancel_settled: 0, stream_cancel_calls: 0,
      stream_cancel_settled: 0, released: false, decoded: false, identity_equal: false,
      outcome: 'pending', events: [] };
    const event = (name) => { if (facts.events.length >= 32) throw new Error('SESSION_PROBE_EVENT_CAP'); facts.events.push(name); };
    const nativeFetch = window.fetch.bind(window);
    let cutSignaled = false;
    const observedFetch = (input, init) => {
      if (input !== '/api/v1/session' || init.method !== 'GET') throw new Error('SESSION_PROBE_REQUEST_REJECTED');
      facts.fetches++; event('fetch');
      return nativeFetch(input, init).then((response) => {
        const stream = response.body;
        if (!stream) throw new Error('SESSION_PROBE_STREAM_MISSING');
        const getReader = stream.getReader.bind(stream), streamCancel = stream.cancel.bind(stream);
        stream.cancel = (...args) => {
          facts.stream_cancel_calls++; event('stream-cancel-call');
          const pending = streamCancel(...args);
          void pending.then(() => { facts.stream_cancel_settled++; event('stream-cancel-settled'); }, () => { facts.stream_cancel_settled++; event('stream-cancel-rejected'); });
          return pending;
        };
        stream.getReader = (...args) => {
          facts.readers++; event('get-reader');
          const reader = getReader(...args), read = reader.read.bind(reader), cancel = reader.cancel.bind(reader), release = reader.releaseLock.bind(reader);
          reader.read = (...readArgs) => {
            const pending = read(...readArgs);
            // Return the ORIGINAL Promise. Observe its settlement without an
            // additional read, await, clone, tee, buffering or stream replacement.
            void pending.then((value) => {
              if (value.done) { facts.read_done = true; event('read-done'); }
              else {
                facts.bytes += value.value.byteLength; event('read-data');
                // Negative control only: cut the owned socket after the real
                // reader has observed its prefix. No extra HTTP or sleep race.
                if (disconnect && !cutSignaled) {
                  cutSignaled = true;
                  void window.sessionProbeReadReady().catch(() => undefined);
                }
              }
            }, () => { facts.read_failed = true; event('read-failed'); });
            return pending;
          };
          reader.cancel = (...cancelArgs) => {
            facts.reader_cancel_calls++; event('reader-cancel-call');
            const pending = cancel(...cancelArgs);
            void pending.then(() => { facts.reader_cancel_settled++; event('reader-cancel-settled'); }, () => { facts.reader_cancel_settled++; event('reader-cancel-rejected'); });
            return pending;
          };
          reader.releaseLock = () => { release(); facts.released = true; event('release'); };
          return reader;
        };
        return response;
      });
    };
    const abort = new AbortController();
    try {
      let view;
      if (consumer === 'account') view = await createAccountAPI(observedFetch).getSession(abort.signal);
      else {
        const response = await observedFetch('/api/v1/session', { method: 'GET', headers: { Accept: 'application/json, application/problem+json' }, credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal: abort.signal });
        const reader = response.body.getReader(), decoder = new TextDecoder('utf-8', { fatal: true });
        let text = '';
        try {
          for (;;) {
            const item = await reader.read();
            if (item.done) break;
            if (facts.bytes > 8192) throw new Error('SESSION_PROBE_BODY_CAP');
            text += decoder.decode(item.value, { stream: true });
          }
          text += decoder.decode(); view = JSON.parse(text);
        } finally { reader.releaseLock(); }
      }
      facts.decoded = true;
      facts.identity_equal = view.user.id === expected.user.id && view.session.id === expected.session.id && view.csrf_token === expected.csrf_token && view.user.display_name === expected.user.display_name;
      facts.outcome = 'success'; event('client-returned');
    } catch (error) {
      facts.outcome = consumer === 'account' && ['transport', 'invalid-response', 'cancelled'].includes(error?.kind) ? error.kind : 'rejected';
      event('client-rejected');
    }
    return facts;
  };
}

// Both arms use the same installed native factory. Only the Promise returned
// to the real account API differs; no reader/cancel implementation is copied.
function installPromiseBoundary(createAccountAPI) {
  window.runSessionPromiseBoundary = async (variant, expected) => {
    const check = (value, code) => { if (!value) throw new Error(code); };
    check(['original', 'derived'].includes(variant), 'SESSION_PROBE_PROMISE_MODE');
    const probe = window.__projectModelsProbe, slot = 'session-promise-boundary';
    check(probe?.sessionBegin(slot, Date.now() + 250, undefined, true) === true, 'SESSION_PROBE_NATIVE_ARM');
    const nativeFetch = window.fetch.bind(window), abort = new AbortController();
    let fetches = 0, returnedOriginal = false, decoded = false, identityEqual = false, outcome = 'pending', snapshot;
    try {
      const fetcher = (input, init) => {
        check(input === '/api/v1/session' && init.method === 'GET', 'SESSION_PROBE_REQUEST_REJECTED');
        fetches++;
        const p = nativeFetch(input, init);
        const returned = variant === 'original' ? p : p.then(response => response);
        returnedOriginal = returned === p;
        return returned;
      };
      const view = await createAccountAPI(fetcher).getSession(abort.signal);
      decoded = true;
      identityEqual = view.user.id === expected.user.id && view.session.id === expected.session.id && view.csrf_token === expected.csrf_token && view.user.display_name === expected.user.display_name;
      outcome = 'success';
    } catch (error) {
      outcome = ['transport', 'invalid-response', 'cancelled'].includes(error?.kind) ? error.kind : 'rejected';
    } finally {
      try { snapshot = probe.sessionSnapshot(slot, expected.session.id); }
      finally { probe.sessionEnd(slot, expected.session.id); }
    }
    const counts = ['requests', 'readers', 'bytes', 'read_rejected', 'reader_cancel_calls', 'reader_cancel_settled', 'reader_cancel_rejected', 'stream_cancel_calls', 'stream_cancel_settled', 'stream_cancel_rejected', 'release_calls', 'release_successes', 'abort_events', 'content_length'];
    const flags = ['read_done', 'request_id_match', 'eof_before_interruption', 'content_length_present', 'content_length_valid', 'content_encoding_identity', 'content_length_comparable', 'content_length_matches_eof'];
    check(snapshot && counts.every(key => Number.isSafeInteger(snapshot[key]) && snapshot[key] >= 0) && flags.every(key => typeof snapshot[key] === 'boolean'), 'SESSION_PROBE_NATIVE_SHAPE');
    const native = Object.fromEntries([...counts, ...flags].map(key => [key, snapshot[key]]));
    return { fetches, readers: native.readers, bytes: native.bytes, read_done: native.read_done, read_failed: native.read_rejected !== 0,
      reader_cancel_calls: native.reader_cancel_calls, reader_cancel_settled: native.reader_cancel_settled,
      stream_cancel_calls: native.stream_cancel_calls, stream_cancel_settled: native.stream_cancel_settled,
      released: native.release_successes === 1, decoded, identity_equal: identityEqual, outcome,
      arm_success: true, returned_original_promise: returnedOriginal, native };
  };
}

function installFixtureConsumer(createAccountAPI) {
  window.runFixtureSession = async (expected) => {
    let fetches = 0;
    const fetcher = (input, init) => {
      if (input !== '/api/v1/session' || init.method !== 'GET') throw new Error('SESSION_PROBE_REQUEST_REJECTED');
      fetches++;
      return window.fetch(input, init);
    };
    try {
      const view = await createAccountAPI(fetcher).getSession(new AbortController().signal);
      return { fetches, decoded: true, identity_equal: view.user.id === expected.user_id && view.session.id === expected.session_id && view.csrf_token === expected.csrf && view.user.role === 'user', outcome: 'success' };
    } catch {
      return { fetches, decoded: false, identity_equal: false, outcome: 'client-rejected' };
    }
  };
  window.finishFixtureSession = (requestID) => {
    const probe = window.__projectModelsProbe;
    let snapshot;
    try { snapshot = probe.sessionSnapshot('session-proxy', requestID); }
    finally { probe.sessionEnd('session-proxy', requestID); }
    const counts = ['requests', 'readers', 'bytes', 'read_rejected', 'reader_cancel_calls', 'reader_cancel_settled', 'reader_cancel_rejected', 'stream_cancel_calls', 'stream_cancel_settled', 'stream_cancel_rejected', 'release_calls', 'release_successes', 'abort_events', 'content_length'];
    const flags = ['read_done', 'request_id_match', 'eof_before_interruption', 'content_length_present', 'content_length_valid', 'content_encoding_identity', 'content_length_comparable', 'content_length_matches_eof'];
    if (!snapshot || !counts.every(key => Number.isSafeInteger(snapshot[key]) && snapshot[key] >= 0) || !flags.every(key => typeof snapshot[key] === 'boolean')) throw new Error('SESSION_PROBE_NATIVE_SHAPE');
    return Object.fromEntries([...counts, ...flags].map(key => [key, snapshot[key]]));
  };
}

async function inputs(mode) {
  need(packageVersion === '1.56.1', 'SESSION_PROBE_PLAYWRIGHT_VERSION');
  const files = [source, ...(['owned-fixture', 'owned-app', 'owned-login'].includes(mode) ? [] : [join(dirname(source), 'run-session-consumption.py'), join(dirname(source), 'run-shared-components.py')]), join(root, 'web/src/api/account.ts'), join(root, 'web/src/api/client.ts'), join(root, 'web/package-lock.json'), join(root, 'tests/account-captcha-web/package-lock.json')];
  if (mode !== 'default') files.push(nativeSource, nativeBundlePath, ...['system-account', 'project-model-credentials', 'project-models'].map(name => join(root, `web/src/api/${name}.ts`)));
  if (['owned-fixture', 'owned-app', 'owned-login'].includes(mode)) files.push(join(root, 'tests/account/project_owner_models_web_fixture_test.go'), join(root, 'tests/account/project_owner_models_web_test.go'), join(dirname(source), 'run-owned-top.py'));
  return Object.fromEntries(await Promise.all(files.map(async (path) => [path, hash(await readFile(path))])));
}
async function prepare(mode) {
  await mkdir(output, { recursive: true, mode: 0o700 });
  need((await lstat(output)).isDirectory() && !(await lstat(output)).isSymbolicLink());
  const before = await inputs(mode);
  if (mode !== 'default') {
    const nativeBuild = await rolldown({ input: nativeSource });
    try {
      const { output: chunks } = await nativeBuild.generate({ format: 'iife', name: 'ProjectModelsNativeProbe' });
      need(chunks.length === 1 && chunks[0].type === 'chunk' && chunks[0].moduleIds.every(path => Object.hasOwn(before, path)) && Buffer.from(chunks[0].code).equals(await readFile(nativeBundlePath)), 'SESSION_PROBE_NATIVE_PAIR');
    } finally { await nativeBuild.close(); }
  }
  const virtual = '\0independent-session-consumption', entry = join(output, 'virtual-session-entry.js');
  const result = await build({ configFile: false, root, logLevel: 'silent', plugins: [{
    name: 'independent-session-entry', enforce: 'pre', resolveId: (id) => id === entry ? virtual : undefined,
    load: (id) => id === virtual ? `import { createAccountAPI } from ${JSON.stringify(join(root, 'web/src/api/account.ts'))};\n(${(['owned-fixture', 'owned-app', 'owned-login'].includes(mode) ? installFixtureConsumer : mode === 'promise-boundary' ? installPromiseBoundary : installBrowser).toString()})(createAccountAPI);` : undefined,
  }], build: { write: false, minify: false, sourcemap: false, lib: { entry, name: 'SessionConsumption', formats: ['iife'] } } });
  const chunks = (Array.isArray(result) ? result : [result]).flatMap((row) => row.output);
  need(chunks.length === 1 && chunks[0].type === 'chunk');
  const bundle = Buffer.from(chunks[0].code);
  need(JSON.stringify(before) === JSON.stringify(await inputs(mode)), 'SESSION_PROBE_INPUT_CHANGED');
  await writeFile(join(output, 'client.js'), bundle, { mode: 0o600 });
  await writeFile(join(output, 'prepared.json'), JSON.stringify({ mode, inputs: before, bundle_sha256: hash(bundle), playwright: packageVersion, browser_version: '151.0.7922.173', cases: casesFor(mode).length, network_started: false }), { mode: 0o600 });
  console.log(JSON.stringify({ prepared: true, mode, cases: casesFor(mode).length, network_started: false }));
}

function checkFixtureNative(native) {
  need(native.requests === 1 && native.request_id_match && native.eof_before_interruption && native.content_length_comparable && native.content_length_matches_eof && native.readers === 1 && native.read_rejected === 0 && native.abort_events === 0 && native.reader_cancel_calls === 1 && native.reader_cancel_settled === 1 && native.reader_cancel_rejected === 0 && native.stream_cancel_calls === 1 && native.stream_cancel_settled === 1 && native.stream_cancel_rejected === 0 && native.release_calls === 1 && native.release_successes === 1, 'SESSION_PROBE_NATIVE_BOUNDARY');
}

async function appHomeReady(page, origin) {
  await page.waitForURL(origin + '/', { timeout: 5000 });
  await page.getByRole('heading', { name: '首页', exact: true }).waitFor({ state: 'visible', timeout: 5000 });
  await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeEnabled({ timeout: 5000 });
}

// Both cells load the same real App and perform the same public navigation.
// Only the second Session action differs; App-private identity is checked in
// the Go proxy's existing buffer, never by a second read or response.json().
async function appSessionCase(browser, fixture, nativeBundle, bundle, action, rows, step) {
  need(action === 'direct-account' || action === 'pageshow', 'SESSION_PROBE_APP_ACTION');
  const context = await browser.newContext({ serviceWorkers: 'block' }), page = await context.newPage();
  const observations = [], report = { frame: 'app', consumer: 'account', action, phases: [], session_requests: 0, unexpected_api_requests: 0, observer_joined_after_close: false };
  rows.push(report);
  let cdp, bootstrapConnection, completed = false;
  const requested = (request) => {
    const url = new URL(request.url());
    if (url.pathname === '/api/v1' || url.pathname.startsWith('/api/v1/')) {
      if (request.url() === fixture.origin + '/api/v1/session' && request.method() === 'GET') report.session_requests++;
      else report.unexpected_api_requests++;
    }
  };
  page.on('request', requested);
  try {
    await context.addCookies(fixture.cookies.map(cookie => ({ ...cookie, url: fixture.origin })));
    await page.addInitScript({ content: nativeBundle.toString('utf8') + '\nProjectModelsNativeProbe.install();' });
    step('app-' + action + '-landing');
    const landing = await page.goto(fixture.origin + '/session-proxy-diagnostic', { waitUntil: 'load', timeout: 5000 });
    need(landing?.status() === 200, 'SESSION_PROBE_APP_DOCUMENT');
    await page.getByRole('heading', { name: '未找到页面', exact: true }).waitFor({ state: 'visible', timeout: 5000 });
    need(report.session_requests === 0 && report.unexpected_api_requests === 0, 'SESSION_PROBE_APP_LANDING_REQUESTS');
    await page.addScriptTag({ content: bundle.toString('utf8') });
    cdp = await context.newCDPSession(page); await cdp.send('Network.enable');
    for (const phase of ['bootstrap', 'measurement']) {
      step('app-' + action + '-' + phase);
      const observation = sessionObservation(page, cdp, fixture.origin, fixture, null);
      observations.push(observation);
      const row = { phase, facts: null, native: null, events: observation.events, application_ready: false };
      report.phases.push(row);
      try {
        need(await page.evaluate(() => window.__projectModelsProbe.sessionBegin('session-proxy', Date.now() + 250, undefined, true)) === true, 'SESSION_PROBE_NATIVE_ARM');
        if (phase === 'bootstrap') await page.getByRole('link', { name: '返回入口', exact: true }).click({ timeout: 5000 });
        else if (action === 'direct-account') row.facts = await bounded(page.evaluate(expected => window.runFixtureSession(expected), fixture.expected), 5000, 'SESSION_PROBE_CONSUMPTION_TIMEOUT');
        else await bounded(page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow'))), 5000, 'SESSION_PROBE_CONSUMPTION_TIMEOUT');
        await appHomeReady(page, fixture.origin);
        row.application_ready = true;
        await bounded(observation.terminal, 2000, 'SESSION_PROBE_EVENTS_TIMEOUT');
        await bounded(observation.headers(), 1000, 'SESSION_PROBE_HEADER_OBSERVATION');
        observation.bind();
        row.native = await bounded(page.evaluate(requestID => window.finishFixtureSession(requestID), observation.requestID()), 1000, 'SESSION_PROBE_NATIVE_SNAPSHOT');
        // Preserve the existing observation window; a pending finished Promise
        // remains pending evidence, never a manufactured completion.
        await bounded(new Promise(resolve => { if (observation.events.response_finished !== 'pending') resolve(); else setTimeout(resolve, 100); }), 250, 'SESSION_PROBE_OBSERVATION_TIMEOUT');
        const events = observation.events;
        need(events.request_count === 1 && events.cdp_request_count === 1 && events.pw_headers_bound && events.cdp_headers_bound, 'SESSION_PROBE_REQUEST_BINDING');
        checkFixtureNative(row.native);
        need(report.session_requests === (phase === 'bootstrap' ? 1 : 2) && report.unexpected_api_requests === 0, 'SESSION_PROBE_APP_REQUESTS');
        if (phase === 'bootstrap') {
          need(events.finished && events.cdp_finished && !events.failed && !events.cdp_failed && events.response_finished === 'complete', 'SESSION_PROBE_APP_BOOTSTRAP_FINISHED');
          bootstrapConnection = observation.connection();
        } else {
          const connection = observation.connection(), comparable = bootstrapConnection !== undefined && connection !== undefined;
          row.connection = { comparable, same_as_bootstrap: comparable && bootstrapConnection === connection };
          if (action === 'direct-account') need(row.facts.fetches === 1 && row.facts.decoded && row.facts.identity_equal && row.facts.outcome === 'success', 'SESSION_PROBE_CONTROL_RESULT');
        }
      } finally {
        // Best-effort failure snapshot closes this slot without replacing the
        // original failure; no body/credentials/exception text is exported.
        if (row.native === null) {
          try { row.native = await bounded(page.evaluate(requestID => window.finishFixtureSession(requestID), observation.requestID() ?? null), 1000, 'SESSION_PROBE_NATIVE_SNAPSHOT'); } catch { /* unavailable */ }
        }
        row.events = { ...observation.events };
        observation.close();
      }
    }
    completed = true;
    await cdp.detach();
  } finally {
    await bounded(context.close(), 2000, 'SESSION_PROBE_CONTEXT_CLOSE');
    await bounded(Promise.all(observations.map(observation => observation.finished())), 1000, 'SESSION_PROBE_OBSERVER_JOIN');
    for (const observation of observations) observation.close();
    page.off('request', requested);
    report.observer_joined_after_close = true;
    if (completed) need(report.session_requests === 2 && report.unexpected_api_requests === 0, 'SESSION_PROBE_APP_REQUESTS');
  }
}

// Request timestamps are Node listener observations on one monotonic clock,
// not browser send times. The selected response is the first real 200 Session
// after arming the wait, including a request observed before the action.
async function loginSessionCase(browser, fixture, nativeBundle, bundle, action, rows, step) {
  need(action === 'early' || action === 'stable', 'SESSION_PROBE_LOGIN_ACTION');
  const context = await browser.newContext({ serviceWorkers: 'block' }), page = await context.newPage();
  const report = { frame: 'browser-login', consumer: 'account', action, requests: [], response_candidates: 0, selected_request_observed: false, selected_request_before_action: false, selected_request_after_action: false, native: null, events: { headers: false, finished: false, failed: false, response_finished: 'pending', pw_headers_bound: false, cdp_headers_bound: false, cdp_finished: false, cdp_failed: false, cdp_canceled: false, cdp_aborted: false }, application_ready: false, observer_joined_after_close: false, fail_code: null };
  rows.push(report);
  const events = report.events;
  const records = new Map(), cdpRecords = new Map(), base = performance.now(), observations = [];
  let cdp, selected, selectedResponse, requestID, actionAt, armed = false, capExceeded = false, responseResolve, finishedPromise;
  const responseWaiting = new Promise(resolve => { responseResolve = resolve; });
  const kindOf = request => request.url() === fixture.origin + '/api/v1/session' && request.method() === 'GET' ? 'session'
    : request.url() === fixture.origin + '/api/v1/auth/bootstrap' && request.method() === 'GET' ? 'bootstrap'
    : request.url() === fixture.origin + '/api/v1/sessions/login' && request.method() === 'POST' ? 'login' : 'other';
  const requested = request => {
    const url = new URL(request.url());
    if (url.origin !== fixture.origin || !(url.pathname === '/api/v1' || url.pathname.startsWith('/api/v1/'))) return;
    if (records.size >= 10) { capExceeded = true; return; }
    const row = { kind: kindOf(request), observed_ms: performance.now() - base, status: null, finished: false, failed: false };
    records.set(request, row); report.requests.push(row);
  };
  const responded = response => {
    const record = records.get(response.request());
    if (record) record.status = response.status();
    if (!armed || kindOf(response.request()) !== 'session' || response.status() !== 200) return;
    report.response_candidates++;
    if (selected) return;
    selected = response.request(); selectedResponse = response; events.headers = true;
    finishedPromise = response.finished();
    observations.push(finishedPromise.then(error => { events.response_finished = error === null ? 'complete' : 'error'; }, () => { events.response_finished = 'rejected'; }));
    responseResolve(response);
  };
  const finished = request => { const row = records.get(request); if (row) row.finished = true; if (request === selected) events.finished = true; };
  const failed = request => { const row = records.get(request); if (row) row.failed = true; if (request === selected) events.failed = true; };
  const sent = event => {
    if (event.request.url !== fixture.origin + '/api/v1/session' || event.request.method !== 'GET') return;
    if (cdpRecords.size >= 6) { capExceeded = true; return; }
    cdpRecords.set(event.requestId, { token: null, finished: false, failed: false, canceled: false, aborted: false });
  };
  const received = event => { const row = cdpRecords.get(event.requestId); if (row) row.token = Object.entries(event.response.headers).find(([name]) => name.toLowerCase() === 'x-request-id')?.[1] ?? null; };
  const loaded = event => { const row = cdpRecords.get(event.requestId); if (row) row.finished = true; };
  const lost = event => { const row = cdpRecords.get(event.requestId); if (row) { row.failed = true; row.canceled = event.canceled === true; row.aborted = event.errorText === 'net::ERR_ABORTED'; } };
  const publish = () => {
    const record = records.get(selected);
    report.selected_request_observed = !!record;
    report.selected_request_before_action = !!record && actionAt !== undefined && record.observed_ms < actionAt;
    report.selected_request_after_action = !!record && actionAt !== undefined && record.observed_ms >= actionAt;
    report.action_observed_ms = actionAt ?? null;
    report.request_cap_exceeded = capExceeded;
    const matches = requestID ? [...cdpRecords.values()].filter(row => row.token === requestID) : [];
    events.cdp_headers_bound = matches.length === 1;
    if (matches.length === 1) {
      const row = matches[0];
      Object.assign(events, { cdp_finished: row.finished, cdp_failed: row.failed, cdp_canceled: row.canceled, cdp_aborted: row.aborted });
    }
    if (record) Object.assign(events, { finished: record.finished, failed: record.failed });
  };
  for (const [name, listener] of [['request', requested], ['response', responded], ['requestfinished', finished], ['requestfailed', failed]]) page.on(name, listener);
  try {
    await page.addInitScript({ content: nativeBundle.toString('utf8') + '\nProjectModelsNativeProbe.install();\n' + bundle.toString('utf8') });
    cdp = await context.newCDPSession(page); await cdp.send('Network.enable');
    for (const [name, listener] of [['Network.requestWillBeSent', sent], ['Network.responseReceived', received], ['Network.loadingFinished', loaded], ['Network.loadingFailed', lost]]) cdp.on(name, listener);
    step('login-' + action + '-form');
    await page.goto(fixture.origin + '/login', { waitUntil: 'load', timeout: 5000 });
    await expect(page.locator('#login-email')).toBeVisible({ timeout: 5000 });
    await page.locator('#login-email').fill(fixture.login.email, { timeout: 5000 });
    await page.locator('#login-password').fill(fixture.login.password, { timeout: 5000 });
    await page.getByRole('button', { name: '登录', exact: true }).click({ timeout: 5000 });
    await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeEnabled({ timeout: 5000 });
    if (action === 'stable') await appHomeReady(page, fixture.origin);
    // This helper only publishes native booleans/counts; App still owns Session
    // decoding and identity checks. No response.json/clone/extra read or GET.
    need(await page.evaluate(() => window.__projectModelsProbe.sessionBegin('session-proxy', Date.now() + 250, undefined, true)) === true, 'SESSION_PROBE_NATIVE_ARM');
    armed = true;
    step('login-' + action + '-pageshow');
    actionAt = performance.now() - base;
    await bounded(page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow'))), 5000, 'SESSION_PROBE_CONSUMPTION_TIMEOUT');
    await bounded(responseWaiting, 5000, 'SESSION_PROBE_RESPONSE_TIMEOUT');
    requestID = await bounded(selectedResponse.headerValue('x-request-id'), 1000, 'SESSION_PROBE_HEADER_OBSERVATION');
    events.pw_headers_bound = typeof requestID === 'string' && requestID.length > 0;
    need(await bounded(finishedPromise, 5000, 'SESSION_PROBE_FINISH_TIMEOUT') === null, 'SESSION_PROBE_FINISH_ERROR');
    await appHomeReady(page, fixture.origin); report.application_ready = true;
    report.native = await bounded(page.evaluate(id => window.finishFixtureSession(id), requestID), 1000, 'SESSION_PROBE_NATIVE_SNAPSHOT');
    publish();
    checkFixtureNative(report.native);
    const count = (kind, status) => report.requests.filter(row => row.kind === kind && (status === undefined || row.status === status)).length;
    need(!capExceeded && count('session') >= 3 && count('session') <= 5 && count('session', 401) >= 1 && count('session', 401) <= 2 && count('session', 200) >= 2 && count('session') === count('session', 401) + count('session', 200) && count('bootstrap') === count('session', 401) && count('bootstrap', 200) === count('bootstrap') && count('login') === 1 && count('login', 200) === 1 && count('other') === 0, 'SESSION_PROBE_LOGIN_REQUESTS');
    need(report.response_candidates === 1 && report.selected_request_observed && events.pw_headers_bound && events.cdp_headers_bound && events.finished && events.cdp_finished && !events.failed && !events.cdp_failed && events.response_finished === 'complete', 'SESSION_PROBE_REQUEST_BINDING');
  } catch (error) {
    report.fail_code = /^SESSION_PROBE_[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'SESSION_PROBE_FAILED';
    throw error;
  } finally {
    if (report.native === null) { try { report.native = await bounded(page.evaluate(id => window.finishFixtureSession(id), requestID ?? null), 1000, 'SESSION_PROBE_NATIVE_SNAPSHOT'); } catch { /* preserve original failure */ } }
    publish();
    // Snapshot measurement before teardown. Join later does not rewrite it.
    report.events = { ...events };
    for (const [name, listener] of [['request', requested], ['response', responded], ['requestfinished', finished], ['requestfailed', failed]]) page.off(name, listener);
    if (cdp) for (const [name, listener] of [['Network.requestWillBeSent', sent], ['Network.responseReceived', received], ['Network.loadingFinished', loaded], ['Network.loadingFailed', lost]]) cdp.off(name, listener);
    await bounded(context.close(), 2000, 'SESSION_PROBE_CONTEXT_CLOSE');
    await bounded(Promise.all(observations), 1000, 'SESSION_PROBE_OBSERVER_JOIN');
    report.observer_joined_after_close = true;
    records.clear(); cdpRecords.clear();
  }
}

function sessionObservation(page, cdp, origin, fixture, owned) {
  let finishedObservation = Promise.resolve(), headersObservation = Promise.resolve();
  const events = { request_count: 0, headers: false, finished: false, failed: false, response_finished: 'pending', cdp_request_count: 0, cdp_headers_bound: false, cdp_finished: false, cdp_failed: false, cdp_canceled: false, cdp_aborted: false };
  let selected, cdpID, requestID, cdpRequestID, connectionID, eventResolve; const terminal = new Promise((resolve) => { eventResolve = resolve; });
  const maybeDone = () => { if ((events.finished || events.failed) && (events.cdp_finished || events.cdp_failed)) eventResolve(); };
  const requested = (request) => { if (request.url() === origin + '/api/v1/session' && request.method() === 'GET') { selected = request; events.request_count++; } };
  const responded = (response) => { if (response.request() === selected) {
    events.headers = true;
    headersObservation = response.headerValue(fixture ? 'x-request-id' : 'x-session-probe').then((token) => { if (fixture) requestID = token; events.pw_headers_bound = fixture ? !!token : token === owned.token; }, () => { events.pw_headers_bound = false; });
    finishedObservation = response.finished().then((error) => { events.response_finished = error === null ? 'complete' : 'error'; }, () => { events.response_finished = 'rejected'; });
  } };
  const finished = (request) => { if (request === selected) { events.finished = true; maybeDone(); } };
  const failed = (request) => { if (request === selected) { events.failed = true; maybeDone(); } };
  const sent = (event) => { if (event.request.url === origin + '/api/v1/session' && event.request.method === 'GET') { cdpID = event.requestId; events.cdp_request_count++; } };
  const received = (event) => { if (event.requestId === cdpID) { connectionID = Number.isSafeInteger(event.response.connectionId) && event.response.connectionId >= 0 ? event.response.connectionId : undefined; const entry = Object.entries(event.response.headers).find(([name]) => name.toLowerCase() === (fixture ? 'x-request-id' : 'x-session-probe')); if (fixture) cdpRequestID = entry?.[1]; else events.cdp_headers_bound = entry?.[1] === owned.token; } };
  const loaded = (event) => { if (event.requestId === cdpID) { events.cdp_finished = true; maybeDone(); } };
  const lost = (event) => { if (event.requestId === cdpID) { events.cdp_failed = true; events.cdp_canceled = event.canceled === true; events.cdp_aborted = event.errorText === 'net::ERR_ABORTED'; maybeDone(); } };
  for (const [name, listener] of [['request', requested], ['response', responded], ['requestfinished', finished], ['requestfailed', failed]]) page.on(name, listener);
  for (const [name, listener] of [['Network.requestWillBeSent', sent], ['Network.responseReceived', received], ['Network.loadingFinished', loaded], ['Network.loadingFailed', lost]]) cdp.on(name, listener);
  return { events, terminal, finished: () => finishedObservation, headers: () => headersObservation,
    requestID: () => requestID, connection: () => connectionID,
    bind: () => { if (fixture) events.cdp_headers_bound = !!requestID && cdpRequestID === requestID; },
    close() {
      for (const [name, listener] of [['request', requested], ['response', responded], ['requestfinished', finished], ['requestfailed', failed]]) page.off(name, listener);
      for (const [name, listener] of [['Network.requestWillBeSent', sent], ['Network.responseReceived', received], ['Network.loadingFinished', loaded], ['Network.loadingFailed', lost]]) cdp.off(name, listener);
    },
  };
}

async function worker(directory, mode, fixture = null) {
  let stage = 'preflight', browserServer, browser, server, browserExit, socketCount = 0, current;
  const sockets = new Set(), rows = [], resources = { server_closed: false, browser_closed: false, browser_server_closed: false, browser_actual_wait: false, browser_exit_code: null, sockets_empty: false };
  let failCode = null, requestedStop;
  const stopped = new Promise((_, reject) => { requestedStop = () => reject(new Error('SESSION_PROBE_STOPPED')); });
  process.on('SIGTERM', requestedStop); process.on('SIGINT', requestedStop);
  try {
    const prepared = JSON.parse(await readFile(join(output, 'prepared.json'), 'utf8'));
    need(prepared.mode === mode && prepared.cases === casesFor(mode).length, 'SESSION_PROBE_PREPARED_MODE');
    if (!fixture) {
      const marker = JSON.parse(await readFile(join(output, 'active.json'), 'utf8'));
      need(marker.directory === directory && marker.nonce === process.env.SESSION_PROBE_SUPERVISED && marker.supervisor_pid === process.ppid, 'SESSION_PROBE_SUPERVISOR_REQUIRED');
    }
    need(JSON.stringify(prepared.inputs) === JSON.stringify(await inputs(mode)), 'SESSION_PROBE_INPUT_CHANGED');
    const bundle = await readFile(join(output, 'client.js')); need(hash(bundle) === prepared.bundle_sha256);
    const nativeBundle = mode !== 'default' ? await readFile(nativeBundlePath) : null;
    const run = async () => {
      stage = 'server';
      let origin = fixture?.origin;
      if (!fixture) {
      server = createServer((request, response) => {
        if (request.method === 'GET' && request.url === '/') { response.writeHead(200, { 'Content-Type': 'text/html' }); response.end('<!doctype html><title>Owned Session probe</title>'); return; }
        if (request.method === 'GET' && request.url === '/favicon.ico') { response.writeHead(204); response.end(); return; }
        if (!current || request.method !== 'GET' || request.url !== '/api/v1/session') { response.writeHead(400); response.end(); return; }
        const row = current; row.requests++;
        if (row.requests !== 1) { response.writeHead(409); response.end(); return; }
        const body = row.frame === 'truncated-json' ? row.body.subarray(0, row.body.length - 1) : row.body;
        response.writeHead(200, { 'Content-Type': 'application/json', 'X-Request-ID': row.value.session.id, 'X-Session-Probe': row.token,
          ...(row.frame === 'chunked' ? { 'Transfer-Encoding': 'chunked' } : { 'Content-Length': row.body.length - (row.frame === 'truncated-json' ? 1 : 0) }) });
        response.on('finish', () => { row.server_finished = true; }); response.on('close', () => { row.server_closed = true; });
        if (row.frame === 'disconnect') { row.response = response; response.flushHeaders(); response.write(body.subarray(0, Math.floor(body.length / 2))); }
        else response.end(body);
      });
      server.on('connection', (socket) => { socketCount++; sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
      await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
      const address = server.address(); need(address && typeof address === 'object'); origin = `http://127.0.0.1:${address.port}`;
      await json(join(directory, 'http-owned.json'), { pid: process.pid, port: address.port });
      }
      stage = 'browser';
      browserServer = await chromium.launchServer({ executablePath: '/usr/bin/chromium', headless: true, host: '127.0.0.1', port: 0, args: ['--no-sandbox', '--disable-background-networking', '--disable-component-update', '--disable-sync'], timeout: 8000 });
      const child = browserServer.process(); resources.browser_pid = child.pid;
      browserExit = new Promise((resolve) => { child.once('exit', (code, signal) => { resources.browser_actual_wait = true; resources.browser_exit_code = code; resources.browser_signal = signal; resolve(); }); });
      await json(join(directory, 'browser-owned.json'), { ...await processIdentity(child.pid), port: Number(new URL(browserServer.wsEndpoint()).port) });
      browser = await chromium.connect(browserServer.wsEndpoint(), { timeout: 5000 });
      need(browser.version() === '151.0.7922.173', 'SESSION_PROBE_BROWSER_VERSION');
      if (mode === 'owned-login') {
        for (const { action } of casesFor(mode)) {
          try { await loginSessionCase(browser, fixture, nativeBundle, bundle, action, rows, next => { stage = next; }); }
          catch (error) { failCode ??= /^SESSION_PROBE_[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'SESSION_PROBE_FAILED'; }
        }
        return;
      }
      if (mode === 'owned-app') {
        for (const { action } of casesFor(mode)) {
          stage = 'app-' + action;
          await appSessionCase(browser, fixture, nativeBundle, bundle, action, rows, next => { stage = next; });
        }
        return;
      }
      let boundaryValue;
      for (const { frame, consumer, promise_mode } of casesFor(mode)) {
        stage = `${frame}-${consumer}${promise_mode ? '-' + promise_mode : ''}`;
        const value = fixture?.expected ?? boundaryValue ?? { user: { id: '01900000-0000-7000-8000-000000000001', email: 'probe@example.com', username: 'probe', display_name: randomBytes(12).toString('hex'), role: 'user', theme: 'system', version: '1', initial_password_suggestion: false },
          session: { id: '01900000-0000-7000-8000-000000000002', issued_at: '2026-10-05T12:34:56.123456Z', absolute_expires_at: '2026-10-05T12:34:56.123456Z', idle_expires_at: '2026-10-05T12:34:56.123456Z' }, csrf_token: randomBytes(32).toString('base64url') };
        if (mode === 'promise-boundary') boundaryValue = value;
        current = { frame, value, token: `r${rows.length + 1}`, body: fixture ? Buffer.alloc(0) : Buffer.from(JSON.stringify(value)), requests: 0, server_finished: false, server_closed: false, cut_applied: false };
        const owned = current, context = await browser.newContext({ serviceWorkers: 'block' }), page = await context.newPage();
        let observation;
        const report = { frame, consumer, ...(promise_mode ? { promise_mode } : {}), facts: null, events: null, server: null, observer_joined_after_close: false };
        rows.push(report);
        try {
          if (fixture) await context.addCookies(fixture.cookies.map(cookie => ({ ...cookie, url: origin })));
          const landing = await page.goto(origin + (fixture ? '/__session-proxy-diagnostic__.txt' : ''), { waitUntil: 'load', timeout: 5000 });
          if (fixture) need(landing?.status() === 404, 'SESSION_PROBE_EMPTY_DOCUMENT');
          if (nativeBundle) await page.addScriptTag({ content: nativeBundle.toString('utf8') + '\nProjectModelsNativeProbe.install();' });
          await page.addScriptTag({ content: bundle.toString('utf8') });
          if (frame === 'disconnect') await page.exposeFunction('sessionProbeReadReady', () => {
            need(current === owned && owned.response && !owned.cut_applied, 'SESSION_PROBE_CUT_OWNERSHIP');
            owned.cut_applied = true; owned.response.destroy();
          });
          const cdp = await context.newCDPSession(page); await cdp.send('Network.enable');
          observation = sessionObservation(page, cdp, origin, fixture, owned);
          const { events, terminal } = observation;
          if (fixture) need(await page.evaluate(() => window.__projectModelsProbe.sessionBegin('session-proxy', Date.now() + 250, undefined, true)) === true, 'SESSION_PROBE_NATIVE_ARM');
          const facts = await bounded(page.evaluate(({ consumer, value, disconnect, promise_mode, fixture }) => fixture ? window.runFixtureSession(value) : promise_mode ? window.runSessionPromiseBoundary(promise_mode, value) : window.runSessionConsumption(consumer, value, disconnect), { consumer, value, disconnect: frame === 'disconnect', promise_mode, fixture: !!fixture }), 5000, 'SESSION_PROBE_CONSUMPTION_TIMEOUT');
          await bounded(terminal, 2000, 'SESSION_PROBE_EVENTS_TIMEOUT');
          await bounded(observation.headers(), 1000, 'SESSION_PROBE_HEADER_OBSERVATION');
          if (fixture) {
            observation.bind();
            const native = await bounded(page.evaluate(requestID => window.finishFixtureSession(requestID), observation.requestID()), 1000, 'SESSION_PROBE_NATIVE_SNAPSHOT');
            Object.assign(facts, { native, readers: native.readers, bytes: native.bytes, read_done: native.read_done, read_failed: native.read_rejected !== 0, released: native.release_successes === 1, reader_cancel_calls: native.reader_cancel_calls, reader_cancel_settled: native.reader_cancel_settled, stream_cancel_calls: native.stream_cancel_calls, stream_cancel_settled: native.stream_cancel_settled });
          }
          // An observation window, never a replacement EOF criterion or retry.
          await bounded(new Promise((resolve) => { if (events.response_finished !== 'pending') resolve(); else setTimeout(resolve, 100); }), 250, 'SESSION_PROBE_OBSERVATION_TIMEOUT');
          Object.assign(report, { facts, events: { ...events }, server: fixture ? { owner: 'go-fixture' } : { requests: owned.requests, finished: owned.server_finished, closed: owned.server_closed, cut_applied: owned.cut_applied } });
          need((fixture || owned.requests === 1) && events.request_count === 1 && events.cdp_request_count === 1 && events.pw_headers_bound && events.cdp_headers_bound, 'SESSION_PROBE_REQUEST_BINDING');
          need(facts.fetches === 1 && facts.readers === 1 && facts.released, 'SESSION_PROBE_READER_OWNERSHIP');
          const complete = fixture || ['length', 'chunked'].includes(frame);
          need(complete ? facts.outcome === 'success' && facts.read_done && facts.decoded && facts.identity_equal && (fixture || facts.bytes === owned.body.length) : facts.outcome !== 'success' && !facts.decoded, 'SESSION_PROBE_CONTROL_RESULT');
          if (frame === 'truncated-json') need(facts.read_done && facts.bytes === owned.body.length - 1, 'SESSION_PROBE_JSON_NEGATIVE');
          if (frame === 'disconnect') need(owned.cut_applied && !facts.read_done && facts.read_failed && facts.bytes < owned.body.length && events.cdp_failed, 'SESSION_PROBE_DISCONNECT_NEGATIVE');
          if (consumer === 'native') need(facts.reader_cancel_calls === 0 && facts.stream_cancel_calls === 0, 'SESSION_PROBE_NATIVE_CANCEL');
          else need(facts.reader_cancel_calls === 1 && facts.reader_cancel_settled === 1 && facts.stream_cancel_calls === 1 && facts.stream_cancel_settled === 1, 'SESSION_PROBE_CLIENT_CANCEL');
          if (promise_mode) need(facts.arm_success && facts.returned_original_promise === (promise_mode === 'original') && facts.native.requests === 1 && facts.native.request_id_match && facts.native.eof_before_interruption && facts.native.content_length_comparable && facts.native.content_length_matches_eof && facts.native.content_length === owned.body.length && facts.native.read_rejected === 0 && facts.native.abort_events === 0 && facts.native.reader_cancel_rejected === 0 && facts.native.stream_cancel_rejected === 0 && facts.native.release_calls === 1 && facts.native.release_successes === 1, 'SESSION_PROBE_NATIVE_BOUNDARY');
          if (fixture) need(facts.native.requests === 1 && facts.native.request_id_match && facts.native.eof_before_interruption && facts.native.content_length_comparable && facts.native.content_length_matches_eof && facts.native.read_rejected === 0 && facts.native.abort_events === 0 && facts.native.reader_cancel_rejected === 0 && facts.native.stream_cancel_rejected === 0 && facts.native.release_calls === 1 && facts.native.release_successes === 1, 'SESSION_PROBE_NATIVE_BOUNDARY');
          await cdp.detach();
        } finally {
          try {
            await bounded(context.close(), 2000, 'SESSION_PROBE_CONTEXT_CLOSE');
            await bounded(observation?.finished() ?? Promise.resolve(), 1000, 'SESSION_PROBE_OBSERVER_JOIN');
            report.observer_joined_after_close = true;
          } finally { observation?.close(); owned.body.fill(0); current = null; }
        }
      }
    };
    await Promise.race([run(), stopped]);
    need(rows.length === casesFor(mode).length, 'SESSION_PROBE_CASE_COUNT');
    need(JSON.stringify(prepared.inputs) === JSON.stringify(await inputs(mode)), 'SESSION_PROBE_INPUT_CHANGED');
  } catch (error) { failCode = /^SESSION_PROBE_[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'SESSION_PROBE_FAILED'; }
  finally {
    process.off('SIGTERM', requestedStop); process.off('SIGINT', requestedStop);
    if (browser) { try { await bounded(browser.close(), 3000, 'SESSION_PROBE_BROWSER_CLOSE'); resources.browser_closed = true; } catch { failCode ??= 'SESSION_PROBE_BROWSER_CLOSE'; } }
    if (browserServer) { try { await bounded(browserServer.close(), 3000, 'SESSION_PROBE_SERVER_CLOSE'); resources.browser_server_closed = true; await bounded(browserExit, 1000, 'SESSION_PROBE_BROWSER_WAIT'); } catch { failCode ??= 'SESSION_PROBE_BROWSER_WAIT'; } }
    if (server) { try { server.closeAllConnections(); await bounded(new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve())), 1000, 'SESSION_PROBE_HTTP_CLOSE'); resources.server_closed = true; } catch { failCode ??= 'SESSION_PROBE_HTTP_CLOSE'; } }
    resources.sockets_empty = sockets.size === 0; resources.connections = socketCount;
    const retired = (fixture || resources.server_closed) && resources.browser_closed && resources.browser_server_closed && resources.browser_actual_wait && resources.sockets_empty;
    if (resources.browser_actual_wait && (resources.browser_exit_code !== 0 || resources.browser_signal !== null)) failCode ??= 'SESSION_PROBE_BROWSER_EXIT';
    await json(join(directory, 'result.json'), { mode, stage, fail_code: failCode, rows, resources, retirement_complete: retired });
    process.exitCode = failCode || !retired ? 1 : 0;
  }
}

async function ownedFixture(directory, mode) {
  const privatePath = process.env.AGENTEAM_SESSION_PROXY_PRIVATE;
  need(typeof privatePath === 'string' && resolve(privatePath) === privatePath && directory === process.env.AGENTEAM_SESSION_PROXY_EVIDENCE, 'SESSION_PROBE_FIXTURE_PRIVATE');
  const info = await lstat(privatePath);
  need(info.isFile() && !info.isSymbolicLink() && info.uid === process.getuid() && (info.mode & 0o777) === 0o600 && info.size <= 8192, 'SESSION_PROBE_FIXTURE_PRIVATE');
  const raw = await readFile(privatePath);
  let fixture;
  try { fixture = JSON.parse(raw.toString('utf8')); } finally { raw.fill(0); }
  const origin = new URL(fixture.origin);
  need(fixture.mode === mode && fixture.protocol === 'project-session-proxy.v1' && fixture.supervisor_pid === process.ppid && fixture.input_hash === process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH && /^[0-9a-f]{64}$/.test(fixture.input_hash) && origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && !!origin.port && origin.origin === fixture.origin, 'SESSION_PROBE_FIXTURE_BINDING');
  if (mode === 'owned-login') {
    need(!Object.hasOwn(fixture, 'cookies') && !Object.hasOwn(fixture, 'expected') && fixture.login && Object.keys(fixture.login).sort().join() === 'email,password,user_id' && Object.values(fixture.login).every(value => typeof value === 'string' && value.length > 0 && value.length <= 1024), 'SESSION_PROBE_FIXTURE_LOGIN');
  } else {
  need(Array.isArray(fixture.cookies) && fixture.cookies.length > 0 && fixture.cookies.length <= 4 && fixture.cookies.every(cookie => Object.keys(cookie).length === 2 && typeof cookie.name === 'string' && typeof cookie.value === 'string'), 'SESSION_PROBE_FIXTURE_COOKIES');
  need(Object.keys(fixture.expected).sort().join() === 'csrf,session_id,user_id' && Object.values(fixture.expected).every(value => typeof value === 'string' && value.length > 0), 'SESSION_PROBE_FIXTURE_IDENTITY');
  }
  need(hash(await readFile(join(output, 'prepared.json'))) === fixture.prepared_hash && hash(await readFile(join(output, 'client.js'))) === fixture.client_hash, 'SESSION_PROBE_FIXTURE_INPUTS');
  try { await worker(directory, mode, fixture); }
  finally { if (mode === 'owned-login') { fixture.login.email = ''; fixture.login.password = ''; } else { for (const cookie of fixture.cookies) cookie.value = ''; fixture.expected.csrf = ''; } }
}

try {
  process.umask(0o077);
  const { args, mode } = argumentsFor(process.argv.slice(2));
  if (args.length === 1 && args[0] === '--prepare') await prepare(mode);
  else if (args.length === 2 && args[0] === '--worker' && !['owned-fixture', 'owned-app', 'owned-login'].includes(mode) && /^run-[0-9a-f]{16}$/.test(args[1].slice(output.length + 1)) && dirname(args[1]) === output) await worker(args[1], mode);
  else if (args.length === 2 && args[0] === '--owned-fixture' && ['owned-fixture', 'owned-app', 'owned-login'].includes(mode)) await ownedFixture(args[1], mode);
  else throw new Error('SESSION_PROBE_ARGUMENTS');
} catch (error) {
  // Preparation has no Session or running browser; compiler diagnostics are
  // safe here. Runtime failures never expose an underlying exception.
  if (process.argv[2] === '--prepare') console.error(error);
  else console.error('SESSION_PROBE_FAILED');
  process.exitCode = 1;
}
