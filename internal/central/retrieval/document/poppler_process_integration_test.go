//go:build integration && linux

package document

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
)

// Exact supplied Linux package executables, not PATH-discovered alternatives.
func popplerTestConfig(t *testing.T) PopplerConfig {
	t.Helper()
	if os.Getenv("AGENTEAM_POPPLER_PROCESS_TEST") != "1" {
		t.Skip("explicit locked Poppler process test required")
	}
	return PopplerConfig{PDFToTextPath: "/usr/bin/pdftotext", PDFToTextSHA256: "7fe3559cac3ba61dd93650a1ba90499b9fc488184ef8d0f2f1305ddf52c0378b", PrlimitPath: "/usr/bin/prlimit", PrlimitSHA256: "06275e616ca2b721482aeb564260489be3fdc6d5f86836b4a78e96485845f060"}
}
func TestPopplerActualPDFAndPinnedVersion(t *testing.T) {
	c := popplerTestConfig(t)
	p, err := NewPoppler(context.Background(), c)
	if err != nil {
		t.Fatal("locked native parser binding", err)
	}
	registry, err := NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry.ParseBytes(context.Background(), pdfSource(t), "Two-page public fixture", PDFMediaType, PopplerRegistryProfile, twoPagePDF())
	if err != nil {
		t.Fatal("actual PDF extraction", err)
	}
	chunks, err := chunker.Split(context.Background(), out, chunker.Config{SoftBytes: 128, HardBytes: 256})
	if err != nil || len(chunks) != 2 || chunks[0].Locator.Page != 1 || chunks[1].Locator.Page != 2 || !strings.Contains(chunks[0].RawText, "Mercury") || !strings.Contains(chunks[1].RawText, "88 Earth days") {
		t.Fatal("actual PDF page/content mismatch", err)
	}
	if _, err = p.Parse(context.Background(), pdfSource(t), "malformed", []byte("not a PDF")); err == nil {
		t.Fatal("malformed PDF accepted")
	}
	c.PDFToTextSHA256 = strings.Repeat("a", 64)
	if _, err = NewPoppler(context.Background(), c); err == nil {
		t.Fatal("changed binary identity accepted")
	}
}
func twoPagePDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for _, text := range []string{"Mercury orbital period", "88 Earth days"} {
		stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET\n"
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	}
	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for n, obj := range objects {
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(out.String())
}
func TestPopplerOriginalProcessCancellationAndOutputLimit(t *testing.T) {
	c := popplerTestConfig(t)
	if _, err := NewPoppler(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "original.pid")
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		// This test-only helper execs one sleep process; production always uses
		// the verified pdftotext executable and never accepts shell arguments.
		_, _, err := runPopplerProcess(run, c, "/bin/sh", []string{"-c", `printf '%s' "$$" > "$1"; exec /usr/bin/sleep 60`, "helper", pidFile}, nil, 1024, 2*time.Second)
		returned <- err
	}()
	deadline := time.Now().Add(time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(raw))
			if pid > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancel result", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("original Wait did not return")
	}
	if pid <= 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("cancel returned before original PID was reaped")
	}
	_, _, err := runPopplerProcess(context.Background(), c, "/usr/bin/yes", []string{"public fixture output"}, nil, 1024, time.Second)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.PayloadTooLarge {
		t.Fatal("output bound did not cancel and join original", err)
	}
}
