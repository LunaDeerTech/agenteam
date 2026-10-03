//go:build integration

package security_test

import (
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestOutboundConcurrentPolicySavesSerialize(t *testing.T) {
	f := newAuditFixture(t)
	s := outboundPolicy(t, f, nil)
	ctx := auditContext(t)
	a, b := policyCommand(t, f.actor, 1), policyCommand(t, f.actor, 1)
	r1, r2 := policyRules(t, "10.11.0.0/16"), policyRules(t, "10.12.0.0/16")
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, e := s.UpdatePolicy(ctx, f.actor, a, r1); done <- e }()
	go func() { <-start; _, e := s.UpdatePolicy(ctx, f.actor, b, r2); done <- e }()
	close(start)
	success, conflict := 0, 0
	for range 2 {
		e := <-done
		if e == nil {
			success++
			continue
		}
		var f *foundation.Fault
		if errors.As(e, &f) && f.Code == foundation.VersionConflict {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 || *s.Status().Version != 2 {
		t.Fatal("save serialization failed")
	}
	var count int
	if e := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_outbound.outbound_policy_receipts`).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate persisted saves", e, count)
	}
}
