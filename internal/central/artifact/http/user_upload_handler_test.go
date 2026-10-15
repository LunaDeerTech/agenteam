package artifacthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const uploadJSON = `{"name":"file.bin","media_type":"application/octet-stream","byte_size":"4"}`

type uploadCall struct {
	action string
	v      ac.Invocation
	meta   f.CommandMeta
	input  ac.UserUploadIntent
	media  string
	length int64
	bytes  []byte
}

// Adapter controls only; real Session/Owner/PG/Object evidence is in App tests.
type testUserUploads struct {
	calls  []uploadCall
	result ac.UserUploadResult
	meta   ac.Metadata
	err    error
	write  func(context.Context, io.ReadCloser)
}

func (p *testUserUploads) record(action string, v ac.Invocation, m f.CommandMeta, input ac.UserUploadIntent) (ac.UserUploadResult, error) {
	p.calls = append(p.calls, uploadCall{action: action, v: v, meta: m, input: input})
	return p.result, p.err
}
func (p *testUserUploads) BeginUserUpload(_ context.Context, v ac.Invocation, m f.CommandMeta, in ac.UserUploadIntent) (ac.UserUploadResult, error) {
	return p.record("begin", v, m, in)
}
func (p *testUserUploads) LookupUserUpload(_ context.Context, v ac.Invocation, m f.CommandMeta, in ac.UserUploadIntent) (ac.UserUploadResult, error) {
	return p.record("lookup", v, m, in)
}
func (p *testUserUploads) CompleteUserUpload(_ context.Context, v ac.Invocation, m f.CommandMeta, in ac.UserUploadIntent) (ac.Metadata, error) {
	_, _ = p.record("complete", v, m, in)
	return p.meta, p.err
}
func (p *testUserUploads) CancelUserUpload(_ context.Context, v ac.Invocation, m f.CommandMeta, in ac.UserUploadIntent) (ac.UserUploadResult, error) {
	return p.record("cancel", v, m, in)
}
func (p *testUserUploads) WriteUserUpload(ctx context.Context, v ac.Invocation, m f.CommandMeta, media string, length int64, body io.ReadCloser) (ac.UserUploadResult, error) {
	if p.write != nil {
		p.write(ctx, body)
	}
	raw, err := io.ReadAll(body)
	closeErr := body.Close()
	p.calls = append(p.calls, uploadCall{action: "payload", v: v, meta: m, media: media, length: length, bytes: raw})
	if err != nil {
		return ac.UserUploadResult{}, err
	}
	if closeErr != nil {
		return ac.UserUploadResult{}, closeErr
	}
	return p.result, p.err
}

func uploadMetadata(t *testing.T) ac.Metadata {
	return metadataWith(t, func(d *ac.MetadataDetails) {
		d.Kind, d.Name, d.Description = ac.UserUpload, "file.bin", ""
		d.Object.MediaType, d.Object.ByteSize = "application/octet-stream", 4
	})
}
func uploadHandler(t *testing.T) (*handler, *testBoundary, *testUserUploads) {
	t.Helper()
	h, b, _ := testHandler()
	p := &testUserUploads{meta: uploadMetadata(t), result: ac.UserUploadResult{State: ac.UserUploadPending}}
	h.uploads = p
	return h, b, p
}
func uploadPath(action string) string { return testPath(false) + "/upload-commands/" + action }
func uploadRequest(action, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, uploadPath(action), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-upload-key")
	if action == "payload" {
		r.Method = http.MethodPut
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	return r
}

func TestUserUploadHTTPFiveStagesAndBinaryIdentity(t *testing.T) {
	h, b, p := uploadHandler(t)
	for _, action := range []string{"begin", "lookup", "complete", "cancel", "payload"} {
		if !HandlesPath(uploadPath(action)) {
			t.Fatal("installed App would miss the upload stage", action)
		}
		p.result.State = ac.UserUploadPending
		body := uploadJSON
		if action == "cancel" {
			p.result.State = ac.UserUploadCancelled
		}
		if action == "payload" {
			body = string([]byte{0xff, 0, 0x80, '\n'})
			p.result.State = ac.UserUploadUploaded
		}
		w := commandResponse(t, h, uploadRequest(action, body))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("stage rejected", action, w.Code)
		}
		call := p.calls[len(p.calls)-1]
		d := call.v.Details()
		if call.action != action || !d.Actor.Equal(testActor()) || d.ProjectID != testReference().ProjectID || d.AttemptID.Validate() != nil || d.ExecutionID != "" || d.ToolID != "" || call.meta.IdempotencyKey != "original-upload-key" || call.meta.RequestID.Validate() != nil {
			t.Fatal("stage lost original Human/intent")
		}
		if action == "payload" {
			if !bytes.Equal(call.bytes, []byte(body)) || call.length != 4 || call.media != "application/octet-stream" {
				t.Fatal("raw binary was decoded or rewritten")
			}
		} else if call.input.Display.Name != "file.bin" || call.input.ByteSize != 4 || call.input.MediaType != "application/octet-stream" {
			t.Fatal("full command intent changed")
		}
		if strings.Contains(w.Body.String(), p.meta.Details().Object.ID.String()) || strings.Contains(w.Body.String(), "original-upload-key") {
			t.Fatal("private storage/key projection leaked")
		}
	}
	if b.auths != 5 || b.checks != 5 {
		t.Fatal("each stage needs original current boundary")
	}
}

func TestUserUploadHTTPUnknownAndCurrentBoundaryNeverReturnArtifact(t *testing.T) {
	for _, action := range []string{"begin", "lookup", "complete", "cancel", "payload"} {
		h, _, p := uploadHandler(t)
		p.err = f.NewFault(f.CommitUnknown, f.Unknown)
		body := uploadJSON
		if action == "payload" {
			body = "data"
		}
		w := commandResponse(t, h, uploadRequest(action, body))
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &problem)
		if w.Code == 200 || problem.Code != f.CommitUnknown || len(p.calls) != 1 || strings.Contains(w.Body.String(), "artifact_ref") {
			t.Fatal("unknown transformed into success or internal retry", action, w.Code)
		}
	}
	for _, fault := range []f.Code{f.Unauthenticated, f.CSRFFailed, f.OriginDenied} {
		h, b, p := uploadHandler(t)
		b.auth = f.NewFault(fault, f.NotStarted)
		w := commandResponse(t, h, uploadRequest("payload", "data"))
		if w.Code == 200 || len(p.calls) != 0 {
			t.Fatal("auth bypass", fault)
		}
	}
}

func TestUserUploadHTTPPayloadRetainsEarlierDeadlineAndActualReturn(t *testing.T) {
	h, _, p := uploadHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached, release := make(chan struct{}), make(chan struct{})
	p.write = func(ctx context.Context, _ io.ReadCloser) {
		if until, ok := ctx.Deadline(); !ok || time.Until(until) > userUploadPayloadBudget {
			t.Error("missing bounded upload deadline")
		}
		close(reached)
		<-ctx.Done()
		<-release
	}
	done := make(chan bool, 1)
	r := uploadRequest("payload", "data").WithContext(ctx)
	go func() { done <- serveTest(h, r, newTestWriter()) }()
	<-reached
	cancel()
	select {
	case <-done:
		t.Fatal("HTTP completion substituted cancellation for original return")
	default:
	}
	close(release)
	select {
	case aborted := <-done:
		if !aborted {
			t.Fatal("cancelled upload emitted success")
		}
	case <-time.After(time.Second):
		t.Fatal("original upload did not return")
	}
}
