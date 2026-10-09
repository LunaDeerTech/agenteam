package object

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func TestProjectStopBeforeNativeRetainsRealWorkWithoutHistoryScan(t *testing.T) {
	for _, kind := range []string{"preparation", "download"} {
		t.Run(kind, func(t *testing.T) {
			project := auditID[identity.Project](t)
			process := auditID[oc.Process](t)
			id := auditID[struct{}](t).String()
			resource := id
			if kind == "download" {
				resource = auditID[oc.DownloadGrant](t).String()
			}
			store := &auditTestStore{}
			store.row = func(query string, args ...any) postgres.Row {
				if strings.Contains(query, "FROM agenteam_object.project_work") {
					return auditValues(id, project.String(), process.String(), kind, resource, (*string)(nil), int64(1), "", "", int64(0), false)
				}
				if kind == "download" && strings.HasPrefix(query, "SELECT to_jsonb(r) FROM agenteam_download.grants") {
					return auditTestRow(func(...any) error { return pgx.ErrNoRows })
				}
				t.Fatalf("pre-native work expanded unrelated history: %s", query)
				return nil
			}
			state := &serviceState{store: store, projectWork: map[string]*projectWorkHandle{}}
			s := &Service{data: func() *serviceState { return state }}
			b := &stopBatchBuilder{service: s, ctx: context.Background(), x: store, project: project.String(), seen: map[string]bool{}, projections: map[string]json.RawMessage{}, processes: map[oc.ProcessID]bool{}}
			if err := b.addWork(id); err != nil {
				t.Fatal(err)
			}
			if b.facts.overflow || len(b.facts.works) != 1 || b.facts.works[0].joined || len(b.facts.objects) != 0 || len(b.facts.locks) != 2 {
				t.Fatal("missing pre-native row lost original work/locks or invented join")
			}
			if state.projectWork[id] != nil {
				t.Fatal("discovery manufactured actual-return handle")
			}
		})
	}
}

func TestProjectStopNativeWorkCannotCrossObjectOrProcess(t *testing.T) {
	object := auditID[oc.StoredObject](t)
	process := auditID[oc.Process](t)
	resource := auditID[struct{}](t).String()
	for _, kind := range []string{"preparation", "verification", "reader", "source", "cleanup", "transfer_get", "transfer_put", "download"} {
		t.Run(kind, func(t *testing.T) {
			store := &auditTestStore{row: func(query string, args ...any) postgres.Row {
				if !strings.HasPrefix(query, "SELECT EXISTS(") || len(args) < 2 || args[0] != resource || args[1] != object.String() {
					t.Fatal("native check omitted exact resource/Object")
				}
				if kind == "preparation" || kind == "reader" || kind == "source" {
					if len(args) < 3 || args[2] != process.String() {
						t.Fatal("original process omitted")
					}
				}
				return auditValues(false)
			}}
			b := &stopBatchBuilder{ctx: context.Background(), x: store}
			if matches, err := b.nativeWorkMatches(projectWork{kind: kind, object: object, resource: resource, process: process}); err != nil || matches {
				t.Fatal("mismatched native row accepted", err)
			}
		})
	}
}

func TestProjectStopProjectionOnlyUsesExactNativePrimaryKeys(t *testing.T) {
	id := auditID[struct{}](t).String()
	store := &auditTestStore{row: func(query string, args ...any) postgres.Row {
		if !strings.HasSuffix(query, " r WHERE id=$1") || len(args) != 1 || args[0] != id {
			t.Fatal("native projection became an Object history scan")
		}
		return auditValues([]byte(`{"id":"` + id + `"}`))
	}}
	b := &stopBatchBuilder{ctx: context.Background(), x: store, projections: map[string]json.RawMessage{}}
	for _, table := range []string{"agenteam_object.upload_attempts", "agenteam_object.object_leases", "agenteam_object.cleanup_operations", "agenteam_download.attempts"} {
		if found, err := b.projectRow(table, id, false); err != nil || !found {
			t.Fatal(err)
		}
	}
	before := store.queries
	if _, err := b.projectRow("agenteam_object.objects WHERE true;--", id, false); err == nil || store.queries != before {
		t.Fatal("non-closed table reached SQL")
	}
	store.row = func(string, ...any) postgres.Row { return auditTestRow(func(...any) error { return pgx.ErrNoRows }) }
	if found, err := b.projectRow("agenteam_object.upload_attempts", id, false); err != nil || found || !b.facts.overflow {
		t.Fatal("missing required native row became terminal absence", err)
	}
}
