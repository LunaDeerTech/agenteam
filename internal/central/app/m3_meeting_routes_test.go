package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMeetingRoutesIncludeDeletionAndOriginalLookup(t *testing.T) {
	const base = "/api/v1/projects/01900000-0000-7000-8000-000000000001/meetings/01900000-0000-7000-8000-000000000002"
	for _, tc := range []struct {
		path string
		want string
	}{
		{base + "/deletion", "deletion"},
		{base + "/deletion/lookup", "deletion"},
		{base, "commands"},
		{base + "/turns", "commands"},
		{base + "/controls/lookup", "commands"},
		{base + "/deletion/other", "previous"},
		{base + "/deletion/lookup/extra", "previous"},
		{base + "/deletion/", "previous"},
		{"/api/v1/system/meetings/deletion", "previous"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			calls := 0
			reached := ""
			handler := func(name string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls++
					reached = name
					w.WriteHeader(http.StatusNoContent)
				})
			}
			router := meetingRoutes(handler("previous"), meetingDeletionRoutes(handler("commands"), handler("deletion")))
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tc.path, nil))
			if calls != 1 || reached != tc.want {
				t.Fatalf("route reached %q (%d calls), want %q once", reached, calls, tc.want)
			}
		})
	}
}
