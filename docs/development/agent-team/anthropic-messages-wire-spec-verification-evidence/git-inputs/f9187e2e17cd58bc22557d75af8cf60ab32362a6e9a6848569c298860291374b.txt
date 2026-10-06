#!/bin/sh
# Reuse the real owned fixture and its original race/count/6m package budget.
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -ne 0 ]; then
  printf '%s\n' 'Usage: test-models.sh' >&2
  exit 2
fi
GOFLAGS=${GOFLAGS:--p=1}
export GOFLAGS
exec sh scripts/test-objects.sh -run '^TestModel'
