package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// integrityReader owns a fixed working buffer and a 64 KiB tail. Until actual
// EOF and length/digest validation succeed, it never releases that tail. A bad
// small object produces no first byte; a bad large object cannot deliver its
// complete advertised Content-Length before reporting the failure.
type integrityReader struct {
	mu          sync.Mutex
	source      io.ReadCloser
	expected    int64
	digest      foundation.Digest // empty for a bounded range, never a partial SHA claim
	hash        hash.Hash
	read        int64
	buffer      []byte
	position    int
	eof         bool
	closed      bool
	finished    bool
	err         error
	release     func() error
	stop        func() bool
	monitorDone chan struct{}
	monitorOnce sync.Once
}

func newIntegrityReader(ctx context.Context, source io.ReadCloser, size int64, digest foundation.Digest, release func() error) (*integrityReader, error) {
	if nilPort(source) || size < 0 || digest != "" && digest.Validate() != nil || release == nil {
		return nil, invalid()
	}
	r := &integrityReader{source: source, expected: size, digest: digest, hash: sha256.New(), buffer: make([]byte, 0, 2*oc.StreamBufferSize), release: release, monitorDone: make(chan struct{})}
	initialized := make(chan struct{})
	r.stop = context.AfterFunc(ctx, func() {
		<-initialized
		r.closeWithError(unavailable(ctx.Err()))
		r.monitorOnce.Do(func() { close(r.monitorDone) })
	})
	close(initialized)
	r.mu.Lock()
	for int64(len(r.buffer)) < min(size, int64(oc.StreamBufferSize)) && r.err == nil {
		r.fill(min(int64(oc.StreamBufferSize)-int64(len(r.buffer)), size-r.read))
	}
	if size <= oc.StreamBufferSize && r.err == nil && !r.eof {
		r.finishAtEOF()
	}
	err := r.err
	r.mu.Unlock()
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
func (r *integrityReader) fill(limit int64) {
	if limit <= 0 {
		r.finishAtEOF()
		return
	}
	if r.position > 0 {
		copy(r.buffer, r.buffer[r.position:])
		r.buffer = r.buffer[:len(r.buffer)-r.position]
		r.position = 0
	}
	start := len(r.buffer)
	space := min(int64(cap(r.buffer)-start), limit)
	if space <= 0 {
		r.fail(failure(foundation.ObjectIntegrityMismatch, nil))
		return
	}
	r.buffer = r.buffer[:start+int(space)]
	n, e := r.source.Read(r.buffer[start:])
	r.buffer = r.buffer[:start+n]
	if n > 0 {
		_, _ = r.hash.Write(r.buffer[start:])
		r.read += int64(n)
	}
	if r.read > r.expected {
		r.fail(failure(foundation.ObjectIntegrityMismatch, nil))
		return
	}
	if e == io.EOF {
		if r.read != r.expected {
			r.fail(failure(foundation.ObjectIntegrityMismatch, nil))
			return
		}
		r.verifyEOF()
		return
	}
	if e != nil {
		r.fail(storageError(e))
		return
	}
	if n == 0 {
		r.fail(unavailable(io.ErrNoProgress))
		return
	}
	if r.read == r.expected {
		r.finishAtEOF()
	}
}
func (r *integrityReader) finishAtEOF() {
	var extra [1]byte
	n, e := r.source.Read(extra[:])
	if n != 0 || e == io.EOF && r.read != r.expected {
		r.fail(failure(foundation.ObjectIntegrityMismatch, nil))
		return
	}
	if e != io.EOF {
		if e == nil {
			e = io.ErrNoProgress
		}
		r.fail(storageError(e))
		return
	}
	r.verifyEOF()
}
func (r *integrityReader) verifyEOF() {
	if r.digest != "" && r.digest.String() != "sha256:"+hex.EncodeToString(r.hash.Sum(nil)) {
		r.fail(failure(foundation.ObjectIntegrityMismatch, nil))
		return
	}
	r.eof = true
	r.finishSource()
}
func (r *integrityReader) finishSource() {
	if r.finished {
		return
	}
	r.finished = true
	if r.stop() {
		r.monitorOnce.Do(func() { close(r.monitorDone) })
	}
	// Close the source before the release callback. Holding mu proves no Read
	// of that source remains in progress, including one unblocked by cancel.
	closeErr := r.source.Close()
	releaseErr := r.release()
	if r.err == nil {
		if closeErr != nil {
			r.err = storageError(closeErr)
		} else if releaseErr != nil {
			r.err = releaseErr
		}
	}
	if r.err != nil {
		r.buffer = r.buffer[:0]
		r.position = 0
	}
}
func (r *integrityReader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
	r.buffer = r.buffer[:0]
	r.position = 0
	r.finishSource()
}
func (r *integrityReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return 0, r.err
	}
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	for !r.eof && len(r.buffer)-r.position <= oc.StreamBufferSize && r.err == nil {
		r.fill(min(int64(oc.StreamBufferSize), r.expected-r.read))
	}
	if r.err != nil {
		return 0, r.err
	}
	ready := len(r.buffer) - r.position
	if !r.eof {
		ready -= oc.StreamBufferSize
	}
	if ready == 0 && r.eof {
		return 0, io.EOF
	}
	n := copy(p, r.buffer[r.position:r.position+ready])
	r.position += n
	return n, nil
}
func (r *integrityReader) closeWithError(err error) {
	// Source Close precedes waiting for the in-flight Read lock. Network/body
	// cancellation therefore cannot deadlock behind a blocked source.Read.
	_ = r.source.Close()
	r.mu.Lock()
	r.closed = true
	if r.err == nil && err != nil {
		r.err = err
	}
	r.buffer = r.buffer[:0]
	r.position = 0
	r.finishSource()
	r.mu.Unlock()
}
func (r *integrityReader) Close() error {
	r.closeWithError(nil)
	<-r.monitorDone
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}
