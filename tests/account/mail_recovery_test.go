//go:build integration

package account_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func manualMailForService(t *testing.T, service *account.Service, process c.ProcessID) (c.DeliveryPort, *manualMailRuntime) {
	t.Helper()
	r := &manualMailRuntime{process: process, issuer: c.NewDeliveryIssuer(), active: map[string]c.DeliveryAttempt{}, joined: map[string]c.DeliveryCompletion{}}
	p, e := account.NewDeliveryPort(service, r)
	if e != nil {
		t.Fatal(e)
	}
	return p, r
}

type mailChildInput struct{ Database, Process, Job, Stage string }

// The owned child hosts the actual Account port and actual Secret borrowers.
// It emits only a fixed boundary marker. Parent Kill+Wait is the death proof.
func TestAccountMailCrashHelper(t *testing.T) {
	path := os.Getenv("AGENTEAM_ACCOUNT_MAIL_CRASH_INPUT")
	if path == "" {
		t.Skip("owned subprocess helper")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned helper input")
	}
	b, e := os.ReadFile(path)
	var input mailChildInput
	if e != nil || json.Unmarshal(b, &input) != nil {
		t.Fatal("invalid helper descriptor")
	}
	pg, e := pgfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := pg.Config(input.Database, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw := openStore(t, cfg)
	key, _, _ := keys(t)
	authority, e := account.NewAuthority(raw, key)
	if e != nil {
		t.Fatal(e)
	}
	process, e := foundation.ParseID[c.Process](input.Process)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleAccount(t, raw, raw, authority, liveProcess{process})
	port, runtime := manualMailForService(t, f.service, process)
	job, e := foundation.ParseID[c.MailJob](input.Job)
	if e != nil {
		t.Fatal(e)
	}
	a, e := port.ClaimDelivery(ctxFor(t), job)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	runtime.accept(a)
	pause := func() error {
		fmt.Println("ACCOUNT_MAIL_BOUNDARY")
		_, e := bufio.NewReader(os.Stdin).ReadByte()
		return e
	}
	if input.Stage == "claim" {
		_ = pause()
		return
	}
	m, e := port.PrepareDelivery(ctxFor(t), a)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	defer m.Destroy()
	if e = m.Use(func(c.DeliveryMaterialFields) error { return pause() }); e != nil {
		t.Fatal(e)
	}
}

func TestAccountMailExactChildDeathProtectsBothMaterialSlots(t *testing.T) {
	for _, stage := range []string{"claim", "acquired", "legacy"} {
		t.Run(stage, func(t *testing.T) {
			f := newB02Account(t)
			admin := b02Admin(t, f)
			cfg, e := f.service.GetSMTPSettings(ctxFor(t), admin)
			if e != nil {
				t.Fatal(e)
			}
			password := testPassword(t, "owned child SMTP credential")
			update, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: cfg.Version, Configured: true, Host: "mail.example.test", Port: 2525, TLSMode: "none", Username: "fixture", SenderEmail: "sender@example.test", RetryCount: 3, RetryIntervalSeconds: 10, Password: &password})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.service.UpdateSMTPSettings(ctxFor(t), update); e != nil {
				t.Fatal(e)
			}
			q, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "owned-child@example.test"})
			if e != nil {
				t.Fatal(e)
			}
			invite, e := f.service.CreateInvitation(ctxFor(t), q)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
			var job string
			if e = f.store.QueryRow(ctxFor(t), `SELECT j.id::text FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id WHERE i.link_id=$1`, invite.ID.String()).Scan(&job); e != nil {
				t.Fatal(e)
			}
			process := id[c.Process](t)
			input := mailChildInput{f.db.Name, process.String(), job, stage}
			dir := t.TempDir()
			if e = os.Chmod(dir, 0700); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "input.json")
			raw, _ := json.Marshal(input)
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAccountMailCrashHelper$")
			cmd.Env = append(os.Environ(), "AGENTEAM_ACCOUNT_MAIL_CRASH_INPUT="+path, "TMPDIR="+dir)
			stdin, e := cmd.StdinPipe()
			if e != nil {
				t.Fatal(e)
			}
			defer stdin.Close()
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("owned mail child did not join")
				}
			})
			line := make(chan string, 1)
			go func() {
				scan := bufio.NewScanner(stdout)
				if scan.Scan() {
					line <- scan.Text()
				} else {
					line <- ""
				}
			}()
			select {
			case got := <-line:
				if got != "ACCOUNT_MAIL_BOUNDARY" {
					t.Fatalf("child missed safe boundary: %q", got)
				}
			case <-ctx.Done():
				t.Fatal("owned mail child boundary timeout")
			}
			current := id[c.Process](t)
			restarted := assembleAccount(t, f.store, f.store, f.authority, childDeath{current, process, done})
			port, _ := manualMailForService(t, restarted.service, current)
			status, e := port.RecoverDeliveries(ctxFor(t))
			if !hasCode(e, foundation.ResourceBusy) || status.Pending != 1 || status.Advanced != 0 {
				t.Fatal("live child was not protected", status, e, safeFailure(e))
			}
			var leases int
			var terminal bool
			if e = f.store.QueryRow(ctxFor(t), `SELECT a.terminal,(SELECT count(*) FROM agenteam_secret.secret_leases l WHERE l.owner_id=a.id AND NOT l.released) FROM agenteam_account.mail_attempts a WHERE a.job_id=$1`, job).Scan(&terminal, &leases); e != nil || terminal {
				t.Fatal("live child terminal", terminal, e)
			}
			want := 0
			if stage != "claim" {
				want = 2
			}
			if leases != want {
				t.Fatal("material-slot preparation", leases, want)
			}
			var originalToken, originalCredential string
			if stage == "legacy" {
				if e = f.store.QueryRow(ctxFor(t), `SELECT token_ref::text,credential_ref::text FROM agenteam_account.mail_attempts WHERE job_id=$1`, job).Scan(&originalToken, &originalCredential); e != nil {
					t.Fatal(e)
				}
				for range 2 {
					settings, err := f.service.GetSMTPSettings(ctxFor(t), admin)
					if err != nil {
						t.Fatal(err)
					}
					newPassword := testPassword(t, "owned replacement credential "+id[struct{}](t).String())
					replacement, err := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: "mail.example.test", Port: 2525, TLSMode: "none", Username: "fixture", SenderEmail: "sender@example.test", RetryCount: 3, RetryIntervalSeconds: 10, Password: &newPassword})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = f.service.UpdateSMTPSettings(ctxFor(t), replacement); err != nil {
						t.Fatal(err, safeFailure(err))
					}
				}
				if e = f.service.RevokeInvitation(ctxFor(t), c.InvitationRevoke{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ID: invite.ID, ExpectedVersion: invite.Version}); e != nil {
					t.Fatal(e, safeFailure(e))
				}
				// Simulate the precise pre-00011 representation. The real original
				// leases remain active and the child still holds their material.
				if _, e = f.db.Connect(t).Exec(ctxFor(t), `UPDATE agenteam_account.mail_attempts SET token_ref=NULL,credential_ref=NULL WHERE job_id=$1`, job); e != nil {
					t.Fatal(e)
				}
				if _, e = port.RecoverDeliveries(ctxFor(t)); !hasCode(e, foundation.ResourceBusy) {
					t.Fatal("legacy live work must stay protected", safeFailure(e))
				}
				var protected int
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.material_cleanup WHERE credential_id IN ($1,$2) AND phase<>'completed'`, originalToken, originalCredential).Scan(&protected); e != nil || protected != 2 {
					t.Fatal("original mapping lost before join", protected, e)
				}
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("owned mail child kill did not join")
			}
			for range 2 {
				if _, e = port.RecoverDeliveries(ctxFor(t)); e != nil {
					t.Fatal("exact-death recovery", e, safeFailure(e))
				}
			}
			var joined bool
			var phase, result string
			wantPhase := "retry_wait"
			if stage == "legacy" {
				wantPhase = "failed"
			}
			if e = f.store.QueryRow(ctxFor(t), `SELECT a.terminal,a.io_joined,a.result,j.phase,(SELECT count(*) FROM agenteam_secret.secret_leases l WHERE l.owner_id=a.id AND NOT l.released) FROM agenteam_account.mail_attempts a JOIN agenteam_account.mail_jobs j ON j.id=a.job_id WHERE a.job_id=$1`, job).Scan(&terminal, &joined, &result, &phase, &leases); e != nil || !terminal || !joined || result != "failed" || phase != wantPhase || leases != 0 {
				t.Fatal("exact-death final facts", terminal, joined, result, phase, leases, e)
			}
			if stage == "legacy" {
				var token, credential, current string
				if e = f.store.QueryRow(ctxFor(t), `SELECT token_ref::text,credential_ref::text,(SELECT password_ref::text FROM agenteam_account.smtp_settings WHERE singleton) FROM agenteam_account.mail_attempts WHERE job_id=$1`, job).Scan(&token, &credential, &current); e != nil || token != originalToken || credential != originalCredential || credential == current {
					t.Fatal("legacy did not recover exact original refs", e)
				}
			}
		})
	}
}
