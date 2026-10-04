package logging

// Security phases are process diagnostics, never domain objects or raw errors.
type SecurityPhase string

const (
	CursorInitializing   SecurityPhase = "cursor_initializing"
	AuditInitializing    SecurityPhase = "audit_initializing"
	SecretInitializing   SecurityPhase = "secret_initializing"
	SecretMaintaining    SecurityPhase = "secret_maintenance_starting"
	SecretUnavailable    SecurityPhase = "secret_unavailable"
	OutboundInitializing SecurityPhase = "outbound_initializing"
	ObjectInitializing   SecurityPhase = "object_initializing"
	ObjectAvailable      SecurityPhase = "object_available"
	ObjectUnavailable    SecurityPhase = "object_unavailable"
	SecurityInitialized  SecurityPhase = "initialized"
	SecurityFailed       SecurityPhase = "failed"
)

func (l *Logger) Security(phase SecurityPhase) {
	switch phase {
	case ObjectInitializing, ObjectAvailable, ObjectUnavailable, CursorInitializing, AuditInitializing, SecretInitializing, SecretMaintaining, SecretUnavailable, OutboundInitializing, SecurityInitialized, SecurityFailed:
	default:
		phase = SecurityFailed
	}
	l.logger.Info("security", "event", "security", "phase", phase)
}
