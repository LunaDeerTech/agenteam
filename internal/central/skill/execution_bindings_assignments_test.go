package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Frozen format-1 wire definition: adding a changed-set source must not alter
// the bytes/digest of already persisted initialized captures.
type legacyCaptureWire struct {
	Result                sc.InitialSkillBindings `json:"result"`
	CreationCommand       string                  `json:"creation_command"`
	PlanRevision          f.Version               `json:"plan_revision"`
	RequestBinding        f.Digest                `json:"request_binding"`
	AddSkillsEnabled      bool                    `json:"add_skills_enabled"`
	InitialSkillID        sc.SkillID              `json:"initial_skill_id"`
	ObservedRevision      f.Revision              `json:"observed_revision"`
	HeadCreatedAt         f.Instant               `json:"head_created_at"`
	CreationID            pc.CreationID           `json:"creation_id"`
	InitializationKey     f.IdempotencyKey        `json:"initialization_key"`
	InitializationVersion f.Version               `json:"initialization_version"`
	InitializationDigest  f.Digest                `json:"initialization_digest"`
	Metadata              sc.Metadata             `json:"metadata"`
	RevisionID            sc.RevisionID           `json:"revision_id"`
	ObjectID              oc.ObjectID             `json:"object_id"`
	ObjectVersion         f.Version               `json:"object_version"`
	ObjectCreatedAt       f.Instant               `json:"object_created_at"`
	PublishedAt           f.Instant               `json:"published_at"`
	PackageDigest         f.Digest                `json:"package_digest"`
	ManifestDigest        f.Digest                `json:"manifest_digest"`
}

func TestSkillCaptureFormatOneBytesRemainStable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		p, _, _, ctx, r := captureFixture(t, enabled)
		plan, err := p.DiscoverInitialBindings(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		s := plan.(*initialBindingsPlan).source
		old := legacyCaptureWire{s.Result, s.CreationCommand, s.PlanRevision, s.RequestBinding, s.AddSkillsEnabled, s.InitialSkillID, s.ObservedRevision, s.HeadCreatedAt, s.CreationID, s.InitializationKey, s.InitializationVersion, s.InitializationDigest, s.Metadata, s.RevisionID, s.ObjectID, s.ObjectVersion, s.ObjectCreatedAt, s.PublishedAt, s.PackageDigest, s.ManifestDigest}
		a, raw, err := captureDigest(s)
		b, legacy, e := captureDigest(old)
		if err != nil || e != nil || a != b || !bytes.Equal(raw, legacy) || s.format() != 1 || s.validate() != nil {
			t.Fatal("legacy capture representation changed")
		}
	}
}

// These are controlled SQL rows, not a fake postgres.Rows implementation or
// a claim of real multi-Skill Preparation. Real current-set Query/rows and its
// transaction atomicity belong to the scoped PG fixture.
type changedCaptureStore struct {
	*captureBindingStore
	state            assignmentSet
	installationWire []any
	installation     installationRow
	revision         assignmentRevision
	currentReads     int
	brokenHistory    bool
	missingGrant     bool
	missingRevision  bool
}

func (s *changedCaptureStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.captureBindingStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *changedCaptureStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.Contains(q, "assignment_current"):
		s.currentReads++
		return skillRowValues{err: errors.New("historical reader consulted current assignment")}
	case strings.HasPrefix(q, "SELECT state_data,state_digest FROM agenteam_skill.assignment_changes"):
		if !reflect.DeepEqual(args, []any{s.state.Project.String(), s.state.Agent.String(), int64(s.state.Sequence)}) {
			return skillRowValues{err: errors.New("wrong historical sequence")}
		}
		d, raw, e := captureDigest(s.state)
		if s.brokenHistory {
			raw = []byte(`{}`)
		}
		return skillRowValues{values: []any{raw, string(d)}, err: e}
	case strings.HasPrefix(q, "SELECT count(*) FROM agenteam_skill.agent_assignments WHERE"):
		n := int64(0)
		if strings.Contains(q, "removed_at") {
			return skillRowValues{err: errors.New("removed historical grant became unavailable")}
		}
		for _, a := range s.state.Items {
			if reflect.DeepEqual(args, []any{a.ProjectID.String(), a.AgentID.String(), a.ID.String(), a.SkillID.String(), a.Enabled, int64(a.Sequence), a.CreatedAt.Time()}) && !s.missingGrant {
				n++
			}
		}
		return skillRowValues{values: []any{n}}
	case strings.Contains(q, "FROM agenteam_skill.installations WHERE"):
		if !strings.Contains(q, "revision_id=$2") || !reflect.DeepEqual(args, []any{s.installation.project.String(), s.installation.revision.String()}) {
			return skillRowValues{err: errors.New("current installation lookup")}
		}
		return skillRowValues{values: s.installationWire}
	case strings.HasPrefix(q, "SELECT count(*) FROM agenteam_skill.revisions r JOIN agenteam_skill.skills s") && args[1] == s.revision.SkillID.String():
		r := s.revision
		want := []any{r.ProjectID.String(), r.SkillID.String(), r.ID.String(), int64(r.Revision), r.ObjectID.String(), int64(r.ObjectVersion), r.ObjectCreatedAt.Time(), r.PublishedAt.Time(), s.installation.id.String()}
		count := int64(0)
		if !strings.Contains(q, "s.revision_id") && strings.Contains(q, "r.id=$3") && reflect.DeepEqual(args[:9], want) && args[10] == s.installation.attempt.String() && args[11] == s.installation.upload.String() && !s.missingRevision {
			count = 1
		}
		return skillRowValues{values: []any{count}}
	default:
		return s.captureBindingStore.QueryRow(ctx, q, args...)
	}
}

func changedCaptureFixture(t *testing.T) (*initialBindingsPlan, *changedCaptureStore, *captureOwnerDouble, context.Context) {
	t.Helper()
	p, base, owner, ctx, r := captureFixture(t, true)
	discovered, e := p.DiscoverInitialBindings(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	original := discovered.(*initialBindingsPlan)
	source := original.source
	state := source.assignmentSet()
	state.Sequence = 2
	installed := installRepositoryRow(t)
	installed.project = r.ProjectID
	installed.phase = installationPublished
	installed.object, installed.upload, installed.attempt = stateID[oc.StoredObject](981), stateID[oc.Upload](982), stateID[oc.Attempt](983)
	installed.semantic, e = installationSemantic(installed.project, installed.user, installed.skill, installed.pkg)
	if e != nil {
		t.Fatal(e)
	}
	fixed := assignmentRevision{installed.revision, installed.skill, r.ProjectID, 1, installed.pkg.name, installed.pkg.description, sc.EntryPath, installed.object, 1, installed.created, installed.updated, installed.pkg.packageDigest, installed.pkg.manifestDigest}
	grant := sc.AgentSkillAssignment{ID: stateID[sc.Assignment](984), ProjectID: r.ProjectID, AgentID: r.AgentID, SkillID: fixed.SkillID, Enabled: true, Sequence: 2, CreatedAt: installed.created}
	state.Items = append(state.Items, grant)
	slices.SortFunc(state.Items, func(a, b sc.AgentSkillAssignment) int { return strings.Compare(a.SkillID.String(), b.SkillID.String()) })
	builtin := assignmentRevision{source.RevisionID, source.InitialSkillID, r.ProjectID, source.ObservedRevision, source.Metadata.Name, source.Metadata.Description, sc.EntryPath, source.ObjectID, source.ObjectVersion, source.ObjectCreatedAt, source.PublishedAt, source.PackageDigest, source.ManifestDigest}
	source.Assignments = &skillCaptureAssignments{State: state, Revisions: []assignmentRevision{}}
	source.Result.AssignmentSequence = 2
	source.Result.Bindings = []sc.SkillBinding{}
	for _, a := range state.Items {
		revision := fixed
		if a.SkillID == source.InitialSkillID {
			revision = builtin
		}
		source.Assignments.Revisions = append(source.Assignments.Revisions, revision)
		source.Result.Bindings = append(source.Result.Bindings, bindingForAssignment(a, revision))
	}
	if e = source.validate(); e != nil {
		t.Fatal(e)
	}
	digest, _, e := captureDigest(source)
	if e != nil {
		t.Fatal(e)
	}
	locks, e := captureSetLocks(r, assignmentSkills(state, source.InitialSkillID))
	if e != nil {
		t.Fatal(e)
	}
	plan := &initialBindingsPlan{owner: p, request: r, attempt: original.attempt, source: source, digest: digest, locks: locks}
	return plan, &changedCaptureStore{captureBindingStore: base, state: state, installation: installed, installationWire: installationValues(t, installed), revision: fixed}, owner, ctx
}

func TestSkillCaptureChangedSetUsesEveryFixedObject(t *testing.T) {
	plan, s, o, ctx := changedCaptureFixture(t)
	if len(plan.locks) != 6 {
		t.Fatal("not every enabled Skill is locked")
	}
	result := captureFinal(t, s.captureBindingStore, o, ctx, plan, func(ctx context.Context, tx f.Tx) error { return saveSkillCapture(ctx, s, plan) })
	if result.State() != f.Committed || len(s.refs) != 2 {
		t.Fatal("controlled persistence", result.Fault())
	}
	for n, b := range plan.source.Result.Bindings {
		expected := plan.source.Assignments.Revisions[n].ObjectID.String()
		if s.refs[n][3] != b.SkillID.String() || s.refs[n][8] != expected {
			t.Fatal("binding used another Skill Object")
		}
	}
	if s.refs[0][8] == s.refs[1][8] {
		t.Fatal("fixture did not distinguish Objects")
	}
	s.refs[1][8] = s.refs[0][8]
	if ok, e := verifySkillCapture(ctx, s, plan); e == nil || ok {
		t.Fatal("cross-Skill Object damage accepted")
	}
}

func TestSkillCaptureChangedEmptySetRemainsHistorical(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		plan, s, o, ctx := changedCaptureFixture(t)
		state := plan.source.Assignments.State
		state.Items = []sc.AgentSkillAssignment{}
		if disabled {
			a := plan.source.Assignments.State.Items[0]
			a.ID = stateID[sc.Assignment](991)
			a.Sequence = 2
			a.Enabled = false
			state.Items = append(state.Items, a)
		}
		s.state = state
		plan.source.Assignments = &skillCaptureAssignments{State: state, Revisions: []assignmentRevision{}}
		plan.source.Result.Bindings = []sc.SkillBinding{}
		var err error
		plan.digest, _, err = captureDigest(plan.source)
		if err != nil {
			t.Fatal(err)
		}
		plan.locks, err = captureSetLocks(plan.request, assignmentSkills(state, plan.source.InitialSkillID))
		if err != nil || len(plan.locks) != 5 {
			t.Fatal("disabled Skill acquired a resource lock", err)
		}
		result := captureFinal(t, s.captureBindingStore, o, ctx, plan, func(ctx context.Context, tx f.Tx) error { return saveSkillCapture(ctx, s, plan) })
		if result.State() != f.Committed || s.writes != 1 || len(s.refs) != 0 {
			t.Fatal("empty changed set lost its head", result.Fault())
		}
		reader := &InitialRoundBindings{store: s}
		r := sc.InitialRoundBindingsRequest{Initial: plan.source.Result.Clone(), CaptureAttemptBinding: plan.attempt}
		selected, err := reader.DiscoverInitialRoundBindings(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		result = initialRoundTx(t, s.captureBindingStore, selected.RequiredLocks(), func(ctx context.Context, tx f.Tx) error {
			got, err := reader.ReadInitialRoundBindingsInTx(ctx, tx, r, selected)
			if err != nil || got.AssignmentSequence != 2 || got.Bindings == nil || len(got.Bindings) != 0 {
				t.Fatal("empty history changed", err)
			}
			return err
		})
		if result.State() != f.Committed || s.currentReads != 0 || s.writes != 1 {
			t.Fatal("empty historical read used current state or wrote")
		}
	}
}

func TestSkillCaptureHistoryIgnoresLaterAssignmentState(t *testing.T) {
	for _, name := range []string{"original", "changed-history", "missing-grant", "missing-revision", "missing-lock"} {
		t.Run(name, func(t *testing.T) {
			plan, s, o, ctx := changedCaptureFixture(t)
			result := captureFinal(t, s.captureBindingStore, o, ctx, plan, func(ctx context.Context, tx f.Tx) error { return saveSkillCapture(ctx, s, plan) })
			if result.State() != f.Committed {
				t.Fatal(result.Fault())
			}
			reader := &InitialRoundBindings{store: s}
			request := sc.InitialRoundBindingsRequest{Initial: plan.source.Result.Clone(), CaptureAttemptBinding: plan.attempt}
			discovered, e := reader.DiscoverInitialRoundBindings(ctx, request)
			if e != nil {
				t.Fatal(e)
			}
			switch name {
			case "changed-history":
				s.brokenHistory = true
			case "missing-grant":
				s.missingGrant = true
			case "missing-revision":
				s.missingRevision = true
			}
			locks := discovered.RequiredLocks()
			if name == "missing-lock" {
				locks = locks[:len(locks)-1]
			}
			result = initialRoundTx(t, s.captureBindingStore, locks, func(ctx context.Context, tx f.Tx) error {
				got, e := reader.ReadInitialRoundBindingsInTx(ctx, tx, request, discovered)
				if name == "original" {
					if e != nil || !slices.Equal(got.Bindings, request.Initial.Bindings) || got.AssignmentSequence != 2 {
						t.Fatal("lost original capture", e)
					}
				} else if e == nil || got.Bindings != nil {
					t.Fatal("damaged historical source returned bindings")
				}
				return e
			})
			if (result.State() == f.Committed) != (name == "original") || s.currentReads != 0 || s.writes != 3 {
				t.Fatal("history changed facts or read current state")
			}
		})
	}
}

func TestSkillCaptureFormatTwoRejectsPartialOrMixedRecord(t *testing.T) {
	for _, name := range []string{"sequence-one", "missing-revision", "foreign-revision", "wrong-binding", "disabled-binding", "extra-revision", "format-one"} {
		t.Run(name, func(t *testing.T) {
			plan, s, o, ctx := changedCaptureFixture(t)
			switch name {
			case "sequence-one":
				plan.source.Assignments.State.Sequence = 1
			case "missing-revision":
				plan.source.Assignments.Revisions = plan.source.Assignments.Revisions[:1]
			case "foreign-revision":
				plan.source.Assignments.Revisions[1].ProjectID = stateID[id.Project](999)
			case "wrong-binding":
				plan.source.Result.Bindings[1].AssignmentID = stateID[sc.Assignment](999)
			case "disabled-binding":
				plan.source.Assignments.State.Items[1].Enabled = false
			case "extra-revision":
				plan.source.Assignments.Revisions = append(plan.source.Assignments.Revisions, plan.source.Assignments.Revisions[0])
			case "format-one":
				result := captureFinal(t, s.captureBindingStore, o, ctx, plan, func(ctx context.Context, tx f.Tx) error { return saveSkillCapture(ctx, s, plan) })
				if result.State() != f.Committed {
					t.Fatal(result.Fault())
				}
				record := skillCaptureRecord{1, plan.attempt, plan.digest, plan.source}
				raw, e := json.Marshal(record)
				if e != nil {
					t.Fatal(e)
				}
				s.captured[6] = raw
				_, e = loadInitialRoundCapture(ctx, s, sc.InitialRoundBindingsRequest{Initial: plan.source.Result, CaptureAttemptBinding: plan.attempt})
				if e == nil {
					t.Fatal("format one accepted changed set")
				}
				return
			}
			if e := plan.source.validate(); e == nil {
				t.Fatal("partial/mixed set accepted")
			}
			if e := saveSkillCapture(ctx, s, plan); e == nil || s.writes != 0 {
				t.Fatal("invalid set persisted partial facts")
			}
		})
	}
}
