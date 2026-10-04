# Account captcha integration harness

This test-only Vue application is not the D26 account UI. The Go test creates an owned database, an account Service, and a loopback HTTP adapter. The browser renders the official rotate component, solves only its public pixels, verifies the answer, and consumes the proof in a real login transaction. There is no private answer endpoint.

Dependencies are pinned in [package-lock.json](package-lock.json): GoCaptcha Vue 2.0.7 and Vue 3.5.43 (MIT), Playwright 1.56.1 (Apache-2.0), Vite 8.3.1, TypeScript 5.9.3 and jsdom 27.4.0. jsdom does not replace the browser test. Server artwork is generated locally; no external captcha assets or fonts are downloaded.

The paired Playwright Chromium 141 build 1194 download returned HTTP 403. The approved test combination uses `/usr/bin/chromium`, Chromium **151.0.7922.173**, with `/usr/lib/chromium/chromium` SHA256 `d387400aaf740ccb75e5e996a34aa0940e6e97683a4eabd6ae34c1eed6804723`. It is not the browser shipped with Playwright 1.56.1. Do not silently replace it or retry blocked downloads. This isolated container requires `--no-sandbox`; it does not browse external sites or existing infrastructure.

```sh
npm ci --prefix tests/account-captcha-web
npm run build --prefix tests/account-captcha-web
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio GOFLAGS=-p=1 \
  sh scripts/test-objects.sh -run '^TestAccountCaptchaOfficialVueActualBrowser$'
```

Run from the repository root. The Go fixture supplies `ACCOUNT_CAPTCHA_BASE_URL`, an exact `ACCOUNT_CAPTCHA_CASE` (`desktop` or `keyboard`), and a newly generated password only to its child; standalone `npm test` without that fixture must fail. Each case gets its own account database and HTTP server, so a failed case cannot change another case's failure counter. Both cases share the original two-minute Go budget; each Playwright invocation selects exactly one case with a 45-second timeout and no retries. Add `/desktop$` or `/keyboard$` to the Go `-run` expression to run either case alone.

The child uses an owned short `/tmp/acct-web-*` directory for Chromium's Unix socket limit, removed after exit. Traces, screenshots and videos are disabled. Dependencies, build output and test output are ignored. The pointer test measures the component's actual integer travel, chooses the nearest physically reachable integer angle, and checks that the actual POST matches it exactly and is within one degree of the public solution. It uses real mouse events, including mouseup; the server's original five-degree tolerance is unchanged.

The public-image solvers sample the opaque disc across its full area and account for the pinned GoCaptcha crop, rotation canvas and pixel centers. They search all 360 integer angles, check available texture, and never read the server's answer. Production generation fixes the master at 220 pixels and the thumb at 160 pixels; the solver also accepts the historical 140–170 pixel public regression images. Deterministic geometry and Go/JavaScript parity can be checked without a browser or database:

```sh
GOTOOLCHAIN=local ACCOUNT_SOLVER_CORPUS_OUT=/tmp/account-known-geometry.json \
  /path/to/go1.27.1/bin/go test -count=1 -run '^TestPublicRotation' ./tests/account
node tests/account-captcha-web/e2e/public-solver-check.mjs /tmp/account-known-geometry.json
```

The corpus contains only test-owned known rotations and public image pixels. The [regression image notes](../account/testdata/challenge/README.md) distinguish these known inputs from the original public counterexamples, whose private answers were not retained. No random resampling or relaxed verification threshold is used to select successful puzzles.

The flows cover desktop pointer rotation and a narrow dark/reduced-motion keyboard interaction. A labelled range control and focus restoration use the same verification. The challenge remains visual; it does not claim complete accessibility for people unable to perceive it. Production HTTP/session routing and full account pages are outside this harness; see the [account library guide](../../docs/development/backend/account.md).
