package contract

import "context"

// InvocationRetirementAuthority proves the original local Tool owner has
// returned and is joining retained accounting. It grants no read or mutation
// permission and remains usable after cancellation or current revocation.
type InvocationRetirementAuthority interface {
	RequireSkillInvocationRetirement(context.Context, Invocation) error
}
