package foundation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCauseIdentityValidationAndIsolation(t *testing.T) {
	id := "01900000-0000-7000-8000-000000000001"
	owners := []string{id}
	command, err := NewCommandIdentity("project", owners, "task.complete", "private-sentinel-key")
	if err != nil {
		t.Fatal(err)
	}
	owners[0] = "changed"
	if command.OwnerIDs()[0] != id {
		t.Fatal("caller can mutate identity")
	}
	gotOwners := command.OwnerIDs()
	gotOwners[0] = "changed"
	if command.OwnerIDs()[0] != id {
		t.Fatal("getter can mutate identity")
	}
	cause, err := NewCommandsCause(command, command)
	if err != nil {
		t.Fatal(err)
	}
	details := cause.Details()
	details.Related[0] = CommandIdentity{}
	if cause.Details().Related[0].Validate() != nil {
		t.Fatal("getter can mutate cause")
	}
	for _, value := range []any{command, cause, struct{ private TransactionCause }{cause}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, value), "private-sentinel") {
				t.Fatal("cause leaked")
			}
		}
		b, err := json.Marshal(value)
		if err != nil || strings.Contains(string(b), "private-sentinel") {
			t.Fatal("cause JSON leaked")
		}
	}
	if _, err := NewCommandIdentity("project", []string{"bad"}, "task.complete", "key"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err := NewJobCause("job", id, "bad"); err == nil {
		t.Fatal("invalid attempt accepted")
	}
	if _, err := NewDeliveryCause(id, "handler\ninvalid"); err == nil {
		t.Fatal("invalid handler accepted")
	}
	if _, err := NewRecoveryCause("owner", id, "bad checkpoint"); err == nil {
		t.Fatal("invalid checkpoint accepted")
	}
	if _, err := NewCommandsCause(CommandIdentity{}); err == nil {
		t.Fatal("empty identity accepted")
	}
	job, _ := NewJobCause("index", id, id)
	delivery, _ := NewDeliveryCause(id, "outbox.index")
	recovery, _ := NewRecoveryCause("execution", id, "")
	if job.Kind() != JobCause || delivery.Kind() != DeliveryCause || recovery.Kind() != RecoveryCause || (TransactionCause{}).Validate() == nil {
		t.Fatal("cause discriminant")
	}
}
