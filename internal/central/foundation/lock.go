package foundation

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type LockMode string

const (
	Shared    LockMode = "shared"
	Exclusive LockMode = "exclusive"
)

func (m LockMode) Valid() bool { return m == Shared || m == Exclusive }

// LockKey constructors fix the global namespace and rank. Callers cannot pick
// an arbitrary rank to disguise a gate as a later aggregate lock.
type LockKey struct {
	rank      uint8
	kindRank  uint8
	canonical func() string
}
type LockRequest struct {
	Key  LockKey
	Mode LockMode
}

var errLockKey = errors.New("INVALID_LOCK_KEY")

func (k LockKey) Validate() error {
	if k.canonical == nil {
		return errLockKey
	}
	return nil
}

// Canonical is an explicit lock/lookup projection. Generic formatting must not
// expose business command keys, even through unexported enclosing fields.
func (k LockKey) Canonical() string {
	if k.canonical == nil {
		return ""
	}
	return k.canonical()
}
func newLockKey(rank uint8, canonical string) LockKey {
	return LockKey{rank: rank, canonical: func() string { return canonical }}
}
func (k LockKey) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "lock_key") }
func (k LockKey) MarshalJSON() ([]byte, error) { return []byte(`"lock_key"`), nil }
func (k LockKey) LogValue() slog.Value         { return slog.StringValue("lock_key") }
func CompareLockKeys(a, b LockKey) int {
	if c := cmp.Compare(a.rank, b.rank); c != 0 {
		return c
	}
	if c := cmp.Compare(a.kindRank, b.kindRank); c != 0 {
		return c
	}
	return strings.Compare(a.Canonical(), b.Canonical())
}
func (k LockKey) AdvisoryKey() int64 {
	digest := sha256.Sum256([]byte("agenteam.lock.v1\x00" + k.Canonical()))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}
func CommandLock(identity CommandIdentity) (LockKey, error) {
	if identity.Validate() != nil {
		return LockKey{}, errLockKey
	}
	return newLockKey(0, "command:"+identity.Canonical()), nil
}
func SystemConfigLock(kind string) (LockKey, error) {
	if !stableName(kind) {
		return LockKey{}, errLockKey
	}
	return newLockKey(1, "system-config:"+kind), nil
}
func identityLock(rank uint8, kind, id string) (LockKey, error) {
	if !validScalarID(id) {
		return LockKey{}, errLockKey
	}
	return newLockKey(rank, kind+":"+id), nil
}
func UserLock(id string) (LockKey, error)            { return identityLock(1, "user", id) }
func ProjectLock(id string) (LockKey, error)         { return identityLock(2, "project", id) }
func ProjectScheduleLock(id string) (LockKey, error) { return identityLock(3, "project-schedule", id) }
func KnowledgeTreeLock(id string) (LockKey, error)   { return identityLock(3, "knowledge-tree", id) }
func RankGroupLock(scope string) (LockKey, error) {
	if IdempotencyKey(scope).Validate() != nil {
		return LockKey{}, errLockKey
	}
	return newLockKey(3, "rank-group:"+scope), nil
}
func AgentLock(id string) (LockKey, error) { return identityLock(4, "agent", id) }

type AggregateKind string

const (
	ProviderAggregate      AggregateKind = "provider"
	ModelConfigAggregate   AggregateKind = "model_config"
	ToolSpecAggregate      AggregateKind = "tool_spec"
	MCPConnectionAggregate AggregateKind = "mcp_connection"
	CredentialRefAggregate AggregateKind = "credential_ref"
	SprintAggregate        AggregateKind = "sprint"
	TaskAggregate          AggregateKind = "task"
	MeetingAggregate       AggregateKind = "meeting"
	TurnAggregate          AggregateKind = "turn"
	ContributionAggregate  AggregateKind = "contribution"
	DispatchAggregate      AggregateKind = "dispatch"
	ExecutionAggregate     AggregateKind = "execution"
	OperationAggregate     AggregateKind = "operation"
	ApprovalAggregate      AggregateKind = "approval"
	DecisionAggregate      AggregateKind = "decision"
	SkillAggregate         AggregateKind = "skill"
	MemoryAggregate        AggregateKind = "memory"
	ObjectAggregate        AggregateKind = "object"
)

var aggregateOrder = [...]AggregateKind{ProviderAggregate, ModelConfigAggregate, ToolSpecAggregate, MCPConnectionAggregate, CredentialRefAggregate, SprintAggregate, TaskAggregate, MeetingAggregate, TurnAggregate, ContributionAggregate, DispatchAggregate, ExecutionAggregate, OperationAggregate, ApprovalAggregate, DecisionAggregate, SkillAggregate, MemoryAggregate, ObjectAggregate}

func AggregateLock(kind AggregateKind, id string) (LockKey, error) {
	key, err := identityLock(5, "aggregate:"+string(kind), id)
	if err != nil {
		return LockKey{}, err
	}
	for i, known := range aggregateOrder {
		if kind == known {
			key.kindRank = uint8(i)
			return key, nil
		}
	}
	return LockKey{}, errLockKey
}

type RecordKind string

const (
	CommandRecordLock    RecordKind = "command-record"
	OutboxRecordLock     RecordKind = "outbox"
	ReferenceRecordLock  RecordKind = "reference"
	ProjectionRecordLock RecordKind = "projection"
)

func RecordLock(kind RecordKind, identity string) (LockKey, error) {
	if IdempotencyKey(identity).Validate() != nil {
		return LockKey{}, errLockKey
	}
	switch kind {
	case CommandRecordLock, OutboxRecordLock, ReferenceRecordLock, ProjectionRecordLock:
	default:
		return LockKey{}, errLockKey
	}
	return newLockKey(6, string(kind)+":"+identity), nil
}
