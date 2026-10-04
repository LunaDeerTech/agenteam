package objectfixture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// ConfigOnlyValues never reaches storage; it is solely for configuration/unit
// process tests whose package-local composition replaces runtime assembly.
func ConfigOnlyValues() map[string]string {
	return map[string]string{
		"AGENTEAM_CENTRAL_OBJECT_ENDPOINT":         "http://127.0.0.1:1",
		"AGENTEAM_CENTRAL_OBJECT_BUCKET":           "configuration-only",
		"AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY":       "object-unit-access",
		"AGENTEAM_CENTRAL_OBJECT_SECRET_KEY":       "object-unit-configuration-secret",
		"AGENTEAM_CENTRAL_OBJECT_TLS_MODE":         "disable",
		"AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING": `{"format":1,"current_kid":"download","keys":[{"kid":"download","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJqbnJ2en58="}]}`,
	}
}
func ConfigOnlyEnvironment() []string {
	var env []string
	for key, value := range ConfigOnlyValues() {
		env = append(env, key+"="+value)
	}
	return env
}

// Environment provisions a dedicated bucket/spool for one fixture database.
// Identity is hashed only to reuse the same owned resources on process restart.
func Environment(ctx context.Context, identity string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	d, err := Load()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(identity))
	suffix := hex.EncodeToString(sum[:12])
	bucket := "d05-" + d.Nonce[:12] + "-" + suffix
	client, transport, err := d.Client()
	if err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, bad()
	}
	if !exists {
		if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			return nil, bad()
		}
	}
	parent := filepath.Join(d.Directory, "process-runtime")
	if err = os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
		return nil, bad()
	}
	actual, err := filepath.EvalSymlinks(parent)
	if err != nil || actual != parent {
		return nil, bad()
	}
	st, err := os.Lstat(parent)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, bad()
	}
	values := ConfigOnlyValues()
	values["AGENTEAM_CENTRAL_OBJECT_ENDPOINT"] = d.Endpoint()
	values["AGENTEAM_CENTRAL_OBJECT_BUCKET"] = bucket
	values["AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY"] = d.AccessKey
	values["AGENTEAM_CENTRAL_OBJECT_SECRET_KEY"] = d.SecretKey
	values["AGENTEAM_CENTRAL_OBJECT_TLS_MODE"] = "verify-full"
	values["AGENTEAM_CENTRAL_OBJECT_CA_FILE"] = d.CAFile
	values["AGENTEAM_CENTRAL_OBJECT_SPOOL_DIR"] = filepath.Join(parent, suffix)
	var env []string
	for key, value := range values {
		if strings.ContainsAny(value, "\r\n") {
			return nil, bad()
		}
		env = append(env, key+"="+value)
	}
	return env, nil
}
