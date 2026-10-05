package project

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// SecretAuthority keeps Secret's distinct cleanup contract separate from the
// Audit port on Authority. Its captured Authority has no mutable registry.
type SecretAuthority struct{ authority Authority }

func NewSecretAuthority(authority *Authority) (*SecretAuthority, error) {
	if authority.state() == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	return &SecretAuthority{authority: *authority}, nil
}

func (a *SecretAuthority) bound() bool { return a != nil && a.authority.state() != nil }

func (a *SecretAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if !a.bound() {
		return identity.AccessGrant{}, fault(foundation.DependencyUnbound)
	}
	return a.authority.AuthorizeProject(ctx, tx, actor, project, intent)
}

func (a *SecretAuthority) CheckMutationInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref secret.CredentialRef) error {
	if !a.bound() {
		return fault(foundation.DependencyUnbound)
	}
	if ref.Validate() != nil || ref.Details().Scope.Details().Kind != identity.ProjectScope {
		return invalid()
	}
	project, err := parseID[identity.Project](ref.Details().Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	_, err = a.authority.RequireOwnerInTx(ctx, tx, actor, project, identity.Mutate)
	return err
}

func (*SecretAuthority) CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, secret.LifecycleCause, identity.ProjectID) error {
	return fault(foundation.DependencyUnbound)
}

var _ secret.ProjectAuthority = (*SecretAuthority)(nil)
