package contract

import (
	"encoding/json"
	"testing"
)

func TestAccountServiceRegistrationIsClosedAndScoped(t *testing.T) {
	for _, name := range []ServiceName{AccountBootstrap, AccountAuth, AccountMaintenance, AccountMail} {
		r, e := RegisterService(name)
		if e != nil {
			t.Fatal(e)
		}
		a, e := r.Actor("01900000-0000-7000-8000-000000000001", SystemScope())
		if e != nil || a.Details().Kind != Service || a.Details().ServiceName != name || a.Details().UserID != "" {
			t.Fatal(e)
		}
		if json.Unmarshal([]byte(`{"kind":"human"}`), &a) == nil {
			t.Fatal("service manufactured human")
		}
	}
	if _, e := RegisterService("account"); e == nil {
		t.Fatal("unregistered generic account service")
	}
}
