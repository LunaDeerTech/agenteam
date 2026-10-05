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

const stopBatchLimit = 100
const stopFanoutLimit = 1000

type projectStopRow struct {
	action, state, after string
	version              int64
	lane                 int
}

type projectStopFacts struct {
	ids, objects []string
	works        []projectWork
	locks        []foundation.LockRequest
	binding      foundation.Digest
	overflow     bool
}

// A plan is private to one Service and carries both the authority's mapping
// and our exact bounded native mapping. Neither input is an authorization.
type projectStopPlan struct {
	service      *Service
	request      oc.ProjectStopRequest
	dependencies oc.AccessDependencies
	lane         int
	after        string
	facts        projectStopFacts
	locks        []foundation.LockRequest
}

func readProjectStop(ctx context.Context, e postgres.SQLExecutor, cause oc.ProjectStopCause) (*projectStopRow, error) {
	d := cause.Details()
	var r projectStopRow
	err := e.QueryRow(ctx, `SELECT action,project_version,state,scan_kind,COALESCE(scan_after::text,'') FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2`, d.ProjectID.String(), d.OperationID.String()).Scan(&r.action, &r.version, &r.state, &r.lane, &r.after)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if r.action != string(d.Action) || r.version != int64(d.ProjectVersion) {
		return nil, failure(foundation.InvalidState, nil)
	}
	return &r, nil
}

func stopWorkRelevant(kind string, action oc.ProjectStopAction) bool {
	return action == oc.ProjectStopDelete || kind == "preparation" || kind == "transfer_put" || kind == "verification" || kind == "cleanup"
}

func queryStopIDs(ctx context.Context, e postgres.SQLExecutor, query string, args ...any) ([]string, error) {
	rows, err := e.Query(ctx, query, args...)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, unavailable(err)
		}
		ids = append(ids, id)
	}
	return ids, unavailableIf(rows.Err())
}

func (s *Service) discoverProjectStop(ctx context.Context, request oc.ProjectStopRequest, lane int, after string) (projectStopPlan, error) {
	p := projectStopPlan{service: s, request: request, lane: lane, after: after}
	var err error
	p.dependencies, err = s.state().auth.ProjectStop.DiscoverProjectStop(ctx, request)
	if err != nil {
		return p, portError(err)
	}
	if p.dependencies.Validate() != nil {
		return p, unavailable(nil)
	}
	p.facts, err = s.projectStopFacts(ctx, s.state().store, request.Details().Cause, lane, after)
	if err != nil {
		return p, err
	}
	p.locks, err = oc.NormalizeAccessLocks(append(p.dependencies.Locks(), p.facts.locks...))
	return p, err
}

func (s *Service) projectStopFacts(ctx context.Context, e postgres.SQLExecutor, cause oc.ProjectStopCause, lane int, after string) (projectStopFacts, error) {
	var f projectStopFacts
	if lane < 0 || lane > 4 {
		return f, invalid()
	}
	d := cause.Details()
	project := d.ProjectID.String()
	key, _ := foundation.ProjectLock(project)
	f.locks = append(f.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	tables := []string{"agenteam_object.project_work", "agenteam_object.objects", "agenteam_object.object_leases", "agenteam_download.grants", "agenteam_object.object_transfers"}
	from := tables[lane] + " r"
	where := "r.project_id=$1"
	if lane == 2 {
		from += " JOIN agenteam_object.objects o ON o.id=r.object_id"
		where = "o.project_id=$1"
	}
	ids, err := queryStopIDs(ctx, e, `SELECT r.id::text FROM `+from+` WHERE `+where+` AND ($2::uuid IS NULL OR r.id>$2) ORDER BY r.id LIMIT 100`, project, null(after))
	if err != nil {
		return f, err
	}
	f.ids = ids
	objectSet := map[string]bool{}
	if lane == 1 {
		for _, id := range ids {
			objectSet[id] = true
		}
	} else if len(ids) > 0 {
		objects, err := queryStopIDs(ctx, e, `SELECT DISTINCT object_id::text FROM `+tables[lane]+` WHERE id=ANY($1::uuid[]) AND object_id IS NOT NULL ORDER BY object_id`, ids)
		if err != nil {
			return f, err
		}
		for _, id := range objects {
			objectSet[id] = true
		}
	}
	for id := range objectSet {
		f.objects = append(f.objects, id)
	}
	sort.Strings(f.objects)
	// Row projections are only a mapping fingerprint. A bounded projection is
	// never used as evidence that no pending relationship exists.
	var projections []string
	for _, table := range []string{"agenteam_object.objects", "agenteam_object.uploads", "agenteam_object.upload_attempts", "agenteam_object.object_leases", "agenteam_object.cleanup_operations", "agenteam_download.grants", "agenteam_object.object_transfers"} {
		column := "object_id"
		if table == "agenteam_object.objects" {
			column = "id"
		}
		rows, err := queryStopIDs(ctx, e, `SELECT row_to_json(r)::text FROM `+table+` r WHERE `+column+`=ANY($1::uuid[]) ORDER BY id LIMIT 1001`, f.objects)
		if err != nil {
			return f, err
		}
		if len(rows) > stopFanoutLimit {
			f.overflow = true
		}
		projections = append(projections, rows...)
	}
	workIDs, err := queryStopIDs(ctx, e, `SELECT id::text FROM agenteam_object.project_work WHERE project_id=$1 AND (id=ANY($2::uuid[]) OR object_id=ANY($3::uuid[])) ORDER BY id LIMIT 1001`, project, ids, f.objects)
	if err != nil {
		return f, err
	}
	if len(workIDs) > stopFanoutLimit {
		f.overflow = true
	}
	for _, id := range workIDs {
		w, err := loadProjectWork(ctx, e, id)
		if err != nil {
			return f, err
		}
		if w == nil {
			return f, accessChanged()
		}
		f.works = append(f.works, *w)
		locks, err := s.projectWorkLocks(ctx, e, *w, nil)
		if err != nil {
			return f, err
		}
		f.locks = append(f.locks, locks...)
		s.state().mu.Lock()
		h := s.state().projectWork[id]
		if h != nil {
			f.locks = append(f.locks, h.locks...)
		}
		s.state().mu.Unlock()
	}
	for _, raw := range f.objects {
		var actualProject *string
		err := e.QueryRow(ctx, `SELECT project_id::text FROM agenteam_object.objects WHERE id=$1`, raw).Scan(&actualProject)
		if errors.Is(err, pgx.ErrNoRows) {
			for _, w := range f.works {
				if w.object.String() == raw && !w.joined {
					f.overflow = true
				}
			}
		} else if err != nil {
			return f, unavailable(err)
		} else if actualProject == nil || *actualProject != project {
			return f, failure(foundation.InvalidState, nil)
		}
		_, err = foundation.ParseID[oc.StoredObject](raw)
		if err != nil {
			return f, unavailable(err)
		}
		key, _ := foundation.AggregateLock(foundation.ObjectAggregate, raw)
		f.locks = append(f.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
		u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, raw))
		if err != nil {
			return f, err
		}
		if found {
			c, err := commandIdentity(u.owner, u.command)
			if err != nil {
				return f, err
			}
			k, _ := foundation.CommandLock(c)
			f.locks = append(f.locks, foundation.LockRequest{Key: k, Mode: foundation.Exclusive})
		}
	}

	// Include every related original writer mutex, even when it was reached from
	// another primary lane (for example a local work pointing at a remote grant).
	grants, err := queryStopIDs(ctx, e, `SELECT id::text FROM agenteam_download.grants WHERE project_id=$1 AND (id=ANY($2::uuid[]) OR object_id=ANY($3::uuid[])) ORDER BY id LIMIT 1001`, project, ids, f.objects)
	if err != nil {
		return f, err
	}
	if len(grants) > stopFanoutLimit {
		f.overflow = true
	}
	for _, raw := range grants {
		id, _ := foundation.ParseID[oc.DownloadGrant](raw)
		f.locks = append(f.locks, downloadGrantLock(id))
	}
	attempts, err := queryStopIDs(ctx, e, `SELECT row_to_json(a)::text FROM agenteam_download.attempts a WHERE grant_id=ANY($1::uuid[]) ORDER BY id LIMIT 1001`, grants)
	if err != nil {
		return f, err
	}
	if len(attempts) > stopFanoutLimit {
		f.overflow = true
	}
	projections = append(projections, attempts...)
	transfers, err := queryStopIDs(ctx, e, `SELECT id::text FROM agenteam_object.object_transfers WHERE project_id=$1 AND (id=ANY($2::uuid[]) OR object_id=ANY($3::uuid[])) ORDER BY id LIMIT 1001`, project, ids, f.objects)
	if err != nil {
		return f, err
	}
	if len(transfers) > stopFanoutLimit {
		f.overflow = true
	}
	for _, raw := range transfers {
		id, _ := foundation.ParseID[oc.Transfer](raw)
		r, ok, err := loadTransfer(ctx, e, id)
		if err != nil {
			return f, err
		}
		if !ok {
			return f, accessChanged()
		}
		request, err := r.request(r.actor, oc.TransferInspect, nil, nil, oc.PreparedPayload{})
		if err != nil {
			return f, err
		}
		facts, err := s.transferAccessFacts(ctx, e, request.Details().Transfer)
		if err != nil {
			return f, err
		}
		f.locks = append(f.locks, facts.locks...)
	}

	for i := range f.locks {
		f.locks[i].Mode = foundation.Exclusive
	}
	raw, err := json.Marshal([]any{lane, after, ids, f.objects, projections, workProjection(f.works), f.overflow})
	if err != nil {
		return f, invalid()
	}
	sum := sha256.Sum256(raw)
	f.binding = newDigest(sum[:])
	return f, nil
}

func workProjection(works []projectWork) [][]any {
	out := make([][]any, 0, len(works))
	for _, w := range works {
		out = append(out, []any{w.id, w.project.String(), w.process.String(), w.kind, w.resource, w.object.String(), w.epoch, w.epochOperation, w.revoked, w.fence, w.joined})
	}
	return out
}

func (s *Service) projectStopTransaction(ctx context.Context, p projectStopPlan, fn func(context.Context, foundation.Tx, postgres.SQLExecutor, oc.ProjectStopAuthorization, projectStopFacts) error) foundation.CommitResult {
	if p.service != s || p.request.Validate() != nil {
		return rejectedAccess(invalid())
	}
	return s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks); err != nil {
			return unavailable(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		a, err := s.state().auth.ProjectStop.ValidateProjectStopInTx(ctx, tx, p.request, p.dependencies)
		if err != nil {
			return portError(err)
		}
		if !a.Matches(tx, p.request, p.dependencies) {
			return failure(foundation.Forbidden, nil)
		}
		f, err := s.projectStopFacts(ctx, e, p.request.Details().Cause, p.lane, p.after)
		if err != nil {
			return err
		}
		if f.binding != p.facts.binding {
			return accessChanged()
		}
		return fn(ctx, tx, e, a, f)
	})
}

// Full existence predicates, rather than bounded diagnostics or cursor state,
// establish completion under the current Project EX gate.
func projectStopPending(ctx context.Context, e postgres.SQLExecutor, cause oc.ProjectStopCause) (bool, error) {
	d := cause.Details()
	var pending bool
	err := e.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.project_id=$1 AND w.joined_at IS NULL AND ($2 OR w.kind IN ('preparation','transfer_put','verification','cleanup')))
 OR EXISTS(SELECT 1 FROM agenteam_object.uploads u WHERE u.project_id=$1 AND u.disposition='reserved')
 OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a JOIN agenteam_object.objects o ON o.id=a.object_id WHERE o.project_id=$1 AND a.kind='private_candidate' AND NOT a.io_closed)
 OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c JOIN agenteam_object.objects o ON o.id=c.object_id WHERE o.project_id=$1 AND c.phase='applying' AND NOT EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.id=c.worker_id AND w.kind='cleanup' AND w.resource_id=c.id AND w.cleanup_claim_fence=c.fence AND w.joined_at IS NOT NULL))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_leases l JOIN agenteam_object.objects o ON o.id=l.object_id LEFT JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE o.project_id=$1 AND l.state='active' AND ($2 OR l.owner_kind='writer' OR l.owner_kind='transfer' AND t.direction='put'))
 OR ($2 AND EXISTS(SELECT 1 FROM agenteam_download.grants g WHERE g.project_id=$1 AND (NOT g.revoked OR EXISTS(SELECT 1 FROM agenteam_download.attempts a WHERE a.grant_id=g.id AND a.phase='started' AND (a.pending_phase IS NULL OR NOT EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.kind='download' AND w.resource_id=a.id AND w.joined_at IS NOT NULL))))))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.project_id=$1 AND ($2 OR t.direction='put') AND (t.revoked_at IS NULL OR l.state='active' OR t.retirement_evidence IS NULL))`, d.ProjectID.String(), d.Action == oc.ProjectStopDelete).Scan(&pending)
	return pending, unavailableIf(err)
}
