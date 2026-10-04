//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func waitTransferClock(t *testing.T, at time.Time) {
	t.Helper()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-contextFor(t).Done():
		t.Fatal("actual transfer clock deadline")
	}
}

func TestTransferOneSecondSignedWire(t *testing.T) {
	for _, direction := range []oc.TransferDirection{oc.TransferGET, oc.TransferPUT} {
		t.Run(string(direction), func(t *testing.T) {
			f := newTransferFixture(t)
			body := []byte("one second is a valid signed interval")
			var spec oc.TransferSpec
			if direction == oc.TransferGET {
				put := f.put(t, "one-second-input", string(body))
				f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
				spec, _ = oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: direction, Owner: f.owner, ObjectID: put.Meta.ID, ExpiresInSeconds: 1})
			} else {
				d := f.spec(t, body).Details()
				d.ExpiresInSeconds = 1
				spec, _ = oc.NewTransferSpec(d)
			}
			waitTransferClock(t, time.Now().Truncate(time.Second).Add(time.Second+20*time.Millisecond))
			issue := command(t, "one-second-wire")
			grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
			if err != nil {
				t.Fatal("one-second Issue failed", err)
			}
			m, err := grant.Material.ForRunner(f.runner, grant.Status.ID)
			if err != nil || m.WireExpiresAt.Time().After(grant.Status.ExpiresAt.Time()) {
				t.Fatal("invalid one-second material", err)
			}
			u, _ := url.Parse(m.URL)
			if u.Query().Get("X-Amz-Expires") != "1" {
				t.Fatal("one-second interval was widened or omitted")
			}
			status, received := transferRequest(t, f, m, body)
			if status != 200 || direction == oc.TransferGET && !bytes.Equal(received, body) {
				t.Fatal("actual one-second wire failed", status)
			}
			waitTransferClock(t, grant.Status.ExpiresAt.Time().Add(20*time.Millisecond))
			_, err = f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
			requireCode(t, err, foundation.InvalidState)
			var stored time.Time
			if err = f.store.QueryRow(contextFor(t), `SELECT expires_at FROM agenteam_object.object_transfers WHERE id=$1`, grant.Status.ID.String()).Scan(&stored); err != nil || !stored.Equal(grant.Status.ExpiresAt.Time()) {
				t.Fatal("expired replay changed durable deadline", err)
			}
			t.Log("real one-second wire accepted; expired replay rejected with original deadline unchanged")
		})
	}
}

func TestTransferCrossSecondReplayPreservesDeadline(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "cross-second-input", "bounded replay")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferGET, Owner: f.owner, ObjectID: put.Meta.ID, ExpiresInSeconds: 4})
	waitTransferClock(t, time.Now().Truncate(time.Second).Add(time.Second+20*time.Millisecond))
	issue := command(t, "cross-second-wire")
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	u, _ := url.Parse(m.URL)
	first, err := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date"))
	if err != nil {
		t.Fatal("invalid original SDK time")
	}
	firstDuration, _ := strconv.ParseInt(u.Query().Get("X-Amz-Expires"), 10, 64)
	waitTransferClock(t, first.Add(time.Second+20*time.Millisecond))
	replay, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	if err != nil || replay.Status.ID != grant.Status.ID || replay.Status.ExpiresAt != grant.Status.ExpiresAt {
		t.Fatal("cross-second replay lost original identity/deadline", err)
	}
	m, _ = replay.Material.ForRunner(f.runner, grant.Status.ID)
	u, _ = url.Parse(m.URL)
	second, _ := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date"))
	secondDuration, _ := strconv.ParseInt(u.Query().Get("X-Amz-Expires"), 10, 64)
	if !second.After(first) || secondDuration >= firstDuration || m.WireExpiresAt.Time().After(grant.Status.ExpiresAt.Time()) {
		t.Fatal("new SDK second renewed original wire deadline")
	}
	if status, body := transferRequest(t, f, m, nil); status != 200 || string(body) != "bounded replay" {
		t.Fatal("shortened actual replay rejected", status)
	}
}

func TestTransferActualSignedWireRejectsChangesAndExpiry(t *testing.T) {
	f := newTransferFixture(t)
	body := []byte("wire-bound runner output")
	spec := f.spec(t, body)
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "wire"), spec)
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	for _, mutation := range []string{"method", "path", "content-type", "length", "sha-header", "condition", "body"} {
		t.Run(mutation, func(t *testing.T) {
			m := material
			m.Headers = material.Headers.Clone()
			payload := append([]byte(nil), body...)
			switch mutation {
			case "method":
				m.Method = http.MethodPost
			case "path":
				u, _ := url.Parse(m.URL)
				u.Path += "x"
				m.URL = u.String()
			case "content-type":
				m.Headers.Set("Content-Type", "application/json")
			case "length":
				payload = append(payload, 'x')
				m.Headers.Set("Content-Length", strconv.Itoa(len(payload)))
			case "sha-header":
				m.Headers.Set("X-Amz-Checksum-Sha256", strings.Repeat("A", 43)+"=")
			case "condition":
				m.Headers.Del("If-None-Match")
			case "body":
				payload[0] ^= 1
			}
			request, e := http.NewRequestWithContext(contextFor(t), m.Method, m.URL, bytes.NewReader(payload))
			if e != nil {
				t.Fatal("invalid material")
			}
			request.Header = m.Headers
			request.ContentLength = int64(len(payload))
			response, e := (&http.Client{Transport: f.transport}).Do(request)
			if e != nil {
				t.Fatal("owned request transport failed")
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode/100 != 4 {
				t.Fatalf("wire mutation accepted: status=%d", response.StatusCode)
			}
		})
	}
	if status, _ := transferRequest(t, f, material, body); status != 200 {
		t.Fatal("valid wire rejected", status)
	}
	// Retain the known S3 boundary: deleting an existing key would let the
	// same still-valid conditional bearer PUT create it again. Production never
	// uses DELETE for an externally signed staging key.
	u, _ := url.Parse(material.URL)
	key := strings.TrimPrefix(u.Path, "/"+f.bucket+"/")
	if err = f.s3.RemoveObject(contextFor(t), f.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatal("owned boundary DELETE failed")
	}
	if status, _ := transferRequest(t, f, material, body); status != 200 {
		t.Fatal("deleted key boundary changed", status)
	}
	// A separate GET grant permits a short real expiration without altering
	// the original PUT grant's durable time or continuing its payload writes.
	put := f.put(t, "expiry-object", "get body")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	get, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferGET, Owner: f.owner, ObjectID: put.Meta.ID, ExpiresInSeconds: 3})
	short, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "short"), get)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := short.Material.ForRunner(f.runner, short.Status.ID)
	if m.WireExpiresAt.Time().After(short.Status.ExpiresAt.Time()) {
		t.Fatal("wire extends DB deadline")
	}
	timer := time.NewTimer(time.Until(m.WireExpiresAt.Time().Add(time.Second)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-contextFor(t).Done():
		t.Fatal("expiry bound")
	}
	if status, _ := transferRequest(t, f, m, nil); status/100 != 4 {
		t.Fatal("expired real signature accepted", status)
	}
	_, err = f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "short"), get)
	requireCode(t, err, foundation.InvalidState)
}

func TestTransferEmptyAndLargeSingleSnapshot(t *testing.T) {
	for _, size := range []int{0, 64 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			f := newTransferFixture(t)
			proxy := newTransferProxy(t, f.fixture)
			through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
			body := bytes.Repeat([]byte("x"), size)
			grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "size"), f.spec(t, body))
			if err != nil {
				t.Fatal(err)
			}
			material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
			if status, _ := transferRequest(t, through, material, body); status != 200 {
				t.Fatal("PUT", status)
			}
			proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
			state, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
			if err != nil || state.State != oc.TransferComplete {
				t.Fatal("complete", err)
			}
			if proxy.candidatePUT.Load() != 1 || proxy.stageGET.Load() != 2 {
				t.Fatalf("expected one source stream and one marker verification; GET=%d PUT=%d", proxy.stageGET.Load(), proxy.candidatePUT.Load())
			}
			reader, err := through.service.ReadObject(contextFor(t), f.actor, f.owner, grant.Status.ObjectID, nil)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			n, err := io.CopyBuffer(h, reader, make([]byte, 64<<10))
			closeErr := reader.Close()
			if err != nil || closeErr != nil || n != int64(size) || "sha256:"+hex.EncodeToString(h.Sum(nil)) != grant.SHA256.String() {
				t.Fatal("published candidate integrity mismatch", err, closeErr)
			}
		})
	}
}

func TestTransferSourceCorruptionNeverPublishesAndRevocationJoins(t *testing.T) {
	for _, mode := range []int32{transferCorruptStageRead, transferHoldStageRead} {
		t.Run(strconv.Itoa(int(mode)), func(t *testing.T) {
			f := newTransferFixture(t)
			proxy := newTransferProxy(t, f.fixture)
			through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
			body := bytes.Repeat([]byte("v"), 256<<10)
			grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "corruption"), f.spec(t, body))
			if err != nil {
				t.Fatal(err)
			}
			material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
			if status, _ := transferRequest(t, through, material, body); status != 200 {
				t.Fatal(status)
			}
			proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
			proxy.mode.Store(mode)
			done := make(chan error, 1)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			go func() { _, e := through.transfers.CompleteTransfer(ctx, f.actor, grant.Status.ID, proof); done <- e }()
			if mode == transferHoldStageRead {
				proxy.wait(t)
				state, e := through.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, command(t, "cancel-active-source"))
				if e != nil || state.Cleanup != oc.CleanupPending || !state.LeaseActive {
					t.Fatal("live source deletion protection", e)
				}
				if proxy.stagePUT.Load() != 1 {
					t.Fatal("marker overlapped active source reader")
				}
				cancel()
				proxy.unblock()
			}
			select {
			case err = <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("source failed to join")
			}
			if err == nil {
				t.Fatal("failed/cancelled source published")
			}
			if mode == transferCorruptStageRead {
				requireCode(t, err, foundation.ObjectIntegrityMismatch)
			}
			var available bool
			if e := f.store.QueryRow(contextFor(t), `SELECT state='available' FROM agenteam_object.objects WHERE id=$1`, grant.Status.ObjectID.String()).Scan(&available); e != nil || available || proxy.candidatePUT.Load() != 0 {
				t.Fatal("bad source emitted candidate", e)
			}
			proxy.mode.Store(transferPass)
			if mode == transferHoldStageRead {
				if err = through.transfers.Recover(contextFor(t)); err != nil {
					t.Fatal(err)
				}
				if status, _ := transferRequest(t, through, material, body); status != 412 {
					t.Fatal("joined source not fenced", status)
				}
			}
		})
	}
}
