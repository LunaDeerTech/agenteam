package main

import (
	"strings"
	"testing"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestPGSearchFixtureRequiresExplicitOwnedTarget(t *testing.T) {
	pin := "sha256:" + strings.Repeat("a", 64)
	target := &fixtureTestTarget{}
	for _, original := range []*fixtureTestTarget{nil, target} {
		for _, current := range []bool{false, true} {
			image, profile, err := selectedPGSearchImage("", current, original)
			if err != nil || image != pgfixture.Image || profile != "" {
				t.Fatal("default PG fixture mode changed")
			}
			image, profile, err = selectedPGSearchImage(pin, current, original)
			allowed := original != nil && current
			if allowed && (err != nil || image != pin || profile != pgfixture.PGSearchProfile) || !allowed && (err == nil || image != "" || profile != "") {
				t.Fatal("pg_search selection bypassed explicit target/current-only")
			}
		}
	}
	for _, value := range []string{"tag:mutable", "repository@" + pin, pin + " ", strings.ToUpper(pin)} {
		if _, _, err := selectedPGSearchImage(value, true, target); err == nil {
			t.Fatal("non-immutable image accepted before resource creation")
		}
	}
}
