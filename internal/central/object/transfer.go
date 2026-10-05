package object

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type transferState struct {
	objects      *Service
	authority    oc.RunnerTransferAuthority
	backend      *Backend
	pendingJoins map[oc.AttemptID]oc.UploadAttempt // guarded by objects.state().mu
}
type TransferService struct{ data func() *transferState }

func (t *TransferService) state() *transferState { return t.data() }
func NewTransferService(objects *Service, authority oc.RunnerTransferAuthority, endpoint TransferEndpoint) (*TransferService, error) {
	if objects == nil || objects.data == nil || endpoint.data == nil {
		return nil, invalid()
	}
	if !nilPort(authority) {
		p, ok := objects.state().auth.Planner.(*transferPlanner)
		if !ok || !sameTransferAuthority(p.authority, authority) {
			return nil, invalid()
		}
	}
	backend, err := NewBackend(endpoint.data())
	if err != nil {
		return nil, err
	}
	state := objects.state()
	state.mu.Lock()
	if state.stopped || state.forced || state.transferBackend != nil {
		state.mu.Unlock()
		_ = backend.Close()
		return nil, failure(foundation.InvalidState, nil)
	}
	state.transferBackend = backend
	state.mu.Unlock()
	r := &transferState{objects: objects, authority: authority, backend: backend, pendingJoins: make(map[oc.AttemptID]oc.UploadAttempt)}
	out := &TransferService{func() *transferState { return r }}
	state.mu.Lock()
	state.transfers = out
	state.mu.Unlock()
	return out, nil
}
func sameTransferAuthority(a, b oc.RunnerTransferAuthority) bool {
	if nilPort(a) || nilPort(b) {
		return nilPort(a) && nilPort(b)
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Comparable() && x.Interface() == y.Interface()
}
func (t TransferService) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_transfer_service")
}
func (t TransferService) MarshalJSON() ([]byte, error) {
	return []byte(`"object_transfer_service"`), nil
}
func (*TransferService) UnmarshalJSON([]byte) error { return invalid() }
func (t TransferService) LogValue() slog.Value      { return slog.StringValue("object_transfer_service") }

func (t *TransferService) IssueTransfer(ctx context.Context, actor identity.Actor, command foundation.CommandMeta, spec oc.TransferSpec) (oc.TransferGrant, error) {
	if actor.Validate() != nil || actor.Details().Kind == identity.Service || command.Validate() != nil || spec.Validate() != nil {
		return oc.TransferGrant{}, invalid()
	}
	if nilPort(t.state().authority) {
		return oc.TransferGrant{}, failure(foundation.DependencyUnbound, nil)
	}
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	defer done()
	ctx = op.ctx
	d := spec.Details()
	identityKey, err := transferCommand(d.Owner, command.IdempotencyKey)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	semantic, err := transferSemantic(actor, command, spec)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	r, found, err := loadTransferCommand(ctx, s.state().store, identityKey)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	if !found {
		r = transferRow{actor: actor, stable: stableActor(actor), issue: command, spec: spec, manifest: d.Manifest, digest: semantic, phase: "issued", version: 1, leaseActive: true}
		r.id, err = foundation.NewID[oc.Transfer]()
		if err != nil {
			return oc.TransferGrant{}, unavailable(err)
		}
		r.lease, err = foundation.NewID[oc.Lease]()
		if err != nil {
			return oc.TransferGrant{}, unavailable(err)
		}
		if d.Direction == oc.TransferPUT {
			r.object, err = foundation.NewID[oc.StoredObject]()
			if err != nil {
				return oc.TransferGrant{}, unavailable(err)
			}
			r.upload, err = foundation.NewID[oc.Upload]()
			if err != nil {
				return oc.TransferGrant{}, unavailable(err)
			}
			r.staging, err = foundation.NewID[oc.Attempt]()
			if err != nil {
				return oc.TransferGrant{}, unavailable(err)
			}
			u, err := commandIdentity(d.Owner, d.UploadCommand.IdempotencyKey)
			if err != nil {
				return oc.TransferGrant{}, err
			}
			prior, yes, err := loadCommand(ctx, s.state().store, u)
			if err != nil {
				return oc.TransferGrant{}, err
			}
			if yes {
				r.object = prior.object
				r.upload = prior.id
			}
		} else {
			r.object = d.ObjectID
			meta, err := s.StatObject(ctx, actor, d.Owner, d.ObjectID)
			if err != nil {
				return oc.TransferGrant{}, err
			}
			r.manifest, err = oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: meta.MediaType, Length: int64(meta.ByteSize), SHA256: meta.SHA256})
			if err != nil {
				return oc.TransferGrant{}, err
			}
		}
	}
	kind := "transfer_get"
	if d.Direction == oc.TransferPUT {
		kind = "transfer_put"
	}
	work, err := s.newProjectWork(ctx, workProject(d.Owner), kind, r.id.String(), r.object)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	ctx = context.WithValue(ctx, projectWorkContextKey{}, work)
	request, err := r.request(actor, oc.TransferIssue, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.TransferGrant{}, err
	}
	var extra []oc.AccessRequest
	if d.Direction == oc.TransferGET {
		extra = append(extra, transferReadRequest(actor, d.Owner, r.object))
		leaseOwner, _ := oc.NewLeaseOwner(oc.TransferOwner, r.id.String())
		q, _ := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.AcquireUseAccess, Actor: actor, ObjectID: r.object, LeaseOwner: leaseOwner})
		extra = append(extra, q)
	}
	result := t.within(ctx, request, extra, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, authority oc.TransferAuthorization) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		prior, exists, err := loadTransferCommand(ctx, e, identityKey)
		if err != nil {
			return err
		}
		if exists {
			if prior.id != r.id || prior.object != r.object {
				return accessChanged()
			}
			if prior.stable != stableActor(actor) {
				return failure(foundation.Forbidden, nil)
			}
			if !bytes.Equal(prior.digest, semantic) {
				return failure(foundation.IdempotencyKeyReused, nil)
			}
			if err = t.checkActive(ctx, tx, prior, actor); err != nil {
				return err
			}
			r = prior
			return nil
		}
		if found {
			return accessChanged()
		}
		var outstanding int64
		if err = e.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_transfers t WHERE EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.id=t.lease_id AND l.state='active') OR t.direction='put' AND EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a WHERE a.id=t.staging_id AND a.phase<>'cleaned')`).Scan(&outstanding); err != nil {
			return unavailable(err)
		}
		if outstanding >= 32 {
			return failure(foundation.ResourceBusy, nil)
		}
		ad := authority.Details()
		r.runnerGeneration = ad.RunnerGeneration
		r.operationVersion = ad.OperationVersion
		r.execution = ad.ExecutionID
		if command.ExpectedVersion != nil && *command.ExpectedVersion != ad.OperationVersion {
			return failure(foundation.VersionConflict, nil)
		}
		if d.Direction == oc.TransferGET {
			if _, err = s.authorize(ctx, tx, actor, d.Owner, identity.Read); err != nil {
				return err
			}
			if err = s.gate(ctx, tx, actor, d.Owner, identity.Mutate); err != nil {
				return err
			}
			obj, err := s.readRow(ctx, tx, actor, d.Owner, r.object)
			if err != nil {
				return err
			}
			m := r.manifest.Details()
			if obj.meta.MediaType != m.MediaType || int64(obj.meta.ByteSize) != m.Length || obj.meta.SHA256 != m.SHA256 {
				return accessChanged()
			}
			r.key = obj.key
			leaseOwner, _ := oc.NewLeaseOwner(oc.TransferOwner, r.id.String())
			lease, err := s.AcquireLeaseInTx(ctx, tx, actor, r.object, leaseOwner, plans[2], locked)
			if err != nil {
				return err
			}
			r.lease = lease.ID
		} else {
			if _, err = t.reserveTransferPUTInTx(ctx, tx, transferBinding{row: r, actor: actor}, r.manifest, plans[0], locked); err != nil {
				return err
			}
		}
		if err = e.QueryRow(ctx, `SELECT date_trunc('second',clock_timestamp())+($1*interval '1 second')`, d.ExpiresInSeconds).Scan(&r.expires); err != nil {
			return unavailable(err)
		}
		if err = t.insert(ctx, tx, r); err != nil {
			return err
		}
		return t.appendAudit(ctx, tx, r, ac.ObjectTransferIssue)
	})
	if err = commitError(result); err != nil {
		return oc.TransferGrant{}, err
	}
	return t.material(ctx, actor, r)
}

func (t *TransferService) checkActive(ctx context.Context, tx foundation.Tx, r transferRow, actor identity.Actor) error {
	if r.revoked || r.phase == "complete" || r.phase == "failed" || !r.leaseActive {
		return failure(foundation.InvalidState, nil)
	}
	e, err := executor(t.state().objects, tx)
	if err != nil {
		return err
	}
	var valid bool
	if err = e.QueryRow(ctx, `SELECT clock_timestamp()<$1`, r.expires).Scan(&valid); err != nil {
		return unavailable(err)
	}
	if !valid {
		return failure(foundation.InvalidState, nil)
	}
	intent := identity.Mutate
	if r.spec.Details().Direction == oc.TransferGET {
		intent = identity.Read
	}
	if _, err = t.state().objects.authorize(ctx, tx, actor, r.spec.Details().Owner, intent); err != nil {
		return err
	}
	if r.spec.Details().Direction == oc.TransferGET {
		// Issuing fresh material must still select a currently readable exact
		// object. A transfer lease alone cannot grant fixed-version read access.
		if _, err = t.state().objects.readRow(ctx, tx, actor, r.spec.Details().Owner, r.object); err != nil {
			return err
		}
	}
	return t.state().objects.gate(ctx, tx, actor, r.spec.Details().Owner, identity.Mutate)
}
func (t *TransferService) material(ctx context.Context, actor identity.Actor, r transferRow) (oc.TransferGrant, error) {
	material, err := t.sign(ctx, r)
	if err != nil {
		return oc.TransferGrant{}, err
	}
	request, err := r.request(actor, oc.TransferMaterialize, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.TransferGrant{}, err
	}
	var extras []oc.AccessRequest
	if r.spec.Details().Direction == oc.TransferGET {
		extras = append(extras, transferReadRequest(actor, r.spec.Details().Owner, r.object))
	}
	result := t.within(ctx, request, extras, func(ctx context.Context, tx foundation.Tx, _ []oc.AccessLockPlan, _ oc.LockedAccess, a oc.TransferAuthorization) error {
		e, err := executor(t.state().objects, tx)
		if err != nil {
			return err
		}
		current, ok, err := loadTransfer(ctx, e, r.id)
		if err != nil {
			return err
		}
		if !ok {
			return failure(foundation.NotFound, nil)
		}
		if current.stable != stableActor(actor) {
			return failure(foundation.Forbidden, nil)
		}
		if current.runnerGeneration != a.Details().RunnerGeneration || current.operationVersion != a.Details().OperationVersion || current.execution != a.Details().ExecutionID {
			return failure(foundation.Forbidden, nil)
		}
		return t.checkActive(ctx, tx, current, actor)
	})
	if err = commitError(result); err != nil {
		return oc.TransferGrant{}, err
	}
	m := r.manifest.Details()
	return oc.TransferGrant{Status: r.status(), MediaType: m.MediaType, ByteSize: foundation.Progress(m.Length), SHA256: m.SHA256, Material: material}, nil
}
func (t *TransferService) InspectTransfer(ctx context.Context, actor identity.Actor, id oc.TransferID) (oc.TransferStatusView, error) {
	if actor.Validate() != nil || id.Validate() != nil {
		return oc.TransferStatusView{}, invalid()
	}
	if nilPort(t.state().authority) {
		return oc.TransferStatusView{}, failure(foundation.DependencyUnbound, nil)
	}
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	defer done()
	ctx = op.ctx
	r, ok, err := loadTransfer(ctx, s.state().store, id)
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	if !ok {
		return oc.TransferStatusView{}, failure(foundation.NotFound, nil)
	}
	request, err := r.request(actor, oc.TransferInspect, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	result := t.within(ctx, request, nil, func(ctx context.Context, tx foundation.Tx, _ []oc.AccessLockPlan, _ oc.LockedAccess, _ oc.TransferAuthorization) error {
		if stableActor(actor) != r.stable {
			return failure(foundation.Forbidden, nil)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var exists bool
		r, exists, err = loadTransfer(ctx, e, id)
		if err != nil {
			return err
		}
		if !exists {
			return failure(foundation.NotFound, nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return oc.TransferStatusView{}, err
	}
	return r.status(), nil
}

func (t *TransferService) insert(ctx context.Context, tx foundation.Tx, r transferRow) error {
	e, err := executor(t.state().objects, tx)
	if err != nil {
		return err
	}
	d := r.spec.Details()
	o := d.Owner.Details()
	m := r.manifest.Details()
	command, err := transferCommand(d.Owner, r.issue.IdempotencyKey)
	if err != nil {
		return err
	}
	actor, err := json.Marshal(r.actor.Details())
	if err != nil {
		return invalid()
	}
	var uploadRequest, uploadKey, uploadExpected any
	if d.UploadCommand != nil {
		uploadRequest = d.UploadCommand.RequestID.String()
		uploadKey = string(d.UploadCommand.IdempotencyKey)
		uploadExpected = optionalVersion(d.UploadCommand.ExpectedVersion)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_transfers(id,issue_hash,issue_key,issue_request_id,issue_expected,semantic_digest,actor_json,stable_actor,project_id,owner_kind,owner_id,runner_id,operation_id,runner_generation,operation_version,execution_id,direction,object_id,upload_id,upload_request_id,upload_key,upload_expected,staging_id,lease_id,media_type,byte_size,sha256,candidate_key,duration_seconds,expires_at,phase) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,'issued')`, r.id.String(), commandHash(command), string(r.issue.IdempotencyKey), r.issue.RequestID.String(), optionalVersion(r.issue.ExpectedVersion), r.digest, actor, r.stable, o.ProjectID, string(o.Kind), o.ID, d.RunnerID.String(), d.OperationID.String(), int64(r.runnerGeneration), int64(r.operationVersion), r.execution.String(), string(d.Direction), r.object.String(), optionalTransferID(r.upload), uploadRequest, uploadKey, uploadExpected, optionalTransferID(r.staging), r.lease.String(), m.MediaType, m.Length, digestBytes(m.SHA256), null(r.key), d.ExpiresInSeconds, r.expires)
	return unavailableIf(err)
}
func (t *TransferService) appendAudit(ctx context.Context, tx foundation.Tx, r transferRow, action ac.Action) error {
	return appendTransferAudit(ctx, t.state().objects, tx, r, action)
}
func appendTransferAudit(ctx context.Context, s *Service, tx foundation.Tx, r transferRow, action ac.Action) error {
	m := r.manifest.Details()
	a := r.actor.Details()
	initiator := a.UserID
	if a.Kind == identity.AgentRun {
		initiator = a.AgentID
	}
	media, _, err := mime.ParseMediaType(m.MediaType)
	if err != nil {
		return invalid()
	}
	phase := ac.IssuedPhase
	ordinal := int64(0)
	var sent foundation.Progress
	if action == ac.ObjectTransferComplete {
		phase = ac.SentPhase
		ordinal = 1
		sent = foundation.Progress(m.Length)
	}
	if action == ac.ObjectTransferRevoke {
		phase = ac.RevokedPhase
		ordinal = 2
	}
	metadata, err := ac.ObjectMetadata(action, ac.ObjectMetadataFields{ObjectID: r.object.String(), TransferID: r.id.String(), InitiatorKind: a.Kind, InitiatorID: initiator, InitiatorExecutionID: a.ExecutionID, MediaType: media, ByteSize: foundation.Progress(m.Length), SentBytes: sent, Phase: phase})
	if err != nil {
		return unavailable(err)
	}
	actor, err := s.state().registration.Actor(r.id.String(), r.spec.Details().Owner.Scope())
	if err != nil {
		return unavailable(err)
	}
	resource, err := ac.NewResource(ac.ObjectTransferResource, r.id.String())
	if err != nil {
		return unavailable(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: r.spec.Details().Owner.Scope(), Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return unavailable(err)
	}
	key, err := ac.NewAppendKey(ac.ObjectProducer, r.id.String(), ordinal)
	if err != nil {
		return unavailable(err)
	}
	ctx, err = s.projectAuditWitnessContext(ctx, tx, projectAuditWitness{entry: entry, key: key, transfer: transferAuditFact(r)})
	if err != nil {
		return err
	}
	_, err = s.state().audit.AppendInTx(ctx, tx, entry, key)
	if err != nil {
		return portError(err)
	}
	return nil
}
