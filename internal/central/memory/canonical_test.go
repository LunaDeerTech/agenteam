package memory

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
)

func testID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func testRevision(t *testing.T) c.Revision {
	return c.Revision{MemoryID: testID[c.Memory](t), Revision: 3, Status: c.Active, Content: c.Content{Type: c.Experience, Text: "Run the schema generator before migration", Tags: []string{"migration"}, Importance: c.High}, Provenance: c.Provenance{ExecutionID: testID[i.Execution](t), OperationID: testID[struct{}](t).String(), DerivedFrom: []c.ID{}}, CreatedAt: now()}
}
func requireFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("wanted %s; got %v", code, err)
	}
}
func TestConsolidationMayOnlyMutateExactInputRevision(t *testing.T) {
	r := testRevision(t)
	proposal := c.Proposal{Actions: []c.Action{{Kind: c.Update, TargetID: r.MemoryID.String(), ExpectedRevision: "3", Content: r.Content, Reason: "correct obsolete procedure"}}}
	if _, err := planActions(proposal, []c.Revision{r}); err != nil {
		t.Fatal(err)
	}
	_, err := planActions(proposal, nil)
	requireFault(t, err, f.Forbidden)
	r.Revision = 4
	_, err = planActions(proposal, []c.Revision{r})
	requireFault(t, err, f.Forbidden)
	r.Revision = 3
	r.Status = c.Deleted
	_, err = planActions(proposal, []c.Revision{r})
	requireFault(t, err, f.InvalidArgument)
}
func TestSensitiveGateRejectsBeforeAnyExternalConsumer(t *testing.T) {
	for _, text := range []string{"api_key=an-actual-private-value", "postgres://owner:actual-password@database/service", "-----BEGIN PRIVATE KEY-----", "Bearer private-token-value-123"} {
		_, err := inspect(context.Background(), SensitiveDetector{}, text)
		requireFault(t, err, f.Forbidden)
	}
	for _, text := range []string{"Never write DATABASE_PASSWORD to the log.", "迁移前先生成 schema；错误日志只记录 Secret 的名称。"} {
		got, err := inspect(context.Background(), SensitiveDetector{}, text)
		if err != nil || got.Text != text || got.Outcome != "allow" {
			t.Fatal(got.Outcome, err)
		}
	}
}
func TestMutationLockUnionIsCompleteAndOrdered(t *testing.T) {
	n := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	key, _ := f.AgentLock(n.AgentID.String())
	merged, err := normalizeLocks([]f.LockRequest{namespaceLock(n, f.Shared), {Key: key, Mode: f.Shared}, namespaceLock(n, f.Exclusive)})
	if err != nil || len(merged) != 2 || merged[1].Mode != f.Exclusive || f.CompareLockKeys(merged[0].Key, merged[1].Key) >= 0 {
		t.Fatal(merged, err)
	}
}
