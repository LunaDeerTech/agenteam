#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
: "${AGENTEAM_FIXTURE_TEST_BINARY:?fixed MCP App binary required}"
: "${AGENTEAM_MCP_PROVIDER_BINARY:?fixed official SDK provider required}"
: "${AGENTEAM_MCP_WEB_DIST:?fixed production SPA assets required}"
: "${AGENTEAM_MCP_WEB_EVIDENCE:?owned browser evidence directory required}"
AGENTEAM_GO=${AGENTEAM_GO:-/workspace/toolchains/go1.27.1/bin/go}
AGENTEAM_MINIO_BINARY=${AGENTEAM_MINIO_BINARY:-/workspace/shared/agenteam-m1-tools/minio/minio}
AGENTEAM_MCP_WEB_PLAYWRIGHT=${AGENTEAM_MCP_WEB_PLAYWRIGHT:-/workspace/shared/agenteam-m1-tools/node-browser/node_modules/@playwright/test}
AGENTEAM_AUTH_WEB_CHROMIUM=${AGENTEAM_AUTH_WEB_CHROMIUM:-/usr/bin/chromium}
AGENTEAM_MCP_WEB_CONFIG="$PWD/tests/mcp/browser.config.cjs"
export AGENTEAM_GO AGENTEAM_MINIO_BINARY AGENTEAM_MCP_WEB_PLAYWRIGHT AGENTEAM_AUTH_WEB_CHROMIUM AGENTEAM_MCP_WEB_CONFIG
export GOTOOLCHAIN=local
export GOFLAGS='-mod=readonly -p=1'
test "$("$AGENTEAM_GO" env GOVERSION)" = go1.27.1
umask 077
mcp_browser_root=$(mktemp -d /tmp/m3-mcp-web.XXXXXX)
mcp_browser_pid=''
mcp_browser_cleanup() { rm -rf "$mcp_browser_root"; }
mcp_browser_stop() {
  if [ -n "$mcp_browser_pid" ]; then
    kill -TERM "$mcp_browser_pid" 2>/dev/null || :
  else
    exit 130
  fi
}
trap mcp_browser_cleanup EXIT
trap mcp_browser_stop INT TERM
export AGENTEAM_AUTH_WEB_RUNTIME="$mcp_browser_root"
export AGENTEAM_MCP_WEB_PRIVATE="$mcp_browser_root"
node "$AGENTEAM_MCP_WEB_PLAYWRIGHT/cli.js" test --config "$AGENTEAM_MCP_WEB_CONFIG" --list
"$AGENTEAM_FIXTURE_TEST_BINARY" -test.run '^TestMCPM3AppConfiguration$' -test.count=1
test "$("$AGENTEAM_FIXTURE_TEST_BINARY" -test.list '^TestMCPM3OwnerWeb$')" = TestMCPM3OwnerWeb
export AGENTEAM_FIXTURE_TEST_CWD="$PWD"
export AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
sh scripts/test-objects.sh -run '^TestMCPM3OwnerWeb$' &
mcp_browser_pid=$!
set +e
while :; do
  wait "$mcp_browser_pid"
  mcp_browser_status=$?
  kill -0 "$mcp_browser_pid" 2>/dev/null || break
done
exit "$mcp_browser_status"
