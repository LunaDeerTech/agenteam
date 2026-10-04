//go:build integration

package accountmail_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

// The underlying port already committed and irreversibly handed responsibility
// to its caller. Only delivery of that successful return is paused here.
type claimedReturnPort struct {
	c.DeliveryPort
	claimed chan c.DeliveryAttempt
	release chan struct{}
	prepare atomic.Int32
}

func (p *claimedReturnPort) ClaimDelivery(ctx context.Context, id c.JobID) (c.DeliveryAttempt, error) {
	a, e := p.DeliveryPort.ClaimDelivery(ctx, id)
	if e == nil {
		p.claimed <- a
		<-p.release
	}
	return a, e
}
func (p *claimedReturnPort) PrepareDelivery(ctx context.Context, a c.DeliveryAttempt) (c.DeliveryMaterials, error) {
	p.prepare.Add(1)
	return p.DeliveryPort.PrepareDelivery(ctx, a)
}
func TestAccountMailSuccessfulClaimStopBeforeWorkerAcceptStillJoins(t *testing.T) {
	network := startSMTP(t)
	for _, force := range []bool{false, true} {
		name := "caller_cancel_and_stop"
		if force {
			name = "force"
		}
		t.Run(name, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none"})
			if e != nil {
				t.Fatal(e)
			}
			f := newAccount(t)
			configure(t, f, endpoint, "none", true)
			m := composeMail(t, f, network, uint16(endpoint.Port))
			port := &claimedReturnPort{DeliveryPort: m.port, claimed: make(chan c.DeliveryAttempt, 1), release: make(chan struct{})}
			trust, e := outbound.LoadTrustStore(network.CAFile)
			if e != nil {
				t.Fatal(e)
			}
			worker, e := accountmail.New(accountmail.Dependencies{Port: port, Registry: m.registry, Outbound: m.client, Trust: trust, RecoveryLog: f.sink, PublicOrigin: "https://accounts.example.test"})
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(ctxFor(t))
			defer cancel()
			done := make(chan error, 1)
			job := testJob(t, f)
			go func() { done <- worker.RunJob(ctx, job) }()
			var attempt c.DeliveryAttempt
			select {
			case attempt = <-port.claimed:
			case <-ctx.Done():
				t.Fatal("actual Claim did not return")
			}
			var joined, terminal bool
			if e = f.store.QueryRow(ctxFor(t), `SELECT io_joined,terminal FROM agenteam_account.mail_attempts WHERE id=$1`, attempt.Details().AttemptID.String()).Scan(&joined, &terminal); e != nil || joined || terminal {
				t.Fatal("claim did not retain work responsibility", joined, terminal, e)
			}
			status, e := m.port.RecoverDeliveries(ctxFor(t))
			var fault *foundation.Fault
			if !errors.As(e, &fault) || fault.Code != foundation.ResourceBusy || status.Advanced != 0 || status.Pending != 1 {
				t.Fatal("missing registration was mistaken for join", status, e)
			}
			m.registry.StopAdmission()
			stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
			if force {
				e = m.registry.Force(stopCtx)
			} else {
				cancel()
				e = m.registry.Drain(stopCtx)
			}
			stop()
			if !errors.Is(e, context.DeadlineExceeded) || m.registry.Joined() {
				t.Fatal("returned before actual worker joined", e)
			}
			close(port.release)
			select {
			case e = <-done:
				if e == nil {
					t.Fatal("cancelled handoff resumed sending")
				}
			case <-ctxFor(t).Done():
				t.Fatal("late successful handle was lost")
			}
			if !m.registry.Joined() || port.prepare.Load() != 0 {
				t.Fatal("stopped worker prepared material or failed join")
			}
			// An already expired Force budget leaves the exact completion for
			// fresh recovery; it does not renew the force deadline.
			if _, e = m.port.RecoverDeliveries(ctxFor(t)); e != nil {
				t.Fatal("joined checkpoint did not recover", e)
			}
			if e = f.store.QueryRow(ctxFor(t), `SELECT io_joined,terminal FROM agenteam_account.mail_attempts WHERE id=$1`, attempt.Details().AttemptID.String()).Scan(&joined, &terminal); e != nil || !joined || !terminal {
				t.Fatal("finally responsibility did not become terminal", joined, terminal, e)
			}
			var leases int
			if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt'`).Scan(&leases); e != nil || leases != 0 {
				t.Fatal("zero-Prepare cleanup manufactured leases", leases, e)
			}
			state, e := network.State(ctxFor(t), endpoint.ID)
			if e != nil || state.Connections != 0 || state.Auth != 0 || state.Mail != 0 {
				t.Fatal("post-stop external work", state, e)
			}
		})
	}
}
