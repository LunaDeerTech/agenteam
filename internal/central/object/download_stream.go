package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type DownloadStream struct{ data func() *downloadStream }
type downloadStream struct {
	owner           *Downloads
	actor           identity.Actor
	claims          downloadClaims
	attempt         oc.DownloadAttemptID
	ctx             context.Context
	cancel          context.CancelFunc
	finish          func()
	reader          *oc.ObjectReader
	offset, length  int64
	partial         bool
	mu              sync.Mutex
	running, closed bool
	done            chan struct{}
	result          DownloadResult
	err             error
}

func (DownloadStream) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_download_stream") }
func (DownloadStream) MarshalJSON() ([]byte, error) { return []byte(`"object_download_stream"`), nil }
func (*DownloadStream) UnmarshalJSON([]byte) error  { return invalid() }
func (DownloadStream) LogValue() slog.Value         { return slog.StringValue("object_download_stream") }

// OpenDownload confirms started Audit before opening storage. Range syntax is
// considered only after the current provider has authorized the exact grant.
// The returned stream is single-use and must be StreamHTTP'd or closed.
func (d *Downloads) OpenDownload(ctx context.Context, actor identity.Actor, token, rangeHeader string) (*DownloadStream, error) {
	if err := downloadHuman(actor); err != nil {
		return nil, err
	}
	r := d.state()
	c, err := r.keys.verifyDownload(token)
	if err != nil {
		return nil, err
	}
	if c.user.String() != actor.Details().UserID || !time.Now().Before(c.expires.Time()) {
		return nil, failure(foundation.Forbidden, nil)
	}
	op, finish, err := r.objects.begin(ctx)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			finish()
		}
	}()
	ctx, cancel := context.WithCancel(op.ctx)
	defer func() {
		if !keep {
			cancel()
		}
	}()
	attempt, err := foundation.NewID[oc.DownloadAttempt]()
	if err != nil {
		return nil, unavailable(err)
	}
	work, err := r.objects.newProjectWork(ctx, workProject(c.target.Details().Source.Details().Owner), "download", attempt.String(), c.target.Details().Source.Details().Meta.ID)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, projectWorkContextKey{}, work)
	var offset, length int64
	var partial bool
	checked := d.within(ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		if err := checkGrant(ctx, e, c, true); err != nil {
			return err
		}
		var err error
		offset, length, partial, err = parseDownloadRange(rangeHeader, int64(c.target.Details().Source.Details().Meta.ByteSize))
		return err
	})
	if err = commitError(checked); err != nil {
		if checked.State() == foundation.NotCommitted && hasCode(err, foundation.RangeNotSatisfiable) {
			total := int64(c.target.Details().Source.Details().Meta.ByteSize)
			return nil, &downloadRangeError{func() int64 { return total }}
		}
		return nil, err
	}
	if err = d.start(ctx, actor, c, attempt, offset, length); err != nil {
		return nil, err
	}
	state := &downloadStream{owner: d, actor: actor, claims: c, attempt: attempt, ctx: ctx, cancel: cancel, finish: finish, offset: offset, length: length, partial: partial, done: make(chan struct{})}
	var requested *oc.ByteRange
	m := c.target.Details().Source.Details().Meta
	if offset != 0 || length != int64(m.ByteSize) {
		requested = &oc.ByteRange{Offset: offset, Length: length}
	}
	state.reader, err = r.objects.ReadObject(ctx, actor, c.target.Details().Source.Details().Owner, m.ID, requested)
	if err != nil {
		state.finalize(0, downloadReadFailure(err), err)
		state.seal()
		return nil, err
	}
	actual := state.reader.Meta()
	if actual.ID != m.ID || !actual.Scope.Equal(m.Scope) || actual.Version != m.Version || actual.ByteSize != m.ByteSize || actual.SHA256 != m.SHA256 || actual.MediaType != m.MediaType || !actual.CreatedAt.Time().Equal(m.CreatedAt.Time()) {
		err = failure(foundation.ObjectIntegrityMismatch, nil)
		state.finalize(0, oc.DownloadIntegrityFailed, err)
		state.seal()
		return nil, err
	}
	stream := &DownloadStream{func() *downloadStream { return state }}
	keep = true
	// Even a handle never handed to an HTTP writer is tracked and closed on
	// cancellation/Force. Its technical checkpoint/Audit uses the shared cleanup
	// budget and the operation is finished only after that work joins.
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-state.done:
		}
	}()
	return stream, nil
}

func parseDownloadRange(raw string, total int64) (offset, length int64, partial bool, err error) {
	if raw == "" {
		return 0, total, false, nil
	}
	bad := func() (int64, int64, bool, error) { return 0, 0, false, failure(foundation.RangeNotSatisfiable, nil) }
	if len(raw) > 128 || !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return bad()
	}
	parts := strings.Split(raw[6:], "-")
	if len(parts) != 2 || total <= 0 {
		return bad()
	}
	parse := func(value string) (int64, error) {
		if value == "" {
			return 0, errors.New("range")
		}
		for _, v := range []byte(value) {
			if v < '0' || v > '9' {
				return 0, errors.New("range")
			}
		}
		return strconv.ParseInt(value, 10, 64)
	}
	if parts[0] == "" {
		suffix, e := parse(parts[1])
		if e != nil || suffix == 0 {
			return bad()
		}
		length = min(suffix, total)
		return total - length, length, true, nil
	}
	start, e := parse(parts[0])
	if e != nil || start >= total {
		return bad()
	}
	end := total - 1
	if parts[1] != "" {
		end, e = parse(parts[1])
		if e != nil || end < start {
			return bad()
		}
		end = min(end, total-1)
	}
	return start, end - start + 1, true, nil
}
func (s *DownloadStream) Close() error {
	if s == nil || s.data == nil {
		return nil
	}
	r := s.data()
	r.mu.Lock()
	if r.closed {
		err := r.err
		r.mu.Unlock()
		return err
	}
	if r.running {
		r.cancel()
		done := r.done
		r.mu.Unlock()
		<-done
		r.mu.Lock()
		err := r.err
		r.mu.Unlock()
		return err
	}
	r.running = true
	r.mu.Unlock()
	r.cancel()
	r.finalize(0, oc.DownloadCancelled, context.Canceled)
	r.seal()
	r.mu.Lock()
	err := r.err
	r.mu.Unlock()
	return err
}

// StreamHTTP writes exactly one response. On an error after response headers,
// the authenticated handler must abort the HTTP stream (net/http's
// ErrAbortHandler); it must not append a Problem JSON or replay the payload.
// A real deadline-capable ResponseWriter is required so Force can interrupt a
// blocked write. No generic unbounded io.Writer or replaceable transport exists.
func (s *DownloadStream) StreamHTTP(w http.ResponseWriter) (DownloadResult, error) {
	if s == nil || s.data == nil || nilPort(w) {
		return DownloadResult{}, invalid()
	}
	r := s.data()
	r.mu.Lock()
	if r.running || r.closed {
		r.mu.Unlock()
		return DownloadResult{}, failure(foundation.InvalidState, nil)
	}
	r.running = true
	r.mu.Unlock()
	defer r.seal()
	controller := http.NewResponseController(w)
	deadline, _ := r.ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil {
		r.finalize(0, oc.DownloadWriteFailed, unavailable(err))
		return r.outcome()
	}
	cancelled := make(chan struct{})
	stop := context.AfterFunc(r.ctx, func() { _ = controller.SetWriteDeadline(time.Now()); _ = r.reader.Close(); close(cancelled) })
	defer func() {
		if !stop() {
			<-cancelled
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	prefix := make([]byte, min(r.length, 512))
	n, err := io.ReadFull(r.reader, prefix)
	if err != nil {
		r.finalize(0, downloadReadFailure(err), err)
		return r.outcome()
	}
	prefix = prefix[:n]
	if r.length == 0 {
		var b [1]byte
		n, err := r.reader.Read(b[:])
		if n != 0 || err != io.EOF {
			if err == nil || err == io.EOF {
				err = failure(foundation.ObjectIntegrityMismatch, nil)
			}
			r.finalize(0, downloadReadFailure(err), err)
			return r.outcome()
		}
	}
	if err = r.ctx.Err(); err != nil {
		r.finalize(0, oc.DownloadCancelled, err)
		return r.outcome()
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Accept-Ranges", "bytes")
	media, disposition := downloadPresentation(r.claims.mode, prefix, r.offset)
	h.Set("Content-Type", media)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": r.claims.target.Details().Filename}))
	h.Set("Content-Length", strconv.FormatInt(r.length, 10))
	status := http.StatusOK
	if r.partial {
		status = http.StatusPartialContent
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", r.offset, r.offset+r.length-1, r.claims.target.Details().Source.Details().Meta.ByteSize))
	} else {
		h.Del("Content-Range")
	}
	w.WriteHeader(status)
	sent := int64(0)
	write := func(b []byte) error {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		sent += int64(n)
		if err != nil {
			return err
		}
		if n != len(b) {
			return io.ErrShortWrite
		}
		return nil
	}
	if len(prefix) > 0 {
		err = write(prefix)
		if err != nil {
			reason := oc.DownloadWriteFailed
			if r.ctx.Err() != nil {
				reason = oc.DownloadCancelled
			}
			r.finalize(sent, reason, err)
			return r.outcome()
		}
	}
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := r.reader.Read(buffer)
		if n > 0 {
			if sent+int64(n) > r.length {
				r.finalize(sent, oc.DownloadIntegrityFailed, failure(foundation.ObjectIntegrityMismatch, nil))
				return r.outcome()
			}
			if err = write(buffer[:n]); err != nil {
				reason := oc.DownloadWriteFailed
				if r.ctx.Err() != nil {
					reason = oc.DownloadCancelled
				}
				r.finalize(sent, reason, err)
				return r.outcome()
			}
		}
		if readErr == io.EOF {
			if sent != r.length {
				r.finalize(sent, oc.DownloadIntegrityFailed, failure(foundation.ObjectIntegrityMismatch, nil))
			} else {
				r.finalize(sent, "", nil)
			}
			return r.outcome()
		}
		if readErr != nil {
			r.finalize(sent, downloadReadFailure(readErr), readErr)
			return r.outcome()
		}
		if r.ctx.Err() != nil {
			r.finalize(sent, oc.DownloadCancelled, r.ctx.Err())
			return r.outcome()
		}
	}
}
func downloadPresentation(mode oc.DownloadMode, prefix []byte, offset int64) (string, string) {
	if mode != oc.DownloadPreview || offset != 0 {
		return "application/octet-stream", "attachment"
	}
	detected := http.DetectContentType(prefix)
	switch detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected, "inline"
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(prefix)), "\xef\xbb\xbf"))
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "<") || strings.Contains(lower, "<!doctype") || strings.Contains(lower, "<script") {
		return "application/octet-stream", "attachment"
	}
	end := len(prefix)
	for end > 0 && !utf8.Valid(prefix[:end]) && len(prefix)-end < 4 {
		end--
	}
	if detected == "text/plain; charset=utf-8" && utf8.Valid(prefix[:end]) {
		return "text/plain; charset=utf-8", "inline"
	}
	return "application/octet-stream", "attachment"
}
func downloadReadFailure(err error) oc.DownloadFailure {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return oc.DownloadCancelled
	}
	if hasCode(err, foundation.ObjectIntegrityMismatch) {
		return oc.DownloadIntegrityFailed
	}
	return oc.DownloadReadFailed
}
func (r *downloadStream) finalize(sent int64, reason oc.DownloadFailure, streamErr error) {
	if r.reader != nil {
		if err := r.reader.Close(); streamErr == nil && err != nil {
			streamErr = err
			reason = downloadReadFailure(err)
		}
	}
	event := oc.DownloadEvent{GrantID: r.claims.grant, AttemptID: r.attempt, Phase: oc.DownloadSent, Length: foundation.Progress(r.length), SentBytes: foundation.Progress(sent)}
	if streamErr != nil || reason != "" {
		event.Phase = oc.DownloadFailed
		event.Failure = reason
		if event.Failure == "" {
			event.Failure = oc.DownloadReadFailed
		}
	}
	ctx, finished := r.owner.state().objects.cleanupContext()
	// Retain the admitted operation identity while using the cleanup context's
	// shared deadline. First StopAdmission must not reject its final Audit; Force
	// still bounds this context and the operation stays tracked until seal.
	if op, ok := r.ctx.Value(operationKey{}).(*operation); ok {
		ctx = context.WithValue(ctx, operationKey{}, op)
	}
	auditErr := r.owner.checkpoint(ctx, r.actor, r.claims, event)
	if auditErr == nil {
		auditErr = r.owner.complete(ctx, r.actor, r.claims, event)
	}
	out := resultFromDownload(event)
	out.AuditPending = auditErr != nil
	var err error
	if streamErr != nil {
		err = portError(streamErr)
	} else if auditErr != nil {
		err = portError(auditErr)
	}
	r.mu.Lock()
	r.result = out
	r.err = err
	r.mu.Unlock()
	finished()
	r.cancel()
}
func (r *downloadStream) seal() {
	r.mu.Lock()
	r.closed = true
	close(r.done)
	r.mu.Unlock()
	r.finish()
}
func (r *downloadStream) outcome() (DownloadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result, r.err
}
