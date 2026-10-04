#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
AGENTEAM_GO=${AGENTEAM_GO:-go}
GOTOOLCHAIN=local
export AGENTEAM_GO GOTOOLCHAIN
agenteam_version=$("$AGENTEAM_GO" env GOVERSION)
if [ "$agenteam_version" != 'go1.27.1' ]; then
  printf '%s\n' 'Go go1.27.1 is required; set AGENTEAM_GO to that binary.' >&2
  exit 1
fi
umask 077
agenteam_helper_dir=$(mktemp -d "${TMPDIR:-/tmp}/agenteam-d05-object-helper.XXXXXX")
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
"$AGENTEAM_GO" build -o "$agenteam_helper_dir/fixture" ./tests/testsupport/objectstore/cmd/fixture
"$agenteam_helper_dir/fixture" "$@" &
agenteam_helper_pid=$!
set +e
while :; do
  wait "$agenteam_helper_pid"
  agenteam_status=$?
  kill -0 "$agenteam_helper_pid" 2>/dev/null || break
done
exit "$agenteam_status"
