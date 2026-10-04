package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutboxRegisteredDutyIsAnIdentityOnly(t *testing.T) {
	r, err := RegisterService(OutboxDelivery)
	if err != nil {
		t.Fatal(err)
	}
	a, err := r.Actor("sha256:"+strings.Repeat("a", 64), SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	d := a.Details()
	if d.Kind != Service || d.ServiceName != OutboxDelivery || d.UserID != "" || d.SessionID != "" {
		t.Fatal("service impersonates human")
	}
	b, err := json.Marshal(a)
	if err != nil || string(b) != `"trusted_actor"` {
		t.Fatal("unsafe actor projection")
	}
	if _, err = r.Actor("free-form-claim", SystemScope()); err == nil {
		t.Fatal("unbound cause accepted")
	}
	if _, err = RegisterService("outbox-admin"); err == nil {
		t.Fatal("unregistered duty accepted")
	}
}
