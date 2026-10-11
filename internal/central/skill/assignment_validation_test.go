package skill

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled rows exercise the real decoders and read-only transaction gates.
// They do not implement postgres.Rows, prove a canonical current set in PG, or
// supply Execution authorization. The current-set classifier is tested with
// typed complete facts separately; actual streaming/current integration is PG.
type assignmentValidationStore struct {
	*assignmentLookupStore
	change, grant, revision skillRowValues
	reads, writes, streams  int
}

func (s *assignmentValidationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *assignmentValidationStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.streams++
	return nil, errors.New("unplanned controlled streaming query")
}
func (s *assignmentValidationStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.writes++
	return pgconn.CommandTag{}, errors.New("read-only validator attempted write")
}
func (s *assignmentValidationStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	s.reads++
	if !s.live || strings.Contains(q, "agenteam_execution.") || strings.Contains(q, "revision_id=s.") || strings.Contains(q, "s.revision_id") || strings.Contains(q, "assignment_current") {
		return skillRowValues{err: errors.New("nonlocal, mutable or detached pending read")}
	}
	r := s.record
	c := *r.Candidate.Runtime
	switch {
	case strings.HasPrefix(q, "SELECT runtime_data,state_data,state_digest "):
		if !reflect.DeepEqual(args, []any{c.ID.String(), c.ProjectID.String(), c.AgentID.String(), int64(c.AssignmentSequence), int64(c.AgentVersion), c.RecordedAt.Time()}) {
			return skillRowValues{err: errors.New("change identity not complete")}
		}
		return s.change
	case strings.HasPrefix(q, "SELECT idempotency_key,input,candidate "):
		if !reflect.DeepEqual(args, []any{c.ProjectID.String(), c.AgentID.String(), c.ID.String()}) {
			return skillRowValues{err: errors.New("original command identity lost")}
		}
		_, raw, err := captureDigest(r.Candidate)
		return skillRowValues{values: []any{string(r.Input.Meta.IdempotencyKey), r.Input.Raw, raw}, err: err}
	case strings.Contains(q, "FROM agenteam_skill.agent_assignments WHERE id=$1"):
		if !reflect.DeepEqual(args, []any{c.Binding.AssignmentID.String()}) {
			return skillRowValues{err: errors.New("grant identity lost")}
		}
		return s.grant
	case strings.Contains(q, "FROM agenteam_skill.revisions WHERE id=$1"):
		if !reflect.DeepEqual(args, []any{c.Binding.RevisionID.String()}) {
			return skillRowValues{err: errors.New("fixed revision identity lost")}
		}
		return s.revision
	case strings.Contains(q, "FROM agenteam_skill.initializations"):
		if !reflect.DeepEqual(args, []any{c.ProjectID.String()}) {
			return skillRowValues{err: errors.New("original provenance scope lost")}
		}
		return s.row
	default:
		return s.assignmentLookupStore.QueryRow(ctx, q, args...)
	}
}

func assignmentValidationFixture(t *testing.T) (*AssignmentValidation, *assignmentValidationStore, sc.RuntimeAssignmentChange, i.ExecutionID) {
	t.Helper()
	_, base, projects, actor, _ := readerFixture(t)
	origin, err := scanInitialization(base.row)
	if err != nil || origin == nil {
		t.Fatal("controlled original initialization missing", err)
	}
	r := assignmentChangedControl(t, actor, true)
	revision := assignmentRevision{ID: origin.revision, SkillID: origin.skill, ProjectID: origin.request.ProjectID, Revision: origin.bundle.revision, Name: origin.bundle.name, Description: origin.bundle.description, EntryPath: sc.EntryPath, ObjectID: origin.object, ObjectVersion: 1, ObjectCreatedAt: origin.created, PublishedAt: origin.updated, PackageSHA256: origin.bundle.packageDigest, ManifestDigest: origin.bundle.manifestDigest}
	r.Candidate.Revision = &revision
	r.Candidate.Runtime.Binding = bindingForAssignment(*r.Candidate.Added, revision)
	e := stateID[i.Execution](901)
	routing := &sc.AssignmentIngressReceipt{ChangeID: r.ID, Queued: true, ExecutionID: &e}
	_, r.RuntimeReceipt, err = captureDigest(routing)
	if err != nil {
		t.Fatal(err)
	}
	_, r.Receipt, err = captureDigest(r.mutation(routing))
	r.State = "completed"
	if err != nil || r.validate() != nil {
		t.Fatal("controlled completed assignment invalid", err)
	}
	c, a := *r.Candidate.Runtime, *r.Candidate.Added
	_, runtime, _ := captureDigest(c)
	digest, state, _ := captureDigest(r.Candidate.After)
	s := &assignmentValidationStore{
		assignmentLookupStore: &assignmentLookupStore{initializationReadStore: base, record: r, pending: []any{e.String(), c.ProjectID.String(), c.AgentID.String(), a.ID.String(), int64(a.Sequence), a.SkillID.String(), revision.ID.String(), int64(revision.Revision), revision.ObjectID.String()}},
		change:                skillRowValues{values: []any{runtime, state, string(digest)}},
		grant:                 skillRowValues{values: []any{a.ProjectID.String(), a.AgentID.String(), a.SkillID.String(), a.Enabled, int64(a.Sequence), a.CreatedAt.Time(), (*time.Time)(nil)}},
		revision:              skillRowValues{values: []any{revision.ProjectID.String(), revision.SkillID.String(), int64(revision.Revision), revision.ObjectID.String(), int64(revision.ObjectVersion), revision.ObjectCreatedAt.Time(), revision.PublishedAt.Time(), ""}},
	}
	owner, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewAssignmentValidation(owner)
	if err != nil {
		t.Fatal(err)
	}
	base.tx, base.live, base.held = f.NewTx(), true, map[string]f.LockMode{}
	p, _ := f.ProjectLock(c.ProjectID.String())
	schedule, _ := f.ProjectScheduleLock(c.ProjectID.String())
	agent, _ := f.AgentLock(c.AgentID.String())
	skill, _ := f.AggregateLock(f.SkillAggregate, c.Binding.SkillID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, e.String())
	for _, lock := range []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Exclusive}, {Key: skill, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}} {
		base.held[lock.Key.Canonical()] = lock.Mode
	}
	return v, s, c, e
}

func TestSkillAssignmentValidationRequiresSameLiveTransactionAndLocks(t *testing.T) {
	if _, err := NewAssignmentValidation(nil); err == nil {
		t.Fatal("unbound owner accepted")
	}
	for _, method := range []string{"sequence", "pending", "validity"} {
		for _, condition := range []string{"foreign-tx", "closed-tx", "cancelled", "missing-project", "missing-schedule", "missing-agent", "missing-specific"} {
			if method == "sequence" && condition == "missing-specific" {
				continue
			}
			t.Run(method+"/"+condition, func(t *testing.T) {
				v, s, c, e := assignmentValidationFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				tx := s.tx
				switch condition {
				case "foreign-tx":
					tx = f.NewTx()
				case "closed-tx":
					s.live = false
				case "cancelled":
					cancel()
				default:
					p, _ := f.ProjectLock(c.ProjectID.String())
					key := p
					switch condition {
					case "missing-schedule":
						key, _ = f.ProjectScheduleLock(c.ProjectID.String())
					case "missing-agent":
						key, _ = f.AgentLock(c.AgentID.String())
					case "missing-specific":
						key, _ = f.AggregateLock(f.SkillAggregate, c.Binding.SkillID.String())
					}
					delete(s.held, key.Canonical())
				}
				var err error
				switch method {
				case "sequence":
					_, err = v.ReadAssignmentSequenceInTx(ctx, tx, c.ProjectID, c.AgentID)
				case "pending":
					err = v.RequirePendingAssignmentInTx(ctx, tx, e, c)
				case "validity":
					_, err = v.ValidateAssignmentInTx(ctx, tx, c.ProjectID, c.AgentID, c.Binding.AssignmentID, c.Binding.SkillID, c.Binding.RevisionID, c.Binding.Revision)
				}
				if err == nil || s.reads != 0 || s.writes != 0 || s.streams != 0 || s.txs != 0 || s.acquires != 0 {
					t.Fatal("detached caller reached storage or reader took transaction ownership")
				}
			})
		}
	}
	t.Run("missing-execution", func(t *testing.T) {
		v, s, c, e := assignmentValidationFixture(t)
		key, _ := f.AggregateLock(f.ExecutionAggregate, e.String())
		delete(s.held, key.Canonical())
		if v.RequirePendingAssignmentInTx(context.Background(), s.tx, e, c) == nil || s.reads != 0 {
			t.Fatal("pending read escaped execution gate")
		}
	})
	t.Run("canonical-read-failure-is-not-default-sequence", func(t *testing.T) {
		v, s, c, _ := assignmentValidationFixture(t)
		sequence, err := v.ReadAssignmentSequenceInTx(context.Background(), s.tx, c.ProjectID, c.AgentID)
		if err == nil || sequence != 0 || s.reads == 0 || s.writes != 0 || s.txs != 0 || s.acquires != 0 {
			t.Fatal("missing original current source fabricated a sequence")
		}
	})
}

func TestSkillAssignmentValidationClassifiesCompleteCurrentGrant(t *testing.T) {
	for _, name := range []string{"valid", "removed", "disabled", "deleted", "foreign-project", "foreign-agent", "future-sequence", "missing-live-grant", "changed-live-grant", "removed-still-live"} {
		t.Run(name, func(t *testing.T) {
			actor, _ := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
			r := assignmentChangedControl(t, actor, true)
			current := r.Candidate.After
			grant := validationGrant{assignment: *r.Candidate.Added}
			serving, want := true, sc.AssignmentValid
			switch name {
			case "removed":
				grant.removed = &r.Candidate.Created
				current.Items, want = []sc.AgentSkillAssignment{}, sc.AssignmentRemoved
			case "disabled":
				grant.assignment.Enabled, current.Items[0].Enabled, want = false, false, sc.AssignmentDisabled
			case "deleted":
				serving, want = false, sc.AssignmentDeleted
			case "foreign-project":
				grant.assignment.ProjectID = stateID[i.Project](902)
			case "foreign-agent":
				grant.assignment.AgentID = stateID[i.Agent](903)
			case "future-sequence":
				grant.assignment.Sequence++
			case "missing-live-grant":
				current.Items = []sc.AgentSkillAssignment{}
			case "changed-live-grant":
				current.Items[0].SkillID = stateID[pc.Skill](904)
			case "removed-still-live":
				grant.removed = &r.Candidate.Created
			}
			got, err := classifyValidationGrant(current, grant, serving)
			if name == "valid" || name == "removed" || name == "disabled" || name == "deleted" {
				if err != nil || got != want || got.Validate() != nil {
					t.Fatal("honest current fact lost", got, err)
				}
			} else if err == nil || got != "" {
				t.Fatal("damaged identity became a skipped assignment")
			}
		})
	}
	if sc.AssignmentValidity("missing").Validate() == nil {
		t.Fatal("unrecognized validity accepted")
	}
}

func TestSkillAssignmentValidationPinsOriginalPendingProtection(t *testing.T) {
	for _, name := range []string{"original", "subsequently-removed", "foreign-execution", "changed-runtime", "changed-state", "missing-protection", "foreign-protection-object", "changed-grant", "foreign-revision", "changed-revision-object", "missing-provenance"} {
		t.Run(name, func(t *testing.T) {
			v, s, c, e := assignmentValidationFixture(t)
			switch name {
			case "subsequently-removed":
				at := c.RecordedAt.Time().Add(time.Second)
				s.grant.values[6] = &at
			case "foreign-execution":
				e = stateID[i.Execution](905)
				key, _ := f.AggregateLock(f.ExecutionAggregate, e.String())
				s.held[key.Canonical()] = f.Exclusive
			case "changed-runtime":
				changed := c
				changed.AgentVersion++
				_, s.change.values[0], _ = captureDigest(changed)
			case "changed-state":
				s.change.values[1] = []byte(`{}`)
			case "missing-protection":
				s.pendingErr = pgx.ErrNoRows
			case "foreign-protection-object":
				s.pending[8] = stateID[oc.StoredObject](906).String()
			case "changed-grant":
				s.grant.values[3] = false
			case "foreign-revision":
				s.revision.values[1] = stateID[pc.Skill](907).String()
			case "changed-revision-object":
				s.revision.values[3] = stateID[oc.StoredObject](908).String()
			case "missing-provenance":
				s.row.err = pgx.ErrNoRows
			}
			err := v.RequirePendingAssignmentInTx(context.Background(), s.tx, e, c)
			if name == "original" || name == "subsequently-removed" {
				if err != nil || s.reads < 7 || s.queries != 1 {
					t.Fatal("original fixed pending lost", err)
				}
			} else if err == nil {
				t.Fatal("pending accepted without its original complete proof")
			}
			if s.writes != 0 || s.streams != 0 || s.txs != 0 || s.acquires != 0 {
				t.Fatal("pending reader repaired protection or took transaction ownership")
			}
		})
	}
}
