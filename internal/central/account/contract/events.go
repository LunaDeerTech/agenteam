package contract

import (
	"fmt"
	"io"
	"log/slog"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const AccountProducer event.StableName = "account"
const SessionsRevokedType event.StableName = "account.sessions-revoked"
const UserAuthAggregate event.StableName = "account-user-auth"

type RevocationScope string

const (
	OneSession  RevocationScope = "one"
	AllSessions RevocationScope = "all"
)

type RevocationReason string

const (
	LoggedOut        RevocationReason = "logout"
	PasswordChanged  RevocationReason = "password_changed"
	PasswordWasReset RevocationReason = "password_reset"
)

type SessionsRevoked struct {
	UserID    UserID           `json:"user_id"`
	Scope     RevocationScope  `json:"scope"`
	SessionID string           `json:"session_id,omitempty"`
	Reason    RevocationReason `json:"reason"`
}

func (p SessionsRevoked) Validate() error {
	if p.UserID.Validate() != nil {
		return Invalid()
	}
	if p.Scope == OneSession {
		if p.Reason != LoggedOut {
			return Invalid()
		}
		if _, e := foundation.ParseID[identity.Session](p.SessionID); e != nil {
			return Invalid()
		}
		return nil
	}
	if p.Scope == AllSessions && p.SessionID == "" && (p.Reason == PasswordChanged || p.Reason == PasswordWasReset) {
		return nil
	}
	return Invalid()
}
func (p SessionsRevoked) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "account_sessions_revoked")
}
func (p SessionsRevoked) LogValue() slog.Value { return slog.StringValue("account_sessions_revoked") }
func DefineSessionsRevoked(catalog *event.Catalog) (event.EventType[SessionsRevoked], error) {
	return event.DefineEvent(catalog, event.Definition[SessionsRevoked]{Schema: event.Schema{Producer: AccountProducer, EventType: SessionsRevokedType, AggregateType: UserAuthAggregate, Version: 1}, Codec: event.JSONCodec[SessionsRevoked]{}, Validate: func(v SessionsRevoked) error { return v.Validate() }})
}
