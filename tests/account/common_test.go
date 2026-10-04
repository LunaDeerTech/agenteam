//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func id[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func cause(t *testing.T) foundation.TransactionCause {
	t.Helper()
	c, e := foundation.NewRecoveryCause("account.fixture", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(ctxFor(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func migrate(t *testing.T, cfg postgres.Config, src ...postgres.Source) *postgres.Migrator {
	t.Helper()
	m, e := postgres.NewMigrator(cfg, src...)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctxFor(t)); !r.Migrated {
		t.Fatalf("migration %v", r.Fault)
	}
	return m
}
func keys(t *testing.T) (account.Keyring, cursor.Keyring, secret.Keyring) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	c, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	s, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), c)
	if e != nil {
		t.Fatal(e)
	}
	d, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), c, s)
	if e != nil {
		t.Fatal(e)
	}
	a, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), c, s, d)
	if e != nil {
		t.Fatal(e)
	}
	return a, c, s
}
func database(t *testing.T) (*pgfixture.Database, *postgres.Store, *account.Authority) {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	s := openStore(t, cfg)
	k, _, _ := keys(t)
	a, e := account.NewAuthority(s, k)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return db, s, a
}
