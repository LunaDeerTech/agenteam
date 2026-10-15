package pgfixture

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPGSearchFixtureImmutableIdentity(t *testing.T) {
	pin := "sha256:" + strings.Repeat("a", 64)
	other := "sha256:" + strings.Repeat("b", 64)
	if !ValidPGSearchImageID(pin) || !descriptorImage(PGSearchProfile, pin, pin) || !containerImage(PGSearchProfile, pin, pin, pin) {
		t.Fatal("exact run pin was rejected")
	}
	for _, value := range []string{"", "pg-search:latest", "repository@" + pin, strings.ToUpper(pin), pin + "\n", " " + pin, pin[:len(pin)-1], "sha256:" + strings.Repeat("g", 64)} {
		if ValidPGSearchImageID(value) || descriptorImage(PGSearchProfile, value, value) {
			t.Fatal("noncanonical image pin accepted")
		}
	}
	for _, parts := range [][3]string{{"", pin, pin}, {"other", pin, pin}, {PGSearchProfile, pin, ""}, {PGSearchProfile, pin, other}, {PGSearchProfile, Image, pin}, {"", Image, pin}} {
		if descriptorImage(parts[0], parts[1], parts[2]) {
			t.Fatal("descriptor substituted profile, image or original run pin")
		}
	}
	if containerImage(PGSearchProfile, pin, "tag:mutable", pin) || containerImage(PGSearchProfile, pin, pin, other) {
		t.Fatal("container configuration and actual immutable image diverged")
	}
}

func TestPGSearchFixtureDefaultDescriptorUnchanged(t *testing.T) {
	for _, base := range []string{Image, UnsupportedImage} {
		if !descriptorImage("", base, "") || !containerImage("", base, base, "sha256:local-base-image") {
			t.Fatal("original manifest-pinned default mode changed")
		}
		raw, err := json.Marshal(Descriptor{Image: base})
		if err != nil || strings.Contains(string(raw), `"retrieval"`) {
			t.Fatal("default descriptor changed its wire shape")
		}
	}
}
