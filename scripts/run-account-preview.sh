#!/bin/sh
# Isolated real Account preview. Never consumes deployment database credentials.
# Stop with SIGTERM to this script, or create $AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/stop.
set -eu
cd "$(dirname "$0")/.."
AGENTEAM_GO=${AGENTEAM_GO:-/workspace/toolchains/go1.27.1/bin/go}
GOTOOLCHAIN=local
export AGENTEAM_GO GOTOOLCHAIN
if [ "$("$AGENTEAM_GO" env GOVERSION)" != go1.27.1 ]; then
  printf '%s\n' 'Go go1.27.1 is required.' >&2
  exit 1
fi
if [ -z "${AGENTEAM_MINIO_BINARY:-}" ]; then
  printf '%s\n' 'Set AGENTEAM_MINIO_BINARY to the verified fixed MinIO binary.' >&2
  exit 1
fi
if [ -n "${AGENTEAM_PG_FIXTURE:-}${AGENTEAM_OBJECT_FIXTURE:-}${AGENTEAM_OUTBOUND_FIXTURE:-}" ]; then
  printf '%s\n' 'Start the preview without inherited infrastructure fixture descriptors.' >&2
  exit 1
fi
umask 077
if [ -z "${AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY:-}" ]; then
  AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY=$(mktemp -d "${TMPDIR:-/tmp}/agenteam-account-preview.XXXXXXXX")
else
  mkdir -m 700 "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY"
fi
export AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY
agenteam_preview_child=''
agenteam_preview_stop() {
  : > "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/stop"
}
trap agenteam_preview_stop INT TERM
printf '%s\n' "$$" > "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/launcher.pid"
printf 'Private preview directory: %s\n' "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY"
AGENTEAM_ACCOUNT_PREVIEW_BINARY="$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/app.test"
export AGENTEAM_ACCOUNT_PREVIEW_BINARY
"$AGENTEAM_GO" test -c -tags=integration -o "$AGENTEAM_ACCOUNT_PREVIEW_BINARY" ./internal/central/app
if [ -f "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/stop" ]; then
  rm -f "$AGENTEAM_ACCOUNT_PREVIEW_BINARY" "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/launcher.pid"
  exit 0
fi
cat > "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/test-wrapper" <<'WRAPPER'
#!/bin/sh
set -eu
case "$*" in
  *-test.list=*) exec "$AGENTEAM_ACCOUNT_PREVIEW_BINARY" "$@" ;;
esac
# Explicit preview-only override: the stock runner's integration budget stays intact.
exec "$AGENTEAM_ACCOUNT_PREVIEW_BINARY" "$@" -test.timeout=55m
WRAPPER
chmod 700 "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/test-wrapper"
AGENTEAM_ACCOUNT_PREVIEW=1
AGENTEAM_FIXTURE_TEST_BINARY="$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/test-wrapper"
AGENTEAM_FIXTURE_TEST_CWD=$(pwd)
AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
export AGENTEAM_ACCOUNT_PREVIEW AGENTEAM_FIXTURE_TEST_BINARY AGENTEAM_FIXTURE_TEST_CWD AGENTEAM_FIXTURE_PG_CURRENT_ONLY
sh scripts/test-objects.sh -run '^TestAccountPreviewFixture$' &
agenteam_preview_child=$!
set +e
while :; do
  wait "$agenteam_preview_child"
  agenteam_preview_status=$?
  kill -0 "$agenteam_preview_child" 2>/dev/null || break
done
rm -f "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/app.test" "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/test-wrapper" "$AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY/launcher.pid"
printf 'Preview chain actual_wait pid=%s code=%s\n' "$agenteam_preview_child" "$agenteam_preview_status"
exit "$agenteam_preview_status"
