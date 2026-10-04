package object

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// TransferEndpoint is a deployment origin, never a request parameter. Both
// backends use the same dedicated credentials, bucket, TLS roots and region.
type TransferEndpoint struct{ data func() StorageConfig }

func LoadTransferEndpoint(lookup func(string) (string, bool), config StorageConfig) (TransferEndpoint, error) {
	if lookup == nil || config.Validate() != nil {
		return TransferEndpoint{}, invalid()
	}
	d := config.data()
	raw, present := lookup(EnvironmentPrefix + "TRANSFER_ENDPOINT")
	if present && raw == "" {
		return TransferEndpoint{}, invalid()
	}
	if raw != "" {
		mode := "disable"
		if d.secure {
			mode = "verify-full"
		}
		candidate, err := LoadStorageConfig(func(key string) (string, bool) {
			switch key {
			case EnvironmentPrefix + "ENDPOINT":
				return raw, true
			case EnvironmentPrefix + "BUCKET":
				return d.bucket, true
			case EnvironmentPrefix + "ACCESS_KEY":
				return d.accessKey, true
			case EnvironmentPrefix + "SECRET_KEY":
				return d.secretKey, true
			case EnvironmentPrefix + "TLS_MODE":
				return mode, true
			}
			return "", false
		})
		if err != nil {
			return TransferEndpoint{}, err
		}
		next := candidate.data()
		next.roots = d.roots
		config = StorageConfig{func() storageConfig { return next }}
	}
	return TransferEndpoint{func() StorageConfig { return config }}, nil
}
func (c TransferEndpoint) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_transfer_endpoint")
}
func (c TransferEndpoint) MarshalJSON() ([]byte, error) {
	return []byte(`"object_transfer_endpoint"`), nil
}
func (*TransferEndpoint) UnmarshalJSON([]byte) error { return invalid() }
func (c TransferEndpoint) LogValue() slog.Value      { return slog.StringValue("object_transfer_endpoint") }

func (t *TransferService) sign(ctx context.Context, r transferRow) (oc.TransferMaterial, error) {
	duration, err := transferSigningDuration(time.Now(), r.expires)
	if err != nil {
		return oc.TransferMaterial{}, err
	}
	d := t.state().backend.state().config.data()
	method := http.MethodGet
	key := r.key
	headers := make(http.Header)
	if r.spec.Details().Direction == oc.TransferPUT {
		method = http.MethodPut
		key = "staging/" + r.staging.String()
		m := r.manifest.Details()
		headers.Set("Content-Type", m.MediaType)
		headers.Set("Content-Length", strconv.FormatInt(m.Length, 10))
		headers.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(digestBytes(m.SHA256)))
		headers.Set("If-None-Match", "*")
	}
	u, err := t.state().backend.state().client.PresignHeader(ctx, method, d.bucket, key, duration, nil, headers)
	if err != nil {
		return oc.TransferMaterial{}, storageError(err)
	}
	wire, err := validateTransferSignature(u, method, d, key, headers, r.expires)
	if err != nil {
		return oc.TransferMaterial{}, err
	}
	return oc.NewTransferMaterial(r.spec.Details().RunnerID, r.id, method, u.String(), headers, wire)
}

func transferSigningDuration(now, deadline time.Time) (time.Duration, error) {
	// SigV4 adds Expires to its whole-second X-Amz-Date, not to the instant
	// before PresignHeader. Flooring time.Until would permanently reject a
	// still-live one-second grant. If the SDK crosses a second after this
	// calculation, validateTransferSignature rejects the resulting excess;
	// neither that race nor a pause may extend the durable deadline.
	seconds := deadline.Sub(now.Truncate(time.Second)) / time.Second
	if !deadline.After(now) || seconds < 1 || seconds > 300 {
		return 0, failure(foundation.InvalidState, nil)
	}
	return seconds * time.Second, nil
}

func validateTransferSignature(u *url.URL, method string, d storageConfig, key string, headers http.Header, deadline time.Time) (foundation.Instant, error) {
	if u == nil {
		return foundation.Instant{}, invalid()
	}
	scheme := "http"
	if d.secure {
		scheme = "https"
	}
	if u.Scheme != scheme || u.Host != d.endpoint || u.User != nil || u.Fragment != "" || u.Path != "/"+d.bucket+"/"+key || u.RawPath != "" && u.EscapedPath() != u.Path || method != http.MethodGet && method != http.MethodPut {
		return foundation.Instant{}, unavailable(nil)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return foundation.Instant{}, unavailable(err)
	}
	allowed := map[string]bool{"X-Amz-Algorithm": true, "X-Amz-Credential": true, "X-Amz-Date": true, "X-Amz-Expires": true, "X-Amz-SignedHeaders": true, "X-Amz-Signature": true}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 {
			return foundation.Instant{}, unavailable(nil)
		}
	}
	if len(q) != len(allowed) || q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		return foundation.Instant{}, unavailable(nil)
	}
	at, err := time.Parse("20060102T150405Z", q.Get("X-Amz-Date"))
	if err != nil {
		return foundation.Instant{}, unavailable(err)
	}
	seconds, err := strconv.ParseInt(q.Get("X-Amz-Expires"), 10, 64)
	if err != nil || seconds < 1 || seconds > 300 || strconv.FormatInt(seconds, 10) != q.Get("X-Amz-Expires") {
		return foundation.Instant{}, unavailable(err)
	}
	expires := at.Add(time.Duration(seconds) * time.Second)
	if expires.After(deadline) || !expires.After(time.Now()) || at.After(time.Now().Add(time.Second)) {
		return foundation.Instant{}, failure(foundation.InvalidState, nil)
	}
	wanted := []string{"host"}
	for name := range headers {
		wanted = append(wanted, strings.ToLower(name))
	}
	sort.Strings(wanted)
	if q.Get("X-Amz-SignedHeaders") != strings.Join(wanted, ";") {
		return foundation.Instant{}, unavailable(nil)
	}
	credential := d.accessKey + "/" + at.Format("20060102") + "/us-east-1/s3/aws4_request"
	if q.Get("X-Amz-Credential") != credential || len(q.Get("X-Amz-Signature")) != 64 {
		return foundation.Instant{}, unavailable(nil)
	}
	for _, c := range q.Get("X-Amz-Signature") {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return foundation.Instant{}, unavailable(nil)
		}
	}
	return foundation.NewInstant(expires)
}
