package recoverylog

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const testID = "01900000-0000-7000-8000-000000000001"
const testAttempt = "01900000-0000-7000-8000-000000000002"

func testRecord(t *testing.T) BootstrapRecord {
	t.Helper()
	m, e := sc.NewSecretMaterial([]byte("a-private-24-byte-value!"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	r, e := NewBootstrapRecord(testID, testAttempt, m)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func closeSink(t *testing.T, s *Sink) {
	t.Helper()
	s.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := s.Drain(ctx); e != nil {
		t.Fatal(e)
	}
	if !s.Joined() {
		t.Fatal("unjoined")
	}
}
func TestPrivateFileTypedLinesAndNoOrdinaryProjection(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "recovery.jsonl")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	r := testRecord(t)
	out, e := s.WriteBootstrap(context.Background(), r)
	if e != nil || out.State != Written {
		t.Fatal(out, e)
	}
	link := "https://app.example.com/invite#" + testID + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	m, _ := sc.NewSecretMaterial([]byte(link))
	defer m.Destroy()
	invite, e := NewInvitationRecord(testID, testAttempt, "user@example.com", m)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := s.WriteInvitation(context.Background(), invite); e != nil || r.State != Written {
		t.Fatal(r, e)
	}
	closeSink(t, s)
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Count(b, []byte("\n")) != 2 {
		t.Fatal("line count")
	}
	var w map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if e := json.Unmarshal(line, &w); e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Contains(b, []byte("a-private-24-byte-value!")) || !bytes.Contains(b, []byte(link)) {
		t.Fatal("restricted material absent")
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("mode")
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	closeSink(t, s)
	again, _ := os.ReadFile(path)
	if !bytes.Equal(again, b) {
		t.Fatal("open truncated existing log")
	}
	var normal bytes.Buffer
	for _, v := range []any{r, invite, s, struct{ r BootstrapRecord }{r}} {
		fmt.Fprintf(&normal, "%+v %#v %s", v, v, v)
		b, _ := json.Marshal(v)
		normal.Write(b)
		slog.New(slog.NewTextHandler(&normal, nil)).Info("safe", "v", v)
	}
	for _, secret := range []string{"a-private-24-byte-value!", link, "user@example.com", path} {
		if strings.Contains(normal.String(), secret) {
			t.Fatal("ordinary projection leak")
		}
	}
}
func TestPathAndPayloadRejects(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	target := filepath.Join(dir, "target")
	if e := os.WriteFile(target, nil, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"relative", link, dir} {
		if s, e := Open(p); e == nil {
			closeSink(t, s)
			t.Fatal("bad path accepted")
		}
	}
	_ = os.Chmod(target, 0644)
	if s, e := Open(target); e == nil {
		closeSink(t, s)
		t.Fatal("insecure file")
	}
	_ = os.Chmod(dir, 0755)
	if s, e := Open(filepath.Join(dir, "new")); e == nil {
		closeSink(t, s)
		t.Fatal("insecure directory")
	}
	writer := &observedWriter{}
	s := newSink(writer)
	for _, value := range []string{"https://app.example.com/invite?token=secret", "https://app.example.com/api/reset#" + testID + ".bad", "http://public.example/invite#" + testID + ".bad"} {
		m, _ := sc.NewSecretMaterial([]byte(value))
		r, _ := NewInvitationRecord(testID, testAttempt, "user@example.com", m)
		if got, e := s.WriteInvitation(context.Background(), r); e == nil || got.State != NotWritten {
			t.Fatal("unsafe URL reached sink")
		}
		m.Destroy()
	}
	closeSink(t, s)
	if writer.writes != 0 {
		t.Fatal("invalid payload performed IO")
	}
}

type observedWriter struct {
	mu                                                                               sync.Mutex
	writes, closes                                                                   int
	writeEntered, writeRelease, syncEntered, syncRelease, closeEntered, closeRelease chan struct{}
	short                                                                            bool
	syncErr                                                                          error
}

func (w *observedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	w.mu.Unlock()
	if w.writeEntered != nil {
		close(w.writeEntered)
		<-w.writeRelease
	}
	if w.short {
		return len(p) / 2, nil
	}
	return len(p), nil
}
func (w *observedWriter) Sync() error {
	if w.syncEntered != nil {
		close(w.syncEntered)
		<-w.syncRelease
	}
	return w.syncErr
}
func (w *observedWriter) Close() error {
	w.mu.Lock()
	w.closes++
	w.mu.Unlock()
	if w.closeEntered != nil {
		close(w.closeEntered)
		<-w.closeRelease
	}
	return nil
}
func TestSyncAndCloseAreJoinedUnderOneForceBudget(t *testing.T) {
	w := &observedWriter{syncEntered: make(chan struct{}), syncRelease: make(chan struct{}), closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	s := newSink(w)
	result := make(chan outcome, 1)
	go func() { r, e := s.WriteBootstrap(context.Background(), testRecord(t)); result <- outcome{r, e} }()
	<-w.syncEntered
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	start := time.Now()
	if e := s.Force(ctx); e == nil || time.Since(start) > 200*time.Millisecond || s.Joined() {
		t.Fatal("blocked I/O was not bounded/truthfully joined")
	}
	<-w.closeEntered
	// Finishing Close alone never proves the still-blocked Sync joined.
	close(w.closeRelease)
	if s.Joined() {
		t.Fatal("close falsely retired write")
	}
	close(w.syncRelease)
	o := <-result
	if o.err != nil || o.result.State != Written {
		t.Fatal(o)
	}
	ctx2, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if e := s.Drain(ctx2); e != nil || !s.Joined() {
		t.Fatal(e)
	}
	if e := s.Force(ctx2); e != nil {
		t.Fatal(e)
	}
	if w.closes != 1 {
		t.Fatal("repeated force reopened close budget")
	}
}
func TestQueueBoundAndStopPreserveActualActiveWork(t *testing.T) {
	w := &observedWriter{writeEntered: make(chan struct{}), writeRelease: make(chan struct{})}
	s := newSink(w)
	r := testRecord(t)
	done := make(chan outcome, 34)
	go func() { r, e := s.WriteBootstrap(context.Background(), r); done <- outcome{r, e} }()
	<-w.writeEntered
	for range 32 {
		go func() { r, e := s.WriteBootstrap(context.Background(), r); done <- outcome{r, e} }()
	}
	deadline := time.Now().Add(time.Second)
	for len(s.data().queue) < 32 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(s.data().queue) != 32 {
		t.Fatal("queue did not fill")
	}
	if r, e := s.WriteBootstrap(context.Background(), r); r.State != NotWritten || e == nil {
		t.Fatal("overflow")
	}
	s.StopAdmission()
	if s.Joined() {
		t.Fatal("active write discarded")
	}
	close(w.writeRelease)
	written := 0
	for range 33 {
		r := <-done
		if r.result.State == Written {
			written++
		}
	}
	if written != 1 {
		t.Fatal("queued work sent after stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := s.Drain(ctx); e != nil {
		t.Fatal(e)
	}
	if w.writes != 1 {
		t.Fatal("unadmitted IO")
	}
}
func TestPartialOrSyncFailureIsUnknownAndSafe(t *testing.T) {
	for _, w := range []*observedWriter{{short: true}, {syncErr: errors.New("private path and body")}} {
		s := newSink(w)
		r, e := s.WriteBootstrap(context.Background(), testRecord(t))
		if r.State != Unknown || e == nil {
			t.Fatal("false written")
		}
		if strings.Contains(fmt.Sprintf("%+v", struct{ e error }{e}), "private path") {
			t.Fatal("cause leaked")
		}
		closeSink(t, s)
	}
}
