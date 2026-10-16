package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestCommittedProjectionErrorCopiesFaultAndPreservesPrivateCause(t *testing.T) {
	private := errors.New("private projection diagnostic")
	for _, code := range []foundation.Code{foundation.DependencyUnavailable, foundation.NotFound, foundation.Forbidden, foundation.SessionRevoked, foundation.CommitUnknown} {
		t.Run(string(code), func(t *testing.T) {
			original := foundation.NewFault(code, foundation.NotCommitted).WithCause(private)
			original.RetryHint = "reread"
			original.FieldErrors = []foundation.FieldError{{Path: "/version", Code: "STALE"}}
			before, _ := json.Marshal(original)
			err := committedProjectionError(fmt.Errorf("private wrapper: %w", original))
			var got *foundation.Fault
			if !errors.As(err, &got) || got == original || got.CommitState != foundation.Committed || got.RetryHint != "replay" || !errors.Is(err, private) {
				t.Fatal("lost confirmed command outcome or diagnostic cause")
			}
			want := code
			if code == foundation.CommitUnknown {
				want = foundation.DependencyUnavailable
			}
			if got.Code != want {
				t.Fatal("wrong safe projection code", got.Code)
			}
			if len(got.FieldErrors) > 0 {
				got.FieldErrors[0].Code = "CHANGED"
			}
			after, _ := json.Marshal(original)
			if string(before) != string(after) {
				t.Fatal("shared Fault mutated")
			}
			w := httptest.NewRecorder()
			httpapi.WriteProblem(w, httptest.NewRequest("POST", "/", nil), err)
			var problem httpapi.Problem
			if e := json.Unmarshal(w.Body.Bytes(), &problem); e != nil || problem.CommitState != foundation.Committed || problem.RetryHint != "replay" || problem.Code != want {
				t.Fatal("wire lost committed replay classification", e)
			}
			if strings.Contains(w.Body.String()+fmt.Sprintf("%+v", err), "private") {
				t.Fatal("private cause leaked")
			}
		})
	}
	if committedProjectionError(nil) != nil {
		t.Fatal("successful projection became an error")
	}
	var raw *foundation.Fault
	if !errors.As(committedProjectionError(private), &raw) || raw.Code != foundation.DependencyUnavailable || raw.CommitState != foundation.Committed || !errors.Is(raw, private) {
		t.Fatal("raw read error lost confirmed command outcome")
	}
}
