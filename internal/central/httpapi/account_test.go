package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountChallengeProblemsKeepSafeWireCodes(t *testing.T) {
	for code, status := range map[foundation.Code]int{foundation.ChallengeRequired: http.StatusUnauthorized, foundation.ChallengeInvalid: http.StatusBadRequest} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login?password=private-input", nil)
		Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, foundation.NewFault(code, foundation.NotCommitted).WithCause(errors.New("private-input")))
		})).ServeHTTP(w, r)
		var p Problem
		if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
			t.Fatal(e)
		}
		if w.Code != status || p.Code != code || p.CommitState != foundation.NotCommitted || !code.Known() || strings.Contains(w.Body.String(), "private-input") {
			t.Fatal("unsafe challenge mapping")
		}
	}
}
