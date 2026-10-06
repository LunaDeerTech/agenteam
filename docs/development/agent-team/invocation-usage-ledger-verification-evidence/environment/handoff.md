# Verified test dependency handoff

Resources are ready. No product source test, fixture container, network or database was started during preparation.

- Go: `/workspace/toolchains/go1.27.1/bin/go`, `go1.27.1 linux/amd64`, SHA-256 `30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624`.
- MinIO: `/workspace/scratch/agenteam-d09-ledger-verification-y4jvpxmq/bin/minio`. Exact fixed source/build reproduced SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`; release `RELEASE.2025-10-15T17-29-55Z` and source commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`. Server ZIP, go.mod and go.sum also match the D05 research checksums.
- Repository module cache: `/workspace/scratch/agenteam-d09-ledger-verification-y4jvpxmq/modcache`. Downloaded from fixed d09e8ef go.mod/go.sum using the public Go proxy/checksum service; offline `go mod verify` passed. Treat this cache as prepared and use a separate task-owned GOCACHE and TMPDIR for each execution owner.
- Supported image: `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`, image ID `sha256:5090c7fd921d75d8984233faaf8fb0d8ad18ddc38f912fd1261f9999b5d214a7`. Metadata says PostgreSQL 17.8; actual server/vector SQL values will be checked by the real fixture.
- Unsupported image: `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`, image ID `sha256:4c0c0efbd40e8ff42363074292654a7831cac506fc01a1c89627f1b1040ba341`; metadata PostgreSQL 16.12. It is never a positive migration target.
- Docker client/server are 28.4.0. Existing onboarding baseline remains exactly two containers/four networks by ID/name/labels. Those services were not accessed and must not be used by tests.

Set AGENTEAM_GO and AGENTEAM_MINIO_BINARY to the paths above; set GOTOOLCHAIN=local, GOENV=off, GOWORK=off, GOMODCACHE to the prepared cache, GOPROXY=off, GOSUMDB=off and GOFLAGS='-mod=readonly -p=1'. Use an owner-specific GOCACHE/TMPDIR and original fixture/race/count1/6m budgets.

Docker windows are coordinated by verification_recovery under root's updated ready-first scheduling. Ledger's first real run has priority once actually ready; other ready work may use an available window. Only an explicit current owner may launch. The owner freezes its source/selector first, observes exact live nonce resources, preserves raw output, verifies cleanup twice and returns the window promptly. `docker-current-window.json` and the individual window files record current ownership and actual handoffs. scripts/test-models.sh selects only TestModel, so ledger uses explicit TestUsage selectors through scripts/test-objects.sh.

See environment.json, images.json, preparation-commands.json, module-verification.json, minio-source-checks.json and preparation-final-checks.json for the actual preparation commands, results and limitations. All preparation processes have exited; private TMPDIR is empty. Planning is in verification-plan.md; no product acceptance is claimed.
