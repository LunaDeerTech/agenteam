package schedulerhttp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func testID[K any]() f.ID[K] {
	id, err := f.ParseID[K]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		panic(err)
	}
	return id
}
func testActor() i.Actor {
	a, err := i.NewHuman(testID[i.User](), testID[i.Session]())
	if err != nil {
		panic(err)
	}
	return a
}
func testProject() pc.ProjectRef {
	at, err := f.ParseInstant("2026-10-11T01:02:03.123456Z")
	if err != nil {
		panic(err)
	}
	return pc.ProjectRef{ID: testID[i.Project](), OwnerUserID: testID[i.User](), Name: "Project", NormalizedName: "project", Description: "private description", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
}
func ptr[T any](v T) *T { return &v }

const validInput = `{"expected_version":"1","scheduler_enabled":false,"scheduler_max_concurrency":null}`

var testPath = projectPrefix + testID[i.Project]().String() + schedulerSuffix

func testRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "scheduler-original-key")
	return r
}
func decode(t *testing.T, raw string) (meta f.CommandMeta, input pc.UpdateProjectRequest, err error) {
	t.Helper()
	httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta, input, err = decodeUpdate(w, r)
	})).ServeHTTP(httptest.NewRecorder(), testRequest("PATCH", testPath, raw))
	return
}
func object(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSchedulerWirePreservesCompleteInt64AndExplicitValues(t *testing.T) {
	for _, token := range []string{"null", `"1"`, `"9007199254740993"`, `"9223372036854775807"`} {
		t.Run(token, func(t *testing.T) {
			raw := strings.Replace(validInput, "null", token, 1)
			meta, input, err := decode(t, raw)
			if err != nil || meta.IdempotencyKey != "scheduler-original-key" || *meta.ExpectedVersion != 1 || input.Name != nil || input.Description != nil || input.Scheduler == nil || input.Scheduler.Enabled {
				t.Fatal("presence/metadata", err)
			}
			out, err := encodeCurrent(context.Background(), testActor(), testID[i.Project](), pc.SchedulerProject{Project: testProject(), Config: *input.Scheduler})
			if err != nil {
				t.Fatal(err)
			}
			v := object(t, out)
			if len(v) != 4 || v["scheduler_enabled"] != false {
				t.Fatal("current shape", v)
			}
			if token == "null" {
				if v["scheduler_max_concurrency"] != nil {
					t.Fatal(v)
				}
			} else if v["scheduler_max_concurrency"] != strings.Trim(token, `"`) {
				t.Fatal("precision lost", v)
			}
		})
	}
	meta, input, err := decode(t, strings.Replace(strings.Replace(validInput, `"1"`, `"9223372036854775807"`, 1), "false", "true", 1))
	if err != nil || *meta.ExpectedVersion != f.Version(math.MaxInt64) || !input.Scheduler.Enabled {
		t.Fatal("version precision/bool", err)
	}
}

func TestSchedulerWireRejectsIncompleteAndAmbiguousReplacement(t *testing.T) {
	bad := []string{`{}`, `{"expected_version":"1"}`, `{"expected_version":"1","scheduler_enabled":false}`, `{"expected_version":"1","scheduler_max_concurrency":null}`}
	for _, field := range []string{"expected_version", "scheduler_enabled", "scheduler_max_concurrency"} {
		var v map[string]any
		_ = json.Unmarshal([]byte(validInput), &v)
		delete(v, field)
		b, _ := json.Marshal(v)
		bad = append(bad, string(b))
	}
	for _, limit := range []string{`0`, `1`, `1.5`, `"0"`, `"-1"`, `"01"`, `"+1"`, `"1e2"`, `"9223372036854775808"`, `"１"`, `true`, `{}`, `[]`} {
		bad = append(bad, strings.Replace(validInput, "null", limit, 1))
	}
	for _, version := range []string{`null`, `1`, `"0"`, `"01"`, `"9223372036854775808"`} {
		bad = append(bad, strings.Replace(validInput, `"1"`, version, 1))
	}
	for _, enabled := range []string{`null`, `0`, `"false"`, `{}`} {
		bad = append(bad, strings.Replace(validInput, "false", enabled, 1))
	}
	bad = append(bad, strings.Replace(validInput, "scheduler_enabled", "Scheduler_enabled", 1), strings.TrimSuffix(validInput, "}")+`,"name":"mixed"}`, strings.TrimSuffix(validInput, "}")+`,"scheduler_enabled":true}`, strings.TrimSuffix(validInput, "}")+`,"scheduler_\u0065nabled":true}`, strings.TrimSuffix(validInput, "}")+`,"private":"\ud800"}`, validInput+` {}`, validInput+string([]byte{255}))
	for n, raw := range bad {
		if _, _, err := decode(t, raw); err == nil {
			t.Fatalf("invalid vector %d accepted: %s", n, raw)
		}
	}
}

func TestSchedulerReceiptContainsOnlyHistoricalAcknowledgement(t *testing.T) {
	meta, _, err := decode(t, validInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []f.Version{1, 2} {
		p := testProject()
		p.Version = version
		raw, err := encodeReceipt(context.Background(), testActor(), p.ID, meta, p)
		if err != nil {
			t.Fatal(err)
		}
		v := object(t, raw)
		if len(v) != 2 || v["project_id"] != p.ID.String() || v["version"] != version.String() {
			t.Fatal("not minimal ack", v)
		}
		if strings.Contains(string(raw), "private") {
			t.Fatal("metadata leaked")
		}
	}
	for _, change := range []func(*pc.ProjectRef){func(p *pc.ProjectRef) { p.ID = pc.ProjectID{} }, func(p *pc.ProjectRef) { p.OwnerUserID = i.UserID{} }, func(p *pc.ProjectRef) { p.Version = 3 }, func(p *pc.ProjectRef) { p.Lifecycle = pc.Archiving }} {
		p := testProject()
		change(&p)
		if _, err := encodeReceipt(context.Background(), testActor(), testID[i.Project](), meta, p); err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	p := testProject()
	p.Version = f.Version(math.MaxInt64)
	meta.ExpectedVersion = &p.Version
	if _, err := encodeReceipt(context.Background(), testActor(), p.ID, meta, p); err != nil {
		t.Fatal("max no-op rejected", err)
	}
}

func TestSchedulerCurrentProjectionRequiresScopeOwnerAndValidConfiguration(t *testing.T) {
	for _, change := range []func(*pc.SchedulerProject){func(v *pc.SchedulerProject) { v.Project.ID = pc.ProjectID{} }, func(v *pc.SchedulerProject) { v.Project.OwnerUserID = i.UserID{} }, func(v *pc.SchedulerProject) { v.Config.MaxConcurrency = ptr(int64(0)) }, func(v *pc.SchedulerProject) { v.Project.Lifecycle = pc.Deleting }} {
		v := pc.SchedulerProject{Project: testProject()}
		change(&v)
		if _, err := encodeCurrent(context.Background(), testActor(), testID[i.Project](), v); err == nil {
			t.Fatal("unsafe current projection")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := encodeCurrent(ctx, testActor(), testID[i.Project](), pc.SchedulerProject{Project: testProject()}); err == nil {
		t.Fatal("cancelled publication")
	}
}
