//go:build integration

package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type appFixtureResolver struct{ ip netip.Addr }

func (r appFixtureResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{r.ip}, ctx.Err()
}

type observedOutbound struct{ *outboundRuntime }

func (o observedOutbound) StopAdmission() {
	o.outboundRuntime.StopAdmission()
	fmt.Fprintln(os.Stdout, `{"event":"outbound_admission_stopped"}`)
}
func (o observedOutbound) Drain(ctx context.Context) error {
	err := o.outboundRuntime.Drain(ctx)
	fmt.Fprintln(os.Stdout, `{"event":"outbound_drain_returned"}`)
	return err
}

func configureOutboundFixture(t *testing.T, mode string, cfg config.Config, deps *dependencies, release <-chan struct{}) {
	t.Helper()
	if mode == "outbound_startup_second_signal" {
		deps.outbound = func(ctx context.Context, cfg config.Config, db database, service *audit.Service) (egress, error) {
			result, err := initializeOutbound(ctx, cfg, db, service)
			fmt.Fprintln(os.Stdout, `{"event":"outbound_initialization_returned"}`)
			<-release
			return result, err
		}
		return
	}
	if !strings.HasPrefix(mode, "outbound_") {
		return
	}
	d, err := netfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	var runtime *outboundRuntime
	deps.outbound = func(ctx context.Context, cfg config.Config, db database, service *audit.Service) (egress, error) {
		result, err := initializeOutbound(ctx, cfg, db, service)
		if err != nil {
			return nil, err
		}
		runtime = result.(*outboundRuntime)
		// Only the test executable substitutes DNS. Real classifier, policy,
		// pinning, socket, deployment CA and HTTP transport remain unchanged.
		_ = runtime.client.ForceClose(ctx)
		runtime.client, err = outbound.NewClient(runtime.policy, cfg.OutboundTrust(), appFixtureResolver{netip.MustParseAddr(d.PrivateIP)})
		if err != nil {
			return nil, err
		}
		return observedOutbound{runtime}, nil
	}
	transactionHandler := deps.handler
	deps.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/egress" {
			transactionHandler.ServeHTTP(w, r)
			return
		}
		user, _ := foundation.NewID[identity.User]()
		session, _ := foundation.NewID[identity.Session]()
		actor, _ := identity.NewHuman(user, session)
		cause, _ := foundation.NewID[struct{}]()
		key, _ := ac.NewAppendKey(ac.AccessProducer, cause.String(), 0)
		call, err := outbound.NewCallContext(actor, identity.SystemScope(), key, ac.Associations{})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.Model, Streaming: true, Context: call})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		request, _ := http.NewRequest("GET", "https://fixture.test:8443/case/"+os.Getenv("AGENTEAM_OUTBOUND_CASE"), nil)
		response, err := runtime.client.Do(context.WithoutCancel(r.Context()), request, profile)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		first := make([]byte, 4)
		if _, err = io.ReadFull(response.Body(), first); err != nil {
			response.Close()
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		fmt.Fprintln(os.Stdout, `{"event":"outbound_entered"}`)
		go func() {
			_, _ = io.Copy(io.Discard, response.Body())
			response.Close()
			fmt.Fprintln(os.Stdout, `{"event":"outbound_completed"}`)
		}()
		w.WriteHeader(202)
	})
}

func seedAppOutbound(t *testing.T, db *pgfixture.Database, d *netfixture.Descriptor) {
	t.Helper()
	ctx := databaseTestContext(t)
	store, err := postgres.Open(ctx, db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(ctx); err != nil {
			t.Error(err)
		}
	}()
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	actor, _ := identity.NewHuman(user, session)
	auth := seedSecretIdentity{actor}
	auditing, err := audit.New(store, testConfig(t, "1s").CursorKeyring(), audit.Authorizations{Sessions: auth, System: auth})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := outbound.NewPolicyService(store, auditing, outbound.Authorizations{Sessions: auth, System: auth})
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	ports, _ := outbound.SelectedPorts(8443)
	rule, _ := outbound.NewRule(d.PrivateIP+"/32", ports, false)
	rules, _ := outbound.NewRules(rule)
	command, _ := foundation.NewCommandIdentity("outbound-policy", []string{user.String()}, "update", "app-owned-fixture")
	if _, err := policy.UpdatePolicy(ctx, actor, outbound.CommandMeta{Identity: command, ExpectedVersion: *policy.Status().Version}, rules); err != nil {
		t.Fatal(err)
	}
}

func TestRealOutboundHTTPMaintenanceAndTxDrainForce(t *testing.T) {
	if os.Getenv(netfixture.Env) == "" {
		t.Skip("requires owned network fixture")
	}
	d, err := netfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"outbound_graceful", "outbound_timeout", "outbound_second"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			seedAppSecret(t, db)
			seedAppOutbound(t, db, d)
			admin := db.Connect(t)
			if _, err := admin.Exec(databaseTestContext(t), `CREATE TABLE app_process_fact(value integer PRIMARY KEY)`); err != nil {
				t.Fatal(err)
			}
			locker := db.Connect(t)
			if _, err := locker.Exec(databaseTestContext(t), `BEGIN;SELECT payload_id FROM agenteam_secret.secret_payloads FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			id, err := d.Create(databaseTestContext(t), netfixture.ScenarioConfig{Mode: "stream", Body: "open"})
			if err != nil {
				t.Fatal(err)
			}
			timeout := "5s"
			if mode == "outbound_timeout" {
				timeout = "100ms"
			}
			p := launchDatabaseApp(t, db, mode, timeout, "AGENTEAM_CENTRAL_SECRET_KEYRING="+appMasterTwo, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s", netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+d.CAFile, "AGENTEAM_OUTBOUND_CASE="+id)
			address := p.stderr.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })["listen_address"].(string)
			waitAppDatabase(t, db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND query LIKE 'UPDATE agenteam_secret.secret_payloads SET wrap_nonce=%')`)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			got := receiveResponse(t, asyncGet(client, "http://"+address+"/egress"))
			if got.err != nil || got.status != 202 {
				t.Fatal("outbound fixture not admitted", got.err)
			}
			p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "outbound_entered" })
			state, err := d.State(databaseTestContext(t), id)
			if err != nil || len(state.Requests) != 1 {
				t.Fatal("actual network request absent", err)
			}
			connection := state.Requests[0].Connection
			response := asyncGet(client, "http://"+address+"/transaction")
			p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "transaction_entered" })
			start := time.Now()
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "outbound_admission_stopped" })
			if strings.Contains(p.stdout.String(), "database_admission_stopped") {
				t.Fatal("database stopped before HTTP/worker/outbound")
			}
			if mode == "outbound_graceful" {
				_, _ = io.WriteString(p.stdin, "release\n")
				got := receiveResponse(t, response)
				if got.err != nil || got.status != 200 {
					t.Fatal("HTTP failed to commit while outbound drains", got.err)
				}
				if _, err := locker.Exec(databaseTestContext(t), `ROLLBACK`); err != nil {
					t.Fatal(err)
				}
				waitAppDatabase(t, db, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_rotation_runs WHERE target_version=2 AND processed=2)`)
				if strings.Contains(p.stdout.String(), "database_admission_stopped") {
					t.Fatal("database stopped before held outbound")
				}
				if err := d.Release(databaseTestContext(t), id); err != nil {
					t.Fatal(err)
				}
				p.wait(t, 0)
			} else {
				if mode == "outbound_second" {
					if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
						t.Fatal(err)
					}
				}
				p.wait(t, 1)
				if time.Since(start) > 1600*time.Millisecond {
					t.Fatal("force budgets accumulated")
				}
				if got := receiveResponse(t, response); got.err == nil && got.status == 200 {
					t.Fatal("forced Tx committed")
				}
			}
			waitAppDatabase(t, db, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`)
			ctx, cancel := context.WithTimeout(databaseTestContext(t), time.Second)
			defer cancel()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				state, err := d.State(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if state.Closed[connection] {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("owned outbound socket not closed")
				case <-ticker.C:
				}
			}
		})
	}
}

func TestRealOutboundStartupSecondSignalBound(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	m, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(databaseTestContext(t)); !result.Migrated {
		t.Fatal(result.Fault)
	}
	guard := db.Connect(t)
	key, _ := foundation.SystemConfigLock("outbound-policy")
	if _, err := guard.Exec(databaseTestContext(t), `SELECT pg_advisory_lock($1)`, key.AdvisoryKey()); err != nil {
		t.Fatal(err)
	}
	p := launchDatabaseApp(t, db, "outbound_startup_second_signal", "5s", "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s")
	waitAppDatabase(t, db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND wait_event='advisory')`)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "outbound_initialization_returned" })
	start := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 1)
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("outbound startup reset force budget")
	}
	waitAppDatabase(t, db, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("cancelled policy startup listened")
	}
}
