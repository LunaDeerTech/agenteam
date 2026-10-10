//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

// Reuse the accepted owned root transport/logger/actual stop helpers. Every run
// below calls the public app.Run with nil hooks and no injected handler or extra
// Summary initializer. Account identity comes from formal Bootstrap + Login.
func startMeetingSummarySettingsRoot(t *testing.T, v *systemHTTPFixture, failure bool) *projectUsageRoot {
	t.Helper()
	objects, e := objectfixture.Environment(testContext(t), v.db.Name)
	if e != nil {
		t.Fatal("owned object environment unavailable", e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	values := map[string]string{}
	for _, entry := range objects {
		k, value, _ := strings.Cut(entry, "=")
		values[k] = value
	}
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	for k, value := range map[string]string{
		"DATABASE_URL": v.db.Fixture.URL(v.db.Name), "DATABASE_CA_FILE": v.db.Fixture.CAFile, "DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": systemHTTPOrigin, "SHUTDOWN_TIMEOUT": "3s",
		"ACCOUNT_RECOVERY_LOG":           filepath.Join(dir, "root-recovery.jsonl"),
		"CURSOR_KEYRING":                 fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)),
		"SECRET_KEYRING":                 fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)),
		"OBJECT_DOWNLOAD_KEYRING":        fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)),
		"ACCOUNT_KEYRING":                fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)),
		"KNOWLEDGE_CONFIRMATION_KEYRING": fmt.Sprintf(`{"format":1,"current_kid":"k","keys":[{"kid":"k","key_b64":%q}]}`, b64(5)),
	} {
		values[config.Prefix+k] = value
	}
	var environment []string
	for k, value := range values {
		environment = append(environment, k+"="+value)
	}
	cfg, e := config.Load(func(k string) (string, bool) { value, ok := values[k]; return value, ok }, environment)
	if e != nil {
		t.Fatal("owned root config", e)
	}
	root := &projectUsageRoot{done: make(chan struct{}), logs: &projectUsageRootLog{listening: make(chan string, 1)}}
	logger, e := logging.New(logging.Central, slog.LevelInfo, root.logs)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	root.cancel = cancel
	go func() { root.err = app.Run(ctx, cfg, logger, nil); close(root.done) }()
	if failure {
		t.Cleanup(func() { cancel(); <-root.done })
		select {
		case <-root.done:
		case <-root.logs.listening:
			cancel()
			<-root.done
			t.Fatal("failed Summary startup published listener")
		case <-time.After(20 * time.Second):
			cancel()
			<-root.done
			t.Fatal("startup failure exceeded original budget")
		}
		if root.err == nil || strings.Contains(root.logs.String(), `"event":"listening"`) {
			t.Fatal("bad Summary storage did not fail default initialization")
		}
		return root
	}
	t.Cleanup(func() { root.stop(t) })
	select {
	case root.address = <-root.logs.listening:
	case <-root.done:
		t.Fatal("default root initialization failed", root.err)
	case <-time.After(20 * time.Second):
		cancel()
		<-root.done
		t.Fatal("startup exceeded budget")
	}
	transport := &http.Transport{Proxy: nil, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, e := (&net.Dialer{}).DialContext(ctx, network, root.address)
		if e == nil {
			root.connections.Add(1)
		}
		return conn, e
	}}
	root.client = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return root
}

func TestModelMeetingSummarySettingsRoot(t *testing.T) {
	meetingSummarySettingsBudget(t)
	t.Run("default_init_restart_and_old_routes", func(t *testing.T) {
		v := newMeetingSummaryFixture(t, false)
		// The formal fixture prepared Account/Secret only for authenticated access.
		// Remove its nullable old technical selector, so the root must initialize
		// both selectors in order. Never insert or explicitly initialize Summary.
		if _, e := v.raw.Exec(testContext(t), `DELETE FROM agenteam_model.platform_selection`); e != nil {
			t.Fatal(e)
		}
		var count int
		if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.meeting_summary_selection`).Scan(&count); e != nil || count != 0 {
			t.Fatal("Summary precondition", e)
		}
		root := startMeetingSummarySettingsRoot(t, v.systemHTTPFixture, false)
		initial := root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
		first := initial.object(t)
		if len(first) != 3 || first["version"] != "1" || first["model"] != nil {
			t.Fatal("root invented model or omitted Summary")
		}
		old := root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", nil).want(t, 200).object(t)
		if old["id"] == first["id"] || old["configured"] != nil {
			t.Fatal("root collapsed singleton identities")
		}
		model := v.chat(t, false)
		body := map[string]any{"id": first["id"], "expected_version": "1", "model": model.String()}
		accepted := root.request(t, v.adminBrowser, "PUT", meetingSummarySettingsPath, body).want(t, 200).object(t)
		if accepted["resource_id"] != first["id"] || accepted["version"] != "2" || accepted["affected_references"] != "0" {
			t.Fatal("root PUT receipt")
		}
		configured := root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
		head := root.request(t, v.adminBrowser, "HEAD", meetingSummarySettingsPath, nil).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != configured.headers.Get("Content-Length") {
			t.Fatal("root HEAD changed representation")
		}
		root.request(t, v.adminBrowser, "GET", "/api/v1/session", nil).want(t, 200)
		root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
		root.request(t, v.adminBrowser, "GET", "/api/v1/projects/resolve?username=admin&project_name=absent-summary-root", nil).want(t, 404)
		var problem httpapi.Problem
		ready := root.request(t, v.adminBrowser, "GET", "/readyz", nil)
		if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) {
			t.Fatal("root readiness changed")
		}
		var diagnostic struct {
			Ready *bool `json:"ready"`
		}
		response := root.request(t, v.adminBrowser, "GET", "/diagnostics", nil)
		if response.status != 200 || json.Unmarshal(response.body, &diagnostic) != nil || diagnostic.Ready == nil || *diagnostic.Ready {
			t.Fatal("diagnostics ready changed")
		}
		if root.connections.Load() != 1 {
			t.Fatal("default root successful response failed keepalive")
		}
		root.stop(t)
		if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
			t.Fatal("default root did not actually drain")
		}
		stable := v.summaryFacts(t)
		again := startMeetingSummarySettingsRoot(t, v.systemHTTPFixture, false)
		restarted := again.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
		if !bytes.Equal(configured.body, restarted.body) {
			t.Fatal("restart reset Summary ID/version/value")
		}
		httpSameFacts(t, stable, v.summaryFacts(t))
		again.stop(t)
		for _, root := range []*projectUsageRoot{root, again} {
			for _, private := range []string{v.adminBrowser.cookie, v.adminBrowser.csrf} {
				if strings.Contains(root.logs.String(), private) {
					t.Fatal("root logs exposed private identity")
				}
			}
		}
	})
	for _, mode := range []string{"missing_table", "corrupt_fact", "held_summary_lock"} {
		t.Run(mode, func(t *testing.T) {
			v := newMeetingSummaryFixture(t, false)
			switch mode {
			case "missing_table":
				if _, e := v.raw.Exec(testContext(t), `ALTER TABLE agenteam_model.meeting_summary_selection RENAME TO unavailable_summary_selection`); e != nil {
					t.Fatal(e)
				}
			case "corrupt_fact":
				// Root failure probe only: a canonical pointer with no matching reference is
				// corrupt storage, never a way to establish a successful selected Model.
				m := v.chat(t, false)
				if _, e := v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.meeting_summary_selection(id,singleton,version,model_id,updated_at) VALUES($1,true,2,$2,clock_timestamp())`, newID[struct{}](t).String(), m.String()); e != nil {
					t.Fatal(e)
				}
			case "held_summary_lock":
				held, release, done := make(chan struct{}), make(chan struct{}), make(chan f.CommitResult, 1)
				var once sync.Once
				unlock := func() { once.Do(func() { close(release) }) }
				defer unlock()
				holder := openStore(t, v.db.Config(t, nil))
				key, e := f.SystemConfigLock("model-meeting-summary-selection")
				if e != nil {
					t.Fatal(e)
				}
				var joined sync.Once
				joinHolder := func() {
					joined.Do(func() {
						unlock()
						if result := <-done; result.State() != f.Committed {
							t.Error("holder actual wait", result.Fault())
						}
					})
				}
				t.Cleanup(joinHolder)
				go func() {
					done <- holder.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
						if e := holder.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
							return e
						}
						close(held)
						<-release
						return nil
					})
				}()
				select {
				case <-held:
				case result := <-done:
					joined.Do(func() {})
					t.Fatal("lock holder failed", result.Fault())
				}
				startMeetingSummarySettingsRoot(t, v.systemHTTPFixture, true)
				joinHolder()
				return
			}
			startMeetingSummarySettingsRoot(t, v.systemHTTPFixture, true)
		})
	}
}
