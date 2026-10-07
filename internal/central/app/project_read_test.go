package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

func TestProjectReadRootPureConstruction(t *testing.T) {
	cfg := testConfig(t, "1s")
	store := &projectUsageRootStore{}
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	usage, err := createProjectUsage(cfg, store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := createProjectRead(cfg, store, usage.projects)
	if err != nil || reader == nil {
		t.Fatal("pure shared Authority reader", err)
	}
	var typedNil *projectUsageRootStore
	for _, db := range []database{nil, typedNil, &unitDatabase{}, &projectUsageRootStore{}} {
		if got, e := createProjectRead(cfg, db, usage.projects); e == nil || got != nil {
			t.Fatal("unbound/foreign Store accepted")
		}
	}
	for _, auth := range []*project.Authority{nil, {}} {
		if got, e := createProjectRead(cfg, store, auth); e == nil || got != nil {
			t.Fatal("unbound authority accepted")
		}
	}
	if got, e := projectReadHandler(reader, nil, cfg.PublicOrigin()); e == nil || got != nil {
		t.Fatal("formal Account core missing")
	}
}
func TestProjectReadRootPureRouteOwnership(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, owner string }{
		{"/api/v1/projects", "read"}, {"/api/v1/projects/" + id, "read"}, {"/api/v1/projects/bad-id", "read"},
		{"/api/v1/projects/resolve", "usage"}, {"/api/v1/projects/" + id + "/model-usage", "usage"}, {"/api/v1/projects/" + id + "/model-usage/summary", "usage"},
		{"/api/v1/projects/", "old"}, {"/api/v1/projects/resolve/", "old"}, {"/api/v1/projects/" + id + "/archive", "old"}, {"/api/v1/projects-other", "old"},
		{"/api/v1/session", "old"}, {"/api/v1/system/meeting-summary-selection", "old"}, {"/api/v1/system/models", "old"}, {"/readyz", "old"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest("POST", tc.path+"?cursor=private", strings.NewReader("owned"))
			originalURL, originalBody := request.URL, request.Body
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if owner != tc.owner || r != request || r.URL != originalURL || r.Body != originalBody {
						t.Fatal("wrong owner or altered request")
					}
					w.WriteHeader(204)
				})
			}
			h := projectReadRoutes(projectUsageRoutes(child("old"), child("usage")), child("read"))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request)
			if calls != 1 || w.Code != 204 {
				t.Fatal("dispatch duplicated or dropped request")
			}
		})
	}
}
