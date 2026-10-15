package contract

import (
	"bytes"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMeetingDeletionAuditMetadataClosedAndLossless(t *testing.T) {
	fields := MeetingDeletionMetadataFields{MeetingID: contentID, DeletionID: contentOtherID, InitiatorID: contentID, Version: 7, DeletedRows: 9007199254740993}
	m, err := MeetingDeletionMetadata(MeetingDeleteCompleted, fields)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMetadata(MeetingDeleteCompleted, m.JSON())
	if err != nil || !bytes.Equal(got.JSON(), m.JSON()) || !bytes.Contains(m.JSON(), []byte(`"deleted_rows":"9007199254740993"`)) {
		t.Fatal("lossy metadata", err)
	}
	for _, change := range [][2]string{
		{`"deleted_rows":"9007199254740993"`, `"deleted_rows":9007199254740993`},
		{`"deleted_rows":"9007199254740993"`, `"deleted_rows":"01"`},
		{`"deleted_rows":"9007199254740993"`, `"deleted_rows":"9223372036854775808"`},
		{`"version":"7"`, `"version":null`},
		{`"version":"7"`, `"version":"7","version":"7"`},
		{`"version":"7"`, `"version":"7","title":"private-canary"`},
	} {
		raw := bytes.Replace(m.JSON(), []byte(change[0]), []byte(change[1]), 1)
		if _, err := DecodeMetadata(MeetingDeleteCompleted, raw); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if _, err := DecodeMetadata(MeetingDeleteAccepted, m.JSON()); err == nil {
		t.Fatal("completed count became accepted")
	}
	fields.DeletedRows = 0
	if _, err := MeetingDeletionMetadata(MeetingDeleteCompleted, fields); err == nil {
		t.Fatal("zero-row completion")
	}
}

func TestMeetingDeletionAuditOriginalActorAndCause(t *testing.T) {
	project, _ := f.NewID[i.Project]()
	scope, _ := i.InProject(project)
	user, _ := f.ParseID[i.User](contentID)
	session, _ := f.NewID[i.Session]()
	human, _ := i.NewHuman(user, session)
	reg, _ := i.RegisterService(i.MeetingRuntime)
	runtime, _ := reg.Actor(contentOtherID, scope)
	resource, _ := NewResource(MeetingResource, contentID)
	fields := MeetingDeletionMetadataFields{MeetingID: contentID, DeletionID: contentOtherID, InitiatorID: contentID, Version: 7}
	for _, action := range []Action{MeetingDeleteAccepted, MeetingDeleteCompleted} {
		actor := human
		fields.DeletedRows = 0
		if action == MeetingDeleteCompleted {
			actor = runtime
			fields.DeletedRows = 3
		}
		metadata, err := MeetingDeletionMetadata(action, fields)
		if err != nil {
			t.Fatal(err)
		}
		entry := EntryFields{Actor: actor, Scope: scope, Action: action, Outcome: Success, Resource: resource, Metadata: metadata, Associations: Associations{OperationID: contentOtherID}}
		if _, err := NewEntry(entry); err != nil {
			t.Fatal(err)
		}
		if ProducerFor(action) != MeetingProducer || !MeetingProducer.Valid() {
			t.Fatal("producer lost")
		}
		for _, change := range []func(*EntryFields){
			func(v *EntryFields) { v.Outcome = Unknown },
			func(v *EntryFields) { v.Associations.OperationID = contentID },
			func(v *EntryFields) {
				v.Actor = human
				if action == MeetingDeleteAccepted {
					v.Actor = runtime
				}
			},
			func(v *EntryFields) { v.Resource, _ = NewResource(MeetingResource, contentOtherID) },
			func(v *EntryFields) { v.Scope = i.SystemScope() },
		} {
			bad := entry
			change(&bad)
			if _, err := NewEntry(bad); err == nil {
				t.Fatal("foreign audit identity accepted", action)
			}
		}
	}
}
