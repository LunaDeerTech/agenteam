package secret

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func projectVariableIntentFixture(t *testing.T, edit func(*sc.ProjectVariableWriteFields, *sc.ProjectVariableIntentFields)) sc.ProjectVariableIntent {
	t.Helper()
	user, _ := f.ParseID[i.User]("01900000-0000-7000-8000-000000000001")
	session, _ := f.ParseID[i.Session]("01900000-0000-7000-8000-000000000002")
	project, _ := f.ParseID[i.Project]("01900000-0000-7000-8000-000000000003")
	variable, _ := f.ParseID[i.ProjectVariable]("01900000-0000-7000-8000-000000000004")
	actor, _ := i.NewHuman(user, session)
	identity, _ := f.NewCommandIdentity("projectvariable", []string{project.String()}, "project.secret_variable.update", "fixture-key")
	expected := f.Version(7)
	r := sc.ProjectVariableWriteFields{Actor: actor, ProjectID: project, VariableID: variable, Identity: identity, Kind: sc.Update, ExpectedVersion: &expected}
	name, description := "TOKEN", "description"
	material, err := sc.NewSecretMaterial([]byte("opaque-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	fields := sc.ProjectVariableIntentFields{Name: &name, Description: &description, Value: &material}
	if edit != nil {
		edit(&r, &fields)
	}
	fields.Request, err = sc.NewProjectVariableWriteRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := sc.NewProjectVariableIntent(fields)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(intent.Destroy)
	return intent
}
func TestProjectVariableIntentPrivateDigestGoldenAndOriginalFields(t *testing.T) {
	base := projectVariableIntentFixture(t, nil)
	digest, err := projectVariableIntentDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(digest)
	// Independent Python hashlib + explicit uint64 field lengths, fixed public
	// fixture inputs and the Foundation canonical command wire produced this.
	if hex.EncodeToString(digest) != "2a6541728882c34372f5c54e4c8c707dda0f5a52b584e7ec31469698c91b02a5" {
		t.Fatal("semantic encoding changed")
	}
	newSession := projectVariableIntentFixture(t, func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
		user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
		session, _ := f.ParseID[i.Session]("01900000-0000-7000-8000-000000000099")
		r.Actor, _ = i.NewHuman(user, session)
	})
	same, err := projectVariableIntentDigest(newSession)
	if err != nil || !bytes.Equal(same, digest) {
		t.Fatal("current Session entered original semantics")
	}
	clear(same)
	for name, change := range map[string]func(*sc.ProjectVariableWriteFields, *sc.ProjectVariableIntentFields){
		"writer": func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
			user, _ := f.ParseID[i.User]("01900000-0000-7000-8000-000000000009")
			session, _ := f.ParseID[i.Session](r.Actor.Details().SessionID)
			r.Actor, _ = i.NewHuman(user, session)
		},
		"target": func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
			r.VariableID, _ = f.ParseID[i.ProjectVariable]("01900000-0000-7000-8000-000000000009")
		},
		"expected": func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
			n := f.Version(8)
			r.ExpectedVersion = &n
		},
		"name": func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) { n := "TOKEN2"; v.Name = &n },
		"description": func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) {
			d := "description2"
			v.Description = &d
		},
		"absent-description": func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) { v.Description = nil },
		"empty-description":  func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) { d := ""; v.Description = &d },
		"absent-value":       func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) { v.Value = nil },
		"value": func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) {
			m, err := sc.NewSecretMaterial([]byte("opaque-value2"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Destroy)
			v.Value = &m
		},
	} {
		t.Run(name, func(t *testing.T) {
			other := projectVariableIntentFixture(t, change)
			got, err := projectVariableIntentDigest(other)
			if err != nil || bytes.Equal(got, digest) {
				t.Fatal("different original intent collapsed")
			}
			clear(got)
		})
	}
	base.Destroy()
	if got, err := projectVariableIntentDigest(base); err == nil || len(got) != 0 {
		t.Fatal("destroyed material hashed")
	}
}

type untrustedProjectVariablePrepared struct{ calls *int }

func (p *untrustedProjectVariablePrepared) Preparation() (sc.ProjectVariablePreparation, error) {
	*p.calls++
	panic("untrusted method called")
}
func (p *untrustedProjectVariablePrepared) RequiredLocks() ([]f.LockRequest, error) {
	*p.calls++
	panic("untrusted method called")
}
func (p *untrustedProjectVariablePrepared) Destroy()             { *p.calls++; panic("untrusted method called") }
func (*untrustedProjectVariablePrepared) Format(fmt.State, rune) { panic("untrusted format called") }
func (*untrustedProjectVariablePrepared) LogValue() slog.Value   { panic("untrusted log called") }
func (*untrustedProjectVariablePrepared) MarshalJSON() ([]byte, error) {
	panic("untrusted JSON called")
}

type wrappedProjectVariablePrepared struct {
	sc.PreparedProjectVariableWrite
}

func projectVariablePreparedFixture(t *testing.T) (*Service, *preparedProjectVariableWrite, *projectVariablePreparedState) {
	t.Helper()
	issuer := &serviceState{}
	s := &Service{data: func() *serviceState { return issuer }}
	intent := projectVariableIntentFixture(t, nil)
	r := intent.Fields().Request
	project := r.Fields().ProjectID
	scope, _ := i.InProject(project)
	id, _ := f.ParseID[sc.Credential]("01900000-0000-7000-8000-000000000008")
	ref, _ := sc.NewCredentialRef(id, scope)
	locks, err := sc.ProjectVariableWriteLocks(r, ref)
	if err != nil {
		t.Fatal(err)
	}
	receiptID, _ := f.ParseID[sc.ProjectVariableReceipt]("01900000-0000-7000-8000-000000000009")
	meta, err := sc.NewProjectVariablePreparation(sc.ProjectVariablePreparationFields{Request: r, Ref: ref, ReceiptID: receiptID})
	if err != nil {
		t.Fatal(err)
	}
	state := &projectVariablePreparedState{issuer: issuer, preparation: meta, locks: locks, value: envelope{ciphertext: []byte("private-cipher-canary")}, receipt: envelope{ciphertext: []byte("private-receipt-canary")}}
	p := &preparedProjectVariableWrite{data: func() *projectVariablePreparedState { return state }}
	return s, p, state
}
func TestProjectVariablePreparedRejectsForeignMethodsBeforeDispatch(t *testing.T) {
	s, p, _ := projectVariablePreparedFixture(t)
	defer p.Destroy()
	calls := 0
	var typedNil *preparedProjectVariableWrite
	foreign, other, _ := projectVariablePreparedFixture(t)
	_ = foreign
	defer other.Destroy()
	for _, raw := range []sc.PreparedProjectVariableWrite{nil, typedNil, &untrustedProjectVariablePrepared{&calls}, &wrappedProjectVariablePrepared{p}, other, &preparedProjectVariableWrite{}, &preparedProjectVariableWrite{data: func() *projectVariablePreparedState { return nil }}} {
		if state, err := s.lockProjectVariablePrepared(raw); err == nil || state != nil {
			t.Fatal("foreign or malformed prepared accepted")
		}
	}
	if calls != 0 {
		t.Fatal("untrusted interface executed")
	}
	state, err := s.lockProjectVariablePrepared(p)
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Unlock()
	p.Destroy()
	if _, err := s.lockProjectVariablePrepared(p); err == nil {
		t.Fatal("retired prepared reused")
	}
}
func TestProjectVariablePreparedRetiresAliasesAndKeepsCallerMaterial(t *testing.T) {
	s, p, state := projectVariablePreparedFixture(t)
	intent := projectVariableIntentFixture(t, nil)
	alias := *p
	borrowed := state.value.ciphertext
	receipt := state.receipt.ciphertext
	locks, err := p.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	locks[0].Mode = f.Shared
	again, _ := p.RequiredLocks()
	if again[0].Mode != f.Exclusive {
		t.Fatal("prepared locks alias")
	}
	// Exercise safe surfaces while the private canary bytes are still live.
	var out bytes.Buffer
	for _, v := range []any{p, &alias, struct{ p *preparedProjectVariableWrite }{p}} {
		fmt.Fprintf(&out, "%+v %#v", v, v)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(raw)
		slog.New(slog.NewJSONHandler(&out, nil)).Info("safe", "value", v)
	}
	for _, canary := range []string{"private-cipher-canary", "private-receipt-canary", "opaque-value"} {
		if strings.Contains(out.String(), canary) {
			t.Fatal("unsafe prepared surface")
		}
	}
	held, err := s.lockProjectVariablePrepared(p)
	if err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan struct{})
	go func() { close(started); alias.Destroy(); close(done) }()
	<-started
	select {
	case <-done:
		t.Fatal("Destroy ignored active use")
	default:
	}
	held.mu.Unlock()
	<-done
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) || !bytes.Equal(receipt, make([]byte, len(receipt))) {
		t.Fatal("owned prepared bytes survived retirement")
	}
	if _, err := alias.Preparation(); err == nil {
		t.Fatal("retired projection accepted")
	}
	if _, err := p.RequiredLocks(); err == nil {
		t.Fatal("retired locks accepted")
	}
	if intent.Validate() != nil {
		t.Fatal("retirement destroyed caller intent")
	}

}
