//go:build integration

package security_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type countedNetworkBody struct {
	io.Reader
	reads, closes atomic.Int64
}

func (b *countedNetworkBody) Read(p []byte) (int, error) { b.reads.Add(1); return b.Reader.Read(p) }
func (b *countedNetworkBody) Close() error               { b.closes.Add(1); return nil }

func TestOutboundNetworkSerializedHeaderLimitBeforeAnyRequestBytes(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	for _, connectionClose := range []bool{false, true} {
		id := f.scenario(t, netfixture.ScenarioConfig{})
		// This independent synthetic serialization measures the expected wire
		// header; it never reads the original business Body or calls GetBody.
		measure, _ := http.NewRequest("POST", f.url(id), strings.NewReader("body"))
		measure.Close = connectionClose
		measure.Header.Set("Accept-Encoding", "identity")
		measure.Header.Set("X-Pad", "")
		var encoded bytes.Buffer
		if err := measure.Write(&encoded); err != nil {
			t.Fatal(err)
		}
		base := bytes.Index(encoded.Bytes(), []byte("\r\n\r\n")) + 4
		for _, excess := range []int{0, 1} {
			body := &countedNetworkBody{Reader: strings.NewReader("body")}
			req, _ := http.NewRequest("POST", f.url(id), body)
			req.ContentLength = 4
			req.Close = connectionClose
			req.Header.Set("X-Pad", strings.Repeat("x", (64<<10)-base+excess))
			var rewinds, borrows atomic.Int64
			req.GetBody = func() (io.ReadCloser, error) { rewinds.Add(1); return io.NopCloser(strings.NewReader("body")), nil }
			ctx := httptrace.WithClientTrace(auditContext(t), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { borrows.Add(1) }})
			before := len(f.state(t, id).Requests)
			r, err := f.client.Do(ctx, req, f.profile(t, outbound.ProfileOptions{}))
			if excess == 0 {
				if err != nil {
					t.Fatal("exact complete serialized cap rejected", err)
				}
				readOutbound(t, r)
			} else {
				outboundReason(t, err, ac.ResponseLimit, false)
				if len(f.state(t, id).Requests) != before || body.reads.Load() != 0 {
					t.Fatal("over-cap header read business body or reached server")
				}
			}
			if body.closes.Load() != 1 || rewinds.Load() != 0 || borrows.Load() != 1 {
				t.Fatal("header accounting changed body/attempt ownership")
			}
		}
	}
}
