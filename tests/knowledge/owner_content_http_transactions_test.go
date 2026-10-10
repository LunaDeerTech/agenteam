//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeOwnerContentHTTPReadTransactions(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	for _, commit := range []bool{false, true} {
		name := "commit_not_forwarded"
		if commit {
			name = "commit_applied_ack_lost"
		}
		t.Run(name, func(t *testing.T) {
			x, observer, proxy := unknownPublicationFixture(t)
			v := assembleKnowledgeOwnerHTTPFixture(t, x)
			installContentHTTP(t, v, v.service)
			const text = "private original read unknown content"
			doc := contentHTTPCreate(t, v, kc.PlainText, "Unknown current read", text)
			before := v.facts(t)
			hook := &knowledgeOwnerHTTPReadStore{Store: x.raw, result: make(chan f.CommitResult, 1)}
			var originalTx f.Tx
			hook.after = func(ctx context.Context, tx f.Tx) error {
				originalTx = tx
				pid, err := knowledgeOwnerHTTPTxPID(ctx, x.raw, tx)
				if err != nil {
					return err
				}
				return proxy.Arm(pid)
			}
			service := concurrentService(t, x, hook)
			installContentHTTP(t, v, service)
			t.Cleanup(proxy.Release)
			response := v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, ""), "", "")
			problem := response.want(t, 503)
			if problem["code"] != string(f.CommitUnknown) || problem["commit_state"] != string(f.Unknown) || problem["document"] != nil || problem["text"] != nil || problem["cause_id"] != nil || bytes.Contains(response.body, []byte(text)) {
				t.Fatal("original read Unknown exposed a candidate or unsafe cause")
			}
			runtimeAwait(t, proxy.Reached())
			select {
			case original := <-hook.result:
				if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Kind() != f.RecoveryCause || original.Cause().Details().Owner != "knowledge.read" || proxy.WriterPID() <= 0 {
					t.Fatal("missing original physical read Unknown")
				}
			default:
				t.Fatal("HTTP ended without original Store outcome")
			}
			if _, err := x.raw.InTx(originalTx); err == nil {
				t.Fatal("original returned transaction remains live")
			}
			if commit {
				proxy.Release()
				runtimeAwait(t, proxy.Committed())
				runtimeAwait(t, proxy.HeldJoined())
			} else {
				unknownRollbackWriter(t, observer, proxy)
			}
			contentHTTPNoReader(t, v, doc)
			// A new current read is not confirmation or replay of the lost read.
			contentHTTPText(t, v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, ""), "", ""), doc, text, int64(len(text)), false)
			contentHTTPNoReader(t, v, doc)
			if v.facts(t) != before {
				t.Fatal("read Unknown/fresh read changed business facts")
			}
		})
	}
	t.Run("original_select_cancelled_and_transaction_retired", func(t *testing.T) {
		v := newContentHTTPFixture(t)
		doc := contentHTTPCreate(t, v, kc.PlainText, "Cancelled original read", "cancel body")
		before := v.facts(t)
		gate, release := knowledgeOwnerHTTPGate()
		defer release()
		held := make(chan int32, 1)
		writerDone := make(chan struct{})
		writerResult := make(chan f.CommitResult, 1)
		ctx, stop := context.WithCancel(knowledgeContext(t))
		defer stop()
		cause, err := f.NewRecoveryCause("knowledge.http.fixture", knowledgeID(t), "")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			defer close(writerDone)
			writerResult <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				q, err := v.raw.InTx(tx)
				if err != nil {
					return err
				}
				if _, err = q.Exec(ctx, `LOCK TABLE agenteam_knowledge.documents IN ACCESS EXCLUSIVE MODE`); err != nil {
					return err
				}
				pid, err := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
				if err != nil {
					return err
				}
				held <- pid
				return knowledgeOwnerHTTPWait(ctx, gate)
			})
		}()
		t.Cleanup(func() { stop(); release(); runtimeAwait(t, writerDone) })
		writerPID := knowledgeOwnerHTTPPID(t, held)
		attempted := make(chan int32, 1)
		hook := &knowledgeOwnerHTTPReadStore{Store: v.raw, result: make(chan f.CommitResult, 1)}
		var token f.Tx
		hook.before = func(ctx context.Context, tx f.Tx) error {
			token = tx
			pid, err := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
			if err == nil {
				attempted <- pid
			}
			return err
		}
		service := concurrentService(t, v.ownerTreeFixture, hook)
		installContentHTTP(t, v, service)
		requestCtx, cancel := context.WithCancel(knowledgeContext(t))
		defer cancel()
		replies, done := knowledgeOwnerHTTPRun(t, v, knowledgeOwnerHTTPRequest(requestCtx, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, ""), "", ""))
		readPID := knowledgeOwnerHTTPPID(t, attempted)
		deadline, cancelObserve := context.WithTimeout(knowledgeContext(t), time.Second)
		defer cancelObserve()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			var blocked bool
			err := v.raw.QueryRow(deadline, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND wait_event_type='Lock' AND wait_event='relation' AND $2::int=ANY(pg_blocking_pids(pid)))`, readPID, writerPID).Scan(&blocked)
			if err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			select {
			case <-tick.C:
			case <-deadline.Done():
				t.Fatal("original document SELECT did not reach its exact blocker")
			}
		}
		cancel()
		response := knowledgeOwnerHTTPReply(t, replies, done)
		if !response.aborted || len(response.body) != 0 {
			t.Fatal("cancelled original SELECT published content")
		}
		select {
		case original := <-hook.result:
			if original.State() != f.NotCommitted {
				t.Fatal("original cancelled read did not roll back")
			}
		default:
			t.Fatal("HTTP returned before original physical transaction")
		}
		if _, err := v.raw.InTx(token); err == nil {
			t.Fatal("ended read transaction remains usable")
		}
		release()
		knowledgeOwnerHTTPCommit(t, writerResult, writerDone)
		contentHTTPNoReader(t, v, doc)
		if v.facts(t) != before {
			t.Fatal("original cancelled read changed business facts")
		}
	})
}
