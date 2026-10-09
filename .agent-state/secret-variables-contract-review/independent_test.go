// Independent review of Secret A's pure contract. No database or runtime provider.
package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	s "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func rid[K any](n int) f.ID[K] {
	v, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func metadata(t *testing.T, version f.Version) p.SecretVariable {
	t.Helper()
	at, _ := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	v, err := p.NewSecretVariable(p.SecretVariableFields{ID: rid[i.ProjectVariable](1), ProjectID: rid[i.Project](2), Type: "secret", Name: "KEY", Description: "safe", Version: version, CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func receipt(t *testing.T, command p.SecretCommandName, changed bool) p.SecretVariableMutation {
	t.Helper()
	fields := p.SecretVariableMutationFields{Command: command, Changed: changed, Variable: metadata(t, 1)}
	if command != p.SecretCreateCommand && changed {
		fields.Variable = metadata(t, 2)
	}
	if changed {
		a, e := rid[audit.Record](3), rid[event.EventIdentity](4)
		fields.AuditID, fields.EventID = &a, &e
	}
	if command == p.SecretDeleteCommand {
		v := fields.Variable.Fields()
		fields.Deleted = &p.SecretVariableDeleted{ID: v.ID, ProjectID: v.ProjectID, Type: "secret", Version: 2, DeletedAt: v.UpdatedAt}
		fields.Variable = p.SecretVariable{}
	}
	v, err := p.NewSecretVariableMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestIndependentSecretUntrustedErrorPaths(t *testing.T) {
	// Attacker-controlled JSON member names are input too. A rejected field must
	// not become public Fault metadata or an unbounded response-body echo.
	const marker = "independent-error-member-canary"
	base := map[string]json.Unmarshaler{
		"create": &p.SecretVariableCreate{}, "update": &p.SecretVariableUpdate{},
		"lookup": &p.SecretVariableCommandLookupRequest{}, "metadata": &p.SecretVariable{},
		"receipt": &p.SecretVariableMutation{}, "observation": &p.SecretVariableCommandLookup{},
	}
	inputs := map[string]string{
		"create":   `{"variable_id":"` + rid[i.ProjectVariable](1).String() + `","name":"KEY","description":"","value":"material"}`,
		"update":   `{"name":"KEY"}`,
		"lookup":   `{"project_id":"` + rid[i.Project](2).String() + `","command":"project.secret_variable.create","target_id":"` + rid[i.ProjectVariable](1).String() + `","idempotency_key":"k"}`,
		"metadata": string(encoded(t, metadata(t, 1))), "receipt": string(encoded(t, receipt(t, p.SecretCreateCommand, true))),
		"observation": `{"status":"not_observed","receipt":null}`,
	}
	for name, dst := range base {
		t.Run(name, func(t *testing.T) {
			raw := inputs[name]
			raw = raw[:len(raw)-1] + `,"` + marker + `":"ignored"}`
			err := dst.UnmarshalJSON([]byte(raw))
			if err == nil {
				t.Fatal("unknown member accepted")
			}
			var fault *f.Fault
			if !errors.As(err, &fault) {
				t.Fatal("missing safe Fault")
			}
			if bytes.Contains(encoded(t, fault), []byte(marker)) {
				t.Fatal("untrusted JSON member copied into public Fault")
			}
		})
	}
}

func TestIndependentSecretMaterialAndPresence(t *testing.T) {
	const canary = "isolated-synthetic-material-7"
	m, _ := s.NewSecretMaterial([]byte(canary))
	defer m.Destroy()
	in := p.SecretVariableCreateFields{ID: rid[i.ProjectVariable](1), Name: "KEY", Value: m}
	c, err := p.NewSecretVariableCreate(in)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Destroy()
	clone, err := c.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Destroy()
	m.Destroy()
	for _, v := range []any{in, c, clone, struct{ Request p.SecretVariableCreate }{c}} {
		if bytes.Contains(encoded(t, v), []byte(canary)) {
			t.Fatal("JSON material leak")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), canary) {
				t.Fatal("format material leak")
			}
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).Info("probe", "value", v)
		if strings.Contains(out.String(), canary) {
			t.Fatal("log material leak")
		}
	}
	var borrowed []byte
	sentinel := errors.New("private callback result")
	if !errors.Is(c.UseValue(func(b []byte) error { borrowed = b; b[0] = 'x'; return sentinel }), sentinel) {
		t.Fatal("callback result lost")
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("borrow retained")
	}
	if c.UseValue(func(b []byte) error {
		if string(b) != canary {
			t.Fatal("callback mutated owner")
		}
		return nil
	}) != nil {
		t.Fatal("owner unavailable")
	}
	c.Destroy()
	if c.Validate() == nil || clone.Validate() != nil {
		t.Fatal("clone lifetime")
	}
	var u p.SecretVariableUpdate
	if u.UnmarshalJSON([]byte(`{"description":""}`)) != nil || u.Fields().ValuePresent || u.Fields().Description == nil {
		t.Fatal("presence lost")
	}
	for _, raw := range []string{`{}`, `{"value":null}`, `{"value":""}`, `{"value":"\ud800"}`, `{"value":"\udc00"}`, `{"value":"\u0000"}`, `{"value":"a","\u0076alue":"b"}`, `{"value":"x"}{}`, `{"description":null}`, `{"value":"x","expected_version":"1"}`} {
		if u.UnmarshalJSON([]byte(raw)) == nil || u.Fields().Description == nil || u.Fields().ValuePresent {
			t.Fatal("strict/non-atomic update")
		}
	}
	if u.UnmarshalJSON([]byte(`{"value":" \t\n "}`)) != nil {
		t.Fatal("whitespace normalized")
	}
	defer u.Destroy()
	if u.UseValue(func(b []byte) error {
		if string(b) != " \t\n " {
			t.Fatal("changed bytes")
		}
		return nil
	}) != nil {
		t.Fatal("value unavailable")
	}
	for _, value := range [][]byte{bytes.Repeat([]byte("界"), 21846), {0xff}, {0}} {
		if p.ValidateSecretValue(value) == nil {
			t.Fatal("UTF8/byte cap")
		}
	}
	if p.ValidateSecretValue(bytes.Repeat([]byte("x"), 65536)) != nil {
		t.Fatal("max material rejected")
	}
}

func TestIndependentSecretReceiptsAndIdentity(t *testing.T) {
	for _, name := range []string{"AGENTEAM", "agenteam_key", "9KEY", "KEY-X", " Key"} {
		v := metadata(t, 1).Fields()
		v.Name = name
		if _, e := p.NewSecretVariable(v); e == nil {
			t.Fatal("shared name rule")
		}
	}
	if reflect.TypeFor[p.VariableID]() != reflect.TypeFor[i.ProjectVariableID]() || reflect.TypeFor[p.VariableID]() == reflect.TypeFor[s.CredentialID]() {
		t.Fatal("ID domain")
	}
	for _, purpose := range []s.Purpose{"secret", "project_variable", "project_secret_variable"} {
		if purpose.Valid() {
			t.Fatal("legacy purpose widened")
		}
	}
	for _, command := range []p.SecretCommandName{p.SecretCreateCommand, p.SecretUpdateCommand, p.SecretDeleteCommand} {
		for _, changed := range []bool{false, true} {
			if !changed && command != p.SecretUpdateCommand {
				continue
			}
			r := receipt(t, command, changed)
			var copy p.SecretVariableMutation
			if copy.UnmarshalJSON(encoded(t, r)) != nil {
				t.Fatal("receipt roundtrip")
			}
			fields := r.Fields()
			fields.Changed = !changed
			if _, e := p.NewSecretVariableMutation(fields); e == nil {
				t.Fatal("effect/IDs mismatch")
			}
			fields = r.Fields()
			if changed {
				fields.EventID = nil
				if _, e := p.NewSecretVariableMutation(fields); e == nil {
					t.Fatal("missing event")
				}
			}
			for _, key := range []string{"value", "value_hash", "value_length", "masked_value", "credential_ref", "semantic_digest", "session_id"} {
				raw := encoded(t, r)
				raw = append(raw[:len(raw)-1], []byte(`,"`+key+`":"x"}`)...)
				if copy.UnmarshalJSON(raw) == nil {
					t.Fatal("unsafe receipt extension")
				}
			}
			committed, e := p.NewSecretVariableCommandLookup(p.SecretLookupCommitted, &r)
			if e != nil {
				t.Fatal(e)
			}
			var lookup p.SecretVariableCommandLookup
			if lookup.UnmarshalJSON(encoded(t, committed)) != nil {
				t.Fatal("committed lookup")
			}
			if _, e = p.NewSecretVariableCommandLookup(p.SecretLookupNotObserved, &r); e == nil {
				t.Fatal("not observed receipt")
			}
		}
	}
	for _, status := range []p.SecretLookupStatus{p.SecretLookupCommitted, "in_progress", "unknown", ""} {
		if _, e := p.NewSecretVariableCommandLookup(status, nil); e == nil {
			t.Fatal("lookup union")
		}
	}
	version := f.Version(1)
	for _, cmd := range []p.SecretCommandName{p.SecretCreateCommand, p.SecretUpdateCommand, p.SecretDeleteCommand} {
		for _, expected := range []*f.Version{nil, &version} {
			fields := p.SecretVariableCommandLookupFields{ProjectID: rid[i.Project](2), Command: cmd, TargetID: rid[i.ProjectVariable](1), IdempotencyKey: "k", ExpectedVersion: expected}
			_, e := p.NewSecretVariableCommandLookupRequest(fields)
			want := (cmd == p.SecretCreateCommand) == (expected == nil)
			if (e == nil) != want {
				t.Fatal("identity expected presence")
			}
		}
	}
	if p.SecretCommandName(p.CreateCommand).Validate() == nil || p.CommandName(p.SecretCreateCommand).Validate() == nil {
		t.Fatal("command sets overlap")
	}
}

func reference(t *testing.T) p.SecretReferenceChange {
	t.Helper()
	actor, _ := i.NewHuman(rid[i.User](11), rid[i.Session](12))
	project := rid[i.Project](2)
	command, e := f.NewCommandIdentity("project", []string{project.String()}, "agent.update", "original-key")
	if e != nil {
		t.Fatal(e)
	}
	v := f.Version(5)
	return p.SecretReferenceChange{Actor: actor, ProjectID: project, AgentID: rid[i.Agent](13), Command: command, Operation: p.SecretReferenceUpdate, ExpectedOwnerVersion: &v, ResultOwnerVersion: 6, Before: []p.VariableID{rid[i.ProjectVariable](1)}, After: []p.VariableID{rid[i.ProjectVariable](2)}}
}
func locks(r p.SecretReferenceChange) []f.LockRequest {
	u, _ := f.UserLock(r.Actor.Details().UserID)
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	c, _ := f.CommandLock(r.Command)
	return []f.LockRequest{{Key: u, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Exclusive}, {Key: c, Mode: f.Exclusive}}
}
func TestIndependentSecretAuthorityPlanBinding(t *testing.T) {
	r := reference(t)
	b, e := p.SecretReferenceBinding(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*p.SecretReferenceChange){
		func(x *p.SecretReferenceChange) { x.Actor, _ = i.NewHuman(rid[i.User](11), rid[i.Session](99)) },
		func(x *p.SecretReferenceChange) { x.Actor, _ = i.NewHuman(rid[i.User](99), rid[i.Session](12)) },
		func(x *p.SecretReferenceChange) { x.AgentID = rid[i.Agent](99) },
		func(x *p.SecretReferenceChange) {
			x.Command, _ = f.NewCommandIdentity("project", []string{x.ProjectID.String()}, "agent.update", "other-key")
		},
		func(x *p.SecretReferenceChange) { x.Before = []p.VariableID{rid[i.ProjectVariable](3)} },
		func(x *p.SecretReferenceChange) { x.After = []p.VariableID{rid[i.ProjectVariable](3)} },
		func(x *p.SecretReferenceChange) { *x.ExpectedOwnerVersion = 4; x.ResultOwnerVersion = 5 },
	} {
		other := r.Clone()
		mutate(&other)
		bound, e := p.SecretReferenceBinding(other)
		if e != nil || bound == b {
			t.Fatal("original identity not bound")
		}
	}
	issuer, ownerIssuer := p.NewSecretPlanIssuer(), p.NewSecretPlanIssuer()
	mapping := f.Digest("sha256:" + strings.Repeat("a", 64))
	all := locks(r)
	owner, e := p.NewSecretReferenceOwnerPlan(ownerIssuer, r, mapping, all)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := p.NewSecretReferencePlan(issuer, r, mapping, all, owner)
	if e != nil {
		t.Fatal(e)
	}
	if !plan.Matches(issuer, b, mapping) || plan.Matches(ownerIssuer, b, mapping) || plan.Matches(p.SecretPlanIssuer{}, b, mapping) {
		t.Fatal("issuer proof")
	}
	for n := range all {
		bad := append([]f.LockRequest{}, all...)
		bad = append(bad[:n], bad[n+1:]...)
		if _, e := p.NewSecretReferenceOwnerPlan(ownerIssuer, r, mapping, bad); e == nil {
			t.Fatal("missing authority lock")
		}
		if _, e := p.NewSecretReferencePlan(issuer, r, mapping, bad, owner); e == nil {
			t.Fatal("missing lock union")
		}
	}
	for n, l := range all {
		if l.Mode == f.Shared {
			continue
		}
		bad := append([]f.LockRequest{}, all...)
		bad[n].Mode = f.Shared
		if _, e := p.NewSecretReferenceOwnerPlan(ownerIssuer, r, mapping, bad); e == nil {
			t.Fatal("insufficient lock mode")
		}
	}
	mut := plan.RequiredLocks()
	mut[0].Mode = f.Shared
	for n, lock := range plan.RequiredLocks() {
		other := owner.RequiredLocks()[n]
		if f.CompareLockKeys(lock.Key, other.Key) != 0 || lock.Mode != other.Mode {
			t.Fatal("plan slice alias")
		}
	}
	for _, bad := range []func(*p.SecretReferenceChange){func(x *p.SecretReferenceChange) { x.ResultOwnerVersion = 5 }, func(x *p.SecretReferenceChange) { x.ResultOwnerVersion = 7 }, func(x *p.SecretReferenceChange) { x.ExpectedOwnerVersion = nil }, func(x *p.SecretReferenceChange) { x.After = append(x.After, x.After[0]) }, func(x *p.SecretReferenceChange) { x.ProjectID = rid[i.Project](99) }, func(x *p.SecretReferenceChange) {
		x.Actor, _ = i.NewAgentRun(x.ProjectID, x.AgentID, rid[i.Execution](99))
	}} {
		copy := r.Clone()
		bad(&copy)
		if copy.Validate() == nil {
			t.Fatal("invalid authority shape")
		}
	}
	dr := p.SecretDirectoryRequest{Actor: r.Actor, ProjectID: r.ProjectID, Command: r.Command, IDs: r.Before}
	v := metadata(t, 1)
	if _, e := p.NewSecretDirectoryFacts(dr, []p.SecretDirectoryEntry{{ID: r.Before[0], Status: p.SecretDirectoryValid, Variable: &v}}); e != nil {
		t.Fatal(e)
	}
	for _, status := range []p.SecretDirectoryStatus{p.SecretDirectoryRemoved, p.SecretDirectoryNotInScope} {
		if _, e := p.NewSecretDirectoryFacts(dr, []p.SecretDirectoryEntry{{ID: r.Before[0], Status: status}}); e != nil {
			t.Fatal(e)
		}
	}
	for _, entries := range [][]p.SecretDirectoryEntry{nil, {{ID: r.Before[0], Status: "disabled"}}, {{ID: r.Before[0], Status: p.SecretDirectoryRemoved, Variable: &v}}, {{ID: rid[i.ProjectVariable](99), Status: p.SecretDirectoryNotInScope}}} {
		if _, e := p.NewSecretDirectoryFacts(dr, entries); e == nil {
			t.Fatal("directory positional/safe state")
		}
	}
	var restored p.SecretReferencePlan
	if json.Unmarshal(encoded(t, plan), &restored) == nil || restored.Validate() == nil {
		t.Fatal("serialized capability restored")
	}
}

func TestIndependentSecretEventSourceIsolation(t *testing.T) {
	cat := event.NewCatalog()
	secret, e := p.RegisterSecretVariableEvents(cat)
	if e != nil {
		t.Fatal(e)
	}
	ordinary, e := p.RegisterVariableEvents(cat)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.RegisterSecretVariableEvents(cat); e == nil {
		t.Fatal("duplicate source accepted")
	}
	version := f.Version(2)
	h := event.Header{EventID: rid[event.EventIdentity](7), EventType: p.SecretVariableChangedName, SchemaVersion: 1, OccurredAt: metadata(t, 2).Fields().UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: rid[event.Project](2)}, AggregateType: p.SecretVariableAggregate, AggregateID: rid[event.Aggregate](1), AggregateVersion: &version}
	payload := p.SecretVariableChanged{VariableID: rid[i.ProjectVariable](1), OperationID: rid[p.Operation](8), Change: p.Updated, ChangedFields: []string{"description", "name", "value"}}
	ev, e := secret.NewSecretVariableChanged(h, payload)
	if e != nil {
		t.Fatal(e)
	}
	foreign, _ := p.RegisterSecretVariableEvents(event.NewCatalog())
	if _, e = ordinary.Decode(ev); e == nil {
		t.Fatal("ordinary event confusion")
	}
	if _, e = foreign.Decode(ev); e == nil {
		t.Fatal("foreign catalog")
	}
	if _, e = secret.Decode(ev); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*event.Header){func(v *event.Header) { v.EventType = p.VariableChangedName }, func(v *event.Header) { v.SchemaVersion = 2 }, func(v *event.Header) { v.Scope = event.Scope{Kind: event.SystemScope} }, func(v *event.Header) { v.AggregateID = rid[event.Aggregate](99) }, func(v *event.Header) { v.AggregateVersion = nil }} {
		bad := h
		mutate(&bad)
		if _, e := secret.NewSecretVariableChanged(bad, payload); e == nil {
			t.Fatal("event header binding")
		}
	}
	for _, fields := range [][]string{nil, {"value", "name"}, {"value", "value"}, {"value_hash"}, {"credential_ref"}} {
		bad := payload
		bad.ChangedFields = fields
		if bad.Validate() == nil {
			t.Fatal("changed fields closed")
		}
	}
	for _, k := range []string{"name", "description", "value", "value_hash", "credential_ref", "semantic_digest"} {
		raw := encoded(t, payload)
		raw = append(raw[:len(raw)-1], []byte(`,"`+k+`":"fixture"}`)...)
		if _, e := secret.Restore(h, raw); e == nil {
			t.Fatal("unsafe event member")
		}
	}
}
