// agenteam-account-recovery issues a short-lived, single-use mailbox recovery
// authorization using the existing deployment configuration and database.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func main() { os.Exit(execute(os.Args[1:], os.Stderr)) }
func execute(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("agenteam-account-recovery", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	user := flags.String("user-id", "", "Immutable account UserID")
	purpose := flags.String("purpose", "", "initialize_email or recover_account")
	email := flags.String("email", "", "Exact intended destination mailbox")
	output := flags.String("output", "", "New absolute path for a private authorization file")
	if e := flags.Parse(args); e != nil || flags.NArg() != 0 || *user == "" || *email == "" || (*purpose != "initialize_email" && *purpose != "recover_account") || !filepath.IsAbs(*output) {
		_, _ = fmt.Fprintln(stderr, "Usage: agenteam-account-recovery --user-id UUID --purpose initialize_email|recover_account --email ADDRESS --output /private/new-file.json")
		return 2
	}
	cfg, e := config.Load(os.LookupEnv, os.Environ())
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_CONFIGURATION_INVALID")
		return 2
	}
	file, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_OUTPUT_UNAVAILABLE")
		return 1
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(*output)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, e := postgres.Open(ctx, cfg.Database())
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_DATABASE_UNAVAILABLE")
		return 1
	}
	defer func() {
		store.StopAdmission()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := store.Drain(cleanup); e != nil {
			_ = store.ForceClose(cleanup)
		}
	}()
	authority, e := account.NewAuthority(store, cfg.AccountKeyring())
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_AUTHORITY_UNAVAILABLE")
		return 1
	}
	auditor, e := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: authority})
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_AUDIT_UNAVAILABLE")
		return 1
	}
	issued, e := authority.IssueEmailAuthorization(ctx, auditor, *user, *purpose, *email)
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_AUTHORIZATION_FAILED")
		return 1
	}
	defer issued.Authorization.Destroy()
	e = issued.Authorization.Use(func(token []byte) error {
		data, e := json.Marshal(struct {
			ID            string `json:"id"`
			Authorization string `json:"authorization"`
			UserID        string `json:"user_id"`
			Purpose       string `json:"purpose"`
			Email         string `json:"email"`
			ExpiresAt     any    `json:"expires_at"`
		}{issued.ID, string(token), *user, *purpose, *email, issued.ExpiresAt})
		if e != nil {
			return e
		}
		defer clear(data)
		if _, e = file.Write(append(data, '\n')); e != nil {
			return e
		}
		return file.Sync()
	})
	if e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_OUTPUT_WRITE_FAILED")
		return 1
	}
	if e = file.Close(); e != nil {
		_, _ = fmt.Fprintln(stderr, "RECOVERY_OUTPUT_WRITE_FAILED")
		return 1
	}
	complete = true
	return 0
}
