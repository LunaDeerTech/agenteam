package secret

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type projectAuditWitnessKey struct{}

// This value contains only the safe identities and metadata from the actual
// authorized call. In particular, it retains neither preparedWrite (which has
// sealed material) nor a plaintext/decryption buffer or a Service/Keyring.
type projectAuditWitness struct {
	store      Store
	tx         foundation.Tx
	entry      ac.Entry
	key        ac.AppendKey
	mutation   *projectMutationAudit
	resolution *projectResolutionAudit
}

type projectMutationAudit struct {
	actor          identity.Actor
	command        foundation.CommandIdentity
	kind           sc.MutationKind
	ref            sc.CredentialRef
	purpose        sc.Purpose
	expected       foundation.Version
	prior          sc.Metadata
	result         sc.MutationResult
	receiptID      foundation.ID[receiptMarker]
	receiptPayload payloadID
	valuePayload   payloadID
}

type projectResolutionAudit struct {
	resolution string
	caller     identity.Actor
	leaseID    sc.LeaseID
	lease      leaseRecord
	metadata   sc.Metadata
	payload    payloadID
	grant      sc.UseGrant
	modelRead  *modelUsageReadProof
}

func (projectAuditWitness) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_project_audit_witness")
}

// Only applyWriteInTx calls this, after canonical changes and the receipt have
// been written under the original authorization and locks, just before Append.
func mutationAuditContext(ctx context.Context, store Store, tx foundation.Tx, p preparedWrite, prior sc.Metadata, result sc.MutationResult, entry ac.Entry, key ac.AppendKey) context.Context {
	if p.scope.Details().Kind != identity.ProjectScope {
		return ctx
	}
	w := projectAuditWitness{store: store, tx: tx, entry: entry, key: key,
		mutation: &projectMutationAudit{actor: p.actor, command: p.command, kind: p.kind, ref: p.ref, purpose: p.purpose, expected: p.expected, prior: prior, result: result, receiptID: p.receiptID, receiptPayload: p.receipt.id, valuePayload: p.value.id}}
	return context.WithValue(ctx, projectAuditWitnessKey{}, w)
}

// Only the successful lease/grant/metadata/AEAD tail of an actual read calls
// this. Model reads must also have completed exact usage validation in this Tx.
// A resolution UUID or a persisted lease alone cannot mint either proof.
func resolutionAuditContext(ctx context.Context, store Store, tx foundation.Tx, resolution string, caller identity.Actor, id sc.LeaseID, lease leaseRecord, metadata sc.Metadata, payload payloadID, grant sc.UseGrant, entry ac.Entry, key ac.AppendKey, modelRead *modelUsageReadProof) context.Context {
	if lease.ref.Details().Scope.Details().Kind != identity.ProjectScope {
		return ctx
	}
	if modelRead != nil {
		modelRead = modelRead.copy()
	}
	w := projectAuditWitness{store: store, tx: tx, entry: entry, key: key,
		resolution: &projectResolutionAudit{resolution, caller, id, lease, metadata, payload, grant, modelRead}}
	return context.WithValue(ctx, projectAuditWitnessKey{}, w)
}

func sameProjectAuditStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) &&
		reflect.ValueOf(a).Comparable() && reflect.ValueOf(b).Comparable() && a == b
}

func sameProjectAuditEntry(a, b ac.Entry) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	x, y := a.Fields(), b.Fields()
	// Do not use Audit.SemanticDigest: its deliberate Session/HTTPTraceID
	// exclusions are correct for idempotency, not for this call-local witness.
	return x.Scope.Equal(y.Scope) && x.Actor.Equal(y.Actor) && x.Action == y.Action &&
		x.Outcome == y.Outcome && x.Resource.Details() == y.Resource.Details() &&
		bytes.Equal(x.Metadata.JSON(), y.Metadata.JSON()) && x.Associations == y.Associations
}
