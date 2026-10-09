package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestExplicitFixtureTargetPreservesDefaultAndRejectsIncompleteInput(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "owned.test")
	if err := os.WriteFile(binary, []byte("not executed by this pure test"), 0500); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"", "TestExistingSubset"} {
		value, err := selectedTestTarget("", "", filter)
		if err != nil || value != nil {
			t.Fatal("default multi-package invocation changed")
		}
	}
	for _, input := range [][3]string{
		{binary, "", "^TestExact$"}, {"", dir, "^TestExact$"},
		{"relative.test", dir, "^TestExact$"}, {binary, "relative", "^TestExact$"},
		{dir, dir, "^TestExact$"}, {binary, binary, "^TestExact$"},
		{filepath.Join(dir, "missing"), dir, "^TestExact$"},
		{binary, dir, ""}, {binary, dir, "TestExact"}, {binary, dir, "^[$"},
	} {
		if _, err := selectedTestTarget(input[0], input[1], input[2]); err == nil {
			t.Fatal("accepted incomplete or invalid explicit input")
		}
	}
}

func TestExplicitFixtureOwnedProjectionContainsOnlySevenIDsNoncesAndPaths(t *testing.T) {
	pg := pgfixture.Descriptor{ContainerID: "pg17", NetworkID: "pg-net", Nonce: "pg-nonce", Password: "password-private", User: "user-private", CAFile: "ca-private"}
	unsupported := pg
	unsupported.ContainerID = "pg16"
	object := objectfixture.Descriptor{ContainerID: "object", NetworkID: "object-net", Nonce: "object-nonce", Directory: "/owned/object", AccessKey: "access-private", SecretKey: "secret-private", CAFile: "object-ca-private"}
	outbound := netfixture.Descriptor{ContainerID: "outbound", NetworkID: "outbound-net", Nonce: "outbound-nonce", CAFile: "outbound-ca-private"}
	record := chainRecord("/owned/pg", pg, unsupported, object, outbound, "/owned/outbound")
	raw, err := json.Marshal(record)
	if err != nil || len(record.Resources) != 7 || len(record.Directories) != 3 || strings.Contains(string(raw), "private") {
		t.Fatal("ownership projection lost IDs or exposed private descriptor material")
	}
	seen := map[string]bool{}
	for _, resource := range record.Resources {
		if seen[resource.ID] || resource.Nonce == "" || resource.Label == "" || resource.Kind != "container" && resource.Kind != "network" {
			t.Fatal("ownership projection contains duplicate or incomplete identity")
		}
		seen[resource.ID] = true
	}
	if record.Resources[4].Nonce != record.Resources[5].Nonce || record.Resources[4].Nonce != record.Resources[6].Nonce {
		t.Fatal("PG17/PG16/network no longer share their actual owner nonce")
	}
}

func TestExplicitFixtureTargetUsesExactBinaryCWDAndOriginalSixMinutes(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "owned.test")
	if err := os.WriteFile(binary, []byte("not executed by this pure test"), 0500); err != nil {
		t.Fatal(err)
	}
	filter := "^TestWorkOwnerRootActual(Command|Reader)Join$"
	target, err := selectedTestTarget(binary, dir, filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []bool{false, true} {
		cmd := target.command(context.Background(), list)
		want := []string{binary, "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + filter}
		if list {
			want = []string{binary, "-test.list=" + filter}
		}
		if cmd.Path != binary || cmd.Dir != dir || !reflect.DeepEqual(cmd.Args, want) || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.Cancel == nil || cmd.WaitDelay != 3*time.Second {
			t.Fatal("explicit child changed binary, cwd, selector, timeout or owned wait/cancel")
		}
	}
	if !target.matchesListing("TestWorkOwnerRootActualCommandJoin\nTestWorkOwnerRootActualReaderJoin\n") {
		t.Fatal("actual matching tops not recognized")
	}
	for _, raw := range []string{"", "testing: warning: no tests to run\nPASS\n", "TestUnrelated\n", "TestWorkOwnerRootActualCommandJoinExtra\n", "=== RUN   TestWorkOwnerRootActualCommandJoin\n"} {
		if target.matchesListing(raw) {
			t.Fatal("nonmatching output could masquerade as an executed target")
		}
	}
}

func TestExplicitFixtureTargetDiscoversOnlyApprovedSubtestParent(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "owned.test")
	if err := os.WriteFile(binary, []byte("not executed by this pure test"), 0500); err != nil {
		t.Fatal(err)
	}
	parent := "^TestKnowledgeB02IndependentTreeReference$"
	child := "^revoked_persisted_public_receipt_identity_and_old_attachment$"
	filter := parent + "/" + child
	target, err := selectedTestTarget(binary, dir, filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []bool{false, true} {
		cmd := target.command(context.Background(), list)
		want := []string{binary, "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + filter}
		if list {
			want = []string{binary, "-test.list=" + parent}
		}
		if cmd.Path != binary || cmd.Dir != dir || !reflect.DeepEqual(cmd.Args, want) || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.Cancel == nil || cmd.WaitDelay != 3*time.Second {
			t.Fatal("parent discovery altered original execution, ownership or budget")
		}
	}
	if !target.matchesListing("TestKnowledgeB02IndependentTreeReference\n") {
		t.Fatal("exact parent was not discovered")
	}
	for _, raw := range []string{"", "PASS\n", "TestKnowledgeB02IndependentContent\n", "TestKnowledgeB02IndependentTreeReferenceExtra\n", "TestKnowledgeB02IndependentTreeReference/revoked_persisted_public_receipt_identity_and_old_attachment\n", "=== RUN   TestKnowledgeB02IndependentTreeReference\n"} {
		if target.matchesListing(raw) {
			t.Fatalf("non-parent output accepted: %q", raw)
		}
	}
	// No generic slash parsing: existing parent-only, union and unsupported
	// hierarchical regexp selectors keep their original discovery arguments.
	for _, unchanged := range []string{parent, "^TestKnowledgeB02Independent(Content|TreeReference)$", parent + "/^wrong_child$", parent + "/" + child + "/^extra$", "^TestExample[/]Name$", `^TestExample\/Name$`} {
		value, err := selectedTestTarget(binary, dir, unchanged)
		if err != nil {
			t.Fatal(err)
		}
		if got := value.command(context.Background(), true).Args; !reflect.DeepEqual(got, []string{binary, "-test.list=" + unchanged}) || value.filter != unchanged {
			t.Fatalf("unapproved discovery selector changed: %q", unchanged)
		}
	}
}
