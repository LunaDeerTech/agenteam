//go:build integration

package model_test

import (
	"context"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelMeetingSummaryReplacement(t *testing.T) {
	t.Run("summary-only-plain-text-cross-provider", func(t *testing.T) {
		v := newMeetingSummaryFixture(t, true)
		old, next := v.chat(t, false), v.chat(t, false)
		v.selectModel(t, old)
		request := mc.DeleteModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: old, ExpectedVersion: 1}
		before := v.summaryFacts(t)
		_, e := v.service.DeleteModel(testContext(t), request)
		requireCode(t, e, f.InvalidState)
		httpSameFacts(t, before, v.summaryFacts(t))
		request.Replacement = &next
		receipt, e := v.service.DeleteModel(testContext(t), request)
		if e != nil || receipt.AffectedReferences != 1 {
			t.Fatal("summary replacement", e)
		}
		selected := v.summary(t)
		if selected.Model == nil || *selected.Model != next || selected.Version != 3 {
			t.Fatal("canonical Summary did not move")
		}
		before = v.summaryFacts(t)
		replay, e := v.service.DeleteModel(testContext(t), request)
		if e != nil || replay != receipt {
			t.Fatal("delete replay", e)
		}
		httpSameFacts(t, before, v.summaryFacts(t))
	})
	t.Run("two-owners-atomic-and-current-mapping", func(t *testing.T) {
		v := newMeetingSummaryFixture(t, true)
		old, next, plain := v.chat(t, true), v.chat(t, true), v.chat(t, false)
		v.setMemory(t, old)
		v.selectModel(t, old)
		request := mc.DeleteModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: old, ExpectedVersion: 1, Replacement: &plain}
		before := v.summaryFacts(t)
		_, e := v.service.DeleteModel(testContext(t), request)
		requireCode(t, e, f.CapabilityUnsupported)
		httpSameFacts(t, before, v.summaryFacts(t))
		request.Replacement = &next
		for _, failure := range []string{"audit", "event"} {
			t.Run(failure, func(t *testing.T) {
				before := v.summaryFacts(t)
				if failure == "audit" {
					v.auditFail.fail.Store(true)
				} else {
					v.eventFail.fail.Store(true)
				}
				_, e := v.service.DeleteModel(testContext(t), request)
				v.auditFail.fail.Store(false)
				v.eventFail.fail.Store(false)
				requireCode(t, e, f.DependencyUnavailable)
				httpSameFacts(t, before, v.summaryFacts(t))
			})
		}
		summary := v.summary(t)
		for _, corrupt := range []string{"third-owner", "wrong-version"} {
			t.Run(corrupt, func(t *testing.T) {
				extra := newID[struct{}](t).String()
				if corrupt == "third-owner" {
					_, e = v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,'meeting_summary',$2,1)`, extra, old.String())
				} else {
					_, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.references SET owner_version=owner_version+1 WHERE owner_id=$1`, summary.ID)
				}
				if e != nil {
					t.Fatal(e)
				}
				before := v.summaryFacts(t)
				_, e = v.service.DeleteModel(testContext(t), request)
				if e == nil {
					t.Fatal("corrupt owner mapping accepted")
				}
				httpSameFacts(t, before, v.summaryFacts(t))
				if corrupt == "third-owner" {
					_, e = v.raw.Exec(testContext(t), `DELETE FROM agenteam_model.references WHERE owner_id=$1`, extra)
				} else {
					_, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.references SET owner_version=$2 WHERE owner_id=$1`, summary.ID, int64(summary.Version))
				}
				if e != nil {
					t.Fatal(e)
				}
			})
		}
		// This mutation occurs after dependency discovery and before AcquireAll;
		// the original prepared snapshot must not authorize a different model row.
		var fired atomic.Bool
		v.tracked.setHooks(func(ctx context.Context, cause f.TransactionCause) error {
			if cause.Details().Primary.Command() == "model.delete" && fired.CompareAndSwap(false, true) {
				_, e := v.raw.Exec(ctx, `UPDATE agenteam_model.models SET enabled=false,version=version+1 WHERE id=$1`, next.String())
				return e
			}
			return nil
		}, nil)
		_, e = v.service.DeleteModel(testContext(t), request)
		v.tracked.setHooks(nil, nil)
		requireCode(t, e, f.ResourceBusy)
		if !fired.Load() {
			t.Fatal("preparation race not reached")
		}
		if _, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.models SET enabled=true,version=version+1 WHERE id=$1`, next.String()); e != nil {
			t.Fatal(e)
		}
		legacy, e := v.service.GetPlatformSelection(testContext(t), v.admin)
		if e != nil {
			t.Fatal(e)
		}
		var beforeEvents int64
		if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_outbox.events WHERE producer='model'`).Scan(&beforeEvents); e != nil {
			t.Fatal(e)
		}
		receipt, e := v.service.DeleteModel(testContext(t), request)
		if e != nil || receipt.AffectedReferences != 2 {
			t.Fatal("two-owner delete", e)
		}
		s := v.summary(t)
		p, e := v.service.GetPlatformSelection(testContext(t), v.admin)
		if e != nil || s.Version != summary.Version+1 || s.Model == nil || *s.Model != next || p.Version != legacy.Version+1 || p.Configured == nil || p.Configured.Memory != next {
			t.Fatal("owners not changed once", e)
		}
		var refs, afterEvents int64
		if e = v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_model.references WHERE model_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='model')`, next.String()).Scan(&refs, &afterEvents); e != nil || refs != 2 || afterEvents-beforeEvents != 3 {
			t.Fatal("selector events/reference union", e, refs, afterEvents-beforeEvents)
		}
	})
}
