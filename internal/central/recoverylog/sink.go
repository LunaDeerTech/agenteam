// Package recoverylog is the restricted, explicitly authorized recovery
// channel. It is deliberately unrelated to slog or arbitrary text logging.
package recoverylog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type State string

const (
	Written    State = "written"
	NotWritten State = "not_written"
	Unknown    State = "unknown"
)

type Result struct {
	State State `json:"state"`
}
type recordData struct {
	purpose, id, attempt, recipient string
	material                        sc.SecretMaterial
}
type BootstrapRecord struct{ data func() recordData }
type InvitationRecord struct{ data func() recordData }
type ResetRecord struct{ data func() recordData }

func validID(s string) bool { _, e := foundation.ParseID[struct{}](s); return e == nil }
func record(purpose, id, attempt, recipient string, material sc.SecretMaterial) (recordData, error) {
	if !validID(id) || !validID(attempt) || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
		return recordData{}, bad()
	}
	a, e := mail.ParseAddress(recipient)
	if e != nil || a.Name != "" || a.Address != recipient {
		return recordData{}, bad()
	}
	return recordData{purpose, id, attempt, recipient, material}, nil
}
func NewBootstrapRecord(userID, attemptID string, password sc.SecretMaterial) (BootstrapRecord, error) {
	d, e := record("bootstrap", userID, attemptID, "admin@mail.com", password)
	if e != nil {
		return BootstrapRecord{}, e
	}
	return BootstrapRecord{func() recordData { return d }}, nil
}
func NewInvitationRecord(invitationID, attemptID, email string, link sc.SecretMaterial) (InvitationRecord, error) {
	d, e := record("invitation", invitationID, attemptID, email, link)
	if e != nil {
		return InvitationRecord{}, e
	}
	return InvitationRecord{func() recordData { return d }}, nil
}
func NewResetRecord(resetID, attemptID, email string, link sc.SecretMaterial) (ResetRecord, error) {
	d, e := record("password_reset", resetID, attemptID, email, link)
	if e != nil {
		return ResetRecord{}, e
	}
	return ResetRecord{func() recordData { return d }}, nil
}
func (r BootstrapRecord) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "bootstrap_recovery_record")
}
func (r BootstrapRecord) MarshalJSON() ([]byte, error) {
	return []byte(`"bootstrap_recovery_record"`), nil
}
func (*BootstrapRecord) UnmarshalJSON([]byte) error { return bad() }
func (r BootstrapRecord) LogValue() slog.Value      { return slog.StringValue("bootstrap_recovery_record") }
func (r InvitationRecord) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "invitation_recovery_record")
}
func (r InvitationRecord) MarshalJSON() ([]byte, error) {
	return []byte(`"invitation_recovery_record"`), nil
}
func (*InvitationRecord) UnmarshalJSON([]byte) error { return bad() }
func (r InvitationRecord) LogValue() slog.Value {
	return slog.StringValue("invitation_recovery_record")
}
func (r ResetRecord) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "reset_recovery_record") }
func (r ResetRecord) MarshalJSON() ([]byte, error) { return []byte(`"reset_recovery_record"`), nil }
func (*ResetRecord) UnmarshalJSON([]byte) error    { return bad() }
func (r ResetRecord) LogValue() slog.Value         { return slog.StringValue("reset_recovery_record") }
func bad() error                                   { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
func failure(code foundation.Code, e error) error {
	f := foundation.NewFault(code, foundation.NotStarted)
	if e != nil {
		return f.WithCause(e)
	}
	return f
}

type fileWriter interface {
	io.Writer
	Sync() error
	Close() error
}
type work struct {
	ctx                             context.Context
	record                          recordData
	done                            chan struct{}
	out                             outcome
	mu                              sync.Mutex
	admission                       FirstWriteAdmission
	authorizing, granted, cancelled bool
	cancel                          context.CancelFunc
	stop                            func() bool
}
type outcome struct {
	result Result
	err    error
}
type sinkState struct {
	mu         sync.Mutex
	stopped    bool
	works      map[*work]bool
	writer     fileWriter
	queue      chan *work
	workerDone chan struct{}
	closeStart chan struct{}
	closeDone  chan struct{}
	closeOnce  sync.Once
	closeErr   error
}
type Sink struct{ data func() *sinkState }

func secureInfo(info os.FileInfo, mode os.FileMode) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid() && info.Mode().Perm() == mode
}
func Open(path string) (*Sink, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, bad()
	}
	parent := filepath.Dir(path)
	resolved, e := filepath.EvalSymlinks(parent)
	if e != nil || resolved != parent {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	info, e := os.Lstat(parent)
	if e != nil || !info.IsDir() || !secureInfo(info, 0700) {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	parentInfo := info
	if info, e = os.Lstat(path); e == nil {
		if !info.Mode().IsRegular() || !secureInfo(info, 0600) {
			return nil, failure(foundation.DependencyUnavailable, nil)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	root, e := os.OpenRoot(parent)
	if e != nil {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	defer root.Close()
	opened, e := root.Stat(".")
	if e != nil || !os.SameFile(opened, parentInfo) {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	f, e := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	info, e = f.Stat()
	if e != nil || !info.Mode().IsRegular() || !secureInfo(info, 0600) {
		_ = f.Close()
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	current, e := root.Lstat(filepath.Base(path))
	if e != nil || !os.SameFile(info, current) {
		_ = f.Close()
		return nil, failure(foundation.DependencyUnavailable, e)
	}
	return newSink(f), nil
}
func newSink(w fileWriter) *Sink {
	s := &sinkState{writer: w, works: map[*work]bool{}, queue: make(chan *work, 32), workerDone: make(chan struct{}), closeStart: make(chan struct{}), closeDone: make(chan struct{})}
	sink := &Sink{func() *sinkState { return s }}
	go func() {
		<-s.closeStart
		err := s.writer.Close()
		s.mu.Lock()
		s.closeErr = err
		s.mu.Unlock()
		close(s.closeDone)
	}()
	go s.run()
	return sink
}
func (s *sinkState) startClose() { s.closeOnce.Do(func() { close(s.closeStart) }) }
func (s *sinkState) run() {
	defer close(s.workerDone)
	defer s.startClose()
	for w := range s.queue {
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped || w.ctx.Err() != nil {
			s.complete(w, Result{NotWritten}, failure(foundation.ShuttingDown, w.ctx.Err()))
			continue
		}
		result, err := s.write(w)
		s.complete(w, result, err)
	}
}
func (s *sinkState) complete(w *work, result Result, err error) {
	w.stop()
	w.cancel()
	w.record.material.Destroy()
	w.mu.Lock()
	w.authorizing = false
	w.out = outcome{result, err}
	w.mu.Unlock()
	s.mu.Lock()
	delete(s.works, w)
	s.mu.Unlock()
	close(w.done)
}
func (s *sinkState) write(w *work) (result Result, err error) {
	result = Result{NotWritten}
	defer func() {
		if recover() != nil {
			w.mu.Lock()
			granted := w.granted
			w.authorizing = false
			w.mu.Unlock()
			if granted || result.State != NotWritten {
				result.State = Unknown
			}
			err = failure(foundation.InternalError, nil)
		}
	}()
	d := w.record
	err = d.material.Use(func(secret []byte) error {
		if d.purpose == "bootstrap" {
			if len(secret) != 24 {
				return bad()
			}
			for _, b := range secret {
				if b < 33 || b > 126 {
					return bad()
				}
			}
		} else {
			u, e := url.Parse(string(secret))
			if e != nil || u.User != nil || u.RawQuery != "" || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())) || u.Path != "/invite" && u.Path != "/reset-password" || len(secret) > 2048 {
				return bad()
			}
			if d.purpose == "invitation" && u.Path != "/invite" || d.purpose == "password_reset" && u.Path != "/reset-password" {
				return bad()
			}
			pieces := strings.Split(u.Fragment, ".")
			if len(pieces) != 2 || !validID(pieces[0]) {
				return bad()
			}
			token, e := base64.RawURLEncoding.Strict().DecodeString(pieces[1])
			if e != nil || len(token) != 32 || base64.RawURLEncoding.EncodeToString(token) != pieces[1] {
				clear(token)
				return bad()
			}
			clear(token)
		}
		line, e := json.Marshal(struct {
			Version  int                `json:"version"`
			Purpose  string             `json:"purpose"`
			At       foundation.Instant `json:"at"`
			ID       string             `json:"id"`
			Attempt  string             `json:"attempt_id"`
			Email    string             `json:"email"`
			Password string             `json:"initial_password,omitempty"`
			URL      string             `json:"url,omitempty"`
		}{Version: 1, Purpose: d.purpose, At: now(), ID: d.id, Attempt: d.attempt, Email: d.recipient, Password: passwordField(d.purpose, secret), URL: urlField(d.purpose, secret)})
		if e != nil || len(line)+1 > 4096 {
			clear(line)
			return bad()
		}
		line = append(line, '\n')
		defer clear(line)
		if e = w.admit(); e != nil {
			return e
		}
		result.State = Unknown
		n, e := s.writer.Write(line)
		if e != nil {
			return failure(foundation.DependencyUnavailable, e)
		}
		if n != len(line) {
			return failure(foundation.DependencyUnavailable, io.ErrShortWrite)
		}
		if e = s.writer.Sync(); e != nil {
			return failure(foundation.DependencyUnavailable, e)
		}
		result.State = Written
		return nil
	})
	if err != nil {
		var f *foundation.Fault
		if !errors.As(err, &f) {
			err = failure(foundation.DependencyUnavailable, err)
		}
	}
	return result, err
}
func now() foundation.Instant { i, _ := foundation.NewInstant(time.Now()); return i }
func passwordField(p string, b []byte) string {
	if p == "bootstrap" {
		return string(b)
	}
	return ""
}
func urlField(p string, b []byte) string {
	if p != "bootstrap" {
		return string(b)
	}
	return ""
}
func (s *Sink) enqueue(ctx context.Context, d recordData, admission FirstWriteAdmission) (WriteTicket, error) {
	if s == nil || s.data == nil {
		return WriteTicket{}, bad()
	}
	if e := ctx.Err(); e != nil {
		return WriteTicket{}, failure(foundation.DependencyUnavailable, e)
	}
	var copy sc.SecretMaterial
	e := d.material.Use(func(b []byte) error {
		if len(b) > 2048 {
			return bad()
		}
		var e error
		copy, e = sc.NewSecretMaterial(b)
		return e
	})
	if e != nil {
		return WriteTicket{}, failure(foundation.DependencyUnavailable, e)
	}
	d.material = copy
	workCtx, cancel := context.WithCancel(ctx)
	w := &work{ctx: workCtx, cancel: cancel, record: d, done: make(chan struct{}), admission: admission}
	w.stop = context.AfterFunc(ctx, w.cancelBeforeGrant)
	st := s.data()
	st.mu.Lock()
	if st.stopped {
		st.mu.Unlock()
		copy.Destroy()
		w.stop()
		w.cancel()
		return WriteTicket{}, failure(foundation.ShuttingDown, nil)
	}
	st.works[w] = true
	select {
	case st.queue <- w:
		st.mu.Unlock()
	default:
		delete(st.works, w)
		st.mu.Unlock()
		copy.Destroy()
		w.stop()
		w.cancel()
		return WriteTicket{}, failure(foundation.RateLimited, nil)
	}
	return WriteTicket{func() *work { return w }}, nil
}
func (s *Sink) submit(ctx context.Context, d recordData) (Result, error) {
	t, e := s.enqueue(ctx, d, nil)
	if e != nil {
		return Result{NotWritten}, e
	}
	return t.Wait(ctx)
}
func (s *Sink) WriteBootstrap(ctx context.Context, r BootstrapRecord) (Result, error) {
	if r.data == nil {
		return Result{NotWritten}, bad()
	}
	return s.submit(ctx, r.data())
}
func (s *Sink) WriteInvitation(ctx context.Context, r InvitationRecord) (Result, error) {
	if r.data == nil {
		return Result{NotWritten}, bad()
	}
	return s.submit(ctx, r.data())
}
func (s *Sink) WriteReset(ctx context.Context, r ResetRecord) (Result, error) {
	if r.data == nil {
		return Result{NotWritten}, bad()
	}
	return s.submit(ctx, r.data())
}
func (s *Sink) StopAdmission() {
	st := s.data()
	st.mu.Lock()
	if !st.stopped {
		st.stopped = true
		for w := range st.works {
			w.cancelBeforeGrant()
		}
		close(st.queue)
	}
	st.mu.Unlock()
}
func (s *Sink) Drain(ctx context.Context) error {
	st := s.data()
	for _, done := range []<-chan struct{}{st.workerDone, st.closeDone} {
		select {
		case <-done:
		case <-ctx.Done():
			return failure(foundation.DependencyUnavailable, ctx.Err())
		}
	}
	st.mu.Lock()
	err := st.closeErr
	st.mu.Unlock()
	if err != nil {
		return failure(foundation.DependencyUnavailable, err)
	}
	return nil
}
func (s *Sink) Force(ctx context.Context) error {
	s.StopAdmission()
	s.data().startClose()
	return s.Drain(ctx)
}
func (s *Sink) Joined() bool {
	st := s.data()
	select {
	case <-st.workerDone:
	default:
		return false
	}
	select {
	case <-st.closeDone:
		return true
	default:
		return false
	}
}
func (s Sink) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "recovery_log_sink") }
func (s Sink) MarshalJSON() ([]byte, error) { return []byte(`"recovery_log_sink"`), nil }
func (s Sink) LogValue() slog.Value         { return slog.StringValue("recovery_log_sink") }
