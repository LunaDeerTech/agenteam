#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
AGENTEAM_GO=${AGENTEAM_GO:-go}
GOTOOLCHAIN=local
export AGENTEAM_GO GOTOOLCHAIN
if ! agenteam_version=$("$AGENTEAM_GO" env GOVERSION); then
  printf '%s\n' 'Unable to execute the configured Go toolchain.' >&2
  exit 1
fi
if [ "$agenteam_version" != 'go1.27.1' ]; then
  printf '%s\n' 'Go go1.27.1 is required; set AGENTEAM_GO to that binary.' >&2
  exit 1
fi
"$AGENTEAM_GO" test ./...
"$AGENTEAM_GO" vet ./...
"$AGENTEAM_GO" test -race ./...
sh scripts/build-go.sh
