package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func node(n int, parent *k.DocumentID) k.ScopeNode {
	return k.ScopeNode{ID: keyID[k.Document](n), ProjectID: keyID[id.Project](4), ParentID: parent, ContentVersion: 1, Status: k.Active}
}
func TestMoveRequiresExactParentCompleteSameProjectChain(t *testing.T) {
	root := node(10, nil)
	current := node(11, &root.ID)
	other := node(12, nil)
	req := k.MoveRequest{ExpectedParentID: &root.ID, TargetParentID: &other.ID}
	decision := must(k.CheckMove(current.ProjectID, current.ID, req, k.MoveFacts{Current: current, TargetAncestors: []k.ScopeNode{other}}))
	if !decision.Changed || *decision.From != root.ID || *decision.To != other.ID {
		t.Fatal(decision)
	}
	req.TargetParentID = &root.ID
	decision = must(k.CheckMove(current.ProjectID, current.ID, req, k.MoveFacts{Current: current, TargetAncestors: []k.ScopeNode{root}}))
	if decision.Changed {
		t.Fatal("same-parent was a change")
	}
	for _, facts := range []k.MoveFacts{
		{Current: current}, // missing ancestor
		{Current: root, TargetAncestors: []k.ScopeNode{root}},          // wrong target
		{Current: current, TargetAncestors: []k.ScopeNode{root, root}}, // repeated/excess
	} {
		_, e := k.CheckMove(current.ProjectID, current.ID, req, facts)
		requireError(t, e)
	}
	foreign := root
	foreign.ProjectID = keyID[id.Project](99)
	_, e := k.CheckMove(current.ProjectID, current.ID, req, k.MoveFacts{Current: current, TargetAncestors: []k.ScopeNode{foreign}})
	requireError(t, e)
	stale := req
	stale.ExpectedParentID = nil
	_, e = k.CheckMove(current.ProjectID, current.ID, stale, k.MoveFacts{Current: current, TargetAncestors: []k.ScopeNode{root}})
	requireError(t, e)
	child := node(13, &current.ID)
	req.TargetParentID = &child.ID
	_, e = k.CheckMove(current.ProjectID, current.ID, req, k.MoveFacts{Current: current, TargetAncestors: []k.ScopeNode{child, current, root}})
	requireError(t, e)
	req.TargetParentID = nil
	decision = must(k.CheckMove(current.ProjectID, current.ID, req, k.MoveFacts{Current: current}))
	if decision.To != nil || !decision.Changed {
		t.Fatal(decision)
	}
}
func TestSubtreeCanonicalDigestAndTopology(t *testing.T) {
	root := node(10, nil)
	child := node(11, &root.ID)
	nodes := []k.ScopeNode{child, root}
	d := must(k.SubtreeDigest(root.ProjectID, root.ID, nodes))
	wire := fmt.Sprintf(`{"nodes":[{"content_version":"1","id":"%s","parent_id":null,"status":"active"},{"content_version":"1","id":"%s","parent_id":"%s","status":"active"}],"project_id":"%s","root_id":"%s"}`, root.ID, child.ID, root.ID, root.ProjectID, root.ID)
	hash := sha256.Sum256(append([]byte("agenteam.knowledge.subtree.v1\x00"), []byte(wire)...))
	expected := f.Digest("sha256:" + hex.EncodeToString(hash[:]))
	if d != expected {
		t.Fatalf("canonical protocol changed: %s / %s", d, expected)
	}
	if d != must(k.SubtreeDigest(root.ProjectID, root.ID, []k.ScopeNode{root, child})) {
		t.Fatal("enumeration order changed digest")
	}
	if nodes[0].ID != child.ID {
		t.Fatal("input reordered")
	}
	changed := []k.ScopeNode{root, child}
	changed[1].ContentVersion++
	if d == must(k.SubtreeDigest(root.ProjectID, root.ID, changed)) {
		t.Fatal("version omitted")
	}
	outside := keyID[k.Document](90)
	root.ParentID = &outside
	requireOK(t, func() error { _, e := k.SubtreeDigest(root.ProjectID, root.ID, []k.ScopeNode{root, child}); return e }())
	for _, bad := range [][]k.ScopeNode{
		nil, {child}, {root, child, child}, {root, node(12, nil)},
		{root, node(12, ptr(keyID[k.Document](13))), node(13, ptr(keyID[k.Document](12)))},
	} {
		_, e := k.SubtreeDigest(root.ProjectID, root.ID, bad)
		requireError(t, e)
	}
	foreign := child
	foreign.ProjectID = keyID[id.Project](99)
	_, e := k.SubtreeDigest(root.ProjectID, root.ID, []k.ScopeNode{root, foreign})
	requireError(t, e)
	tomb := child
	tomb.Status = k.Deleted
	_, e = k.SubtreeDigest(root.ProjectID, root.ID, []k.ScopeNode{root, tomb})
	requireError(t, e)
}
func mutation() k.TreeMutation {
	return must(k.NewMoveMutation(actor(), meta(), keyID[id.Project](4), keyID[k.Document](10), k.MoveRequest{TargetParentID: ptr(keyID[k.Document](20))}))
}
func baseLocks(r k.TreeMutation) []f.LockRequest {
	d := r.Details()
	command := must(k.CommandIdentity(d.ProjectID, d.Command, d.Meta.IdempotencyKey))
	return []f.LockRequest{{Key: must(f.CommandLock(command)), Mode: f.Exclusive}, {Key: must(f.UserLock(d.Actor.Details().UserID)), Mode: f.Exclusive}, {Key: must(f.ProjectLock(d.ProjectID.String())), Mode: f.Shared}, {Key: must(f.KnowledgeTreeLock(d.ProjectID.String())), Mode: f.Exclusive}}
}
func TestTreeRequestBindsAttemptAndCopiesPointers(t *testing.T) {
	r := mutation()
	d := r.Details()
	*d.Move.TargetParentID = keyID[k.Document](77)
	if *r.Details().Move.TargetParentID == *d.Move.TargetParentID {
		t.Fatal("request pointer aliased")
	}
	fresh := mutation()
	if !r.Equal(fresh) {
		t.Fatal("same request differs")
	}
	m := meta()
	m.RequestID = keyID[f.Request](99)
	changed := must(k.NewMoveMutation(actor(), m, r.Details().ProjectID, r.Details().DocumentID, *r.Details().Move))
	if r.Equal(changed) {
		t.Fatal("attempt request ID not bound")
	}
	session := must(id.NewHuman(keyID[id.User](1), keyID[id.Session](99)))
	changed = must(k.NewMoveMutation(session, meta(), r.Details().ProjectID, r.Details().DocumentID, *r.Details().Move))
	if r.Equal(changed) {
		t.Fatal("session not bound")
	}
	if must(k.MoveDigest(actor(), meta(), r.Details().ProjectID, r.Details().DocumentID, *r.Details().Move)) != must(k.MoveDigest(session, m, r.Details().ProjectID, r.Details().DocumentID, *r.Details().Move)) {
		t.Fatal("semantic and request bindings conflated")
	}
}
func TestPlanRequiresCompleteLocksAndPrivateIssuerIdentity(t *testing.T) {
	r := mutation()
	issuer := k.NewMutationIssuer()
	needs := baseLocks(r)
	for i := range needs {
		missing := slices.Delete(slices.Clone(needs), i, i+1)
		_, e := k.NewMutationPlan(issuer, k.MutationPlanDetails{Request: r, DomainMapping: sha(), Locks: missing})
		requireError(t, e)
		if needs[i].Mode == f.Exclusive {
			weak := slices.Clone(needs)
			weak[i].Mode = f.Shared
			_, e = k.NewMutationPlan(issuer, k.MutationPlanDetails{Request: r, DomainMapping: sha(), Locks: weak})
			requireError(t, e)
		}
	}
	locks := append(slices.Clone(needs), f.LockRequest{Key: needs[2].Key, Mode: f.Exclusive})
	slices.Reverse(locks)
	plan := must(k.NewMutationPlan(issuer, k.MutationPlanDetails{Request: r, DomainMapping: sha(), Locks: locks}))
	if !plan.Matches(issuer, r) || plan.Matches(k.NewMutationIssuer(), r) {
		t.Fatal("issuer mismatch")
	}
	normalized := plan.Locks()
	if len(normalized) != 4 || normalized[2].Mode != f.Exclusive {
		t.Fatal("lock union lost strongest mode")
	}
	for i := 1; i < len(normalized); i++ {
		if f.CompareLockKeys(normalized[i-1].Key, normalized[i].Key) >= 0 {
			t.Fatal("unsorted")
		}
	}
	normalized[0].Mode = f.Shared
	details := plan.Details()
	details.Locks = nil
	if plan.Locks()[0].Mode != f.Exclusive {
		t.Fatal("plan lock mutation escaped")
	}
	tx := f.NewTx()
	held := must(k.NewLockedMutation(issuer, k.LockedMutationDetails{Tx: tx, Plan: plan}))
	if !held.Matches(issuer, tx, plan, r) || held.Matches(issuer, f.NewTx(), plan, r) || held.Matches(k.NewMutationIssuer(), tx, plan, r) {
		t.Fatal("transaction/issuer binding")
	}
	otherPlan := must(k.NewMutationPlan(issuer, plan.Details()))
	if held.Matches(issuer, tx, otherPlan, r) {
		t.Fatal("equal parameters forged plan identity")
	}
	extra := f.LockRequest{Key: must(f.RecordLock(f.ReferenceRecordLock, "knowledge-test")), Mode: f.Exclusive}
	held = must(k.NewLockedMutation(issuer, k.LockedMutationDetails{Tx: tx, Plan: plan, ExtraLocks: []f.LockRequest{extra}}))
	got := held.Details()
	got.ExtraLocks[0].Mode = f.Shared
	if held.Details().ExtraLocks[0].Mode != f.Exclusive || len(held.Locks()) != 5 {
		t.Fatal("extra snapshot")
	}
	for _, v := range []any{r, issuer, plan, held} {
		if strings.Contains(fmt.Sprintf("%#v", v), string(meta().IdempotencyKey)) {
			t.Fatal("private plan key leaked")
		}
	}
	var zero k.LockedMutation
	requireError(t, zero.Validate())
	if zero.Matches(issuer, tx, plan, r) || len(zero.Locks()) != 0 {
		t.Fatal("zero accepted")
	}
}
func TestPlanComposesFormalObjectAndOutboxHandles(t *testing.T) {
	r := mutation()
	issuer := k.NewMutationIssuer()
	locks := baseLocks(r)
	objectLock := f.LockRequest{Key: must(f.AggregateLock(f.ObjectAggregate, keyID[oc.StoredObject](50).String())), Mode: f.Exclusive}
	owner := must(oc.NewObjectOwner(oc.Knowledge, r.Details().DocumentID.String(), r.Details().ProjectID.String()))
	request := must(oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PrepareAccess, Actor: actor(), Owner: owner, Intent: id.Mutate}))
	providerIssuer := oc.NewAccessIssuer()
	objectPlan := must(oc.NewAccessLockPlan(providerIssuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: must(oc.NewAccessDependencies(sha(), []f.LockRequest{objectLock})), DomainBinding: sha(), Locks: []f.LockRequest{objectLock}}))
	d := doc(10)
	catalog := event.NewCatalog()
	events := must(k.RegisterKnowledgeEvents(catalog))
	evt := must(events.Deleted(eventHeader(d, k.DeletedEvent), k.DeletedPayload{DocumentID: d.ID, ContentVersion: d.ContentVersion}))
	eventLock := f.LockRequest{Key: must(f.RecordLock(f.OutboxRecordLock, "knowledge-event")), Mode: f.Exclusive}
	outboxIssuer := ob.NewPlanIssuer()
	deps := must(ob.NewDependencies(outboxIssuer, sha(), []f.LockRequest{eventLock}, nil))
	appendPlan := must(ob.NewAppendPlan(outboxIssuer, ob.AppendPlanDetails{Event: evt, Semantic: must(ob.SemanticDigest(actor(), evt)), Producer: deps, Project: deps, Locks: []f.LockRequest{eventLock}}))
	details := k.MutationPlanDetails{Request: r, DomainMapping: sha(), ObjectPlans: []oc.AccessLockPlan{objectPlan}, EventPlans: []ob.AppendPlan{appendPlan}, Locks: locks}
	_, e := k.NewMutationPlan(issuer, details)
	requireError(t, e)
	details.Locks = append(locks, objectLock, eventLock)
	plan := must(k.NewMutationPlan(issuer, details))
	tx := f.NewTx()
	_, e = k.NewLockedMutation(issuer, k.LockedMutationDetails{Tx: tx, Plan: plan})
	requireError(t, e)
	access := must(oc.NewLockedAccess(providerIssuer, tx, []oc.AccessLockPlan{objectPlan}, details.Locks))
	held := must(k.NewLockedMutation(issuer, k.LockedMutationDetails{Tx: tx, Plan: plan, ObjectAccess: access}))
	if !held.Matches(issuer, tx, plan, r) || !held.Details().ObjectAccess.Matches(providerIssuer, tx, objectPlan, request) {
		t.Fatal("typed provider handles lost")
	}
	detailCopy := plan.Details()
	detailCopy.ObjectPlans[0] = oc.AccessLockPlan{}
	detailCopy.EventPlans[0] = ob.AppendPlan{}
	if plan.Details().ObjectPlans[0].Validate() != nil || plan.Details().EventPlans[0].Details().Event.Validate() != nil {
		t.Fatal("slice alias")
	}
	var decoded k.MutationPlan
	requireError(t, json.Unmarshal([]byte(`"knowledge_mutation_plan"`), &decoded))
}

func TestDeletePreviewBindsExactScopeAndExpiry(t *testing.T) {
	root := doc(10)
	child := doc(11)
	child.ParentDocumentID = &root.ID
	scope := must(k.SubtreeDigest(root.ProjectID, root.ID, []k.ScopeNode{{ID: root.ID, ProjectID: root.ProjectID, ContentVersion: root.ContentVersion, Status: root.Status}, {ID: child.ID, ProjectID: child.ProjectID, ParentID: child.ParentDocumentID, ContentVersion: child.ContentVersion, Status: child.Status}}))
	c := claims()
	c.ScopeDigest = scope
	keys := must(k.LoadConfirmationKeys(keyJSON("old", true, false)))
	preview := k.DeletePreview{Root: root.ID, Nodes: []k.DocumentRef{root, child}, ScopeDigest: scope, Confirmation: must(keys.Sign(c)), ExpiresAt: c.ExpiresAt}
	requireOK(t, preview.Validate())
	for _, change := range []func(*k.DeletePreview){
		func(p *k.DeletePreview) { p.ExpiresAt = instant("2026-01-01T00:11:00Z") },
		func(p *k.DeletePreview) { p.Confirmation = token() },
		func(p *k.DeletePreview) { p.Nodes = []k.DocumentRef{root} },
		func(p *k.DeletePreview) { p.Root = child.ID },
	} {
		bad := preview
		change(&bad)
		requireError(t, bad.Validate())
	}
	_, e := json.Marshal(preview)
	requireError(t, e)
}
