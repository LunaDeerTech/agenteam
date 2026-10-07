//go:build integration

package model_test

import (
	"bytes"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"testing"
)

func TestModelMeetingSummaryResolutionReplay(t *testing.T) {
	v := newMeetingResolutionFixture(t, true)
	_, first, _ := v.config(t, false, true)
	provider, second, _ := v.config(t, false, true)
	v.choose(t, first.ID)
	r := v.request(t, mc.MeetingSummaryInitial)
	old := v.discover(t, r)
	v.choose(t, second.ID)
	prepared := v.discover(t, r)
	if old.Details().SnapshotID != prepared.Details().SnapshotID || prepared.Details().ModelID != second.ID {
		t.Fatal("prepared replan changed unit or missed current choice")
	}
	var version int64
	if e := v.raw.QueryRow(testContext(t), `SELECT plan_version FROM agenteam_model.resolution_preparations WHERE snapshot_id=$1`, prepared.Details().SnapshotID.String()).Scan(&version); e != nil || version != 2 {
		t.Fatal("prepared plan version", version, e)
	}
	_, result := v.final(t, r, old, true, false)
	if result.State() != f.NotCommitted {
		t.Fatal("stale prepared plan accepted")
	}
	v.atomicFacts(t, r, prepared, 0)
	accepted, result := v.final(t, r, prepared, true, false)
	if result.State() != f.Committed {
		t.Fatal(result.Fault())
	}
	v.atomicFacts(t, r, prepared, 1)
	frozen := resolutionJSON(accepted.Snapshot)
	lease := accepted.CredentialLease.LeaseID
	same := func(t *testing.T, out mc.ResolvedModel) {
		t.Helper()
		if !bytes.Equal(frozen, resolutionJSON(out.Snapshot)) || out.CredentialLease == nil || out.CredentialLease.LeaseID != lease {
			t.Fatal("committed snapshot or lease drifted")
		}
	}
	t.Run("current-switch-disable-endpoint-delete-replacement-do-not-rebind", func(t *testing.T) {
		v.choose(t, first.ID)
		same(t, v.resolve(t, r))
		provider.Input.Enabled = false
		provider.Input.BaseURL = "https://new-endpoint.example/v2"
		receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, "provider-after-accept"), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
		if e != nil {
			t.Fatal(e)
		}
		provider.Version = receipt.Version
		same(t, v.resolve(t, r))
		second.Input.Enabled = false
		receipt, e = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, "model-after-accept"), ID: second.ID, ExpectedVersion: second.Version, Input: second.Input})
		if e != nil {
			t.Fatal(e)
		}
		second.Version = receipt.Version
		same(t, v.resolve(t, r))
		_, e = v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.meta(t, "delete-after-accept"), ID: second.ID, ExpectedVersion: second.Version, Replacement: &first.ID})
		if e != nil {
			t.Fatal(e)
		}
		_, e = v.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.meta(t, "delete-provider-after-accept"), ID: provider.ID, ExpectedVersion: provider.Version})
		if e != nil {
			t.Fatal(e)
		}
		same(t, v.resolve(t, r))
		next := v.resolve(t, v.request(t, mc.MeetingSummaryUpdate))
		if next.Snapshot.Identity.ModelID != first.ID || next.Snapshot.ID == accepted.Snapshot.ID || next.CredentialLease.LeaseID == lease {
			t.Fatal("new Call did not observe current selection")
		}
	})
	t.Run("new-authority-and-formal-session-preserve-stable-unit", func(t *testing.T) {
		restarted := bindMeetingResolution(t, v.projectConfigurationFixture, v.identity)
		r.Actor = v.identity.login(t, v.identity.ownerBrowser.email).actor
		_, result := restarted.final(t, r, prepared, false, false)
		if result.State() != f.NotCommitted {
			t.Fatal("old authority/session in-memory plan reused")
		}
		same(t, restarted.resolve(t, r))
	})
	t.Run("same-call-valid-canonical-but-different-semantics-conflict", func(t *testing.T) {
		for _, mode := range []string{"purpose", "meeting", "operation", "explicit-version"} {
			t.Run(mode, func(t *testing.T) {
				changed := r.Clone()
				switch mode {
				case "purpose":
					changed.Purpose = mc.MeetingSummaryUpdate
					changed.Consumer.Purpose = changed.Purpose
				case "meeting":
					changed.Consumer.MeetingID = newID[struct{}](t).String()
				case "operation":
					changed.Consumer.OperationID = newID[struct{}](t).String()
				case "explicit-version":
					version := *accepted.Snapshot.SelectionVersion
					changed.Selection.Version = &version
				}
				v.bindFacts(t, changed)
				p, e := v.resolving.DiscoverResolve(testContext(t), changed)
				resolutionZeroPlan(t, p, e)
				requireCode(t, e, f.IdempotencyKeyReused)
				v.bindFacts(t, r)
			})
		}
	})
	t.Run("released-canonical-never-resurrected", func(t *testing.T) {
		v.sql(t, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, lease.String())
		out, e := v.resolving.ResolveModel(testContext(t), r)
		resolutionNoResult(t, out, e)
		requireCode(t, e, f.InvalidState)
		var released bool
		if e = v.raw.QueryRow(testContext(t), `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, lease.String()).Scan(&released); e != nil || !released {
			t.Fatal("released lease revived", e)
		}
		t.Log("controlled released fact only; no production material read or lease retirement")
	})
}
