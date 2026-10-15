package contract

import "context"

// MCPCredentialUsageReader requires an exact authorized MCP attempt, its
// current lease plan, and committed resolution audit before issuing material.
// Its request uses Purpose MCP and a connection-request or Execution owner.
type MCPCredentialUsageReader interface {
	ReadMCPCredentialForUsage(context.Context, UsageRequest) (SecretMaterial, error)
}
