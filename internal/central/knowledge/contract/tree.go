package contract

import (
	"context"
	"fmt"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"io"
	"log/slog"
	"slices"
)

type MoveRequest struct {
	ExpectedParentID *DocumentID `json:"expected_parent_id"`
	TargetParentID   *DocumentID `json:"target_parent_id"`
}

func (r MoveRequest) Validate() error {
	if !validParent(r.ExpectedParentID) || !validParent(r.TargetParentID) {
		return invalid()
	}
	return nil
}
func copyMove(r MoveRequest) MoveRequest {
	return MoveRequest{clonePtr(r.ExpectedParentID), clonePtr(r.TargetParentID)}
}

type MoveResult struct {
	Document DocumentRef `json:"document"`
	Changed  bool        `json:"changed"`
}

func (r MoveResult) Validate() error {
	if r.Document.Status != Active {
		return invalid()
	}
	return r.Document.Validate()
}

type ScopeNode struct {
	ID             DocumentID     `json:"id"`
	ProjectID      id.ProjectID   `json:"project_id"`
	ParentID       *DocumentID    `json:"parent_id"`
	ContentVersion f.Version      `json:"content_version"`
	Status         DocumentStatus `json:"status"`
}

func (n ScopeNode) Validate() error {
	if n.ID.Validate() != nil || n.ProjectID.Validate() != nil || !validParent(n.ParentID) || n.ParentID != nil && *n.ParentID == n.ID || n.ContentVersion.Validate() != nil || n.Status.Validate() != nil {
		return invalid()
	}
	return nil
}

type MoveFacts struct {
	Current         ScopeNode
	TargetAncestors []ScopeNode
}
type MoveDecision struct {
	From, To *DocumentID
	Changed  bool
}

func CheckMove(project id.ProjectID, target DocumentID, request MoveRequest, facts MoveFacts) (MoveDecision, error) {
	current := facts.Current
	if project.Validate() != nil || target.Validate() != nil || request.Validate() != nil || current.Validate() != nil || current.ID != target || current.ProjectID != project || current.Status != Active {
		return MoveDecision{}, invalid()
	}
	if !samePtr(request.ExpectedParentID, current.ParentID) {
		return MoveDecision{}, fault(f.VersionConflict)
	}
	next := clonePtr(request.TargetParentID)
	seen := map[DocumentID]bool{target: true}
	for _, n := range facts.TargetAncestors {
		if n.Validate() != nil || n.ProjectID != project || n.Status != Active || next == nil || n.ID != *next || seen[n.ID] {
			return MoveDecision{}, invalid()
		}
		seen[n.ID] = true
		next = clonePtr(n.ParentID)
	}
	if next != nil {
		return MoveDecision{}, invalid()
	}
	return MoveDecision{clonePtr(current.ParentID), clonePtr(request.TargetParentID), !samePtr(current.ParentID, request.TargetParentID)}, nil
}
func SubtreeDigest(project id.ProjectID, root DocumentID, nodes []ScopeNode) (f.Digest, error) {
	if project.Validate() != nil || root.Validate() != nil || len(nodes) == 0 {
		return "", invalid()
	}
	byID := make(map[DocumentID]ScopeNode, len(nodes))
	copied := make([]ScopeNode, len(nodes))
	for i, n := range nodes {
		if n.Validate() != nil || n.ProjectID != project || n.Status != Active {
			return "", invalid()
		}
		if _, ok := byID[n.ID]; ok {
			return "", invalid()
		}
		n.ParentID = clonePtr(n.ParentID)
		byID[n.ID] = n
		copied[i] = n
	}
	r, ok := byID[root]
	if !ok {
		return "", invalid()
	}
	if r.ParentID != nil {
		if _, inside := byID[*r.ParentID]; inside {
			return "", invalid()
		}
	}
	// Every path must reach the requested root. Mark completed paths so the
	// check is linear even for a long chain; no recursive stack is needed.
	connected := map[DocumentID]bool{root: true}
	for _, n := range copied {
		path := make([]DocumentID, 0)
		visiting := map[DocumentID]bool{}
		for !connected[n.ID] {
			if visiting[n.ID] || n.ParentID == nil {
				return "", invalid()
			}
			visiting[n.ID] = true
			path = append(path, n.ID)
			parent, ok := byID[*n.ParentID]
			if !ok {
				return "", invalid()
			}
			n = parent
		}
		for _, x := range path {
			connected[x] = true
		}
	}
	slices.SortFunc(copied, func(a, b ScopeNode) int { return compareIDs(a.ID, b.ID) })
	type tuple struct {
		ID      DocumentID     `json:"id"`
		Parent  *DocumentID    `json:"parent_id"`
		Version f.Version      `json:"content_version"`
		Status  DocumentStatus `json:"status"`
	}
	tuples := make([]tuple, len(copied))
	for i, n := range copied {
		tuples[i] = tuple{n.ID, n.ParentID, n.ContentVersion, n.Status}
	}
	return digest("agenteam.knowledge.subtree.v1", struct {
		Project id.ProjectID `json:"project_id"`
		Root    DocumentID   `json:"root_id"`
		Nodes   []tuple      `json:"nodes"`
	}{project, root, tuples})
}
func compareIDs(a, b DocumentID) int {
	if a.String() < b.String() {
		return -1
	}
	if a == b {
		return 0
	}
	return 1
}

type DeletePreview struct {
	Root         DocumentID        `json:"root"`
	Nodes        []DocumentRef     `json:"nodes"`
	ScopeDigest  f.Digest          `json:"scope_digest"`
	Confirmation ConfirmationToken `json:"-"`
	ExpiresAt    f.Instant         `json:"expires_at"`
}

func (p DeletePreview) Validate() error {
	if p.Root.Validate() != nil || len(p.Nodes) == 0 || p.ScopeDigest.Validate() != nil || p.Confirmation.Validate() != nil || p.ExpiresAt.Validate() != nil {
		return invalid()
	}
	nodes := make([]ScopeNode, len(p.Nodes))
	for i, d := range p.Nodes {
		if d.Validate() != nil {
			return invalid()
		}
		nodes[i] = ScopeNode{d.ID, d.ProjectID, d.ParentDocumentID, d.ContentVersion, d.Status}
	}
	h, e := SubtreeDigest(p.Nodes[0].ProjectID, p.Root, nodes)
	if e != nil || h != p.ScopeDigest {
		return invalid()
	}
	claims := p.Confirmation.data().claims
	if claims.ProjectID != p.Nodes[0].ProjectID || claims.RootID != p.Root || claims.ScopeDigest != p.ScopeDigest || claims.ExpiresAt != p.ExpiresAt {
		return invalid()
	}
	return nil
}

// Confirmation material requires an explicit authorized human response
// projection. A generic preview encoding must not silently drop or emit it.
func (DeletePreview) MarshalJSON() ([]byte, error) { return nil, invalid() }
func (*DeletePreview) UnmarshalJSON([]byte) error  { return invalid() }

type DeleteResult struct {
	Root           DocumentID   `json:"root"`
	DeletedIDs     []DocumentID `json:"deleted_ids"`
	CleanupPending bool         `json:"cleanup_pending"`
}

func (r DeleteResult) Validate() error {
	if r.Root.Validate() != nil || len(r.DeletedIDs) == 0 {
		return invalid()
	}
	hasRoot := false
	for i, d := range r.DeletedIDs {
		if d.Validate() != nil || i > 0 && compareIDs(r.DeletedIDs[i-1], d) >= 0 {
			return invalid()
		}
		hasRoot = hasRoot || d == r.Root
	}
	if !hasRoot {
		return invalid()
	}
	return nil
}

type TreeMutationDetails struct {
	Command    CommandName
	Actor      id.Actor
	Meta       f.CommandMeta
	ProjectID  id.ProjectID
	DocumentID DocumentID
	Move       *MoveRequest
	Delete     *ConfirmationToken
}
type mutationData struct {
	details TreeMutationDetails
	binding f.Digest
}
type TreeMutation struct{ data func() mutationData }

func copyMutation(d TreeMutationDetails) TreeMutationDetails {
	d.Meta.ExpectedVersion = clonePtr(d.Meta.ExpectedVersion)
	if d.Move != nil {
		x := copyMove(*d.Move)
		d.Move = &x
	}
	d.Delete = clonePtr(d.Delete)
	return d
}
func NewMoveMutation(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, r MoveRequest) (TreeMutation, error) {
	if _, e := MoveDigest(a, m, p, d, r); e != nil {
		return TreeMutation{}, e
	}
	return newMutation(TreeMutationDetails{Command: Move, Actor: a, Meta: m, ProjectID: p, DocumentID: d, Move: &r})
}
func NewDeleteMutation(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, t ConfirmationToken) (TreeMutation, error) {
	if _, e := DeleteDigest(a, m, p, d, t); e != nil {
		return TreeMutation{}, e
	}
	return newMutation(TreeMutationDetails{Command: DeleteSubtree, Actor: a, Meta: m, ProjectID: p, DocumentID: d, Delete: &t})
}
func newMutation(d TreeMutationDetails) (TreeMutation, error) {
	d = copyMutation(d)
	var token *f.Digest
	if d.Delete != nil {
		v := rawSHA([]byte(d.Delete.ForHumanResponse()))
		token = &v
	}
	b, e := digest("agenteam.knowledge.tree-request.v1", struct {
		Command  CommandName     `json:"command"`
		Actor    id.ActorDetails `json:"actor"`
		Meta     f.CommandMeta   `json:"meta"`
		Project  id.ProjectID    `json:"project_id"`
		Document DocumentID      `json:"document_id"`
		Move     *MoveRequest    `json:"move"`
		Token    *f.Digest       `json:"confirmation_sha256"`
	}{d.Command, d.Actor.Details(), d.Meta, d.ProjectID, d.DocumentID, d.Move, token})
	if e != nil {
		return TreeMutation{}, e
	}
	return TreeMutation{func() mutationData { return mutationData{copyMutation(d), b} }}, nil
}
func (r TreeMutation) Validate() error {
	if r.data == nil {
		return invalid()
	}
	return nil
}
func (r TreeMutation) Details() TreeMutationDetails {
	if r.data == nil {
		return TreeMutationDetails{}
	}
	return r.data().details
}
func (r TreeMutation) Binding() f.Digest {
	if r.data == nil {
		return ""
	}
	return r.data().binding
}
func (r TreeMutation) Equal(s TreeMutation) bool {
	return r.data != nil && s.data != nil && r.Binding() == s.Binding()
}

type MutationIssuer struct{ data func() f.Tx }

func NewMutationIssuer() MutationIssuer {
	x := f.NewTx()
	return MutationIssuer{func() f.Tx { return x }}
}
func (i MutationIssuer) Validate() error {
	if i.data == nil {
		return invalid()
	}
	return nil
}
func (i MutationIssuer) equal(j MutationIssuer) bool {
	return i.data != nil && j.data != nil && i.data() == j.data()
}

type MutationPlanDetails struct {
	Request       TreeMutation
	DomainMapping f.Digest
	ObjectPlans   []oc.AccessLockPlan
	EventPlans    []ob.AppendPlan
	Locks         []f.LockRequest
}
type planData struct {
	issuer   MutationIssuer
	identity f.Tx
	details  MutationPlanDetails
}
type MutationPlan struct{ data func() planData }

func copyPlan(d MutationPlanDetails) MutationPlanDetails {
	d.ObjectPlans = append([]oc.AccessLockPlan(nil), d.ObjectPlans...)
	d.EventPlans = append([]ob.AppendPlan(nil), d.EventPlans...)
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	return d
}
func requiredLocks(r TreeMutation) ([]f.LockRequest, error) {
	if r.Validate() != nil {
		return nil, invalid()
	}
	d := r.Details()
	command, e := CommandIdentity(d.ProjectID, d.Command, d.Meta.IdempotencyKey)
	if e != nil {
		return nil, e
	}
	c, _ := f.CommandLock(command)
	u, _ := f.UserLock(d.Actor.Details().UserID)
	p, _ := f.ProjectLock(d.ProjectID.String())
	t, _ := f.KnowledgeTreeLock(d.ProjectID.String())
	return []f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Shared}, {Key: t, Mode: f.Exclusive}}, nil
}
func covers(locks, needs []f.LockRequest) bool {
	for _, need := range needs {
		found := false
		for _, have := range locks {
			if have.Key.Canonical() == need.Key.Canonical() && (have.Mode == f.Exclusive || have.Mode == need.Mode) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func sameLocks(a, b []f.LockRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key.Canonical() != b[i].Key.Canonical() || a[i].Mode != b[i].Mode {
			return false
		}
	}
	return true
}
func NewMutationPlan(i MutationIssuer, d MutationPlanDetails) (MutationPlan, error) {
	if i.Validate() != nil || d.Request.Validate() != nil || d.DomainMapping.Validate() != nil {
		return MutationPlan{}, invalid()
	}
	need, e := requiredLocks(d.Request)
	if e != nil {
		return MutationPlan{}, e
	}
	for _, p := range d.ObjectPlans {
		if p.Validate() != nil {
			return MutationPlan{}, invalid()
		}
		v := p.Details()
		need = append(need, v.Locks...)
		need = append(need, v.Dependencies.Locks()...)
	}
	for _, p := range d.EventPlans {
		v := p.Details()
		if v.Event.Validate() != nil || v.Semantic.Validate() != nil || v.Producer.Validate() != nil || v.Project.Validate() != nil || v.Event.Header().Scope.Kind != event.ProjectScope || v.Event.Header().Scope.ProjectID.String() != d.Request.Details().ProjectID.String() {
			return MutationPlan{}, invalid()
		}
		need = append(need, p.Locks()...)
		need = append(need, v.Producer.Locks()...)
		need = append(need, v.Project.Locks()...)
	}
	normalized, e := oc.NormalizeAccessLocks(d.Locks)
	if e != nil || !covers(normalized, need) {
		return MutationPlan{}, invalid()
	}
	d.Locks = normalized
	d = copyPlan(d)
	identity := f.NewTx()
	return MutationPlan{func() planData { return planData{i, identity, copyPlan(d)} }}, nil
}
func (p MutationPlan) Validate() error {
	if p.data == nil {
		return invalid()
	}
	return nil
}
func (p MutationPlan) Details() MutationPlanDetails {
	if p.data == nil {
		return MutationPlanDetails{}
	}
	return p.data().details
}
func (p MutationPlan) IssuedBy(i MutationIssuer) bool {
	return p.data != nil && p.data().issuer.equal(i)
}
func (p MutationPlan) Matches(i MutationIssuer, r TreeMutation) bool {
	return p.IssuedBy(i) && p.Details().Request.Equal(r)
}
func (p MutationPlan) Locks() []f.LockRequest { return p.Details().Locks }

type LockedMutationDetails struct {
	Tx           f.Tx
	Plan         MutationPlan
	ExtraLocks   []f.LockRequest
	ObjectAccess oc.LockedAccess
}
type lockedData struct {
	issuer  MutationIssuer
	details LockedMutationDetails
	locks   []f.LockRequest
}
type LockedMutation struct{ data func() lockedData }

func NewLockedMutation(i MutationIssuer, d LockedMutationDetails) (LockedMutation, error) {
	if !d.Tx.Valid() || !d.Plan.IssuedBy(i) {
		return LockedMutation{}, invalid()
	}
	extra, e := oc.NormalizeAccessLocks(d.ExtraLocks)
	if e != nil {
		return LockedMutation{}, invalid()
	}
	union := append(d.Plan.Locks(), extra...)
	union, e = oc.NormalizeAccessLocks(union)
	if e != nil {
		return LockedMutation{}, invalid()
	}
	if len(d.Plan.Details().ObjectPlans) > 0 {
		// This only checks the supplied lock projection. D05 must subsequently
		// validate its issuer, exact plan identities, transaction and live facts.
		if !covers(d.ObjectAccess.Locks(), union) {
			return LockedMutation{}, invalid()
		}
	} else if len(d.ObjectAccess.Locks()) != 0 {
		return LockedMutation{}, invalid()
	}
	d.ExtraLocks = extra
	return LockedMutation{func() lockedData {
		x := d
		x.ExtraLocks = append([]f.LockRequest(nil), extra...)
		return lockedData{i, x, append([]f.LockRequest(nil), union...)}
	}}, nil
}
func (l LockedMutation) Validate() error {
	if l.data == nil {
		return invalid()
	}
	return nil
}
func (l LockedMutation) Details() LockedMutationDetails {
	if l.data == nil {
		return LockedMutationDetails{}
	}
	return l.data().details
}
func (l LockedMutation) Locks() []f.LockRequest {
	if l.data == nil {
		return nil
	}
	return l.data().locks
}
func (l LockedMutation) Matches(i MutationIssuer, tx f.Tx, p MutationPlan, r TreeMutation) bool {
	if l.data == nil || !tx.Valid() || !p.Matches(i, r) {
		return false
	}
	d := l.data()
	return d.issuer.equal(i) && d.details.Tx == tx && d.details.Plan.data().identity == p.data().identity && d.details.Plan.Matches(i, r) && sameLocks(d.details.Plan.Locks(), p.Locks())
}

type TreeMutationPlanning interface {
	DiscoverMutation(context.Context, TreeMutation) (MutationPlan, error)
	AcquireMutationInTx(context.Context, f.Tx, MutationPlan, []f.LockRequest) (LockedMutation, error)
}
type AtomicTreeMutations interface {
	MoveDocumentInTx(context.Context, f.Tx, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, MoveRequest, LockedMutation) (MoveResult, error)
	DeleteSubtreeInTx(context.Context, f.Tx, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, ConfirmationToken, LockedMutation) (DeleteResult, error)
}

func (v MoveRequest) MarshalJSON() ([]byte, error) {
	type wire MoveRequest
	return checked(wire(v), v.Validate())
}
func (v *MoveRequest) UnmarshalJSON(b []byte) error {
	type wire MoveRequest
	w, e := decode[wire](b, []string{"expected_parent_id", "target_parent_id"}, nil, []string{"expected_parent_id", "target_parent_id"})
	if e != nil {
		return e
	}
	n := MoveRequest(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v MoveResult) MarshalJSON() ([]byte, error) {
	type wire MoveResult
	return checked(wire(v), v.Validate())
}
func (v *MoveResult) UnmarshalJSON(b []byte) error {
	type wire MoveResult
	w, e := decode[wire](b, []string{"document", "changed"}, nil, nil)
	if e != nil {
		return e
	}
	n := MoveResult(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v ScopeNode) MarshalJSON() ([]byte, error) {
	type wire ScopeNode
	return checked(wire(v), v.Validate())
}
func (v *ScopeNode) UnmarshalJSON(b []byte) error {
	type wire ScopeNode
	w, e := decode[wire](b, []string{"id", "project_id", "parent_id", "content_version", "status"}, nil, []string{"parent_id"})
	if e != nil {
		return e
	}
	n := ScopeNode(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v DeleteResult) MarshalJSON() ([]byte, error) {
	type wire DeleteResult
	return checked(wire(v), v.Validate())
}
func (v *DeleteResult) UnmarshalJSON(b []byte) error {
	type wire DeleteResult
	w, e := decode[wire](b, []string{"root", "deleted_ids", "cleanup_pending"}, nil, nil)
	if e != nil {
		return e
	}
	n := DeleteResult(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (TreeMutation) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_tree_mutation") }
func (TreeMutation) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_tree_mutation"`), nil }
func (*TreeMutation) UnmarshalJSON([]byte) error  { return invalid() }
func (TreeMutation) LogValue() slog.Value         { return slog.StringValue("knowledge_tree_mutation") }

func (MutationIssuer) Format(w fmt.State, _ rune) { io.WriteString(w, "knowledge_mutation_issuer") }
func (MutationIssuer) MarshalJSON() ([]byte, error) {
	return []byte(`"knowledge_mutation_issuer"`), nil
}
func (*MutationIssuer) UnmarshalJSON([]byte) error { return invalid() }
func (MutationIssuer) LogValue() slog.Value        { return slog.StringValue("knowledge_mutation_issuer") }

func (MutationPlan) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_mutation_plan") }
func (MutationPlan) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_mutation_plan"`), nil }
func (*MutationPlan) UnmarshalJSON([]byte) error  { return invalid() }
func (MutationPlan) LogValue() slog.Value         { return slog.StringValue("knowledge_mutation_plan") }

func (LockedMutation) Format(w fmt.State, _ rune) { io.WriteString(w, "knowledge_locked_mutation") }
func (LockedMutation) MarshalJSON() ([]byte, error) {
	return []byte(`"knowledge_locked_mutation"`), nil
}
func (*LockedMutation) UnmarshalJSON([]byte) error { return invalid() }
func (LockedMutation) LogValue() slog.Value        { return slog.StringValue("knowledge_locked_mutation") }
