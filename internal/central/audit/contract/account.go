package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	AccountBootstrap             Action       = "account.bootstrap"
	AccountLogin                 Action       = "account.login"
	AccountLogout                Action       = "account.logout"
	AccountInviteCreate          Action       = "account.invite.create"
	AccountInviteRevoke          Action       = "account.invite.revoke"
	AccountInviteRedeem          Action       = "account.invite.redeem"
	AccountPasswordChange        Action       = "account.password.change"
	AccountPasswordResetRequest  Action       = "account.password.reset.request"
	AccountPasswordResetComplete Action       = "account.password.reset.complete"
	AccountProfileUpdate         Action       = "account.profile.update"
	AccountAvatarUpdate          Action       = "account.avatar.update"
	AccountSettingsUpdate        Action       = "account.settings.update"
	SMTPSettingsUpdate           Action       = "smtp.settings.update"
	SMTPTestRequest              Action       = "smtp.test.request"
	SMTPDelivery                 Action       = "smtp.delivery"
	SMTPDeliveryRetry            Action       = "smtp.delivery.retry"
	AccountProducer              Producer     = "account"
	AccountMailProducer          Producer     = "account.mail"
	UserResource                 ResourceKind = "user"
	SessionResource              ResourceKind = "session"
	AccountAttemptResource       ResourceKind = "account_attempt"
	InvitationResource           ResourceKind = "invitation"
	PasswordResetResource        ResourceKind = "password_reset"
	AccountSettingsResource      ResourceKind = "account_settings"
	SMTPSettingsResource         ResourceKind = "smtp_settings"
	MailJobResource              ResourceKind = "mail_job"
)

func AccountAction(a Action) bool {
	switch a {
	case AccountBootstrap, AccountLogin, AccountLogout, AccountInviteCreate, AccountInviteRevoke, AccountInviteRedeem, AccountPasswordChange, AccountPasswordResetRequest, AccountPasswordResetComplete, AccountProfileUpdate, AccountAvatarUpdate, AccountSettingsUpdate, SMTPSettingsUpdate, SMTPTestRequest, SMTPDelivery, SMTPDeliveryRetry:
		return true
	}
	return false
}

type AccountAuthority interface {
	CheckAppendInTx(context.Context, foundation.Tx, Entry, AppendKey) error
	CheckServiceLookup(context.Context, identity.Actor, AppendKey) error
}
type AccountChangedField string

const (
	UsernameChanged           AccountChangedField = "username"
	DisplayNameChanged        AccountChangedField = "display_name"
	ThemeChanged              AccountChangedField = "theme"
	AvatarChanged             AccountChangedField = "avatar"
	PasswordChanged           AccountChangedField = "password"
	SessionIdleChanged        AccountChangedField = "session_idle_seconds"
	SessionAbsoluteChanged    AccountChangedField = "session_absolute_seconds"
	PasswordResetTTLChanged   AccountChangedField = "password_reset_seconds"
	ChallengeThresholdChanged AccountChangedField = "challenge_after_failures"
	SMTPHostChanged           AccountChangedField = "host"
	SMTPPortChanged           AccountChangedField = "port"
	SMTPTLSChanged            AccountChangedField = "tls_mode"
	SMTPUsernameChanged       AccountChangedField = "auth_username"
	SMTPPasswordChanged       AccountChangedField = "password"
	SMTPFromChanged           AccountChangedField = "from"
	SMTPEnabledChanged        AccountChangedField = "enabled"
	SMTPSenderNameChanged     AccountChangedField = "sender_name"
	SMTPAutoRetryCountChanged AccountChangedField = "auto_retry_count"
	SMTPRetryIntervalChanged  AccountChangedField = "retry_interval_seconds"
)

type AccountPhase string

const (
	AccountCreated       AccountPhase = "created"
	AccountAuthenticated AccountPhase = "authenticated"
	AccountRejected      AccountPhase = "rejected"
	AccountRevoked       AccountPhase = "revoked"
	AccountRedeemed      AccountPhase = "redeemed"
	AccountUpdated       AccountPhase = "updated"
	AccountAccepted      AccountPhase = "accepted"
	AccountSent          AccountPhase = "sent"
	AccountFailed        AccountPhase = "failed"
	AccountUnknown       AccountPhase = "unknown"
)

type AccountReason string

const (
	CredentialsRejected AccountReason = "credentials_rejected"
	ChallengeNeeded     AccountReason = "challenge_required"
	ChallengeRejected   AccountReason = "challenge_invalid"
	SessionInvalid      AccountReason = "session_invalid"
	DeliveryRejected    AccountReason = "delivery_rejected"
	DeliveryTimeout     AccountReason = "timeout"
	DeliveryCancelled   AccountReason = "cancelled"
	DeliveryUnknown     AccountReason = "delivery_unknown"
)

type DeliveryChannel string

const (
	SMTPChannel DeliveryChannel = "smtp"
	LogChannel  DeliveryChannel = "log"
)

// Fields contain only schema-checked identifiers and enum values. No transport
// address or user-supplied display text is an audit metadata field.
type AccountMetadataFields struct {
	UserID        string                `json:"user_id,omitempty"`
	SessionID     string                `json:"session_id,omitempty"`
	AttemptID     string                `json:"attempt_id,omitempty"`
	InvitationID  string                `json:"invitation_id,omitempty"`
	ResetID       string                `json:"reset_id,omitempty"`
	JobID         string                `json:"job_id,omitempty"`
	ObjectID      string                `json:"object_id,omitempty"`
	Version       foundation.Version    `json:"version"`
	ChangedFields []AccountChangedField `json:"changed_fields,omitempty"`
	Channel       DeliveryChannel       `json:"channel,omitempty"`
	Phase         AccountPhase          `json:"phase"`
	Reason        AccountReason         `json:"reason,omitempty"`
	InitiatorID   string                `json:"initiator_id,omitempty"`
}

func AccountMetadata(action Action, f AccountMetadataFields) (Metadata, error) {
	if !AccountAction(action) || f.Version.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	for _, id := range []string{f.UserID, f.SessionID, f.AttemptID, f.InvitationID, f.ResetID, f.JobID, f.ObjectID, f.InitiatorID} {
		if id != "" && !validID(id) {
			return Metadata{}, invalid("metadata")
		}
	}
	if f.Channel != "" && f.Channel != SMTPChannel && f.Channel != LogChannel {
		return Metadata{}, invalid("metadata")
	}
	// Each action has an explicit field set. An otherwise valid UUID cannot
	// smuggle an unrelated actor/resource fact through a typed metadata value.
	allowedIDs := map[string]bool{}
	allow := func(ids ...string) {
		for _, id := range ids {
			allowedIDs[id] = true
		}
	}
	switch action {
	case AccountBootstrap:
		allow("user")
	case AccountLogin:
		allow("user", "attempt")
		if f.Phase == AccountAuthenticated {
			allow("session")
		}
	case AccountLogout:
		allow("user", "session")
	case AccountInviteCreate, AccountInviteRevoke:
		allow("invitation", "initiator")
	case AccountInviteRedeem:
		allow("user", "invitation")
	case AccountPasswordChange, AccountProfileUpdate:
		allow("user")
	case AccountPasswordResetRequest:
		allow("attempt", "user")
	case AccountPasswordResetComplete:
		allow("user", "reset")
	case AccountAvatarUpdate:
		allow("user", "object")
	case AccountSettingsUpdate, SMTPSettingsUpdate:
		allow("initiator")
	case SMTPTestRequest, SMTPDeliveryRetry:
		allow("job", "initiator")
	case SMTPDelivery:
		allow("job", "attempt", "initiator")
	}
	for name, value := range map[string]string{"user": f.UserID, "session": f.SessionID, "attempt": f.AttemptID, "invitation": f.InvitationID, "reset": f.ResetID, "job": f.JobID, "object": f.ObjectID, "initiator": f.InitiatorID} {
		if value != "" && !allowedIDs[name] {
			return Metadata{}, invalid("metadata")
		}
	}
	allowedFields := map[AccountChangedField]bool{}
	phase := AccountPhase("")
	switch action {
	case AccountBootstrap:
		phase = AccountCreated
		if f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountLogin:
		if f.AttemptID == "" || f.Phase != AccountAuthenticated && f.Phase != AccountRejected {
			return Metadata{}, invalid("metadata")
		}
		phase = f.Phase
		if phase == AccountAuthenticated && (f.UserID == "" || f.SessionID == "") {
			return Metadata{}, invalid("metadata")
		}
	case AccountLogout:
		phase = AccountRevoked
		if f.UserID == "" || f.SessionID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountInviteCreate:
		phase = AccountCreated
		if f.InvitationID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountInviteRevoke:
		phase = AccountRevoked
		if f.InvitationID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountInviteRedeem:
		phase = AccountRedeemed
		if f.InvitationID == "" || f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountPasswordChange:
		phase = AccountUpdated
		if f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
		allowedFields[PasswordChanged] = true
	case AccountPasswordResetRequest:
		phase = AccountAccepted
		if f.AttemptID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountPasswordResetComplete:
		phase = AccountRedeemed
		if f.ResetID == "" || f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
	case AccountProfileUpdate:
		phase = AccountUpdated
		if f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
		for _, v := range []AccountChangedField{UsernameChanged, DisplayNameChanged, ThemeChanged} {
			allowedFields[v] = true
		}
	case AccountAvatarUpdate:
		phase = AccountUpdated
		if f.UserID == "" {
			return Metadata{}, invalid("metadata")
		}
		allowedFields[AvatarChanged] = true
	case AccountSettingsUpdate:
		phase = AccountUpdated
		for _, v := range []AccountChangedField{SessionIdleChanged, SessionAbsoluteChanged, PasswordResetTTLChanged, ChallengeThresholdChanged} {
			allowedFields[v] = true
		}
	case SMTPSettingsUpdate:
		phase = AccountUpdated
		for _, v := range []AccountChangedField{SMTPHostChanged, SMTPPortChanged, SMTPTLSChanged, SMTPUsernameChanged, SMTPPasswordChanged, SMTPFromChanged, SMTPEnabledChanged, SMTPSenderNameChanged, SMTPAutoRetryCountChanged, SMTPRetryIntervalChanged} {
			allowedFields[v] = true
		}
	case SMTPTestRequest:
		phase = AccountAccepted
		if f.JobID == "" {
			return Metadata{}, invalid("metadata")
		}
	case SMTPDeliveryRetry:
		phase = AccountAccepted
		if f.JobID == "" || f.InitiatorID == "" {
			return Metadata{}, invalid("metadata")
		}
	case SMTPDelivery:
		if f.JobID == "" || f.AttemptID == "" || f.Channel == "" || f.Phase != AccountSent && f.Phase != AccountFailed && f.Phase != AccountUnknown {
			return Metadata{}, invalid("metadata")
		}
		phase = f.Phase
	}
	if f.Phase != phase {
		return Metadata{}, invalid("metadata")
	}
	if f.Phase == AccountRejected {
		if f.Reason != CredentialsRejected && f.Reason != ChallengeNeeded && f.Reason != ChallengeRejected && f.Reason != SessionInvalid {
			return Metadata{}, invalid("metadata")
		}
	} else if f.Phase == AccountFailed || f.Phase == AccountUnknown {
		if f.Reason != DeliveryRejected && f.Reason != DeliveryTimeout && f.Reason != DeliveryCancelled && f.Reason != DeliveryUnknown {
			return Metadata{}, invalid("metadata")
		}
	} else if f.Reason != "" {
		return Metadata{}, invalid("metadata")
	}
	if action != SMTPDelivery && action != SMTPTestRequest && action != AccountInviteCreate && f.Channel != "" {
		return Metadata{}, invalid("metadata")
	}
	fields := append([]AccountChangedField(nil), f.ChangedFields...)
	for _, v := range fields {
		if !allowedFields[v] {
			return Metadata{}, invalid("metadata")
		}
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	if len(allowedFields) > 0 && len(fields) == 0 {
		return Metadata{}, invalid("metadata")
	}
	f.ChangedFields = fields
	b, e := json.Marshal(f)
	if e != nil || len(b) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(b)}
	return Metadata{func() metadataData { return d }}, nil
}
func (m Metadata) AccountFields() (AccountMetadataFields, error) {
	if m.data == nil || !AccountAction(m.data().action) {
		return AccountMetadataFields{}, invalid("metadata")
	}
	var f AccountMetadataFields
	if json.Unmarshal([]byte(m.data().raw), &f) != nil {
		return f, invalid("metadata")
	}
	return f, nil
}
func decodeAccountMetadata(action Action, raw []byte) (Metadata, error) {
	allowed := map[string]bool{"user_id": true, "session_id": true, "attempt_id": true, "invitation_id": true, "reset_id": true, "job_id": true, "object_id": true, "version": true, "changed_fields": true, "channel": true, "phase": true, "reason": true, "initiator_id": true}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		k, e := d.Token()
		name, ok := k.(string)
		if e != nil || !ok || !allowed[name] || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if end, e := d.Token(); e != nil || end != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var f AccountMetadataFields
	if json.Unmarshal(raw, &f) != nil {
		return Metadata{}, invalid("metadata")
	}
	return AccountMetadata(action, f)
}
func validateAccountEntry(f EntryFields) error {
	if f.Scope.Details().Kind != identity.System {
		return invalid("entry")
	}
	a := f.Actor.Details()
	if a.Kind != identity.Human && a.Kind != identity.Service {
		return invalid("entry")
	}
	if f.Action == SMTPDeliveryRetry && a.Kind != identity.Human {
		return invalid("actor")
	}
	m, e := f.Metadata.AccountFields()
	if e != nil {
		return e
	}
	r := f.Resource.Details()
	kind, id := UserResource, m.UserID
	switch f.Action {
	case AccountLogin, AccountPasswordResetRequest:
		kind, id = AccountAttemptResource, m.AttemptID
	case AccountLogout:
		kind, id = SessionResource, m.SessionID
	case AccountInviteCreate, AccountInviteRevoke:
		kind, id = InvitationResource, m.InvitationID
	case AccountPasswordResetComplete:
		kind, id = PasswordResetResource, m.ResetID
	case AccountSettingsUpdate:
		kind, id = AccountSettingsResource, r.ID
	case SMTPSettingsUpdate:
		kind, id = SMTPSettingsResource, r.ID
	case SMTPTestRequest, SMTPDelivery, SMTPDeliveryRetry:
		kind, id = MailJobResource, m.JobID
	}
	if r.Kind != kind || r.ID != id {
		return invalid("resource")
	}
	want := Success
	if m.Phase == AccountRejected {
		want = Denied
	}
	if m.Phase == AccountFailed {
		want = Failed
	}
	if m.Phase == AccountUnknown {
		want = Unknown
	}
	if f.Outcome != want {
		return invalid("outcome")
	}
	return nil
}
