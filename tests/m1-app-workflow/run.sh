#!/bin/sh
# One owned App/Runner/Provider/browser workflow through the official native fixtures.
set -eu
cd "$(dirname "$0")/../.."
umask 077
: "${AGENTEAM_GO:?exact Go 1.27.1 binary required}"
: "${AGENTEAM_MINIO_BINARY:?verified MinIO binary required}"
: "${AGENTEAM_M1_BROWSER_MODULES:?external locked browser node_modules required}"
: "${AGENTEAM_M1_WEB_DIST:?production web dist required}"
case "${AGENTEAM_M1_CASE:-workflow}" in
  workflow)
    : "${AGENTEAM_M1_WEB_MODULES:?external locked web node_modules required}"
    agenteam_m1_selector='^TestM1AppToolWorkflow$'
    ;;
  live)
    : "${AGENTEAM_M1_WEB_MODULES:?external locked web node_modules required}"
    if [ "${AGENTEAM_M1_LIVE_OPT_IN:-}" != external-model-read-write-run-review ]; then
      printf '%s\n' 'M1 live requires explicit AGENTEAM_M1_LIVE_OPT_IN' >&2
      exit 1
    fi
    agenteam_m1_selector='^TestM1AppLiveWorkflow$'
    ;;
  failure-and-cancellation) agenteam_m1_selector='^TestM1AppToolFailureAndCancellation$' ;;
  *) printf '%s\n' 'M1 case must be workflow, failure-and-cancellation or live' >&2; exit 1 ;;
esac
if [ "$("$AGENTEAM_GO" env GOVERSION)" != go1.27.1 ]; then
  printf '%s\n' 'Go 1.27.1 required' >&2
  exit 1
fi
agenteam_m1_root=$(pwd)
agenteam_m1_parent="$agenteam_m1_root/output/ai/m1-app-integration"
mkdir -p "$agenteam_m1_parent"
agenteam_m1_run=$(mktemp -d "$agenteam_m1_parent/run.XXXXXX")
agenteam_m1_link="$agenteam_m1_root/tests/account-captcha-web/node_modules"
agenteam_m1_created_link=0
agenteam_m1_web_link="$agenteam_m1_root/web/node_modules"
agenteam_m1_created_web_link=0
agenteam_m1_cleanup() {
  if [ "$agenteam_m1_created_web_link" = 1 ]; then
    rm "$agenteam_m1_web_link"
  fi
  if [ "$agenteam_m1_created_link" = 1 ]; then
    rm "$agenteam_m1_link"
  fi
}
trap agenteam_m1_cleanup EXIT
if [ ! -e "$agenteam_m1_link" ] && [ ! -L "$agenteam_m1_link" ]; then
  ln -s "$AGENTEAM_M1_BROWSER_MODULES" "$agenteam_m1_link"
  agenteam_m1_created_link=1
elif [ "$(readlink "$agenteam_m1_link")" != "$AGENTEAM_M1_BROWSER_MODULES" ]; then
  printf '%s\n' 'M1 harness dependency location already owned by another install' >&2
  exit 1
fi
if [ "${AGENTEAM_M1_CASE:-workflow}" != failure-and-cancellation ]; then
  if [ ! -e "$agenteam_m1_web_link" ] && [ ! -L "$agenteam_m1_web_link" ]; then
    ln -s "$AGENTEAM_M1_WEB_MODULES" "$agenteam_m1_web_link"
    agenteam_m1_created_web_link=1
  elif [ "$(readlink "$agenteam_m1_web_link")" != "$AGENTEAM_M1_WEB_MODULES" ]; then
    printf '%s\n' 'M1 web dependency location already owned by another install' >&2
    exit 1
  fi
fi
export GOTOOLCHAIN=local GOENV=off GOWORK=off
export GOMAXPROCS=2
export GOFLAGS='-p=2 -mod=readonly'
"$AGENTEAM_GO" build -o "$agenteam_m1_run/agenteam-runner" ./cmd/agenteam-runner
"$AGENTEAM_GO" test -tags=integration -c -o "$agenteam_m1_run/app.test" ./internal/central/app
export AGENTEAM_RUNNER_BINARY="$agenteam_m1_run/agenteam-runner"
export AGENTEAM_FIXTURE_TEST_BINARY="$agenteam_m1_run/app.test"
export AGENTEAM_FIXTURE_TEST_CWD="$agenteam_m1_root/internal/central/app"
export AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
export AGENTEAM_FIXTURE_OWNED_RECORD="$agenteam_m1_run/owned-resources.json"
export AGENTEAM_M1_WEB_HARNESS="$agenteam_m1_root/tests/account-captcha-web"
export AGENTEAM_M1_WEB_EVIDENCE="$agenteam_m1_run/browser"
export AGENTEAM_M1_CASE="${AGENTEAM_M1_CASE:-workflow}"
if [ "$AGENTEAM_M1_CASE" != live ]; then
  # The unchanged controlled selectors cannot inherit a live credential.
  unset DEV_CHAT_BASE_URL DEV_CHAT_API_KEY DEV_CHAT_MODEL AGENTEAM_M1_LIVE_OPT_IN
fi
# Live reads the key only inside the selected test after metadata and original
# system DNS preflight. Runner/browser child environments exclude DEV_CHAT.
printf 'M1 evidence directory: %s\n' "$agenteam_m1_run"
sh scripts/test-objects.sh -run "$agenteam_m1_selector" >"$agenteam_m1_run/native.log" 2>&1 &
agenteam_m1_pid=$!
trap 'kill -TERM "$agenteam_m1_pid" 2>/dev/null || :' INT TERM
set +e
while :; do
  wait "$agenteam_m1_pid"
  agenteam_m1_status=$?
  kill -0 "$agenteam_m1_pid" 2>/dev/null || break
done
printf 'M1 original fixture wait exit=%s log=%s\n' "$agenteam_m1_status" "$agenteam_m1_run/native.log"
exit "$agenteam_m1_status"
