#!/bin/sh
# One original App/Runner and the official nonce-owned native fixtures.
set -eu
cd "$(dirname "$0")/../.."
umask 077
: "${AGENTEAM_GO:?exact Go 1.27.1 binary required}"
: "${AGENTEAM_MINIO_BINARY:?verified MinIO binary required}"
agenteam_m2_browser=0
agenteam_m2_live=0
case "${AGENTEAM_M2_CASE:-approval}" in
  approval) agenteam_m2_selector='^TestM2ApprovalWorkflow$' ;;
  cancellation) agenteam_m2_selector='^TestM2ExecutionCancellationWorkflow$' ;;
  rejection-pairing) agenteam_m2_selector='^TestM2ApprovalRejectsUnpairedResult$' ;;
  cold-recovery) agenteam_m2_selector='^TestM2AppExecutionColdRecovery$' ;;
  waiting-cancellation) agenteam_m2_selector='^TestM2ExecutionWaitingCancellationRecovery$' ;;
  completed-tool-cold) agenteam_m2_selector='^TestM2AppExecutionCompletedToolColdRecovery$' ;;
  pending-cancellation) agenteam_m2_selector='^TestM2PendingUnknownTaskCancellation$' ;;
  artifact-source-mismatch) agenteam_m2_selector='^TestM2ArtifactSourceMismatch$' ;;
  live-task-browser)
    if [ "${AGENTEAM_M2_LIVE_OPT_IN:-}" != external-model-task-artifact-review ]; then
      printf '%s\n' 'M2 live requires explicit AGENTEAM_M2_LIVE_OPT_IN' >&2
      exit 1
    fi
    agenteam_m2_selector='^TestM2AppLiveArtifactBrowserReview$'
    export AGENTEAM_M2_WEB_CASE=return-and-accept
    # Reuse the original M1 credential gate only after both explicit M2 gates.
    export AGENTEAM_M1_CASE=live
    export AGENTEAM_M1_LIVE_OPT_IN=external-model-read-write-run-review
    agenteam_m2_browser=1
    agenteam_m2_live=1
    ;;
  task-human) agenteam_m2_selector='^TestM2AppArtifactHumanReview$' ;;
  task-browser-accept)
    agenteam_m2_selector='^TestM2AppArtifactBrowserReview$/^accept$'
    export AGENTEAM_M2_WEB_CASE=accept
    agenteam_m2_browser=1
    ;;
  task-browser-return)
    agenteam_m2_selector='^TestM2AppArtifactBrowserReview$/^return-and-accept$'
    export AGENTEAM_M2_WEB_CASE=return-and-accept
    agenteam_m2_browser=1
    ;;
  *) printf '%s\n' 'M2 case must be approval, cancellation, rejection-pairing, cold-recovery, waiting-cancellation, completed-tool-cold, pending-cancellation, artifact-source-mismatch, task-human, task-browser-accept, task-browser-return or live-task-browser' >&2; exit 1 ;;
esac
if [ "$("$AGENTEAM_GO" env GOVERSION)" != go1.27.1 ]; then
  printf '%s\n' 'Go 1.27.1 required' >&2
  exit 1
fi
agenteam_m2_root=$(pwd)
agenteam_m2_parent="$agenteam_m2_root/output/ai/m2-app-integration"
mkdir -p "$agenteam_m2_parent"
agenteam_m2_run=$(mktemp -d "$agenteam_m2_parent/run.XXXXXX")
agenteam_m2_link="$agenteam_m2_root/tests/account-captcha-web/node_modules"
agenteam_m2_web_link="$agenteam_m2_root/web/node_modules"
agenteam_m2_created_link=0
agenteam_m2_created_web_link=0
agenteam_m2_cleanup() {
  if [ "$agenteam_m2_created_web_link" = 1 ]; then
    rm "$agenteam_m2_web_link"
  fi
  if [ "$agenteam_m2_created_link" = 1 ]; then
    rm "$agenteam_m2_link"
  fi
}
trap agenteam_m2_cleanup EXIT
if [ "$agenteam_m2_browser" = 1 ]; then
  : "${AGENTEAM_M2_BROWSER_MODULES:?external locked browser node_modules required}"
  : "${AGENTEAM_M2_WEB_MODULES:?external locked web node_modules required}"
  : "${AGENTEAM_M2_WEB_DIST:?production web dist required}"
  if [ ! -e "$agenteam_m2_link" ] && [ ! -L "$agenteam_m2_link" ]; then
    ln -s "$AGENTEAM_M2_BROWSER_MODULES" "$agenteam_m2_link"
    agenteam_m2_created_link=1
  elif [ "$(readlink "$agenteam_m2_link")" != "$AGENTEAM_M2_BROWSER_MODULES" ]; then
    printf '%s\n' 'M2 browser dependency location already owned by another install' >&2
    exit 1
  fi
  if [ ! -e "$agenteam_m2_web_link" ] && [ ! -L "$agenteam_m2_web_link" ]; then
    ln -s "$AGENTEAM_M2_WEB_MODULES" "$agenteam_m2_web_link"
    agenteam_m2_created_web_link=1
  elif [ "$(readlink "$agenteam_m2_web_link")" != "$AGENTEAM_M2_WEB_MODULES" ]; then
    printf '%s\n' 'M2 web dependency location already owned by another install' >&2
    exit 1
  fi
  export AGENTEAM_M2_WEB_HARNESS="$agenteam_m2_root/tests/account-captcha-web"
  export AGENTEAM_M2_WEB_EVIDENCE="$agenteam_m2_run/browser"
fi
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOMAXPROCS=2
export GOFLAGS='-p=2 -mod=readonly'
if [ "$agenteam_m2_live" = 0 ]; then
  # Controlled selectors cannot inherit external model credentials.
  unset DEV_CHAT_BASE_URL DEV_CHAT_API_KEY DEV_CHAT_MODEL AGENTEAM_M1_CASE AGENTEAM_M1_LIVE_OPT_IN
fi
# Live uses the original qualified M1 Secret accessor. Proxy, NO_PROXY and CA/TLS
# settings remain intact; Runner/browser child environments exclude DEV_CHAT.
"$AGENTEAM_GO" build -o "$agenteam_m2_run/agenteam-runner" ./cmd/agenteam-runner
"$AGENTEAM_GO" test -tags=integration -c -o "$agenteam_m2_run/app.test" ./internal/central/app
export AGENTEAM_RUNNER_BINARY="$agenteam_m2_run/agenteam-runner"
export AGENTEAM_FIXTURE_TEST_BINARY="$agenteam_m2_run/app.test"
export AGENTEAM_FIXTURE_TEST_CWD="$agenteam_m2_root/internal/central/app"
export AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
export AGENTEAM_FIXTURE_OWNED_RECORD="$agenteam_m2_run/owned-resources.json"
printf 'M2 evidence directory: %s\n' "$agenteam_m2_run"
sh scripts/test-objects.sh -run "$agenteam_m2_selector" >"$agenteam_m2_run/native.log" 2>&1 &
agenteam_m2_pid=$!
trap 'kill -TERM "$agenteam_m2_pid" 2>/dev/null || :' INT TERM
set +e
while :; do
  wait "$agenteam_m2_pid"
  agenteam_m2_status=$?
  kill -0 "$agenteam_m2_pid" 2>/dev/null || break
done
printf 'M2 original fixture wait exit=%s log=%s\n' "$agenteam_m2_status" "$agenteam_m2_run/native.log"
exit "$agenteam_m2_status"
