package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type deleteNode struct {
	Node    kc.ScopeNode `json:"node"`
	Object  oc.ObjectID  `json:"object"`
	Upload  oc.UploadID  `json:"upload"`
	Cleanup oc.CleanupID `json:"cleanup"`
	Event   ev.Header    `json:"event"`
}
type deletePlan struct {
	Format  int           `json:"format"`
	Project id.ProjectID  `json:"project"`
	Root    kc.DocumentID `json:"root"`
	Nodes   []deleteNode  `json:"nodes"`
}

func (p deletePlan) scope() []kc.ScopeNode {
	out := make([]kc.ScopeNode, len(p.Nodes))
	for j, n := range p.Nodes {
		out[j] = n.Node
	}
	return out
}
func (p deletePlan) validate() error {
	if p.Format != 1 || p.Project.Validate() != nil || p.Root.Validate() != nil || len(p.Nodes) == 0 {
		return internal(nil)
	}
	if _, err := kc.SubtreeDigest(p.Project, p.Root, p.scope()); err != nil {
		return internal(err)
	}
	cleanup := make(map[oc.CleanupID]bool, len(p.Nodes))
	events := make(map[ev.EventID]bool, len(p.Nodes))
	for j, n := range p.Nodes {
		h := n.Event
		if n.Node.Status != kc.Active || n.Object.Validate() != nil || n.Upload.Validate() != nil || n.Cleanup.Validate() != nil || cleanup[n.Cleanup] || events[h.EventID] || j > 0 && p.Nodes[j-1].Node.ID.String() >= n.Node.ID.String() || h.Validate() != nil || h.Scope.Kind != ev.ProjectScope || h.Scope.ProjectID.String() != p.Project.String() || h.EventType != kc.DeletedEvent || h.SchemaVersion != 1 || h.AggregateType != kc.KnowledgeAggregate || h.AggregateID.String() != n.Node.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != n.Node.ContentVersion || h.AggregateSequence != nil {
			return internal(nil)
		}
		cleanup[n.Cleanup], events[h.EventID] = true, true
	}
	return nil
}
func (p deletePlan) mapping() (f.Digest, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", internal(err)
	}
	return cursor.Digest(raw)
}
func loadDeletePlan(ctx context.Context, x postgres.SQLExecutor, record *commandRecord) (deletePlan, error) {
	var out deletePlan
	var raw []byte
	if err := x.QueryRow(ctx, `SELECT plan FROM agenteam_knowledge.commands WHERE project_id=$1 AND id=$2`, record.project.String(), record.id.String()).Scan(&raw); err != nil {
		return out, unavailable(err)
	}
	if len(raw) == 0 || len(raw) > 4<<20 {
		return out, internal(nil)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return out, internal(err)
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.Project != record.project || out.Root != record.document {
		return out, internal(nil)
	}
	return out, out.validate()
}

func newDeletePlan(project id.ProjectID, root kc.DocumentID, rows []documentRow, at f.Instant) (deletePlan, error) {
	p := deletePlan{Format: 1, Project: project, Root: root, Nodes: make([]deleteNode, len(rows))}
	projectID, err := f.ParseID[ev.Project](project.String())
	if err != nil {
		return p, internal(err)
	}
	for j, row := range rows {
		if row.head.Active == nil {
			return p, internal(nil)
		}
		doc := *row.head.Active
		cleanup, err := f.NewID[oc.CleanupOperation]()
		if err != nil {
			return p, unavailable(err)
		}
		eventID, err := f.NewID[ev.EventIdentity]()
		if err != nil {
			return p, unavailable(err)
		}
		aggregate, err := f.ParseID[ev.Aggregate](doc.ID.String())
		if err != nil {
			return p, internal(err)
		}
		version := doc.ContentVersion
		p.Nodes[j] = deleteNode{scopeNode(doc), doc.ObjectID, row.upload, cleanup, ev.Header{EventID: eventID, EventType: kc.DeletedEvent, SchemaVersion: 1, OccurredAt: at, Scope: ev.Scope{Kind: ev.ProjectScope, ProjectID: projectID}, AggregateType: kc.KnowledgeAggregate, AggregateID: aggregate, AggregateVersion: &version}}
	}
	return p, p.validate()
}

func (s *Service) validateDeleteScope(ctx context.Context, x postgres.SQLExecutor, request kc.TreeMutation) ([]documentRow, error) {
	d := request.Details()
	rows, nodes, err := loadSubtree(ctx, x, d.ProjectID, d.DocumentID)
	if err != nil {
		return nil, err
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return nil, err
	}
	claims, err := s.state().deps.Confirmations.Verify(*d.Delete, now)
	if err != nil {
		return nil, portError(err)
	}
	digest, err := kc.SubtreeDigest(d.ProjectID, d.DocumentID, nodes)
	if err != nil {
		return nil, internal(err)
	}
	if claims.UserID.String() != d.Actor.Details().UserID || claims.ProjectID != d.ProjectID || claims.RootID != d.DocumentID || claims.ScopeDigest != digest {
		return nil, fault(f.VersionConflict)
	}
	return rows, nil
}

// DiscoverMutation may persist the original delete plan, but never deletes a
// document. Final execution rechecks current identity, token, scope and plans.
func (s *Service) DiscoverMutation(ctx context.Context, request kc.TreeMutation) (kc.MutationPlan, error) {
	if request.Validate() != nil {
		return kc.MutationPlan{}, fault(f.InvalidArgument)
	}
	if request.Details().Command == kc.Move {
		return s.discoverMove(ctx, request)
	}
	if request.Details().Command != kc.DeleteSubtree {
		return kc.MutationPlan{}, fault(f.InvalidArgument)
	}
	d := request.Details()
	if err := readInput(ctx, d.Actor, d.ProjectID); err != nil {
		return kc.MutationPlan{}, err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	defer done()
	semantic, err := kc.DeleteDigest(d.Actor, d.Meta, d.ProjectID, d.DocumentID, *d.Delete)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	locks, err := mutationLocks(request)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	identity, err := kc.CommandIdentity(d.ProjectID, kc.DeleteSubtree, d.Meta.IdempotencyKey)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.MutationPlan{}, portError(err)
	}
	var record *commandRecord
	var planned deletePlan
	var mapping f.Digest
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.readScope(ctx, tx, d.Actor, d.ProjectID)
		if err != nil {
			return err
		}
		record, err = loadCommand(ctx, x, d.ProjectID, kc.DeleteSubtree, d.Meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if record != nil {
			if record.digest != semantic || record.user.String() != d.Actor.Details().UserID {
				return fault(f.IdempotencyKeyReused)
			}
			if record.state == kc.Committed {
				mapping = ob.DigestBytes([]byte("knowledge.completed:" + record.id.String() + ":" + semantic.String()))
				return nil
			}
		}
		grant, err := s.state().deps.Projects.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, id.Mutate)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(d.Actor, d.ProjectID) {
			return internal(nil)
		}
		rows, err := s.validateDeleteScope(ctx, x, request)
		if err != nil {
			return err
		}
		if record != nil {
			planned, err = loadDeletePlan(ctx, x, record)
			if err != nil {
				return err
			}
			if !deleteRowsMatch(planned, rows) {
				return fault(f.VersionConflict)
			}
			mapping, err = planned.mapping()
			return err
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		planned, err = newDeletePlan(d.ProjectID, d.DocumentID, rows, now)
		if err != nil {
			return err
		}
		mapping, err = planned.mapping()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(planned)
		if err != nil {
			return internal(err)
		}
		if len(raw) > 4<<20 {
			return fault(f.ResourceBusy)
		}
		commandID, err := f.NewID[command]()
		if err != nil {
			return unavailable(err)
		}
		user, err := f.ParseID[id.User](d.Actor.Details().UserID)
		if err != nil {
			return internal(err)
		}
		requestRaw, err := json.Marshal(struct {
			Mapping f.Digest `json:"mapping"`
		}{mapping})
		if err != nil {
			return internal(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.commands
 (id,project_id,document_id,actor_user_id,command_name,command_key,semantic_digest,request,plan,state,created_at)
 VALUES($1,$2,$3,$4,'delete-subtree',$5,$6,$7::jsonb,$8::jsonb,'planned',$9)`, commandID.String(), d.ProjectID.String(), d.DocumentID.String(), user.String(), string(d.Meta.IdempotencyKey), semantic.String(), requestRaw, raw, now.Time())
		if err != nil {
			return unavailable(err)
		}
		record = &commandRecord{id: commandID, project: d.ProjectID, document: d.DocumentID, user: user, name: kc.DeleteSubtree, key: d.Meta.IdempotencyKey, digest: semantic, state: kc.InProgress, created: now}
		for _, node := range planned.Nodes {
			event, err := s.state().deps.Events.Deleted(node.Event, kc.DeletedPayload{DocumentID: node.Node.ID, ContentVersion: node.Node.ContentVersion})
			if err != nil {
				return portError(err)
			}
			header, err := event.HeaderJSON()
			if err != nil {
				return internal(err)
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.command_events(id,project_id,command_id,document_id,header,payload,event_type)
 VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)`, node.Event.EventID.String(), d.ProjectID.String(), commandID.String(), node.Node.ID.String(), header, event.PayloadBytes(), string(kc.DeletedEvent))
			if err != nil {
				return unavailable(err)
			}
		}
		return nil
	})
	if err = txError(result); err != nil {
		return kc.MutationPlan{}, err
	}
	details := kc.MutationPlanDetails{Request: request, DomainMapping: mapping, Locks: locks}
	if record.state != kc.Committed {
		for _, node := range planned.Nodes {
			owner, err := oc.NewObjectOwner(oc.Knowledge, node.Node.ID.String(), d.ProjectID.String())
			if err != nil {
				return kc.MutationPlan{}, internal(err)
			}
			cleanup, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: node.Cleanup, Owner: owner, Reason: oc.OwnerDeleted})
			if err != nil {
				return kc.MutationPlan{}, internal(err)
			}
			access, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cleanup, ObjectID: node.Object, UploadID: node.Upload})
			if err != nil {
				return kc.MutationPlan{}, internal(err)
			}
			objectPlan, err := s.state().deps.Objects.DiscoverAccess(ctx, access)
			if err != nil {
				return kc.MutationPlan{}, portError(err)
			}
			event, err := s.state().deps.Events.Deleted(node.Event, kc.DeletedPayload{DocumentID: node.Node.ID, ContentVersion: node.Node.ContentVersion})
			if err != nil {
				return kc.MutationPlan{}, portError(err)
			}
			eventPlan, err := s.state().deps.Outbox.PrepareAppend(ctx, d.Actor, event)
			if err != nil {
				return kc.MutationPlan{}, portError(err)
			}
			details.ObjectPlans = append(details.ObjectPlans, objectPlan)
			details.EventPlans = append(details.EventPlans, eventPlan)
			details.Locks = append(details.Locks, objectPlan.Details().Locks...)
			details.Locks = append(details.Locks, eventPlan.Locks()...)
		}
	}
	return kc.NewMutationPlan(s.state().issuer, details)
}

func deleteRowsMatch(plan deletePlan, rows []documentRow) bool {
	if len(plan.Nodes) != len(rows) {
		return false
	}
	for j, row := range rows {
		if row.head.Active == nil {
			return false
		}
		n, d := plan.Nodes[j], *row.head.Active
		if n.Node.ID != d.ID || n.Node.ProjectID != d.ProjectID || n.Node.ContentVersion != d.ContentVersion || n.Node.Status != d.Status || n.Object != d.ObjectID || n.Upload != row.upload {
			return false
		}
		if (n.Node.ParentID == nil) != (d.ParentDocumentID == nil) || n.Node.ParentID != nil && *n.Node.ParentID != *d.ParentDocumentID {
			return false
		}
	}
	return true
}

var _ kc.TreeMutationPlanning = (*Service)(nil)

func mutationLocks(request kc.TreeMutation) ([]f.LockRequest, error) {
	if request.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	d := request.Details()
	locks, err := scopeLocks(d.Actor, d.ProjectID, true)
	if err != nil {
		return nil, err
	}
	identity, err := kc.CommandIdentity(d.ProjectID, d.Command, d.Meta.IdempotencyKey)
	if err != nil {
		return nil, portError(err)
	}
	key, err := f.CommandLock(identity)
	if err != nil {
		return nil, portError(err)
	}
	return ob.NormalizeLocks(append(locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
}

func (s *Service) discoverMove(ctx context.Context, request kc.TreeMutation) (kc.MutationPlan, error) {
	if request.Validate() != nil || request.Details().Command != kc.Move {
		return kc.MutationPlan{}, fault(f.InvalidArgument)
	}
	d := request.Details()
	if err := readInput(ctx, d.Actor, d.ProjectID); err != nil {
		return kc.MutationPlan{}, err
	}
	semantic, err := kc.MoveDigest(d.Actor, d.Meta, d.ProjectID, d.DocumentID, *d.Move)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	var mapping f.Digest
	err = s.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadCommand(ctx, x, d.ProjectID, kc.Move, d.Meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if row != nil {
			if row.digest != semantic || row.user.String() != d.Actor.Details().UserID {
				return fault(f.IdempotencyKeyReused)
			}
			if row.state == kc.Committed {
				mapping = ob.DigestBytes([]byte("knowledge.completed:" + row.id.String() + ":" + semantic.String()))
				return nil
			}
		}
		_, facts, err := moveFacts(ctx, x, d.ProjectID, d.DocumentID, *d.Move)
		if err != nil {
			return err
		}
		if _, err = kc.CheckMove(d.ProjectID, d.DocumentID, *d.Move, facts); err != nil {
			return err
		}
		mapping, err = moveMapping(facts)
		return err
	})
	if err != nil {
		return kc.MutationPlan{}, err
	}
	locks, err := mutationLocks(request)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	return kc.NewMutationPlan(s.state().issuer, kc.MutationPlanDetails{Request: request, DomainMapping: mapping, Locks: locks})
}

func (s *Service) AcquireMutationInTx(ctx context.Context, tx f.Tx, plan kc.MutationPlan, extra []f.LockRequest) (kc.LockedMutation, error) {
	st := s.state()
	if st == nil {
		return kc.LockedMutation{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || !plan.IssuedBy(st.issuer) {
		return kc.LockedMutation{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.LockedMutation{}, err
	}
	defer done()
	if _, err = st.store.InTx(tx); err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	union, err := oc.NormalizeAccessLocks(append(plan.Locks(), extra...))
	if err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	var access oc.LockedAccess
	if len(plan.Details().ObjectPlans) > 0 {
		access, err = st.deps.Objects.AcquireAccessPlansInTx(ctx, tx, plan.Details().ObjectPlans, union)
	} else {
		err = st.store.AcquireAll(ctx, tx, union)
	}
	if err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, union); err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	return kc.NewLockedMutation(st.issuer, kc.LockedMutationDetails{Tx: tx, Plan: plan, ExtraLocks: extra, ObjectAccess: access})
}
