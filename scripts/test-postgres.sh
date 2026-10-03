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
umask 077
agenteam_helper_dir=$(mktemp -d "${TMPDIR:-/tmp}/agenteam-d03-helper.XXXXXX")
agenteam_helper_pid=''
agenteam_cleanup() {
  rm -f "$agenteam_helper_dir/fixture"
  rmdir "$agenteam_helper_dir"
}
agenteam_stop() {
  if [ -n "$agenteam_helper_pid" ]; then
    kill -TERM "$agenteam_helper_pid" 2>/dev/null || :
  else
    exit 130
  fi
}
trap agenteam_cleanup EXIT
trap agenteam_stop INT TERM
"$AGENTEAM_GO" build -o "$agenteam_helper_dir/fixture" ./tests/testsupport/postgres/cmd/fixture
"$agenteam_helper_dir/fixture" "$@" &
agenteam_helper_pid=$!
# wait can be interrupted by the forwarding trap. Keep waiting for the helper
# to finish its exact-ID cleanup before removing its temporary executable.
set +e
while :; do
  wait "$agenteam_helper_pid"
  agenteam_status=$?
  kill -0 "$agenteam_helper_pid" 2>/dev/null || break
done
exit "$agenteam_status"
