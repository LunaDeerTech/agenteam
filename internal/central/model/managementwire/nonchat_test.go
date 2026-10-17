package managementwire

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const validRerank = `{"results":[{"index":1,"relevance_score":12.5}]}`
const validImage = `{"data":[{"b64_json":"aW1hZ2U="}]}`

func TestNonchatParsersRejectUntrustedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		valid                bool
	}{
		{"rerank-positive", rerankProtocol, validRerank, true},
		{"rerank-negative", rerankProtocol, `{"results":[{"index":0,"relevance_score":-7}]}`, true},
		{"rerank-empty", rerankProtocol, `{"results":[]}`, false},
		{"rerank-duplicate", rerankProtocol, `{"results":[{"index":1,"relevance_score":1},{"index":1,"relevance_score":2}]}`, false},
		{"rerank-index-range", rerankProtocol, `{"results":[{"index":2,"relevance_score":1}]}`, false},
		{"rerank-index-missing", rerankProtocol, `{"results":[{"relevance_score":1}]}`, false},
		{"rerank-score-null", rerankProtocol, `{"results":[{"index":0,"relevance_score":null}]}`, false},
		{"rerank-score-overflow", rerankProtocol, `{"results":[{"index":0,"relevance_score":1e999}]}`, false},
		{"rerank-score-string", rerankProtocol, `{"results":[{"index":0,"relevance_score":"1"}]}`, false},
		{"image-base64", imageProtocol, validImage, true},
		{"image-url", imageProtocol, `{"data":[{"url":"https://image.example/result.png?signature=opaque"}]}`, true},
		{"image-empty", imageProtocol, `{"data":[]}`, false},
		{"image-multiple", imageProtocol, `{"data":[{"b64_json":"aW1hZ2U="},{"b64_json":"aW1hZ2U="}]}`, false},
		{"image-missing-content", imageProtocol, `{"data":[{"b64_json":null,"url":null}]}`, false},
		{"image-invalid-base64", imageProtocol, `{"data":[{"b64_json":"not base64!"}]}`, false},
		{"image-invalid-padding", imageProtocol, `{"data":[{"b64_json":"aW1hZ2V="}]}`, false},
		{"image-http-url", imageProtocol, `{"data":[{"url":"http://image.example/result.png"}]}`, false},
		{"image-url-userinfo", imageProtocol, `{"data":[{"url":"https://user:secret@image.example/result.png"}]}`, false},
		{"image-url-fragment", imageProtocol, `{"data":[{"url":"https://image.example/result.png#hidden"}]}`, false},
		{"image-relative-url", imageProtocol, `{"data":[{"url":"/result.png"}]}`, false},
		{"image-wrong-payload", imageProtocol, validEmbedding, false},
		{"rerank-wrong-payload", rerankProtocol, validImage, false},
		{"unknown-protocol", "unrecognized", validEmbedding, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := parse(context.Background(), Request{Action: "test", Protocol: tc.protocol}, []byte(tc.body))
			if (result.Reason == "") != tc.valid || result.Items != nil {
				t.Fatalf("unexpected safe probe result: reason=%s", result.Reason)
			}
		})
	}
}

func ownedManagementTLS(t *testing.T, nonce string, handler http.Handler) (*httptest.Server, outbound.TrustStore) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "owned-management-" + nonce},
		DNSNames: []string{"fixture.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "owned-ca.pem")
	if err := os.WriteFile(root, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, trust
}

func TestNonchatRunOwnedTLSFixedRequestsAndNoImageFetch(t *testing.T) {
	nonce := rand.Text()
	secret := "owned-management-" + nonce
	material, err := sc.NewSecretMaterial([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var secondary atomic.Int32
	imageTarget := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { secondary.Add(1) }))
	defer imageTarget.Close()
	var requests atomic.Int32
	var reply atomic.Value
	reply.Store(validRerank)
	server, trust := ownedManagementTLS(t, nonce, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("fixed method or exact-origin credential binding changed")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		var actual map[string]any
		if err != nil || len(body) > 4096 || json.Unmarshal(body, &actual) != nil {
			t.Error("invalid probe request body")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var expected map[string]any
		switch r.URL.EscapedPath() {
		case "/v1%2Fowned/rerank":
			expected = map[string]any{"model": "custom-model", "query": "Which document mentions a cat?", "documents": []any{"A cat is resting.", "A tree is growing."}, "top_n": float64(1), "return_documents": false}
		case "/v1%2Fowned/images/generations":
			expected = map[string]any{"model": "custom-model", "prompt": "A plain blue circle on a white background.", "n": float64(1)}
		default:
			t.Error("incorrect escaped provider prefix or nonchat endpoint")
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Error("fixed nonchat probe added or changed request fields")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply.Load().(string))
	}))
	transport, call, endpoint := wireFixture(t, server, true)
	transport.Trust = trust
	urlBody, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"url": imageTarget.URL + "/" + nonce + "?signature=" + secret}}})
	largeImage, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 1<<20+1)))}}})
	for _, tc := range []struct {
		name, protocol, body, reason string
	}{
		{"rerank", rerankProtocol, validRerank, ""},
		{"rerank-invalid", rerankProtocol, `{"results":[{"index":-1,"relevance_score":0.5}]}`, "invalid_response"},
		{"image-base64", imageProtocol, validImage, ""},
		{"image-over-one-mib", imageProtocol, string(largeImage), ""},
		{"image-url", imageProtocol, string(urlBody), ""},
		{"image-invalid", imageProtocol, `{"data":[{"b64_json":"!"}]}`, "invalid_response"},
		{"image-limit", imageProtocol, strings.Repeat(" ", imageTestLimit+1), "response_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply.Store(tc.body)
			before := requests.Load()
			result, err := transport.Run(context.Background(), Request{Action: "test", Protocol: tc.protocol, Endpoint: endpoint, Model: "custom-model"}, &material, call)
			if err != nil || result.Reason != tc.reason || result.Items != nil || requests.Load() != before+1 {
				t.Fatal("controlled nonchat probe failed or retried", result.Reason, err)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), secret) || strings.Contains(string(raw), imageTarget.URL) || strings.Contains(string(raw), "aW1hZ2U=") {
				t.Fatal("vendor body escaped the safe management result")
			}
		})
	}
	if secondary.Load() != 0 {
		t.Fatal("image response caused a second outbound fetch")
	}
	before := requests.Load()
	for _, input := range []Request{
		{Action: "test", Protocol: "unknown", Endpoint: endpoint, Model: "custom-model"},
		{Action: "discovery", Protocol: rerankProtocol, Endpoint: endpoint},
		{Action: "discovery", Protocol: imageProtocol, Endpoint: endpoint},
	} {
		if request, _, err := prepare(input); err == nil || request != nil {
			t.Fatal("unsupported protocol operation prepared a request")
		}
		result, err := transport.Run(context.Background(), input, &material, call)
		if err != nil || result.Reason != "unsupported_protocol" || requests.Load() != before {
			t.Fatal("unsupported protocol operation performed network I/O")
		}
	}
}

func TestImageDecodedLimitAndBudget(t *testing.T) {
	encodedLimit := strings.Repeat("A", base64.StdEncoding.EncodedLen(imageDecodedLimit))
	if !validImageBase64(encodedLimit) || validImageBase64(encodedLimit+"AAAA") || validImageBase64(encodedLimit[:len(encodedLimit)-1]+"!") {
		t.Fatal("decoded image limit or whole-input base64 validation changed")
	}
	for _, protocol := range []string{chatProtocol, embedProtocol, rerankProtocol, imageProtocol} {
		input := Request{Action: "test", Protocol: protocol}
		wantOverall, wantHeaders, wantIdle := 20*time.Second, 15*time.Second, 5*time.Second
		limit := testLimit
		if protocol == imageProtocol {
			wantOverall, wantHeaders, wantIdle = 120*time.Second, 120*time.Second, 30*time.Second
			limit = imageTestLimit
		}
		actual := probeLimits(input, limit)
		if Timeout(input) != wantOverall || actual.Overall != wantOverall || actual.ResponseHeaders != wantHeaders || actual.ReadIdle != wantIdle || actual.ResponseBodyBytes != int64(limit) {
			t.Fatal("per-protocol operation or wire budget changed")
		}
	}
	if Timeout(Request{Action: "discovery", Protocol: imageProtocol}) != 20*time.Second {
		t.Fatal("unsupported image discovery acquired a generation budget")
	}
}

func TestImageRunHonorsEarlierCallerDeadline(t *testing.T) {
	entered := make(chan struct{})
	server, trust := ownedManagementTLS(t, rand.Text(), http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		close(entered)
		<-r.Context().Done()
	}))
	transport, call, endpoint := wireFixture(t, server, true)
	transport.Trust = trust
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := transport.Run(ctx, Request{Action: "test", Protocol: imageProtocol, Endpoint: endpoint, Model: "custom-model"}, nil, call)
	if err != nil || result.Reason != "timeout" || time.Since(started) > 2*time.Second {
		t.Fatal("image budget extended the caller deadline", result.Reason, err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("deadline test did not reach the owned TLS handler")
	}
}
