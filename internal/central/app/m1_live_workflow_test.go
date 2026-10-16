//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

const (
	m1LiveTotalBudget   = 300 * time.Second
	m1LiveJoinBudget    = 30 * time.Second
	m1LiveMaxOutput     = 4096
	m1LiveSentThreshold = 12
	m1LiveOptIn         = "external-model-read-write-run-review"
	m1LiveSentQuery     = `SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND dispatch='sent'`
	m1LiveTaskPlan      = "Use only the supplied Runner mount and Python standard library; do not access the network or paths outside that mount. Read input.json, which contains a values array. Create stats.py that reads that array and calculates its count, sum and arithmetic mean. The program must write those three numeric fields as a JSON object to result.json and print the same JSON object to stdout. Use the run command tool in argv mode with program /usr/bin/python3, args [stats.py], and cwd . in the supplied workspace, inspect the real result.json and command exit status, and report the actual outcome. Use relative file paths and the captured mount identity. Generate the program yourself; do not replace tool execution with a proposed answer."
)

// This configuration contains metadata only. No credential is retained in it.
type m1LiveConfiguration struct {
	BaseURL, Model string
	qualified      bool
	proxyRoute     bool
	deadline       time.Time
}

func m1LiveMetadata(selection, optIn, base, model string) (*m1LiveConfiguration, error) {
	if selection != "live" || optIn != m1LiveOptIn {
		return nil, errors.New("M1 live requires explicit case and opt-in")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/chat/completions") {
		return nil, errors.New("M1 live endpoint metadata invalid")
	}
	target, err := outbound.ParseTarget(base)
	if err != nil || target.Origin().Scheme() != "https" {
		return nil, errors.New("M1 live target rejected")
	}
	if _, err := netip.ParseAddr(target.Origin().Host()); err == nil {
		return nil, errors.New("M1 live requires a DNS hostname")
	}
	if !utf8.ValidString(model) || len(model) == 0 || len(model) > 256 || strings.TrimSpace(model) != model || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return nil, errors.New("M1 live model metadata invalid")
	}
	return &m1LiveConfiguration{BaseURL: base, Model: model}, nil
}

func (c *m1LiveConfiguration) preflight(ctx context.Context, resolver outbound.Resolver) error {
	if c == nil || resolver == nil {
		return errors.New("M1 live preflight dependency missing")
	}
	c.qualified = false
	c.proxyRoute = false
	target, err := outbound.ParseTarget(c.BaseURL)
	if err != nil {
		return errors.New("M1 live target rejected")
	}
	addresses, err := resolver.Lookup(ctx, target.Origin().Host())
	if err != nil || len(addresses) == 0 {
		return errors.New("M1 live original D04 DNS preflight blocked")
	}
	for _, address := range addresses {
		if outbound.Classify(address) != outbound.Public {
			return errors.New("M1 live preflight requires public addresses under unchanged policy")
		}
	}
	c.qualified = true
	return nil
}

// Only the explicitly selected live test reaches this function, after the
// configured route preflight. Runner/browser children never inherit it.
func (c *m1LiveConfiguration) credential(t *testing.T) []byte {
	t.Helper()
	if c == nil || !c.qualified || c.deadline.IsZero() || !time.Now().Before(c.deadline) || os.Getenv("AGENTEAM_M1_CASE") != "live" || os.Getenv("AGENTEAM_M1_LIVE_OPT_IN") != m1LiveOptIn {
		t.Fatal("M1 live credential access before explicit qualified run")
	}
	raw := []byte(os.Getenv("DEV_CHAT_API_KEY"))
	if len(raw) == 0 {
		t.Fatal("M1 live configured credential absent")
	}
	return raw
}

func TestM1AppLiveWorkflow(t *testing.T) {
	if os.Getenv("AGENTEAM_M1_CASE") != "live" && os.Getenv("AGENTEAM_M1_LIVE_OPT_IN") == "" {
		t.Skip("explicit M1 live case and opt-in required; no external configuration inspected")
	}
	started := time.Now()
	configuration, err := m1LiveMetadata(os.Getenv("AGENTEAM_M1_CASE"), os.Getenv("AGENTEAM_M1_LIVE_OPT_IN"), os.Getenv("DEV_CHAT_BASE_URL"), os.Getenv("DEV_CHAT_MODEL"))
	workAppCheck(t, "m1-live-metadata", err)
	configuration.deadline = started.Add(m1LiveTotalBudget)
	resolver, err := outbound.SystemResolver()
	workAppCheck(t, "m1-live-system-resolver", err)
	preflight, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = configuration.preflight(preflight, resolver)
	cancel()
	workAppCheck(t, "m1-live-route-preflight", err)
	t.Logf("M1_LIVE_ROUTE explicit_origin=true trusted_proxy=%t tls_verification=true", configuration.proxyRoute)
	w, listener := newM1WorkflowConfigured(t, nil, true, configuration)
	w.seed(t)
	w.startRunner(t)
	budget := w.startLiveBudget(t, configuration.deadline)
	w.createModelAndTask(t, configuration.BaseURL)
	w.app.store.releaseDirectory()
	w.awaitLiveResult(t)
	w.checkLiveArtifacts(t)
	w.browserWithExpectation(t, listener, "live")
	w.app.runtime.StopAdmission()
	drain, cancelDrain := context.WithTimeout(context.Background(), m1LiveJoinBudget)
	err = w.app.runtime.Drain(drain)
	cancelDrain()
	workAppCheck(t, "m1-live-original-runtime-drain", err)
	budget.finish(t)
	w.checkLiveFinalFacts(t)
	w.stopRunner(t)
	v := w.app
	v.requestStop()
	usageAppWait(t, v.directory.returned, "m1-live-manager-original-return")
	usageAppWait(t, v.root.done, "m1-live-original-app-return")
	workAppCheck(t, "m1-live-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("M1 live App returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	t.Log("M1_LIVE_WORKFLOW actual_external_model=true dynamic_tool_choices=true actual_read_write_run=true durable_final_v2=true owner_review=true original_app_runner_wait=true review_decision=false")
}

// The observer threshold is a soft stop across work/review/retry attempts. It
// can observe already in-flight calls; it is not a hard request or billing cap.
type m1LiveBudget struct {
	stop, done chan struct{}
	once       sync.Once
	reason     string // written only by owner goroutine, read only after done
}

func (w *m1Workflow) startLiveBudget(t *testing.T, deadline time.Time) *m1LiveBudget {
	t.Helper()
	connection := w.app.root.db.Connect(t) // separate pgx connection; never share admin concurrently
	budget := &m1LiveBudget{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		w.runLiveBudget(budget, timer.C, tick.C, func(ctx context.Context) (int, error) {
			var sent int
			err := connection.QueryRow(ctx, m1LiveSentQuery, w.app.a.String()).Scan(&sent)
			return sent, err
		})
	}()
	// Runs before the earlier Runner and App cleanup registrations. Even an
	// assertion failure first stops the original Runtime, while its ports live.
	t.Cleanup(func() {
		budget.once.Do(func() { close(budget.stop) })
		<-budget.done
		if budget.reason != "" {
			t.Errorf("M1 live incomplete: %s", budget.reason)
			return // original watchdog already drained/cancelled the root
		}
		w.app.runtime.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), m1LiveJoinBudget)
		err := w.app.runtime.Drain(ctx)
		cancel()
		if err != nil {
			w.app.requestStop()
			w.app.root.cancel()
			t.Error("M1 live original Runtime drain incomplete")
		}
	})
	return budget
}

func m1ObserveLiveBudget(stop <-chan struct{}, deadline, tick <-chan time.Time, count func(context.Context) (int, error)) string {
	for {
		select {
		case <-stop:
			return ""
		case <-deadline:
			return "total_deadline"
		case <-tick:
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			sent, err := count(ctx)
			cancel()
			if err != nil {
				return "budget_observation_failed"
			}
			if sent >= m1LiveSentThreshold {
				return "sent_stop_threshold"
			}
		}
	}
}

func (w *m1Workflow) runLiveBudget(budget *m1LiveBudget, deadline, tick <-chan time.Time, count func(context.Context) (int, error)) {
	defer close(budget.done)
	budget.reason = m1ObserveLiveBudget(budget.stop, deadline, tick, count)
	if budget.reason == "" {
		return
	}
	w.app.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), m1LiveJoinBudget)
	_ = w.app.runtime.Drain(drain)
	cancel()
	w.app.requestStop()
	w.app.root.cancel()
}
func (b *m1LiveBudget) finish(t *testing.T) {
	t.Helper()
	b.once.Do(func() { close(b.stop) })
	<-b.done
	if b.reason != "" {
		t.Fatalf("M1 live incomplete: %s", b.reason)
	}
}

// These preparation checks use fake metadata and a local resolver function;
// they neither inspect the process DEV_CHAT values nor create an App or HTTP.
type m1LiveResolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f m1LiveResolverFunc) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}
func TestM1LivePreparation(t *testing.T) {
	for _, v := range []struct{ name, selection, optIn, base, model string }{
		{"default_case", "workflow", m1LiveOptIn, "https://model.example/v1", "chat"},
		{"missing_opt_in", "live", "", "https://model.example/v1", "chat"},
		{"plain_http", "live", m1LiveOptIn, "http://model.example/v1", "chat"},
		{"userinfo", "live", m1LiveOptIn, "https://user@model.example/v1", "chat"},
		{"query", "live", m1LiveOptIn, "https://model.example/v1?key=fake", "chat"},
		{"wrong_path", "live", m1LiveOptIn, "https://model.example/v1/chat/completions", "chat"},
		{"numeric_host", "live", m1LiveOptIn, "https://8.8.8.8/v1", "chat"},
		{"invalid_model", "live", m1LiveOptIn, "https://model.example/v1", "chat\n"},
	} {
		t.Run(v.name, func(t *testing.T) {
			if _, err := m1LiveMetadata(v.selection, v.optIn, v.base, v.model); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
	t.Run("formal_max_output", func(t *testing.T) {
		raw, err := json.Marshal(struct {
			MaxOutput mc.TokenCount `json:"max_output"`
		}{mc.TokenCount(m1LiveMaxOutput)})
		if err != nil {
			t.Fatal("max output fixture encoding failed")
		}
		var decoded struct {
			MaxOutput *mc.TokenCount `json:"max_output"`
		}
		if json.Unmarshal(raw, &decoded) != nil || decoded.MaxOutput == nil || *decoded.MaxOutput != mc.TokenCount(4096) || json.Unmarshal([]byte(`{"max_output":4096}`), &decoded) == nil {
			t.Fatal("live max output must use the formal decimal-string DTO")
		}
	})

	for _, v := range []struct {
		name      string
		addresses []netip.Addr
		err       error
		want      bool
	}{
		{"complete_public", []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")}, nil, true},
		{"family_failure", nil, errors.New("dns failure"), false},
		{"mixed_private", []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}, nil, false},
		{"empty", nil, nil, false},
	} {
		t.Run(v.name, func(t *testing.T) {
			c, err := m1LiveMetadata("live", m1LiveOptIn, "https://model.example/v1", "chat")
			if err != nil {
				t.Fatal(err)
			}
			err = c.preflight(context.Background(), m1LiveResolverFunc(func(context.Context, string) ([]netip.Addr, error) { return v.addresses, v.err }))
			if (err == nil) != v.want || c.qualified != v.want {
				t.Fatal("preflight qualification differs")
			}
		})
	}
}
