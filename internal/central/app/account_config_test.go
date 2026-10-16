package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type devModeNoCallDelivery struct{ c.DeliveryPort }

func TestAccountDevModeDeploymentAssembly(t *testing.T) {
	for _, mode := range []string{"unset", "false", "true", "true_remote_origin"} {
		t.Run(mode, func(t *testing.T) {
			values := map[string]string{}
			dev := mode == "true" || mode == "true_remote_origin"
			if mode != "unset" {
				values["DEV_MODE"] = "false"
				if dev {
					values["DEV_MODE"] = "true"
				}
			}
			if mode == "true_remote_origin" {
				values["PUBLIC_ORIGIN"] = "http://public.example.test:8080"
			}
			cfg := testConfig(t, "1s", values)
			authority, err := account.NewAuthority(&riNoCallStore{}, cfg.AccountKeyring())
			if err != nil {
				t.Fatal(err)
			}
			catalog := event.NewCatalog()
			revoked, err := c.DefineSessionsRevoked(catalog)
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := c.DefineDeliveryRequested(catalog)
			if err != nil {
				t.Fatal(err)
			}
			core, err := createAccountService(cfg, account.Dependencies{DevMode: !dev, Authority: authority, Audit: riNoCallAudit{}, Secrets: &secret.Service{}, Events: riNoCallEvents{}, SessionsRevoked: revoked, DeliveryRequested: delivery, Challenges: riNoCallChallenges{}, RecoveryLog: &recoverylog.Sink{}, Processes: riNoCallProcesses{}})
			if err != nil {
				t.Fatal(err)
			}
			boundary, err := account.NewHTTPBoundary(core, cfg.PublicOrigin())
			if err != nil {
				t.Fatal("configured origin did not reach shared boundary", err)
			}
			r := httptest.NewRequest("POST", "http://unrelated.example.test:9090/api/v1/session", nil)
			r.Header.Set("Origin", "http://another.example.test")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			if accepted := boundary.CheckRequest(httptest.NewRecorder(), r) == nil; accepted != dev {
				t.Fatal("deployment mode did not override HTTP dependencies")
			}
			worker, err := createAccountMailWorker(cfg, accountmail.Dependencies{DevMode: !dev, PublicOrigin: "ftp://unused.example.test", Port: devModeNoCallDelivery{}, Registry: &accountmail.WorkRegistry{}, Trust: cfg.OutboundTrust(), RecoveryLog: &recoverylog.Sink{}})
			if err != nil || worker == nil {
				t.Fatal("deployment mail origin/mode not propagated", err)
			}
			sink, err := openAccountRecoveryLog(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				sink.StopAdmission()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := sink.Drain(ctx); err != nil || !sink.Joined() {
					t.Error("recovery log did not join", err)
				}
			})
			const id = "01900000-0000-7000-8000-000000000001"
			const attempt = "01900000-0000-7000-8000-000000000002"
			for _, path := range []string{"/invite", "/reset-password"} {
				material, err := sc.NewSecretMaterial([]byte("http://public.example.test:8080" + path + "#" + id + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))))
				if err != nil {
					t.Fatal(err)
				}
				defer material.Destroy()
				var result recoverylog.Result
				if path == "/invite" {
					record, recordErr := recoverylog.NewInvitationRecord(id, attempt, "test@example.test", material)
					if recordErr != nil {
						t.Fatal(recordErr)
					}
					result, err = sink.WriteInvitation(context.Background(), record)
				} else {
					record, recordErr := recoverylog.NewResetRecord(id, attempt, "test@example.test", material)
					if recordErr != nil {
						t.Fatal(recordErr)
					}
					result, err = sink.WriteReset(context.Background(), record)
				}
				if dev {
					if err != nil || result.State != recoverylog.Written {
						t.Fatal("development recovery-link mode not propagated", err)
					}
				} else if err == nil || result.State != recoverylog.NotWritten {
					t.Fatal("production recovery log accepted HTTP link")
				}
			}
		})
	}
}
