package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRoundProjectionDefaultRepresentationsHideBorrowedContent(t *testing.T) {
	entry := RoundProjectionEntry{CanonicalMessage: []byte("private-projection-canary")}
	source := RoundProjectionSource{Arguments: []byte("private-projection-canary"), Transcript: []RoundProjectionEntry{entry}, ControlCanonical: []byte("private-projection-canary")}
	for _, v := range []any{source, entry} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{string(raw), fmt.Sprintf("%+v", v), fmt.Sprintf("%#v", v)} {
			if strings.Contains(text, "private-projection-canary") {
				t.Fatal("default representation exposed content")
			}
		}
	}
}
