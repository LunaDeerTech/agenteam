package prompt_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/prompt"
)

func TestPlatformPromptVersionedContent(t *testing.T) {
	p := prompt.Current()
	const wantDigest = "sha256:b82e9c4e42f60458386524d5ac4e2e4621e34665de2b7dd1d746342d1640138d"
	if p.Validate() != nil || p.Revision() != "agenteam/platform-prompt/v1" || string(p.Digest()) != wantDigest || p.Digest() != ec.TriggerInputDigest([]byte(p.Content())) {
		t.Fatal("platform prompt version/content changed without explicit vector update")
	}
	// These are platform responsibilities, not declarations that the respective
	// tools are present in this execution or that a Context is already built.
	for _, text := range []string{"Knowledge Retrieval if it is available", "Agent Memory recall if it is available", "use retain", "Never store secrets", "reflect if it is available", "does not automatically retain", "do not assume"} {
		if !strings.Contains(p.Content(), text) {
			t.Fatal("platform instruction missing")
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	safe := fmt.Sprintf("%v %+v %#v", p, p, p) + string(b) + slog.AnyValue(p).Resolve().String()
	if strings.Contains(safe, "Knowledge Retrieval") || strings.Contains(safe, p.Revision()) {
		t.Fatal("prompt default output was not safe")
	}
	for _, pair := range [][2]string{{"", "text"}, {"revision with space", "text"}, {"v1", ""}, {"v1", " \n"}, {"v1", "x\x00y"}, {"v1", string([]byte{0xff})}, {"v1", strings.Repeat("x", ec.MaxPlatformPromptBytes+1)}} {
		if _, err := ec.NewPlatformPrompt(pair[0], pair[1]); err == nil {
			t.Fatal("invalid prompt accepted")
		}
	}
	if next := prompt.Current(); next.Digest() != p.Digest() || next.Content() != p.Content() {
		t.Fatal("prompt is not stable")
	}
}
