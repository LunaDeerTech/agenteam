package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func independentIntentID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func independentIntentRequestFields(t *testing.T, kind sc.MutationKind) sc.ProjectVariableWriteFields {
	t.Helper()
	actor, err := i.NewHuman(independentIntentID[i.User](t, 1), independentIntentID[i.Session](t, 2))
	if err != nil {
		t.Fatal(err)
	}
	project := independentIntentID[i.Project](t, 3)
	id, err := pv.SecretVariableCommandIdentity(project, pv.SecretCommandName("project.secret_variable."+string(kind)), "independent-key-canary")
	if err != nil {
		t.Fatal(err)
	}
	fields := sc.ProjectVariableWriteFields{Actor: actor, ProjectID: project, VariableID: independentIntentID[i.ProjectVariable](t, 4), Identity: id, Kind: kind}
	if kind != sc.Create {
		n := f.Version(13)
		fields.ExpectedVersion = &n
	}
	return fields
}

func independentIntentRequest(t *testing.T, kind sc.MutationKind) sc.ProjectVariableWriteRequest {
	t.Helper()
	r, err := sc.NewProjectVariableWriteRequest(independentIntentRequestFields(t, kind))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func independentValueIntent(t *testing.T) (sc.ProjectVariableIntent, sc.SecretMaterial, []byte) {
	t.Helper()
	want := []byte(" \t独立 e\u0301 \r\n value-canary ")
	m, err := sc.NewSecretMaterial(want)
	if err != nil {
		t.Fatal(err)
	}
	v, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Value: &m})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Destroy)
	t.Cleanup(v.Destroy)
	return v, m, want
}

func TestIndependentSecretIntentOwnedBorrowAndAliasRetirement(t *testing.T) {
	v, original, want := independentValueIntent(t)
	clone, err := v.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Destroy()
	alias := v
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var borrowed []byte
	go func() {
		done <- v.UseValue(func(value []byte) error {
			borrowed = value
			close(entered)
			<-release
			if !bytes.Equal(value, want) {
				return errors.New("active borrow changed")
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("borrow did not enter")
	}
	original.Destroy()
	alias.Destroy()
	called := false
	if v.Validate() == nil || v.UseValue(func([]byte) error { called = true; return nil }) == nil || called {
		t.Fatal("copy alias did not retire new borrows")
	}
	if _, err = alias.Clone(); err == nil {
		t.Fatal("retired alias cloned")
	}
	// Destroy is not callback join. The old private synchronous borrow remains
	// owned by that invocation and is cleared only on its actual return.
	close(release)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("borrow did not join")
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("joined borrow not zeroed")
	}
	if err = clone.UseValue(func(b []byte) error {
		if !bytes.Equal(b, want) {
			return errors.New("independent clone changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var panicBorrow []byte
	func() {
		defer func() {
			if recover() != "controlled-panic" {
				t.Error("panic identity changed")
			}
		}()
		_ = clone.UseValue(func(b []byte) error { panicBorrow = b; panic("controlled-panic") })
	}()
	if !bytes.Equal(panicBorrow, make([]byte, len(panicBorrow))) {
		t.Fatal("panic borrow not zeroed")
	}
	if err = clone.UseValue(func(b []byte) error { b[0] = 'X'; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = clone.UseValue(func(b []byte) error {
		if !bytes.Equal(b, want) {
			return errors.New("borrow modified owned material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if clone.UseValue(nil) == nil {
		t.Fatal("nil callback accepted")
	}
}

func TestIndependentSecretIntentConcurrentCopiesAndSafeSurfaces(t *testing.T) {
	v, _, want := independentValueIntent(t)
	name, desc := "NAME_CANARY", "DESCRIPTION_CANARY"
	material, err := sc.NewSecretMaterial(want)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	fields := sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Name: &name, Description: &desc, Value: &material}
	withMetadata, err := sc.NewProjectVariableIntent(fields)
	if err != nil {
		t.Fatal(err)
	}
	defer withMetadata.Destroy()
	values := []any{withMetadata, &withMetadata, fields, &fields, withMetadata.Fields(), withMetadata.Fields().Request, withMetadata.Fields().Request.Fields(), map[string]any{"v": withMetadata, "f": fields}, []sc.ProjectVariableIntent{withMetadata}, struct {
		hidden sc.ProjectVariableIntentFields
	}{fields}}
	for _, value := range values {
		var output bytes.Buffer
		for _, format := range []string{"%v", "%+v", "%#v", "%q", "%s", "%x"} {
			fmt.Fprintf(&output, format, value)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("safe JSON failed")
		}
		output.Write(raw)
		slog.New(slog.NewTextHandler(&output, nil)).Info("projection", "v", value)
		for _, canary := range []string{name, desc, string(want), "independent-key-canary", fields.Request.Fields().ProjectID.String()} {
			if strings.Contains(output.String(), canary) {
				t.Fatal("nested formatting leaked a protected field")
			}
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"data":"material-canary"}`, `"project_variable_secret_write"`} {
		request := fields.Request
		if json.Unmarshal([]byte(raw), &withMetadata) == nil || json.Unmarshal([]byte(raw), &request) == nil {
			t.Fatal("opaque decode accepted")
		}
		if withMetadata.Validate() != nil || request.Validate() != nil {
			t.Fatal("failed decode changed live object")
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for j := 0; j < 16; j++ {
				if n%3 == 0 {
					v.Destroy()
					continue
				}
				_ = v.Validate()
				_ = v.Fields()
				_ = v.UseValue(func(b []byte) error {
					if !bytes.Equal(b, want) {
						t.Error("concurrent borrow corrupted")
					}
					return nil
				})
				c, err := v.Clone()
				if err == nil {
					_ = c.UseValue(func(b []byte) error {
						if !bytes.Equal(b, want) {
							t.Error("concurrent clone corrupted")
						}
						return nil
					})
					c.Destroy()
				}
			}
		}(n)
	}
	close(start)
	wg.Wait()
	if v.Validate() == nil {
		t.Fatal("final destruction missing")
	}
}

func TestIndependentSecretIntentD10UpdateDeleteAndStructuralFailure(t *testing.T) {
	want := []byte("  \r\n原始 e\u0301 \t ")
	m, err := sc.NewSecretMaterial(want)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	a, err := pv.NewSecretVariableUpdate(pv.SecretVariableUpdateFields{Value: &m})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Destroy()
	var v sc.ProjectVariableIntent
	err = a.UseValue(func(b []byte) error {
		material, err := sc.NewSecretMaterial(b)
		if err != nil {
			return err
		}
		defer material.Destroy()
		metadata := a.Fields()
		v, err = sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Name: metadata.Name, Description: metadata.Description, Value: &material})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Destroy()
	a.Destroy()
	m.Destroy()
	if err = v.UseValue(func(b []byte) error {
		if !bytes.Equal(b, want) {
			return errors.New("D10 adapter normalized material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if *v.Fields().Request.Fields().ExpectedVersion != 13 || v.Fields().Name != nil || v.Fields().Description != nil || !v.Fields().ValuePresent {
		t.Fatal("D10 presence/expected changed")
	}
	deleted, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Delete)})
	if err != nil || deleted.Validate() != nil {
		t.Fatal("delete request rejected")
	}
	called := false
	if deleted.UseValue(func([]byte) error { called = true; return nil }) == nil || called {
		t.Fatal("delete acquired material")
	}
	deleted.Destroy() // No value to clear; this is not the prepared capability.
	if deleted.Fields().ValuePresent {
		t.Fatal("delete gained material")
	}
	for _, name := range []string{"", strings.Repeat("a", 129), "bad\x00name", string([]byte{0xff})} {
		_, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Name: &name})
		if err == nil || strings.Contains(err.Error(), name) && name != "" {
			t.Fatal("invalid text accepted or reflected")
		}
	}
	badDesc := strings.Repeat("d", 4097)
	if _, err = sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Description: &badDesc}); err == nil {
		t.Fatal("unbounded description accepted")
	}
	for _, raw := range [][]byte{{0xed, 0xa0, 0x80}, {0x00}, {0xc0, 0x80}} {
		material, err := sc.NewSecretMaterial(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: independentIntentRequest(t, sc.Update), Value: &material})
		material.Destroy()
		if err == nil {
			t.Fatal("non-scalar material accepted")
		}
	}
}

func TestIndependentSecretIntentClosedIdentityAndOriginalRequest(t *testing.T) {
	base := independentIntentRequestFields(t, sc.Update)
	wantActor, wantIdentity := base.Actor, base.Identity.Canonical()
	request, err := sc.NewProjectVariableWriteRequest(base)
	if err != nil {
		t.Fatal(err)
	}
	*base.ExpectedVersion = 99
	project := base.ProjectID
	for _, identity := range []struct {
		ns      string
		owners  []string
		command string
	}{
		{"projectvariable", []string{project.String()}, "project.variable.update"},
		{"projectvariable", []string{project.String()}, "project.secret_variable.create"},
		{"secret", []string{project.String()}, "project.secret_variable.update"},
		{"projectvariable", nil, "project.secret_variable.update"},
		{"projectvariable", []string{project.String(), project.String()}, "project.secret_variable.update"},
		{"projectvariable", []string{independentIntentID[i.Project](t, 9).String()}, "project.secret_variable.update"},
	} {
		changed := independentIntentRequestFields(t, sc.Update)
		changed.Identity, err = f.NewCommandIdentity(identity.ns, identity.owners, identity.command, "new-key")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = sc.NewProjectVariableWriteRequest(changed); err == nil {
			t.Fatal("cross-use identity accepted")
		}
	}
	owners := request.Fields().Identity.OwnerIDs()
	owners[0] = independentIntentID[i.Project](t, 9).String()
	returned := request.Fields()
	*returned.ExpectedVersion = 100
	if request.Fields().Identity.Canonical() != wantIdentity || !request.Fields().Actor.Equal(wantActor) || *request.Fields().ExpectedVersion != 13 {
		t.Fatal("original authority request changed")
	}
	registration, err := i.RegisterService(i.SecretService)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(independentIntentID[i.User](t, 10).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	changed := independentIntentRequestFields(t, sc.Update)
	changed.Actor = service
	if _, err = sc.NewProjectVariableWriteRequest(changed); err == nil {
		t.Fatal("service role substituted for Human")
	}
	for _, purpose := range []sc.Purpose{"project_variable", "secret_variable", "projectvariable"} {
		if purpose.Valid() {
			t.Fatal("legacy purpose broadened")
		}
	}
}
