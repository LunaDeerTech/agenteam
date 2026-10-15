package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestOAuthRecoveryCleanupKeepsFlowAndOriginalDeleteTuple(t *testing.T) {
	source := oauthRecoveryTestFact(t)
	deletion := source
	deletion.request = source.flow
	deletion.operation, deletion.phase, deletion.lease = "retire", "claimed", ""
	deletion.retired, deletion.released = true, true
	r, err := makeOAuthRecoveryDeleteRequest(deletion)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := sc.MCPOAuthWriteBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	deletion.writeBinding = string(binding)
	if !sameOAuthCleanupSource(source, deletion) {
		t.Fatal("original flow cleanup tuple rejected")
	}
	for _, change := range []func(*oauthRecoveryFact){
		func(v *oauthRecoveryFact) { v.project = source.config },
		func(v *oauthRecoveryFact) { v.config = source.project },
		func(v *oauthRecoveryFact) { v.revision++ },
		func(v *oauthRecoveryFact) { v.connection = source.flow },
		func(v *oauthRecoveryFact) { v.version++ },
		func(v *oauthRecoveryFact) { v.flow = source.connection },
		func(v *oauthRecoveryFact) { v.command = digest([]byte("foreign command")) },
		func(v *oauthRecoveryFact) { v.user = source.session },
		func(v *oauthRecoveryFact) { v.session = source.user },
		func(v *oauthRecoveryFact) { v.credential = source.request },
		func(v *oauthRecoveryFact) { v.credentialVersion++ },
		func(v *oauthRecoveryFact) { v.operation = "exchange" },
	} {
		other := deletion
		change(&other)
		if sameOAuthCleanupSource(source, other) {
			t.Fatal("substituted original cleanup tuple accepted")
		}
	}
	// Begin publishes the flow credential as its result; it is never the
	// token credential produced by the subsequent exchange.
	begin := source
	begin.operation, begin.resultCredential, begin.resultVersion = "begin", source.credential, source.credentialVersion
	begin.credential, begin.credentialVersion, begin.lease = "", 0, ""
	if !sameOAuthCleanupSource(begin, deletion) {
		t.Fatal("published Begin flow credential rejected")
	}
	begin.resultCredential = source.request
	if sameOAuthCleanupSource(begin, deletion) {
		t.Fatal("another credential substituted for the flow")
	}
}

func TestOAuthRecoveryCleanupDataNeverCreatesPrivateAuthority(t *testing.T) {
	source := oauthRecoveryTestFact(t)
	deletion := source
	deletion.operation, deletion.lease = "retire", ""
	deletion.retired, deletion.released = true, true
	r, err := makeOAuthRecoveryDeleteRequest(deletion)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	deletion.writeBinding = string(binding)
	service := &Service{}
	recovery := &oauthRecoveryUse{service: service, original: source}
	owner := &oauthRecoveryDeleteOwner{recovery: recovery, deletion: deletion}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), oauthRecoveryKey{}, recovery)} {
		if err := owner.checkOAuthWriteOwnerInTx(ctx, f.NewTx()); err == nil {
			t.Fatal("canonical data without actual stopped Guard/admitted call authorized Delete")
		}
	}
	// Knowing the exact original binding does not activate the Secret plan.
	if _, err := service.DiscoverMCPOAuthWrite(context.Background(), r); err == nil {
		t.Fatal("bare original Delete obtained a private write plan")
	}
}
