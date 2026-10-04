package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func mailAttempt(t *testing.T, issuer DeliveryIssuer) DeliveryAttempt {
	t.Helper()
	job, _ := foundation.NewID[MailJob]()
	intent, _ := foundation.NewID[DeliveryIntent]()
	attempt, _ := foundation.NewID[Attempt]()
	process, _ := foundation.NewID[Process]()
	h := sha256.Sum256([]byte("private-source-binding"))
	a, e := NewDeliveryAttempt(issuer, DeliveryAttemptDetails{job, intent, attempt, InvitationDelivery, process, 1, 1, SMTPChannel}, foundation.Digest("sha256:"+hex.EncodeToString(h[:])))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestDeliveryCapabilitiesBindIssuerAndFullAttempt(t *testing.T) {
	issuer := NewDeliveryIssuer()
	a := mailAttempt(t, issuer)
	other := mailAttempt(t, issuer)
	completion, e := NewDeliveryCompletion(issuer, a, DeliveryOutcome{DeliverySent, ReasonSent, false})
	if e != nil {
		t.Fatal(e)
	}
	if !completion.Matches(issuer, a) || completion.Matches(NewDeliveryIssuer(), a) || completion.Matches(issuer, other) || a.IssuedBy(NewDeliveryIssuer()) {
		t.Fatal("capability substitution")
	}
	for _, out := range []DeliveryOutcome{{DeliverySent, ReasonNetwork, false}, {DeliverySent, ReasonSent, true}, {DeliveryCancelled, ReasonCancelled, true}, {DeliveryUnknown, ReasonSent, false}} {
		if out.Validate() == nil {
			t.Fatal("contradictory result")
		}
	}
	var b bytes.Buffer
	for _, v := range []any{a, completion, struct {
		attempt    DeliveryAttempt
		completion DeliveryCompletion
	}{a, completion}} {
		fmt.Fprintf(&b, "%+v %#v %s", v, v, v)
		raw, _ := json.Marshal(v)
		b.Write(raw)
		slog.New(slog.NewTextHandler(&b, nil)).Info("safe", "v", v)
	}
	if strings.Contains(b.String(), a.Binding().String()) || strings.Contains(b.String(), a.Details().JobID.String()) {
		t.Fatal("opaque capability leaked")
	}
	if json.Unmarshal([]byte(`"mail_attempt"`), &a) == nil {
		t.Fatal("JSON manufactured capability")
	}
}
func TestDeliveryMaterialsDestroyWaitsForBorrowerAndClosesAdmission(t *testing.T) {
	a := mailAttempt(t, NewDeliveryIssuer())
	token, _ := sc.NewSecretMaterial([]byte("private-material"))
	m, e := NewDeliveryMaterials(DeliveryMaterialFields{Attempt: a, Recipient: "recipient@example.test", Token: token})
	if e != nil {
		t.Fatal(e)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = m.Use(func(f DeliveryMaterialFields) error {
			close(entered)
			<-release
			return f.Token.Use(func(b []byte) error {
				if string(b) != "private-material" {
					t.Error("borrow invalidated before join")
				}
				return nil
			})
		})
	}()
	<-entered
	m.Destroy()
	m.Destroy()
	if m.Joined() {
		t.Fatal("destroy falsely joined")
	}
	if m.Use(func(DeliveryMaterialFields) error { return nil }) == nil {
		t.Fatal("new borrower after destroy")
	}
	close(release)
	<-done
	<-m.Done()
	if !m.Joined() {
		t.Fatal("actual borrower not joined")
	}
	if token.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("material retained")
	}
}
func TestSendPermitCancellationDoesNotReleaseActiveWriteEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var released atomic.Int32
	p, e := NewSendPermit(ctx, SMTPAuth, time.Now().Add(time.Second), func() { released.Add(1) })
	if e != nil {
		t.Fatal(e)
	}
	entered, leave, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.RunSMTPFirstWrite(func(c context.Context) (int, error) { close(entered); <-leave; return 1, c.Err() })
	}()
	<-entered
	cancel()
	p.Close()
	if released.Load() != 0 {
		t.Fatal("gate released before real write join")
	}
	if _, e = p.RunSMTPFirstWrite(func(context.Context) (int, error) { return 1, nil }); e == nil {
		t.Fatal("permit reused")
	}
	close(leave)
	<-done
	if released.Load() != 1 {
		t.Fatal("gate not released exactly once")
	}
	p.Close()
	if released.Load() != 1 {
		t.Fatal("double release")
	}
}
func TestSendPermitModeAndUnusedCancellation(t *testing.T) {
	released := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	p, e := NewSendPermit(ctx, RecoveryLog, time.Now().Add(time.Second), func() { close(released) })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.RunSMTPFirstWrite(func(context.Context) (int, error) { t.Error("wrong mode ran"); return 0, nil }); e == nil {
		t.Fatal("wrong phase allowed")
	}
	cancel()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("unused permit retained gate")
	}
	if p.GrantRecoveryLog(func() error { t.Error("expired grant ran"); return nil }) == nil {
		t.Fatal("expired permit allowed")
	}
}
