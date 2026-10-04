// Package objectfixture exposes only verified task-owned MinIO coordinates.
package objectfixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const Env = "AGENTEAM_OBJECT_FIXTURE"
const Label = "agenteam.d05.objectfixture"
const BinarySHA = "dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8"
const Release = "RELEASE.2025-10-15T17-29-55Z"
const CachedBinary = "/tmp/agenteam-d05-r01-hxwb6ogm/bin/minio"

type Descriptor struct{ ContainerID, NetworkID, Nonce, PrivateIP, Directory, CAFile, AccessKey, SecretKey string }

func bad() error { return errors.New("owned object fixture invalid or unavailable") }
func VerifyBinary(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return bad()
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return bad()
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil || hex.EncodeToString(h.Sum(nil)) != BinarySHA {
		return bad()
	}
	return nil
}
func Load() (*Descriptor, error) {
	path := os.Getenv(Env)
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return nil, bad()
	}
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 8192 {
		return nil, bad()
	}
	var d Descriptor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil {
		return nil, bad()
	}
	if e = d.Verify(); e != nil {
		return nil, e
	}
	return &d, nil
}
func (d *Descriptor) Verify() error {
	n, e := hex.DecodeString(d.Nonce)
	ip, ie := netip.ParseAddr(d.PrivateIP)
	if e != nil || len(n) != 16 || ie != nil || !ip.IsPrivate() || len(d.ContainerID) != 64 || len(d.NetworkID) != 64 || len(d.AccessKey) < 16 || len(d.SecretKey) < 32 || !filepath.IsAbs(d.Directory) || filepath.Base(d.Directory) != "agenteam-d05-object-"+d.Nonce || d.CAFile != filepath.Join(d.Directory, "certs", "ca.crt") {
		return bad()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, e := pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", d.ContainerID)
	if e != nil {
		return e
	}
	var got struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State  struct{ Running bool }
		Mounts []struct {
			Source, Destination string
			RW                  bool
		}
		NetworkSettings struct {
			Ports    map[string][]struct{ HostIP, HostPort string }
			Networks map[string]struct{ NetworkID, IPAddress string }
		}
	}
	if json.Unmarshal(raw, &got) != nil || got.ID != d.ContainerID || got.Name != "/agenteam-d05-object-"+d.Nonce || got.Config.Image != pgfixture.Image || got.Config.Labels[Label] != d.Nonce || !got.State.Running || len(got.NetworkSettings.Ports["9000/tcp"]) != 0 {
		return bad()
	}
	network := false
	for _, v := range got.NetworkSettings.Networks {
		if v.NetworkID == d.NetworkID && v.IPAddress == d.PrivateIP {
			network = true
		}
	}
	if !network {
		return bad()
	}
	expected := map[string]string{"/fixture": d.Directory, "/fixture/data": filepath.Join(d.Directory, "data")}
	for _, m := range got.Mounts {
		if expected[m.Destination] == m.Source && m.RW == (m.Destination == "/fixture/data") {
			delete(expected, m.Destination)
		}
	}
	if len(expected) != 0 {
		return bad()
	}
	raw, e = pgfixture.Docker(ctx, "network", "inspect", "--format", "{{json .}}", d.NetworkID)
	var networkInfo struct {
		ID       string `json:"Id"`
		Internal bool
		Labels   map[string]string
	}
	if e != nil || json.Unmarshal(raw, &networkInfo) != nil || networkInfo.ID != d.NetworkID || !networkInfo.Internal || networkInfo.Labels[Label] != d.Nonce {
		return bad()
	}
	return nil
}
func (d *Descriptor) Endpoint() string { return "https://" + net.JoinHostPort(d.PrivateIP, "9000") }
func (d *Descriptor) Client() (*minio.Client, *http.Transport, error) {
	if e := d.Verify(); e != nil {
		return nil, nil, e
	}
	raw, e := os.ReadFile(d.CAFile)
	if e != nil {
		return nil, nil, bad()
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, nil, bad()
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: 15 * time.Second, DisableCompression: true}
	client, e := minio.New(net.JoinHostPort(d.PrivateIP, "9000"), &minio.Options{Creds: credentials.NewStaticV4(d.AccessKey, d.SecretKey, ""), Secure: true, Region: "us-east-1", BucketLookup: minio.BucketLookupPath, Transport: transport, MaxRetries: 1, TrailingHeaders: true})
	if e != nil {
		transport.CloseIdleConnections()
		return nil, nil, bad()
	}
	return client, transport, nil
}
