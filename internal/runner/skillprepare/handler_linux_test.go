//go:build linux

package skillprepare

import (
	"archive/zip"
	"bytes"
	"context"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func fixture(t *testing.T) (*workspace.Manager, *Handler) {
	t.Helper()
	root, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("workspace", err, closeErr)
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("manager join", err)
		}
	})
	handler, err := New(manager)
	if err != nil {
		t.Fatal(err)
	}
	return manager, handler
}
func newID(t *testing.T) p.ID {
	t.Helper()
	id, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func packageInput(t *testing.T) ([]byte, p.SkillPreparePayload) {
	t.Helper()
	var raw bytes.Buffer
	writer := zip.NewWriter(&raw)
	files := []struct{ path, body string }{{"SKILL.md", "# Prepared\n"}, {"scripts/run.sh", "printf 'prepared-script-ok\\n'\n"}}
	manifest := p.SkillManifest{Format: p.SkillPackageFormat, EntryPath: "SKILL.md"}
	for _, file := range files {
		data := []byte(file.body)
		header := &zip.FileHeader{Name: file.path, Method: zip.Store, Flags: 0x800, CreatorVersion: 3<<8 | 20, ReaderVersion: 20, ModifiedDate: 1<<5 | 1, ExternalAttrs: uint32(0100644) << 16, CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data))}
		w, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, p.SkillFile{Path: file.path, MediaType: "text/plain", ByteSize: p.Decimal(strconv.Itoa(len(data))), SHA256: packageDigest(data)})
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	expires, _ := p.NewInstant(time.Now().Add(time.Minute))
	input := p.SkillPreparePayload{WorkspaceName: "work", SkillID: newID(t), RevisionID: newID(t), Revision: "1", ArchiveBytes: p.Decimal(strconv.Itoa(raw.Len())), ArchiveSHA256: packageDigest(raw.Bytes()), Manifest: manifest, Transfer: p.SkillTransfer{ID: newID(t), Method: http.MethodGet, Headers: http.Header{}, ExpiresAt: expires}}
	return raw.Bytes(), input
}
func request(t *testing.T, input p.SkillPreparePayload) p.Request {
	t.Helper()
	raw, err := p.EncodeSkillPreparePayload(input)
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: newID(t), OperationID: newID(t), ExecutionID: newID(t), ProjectID: newID(t), AgentID: newID(t), Mount: p.Mount{MountID: newID(t), WorkspaceID: newID(t)}, OperationName: p.SkillPrepareOperation, OperationRevision: p.SkillPrepareRevision, Payload: raw}
}
func TestSkillPrepareHTTPToActualScript(t *testing.T) {
	manager, handler := fixture(t)
	raw, input := packageInput(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "private-test-grant" {
			t.Error("missing exact private grant")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	input.Transfer.URL = server.URL
	input.Transfer.Headers.Set("Authorization", "private-test-grant")
	call := request(t, input)
	response, err := handler.Execute(context.Background(), call)
	if err != nil || response.Outcome != p.Success {
		t.Fatal("actual prepare", err, response.Outcome, response.Code)
	}
	for _, b := range call.Payload {
		if b != 0 {
			t.Fatal("original private control bytes retained")
		}
	}
	result, err := p.DecodeSkillPrepareResult(response.Payload)
	if err != nil || !result.Durable || result.Reused || requests.Load() != 1 {
		t.Fatal("publication facts", err)
	}
	command, err := runcommand.New(manager)
	if err != nil {
		t.Fatal(err)
	}
	commandCall := call
	commandCall.RequestID = newID(t)
	commandCall.OperationID = newID(t)
	commandCall.OperationName = p.RunCommandOperation
	commandCall.OperationRevision = p.RunCommandRevision
	commandCall.Payload, err = p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "work", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/sh", Args: []string{"scripts/run.sh"}, CWD: result.RelativePath}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	commandResult, err := command.Execute(context.Background(), commandCall, func(stream, data string) error {
		if stream != "stdout" {
			t.Error("unexpected stderr")
		}
		_, _ = output.WriteString(data)
		return nil
	})
	if err != nil || commandResult.Outcome != p.Success || output.String() != "prepared-script-ok\n" {
		t.Fatal("prepared script was not actually executed", err, commandResult.Outcome)
	}
	invocation, err := p.DecodeRunCommandResult(commandResult.Payload)
	if err != nil || invocation.ExitCode != 0 || invocation.Cancelled || invocation.TimedOut || !invocation.OutputComplete || !invocation.ProcessGroupTerminationConfirmed {
		t.Fatal("script process did not complete and join", err)
	}
}
func TestSkillPrepareCorruptionAndRedirectAreBounded(t *testing.T) {
	for _, mode := range []string{"corrupt", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			_, handler := fixture(t)
			raw, input := packageInput(t)
			var next atomic.Int32
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.Add(1); _, _ = w.Write(raw) }))
			defer other.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
				case "corrupt":
					bad := bytes.Clone(raw)
					bad[0] ^= 1
					_, _ = w.Write(bad)
				case "oversize":
					_, _ = w.Write(append(bytes.Clone(raw), 'x'))
				}
			}))
			defer server.Close()
			input.Transfer.URL = server.URL
			response, err := handler.Execute(context.Background(), request(t, input))
			if err != nil || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.TransferFailed || next.Load() != 0 {
				t.Fatal("bad transfer escaped closed failure", err, response.Outcome)
			}
		})
	}
}
func TestSkillPrepareCancelWaitsForDownloadAndHandle(t *testing.T) {
	_, handler := fixture(t)
	raw, input := packageInput(t)
	entered, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	input.Transfer.URL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan p.Response, 1)
	call := request(t, input)
	go func() {
		out, err := handler.Execute(ctx, call)
		if err != nil {
			t.Error(err)
		}
		returned <- out
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP not entered")
	}
	cancel()
	select {
	case out := <-returned:
		if out.Outcome != p.Cancelled {
			t.Fatal("cancel not joined", out.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original download did not return")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("original server request remained")
	}
}
