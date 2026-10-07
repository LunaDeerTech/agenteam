//go:build integration

package model_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelMeetingSummarySelection(t *testing.T) {
	v := newMeetingSummaryFixture(t, false)
	if out, e := v.service.GetMeetingSummarySelection(testContext(t), v.admin); e == nil || out.ID != "" {
		t.Fatal("uninitialized selection published", e)
	} else {
		requireCode(t, e, f.DependencyUnbound)
	}
	m := v.chat(t, false)
	before := v.summaryFacts(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- v.service.InitializeMeetingSummarySelection(testContext(t)) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	s := v.summary(t)
	if s.Model != nil || s.Version != 1 {
		t.Fatal("invented Summary default")
	}
	old, e := v.service.GetPlatformSelection(testContext(t), v.admin)
	if e != nil || old.ID == s.ID || old.Version != 1 || old.Configured != nil {
		t.Fatal("selectors share state", e)
	}
	after := v.summaryFacts(t)
	delete(before, "agenteam_model.meeting_summary_selection")
	httpSameFacts(t, before, after)
	if e = v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
		t.Fatal(e)
	}
	httpSameFacts(t, after, v.summaryFacts(t))
	r := v.choice(t, m)
	receipt, e := v.service.UpdateMeetingSummarySelection(testContext(t), r)
	if e != nil || receipt.Kind != "model.selection.update" || receipt.ResourceID != s.ID || receipt.Version != 2 || receipt.AffectedReferences != 0 {
		t.Fatal("first selection", e)
	}
	stable := v.summaryFacts(t)
	replay, e := v.service.UpdateMeetingSummarySelection(testContext(t), r)
	if e != nil || replay != receipt {
		t.Fatal("same request did not replay", e)
	}
	httpSameFacts(t, stable, v.summaryFacts(t))
	lookup, e := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: r.CommandMeta, Command: "model.selection.update"})
	if e != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != receipt {
		t.Fatal("original lookup", e)
	}
	// Saving the same model with a new key is an explicit new version.
	saved := v.selectModel(t, m)
	if saved.Version != 3 {
		t.Fatal("same value was mistaken for replay")
	}
	next := v.chat(t, false)
	a, b := v.choice(t, m), v.choice(t, next)
	results := make(chan error, 2)
	for _, request := range []mc.UpdateMeetingSummarySelectionRequest{a, b} {
		wg.Add(1)
		go func(r mc.UpdateMeetingSummarySelectionRequest) {
			defer wg.Done()
			_, e := v.service.UpdateMeetingSummarySelection(testContext(t), r)
			results <- e
		}(request)
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !isSummaryConflict(e) {
			t.Fatal("uncontrolled concurrent result", e)
		}
	}
	if successes != 1 {
		t.Fatal("expected version admitted two writers")
	}
	for _, mode := range []string{"missing", "nonchat", "disabled-model", "disabled-provider", "nonadmin", "logged-out"} {
		t.Run(mode, func(t *testing.T) {
			request := v.choice(t, m)
			want := f.NotFound
			switch mode {
			case "missing":
				request.Model = newID[mc.Model](t)
			case "nonchat":
				raw := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
				request.Model, _ = f.ParseID[mc.Model](raw)
				want = f.InvalidArgument
			case "disabled-model":
				request.Model = v.chat(t, false)
				view, e := v.service.GetModel(testContext(t), v.admin, request.Model)
				if e != nil {
					t.Fatal(e)
				}
				view.Input.Enabled = false
				_, e = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: view.ID, ExpectedVersion: view.Version, Input: view.Input})
				if e != nil {
					t.Fatal(e)
				}
				want = f.InvalidArgument
			case "disabled-provider":
				request.Model = v.chat(t, false)
				view, e := v.service.GetModel(testContext(t), v.admin, request.Model)
				if e != nil {
					t.Fatal(e)
				}
				provider, e := v.service.GetProvider(testContext(t), v.admin, view.ProviderID)
				if e != nil {
					t.Fatal(e)
				}
				provider.Input.Enabled = false
				_, e = v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
				if e != nil {
					t.Fatal(e)
				}
				want = f.InvalidState
			case "nonadmin":
				request.Actor = v.addBrowser(t, "user").actor
				want = f.Forbidden
			case "logged-out":
				browser := v.addBrowser(t, "admin")
				request.Actor = browser.actor
				if e := v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
					t.Fatal(e)
				}
				want = f.SessionRevoked
			}
			before := v.summaryFacts(t)
			out, e := v.service.UpdateMeetingSummarySelection(testContext(t), request)
			requireCode(t, e, want)
			if out != (mc.CommandReceipt{}) {
				t.Fatal("failed update leaked receipt")
			}
			httpSameFacts(t, before, v.summaryFacts(t))
		})
	}
	// Historical receipt survives a later disabled model and current configuration.
	view, e := v.service.GetModel(testContext(t), v.admin, m)
	if e != nil {
		t.Fatal(e)
	}
	view.Input.Enabled = false
	if _, e = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: m, ExpectedVersion: view.Version, Input: view.Input}); e != nil {
		t.Fatal(e)
	}
	replay, e = v.service.UpdateMeetingSummarySelection(testContext(t), r)
	if e != nil || replay != receipt {
		t.Fatal("historical replay depended on current model availability", e)
	}
}

func isSummaryConflict(e error) bool {
	var fault *f.Fault
	return errors.As(e, &fault) && (fault.Code == f.VersionConflict || fault.Code == f.ResourceBusy)
}
