package outbound

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Decision struct {
	Reason        ac.Reason           `json:"reason,omitempty"`
	Consumer      ac.Consumer         `json:"consumer"`
	Origin        string              `json:"origin,omitempty"`
	PolicyVersion *foundation.Version `json:"policy_version,omitempty"`
	AddressClass  AddressClass        `json:"address_class,omitempty"`
	RedirectCount int                 `json:"redirect_count"`
	Sent          bool                `json:"sent"`
	HTTPTraceID   string              `json:"http_trace_id,omitempty"`
}
type networkFailure struct {
	decision Decision
	cause    error
}
type NetworkError struct{ data func() networkFailure }

func networkError(d Decision, reason ac.Reason, cause error) *NetworkError {
	if !reason.Valid() {
		reason = ac.InternalError
	}
	d.Reason = reason
	if d.PolicyVersion != nil {
		v := *d.PolicyVersion
		d.PolicyVersion = &v
	}
	v := networkFailure{d, cause}
	return &NetworkError{data: func() networkFailure { return v }}
}
func (e *NetworkError) Error() string {
	if e == nil || e.data == nil {
		return "OUTBOUND_INTERNAL_ERROR"
	}
	return "OUTBOUND_" + strings.ToUpper(string(e.data().decision.Reason))
}
func (e *NetworkError) Decision() Decision {
	if e == nil || e.data == nil {
		return Decision{Reason: ac.InternalError}
	}
	d := e.data().decision
	if d.PolicyVersion != nil {
		v := *d.PolicyVersion
		d.PolicyVersion = &v
	}
	return d
}
func (e *NetworkError) Unwrap() error {
	if e == nil || e.data == nil {
		return nil
	}
	return e.data().cause
}
func (e NetworkError) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, e.Error()) }
func (e NetworkError) MarshalJSON() ([]byte, error) { return json.Marshal(e.Decision()) }
func (e NetworkError) LogValue() slog.Value {
	d := e.Decision()
	fields := []slog.Attr{slog.String("reason", string(d.Reason)), slog.String("consumer", string(d.Consumer)), slog.String("origin", d.Origin), slog.String("address_class", string(d.AddressClass)), slog.Int("redirect_count", d.RedirectCount), slog.Bool("sent", d.Sent), slog.String("http_trace_id", d.HTTPTraceID)}
	if d.PolicyVersion != nil {
		fields = append(fields, slog.String("policy_version", d.PolicyVersion.String()))
	}
	return slog.GroupValue(fields...)
}
