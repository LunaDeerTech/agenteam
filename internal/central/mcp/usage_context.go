package mcp

import "context"

// OwnsUsageContext is a static router discriminator, never a lease grant. The
// selected AuthorizeLeaseInTx still verifies the original request and exact
// validated transaction. No credential lookup or rejected-call fallback is
// needed to choose the MCP branch of Secret's shared authority port.
func (a *Authority) OwnsUsageContext(ctx context.Context) bool {
	s, err := a.service()
	if err != nil || ctx == nil {
		return false
	}
	if w, ok := ctx.Value(humanRefreshRecoveryKey{}).(*humanRefreshRecoveryUse); ok && w != nil {
		_, err = s.humanRefreshRecoveryUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(oauthConnectionRecoveryKey{}).(*oauthConnectionRecoveryUse); ok && w != nil {
		_, err = s.oauthConnectionRecoveryUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(startupSecretKey{}).(*startupSecretUse); ok && w != nil {
		_, err = s.startupSecretUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(oauthUsageKey{}).(*oauthUsage); ok && w != nil {
		_, err = s.oauthUsage(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(secretUseKey{}).(*secretUse); ok && w != nil {
		_, err = s.secretUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(runtimeSecretKey{}).(*runtimeSecretUse); ok && w != nil {
		_, err = s.runtimeSecretUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(resourceSecretKey{}).(*resourceSecretUse); ok && w != nil {
		_, err = s.resourceSecretUse(ctx, w.request)
		return err == nil
	}
	if w, ok := ctx.Value(retentionUseKey{}).(*retentionUse); ok && w != nil && w.entry != nil {
		_, err = s.retentionUse(ctx, w.entry.usage)
		return err == nil
	}
	if w, ok := ctx.Value(executionRetirementKey{}).(*executionRetirementUse); ok && w != nil && w.entry != nil {
		_, err = s.executionRetirementUse(ctx, w.entry.usage)
		return err == nil
	}
	return false
}
