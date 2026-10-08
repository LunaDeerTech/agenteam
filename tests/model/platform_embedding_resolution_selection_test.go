//go:build integration

package model_test

import (
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelPlatformEmbeddingResolutionSelection(t *testing.T) {
	v := newPlatformEmbeddingResolutionFixture(t)
	selectionRequest := func(r mc.ResolveRequest) mc.SelectionRequest {
		return mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection}
	}
	t.Run("missing-and-unconfigured-not-an-optional-success", func(t *testing.T) {
		for _, missing := range []bool{true, false} {
			if missing {
				v.sql(t, `DELETE FROM agenteam_model.platform_selection WHERE singleton`)
				t.Cleanup(func() {
					if e := v.service.Initialize(testContext(t)); e != nil {
						t.Error("restore missing singleton", e)
					}
				})
			}
			for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
				r := v.request(t, purpose)
				p, e := v.resolving.DiscoverResolve(testContext(t), r)
				resolutionZeroPlan(t, p, e)
				requireCode(t, e, f.InvalidState)
				_, e = v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
				requireCode(t, e, f.InvalidState)
				r.Actor = v.admin
				p, e = v.resolving.DiscoverResolve(testContext(t), r)
				resolutionZeroPlan(t, p, e)
				requireCode(t, e, f.NotFound)
				_, e = v.resolving.SelectModel(testContext(t), v.admin, selectionRequest(r))
				requireCode(t, e, f.NotFound)
			}
			if missing {
				if e := v.service.Initialize(testContext(t)); e != nil {
					t.Fatal(e)
				}
			}
		}
		if v.count(t, "resolution_preparations") != 0 || v.count(t, "snapshots") != 0 || v.tap.calls.Load() != 0 {
			t.Fatal("failed selection wrote resolution state")
		}
	})
	_, m := v.embeddingConfig(t, false)
	chosen := v.choose(t, m.ID)
	t.Run("safe-owner-select-does-not-authorize-or-accept-input", func(t *testing.T) {
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			r := v.request(t, purpose)
			r.Consumer.OperationID = newID[struct{}](t).String()
			out, e := v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
			if e != nil || out.Selected == nil || *out.Selected != m.ID || out.ModelVersion == nil || *out.ModelVersion != m.Version || out.SelectionVersion == nil || *out.SelectionVersion != chosen.Version {
				t.Fatal("safe current selection", e)
			}
			p, e := v.resolving.DiscoverResolve(testContext(t), r)
			resolutionZeroPlan(t, p, e)
			requireCode(t, e, f.Forbidden)
			_, e = v.resolving.SelectModel(testContext(t), v.admin, selectionRequest(r))
			requireCode(t, e, f.NotFound)
		}
		if v.count(t, "resolution_preparations") != 0 || v.count(t, "snapshots") != 0 || v.count(t, "snapshot_bindings") != 0 || v.tap.calls.Load() != 0 {
			t.Fatal("Select accepted a model call")
		}
	})
	t.Run("two-purposes-current-and-explicit-version-float-snapshot", func(t *testing.T) {
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			for _, explicit := range []bool{false, true} {
				r := v.request(t, purpose)
				if explicit {
					version := chosen.Version
					r.Selection.Version = &version
				}
				out := v.resolve(t, r)
				if out.Snapshot.Identity.ModelID != m.ID || out.Snapshot.Identity.Protocol != mc.OpenAIEmbeddings || out.Snapshot.Identity.Profile != mc.OpenAIEmbeddingsV1 || out.Snapshot.Identity.AdapterRevision != adapter.OpenAIEmbeddingsFloatRevision || out.Snapshot.SelectionVersion == nil || *out.Snapshot.SelectionVersion != chosen.Version || !reflect.DeepEqual(out.Snapshot.Capabilities, m.Input.Capabilities) || out.CredentialLease != nil {
					t.Fatal("wrong current embedding snapshot")
				}
			}
		}
	})
	t.Run("four-purpose-version-changes-even-if-embedding-id-does-not", func(t *testing.T) {
		_, chat, _ := v.config(t, false, false)
		state, e := v.service.GetPlatformSelection(testContext(t), v.admin)
		if e != nil {
			t.Fatal(e)
		}
		next := state.Configured.Clone()
		next.Memory = chat.ID
		receipt, e := v.service.UpdatePlatformSelection(testContext(t), mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ExpectedVersion: state.Version, Selection: next})
		if e != nil || receipt.Version != state.Version+1 {
			t.Fatal("four-purpose version", e)
		}
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			r := v.request(t, purpose)
			old := state.Version
			r.Selection.Version = &old
			p, e := v.resolving.DiscoverResolve(testContext(t), r)
			resolutionZeroPlan(t, p, e)
			requireCode(t, e, f.ResourceBusy)
			_, e = v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
			requireCode(t, e, f.ResourceBusy)
			r.Selection.Version = nil
			out := v.resolve(t, r)
			if out.Snapshot.Identity.ModelID != m.ID || *out.Snapshot.SelectionVersion != receipt.Version {
				t.Fatal("new unit ignored shared singleton version")
			}
		}
		if e = v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
			t.Fatal(e)
		}
		summary, e := v.service.GetMeetingSummarySelection(testContext(t), v.admin)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.service.UpdateMeetingSummarySelection(testContext(t), mc.UpdateMeetingSummarySelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), SelectionID: summary.ID, ExpectedVersion: summary.Version, Model: chat.ID}); e != nil {
			t.Fatal(e)
		}
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			r := v.request(t, purpose)
			version := receipt.Version
			r.Selection.Version = &version
			out, e := v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
			if e != nil || out.SelectionVersion == nil || *out.SelectionVersion != version {
				t.Fatal("independent Summary version gated embedding", e)
			}
		}
	})
	t.Run("disabled-and-valid-but-unsupported-have-no-fallback", func(t *testing.T) {
		provider, target := v.embeddingConfig(t, false)
		v.choose(t, target.ID)
		for _, mode := range []string{"model-disabled", "provider-disabled", "missing-text-capability", "max-output"} {
			t.Run(mode, func(t *testing.T) {
				oldModel, oldProvider := target.Input.Clone(), provider.Input.Clone()
				if mode == "provider-disabled" {
					provider.Input.Enabled = false
					receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
					if e != nil {
						t.Fatal(e)
					}
					provider.Version = receipt.Version
				} else {
					switch mode {
					case "model-disabled":
						target.Input.Enabled = false
					case "missing-text-capability":
						target.Input.Capabilities.InputModalities = nil
					case "max-output":
						n := mc.TokenCount(1)
						target.Input.Capabilities.MaxOutput = &n
					}
					receipt, e := v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: target.ID, ExpectedVersion: target.Version, Input: target.Input})
					if e != nil {
						t.Fatal(e)
					}
					target.Version = receipt.Version
				}
				defer func() {
					if mode == "provider-disabled" {
						provider.Input = oldProvider
						receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
						if e != nil {
							t.Error(e)
						} else {
							provider.Version = receipt.Version
						}
					} else {
						target.Input = oldModel
						receipt, e := v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: target.ID, ExpectedVersion: target.Version, Input: target.Input})
						if e != nil {
							t.Error(e)
						} else {
							target.Version = receipt.Version
						}
					}
				}()
				want := f.CapabilityUnsupported
				if mode == "model-disabled" || mode == "provider-disabled" {
					want = f.InvalidState
				}
				for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
					r := v.request(t, purpose)
					before := v.count(t, "resolution_preparations")
					p, e := v.resolving.DiscoverResolve(testContext(t), r)
					resolutionZeroPlan(t, p, e)
					requireCode(t, e, want)
					_, e = v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
					requireCode(t, e, want)
					r.Actor = v.admin
					p, e = v.resolving.DiscoverResolve(testContext(t), r)
					resolutionZeroPlan(t, p, e)
					requireCode(t, e, f.NotFound)
					if before != v.count(t, "resolution_preparations") {
						t.Fatal("unsupported profile produced preparation")
					}
				}
			})
		}
	})
	t.Run("project-target-is-never-a-platform-fallback", func(t *testing.T) {
		_, projectModel, _ := v.config(t, true, false)
		state, e := v.service.GetPlatformSelection(testContext(t), v.admin)
		if e != nil {
			t.Fatal(e)
		}
		bad := state.Configured.Clone()
		bad.Embedding = projectModel.ID
		if _, e = v.service.UpdatePlatformSelection(testContext(t), mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ExpectedVersion: state.Version, Selection: bad}); e == nil {
			t.Fatal("Project model admitted to platform selector")
		}
		// Controlled negative storage only; restore the original selector bytes.
		v.sql(t, `UPDATE agenteam_model.platform_selection SET embedding_id=$1 WHERE singleton`, projectModel.ID.String())
		defer v.sql(t, `UPDATE agenteam_model.platform_selection SET embedding_id=$1 WHERE singleton`, state.Configured.Embedding.String())
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			r := v.request(t, purpose)
			p, e := v.resolving.DiscoverResolve(testContext(t), r)
			resolutionZeroPlan(t, p, e)
			requireCode(t, e, f.NotFound)
			_, e = v.resolving.SelectModel(testContext(t), v.owner, selectionRequest(r))
			requireCode(t, e, f.NotFound)
		}
	})
	v.noSecretRead(t)
}
