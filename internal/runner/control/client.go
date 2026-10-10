package control

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Options are copied at construction. A nonzero EnrollmentToken means explicit
// credential enrollment; a normal reconnect never reuses an enrollment token.
// Configuration may be omitted when the private identity file already exists.
type Options struct {
	IdentityFile     string
	Configuration    *identity.Configuration
	EnrollmentToken  p.EnrollmentToken
	EnrollmentSource func(context.Context) (p.EnrollmentToken, error)
	RunnerVersion    string
	Roots            *x509.CertPool
	Observe          func(context.Context, ConnectionState)
}

// ConnectionState is a safe public projection. Connected means the current
// authenticated connection completed its Hello, not that an operation is bound.
type ConnectionState string

const (
	Disconnected ConnectionState = "disconnected"
	Connecting   ConnectionState = "connecting"
	Connected    ConnectionState = "connected"
	Incompatible ConnectionState = "incompatible"
)

func (Options) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_control_options") }
func (Options) MarshalJSON() ([]byte, error) { return []byte(`"runner_control_options"`), nil }
func (Options) LogValue() slog.Value         { return slog.StringValue("runner_control_options") }

type ownedIdentity interface {
	Load() (identity.Identity, error)
	Save(identity.Identity) error
	Close() error
}
type deviceBackend interface {
	challenge(context.Context, p.ID) (p.ChallengeResponse, error)
	enroll(context.Context, identity.Identity, p.EnrollmentToken, string, string) (p.EnrollmentResponse, error)
	connect(context.Context, identity.Identity, p.Nonce) (*wire, error)
	close()
}

// Client owns its persistent identity lock, native device HTTP transport and all
// connection loops until Run actually returns. New does not create a file or
// open a socket. Its private seams never become a production operation registry.
type Client struct {
	mu               sync.Mutex
	options          Options
	started, stopped bool
	cancel           context.CancelFunc
	done             chan struct{}
	open             func(string) (ownedIdentity, error)
	device           func(identity.Configuration, *x509.CertPool) (deviceBackend, error)
	wait             func(context.Context, time.Duration) error
	jitter           func(time.Duration) (time.Duration, error)
}

func (*Client) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_control_client") }
func (*Client) MarshalJSON() ([]byte, error) { return []byte(`"runner_control_client"`), nil }
func (*Client) LogValue() slog.Value         { return slog.StringValue("runner_control_client") }

func New(options Options) (*Client, error) {
	if options.EnrollmentToken.Valid() && options.EnrollmentSource != nil {
		return nil, identity.ErrInvalid
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return nil, identity.ErrUnsupported
	}
	if !filepath.IsAbs(options.IdentityFile) || filepath.Clean(options.IdentityFile) != options.IdentityFile {
		return nil, identity.ErrInvalid
	}
	if options.Configuration != nil {
		v := *options.Configuration
		if v.Validate() != nil {
			return nil, identity.ErrInvalid
		}
		options.Configuration = &v
	}
	if options.Roots != nil {
		options.Roots = options.Roots.Clone()
	}
	// Validate the same Hello contract consumed by the native connection, before
	// taking ownership of private credentials. No operation or feature is offered.
	id, err := p.NewID()
	if err != nil {
		return nil, ErrTransport
	}
	if _, err = newMessage(clientHello(id, options.RunnerVersion), "", ""); err != nil {
		return nil, ErrProtocol
	}
	return &Client{options: options, done: make(chan struct{}),
		open: func(path string) (ownedIdentity, error) { return identity.Open(path) },
		device: func(c identity.Configuration, roots *x509.CertPool) (deviceBackend, error) {
			return newDeviceClient(c, roots)
		},
		wait: waitReconnect, jitter: reconnectJitter}, nil
}
func clientHello(id p.ID, version string) p.Hello {
	return p.Hello{RunnerID: id, RunnerVersion: version, ProtocolVersion: p.CurrentVersion(), OS: runtime.GOOS, Arch: runtime.GOARCH, Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
}

func (c *Client) Run(ctx context.Context) (result error) {
	if c == nil || ctx == nil {
		return identity.ErrInvalid
	}
	c.mu.Lock()
	if c.started || c.stopped {
		c.mu.Unlock()
		return ErrClosed
	}
	c.started = true
	owned, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	options := c.options
	c.options.EnrollmentToken = p.EnrollmentToken{}
	c.options.EnrollmentSource = nil
	c.mu.Unlock()
	defer func() { cancel(); close(c.done) }()
	if err := owned.Err(); err != nil {
		return err
	}
	file, err := c.open(options.IdentityFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			result = identity.ErrUnavailable
		}
	}()
	id, err := file.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && options.Configuration != nil && id.Configuration() != *options.Configuration {
		return identity.ErrInvalid
	}
	if options.EnrollmentSource != nil {
		if err != nil && options.Configuration == nil {
			return identity.ErrInvalid
		}
		token, readErr := options.EnrollmentSource(owned)
		options.EnrollmentSource = nil
		if readErr != nil {
			if owned.Err() != nil {
				return owned.Err()
			}
			return identity.ErrInvalid
		}
		if owned.Err() != nil {
			return owned.Err()
		}
		if !token.Valid() {
			return identity.ErrInvalid
		}
		options.EnrollmentToken = token
	}
	if options.EnrollmentToken.Valid() {
		configuration := options.Configuration
		if err == nil {
			stored := id.Configuration()
			configuration = &stored
		}
		if configuration == nil {
			return identity.ErrInvalid
		}
		// Explicit enrollment replaces a credential only while holding its original
		// file lock. Persist the new recoverable key before any request can commit.
		id, err = identity.NewPending(*configuration)
		if err != nil {
			return err
		}
		if err = file.Save(id); err != nil {
			return err
		}
	} else if err != nil {
		return identity.ErrInvalid
	}
	if err = owned.Err(); err != nil {
		return err
	}
	device, err := c.device(id.Configuration(), options.Roots)
	if err != nil {
		return err
	}
	defer device.close()
	if options.EnrollmentToken.Valid() {
		_, enrollErr := device.enroll(owned, id, options.EnrollmentToken, runtime.GOOS, runtime.GOARCH)
		options.EnrollmentToken = p.EnrollmentToken{}
		if err = owned.Err(); err != nil {
			return err
		}
		if enrollErr == nil {
			id, err = id.AsActive()
			if err != nil {
				return err
			}
			if err = file.Save(id); err != nil {
				return err
			}
		}
		// A rejected, lost or partial reply does not prove rollback. The original
		// pending key may only recover by a fresh challenge and actual connection.
	}
	attempts := 0
	for {
		if err = owned.Err(); err != nil {
			return err
		}
		observeConnection(owned, options.Observe, Connecting)
		if err = owned.Err(); err != nil {
			return err
		}
		challenge, attemptErr := device.challenge(owned, id.Configuration().RunnerID)
		var connection *wire
		if attemptErr == nil {
			connection, attemptErr = device.connect(owned, id, challenge.Nonce())
		}
		var activeAt time.Time
		var localErr error
		if attemptErr == nil {
			s, makeErr := newSession(connection, clientHello(id.Configuration().RunnerID, options.RunnerVersion), nil)
			if makeErr != nil {
				connection.stop()
				return makeErr
			}
			s.onHello = func() error {
				if id.State() == identity.Pending {
					active, activationErr := id.AsActive()
					if activationErr == nil {
						activationErr = file.Save(active)
					}
					if activationErr != nil {
						localErr = activationErr
						return activationErr
					}
					id = active
				}
				activeAt = time.Now()
				observeConnection(owned, options.Observe, Connected)
				return nil
			}
			attemptErr = s.run(owned)
		}
		// session.run has joined the original reader and callback before these local
		// values are inspected or the next authentication may begin.
		state := Disconnected
		var protocol protocolFailure
		if errors.Is(attemptErr, p.ErrIncompatibleVersion) || errors.As(attemptErr, &protocol) && protocol.code == p.IncompatibleVersion {
			state = Incompatible
		}
		observeConnection(owned, options.Observe, state)
		if localErr != nil {
			return localErr
		}
		if err = owned.Err(); err != nil {
			return err
		}
		if id.State() == identity.Pending {
			return ErrAuthentication
		}
		if !activeAt.IsZero() && time.Since(activeAt) >= 60*time.Second {
			attempts = 0
		}
		bound := reconnectBound(attempts)
		if attempts < 5 {
			attempts++
		}
		delay, jitterErr := c.jitter(bound)
		if jitterErr != nil {
			return ErrTransport
		}
		if delay < 0 || delay > bound {
			return ErrProtocol
		}
		if err = c.wait(owned, delay); err != nil {
			return err
		}
	}
}

func observeConnection(ctx context.Context, observer func(context.Context, ConnectionState), state ConnectionState) {
	if observer != nil {
		observer(ctx, state)
	}
}

// Stop closes admission and cancels the original operation context. It does not
// claim that a native read, file sync or operation callback has already joined.
func (c *Client) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true
	if !c.started {
		close(c.done)
	} else if c.cancel != nil {
		c.cancel()
	}
}
func (c *Client) Drain(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.Stop()
	return c.join(ctx)
}
func (c *Client) Force(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.Stop()
	return c.join(ctx)
}
func (c *Client) join(ctx context.Context) error {
	if ctx == nil {
		return identity.ErrInvalid
	}
	select {
	case <-c.done:
		return nil
	default:
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func reconnectBound(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= 5 {
		return 30 * time.Second
	}
	return time.Second * time.Duration(1<<uint(attempt))
}
func reconnectJitter(bound time.Duration) (time.Duration, error) {
	if bound < 0 || bound > 30*time.Second {
		return 0, ErrProtocol
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(bound)+1))
	if err != nil {
		return 0, ErrTransport
	}
	return time.Duration(n.Int64()), nil
}
func waitReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
