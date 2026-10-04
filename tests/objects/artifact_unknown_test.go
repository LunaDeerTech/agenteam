//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type artifactCommitArm struct {
	au.Appender
	proxy *commitProxy
	once  sync.Once
}

func (a *artifactCommitArm) AppendInTx(ctx context.Context, tx foundation.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
	r, err := a.Appender.AppendInTx(ctx, tx, e, k)
	if err == nil && e.Fields().Action == au.ArtifactCreate {
		a.once.Do(func() { a.proxy.armed.Store(1) })
	}
	return r, err
}

type sourceCommitArm struct {
	oc.SourceReads
	proxy *commitProxy
	once  sync.Once
}

func (a *sourceCommitArm) AcquireSourceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, source oc.ResolvedSource, p oc.AccessLockPlan, l oc.LockedAccess) (oc.SourceLease, error) {
	lease, err := a.SourceReads.AcquireSourceInTx(ctx, tx, actor, source, p, l)
	if err == nil {
		a.once.Do(func() { a.proxy.armed.Store(1) })
	}
	return lease, err
}
func TestArtifactSourceCommandUnknownAndOriginalFactRecovery(t *testing.T) {
	for _, phase := range []string{"source_capture", "publication"} {
		t.Run(phase, func(t *testing.T) {
			f := newArtifactFixture(t)
			original := f.create(t, "original", "text/plain", strings.Repeat("s", 100000))
			ref, _ := original.Details().Reference.BusinessFile()
			store, proxy := proxyStore(t, f.fixture, false)
			proxy.late.Store(true)
			var auditWrap func(au.Appender) au.Appender
			var sourceWrap func(oc.SourceReads) oc.SourceReads
			if phase == "publication" {
				auditWrap = func(a au.Appender) au.Appender { return &artifactCommitArm{Appender: a, proxy: proxy} }
			} else {
				sourceWrap = func(r oc.SourceReads) oc.SourceReads { return &sourceCommitArm{SourceReads: r, proxy: proxy} }
			}
			on := f.bindOn(t, store, auditWrap, sourceWrap)
			cmd := command(t, "copy-unknown")
			display := art.Display{Name: "copy.txt"}
			gets, puts := f.proxy.gets.Load(), f.proxy.puts.Load()
			done := make(chan error, 1)
			go func() {
				_, err := on.artifact.CreateFromSource(contextFor(t), on.invocation(t), cmd, display, ref)
				done <- err
			}()
			select {
			case <-proxy.reached:
			case <-time.After(3 * time.Second):
				t.Fatal("exact Artifact COMMIT not held")
			}
			select {
			case err := <-done:
				requireCode(t, err, foundation.CommitUnknown)
			case <-time.After(3 * time.Second):
				t.Fatal("lost connection did not return unknown")
			}
			if phase == "source_capture" && (f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts) {
				t.Fatal("source command unknown initiated I/O")
			}
			beforeGet, beforePut, beforeResolve := f.proxy.gets.Load(), f.proxy.puts.Load(), on.resolver.resolves.Load()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			_, err := on.artifact.CreateFromSource(ctx, on.invocation(t), cmd, display, ref)
			cancel()
			if err == nil || f.proxy.gets.Load() != beforeGet || f.proxy.puts.Load() != beforePut || on.resolver.resolves.Load() != beforeResolve {
				t.Fatal("retry speculated before original writer terminal")
			}
			close(proxy.release)
			select {
			case <-proxy.committed:
			case <-time.After(3 * time.Second):
				t.Fatal("original COMMIT did not terminate")
			}
			var commands, leases int
			err = f.store.QueryRow(contextFor(t), `SELECT count(*),count(*) FILTER(WHERE source_lease_id IS NOT NULL) FROM agenteam_artifact.commands WHERE command_key='copy-unknown'`).Scan(&commands, &leases)
			if err != nil || commands != 1 || leases != 1 {
				t.Fatal("source+command checkpoint not durable", err, commands, leases)
			}
			if phase == "source_capture" {
				foreign := f.bindOn(t, f.store, nil, nil)
				get, put := f.proxy.gets.Load(), f.proxy.puts.Load()
				_, err = foreign.artifact.CreateFromSource(contextFor(t), foreign.invocation(t), cmd, display, ref)
				requireCode(t, err, foundation.ResourceBusy)
				if f.proxy.gets.Load() != get || f.proxy.puts.Load() != put {
					t.Fatal("foreign service speculated through live source checkpoint")
				}
				var active int
				if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE id=(SELECT source_lease_id FROM agenteam_artifact.commands WHERE command_key='copy-unknown') AND state='active'`).Scan(&active); err != nil || active != 1 {
					t.Fatal("live source lease taken over", active, err)
				}
			}
			out, err := on.artifact.CreateFromSource(contextFor(t), on.invocation(t), cmd, display, ref)
			if err != nil {
				t.Fatal("recover original source", err)
			}
			if out.Details().Object.ID == original.Details().Object.ID || out.Details().Object.SHA256 != original.Details().Object.SHA256 {
				t.Fatal("source copied wrong content/identity")
			}
			if phase == "publication" && (f.proxy.gets.Load() != beforeGet || f.proxy.puts.Load() != beforePut || on.resolver.resolves.Load() != beforeResolve) {
				t.Fatal("completed unknown replay consulted source")
			}
			_, err = on.artifact.CreateFromSource(contextFor(t), on.invocation(t), cmd, art.Display{Name: "different.txt"}, ref)
			requireCode(t, err, foundation.IdempotencyKeyReused)
			var count int
			err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_artifact.artifacts`).Scan(&count)
			if err != nil || count != 2 {
				t.Fatal("unknown duplicated Artifact", count, err)
			}
			// A separately composed service has no source-handle registry. Completed
			// replay still reads only current target/command facts, even if its resolver
			// is now unavailable.
			restarted := f.bindOn(t, f.store, nil, nil)
			restarted.resolver.reject.Store(true)
			beforeGet, beforePut = f.proxy.gets.Load(), f.proxy.puts.Load()
			repeated, err := restarted.artifact.CreateFromSource(contextFor(t), restarted.invocation(t), cmd, display, ref)
			if err != nil || repeated.Details().Reference != out.Details().Reference || restarted.resolver.resolves.Load() != 0 || f.proxy.gets.Load() != beforeGet || f.proxy.puts.Load() != beforePut {
				t.Fatal("completed durable replay consulted source after recomposition", err)
			}
		})
	}
}

type downloadCommitArm struct {
	oc.DownloadProvider
	phase oc.DownloadPhase
	proxy *commitProxy
	once  sync.Once
}

func (a *downloadCommitArm) AppendDownloadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, target oc.DownloadTarget, event oc.DownloadEvent, p oc.AccessLockPlan, l oc.LockedAccess) error {
	err := a.DownloadProvider.AppendDownloadInTx(ctx, tx, actor, target, event, p, l)
	if err == nil && event.Phase == a.phase {
		a.once.Do(func() { a.proxy.armed.Store(1) })
	}
	return err
}
func TestArtifactDownloadUnknownConfirmsOriginalPhaseWithoutResending(t *testing.T) {
	for _, phase := range []oc.DownloadPhase{oc.DownloadIssued, oc.DownloadStarted, oc.DownloadSent} {
		t.Run(string(phase), func(t *testing.T) {
			f := newArtifactFixture(t)
			m := f.create(t, "download-unknown", "text/plain", strings.Repeat("v", 100000))
			store, proxy := proxyStore(t, f.fixture, true)
			on := f.bindOn(t, store, nil, nil)
			provider := &downloadCommitArm{DownloadProvider: on.sources, phase: phase, proxy: proxy}
			downloads := on.downloads(t, provider)
			ref, _ := m.Details().Reference.BusinessFile()
			before := f.proxy.gets.Load()
			if phase == oc.DownloadIssued {
				done := make(chan error, 1)
				go func() {
					value, err := downloads.IssueDownload(contextFor(t), f.actor, ref, oc.DownloadAttachment, 0)
					if err == nil {
						_, err = value.ForHuman(f.actor)
					}
					done <- err
				}()
				select {
				case <-proxy.reached:
				case <-time.After(3 * time.Second):
					t.Fatal("issued COMMIT not held")
				}
				if f.proxy.gets.Load() != before {
					t.Fatal("issued accessed object")
				}
				select {
				case <-done:
					t.Fatal("URL issued before confirmed grant")
				default:
				}
				close(proxy.release)
				select {
				case err := <-done:
					if err != nil {
						t.Fatal("issued same grant verification", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("issued verification did not finish")
				}
			} else {
				token := on.downloadToken(t, downloads, m, oc.DownloadAttachment)
				done := make(chan error, 1)
				go func() {
					resp, body, err, out := downloadHTTP(t, downloads, f.actor, token, "", 0, nil)
					if err == nil && out.err != nil {
						err = out.err
					}
					if err == nil && (resp.StatusCode != 200 || len(body) != 100000 || out.result.Phase != oc.DownloadSent || out.result.AuditPending) {
						err = io.ErrUnexpectedEOF
					}
					done <- err
				}()
				select {
				case <-proxy.reached:
				case <-time.After(3 * time.Second):
					t.Fatal("download phase COMMIT not held")
				}
				if phase == oc.DownloadStarted && f.proxy.gets.Load() != before {
					t.Fatal("started unknown read object")
				}
				close(proxy.release)
				select {
				case err := <-done:
					if err != nil {
						t.Fatal("same attempt phase verification", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("phase verification did not finish")
				}
				if f.proxy.gets.Load() != before+1 {
					t.Fatal("unknown retried GET")
				}
			}
			var grants, attempts int
			err := f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_download.grants),(SELECT count(*) FROM agenteam_download.attempts)`).Scan(&grants, &attempts)
			if err != nil || grants != 1 || phase == oc.DownloadIssued && attempts != 0 || phase != oc.DownloadIssued && attempts != 1 {
				t.Fatal("unknown duplicated logical record", err, grants, attempts)
			}
		})
	}
}
