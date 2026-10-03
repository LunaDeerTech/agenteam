package secret

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type receiptMarker struct{}
type preparedWrite struct {
	actor          identity.Actor
	scope          identity.Scope
	command        foundation.CommandIdentity
	kind           sc.MutationKind
	ref            sc.CredentialRef
	expected       foundation.Version
	purpose        sc.Purpose
	value, receipt envelope
	receiptID      foundation.ID[receiptMarker]
	version, epoch foundation.Version
	commandDigest  foundation.Digest
}
type PreparedWrite struct{ data func() preparedWrite }

func (p PreparedWrite) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "prepared_secret_write") }
func (p PreparedWrite) MarshalJSON() ([]byte, error) { return []byte(`"prepared_secret_write"`), nil }
func (p *PreparedWrite) UnmarshalJSON([]byte) error  { return invalid() }
func (p PreparedWrite) LogValue() slog.Value         { return slog.StringValue("prepared_secret_write") }
func validateWrite(r sc.WriteRequest) error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != identity.Human || !validScope(r.Scope) || r.Identity.Validate() != nil || !r.Purpose.Valid() {
		return invalid()
	}
	if r.Kind != sc.Create && r.Kind != sc.Update && r.Kind != sc.Delete {
		return invalid()
	}
	if r.Identity.Namespace() != "secret" || r.Identity.Command() != string(r.Kind) {
		return invalid()
	}
	owners := []string{r.Actor.Details().UserID}
	if r.Scope.Details().Kind == identity.ProjectScope {
		owners = append([]string{r.Scope.Details().ProjectID}, owners...)
	}
	if !slices.Equal(owners, r.Identity.OwnerIDs()) {
		return invalid()
	}
	if r.Kind == sc.Create {
		if r.ExpectedVersion != 0 || r.Ref.Validate() == nil {
			return invalid()
		}
	} else if r.Ref.Validate() != nil || !r.Ref.Details().Scope.Equal(r.Scope) || r.ExpectedVersion.Validate() != nil {
		return invalid()
	}
	return nil
}
func semanticWriteDigest(r sc.WriteRequest, value []byte) ([]byte, error) {
	refID := ""
	if r.Kind != sc.Create {
		refID = r.Ref.Details().ID.String()
	}
	wire := struct {
		Format       int             `json:"format"`
		Scope        identity.Scope  `json:"scope"`
		ActorUser    string          `json:"actor_user_id"`
		Kind         sc.MutationKind `json:"kind"`
		CredentialID string          `json:"credential_id"`
		Expected     string          `json:"expected_version"`
		Purpose      sc.Purpose      `json:"purpose"`
		Value        []byte          `json:"value"`
	}{1, r.Scope, r.Actor.Details().UserID, r.Kind, refID, strconv.FormatInt(int64(r.ExpectedVersion), 10), r.Purpose, value}
	raw, err := json.Marshal(wire)
	if err != nil {
		return nil, invalid()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return nil, invalid()
	}
	defer clear(canonical)
	digest := sha256.Sum256(canonical)
	return append([]byte(nil), digest[:]...), nil
}
func (s *Service) PrepareWrite(ctx context.Context, r sc.WriteRequest) (PreparedWrite, error) {
	if err := validateWrite(r); err != nil {
		return PreparedWrite{}, err
	}
	if err := s.writable(); err != nil {
		return PreparedWrite{}, err
	}
	state := s.state()
	version, epoch, err := s.control(ctx, state.store)
	if err != nil {
		return PreparedWrite{}, err
	}
	if version != state.keys.CurrentVersion() {
		return PreparedWrite{}, failure(EpochChanged, foundation.InvalidState, nil)
	}
	ref := r.Ref
	if r.Kind == sc.Create {
		id, err := foundation.NewID[sc.Credential]()
		if err != nil {
			return PreparedWrite{}, unavailable(err)
		}
		ref, err = sc.NewCredentialRef(id, r.Scope)
		if err != nil {
			return PreparedWrite{}, invalid()
		}
	}
	receiptID, err := foundation.NewID[receiptMarker]()
	if err != nil {
		return PreparedWrite{}, unavailable(err)
	}
	receiptPayloadID, err := foundation.NewID[payloadMarker]()
	if err != nil {
		return PreparedWrite{}, unavailable(err)
	}
	commandDigest, err := cursor.Digest([]byte(r.Identity.Canonical()))
	if err != nil {
		return PreparedWrite{}, invalid()
	}
	p := preparedWrite{actor: r.Actor, scope: r.Scope, command: r.Identity, kind: r.Kind, ref: ref, expected: r.ExpectedVersion, purpose: r.Purpose, receiptID: receiptID, version: version, epoch: epoch, commandDigest: commandDigest}
	receiptNonce, err := s.nextNonce(ctx, version)
	if err != nil {
		return PreparedWrite{}, err
	}
	var valueNonce []byte
	var valueID payloadID
	if r.Kind != sc.Delete {
		valueNonce, err = s.nextNonce(ctx, version)
		if err != nil {
			return PreparedWrite{}, err
		}
		valueID, err = foundation.NewID[payloadMarker]()
		if err != nil {
			return PreparedWrite{}, unavailable(err)
		}
	}
	prepare := func(value []byte) error {
		digest, err := semanticWriteDigest(r, value)
		if err != nil {
			return err
		}
		defer clear(digest)
		p.receipt, err = seal(state.keys, version, receiptNonce, r.Scope, receiptOwner, receiptID.String(), receiptPayloadID, digest)
		if err != nil {
			return err
		}
		if r.Kind != sc.Delete {
			p.value, err = seal(state.keys, version, valueNonce, r.Scope, valueOwner, ref.Details().ID.String(), valueID, value)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if r.Kind == sc.Delete {
		err = prepare(nil)
	} else {
		err = r.Value.Use(prepare)
	}
	if err != nil {
		return PreparedWrite{}, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return PreparedWrite{}, unavailable(err)
	}
	// Only sealed envelopes survive preparation. The request's SecretMaterial
	// handle, canonical bytes and plaintext semantic digest are not retained.
	return PreparedWrite{data: func() preparedWrite { return p }}, nil
}
func (s *Service) ExecuteWrite(ctx context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	prepared, err := s.PrepareWrite(ctx, r)
	if err != nil {
		return sc.MutationResult{}, err
	}
	cause, err := foundation.NewCommandsCause(r.Identity)
	if err != nil {
		return sc.MutationResult{}, invalid()
	}
	var result sc.MutationResult
	committed := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		var err error
		result, err = s.ApplyPreparedWriteInTx(ctx, tx, prepared)
		return err
	})
	if err = commitError(committed); err != nil {
		return sc.MutationResult{}, err
	}
	return result, nil
}
func (s *Service) ApplyPreparedWriteInTx(ctx context.Context, tx foundation.Tx, prepared PreparedWrite) (sc.MutationResult, error) {
	empty := sc.MutationResult{}
	if prepared.data == nil {
		return empty, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	p := prepared.data()
	state := s.state()
	e, err := state.store.InTx(tx)
	if err != nil {
		return empty, unavailable(err)
	}
	// This requests the complete set at once. Outer composite commands must
	// precollect the same lower-order locks before acquiring any later resource.
	if err = s.acquireMutationLocks(ctx, tx, p.command, p.ref); err != nil {
		return empty, unavailable(err)
	}
	if err = s.authorize(ctx, tx, p.actor, p.scope, identity.Mutate); err != nil {
		return empty, err
	}
	saved, found, err := s.findReceipt(ctx, e, p)
	if err != nil {
		return empty, err
	}
	if found {
		return saved, nil
	}
	if err = s.writable(); err != nil {
		return empty, err
	}
	if err = s.mutationGate(ctx, tx, p.actor, p.ref); err != nil {
		return empty, err
	}
	version, epoch, err := s.control(ctx, e)
	if err != nil {
		return empty, err
	}
	if version != p.version || epoch != p.epoch {
		return empty, failure(EpochChanged, foundation.InvalidState, nil)
	}
	result := sc.MutationResult{Metadata: sc.Metadata{CredentialRef: p.ref, Purpose: p.purpose, Version: 1}, Deleted: p.kind == sc.Delete}
	var oldPayload payloadID
	var old sc.Metadata
	if p.kind != sc.Create {
		old, oldPayload, err = loadMetadata(ctx, e, p.ref)
		if err != nil {
			return empty, err
		}
		if old.Version != p.expected {
			return empty, failure(Conflict, foundation.VersionConflict, nil)
		}
		result.Metadata.Version = old.Version + 1
		if result.Metadata.Version.Validate() != nil {
			return empty, failure(Conflict, foundation.VersionConflict, nil)
		}
	}
	if p.kind == sc.Delete || p.kind == sc.Update && p.purpose != old.Purpose {
		var busy bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1) OR EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released)`, p.ref.Details().ID.String()).Scan(&busy); err != nil {
			return empty, unavailable(err)
		}
		if busy {
			return empty, failure(Busy, foundation.ResourceBusy, nil)
		}
	}
	if p.kind != sc.Delete {
		if err = insertPayload(ctx, e, p.value); err != nil {
			return empty, err
		}
	}
	switch p.kind {
	case sc.Create:
		_, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secrets(id,scope,project_id,purpose,version,current_payload_id) VALUES($1,$2,$3,$4,1,$5)`, p.ref.Details().ID.String(), string(p.scope.Details().Kind), null(p.scope.Details().ProjectID), string(p.purpose), p.value.id.String())
	case sc.Update:
		_, err = e.Exec(ctx, `UPDATE agenteam_secret.secrets SET purpose=$2,version=$3,current_payload_id=$4 WHERE id=$1`, p.ref.Details().ID.String(), string(p.purpose), int64(result.Metadata.Version), p.value.id.String())
	case sc.Delete:
		// Released owner tombstones support retry while the credential exists;
		// they carry no retention after its authorized permanent deletion.
		if _, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND released`, p.ref.Details().ID.String()); err != nil {
			return empty, unavailable(err)
		}
		_, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secrets WHERE id=$1`, p.ref.Details().ID.String())
	}
	if err != nil {
		return empty, unavailable(err)
	}
	if oldPayload.Validate() == nil {
		if _, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, oldPayload.String()); err != nil {
			return empty, unavailable(err)
		}
	}
	if err = insertPayload(ctx, e, p.receipt); err != nil {
		return empty, err
	}
	if _, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secret_command_receipts(id,scope,project_id,command_digest,mutation_kind,credential_id,purpose,result_version,deleted,digest_payload_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.receiptID.String(), string(p.scope.Details().Kind), null(p.scope.Details().ProjectID), string(p.commandDigest), string(p.kind), p.ref.Details().ID.String(), string(p.purpose), int64(result.Metadata.Version), result.Deleted, p.receipt.id.String()); err != nil {
		return empty, unavailable(err)
	}
	action := ac.SecretCreate
	changed := []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged}
	if p.kind == sc.Update {
		action = ac.SecretUpdate
		changed = []ac.ChangedField{ac.ValueChanged}
		if p.purpose != old.Purpose {
			changed = append(changed, ac.PurposeChanged)
		}
	}
	if p.kind == sc.Delete {
		action = ac.SecretDelete
		changed = nil
	}
	metadata, err := ac.SecretMutationMetadata(action, result.Metadata.Version, changed)
	if err != nil {
		return empty, unavailable(err)
	}
	resource, _ := ac.NewResource(ac.SecretResource, p.ref.Details().ID.String())
	entry, err := ac.NewEntry(ac.EntryFields{Scope: p.scope, Actor: p.actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return empty, unavailable(err)
	}
	key, err := audit.CommandAppendKey(ac.SecretProducer, p.command, 0)
	if err != nil {
		return empty, unavailable(err)
	}
	if _, err = state.audit.AppendInTx(ctx, tx, entry, key); err != nil {
		return empty, unavailable(err)
	}
	return result, nil
}
func (s *Service) findReceipt(ctx context.Context, e postgres.SQLExecutor, p preparedWrite) (sc.MutationResult, bool, error) {
	var id, refID, purpose, payload string
	var version int64
	var deleted bool
	err := e.QueryRow(ctx, `SELECT id::text,credential_id::text,purpose,result_version,deleted,digest_payload_id::text FROM agenteam_secret.secret_command_receipts WHERE scope=$1 AND scope_key=$2 AND command_digest=$3`, string(p.scope.Details().Kind), scopeKey(p.scope), string(p.commandDigest)).Scan(&id, &refID, &purpose, &version, &deleted, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.MutationResult{}, false, nil
	}
	if err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	payloadID, err := foundation.ParseID[payloadMarker](payload)
	if err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	sealed, err := loadPayload(ctx, e, payloadID)
	if err != nil {
		return sc.MutationResult{}, false, err
	}
	if sealed.ownerKind != receiptOwner || sealed.ownerID != id || !sealed.scope.Equal(p.scope) {
		return sc.MutationResult{}, false, failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	digest, err := openEnvelope(s.state().keys, sealed)
	if err != nil {
		return sc.MutationResult{}, false, err
	}
	defer clear(digest)
	expected, err := openEnvelope(s.state().keys, p.receipt)
	if err != nil {
		return sc.MutationResult{}, false, err
	}
	defer clear(expected)
	if subtle.ConstantTimeCompare(digest, expected) != 1 {
		return sc.MutationResult{}, false, failure(Conflict, foundation.IdempotencyKeyReused, nil)
	}
	refUUID, err := foundation.ParseID[sc.Credential](refID)
	if err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	ref, err := sc.NewCredentialRef(refUUID, p.scope)
	v := foundation.Version(version)
	if err != nil || v.Validate() != nil || !sc.Purpose(purpose).Valid() {
		return sc.MutationResult{}, false, unavailable(err)
	}
	return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: sc.Purpose(purpose), Version: v}, Deleted: deleted}, true, nil
}

type MutationLookup struct {
	Observed bool               `json:"observed"`
	Result   *sc.MutationResult `json:"result,omitempty"`
}

func (s *Service) LookupWrite(ctx context.Context, prepared PreparedWrite) (MutationLookup, error) {
	if prepared.data == nil {
		return MutationLookup{}, invalid()
	}
	p := prepared.data()
	if err := s.authorize(ctx, foundation.Tx{}, p.actor, p.scope, identity.Read); err != nil {
		return MutationLookup{}, err
	}
	result, found, err := s.findReceipt(ctx, s.state().store, p)
	if err != nil {
		return MutationLookup{}, err
	}
	if !found {
		return MutationLookup{}, nil
	}
	return MutationLookup{Observed: true, Result: &result}, nil
}
func (s *Service) Metadata(ctx context.Context, actor identity.Actor, ref sc.CredentialRef) (sc.Metadata, error) {
	if ref.Validate() != nil {
		return sc.Metadata{}, invalid()
	}
	if err := s.authorize(ctx, foundation.Tx{}, actor, ref.Details().Scope, identity.Read); err != nil {
		return sc.Metadata{}, err
	}
	metadata, _, err := loadMetadata(ctx, s.state().store, ref)
	return metadata, err
}
