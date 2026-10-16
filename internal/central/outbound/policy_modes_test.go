package outbound

import (
	"context"
	"encoding/binary"
	"net/netip"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestPolicyModeIsSnapshotAndCommandState(t *testing.T) {
	rules, _ := NewRules()
	st := &policyState{mirror: policy(1, Denylist, rules), available: true, proxyAvailable: true}
	s := &PolicyService{data: func() *policyState { return st }}
	if err := s.publish(policy(1, Allowlist, rules)); err == nil || s.Status().Available {
		t.Fatal("same-version mode flip was accepted or left admission open")
	}
	if err := s.publish(policy(2, RuleMode("invalid"), rules)); err == nil || s.Status().Available {
		t.Fatal("invalid mode became a default")
	}
	if _, err := s.snapshot(); err == nil {
		t.Fatal("invalid snapshot admitted traffic")
	}
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	actor, _ := identity.NewHuman(user, session)
	id, _ := foundation.NewCommandIdentity("outbound-policy-v2", []string{user.String()}, "update", "mode-intent")
	meta := CommandMeta{Identity: id, ExpectedVersion: 1}
	if commandDigest(actor, meta, Allowlist, rules) == commandDigest(actor, meta, Denylist, rules) {
		t.Fatal("mode omitted from semantic digest")
	}
	if _, err := s.UpdatePolicy(context.Background(), actor, meta, RuleMode(""), rules); err == nil {
		t.Fatal("missing mode reached storage")
	}
}

func TestVisibleDefaultRulesMatchPreviousAddressSetAndCanBeRemoved(t *testing.T) {
	defaults := DefaultRules()
	if defaults.Count() != 32 {
		t.Fatal("visible default list changed unexpectedly")
	}
	empty, _ := NewRules()
	check := func(ip netip.Addr) {
		t.Helper()
		class := Classify(ip)
		wantDenied := class != Public && class != Private
		got := defaults.check(Denylist, "sample.example", []netip.Addr{ip}, 443)
		if (got == ac.RuleNotAllowed) != wantDenied {
			t.Fatal("explicit defaults differ from prior address set", ip, class, got)
		}
		if empty.check(Denylist, "sample.example", []netip.Addr{ip}, 443) != "" {
			t.Fatal("removed default remained a hidden address restriction", ip)
		}
	}
	for _, text := range []string{"0.0.0.0", "127.0.0.1", "169.254.169.254", "100.100.100.200", "168.63.129.16", "10.1.2.3", "8.8.8.8", "224.0.0.1", "255.255.255.255", "::", "::1", "fd00:ec2::254", "fc00::1", "fe80::1", "2001:db8::1", "2001:4860::1", "::ffff:127.0.0.1"} {
		check(netip.MustParseAddr(text))
	}
	// Stable samples cover both address spaces, in addition to explicit edges.
	var seed uint64 = 0x123456789abcdef
	for range 2048 {
		seed = seed*6364136223846793005 + 1442695040888963407
		var v4 [4]byte
		binary.BigEndian.PutUint32(v4[:], uint32(seed>>32))
		check(netip.AddrFrom4(v4))
		var v6 [16]byte
		binary.BigEndian.PutUint64(v6[:8], seed)
		binary.BigEndian.PutUint64(v6[8:], seed^0xabcd)
		check(netip.AddrFrom16(v6))
	}
	for _, host := range []string{"metadata.google.internal", "metadata.goog"} {
		ips := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
		if defaults.check(Denylist, host, ips, 443) != ac.RuleNotAllowed || empty.check(Denylist, host, ips, 443) != "" {
			t.Fatal("metadata name is not an ordinary removable rule")
		}
	}
}
