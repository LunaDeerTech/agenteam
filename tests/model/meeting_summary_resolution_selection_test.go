//go:build integration

package model_test

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"testing"
)

func TestModelMeetingSummaryResolutionSelection(t *testing.T) {
	v := newMeetingResolutionFixture(t, false)
	r := v.request(t, mc.MeetingSummaryInitial)
	selectRequest := func(r mc.ResolveRequest) mc.SelectionRequest {
		return mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection}
	}
	t.Run("missing-null-and-current-authority-before-state", func(t *testing.T) {
		p, e := v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.DependencyUnbound)
		if e = v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
			t.Fatal(e)
		}
		p, e = v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.InvalidState)
		changed := r.Clone()
		changed.Actor = v.admin
		p, e = v.resolving.DiscoverResolve(testContext(t), changed)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.NotFound)
	})
	_, m, _ := v.config(t, false, false)
	plain := m.Input.Clone()
	plain.Capabilities.StructuredOutputModes = nil
	receipt, e := v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, "plain-text"), ID: m.ID, ExpectedVersion: m.Version, Input: plain})
	if e != nil {
		t.Fatal(e)
	}
	m.Input = plain
	m.Version = receipt.Version
	chosen := v.choose(t, m.ID)
	t.Run("safe-select-is-not-generation-authority", func(t *testing.T) {
		q := selectRequest(r)
		q.Consumer.MeetingID = newID[struct{}](t).String()
		q.Consumer.OperationID = newID[struct{}](t).String()
		out, e := v.resolving.SelectModel(testContext(t), v.owner, q)
		if e != nil || out.Selected == nil || *out.Selected != m.ID || *out.SelectionVersion != chosen.Version {
			t.Fatal("nonadministrator Owner Select", e)
		}
		if v.count(t, "resolution_preparations") != 0 || v.count(t, "snapshots") != 0 || v.tap.calls.Load() != 0 {
			t.Fatal("Select created resolution state")
		}
		missing := r.Clone()
		missing.Consumer = q.Consumer
		p, e := v.resolving.DiscoverResolve(testContext(t), missing)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.Forbidden)
		out, e = v.resolving.SelectModel(testContext(t), v.admin, q)
		if e == nil || out.Selected != nil {
			t.Fatal("admin read another Project")
		}
		requireCode(t, e, f.NotFound)
	})
	t.Run("both-purposes-plain-text-independent-of-old-selector", func(t *testing.T) {
		var configured bool
		if e := v.raw.QueryRow(testContext(t), `SELECT configured FROM agenteam_model.platform_selection`).Scan(&configured); e != nil || configured {
			t.Fatal("old four configured prerequisite", e)
		}
		for _, purpose := range []mc.Purpose{mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate} {
			request := v.request(t, purpose)
			version := chosen.Version
			request.Selection.Version = &version
			out := v.resolve(t, request)
			if out.Snapshot.Identity.AdapterRevision != adapter.OpenAIChatTextRevision || *out.Snapshot.SelectionVersion != chosen.Version || len(out.Snapshot.Capabilities.StructuredOutputModes) != 0 || out.CredentialLease != nil {
				t.Fatal("plain Summary snapshot")
			}
		}
		bad := r.Clone()
		wrong := chosen.Version + 1
		bad.Selection.Version = &wrong
		p, e := v.resolving.DiscoverResolve(testContext(t), bad)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.ResourceBusy)
		_, e = v.resolving.SelectModel(testContext(t), v.owner, selectRequest(bad))
		requireCode(t, e, f.ResourceBusy)
	})
	t.Run("structured-capability-retained-and-memory-unmodified", func(t *testing.T) {
		_, structured, _ := v.config(t, false, false)
		if len(structured.Input.Capabilities.StructuredOutputModes) == 0 {
			t.Fatal("fixture lacks real schema capability")
		}
		current := v.choose(t, structured.ID)
		for _, purpose := range []mc.Purpose{mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate} {
			out := v.resolve(t, v.request(t, purpose))
			if out.Snapshot.Identity.AdapterRevision != adapter.OpenAIChatStructuredRevision || *out.Snapshot.SelectionVersion != current.Version {
				t.Fatal("structured Summary snapshot")
			}
		}
		oldVersion := v.selectMemory(t, structured)
		request := v.request(t, mc.MeetingSummaryUpdate)
		version := current.Version
		request.Selection.Version = &version
		selected, e := v.resolving.SelectModel(testContext(t), v.owner, selectRequest(request))
		if e != nil || *selected.SelectionVersion != version {
			t.Fatal("old selector version gated Summary", oldVersion, e)
		}
	})
	t.Run("disabled-and-unsupported-remain-explicit", func(t *testing.T) {
		provider, model, _ := v.config(t, false, false)
		v.choose(t, model.ID)
		for _, mode := range []string{"disabled-model", "disabled-provider", "unsupported"} {
			t.Run(mode, func(t *testing.T) {
				before := model.Input.Clone()
				pBefore := provider.Input.Clone()
				if mode == "disabled-provider" {
					provider.Input.Enabled = false
					receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
					if e != nil {
						t.Fatal(e)
					}
					provider.Version = receipt.Version
				} else {
					if mode == "disabled-model" {
						model.Input.Enabled = false
					} else {
						model.Input.Capabilities.ToolCalls = true
					}
					receipt, e := v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: model.ID, ExpectedVersion: model.Version, Input: model.Input})
					if e != nil {
						t.Fatal(e)
					}
					model.Version = receipt.Version
				}
				p, e := v.resolving.DiscoverResolve(testContext(t), v.request(t, mc.MeetingSummaryInitial))
				resolutionZeroPlan(t, p, e)
				want := f.InvalidState
				if mode == "unsupported" {
					want = f.CapabilityUnsupported
				}
				requireCode(t, e, want)
				if mode == "disabled-provider" {
					provider.Input = pBefore
					receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
					if e != nil {
						t.Fatal(e)
					}
					provider.Version = receipt.Version
				} else {
					model.Input = before
					receipt, e := v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: model.ID, ExpectedVersion: model.Version, Input: model.Input})
					if e != nil {
						t.Fatal(e)
					}
					model.Version = receipt.Version
				}
			})
		}
	})
	t.Run("legacy-summary-and-project-target-no-fallback", func(t *testing.T) {
		old := r.Clone()
		old.Selection.Kind = "project_summary"
		old.Selection.ProjectID = &old.Consumer.ProjectID
		p, e := v.resolving.DiscoverResolve(testContext(t), old)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.DependencyUnbound)
		_, projectModel, _ := v.config(t, true, false)
		state := v.summary(t)
		_, e = v.service.UpdateMeetingSummarySelection(testContext(t), mc.UpdateMeetingSummarySelectionRequest{CommandMeta: v.meta(t, "project-not-system"), SelectionID: state.ID, ExpectedVersion: state.Version, Model: projectModel.ID})
		if e == nil {
			t.Fatal("Project model selected as system")
		}
		// Controlled corrupt storage proves no Project fallback inside Resolution.
		v.sql(t, `UPDATE agenteam_model.meeting_summary_selection SET model_id=$1`, projectModel.ID.String())
		v.sql(t, `UPDATE agenteam_model.references SET model_id=$1 WHERE owner_id=$2`, projectModel.ID.String(), state.ID)
		defer func() {
			v.sql(t, `UPDATE agenteam_model.meeting_summary_selection SET model_id=$1`, state.Model.String())
			v.sql(t, `UPDATE agenteam_model.references SET model_id=$1 WHERE owner_id=$2`, state.Model.String(), state.ID)
		}()
		p, e = v.resolving.DiscoverResolve(testContext(t), v.request(t, mc.MeetingSummaryInitial))
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.NotFound)
	})
}
