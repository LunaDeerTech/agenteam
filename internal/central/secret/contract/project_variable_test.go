package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func pvID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func pvWrite(t *testing.T, kind sc.MutationKind) sc.ProjectVariableWriteFields {
	t.Helper()
	actor, err := i.NewHuman(pvID[i.User](t, 1), pvID[i.Session](t, 2))
	if err != nil {
		t.Fatal(err)
	}
	project := pvID[i.Project](t, 3)
	command, err := pv.SecretVariableCommandIdentity(project, pv.SecretCommandName("project.secret_variable."+string(kind)), "private-command-canary")
	if err != nil {
		t.Fatal(err)
	}
	v := sc.ProjectVariableWriteFields{Actor: actor, ProjectID: project, VariableID: pvID[i.ProjectVariable](t, 4), Identity: command, Kind: kind}
	if kind != sc.Create {
		expected := f.Version(7)
		v.ExpectedVersion = &expected
	}
	return v
}
func pvRequest(t *testing.T, kind sc.MutationKind) sc.ProjectVariableWriteRequest {
	t.Helper()
	v, err := sc.NewProjectVariableWriteRequest(pvWrite(t, kind))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func pvMaterial(t *testing.T, b []byte) sc.SecretMaterial {
	t.Helper()
	m, err := sc.NewSecretMaterial(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Destroy)
	return m
}

func TestProjectVariableRequestOriginalIdentityAndClosedShape(t *testing.T) {
	for _, kind := range []sc.MutationKind{sc.Create, sc.Update, sc.Delete} {
		t.Run(string(kind), func(t *testing.T) {
			base := pvWrite(t, kind)
			if got, err := sc.NewProjectVariableWriteRequest(base); err != nil || got.Validate() != nil {
				t.Fatal("D10 original identity rejected")
			}
			changes := []func(*sc.ProjectVariableWriteFields){
				func(v *sc.ProjectVariableWriteFields) { v.Actor = i.Actor{} },
				func(v *sc.ProjectVariableWriteFields) { v.ProjectID = pvID[i.Project](t, 99) },
				func(v *sc.ProjectVariableWriteFields) { v.VariableID = i.ProjectVariableID{} },
				func(v *sc.ProjectVariableWriteFields) { v.Kind = "replace" },
				func(v *sc.ProjectVariableWriteFields) {
					v.Identity, _ = f.NewCommandIdentity("secret", []string{v.ProjectID.String()}, string(kind), "key")
				},
				func(v *sc.ProjectVariableWriteFields) {
					v.Identity, _ = f.NewCommandIdentity("projectvariable", []string{v.ProjectID.String(), pvID[i.User](t, 1).String()}, "project.secret_variable."+string(kind), "key")
				},
				func(v *sc.ProjectVariableWriteFields) {
					v.Identity, _ = f.NewCommandIdentity("projectvariable", []string{v.ProjectID.String()}, "project.secret_variable.replace", "key")
				},
				func(v *sc.ProjectVariableWriteFields) {
					v.Actor, _ = i.NewAgentRun(v.ProjectID, pvID[i.Agent](t, 5), pvID[i.Execution](t, 6))
				},
				func(v *sc.ProjectVariableWriteFields) {
					if kind == sc.Create {
						n := f.Version(1)
						v.ExpectedVersion = &n
					} else {
						v.ExpectedVersion = nil
					}
				},
				func(v *sc.ProjectVariableWriteFields) { n := f.Version(0); v.ExpectedVersion = &n },
			}
			for n, change := range changes {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					v := pvWrite(t, kind)
					change(&v)
					if _, err := sc.NewProjectVariableWriteRequest(v); err == nil {
						t.Fatal("invalid request accepted")
					}
				})
			}
		})
	}
	if (sc.ProjectVariableWriteRequest{}).Validate() == nil || sc.Purpose("project_variable").Valid() {
		t.Fatal("zero request or legacy purpose expanded")
	}
}

func TestProjectVariableIntentPresenceAndMaterialBoundary(t *testing.T) {
	name, description := "TOKEN", ""
	material := pvMaterial(t, []byte("value"))
	for mask := 0; mask < 8; mask++ {
		for _, kind := range []sc.MutationKind{sc.Create, sc.Update, sc.Delete} {
			t.Run(fmt.Sprintf("%s/%d", kind, mask), func(t *testing.T) {
				fields := sc.ProjectVariableIntentFields{Request: pvRequest(t, kind)}
				if mask&1 != 0 {
					fields.Name = &name
				}
				if mask&2 != 0 {
					fields.Description = &description
				}
				if mask&4 != 0 {
					fields.Value = &material
				}
				intent, err := sc.NewProjectVariableIntent(fields)
				want := kind == sc.Create && mask == 7 || kind == sc.Update && mask != 0 || kind == sc.Delete && mask == 0
				if (err == nil) != want {
					t.Fatal("incorrect field presence")
				}
				if want {
					defer intent.Destroy()
					if intent.Validate() != nil {
						t.Fatal("valid intent invalid")
					}
					if mask&4 == 0 && intent.UseValue(func([]byte) error { return nil }) == nil {
						t.Fatal("absent material readable")
					}
				}
			})
		}
	}
	for _, b := range [][]byte{{0}, {0xff}, []byte("x\x00y")} {
		m := pvMaterial(t, b)
		if _, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: pvRequest(t, sc.Update), Value: &m}); err == nil {
			t.Fatal("non-wire material accepted")
		}
	}
	max := pvMaterial(t, bytes.Repeat([]byte("x"), sc.MaxValueBytes))
	intent, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: pvRequest(t, sc.Update), Value: &max})
	if err != nil {
		t.Fatal("maximum value rejected")
	}
	intent.Destroy()
	zero := sc.SecretMaterial{}
	if _, err = sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: pvRequest(t, sc.Update), Value: &zero}); err == nil {
		t.Fatal("absent handle accepted as present value")
	}
	if (sc.ProjectVariableIntent{}).Validate() == nil {
		t.Fatal("zero intent accepted")
	}
}

func TestProjectVariableIntentIndependentCopiesAndBorrowLifetime(t *testing.T) {
	base := pvWrite(t, sc.Update)
	request, err := sc.NewProjectVariableWriteRequest(base)
	if err != nil {
		t.Fatal(err)
	}
	*base.ExpectedVersion = 99
	out := request.Fields()
	*out.ExpectedVersion = 100
	if *request.Fields().ExpectedVersion != 7 {
		t.Fatal("request aliases expected version")
	}
	name, description := "TOKEN", "description-canary"
	material := pvMaterial(t, []byte("secret-value-canary"))
	intent, err := sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: request, Name: &name, Description: &description, Value: &material})
	if err != nil {
		t.Fatal(err)
	}
	defer intent.Destroy()
	name = "changed"
	description = "changed"
	material.Destroy()
	meta := intent.Fields()
	*meta.Name = "again"
	*meta.Description = "again"
	if *intent.Fields().Name != "TOKEN" || *intent.Fields().Description != "description-canary" {
		t.Fatal("metadata aliases caller")
	}
	clone, err := intent.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Destroy()
	var borrowed []byte
	callbackErr := errors.New("callback-only")
	if err = intent.UseValue(func(value []byte) error { borrowed = value; return callbackErr }); err != callbackErr {
		t.Fatal("callback error changed")
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("borrow not zeroed")
	}
	intent.Destroy()
	if intent.Validate() == nil || intent.UseValue(func([]byte) error { return nil }) == nil {
		t.Fatal("destroyed material survived")
	}
	if err = clone.UseValue(func(value []byte) error {
		if string(value) != "secret-value-canary" {
			t.Fatal("clone lost material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectVariableIntentSafeSurfacesAndNoDeserialization(t *testing.T) {
	name, description := "NAME_CANARY", "description-canary"
	material := pvMaterial(t, []byte("secret-value-canary"))
	request := pvRequest(t, sc.Create)
	fields := sc.ProjectVariableIntentFields{Request: request, Name: &name, Description: &description, Value: &material}
	intent, err := sc.NewProjectVariableIntent(fields)
	if err != nil {
		t.Fatal(err)
	}
	defer intent.Destroy()
	var out bytes.Buffer
	for _, v := range []any{request, &request, request.Fields(), intent, &intent, fields, intent.Fields(), struct{ Request sc.ProjectVariableIntent }{intent}, struct{ request sc.ProjectVariableIntent }{intent}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&out, format, v)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(raw)
		slog.New(slog.NewJSONHandler(&out, nil)).Info("safe", "value", v)
	}
	for _, canary := range []string{name, description, "secret-value-canary", "private-command-canary", request.Fields().ProjectID.String()} {
		if strings.Contains(out.String(), canary) {
			t.Fatal("unsafe projection")
		}
	}
	for _, raw := range []string{`{}`, `null`, `"project_variable_secret_write"`} {
		if json.Unmarshal([]byte(raw), &intent) == nil || json.Unmarshal([]byte(raw), &request) == nil {
			t.Fatal("JSON fabricated intent/request")
		}
	}
	if intent.Validate() != nil || request.Validate() != nil {
		t.Fatal("failed decode corrupted existing handle")
	}
}

func TestProjectVariableIntentConsumesActualD10Contract(t *testing.T) {
	material := pvMaterial(t, []byte("actual-a-contract"))
	a, err := pv.NewSecretVariableCreate(pv.SecretVariableCreateFields{ID: pvID[i.ProjectVariable](t, 4), Name: "TOKEN", Description: "", Value: material})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Destroy()
	var intent sc.ProjectVariableIntent
	err = a.UseValue(func(value []byte) error {
		m, err := sc.NewSecretMaterial(value)
		if err != nil {
			return err
		}
		defer m.Destroy()
		meta := a.Fields()
		intent, err = sc.NewProjectVariableIntent(sc.ProjectVariableIntentFields{Request: pvRequest(t, sc.Create), Name: &meta.Name, Description: &meta.Description, Value: &m})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer intent.Destroy()
	a.Destroy()
	if err = intent.UseValue(func(value []byte) error {
		if string(value) != "actual-a-contract" {
			t.Fatal("adapter changed raw bytes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
