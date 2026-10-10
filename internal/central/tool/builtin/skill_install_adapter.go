package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// SkillInstallOperationAuthority is a narrow consumer port, not an authority
// implementation. The actual Tool Runtime must check the original durable
// Operation, actor, selected spec/binding, fixed SkillID, full package digests,
// key, current Capability/Policy and approval state. Returning nil is a current
// precheck only; it cannot replace Skill's own plan/final publication checks.
type SkillInstallOperationAuthority interface {
	RequireCurrentSkillInstall(context.Context, SkillInstallCall) error
}

type skillInstallAdapterData struct {
	service   SkillInstallService
	operation SkillInstallOperationAuthority
}

// SkillInstallAdapter is not a Registry source. No source is exported until an
// actual Service and Runtime implementation are joined by a trusted owner.
type SkillInstallAdapter struct {
	data func() skillInstallAdapterData
}

// SkillInstallResult keeps the complete validated Service receipt for the
// Runtime's original Operation record. Only ModelOutput belongs in the model
// response. This value does not persist that record or grant Runtime authority.
type SkillInstallResult struct {
	receipt skill.InstallReceipt
	output  json.RawMessage
}

func (r SkillInstallResult) Receipt() skill.InstallReceipt { return r.receipt }
func (r SkillInstallResult) ModelOutput() json.RawMessage {
	return append(json.RawMessage(nil), r.output...)
}
func (SkillInstallResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_result")
}
func (SkillInstallResult) LogValue() slog.Value { return slog.StringValue("skill_install_result") }
func (SkillInstallResult) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_result"`), nil
}

func NewSkillInstallAdapter(service SkillInstallService, operation SkillInstallOperationAuthority) (*SkillInstallAdapter, error) {
	if skillInstallNil(service) || skillInstallNil(operation) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	d := skillInstallAdapterData{service, operation}
	return &SkillInstallAdapter{data: func() skillInstallAdapterData { return d }}, nil
}

func skillInstallNil(port any) bool {
	if port == nil {
		return true
	}
	v := reflect.ValueOf(port)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func (c SkillInstallCall) Actor() (id.Actor, error) {
	if err := c.Validate(); err != nil {
		return id.Actor{}, err
	}
	return c.data().actor, nil
}

// Execute performs exactly one synchronous Service call after the actual
// Runtime precheck. It never retries, regenerates SkillID/key, changes actor,
// starts a goroutine, or returns while the admitted Service call is in flight.
func (a *SkillInstallAdapter) Execute(ctx context.Context, call SkillInstallCall, requestID f.ID[f.Request]) (SkillInstallResult, error) {
	if err := skillInstallContext(ctx); err != nil {
		return SkillInstallResult{}, err
	}
	if a == nil || a.data == nil {
		return SkillInstallResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if err := call.Validate(); err != nil {
		return SkillInstallResult{}, err
	}
	if requestID.Validate() != nil {
		return SkillInstallResult{}, skillInstallInvalid()
	}
	s := a.data()
	if err := s.operation.RequireCurrentSkillInstall(ctx, call); err != nil {
		return SkillInstallResult{}, skillInstallPortError(err, f.NotStarted)
	}
	if err := ctx.Err(); err != nil {
		return SkillInstallResult{}, err
	}
	d := call.data()
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: d.details.Key}
	receipt, err := s.service.Install(ctx, d.actor, meta, d.details.ProjectID, d.request)
	if err != nil {
		// Preserve domain commit-state and the original cause. An unexpected
		// service error cannot certify that publication never happened.
		return SkillInstallResult{}, skillInstallPortError(err, f.Unknown)
	}
	// The original Service call has actually returned a known publication.
	// Cancellation may stop later model delivery, but must not erase that
	// receipt. This final bounded validation/projection performs no I/O.
	output, err := ProjectSkillInstallReceipt(context.WithoutCancel(ctx), call, receipt)
	if err != nil {
		return SkillInstallResult{}, err
	}
	return SkillInstallResult{receipt: receipt, output: output}, nil
}

func skillInstallPortError(err error, state f.CommitState) error {
	var fault *f.Fault
	if errors.As(err, &fault) {
		return fault
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return err
	}
	return f.NewFault(f.DependencyUnavailable, state).WithCause(err)
}

func (SkillInstallAdapter) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_adapter")
}
func (SkillInstallAdapter) LogValue() slog.Value { return slog.StringValue("skill_install_adapter") }
func (SkillInstallAdapter) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_adapter"`), nil
}
