package httpapi

import (
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"net/http/httptest"
	"testing"
)

func TestCommitUnknownExplicitReplayAndLegacyLookup(t *testing.T) {
	for _, v := range []struct{ hint, want string }{{"", "lookup"}, {"lookup", "lookup"}, {"replay", "replay"}, {"reread", "lookup"}, {"private-sentinel", "lookup"}} {
		fault := f.NewFault(f.CommitUnknown, f.Unknown)
		fault.RetryHint = v.hint
		w := httptest.NewRecorder()
		WriteProblem(w, httptest.NewRequest("POST", "/api/v1/system/models/probe/test", nil), fault)
		var p Problem
		if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
			t.Fatal(e)
		}
		if w.Code != 503 || p.CommitState != f.Unknown || p.RetryHint != v.want {
			t.Fatalf("hint %q: status=%d hint=%q", v.hint, w.Code, p.RetryHint)
		}
	}
}
