#!/usr/bin/env node
// Independent investigation only. --prepare is offline; --run needs the
// separately assigned loopback/browser window. No production or D27 gate edits.
import { createHash, randomBytes } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lstat, mkdir, readFile, writeFile, unlink } from 'node:fs/promises';
import { build } from '../../web/node_modules/vite/dist/node/index.js';

const source = fileURLToPath(import.meta.url), root = resolve(dirname(source), '../..');
const output = join(root, 'output/ai/model-ui-session-probe');
const require = createRequire(join(root, 'tests/account-captcha-web/package.json'));
const { chromium } = require('playwright');
const packageVersion = require('@playwright/test/package.json').version;
const need = (value, code = 'SESSION_PROBE_INVARIANT') => { if (!value) throw new Error(code); };
const json = async (path, value) => writeFile(path, JSON.stringify(value, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
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

async function inputs() {
  need(packageVersion === '1.56.1', 'SESSION_PROBE_PLAYWRIGHT_VERSION');
  const files = [source, join(root, 'web/src/api/account.ts'), join(root, 'web/src/api/client.ts'), join(root, 'web/package-lock.json'), join(root, 'tests/account-captcha-web/package-lock.json')];
  return Object.fromEntries(await Promise.all(files.map(async (path) => [path, hash(await readFile(path))])));
}
async function prepare() {
  await mkdir(output, { recursive: true, mode: 0o700 });
  need((await lstat(output)).isDirectory() && !(await lstat(output)).isSymbolicLink());
  const before = await inputs();
  const virtual = '\0independent-session-consumption', entry = join(output, 'virtual-session-entry.js');
  const result = await build({ configFile: false, root, logLevel: 'silent', plugins: [{
    name: 'independent-session-entry', enforce: 'pre', resolveId: (id) => id === entry ? virtual : undefined,
    load: (id) => id === virtual ? `import { createAccountAPI } from ${JSON.stringify(join(root, 'web/src/api/account.ts'))};\n(${installBrowser.toString()})(createAccountAPI);` : undefined,
  }], build: { write: false, minify: false, sourcemap: false, lib: { entry, name: 'SessionConsumption', formats: ['iife'] } } });
  const chunks = (Array.isArray(result) ? result : [result]).flatMap((row) => row.output);
  need(chunks.length === 1 && chunks[0].type === 'chunk');
  const bundle = Buffer.from(chunks[0].code);
  need(JSON.stringify(before) === JSON.stringify(await inputs()), 'SESSION_PROBE_INPUT_CHANGED');
  await writeFile(join(output, 'client.js'), bundle, { mode: 0o600 });
  await writeFile(join(output, 'prepared.json'), JSON.stringify({ inputs: before, bundle_sha256: hash(bundle), playwright: packageVersion, browser_version: '151.0.7922.173', cases: 8, network_started: false }), { mode: 0o600 });
  console.log(JSON.stringify({ prepared: true, cases: 8, network_started: false }));
}

async function worker(directory) {
  let stage = 'preflight', browserServer, browser, server, browserExit, socketCount = 0, current;
  const sockets = new Set(), rows = [], resources = { server_closed: false, browser_closed: false, browser_actual_wait: false, browser_exit_code: null, sockets_empty: false };
  let failCode = null, requestedStop;
  const stopped = new Promise((_, reject) => { requestedStop = () => reject(new Error('SESSION_PROBE_STOPPED')); });
  process.on('SIGTERM', requestedStop); process.on('SIGINT', requestedStop);
  try {
    const prepared = JSON.parse(await readFile(join(output, 'prepared.json'), 'utf8'));
    need(JSON.stringify(prepared.inputs) === JSON.stringify(await inputs()), 'SESSION_PROBE_INPUT_CHANGED');
    const bundle = await readFile(join(output, 'client.js')); need(hash(bundle) === prepared.bundle_sha256);
    const run = async () => {
      stage = 'server';
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
      const address = server.address(); need(address && typeof address === 'object'); const origin = `http://127.0.0.1:${address.port}`;
      stage = 'browser';
      browserServer = await chromium.launchServer({ executablePath: '/usr/bin/chromium', headless: true, host: '127.0.0.1', port: 0, args: ['--no-sandbox', '--disable-background-networking', '--disable-component-update', '--disable-sync'], timeout: 8000 });
      const child = browserServer.process(); resources.browser_pid = child.pid;
      browserExit = new Promise((resolve) => { child.once('exit', (code, signal) => { resources.browser_actual_wait = true; resources.browser_exit_code = code; resources.browser_signal = signal; resolve(); }); });
      await json(join(directory, 'browser-owned.json'), await processIdentity(child.pid));
      browser = await chromium.connect(browserServer.wsEndpoint(), { timeout: 5000 });
      need(browser.version() === '151.0.7922.173', 'SESSION_PROBE_BROWSER_VERSION');
      for (const frame of ['length', 'chunked', 'truncated-json', 'disconnect']) for (const consumer of ['native', 'account']) {
        stage = `${frame}-${consumer}`;
        const value = { user: { id: '01900000-0000-7000-8000-000000000001', email: 'probe@example.com', username: 'probe', display_name: randomBytes(12).toString('hex'), role: 'user', theme: 'system', version: '1', initial_password_suggestion: false },
          session: { id: '01900000-0000-7000-8000-000000000002', issued_at: '2026-10-05T12:34:56.123456Z', absolute_expires_at: '2026-10-05T12:34:56.123456Z', idle_expires_at: '2026-10-05T12:34:56.123456Z' }, csrf_token: randomBytes(32).toString('base64url') };
        current = { frame, value, token: `r${rows.length + 1}`, body: Buffer.from(JSON.stringify(value)), requests: 0, server_finished: false, server_closed: false, cut_applied: false };
        const owned = current, context = await browser.newContext({ serviceWorkers: 'block' }), page = await context.newPage();
        let finishedObservation = Promise.resolve(), headersObservation = Promise.resolve();
        const report = { frame, consumer, facts: null, events: null, server: null, observer_joined_after_close: false };
        rows.push(report);
        try {
          await page.goto(origin, { waitUntil: 'load', timeout: 5000 }); await page.addScriptTag({ content: bundle.toString('utf8') });
          if (frame === 'disconnect') await page.exposeFunction('sessionProbeReadReady', () => {
            need(current === owned && owned.response && !owned.cut_applied, 'SESSION_PROBE_CUT_OWNERSHIP');
            owned.cut_applied = true; owned.response.destroy();
          });
          const cdp = await context.newCDPSession(page); await cdp.send('Network.enable');
          const events = { request_count: 0, headers: false, finished: false, failed: false, response_finished: 'pending', cdp_request_count: 0, cdp_headers_bound: false, cdp_finished: false, cdp_failed: false, cdp_canceled: false, cdp_aborted: false };
          let selected, cdpID, eventResolve; const terminal = new Promise((resolve) => { eventResolve = resolve; });
          const maybeDone = () => { if ((events.finished || events.failed) && (events.cdp_finished || events.cdp_failed)) eventResolve(); };
          page.on('request', (request) => { if (request.url() === origin + '/api/v1/session' && request.method() === 'GET') { selected = request; events.request_count++; } });
          page.on('response', (response) => { if (response.request() === selected) {
            events.headers = true;
            headersObservation = response.headerValue('x-session-probe').then((token) => { events.pw_headers_bound = token === owned.token; }, () => { events.pw_headers_bound = false; });
            finishedObservation = response.finished().then((error) => { events.response_finished = error === null ? 'complete' : 'error'; }, () => { events.response_finished = 'rejected'; });
          } });
          page.on('requestfinished', (request) => { if (request === selected) { events.finished = true; maybeDone(); } });
          page.on('requestfailed', (request) => { if (request === selected) { events.failed = true; maybeDone(); } });
          cdp.on('Network.requestWillBeSent', (event) => { if (event.request.url === origin + '/api/v1/session' && event.request.method === 'GET') { cdpID = event.requestId; events.cdp_request_count++; } });
          cdp.on('Network.responseReceived', (event) => { if (event.requestId === cdpID) { const entry = Object.entries(event.response.headers).find(([name]) => name.toLowerCase() === 'x-session-probe'); events.cdp_headers_bound = entry?.[1] === owned.token; } });
          cdp.on('Network.loadingFinished', (event) => { if (event.requestId === cdpID) { events.cdp_finished = true; maybeDone(); } });
          cdp.on('Network.loadingFailed', (event) => { if (event.requestId === cdpID) { events.cdp_failed = true; events.cdp_canceled = event.canceled === true; events.cdp_aborted = event.errorText === 'net::ERR_ABORTED'; maybeDone(); } });
          const facts = await bounded(page.evaluate(({ consumer, value, disconnect }) => window.runSessionConsumption(consumer, value, disconnect), { consumer, value, disconnect: frame === 'disconnect' }), 5000, 'SESSION_PROBE_CONSUMPTION_TIMEOUT');
          await bounded(terminal, 2000, 'SESSION_PROBE_EVENTS_TIMEOUT');
          await bounded(headersObservation, 1000, 'SESSION_PROBE_HEADER_OBSERVATION');
          // An observation window, never a replacement EOF criterion or retry.
          await bounded(new Promise((resolve) => { if (events.response_finished !== 'pending') resolve(); else setTimeout(resolve, 100); }), 250, 'SESSION_PROBE_OBSERVATION_TIMEOUT');
          Object.assign(report, { facts, events: { ...events }, server: { requests: owned.requests, finished: owned.server_finished, closed: owned.server_closed, cut_applied: owned.cut_applied } });
          need(owned.requests === 1 && events.request_count === 1 && events.cdp_request_count === 1 && events.pw_headers_bound && events.cdp_headers_bound, 'SESSION_PROBE_REQUEST_BINDING');
          need(facts.fetches === 1 && facts.readers === 1 && facts.released, 'SESSION_PROBE_READER_OWNERSHIP');
          const complete = ['length', 'chunked'].includes(frame);
          need(complete ? facts.outcome === 'success' && facts.read_done && facts.decoded && facts.identity_equal && facts.bytes === owned.body.length : facts.outcome !== 'success' && !facts.decoded, 'SESSION_PROBE_CONTROL_RESULT');
          if (frame === 'truncated-json') need(facts.read_done && facts.bytes === owned.body.length - 1, 'SESSION_PROBE_JSON_NEGATIVE');
          if (frame === 'disconnect') need(owned.cut_applied && !facts.read_done && facts.read_failed && facts.bytes < owned.body.length && events.cdp_failed, 'SESSION_PROBE_DISCONNECT_NEGATIVE');
          if (consumer === 'native') need(facts.reader_cancel_calls === 0 && facts.stream_cancel_calls === 0, 'SESSION_PROBE_NATIVE_CANCEL');
          else need(facts.reader_cancel_calls === 1 && facts.reader_cancel_settled === 1 && facts.stream_cancel_calls === 1 && facts.stream_cancel_settled === 1, 'SESSION_PROBE_CLIENT_CANCEL');
          await cdp.detach();
        } finally {
          try {
            await bounded(context.close(), 2000, 'SESSION_PROBE_CONTEXT_CLOSE');
            await bounded(finishedObservation, 1000, 'SESSION_PROBE_OBSERVER_JOIN');
            report.observer_joined_after_close = true;
          } finally { owned.body.fill(0); current = null; }
        }
      }
    };
    await Promise.race([run(), stopped]);
    need(rows.length === 8, 'SESSION_PROBE_CASE_COUNT');
    need(JSON.stringify(prepared.inputs) === JSON.stringify(await inputs()), 'SESSION_PROBE_INPUT_CHANGED');
  } catch (error) { failCode = /^SESSION_PROBE_[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'SESSION_PROBE_FAILED'; }
  finally {
    process.off('SIGTERM', requestedStop); process.off('SIGINT', requestedStop);
    if (browser) { try { await bounded(browser.close(), 3000, 'SESSION_PROBE_BROWSER_CLOSE'); resources.browser_closed = true; } catch { failCode ??= 'SESSION_PROBE_BROWSER_CLOSE'; } }
    if (browserServer) { try { await bounded(browserServer.close(), 3000, 'SESSION_PROBE_SERVER_CLOSE'); await bounded(browserExit, 1000, 'SESSION_PROBE_BROWSER_WAIT'); } catch { failCode ??= 'SESSION_PROBE_BROWSER_WAIT'; } }
    if (server) { try { server.closeAllConnections(); await bounded(new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve())), 1000, 'SESSION_PROBE_HTTP_CLOSE'); resources.server_closed = true; } catch { failCode ??= 'SESSION_PROBE_HTTP_CLOSE'; } }
    resources.sockets_empty = sockets.size === 0; resources.connections = socketCount;
    const retired = resources.server_closed && resources.browser_closed && resources.browser_actual_wait && resources.sockets_empty;
    if (resources.browser_actual_wait && (resources.browser_exit_code !== 0 || resources.browser_signal !== null)) failCode ??= 'SESSION_PROBE_BROWSER_EXIT';
    await json(join(directory, 'result.json'), { stage, fail_code: failCode, rows, resources, retirement_complete: retired });
    process.exitCode = failCode || !retired ? 1 : 0;
  }
}

async function run() {
  const directory = join(output, 'run-' + randomBytes(8).toString('hex'));
  await mkdir(directory, { mode: 0o700 });
  const active = join(output, 'active.json'); await json(active, { directory });
  const child = spawn(process.execPath, [source, '--worker', directory], { cwd: root, detached: true, stdio: 'ignore', env: { ...process.env, DEBUG: '', PWDEBUG: '' } });
  let timedOut = false;
  const signal = (pid, name) => { try { process.kill(pid, name); } catch (error) { if (error.code !== 'ESRCH') throw error; } };
  const graceful = () => { timedOut = true; if (child.pid) signal(-child.pid, 'SIGTERM'); };
  const force = async () => {
    timedOut = true;
    try {
      const owned = JSON.parse(await readFile(join(directory, 'browser-owned.json'), 'utf8'));
      const actual = await processIdentity(owned.pid);
      if (Number.isSafeInteger(owned.pid) && owned.pid > 1 && actual.group === owned.group && actual.start === owned.start)
        signal(actual.group === actual.pid ? -actual.pid : actual.pid, 'SIGKILL');
    } catch { /* Failure remains unretired; never infer a safe broad kill. */ }
    if (child.pid) signal(-child.pid, 'SIGKILL');
  };
  process.on('SIGTERM', graceful); process.on('SIGINT', graceful);
  const stop = setTimeout(graceful, 35_000);
  const kill = setTimeout(() => { void force(); }, 45_000);
  let terminal;
  try { terminal = await new Promise((resolve) => { child.once('exit', (code, signal) => resolve({ actual_wait: true, code, signal })); child.once('error', () => resolve({ actual_wait: false, code: null, signal: null })); }); }
  finally { clearTimeout(stop); clearTimeout(kill); process.off('SIGTERM', graceful); process.off('SIGINT', graceful); }
  let result; try { result = JSON.parse(await readFile(join(directory, 'result.json'), 'utf8')); } catch { result = null; }
  const passed = !timedOut && terminal.actual_wait && terminal.code === 0 && result?.retirement_complete === true && result.rows?.length === 8 && result.fail_code === null;
  await json(join(directory, 'terminal.json'), { ...terminal, worker_pid: child.pid, timed_out: timedOut, passed });
  if (result?.retirement_complete === true) await unlink(active);
  console.log(JSON.stringify({ directory, passed, actual_wait: terminal.actual_wait, code: terminal.code }));
  process.exitCode = passed ? 0 : 1;
}

try {
  process.umask(0o077);
  if (process.argv.length === 3 && process.argv[2] === '--prepare') await prepare();
  else if (process.argv.length === 3 && process.argv[2] === '--run') await run();
  else if (process.argv.length === 4 && process.argv[2] === '--worker' && /^run-[0-9a-f]{16}$/.test(process.argv[3].slice(output.length + 1)) && dirname(process.argv[3]) === output) await worker(process.argv[3]);
  else throw new Error('SESSION_PROBE_ARGUMENTS');
} catch (error) {
  // Preparation has no Session or running browser; compiler diagnostics are
  // safe here. Runtime failures never expose an underlying exception.
  if (process.argv[2] === '--prepare') console.error(error);
  else console.error('SESSION_PROBE_FAILED');
  process.exitCode = 1;
}
