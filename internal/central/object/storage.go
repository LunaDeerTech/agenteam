package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const markerMediaType = "application/x-agenteam-tombstone"
const controlKey = "control/store-identity"

type backendState struct {
	client      *minio.Client
	transport   *http.Transport
	config      StorageConfig
	mu          sync.Mutex
	connections map[*storageConn]bool
	closed      bool
	lifetime    context.Context
	stop        context.CancelFunc
	dials       sync.WaitGroup
}
type Backend struct{ data func() *backendState }

// NewBackend constructs a private pinned-origin transport. No caller can
// replace its dial, proxy, TLS verifier, SDK client or credentials afterwards.
func NewBackend(config StorageConfig) (*Backend, error) {
	if config.Validate() != nil {
		return nil, invalid()
	}
	d := config.data()
	s := &backendState{config: config, connections: map[*storageConn]bool{}}
	s.lifetime, s.stop = context.WithCancel(context.Background())
	transport := &http.Transport{Proxy: nil, TLSClientConfig: config.tlsConfig(), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 60 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 64, DisableCompression: true, ForceAttemptHTTP2: false}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, unavailable(nil)
		}
		s.dials.Add(1)
		s.mu.Unlock()
		defer s.dials.Done()
		dialCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(s.lifetime, cancel)
		defer func() { stop(); cancel() }()
		conn, e := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second, Resolver: &net.Resolver{PreferGo: true}}).DialContext(dialCtx, network, address)
		if e != nil {
			return nil, e
		}
		owned := &storageConn{Conn: conn, state: s}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return nil, unavailable(nil)
		}
		s.connections[owned] = true
		s.mu.Unlock()
		return owned, nil
	}
	s.transport = transport
	client, e := minio.New(d.endpoint, &minio.Options{Creds: credentials.NewStaticV4(d.accessKey, d.secretKey, ""), Secure: d.secure, Transport: storageTransport{s}, Region: "us-east-1", BucketLookup: minio.BucketLookupPath, TrailingHeaders: true, MaxRetries: 1})
	if e != nil {
		s.stop()
		return nil, unavailable(e)
	}
	s.client = client
	return &Backend{func() *backendState { return s }}, nil
}
func (b *Backend) state() *backendState        { return b.data() }
func (b Backend) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_storage_backend") }
func (b Backend) MarshalJSON() ([]byte, error) { return []byte(`"object_storage_backend"`), nil }
func (*Backend) UnmarshalJSON([]byte) error    { return invalid() }
func (b Backend) LogValue() slog.Value         { return slog.StringValue("object_storage_backend") }

type storageConn struct {
	net.Conn
	state *backendState
	once  sync.Once
}

func (c *storageConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Read(p)
}
func (c *storageConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Write(p)
}
func (c *storageConn) Close() error {
	e := c.Conn.Close()
	c.once.Do(func() { c.state.mu.Lock(); delete(c.state.connections, c); c.state.mu.Unlock() })
	return e
}

type storageTransport struct{ state *backendState }

func (t storageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.state.mu.Lock()
	closed := t.state.closed
	t.state.mu.Unlock()
	if closed {
		return nil, unavailable(nil)
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(t.state.lifetime, cancel)
	finish := func() { stop(); cancel() }
	request = request.Clone(ctx)
	d := t.state.config.data()
	scheme := "http"
	if d.secure {
		scheme = "https"
	}
	if request.URL.Scheme != scheme || request.URL.Host != d.endpoint || request.URL.User != nil {
		finish()
		return nil, unavailable(nil)
	}
	response, err := t.state.transport.RoundTrip(request)
	if err != nil {
		finish()
		return nil, err
	}
	if request.Method == http.MethodGet && (request.Header.Get("Range") != "" && response.StatusCode == http.StatusOK || request.Header.Get("Range") == "" && response.StatusCode == http.StatusPartialContent) {
		finish()
		_ = response.Body.Close()
		return nil, failure(foundation.ObjectIntegrityMismatch, nil)
	}
	response.Body = &storageBody{body: response.Body, cancel: finish}
	return response, nil
}
func storageError(err error) error {
	if err == nil {
		return nil
	}
	var f *foundation.Fault
	if errors.As(err, &f) {
		return portError(f)
	}
	e := minio.ToErrorResponse(err)
	if e.Code == "NoSuchKey" || e.Code == "NoSuchObject" {
		return failure(foundation.ObjectPayloadMissing, err)
	}
	if e.Code == "InvalidRange" {
		return failure(foundation.RangeNotSatisfiable, err)
	}
	return unavailable(err)
}
func (b *Backend) CheckBucket(ctx context.Context) error {
	s := b.state()
	bucket := s.config.data().bucket
	if exists, e := s.client.BucketExists(ctx, bucket); e != nil || !exists {
		return unavailable(e)
	}
	version, e := s.client.GetBucketVersioning(ctx, bucket)
	if e != nil {
		return storageError(e)
	}
	if version.Status != "" {
		return unavailable(nil)
	}
	_, _, _, _, e = s.client.GetObjectLockConfig(ctx, bucket)
	if minio.ToErrorResponse(e).Code != "ObjectLockConfigurationNotFoundError" {
		return unavailable(e)
	}
	_, e = s.client.GetBucketLifecycle(ctx, bucket)
	if minio.ToErrorResponse(e).Code != "NoSuchLifecycleConfiguration" {
		return unavailable(e)
	}
	policy, e := s.client.GetBucketPolicy(ctx, bucket)
	if e != nil && minio.ToErrorResponse(e).Code != "NoSuchBucketPolicy" {
		return unavailable(e)
	}
	if policy != "" {
		return unavailable(nil)
	}
	return nil
}
func (b *Backend) put(ctx context.Context, key, media string, length int64, digest foundation.Digest, body io.Reader, conditional bool) error {
	if digest.Validate() != nil || length < 0 || length > oc.MaxObjectSize {
		return invalid()
	}
	checksum, e := hex.DecodeString(digest.String()[7:])
	if e != nil {
		return invalid()
	}
	op, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	options := minio.PutObjectOptions{ContentType: media, DisableMultipart: true, SendContentMd5: false, UserMetadata: map[string]string{"x-amz-checksum-sha256": base64.StdEncoding.EncodeToString(checksum)}}
	if conditional {
		options.SetMatchETagExcept("*")
	}
	s := b.state()
	_, e = s.client.PutObject(op, s.config.data().bucket, key, body, length, options)
	return storageError(e)
}

type storageBody struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (r *storageBody) Read(p []byte) (int, error) {
	n, e := r.body.Read(p)
	if e != nil && e != io.EOF {
		e = storageError(e)
	}
	return n, e
}
func (r *storageBody) Close() error {
	var e error
	r.once.Do(func() { r.cancel(); e = r.body.Close() })
	return storageError(e)
}
func (b *Backend) get(ctx context.Context, key string, size int64, rangeValue *oc.ResolvedRange) (io.ReadCloser, error) {
	op, cancel := context.WithTimeout(ctx, 15*time.Minute)
	options := minio.GetObjectOptions{}
	if rangeValue != nil {
		_ = options.SetRange(int64(rangeValue.Offset), int64(rangeValue.Offset+rangeValue.Length)-1)
	}
	s := b.state()
	body, info, headers, e := (minio.Core{Client: s.client}).GetObject(op, s.config.data().bucket, key, options)
	if e != nil {
		cancel()
		return nil, storageError(e)
	}
	expected := size
	if rangeValue != nil {
		expected = int64(rangeValue.Length)
		contentRange := "bytes " + rangeValue.Offset.String() + "-" + strconv.FormatInt(int64(rangeValue.Offset+rangeValue.Length)-1, 10) + "/" + rangeValue.Total.String()
		if headers.Get("Content-Range") != contentRange {
			cancel()
			_ = body.Close()
			return nil, failure(foundation.ObjectIntegrityMismatch, nil)
		}
	} else if headers.Get("Content-Range") != "" {
		cancel()
		_ = body.Close()
		return nil, failure(foundation.ObjectIntegrityMismatch, nil)
	}
	if info.Size != expected {
		cancel()
		_ = body.Close()
		return nil, failure(foundation.ObjectIntegrityMismatch, nil)
	}
	return &storageBody{body: body, cancel: cancel}, nil
}
func (b *Backend) verify(ctx context.Context, key string, size int64, digest foundation.Digest) error {
	r, e := b.get(ctx, key, size, nil)
	if e != nil {
		return e
	}
	defer r.Close()
	h := sha256.New()
	n, e := io.CopyBuffer(h, io.LimitReader(r, size+1), make([]byte, oc.StreamBufferSize))
	if e != nil {
		return storageError(e)
	}
	if n != size || digest.String() != "sha256:"+hex.EncodeToString(h.Sum(nil)) {
		return failure(foundation.ObjectIntegrityMismatch, nil)
	}
	return nil
}
func (b *Backend) absent(ctx context.Context, key string) (bool, error) {
	s := b.state()
	_, e := s.client.StatObject(ctx, s.config.data().bucket, key, minio.StatObjectOptions{})
	if e == nil {
		return false, nil
	}
	code := minio.ToErrorResponse(e).Code
	if code == "NoSuchKey" || code == "NoSuchObject" {
		return true, nil
	}
	return false, storageError(e)
}
func (b *Backend) remove(ctx context.Context, key string) error {
	s := b.state()
	if e := s.client.RemoveObject(ctx, s.config.data().bucket, key, minio.RemoveObjectOptions{}); e != nil {
		return storageError(e)
	}
	absent, e := b.absent(ctx, key)
	if e != nil {
		return e
	}
	if !absent {
		return unavailable(nil)
	}
	return nil
}
func (b *Backend) zero(ctx context.Context, key string) error {
	zero := sha256.Sum256(nil)
	digest := foundation.Digest("sha256:" + hex.EncodeToString(zero[:]))
	if e := b.put(ctx, key, markerMediaType, 0, digest, bytes.NewReader(nil), false); e != nil {
		return e
	}
	if e := b.verify(ctx, key, 0, digest); e != nil {
		return e
	}
	s := b.state()
	info, e := s.client.StatObject(ctx, s.config.data().bucket, key, minio.StatObjectOptions{})
	if e != nil {
		return storageError(e)
	}
	if info.Size != 0 || info.ContentType != markerMediaType || len(info.UserMetadata) != 0 {
		return failure(foundation.ObjectIntegrityMismatch, nil)
	}
	return nil
}
func (b *Backend) empty(ctx context.Context) (bool, error) {
	s := b.state()
	op, cancel := context.WithCancel(ctx)
	defer cancel()
	for item := range s.client.ListObjects(op, s.config.data().bucket, minio.ListObjectsOptions{Recursive: true, MaxKeys: 1}) {
		if item.Err != nil {
			return false, storageError(item.Err)
		}
		return false, nil
	}
	return true, nil
}
func (b *Backend) readControl(ctx context.Context) (string, error) {
	s := b.state()
	op, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, info, _, e := (minio.Core{Client: s.client}).GetObject(op, s.config.data().bucket, controlKey, minio.GetObjectOptions{})
	if e != nil {
		return "", storageError(e)
	}
	defer body.Close()
	if info.Size != 36 {
		return "", failure(foundation.ObjectIntegrityMismatch, nil)
	}
	raw, e := io.ReadAll(io.LimitReader(body, 37))
	if e != nil || len(raw) != 36 {
		return "", unavailable(e)
	}
	if _, e = foundation.ParseID[struct{}](string(raw)); e != nil {
		return "", failure(foundation.ObjectIntegrityMismatch, nil)
	}
	return string(raw), nil
}
func (b *Backend) writeControl(ctx context.Context, id string) error {
	if _, e := foundation.ParseID[struct{}](id); e != nil {
		return invalid()
	}
	sum := sha256.Sum256([]byte(id))
	return b.put(ctx, controlKey, "application/x-agenteam-store-identity", int64(len(id)), foundation.Digest("sha256:"+hex.EncodeToString(sum[:])), strings.NewReader(id), true)
}
func (b *Backend) Close() error {
	s := b.state()
	s.mu.Lock()
	s.closed = true
	owned := make([]*storageConn, 0, len(s.connections))
	for c := range s.connections {
		owned = append(owned, c)
	}
	s.mu.Unlock()
	s.stop()
	s.transport.CloseIdleConnections()
	for _, c := range owned {
		_ = c.Close()
	}
	s.dials.Wait()
	return nil
}
