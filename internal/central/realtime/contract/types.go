// Package contract defines the bounded M3 browser notification protocol.
// Topics identify resources; they never constitute permission to read them.
package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TopicKind string

const (
	MeetingTimeline        TopicKind = "meeting.timeline"
	MeetingExecution       TopicKind = "meeting.execution"
	TimelineSchema                   = "meeting_timeline_v1"
	ExecutionSchema                  = "meeting_execution_runtime_v1"
	MaxSubscriptions                 = 16
	MaxQueuedNotifications           = 64
	MaxInboundBytes                  = 8 << 10
	MaxSnapshotBytes                 = (16 << 20) + (256 << 10)
)

// Meeting/Contribution IDs remain canonical UUID strings at this transport
// boundary, as in Execution.Trigger. Their actual domain relationships must be
// checked by the original Meeting authority, including the current g/a/E.
type Topic struct {
	Kind           TopicKind     `json:"kind"`
	ProjectID      i.ProjectID   `json:"project_id"`
	MeetingID      string        `json:"meeting_id"`
	ContributionID string        `json:"contribution_id,omitempty"`
	Generation     f.Version     `json:"generation,omitempty"`
	Attempt        f.Version     `json:"attempt,omitempty"`
	ExecutionID    i.ExecutionID `json:"execution_id,omitzero"`
}

func canonicalID(value string) bool {
	id, err := f.ParseID[struct{}](value)
	return err == nil && id.String() == value
}
func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

func (t Topic) Validate() error {
	if t.ProjectID.Validate() != nil || !canonicalID(t.MeetingID) {
		return invalid()
	}
	switch t.Kind {
	case MeetingTimeline:
		if t.ContributionID != "" || t.Generation != 0 || t.Attempt != 0 || t.ExecutionID != (i.ExecutionID{}) {
			return invalid()
		}
	case MeetingExecution:
		if !canonicalID(t.ContributionID) || t.Generation.Validate() != nil || t.Attempt.Validate() != nil || t.ExecutionID.Validate() != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

type StreamEpoch struct{}
type Subscription struct{}
type EpochID = f.ID[StreamEpoch]
type SubscriptionID = f.ID[Subscription]

// Watermark belongs to this Hub epoch only. It is neither Session.Version nor
// Execution's durable progress, and is not a replay cursor after reconnect.
type Watermark struct {
	Epoch    EpochID    `json:"epoch"`
	Sequence f.Progress `json:"sequence"`
}

func (w Watermark) Validate() error {
	if w.Epoch.Validate() != nil || w.Sequence.Validate() != nil {
		return invalid()
	}
	return nil
}

// Snapshot carries an already-authorized, domain-validated public projection.
// The digest binds its actual canonical bytes, not the transport watermark.
// Domain readers must never include private Model frames, reasoning, Secrets,
// or a synthetic claim that an uncommitted update was included.
type Snapshot struct {
	Topic   Topic           `json:"topic"`
	Schema  string          `json:"schema"`
	Content json.RawMessage `json:"content"`
	Digest  f.Digest        `json:"digest"`
}

func NewSnapshot(topic Topic, publicJSON []byte) (Snapshot, error) {
	if topic.Validate() != nil || len(publicJSON) == 0 || len(publicJSON) > MaxSnapshotBytes {
		return Snapshot{}, invalid()
	}
	raw, err := cursor.CanonicalJSON(publicJSON)
	if err != nil || len(raw) == 0 || raw[0] != '{' || len(raw) > MaxSnapshotBytes {
		return Snapshot{}, invalid()
	}
	schema := TimelineSchema
	if topic.Kind == MeetingExecution {
		schema = ExecutionSchema
	}
	digest := sha256.Sum256(raw)
	return Snapshot{Topic: topic, Schema: schema, Content: raw, Digest: f.Digest("sha256:" + hex.EncodeToString(digest[:]))}, nil
}

func (s Snapshot) Validate() error {
	want, err := NewSnapshot(s.Topic, s.Content)
	if err != nil || s.Schema != want.Schema || s.Digest.Validate() != nil || s.Digest != want.Digest || !bytes.Equal(s.Content, want.Content) {
		return invalid()
	}
	return nil
}

type ClientType string

const (
	Subscribe    ClientType = "subscribe"
	Unsubscribe  ClientType = "unsubscribe"
	ReadSnapshot ClientType = "snapshot"
)

// A SubscriptionID is chosen by the browser and unique within its connection.
// Reuse cannot replace a live topic. Snapshot reads use the admitted topic.
type ClientMessage struct {
	Type           ClientType     `json:"type"`
	SubscriptionID SubscriptionID `json:"subscription_id"`
	Topic          *Topic         `json:"topic,omitempty"`
}

func (m ClientMessage) Validate() error {
	if m.SubscriptionID.Validate() != nil {
		return invalid()
	}
	switch m.Type {
	case Subscribe:
		if m.Topic == nil || m.Topic.Validate() != nil {
			return invalid()
		}
	case Unsubscribe, ReadSnapshot:
		if m.Topic != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

func DecodeClientMessage(raw []byte) (ClientMessage, error) {
	if len(raw) == 0 || len(raw) > MaxInboundBytes {
		return ClientMessage{}, invalid()
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return ClientMessage{}, invalid()
	}
	var m ClientMessage
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Validate() != nil {
		return ClientMessage{}, invalid()
	}
	// encoding/json accepts case-insensitive field aliases. The wire protocol
	// does not: require exact keys, including the absence of a null topic on
	// unsubscribe/snapshot and irrelevant fields on a timeline topic.
	if m.Type != Subscribe {
		if _, ok := exactObject(canonical, "type", "subscription_id"); !ok {
			return ClientMessage{}, invalid()
		}
		return m, nil
	}
	fields, ok := exactObject(canonical, "type", "subscription_id", "topic")
	if !ok {
		return ClientMessage{}, invalid()
	}
	keys := []string{"kind", "project_id", "meeting_id"}
	if m.Topic.Kind == MeetingExecution {
		keys = append(keys, "contribution_id", "generation", "attempt", "execution_id")
	}
	if _, ok := exactObject(fields["topic"], keys...); !ok {
		return ClientMessage{}, invalid()
	}
	return m, nil
}

func exactObject(raw []byte, keys ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return fields, true
}

type ServerType string

const (
	Subscribed   ServerType = "subscribed"
	SnapshotRead ServerType = "snapshot"
	Invalidated  ServerType = "invalidated"
	Resync       ServerType = "resync"
	Unsubscribed ServerType = "unsubscribed"
)

type ServerMessage struct {
	Type           ServerType     `json:"type"`
	SubscriptionID SubscriptionID `json:"subscription_id"`
	Topic          *Topic         `json:"topic,omitempty"`
	Watermark      *Watermark     `json:"watermark,omitempty"`
	Snapshot       *Snapshot      `json:"snapshot,omitempty"`
	Reason         string         `json:"reason,omitempty"`
}

func (m ServerMessage) Validate() error {
	if m.SubscriptionID.Validate() != nil {
		return invalid()
	}
	if m.Type == Unsubscribed {
		if m.Topic != nil || m.Watermark != nil || m.Snapshot != nil || m.Reason != "" {
			return invalid()
		}
		return nil
	}
	if m.Topic == nil || m.Topic.Validate() != nil || m.Watermark == nil || m.Watermark.Validate() != nil {
		return invalid()
	}
	switch m.Type {
	case Subscribed, Invalidated:
		if m.Snapshot != nil || m.Reason != "" {
			return invalid()
		}
	case SnapshotRead:
		if m.Snapshot == nil || m.Snapshot.Validate() != nil || m.Snapshot.Topic != *m.Topic || m.Reason != "" {
			return invalid()
		}
	case Resync:
		if m.Snapshot != nil || m.Reason != "overflow" && m.Reason != "epoch_changed" && m.Reason != "unavailable" {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
