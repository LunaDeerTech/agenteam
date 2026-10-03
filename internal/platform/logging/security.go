package logging

// Security phases are process diagnostics, never domain objects or raw errors.
type SecurityPhase string

const (
	CursorInitializing  SecurityPhase = "cursor_initializing"
	AuditInitializing   SecurityPhase = "audit_initializing"
	SecurityInitialized SecurityPhase = "initialized"
	SecurityFailed      SecurityPhase = "failed"
)

func (l *Logger) Security(phase SecurityPhase) {
	switch phase {
	case CursorInitializing, AuditInitializing, SecurityInitialized, SecurityFailed:
	default:
		phase = SecurityFailed
	}
	l.logger.Info("security", "event", "security", "phase", phase)
}
