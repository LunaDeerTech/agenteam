package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This private database wire is written only by the original INSERT's live
// transaction. A successful replay never creates or repairs provenance.
type executionRetirementOrigin struct {
	Schema    int64  `json:"schema"`
	Kind      string `json:"kind"`
	Execution string `json:"execution_id"`
	Subject   string `json:"subject_id"`
	Related   string `json:"related_id"`
	Version   int64  `json:"version"`
	Digest    string `json:"digest"`
}

type executionRetirementSubject struct{}
type executionRetirementCallOrigin struct {
	ID      f.ID[projectCallID]
	Present bool
	Result  executionRetirementOrigin
}

func (o executionRetirementOrigin) valid(lane projectCallLane) bool {
	if o.Schema != 1 || o.Version < 1 || f.Digest(o.Digest).Validate() != nil {
		return false
	}
	if _, err := f.ParseID[i.Execution](o.Execution); err != nil {
		return false
	}
	if _, err := f.ParseID[executionRetirementSubject](o.Subject); err != nil {
		return false
	}
	switch o.Kind {
	case "created":
		return lane == projectLaneService && o.Subject == o.Execution && o.Related == "" && o.Version == 1
	case "preparation":
		return lane == projectLanePreparation && o.Related == ""
	case "snapshot":
		_, err := f.ParseID[executionRetirementSubject](o.Related)
		return lane == projectLaneDirect && o.Version >= 2 && err == nil
	}
	return false
}

func bindRetirementOriginInTx(ctx context.Context, tx f.Tx, store Store, project i.ProjectID, agent i.AgentID, launch f.Digest, origin executionRetirementOrigin) error {
	if ctx == nil {
		return invalid()
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok {
		return nil // Legacy constructors keep the pre-84 business path.
	}
	if w.state == nil || w.entry == nil {
		return fault(f.DependencyUnbound)
	}
	s, e := w.state, w.entry
	s.mu.Lock()
	enabled := s.retirementOrigins
	r := e.record
	valid := s.owners[r.ID] == e && !e.bodyReturned && !e.settling && e.registrationKnown && e.activeMethod == e.identity.Command()
	s.mu.Unlock()
	if !enabled {
		return nil
	}
	if !valid || !sameProjectCallPort(s.store, store) || !origin.valid(r.Lane) || !retirementOriginInvocation(r, project, agent, origin.Execution, launch) {
		return fault(f.Forbidden)
	}
	if err := requireProjectCallInTx(ctx, tx, store); err != nil {
		return err
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	raw, err := json.Marshal(origin)
	if err != nil {
		return unavailable(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.project_calls SET original_result=$8::jsonb WHERE id=$1::uuid AND project_id=$2::uuid AND process_id=$3::uuid AND instance_id=$4::uuid AND lane=$5 AND invocation_identity=$6 AND invocation_digest=$7 AND phase='running' AND original_result IS NULL`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), r.Identity, r.Digest.String(), raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func retirementOriginInvocation(call projectCallRecord, project i.ProjectID, agent i.AgentID, execution string, launch f.Digest) bool {
	var identity struct {
		Invocation struct {
			Namespace string   `json:"namespace"`
			Owners    []string `json:"owner_ids"`
			Command   string   `json:"command"`
			Key       string   `json:"key"`
		} `json:"invocation"`
		Digest f.Digest `json:"request_digest"`
	}
	if json.Unmarshal([]byte(call.Identity), &identity) != nil || call.Project != project || identity.Digest != launch {
		return false
	}
	v := identity.Invocation
	if v.Namespace != "execution" {
		return false
	}
	if call.Lane == projectLaneService {
		return v.Command == "launch" && len(v.Owners) == 2 && v.Owners[0] == project.String() && v.Owners[1] == agent.String()
	}
	method := "prepare"
	if call.Lane == projectLaneDirect {
		method = "start"
	} else if call.Lane != projectLanePreparation {
		return false
	}
	return v.Command == method && len(v.Owners) == 4 && v.Owners[0] == project.String() && v.Owners[1] == execution && v.Owners[2] == agent.String() && v.Key == execution
}

// The old Stop projection deliberately remains unchanged: it also runs on
// schema 83. Only the explicit retirement consumer reads the new field.
func readRetirementOrigins(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, calls []projectCallRecord) ([]executionRetirementCallOrigin, error) {
	var raw []byte
	if err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'original_result',original_result) ORDER BY id),'[]'::jsonb) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid`, project.String()).Scan(&raw); err != nil {
		return nil, portError(err)
	}
	var wire []struct {
		ID     f.ID[projectCallID] `json:"id"`
		Result json.RawMessage     `json:"original_result"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire == nil || len(wire) != len(calls) {
		return nil, unavailable(nil)
	}
	result := make([]executionRetirementCallOrigin, len(wire))
	for n, row := range wire {
		if row.ID != calls[n].ID {
			return nil, fault(f.VersionConflict)
		}
		result[n].ID = row.ID
		if bytes.Equal(row.Result, []byte("null")) {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(row.Result, &fields) != nil || len(fields) != 7 {
			return nil, unavailable(nil)
		}
		for _, value := range fields {
			if bytes.Equal(value, []byte("null")) {
				return nil, unavailable(nil)
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(row.Result))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result[n].Result) != nil || decoder.Decode(new(any)) != io.EOF || !result[n].Result.valid(calls[n].Lane) {
			return nil, unavailable(nil)
		}
		result[n].Present = true
	}
	return result, ctx.Err()
}

func requireOriginalExecution(before *executionRetirementBefore, calls []projectCallRecord, origins []executionRetirementCallOrigin) error {
	if len(calls) != len(origins) {
		return fault(f.ResourceBusy)
	}
	s := before.record.summary
	created := executionRetirementOrigin{1, "created", s.ID.String(), s.ID.String(), "", 1, before.record.digest.String()}
	key, _ := f.NewCommandIdentity("execution", []string{s.ProjectID.String(), s.AgentID.String()}, "launch", before.record.launch.Meta.IdempotencyKey)
	expected, _ := json.Marshal(struct {
		Invocation json.RawMessage `json:"invocation"`
		Digest     string          `json:"request_digest"`
	}{json.RawMessage(key.Canonical()), before.record.digest.String()})
	found := false
	for n, row := range calls {
		if origins[n].ID == row.ID && origins[n].Present && origins[n].Result == created && row.Phase == "business_closed" && row.Lane == projectLaneService && row.Identity == string(expected) {
			found = true
			break
		}
	}
	if !found {
		return fault(f.ResourceBusy)
	}
	for n, table := range retirementHistoryTables {
		lane := projectLanePreparation
		switch table {
		case "preparation_attempts", "preparation_claims", "preparation_inputs":
		case "snapshots":
			lane = projectLaneDirect
		default:
			continue
		}
		var rows []struct {
			Project   string `json:"project_id"`
			Agent     string `json:"agent_id"`
			Process   string `json:"process_id"`
			Execution string `json:"execution_id"`
			Attempt   string `json:"attempt_id"`
			Fence     int64  `json:"fence"`
			Snapshot  string `json:"id"`
			Start     string `json:"start_id"`
			Version   int64  `json:"started_version"`
			Digest    string `json:"digest"`
		}
		if json.Unmarshal(before.history[n], &rows) != nil {
			return unavailable(nil)
		}
		for _, row := range rows {
			if row.Project != s.ProjectID.String() || row.Agent != s.AgentID.String() || row.Execution != s.ID.String() {
				return unavailable(nil)
			}
			origin := executionRetirementOrigin{1, "preparation", row.Execution, row.Attempt, "", row.Fence, before.record.digest.String()}
			if lane == projectLaneDirect {
				origin = executionRetirementOrigin{1, "snapshot", row.Execution, row.Snapshot, row.Start, row.Version, row.Digest}
			}
			matched := false
			for m, call := range calls {
				if call.Lane == lane && call.Process.String() == row.Process && call.Phase == "business_closed" && origins[m].ID == call.ID && origins[m].Present && origins[m].Result == origin && retirementOriginInvocation(call, s.ProjectID, s.AgentID, row.Execution, before.record.digest) {
					matched = true
					break
				}
			}
			if !matched {
				return fault(f.ResourceBusy)
			}
		}
	}
	return nil
}
