package accountenv_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

func TestB04ConfigAccountEnvironmentOwnsPrivateInputs(t *testing.T) {
	t.Setenv("AGENTEAM_CENTRAL_ACCOUNT_KEYRING", "ignored deployment value")
	t.Setenv("AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG", "/ignored/deployment/path")
	a, b := accountenv.New(t), accountenv.New(t)
	av, bv := a.Values(), b.Values()
	const ringKey = "AGENTEAM_CENTRAL_ACCOUNT_KEYRING"
	const logKey = "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG"
	if len(av) != 2 || av[ringKey] == bv[ringKey] || av[logKey] == bv[logKey] {
		t.Fatal("independent account environments reused inputs")
	}
	av[ringKey], av[logKey] = "changed", "changed"
	if reflect.DeepEqual(av, a.Values()) || !reflect.DeepEqual(a.Environ(), a.Environ()) {
		t.Fatal("retained test environment is mutable")
	}
	for _, e := range []*accountenv.Environment{a, b} {
		values := e.Values()
		path := values[logKey]
		for _, item := range []struct {
			path      string
			mode      os.FileMode
			directory bool
		}{{filepath.Dir(path), 0700, true}, {path, 0600, false}} {
			info, err := os.Lstat(item.path)
			if err != nil || info.Mode().Perm() != item.mode || info.IsDir() != item.directory || !item.directory && !info.Mode().IsRegular() {
				t.Fatal("account environment has unsafe file type or mode")
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != uint32(os.Geteuid()) {
				t.Fatal("account environment is not owned by the test UID")
			}
		}
		sink, err := recoverylog.Open(path)
		if err != nil {
			t.Fatal("owned recovery log failed real open")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		sink.StopAdmission()
		err = sink.Drain(ctx)
		cancel()
		if err != nil || !sink.Joined() {
			t.Fatal("owned recovery log did not actually close")
		}
		for _, entry := range e.Environ() {
			key, value, ok := strings.Cut(entry, "=")
			if !ok || values[key] != value {
				t.Fatal("account environment serialization changed its values")
			}
		}
	}
}

func TestB04ConfigAccountEnvironmentIndependentKeys(t *testing.T) {
	keys := accountenv.New(t).Values()["AGENTEAM_CENTRAL_ACCOUNT_KEYRING"]
	cursors, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.LoadKeyring(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, cursors)
	if err != nil {
		t.Fatal(err)
	}
	downloads, err := object.LoadDownloadKeyring(objectfixture.ConfigOnlyValues()["AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING"], cursors, secrets)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.LoadKeyring(keys, cursors, secrets, downloads)
	if err != nil || accounts.Validate() != nil {
		t.Fatal("test account material is invalid or reused")
	}
	var wire struct {
		Keys []struct {
			Material string `json:"key_b64"`
		} `json:"keys"`
	}
	if json.Unmarshal([]byte(keys), &wire) != nil || len(wire.Keys) != 1 || wire.Keys[0].Material == "" {
		t.Fatal("test account key is missing")
	}
}

func TestB04ConfigAccountEnvironmentCleansExactPath(t *testing.T) {
	var path string
	t.Run("owner", func(t *testing.T) {
		path = accountenv.New(t).Values()["AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG"]
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("owned recovery log was not created")
		}
	})
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("test cleanup left its exact recovery log behind")
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("test cleanup left its private recovery directory behind")
	}
}
