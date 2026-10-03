package foundation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestLockKeyDoesNotFormatBusinessIdentity(t *testing.T) {
	identity, err := NewCommandIdentity("system", nil, "test.command", "private-business-key-sentinel")
	if err != nil {
		t.Fatal(err)
	}
	key, err := CommandLock(identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(key.Canonical(), "private-business-key-sentinel") {
		t.Fatal("explicit canonical identity lost")
	}
	for _, value := range []any{key, &key, struct{ hidden LockKey }{key}, []LockKey{key}, map[string]LockKey{"key": key}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, value), "private-business-key-sentinel") {
				t.Fatal("fmt leaked command identity")
			}
		}
		encoded, err := json.Marshal(value)
		if err != nil || strings.Contains(string(encoded), "private-business-key-sentinel") {
			t.Fatal("JSON leaked command identity")
		}
		var output bytes.Buffer
		slog.New(slog.NewTextHandler(&output, nil)).LogAttrs(context.Background(), slog.LevelInfo, "test", slog.Any("key", value))
		if strings.Contains(output.String(), "private-business-key-sentinel") {
			t.Fatal("slog leaked command identity")
		}
	}
}

func TestLockNamespacesAndGlobalOrdering(t *testing.T) {
	id := "01900000-0000-7000-8000-000000000001"
	command, _ := NewCommandIdentity("project", []string{id}, "complete", "key")
	c, _ := CommandLock(command)
	system, _ := SystemConfigLock("model")
	user, _ := UserLock(id)
	project, _ := ProjectLock(id)
	tree, _ := KnowledgeTreeLock(id)
	schedule, _ := ProjectScheduleLock(id)
	rank, _ := RankGroupLock("project/" + id)
	agent, _ := AgentLock(id)
	provider, _ := AggregateLock(ProviderAggregate, id)
	object, _ := AggregateLock(ObjectAggregate, id)
	record, _ := RecordLock(OutboxRecordLock, id)
	keys := []LockKey{c, system, user, project, tree, schedule, rank, agent, provider, object, record}
	for i, key := range keys {
		if key.Validate() != nil {
			t.Fatal("constructed invalid key")
		}
		if i > 0 && CompareLockKeys(keys[i-1], key) >= 0 {
			t.Fatalf("out of order: %v then %v", keys[i-1], key)
		}
		hash := sha256.Sum256([]byte("agenteam.lock.v1\x00" + key.Canonical()))
		if key.AdvisoryKey() != int64(binary.BigEndian.Uint64(hash[:8])) {
			t.Fatal("wrong advisory hash")
		}
	}
	if _, err := AggregateLock("invented", id); err == nil {
		t.Fatal("unknown aggregate")
	}
	if _, err := ProjectLock("invalid"); err == nil {
		t.Fatal("invalid identity")
	}
	if _, err := RecordLock("invented", id); err == nil {
		t.Fatal("unknown record")
	}
	if (LockKey{}).Validate() == nil || LockMode("invented").Valid() {
		t.Fatal("invalid zero/mode accepted")
	}
}
