package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// Both newly composed owners participate; cancellation alone is not retirement.
func TestIndependentRunnerVariablesCombinedRetirement(t *testing.T) {
	names := []string{"runner", "variables", "work", "project", "mail", "core"}
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			owners := make([]*b04Work, len(names))
			for n := range owners {
				owners[n] = &b04Work{}
			}
			a := &accountAssembly{runners: owners[0], variables: owners[1], planning: owners[2], projects: owners[3], mail: owners[4], core: owners[5]}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if force {
				cancel()
			}
			var order []string
			for n, w := range owners {
				call := func(actual context.Context) error {
					if actual != ctx {
						t.Fatal("changed original context")
					}
					for _, peer := range owners {
						if !peer.stopped.Load() {
							t.Fatal("waiting before all admission stopped")
						}
					}
					order = append(order, names[n])
					// A successful return from the first owner still does not prove Join.
					if n != 0 {
						w.joined.Store(true)
					}
					return nil
				}
				w.drain, w.force = call, call
			}
			if force {
				if e := a.Force(ctx); e != nil || !slices.Equal(order, names) {
					t.Fatal("force skipped owner after expired budget", order, e)
				}
			} else {
				if e := a.Drain(ctx); e == nil || !slices.Equal(order, names[:1]) {
					t.Fatal("drain retired providers before runner joined", order, e)
				}
			}
			if a.Joined() {
				t.Fatal("unjoined runner reported complete")
			}
			owners[0].joined.Store(true)
			order = nil
			if !force {
				if e := a.Drain(ctx); e != nil || !slices.Equal(order, names) {
					t.Fatal("missing combined owner/order", order, e)
				}
			}
			if !a.Joined() {
				t.Fatal("actual final joins not observed")
			}
			late := &b04Work{}
			late.force = func(actual context.Context) error {
				if actual != ctx {
					t.Fatal("late install changed Force context")
				}
				return context.Canceled
			}
			if force && a.install(context.Background(), func() { a.variables = late }) {
				t.Fatal("late install reopened admission")
			}
			if force && (!late.stopped.Load() || late.forced.Load() != 1 || a.Joined()) {
				t.Fatal("late Variables owner not force-owned")
			}
		})
	}
}

func TestIndependentRunnerVariablesRouteComposition(t *testing.T) {
	const project = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, want string }{
		{project + "/variables", "variables"}, {project + "/variables/01900000-0000-7000-8000-000000000002", "variables"},
		{project + "/variables/commands/lookup", "variables"}, {"/api/v1/system/runners", "runner-admin"},
		{"/api/v1/runner/enroll", "runner-device"}, {"/api/v1/runner/challenge", "runner-device"},
		{project + "/audit", "existing"}, {project + "/models", "existing"}, {project + "/variables/commands/other", "existing"},
		{"/api/v1/system/runners-other", "existing"}, {"/api/v1/runner/enroll/extra", "existing"},
	} {
		t.Run(tc.want+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path+"?untouched=true", nil)
			count := 0
			makeHandler := func(owner string) http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, actual *http.Request) {
					count++
					if actual != r || owner != tc.want {
						t.Errorf("wrong owner or request: %s", owner)
					}
				})
			}
			h := runnerControlRoutes(projectVariablesRoutes(makeHandler("existing"), makeHandler("variables")), makeHandler("runner-admin"), makeHandler("runner-device"))
			h.ServeHTTP(httptest.NewRecorder(), r)
			if count != 1 {
				t.Fatal("request was lost/duplicated", count)
			}
		})
	}
}
