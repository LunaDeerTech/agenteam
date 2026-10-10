package projectvariable

import (
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestVariableProducerBindsExactEventAndPlan(t *testing.T) {
	r, a := runtimeRecord(t)
	payload, _ := canonical(recordPayload(r))
	summary := event.Summary{Producer: c.VariableProducer, Header: recordHeader(r), PayloadDigest: digest(payload)}
	binding, locks, opaque, e := appendBinding(r, a, summary)
	if e != nil || len(locks) != 3 || len(opaque) == 0 {
		t.Fatal(e)
	}
	for _, lock := range locks {
		if lock.Mode != f.Exclusive {
			t.Fatal("mutation lock")
		}
	}
	issuer := oc.NewPlanIssuer()
	deps, e := oc.NewDependencies(issuer, binding, locks, opaque)
	if e != nil || !deps.Matches(issuer, binding) || deps.Matches(oc.NewPlanIssuer(), binding) {
		t.Fatal("issuer")
	}
	for _, change := range []func(*event.Summary){func(v *event.Summary) { v.Producer = "work" }, func(v *event.Summary) { v.Header.EventType = "project.changed" }, func(v *event.Summary) { v.Header.AggregateID = testID[event.Aggregate](90) }, func(v *event.Summary) { v.PayloadDigest = digest([]byte("wrong")) }, func(v *event.Summary) { version := f.Version(2); v.Header.AggregateVersion = &version }} {
		bad := summary
		change(&bad)
		if _, _, _, e = appendBinding(r, a, bad); e == nil {
			t.Fatal("wrong event")
		}
	}
	entry, key, e := recordAudit(r, a)
	if e != nil || entry.Fields().Resource.Details().ID != r.Target.String() || key.Details().Ordinal != 0 {
		t.Fatal("audit")
	}
	r.Input.Command = c.DeleteCommand
	if _, _, _, e = appendBinding(r, a, summary); e == nil {
		t.Fatal("original command")
	}
}
