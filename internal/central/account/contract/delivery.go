package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type DeliveryPort interface {
	NextDeliveries(context.Context) ([]JobID, error)
	ClaimDelivery(context.Context, JobID) (DeliveryAttempt, error)
	PrepareDelivery(context.Context, DeliveryAttempt) (DeliveryMaterials, error)
	BeginDelivery(context.Context, DeliveryAttempt, DeliveryPhase) (SendPermit, error)
	CheckpointDelivery(context.Context, DeliveryAttempt, DeliveryProtocolPhase) error
	FinishDelivery(context.Context, DeliveryAttempt, DeliveryCompletion) error
	RecoverDeliveries(context.Context) (DeliveryRecoveryStatus, error)
}
type DeliveryRuntime interface {
	CurrentProcess() ProcessID
	RequireActive(DeliveryAttempt) error
	RequireJoined(DeliveryAttempt, DeliveryCompletion) error
}
type DeliveryRecoveryStatus struct{ Examined, Advanced, Pending foundation.Progress }
type DeliveryChannel string

const (
	SMTPChannel        DeliveryChannel = "smtp"
	RecoveryLogChannel DeliveryChannel = "log"
)

func (c DeliveryChannel) Valid() bool { return c == SMTPChannel || c == RecoveryLogChannel }

type DeliveryPhase string

const (
	SMTPAuth    DeliveryPhase = "smtp_auth"
	SMTPMail    DeliveryPhase = "smtp_mail"
	RecoveryLog DeliveryPhase = "recovery_log"
)

func (p DeliveryPhase) Valid() bool { return p == SMTPAuth || p == SMTPMail || p == RecoveryLog }

type DeliveryProtocolPhase string

const (
	Negotiation        DeliveryProtocolPhase = "negotiation"
	Authentication     DeliveryProtocolPhase = "auth"
	Envelope           DeliveryProtocolPhase = "envelope"
	Data               DeliveryProtocolPhase = "data"
	AwaitingAcceptance DeliveryProtocolPhase = "awaiting_acceptance"
	ProtocolClosed     DeliveryProtocolPhase = "closed"
)

type DeliveryResult string

const (
	DeliverySent      DeliveryResult = "sent"
	DeliveryFailed    DeliveryResult = "failed"
	DeliveryUnknown   DeliveryResult = "unknown"
	DeliveryCancelled DeliveryResult = "cancelled"
)

type DeliveryReason string

const (
	ReasonSent          DeliveryReason = "sent"
	ReasonTokenInvalid  DeliveryReason = "token_invalid"
	ReasonConfiguration DeliveryReason = "configuration_invalid"
	ReasonPolicy        DeliveryReason = "policy_rejected"
	ReasonNetwork       DeliveryReason = "network_failed"
	ReasonSMTP          DeliveryReason = "smtp_rejected"
	ReasonTimeout       DeliveryReason = "timeout"
	ReasonCancelled     DeliveryReason = "cancelled"
	ReasonUnknown       DeliveryReason = "unknown"
)

func (r DeliveryReason) Valid() bool {
	switch r {
	case ReasonSent, ReasonTokenInvalid, ReasonConfiguration, ReasonPolicy, ReasonNetwork, ReasonSMTP, ReasonTimeout, ReasonCancelled, ReasonUnknown:
		return true
	}
	return false
}

// Issuers are created by the concrete port/registry and never supplied by a
// request. A separately constructed issuer cannot issue either one's handles.
type DeliveryIssuer struct{ id *byte }

func NewDeliveryIssuer() DeliveryIssuer { return DeliveryIssuer{new(byte)} }

type DeliveryAttemptDetails struct {
	JobID         JobID
	IntentID      IntentID
	AttemptID     AttemptID
	Kind          DeliveryKind
	ProcessID     ProcessID
	Fence         foundation.Sequence
	ConfigVersion foundation.Version
	Channel       DeliveryChannel
}
type DeliveryAttempt struct{ data func() attemptData }
type attemptData struct {
	issuer  DeliveryIssuer
	details DeliveryAttemptDetails
	binding foundation.Digest
}

func NewDeliveryAttempt(i DeliveryIssuer, d DeliveryAttemptDetails, b foundation.Digest) (DeliveryAttempt, error) {
	if i.id == nil || d.JobID.Validate() != nil || d.IntentID.Validate() != nil || d.AttemptID.Validate() != nil || !d.Kind.Valid() || d.ProcessID.Validate() != nil || d.Fence.Validate() != nil || d.ConfigVersion.Validate() != nil || !d.Channel.Valid() || b.Validate() != nil {
		return DeliveryAttempt{}, Invalid()
	}
	a := attemptData{i, d, b}
	return DeliveryAttempt{func() attemptData { return a }}, nil
}
func (a DeliveryAttempt) Validate() error {
	if a.data == nil {
		return Invalid()
	}
	return nil
}
func (a DeliveryAttempt) Details() DeliveryAttemptDetails {
	if a.data == nil {
		return DeliveryAttemptDetails{}
	}
	return a.data().details
}
func (a DeliveryAttempt) Binding() foundation.Digest {
	if a.data == nil {
		return ""
	}
	return a.data().binding
}
func (a DeliveryAttempt) IssuedBy(i DeliveryIssuer) bool {
	return a.data != nil && i.id != nil && a.data().issuer == i
}
func (a DeliveryAttempt) Same(b DeliveryAttempt) bool {
	return a.data != nil && b.data != nil && a.data() == b.data()
}
func (a DeliveryAttempt) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mail_attempt") }
func (a DeliveryAttempt) MarshalJSON() ([]byte, error) { return []byte(`"mail_attempt"`), nil }
func (*DeliveryAttempt) UnmarshalJSON([]byte) error    { return Invalid() }
func (a DeliveryAttempt) LogValue() slog.Value         { return slog.StringValue("mail_attempt") }

type DeliveryOutcome struct {
	Result    DeliveryResult
	Reason    DeliveryReason
	Retryable bool
}

func (o DeliveryOutcome) Validate() error {
	if !o.Reason.Valid() {
		return Invalid()
	}
	switch o.Result {
	case DeliverySent:
		if o.Reason != ReasonSent || o.Retryable {
			return Invalid()
		}
	case DeliveryUnknown:
		if o.Reason != ReasonUnknown && o.Reason != ReasonTimeout && o.Reason != ReasonNetwork {
			return Invalid()
		}
	case DeliveryCancelled:
		if o.Reason != ReasonCancelled && o.Reason != ReasonTokenInvalid {
			return Invalid()
		}
		if o.Retryable {
			return Invalid()
		}
	case DeliveryFailed:
		if o.Reason == ReasonSent || o.Reason == ReasonUnknown {
			return Invalid()
		}
	default:
		return Invalid()
	}
	return nil
}

type DeliveryCompletion struct{ data func() completionData }
type completionData struct {
	issuer  DeliveryIssuer
	attempt DeliveryAttempt
	outcome DeliveryOutcome
}

func NewDeliveryCompletion(i DeliveryIssuer, a DeliveryAttempt, o DeliveryOutcome) (DeliveryCompletion, error) {
	if i.id == nil || a.Validate() != nil || o.Validate() != nil {
		return DeliveryCompletion{}, Invalid()
	}
	d := completionData{i, a, o}
	return DeliveryCompletion{func() completionData { return d }}, nil
}
func (c DeliveryCompletion) Matches(i DeliveryIssuer, a DeliveryAttempt) bool {
	return c.data != nil && i.id != nil && c.data().issuer == i && c.data().attempt.Same(a)
}
func (c DeliveryCompletion) Outcome() DeliveryOutcome {
	if c.data == nil {
		return DeliveryOutcome{}
	}
	return c.data().outcome
}
func (c DeliveryCompletion) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mail_completion") }
func (c DeliveryCompletion) MarshalJSON() ([]byte, error) { return []byte(`"mail_completion"`), nil }
func (*DeliveryCompletion) UnmarshalJSON([]byte) error    { return Invalid() }
func (c DeliveryCompletion) LogValue() slog.Value         { return slog.StringValue("mail_completion") }

// Materials have an explicit trusted-adapter projection, never a JSON DTO.
// The owner must join every Use before Destroy is considered complete.
type DeliveryMaterialFields struct {
	Attempt                                                    DeliveryAttempt
	Recipient, Host, Username, SenderEmail, SenderName, LinkID string
	Port                                                       int
	TLSMode                                                    string
	ExpiresAt                                                  time.Time
	Token, Password                                            sc.SecretMaterial
}
type materialState struct {
	mu     sync.Mutex
	fields DeliveryMaterialFields
	closed bool
	uses   int
	done   chan struct{}
	once   sync.Once
}
type DeliveryMaterials struct{ data func() *materialState }

func NewDeliveryMaterials(f DeliveryMaterialFields) (DeliveryMaterials, error) {
	if f.Attempt.Validate() != nil || f.Recipient == "" {
		return DeliveryMaterials{}, Invalid()
	}
	d := &materialState{fields: f, done: make(chan struct{})}
	return DeliveryMaterials{func() *materialState { return d }}, nil
}
func (m DeliveryMaterials) Use(fn func(DeliveryMaterialFields) error) error {
	if m.data == nil || fn == nil {
		return Invalid()
	}
	s := m.data()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Invalid()
	}
	s.uses++
	f := s.fields
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.uses--; s.finish(); s.mu.Unlock() }()
	return fn(f)
}
func (s *materialState) finish() {
	if s.closed && s.uses == 0 {
		s.once.Do(func() {
			s.fields.Token.Destroy()
			s.fields.Password.Destroy()
			s.fields = DeliveryMaterialFields{}
			close(s.done)
		})
	}
}
func (m DeliveryMaterials) Destroy() {
	if m.data == nil {
		return
	}
	s := m.data()
	s.mu.Lock()
	s.closed = true
	s.finish()
	s.mu.Unlock()
}
func (m DeliveryMaterials) Joined() bool {
	if m.data == nil {
		return true
	}
	select {
	case <-m.data().done:
		return true
	default:
		return false
	}
}
func (m DeliveryMaterials) Done() <-chan struct{} {
	if m.data == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return m.data().done
}
func (m DeliveryMaterials) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mail_materials") }
func (m DeliveryMaterials) MarshalJSON() ([]byte, error) { return []byte(`"mail_materials"`), nil }
func (*DeliveryMaterials) UnmarshalJSON([]byte) error    { return Invalid() }
func (m DeliveryMaterials) LogValue() slog.Value         { return slog.StringValue("mail_materials") }

type permitState struct {
	mu                    sync.Mutex
	ctx                   context.Context
	cancel                context.CancelFunc
	phase                 DeliveryPhase
	used, closed, running bool
	release               func()
	once                  sync.Once
	stop                  func() bool
}
type SendPermit struct{ data func() *permitState }

// NewSendPermit is for the account port after its exact checkpoint commits.
// Its callback releases a private gate; the gate itself is never projected.
func NewSendPermit(ctx context.Context, phase DeliveryPhase, deadline time.Time, release func()) (SendPermit, error) {
	if ctx == nil || !phase.Valid() || release == nil || !deadline.After(time.Now()) {
		return SendPermit{}, Invalid()
	}
	c, cancel := context.WithDeadline(ctx, deadline)
	s := &permitState{ctx: c, cancel: cancel, phase: phase, release: release}
	s.stop = context.AfterFunc(c, func() {
		s.mu.Lock()
		s.closed = true
		if !s.running {
			s.finish()
		}
		s.mu.Unlock()
	})
	return SendPermit{func() *permitState { return s }}, nil
}
func (s *permitState) finish() { s.once.Do(func() { s.cancel(); s.release() }) }
func (p SendPermit) run(log bool, f func(context.Context) (int, error)) (int, error) {
	if p.data == nil || f == nil {
		return 0, Invalid()
	}
	s := p.data()
	s.mu.Lock()
	if s.used || s.closed || s.ctx.Err() != nil || (log != (s.phase == RecoveryLog)) {
		s.mu.Unlock()
		return 0, Invalid()
	}
	s.used = true
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.closed = true; s.finish(); s.mu.Unlock() }()
	return f(s.ctx)
}
func (p SendPermit) RunSMTPFirstWrite(f func(context.Context) (int, error)) (int, error) {
	return p.run(false, f)
}
func (p SendPermit) GrantRecoveryLog(f func() error) error {
	if f == nil {
		return Invalid()
	}
	_, e := p.run(true, func(ctx context.Context) (int, error) {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, f()
	})
	return e
}
func (p SendPermit) Close() {
	if p.data == nil {
		return
	}
	s := p.data()
	s.mu.Lock()
	s.closed = true
	s.cancel()
	if !s.running {
		s.finish()
	}
	s.mu.Unlock()
}
func (p SendPermit) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mail_send_permit") }
func (p SendPermit) MarshalJSON() ([]byte, error) { return []byte(`"mail_send_permit"`), nil }
func (*SendPermit) UnmarshalJSON([]byte) error    { return Invalid() }
func (p SendPermit) LogValue() slog.Value         { return slog.StringValue("mail_send_permit") }

type SMTPSettings struct {
	ID                   SettingsID          `json:"id"`
	Version              foundation.Version  `json:"version"`
	Configured           bool                `json:"configured"`
	Host                 string              `json:"host"`
	Port                 int                 `json:"port"`
	TLSMode              string              `json:"encryption"`
	Username             string              `json:"username"`
	SenderEmail          string              `json:"sender_email"`
	SenderName           string              `json:"sender_name"`
	CredentialPresent    bool                `json:"credential_present"`
	RetryCount           foundation.Progress `json:"auto_retry_count"`
	RetryIntervalSeconds foundation.Progress `json:"retry_interval_seconds"`
}

func (s SMTPSettings) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "smtp_settings") }
func (s SMTPSettings) LogValue() slog.Value       { return slog.StringValue("smtp_settings") }

type SMTPUpdateFields struct {
	Actor                                            identity.Actor
	Key                                              foundation.IdempotencyKey
	ExpectedVersion                                  foundation.Version
	Configured                                       bool
	Host, TLSMode, Username, SenderEmail, SenderName string
	Port                                             int
	RetryCount, RetryIntervalSeconds                 int64
	CredentialAction                                 string // keep or remove; a nonempty Password replaces it
	Password                                         *sc.SecretMaterial
}
type SMTPUpdate struct{ data func() SMTPUpdateFields }

func NewSMTPUpdate(f SMTPUpdateFields) (SMTPUpdate, error) {
	if f.Actor.Validate() != nil || f.Actor.Details().Kind != identity.Human || f.Key.Validate() != nil || f.ExpectedVersion.Validate() != nil {
		return SMTPUpdate{}, Invalid()
	}
	if f.Password != nil {
		value := *f.Password
		f.Password = &value
	}
	return SMTPUpdate{func() SMTPUpdateFields { return f }}, nil
}
func (r SMTPUpdate) Fields() SMTPUpdateFields {
	if r.data == nil {
		return SMTPUpdateFields{}
	}
	f := r.data()
	if f.Password != nil {
		v := *f.Password
		f.Password = &v
	}
	return f
}
func (r SMTPUpdate) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r SMTPUpdate) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "smtp_update") }
func (r SMTPUpdate) MarshalJSON() ([]byte, error) { return []byte(`"smtp_update"`), nil }
func (*SMTPUpdate) UnmarshalJSON([]byte) error    { return Invalid() }
func (r SMTPUpdate) LogValue() slog.Value         { return slog.StringValue("smtp_update") }

type SMTPTest struct {
	Actor     identity.Actor
	Key       foundation.IdempotencyKey
	Recipient string
}

func (r SMTPTest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "smtp_test") }
func (r SMTPTest) MarshalJSON() ([]byte, error) { return []byte(`"smtp_test"`), nil }
func (*SMTPTest) UnmarshalJSON([]byte) error    { return Invalid() }
func (r SMTPTest) LogValue() slog.Value         { return slog.StringValue("smtp_test") }

type MailJobStatus struct {
	JobID    JobID               `json:"job_id"`
	Phase    string              `json:"phase"`
	Attempts foundation.Progress `json:"attempts"`
	Version  foundation.Version  `json:"version"`
	Reason   DeliveryReason      `json:"reason,omitempty"`
}

type SMTPSettingsReceipt struct {
	ID      SettingsID         `json:"id"`
	Version foundation.Version `json:"version"`
}

// MailJobRetry starts one independently audited cycle from a terminal job.
// The public key identifies the command, never an actual SMTP attempt.
type MailJobRetry struct {
	Actor           identity.Actor
	Key             foundation.IdempotencyKey
	JobID           JobID
	ExpectedVersion foundation.Version
}
