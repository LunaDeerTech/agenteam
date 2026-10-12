package artifact

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type artifactCleanupWork struct {
	ID, Project, Process, Instance, Source string
	Joined, SourceUse                      bool
}
type artifactCleanupStop struct {
	Operation, Action, State, Process, Instance, After string
	Version, Scan                                      int64
}
type artifactCleanupCalls struct {
	Work  []artifactCleanupWork
	Stops []artifactCleanupStop
}
type artifactCleanupCallsKey struct{}
type artifactCleanupCallsProof struct {
	owner *projectCleanupCall
	state *projectCallOwnershipState
	dead  map[string]bool
}

// This reads only the Artifact owner's technical facts. Source-side rows are
// included for denial, never retired through a destination Project's grant.
func readArtifactCleanupCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (artifactCleanupCalls, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_object(
 'Work',COALESCE((SELECT jsonb_agg(jsonb_build_object('ID',id,'Project',project_id,'Process',process_id,'Instance',COALESCE(instance_id::text,''),'Source',COALESCE(source_project_id::text,''),'Joined',joined_at IS NOT NULL,'SourceUse',source_lease_id IS NOT NULL OR source_revoked_by IS NOT NULL OR source_joined_at IS NOT NULL) ORDER BY id) FROM agenteam_artifact.project_work WHERE project_id=$1 OR source_project_id=$1),'[]'::jsonb),
 'Stops',COALESCE((SELECT jsonb_agg(jsonb_build_object('Operation',operation_id,'Action',action,'State',state,'Process',COALESCE(owner_process_id::text,''),'Instance',COALESCE(owner_instance_id::text,''),'Version',project_version,'Scan',scan_kind,'After',COALESCE(scan_after::text,'')) ORDER BY operation_id) FROM agenteam_artifact.project_stops WHERE project_id=$1),'[]'::jsonb))`, project.String()).Scan(&raw)
	var facts artifactCleanupCalls
	if err != nil {
		return facts, unavailable(err)
	}
	if json.Unmarshal(raw, &facts) != nil || facts.Work == nil || facts.Stops == nil {
		return facts, unavailable(nil)
	}
	for n, work := range facts.Work {
		if !artifactTechnicalID(work.ID) || !artifactTechnicalID(work.Project) || !artifactTechnicalID(work.Process) ||
			work.Instance != "" && !artifactTechnicalID(work.Instance) || work.Source != "" && !artifactTechnicalID(work.Source) ||
			work.Project != project.String() && work.Source != project.String() || n > 0 && facts.Work[n-1].ID >= work.ID {
			return facts, unavailable(nil)
		}
	}
	for n, stop := range facts.Stops {
		if !artifactTechnicalID(stop.Operation) || stop.Version <= 0 ||
			(stop.Action != "archive" && stop.Action != "delete") || (stop.State != "stopped" && stop.State != "stopping") ||
			(stop.Process == "") != (stop.Instance == "") || stop.Process != "" && (!artifactTechnicalID(stop.Process) || !artifactTechnicalID(stop.Instance)) ||
			stop.After != "" && !artifactTechnicalID(stop.After) || n > 0 && facts.Stops[n-1].Operation >= stop.Operation {
			return facts, unavailable(nil)
		}
	}
	return facts, ctx.Err()
}

func artifactTechnicalID(raw string) bool { _, err := f.ParseID[struct{}](raw); return err == nil }

func (st *projectCallOwnershipState) cleanupCallsPending(project i.ProjectID) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stops[project] != nil {
		return true
	}
	for _, call := range st.owners {
		if call.record.Project == project {
			return true
		}
	}
	return false
}

// An original instance with no live/retained local call can consume its own
// joined rows. Neither a durable joined bit nor a same-process replacement
// instance substitutes for the original Store return.
func (st *projectCallOwnershipState) cleanupCallIdentities(facts artifactCleanupCalls, cause oc.ProjectCleanupDetails) (map[string]bool, bool) {
	foreign := map[string]bool{}
	if st.cleanupCallsPending(cause.ProjectID) {
		return foreign, false
	}
	identity := func(process, instance string) bool {
		if process == "" || instance == "" {
			return false
		}
		if process == st.process.String() {
			return instance == st.instance.String()
		}
		foreign[process] = true
		return true
	}
	current := false
	for _, stop := range facts.Stops {
		if stop.State != "stopped" || stop.Scan != 0 || stop.After != "" || !identity(stop.Process, stop.Instance) {
			return foreign, false
		}
		if stop.Operation == cause.OperationID.String() {
			if stop.Action != "delete" || stop.Version != int64(cause.Version) {
				return foreign, false
			}
			current = true
		} else if stop.Action != "archive" || stop.Version >= int64(cause.Version) {
			return foreign, false
		}
	}
	for _, work := range facts.Work {
		if work.Project != cause.ProjectID.String() || !work.Joined || work.Source != "" || work.SourceUse || !identity(work.Process, work.Instance) {
			return foreign, false
		}
	}
	// An explicitly owned composition requires its real current Delete stop
	// receipt even when there was no business work. Legacy compatibility is
	// handled separately and cannot provide evidence of a real Stop.
	return foreign, current
}

// The preliminary read must actually return before any foreign-process proof.
// Its Unknown is owned by the same original CleanupProject call as every other
// cleanup transaction; it is never retried inside this invocation.
func (s *Service) prepareCleanupCalls(ctx context.Context, actor i.Actor, cause oc.ProjectCleanupCause, call *projectCleanupCall) (context.Context, bool, error) {
	st := s.state().ownership
	if st == nil {
		return ctx, true, nil
	}
	var foreign map[string]bool
	ready := false
	result := s.withinCleanup(ctx, actor, cause, nil, nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, _ oc.LockedAccess) error {
		if err := st.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		facts, err := readArtifactCleanupCalls(ctx, x, cause.Details().ProjectID)
		if err != nil {
			return err
		}
		foreign, ready = st.cleanupCallIdentities(facts, cause.Details())
		return ctx.Err()
	})
	if err := projectCleanupResultError(result); err != nil {
		return ctx, false, err
	}
	if !ready {
		return ctx, false, nil
	}
	proof := &artifactCleanupCallsProof{owner: call, state: st, dead: map[string]bool{}}
	for raw := range foreign {
		process, _ := f.ParseID[oc.Process](raw)
		if err := st.proof.ConfirmStopped(ctx, process); err != nil {
			return ctx, false, nil
		}
		proof.dead[raw] = true
	}
	if err := ctx.Err(); err != nil {
		return ctx, false, unavailable(err)
	}
	return context.WithValue(ctx, artifactCleanupCallsKey{}, proof), true, nil
}

// All three Completed paths use this predicate. Only the mutation tail may
// retire rows; Unknown confirmation and completed replay remain read-only.
func (s *Service) cleanupCallsInTx(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, cause oc.ProjectCleanupCause, retire bool) (ready, clean bool, err error) {
	d := cause.Details()
	st := s.state().ownership
	if st != nil {
		if err = st.requireProcess(ctx, tx); err != nil {
			return false, false, portError(err)
		}
	}
	facts, err := readArtifactCleanupCalls(ctx, x, d.ProjectID)
	if err != nil {
		return false, false, err
	}
	if st == nil {
		empty := len(facts.Work) == 0 && len(facts.Stops) == 0
		return empty, empty, nil
	}
	foreign, ready := st.cleanupCallIdentities(facts, d)
	if !ready {
		return false, false, nil
	}
	proof, _ := ctx.Value(artifactCleanupCallsKey{}).(*artifactCleanupCallsProof)
	original, _ := ctx.Value(projectCleanupContextKey{}).(projectCleanupContext)
	for process := range foreign {
		if proof == nil || proof.state != st || proof.owner != original.call || !proof.dead[process] {
			return false, false, nil
		}
	}
	clean = len(facts.Work) == 0 && len(facts.Stops) <= 1
	if !retire || clean {
		return true, clean, nil
	}
	// The caller's original active private cleanup owner, not the public
	// cause or a proof-shaped value, covers this exact authorized transaction.
	if original.state != s.state() || original.call == nil || !s.state().originalCleanupCall(ctx, subject(original.call.actor, d.ProjectID), i.Lifecycle) || proof == nil || proof.owner != original.call || proof.state != st {
		return false, false, failure(f.Forbidden, nil)
	}
	ids := make([]string, 0, min(len(facts.Work), 100))
	for _, work := range facts.Work {
		if len(ids) == 100 {
			break
		}
		ids = append(ids, work.ID)
	}
	if len(ids) > 0 {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_artifact.project_work WHERE project_id=$1 AND id=ANY($2::uuid[]) AND joined_at IS NOT NULL AND source_project_id IS NULL AND source_lease_id IS NULL AND source_revoked_by IS NULL AND source_joined_at IS NULL`, d.ProjectID.String(), ids)
		if err != nil {
			return false, false, unavailable(err)
		}
		if tag.RowsAffected() != int64(len(ids)) {
			return false, false, failure(f.VersionConflict, nil)
		}
	}
	if len(facts.Work) > len(ids) {
		return true, false, ctx.Err()
	}
	old := make([]string, 0, len(facts.Stops))
	for _, stop := range facts.Stops {
		if stop.Operation != d.OperationID.String() {
			old = append(old, stop.Operation)
		}
	}
	if len(old) > 0 {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=ANY($2::uuid[]) AND action='archive' AND state='stopped' AND project_version<$3`, d.ProjectID.String(), old, int64(d.Version))
		if err != nil {
			return false, false, unavailable(err)
		}
		if tag.RowsAffected() != int64(len(old)) {
			return false, false, failure(f.VersionConflict, nil)
		}
	}
	return true, true, ctx.Err()
}
