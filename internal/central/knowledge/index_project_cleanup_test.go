package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestIndexProjectCleanupReleaseRequiresOriginalControl(t *testing.T) {
	x := newKnowledgeProjectPureFixture(t)
	actor, cause, scope := x.stopIdentity(t, x.a)
	call, err := x.pair.state.borrowProjectCleanup(context.Background(), actor, cause, scope, nil, x.gate)
	if err != nil {
		t.Fatal(err)
	}
	s, owner, p := retentionFixture(t)
	p.Project, p.Snapshot = x.a, newID[mc.Snapshot](t)
	s.documents = x.pair.Service()
	x.pair.state.index = s
	projectScope, _ := id.InProject(x.a)
	technical, err := s.authority.registration.Actor(cause.OperationID.String(), projectScope)
	if err != nil {
		t.Fatal(err)
	}
	g := &indexProjectRelease{state: x.pair.state, call: call, index: s, actor: technical, before: "active", target: indexRetentionTarget{Profile: p, Operation: newID[struct{}](t).String(), Process: x.pair.state.process}}
	call.indexRelease = g
	ctx := context.WithValue(context.Background(), projectCleanupContextKey{}, projectCleanupWitness{x.pair.state, call})
	r := g.request()
	plan, err := s.authority.DiscoverServingSnapshot(ctx, r)
	if err != nil || plan.Validate() != nil || plan.Details().RetryPolicy != nil {
		t.Fatal("original cleanup release discovery rejected", err)
	}
	if len(plan.RequiredLocks()) != 1 || plan.RequiredLocks()[0].Mode != f.Exclusive || f.CompareLockKeys(plan.RequiredLocks()[0].Key, knowledgeProjectLock(x.a, f.Exclusive).Key) != 0 {
		t.Fatal("cleanup did not carry original Project EX")
	}
	for name, mutate := range map[string]func(*mc.ServingSnapshotRetentionRequest){
		"retain":   func(r *mc.ServingSnapshotRetentionRequest) { r.Action = mc.RetainServingSnapshot },
		"project":  func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.ProjectID = x.b },
		"profile":  func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.GenerationID = newID[struct{}](t).String() },
		"snapshot": func(r *mc.ServingSnapshotRetentionRequest) { r.SnapshotID = newID[mc.Snapshot](t) },
		"version":  func(r *mc.ServingSnapshotRetentionRequest) { r.OwnerVersion++ },
		"owner":    func(r *mc.ServingSnapshotRetentionRequest) { r.Actor = owner },
	} {
		t.Run(name, func(t *testing.T) {
			changed := r.Clone()
			mutate(&changed)
			if _, err := s.authority.DiscoverServingSnapshot(ctx, changed); err == nil {
				t.Fatal("cleanup released a different authority tuple")
			}
		})
	}
	if _, err := s.authority.DiscoverServingSnapshot(context.Background(), r); err == nil {
		t.Fatal("public request replaced private cleanup proof")
	}
	// Unknown retention uses the same request and mapping, but the original
	// control must be actively borrowed by its cleanup driver for observation.
	unknown := f.UnknownResult(newID[f.TransactionAttempt](t), x.pair.state.projectCleanupCause(call))
	x.pair.state.retainProjectCleanup(call, unknown)
	after, err := s.authority.DiscoverServingSnapshot(ctx, r)
	if err != nil || after.Details().Mapping != plan.Details().Mapping {
		t.Fatal("Unknown changed original release mapping", err)
	}
	x.pair.state.returnProjectCleanup(call)
	if _, err := s.authority.DiscoverServingSnapshot(ctx, r); err == nil {
		t.Fatal("returned cleanup context authorized another Model action")
	}
	if transactions, _, writes := x.store.counts(); transactions != 0 || writes != 0 {
		t.Fatal("closed discovery boundary performed SQL")
	}
}

func TestIndexProjectCleanupSourcePairObservation(t *testing.T) {
	for _, mode := range []string{"unchanged", "absent", "profile_only", "source_only", "drift"} {
		t.Run(mode, func(t *testing.T) {
			p := newID[id.Project](t)
			profileID := newID[struct{}](t).String()
			source := knowledgeCleanupPureRow(t, p, "model_operations", map[string]any{"profile_id": profileID, "generation_id": profileID, "kind": "profile", "job_id": nil})
			profile := projectCleanupRow{Key: profileID, Record: knowledgeCleanupPureJSON(map[string]any{"id": profileID, "project_id": p.String(), "retention_state": "released", "source_operation_id": source.Key})}
			x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{"index_profiles": {profile.Key: profile.Record}, "model_operations": {source.Key: source.Record}}}
			batch := &projectCleanupBatch{stage: "index_profiles", rows: []projectCleanupRow{profile}}
			if err := captureIndexCleanupSources(context.Background(), x, p, batch); err != nil {
				t.Fatal(err)
			}
			if batch.sources == nil || len(batch.sources.rows) != 1 || batch.sources.rows[0].Key != source.Key {
				t.Fatal("profile lost original source preimage")
			}
			switch mode {
			case "absent":
				delete(x.rows["index_profiles"], profile.Key)
				delete(x.rows["model_operations"], source.Key)
			case "profile_only":
				delete(x.rows["index_profiles"], profile.Key)
			case "source_only":
				delete(x.rows["model_operations"], source.Key)
			case "drift":
				fields, _ := knowledgeCleanupPureFields(source.Record)
				fields["input_digest"] = json.RawMessage(`"different"`)
				x.rows["model_operations"][source.Key] = knowledgeCleanupPureJSON(fields)
			}
			applied, err := confirmProjectCleanupBatch(context.Background(), x, p, batch)
			if mode == "unchanged" || mode == "absent" {
				if err != nil || applied != (mode == "absent") {
					t.Fatal("wrong original pair observation", err)
				}
			} else {
				knowledgeProjectAssertFault(t, err, f.ConfirmationStale)
			}
			if x.writes != 0 {
				t.Fatal("pair observation performed a write")
			}
		})
	}
}

func TestIndexProjectCleanupBlocksLiveFacts(t *testing.T) {
	for _, table := range []string{"model_operations", "index_profiles"} {
		p := newID[id.Project](t)
		extra := map[string]any{"state": "completed", "version": 2}
		if table == "index_profiles" {
			extra = map[string]any{"retention_state": "retained"}
		}
		row := knowledgeCleanupPureRow(t, p, table, extra)
		x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{table: {row.Key: row.Record}}}
		_, err := readProjectCleanupRows(context.Background(), x, p, table, nil)
		knowledgeProjectAssertFault(t, err, f.ResourceBusy)
		if x.writes != 0 {
			t.Fatal("unfinished Model or pin state allowed deletion")
		}
	}
}
