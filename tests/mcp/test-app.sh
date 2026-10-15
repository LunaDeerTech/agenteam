#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
AGENTEAM_GO=${AGENTEAM_GO:-/workspace/toolchains/go1.27.1/bin/go}
AGENTEAM_MINIO_BINARY=${AGENTEAM_MINIO_BINARY:-/workspace/shared/agenteam-m1-tools/minio/minio}
export AGENTEAM_GO AGENTEAM_MINIO_BINARY
export GOTOOLCHAIN=local
export GOFLAGS='-mod=readonly -p=1'
test "$("$AGENTEAM_GO" env GOVERSION)" = go1.27.1
mcp_app_top=${1:-TestMCPM3OwnerConnection}
case "$mcp_app_top" in
  TestMCPM3OwnerConnection|TestMCPM3OwnerCredentials|TestMCPM3AgentToolsAndResources) ;;
  *) echo 'unknown MCP App top' >&2; exit 2 ;;
esac
umask 077
mcp_app_dir=$(mktemp -d /tmp/agenteam-m3-mcp-app.XXXXXX)
mcp_app_pid=''
mcp_app_cleanup() { rm -rf "$mcp_app_dir"; }
mcp_app_stop() {
  if [ -n "$mcp_app_pid" ]; then
    kill -TERM "$mcp_app_pid" 2>/dev/null || :
  else
    exit 130
  fi
}
trap mcp_app_cleanup EXIT
trap mcp_app_stop INT TERM
if [ "$mcp_app_top" != TestMCPM3OwnerCredentials ]; then
  CGO_ENABLED=0 "$AGENTEAM_GO" build -o "$mcp_app_dir/provider" ./tests/mcp/provider
fi
"$AGENTEAM_GO" test -race -c -tags=integration -o "$mcp_app_dir/app.test" ./internal/central/app
# Fail closed on the complete original Config before provisioning any resource.
"$mcp_app_dir/app.test" -test.run '^TestMCPM3AppConfiguration$' -test.count=1
test "$("$mcp_app_dir/app.test" -test.list "^$mcp_app_top$")" = "$mcp_app_top"
export AGENTEAM_MCP_PROVIDER_BINARY="$mcp_app_dir/provider"
export AGENTEAM_FIXTURE_TEST_BINARY="$mcp_app_dir/app.test"
export AGENTEAM_FIXTURE_TEST_CWD="$PWD"
export AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
sh scripts/test-objects.sh -run "^$mcp_app_top$" &
mcp_app_pid=$!
set +e
while :; do
  wait "$mcp_app_pid"
  mcp_app_status=$?
  kill -0 "$mcp_app_pid" 2>/dev/null || break
done
exit "$mcp_app_status"
