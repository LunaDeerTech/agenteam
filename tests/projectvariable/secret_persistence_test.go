//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretVariableOwnerPersistence(t *testing.T) {
	v := newSecretOwnerFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	canary := []byte("secret-owner-private-value-41ca7d")
	defer clear(canary)
	in := secretCreateInput(t, "PRIVATE_TOKEN", canary)
	id := in.Fields().ID
	createMeta := meta(t, "secret-create", nil)
	created, err := v.owner.CreateSecretVariable(ctxFor(t), a, createMeta, p, in)
	if err != nil {
		t.Fatal("real Secret create", err)
	}
	if d := created.Fields(); !d.Changed || d.EventID == nil || d.AuditID == nil || d.Variable.Fields().Version != 1 {
		t.Fatal("create did not return complete real facts")
	}
	if got := v.secretCounts(t); got != [6]int64{2, 1, 1, 1, 1, 1} {
		t.Fatal("create fact counts", got)
	}
	for range 2 {
		replayed, err := v.owner.CreateSecretVariable(ctxFor(t), a, createMeta, p, in)
		if err != nil {
			t.Fatal("original-intent replay", err)
		}
		sameSecretReceipt(t, created, replayed)
	}
	if got := v.secretCounts(t); got != [6]int64{2, 1, 1, 1, 1, 1} {
		t.Fatal("replay added facts", got)
	}
	// A correctly shaped identity-only lookup is safe; it is not a substitute
	// for proving original material when executing the write again.
	q := secretLookup(t, p, id, vc.SecretCreateCommand, createMeta)
	lookup, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, q)
	if err != nil || lookup.Status() != vc.SecretLookupCommitted || lookup.Receipt() == nil {
		t.Fatal("safe lookup", err)
	}
	sameSecretReceipt(t, created, *lookup.Receipt())
	changedValue, err := sc.NewSecretMaterial([]byte("different-private-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer changedValue.Destroy()
	wrong, err := vc.NewSecretVariableCreate(vc.SecretVariableCreateFields{ID: id, Name: in.Fields().Name, Description: in.Fields().Description, Value: changedValue})
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Destroy()
	_, err = v.owner.CreateSecretVariable(ctxFor(t), a, createMeta, p, wrong)
	requireCode(t, err, f.IdempotencyKeyReused)

	description := "new safe metadata"
	version := f.Version(1)
	metadata := secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
	updated, err := v.owner.UpdateSecretVariable(ctxFor(t), a, meta(t, "metadata", &version), p, id, metadata)
	if err != nil || !updated.Fields().Changed || updated.Fields().Variable.Fields().Version != 2 {
		t.Fatal("metadata update", err)
	}
	v.assertSecretVersions(t, id, 2, 1)
	version = 2
	noOpMeta := meta(t, "metadata-noop", &version)
	noop, err := v.owner.UpdateSecretVariable(ctxFor(t), a, noOpMeta, p, id, metadata)
	if err != nil || noop.Fields().Changed || noop.Fields().Variable.Fields().Version != 2 {
		t.Fatal("metadata no-op", err)
	}
	if got := v.secretCounts(t); got != [6]int64{3, 2, 2, 2, 3, 3} {
		t.Fatal("no-op must only add two protected receipts", got)
	}
	// Explicit same material is still a real replacement, not digest dedup.
	material, err := sc.NewSecretMaterial(canary)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	replace := secretUpdateInput(t, vc.SecretVariableUpdateFields{Value: &material})
	replaced, err := v.owner.UpdateSecretVariable(ctxFor(t), a, meta(t, "replace", &version), p, id, replace)
	if err != nil || !replaced.Fields().Changed || replaced.Fields().Variable.Fields().Version != 3 {
		t.Fatal("explicit replacement", err)
	}
	v.assertSecretVersions(t, id, 3, 2)

	ordinary := v.createVariable(t, "PUBLIC_TOKEN", "ordinary")
	_, err = v.service.GetVariable(ctxFor(t), a, p, id)
	requireCode(t, err, f.NotFound)
	_, err = v.owner.GetSecretVariable(ctxFor(t), a, p, ordinary.Fields().ID)
	requireCode(t, err, f.NotFound)
	page, err := v.owner.ListSecretVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Fields().ID != id || page.NextCursor != "" {
		t.Fatal("type-isolated Secret list", err)
	}
	publicPage, err := v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 10})
	if err != nil || len(publicPage.Items) != 1 || publicPage.Items[0].Fields().ID != ordinary.Fields().ID {
		t.Fatal("type-isolated ordinary list", err)
	}
	_, err = v.service.CreateVariable(ctxFor(t), a, meta(t, "ordinary-name-conflict", nil), p, createInput(t, in.Fields().Name, "ordinary"))
	requireCode(t, err, f.ResourceBusy)
	_, err = v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "secret-name-conflict", nil), p, secretCreateInput(t, ordinary.Fields().Name, canary))
	requireCode(t, err, f.ResourceBusy)

	version = 3
	deleteMeta := meta(t, "delete", &version)
	deleted, err := v.owner.DeleteSecretVariable(ctxFor(t), a, deleteMeta, p, id)
	if err != nil || deleted.Fields().Deleted == nil || deleted.Fields().Deleted.Version != 4 {
		t.Fatal("Secret deletion", err)
	}
	if got := v.secretCounts(t); got != [6]int64{5, 4, 4, 4, 5, 5} {
		t.Fatal("five intended commands only", got)
	}
	_, err = v.owner.GetSecretVariable(ctxFor(t), a, p, id)
	requireCode(t, err, f.NotFound)
	var liveCredentials, livePayloads int
	err = v.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_secret.secrets c JOIN agenteam_secret.project_variable_receipts r ON c.id=r.credential_id WHERE r.project_id=$1 AND r.variable_id=$2),
 (SELECT count(*) FROM agenteam_secret.secret_payloads s WHERE s.owner_kind=1 AND s.owner_id IN (SELECT credential_id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 AND variable_id=$2))`, p.String(), id.String()).Scan(&liveCredentials, &livePayloads)
	if err != nil || liveCredentials != 0 || livePayloads != 0 {
		t.Fatal("delete retained live material", err)
	}

	// Original creation/metadata/no-op receipts survive both deletion and a
	// current new Session. Reconstruction must not require live canonical D04.
	newActor := v.login(t, v.ownerBrowser.email).actor
	if newActor.Details().SessionID == a.Details().SessionID {
		t.Fatal("expected new actual Session")
	}
	v.owner.Stop()
	drain, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = v.owner.Drain(drain)
	cancel()
	if err != nil {
		t.Fatal("old Owner did not actually drain", err)
	}
	v.owner = v.newOwner(t)
	for _, item := range []struct {
		query vc.SecretVariableCommandLookupRequest
		want  vc.SecretVariableMutation
	}{
		{q, created}, {secretLookup(t, p, id, vc.SecretUpdateCommand, noOpMeta), noop}, {secretLookup(t, p, id, vc.SecretDeleteCommand, deleteMeta), deleted},
	} {
		got, err := v.owner.LookupSecretVariableCommand(ctxFor(t), newActor, item.query)
		if err != nil || got.Receipt() == nil {
			t.Fatal("historical safe lookup", err)
		}
		sameSecretReceipt(t, item.want, *got.Receipt())
	}
	replayed, err := v.owner.CreateSecretVariable(ctxFor(t), newActor, createMeta, p, in)
	if err != nil {
		t.Fatal("original material historical replay", err)
	}
	sameSecretReceipt(t, created, replayed)
	if got := v.secretCounts(t); got != [6]int64{5, 4, 4, 4, 5, 5} {
		t.Fatal("history recovery added facts", got)
	}
	v.assertNoSecretLeak(t, canary, created, updated, noop, replaced, deleted, page, lookup)
}

func (v *secretOwnerFixture) assertSecretVersions(t *testing.T, id vc.VariableID, variable, credential int64) {
	t.Helper()
	var actualVariable, actualCredential int64
	err := v.raw.QueryRow(ctxFor(t), `SELECT version,credential_version FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2 AND type='secret'`, v.project.ID.String(), id.String()).Scan(&actualVariable, &actualCredential)
	if err != nil || actualVariable != variable || actualCredential != credential {
		t.Fatal("independent version evolution", err, actualVariable, actualCredential)
	}
}

func (v *secretOwnerFixture) assertNoSecretLeak(t *testing.T, canary []byte, values ...any) {
	t.Helper()
	var persisted string
	err := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'variables',(SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_projectvariable.variables x),
 'commands',(SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_projectvariable.secret_commands x),
 'history',(SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_projectvariable.secret_history x),
 'audit',(SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_audit.audit_records x),
 'events',(SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_outbox.events x))::text`).Scan(&persisted)
	if err != nil {
		t.Fatal("safe data inspection", err)
	}
	var output bytes.Buffer
	output.WriteString(persisted)
	output.WriteString(v.logs.text())
	for _, value := range values {
		output.Write(jsonBytes(t, value))
	}
	digest := sha256.Sum256(canary)
	for _, forbidden := range []string{string(canary), base64.StdEncoding.EncodeToString(canary), hex.EncodeToString(digest[:])} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatal("private material or enumerable digest escaped")
		}
	}
}
