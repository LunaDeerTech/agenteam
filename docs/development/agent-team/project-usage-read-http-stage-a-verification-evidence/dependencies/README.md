# Go dependency recovery

Fixed project dependencies are available through `GOMODCACHE=/workspace/go/pkg/mod`. The project requires 31 modules; 25 incomplete entries were downloaded at their existing versions and verified against go.sum before adding only missing cache files. All 31 ZIP, extracted-source and go.mod h1 checks passed. Existing file bytes and version lists were preserved. Project go.mod/go.sum were unchanged.

Offline `go list -deps -test` passed for Account/Usage HTTP, all ordinary packages, and all integration-tag packages. This establishes dependency availability only. Inputs are main175694bf plus the five Usage A worktree paths bound in evidence/availability-input-before.json; their hashes did not change during the checks. No product build/test or runtime fixture was executed.

Evidence includes every command, required Go environment, raw output, exit, PID/starttime, original missing list, checksum results, new cache paths and a record of the corrected diagnostic sorting error. All nine recorded command processes have exited. The earlier MinIO recovery remains independently frozen.
