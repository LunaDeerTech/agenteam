package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	m "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// This controlled SQL row passes through the public Lookup, original current
// Owner gate, loadCommand strict decoder, and frozen-command receipt check. It
// is not a PG authority or evidence of transaction commit behavior.
type runnerMountLookupStore struct {
	*directoryControlStore
	record commandRecord
}

func (s *runnerMountLookupStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || !s.authorized {
		s.t.Fatal("lookup SQL outside authorized original transaction")
	}
	return s, nil
}
func (s *runnerMountLookupStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	if !s.live || !s.authorized || !strings.Contains(sql, "FROM agenteam_agent.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3") || len(args) != 3 || args[0] != s.record.Project.String() || args[1] != string(s.record.Name) || args[2] != string(s.record.Key) {
		s.t.Fatal("lookup changed original command read")
	}
	s.reads++
	r := s.record
	after, err := json.Marshal(r.After)
	if err != nil {
		s.t.Fatal(err)
	}
	var bindings []byte
	if r.RunnerMounts != nil {
		bindings, err = json.Marshal(*r.RunnerMounts)
		if err != nil {
			s.t.Fatal(err)
		}
	}
	return runnerMountLookupRow{s.t, []any{r.ID.String(), r.Project.String(), r.User.String(), r.Target.String(), string(r.Name), string(r.Semantic), r.Input, nil, int64(r.Revision), nil, after, r.AddSkillsEnabled, r.InstallSkillEnabled, r.ChangedFields, r.State, r.Receipt, r.Created.Time(), func() any {
		if r.Committed == nil {
			return nil
		}
		v := r.Committed.Time()
		return &v
	}(), bindings}}
}

type runnerMountLookupRow struct {
	t      *testing.T
	values []any
}

func (r runnerMountLookupRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		r.t.Fatalf("stored command column mismatch: %d/%d", len(dest), len(r.values))
	}
	for n, value := range r.values {
		out := reflect.ValueOf(dest[n]).Elem()
		if value == nil {
			out.SetZero()
			continue
		}
		in := reflect.ValueOf(value)
		if !in.Type().ConvertibleTo(out.Type()) {
			r.t.Fatalf("stored column %d has incompatible type", n)
		}
		out.Set(in.Convert(out.Type()))
	}
	return nil
}
func runnerMountCompletedRecord(t *testing.T, newProfile bool) (commandInput, commandRecord) {
	t.Helper()
	in, at := planFixture(t)
	if newProfile {
		decl := []c.RunnerMountDeclaration{{RunnerID: commandID[rc.Runner](t, "01900000-0000-7000-8000-000000000031"), Workspace: "first"}, {RunnerID: commandID[rc.Runner](t, "01900000-0000-7000-8000-000000000031"), Workspace: "second"}}
		d := in.create.Fields()
		d.RunnerMounts = &decl
		d.AllowedMountIDs = nil
		create, err := c.NewAgentCreate(d)
		if err != nil {
			t.Fatal(err)
		}
		in.create = &create
		bindings := []c.RunnerMountBinding{{RunnerID: decl[0].RunnerID, Workspace: decl[0].Workspace, MountID: commandID[i.Mount](t, "01900000-0000-7000-8000-000000000032"), WorkspaceID: commandID[m.Workspace](t, "01900000-0000-7000-8000-000000000034")}, {RunnerID: decl[1].RunnerID, Workspace: decl[1].Workspace, MountID: commandID[i.Mount](t, "01900000-0000-7000-8000-000000000033"), WorkspaceID: commandID[m.Workspace](t, "01900000-0000-7000-8000-000000000035")}}
		in.runnerMounts = &bindings
	}
	if err := in.validate(); err != nil {
		t.Fatal(err)
	}
	after, fields, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	d := in.create.Fields()
	r := commandRecord{RunnerMounts: in.runnerMounts, ID: commandID[c.AgentCommand](t, "01900000-0000-7000-8000-000000000036"), Project: in.project, User: commandID[i.User](t, in.actor.Details().UserID), Target: in.target, Name: in.name, Key: in.meta.IdempotencyKey, Semantic: in.semantic, Input: in.raw, Revision: 1, After: after, AddSkillsEnabled: d.AddSkillsEnabled, InstallSkillEnabled: d.InstallSkillEnabled, ChangedFields: fields, State: "completed", Created: at, Committed: &at}
	eventID := commandID[e.EventIdentity](t, r.ID.String())
	mutation, err := c.NewAgentMutation(c.AgentMutationFields{Agent: after, Changed: true, EventIDs: []e.EventID{eventID}, RunnerMounts: r.RunnerMounts})
	if err != nil {
		t.Fatal(err)
	}
	r.Receipt, err = json.Marshal(mutation)
	if err != nil || r.validate() != nil {
		t.Fatal("invalid original frozen record", err)
	}
	return in, r
}
func TestAgentRunnerMountLookupValidatesFrozenReceipt(t *testing.T) {
	for _, name := range []string{"valid-new", "wrong-workspace-id", "missing-mapping", "wrong-order", "wrong-event-id", "valid-legacy"} {
		t.Run(name, func(t *testing.T) {
			in, record := runnerMountCompletedRecord(t, name != "valid-legacy")
			value, err := c.DecodeAgentMutation(record.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			fields := value.Fields()
			switch name {
			case "wrong-workspace-id":
				bindings := slices.Clone(*fields.RunnerMounts)
				bindings[0].WorkspaceID = commandID[m.Workspace](t, "01900000-0000-7000-8000-000000000039")
				fields.RunnerMounts = &bindings
			case "missing-mapping":
				fields.RunnerMounts = nil
			case "wrong-order":
				bindings := slices.Clone(*fields.RunnerMounts)
				slices.Reverse(bindings)
				fields.RunnerMounts = &bindings
			case "wrong-event-id":
				fields.EventIDs = []e.EventID{commandID[e.EventIdentity](t, "01900000-0000-7000-8000-000000000038")}
			}
			value, err = c.NewAgentMutation(fields)
			if err != nil {
				t.Fatal("counterexample must have valid typed shape", err)
			}
			record.Receipt, err = json.Marshal(value)
			if err != nil || record.validate() != nil {
				t.Fatal("counterexample must reach completed receipt check", err)
			}
			store := &runnerMountLookupStore{directoryControlStore: &directoryControlStore{t: t}, record: record}
			_, _, grant := executionConfigurationFixture(t)
			projects := &executionConfigurationProjects{check: func(_ context.Context, _ f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
				if !store.live || actor.Details() != in.actor.Details() || project != in.project || intent != i.Read || len(store.locks) != 3 {
					t.Fatal("wrong lookup authority boundary")
				}
				store.authorized = true
				return grant, nil
			}}
			authority, err := NewAuthority(store, projects)
			if err != nil {
				t.Fatal(err)
			}
			service := &Service{state: &serviceState{store: store, deps: Dependencies{Authority: authority}, calls: newCalls()}}
			out, err := service.LookupAgentCommand(context.Background(), in.actor, in.project, in.name, in.meta.IdempotencyKey, in.semantic)
			valid := name == "valid-new" || name == "valid-legacy"
			if valid {
				if err != nil || out.Status() != c.AgentLookupCommitted || out.Receipt() == nil || !sameValue(out.Receipt().Fields(), value.Fields()) {
					t.Fatal("valid original receipt rejected", err)
				}
			} else {
				if err == nil || out.Receipt() != nil {
					t.Fatal("Lookup accepted receipt detached from frozen command")
				}
				requireCode(t, err, f.DependencyUnavailable)
			}
			if store.reads != 1 || store.calls != 1 || store.live {
				t.Fatal("lookup did not retain original read boundary")
			}
			service.Stop()
			if err := service.Drain(context.Background()); err != nil || !service.Joined() {
				t.Fatal("lookup call not retired", err)
			}
		})
	}
}
