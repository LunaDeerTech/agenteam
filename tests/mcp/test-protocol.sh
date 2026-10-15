#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
AGENTEAM_GO=${AGENTEAM_GO:-/workspace/toolchains/go1.27.1/bin/go}
export AGENTEAM_GO
export GOTOOLCHAIN=local
test "$("$AGENTEAM_GO" env GOVERSION)" = go1.27.1
umask 077
mcp_test_dir=$(mktemp -d /tmp/agenteam-m3-mcp.XXXXXX)
trap 'rm -rf "$mcp_test_dir"' EXIT
CGO_ENABLED=0 "$AGENTEAM_GO" build -mod=readonly -o "$mcp_test_dir/provider" ./tests/mcp/provider
"$AGENTEAM_GO" test -mod=readonly -c -tags=integration -o "$mcp_test_dir/mcp.test" ./tests/mcp
"$AGENTEAM_GO" build -mod=readonly -o "$mcp_test_dir/pg-fixture" ./tests/testsupport/postgres/cmd/fixture
export AGENTEAM_MCP_PROVIDER_BINARY="$mcp_test_dir/provider"
export AGENTEAM_FIXTURE_TEST_BINARY="$mcp_test_dir/mcp.test"
export AGENTEAM_FIXTURE_TEST_CWD="$PWD"
export AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1
"$mcp_test_dir/pg-fixture" -run '^TestMCPProtocolRealControlled$'
