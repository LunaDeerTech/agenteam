package contract

import (
	"bytes"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const contentID = "01900000-0000-7000-8000-000000000021"
const contentOtherID = "01900000-0000-7000-8000-000000000022"

func TestContentMetadataStrictTypedRoundTrip(t *testing.T) {
	f := ObjectMetadataFields{ObjectID: contentID, InitiatorKind: identity.Human, InitiatorID: contentOtherID, MediaType: "text/plain; charset=utf-8", ByteSize: 9007199254740993, Phase: PublishedPhase}
	m, err := ObjectMetadata(ObjectUploadComplete, f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(m.JSON(), []byte(`"byte_size":"9007199254740993"`)) {
		t.Fatal("counter lost string precision")
	}
	got, err := DecodeMetadata(ObjectUploadComplete, m.JSON())
	if err != nil || !bytes.Equal(got.JSON(), m.JSON()) {
		t.Fatal("typed object metadata failed DB round trip")
	}
	for _, replace := range [][2]string{
		{`"byte_size":"9007199254740993"`, `"byte_size":9007199254740993`},
		{`"byte_size":"9007199254740993"`, `"byte_size":"01"`},
		{`"sent_bytes":"0"`, `"sent_bytes":"-1"`},
		{`"sent_bytes":"0"`, `"sent_bytes":"0","sent_bytes":"0"`},
		{`"phase":"published"`, `"phase":null`},
		{`"phase":"published"`, `"phase":"private-canary"`},
		{`"phase":"published"`, `"phase":"published","key":"private-canary"`},
	} {
		b := bytes.Replace(m.JSON(), []byte(replace[0]), []byte(replace[1]), 1)
		if _, err := DecodeMetadata(ObjectUploadComplete, b); err == nil {
			t.Fatal("invalid content metadata accepted")
		}
	}
	for _, mutate := range []func(*ObjectMetadataFields){
		func(f *ObjectMetadataFields) { f.ObjectID = "private-canary" },
		func(f *ObjectMetadataFields) { f.TransferID = contentID },
		func(f *ObjectMetadataFields) { f.InitiatorKind = "admin" },
		func(f *ObjectMetadataFields) { f.InitiatorExecutionID = contentID },
		func(f *ObjectMetadataFields) { f.MediaType = "text/plain\r\nprivate-canary" },
		func(f *ObjectMetadataFields) { f.SentBytes = -1 },
		func(f *ObjectMetadataFields) { f.Reason = Cancelled },
	} {
		next := f
		mutate(&next)
		if _, err := ObjectMetadata(ObjectUploadComplete, next); err == nil {
			t.Fatal("invalid typed metadata accepted")
		}
	}
	a := ArtifactMetadataFields{ArtifactID: contentID, ObjectID: contentOtherID, SourceKind: KnowledgeSource, SourceID: contentOtherID, SourceRevision: foundation.Version(9223372036854775807), MediaType: "text/plain", ByteSize: 1, Phase: PublishedPhase}
	m, err = ArtifactMetadata(ArtifactCreate, a)
	if err != nil {
		t.Fatal(err)
	}
	got, err = DecodeMetadata(ArtifactCreate, m.JSON())
	if err != nil || !bytes.Equal(m.JSON(), got.JSON()) {
		t.Fatal("source revision round trip failed")
	}
	list, err := ArtifactMetadata(ArtifactList, ArtifactMetadataFields{Count: 3, Phase: ListedPhase})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMetadata(ArtifactList, append(list.JSON()[:len(list.JSON())-1], []byte(`,"name":"private-canary"}`)...)); err == nil {
		t.Fatal("list retained user search text")
	}
	if _, err := ArtifactMetadata(ArtifactDownload, ArtifactMetadataFields{ArtifactID: contentID, ObjectID: contentOtherID, MediaType: "text/plain", ByteSize: 1, SentBytes: 2, Phase: FailedPhase, Reason: IntegrityMismatch}); err == nil {
		t.Fatal("impossible sent count accepted")
	}
	if _, err := DecodeMetadata(SecretCreate, m.JSON()); err == nil {
		t.Fatal("content crossed a D04 metadata schema")
	}
}

func TestContentEntryResourceScopeAndActorClosed(t *testing.T) {
	project, _ := foundation.ParseID[identity.Project](contentID)
	scope, _ := identity.InProject(project)
	registration, _ := identity.RegisterService(identity.ObjectService)
	service, _ := registration.Actor(contentID, scope)
	resource, _ := NewResource(ObjectResource, contentOtherID)
	metadata, _ := ObjectMetadata(ObjectUploadComplete, ObjectMetadataFields{ObjectID: contentOtherID, InitiatorKind: identity.Human, InitiatorID: contentID, MediaType: "text/plain", Phase: PublishedPhase})
	f := EntryFields{Scope: scope, Actor: service, Action: ObjectUploadComplete, Outcome: Success, Resource: resource, Metadata: metadata}
	if _, err := NewEntry(f); err != nil {
		t.Fatal(err)
	}
	wrong, _ := NewResource(ObjectResource, contentID)
	f.Resource = wrong
	if _, err := NewEntry(f); err == nil {
		t.Fatal("object resource did not bind metadata")
	}
	u, _ := foundation.ParseID[identity.User](contentID)
	session, _ := foundation.ParseID[identity.Session](contentOtherID)
	human, _ := identity.NewHuman(u, session)
	f.Resource, f.Actor = resource, human
	if _, err := NewEntry(f); err == nil {
		t.Fatal("human forged a technical object event")
	}
	list, _ := ArtifactMetadata(ArtifactList, ArtifactMetadataFields{Phase: ListedPhase, Count: 0})
	collection, _ := NewResource(ArtifactCollectionResource, contentID)
	f = EntryFields{Scope: scope, Actor: human, Action: ArtifactList, Outcome: Success, Resource: collection, Metadata: list}
	if _, err := NewEntry(f); err != nil {
		t.Fatal(err)
	}
	f.Actor = service
	if _, err := NewEntry(f); err == nil {
		t.Fatal("maintenance impersonated an Artifact user")
	}
	f.Actor = human
	f.Resource, _ = NewResource(ArtifactCollectionResource, contentOtherID)
	if _, err := NewEntry(f); err == nil {
		t.Fatal("collection escaped project")
	}
	f.Scope = identity.SystemScope()
	if _, err := NewEntry(f); err == nil {
		t.Fatal("Artifact event entered system scope")
	}
	for _, action := range []Action{ObjectUploadComplete, ObjectUploadFailed, ObjectDelete, ObjectTransferIssue, ObjectTransferComplete, ObjectTransferRevoke} {
		if !action.Valid() || ProducerFor(action) != ObjectProducer {
			t.Fatal("object action registry drift")
		}
	}
	for _, action := range []Action{ArtifactCreate, ArtifactList, ArtifactRead, ArtifactDownload} {
		if !action.Valid() || ProducerFor(action) != ArtifactProducer {
			t.Fatal("Artifact action registry drift")
		}
	}
}

func TestContentActionPhaseOutcomeMatrix(t *testing.T) {
	project, _ := foundation.ParseID[identity.Project](contentID)
	scope, _ := identity.InProject(project)
	user, _ := foundation.ParseID[identity.User](contentID)
	session, _ := foundation.ParseID[identity.Session](contentOtherID)
	human, _ := identity.NewHuman(user, session)
	registration, _ := identity.RegisterService(identity.ObjectService)
	service, _ := registration.Actor(contentID, scope)
	cases := []struct {
		action Action
		phase  ContentPhase
	}{
		{ObjectUploadComplete, PublishedPhase}, {ObjectUploadFailed, FailedPhase}, {ObjectDelete, DeletedPhase},
		{ObjectTransferIssue, IssuedPhase}, {ObjectTransferComplete, SentPhase}, {ObjectTransferRevoke, RevokedPhase},
		{ArtifactCreate, PublishedPhase}, {ArtifactList, ListedPhase}, {ArtifactRead, ReadPhase},
		{ArtifactDownload, IssuedPhase}, {ArtifactDownload, StartedPhase}, {ArtifactDownload, SentPhase}, {ArtifactDownload, FailedPhase},
	}
	for _, tc := range cases {
		t.Run(string(tc.action)+"/"+string(tc.phase), func(t *testing.T) {
			actor := human
			kind, id := ArtifactResource, contentOtherID
			var metadata Metadata
			var err error
			if ProducerFor(tc.action) == ObjectProducer {
				actor, kind = service, ObjectResource
				f := ObjectMetadataFields{ObjectID: contentOtherID, InitiatorKind: identity.Human, InitiatorID: contentID, MediaType: "text/plain", ByteSize: 10, Phase: tc.phase}
				if tc.action == ObjectTransferIssue || tc.action == ObjectTransferComplete || tc.action == ObjectTransferRevoke {
					kind = ObjectTransferResource
					f.TransferID = contentOtherID
				}
				if tc.phase == SentPhase {
					f.SentBytes = 10
				}
				if tc.phase == FailedPhase {
					f.Reason = StorageUnavailable
				}
				metadata, err = ObjectMetadata(tc.action, f)
			} else {
				f := ArtifactMetadataFields{ArtifactID: contentOtherID, ObjectID: contentID, MediaType: "text/plain", ByteSize: 10, Phase: tc.phase}
				if tc.action == ArtifactCreate {
					f.SourceKind = InlineSource
				}
				if tc.action == ArtifactList {
					kind, id = ArtifactCollectionResource, contentID
					f = ArtifactMetadataFields{Phase: ListedPhase}
				}
				if tc.phase == SentPhase {
					f.SentBytes = 10
				}
				if tc.phase == FailedPhase {
					f.Reason = IntegrityMismatch
				}
				metadata, err = ArtifactMetadata(tc.action, f)
			}
			if err != nil {
				t.Fatal(err)
			}
			resource, _ := NewResource(kind, id)
			for _, outcome := range []Outcome{Success, Denied, Failed, Unknown} {
				valid := outcome == Success
				if tc.phase == FailedPhase {
					valid = outcome == Failed || tc.action == ObjectUploadFailed && outcome == Unknown
				}
				_, err := NewEntry(EntryFields{Scope: scope, Actor: actor, Action: tc.action, Outcome: outcome, Resource: resource, Metadata: metadata})
				if (err == nil) != valid {
					t.Fatalf("contradictory phase/outcome accepted or valid outcome rejected: %s", outcome)
				}
			}
		})
	}
}

func TestAuditMIMEHasOnlyBaseOrClosedCharset(t *testing.T) {
	for _, media := range []string{"text/plain; name=private-canary", "text/plain; filename=private-canary", "text/plain; token=private-canary", "text/plain; charset=private-canary"} {
		if _, err := ObjectMetadata(ObjectUploadComplete, ObjectMetadataFields{ObjectID: contentID, InitiatorKind: identity.Human, InitiatorID: contentOtherID, MediaType: media, Phase: PublishedPhase}); err == nil {
			t.Fatal("arbitrary MIME parameter entered object audit")
		}
		if _, err := ArtifactMetadata(ArtifactCreate, ArtifactMetadataFields{ArtifactID: contentID, ObjectID: contentOtherID, SourceKind: InlineSource, MediaType: media, Phase: PublishedPhase}); err == nil {
			t.Fatal("arbitrary MIME parameter entered Artifact audit")
		}
	}
	for _, media := range []string{"text/plain", "text/plain; charset=utf-8", "text/plain; charset=us-ascii"} {
		m, err := ObjectMetadata(ObjectUploadComplete, ObjectMetadataFields{ObjectID: contentID, InitiatorKind: identity.Human, InitiatorID: contentOtherID, MediaType: media, Phase: PublishedPhase})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeMetadata(ObjectUploadComplete, m.JSON()); err != nil {
			t.Fatal(err)
		}
	}
}
