//go:build integration

package objects_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

func fixtureBackend(t *testing.T, f *fixture, changes map[string]string) *object.Backend {
	t.Helper()
	values := map[string]string{"ENDPOINT": f.remote.Endpoint(), "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": f.remote.CAFile}
	for k, v := range changes {
		values[k] = v
	}
	c, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := object.NewBackend(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}
func TestObjectBucketControlsTLSAndCredentialBoundaries(t *testing.T) {
	f := newFixture(t, false)
	ctx := contextFor(t)
	if err := f.backend.CheckBucket(ctx); err != nil {
		t.Fatal(err)
	}
	for _, changes := range []map[string]string{{"CA_FILE": f.db.Fixture.WrongCAFile}, {"ACCESS_KEY": "wrong-owned-fixture-user"}, {"SECRET_KEY": "wrong-owned-fixture-password"}, {"BUCKET": "missing-" + f.remote.Nonce}} {
		b := fixtureBackend(t, f, changes)
		if err := b.CheckBucket(ctx); err == nil {
			t.Fatal("TLS/credential/bucket error accepted")
		}
	}
	t.Run("lifecycle", func(t *testing.T) {
		c := &lifecycle.Configuration{Rules: []lifecycle.Rule{{ID: "owned-rule", Status: "Enabled", Expiration: lifecycle.Expiration{Days: 1}}}}
		if err := f.s3.SetBucketLifecycle(ctx, f.bucket, c); err != nil {
			t.Fatal("owned lifecycle setup failed")
		}
		if err := f.backend.CheckBucket(ctx); err == nil {
			t.Fatal("automatic marker deletion policy accepted")
		}
		if err := f.s3.SetBucketLifecycle(ctx, f.bucket, nil); err != nil {
			t.Fatal("owned lifecycle cleanup failed")
		}
		if err := f.backend.CheckBucket(ctx); err != nil {
			t.Fatal("absent lifecycle not accepted", err)
		}
	})
	t.Run("public-policy", func(t *testing.T) {
		policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + f.bucket + `/*"]}]}`
		if err := f.s3.SetBucketPolicy(ctx, f.bucket, policy); err != nil {
			t.Fatal("owned policy setup failed")
		}
		if err := f.backend.CheckBucket(ctx); err == nil {
			t.Fatal("public bucket accepted")
		}
		if err := f.s3.SetBucketPolicy(ctx, f.bucket, ""); err != nil {
			t.Fatal("owned policy cleanup failed")
		}
	})
	if err := f.s3.SetBucketVersioning(ctx, f.bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal("owned versioning setup failed")
	}
	if err := f.backend.CheckBucket(ctx); err == nil {
		t.Fatal("versioned bucket accepted")
	}
	if err := f.s3.SetBucketVersioning(ctx, f.bucket, minio.BucketVersioningConfiguration{Status: "Suspended"}); err != nil {
		t.Fatal("owned versioning suspension failed")
	}
	if err := f.backend.CheckBucket(ctx); err == nil {
		t.Fatal("previously versioned bucket treated as never versioned")
	}
	lockBucket := f.bucket + "-lock"
	if err := f.s3.MakeBucket(ctx, lockBucket, minio.MakeBucketOptions{Region: "us-east-1", ObjectLocking: true}); err != nil {
		t.Fatal("owned locked bucket creation failed")
	}
	locked := fixtureBackend(t, f, map[string]string{"BUCKET": lockBucket})
	if err := locked.CheckBucket(ctx); err == nil {
		t.Fatal("object-locked bucket accepted")
	}
}
func TestObjectStoreIdentityNeverSilentlyRebinds(t *testing.T) {
	for _, mode := range []string{"missing", "mismatch", "new-nonempty"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, false)
			ctx := contextFor(t)
			var original string
			if err := f.store.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_object.store_identity`).Scan(&original); err != nil {
				t.Fatal(err)
			}
			if err := f.s3.RemoveObject(ctx, f.bucket, "control/store-identity", minio.RemoveObjectOptions{}); err != nil {
				t.Fatal(err)
			}
			if mode == "mismatch" {
				if _, err := f.s3.PutObject(ctx, f.bucket, "control/store-identity", strings.NewReader(id[oc.StoredObject](t).String()), 36, minio.PutObjectOptions{DisableMultipart: true}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "new-nonempty" {
				f.sql(t, `DELETE FROM agenteam_object.store_identity`)
				if _, err := f.s3.PutObject(ctx, f.bucket, "unrelated-owned-object", strings.NewReader("must-survive"), 12, minio.PutObjectOptions{DisableMultipart: true}); err != nil {
					t.Fatal(err)
				}
			}
			service, _ := f.createService(t, f.store, f.remote.Endpoint(), nil, id[oc.Process](t), filepath.Join(t.TempDir(), "spool"))
			if err := service.Initialize(ctx); err == nil {
				t.Fatal("unsafe bucket identity reinitialized")
			}
			if mode != "new-nonempty" {
				var current string
				if err := f.store.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_object.store_identity`).Scan(&current); err != nil || current != original {
					t.Fatal("confirmed identity changed", err)
				}
			} else {
				var reserved string
				if err := f.store.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_object.store_identity WHERE phase='reserved'`).Scan(&reserved); err != nil {
					t.Fatal(err)
				}
				if err := f.s3.RemoveObject(ctx, f.bucket, "unrelated-owned-object", minio.RemoveObjectOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := service.Initialize(ctx); err != nil {
					t.Fatal("original reserved identity did not resume", err)
				}
				reader, err := f.s3.GetObject(ctx, f.bucket, "control/store-identity", minio.GetObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || string(data) != reserved {
					t.Fatal("identity regenerated during retry", err)
				}
			}
		})
	}
}
func TestObjectZeroPayloadHasFullEmptyIntegrityAndNoMultipart(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "empty", "")
	if stored.Meta.ByteSize != 0 || stored.Meta.SHA256 != digest(nil) {
		t.Fatal("empty metadata incorrect")
	}
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || len(body) != 0 {
		t.Fatal("empty object not verified", err)
	}
	_, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, &oc.ByteRange{Offset: 0, Length: 1})
	requireCode(t, err, foundation.RangeNotSatisfiable)
	for item := range f.s3.ListIncompleteUploads(contextFor(t), f.bucket, "", true) {
		if item.Err != nil {
			t.Fatal("multipart inspection unavailable")
		}
		t.Fatal("single PUT left multipart upload")
	}
}
