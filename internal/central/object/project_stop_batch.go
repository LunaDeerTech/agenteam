package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type stopBatchBuilder struct {
	service     *Service
	ctx         context.Context
	x           postgres.SQLExecutor
	project     string
	facts       projectStopFacts
	seen        map[string]bool
	projections map[string]json.RawMessage
	processes   map[oc.ProcessID]bool
}

// Each lane selects current pending primary records only. Expansion follows
// fixed native pointers, never every historical record for an Object.
func (s *Service) projectStopFacts(ctx context.Context, x postgres.SQLExecutor, cause oc.ProjectStopCause, lane int, after string) (projectStopFacts, error) {
	if lane < 0 || lane > 4 {
		return projectStopFacts{}, invalid()
	}
	d := cause.Details()
	b := &stopBatchBuilder{service: s, ctx: ctx, x: x, project: d.ProjectID.String(), seen: map[string]bool{}, projections: map[string]json.RawMessage{}, processes: map[oc.ProcessID]bool{}}
	key, _ := foundation.ProjectLock(b.project)
	b.facts.locks = append(b.facts.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	queries := []string{
		`SELECT r.id::text FROM agenteam_object.project_work r WHERE r.project_id=$1 AND r.joined_at IS NULL AND ($3 OR r.kind IN ('preparation','transfer_put','verification','cleanup'))`,
		`SELECT r.id::text FROM agenteam_object.objects r WHERE r.project_id=$1 AND EXISTS(SELECT 1 FROM agenteam_object.uploads u WHERE u.object_id=r.id AND u.disposition='reserved') AND ($3 OR NOT $3)`,
		`SELECT r.id::text FROM agenteam_object.object_leases r JOIN agenteam_object.objects o ON o.id=r.object_id LEFT JOIN agenteam_object.object_transfers t ON t.lease_id=r.id WHERE o.project_id=$1 AND r.state='active' AND ($3 OR r.owner_kind='writer' OR r.owner_kind='transfer' AND t.direction='put')`,
		`SELECT r.id::text FROM agenteam_download.grants r WHERE r.project_id=$1 AND $3 AND NOT r.revoked`,
		`SELECT r.id::text FROM agenteam_object.object_transfers r WHERE r.project_id=$1 AND ($3 OR r.direction='put') AND (r.revoked_at IS NULL OR r.retirement_evidence IS NULL OR EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.id=r.lease_id AND l.state='active'))`,
	}
	ids, err := queryStopIDs(ctx, x, queries[lane]+` AND ($2::uuid IS NULL OR r.id>$2) ORDER BY r.id LIMIT 32`, b.project, null(after), d.Action == oc.ProjectStopDelete)
	if err != nil {
		return b.facts, err
	}
	if len(ids) > stopBatchLimit {
		return b.facts, unavailable(nil)
	}
	b.facts.ids = ids
	for _, id := range ids {
		switch lane {
		case 0:
			err = b.addWork(id)
		case 1:
			err = b.addObject(id)
		case 2:
			err = b.addLease(id)
		case 3:
			err = b.addGrant(id, false)
		case 4:
			err = b.addTransfer(id)
		}
		if err != nil {
			return b.facts, err
		}
	}
	for i := range b.facts.locks {
		b.facts.locks[i].Mode = foundation.Exclusive
	}
	b.facts.locks, err = oc.NormalizeAccessLocks(b.facts.locks)
	if err != nil {
		return b.facts, err
	}
	lockProjection := make([][]string, 0, len(b.facts.locks))
	for _, lock := range b.facts.locks {
		lockProjection = append(lockProjection, []string{lock.Key.Canonical(), string(lock.Mode)})
	}
	for process := range b.processes {
		b.facts.processes = append(b.facts.processes, process)
	}
	sort.Slice(b.facts.processes, func(i, j int) bool { return b.facts.processes[i].String() < b.facts.processes[j].String() })
	for _, ids := range [][]string{b.facts.objects, b.facts.attempts, b.facts.leases, b.facts.grants, b.facts.transfers} {
		sort.Strings(ids)
	}
	raw, err := json.Marshal([]any{lane, after, ids, b.facts.objects, b.facts.attempts, b.facts.leases, b.facts.grants, b.facts.transfers, b.projections, workProjection(b.facts.works), lockProjection, b.facts.overflow})
	if err != nil {
		return b.facts, invalid()
	}
	hash := sha256.Sum256(raw)
	b.facts.binding = newDigest(hash[:])
	return b.facts, nil
}

func (b *stopBatchBuilder) first(table, id string) (bool, error) {
	if _, err := foundation.ParseID[struct{}](id); err != nil {
		return false, unavailable(err)
	}
	key := table + ":" + id
	if b.seen[key] {
		return false, nil
	}
	b.seen[key] = true
	// Fixed pointers can add at most a small constant per primary. This is a
	// structural assertion, not a history threshold that can strand a Project.
	if len(b.seen) > stopBatchLimit*16 {
		return false, unavailable(nil)
	}
	return true, nil
}

func (b *stopBatchBuilder) projectRow(table, id string, optional bool) (bool, error) {
	switch table {
	case "agenteam_object.objects", "agenteam_object.uploads", "agenteam_object.upload_attempts", "agenteam_object.object_leases", "agenteam_object.cleanup_operations", "agenteam_object.object_transfers", "agenteam_download.grants", "agenteam_download.attempts":
	default:
		return false, invalid()
	}
	var raw []byte
	err := b.x.QueryRow(b.ctx, `SELECT to_jsonb(r) FROM `+table+` r WHERE id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		if !optional {
			b.facts.overflow = true
		}
		b.projections[table+":"+id] = nil
		return false, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	b.projections[table+":"+id] = json.RawMessage(raw)
	return true, nil
}

func (b *stopBatchBuilder) addObject(raw string) error {
	first, err := b.first("object", raw)
	if err != nil || !first {
		return err
	}
	id, _ := foundation.ParseID[oc.StoredObject](raw)
	b.facts.objects = append(b.facts.objects, raw)
	key, _ := foundation.AggregateLock(foundation.ObjectAggregate, raw)
	b.facts.locks = append(b.facts.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	obj, found, err := loadObject(b.ctx, b.x, id)
	if err != nil {
		return err
	}
	if !found {
		b.facts.overflow = true
		return nil
	}
	if obj.meta.Scope.Details().ProjectID != b.project {
		return failure(foundation.InvalidState, nil)
	}
	if _, err = b.projectRow("agenteam_object.objects", raw, false); err != nil {
		return err
	}
	u, found, err := scanUpload(b.x.QueryRow(b.ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, raw))
	if err != nil {
		return err
	}
	if !found {
		b.facts.overflow = true
		return nil
	}
	if _, err = b.projectRow("agenteam_object.uploads", u.id.String(), false); err != nil {
		return err
	}
	command, err := commandIdentity(u.owner, u.command)
	if err != nil {
		return err
	}
	key, _ = foundation.CommandLock(command)
	b.facts.locks = append(b.facts.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	return nil
}

func (b *stopBatchBuilder) addAttempt(raw string) error {
	first, err := b.first("attempt", raw)
	if err != nil || !first {
		return err
	}
	id, _ := foundation.ParseID[oc.Attempt](raw)
	a, found, err := loadAttempt(b.ctx, b.x, id)
	if err != nil {
		return err
	}
	if !found {
		b.facts.overflow = true
		return nil
	}
	if _, err = b.projectRow("agenteam_object.upload_attempts", raw, false); err != nil {
		return err
	}
	b.facts.attempts = append(b.facts.attempts, raw)
	if err = b.addObject(a.object.String()); err != nil {
		return err
	}
	if a.kind == "private_candidate" {
		if !a.closed {
			b.processes[a.process] = true
		}
		// Writer ownership is unique per Object/attempt, including released
		// history. There is no scan of all leases belonging to the Object.
		var lease string
		err = b.x.QueryRow(b.ctx, `SELECT id::text FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='writer' AND owner_id=$2`, a.object.String(), raw).Scan(&lease)
		if errors.Is(err, pgx.ErrNoRows) {
			b.facts.overflow = true
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		return b.addLease(lease)
	}
	return b.addTransfer(a.transfer.String())
}

func (b *stopBatchBuilder) addLease(raw string) error {
	first, err := b.first("lease", raw)
	if err != nil || !first {
		return err
	}
	var object, kind, owner, process, attempt, state string
	err = b.x.QueryRow(b.ctx, `SELECT object_id::text,owner_kind,owner_id::text,COALESCE(process_id::text,''),COALESCE(attempt_id::text,''),state FROM agenteam_object.object_leases WHERE id=$1`, raw).Scan(&object, &kind, &owner, &process, &attempt, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		b.facts.overflow = true
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if _, err = b.projectRow("agenteam_object.object_leases", raw, false); err != nil {
		return err
	}
	b.facts.leases = append(b.facts.leases, raw)
	if err = b.addObject(object); err != nil {
		return err
	}
	key, err := foundation.RecordLock(foundation.ReferenceRecordLock, "object-lease:"+kind+":"+owner)
	if err != nil {
		return invalid()
	}
	b.facts.locks = append(b.facts.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	if process != "" && state == "active" {
		pid, err := foundation.ParseID[oc.Process](process)
		if err != nil {
			return unavailable(err)
		}
		b.processes[pid] = true
	}
	if kind == "writer" {
		return b.addAttempt(attempt)
	}
	if kind == "transfer" {
		return b.addTransfer(owner)
	}
	return nil
}

func (b *stopBatchBuilder) addGrant(raw string, optional bool) error {
	first, err := b.first("grant", raw)
	if err != nil || !first {
		return err
	}
	if found, err := b.projectRow("agenteam_download.grants", raw, optional); err != nil || !found {
		return err
	}
	var project, object string
	err = b.x.QueryRow(b.ctx, `SELECT project_id::text,object_id::text FROM agenteam_download.grants WHERE id=$1`, raw).Scan(&project, &object)
	if err != nil {
		return unavailable(err)
	}
	if project != b.project {
		return failure(foundation.InvalidState, nil)
	}
	id, _ := foundation.ParseID[oc.DownloadGrant](raw)
	b.facts.grants = append(b.facts.grants, raw)
	b.facts.locks = append(b.facts.locks, downloadGrantLock(id))
	return b.addObject(object)
}

func (b *stopBatchBuilder) addTransfer(raw string) error {
	first, err := b.first("transfer", raw)
	if err != nil || !first {
		return err
	}
	id, _ := foundation.ParseID[oc.Transfer](raw)
	t, found, err := loadTransfer(b.ctx, b.x, id)
	if err != nil {
		return err
	}
	if !found {
		b.facts.overflow = true
		return nil
	}
	if t.spec.Details().Owner.Details().ProjectID != b.project {
		return failure(foundation.InvalidState, nil)
	}
	if _, err = b.projectRow("agenteam_object.object_transfers", raw, false); err != nil {
		return err
	}
	b.facts.transfers = append(b.facts.transfers, raw)
	request, err := t.request(t.actor, oc.TransferInspect, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return err
	}
	facts, err := b.service.transferAccessFacts(b.ctx, b.x, request.Details().Transfer)
	if err != nil {
		return err
	}
	b.facts.locks = append(b.facts.locks, facts.locks...)
	if err = b.addObject(t.object.String()); err != nil {
		return err
	}
	for _, aid := range []oc.AttemptID{t.staging, t.candidate} {
		if aid.Validate() == nil {
			if err = b.addAttempt(aid.String()); err != nil {
				return err
			}
		}
	}
	for _, lid := range []oc.LeaseID{t.lease, t.sourceLease} {
		if lid.Validate() == nil {
			if err = b.addLease(lid.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *stopBatchBuilder) addWork(raw string) error {
	w, err := loadProjectWork(b.ctx, b.x, raw)
	if err != nil {
		return err
	}
	if w == nil {
		return accessChanged()
	}
	if w.project.String() != b.project {
		return failure(foundation.InvalidState, nil)
	}
	b.facts.works = append(b.facts.works, *w)
	locks, err := b.service.projectWorkLocks(b.ctx, b.x, *w, nil)
	if err != nil {
		return err
	}
	b.facts.locks = append(b.facts.locks, locks...)
	r := b.service.state()
	r.mu.Lock()
	if h := r.projectWork[raw]; h != nil {
		b.facts.locks = append(b.facts.locks, h.locks...)
	}
	r.mu.Unlock()
	if w.object.Validate() == nil {
		matches, err := b.nativeWorkMatches(*w)
		if err != nil {
			return err
		}
		if !matches {
			b.facts.overflow = true
			return nil
		}
		if err = b.addObject(w.object.String()); err != nil {
			return err
		}
	}
	switch w.kind {
	case "preparation":
		if w.object.Validate() != nil {
			// Original Prepare is registered before there is any native row.
			// Retirement still requires actual return/death and original locks.
			if w.resource != w.id {
				b.facts.overflow = true
			}
			return nil
		}
		return b.addAttempt(w.resource)
	case "verification":
		return b.addAttempt(w.resource)
	case "reader", "source":
		return b.addLease(w.resource)
	case "transfer_put", "transfer_get":
		return b.addTransfer(w.resource)
	case "cleanup":
		found, err := b.projectRow("agenteam_object.cleanup_operations", w.resource, false)
		if err != nil || !found {
			return err
		}
		var attempt string
		if err = b.x.QueryRow(b.ctx, `SELECT attempt_id::text FROM agenteam_object.cleanup_operations WHERE id=$1`, w.resource).Scan(&attempt); err != nil {
			return unavailable(err)
		}
		return b.addAttempt(attempt)
	case "download":
		if w.object.Validate() != nil {
			return b.addGrant(w.resource, true)
		}
		found, err := b.projectRow("agenteam_download.attempts", w.resource, false)
		if err != nil || !found {
			return err
		}
		var grant string
		if err = b.x.QueryRow(b.ctx, `SELECT grant_id::text FROM agenteam_download.attempts WHERE id=$1`, w.resource).Scan(&grant); err != nil {
			return unavailable(err)
		}
		return b.addGrant(grant, false)
	default:
		return unavailable(nil)
	}
}

func (b *stopBatchBuilder) nativeWorkMatches(w projectWork) (bool, error) {
	var query string
	args := []any{w.resource, w.object.String()}
	switch w.kind {
	case "preparation":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE id=$1 AND object_id=$2 AND kind='private_candidate' AND process_id=$3)`
		args = append(args, w.process.String())
	case "verification":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE id=$1 AND object_id=$2 AND kind='private_candidate')`
	case "reader", "source":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$2 AND process_id=$3 AND owner_kind=$4)`
		args = append(args, w.process.String(), w.kind)
	case "cleanup":
		// A superseded old worker is still a real lifetime. Its actual return
		// is required independently of the current cleanup claim's worker/fence.
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE id=$1 AND object_id=$2)`
	case "transfer_put", "transfer_get":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers WHERE id=$1 AND object_id=$2 AND direction=$3)`
		direction := "get"
		if w.kind == "transfer_put" {
			direction = "put"
		}
		args = append(args, direction)
	case "download":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_download.attempts a JOIN agenteam_download.grants g ON g.id=a.grant_id WHERE a.id=$1 AND g.object_id=$2)`
	default:
		return false, invalid()
	}
	var matches bool
	err := b.x.QueryRow(b.ctx, query, args...).Scan(&matches)
	return matches, unavailableIf(err)
}
