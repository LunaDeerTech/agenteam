//go:build integration

package skill_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// The only transaction wrapper is an observation seam: it passes the same
// original ctx/Tx to the original callback, reads that Tx's actual Skill state
// after success, and arms the accepted full-frame proxy on its real backend
// PID. CommitResult is always the result of the actual Store, never a fixture
// replacement. No external Object success or production root is implied.
type admissionCommitStore struct {
	*postgres.Store
	proxy    *commitproxy.Proxy
	project  id.ProjectID
	target   string
	armed    bool
	writer   int32
	original f.CommitResult
}

func (s *admissionCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	selected := false
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := callback(ctx, tx); err != nil {
			return err
		}
		if s.armed {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var phase string
		var hasAttempt bool
		var work int
		if err = x.QueryRow(ctx, `SELECT phase,current_attempt_id IS NOT NULL,
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase='running')
 FROM agenteam_skill.initializations WHERE project_id=$1`, s.project.String()).Scan(&phase, &hasAttempt, &work); err != nil {
			return err
		}
		matches := s.target == "work" && phase == "planned" && !hasAttempt && work == 1 ||
			s.target == "reserve" && phase == "reserved" && hasAttempt && work == 1
		if !matches {
			return nil
		}
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&s.writer); err != nil {
			return err
		}
		if err = s.proxy.Arm(s.writer); err != nil {
			return err
		}
		s.armed, selected = true, true
		return nil
	})
	if selected {
		s.original = result
	}
	return result
}

func TestSkillInitializationAdmissionUnknown(t *testing.T) {
	for _, target := range []string{"work", "reserve"} {
		t.Run(target, func(t *testing.T) {
			direct := newSkillPG(t)
			p, proxy := withCommitProxy(t, direct)
			v := p.newCase(t)
			observing := &admissionCommitStore{Store: p.store, proxy: proxy, project: v.request.ProjectID, target: target}
			authority, err := skill.NewAuthority(observing, p.projects)
			if err != nil {
				t.Fatal(err)
			}
			v.objects.store, v.objects.authority = observing, authority
			service := p.newService(t, authority, v.objects)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			type result struct {
				value pc.InitializationResult
				err   error
			}
			returned, done := make(chan result, 1), make(chan struct{})
			t.Cleanup(func() {
				cancel()
				proxy.Release()
				<-done
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if err := service.Drain(cleanup); err != nil {
					t.Error("original admission call/work did not actually retire", err)
				}
			})
			go func() {
				defer close(done)
				value, err := service.InitializeProjectSkills(ctx, v.actor, v.request)
				returned <- result{value, err}
			}()
			waitSignal(t, proxy.Reached(), "original admission COMMIT frame not held")
			waitSignal(t, done, "admission Unknown call did not return")
			first := <-returned
			unknown, ok := skill.UnknownAttempt(first.err)
			if !ok || unknown.State() != f.Unknown || observing.original.State() != f.Unknown || unknown.AttemptID().Validate() != nil || unknown.AttemptID() != observing.original.AttemptID() ||
				!reflect.DeepEqual(unknown.Cause().Details(), observing.original.Cause().Details()) || first.value.State != "" || proxy.WriterPID() != observing.writer {
				t.Fatal("admission lost its actual physical Unknown or returned completion")
			}
			wantSteps := ""
			if target == "reserve" {
				wantSteps = "prepare,reserve,discard"
			}
			if strings.Join(v.objects.steps, ",") != wantSteps {
				t.Fatal("unknown admission crossed the physical boundary")
			}
			select {
			case <-proxy.Committed():
				t.Fatal("held admission commit was forwarded before release")
			default:
			}
			committedWork := 0
			if target == "reserve" {
				committedWork = 1
			}
			assertAdmissionFacts(t, direct, v.request, "planned", 0, committedWork, 0)
			proxy.Release()
			waitSignal(t, proxy.Committed(), "original admission commit not completed upstream")
			waitSignal(t, proxy.HeldJoined(), "admission proxy writer not actually joined")
			observed, err := service.InspectProjectSkills(testContext(t), v.actor, v.request)
			if err != nil || observed.State != pc.InitializationResultPending || !observed.Matches(v.request) {
				t.Fatal("read-only admission recovery manufactured publication", err)
			}
			if strings.Join(v.objects.steps, ",") != wantSteps {
				t.Fatal("read-only recovery retried physical work")
			}
			// Registration or reservation was committed, but no upload began.
			// Actual caller return and the original serialized Tx permit only
			// accounting retirement, not an automatic continuation of the write.
			drain, stop := context.WithTimeout(context.Background(), 3*time.Second)
			err = service.Drain(drain)
			stop()
			if err != nil {
				t.Fatal("admission accounting did not actually drain", err)
			}
			phase, attempts := "planned", 0
			if target == "reserve" {
				phase, attempts = "reserved", 1
			}
			assertAdmissionFacts(t, direct, v.request, phase, attempts, 0, 1)
			preserved, ok := skill.UnknownAttempt(first.err)
			if !ok || preserved.State() != f.Unknown || preserved.AttemptID() != unknown.AttemptID() ||
				!reflect.DeepEqual(preserved.Cause().Details(), unknown.Cause().Details()) {
				t.Fatal("later committed evidence rewrote the original Unknown")
			}
		})
	}
}

func assertAdmissionFacts(t *testing.T, p *skillPG, request pc.InitializationRequest, phase string, attempts, running, joined int) {
	t.Helper()
	var actualPhase string
	var actualAttempts, actualRunning, actualJoined, skills, revisions int
	err := p.store.QueryRow(testContext(t), `SELECT phase,
 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase='running'),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase='joined'),
 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)
 FROM agenteam_skill.initializations WHERE project_id=$1`, request.ProjectID.String()).Scan(&actualPhase, &actualAttempts, &actualRunning, &actualJoined, &skills, &revisions)
	if err != nil || actualPhase != phase || actualAttempts != attempts || actualRunning != running || actualJoined != joined || skills != 0 || revisions != 0 {
		t.Fatal("admission commit visibility/physical/publication invariant failed", err)
	}
}

var _ skill.Store = (*admissionCommitStore)(nil)
