// Package contract defines account facts and trusted backend capabilities.
// It contains no HTTP identity inference or implicit administrator authority.
package contract

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type UserID = identity.UserID
type SessionID = identity.SessionID
type Browser struct{}
type Attempt struct{}
type Command struct{}
type Process struct{}
type Invitation struct{}
type PasswordReset struct{}
type SettingsRecord struct{}
type Cleanup struct{}
type BrowserID = foundation.ID[Browser]
type AttemptID = foundation.ID[Attempt]
type CommandID = foundation.ID[Command]
type ProcessID = foundation.ID[Process]
type InvitationID = foundation.ID[Invitation]
type ResetID = foundation.ID[PasswordReset]
type SettingsID = foundation.ID[SettingsRecord]
type CleanupID = foundation.ID[Cleanup]

type Role string

const (
	Administrator Role = "admin"
	RegularUser   Role = "user"
)

func (r Role) Valid() bool { return r == Administrator || r == RegularUser }

type Theme string

const (
	SystemTheme Theme = "system"
	LightTheme  Theme = "light"
	DarkTheme   Theme = "dark"
)

func (t Theme) Valid() bool { return t == SystemTheme || t == LightTheme || t == DarkTheme }

// User is an explicit authenticated response. No credential/ref/verifier is
// part of this projection. Generic formatting is deliberately less detailed.
type User struct {
	ID                        UserID             `json:"id"`
	Email                     string             `json:"email"`
	Username                  string             `json:"username"`
	DisplayName               string             `json:"display_name"`
	Role                      Role               `json:"role"`
	Theme                     Theme              `json:"theme"`
	Version                   foundation.Version `json:"version"`
	InitialPasswordSuggestion bool               `json:"initial_password_suggestion"`
}

func (u User) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "account_user") }
func (u User) LogValue() slog.Value       { return slog.StringValue("account_user") }

type Session struct {
	ID                SessionID          `json:"id"`
	IssuedAt          foundation.Instant `json:"issued_at"`
	AbsoluteExpiresAt foundation.Instant `json:"absolute_expires_at"`
	IdleExpiresAt     foundation.Instant `json:"idle_expires_at"`
}

func Invalid() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
