package contract

import "time"

// AgentRetryTiming is explicit Runtime configuration, never consumer authority.
// It bounds individual requests and delays; it imposes no logical-call deadline
// or attempt limit. The consumer still authorizes the retry categories.
type AgentRetryTiming struct{ fields AgentRetryTimingFields }

type AgentRetryTimingFields struct {
	InitialRequestTimeout time.Duration `json:"initial_request_timeout"`
	MaxRequestTimeout     time.Duration `json:"max_request_timeout"`
	TimeoutMultiplier     uint32        `json:"timeout_multiplier"`
	InitialBackoff        time.Duration `json:"initial_backoff"`
	MaxBackoff            time.Duration `json:"max_backoff"`
}

func NewAgentRetryTiming(fields AgentRetryTimingFields) (AgentRetryTiming, error) {
	t := AgentRetryTiming{fields: fields}
	if err := t.Validate(); err != nil {
		return AgentRetryTiming{}, err
	}
	return t, nil
}

func (t AgentRetryTiming) Fields() AgentRetryTimingFields { return t.fields }
func (t AgentRetryTiming) Clone() AgentRetryTiming        { return t }
func (t AgentRetryTiming) Validate() error {
	f := t.fields
	if f.InitialRequestTimeout <= 0 || f.MaxRequestTimeout < f.InitialRequestTimeout || f.TimeoutMultiplier < 2 || f.InitialBackoff <= 0 || f.MaxBackoff < f.InitialBackoff {
		return bad()
	}
	return nil
}
