package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestReferenceIdentityNormalizationPreservesResourceSemantics(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"HTTPS://Example.COM:443", "https://example.com/"},
		{"http://Example.COM:80/a%2Fb?q=1&q=2#Section", "http://example.com/a%2Fb?q=1&q=2#Section"},
		{"https://Example.COM:8443/?token=public#anchor", "https://example.com:8443/?token=public#anchor"},
		{"https://[2001:DB8::1]:443", "https://[2001:db8::1]/"},
	} {
		got, err := CanonicalReference(Reference{Kind: "link", ID: tc.input, Label: "label"})
		if err != nil || got.ID != tc.want || got.Label != "label" {
			t.Fatalf("canonical link %q: %q %v", tc.input, got.ID, err)
		}
	}
	for _, input := range []string{"javascript:alert(1)", "data:text/plain,a", "file:///tmp/a", "https://user:password@example.com/", "//example.com/a", " https://example.com", "https://example.com:0/", "https://example.com:65536/", "https://example.com/\n"} {
		if _, err := CanonicalReference(Reference{Kind: "link", ID: input}); err == nil {
			t.Fatalf("unsafe link accepted: %q", input)
		}
	}
	id := fresh[struct{}](t).String()
	for _, kind := range []string{"task", "knowledge", "file"} {
		if got, err := CanonicalReference(Reference{Kind: kind, ID: id}); err != nil || got.ID != id {
			t.Fatal(kind, err)
		}
		if _, err := CanonicalReference(Reference{Kind: kind, ID: "display label"}); err == nil {
			t.Fatal("display text accepted as identity", kind)
		}
	}
}

func TestReferencesCanonicalizeBeforeDeduplicationWithoutMutatingRequest(t *testing.T) {
	r := UpdateReferencesRequest{ProjectID: fresh[i.Project](t), MeetingID: fresh[Meeting](t), ExpectedVersion: 1, References: []Reference{{Kind: "link", ID: "HTTPS://Example.com:443"}}, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "references-1"}}
	got, err := r.Canonical()
	if err != nil || got.References[0].ID != "https://example.com/" || r.References[0].ID != "HTTPS://Example.com:443" {
		t.Fatal("canonical copy", err)
	}
	r.References = append(r.References, Reference{Kind: "link", ID: "https://example.com/", Label: "another label"})
	if r.Validate() == nil {
		t.Fatal("duplicate canonical identity")
	}
	r.References = nil
	if r.Validate() == nil {
		t.Fatal("omitted references")
	}
	r.References = []Reference{}
	if err := r.Validate(); err != nil {
		t.Fatal("explicit unpin all", err)
	}
}
