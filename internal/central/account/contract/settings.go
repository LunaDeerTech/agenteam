package contract

import "github.com/LunaDeerTech/agenteam/internal/central/foundation"

type Settings struct {
	ID                     SettingsID          `json:"id"`
	Version                foundation.Version  `json:"version"`
	SessionIdleSeconds     foundation.Progress `json:"session_idle_seconds"`
	SessionAbsoluteSeconds foundation.Progress `json:"session_absolute_seconds"`
	PasswordResetSeconds   foundation.Progress `json:"password_reset_seconds"`
	ChallengeAfterFailures foundation.Progress `json:"challenge_after_failures"`
}

func (s Settings) Validate() error {
	if s.ID.Validate() != nil || s.Version.Validate() != nil || s.SessionIdleSeconds < 900 || s.SessionIdleSeconds > 2592000 || s.SessionAbsoluteSeconds < 3600 || s.SessionAbsoluteSeconds > 7776000 || s.SessionIdleSeconds > s.SessionAbsoluteSeconds || s.PasswordResetSeconds < 300 || s.PasswordResetSeconds > 7200 || s.ChallengeAfterFailures < 1 || s.ChallengeAfterFailures > 20 {
		return Invalid()
	}
	return nil
}
