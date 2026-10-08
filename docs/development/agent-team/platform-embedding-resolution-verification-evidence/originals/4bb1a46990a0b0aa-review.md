# Platform embedding six PG sources: offline compilation

STOP. The six original #3–8 candidates from freeze `a442fcb8c14075b56511f012d97c9ecd3efb9a9d1f6c2e45b2e03599c11da22e` were installed only after all six target paths were confirmed absent. Their 76,506 source bytes and individual SHA-256 values remain unchanged; exact final values are in `stop.json`. The accepted policy `c8ccd095` and pure test `dd1e60b7`, rev3 card `f6535ef6`, existing helpers and lockfiles were not modified.

With `/workspace/toolchains/go1.27.1/bin/go`, local toolchain, `GOWORK=off`, `GOPROXY=off`, `GOSUMDB=off`, read-only modules and `-p=1`, both actual commands passed on their first attempt:

- `go test -race -c -tags=integration -vet=off -work -x -o <private output> ./tests/model`: exit 0 in 7.512087 s.
- `go vet -race -tags=integration -work -x ./tests/model`: exit 0 in 8.975299 s.

Each command had its own 40-second timeout and a 45-second outer wait bound. Original argv, selected environment, PID/start time, stdout, stderr and exit data are retained under `compile01/` and `vet01/`. Direct waits completed; both initial and subsequent observations found each owned process group empty. The Go/cache window was handed back to root at `stop.json` before this report was prepared. No compilation defect or source correction arose.

The pre-command import-directed fingerprint record contained 486 named inputs, reusing 315 exact fingerprints from the existing pure-stage baseline and adding/changing 171. The successful vet action supplies 41 local package configurations and 463 actual Go source paths. Their original configuration bytes are mapped to byte ranges of the unchanged vet stderr in `observed-inputs.json`; no duplicate configuration tree was copied. The other 23 inputs are 21 embedded repository assets and the two module files. All 486 before/after byte counts and hashes match. This is the dependency scope of the exact `tests/model` package, not an index of the repository or module cache. No Go list command was used.

The private 52,547,665-byte compiled binary was only hashed, never run. Compiler-generated temporary work and the binary are outside the small evidence manifest. No tests, test listing, PG, MinIO, listener, model endpoint or business resource was executed. This author result establishes compilation and vet only; independent STATIC review and actual PG assertions/cleanup remain pending. It does not bind production consumers, serving, ExpectedDimensions, Nonchat/Invocation or root, and does not complete the card or its README. Previous pure-stage failures and assertion limitations remain preserved in their original evidence. The existing three stops and Jina boundary remain.
