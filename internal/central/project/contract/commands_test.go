package contract

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// The Retry port must carry the final minimal delete receipt as well as an
// operation. This compile assertion needs no fake Project service.
var _ func(ProjectService, context.Context, identity.Actor, foundation.CommandMeta, ProjectID, OperationID) (LifecycleResult, error) = ProjectService.RetryLifecycle

func TestCreateIdentityTargetAndCanonicalDigestVector(t *testing.T) {
	project := testID[identity.Project](1)
	human := testHuman(2, 10)
	meta := testMeta(false)
	r := CreateProjectRequest{ProjectID: project, Name: "API"}
	first, err := CommandIdentity(project, CreateCommand, meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CommandIdentity(testID[identity.Project](3), CreateCommand, meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if first.Namespace() != "project" || first.Command() != "create" || !reflect.DeepEqual(first.OwnerIDs(), []string{project.String()}) || first.Canonical() == second.Canonical() {
		t.Fatal("Create did not scope to stable target")
	}
	owners := first.OwnerIDs()
	owners[0] = "changed"
	if first.OwnerIDs()[0] != project.String() {
		t.Fatal("identity aliases owner IDs")
	}
	digest, err := CreateDigest(human, meta, r)
	if err != nil {
		t.Fatal(err)
	}
	const vector = "sha256:51b09b54f14df46aa0480bc2037d0e6a272a94092a3a6940747882fa6369c194"
	if string(digest) != vector {
		t.Fatalf("canonical-v1 vector drift: %s", digest)
	}
	meta.RequestID = testID[foundation.Request](101)
	meta.IdempotencyKey = "other-key"
	newSession, err := CreateDigest(testHuman(2, 11), meta, r)
	if err != nil || newSession != digest {
		t.Fatal("transport identity/session entered semantic digest")
	}
	for _, mutate := range []func(*CreateProjectRequest){func(r *CreateProjectRequest) { r.ProjectID = testID[identity.Project](3) }, func(r *CreateProjectRequest) { r.Name = "api" }, func(r *CreateProjectRequest) { r.Description = " " }} {
		copy := r
		mutate(&copy)
		changed, err := CreateDigest(human, meta, copy)
		if err != nil || changed == digest {
			t.Fatal("semantic change collapsed")
		}
	}
	otherOwner, _ := CreateDigest(testHuman(5, 11), meta, r)
	if otherOwner == digest {
		t.Fatal("Owner excluded from digest")
	}
	if _, err := CommandIdentity(ProjectID{}, CreateCommand, "x"); err == nil {
		t.Fatal("zero create target accepted")
	}
	if _, err := CommandIdentity(project, "unknown", "x"); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestPatchPresenceAndDeleteConfirmationDigests(t *testing.T) {
	human, meta, project := testHuman(2, 10), testMeta(true), testID[identity.Project](1)
	onlyName := UpdateProjectRequest{Name: ptr("X")}
	withEmptyDescription := UpdateProjectRequest{Name: ptr("X"), Description: ptr("")}
	a, err := UpdateDigest(human, meta, project, onlyName)
	if err != nil {
		t.Fatal(err)
	}
	b, err := UpdateDigest(human, meta, project, withEmptyDescription)
	if err != nil || a == b {
		t.Fatal("omitted/explicit patch collapsed")
	}
	meta2 := meta
	meta2.ExpectedVersion = ptr(foundation.Version(4))
	c, _ := UpdateDigest(human, meta2, project, onlyName)
	if a == c {
		t.Fatal("expected_version excluded")
	}
	d1, err := DeleteDigest(human, meta, project, DeleteProjectRequest{"Admin/API", true})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := DeleteDigest(human, meta, project, DeleteProjectRequest{"admin/api", true})
	if err != nil || d1 != d2 {
		t.Fatal("confirmation not canonicalized")
	}
	if _, err := DeleteDigest(human, meta, project, DeleteProjectRequest{"admin/api", false}); err == nil {
		t.Fatal("permanent false accepted")
	}
	archive, _ := ArchiveDigest(human, meta, project)
	restore, _ := RestoreDigest(human, meta, project)
	retry1, _ := RetryLifecycleDigest(human, meta, project, testID[Operation](5))
	retry2, _ := RetryLifecycleDigest(human, meta, project, testID[Operation](6))
	if archive == restore || retry1 == retry2 {
		t.Fatal("command/operation identity collapsed")
	}
	key1, _ := DeletionCommandKeyHash(project, testID[identity.User](2), "k")
	key2, _ := DeletionCommandKeyHash(project, testID[identity.User](3), "k")
	key3, _ := DeletionCommandKeyHash(testID[identity.Project](3), testID[identity.User](2), "k")
	key4, _ := DeletionCommandKeyHash(project, testID[identity.User](2), "kk")
	if key1 == key2 || key1 == key3 || key1 == key4 || key1 == d1 {
		t.Fatal("delete hash scope/domain collision")
	}
}

func TestCommandMetaAndActorRequirements(t *testing.T) {
	for _, command := range []CommandName{CreateCommand, UpdateCommand, ArchiveCommand, RestoreCommand, DeleteCommand, RetryLifecycleCommand} {
		if err := ValidateCommandMeta(command, testMeta(command != CreateCommand)); err != nil {
			t.Fatalf("valid meta %s: %v", command, err)
		}
		if ValidateCommandMeta(command, testMeta(command == CreateCommand)) == nil {
			t.Fatalf("wrong expected_version presence accepted for %s", command)
		}
	}
	m := testMeta(true)
	*m.ExpectedVersion = 0
	if ValidateCommandMeta(UpdateCommand, m) == nil {
		t.Fatal("zero expected version")
	}
	if _, err := CreateDigest(identity.Actor{}, testMeta(false), CreateProjectRequest{ProjectID: testID[identity.Project](1), Name: "X"}); err == nil {
		t.Fatal("zero actor accepted")
	}
	actor, _ := identity.NewAgentRun(testID[identity.Project](1), testID[identity.Agent](7), testID[identity.Execution](8))
	_, err := CreateDigest(actor, testMeta(false), CreateProjectRequest{ProjectID: testID[identity.Project](1), Name: "X"})
	requireCode(t, err, foundation.Unauthenticated)
}

func TestOwnedListCursorScopeAndDeletingProjection(t *testing.T) {
	owner := testID[identity.User](2)
	all, _ := OwnedProjectsQueryDigest(owner, ListOwnedProjectsRequest{})
	explicit, _ := OwnedProjectsQueryDigest(owner, ListOwnedProjectsRequest{[]Lifecycle{Deleting, Archived, Active, Archiving}})
	if all != explicit {
		t.Fatal("all filter normalization differs")
	}
	x, _ := OwnedProjectsQueryDigest(owner, ListOwnedProjectsRequest{[]Lifecycle{Active, Archived}})
	y, _ := OwnedProjectsQueryDigest(owner, ListOwnedProjectsRequest{[]Lifecycle{Archived, Active}})
	other, _ := OwnedProjectsQueryDigest(testID[identity.User](3), ListOwnedProjectsRequest{[]Lifecycle{Active, Archived}})
	if x != y || x == all || x == other {
		t.Fatal("cursor digest binding")
	}
	for _, filter := range [][]Lifecycle{{}, {Active, Active}, {"unknown"}} {
		if (ListOwnedProjectsRequest{filter}).Validate() == nil {
			t.Fatal("invalid lifecycle filter")
		}
	}
	for _, limit := range []int{0, 101, 201} {
		if ValidateProjectPage(foundation.PageRequest{Limit: limit}) == nil {
			t.Fatalf("limit %d", limit)
		}
	}
	for _, limit := range []int{1, 50, 100} {
		if ValidateProjectPage(foundation.PageRequest{Limit: limit}) != nil {
			t.Fatalf("limit %d", limit)
		}
	}
	item := ProjectListItem{ID: testID[identity.Project](1), Name: "X", Lifecycle: Deleting, Version: 3, OperationID: ptr(testID[Operation](5))}
	if _, err := json.Marshal(item); err != nil {
		t.Fatal(err)
	}
	item.Description = ptr("must not leak")
	if _, err := json.Marshal(item); err == nil {
		t.Fatal("deleting list exposed description")
	}
}

func TestLookupClosedResultVariants(t *testing.T) {
	p := testProject()
	r := CommandResult{Command: UpdateCommand, Project: &p}
	for _, result := range []CommandLookupResult{{State: LookupCommitted, Result: &r}, {State: LookupInProgress}, {State: LookupNotObserved}} {
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var decoded CommandLookupResult
		if json.Unmarshal(raw, &decoded) != nil {
			t.Fatal("lookup round trip")
		}
	}
	for _, result := range []CommandLookupResult{{}, {State: LookupCommitted}, {State: LookupInProgress, Result: &r}, {State: "unknown"}} {
		if result.Validate() == nil {
			t.Fatal("unsafe unknown/replay variant accepted")
		}
	}
	r.Command = CreateCommand
	if r.Validate() == nil {
		t.Fatal("Create returned unwrapped Project instead of creation result")
	}
	for _, raw := range []string{`{"state":"committed","result":null}`, `{"state":"not_observed","result":{}}`, `{"state":"not_observed","State":"committed"}`} {
		var result CommandLookupResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			t.Fatal("unsafe lookup encoding")
		}
	}
}

func TestRetryLifecycleResultPreservesMinimalDeletionReceipt(t *testing.T) {
	operation := testOperation()
	for _, result := range []LifecycleResult{{Operation: &operation}, {Receipt: ptr(testReceipt(t))}} {
		command := CommandResult{Command: RetryLifecycleCommand, Lifecycle: &result}
		if err := command.Validate(); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if result.Receipt != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 || fields["receipt"] == nil {
				t.Fatal("completed delete retry returned operation content")
			}
			for _, forbidden := range []string{"sha256:", "name", "description", "project_version", "command_key", "request_digest"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatalf("completed delete retry leaked %s", forbidden)
				}
			}
		}
	}
}
