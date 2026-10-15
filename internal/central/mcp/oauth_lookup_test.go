package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type oauthLookupSQL struct {
	postgres.SQLExecutor // Any mutation fails the test rather than being emulated.
	session              string
	phase                string
	body                 []byte
	current              bool
	stopped              bool
	reads                int
}
type oauthLookupRow func(...any) error

func (r oauthLookupRow) Scan(dest ...any) error { return r(dest...) }
func (s *oauthLookupSQL) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	s.reads++
	return oauthLookupRow(func(dest ...any) error {
		if len(dest) == 1 {
			if len(args) == 7 {
				*dest[0].(*bool) = s.stopped
			} else {
				*dest[0].(*bool) = s.current
			}
			return nil
		}
		if len(args) != 6 || args[5] != s.session {
			return pgx.ErrNoRows
		}
		*dest[0].(*string), *dest[1].(*[]byte) = s.phase, s.body
		return nil
	})
}
func TestMCPOAuthLookupOriginalSessionAndPassiveOutcome(t *testing.T) {
	user := contentTailID[id.User](t, "01")
	session := contentTailID[id.Session](t, "02")
	actor, _ := id.NewHuman(user, session)
	scope, _ := id.InProject(contentTailID[id.Project](t, "03"))
	identity, _ := CommandIdentity(actor, scope, "oauth.begin", "original-lookup")
	x := &oauthLookupSQL{session: session.String(), phase: "pending", current: true}
	observe := func(a id.Actor) (CommandObservation, error) {
		return lookupOAuthCommand(context.Background(), x, a, scope, "oauth.begin", identity)
	}
	value, err := observe(actor)
	if err != nil || value.State != "pending" || value.Result != nil || x.reads != 2 {
		t.Fatal("pending observation changed original command")
	}
	x.stopped = true
	value, err = observe(actor)
	if err != nil || value.State != "stopped" || value.Result != nil || x.phase != "pending" {
		t.Fatal("retired original intent fabricated a command or remote result")
	}
	other, _ := id.NewHuman(user, contentTailID[id.Session](t, "04"))
	value, err = observe(other)
	if err != nil || value.State != "not_found" || value.Result != nil {
		t.Fatal("another session observed original OAuth command")
	}
	x.body, _ = json.Marshal(CommandResult{OAuth: &OAuthAuthorization{FlowID: contentTailID[struct{}](t, "05").String(), AuthorizationURL: "https://issuer.fixture/authorize?state=original", ExpiresAt: time.Now().Add(time.Minute)}})
	x.phase = "complete"
	value, err = observe(actor)
	if err != nil || value.State != "complete" || value.Result == nil || value.Result.OAuth == nil {
		t.Fatal("original ready flow was not recoverable")
	}
	x.current = false
	x.stopped = false
	_, err = observe(actor)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.ConfirmationStale {
		t.Fatal("expired or consumed flow returned authorization URL")
	}
	x.stopped = true
	value, err = observe(actor)
	if err != nil || value.State != "stopped" || value.Result != nil || x.phase != "complete" {
		t.Fatal("retired stale flow replaced its historical successful Begin receipt")
	}
	x.body = []byte(`{}`)
	identity, _ = CommandIdentity(actor, scope, "oauth.complete", "original-complete")
	value, err = lookupOAuthCommand(context.Background(), x, actor, scope, "oauth.complete", identity)
	if err != nil || value.State != "complete" {
		t.Fatal("completed exchange observation required a live flow")
	}
}
