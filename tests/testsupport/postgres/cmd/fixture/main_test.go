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

func TestExplicitFixturePGCurrentOnlyRequiresExplicitTarget(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "owned.test")
	if err := os.WriteFile(binary, []byte("not executed by this pure test"), 0500); err != nil {
		t.Fatal(err)
	}
	target, err := selectedTestTarget(binary, dir, "^TestExact$")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []*fixtureTestTarget{nil, target} {
		if current, err := selectedPGCurrentOnly("", target); err != nil || current {
			t.Fatal("default full PG chain changed")
		}
		for _, value := range []string{"0", "true", " 1", "1 ", "01", "2"} {
			if current, err := selectedPGCurrentOnly(value, target); err == nil || current {
				t.Fatal("invalid PG mode was accepted")
			}
		}
	}
	if current, err := selectedPGCurrentOnly("1", nil); err == nil || current {
		t.Fatal("current-only weakened the default multi-package chain")
	}
	if current, err := selectedPGCurrentOnly("1", target); err != nil || !current {
		t.Fatal("explicit validated target could not select current-only PG")
	}
}

func TestExplicitFixturePGCurrentOnlyOmitsUnsupportedEnvironmentAndResources(t *testing.T) {
	base := []string{"KEEP=owned", pgfixture.UnsupportedEnv + "=/unrelated/first", pgfixture.UnsupportedEnv + "=/unrelated/second"}
	original := append([]string{}, base...)
	if env := unsupportedFixtureEnvironment(base, ""); !reflect.DeepEqual(env, []string{"KEEP=owned"}) {
		t.Fatal("current-only inherited or generated an unsupported descriptor environment")
	}
	if env := unsupportedFixtureEnvironment(base, "/owned/unsupported-fixture.json"); !reflect.DeepEqual(env, []string{"KEEP=owned", pgfixture.UnsupportedEnv + "=/owned/unsupported-fixture.json"}) {
		t.Fatal("full mode lost its actual unsupported descriptor")
	}
	if !reflect.DeepEqual(base, original) {
		t.Fatal("fixture environment preparation mutated its input")
	}
	t.Setenv(pgfixture.UnsupportedEnv, "")
	if descriptor, err := pgfixture.LoadUnsupported(); err == nil || descriptor != nil {
		t.Fatal("missing unsupported fixture was accepted")
	}
	pg := pgfixture.Descriptor{ContainerID: "pg17", NetworkID: "pg-net", Nonce: "pg-nonce", Password: "private-password"}
	object := objectfixture.Descriptor{ContainerID: "object", NetworkID: "object-net", Nonce: "object-nonce", Directory: "/owned/object"}
	outbound := netfixture.Descriptor{ContainerID: "outbound", NetworkID: "outbound-net", Nonce: "outbound-nonce"}
	record := chainRecord("/owned/pg", pg, pgfixture.Descriptor{}, object, outbound, "/owned/outbound")
	want := []ownedChainResource{
		{"container", "object", objectfixture.Label, "object-nonce"},
		{"network", "object-net", objectfixture.Label, "object-nonce"},
		{"container", "outbound", netfixture.Label, "outbound-nonce"},
		{"network", "outbound-net", netfixture.Label, "outbound-nonce"},
		{"container", "pg17", pgfixture.Label, "pg-nonce"},
		{"network", "pg-net", pgfixture.Label, "pg-nonce"},
	}
	raw, err := json.Marshal(record)
	if err != nil || !reflect.DeepEqual(record.Resources, want) || len(record.Directories) != 3 || strings.Contains(string(raw), "private") {
		t.Fatal("current-only record invented a resource or lost actual ownership")
	}
}

func TestExplicitFixtureSubtestListsTopAndRunsOriginalSelector(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "owned.test")
	if err := os.WriteFile(binary, []byte("not executed by this pure test"), 0500); err != nil {
		t.Fatal(err)
	}
	filter := "^TestProjectWorkCleanup$/^real-relations-upgrade-and-original-return$"
	target, err := selectedTestTarget(binary, dir, filter)
	if err != nil {
		t.Fatal(err)
	}
	listed := target.command(context.Background(), true)
	run := target.command(context.Background(), false)
	if !reflect.DeepEqual(listed.Args, []string{binary, "-test.list=^TestProjectWorkCleanup$"}) || run.Args[len(run.Args)-1] != "-test.run="+filter {
		t.Fatal("listing retained subtest or execution widened to whole top")
	}
	if !target.matchesListing("TestProjectWorkCleanup\n") || target.matchesListing("TestProjectWorkCleanupExtra\n") || target.matchesListing("TestOther\n") {
		t.Fatal("listing did not enforce the selected top")
	}
	for _, pattern := range []string{"^Test]?$", "^Test[^/]+$", "^Test[[:alpha:]/]+$", "^Test(One/Two|Exact)$", `^Test\/Exact$`} {
		if topTestFilter(pattern+"/^child$") != pattern {
			t.Fatal("regexp slash was mistaken for a subtest separator")
		}
	}
}
