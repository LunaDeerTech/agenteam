package service

import (
	"context"
	"crypto/ed25519"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type authenticationExecutor struct {
	*deviceExecutor
	nonceHash [32]byte
	sequence  int64
	used      bool
	owner     string
}

func (x *authenticationExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT root_path") {
		if args[0] != x.target.String() || x.missing {
			return deviceRow{err: pgx.ErrNoRows}
		}
		return deviceRow{values: []any{"/srv", int64(1), int64(1), x.sequence, x.public, x.issued}}
	}
	if strings.HasPrefix(query, "SELECT nonce_hash") {
		if x.used || x.badToken || string(args[0].([]byte)) != string(x.nonceHash[:]) || args[1] != x.target.String() || args[2] != int64(1) {
			return deviceRow{err: pgx.ErrNoRows}
		}
		return deviceRow{values: []any{x.nonceHash[:], x.issued, x.expires}}
	}
	return x.deviceExecutor.QueryRow(ctx, query, args...)
}
func (x *authenticationExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	x.writes = append(x.writes, query)
	switch {
	case strings.HasPrefix(query, "UPDATE agenteam_runner.challenges"):
		x.used = true
	case strings.HasPrefix(query, "UPDATE agenteam_runner.runners"):
		if !x.used || args[2] != x.sequence {
			return pgconn.CommandTag{}, errors.New("generation before consumption")
		}
		x.sequence++
	case strings.HasPrefix(query, "INSERT INTO agenteam_runner.connections"):
		if !x.used || args[3] != x.sequence || args[2] != int64(1) {
			return pgconn.CommandTag{}, errors.New("connection before reservation")
		}
		x.owner = args[4].(string)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected authentication write")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type authenticationStore struct {
	*deviceStore
	auth *authenticationExecutor
}

func (s *authenticationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign authentication transaction")
	}
	return s.auth, nil
}

func authenticationFixture(t *testing.T) (*Service, *authenticationStore, func(p.ID, p.Nonce, int64) p.Authentication, p.Authentication) {
	t.Helper()
	_, base, _, enroll := deviceFixture(t)
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	nonce, _ := p.NewNonce()
	hash, _ := nonce.Digest()
	base.x.public = append([]byte(nil), private.Public().(ed25519.PublicKey)...)
	x := &authenticationExecutor{deviceExecutor: base.x, nonceHash: hash, sequence: 4}
	store := &authenticationStore{base, x}
	authority, _ := NewAuthority(store, &readerAuthority{})
	s, e := New(authority, readerAudit{})
	if e != nil {
		t.Fatal(e)
	}
	sign := func(target p.ID, nonce p.Nonce, seconds int64) p.Authentication {
		timestamp := p.NewUnixSeconds(seconds)
		bytes, e := p.SigningBytes(target, nonce, timestamp)
		if e != nil {
			t.Fatal(e)
		}
		out, e := p.NewAuthentication(target, nonce, timestamp, ed25519.Sign(private, bytes))
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	request := sign(enroll.RunnerID(), nonce, x.now.Unix())
	return s, store, sign, request
}

func TestRunnerAuthenticationPureOneTimeGenerationAndIssuer(t *testing.T) {
	s, store, _, request := authenticationFixture(t)
	got, e := s.Authenticate(context.Background(), request)
	if e != nil || got.RunnerID() != store.x.target || !got.ID().Valid() || got.RootPath() != "/srv" || store.auth.sequence != 5 || store.auth.owner != s.state().owner.String() || len(store.x.writes) != 3 {
		t.Fatal("current reservation", e)
	}
	reserved, e := s.reservation(got)
	if e != nil || reserved.credential != 1 || reserved.generation != 5 {
		t.Fatal("wrong private generation")
	}
	other, _, _, _ := authenticationFixture(t)
	if _, e = other.reservation(got); e == nil {
		t.Fatal("foreign service accepted a connection")
	}
	if repeated, e := s.Authenticate(context.Background(), request); e == nil || repeated.ID().Valid() || len(store.x.writes) != 3 {
		t.Fatal("nonce replay reserved another generation")
	}
}

func TestRunnerAuthenticationPureCredentialWindowAndUnknown(t *testing.T) {
	for _, mode := range []string{"minus30", "plus30", "minus31", "plus31", "min-int", "max-int", "expired", "not-issued", "wrong-runner", "wrong-nonce", "wrong-key", "missing", "capacity", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, store, sign, request := authenticationFixture(t)
			switch mode {
			case "minus30":
				request = sign(request.RunnerID(), request.Nonce(), store.x.now.Unix()-30)
			case "plus30":
				request = sign(request.RunnerID(), request.Nonce(), store.x.now.Unix()+30)
			case "minus31":
				request = sign(request.RunnerID(), request.Nonce(), store.x.now.Unix()-31)
			case "plus31":
				request = sign(request.RunnerID(), request.Nonce(), store.x.now.Unix()+31)
			case "min-int":
				request = sign(request.RunnerID(), request.Nonce(), math.MinInt64)
			case "max-int":
				request = sign(request.RunnerID(), request.Nonce(), math.MaxInt64)
			case "expired":
				store.x.expires = store.x.now
			case "not-issued":
				store.x.issued = store.x.now.Add(time.Microsecond)
			case "wrong-runner":
				request = sign(p.ID(sid[c.Runner](t).String()), request.Nonce(), store.x.now.Unix())
			case "wrong-nonce":
				nonce, _ := p.NewNonce()
				request = sign(request.RunnerID(), nonce, store.x.now.Unix())
			case "wrong-key":
				store.x.public = make([]byte, 32)
			case "missing":
				store.x.missing = true
			case "capacity":
				store.auth.sequence = math.MaxInt64
			case "unknown":
				store.unknown = true
			}
			got, e := s.Authenticate(context.Background(), request)
			if mode == "minus30" || mode == "plus30" {
				if e != nil || !got.ID().Valid() {
					t.Fatal("inclusive timestamp boundary", e)
				}
				return
			}
			if e == nil || got.ID().Valid() {
				t.Fatal("rejected/unknown authentication leaked reservation")
			}
			if mode == "unknown" {
				var original commitFailure
				if !errors.As(e, &original) || original.result.AttemptID() != store.attempt || !store.auth.used {
					t.Fatal("unknown provenance")
				}
			} else if store.auth.used || len(store.x.writes) != 0 {
				t.Fatal("bad credential consumed nonce")
			}
		})
	}
}
