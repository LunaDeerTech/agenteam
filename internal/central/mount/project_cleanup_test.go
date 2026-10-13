package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type mountProjectSnapshotStore struct {
	postgres.SQLExecutor
	project, agent string
	raw            []byte
}

func (s *mountProjectSnapshotStore) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	if !reflect.DeepEqual(args, []any{s.project, s.agent}) {
		return controlRow{err: fault(f.Forbidden)}
	}
	return controlRow{values: []any{bytes.Clone(s.raw)}}
}

// These snapshots are controlled logical rows, including historical definitions.
// They neither issue an Agent Applied witness nor model physical Runner data.
func mountProjectPureSnapshot(t *testing.T) (ac.ProjectRetirementChange, projectMountSnapshot) {
	t.Helper()
	_, _, _, change := retirementRefFixture(t)
	fields := change.Before.Fields()
	fields.AllowedMountIDs = []i.MountID{retirementRefID[i.Mount](t, 31), retirementRefID[i.Mount](t, 32)}
	fields.AllowedToolIDs = []i.ToolID{retirementRefID[i.Tool](t, 41)}
	fields.AllowedSecretVariableIDs = []i.ProjectVariableID{retirementRefID[i.ProjectVariable](t, 42)}
	var err error
	change.Before, err = ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	marshal := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	p, a := change.Request.ProjectID.String(), change.Request.AgentID.String()
	snapshot := projectMountSnapshot{
		Head:       marshal(map[string]any{"project_id": p, "agent_id": a, "owner_version": int64(change.Request.ExpectedVersion)}),
		References: []json.RawMessage{}, Workspaces: []json.RawMessage{}, Definitions: []json.RawMessage{},
	}
	for n := range 3 {
		id := retirementRefID[i.Mount](t, 31+n).String()
		workspace := fmt.Sprintf("data-%d", n)
		runner := retirementRefID[struct{}](t, 61).String()
		lifecycle := "active"
		if n == 2 {
			lifecycle = "removed"
		} else {
			snapshot.References = append(snapshot.References, marshal(map[string]any{"project_id": p, "agent_id": a, "mount_id": id}))
		}
		snapshot.Definitions = append(snapshot.Definitions, marshal(map[string]any{
			"id": id, "project_id": p, "agent_id": a, "runner_id": runner,
			"name": workspace, "description": "<original>&", "workspace": workspace, "lifecycle": lifecycle, "version": int64(3),
		}))
		snapshot.Workspaces = append(snapshot.Workspaces, marshal(map[string]any{
			"workspace_id": retirementRefID[struct{}](t, 51+n).String(), "mount_id": id,
			"project_id": p, "agent_id": a, "runner_id": runner, "workspace": workspace,
		}))
	}
	return change, snapshot
}

func mountProjectPureJSONField(t *testing.T, raw json.RawMessage, key string, value any) json.RawMessage {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields[key] = value
	changed, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestMountProjectCleanupExactConfiguration(t *testing.T) {
	for _, mode := range []string{"nonempty-before", "head-version", "allowlist", "mapping-runner", "mapping-workspace", "missing-definition", "other-project"} {
		t.Run(mode, func(t *testing.T) {
			change, snapshot := mountProjectPureSnapshot(t)
			switch mode {
			case "head-version":
				snapshot.Head = mountProjectPureJSONField(t, snapshot.Head, "owner_version", int64(change.Request.ExpectedVersion)+1)
			case "allowlist":
				fields := change.Before.Fields()
				fields.AllowedMountIDs = []i.MountID{}
				var err error
				change.Before, err = ac.NewAgentConfig(fields)
				if err != nil {
					t.Fatal(err)
				}
			case "mapping-runner":
				snapshot.Workspaces[0] = mountProjectPureJSONField(t, snapshot.Workspaces[0], "runner_id", retirementRefID[struct{}](t, 62).String())
			case "mapping-workspace":
				snapshot.Workspaces[0] = mountProjectPureJSONField(t, snapshot.Workspaces[0], "workspace", "another")
			case "missing-definition":
				snapshot.Definitions = snapshot.Definitions[1:]
			case "other-project":
				snapshot.References[0] = mountProjectPureJSONField(t, snapshot.References[0], "project_id", retirementRefID[i.Project](t, 70).String())
			}
			err := validateProjectMountSnapshot(snapshot, change.Before)
			if mode == "nonempty-before" {
				if err != nil || len(change.Before.Fields().AllowedMountIDs) != 2 {
					t.Fatal("original nonempty configuration rejected", err)
				}
			} else if err == nil {
				t.Fatal("drifted original Agent/mapping snapshot accepted")
			}
		})
	}
}

type mountProjectDeleteStore struct {
	postgres.SQLExecutor
	t              *testing.T
	project, agent string
	rows           map[string][]json.RawMessage
	order          []string
	short          string
}

func (s *mountProjectDeleteStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	var table string
	for _, name := range []string{"agent_mount_refs", "runner_workspaces", "mounts"} {
		if strings.HasPrefix(sql, "DELETE FROM agenteam_mount."+name+" ") {
			table = name
		}
	}
	if table == "" || !strings.Contains(sql, "t.project_id=$1::uuid") || !strings.Contains(sql, "t.agent_id=$2::uuid") || !strings.Contains(sql, "to_jsonb(t)=b.value") || len(args) != 3 || args[0] != s.project || args[1] != s.agent {
		s.t.Error("cleanup escaped its original Agent/Project full-row predicate")
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	var got, want []any
	raw, ok := args[2].([]byte)
	expected, err := json.Marshal(s.rows[table])
	if !ok || err != nil || json.Unmarshal(raw, &got) != nil || json.Unmarshal(expected, &want) != nil || !reflect.DeepEqual(got, want) {
		s.t.Error("cleanup changed or dropped part of an original row")
		return pgconn.CommandTag{}, fault(f.ConfirmationStale)
	}
	s.order = append(s.order, table)
	count := len(got)
	if table == s.short {
		count--
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", count)), nil
}

func TestMountProjectCleanupFullRows(t *testing.T) {
	for _, mode := range []string{"complete", "short-delete"} {
		t.Run(mode, func(t *testing.T) {
			change, snapshot := mountProjectPureSnapshot(t)
			store := &mountProjectDeleteStore{t: t, project: change.Request.ProjectID.String(), agent: change.Request.AgentID.String(), rows: map[string][]json.RawMessage{
				"agent_mount_refs": snapshot.References, "runner_workspaces": snapshot.Workspaces, "mounts": snapshot.Definitions,
			}}
			if mode == "short-delete" {
				store.short = "runner_workspaces"
			}
			err := deleteProjectMountSnapshot(context.Background(), store, change.Request.ProjectID, change.Request.AgentID, snapshot)
			if mode == "complete" {
				if err != nil || !reflect.DeepEqual(store.order, []string{"agent_mount_refs", "runner_workspaces", "mounts"}) {
					t.Fatal("complete logical resources were not deleted in dependency order", err, store.order)
				}
			} else if !retirementRefCode(err, f.ConfirmationStale) || !reflect.DeepEqual(store.order, []string{"agent_mount_refs", "runner_workspaces"}) {
				t.Fatal("partial deletion continued into later resources", err, store.order)
			}
		})
	}
}

func TestMountProjectCleanupSnapshotBound(t *testing.T) {
	for _, count := range []int{1024, 1025} {
		t.Run(fmt.Sprintf("definitions-%d", count), func(t *testing.T) {
			change, snapshot := mountProjectPureSnapshot(t)
			var definition map[string]any
			if err := json.Unmarshal(snapshot.Definitions[0], &definition); err != nil {
				t.Fatal(err)
			}
			// Valid stored text must retain its raw JSON size. Escaping '<' into
			// six bytes would spuriously exceed the batch limit at this row count.
			definition["description"] = strings.Repeat("<", 4096)
			snapshot.Definitions = make([]json.RawMessage, 0, count)
			for n := range count {
				definition["id"] = retirementRefID[i.Mount](t, 31+n).String()
				definition["workspace"] = fmt.Sprintf("data-%d", n)
				definition["name"] = fmt.Sprintf("data-%d", n)
				var row bytes.Buffer
				encoder := json.NewEncoder(&row)
				encoder.SetEscapeHTML(false)
				if err := encoder.Encode(definition); err != nil {
					t.Fatal(err)
				}
				snapshot.Definitions = append(snapshot.Definitions, bytes.TrimSpace(bytes.Clone(row.Bytes())))
			}
			var body bytes.Buffer
			encoder := json.NewEncoder(&body)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(snapshot); err != nil {
				t.Fatal(err)
			}
			if body.Len() >= 8<<20 {
				t.Fatal("controlled raw snapshot exceeded the specified byte budget")
			}
			store := &mountProjectSnapshotStore{project: change.Request.ProjectID.String(), agent: change.Request.AgentID.String(), raw: body.Bytes()}
			got, raw, err := readProjectMountSnapshot(context.Background(), store, change.Request.ProjectID, change.Request.AgentID)
			if count == 1024 {
				if err != nil || len(got.Definitions) != count || len(raw) >= 8<<20 || bytes.Contains(raw, []byte(`\u003c`)) {
					t.Fatal("bounded full batch was expanded or truncated", err, len(raw))
				}
			} else if !retirementRefCode(err, f.ResourceBusy) || raw != nil {
				t.Fatal("oversized row set became a partial successful batch", err)
			}
		})
	}
}

func TestMountProjectCleanupPreservesBeforeAndOtherProject(t *testing.T) {
	v := newMountProjectPure(t)
	b := mountProjectPureAdd(t, v, 1)
	p := v.change.Request.ProjectID
	head := bytes.Clone(v.store.rows[p].snapshot.Head)
	other := mountProjectPureJSON(v.store.rows[b.Request.ProjectID].snapshot)
	report, err := v.cleanup(context.Background(), nil)
	if err != nil || report.Details().State != pc.CleanupPending || !v.pair.Joined() {
		t.Fatal("original Agent batch did not return pending", err)
	}
	r := v.store.rows[p]
	var before ac.AgentConfig
	if r.proof == nil || json.Unmarshal(r.proof[5].([]byte), &before) != nil || !reflect.DeepEqual(before.Fields(), v.change.Before.Fields()) || len(before.Fields().AllowedMountIDs) != 2 || !bytes.Equal(head, r.snapshot.Head) || len(r.snapshot.References)+len(r.snapshot.Workspaces)+len(r.snapshot.Definitions) != 0 {
		t.Fatal("logical cleanup replaced the original nonempty Agent or removed its head")
	}
	if !bytes.Equal(other, mountProjectPureJSON(v.store.rows[b.Request.ProjectID].snapshot)) || v.store.rows[b.Request.ProjectID].proof != nil {
		t.Fatal("cleanup changed another Project")
	}
	writes := v.store.writes.Load()
	report, err = v.cleanup(context.Background(), report.Details().Checkpoint)
	if err != nil || report.Details().State != pc.CleanupCompleted || v.store.writes.Load() != writes || r.proof == nil || r.snapshot.Head == nil {
		t.Fatal("logical completion consumed the Agent Applied tail or repeated mutation", err)
	}
}

func TestMountProjectCleanupUnknownActualReturn(t *testing.T) {
	for _, physical := range []string{"committed", "rolled-back", "drifted-confirmation", "discovery-read-error"} {
		t.Run(physical, func(t *testing.T) {
			v := newMountProjectPure(t)
			b := mountProjectPureAdd(t, v, 1)
			attempt := retirementRefID[f.TransactionAttempt](t, 91)
			var original f.TransactionCause
			masked := false
			if physical == "discovery-read-error" {
				v.store.nextError = map[i.ProjectID]error{v.change.Request.ProjectID: fault(f.DependencyUnavailable)}
			}
			v.store.outcome = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) (f.CommitResult, bool) {
				if !masked && physical == "discovery-read-error" {
					if result.State() != f.NotCommitted || v.store.writes.Load() != 0 {
						t.Error("original discovery did not fail before candidate observation")
					}
					masked = true
					original = cause
					return f.UnknownResult(attempt, cause), false
				}
				if !masked && v.store.writes.Load() > 0 && result.State() == f.Committed {
					masked = true
					original = cause
					return f.UnknownResult(attempt, cause), physical != "rolled-back"
				}
				return result, result.State() == f.Committed
			}
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			v.store.tail = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) {
				if result.State() == f.Unknown {
					close(entered)
					<-release
				}
			}
			var first error
			t.Cleanup(func() { open(); mountProjectPureWait(t, returned) })
			go func() { defer close(returned); _, first = v.cleanup(context.Background(), nil) }()
			mountProjectPureWait(t, entered)
			if v.pair.Joined() {
				t.Fatal("physical commit/callback completion impersonated original Store return")
			}
			// A different Project proceeds through the same original owner and Store.
			r := b.Request
			other, err := v.participant.Cleanup(context.WithValue(context.Background(), mountProjectPureCleanupKey{}, v), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID}, nil)
			if err != nil || other.Details().State != pc.CleanupPending {
				t.Fatal("unrelated Project was globally retired or serialized", err)
			}
			open()
			mountProjectPureWait(t, returned)
			got, ok := UnknownAttempt(first)
			if !ok || got.AttemptID() != attempt || !reflect.DeepEqual(got.Cause().Details(), original.Details()) || v.pair.Joined() {
				t.Fatal("original Unknown provenance or owner was discarded", first)
			}
			v.store.outcome = nil
			v.store.nextError = nil
			writes := v.store.writes.Load()
			head := bytes.Clone(v.store.rows[v.change.Request.ProjectID].snapshot.Head)
			if physical == "drifted-confirmation" {
				v.store.rows[v.change.Request.ProjectID].snapshot.Head = mountProjectPureJSONField(t, head, "owner_version", int64(v.change.Request.ExpectedVersion)+1)
			}
			confirmed, allowConfirm, confirmReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var confirmOnce sync.Once
			finishConfirm := func() { confirmOnce.Do(func() { close(allowConfirm) }) }
			v.store.tail = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) {
				if !reflect.DeepEqual(cause.Details(), original.Details()) {
					t.Error("read confirmation replaced original cause")
				}
				close(confirmed)
				<-allowConfirm
			}
			var report pc.CleanupReport
			var confirmErr error
			t.Cleanup(func() { finishConfirm(); mountProjectPureWait(t, confirmReturned) })
			go func() { defer close(confirmReturned); report, confirmErr = v.cleanup(context.Background(), nil) }()
			mountProjectPureWait(t, confirmed)
			if v.store.writes.Load() != writes || v.pair.Joined() {
				t.Fatal("readonly callback wrote or retired the original Unknown")
			}
			finishConfirm()
			mountProjectPureWait(t, confirmReturned)
			if physical == "drifted-confirmation" {
				retained, ok := UnknownAttempt(confirmErr)
				if !ok || retained.AttemptID() != attempt || !reflect.DeepEqual(retained.Cause().Details(), original.Details()) || v.store.writes.Load() != writes || v.pair.Joined() {
					t.Fatal("drift replaced the first Unknown or retired its owner", confirmErr)
				}
				v.store.rows[v.change.Request.ProjectID].snapshot.Head = head
				v.store.tail = nil
				report, confirmErr = v.cleanup(context.Background(), nil)
			}
			if confirmErr != nil || report.Details().State != pc.CleanupPending || v.store.writes.Load() != writes || !v.pair.Joined() {
				t.Fatal("readonly actual return advanced beyond Pending", confirmErr)
			}
			v.store.tail = nil
			report, err = v.cleanup(context.Background(), report.Details().Checkpoint)
			if err != nil {
				t.Fatal("next explicit round failed", err)
			}
			if physical == "committed" || physical == "drifted-confirmation" {
				if report.Details().State != pc.CleanupCompleted || v.store.writes.Load() != writes {
					t.Fatal("committed original batch was repeated")
				}
			} else if report.Details().State != pc.CleanupPending || v.store.writes.Load() != writes+4 {
				t.Fatal("rolled-back original batch did not wait for the next explicit writer")
			}
		})
	}
}

func TestMountProjectCleanupRejectsDrift(t *testing.T) {
	for _, mode := range []string{"missing-claim", "wrong-claim", "foreign-process", "config-version", "mapping", "unsupported-operation"} {
		t.Run(mode, func(t *testing.T) {
			v := newMountProjectPure(t)
			r := v.change.Request
			ctx := context.WithValue(context.Background(), mountProjectPureCleanupKey{}, v)
			switch mode {
			case "missing-claim":
				ctx = context.Background()
			case "wrong-claim":
				r.Cause.ProjectVersion++
			case "foreign-process":
				v.pair.process = retirementRefID[oc.Process](t, 82)
			case "config-version":
				fields := v.store.rows[r.ProjectID].change.Before.Fields()
				fields.Core.Version++
				var err error
				v.store.rows[r.ProjectID].change.Before, err = ac.NewAgentConfig(fields)
				if err != nil {
					t.Fatal(err)
				}
			case "mapping":
				rows := v.store.rows[r.ProjectID]
				rows.snapshot.Workspaces[0] = mountProjectPureJSONField(t, rows.snapshot.Workspaces[0], "workspace", "drifted")
			case "unsupported-operation":
				v.store.rows[r.ProjectID].unsupported = true
				report, err := v.pair.RequestProjectStop(context.Background(), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID})
				if err != nil || report.Details().State != pc.StopPending {
					t.Fatal("unbound Runner operation was declared stopped", err)
				}
			}
			_, err := v.participant.Cleanup(ctx, r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID}, nil)
			if err == nil || v.store.writes.Load() != 0 {
				t.Fatal("drifted claim/configuration/mapping reached mutation", err)
			}
		})
	}
}

func TestMountProjectCleanupAppliedConsumesExactProof(t *testing.T) {
	for _, mode := range []string{"exact-applied", "without-applied", "before-drift", "foreign-store"} {
		t.Run(mode, func(t *testing.T) {
			v := newMountProjectPure(t)
			if _, err := v.cleanup(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			change := v.change.Clone()
			if mode == "before-drift" {
				fields := change.Before.Fields()
				fields.Core.Instructions = "different original config"
				var err error
				change.Before, err = ac.NewAgentConfig(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			// This is the existing controlled Applied port, not Agent's private
			// canonical writer. The real provider must consume its exact tuple.
			owner := &retirementRefOwner{change: change, live: true}
			store := Store(v.store)
			if mode == "foreign-store" {
				store = &mountProjectPureStore{}
			}
			adapter, err := NewProjectAgentRetirementReferencesWithRunnerManaged(store, owner, v.pair)
			if mode == "foreign-store" {
				if err == nil {
					t.Fatal("foreign Store accepted the original proof owner")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			writes, reads := v.store.writes.Load(), v.store.reads.Load()
			cause, err := f.NewRecoveryCause("mount-pure-applied", change.Request.Cause.OperationID.String(), change.Request.AgentID.String())
			if err != nil {
				t.Fatal(err)
			}
			result := v.store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				owner.tx = tx
				if err := v.store.AcquireAll(ctx, tx, change.Request.RequiredLocks()); err != nil {
					return err
				}
				if mode != "without-applied" {
					ctx = context.WithValue(ctx, retirementRefWitness{}, owner)
				}
				if err := adapter.RetireProjectAgentReferencesInTx(ctx, tx, change); err != nil {
					return err
				}
				return adapter.RequireProjectAgentReferencesRetiredInTx(ctx, tx, change)
			})
			rows := v.store.rows[v.change.Request.ProjectID]
			if mode == "exact-applied" {
				if result.State() != f.Committed || v.store.writes.Load() != writes+2 || rows.proof != nil || rows.snapshot.Head != nil || owner.calls != 2 {
					t.Fatal("exact Applied did not consume the retained head/proof", result.Fault())
				}
			} else if result.State() != f.NotCommitted || v.store.writes.Load() != writes || rows.proof == nil || rows.snapshot.Head == nil {
				t.Fatal("unrelated Applied tuple consumed proof", result.Fault())
			}
			if mode == "without-applied" && v.store.reads.Load() != reads {
				t.Fatal("missing Applied reached resource SQL")
			}
		})
	}
}
