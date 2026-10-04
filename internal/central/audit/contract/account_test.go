package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAccountMetadataClosedPhaseAndSafeProjection(t *testing.T) {
	user := "01900000-0000-7000-8000-000000000001"
	attempt := "01900000-0000-7000-8000-000000000002"
	session := "01900000-0000-7000-8000-000000000003"
	registration, _ := identity.RegisterService(identity.AccountAuth)
	actor, _ := registration.Actor(attempt, identity.SystemScope())
	resource, _ := NewResource(AccountAttemptResource, attempt)
	f := AccountMetadataFields{UserID: user, SessionID: session, AttemptID: attempt, Version: 1, Phase: AccountAuthenticated}
	m, e := AccountMetadata(AccountLogin, f)
	if e != nil {
		t.Fatal(e)
	}
	entry, e := NewEntry(EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: AccountLogin, Outcome: Success, Resource: resource, Metadata: m})
	if e != nil || entry.Validate() != nil {
		t.Fatal(e)
	}
	if _, e := NewEntry(EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: AccountLogin, Outcome: Denied, Resource: resource, Metadata: m}); e == nil {
		t.Fatal("contradictory outcome")
	}
	if _, e := DecodeMetadata(AccountLogin, m.JSON()); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(string(m.JSON()), `"version":"1"`, `"version":null`, 1), strings.Replace(string(m.JSON()), `"phase":"authenticated"`, `"phase":"sent"`, 1), strings.Replace(string(m.JSON()), `"attempt_id":`, `"ATTEMPT_ID":`, 1), strings.Replace(string(m.JSON()), `"version":"1"`, `"version":"1","version":"2"`, 1), strings.TrimSuffix(string(m.JSON()), "}") + `,"email":"private@example.com"}`, string(m.JSON()) + `{}`} {
		if _, e := DecodeMetadata(AccountLogin, []byte(raw)); e == nil {
			t.Fatal("untyped or invalid metadata accepted")
		}
	}
	changed := []AccountChangedField{DisplayNameChanged, ThemeChanged}
	m, e = AccountMetadata(AccountProfileUpdate, AccountMetadataFields{UserID: user, Version: 2, Phase: AccountUpdated, ChangedFields: changed})
	if e != nil {
		t.Fatal(e)
	}
	changed[0] = "private text"
	copy, _ := m.AccountFields()
	copy.ChangedFields[0] = "leak"
	copy, _ = m.AccountFields()
	if copy.ChangedFields[0] == "leak" || strings.Contains(string(m.JSON()), "private text") {
		t.Fatal("mutable metadata")
	}
	if _, e := AccountMetadata(AccountProfileUpdate, AccountMetadataFields{UserID: user, Version: 2, Phase: AccountUpdated, ChangedFields: []AccountChangedField{"email"}}); e == nil {
		t.Fatal("unregistered field")
	}
	if json.Unmarshal([]byte(`{}`), &m) == nil {
		t.Fatal("manufactured metadata")
	}
	for _, extra := range []func(*AccountMetadataFields){
		func(f *AccountMetadataFields) { f.ObjectID = user }, func(f *AccountMetadataFields) { f.JobID = user },
		func(f *AccountMetadataFields) { f.ResetID = user }, func(f *AccountMetadataFields) { f.InvitationID = user },
		func(f *AccountMetadataFields) { f.InitiatorID = user },
	} {
		copy := f
		extra(&copy)
		if _, e := AccountMetadata(AccountLogin, copy); e == nil {
			t.Fatal("unrelated metadata fact accepted")
		}
	}
	_ = foundation.NotStarted
}
func TestAccountActionsResourcesAndProducers(t *testing.T) {
	for _, a := range []Action{AccountBootstrap, AccountLogin, AccountLogout, AccountInviteCreate, AccountInviteRevoke, AccountInviteRedeem, AccountPasswordChange, AccountPasswordResetRequest, AccountPasswordResetComplete, AccountProfileUpdate, AccountAvatarUpdate, AccountSettingsUpdate, SMTPSettingsUpdate, SMTPTestRequest, SMTPDelivery} {
		if !a.Valid() || !ProducerFor(a).Valid() {
			t.Fatal(a)
		}
	}
	for _, a := range []Action{"account.any", "smtp.password", "account.login.extra"} {
		if a.Valid() {
			t.Fatal(a)
		}
	}
	for _, r := range []ResourceKind{UserResource, SessionResource, AccountAttemptResource, InvitationResource, PasswordResetResource, AccountSettingsResource, SMTPSettingsResource, MailJobResource} {
		if _, e := NewResource(r, ""); e == nil {
			t.Fatal("singleton manufactured without stable ID")
		}
	}
}

func TestSMTPDeliverySettingFieldNamesAreClosed(t *testing.T) {
	for _, name := range []AccountChangedField{SMTPSenderNameChanged, SMTPAutoRetryCountChanged, SMTPRetryIntervalChanged} {
		fields := AccountMetadataFields{Version: 2, Phase: AccountUpdated, ChangedFields: []AccountChangedField{name}}
		metadata, err := AccountMetadata(SMTPSettingsUpdate, fields)
		if err != nil {
			t.Fatal(name, err)
		}
		if _, err := DecodeMetadata(SMTPSettingsUpdate, metadata.JSON()); err != nil {
			t.Fatal(err)
		}
		for _, action := range []Action{AccountSettingsUpdate, AccountProfileUpdate, AccountPasswordChange, SMTPTestRequest, SMTPDelivery} {
			fields.UserID = "01900000-0000-7000-8000-000000000001"
			if _, err := AccountMetadata(action, fields); err == nil {
				t.Fatal("field escaped SMTP update", name, action)
			}
		}
	}
	for _, name := range []AccountChangedField{"sender_name=private sender", "auto_retry_count=3", "retry_interval_seconds=60", "smtp_any"} {
		if _, err := AccountMetadata(SMTPSettingsUpdate, AccountMetadataFields{Version: 2, Phase: AccountUpdated, ChangedFields: []AccountChangedField{name}}); err == nil {
			t.Fatal("value or unknown field accepted")
		}
	}
	raw := []byte(`{"version":"2","phase":"updated","changed_fields":["sender_name"],"sender_name":"private sender"}`)
	if _, err := DecodeMetadata(SMTPSettingsUpdate, raw); err == nil {
		t.Fatal("value-bearing metadata accepted")
	}
}

func TestMailRetryMetadataOnlyRecordsOriginalJobAndCurrentInitiator(t *testing.T) {
	user, _ := foundation.ParseID[identity.User]("01900000-0000-7000-8000-000000000001")
	session, _ := foundation.ParseID[identity.Session]("01900000-0000-7000-8000-000000000002")
	job := "01900000-0000-7000-8000-000000000003"
	actor, _ := identity.NewHuman(user, session)
	fields := AccountMetadataFields{Version: 3, JobID: job, InitiatorID: user.String(), Phase: AccountAccepted}
	m, e := AccountMetadata(SMTPDeliveryRetry, fields)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeMetadata(SMTPDeliveryRetry, m.JSON())
	if e != nil || string(decoded.JSON()) != string(m.JSON()) {
		t.Fatal(e)
	}
	r, _ := NewResource(MailJobResource, job)
	f := EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: SMTPDeliveryRetry, Outcome: Success, Resource: r, Metadata: m}
	if _, e = NewEntry(f); e != nil || ProducerFor(f.Action) != AccountProducer {
		t.Fatal(e)
	}
	reg, _ := identity.RegisterService(identity.AccountMail)
	service, _ := reg.Actor(job, identity.SystemScope())
	bad := f
	bad.Actor = service
	if _, e = NewEntry(bad); e == nil {
		t.Fatal("service manufactured an administrator retry")
	}
	bad = f
	bad.Outcome = Unknown
	if _, e = NewEntry(bad); e == nil {
		t.Fatal("accepted retry is not unknown delivery")
	}
	for _, modify := range []func(*AccountMetadataFields){
		func(x *AccountMetadataFields) { x.JobID = "" }, func(x *AccountMetadataFields) { x.InitiatorID = "" },
		func(x *AccountMetadataFields) { x.AttemptID = job }, func(x *AccountMetadataFields) { x.UserID = user.String() }, func(x *AccountMetadataFields) { x.Channel = SMTPChannel },
		func(x *AccountMetadataFields) { x.Phase = AccountSent }, func(x *AccountMetadataFields) { x.Reason = DeliveryUnknown }, func(x *AccountMetadataFields) { x.ChangedFields = []AccountChangedField{SMTPSenderNameChanged} },
	} {
		copy := fields
		modify(&copy)
		if _, e = AccountMetadata(SMTPDeliveryRetry, copy); e == nil {
			t.Fatal("unrelated retry metadata accepted")
		}
	}
	if _, e = DecodeMetadata(SMTPDeliveryRetry, []byte(strings.TrimSuffix(string(m.JSON()), "}")+`,"recipient":"private@example.test"}`)); e == nil {
		t.Fatal("private value accepted")
	}
}
