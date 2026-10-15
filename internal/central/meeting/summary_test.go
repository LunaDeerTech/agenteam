package meeting

import (
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func TestSummaryRequiresFourStringsAndFirstTitle(t *testing.T) {
	valid := `{"title":"A decision","goals":"","decisions":"Use A","unresolved":"","facts":"Measured input"}`
	got, err := ParseSummary([]byte(valid), true)
	if err != nil || got.Title == nil || got.Decisions != "Use A" {
		t.Fatal(got, err)
	}
	for _, invalid := range []string{
		`{"goals":"","decisions":"","unresolved":"","facts":""}`,
		`{"title":"x","goals":[],"decisions":"","unresolved":"","facts":""}`,
		`{"title":"x","goals":null,"decisions":"","unresolved":"","facts":""}`,
		`{"title":"x","goals":"","decisions":"","unresolved":"","facts":"","extra":true}`,
		`{"title":" ","goals":"","decisions":"","unresolved":"","facts":""}`,
	} {
		if _, err := ParseSummary([]byte(invalid), true); err == nil {
			t.Fatal("invalid summary accepted", invalid)
		}
	}
	if _, err := ParseSummary([]byte(valid), false); err == nil {
		t.Fatal("later generation attempted title rewrite")
	}
}
func TestSummaryMessagesExcludePreviousSummaryAndRuntimeFacts(t *testing.T) {
	input := SummaryInput{Initial: true, Messages: []FrozenMessage{{Message: c.MessageView{Content: []c.InlineNode{{Type: "text", Text: "A verified user statement"}}}}}}
	// Invalid identities do not appear in the model payload; the production
	// caller separately verifies source identity and digest in its transaction.
	messages, format, err := summaryMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if format.Kind != "text" || format.Name != "" || len(format.Schema) != 0 || len(messages) != 2 || !strings.Contains(messages[1].Parts[0].Text.Text, "verified user statement") {
		t.Fatal("missing exact message input")
	}
	for _, value := range []string{"turn_id", "generation", "attempt", "rolling_summary", "execution_id", "approval"} {
		if strings.Contains(messages[1].Parts[0].Text.Text, value) {
			t.Fatal("control input leaked", value)
		}
	}
}

func TestSummaryRejectsDuplicateFacts(t *testing.T) {
	if _, err := ParseSummary([]byte(`{"goals":"first","goals":"second","decisions":"","unresolved":"","facts":""}`), false); err == nil {
		t.Fatal("duplicate summary facts accepted")
	}
}
