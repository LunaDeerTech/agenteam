package project

import (
	"context"
	"reflect"
	"testing"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func artifactAuditGateEntry(t *testing.T, v *auditGateFixture, action au.Action) (au.Entry, au.AppendKey) {
	t.Helper()
	scope, _ := identity.InProject(v.project)
	id := testID[struct{}](t).String()
	resource, _ := au.NewResource(au.ArtifactResource, id)
	fields := au.ArtifactMetadataFields{ArtifactID: id, ObjectID: testID[struct{}](t).String(), MediaType: "text/plain", ByteSize: 3, Phase: au.ReadPhase, SentBytes: 3}
	if action == au.ArtifactCreate {
		fields.Phase, fields.SourceKind, fields.SentBytes = au.PublishedPhase, au.InlineSource, 0
	}
	if action == au.ArtifactDownload {
		fields.Phase, fields.SentBytes = au.IssuedPhase, 0
	}
	if action == au.ArtifactList {
		resource, _ = au.NewResource(au.ArtifactCollectionResource, v.project.String())
		fields = au.ArtifactMetadataFields{Phase: au.ListedPhase, Count: 0}
	}
	metadata, err := au.ArtifactMetadata(action, fields)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: v.actor, Action: action, Outcome: au.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := au.NewAppendKey(au.ArtifactProducer, testID[struct{}](t).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestArtifactAuditCurrentOwnerAndExactFacts(t *testing.T) {
	for _, mode := range []string{"create", "read", "archived-read", "archived-create", "deleting-read", "revoked-session", "missing-provider", "issued", "archived-issued", "deleting-issued", "revoked-issued"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			action := au.ArtifactRead
			if mode == "create" || mode == "archived-create" {
				action = au.ArtifactCreate
			}
			if mode == "issued" || mode == "archived-issued" || mode == "deleting-issued" || mode == "revoked-issued" {
				action = au.ArtifactDownload
			}
			entry, key := artifactAuditGateEntry(t, v, action)
			type marker struct{}
			ctx := context.WithValue(context.Background(), marker{}, "original-artifact-witness")
			calls := 0
			v.a.state().auditFacts[au.ArtifactProducer] = auditFactFunc(func(got context.Context, tx f.Tx, e au.Entry, k au.AppendKey) error {
				calls++
				if got != ctx || tx != v.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || !reflect.DeepEqual(e.Fields().Metadata.JSON(), entry.Fields().Metadata.JSON()) || k.Details() != key.Details() || len(v.order) < 3 || !reflect.DeepEqual(v.order[:3], []string{"locks", "session", "executor"}) {
					t.Fatal("original Session/Owner/Tx/witness/entry order changed", v.order)
				}
				return nil
			})
			want := f.Code("")
			switch mode {
			case "archived-read", "archived-issued":
				v.lifecycle = c.Archived
			case "archived-create":
				v.lifecycle, want = c.Archived, f.ProjectNotActive
			case "deleting-read", "deleting-issued":
				v.lifecycle, want = c.Deleting, f.ProjectNotActive
			case "revoked-session", "revoked-issued":
				v.sessionErr, want = fault(f.SessionRevoked), f.SessionRevoked
			case "missing-provider":
				delete(v.a.state().auditFacts, au.ArtifactProducer)
				want = f.DependencyUnbound
			}
			err := v.a.CheckAppendInTx(ctx, v.store.tx, entry, key)
			if want == "" {
				if err != nil || calls != 1 {
					t.Fatal("real Artifact fact dispatch rejected", err, calls)
				}
			} else {
				hasCode(t, err, want)
				if calls != 0 {
					t.Fatal("denial reached Artifact checker")
				}
			}
		})
	}
}

func TestArtifactAuditClosedDispatch(t *testing.T) {
	for _, mode := range []string{"list", "ordinal", "wrong-producer", "provider-error", "download-started", "download-sent", "download-failed"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			action := au.ArtifactRead
			if mode == "list" {
				action = au.ArtifactList
			}
			if mode == "download-started" || mode == "download-sent" || mode == "download-failed" {
				action = au.ArtifactDownload
			}
			entry, key := artifactAuditGateEntry(t, v, action)
			if action == au.ArtifactDownload {
				fields := entry.Fields()
				meta := au.ArtifactMetadataFields{ArtifactID: fields.Resource.Details().ID, ObjectID: testID[struct{}](t).String(), MediaType: "text/plain", ByteSize: 3, Phase: au.StartedPhase}
				if mode == "download-sent" {
					meta.Phase, meta.SentBytes = au.SentPhase, 3
				}
				if mode == "download-failed" {
					meta.Phase, meta.Reason, fields.Outcome = au.FailedPhase, au.Cancelled, au.Failed
				}
				var err error
				fields.Metadata, err = au.ArtifactMetadata(au.ArtifactDownload, meta)
				if err != nil {
					t.Fatal(err)
				}
				entry, err = au.NewEntry(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "ordinal" {
				key, _ = au.NewAppendKey(au.ArtifactProducer, key.Details().CauseRef, 1)
			}
			if mode == "wrong-producer" {
				key, _ = au.NewAppendKey(au.SecretProducer, key.Details().CauseRef, 0)
			}
			calls := 0
			v.a.state().auditFacts[au.ArtifactProducer] = auditFactFunc(func(context.Context, f.Tx, au.Entry, au.AppendKey) error { calls++; return fault(f.Forbidden) })
			hasCode(t, v.a.CheckAppendInTx(context.Background(), v.store.tx, entry, key), f.Forbidden)
			want := 0
			if mode == "provider-error" {
				want = 1
			}
			if calls != want {
				t.Fatal("closed dispatch or provider rejection bypassed", calls)
			}
		})
	}
}
