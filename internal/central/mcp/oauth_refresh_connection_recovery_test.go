package mcp

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func humanRefreshRecoveryFacts(t *testing.T) (oauthRefreshFact, oauthConnectionRecoveryFact) {
	t.Helper()
	source := refreshRecoveryFact(t)
	source.caller = oauthRefreshCaller{command: digest([]byte("original-owner-refresh")),
		user: contentTailID[id.User](t, "51").String(), session: contentTailID[id.Session](t, "52").String()}
	connection := oauthConnectionRecoveryFact{request: contentTailID[f.Request](t, "53").String(),
		command: source.caller.command, user: source.caller.user, session: source.caller.session,
		project: source.project, config: source.config, revision: source.revision,
		connection: source.connection, version: source.connectionVersion, credential: source.credential,
		lease: contentTailID[sc.Lease](t, "54").String(), phase: "pending"}
	return source, connection
}

type humanRefreshRows struct {
	values  []oauthConnectionRecoveryFact
	next    int
	failure error
}

func (r *humanRefreshRows) Next() bool { r.next++; return r.next <= len(r.values) }
func (r *humanRefreshRows) Err() error { return r.failure }
func (r *humanRefreshRows) Scan(dest ...any) error {
	v := r.values[r.next-1]
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = v.request, v.command, v.project
	*dest[3].(*string), *dest[4].(*f.Version), *dest[5].(*string) = v.config, v.revision, v.connection
	*dest[6].(*f.Version), *dest[7].(*string), *dest[8].(*string) = v.version, v.user, v.session
	*dest[9].(*string), *dest[10].(*string), *dest[11].(*string) = v.credential, v.lease, v.phase
	*dest[12].(*bool), *dest[13].(*bool) = v.retired, v.released
	return nil
}

func TestMCPOAuthHumanRefreshRequiresUniqueOriginalOuterTuple(t *testing.T) {
	source, original := humanRefreshRecoveryFacts(t)
	if source.validate() != nil || !original.matchesHumanRefresh(source) {
		t.Fatal("invalid original fixture")
	}
	for _, values := range [][]oauthConnectionRecoveryFact{nil, {original, original}} {
		if _, err := scanHumanRefreshConnection(&humanRefreshRows{values: values}, source); err == nil {
			t.Fatal("absence or ambiguous association granted outer cleanup")
		}
	}
	for _, mutate := range []func(*oauthConnectionRecoveryFact){
		func(v *oauthConnectionRecoveryFact) { v.command = digest([]byte("other-command")) },
		func(v *oauthConnectionRecoveryFact) { v.project = source.config },
		func(v *oauthConnectionRecoveryFact) { v.config = source.project },
		func(v *oauthConnectionRecoveryFact) { v.revision++ },
		func(v *oauthConnectionRecoveryFact) { v.connection = source.config },
		func(v *oauthConnectionRecoveryFact) { v.version++ },
		func(v *oauthConnectionRecoveryFact) { v.credential = source.config },
		func(v *oauthConnectionRecoveryFact) { v.user = source.caller.session },
		func(v *oauthConnectionRecoveryFact) { v.session = source.caller.user },
		func(v *oauthConnectionRecoveryFact) { v.request = "" },
		func(v *oauthConnectionRecoveryFact) { v.lease = "" },
		func(v *oauthConnectionRecoveryFact) { v.released = true },
	} {
		other := original
		mutate(&other)
		if _, err := scanHumanRefreshConnection(&humanRefreshRows{values: []oauthConnectionRecoveryFact{other}}, source); err == nil {
			t.Fatal("foreign outer request matched original Human refresh")
		}
	}
	closed := original
	closed.phase, closed.retired, closed.released = "abandoned", true, true
	if found, err := scanHumanRefreshConnection(&humanRefreshRows{values: []oauthConnectionRecoveryFact{closed}}, source); err != nil || !found.sameOrigin(original) {
		t.Fatal("original retired lease lost its binding", err)
	}
	if _, err := scanHumanRefreshConnection(&humanRefreshRows{values: []oauthConnectionRecoveryFact{original}, failure: errors.New("read failed")}, source); err == nil {
		t.Fatal("failed observation became a unique association")
	}
}

func TestMCPOAuthHumanRefreshOnlyPrivateValidatedRelease(t *testing.T) {
	source, original := humanRefreshRecoveryFacts(t)
	project, _ := f.ParseID[id.Project](source.project)
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(original.request, scope)
	credential, _ := f.ParseID[sc.Credential](original.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	lease, _ := f.ParseID[sc.Lease](original.lease)
	leaseOwner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, original.request)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseID: lease, LeaseOwner: leaseOwner, Action: sc.ReleaseLeaseUsage}
	owner := &processOwner{process: contentTailID[oc.Process](t, "55"), calls: map[*processCall]struct{}{}}
	s := &Service{owner: owner}
	call := &processCall{owner: owner}
	recovery := &oauthRefreshRecovery{service: s, original: source, call: call}
	use := &humanRefreshRecoveryUse{recovery: recovery, original: original, request: r, active: true}
	ctx := context.WithValue(context.Background(), humanRefreshRecoveryKey{}, use)
	if _, err := s.humanRefreshRecoveryUse(ctx, r); err == nil {
		t.Fatal("row DTO proved stopped")
	}
	recovery.stopped = true
	if _, err := s.humanRefreshRecoveryUse(ctx, r); err == nil {
		t.Fatal("unregistered call granted discovery")
	}
	owner.calls[call] = struct{}{}
	if _, err := s.humanRefreshRecoveryUse(ctx, r); err != nil {
		t.Fatal("private original discovery", err)
	}
	for _, action := range []sc.UsageAction{sc.AcquireLeaseUsage, sc.ReadLeaseUsage} {
		other := r
		other.Action = action
		if _, err := s.humanRefreshRecoveryUse(ctx, other); err == nil {
			t.Fatal("cleanup granted credential read/acquire")
		}
	}
	if _, err := s.AuthorizeLeaseInTx(ctx, f.NewTx(), actor, ref, leaseOwner, sc.ReleaseLease); err == nil {
		t.Fatal("discovery without original transaction validation granted release")
	}
	other := r
	other.LeaseID = contentTailID[sc.Lease](t, "56")
	if _, err := s.humanRefreshRecoveryUse(ctx, other); err == nil {
		t.Fatal("substituted lease accepted")
	}
	use.close()
	if _, err := s.humanRefreshRecoveryUse(ctx, r); err == nil {
		t.Fatal("retired context reused")
	}
}
