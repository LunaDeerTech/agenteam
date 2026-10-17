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
	v := newMeetingSummaryFixture(t)
	s := v.summary(t)
	if s.Configured != nil || s.Version != 1 {
		t.Fatal("invented shared Summary default")
	}
	m := v.chat(t, true)
	before := v.summaryFacts(t)
	var wg sync.WaitGroup
	if e := v.service.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	httpSameFacts(t, before, v.summaryFacts(t))
	r := v.choice(t, m)
	receipt, e := v.service.UpdatePlatformSelection(testContext(t), r)
	if e != nil || receipt.Kind != "model.selection.update" || receipt.ResourceID != s.ID || receipt.Version != 2 || receipt.AffectedReferences != 0 {
		t.Fatal("first selection", e)
	}
	stable := v.summaryFacts(t)
	replay, e := v.service.UpdatePlatformSelection(testContext(t), r)
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
	next := v.chat(t, true)
	a, b := v.choice(t, m), v.choice(t, next)
	results := make(chan error, 2)
	for _, request := range []mc.UpdatePlatformSelectionRequest{a, b} {
		wg.Add(1)
		go func(r mc.UpdatePlatformSelectionRequest) {
			defer wg.Done()
			_, e := v.service.UpdatePlatformSelection(testContext(t), r)
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
	for _, mode := range []string{"missing", "nonchat", "plain-text-only", "disabled-model", "disabled-provider", "nonadmin", "logged-out"} {
		t.Run(mode, func(t *testing.T) {
			request := v.choice(t, m)
			want := f.NotFound
			switch mode {
			case "missing":
				request.Selection.Summary = newID[mc.Model](t)
			case "nonchat":
				raw := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
				request.Selection.Summary, _ = f.ParseID[mc.Model](raw)
				want = f.InvalidArgument
			case "plain-text-only":
				request.Selection.Summary = v.chat(t, false)
				want = f.CapabilityUnsupported
			case "disabled-model":
				request.Selection.Summary = v.chat(t, true)
				view, e := v.service.GetModel(testContext(t), v.admin, request.Selection.Summary)
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
				request.Selection.Summary = v.chat(t, true)
				view, e := v.service.GetModel(testContext(t), v.admin, request.Selection.Summary)
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
			out, e := v.service.UpdatePlatformSelection(testContext(t), request)
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
	replay, e = v.service.UpdatePlatformSelection(testContext(t), r)
	if e != nil || replay != receipt {
		t.Fatal("historical replay depended on current model availability", e)
	}
}

func isSummaryConflict(e error) bool {
	var fault *f.Fault
	return errors.As(e, &fault) && (fault.Code == f.VersionConflict || fault.Code == f.ResourceBusy)
}
