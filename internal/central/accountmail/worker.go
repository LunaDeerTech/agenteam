package accountmail

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type Dependencies struct {
	Port         c.DeliveryPort
	Registry     *WorkRegistry
	Outbound     *outbound.Client
	Trust        outbound.TrustStore
	RecoveryLog  *recoverylog.Sink
	PublicOrigin string
}
type Worker struct{ data func() *Dependencies }

func New(d Dependencies) (*Worker, error) {
	if nilPort(d.Port) || d.Registry == nil || d.Outbound == nil || d.Trust.Validate() != nil || d.RecoveryLog == nil {
		return nil, invalid()
	}
	u, e := url.Parse(d.PublicOrigin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, invalid()
	}
	d.PublicOrigin = strings.TrimSuffix(d.PublicOrigin, "/")
	return &Worker{func() *Dependencies { return &d }}, nil
}
func (w *Worker) RunJob(ctx context.Context, id c.JobID) (err error) {
	d := w.data()
	work, e := d.Registry.begin(ctx)
	if e != nil {
		return e
	}
	var attempt c.DeliveryAttempt
	var materials c.DeliveryMaterials
	out := c.DeliveryOutcome{Result: c.DeliveryCancelled, Reason: c.ReasonCancelled}
	// Installed before Claim: a successful handle is always accepted, even
	// when the caller was cancelled while Claim committed its final result.
	defer func() {
		if recover() != nil {
			// An unexpected adapter panic cannot turn an unobserved protocol
			// result into success or leak the panic's arbitrary value.
			out = c.DeliveryOutcome{Result: c.DeliveryUnknown, Reason: c.ReasonUnknown, Retryable: true}
			err = fail(foundation.InternalError, nil)
		}
		materials.Destroy()
		<-materials.Done()
		if attempt.Validate() == nil {
			completion, e := work.seal(out)
			if e == nil {
				s := work.registry
				s.mu.Lock()
				parent := context.WithoutCancel(ctx)
				if s.force != nil {
					parent = s.force
				}
				cleanup, cancel := context.WithTimeout(parent, 2*time.Second)
				work.cleanupCancel = cancel
				s.mu.Unlock()
				e = d.Port.FinishDelivery(cleanup, attempt, completion)
				cancel()
			}
			if e != nil {
				err = errors.Join(err, e)
			}
		}
		work.finish()
	}()
	attempt, e = d.Port.ClaimDelivery(work.ctx, id)
	if e != nil {
		return e
	}
	if e = work.accept(attempt); e != nil {
		return e
	}
	if work.ctx.Err() != nil {
		return fail(foundation.ShuttingDown, work.ctx.Err())
	}
	materials, e = d.Port.PrepareDelivery(work.ctx, attempt)
	if e != nil {
		out = classify(e, false)
		return e
	}
	return materials.Use(func(f c.DeliveryMaterialFields) error {
		if f.Attempt.Details().Channel == c.RecoveryLogChannel {
			out, e = w.sendLog(work.ctx, f)
		} else {
			out, e = w.sendSMTP(work.ctx, f)
		}
		return e
	})
}

type logAdmission struct {
	port     c.DeliveryPort
	attempt  c.DeliveryAttempt
	kind, id string
}

func (a logAdmission) Authorize(ctx context.Context, id recoverylog.RecordIdentity, g recoverylog.GrantOnce) error {
	if id.Purpose != a.kind || id.ResourceID != a.id || id.AttemptID != a.attempt.Details().AttemptID.String() {
		return fail(foundation.Forbidden, nil)
	}
	p, e := a.port.BeginDelivery(ctx, a.attempt, c.RecoveryLog)
	if e != nil {
		return e
	}
	defer p.Close()
	return p.GrantRecoveryLog(g.Grant)
}
func (w *Worker) link(f c.DeliveryMaterialFields) (sc.SecretMaterial, error) {
	path := "/invite"
	if f.Attempt.Details().Kind == c.ResetDelivery {
		path = "/reset-password"
	}
	var out sc.SecretMaterial
	e := f.Token.Use(func(b []byte) error {
		var e error
		out, e = sc.NewSecretMaterial(append([]byte(w.data().PublicOrigin+path+"#"+f.LinkID+"."), b...))
		return e
	})
	return out, e
}
func (w *Worker) sendLog(ctx context.Context, f c.DeliveryMaterialFields) (c.DeliveryOutcome, error) {
	link, e := w.link(f)
	if e != nil {
		return classify(e, false), e
	}
	defer link.Destroy()
	id := f.Attempt.Details()
	admission := logAdmission{w.data().Port, f.Attempt, string(id.Kind), f.LinkID}
	var ticket recoverylog.WriteTicket
	if id.Kind == c.InvitationDelivery {
		record, err := recoverylog.NewInvitationRecord(f.LinkID, id.AttemptID.String(), f.Recipient, link)
		e = err
		if e == nil {
			ticket, e = w.data().RecoveryLog.SubmitInvitation(ctx, record, admission)
		}
	} else if id.Kind == c.ResetDelivery {
		record, err := recoverylog.NewResetRecord(f.LinkID, id.AttemptID.String(), f.Recipient, link)
		e = err
		if e == nil {
			ticket, e = w.data().RecoveryLog.SubmitReset(ctx, record, admission)
		}
	} else {
		e = invalid()
	}
	if e != nil {
		return classify(e, false), e
	}
	result, e := ticket.Wait(ctx)
	if e != nil {
		ticket.Cancel()
	}
	// No timeout masquerades as join. The registry remains active if a file
	// Write/Sync cannot be stopped, even after a force caller has returned.
	<-ticket.Done()
	result, e = ticket.Wait(context.Background())
	switch result.State {
	case recoverylog.Written:
		return c.DeliveryOutcome{Result: c.DeliverySent, Reason: c.ReasonSent}, nil
	case recoverylog.Unknown:
		return c.DeliveryOutcome{Result: c.DeliveryUnknown, Reason: c.ReasonUnknown, Retryable: true}, e
	default:
		return classify(e, false), e
	}
}
func classify(e error, uncertain bool) c.DeliveryOutcome {
	if uncertain {
		return c.DeliveryOutcome{Result: c.DeliveryUnknown, Reason: c.ReasonUnknown, Retryable: true}
	}
	if errors.Is(e, context.Canceled) {
		return c.DeliveryOutcome{Result: c.DeliveryCancelled, Reason: c.ReasonCancelled}
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonTimeout, Retryable: true}
	}
	var f *foundation.Fault
	if errors.As(e, &f) {
		switch f.Code {
		case foundation.ResourceDeleted, foundation.SessionRevoked, foundation.Unauthenticated:
			return c.DeliveryOutcome{Result: c.DeliveryCancelled, Reason: c.ReasonTokenInvalid}
		case foundation.InvalidArgument, foundation.InvalidState, foundation.Forbidden:
			return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonConfiguration}
		}
	}
	return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonNetwork, Retryable: true}
}
