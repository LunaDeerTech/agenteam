//go:build integration

package model_test

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelPlatformSummarySharedSelection(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	_, first, _ := v.config(t, false, false)
	_, next, _ := v.config(t, false, false)
	initialVersion := v.selectMemory(t, first)
	agent := newID[id.Agent](t)
	requests := make([]mc.SelectionRequest, 0, 5)
	for _, purpose := range []mc.Purpose{mc.MemoryExtraction, mc.MemoryConsolidation, mc.MemoryReflection, mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate} {
		consumer := mc.Consumer{ProjectID: v.project.ID, OperationID: newID[struct{}](t).String(), Purpose: purpose}
		if purpose == mc.MeetingSummaryInitial || purpose == mc.MeetingSummaryUpdate {
			consumer.Kind = mc.MeetingConsumer
			consumer.MeetingID = newID[struct{}](t).String()
		} else {
			consumer.Kind = mc.MemoryConsumer
			consumer.AgentID = &agent
		}
		request := mc.SelectionRequest{Consumer: consumer, Selection: mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector}}
		selected, err := v.resolving.SelectModel(testContext(t), v.owner, request)
		if err != nil || selected.Selected == nil || *selected.Selected != first.ID || selected.SelectionVersion == nil || *selected.SelectionVersion != initialVersion {
			t.Fatalf("purpose %s did not share initial selection: %v", purpose, err)
		}
		requests = append(requests, request)
	}
	currentVersion := v.selectMemory(t, next)
	for _, request := range requests {
		stale := request.Clone()
		stale.Selection.Version = &initialVersion
		_, err := v.resolving.SelectModel(testContext(t), v.owner, stale)
		requireCode(t, err, f.ResourceBusy)
		selected, err := v.resolving.SelectModel(testContext(t), v.owner, request)
		if err != nil || selected.Selected == nil || *selected.Selected != next.ID || selected.SelectionVersion == nil || *selected.SelectionVersion != currentVersion {
			t.Fatalf("purpose %s did not share updated selection: %v", request.Consumer.Purpose, err)
		}
	}
	var owners, references int
	if err := v.raw.QueryRow(testContext(t), `SELECT count(DISTINCT owner_id),count(*) FROM agenteam_model.references WHERE owner_kind='platform_selector' AND role='summary'`).Scan(&owners, &references); err != nil || owners != 1 || references != 1 {
		t.Fatal("shared Summary created independent reference owners", err, owners, references)
	}
}

func TestModelPlatformSummarySchema(t *testing.T) {
	v := newMeetingSummaryFixture(t)
	var legacyTable, legacyColumn bool
	if err := v.raw.QueryRow(testContext(t), `SELECT to_regclass('agenteam_model.meeting_summary_selection') IS NOT NULL, EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='agenteam_model' AND table_name='platform_selection' AND column_name='memory_id')`).Scan(&legacyTable, &legacyColumn); err != nil || legacyTable || legacyColumn {
		t.Fatal("legacy independent selector schema survived", err)
	}
	summary := v.chat(t, true)
	selected := v.selectModel(t, summary)
	state := v.summary(t)
	if state.ID != selected.ResourceID || state.Configured == nil || state.Configured.Summary != summary {
		t.Fatal("shared Summary canonical is missing")
	}
	conn := v.db.Connect(t)
	for _, role := range []string{"memory", "meeting_summary"} {
		expectModelSQLState(t, conn, "23514", `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,$2,$3,$4)`, state.ID, role, summary.String(), int64(state.Version))
	}
	expectModelSQLState(t, conn, "23514", `UPDATE agenteam_model.platform_selection SET summary_id=NULL`)
	expectModelSQLState(t, conn, "23514", `UPDATE agenteam_model.platform_selection SET embedding_id=NULL`)
	expectModelSQLState(t, conn, "23505", `INSERT INTO agenteam_model.platform_selection(id,singleton,version,configured,updated_at) VALUES($1,true,1,false,clock_timestamp())`, newID[struct{}](t).String())
}

func TestModelPlatformSummaryMemoryResolutionOrigin(t *testing.T) {
	v := newFixture(t)
	conn := v.db.Connect(t)
	const query = `INSERT INTO agenteam_memory.model_calls(call_id,operation_id,project_id,agent_id,execution_id,purpose,input_digest,created_at,process_id,resolve_source,resolve_binding) VALUES($1,$2,$3,$4,$5,$6,'sha256:0000000000000000000000000000000000000000000000000000000000000000',clock_timestamp(),$7,jsonb_build_object('schema','1','source','current_selection','selection',jsonb_build_object('kind','platform','selector',$8::text)),'sha256:0000000000000000000000000000000000000000000000000000000000000000')`
	for _, purpose := range []mc.Purpose{mc.MemoryExtraction, mc.MemoryConsolidation, mc.MemoryReflection, mc.MemoryEmbedding} {
		selector := string(mc.SummarySelector)
		if purpose == mc.MemoryEmbedding {
			selector = string(mc.EmbeddingSelector)
		}
		args := []any{newID[struct{}](t).String(), newID[struct{}](t).String(), newID[id.Project](t).String(), newID[id.Agent](t).String(), newID[id.Execution](t).String(), string(purpose), newID[struct{}](t).String(), selector}
		if _, err := conn.Exec(testContext(t), query, args...); err != nil {
			t.Fatalf("current %s origin rejected: %v", purpose, err)
		}
		for _, legacy := range []string{"memory", "meeting_summary"} {
			args[0], args[7] = newID[struct{}](t).String(), legacy
			expectModelSQLState(t, conn, "23514", query, args...)
		}
	}
}
