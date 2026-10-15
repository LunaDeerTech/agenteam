package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestStoppedInvocationOwnerBindsOriginalIdentityAndRedacts(t *testing.T) {
	p, _ := f.ParseID[id.Project]("01900000-0000-7000-8000-000000000001")
	c, _ := f.ParseID[mc.Call]("01900000-0000-7000-8000-000000000002")
	process, _ := f.ParseID[oc.Process]("01900000-0000-7000-8000-000000000003")
	r := StoppedInvocationOwner{ProjectID: p, CallID: c, ProcessID: process}
	b, err := StoppedInvocationOwnerBinding(r)
	if err != nil || b.Validate() != nil {
		t.Fatal("valid original owner", err)
	}
	for _, mutate := range []func(*StoppedInvocationOwner){
		func(v *StoppedInvocationOwner) {
			v.ProjectID, _ = f.ParseID[id.Project]("01900000-0000-7000-8000-000000000004")
		},
		func(v *StoppedInvocationOwner) {
			v.CallID, _ = f.ParseID[mc.Call]("01900000-0000-7000-8000-000000000004")
		},
		func(v *StoppedInvocationOwner) {
			v.ProcessID, _ = f.ParseID[oc.Process]("01900000-0000-7000-8000-000000000004")
		},
	} {
		x := r
		mutate(&x)
		changed, err := StoppedInvocationOwnerBinding(x)
		if err != nil || changed == b {
			t.Fatal("foreign identity reused original binding", err)
		}
	}
	raw, _ := json.Marshal(r)
	for _, output := range []string{string(raw), fmt.Sprintf("%+v", r), r.LogValue().String()} {
		if strings.Contains(output, p.String()) || strings.Contains(output, c.String()) || strings.Contains(output, process.String()) {
			t.Fatal("owner escaped redaction")
		}
	}
	if json.Unmarshal([]byte(`{}`), &r) == nil || (StoppedInvocationOwner{}).Validate() == nil {
		t.Fatal("wire or zero owner became recovery input")
	}
}
