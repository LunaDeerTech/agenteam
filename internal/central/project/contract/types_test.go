package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testID[K any](n int) foundation.ID[K] {
	v, err := foundation.ParseID[K](fmt.Sprintf("01960000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func testTime() foundation.Instant {
	v, err := foundation.ParseInstant("2026-04-01T12:00:00.000000Z")
	if err != nil {
		panic(err)
	}
	return v
}
func testHuman(user, session int) identity.Actor {
	v, err := identity.NewHuman(testID[identity.User](user), testID[identity.Session](session))
	if err != nil {
		panic(err)
	}
	return v
}
func testMeta(version bool) foundation.CommandMeta {
	m := foundation.CommandMeta{RequestID: testID[foundation.Request](100), IdempotencyKey: "intent/one"}
	if version {
		v := foundation.Version(3)
		m.ExpectedVersion = &v
	}
	return m
}
func testProject() ProjectRef {
	return ProjectRef{ID: testID[identity.Project](1), OwnerUserID: testID[identity.User](2), Name: "Build.API", NormalizedName: "build.api", Description: "keep \tthis\n", Lifecycle: Active, Version: 1, CreatedAt: testTime(), UpdatedAt: testTime()}
}
func testCreation() CreationOperation {
	return CreationOperation{ID: testID[Creation](3), ProjectID: testID[identity.Project](1), State: CreationAccepted, Version: 1, CreatedAt: testTime(), UpdatedAt: testTime()}
}
func requireCode(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
	for _, field := range fault.FieldErrors {
		if err := field.Validate(); err != nil {
			t.Fatalf("unsafe field error: %v", err)
		}
	}
}
func ptr[T any](value T) *T { return &value }

func TestProjectRefCanonicalShape(t *testing.T) {
	p := testProject()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version":"1"`) || !strings.Contains(string(raw), `"current_sprint_id":null`) {
		t.Fatalf("wrong scalar encoding: %s", raw)
	}
	var restored ProjectRef
	if err = json.Unmarshal(raw, &restored); err != nil || !reflect.DeepEqual(p, restored) {
		t.Fatalf("round trip: %v", err)
	}
	for _, mutate := range []func(*ProjectRef){func(p *ProjectRef) { p.NormalizedName = "Build.API" }, func(p *ProjectRef) { p.Lifecycle = "deleted" }, func(p *ProjectRef) { p.Version = 0 }, func(p *ProjectRef) { p.Lifecycle = Archived }, func(p *ProjectRef) { p.ArchivedAt = ptr(testTime()) }, func(p *ProjectRef) { p.CurrentSprintID = ptr(SprintID{}) }} {
		copy := p
		mutate(&copy)
		if _, err := json.Marshal(copy); err == nil {
			t.Fatalf("invalid project encoded: %+v", copy)
		}
	}
	for _, raw := range []string{`null`, `{}`, strings.Replace(string(raw), `"description":"keep \tthis\n"`, `"description":null`, 1), strings.Replace(string(raw), `"version":"1"`, `"version":1`, 1), strings.Replace(string(raw), `"name":`, `"NAME":`, 1), strings.Replace(string(raw), `"name":"Build.API"`, `"name":"Build.API","name":"other"`, 1)} {
		var out ProjectRef
		if json.Unmarshal([]byte(raw), &out) == nil {
			t.Fatalf("accepted invalid project JSON: %s", raw)
		}
	}
	p.Lifecycle = Archived
	p.ArchivedAt = ptr(testTime())
	if p.Validate() != nil {
		t.Fatal("valid archived project rejected")
	}
}

func TestCreationResultAndReasonVariants(t *testing.T) {
	p, o := testProject(), testCreation()
	valid := []CreationResult{{State: CreationReady, Project: &p}, {State: CreationPending, Operation: &o}}
	for _, value := range valid {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var restored CreationResult
		if json.Unmarshal(raw, &restored) != nil {
			t.Fatal("valid union failed to decode")
		}
	}
	invalid := []CreationResult{{}, {State: CreationReady}, {State: CreationReady, Project: &p, Operation: &o}, {State: CreationPending, Project: &p}}
	for _, value := range invalid {
		if value.Validate() == nil {
			t.Fatal("invalid union accepted")
		}
	}
	o.State = CreationFailed
	if o.Validate() == nil {
		t.Fatal("failed without safe reason")
	}
	o.SafeReason = ReasonDependencyUnavailable
	if o.Validate() != nil {
		t.Fatal("safe failure rejected")
	}
	o.SafeReason = "raw database password"
	if o.Validate() == nil {
		t.Fatal("raw reason accepted")
	}
	o.State = CreationCompleted
	o.SafeReason = ""
	if (CreationResult{State: CreationPending, Operation: &o}).Validate() == nil {
		t.Fatal("completed creation exposed as accepted")
	}
}

func testReceipt(t *testing.T) ProjectDeletionReceipt {
	t.Helper()
	hash, err := DeletionCommandKeyHash(testID[identity.Project](1), testID[identity.User](2), "delete-key")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewProjectDeletionReceipt(DeletionReceiptData{OperationID: testID[Operation](5), DeletedProjectID: testID[identity.Project](1), OriginalOwnerUserID: testID[identity.User](2), CommandKeyHash: hash, RequestDigest: hash, CompletedAt: testTime()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestDeletionReceiptMinimalSafeProjection(t *testing.T) {
	r := testReceipt(t)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 5 || string(fields["status"]) != `"completed"` {
		t.Fatalf("unexpected receipt: %s", raw)
	}
	for _, text := range []string{"sha256:", "delete-key", "name", "description", "project_version"} {
		if strings.Contains(string(raw), text) {
			t.Fatalf("receipt exposed %q", text)
		}
	}
	var untrusted ProjectDeletionReceipt
	if json.Unmarshal(raw, &untrusted) == nil || untrusted.Validate() == nil {
		t.Fatal("JSON minted a receipt")
	}
	if _, err := NewProjectDeletionReceipt(DeletionReceiptData{}); err == nil {
		t.Fatal("zero receipt accepted")
	}
	if got := fmt.Sprintf("%#v", struct{ receipt ProjectDeletionReceipt }{r}); strings.Contains(got, "sha256:") {
		t.Fatal("nested formatting exposed receipt hash")
	}
}

func TestProjectAccessCopiesAndIsNotJSONAuthority(t *testing.T) {
	p := testProject()
	p.Lifecycle = Archived
	p.ArchivedAt = ptr(testTime())
	p.CurrentSprintID = ptr(testID[Sprint](8))
	actor := testHuman(2, 9)
	access, err := NewProjectAccess(actor, p, testTime())
	if err != nil {
		t.Fatal(err)
	}
	*p.CurrentSprintID = testID[Sprint](99)
	out := access.Project()
	*out.ArchivedAt = foundation.Instant{}
	*out.CurrentSprintID = testID[Sprint](100)
	if access.Project().CurrentSprintID.String() != testID[Sprint](8).String() || access.Project().ArchivedAt.String() != testTime().String() {
		t.Fatal("access projection aliases caller memory")
	}
	if !access.Matches(actor, p.ID) || access.Matches(testHuman(2, 10), p.ID) {
		t.Fatal("actor mapping lost")
	}
	if _, err := NewProjectAccess(testHuman(99, 9), p, testTime()); err == nil {
		t.Fatal("foreign Owner shape accepted")
	}
	if json.Unmarshal([]byte(`{}`), &access) == nil {
		t.Fatal("JSON minted access")
	}
}
